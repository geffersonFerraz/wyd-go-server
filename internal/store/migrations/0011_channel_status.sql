-- dbsrv also writes, with each heartbeat, how many accounts are online on
-- each channel (-1 = TMSRV not connected). The admin panel serves it as
-- servNN.htm, the URL the client reads the channel list counts from.
ALTER TABLE server_state
    ADD COLUMN channel_online integer[] NOT NULL DEFAULT '{}';
