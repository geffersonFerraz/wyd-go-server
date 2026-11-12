-- NPC generators (formerly config/npc_generators.yaml, the C++ NPCGener.txt).
-- The id is the position in the game's list: the code knows some of them by
-- number (kings, towers, Kefra), so ids are never renumbered. An id without a
-- row is an empty generator.
CREATE TABLE gamedata.npc_generators (
    id            integer PRIMARY KEY CHECK (id >= 0 AND id < 8192),
    minute        integer NOT NULL DEFAULT 0,
    max_mobs      integer NOT NULL DEFAULT 0,
    min_group     integer NOT NULL DEFAULT 0,
    max_group     integer NOT NULL DEFAULT 0,
    formation     integer NOT NULL DEFAULT 0,
    route         integer NOT NULL DEFAULT 0,
    leader        text    NOT NULL DEFAULT '',
    follower      text    NOT NULL DEFAULT '',
    -- [{"Segment":0,"X":..,"Y":..,"Range":..,"Wait":..,"Action":".."}]
    segments      jsonb   NOT NULL DEFAULT '[]',
    fight_actions text[]  NOT NULL DEFAULT '{}' CHECK (cardinality(fight_actions) <= 4),
    die_actions   text[]  NOT NULL DEFAULT '{}' CHECK (cardinality(die_actions) <= 4)
);
CREATE INDEX npc_generators_leader ON gamedata.npc_generators (leader);
CREATE INDEX npc_generators_follower ON gamedata.npc_generators (follower);

-- Generators and NPC templates reach the running TMSRVs like the other tables.
CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.npc_generators
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.mob_templates
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
