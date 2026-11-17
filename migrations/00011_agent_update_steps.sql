-- +goose Up
-- Stages of the agent update that is running now (or ended badly): [{id, state, started_ms, finished_ms, detail}].
ALTER TABLE agent_updates ADD COLUMN steps jsonb NOT NULL DEFAULT '[]'::jsonb;
