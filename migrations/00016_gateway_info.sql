-- +goose Up
-- What a device says about itself beyond ports and names: the service types it announces over mDNS, its UPnP
-- description and whether it is the default gateway. The kind of the device ("printer", "camera") is not stored: the
-- panel works it out from these fields when it shows them, so better rules apply to the data already collected.
ALTER TABLE gateway_devices ADD COLUMN info jsonb NOT NULL DEFAULT '{}';
