package game

import "dbsrv/internal/wire"

// LevelItem is STRUCT_LEVELITEM: an item given on reaching a level
// (level_items of tmsrv.yaml).
type LevelItem struct {
	Evolution, Level, Class, Type int
	Item                          wire.Item
}

// noCPArea is the g_pPositionCP check.
func (g *Game) noCPArea(x, y int) bool {
	for _, a := range g.cfg.NoCPAreas {
		if x >= a.MinX && x <= a.MaxX && y >= a.MinY && y <= a.MaxY {
			return true
		}
	}
	return false
}

// levelItemApplies checks class and attribute build of a reward.
func levelItemApplies(li *LevelItem, pl *wire.Mob) bool {
	if li.Type != -1 {
		class := int(pl.ClassInfo)
		if evolution(pl) >= evoArch {
			class = infoClass(int(pl.Equip[0].Effect[1].Index))
		}
		if class != li.Class {
			return false
		}
	}
	st := &pl.Status
	switch li.Type {
	case 1:
		return int(st.STR)+int(st.DEX) > int(st.INT)
	case 2:
		return int(st.INT) > int(st.STR)+int(st.DEX)
	case 3:
		return st.DEX > st.STR
	}
	return true
}

// giveLevelItems is LevelItem: the rewards of the level just reached go to
// the inventory, or to the storage when it is full.
func (g *Game) giveLevelItems(u *User) {
	pl := &g.mobs[u.id].info.Player
	level, ev := int(pl.Status.Level), evolution(pl)
	for i := range g.cfg.LevelItems {
		li := &g.cfg.LevelItems[i]
		if li.Level != level || li.Evolution != ev || !levelItemApplies(li, pl) {
			continue
		}
		name := g.itemName(li.Item.Index)
		if slot := firstCarrySlot(&pl.Inventory, li.Item.Index); slot >= 0 {
			pl.Inventory[slot] = li.Item
			g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
		} else {
			slot = firstStorageSlot(u.account.StorageItems[:], li.Item.Index)
			if slot < 0 {
				g.clientMessage(u, "Sem espaço no banco para receber o item!")
				g.log.Info("level item not given: storage full", "slot", u.id, "item", li.Item.Index)
				continue
			}
			u.account.StorageItems[slot] = li.Item
			g.sendItem(u, wire.SlotStorage, slot, &u.account.StorageItems[slot])
		}
		g.clientMessage(u, "Chegou um item: [ %s ]", name)
		g.log.Info("level item", "slot", u.id, "char", g.charName(u.id), "item", li.Item.Index, "level", level)
	}
}
