-- The world of one server: every table of schema s<group>. Runs with
-- search_path = s<group>, public, so the unqualified names below are created
-- in that schema; what is shared stays qualified (public.accounts...).
--
-- This is the layout server 1 has after migrations 0001..0032 (pg_dump of
-- s1). TestWorldSchemasMatch checks that a new server gets the same tables,
-- indexes, constraints and triggers. Later changes to a world table go in a
-- new file of this folder, never in migrations/, so every server gets them.

CREATE TABLE account_world (
    account_id bigint NOT NULL,
    novato bigint DEFAULT 0 NOT NULL,
    storage_gold bigint DEFAULT 0 NOT NULL,
    already_reset boolean DEFAULT false NOT NULL,
    data jsonb DEFAULT '{"CharSlot": -1}'::jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT account_world_novato_check CHECK (((novato >= 0) AND (novato <= '4294967295'::bigint))),
    CONSTRAINT account_world_storage_gold_check CHECK (((storage_gold >= 0) AND (storage_gold <= '1000000000000000'::bigint)))
);

CREATE TABLE alerts (
    id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    kind text NOT NULL,
    username text DEFAULT ''::text NOT NULL,
    char_name text DEFAULT ''::text NOT NULL,
    ip text DEFAULT ''::text NOT NULL,
    channel smallint DEFAULT 0 NOT NULL,
    detail text DEFAULT ''::text NOT NULL,
    seen_at timestamp with time zone,
    seen_by text DEFAULT ''::text NOT NULL
);

CREATE SEQUENCE alerts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE alerts_id_seq OWNED BY alerts.id;

