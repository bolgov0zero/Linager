-- +goose Up
-- Recovery codes for 2FA: one-time, only the hash is stored.
CREATE TABLE recovery_codes (
    id         bigserial PRIMARY KEY,
    user_id    bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  bytea NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX recovery_codes_user_idx ON recovery_codes (user_id);

-- Passkeys (WebAuthn credentials). credential is the serialized webauthn.Credential.
CREATE TABLE webauthn_credentials (
    id           bytea PRIMARY KEY,
    user_id      bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text NOT NULL,
    credential   jsonb NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz
);
CREATE INDEX webauthn_credentials_user_idx ON webauthn_credentials (user_id);

-- The opaque user handle that identifies the account to authenticators (random, not the login).
ALTER TABLE users ADD COLUMN webauthn_handle bytea;

-- Terminal snippets, shared by all administrators.
CREATE TABLE snippets (
    id         bigserial PRIMARY KEY,
    name       text NOT NULL,
    command    text NOT NULL,
    position   integer NOT NULL DEFAULT 0,
    created_by bigint REFERENCES users(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
