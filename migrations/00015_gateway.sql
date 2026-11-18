-- +goose Up
-- The gateway module: an agent scans the networks it is told to and the panel keeps what it found.

-- One row per agent: is scanning on, how often, and the networks with the result of the last pass of each.
CREATE TABLE gateways (
    server_id  uuid PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    enabled    boolean NOT NULL DEFAULT false,
    interval_s integer NOT NULL DEFAULT 300,
    nets       jsonb NOT NULL DEFAULT '[]',
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by text NOT NULL DEFAULT '',
    data_at    timestamptz
);

-- A device found by an agent. In a network the agent is attached to it is known by its MAC address (so it keeps its
-- row when DHCP gives it another address), behind a router by its address.
CREATE TABLE gateway_devices (
    id         bigserial PRIMARY KEY,
    server_id  uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    net        text NOT NULL,
    ip         inet NOT NULL,
    mac        text NOT NULL DEFAULT '',
    name       text NOT NULL DEFAULT '',
    name_src   text NOT NULL DEFAULT '',
    own_name   text NOT NULL DEFAULT '',
    vendor     text NOT NULL DEFAULT '',
    random_mac boolean NOT NULL DEFAULT false,
    ports      jsonb NOT NULL DEFAULT '[]',
    via        text[] NOT NULL DEFAULT '{}',
    first_seen timestamptz NOT NULL DEFAULT now(),
    last_seen  timestamptz NOT NULL DEFAULT now(),
    online     boolean NOT NULL DEFAULT true,
    -- passes in a row in which the device did not answer; it is "gone" after two
    missed     integer NOT NULL DEFAULT 0,
    excluded   boolean NOT NULL DEFAULT false,
    -- found by the first pass of its network: everything is new then, so it is not shown as new
    initial    boolean NOT NULL DEFAULT false
);
CREATE INDEX gateway_devices_ip_idx ON gateway_devices (server_id, ip);
CREATE INDEX gateway_devices_mac_idx ON gateway_devices (server_id, mac) WHERE mac <> '';

-- Appeared and disappeared, kept for 90 days.
CREATE TABLE gateway_events (
    id        bigserial PRIMARY KEY,
    server_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    ts        timestamptz NOT NULL DEFAULT now(),
    kind      text NOT NULL,
    ip        inet NOT NULL,
    name      text NOT NULL DEFAULT '',
    vendor    text NOT NULL DEFAULT '',
    net       text NOT NULL
);
CREATE INDEX gateway_events_srv_idx ON gateway_events (server_id, ts DESC);
