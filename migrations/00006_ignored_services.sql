-- +goose Up
-- Failed systemd units the administrator chose to ignore: they do not count towards the server status.
CREATE TABLE ignored_services (
    server_id uuid NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    unit      text NOT NULL,
    PRIMARY KEY (server_id, unit)
);
