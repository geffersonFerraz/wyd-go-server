-- Gold in the storage and in the drop table is 64-bit now; the game caps
-- it with limits.max_gold of tmsrv.yaml (at most 1e15, wire.MaxLimit).
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_storage_gold_check;
ALTER TABLE accounts ADD CONSTRAINT accounts_storage_gold_check
    CHECK (storage_gold BETWEEN 0 AND 1000000000000000);

ALTER TABLE gamedata.drops ALTER COLUMN gold TYPE bigint;
