-- Item effect values are 16-bit (STRUCT_ITEM is 14 bytes now). smallint stops
-- at 32767, and the drop table only accepted up to 255.
ALTER TABLE items
    ALTER COLUMN efv1 TYPE integer,
    ALTER COLUMN efv2 TYPE integer,
    ALTER COLUMN efv3 TYPE integer;

ALTER TABLE donate_store
    ALTER COLUMN efv1 TYPE integer,
    ALTER COLUMN efv2 TYPE integer,
    ALTER COLUMN efv3 TYPE integer;

ALTER TABLE gamedata.drop_items DROP CONSTRAINT IF EXISTS drop_items_value_check;
ALTER TABLE gamedata.drop_items ALTER COLUMN value TYPE integer;
ALTER TABLE gamedata.drop_items ADD CONSTRAINT drop_items_value_check CHECK (value BETWEEN 0 AND 65535);
