-- Live reload: any change to a gamedata table notifies the TMSRVs
-- (LISTEN gamedata_changed), which reload the tables without a restart.
-- One notification per statement; identical ones in a transaction collapse.
CREATE FUNCTION gamedata.notify_changed() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('gamedata_changed', TG_TABLE_NAME);
    RETURN NULL;
END $$;

CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.effect_names
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.items
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.item_effects
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.skills
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.drops
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.drop_items
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.teleports
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();

-- Who changed what (written by the admin panel and gamedata-import).
CREATE TABLE gamedata.change_log (
    id         bigserial   PRIMARY KEY,
    changed_at timestamptz NOT NULL DEFAULT now(),
    changed_by text        NOT NULL,
    entity     text        NOT NULL,
    key        text        NOT NULL,
    summary    text        NOT NULL DEFAULT ''
);
CREATE INDEX change_log_entity ON gamedata.change_log (entity, key, changed_at DESC);
