-- dbsrv writes a heartbeat with the delivery payload version it understands,
-- so the admin panel can refuse to queue work for a stopped or outdated dbsrv.
ALTER TABLE server_state
    ADD COLUMN dbsrv_protocol integer NOT NULL DEFAULT 0,
    ADD COLUMN dbsrv_seen_at  timestamptz;