CREATE TABLE channel_state (
    channel smallint NOT NULL,
    state jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE character_names (
    name_key text NOT NULL,
    account_id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT character_names_name_key_check CHECK ((name_key = lower(name_key)))
);

CREATE TABLE characters (
    account_id bigint NOT NULL,
    slot smallint NOT NULL,
    name text NOT NULL,
    class_info smallint NOT NULL,
    face integer NOT NULL,
    evolution smallint NOT NULL,
    level integer NOT NULL,
    exp bigint NOT NULL,
    gold bigint NOT NULL,
    guild_index integer DEFAULT 0 NOT NULL,
    guild_member_type smallint DEFAULT 0 NOT NULL,
    fame bigint DEFAULT 0 NOT NULL,
    citizen smallint DEFAULT 0 NOT NULL,
    data jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT characters_name_check CHECK (((length(name) >= 1) AND (length(name) <= 15))),
    CONSTRAINT characters_slot_check CHECK (((slot >= 0) AND (slot <= 3)))
);

CREATE TABLE city_owners (
    city smallint NOT NULL,
    guild_id integer NOT NULL,
    changed_by text DEFAULT ''::text NOT NULL,
    changed_at timestamp with time zone DEFAULT now() NOT NULL,
    channel smallint DEFAULT 1 NOT NULL,
    CONSTRAINT city_owners_channel_check CHECK (((channel >= 1) AND (channel <= 10))),
    CONSTRAINT city_owners_city_check CHECK (((city >= 0) AND (city <= 4))),
    CONSTRAINT city_owners_guild_id_check CHECK (((guild_id >= 0) AND (guild_id < 65535)))
);

CREATE TABLE gm_messages (
    id bigint NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    channel smallint NOT NULL,
    username text NOT NULL,
    char_name text NOT NULL,
    text text NOT NULL,
    seen_at timestamp with time zone,
    seen_by text DEFAULT ''::text NOT NULL
);

CREATE SEQUENCE gm_messages_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE gm_messages_id_seq OWNED BY gm_messages.id;

CREATE TABLE guild_relations (
    guild_id integer NOT NULL,
    kind smallint NOT NULL,
    other_guild_id integer NOT NULL,
    CONSTRAINT guild_relations_kind_check CHECK ((kind = ANY (ARRAY[1, 2])))
);

CREATE TABLE guilds (
    id integer NOT NULL,
    name text NOT NULL,
    kingdom smallint DEFAULT 0 NOT NULL,
    fame integer DEFAULT 0 NOT NULL,
    citizen integer DEFAULT 0 NOT NULL,
    wins integer DEFAULT 0 NOT NULL,
    sub1 text DEFAULT ''::text NOT NULL,
    sub2 text DEFAULT ''::text NOT NULL,
    sub3 text DEFAULT ''::text NOT NULL,
    CONSTRAINT guilds_id_check CHECK (((id >= 1) AND (id <= 65534)))
);

CREATE TABLE item_track_events (
    id bigint NOT NULL,
    track_id bigint NOT NULL,
    at timestamp with time zone NOT NULL,
    kind text NOT NULL,
    channel smallint NOT NULL,
    from_account text DEFAULT ''::text NOT NULL,
    from_char text DEFAULT ''::text NOT NULL,
    to_account text DEFAULT ''::text NOT NULL,
    to_char text DEFAULT ''::text NOT NULL,
    price bigint DEFAULT 0 NOT NULL,
    item integer NOT NULL,
    effects jsonb NOT NULL,
    CONSTRAINT item_track_events_kind_check CHECK ((kind = ANY (ARRAY['pickup'::text, 'npc_buy'::text, 'npc_reward'::text, 'event_reward'::text, 'trade'::text, 'store'::text, 'ground'::text, 'npc_sell'::text, 'buyback'::text, 'destroyed'::text, 'gone'::text, 'changed'::text])))
);

CREATE SEQUENCE item_track_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE item_track_events_id_seq OWNED BY item_track_events.id;

CREATE TABLE item_tracks (
    id bigint NOT NULL,
    origin text DEFAULT 'drop'::text NOT NULL,
    item integer NOT NULL,
    effects jsonb NOT NULL,
    mob text NOT NULL,
    price bigint DEFAULT 0 NOT NULL,
    channel smallint NOT NULL,
    drop_x integer NOT NULL,
    drop_y integer NOT NULL,
    dropped_at timestamp with time zone NOT NULL,
    killer_account text NOT NULL,
    killer_char text NOT NULL,
    picked_at timestamp with time zone NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    holder_account text,
    holder_char text,
    holder_char_slot smallint,
    loc_type smallint,
    loc_slot smallint,
    current_item integer NOT NULL,
    current_effects jsonb NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT item_tracks_origin_check CHECK ((origin = ANY (ARRAY['drop'::text, 'npc_buy'::text, 'npc_reward'::text, 'event_reward'::text]))),
    CONSTRAINT item_tracks_status_check CHECK ((status = ANY (ARRAY['active'::text, 'sold'::text, 'destroyed'::text, 'gone'::text])))
);

CREATE TABLE items (
    account_id bigint NOT NULL,
    char_slot smallint NOT NULL,
    container smallint NOT NULL,
    slot smallint NOT NULL,
    item_id integer NOT NULL,
    ef1 smallint DEFAULT 0 NOT NULL,
    efv1 integer DEFAULT 0 NOT NULL,
    ef2 smallint DEFAULT 0 NOT NULL,
    efv2 integer DEFAULT 0 NOT NULL,
    ef3 smallint DEFAULT 0 NOT NULL,
    efv3 integer DEFAULT 0 NOT NULL,
    CONSTRAINT items_char_slot_check CHECK (((char_slot >= '-1'::integer) AND (char_slot <= 3))),
    CONSTRAINT items_check CHECK (((char_slot = '-1'::integer) = (container = 2))),
    CONSTRAINT items_container_check CHECK (((container >= 0) AND (container <= 4))),
    CONSTRAINT items_item_id_check CHECK (((item_id >= 0) AND (item_id <= 65535))),
    CONSTRAINT items_slot_check CHECK ((slot >= 0))
);

CREATE TABLE open_stores (
    channel smallint NOT NULL,
    owner_name text NOT NULL,
    store_name text NOT NULL,
    items integer NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE ranking (
    name_key text NOT NULL,
    name text NOT NULL,
    level integer NOT NULL,
    class_info integer NOT NULL,
    evolution integer NOT NULL,
    guild_index integer NOT NULL,
    patent integer NOT NULL,
    kingdom integer NOT NULL,
    frags integer NOT NULL,
    power integer NOT NULL,
    fame integer NOT NULL,
    citizen integer NOT NULL,
    pvp_kill integer NOT NULL,
    pvm_kill integer NOT NULL,
    mantle integer NOT NULL,
    exp bigint NOT NULL,
    equip bytea NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ranking_name_key_check CHECK ((name_key = lower(name_key)))
);

CREATE TABLE seals (
    id integer NOT NULL,
    info jsonb NOT NULL,
    "character" jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    used_at timestamp with time zone
);

CREATE SEQUENCE seal_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE seal_id_seq OWNED BY seals.id;

CREATE TABLE server_state (
    id smallint DEFAULT 1 NOT NULL,
    sapphire integer DEFAULT 8 NOT NULL,
    first_kefra_channel integer DEFAULT 10 NOT NULL,
    kefra_killer text DEFAULT ''::text NOT NULL,
    CONSTRAINT server_state_id_check CHECK ((id = 1))
);

CREATE TABLE store_sales (
    id bigint NOT NULL,
    sold_at timestamp with time zone DEFAULT now() NOT NULL,
    channel smallint NOT NULL,
    buyer text NOT NULL,
    seller text NOT NULL,
    item integer NOT NULL,
    effects jsonb NOT NULL,
    price bigint NOT NULL,
    tax bigint NOT NULL,
    category smallint NOT NULL
);

CREATE SEQUENCE store_sales_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE store_sales_id_seq OWNED BY store_sales.id;

ALTER TABLE ONLY alerts ALTER COLUMN id SET DEFAULT nextval('alerts_id_seq'::regclass);

ALTER TABLE ONLY gm_messages ALTER COLUMN id SET DEFAULT nextval('gm_messages_id_seq'::regclass);

ALTER TABLE ONLY item_track_events ALTER COLUMN id SET DEFAULT nextval('item_track_events_id_seq'::regclass);

ALTER TABLE ONLY store_sales ALTER COLUMN id SET DEFAULT nextval('store_sales_id_seq'::regclass);

ALTER TABLE ONLY account_world
    ADD CONSTRAINT account_world_pkey PRIMARY KEY (account_id);

ALTER TABLE ONLY alerts
    ADD CONSTRAINT alerts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY channel_state
    ADD CONSTRAINT channel_state_pkey PRIMARY KEY (channel);

ALTER TABLE ONLY character_names
    ADD CONSTRAINT character_names_pkey PRIMARY KEY (name_key);

ALTER TABLE ONLY characters
    ADD CONSTRAINT characters_pkey PRIMARY KEY (account_id, slot);

ALTER TABLE ONLY city_owners
    ADD CONSTRAINT city_owners_pkey PRIMARY KEY (channel, city);

ALTER TABLE ONLY gm_messages
    ADD CONSTRAINT gm_messages_pkey PRIMARY KEY (id);

ALTER TABLE ONLY guild_relations
    ADD CONSTRAINT guild_relations_pkey PRIMARY KEY (guild_id, kind);

ALTER TABLE ONLY guilds
    ADD CONSTRAINT guilds_pkey PRIMARY KEY (id);

ALTER TABLE ONLY item_track_events
    ADD CONSTRAINT item_track_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY item_tracks
    ADD CONSTRAINT item_tracks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY items
    ADD CONSTRAINT items_pkey PRIMARY KEY (account_id, char_slot, container, slot);

ALTER TABLE ONLY open_stores
    ADD CONSTRAINT open_stores_pkey PRIMARY KEY (channel, owner_name);

ALTER TABLE ONLY ranking
    ADD CONSTRAINT ranking_pkey PRIMARY KEY (name_key);

ALTER TABLE ONLY seals
    ADD CONSTRAINT seals_pkey PRIMARY KEY (id);

ALTER TABLE ONLY server_state
    ADD CONSTRAINT server_state_pkey PRIMARY KEY (id);

ALTER TABLE ONLY store_sales
    ADD CONSTRAINT store_sales_pkey PRIMARY KEY (id);

CREATE INDEX alerts_unseen ON alerts USING btree (id) WHERE (seen_at IS NULL);

CREATE INDEX alerts_username ON alerts USING btree (username, id DESC);

CREATE INDEX character_names_account_idx ON character_names USING btree (account_id);

CREATE INDEX characters_guild_idx ON characters USING btree (guild_index) WHERE (guild_index <> 0);

CREATE INDEX characters_name_idx ON characters USING btree (lower(name));

CREATE INDEX gm_messages_unseen ON gm_messages USING btree (id) WHERE (seen_at IS NULL);

CREATE INDEX item_track_events_from_idx ON item_track_events USING btree (from_account) WHERE (from_account <> ''::text);

CREATE INDEX item_track_events_to_idx ON item_track_events USING btree (to_account) WHERE (to_account <> ''::text);

CREATE INDEX item_track_events_track_idx ON item_track_events USING btree (track_id, id);

CREATE INDEX item_tracks_current_item_idx ON item_tracks USING btree (current_item);

CREATE INDEX item_tracks_holder_idx ON item_tracks USING btree (holder_account) WHERE (status = 'active'::text);

CREATE INDEX item_tracks_item_idx ON item_tracks USING btree (item);

CREATE INDEX item_tracks_killer_idx ON item_tracks USING btree (lower(killer_char));

CREATE INDEX items_item_id_idx ON items USING btree (item_id);

CREATE TRIGGER account_world_online_guard BEFORE INSERT OR DELETE OR UPDATE ON account_world FOR EACH ROW EXECUTE FUNCTION public.guard_online_account();

CREATE TRIGGER characters_online_guard BEFORE INSERT OR DELETE OR UPDATE ON characters FOR EACH ROW EXECUTE FUNCTION public.guard_online_account();

CREATE TRIGGER items_online_guard BEFORE INSERT OR DELETE OR UPDATE ON items FOR EACH ROW EXECUTE FUNCTION public.guard_online_account();

CREATE TRIGGER notify_changed AFTER INSERT OR DELETE OR UPDATE OR TRUNCATE ON city_owners FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();

ALTER TABLE ONLY account_world
    ADD CONSTRAINT account_world_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

ALTER TABLE ONLY character_names
    ADD CONSTRAINT character_names_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

ALTER TABLE ONLY characters
    ADD CONSTRAINT characters_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

ALTER TABLE ONLY item_track_events
    ADD CONSTRAINT item_track_events_track_id_fkey FOREIGN KEY (track_id) REFERENCES item_tracks(id) ON DELETE CASCADE;

ALTER TABLE ONLY items
    ADD CONSTRAINT items_account_id_fkey FOREIGN KEY (account_id) REFERENCES public.accounts(id) ON DELETE CASCADE;

-- Config.txt singletons (sapphire, Kefra).
INSERT INTO server_state (id) VALUES (1);
