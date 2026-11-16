-- +goose Up
ALTER TABLE server_groups ADD COLUMN position integer NOT NULL DEFAULT 0;
UPDATE server_groups g SET position = r.n FROM (SELECT id, row_number() OVER (ORDER BY id) AS n FROM server_groups) r WHERE g.id = r.id;
