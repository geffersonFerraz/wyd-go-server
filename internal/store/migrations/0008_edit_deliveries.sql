-- "edit" deliveries: staff edits of a character or the storage from the
-- admin panel (package acctedit), applied live by TMSRV when in game.
ALTER TABLE deliveries DROP CONSTRAINT deliveries_kind_check;
ALTER TABLE deliveries ADD CONSTRAINT deliveries_kind_check
    CHECK (kind IN ('create_account', 'password', 'ban', 'item', 'cash', 'kick', 'edit'));
