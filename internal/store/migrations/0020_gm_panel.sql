-- GM commands now come from the admin panel, and /gm in the game is a
-- message from the player to the staff.
ALTER TABLE deliveries DROP CONSTRAINT deliveries_kind_check;
ALTER TABLE deliveries ADD CONSTRAINT deliveries_kind_check
    CHECK (kind IN ('create_account', 'password', 'ban', 'item', 'cash', 'kick', 'edit', 'notice', 'gm_command'));

CREATE TABLE gm_messages (
    id         bigserial   PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    channel    smallint    NOT NULL,
    username   text        NOT NULL,
    char_name  text        NOT NULL,
    text       text        NOT NULL,
    seen_at    timestamptz,
    seen_by    text        NOT NULL DEFAULT ''
);
CREATE INDEX gm_messages_unseen ON gm_messages (id) WHERE seen_at IS NULL;
