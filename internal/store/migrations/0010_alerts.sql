-- Security alerts raised by the TMSRVs (e.g. a WPE warning from the client
-- protection), stored by DBSRV and shown in the admin panel ("Alertas").
CREATE TABLE alerts (
    id         bigserial   PRIMARY KEY,
    created_at timestamptz NOT NULL DEFAULT now(),
    kind       text        NOT NULL,
    username   text        NOT NULL DEFAULT '',
    char_name  text        NOT NULL DEFAULT '',
    ip         text        NOT NULL DEFAULT '',
    channel    smallint    NOT NULL DEFAULT 0,
    detail     text        NOT NULL DEFAULT '',
    seen_at    timestamptz,
    seen_by    text        NOT NULL DEFAULT ''
);
CREATE INDEX alerts_unseen ON alerts (id) WHERE seen_at IS NULL;
CREATE INDEX alerts_username ON alerts (username, id DESC);
