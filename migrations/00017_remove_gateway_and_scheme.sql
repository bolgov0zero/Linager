-- +goose Up
-- The Scheme view of the dashboard (0.2.4) and the Gateway module (0.2.6) were taken out again in 0.2.8. A panel that
-- ran those releases has the leftovers of their migrations (00014 to 00016, which no longer exist): this removes them.
-- A panel that never had them finds nothing to remove. Metrics, servers and users are not touched.
DROP TABLE IF EXISTS gateway_events;
DROP TABLE IF EXISTS gateway_devices;
DROP TABLE IF EXISTS gateways;
ALTER TABLE dashboards DROP COLUMN IF EXISTS scheme;
-- an operator who was given the module "gateway" (it exists no more)
DELETE FROM user_modules WHERE module = 'gateway';
