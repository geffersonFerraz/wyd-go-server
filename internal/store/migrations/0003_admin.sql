-- Admin panel: who asked for each delivery.
ALTER TABLE deliveries ADD COLUMN requested_by text NOT NULL DEFAULT '';
CREATE INDEX deliveries_username_idx ON deliveries (username, id DESC);
