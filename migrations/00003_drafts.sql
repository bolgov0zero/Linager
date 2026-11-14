-- +goose Up
-- Unapplied network/firewall edits (drafts) live in the panel until the administrator applies them.
CREATE TABLE drafts (
    server_id  uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    kind       text NOT NULL CHECK (kind IN ('network', 'firewall')),
    payload    jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (server_id, kind)
);

-- A change applied on an agent that waits for confirmation (the agent itself enforces the rollback timer).
CREATE TABLE pending_changes (
    server_id  uuid PRIMARY KEY REFERENCES servers(id) ON DELETE CASCADE,
    kind       text NOT NULL,
    change_id  text NOT NULL,
    deadline   timestamptz NOT NULL
);
