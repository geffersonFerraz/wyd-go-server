-- The ranking window and the profile of ranked characters (the C++ kept
-- Ranking.txt and Rank\<name>.bin per channel). Every TMSRV sends its
-- characters to DBSRV, so all channels show the same list.
CREATE TABLE ranking (
    name_key    text        PRIMARY KEY CHECK (name_key = lower(name_key)),
    name        text        NOT NULL,
    level       integer     NOT NULL,
    class_info  integer     NOT NULL,
    evolution   integer     NOT NULL,
    guild_index integer     NOT NULL,
    patent      integer     NOT NULL,
    kingdom     integer     NOT NULL,
    frags       integer     NOT NULL,
    power       integer     NOT NULL,
    fame        integer     NOT NULL,
    citizen     integer     NOT NULL,
    pvp_kill    integer     NOT NULL,
    pvm_kill    integer     NOT NULL,
    mantle      integer     NOT NULL,
    exp         bigint      NOT NULL,
    equip       bytea       NOT NULL,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
