-- War and tax state of each channel: what the C++ TMSRV kept in
-- guild_zone.txt and gameconfig_v2.xml.
CREATE TABLE channel_state (
    channel    smallint    PRIMARY KEY,
    state      jsonb       NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
