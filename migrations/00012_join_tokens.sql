-- +goose Up
-- Tokens of the install-by-link and install-over-SSH flows. Only the SHA-256 of a token is kept: the token itself
-- is shown once, when it is created.
CREATE TABLE join_tokens (
    id           bigserial PRIMARY KEY,
    token_hash   bytea NOT NULL UNIQUE,
    label        text NOT NULL,
    kind         text NOT NULL DEFAULT 'link' CHECK (kind IN ('link', 'ssh')),
    created_by   text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    max_uses     integer NOT NULL DEFAULT 1,           -- 0 = no limit
    uses         integer NOT NULL DEFAULT 0,
    auto_approve boolean NOT NULL DEFAULT false,
    networks     text[] NOT NULL DEFAULT '{}',         -- allowed source networks; empty = any
    group_id     bigint REFERENCES server_groups(id) ON DELETE SET NULL,
    revoked      boolean NOT NULL DEFAULT false,
    used         jsonb NOT NULL DEFAULT '[]'::jsonb    -- [{host, at}] of the agents that came with it
);
CREATE INDEX join_tokens_expires_idx ON join_tokens (expires_at);

ALTER TABLE enrollments ADD COLUMN token_id bigint REFERENCES join_tokens(id) ON DELETE SET NULL;
