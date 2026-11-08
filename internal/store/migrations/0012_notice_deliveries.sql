-- "notice" deliveries: a notice from the admin panel to every channel
-- (the C++ staff used the NP program). Username is "*".
ALTER TABLE deliveries DROP CONSTRAINT deliveries_kind_check;
ALTER TABLE deliveries ADD CONSTRAINT deliveries_kind_check
    CHECK (kind IN ('create_account', 'password', 'ban', 'item', 'cash', 'kick', 'edit', 'notice'));
