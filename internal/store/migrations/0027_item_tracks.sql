-- Tracking of the items monsters drop, NPCs sell or give, and events give
-- as prizes. A track is created when the first player picks a drop up, or
-- when the item comes from an NPC or an event (only items that do not stack), and every time it changes
-- account an event is appended. Drops nobody picks up vanish from the
-- ground without a track.
CREATE TABLE item_tracks (
    id               bigint      PRIMARY KEY,               -- made by TMSRV (random, never 0)
    origin           text        NOT NULL DEFAULT 'drop' CHECK (origin IN ('drop', 'npc_buy', 'npc_reward', 'event_reward')),
    item             integer     NOT NULL,
    effects          jsonb       NOT NULL,                   -- as it was picked up / received
    mob              text        NOT NULL,                   -- the monster, the NPC or the event
    price            bigint      NOT NULL DEFAULT 0,         -- npc_buy: gold paid
    channel          smallint    NOT NULL,
    drop_x           integer     NOT NULL,
    drop_y           integer     NOT NULL,
    dropped_at       timestamptz NOT NULL,
    killer_account   text        NOT NULL,                   -- '' unless a monster's drop
    killer_char      text        NOT NULL,
    picked_at        timestamptz NOT NULL,
    -- Where it is now. holder_account is NULL once it left the game.
    status           text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'sold', 'destroyed', 'gone')),
    holder_account   text,
    holder_char      text,
    holder_char_slot smallint,                               -- -1 = account storage
    loc_type         smallint,                               -- 0 equip, 1 inventory, 2 storage
    loc_slot         smallint,
    current_item     integer     NOT NULL,
    current_effects  jsonb       NOT NULL,
    updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX item_tracks_holder_idx ON item_tracks (holder_account) WHERE status = 'active';
CREATE INDEX item_tracks_killer_idx ON item_tracks (lower(killer_char));
CREATE INDEX item_tracks_item_idx ON item_tracks (item);

CREATE TABLE item_track_events (
    id           bigserial   PRIMARY KEY,
    track_id     bigint      NOT NULL REFERENCES item_tracks (id) ON DELETE CASCADE,
    at           timestamptz NOT NULL,
    kind         text        NOT NULL CHECK (kind IN ('pickup', 'npc_buy', 'npc_reward', 'event_reward', 'trade', 'store', 'ground', 'npc_sell', 'buyback', 'destroyed', 'gone')),
    channel      smallint    NOT NULL,
    from_account text        NOT NULL DEFAULT '',
    from_char    text        NOT NULL DEFAULT '',
    to_account   text        NOT NULL DEFAULT '',
    to_char      text        NOT NULL DEFAULT '',
    price        bigint      NOT NULL DEFAULT 0,
    item         integer     NOT NULL,
    effects      jsonb       NOT NULL
);
CREATE INDEX item_track_events_track_idx ON item_track_events (track_id, id);
