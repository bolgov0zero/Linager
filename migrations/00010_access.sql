-- +goose Up
-- Journal of visits to the admin panel (page opened, sign-ins, requests refused by the access restriction).
-- Like the audit log it has no foreign keys. The restriction itself lives in the settings table (key "access").
CREATE TABLE access_log (
    id            bigserial PRIMARY KEY,
    ts            timestamptz NOT NULL DEFAULT now(),
    client_ip     text NOT NULL,
    remote_addr   text NOT NULL DEFAULT '',
    via_proxy     boolean NOT NULL DEFAULT false,
    proxy_trusted boolean NOT NULL DEFAULT false,
    event         text NOT NULL,
    login         text NOT NULL DEFAULT '',
    path          text NOT NULL DEFAULT '',
    user_agent    text NOT NULL DEFAULT '',
    reason        text NOT NULL DEFAULT ''
);
CREATE INDEX access_log_ts_idx ON access_log (ts DESC);
CREATE INDEX access_log_event_idx ON access_log (event, id DESC);
