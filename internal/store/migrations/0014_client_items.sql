-- The 15 items that existed only in the client's ItemList.bin, and room for
-- the client's positions above 16 bits (necklaces 65536, belts 131072).
-- An empty table (new install, tests) gets them from ItemList.csv instead.
ALTER TABLE gamedata.items DROP CONSTRAINT items_pos_check;
ALTER TABLE gamedata.items ADD CONSTRAINT items_pos_check CHECK (pos >= 0);

INSERT INTO gamedata.items (id, name, mesh1, mesh2, level, req_str, req_int, req_dex, req_con, price, unique_type, pos, extreme, grade)
SELECT v.* FROM (VALUES
    (3265, 'Cupom_da_Sorte', 2711, 0, 0, 0, 0, 0, 0, 10000, 0, 0, 0, 0),
    (3288, '1_de_honra', 2711, 0, 0, 0, 0, 0, 0, 10000, 0, 0, 0, 0),
    (4899, 'Removedor_de_Skin(Montaria)', 2885, 0, 0, 0, 0, 0, 0, 1200, 0, 0, 0, 0),
    (5715, 'Physical_Strength_belt(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 131072, 10000, 0),
    (5716, 'Dexterity_belt(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 131072, 10000, 0),
    (5717, 'Magical_belt(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 131072, 10000, 0),
    (5718, 'Power_belt(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 131072, 10000, 0),
    (5719, 'Madness_belt(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 131072, 10000, 0),
    (5720, 'Guardian_belt(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 131072, 10000, 0),
    (5765, 'Physical_Strength_necklace(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 65536, 10000, 0),
    (5766, 'Dexterity_necklace(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 65536, 10000, 0),
    (5767, 'Magical_necklace(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 65536, 10000, 0),
    (5768, 'Power_necklace(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 65536, 10000, 0),
    (5769, 'Madness_necklace(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 65536, 10000, 0),
    (5770, 'Guardian_necklace(Arcane)', 0, 0, 0, 0, 0, 0, 0, 10000000, 0, 65536, 10000, 0)
) AS v(id, name, mesh1, mesh2, level, req_str, req_int, req_dex, req_con, price, unique_type, pos, extreme, grade)
WHERE EXISTS (SELECT 1 FROM gamedata.items)
ON CONFLICT (id) DO NOTHING;

INSERT INTO gamedata.item_effects (item_id, slot, effect, value)
SELECT v.* FROM (VALUES
    (5715, 2, 18, 255),
    (5715, 3, 45, 20),
    (5715, 4, 3, 80),
    (5715, 5, 0, 4),
    (5715, 6, 112, 2),
    (5716, 2, 18, 255),
    (5716, 3, 26, 250),
    (5716, 4, 3, 80),
    (5716, 5, 0, 4),
    (5716, 6, 112, 2),
    (5717, 2, 18, 255),
    (5717, 3, 60, 12),
    (5717, 4, 3, 80),
    (5717, 5, 0, 4),
    (5717, 6, 112, 2),
    (5718, 2, 18, 255),
    (5718, 3, 2, 54),
    (5718, 4, 3, 80),
    (5718, 5, 0, 4),
    (5718, 6, 112, 2),
    (5719, 2, 18, 255),
    (5719, 3, 76, 15),
    (5719, 4, 3, 100),
    (5719, 5, 0, 4),
    (5719, 6, 112, 2),
    (5720, 2, 18, 255),
    (5720, 3, 77, 13),
    (5720, 4, 3, 100),
    (5720, 5, 0, 4),
    (5720, 6, 112, 2),
    (5765, 2, 18, 255),
    (5765, 3, 45, 20),
    (5765, 4, 0, 4),
    (5765, 5, 112, 2),
    (5766, 2, 18, 255),
    (5766, 3, 26, 250),
    (5766, 4, 0, 4),
    (5766, 5, 112, 2),
    (5767, 2, 18, 255),
    (5767, 3, 60, 12),
    (5767, 4, 0, 4),
    (5767, 5, 112, 2),
    (5768, 2, 18, 255),
    (5768, 3, 2, 54),
    (5768, 4, 0, 4),
    (5768, 5, 112, 2),
    (5769, 2, 18, 255),
    (5769, 3, 76, 13),
    (5769, 4, 0, 5),
    (5769, 5, 112, 2),
    (5770, 2, 18, 255),
    (5770, 3, 77, 11),
    (5770, 4, 0, 5),
    (5770, 5, 112, 2)
) AS v(item_id, slot, effect, value)
WHERE EXISTS (SELECT 1 FROM gamedata.items i WHERE i.id = v.item_id)
ON CONFLICT (item_id, slot) DO NOTHING;

INSERT INTO gamedata.change_log (changed_by, entity, key, summary)
SELECT 'migration', 'item', 'itemlist.bin', '15 itens trazidos do ItemList.bin do cliente'
WHERE EXISTS (SELECT 1 FROM gamedata.items);
