-- Computers banned with +banmac (BlackList.txt in the C++ server).
CREATE TABLE mac_bans (
    mac        bytea       PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now()
);
