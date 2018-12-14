package main

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
)

type columnUsage int

const (
	LABEL    columnUsage = iota // Use this column as a label
	COUNTER  columnUsage = iota // Use this column as a counter
	GAUGE    columnUsage = iota // Use this column as a gauge
	GAUGE_MS columnUsage = iota // Use this column for gauges that are microsecond data
)

type rowResult struct {
	ColumnNames []string
	ColumnIdx   map[string]int
	ColumnData  []interface{}
}

type RowConverter func(*MetricMapNamespace, *rowResult, chan<- prometheus.Metric) ([]error, error)

// Groups metric maps under a shared set of labels
type MetricMapNamespace struct {
	namespace      string
	columnMappings map[string]MetricMap // Column mappings in this namespace
	labels         []string
	rowFunc        RowConverter
}

// the scrape fails, and a slice of errors if they were non-fatal.
func (m *MetricMapNamespace) Query(ch chan<- prometheus.Metric, db *sql.DB) ([]error, error) {
	query := fmt.Sprintf("SHOW %s;", m.namespace)

	// Don't fail on a bad scrape of one metric
	rows, err := db.Query(query)
	if err != nil {
		return []error{}, errors.New(fmt.Sprintln("Error running query on database: ", m.namespace, err))
	}

	defer rows.Close()

	var result rowResult
	result.ColumnNames, err = rows.Columns()
	if err != nil {
		return []error{}, errors.New(fmt.Sprintln("Error retrieving column list for: ", m.namespace, err))
	}

	// Make a lookup map for the column indices
	result.ColumnIdx = make(map[string]int, len(result.ColumnNames))
	for i, n := range result.ColumnNames {
		result.ColumnIdx[n] = i
	}

	result.ColumnData = make([]interface{}, len(result.ColumnNames))
	var scanArgs = make([]interface{}, len(result.ColumnNames))
	for i := range result.ColumnData {
		scanArgs[i] = &(result.ColumnData[i])
	}

	nonfatalErrors := []error{}

	for rows.Next() {
		err = rows.Scan(scanArgs...)
		if err != nil {
			return []error{}, errors.New(fmt.Sprintln("Error retrieving rows:", m.namespace, err))
		}

		n, e := m.rowFunc(m, &result, ch)
		if n != nil {
			nonfatalErrors = append(nonfatalErrors, n...)
		}
		if e != nil {
			return nonfatalErrors, e
		}
	}
	if err := rows.Err(); err != nil {
		log.Errorf("Failed scaning all rows due to scan failure: error was; %s", err)
		nonfatalErrors = append(nonfatalErrors, errors.New(fmt.Sprintf("Failed to consume all rows due to: %s", err)))
	}
	return nonfatalErrors, nil
}

func metricRowConverter(m *MetricMapNamespace, result *rowResult, ch chan<- prometheus.Metric) ([]error, error) {
	var nonFatalErrors []error
	labelValues := []string{}
	// collect label data first.
	for _, name := range m.labels {
		val := result.ColumnData[result.ColumnIdx[name]]
		if val == nil {
			labelValues = append(labelValues, "")
		} else if v, ok := val.(string); ok {
			labelValues = append(labelValues, v)
		} else if v, ok := val.(int64); ok {
			labelValues = append(labelValues, strconv.FormatInt(v, 10))
		}
	}

	for idx, columnName := range result.ColumnNames {
		if metricMapping, ok := m.columnMappings[columnName]; ok {
			value, ok := dbToFloat64(result.ColumnData[idx])
			if !ok {
				nonFatalErrors = append(nonFatalErrors, errors.New(fmt.Sprintln("Unexpected error parsing column: ", m.namespace, columnName, result.ColumnData[idx])))
				continue
			}
			log.Debugln("successfully parsed column:", m.namespace, columnName, result.ColumnData[idx])
			// Generate the metric
			ch <- prometheus.MustNewConstMetric(metricMapping.desc, metricMapping.vtype, value*metricMapping.multiplier, labelValues...)
		} else {
			log.Debugln("Ignoring column for metric conversion:", m.namespace, columnName)
		}
	}
	return nonFatalErrors, nil
}

