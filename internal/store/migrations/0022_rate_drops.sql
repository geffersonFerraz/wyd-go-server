-- Mixed drops, managed only in the admin panel. Each monster has the fixed
-- list (gamedata.drop_items: every N kills) and the chance list below. The
-- chance list starts as a copy of the monsters' inventories (npc/ files),
-- done once by the panel; the files are not read for drops after that.
ALTER TABLE gamedata.drops ADD COLUMN rate_gold bigint NOT NULL DEFAULT 0 CHECK (rate_gold >= 0);

-- chance > 0: 1 in chance. 0: uses the slot's base and rate (drop_slots).
CREATE TABLE gamedata.rate_drops (
    drop_name text     NOT NULL REFERENCES gamedata.drops (name) ON UPDATE CASCADE ON DELETE CASCADE,
    slot      smallint NOT NULL CHECK (slot BETWEEN 0 AND 63),
    item      integer  NOT NULL CHECK (item BETWEEN 1 AND 6499),
    effect1   integer  NOT NULL DEFAULT 0 CHECK (effect1 BETWEEN 0 AND 255),
    value1    integer  NOT NULL DEFAULT 0 CHECK (value1 BETWEEN 0 AND 65535),
    effect2   integer  NOT NULL DEFAULT 0 CHECK (effect2 BETWEEN 0 AND 255),
    value2    integer  NOT NULL DEFAULT 0 CHECK (value2 BETWEEN 0 AND 65535),
    effect3   integer  NOT NULL DEFAULT 0 CHECK (effect3 BETWEEN 0 AND 255),
    value3    integer  NOT NULL DEFAULT 0 CHECK (value3 BETWEEN 0 AND 65535),
    chance    integer  NOT NULL DEFAULT 0 CHECK (chance >= 0),
    PRIMARY KEY (drop_name, slot)
);

-- FixDropTax (base) and the rates of gameconfig.txt, one per inventory slot.
CREATE TABLE gamedata.drop_slots (
    slot smallint PRIMARY KEY CHECK (slot BETWEEN 0 AND 63),
    base integer  NOT NULL CHECK (base >= 0),
    rate integer  NOT NULL CHECK (rate >= 0)
);
INSERT INTO gamedata.drop_slots (slot, base, rate) VALUES
    (0, 400, 400),
    (1, 400, 400),
    (2, 400, 400),
    (3, 400, 400),
    (4, 400, 400),
    (5, 400, 400),
    (6, 400, 400),
    (7, 400, 400),
    (8, 4, 400),
    (9, 4, 400),
    (10, 4, 400),
    (11, 4, 400),
    (12, 900, 400),
    (13, 900, 400),
    (14, 900, 400),
    (15, 900, 300),
    (16, 20000, 500),
    (17, 20000, 500),
    (18, 20000, 500),
    (19, 20000, 500),
    (20, 20000, 500),
    (21, 20000, 200),
    (22, 35, 200),
    (23, 35, 500),
    (24, 1500, 500),
    (25, 1500, 500),
    (26, 1500, 500),
    (27, 1500, 500),
    (28, 1500, 500),
    (29, 1500, 500),
    (30, 1500, 500),
    (31, 1500, 500),
    (32, 1500, 500),
    (33, 1500, 500),
    (34, 1500, 500),
    (35, 1500, 500),
    (36, 1500, 500),
    (37, 1500, 500),
    (38, 1500, 500),
    (39, 1500, 500),
    (40, 1500, 500),
    (41, 1500, 500),
    (42, 1500, 500),
    (43, 1500, 500),
    (44, 1500, 500),
    (45, 1500, 500),
    (46, 1500, 500),
    (47, 1500, 500),
    (48, 3000, 200),
    (49, 3000, 200),
    (50, 3000, 500),
    (51, 3000, 500),
    (52, 3000, 500),
    (53, 3000, 500),
    (54, 3000, 500),
    (55, 3000, 200),
    (56, 1, 200),
    (57, 35, 200),
    (58, 500, 200),
    (59, 2500, 200),
    (60, 5000, 200),
    (61, 5000, 200),
    (62, 10000, 650),
    (63, 20000, 700);

-- BlockDropItem.txt: never dropped by chance.
CREATE TABLE gamedata.blocked_drop_items (
    item integer PRIMARY KEY CHECK (item BETWEEN 1 AND 6499)
);
INSERT INTO gamedata.blocked_drop_items (item) VALUES
    (3170),
    (2317),
    (2318),
    (2319),
    (2320),
    (2324),
    (2325),
    (2326),
    (2327),
    (2328),
    (2329),
    (2407),
    (2408),
    (2409),
    (2410),
    (2417),
    (2418),
    (2419),
    (4010),
    (4011),
    (4038),
    (4039),
    (4040),
    (4041),
    (4042),
    (4016),
    (4017),
    (4018),
    (667);

-- Set once the inventories were copied, so it never runs over panel edits.
CREATE TABLE gamedata.rate_drop_import (
    id          smallint    PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    imported_at timestamptz NOT NULL DEFAULT now(),
    imported_by text        NOT NULL
);

CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.rate_drops
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.drop_slots
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.blocked_drop_items
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
