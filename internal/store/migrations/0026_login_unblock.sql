-- The panel can clear the "3 wrong passwords" lock the channels keep in memory.
ALTER TABLE deliveries DROP CONSTRAINT deliveries_kind_check;
ALTER TABLE deliveries ADD CONSTRAINT deliveries_kind_check
    CHECK (kind IN ('create_account', 'password', 'ban', 'item', 'cash', 'kick', 'edit', 'notice', 'gm_command', 'login_unblock'));
