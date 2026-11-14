-- +goose Up
CREATE TABLE server_groups (
    id         bigserial PRIMARY KEY,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX server_groups_name_idx ON server_groups (lower(name));

ALTER TABLE servers ADD COLUMN group_id bigint REFERENCES server_groups(id) ON DELETE SET NULL;
