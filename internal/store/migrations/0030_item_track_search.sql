-- The panel's item tracking search ("Rastreio"): every track an account
-- ever held, and every track of an item.
CREATE INDEX item_track_events_to_idx ON item_track_events (to_account) WHERE to_account <> '';
CREATE INDEX item_track_events_from_idx ON item_track_events (from_account) WHERE from_account <> '';
CREATE INDEX item_tracks_current_item_idx ON item_tracks (current_item);
