-- +goose Up
-- The picture settings of the Assistant (colours, frame rate, traffic limit) of a Windows computer. An empty object
-- means the defaults of the panel.
ALTER TABLE servers ADD COLUMN assistant jsonb NOT NULL DEFAULT '{}';
