-- While a player is online, the live copy of the account is in TMSRV's
-- memory and every TMSRV save rewrites the whole account. A direct SQL edit
-- would be silently undone by the next save (and never seen in game). This
-- migration makes such edits fail loudly instead.

-- Accounts online right now, maintained by dbsrv.
CREATE TABLE online_accounts (
    account_id bigint      PRIMARY KEY REFERENCES accounts (id) ON DELETE CASCADE,
    username   text        NOT NULL,
    channel    smallint    NOT NULL,
    client     integer     NOT NULL,
    since      timestamptz NOT NULL DEFAULT now()
);

-- dbsrv's own connections set dbsrv.writer = 'on' (see store.Open) and are
-- allowed through. Everyone else (psql, pgAdmin, the admin panel) is blocked
-- while the account is online, unless dbsrv has stopped heartbeating (then
-- its sessions are gone and the rows are stale).
CREATE FUNCTION guard_online_account() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    acc  bigint;
    who  text;
    beat timestamptz;
BEGIN
    IF current_setting('dbsrv.writer', true) = 'on' THEN
        RETURN COALESCE(NEW, OLD);
    END IF;

    IF TG_TABLE_NAME = 'accounts' THEN
        acc := COALESCE(NEW.id, OLD.id);
    ELSE
        acc := COALESCE(NEW.account_id, OLD.account_id);
    END IF;

    SELECT o.username INTO who FROM online_accounts o WHERE o.account_id = acc;
    IF who IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;
    SELECT dbsrv_seen_at INTO beat FROM server_state WHERE id = 1;
    IF beat IS NULL OR beat < now() - interval '30 seconds' THEN
        RETURN COALESCE(NEW, OLD); -- dbsrv is down; nothing will overwrite this
    END IF;

    RAISE EXCEPTION 'A conta % está online no jogo: o próximo save do TMSRV desfaria esta alteração.', who
        USING HINT = 'Desconecte o jogador (painel admin: "Desconectar do jogo") e repita a alteração.',
              ERRCODE = 'object_in_use';
END $$;

CREATE TRIGGER accounts_online_guard BEFORE UPDATE OR DELETE ON accounts
    FOR EACH ROW EXECUTE FUNCTION guard_online_account();
CREATE TRIGGER characters_online_guard BEFORE INSERT OR UPDATE OR DELETE ON characters
    FOR EACH ROW EXECUTE FUNCTION guard_online_account();
CREATE TRIGGER items_online_guard BEFORE INSERT OR UPDATE OR DELETE ON items
    FOR EACH ROW EXECUTE FUNCTION guard_online_account();

-- New delivery kind: save and disconnect an online player.
ALTER TABLE deliveries DROP CONSTRAINT deliveries_kind_check;
ALTER TABLE deliveries ADD CONSTRAINT deliveries_kind_check
    CHECK (kind IN ('create_account', 'password', 'ban', 'item', 'cash', 'kick'));
