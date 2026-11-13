-- Several servers (worlds) on one database.
--
-- Shared by every server (schema public / gamedata): accounts (login,
-- password, ban, access, cash, honra, e-mail), account locks, MAC bans,
-- the game data, the delivery queue and who is online.
--
-- Per server (schema s<group>: s1, s2...): the world of that server, i.e.
-- characters, names, items, storage (items + gold in account_world), guilds,
-- seals, ranking, city owners, channel state, stores, item tracks, staff
-- messages and alerts. Each program connects with
--     search_path = s<group>, public
-- so the same unqualified queries reach the world of its own server and
-- nothing else. In psql, qualify: s1.characters, s2.characters.
--
-- This migration turns the single world there was into server 1. The tables
-- of a new server are created by the world migrations (migrations/world).

CREATE SCHEMA s1;

ALTER TABLE characters        SET SCHEMA s1;
ALTER TABLE character_names   SET SCHEMA s1;
ALTER TABLE items             SET SCHEMA s1;
ALTER TABLE guilds            SET SCHEMA s1;
ALTER TABLE guild_relations   SET SCHEMA s1;
ALTER TABLE seals             SET SCHEMA s1;
ALTER TABLE server_state      SET SCHEMA s1;
ALTER TABLE ranking           SET SCHEMA s1;
ALTER TABLE channel_state     SET SCHEMA s1;
ALTER TABLE open_stores       SET SCHEMA s1;
ALTER TABLE store_sales       SET SCHEMA s1;
ALTER TABLE item_tracks       SET SCHEMA s1;
ALTER TABLE item_track_events SET SCHEMA s1;
ALTER TABLE gm_messages       SET SCHEMA s1;
ALTER TABLE alerts            SET SCHEMA s1;
ALTER TABLE gamedata.city_owners SET SCHEMA s1;

-- The part of an account that belongs to a world: storage gold and the rest
-- of the C++ STRUCT_ACCOUNT image (friends, daily reward, position...). An
-- account without a row has not played on that server yet.
CREATE TABLE s1.account_world (
    account_id    bigint      PRIMARY KEY REFERENCES public.accounts (id) ON DELETE CASCADE,
    novato        bigint      NOT NULL DEFAULT 0 CHECK (novato BETWEEN 0 AND 4294967295),
    storage_gold  bigint      NOT NULL DEFAULT 0 CHECK (storage_gold BETWEEN 0 AND 1000000000000000),
    already_reset boolean     NOT NULL DEFAULT false,
    data          jsonb       NOT NULL DEFAULT '{"CharSlot": -1}',
    updated_at    timestamptz NOT NULL DEFAULT now()
);
INSERT INTO s1.account_world (account_id, novato, storage_gold, already_reset, data, updated_at)
    SELECT id, novato, storage_gold, already_reset, data, updated_at FROM accounts;
ALTER TABLE accounts
    DROP COLUMN novato,
    DROP COLUMN storage_gold,
    DROP COLUMN already_reset,
    DROP COLUMN data;

-- Heartbeat and online counts of each DBSRV (they were in server_state).
CREATE TABLE server_status (
    server         smallint    PRIMARY KEY CHECK (server BETWEEN 1 AND 10),
    dbsrv_protocol integer     NOT NULL DEFAULT 0,
    dbsrv_seen_at  timestamptz,
    channel_online integer[]   NOT NULL DEFAULT '{}'
);
INSERT INTO server_status (server, dbsrv_protocol, dbsrv_seen_at, channel_online)
    SELECT 1, dbsrv_protocol, dbsrv_seen_at, channel_online FROM s1.server_state;
ALTER TABLE s1.server_state
    DROP COLUMN dbsrv_protocol,
    DROP COLUMN dbsrv_seen_at,
    DROP COLUMN channel_online;

-- An account plays on one server at a time (cash and bans are shared, and a
-- TMSRV save rewrites the account): the row is also the lease of the account,
-- held by the DBSRV of that server.
ALTER TABLE online_accounts ADD COLUMN server smallint NOT NULL DEFAULT 1 CHECK (server BETWEEN 1 AND 10);
ALTER TABLE online_accounts ALTER COLUMN server DROP DEFAULT;

-- Each delivery is processed by the DBSRV of one server.
ALTER TABLE deliveries ADD COLUMN server smallint NOT NULL DEFAULT 1 CHECK (server BETWEEN 1 AND 10);
ALTER TABLE deliveries ALTER COLUMN server DROP DEFAULT;
DROP INDEX deliveries_pending_idx;
CREATE INDEX deliveries_pending_idx ON deliveries (server, id) WHERE processed_at IS NULL;

-- The online guard looks at the heartbeat of the server the account is on.
CREATE OR REPLACE FUNCTION public.guard_online_account() RETURNS trigger
LANGUAGE plpgsql AS $$
DECLARE
    acc  bigint;
    who  text;
    srv  smallint;
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

    SELECT o.username, o.server INTO who, srv FROM public.online_accounts o WHERE o.account_id = acc;
    IF who IS NULL THEN
        RETURN COALESCE(NEW, OLD);
    END IF;
    SELECT dbsrv_seen_at INTO beat FROM public.server_status WHERE server = srv;
    IF beat IS NULL OR beat < now() - interval '30 seconds' THEN
        RETURN COALESCE(NEW, OLD); -- that dbsrv is down; nothing will overwrite this
    END IF;

    RAISE EXCEPTION 'A conta % está online no jogo (servidor %): o próximo save do TMSRV desfaria esta alteração.', who, srv
        USING HINT = 'Desconecte o jogador (painel admin: "Desconectar do jogo") e repita a alteração.',
              ERRCODE = 'object_in_use';
END $$;

CREATE TRIGGER account_world_online_guard BEFORE INSERT OR UPDATE OR DELETE ON s1.account_world
    FOR EACH ROW EXECUTE FUNCTION public.guard_online_account();

-- Server 1 already has every world table: its world migrations start after
-- the baseline (migrations/world/0001_init.sql, which is this same layout).
CREATE TABLE s1.world_migrations (
    version    text PRIMARY KEY,
    applied_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO s1.world_migrations (version) VALUES ('0001_init');