func metricKVConverter(m *MetricMapNamespace, result *rowResult, ch chan<- prometheus.Metric) ([]error, error) {
	// format is key, value, <ignorable> for row results.
	if len(result.ColumnData) < 2 {
		return nil, errors.New(fmt.Sprintln("Received row results for KV parsing, but not enough columns; something is deeply broken:", m.namespace, result.ColumnData))
	}
	var key string
	switch v := result.ColumnData[0].(type) {
	case string:
		key = v
	default:
		return nil, errors.New(fmt.Sprintln("Received row results for KV parsing, but key field isn't string:", m.namespace, result.ColumnData))
	}
	// is it a key we care about?
	if metricMapping, ok := m.columnMappings[key]; ok {
		value, ok := dbToFloat64(result.ColumnData[1])
		if !ok {
			return append([]error{}, errors.New(fmt.Sprintln("Unexpected error KV value: ", m.namespace, key, result.ColumnData[1]))), nil
		}
		log.Debugln("successfully parsed column:", m.namespace, key, result.ColumnData[1])
		// Generate the metric
		ch <- prometheus.MustNewConstMetric(metricMapping.desc, metricMapping.vtype, value*metricMapping.multiplier)
	} else {
		log.Debugln("Ignoring column for KV conversion:", m.namespace, key)
	}
	return nil, nil
}

// Stores the prometheus metric description which a given column will be mapped
// to by the collector
type MetricMap struct {
	vtype      prometheus.ValueType // Prometheus valuetype
	namespace  string
	desc       *prometheus.Desc // Prometheus descriptor
	multiplier float64          // This is a multiplier to apply pgbouncer values in converting to prometheus norms.
}

type ColumnMapping struct {
	usage       columnUsage `yaml:"usage"`
	description string      `yaml:"description"`
}

// Exporter collects PgBouncer stats from the given server and exports
// them using the prometheus metrics package.
type Exporter struct {
	connectionString string
	namespace        string
	mutex            sync.RWMutex

	duration, up, error prometheus.Gauge
	totalScrapes        prometheus.Counter

	metricMap []*MetricMapNamespace

	db *sql.DB
}

