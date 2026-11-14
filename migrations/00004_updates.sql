-- +goose Up
-- History of Linager updates (server and agent rollouts).
CREATE TABLE update_runs (
    id           bigserial PRIMARY KEY,
    ts           timestamptz NOT NULL DEFAULT now(),
    kind         text NOT NULL CHECK (kind IN ('server', 'agents')),
    from_version text NOT NULL DEFAULT '',
    to_version   text NOT NULL DEFAULT '',
    status       text NOT NULL,
    detail       text NOT NULL DEFAULT ''
);
CREATE INDEX update_runs_ts_idx ON update_runs (ts DESC);

-- Per-agent update state; survives server restarts and keeps the queue of offline agents.
CREATE TABLE agent_updates (
    server_id  uuid PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    version    text NOT NULL,
    status     text NOT NULL CHECK (status IN ('queued', 'updating', 'done', 'failed', 'rolled_back')),
    step       integer NOT NULL DEFAULT 0,
    message    text NOT NULL DEFAULT '',
    run_id     text NOT NULL DEFAULT '',
    queued_at  timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
