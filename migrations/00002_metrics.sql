-- +goose Up
-- Metric tables are range-partitioned by ts; partitions are created and dropped by the server scheduler.
CREATE TABLE metrics_raw (
    server_id uuid        NOT NULL,
    ts        timestamptz NOT NULL,
    cpu       real        NOT NULL,
    mem       real        NOT NULL,
    disk      real        NOT NULL,
    rx        real        NOT NULL,
    tx        real        NOT NULL,
    load1     real        NOT NULL DEFAULT 0,
    PRIMARY KEY (server_id, ts)
) PARTITION BY RANGE (ts);

CREATE TABLE metrics_1m (
    server_id uuid NOT NULL, ts timestamptz NOT NULL, samples integer NOT NULL DEFAULT 0,
    cpu_avg real NOT NULL, cpu_min real NOT NULL, cpu_max real NOT NULL, cpu_p95 real NOT NULL,
    mem_avg real NOT NULL, mem_min real NOT NULL, mem_max real NOT NULL, mem_p95 real NOT NULL,
    disk_avg real NOT NULL, disk_min real NOT NULL, disk_max real NOT NULL, disk_p95 real NOT NULL,
    rx_avg real NOT NULL, rx_min real NOT NULL, rx_max real NOT NULL, rx_p95 real NOT NULL,
    tx_avg real NOT NULL, tx_min real NOT NULL, tx_max real NOT NULL, tx_p95 real NOT NULL,
    PRIMARY KEY (server_id, ts)
) PARTITION BY RANGE (ts);

CREATE TABLE metrics_5m  (LIKE metrics_1m INCLUDING DEFAULTS, PRIMARY KEY (server_id, ts)) PARTITION BY RANGE (ts);
CREATE TABLE metrics_15m (LIKE metrics_1m INCLUDING DEFAULTS, PRIMARY KEY (server_id, ts)) PARTITION BY RANGE (ts);
CREATE TABLE metrics_1h  (LIKE metrics_1m INCLUDING DEFAULTS, PRIMARY KEY (server_id, ts)) PARTITION BY RANGE (ts);

CREATE TABLE events (
    id        bigserial PRIMARY KEY,
    server_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    ts        timestamptz NOT NULL DEFAULT now(),
    kind      text NOT NULL,
    message   text NOT NULL DEFAULT ''
);
CREATE INDEX events_server_ts_idx ON events (server_id, ts DESC);

ALTER TABLE servers
    ADD COLUMN last_sample jsonb,
    ADD COLUMN system_info jsonb;
