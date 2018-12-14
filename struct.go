package main

// Elasticsearch Node Stats Structs
import (
	"database/sql"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

type columnUsage int

const (
	LABEL        columnUsage = iota // Use this column as a label
	COUNTER      columnUsage = iota // Use this column as a counter
	GAUGE        columnUsage = iota // Use this column as a gauge
)

// Groups metric maps under a shared set of labels
type MetricMapNamespace struct {
	columnMappings map[string]MetricMap // Column mappings in this namespace
	labels []string
}

// Stores the prometheus metric description which a given column will be mapped
// to by the collector
type MetricMap struct {
	vtype      prometheus.ValueType // Prometheus valuetype
	namespace  string
	desc       *prometheus.Desc                  // Prometheus descriptor
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

	metricMap map[string]MetricMapNamespace

	db *sql.DB
}
