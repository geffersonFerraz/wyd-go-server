-- City owners set in the admin panel (one guild per city, every channel).
-- A city without a row keeps the owner of TMSRV's guild_zone.txt. Changes
-- reach the running TMSRVs through the gamedata live reload.
CREATE TABLE gamedata.city_owners (
    city       smallint    PRIMARY KEY CHECK (city BETWEEN 0 AND 4),
    guild_id   integer     NOT NULL CHECK (guild_id >= 0 AND guild_id < 65535),
    changed_by text        NOT NULL DEFAULT '',
    changed_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.city_owners
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
