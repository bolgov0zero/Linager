-- +goose Up
-- A free-text note (Markdown) on a server: who owns it, what it is for, where the backups are.
ALTER TABLE servers ADD COLUMN note text NOT NULL DEFAULT '';
ALTER TABLE servers ADD COLUMN note_updated_at timestamptz;
ALTER TABLE servers ADD COLUMN note_updated_by text NOT NULL DEFAULT '';
