-- +goose Up
-- Where the user dragged the panel and the servers on the scheme view of the dashboard: a JSON object with
-- positions on the scheme plane (pixels at 100% zoom), by server id. NULL means the automatic layout.
ALTER TABLE dashboards ADD COLUMN scheme jsonb;