var metricKVMaps = map[string]map[string]ColumnMapping{
	"config": {
		"listen_backlog":       {COUNTER, "Maximum number of backlogged listen connections before further connection attempts are dropped"},
		"max_client_conn":      {GAUGE, "Maximum number of client connections allowed"},
		"default_pool_size":    {GAUGE, "The default for how many server connections to allow per user/database pair"},
		"min_pool_size":        {GAUGE, "Mininum number of backends a pool will always retain."},
		"reserve_pool_size":    {GAUGE, "How many additional connections to allow to a pool once it's crossed it's maximum"},
		"reserve_pool_timeout": {GAUGE, "If a client has not been serviced in this many seconds, pgbouncer enables use of additional connections from reserve pool."},
		"max_db_connections":   {GAUGE, "Server level maximum connections enforced for a given db, irregardless of pool limits"},
		"max_user_connections": {GAUGE, "Maximum number of connections a user can open irregardless of pool limits"},
		"autodb_idle_timeout":  {GAUGE, "Unused pools created via '*' are reclaimed after this interval"},
		// server_reset_query should be enabled as a label for just this metric.
		"server_reset_query_always": {GAUGE, "Boolean indicating whether or not server_reset_query is enforced for all pooling modes, or just session"},
		// server_check_query should be enabled as a label for just this metric.
		"server_check_delay":        {GAUGE, "How long to keep released connections available for immediate re-use, without running sanity-check queries on it. If 0 then the query is ran always."},
		"query_timeout":             {GAUGE, "Maximum time that a query can run for before being cancelled."},
		"query_wait_timeout":        {GAUGE, "Maximum time that a query can wait to be executed before being cancelled."},
		"client_idle_timeout":       {GAUGE, "Client connections idling longer than this many seconds are closed"},
		"client_login_timeout":      {GAUGE, "Maximum time in seconds for a client to either login, or be disconnected"},
		"idle_transaction_timeout":  {GAUGE, "If client has been in 'idle in transaction' state longer than this amount in seconds, it will be disconnected."},
		"server_lifetime":           {GAUGE, "The pooler will close an unused server connection that has been connected longer than this many seconds"},
		"server_idle_timeout":       {GAUGE, "If a server connection has been idle more than this many seconds it will be dropped"},
		"server_connect_timeout":    {GAUGE, "Maximum time allowed for connecting and logging into a backend server"},
		"server_login_retry":        {GAUGE, "If connecting to a backend failed, this is the wait interval in seconds before retrying"},
		"server_round_robin":        {GAUGE, "Boolean; if 1, pgbouncer uses backends in a round robin fashion.  If 0, it uses LIFO to minimize connectivity to backends"},
		"suspend_timeout":           {GAUGE, "Timeout for how long pgbouncer waits for buffer flushes before killing connections during pgbouncer admin SHUTDOWN and SUSPEND invocations."},
		"disable_pqexec":            {COUNTER, "Boolean; 1 means pgbouncer enforce Simple Query Protocol; 0 means it allows multiple queries in a single packet"},
		"dns_max_ttl":               {GAUGE, "Irregardless of DNS TTL, this is the TTL that pgbouncer enforces for dns lookups it does for backends"},
		"dns_nxdomain_ttl":          {GAUGE, "Irregardless of DNS TTL, this is the period enforced for negative DNS answers"},
		"dns_zone_check_period":     {GAUGE, "Period to check if zone serial has changed."},
		"max_packet_size":           {GAUGE, "Maximum packet size for postgresql packets that pgbouncer will relay to backends"},
		"pkt_buf":                   {COUNTER, "Internal buffer size for packets.  See docs"},
		"sbuf_loopcnt":              {GAUGE, "How many results to process for a given connection's packet results before switching to others to ensure fairness.  See docs."},
		"tcp_defer_accept":          {GAUGE, "Configurable for TCP_DEFER_ACCEPT"},
		"tcp_socket_buffer":         {GAUGE, "Configurable for tcp socket buffering; 0 is kernel managed"},
		"tcpkeepalive":              {GAUGE, "Boolean; if 1, tcp keepalive is enabled w/ OS defaults.  If 0, disabled."},
		"tcp_keepcnt":               {GAUGE, "See TCP documentation for this field"},
		"tcp_keepidle":              {GAUGE, "See TCP documentation for this field"},
		"tcp_keepintvl":             {GAUGE, "See TCP documentation for this field"},
		"verbose":                   {GAUGE, "If log verbosity is increased.  Only relevant as a metric if log volume begins exceeding log consumption"},
		"stats_period":              {GAUGE, "Periodicity in seconds of pgbouncer recalculating internal stats."},
		"log_connections":           {GAUGE, "Whether connections are logged or not."},
		"log_disconnections":        {GAUGE, "Whether connection disconnects are logged."},
		"log_pooler_errors":         {GAUGE, "Whether pooler errors are logged or not"},
		"application_name_add_host": {GAUGE, "Whether pgbouncer add the client host address and port to the application name setting set on connection start or not"},
	},
}

