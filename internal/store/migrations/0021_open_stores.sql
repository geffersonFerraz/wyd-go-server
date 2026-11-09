-- Stores open in the game, refreshed by each TMSRV once a minute.
CREATE TABLE open_stores (
    channel    smallint    NOT NULL,
    owner_name text        NOT NULL,
    store_name text        NOT NULL,
    items      integer     NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (channel, owner_name)
);
