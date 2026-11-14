-- +goose Up
CREATE TABLE roles (
    id   smallserial PRIMARY KEY,
    name text NOT NULL UNIQUE
);
INSERT INTO roles (name) VALUES ('admin');

CREATE TABLE users (
    id              bigserial PRIMARY KEY,
    login           text NOT NULL UNIQUE,
    password_hash   text NOT NULL,
    totp_secret     text,
    totp_enabled    boolean NOT NULL DEFAULT false,
    totp_last_step  bigint NOT NULL DEFAULT 0,
    failed_attempts integer NOT NULL DEFAULT 0,
    locked_until    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE user_roles (
    user_id bigint   NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id smallint NOT NULL REFERENCES roles(id),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE sessions (
    id         bytea PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf       text NOT NULL,
    ip         text NOT NULL DEFAULT '',
    user_agent text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen  timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

CREATE TABLE servers (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name          text NOT NULL UNIQUE,
    hostname      text NOT NULL DEFAULT '',
    address       text NOT NULL DEFAULT '',
    agent_pubkey  bytea NOT NULL UNIQUE,
    os            text NOT NULL DEFAULT '',
    agent_version text NOT NULL DEFAULT '',
    protocol      integer NOT NULL DEFAULT 1,
    modules       jsonb NOT NULL DEFAULT '[]',
    enrolled_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz
);

CREATE TABLE enrollments (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_pubkey  bytea NOT NULL,
    hostname      text NOT NULL,
    address       text NOT NULL DEFAULT '',
    remote_addr   text NOT NULL DEFAULT '',
    os            text NOT NULL DEFAULT '',
    agent_version text NOT NULL DEFAULT '',
    protocol      integer NOT NULL DEFAULT 1,
    nonce         bytea NOT NULL,
    attempts      integer NOT NULL DEFAULT 0,
    status        text NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'approved', 'rejected', 'expired')),
    cert_pem      text,
    server_id     uuid REFERENCES servers(id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL
);
CREATE INDEX enrollments_status_idx ON enrollments (status, expires_at);

CREATE TABLE dashboards (
    id         bigserial PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       text NOT NULL DEFAULT 'default',
    UNIQUE (user_id, name)
);

CREATE TABLE dashboard_items (
    dashboard_id bigint NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
    server_id    uuid   NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    position     integer NOT NULL,
    PRIMARY KEY (dashboard_id, server_id)
);

CREATE TABLE commands (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id   uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    type        text NOT NULL,
    params      jsonb NOT NULL DEFAULT '{}',
    status      text NOT NULL DEFAULT 'queued',
    result      jsonb,
    created_by  bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz,
    finished_at timestamptz
);
CREATE INDEX commands_server_idx ON commands (server_id, status);

-- Audit rows deliberately have no foreign keys: they must survive deletion of users and servers.
CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    ts          timestamptz NOT NULL DEFAULT now(),
    user_id     bigint,
    actor       text NOT NULL DEFAULT '',
    action      text NOT NULL,
    target_type text NOT NULL DEFAULT '',
    target_id   text NOT NULL DEFAULT '',
    target_name text NOT NULL DEFAULT '',
    remote_addr text NOT NULL DEFAULT '',
    client_ip   text NOT NULL DEFAULT '',
    result      text NOT NULL DEFAULT 'ok',
    details     jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_ts_idx ON audit_log (ts DESC);

CREATE TABLE settings (
    key   text PRIMARY KEY,
    value jsonb NOT NULL
);
