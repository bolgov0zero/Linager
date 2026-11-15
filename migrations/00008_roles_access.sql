-- +goose Up
-- Roles: the operator works on the servers and with the modules the administrator gave, the auditor only looks
-- at the dashboard, the overview and the metrics of the servers the administrator gave.
INSERT INTO roles (name) VALUES ('operator'), ('auditor') ON CONFLICT (name) DO NOTHING;

-- Servers a user may see: single servers and whole groups (a server added to the group later is included).
CREATE TABLE user_servers (
    user_id   bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    server_id uuid   NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, server_id)
);

CREATE TABLE user_groups (
    user_id  bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_id bigint NOT NULL REFERENCES server_groups(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, group_id)
);

-- Agent modules open to an operator (on every server the operator may see).
CREATE TABLE user_modules (
    user_id bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    module  text   NOT NULL,
    PRIMARY KEY (user_id, module)
);
