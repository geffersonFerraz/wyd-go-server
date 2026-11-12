-- The experience rule the staff tunes in the admin panel: base experience of
-- a monster, the effect of the level difference, the share of each evolution
-- and the global multiplier (events). One row; the fields are in settings
-- (store.ExpRule) and are validated by the panel and by the TMSRV.
-- A row with "Enabled": false keeps the calculation of the C++ server.
CREATE TABLE gamedata.exp_rule (
    id         boolean     PRIMARY KEY DEFAULT true CHECK (id),
    settings   jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TRIGGER notify_changed AFTER INSERT OR UPDATE OR DELETE OR TRUNCATE ON gamedata.exp_rule
    FOR EACH STATEMENT EXECUTE FUNCTION gamedata.notify_changed();
