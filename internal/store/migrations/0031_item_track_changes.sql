-- Item tracking keeps every change of a tracked item's effects (refine,
-- time left, any other edit) as a 'changed' event, recorded by DBSRV when a
-- channel reports the item different from what was stored. Before this the
-- current effects were overwritten and the old ones were lost.
ALTER TABLE item_track_events DROP CONSTRAINT item_track_events_kind_check;
ALTER TABLE item_track_events ADD CONSTRAINT item_track_events_kind_check CHECK (kind IN ('pickup', 'npc_buy', 'npc_reward',
    'event_reward', 'trade', 'store', 'ground', 'npc_sell', 'buyback', 'destroyed', 'gone', 'changed'));
