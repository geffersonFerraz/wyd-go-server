-- Every sale in the auction stores, to investigate duplication and fraud.
CREATE TABLE store_sales (
    id         bigserial   PRIMARY KEY,
    sold_at    timestamptz NOT NULL DEFAULT now(),
    channel    smallint    NOT NULL,
    buyer      text        NOT NULL,
    seller     text        NOT NULL,
    item       integer     NOT NULL,
    effects    jsonb       NOT NULL,
    price      bigint      NOT NULL,
    tax        bigint      NOT NULL,
    category   smallint    NOT NULL
);
