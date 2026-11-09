-- The binary files of the game world, formerly in the data folder.
-- gamedata.assets: the height and attribute maps and the client's ItemList.bin.
CREATE TABLE gamedata.assets (
    name       text        PRIMARY KEY,
    content    bytea       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- gamedata.mob_templates: STRUCT_MOB images in the C++ 32-bit layout.
-- kind: npc (monsters and merchants), npc_base (mounts and summons),
-- class (the TK, FM, BM and HT starting characters).
CREATE TABLE gamedata.mob_templates (
    kind    text  NOT NULL CHECK (kind IN ('npc', 'npc_base', 'class')),
    name    text  NOT NULL,
    content bytea NOT NULL,
    PRIMARY KEY (kind, name)
);

-- The in-game account lock (formerly Cache/LockAccount/<LOGIN>.bin): 10 bytes
-- of password and 8 switch bytes.
CREATE TABLE account_locks (
    username   text        PRIMARY KEY,
    data       bytea       NOT NULL CHECK (octet_length(data) = 18),
    updated_at timestamptz NOT NULL DEFAULT now()
);
