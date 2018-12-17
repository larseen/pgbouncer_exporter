# PgBouncer exporter

Prometheus exporter for PgBouncer.
Exports metrics at `9127/metrics`

## Building and running

    make
    ./pgbouncer_exporter <flags>

To see all available configuration flags:

    ./pgbouncer_exporter -h


## Metrics

Metric | Description
-------|------------
config_application_name_add_host | Whether pgbouncer add the client host address and port to the application name setting set on connection start or not
config_autodb_idle_timeout | Unused pools created via '*' are reclaimed after this interval
config_client_idle_timeout | Client connections idling longer than this many seconds are closed
config_client_login_timeout | Maximum time in seconds for a client to either login, or be disconnected
config_default_pool_size | The default for how many server connections to allow per user/database pair
config_disable_pqexec | Boolean; 1 means pgbouncer enforce Simple Query Protocol; 0 means it allows multiple queries in a single packet
config_dns_max_ttl | Irregardless of DNS TTL, this is the TTL that pgbouncer enforces for dns lookups it does for backends
config_dns_nxdomain_ttl | Irregardless of DNS TTL, this is the period enforced for negative DNS answers
config_dns_zone_check_period | Period to check if zone serial has changed.
config_idle_transaction_timeout | If client has been in 'idle in transaction' state longer than this amount in seconds, it will be disconnected.
config_listen_backlog | Maximum number of backlogged listen connections before further connection attempts are dropped
config_log_connections | Whether connections are logged or not.
config_log_disconnections | Whether connection disconnects are logged.
config_log_pooler_errors | Whether pooler errors are logged or not
config_max_client_conn | Maximum number of client connections allowed
config_max_db_connections | Server level maximum connections enforced for a given db, irregardless of pool limits
config_max_packet_size | Maximum packet size for postgresql packets that pgbouncer will relay to backends
config_max_user_connections | Maximum number of connections a user can open irregardless of pool limits
config_min_pool_size | Mininum number of backends a pool will always retain.
config_pkt_buf | Internal buffer size for packets.  See docs
config_query_timeout | Maximum time that a query can run for before being cancelled.
config_query_wait_timeout | Maximum time that a query can wait to be executed before being cancelled.
config_reserve_pool_size | How many additional connections to allow to a pool once it's crossed it's maximum
config_reserve_pool_timeout | If a client has not been serviced in this many seconds, pgbouncer enables use of additional connections from reserve pool.
config_sbuf_loopcnt | How many results to process for a given connection's packet results before switching to others to ensure fairness.  See docs.
config_server_check_delay | How long to keep released connections available for immediate re-use, without running sanity-check queries on it. If 0 then the query is ran always.
config_server_connect_timeout | Maximum time allowed for connecting and logging into a backend server
config_server_idle_timeout | If a server connection has been idle more than this many seconds it will be dropped
config_server_lifetime | The pooler will close an unused server connection that has been connected longer than this many seconds
config_server_login_retry | If connecting to a backend failed, this is the wait interval in seconds before retrying
config_server_reset_query_always | Boolean indicating whether or not server_reset_query is enforced for all pooling modes, or just session
config_server_round_robin | Boolean; if 1, pgbouncer uses backends in a round robin fashion.  If 0, it uses LIFO to minimize connectivity to backends
config_stats_period | Periodicity in seconds of pgbouncer recalculating internal stats_
config_suspend_timeout | Timeout for how long pgbouncer waits for buffer flushes before killing connections during pgbouncer admin SHUTDOWN and SUSPEND invocations.
config_tcp_defer_accept | Configurable for TCP_DEFER_ACCEPT
config_tcp_keepcnt | See TCP documentation for this field
config_tcp_keepidle | See TCP documentation for this field
config_tcp_keepintvl | See TCP documentation for this field
config_tcp_socket_buffer | Configurable for tcp socket buffering; 0 is kernel managed
config_tcpkeepalive | Boolean; if 1, tcp keepalive is enabled w/ OS defaults.  If 0, disabled.
config_verbose | If log verbosity is increased.  Only relevant as a metric if log volume begins exceeding log consumption
databases_current_connections | Current number of client connections
databases_disabled | Boolean indicating whether a pgbouncer DISABLE is currently active for this database
databases_max_connections | Maximum number of client connections allowed
databases_paused | Boolean indicating whether a pgbouncer PAUSE is currently active for this database
databases_pool_size | Maximum number of pool backend connections
databases_reserve_pool | Maximum amount that the pool size can be exceeded temporarily
lists_databases | Count of databases
lists_free_clients | Count of free clients
lists_free_servers | Count of free servers
lists_login_clients | Count of clients in login state
lists_pools | Count of pools
lists_used_clients | Count of used clients
lists_used_servers | Count of used servers
lists_users | Count of users
pools_cl_active | Client connections linked to server connection and able to process queries, shown as connection
pools_cl_waiting | Client connections waiting on a server connection, shown as connection
pools_maxwait | Age of oldest unserved client connection, shown as second
pools_sv_active | Server connections linked to a client connection, shown as connection
pools_sv_idle | Server connections idle and ready for a client query, shown as connection
pools_sv_login | Server connections currently in the process of logging in, shown as connection
pools_sv_tested | Server connections currently running either server_reset_query or server_check_query, shown as connection
pools_sv_used | Server connections idle more than server_check_delay, needing server_check_query, shown as connection
stats_avg_query | The average query duration, shown as microsecond
stats_avg_query_count | Average queries per second in last stat period
stats_avg_query_time | Average query duration in microseconds
stats_avg_recv | Average received (from clients) bytes per second
stats_avg_req | The average number of requests per second in last stat period, shown as request/second
stats_avg_sent | Average sent (to clients) bytes per second
stats_avg_wait_time | Time spent by clients waiting for a server in microseconds (average per second)
stats_avg_xact_count | Average transactions per second in last stat period
stats_avg_xact_time | Average transaction duration in microseconds
stats_bytes_received_per_second | The total network traffic received, shown as byte/second
stats_bytes_sent_per_second | The total network traffic sent, shown as byte/second
stats_total_query_count | Total number of SQL queries pooled
stats_total_query_time | Total number of microseconds spent by pgbouncer when actively connected to PostgreSQL, executing queries
stats_total_received | Total volume in bytes of network traffic received by pgbouncer, shown as bytes
stats_total_requests | Total number of SQL requests pooled by pgbouncer, shown as requests
stats_total_sent | Total volume in bytes of network traffic sent by pgbouncer, shown as bytes
stats_total_wait_time | Time spent by clients waiting for a server in microseconds
stats_total_xact_count | Total number of SQL transactions pooled
stats_total_xact_time | Total number of microseconds spent by pgbouncer when connected to PostgreSQL in a transaction, either idle in transaction or executing queries