var metricRowMaps = map[string]map[string]ColumnMapping{
	"databases": {
		"name":                {LABEL, ""},
		"host":                {LABEL, ""},
		"port":                {LABEL, ""},
		"database":            {LABEL, ""},
		"force_user":          {LABEL, ""},
		"pool_size":           {GAUGE, "Maximum number of pool backend connections"},
		"reserve_pool":        {GAUGE, "Maximum amount that the pool size can be exceeded temporarily"},
		"pool_mode":           {LABEL, ""},
		"max_connections":     {GAUGE, "Maximum number of client connections allowed"},
		"current_connections": {GAUGE, "Current number of client connections"},
		"paused":              {GAUGE, "Boolean indicating whether a pgbouncer PAUSE is currently active for this database"},
		"disabled":            {GAUGE, "Boolean indicating whether a pgbouncer DISABLE is currently active for this database"},
	},
	"lists": {
		"databases":     {GAUGE, "Count of databases"},
		"users":         {GAUGE, "Count of users"},
		"pools":         {GAUGE, "Count of pools"},
		"free_clients":  {GAUGE, "Count of free clients"},
		"used_clients":  {GAUGE, "Count of used clients"},
		"login_clients": {GAUGE, "Count of clients in login state"},
		"free_servers":  {GAUGE, "Count of free servers"},
		"used_servers":  {GAUGE, "Count of used servers"},
	},
	"pools": {
		"database":   {LABEL, ""},
		"user":       {LABEL, ""},
		"cl_active":  {GAUGE, "Client connections linked to server connection and able to process queries, shown as connection"},
		"cl_waiting": {GAUGE, "Client connections waiting on a server connection, shown as connection"},
		"sv_active":  {GAUGE, "Server connections linked to a client connection, shown as connection"},
		"sv_idle":    {GAUGE, "Server connections idle and ready for a client query, shown as connection"},
		"sv_used":    {GAUGE, "Server connections idle more than server_check_delay, needing server_check_query, shown as connection"},
		"sv_tested":  {GAUGE, "Server connections currently running either server_reset_query or server_check_query, shown as connection"},
		"sv_login":   {GAUGE, "Server connections currently in the process of logging in, shown as connection"},
		"maxwait":    {GAUGE, "Age of oldest unserved client connection, shown as second"},
		"pool_mode":  {LABEL, ""},
	},
	"stats": {
		"database":                  {LABEL, ""},
		"avg_query_count":           {GAUGE, "Average queries per second in last stat period"},
		"avg_query":                 {GAUGE_MS, "The average query duration, shown as microsecond"},
		"avg_query_time":            {GAUGE_MS, "Average query duration in microseconds"},
		"avg_recv":                  {GAUGE, "Average received (from clients) bytes per second"},
		"avg_req":                   {GAUGE, "The average number of requests per second in last stat period, shown as request/second"},
		"avg_sent":                  {GAUGE, "Average sent (to clients) bytes per second"},
		"avg_wait_time":             {GAUGE_MS, "Time spent by clients waiting for a server in microseconds (average per second)"},
		"avg_xact_count":            {GAUGE, "Average transactions per second in last stat period"},
		"avg_xact_time":             {GAUGE_MS, "Average transaction duration in microseconds"},
		"bytes_received_per_second": {GAUGE, "The total network traffic received, shown as byte/second"},
		"bytes_sent_per_second":     {GAUGE, "The total network traffic sent, shown as byte/second"},
		"total_query_count":         {GAUGE, "Total number of SQL queries pooled"},
		"total_query_time":          {GAUGE_MS, "Total number of microseconds spent by pgbouncer when actively connected to PostgreSQL, executing queries"},
		"total_received":            {GAUGE, "Total volume in bytes of network traffic received by pgbouncer, shown as bytes"},
		"total_requests":            {GAUGE, "Total number of SQL requests pooled by pgbouncer, shown as requests"},
		"total_sent":                {GAUGE, "Total volume in bytes of network traffic sent by pgbouncer, shown as bytes"},
		"total_wait_time":           {GAUGE_MS, "Time spent by clients waiting for a server in microseconds"},
		"total_xact_count":          {GAUGE, "Total number of SQL transactions pooled"},
		"total_xact_time":           {GAUGE_MS, "Total number of microseconds spent by pgbouncer when connected to PostgreSQL in a transaction, either idle in transaction or executing queries"},
	},
}

func NewExporter(connectionString string, namespace string) *Exporter {

	db, err := getDB(connectionString)

	if err != nil {
		log.Fatal(err)
	}

	return &Exporter{
		metricMap: makeDescMap(namespace),
		namespace: namespace,
		db:        db,
		up: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "up",
			Help:      "Was the PgBouncer instance query successful?",
		}),

		duration: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_scrape_duration_seconds",
			Help:      "Duration of the last scrape of metrics from PgBouncer.",
		}),

		totalScrapes: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "scrapes_total",
			Help:      "Total number of times PgBouncer has been scraped for metrics.",
		}),

		error: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_scrape_error",
			Help:      "Whether the last scrape of metrics from PgBouncer resulted in an error (1 for error, 0 for success).",
		}),
	}
}

// Query within a namespace mapping and emit metrics. Returns fatal errors if

func getDB(conn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", conn)
	if err != nil {
		return nil, err
	}
	err = db.Ping()
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	return db, nil
}

// Convert database.sql to string for Prometheus labels. Null types are mapped to empty strings.
func dbToString(t interface{}) (string, bool) {
	switch v := t.(type) {
	case int64:
		return fmt.Sprintf("%v", v), true
	case float64:
		return fmt.Sprintf("%v", v), true
	case time.Time:
		return fmt.Sprintf("%v", v.Unix()), true
	case nil:
		return "", true
	case []byte:
		// Try and convert to string
		return string(v), true
	case string:
		return v, true
	default:
		return "", false
	}
}

