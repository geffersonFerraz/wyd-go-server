-- Each channel has its own city owners, as in the C++ server
-- (ChargedGuildList[channel][city]). The owners set so far held for every
-- channel, so each one starts with a copy of them.
ALTER TABLE gamedata.city_owners ADD COLUMN channel smallint NOT NULL DEFAULT 1 CHECK (channel BETWEEN 1 AND 10);
ALTER TABLE gamedata.city_owners DROP CONSTRAINT city_owners_pkey;
ALTER TABLE gamedata.city_owners ADD PRIMARY KEY (channel, city);
INSERT INTO gamedata.city_owners (channel, city, guild_id, changed_by, changed_at)
    SELECT n, city, guild_id, changed_by, changed_at
    FROM gamedata.city_owners, generate_series(2, 10) AS n;
