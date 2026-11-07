-- Phase 4: state the C++ DBSRV kept only in memory or in drop folders.

-- Kefra: which channel killed it first this week (10 = nobody yet,
-- MAX_SERVERGROUP in C++) and who. Lost on every C++ restart.
ALTER TABLE server_state
    ADD COLUMN first_kefra_channel integer NOT NULL DEFAULT 10,
    ADD COLUMN kefra_killer        text    NOT NULL DEFAULT '';

-- Audit: when a seal was taken out (3132). The C++ never removed the file.
ALTER TABLE seals ADD COLUMN used_at timestamptz;

-- Replaces the DATA/Import{User,Pass,Ban,Item,Cash}/*.txt drop folders.
-- Insert a row; DBSRV picks it up within a second and fills processed_at/result.
--
--   kind            payload
--   create_account  {"password": "...", "access": 0}
--   password        {"password": "...", "numeric": "1234"}      (numeric optional)
--   ban             {"type": 1, "until": "2026-12-31T23:59:00-03:00"}
--   item            {"item_id": 4140, "effects": [[61, 10], [0, 0], [0, 0]]}
--   cash            {"amount": 1000}
CREATE TABLE deliveries (
    id           bigserial   PRIMARY KEY,
    kind         text        NOT NULL CHECK (kind IN ('create_account', 'password', 'ban', 'item', 'cash')),
    username     text        NOT NULL,
    payload      jsonb       NOT NULL DEFAULT '{}',
    created_at   timestamptz NOT NULL DEFAULT now(),
    attempts     integer     NOT NULL DEFAULT 0,
    processed_at timestamptz,
    result       text
);
CREATE INDEX deliveries_pending_idx ON deliveries (id) WHERE processed_at IS NULL;