// Convert database.sql types to float64s for Prometheus consumption. Null types are mapped to NaN. string and []byte
// types are mapped as NaN and !ok
func dbToFloat64(t interface{}) (float64, bool) {
	switch v := t.(type) {
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case time.Time:
		return float64(v.Unix()), true
	case []byte:
		// Try and convert to string and then parse to a float64
		strV := string(v)
		result, err := strconv.ParseFloat(strV, 64)
		if err != nil {
			return math.NaN(), false
		}
		return result, true
	case string:
		result, err := strconv.ParseFloat(v, 64)
		if err != nil {
			log.Infoln("Could not parse string:", err)
			return math.NaN(), false
		}
		return result, true
	case nil:
		return math.NaN(), true
	default:
		return math.NaN(), false
	}
}

// Describe implements prometheus.Collector.
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	// We cannot know in advance what metrics the exporter will generate
	// from Postgres. So we use the poor man's describe method: Run a collect
	// and send the descriptors of all the collected metrics. The problem
	// here is that we need to connect to the Postgres DB. If it is currently
	// unavailable, the descriptors will be incomplete. Since this is a
	// stand-alone exporter and not used as a library within other code
	// implementing additional metrics, the worst that can happen is that we
	// don't detect inconsistent metrics created by this exporter
	// itself. Also, a change in the monitored Postgres instance may change the
	// exported metrics during the runtime of the exporter.

	metricCh := make(chan prometheus.Metric)
	doneCh := make(chan struct{})

	go func() {
		for m := range metricCh {
			ch <- m.Desc()
		}
		close(doneCh)
	}()

	e.Collect(metricCh)
	close(metricCh)
	<-doneCh
}

// Collect implements prometheus.Collector.
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	e.scrape(ch)
	ch <- e.duration
	ch <- e.up
	ch <- e.totalScrapes
	ch <- e.error
}

func (e *Exporter) scrape(ch chan<- prometheus.Metric) {
	defer func(begun time.Time) {
		e.duration.Set(time.Since(begun).Seconds())
		log.Info("Ending scrape")
	}(time.Now())
	log.Info("Starting scrape")

	e.error.Set(0)
	e.totalScrapes.Inc()

	e.mutex.RLock()
	defer e.mutex.RUnlock()

	for _, mapping := range e.metricMap {
		nonfatal, err := mapping.Query(ch, e.db)
		if len(nonfatal) > 0 {
			for _, suberr := range nonfatal {
				log.Errorln(suberr.Error())
			}
		}

		if err != nil {
			// this needs to be removed.
			log.Fatal(err)
		}
		e.error.Add(float64(len(nonfatal)))
	}
}

func makeDescMap(metricNamespace string) []*MetricMapNamespace {
	var metricMap []*MetricMapNamespace

	convert := func(namespace string, mappings map[string]ColumnMapping, converter RowConverter) *MetricMapNamespace {
		thisMap := make(map[string]MetricMap)

		labels := []string{}
		for columnName, columnMapping := range mappings {
			if columnMapping.usage == LABEL {
				labels = append(labels, columnName)
			}
		}
		for columnName, columnMapping := range mappings {
			// Determine how to convert the column based on its usage.
			desc := prometheus.NewDesc(fmt.Sprintf("%s_%s_%s", metricNamespace, namespace, columnName), columnMapping.description, labels, nil)
			switch columnMapping.usage {
			case COUNTER:
				thisMap[columnName] = MetricMap{
					vtype:      prometheus.CounterValue,
					desc:       desc,
					multiplier: 1,
				}
			case GAUGE:
				thisMap[columnName] = MetricMap{
					vtype:      prometheus.GaugeValue,
					desc:       desc,
					multiplier: 1,
				}
			case GAUGE_MS:
				thisMap[columnName] = MetricMap{
					vtype:      prometheus.GaugeValue,
					desc:       desc,
					multiplier: 1e-6,
				}
			}
		}
		return &MetricMapNamespace{namespace: namespace, columnMappings: thisMap, labels: labels, rowFunc: converter}
	}

	for namespace, mappings := range metricRowMaps {
		metricMap = append(metricMap, convert(namespace, mappings, metricRowConverter))
	}
	for namespace, mappings := range metricKVMaps {
		metricMap = append(metricMap, convert(namespace, mappings, metricKVConverter))
	}
	return metricMap
}
