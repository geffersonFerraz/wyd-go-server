-- Static game data the TMSRV loads at startup, formerly read from files in
-- the C++ TMSRV folder: ItemList.csv (+ ItemEffect.h names), skilldata.csv,
-- "controle drop/Controle.txt" and Teleport.txt. Filled by cmd/gamedata-import
-- and edited here (SQL, admin panel). Names are UTF-8; the servers convert
-- them to Windows-1252 in memory.
CREATE SCHEMA gamedata;

-- ItemEffect.h: effect ids and their names (EF_DAMAGE, EF_HP, ...).
CREATE TABLE gamedata.effect_names (
    id   smallint PRIMARY KEY CHECK (id BETWEEN 0 AND 255),
    name text     NOT NULL UNIQUE
);

-- ItemList.csv (STRUCT_ITEMLIST).
CREATE TABLE gamedata.items (
    id          smallint PRIMARY KEY CHECK (id BETWEEN 1 AND 6499),
    name        text     NOT NULL,
    mesh1       smallint NOT NULL DEFAULT 0,
    mesh2       integer  NOT NULL DEFAULT 0,
    level       smallint NOT NULL DEFAULT 0,
    req_str     smallint NOT NULL DEFAULT 0,
    req_int     smallint NOT NULL DEFAULT 0,
    req_dex     smallint NOT NULL DEFAULT 0,
    req_con     smallint NOT NULL DEFAULT 0,
    price       integer  NOT NULL DEFAULT 0 CHECK (price >= 0),
    unique_type smallint NOT NULL DEFAULT 0,
    pos         integer  NOT NULL DEFAULT 0 CHECK (pos BETWEEN 0 AND 65535),
    extreme     smallint NOT NULL DEFAULT 0,
    grade       smallint NOT NULL DEFAULT 0
);

-- The 12 (effect, value) pairs of an item; slot keeps the column order.
CREATE TABLE gamedata.item_effects (
    item_id smallint NOT NULL REFERENCES gamedata.items (id) ON DELETE CASCADE,
    slot    smallint NOT NULL CHECK (slot BETWEEN 0 AND 11),
    effect  smallint NOT NULL CHECK (effect BETWEEN 0 AND 255),
    value   smallint NOT NULL,
    PRIMARY KEY (item_id, slot)
);

-- skilldata.csv (STRUCT_SKILLDATA). act1/act2 are the client animation
-- columns the server ignores.
CREATE TABLE gamedata.skills (
    id                 smallint PRIMARY KEY CHECK (id BETWEEN 0 AND 255),
    name               text     NOT NULL DEFAULT '',
    points             integer  NOT NULL DEFAULT 0,
    target             integer  NOT NULL DEFAULT 0,
    mana               integer  NOT NULL DEFAULT 0,
    delay              integer  NOT NULL DEFAULT 0,
    range              integer  NOT NULL DEFAULT 0,
    instance_type      integer  NOT NULL DEFAULT 0,
    instance_value     integer  NOT NULL DEFAULT 0,
    tick_type          integer  NOT NULL DEFAULT 0,
    tick_value         integer  NOT NULL DEFAULT 0,
    affect_type        integer  NOT NULL DEFAULT 0,
    affect_value       integer  NOT NULL DEFAULT 0,
    time               integer  NOT NULL DEFAULT 0,
    instance_attribute integer  NOT NULL DEFAULT 0,
    tick_attribute     integer  NOT NULL DEFAULT 0,
    aggressive         integer  NOT NULL DEFAULT 0,
    max_target         integer  NOT NULL DEFAULT 0,
    party_check        integer  NOT NULL DEFAULT 0,
    affect_resist      integer  NOT NULL DEFAULT 0,
    passive            integer  NOT NULL DEFAULT 0,
    force_damage       integer  NOT NULL DEFAULT 0,
    act1               text     NOT NULL DEFAULT '',
    act2               text     NOT NULL DEFAULT ''
);

-- Controle.txt: drops per monster name. The name must equal the monster's
-- name exactly, trailing spaces included ("Guer Caveira " and
-- "Guer Caveira" are different monsters).
CREATE TABLE gamedata.drops (
    name      text    PRIMARY KEY CHECK (name <> ''),
    section   text    NOT NULL DEFAULT '',
    max_drops integer NOT NULL DEFAULT 0 CHECK (max_drops >= 0),
    gold      integer NOT NULL DEFAULT 0 CHECK (gold >= 0),
    sort      integer NOT NULL DEFAULT 0
);

-- Every `kills` kills of the monster (per player and item) give the item.
CREATE TABLE gamedata.drop_items (
    drop_name text     NOT NULL REFERENCES gamedata.drops (name) ON UPDATE CASCADE ON DELETE CASCADE,
    ord       smallint NOT NULL,
    kills     integer  NOT NULL CHECK (kills > 0),
    item      smallint NOT NULL CHECK (item BETWEEN 1 AND 6499),
    effect    smallint NOT NULL DEFAULT 0 CHECK (effect BETWEEN 0 AND 255),
    value     smallint NOT NULL DEFAULT 0 CHECK (value BETWEEN 0 AND 255),
    PRIMARY KEY (drop_name, ord)
);

-- Teleport.txt. The id is the line index: some are special in the code
-- (6 RvR, 14 Kefra hall, 43 fairy valley, 44-47 events).
CREATE TABLE gamedata.teleports (
    id    smallint PRIMARY KEY CHECK (id BETWEEN 0 AND 127),
    src_x smallint NOT NULL,
    src_y smallint NOT NULL,
    dst_x smallint NOT NULL,
    dst_y smallint NOT NULL,
    price integer  NOT NULL DEFAULT 0 CHECK (price >= 0)
);
