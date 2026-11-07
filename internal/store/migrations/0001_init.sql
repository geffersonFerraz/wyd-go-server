-- DBSRV schema v1. Replaces account/*.xml, char/*, seal/*.xml, guilds.txt,
-- Donate.txt and Config.txt.
--
-- Convention: columns are authoritative for everything an operator might
-- query or change with SQL (cash, bans, access, names, levels, items). The
-- jsonb `data` columns hold the remaining game state as the Go image of the
-- C++ struct (wire.Account / wire.CharInfo) with those fields stripped; the
-- store overlays the columns on load.

CREATE TABLE accounts (
    id                bigserial PRIMARY KEY,
    username          text        NOT NULL CHECK (username = upper(username) AND length(username) BETWEEN 1 AND 15),
    password_hash     text        NOT NULL,                  -- argon2id PHC string
    -- Legacy encoded state in the first password char: '@' blocked, '_' defective, '#' disabled.
    status            smallint    NOT NULL DEFAULT 0 CHECK (status BETWEEN 0 AND 3),
    numeric_pass_hash text,                                   -- "token"/SecondPass; NULL = not set yet
    pass_block        text        NOT NULL DEFAULT '',        -- sent to TMSRV in clear (p415/p820), so not hashed
    access_level      integer     NOT NULL DEFAULT 0,
    cash              bigint      NOT NULL DEFAULT 0 CHECK (cash BETWEEN 0 AND 4294967295),
    honra             bigint      NOT NULL DEFAULT 0 CHECK (honra BETWEEN 0 AND 4294967295),
    novato            bigint      NOT NULL DEFAULT 0 CHECK (novato BETWEEN 0 AND 4294967295),
    storage_gold      bigint      NOT NULL DEFAULT 0 CHECK (storage_gold BETWEEN 0 AND 4294967295),
    ban_type          integer     NOT NULL DEFAULT 0,
    ban_until         timestamptz,
    already_reset     boolean     NOT NULL DEFAULT false,
    data              jsonb       NOT NULL DEFAULT '{}',
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX accounts_username_key ON accounts (username);

CREATE TABLE characters (
    account_id        bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    slot              smallint    NOT NULL CHECK (slot BETWEEN 0 AND 3),
    name              text        NOT NULL CHECK (length(name) BETWEEN 1 AND 15),
    class_info        smallint    NOT NULL,
    face              integer     NOT NULL,                   -- Equip[0].Index
    evolution         smallint    NOT NULL,                   -- Equip[0].EFV2: 1 mortal, 2 arch, 3 celestial, 4 subcelestial
    level             integer     NOT NULL,
    exp               bigint      NOT NULL,
    gold              bigint      NOT NULL,
    guild_index       integer     NOT NULL DEFAULT 0,
    guild_member_type smallint    NOT NULL DEFAULT 0,
    fame              bigint      NOT NULL DEFAULT 0,
    citizen           smallint    NOT NULL DEFAULT 0,
    data              jsonb       NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, slot)
);
CREATE INDEX characters_name_idx ON characters (lower(name));
CREATE INDEX characters_guild_idx ON characters (guild_index) WHERE guild_index <> 0;

-- Replaces the char/<letter>/<NAME> files: a name belongs to one account.
-- It is NOT unique per character row: creating an Arch (0x80B) copies the
-- mortal's name into another slot of the same account. The old files lived
-- on case-insensitive NTFS, so reservations are case-insensitive.
CREATE TABLE character_names (
    name_key   text        PRIMARY KEY CHECK (name_key = lower(name_key)),
    account_id bigint      NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX character_names_account_idx ON character_names (account_id);

-- One row per non-empty item slot. char_slot = -1 is the account storage.
-- container: 0 equip, 1 inventory, 2 storage, 3 drop filter, 4 sub-celestial equip.
CREATE TABLE items (
    account_id  bigint   NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    char_slot   smallint NOT NULL CHECK (char_slot BETWEEN -1 AND 3),
    container   smallint NOT NULL CHECK (container BETWEEN 0 AND 4),
    slot        smallint NOT NULL CHECK (slot >= 0),
    item_id     integer  NOT NULL CHECK (item_id BETWEEN 0 AND 65535),
    ef1         smallint NOT NULL DEFAULT 0, efv1 smallint NOT NULL DEFAULT 0,
    ef2         smallint NOT NULL DEFAULT 0, efv2 smallint NOT NULL DEFAULT 0,
    ef3         smallint NOT NULL DEFAULT 0, efv3 smallint NOT NULL DEFAULT 0,
    PRIMARY KEY (account_id, char_slot, container, slot),
    CHECK ((char_slot = -1) = (container = 2))
);
CREATE INDEX items_item_id_idx ON items (item_id);

CREATE TABLE guilds (
    id       integer  PRIMARY KEY CHECK (id BETWEEN 1 AND 65534),
    name     text     NOT NULL,
    kingdom  smallint NOT NULL DEFAULT 0,
    fame     integer  NOT NULL DEFAULT 0,
    citizen  integer  NOT NULL DEFAULT 0,
    wins     integer  NOT NULL DEFAULT 0,
    sub1     text     NOT NULL DEFAULT '',
    sub2     text     NOT NULL DEFAULT '',
    sub3     text     NOT NULL DEFAULT ''
);

-- g_pGuildAlly / g_pGuildWar: one target per guild per kind. The C++ server
-- kept these in memory only, so they reset on every restart.
CREATE TABLE guild_relations (
    guild_id       integer  NOT NULL,
    kind           smallint NOT NULL CHECK (kind IN (1, 2)),  -- 1 ally, 2 war
    other_guild_id integer  NOT NULL,
    PRIMARY KEY (guild_id, kind)
);

-- Donate.txt / g_pStore[10][27].
CREATE TABLE donate_store (
    store     smallint NOT NULL CHECK (store BETWEEN 0 AND 9),
    slot      smallint NOT NULL CHECK (slot BETWEEN 0 AND 26),
    price     bigint   NOT NULL,
    loop      integer  NOT NULL DEFAULT 0,
    available integer  NOT NULL DEFAULT -1,                  -- -1 = unlimited
    item_id   integer  NOT NULL,
    ef1 smallint NOT NULL DEFAULT 0, efv1 smallint NOT NULL DEFAULT 0,
    ef2 smallint NOT NULL DEFAULT 0, efv2 smallint NOT NULL DEFAULT 0,
    ef3 smallint NOT NULL DEFAULT 0, efv3 smallint NOT NULL DEFAULT 0,
    PRIMARY KEY (store, slot)
);

-- seal/<id>.xml. The sealed character keeps its items inside the document.
CREATE TABLE seals (
    id         integer     PRIMARY KEY,
    info       jsonb       NOT NULL,                         -- wire.SealInfo
    character  jsonb       NOT NULL,                         -- wire.CharInfo, items included
    created_at timestamptz NOT NULL DEFAULT now()
);
-- Replaces Config.txt LastSeal.
CREATE SEQUENCE seal_id_seq START 1 OWNED BY seals.id;

-- Config.txt and other singletons.
CREATE TABLE server_state (
    id       smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    sapphire integer  NOT NULL DEFAULT 8
);
INSERT INTO server_state (id) VALUES (1);
