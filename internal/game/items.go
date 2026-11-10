package game

import "dbsrv/internal/wire"

// Item helpers ported from GetFunc.cpp / Basedef.cpp.

// itemSanc is GetItemSanc.
func itemSanc(it *wire.Item) int {
	if it.Index >= 2360 && it.Index <= 2389 { // mounts
		v := int(it.Effect[2].Index) / 10
		return min(v, 9)
	}
	if it.Index >= 2330 && it.Index <= 2359 { // mount eggs
		return 0
	}
	value := 0
	switch {
	case it.Effect[0].Index == EF_SANC:
		value = int(it.Effect[0].Value)
	case it.Effect[1].Index == EF_SANC:
		value = int(it.Effect[1].Value)
	case it.Effect[2].Index == EF_SANC:
		value = int(it.Effect[2].Value)
	default:
		for i := 0; i < 3; i++ {
			if it.Effect[i].Index >= 116 && it.Effect[i].Index <= 125 {
				value = int(it.Effect[i].Value)
				break
			}
		}
	}
	if value >= 230 {
		return min(10+(value-230)/4, 15)
	}
	return value % 10
}

// effectValueByIndex is GetEffectValueByIndex: the ItemList base value.
func (d *Data) effectValueByIndex(id uint16, eff int) int {
	it := d.Item(id)
	for _, e := range it.Effect {
		if int(e.Index) == eff {
			return int(e.Value)
		}
	}
	return 0
}

// itemAbility is GetItemAbility without mount stats (mMont, Phase C): it
// sums the ItemList value and the item's own effects, scaled by refining
// for combat effects.
func (d *Data) itemAbility(it *wire.Item, eff int) int {
	return d.ability(it, eff, true)
}

// itemAbilityNoSanc is GetItemAbilityNoSanc: the same without the refine
// multiplier.
func (d *Data) itemAbilityNoSanc(it *wire.Item, eff int) int {
	return d.ability(it, eff, false)
}

func (d *Data) ability(it *wire.Item, eff int, withSanc bool) int {
	data := d.Item(it.Index)
	unique := int(data.Unique)
	pos := int(data.Pos)
	result := 0

	if (eff == EF_DAMAGEADD || eff == EF_MAGICADD) && (unique < 41 || unique > 50) {
		return 0
	}
	if eff == EF_CRITICAL && (it.Effect[1].Index == EF_CRITICAL2 || it.Effect[2].Index == EF_CRITICAL2) {
		eff = EF_CRITICAL2
	}
	if eff == EF_DAMAGE && pos == 32 && (it.Effect[1].Index == EF_DAMAGE2 || it.Effect[2].Index == EF_DAMAGE2) {
		eff = EF_DAMAGE2
	}
	if eff == EF_ACADD && (it.Effect[1].Index == EF_ACADD2 || it.Effect[2].Index == EF_ACADD2) {
		eff = EF_ACADD2
	}
	switch {
	case eff == EF_LEVEL && it.Index >= 2330 && it.Index < 2360:
		result = int(it.Effect[1].Index) - 1
	case eff == EF_LEVEL:
		result += int(data.Level)
	}
	switch eff {
	case EF_REQ_STR:
		result += int(data.Str)
	case EF_REQ_INT:
		result += int(data.Int)
	case EF_REQ_DEX:
		result += int(data.Dex)
	case EF_REQ_CON:
		result += int(data.Con)
	case EF_POS:
		result += int(data.Pos)
	}
	if eff != EF_INCUBATE {
		for _, e := range data.Effect {
			if int(e.Index) != eff {
				continue
			}
			v := int(e.Value)
			if eff == EF_ATTSPEED && v == 1 {
				v = 10
			}
			result += v
			break
		}
	}

	if it.Index >= 2330 && it.Index < 2390 {
		switch eff {
		case EF_MOUNTHP:
			return int(uint16(it.Effect[0].Index) | uint16(it.Effect[0].Value)<<8)
		case EF_MOUNTSANC:
			return int(it.Effect[1].Index)
		case EF_MOUNTLIFE:
			return int(it.Effect[1].Value)
		case EF_MOUNTFEED:
			return int(it.Effect[2].Index)
		case EF_MOUNTKILL:
			return int(it.Effect[2].Value)
		}
		hp := int16(uint16(it.Effect[0].Index) | uint16(it.Effect[0].Value)<<8)
		if it.Index >= 2360 && hp > 0 {
			mont := mountData[it.Index-2360]
			lv := int(it.Effect[1].Index)
			switch eff {
			case EF_DAMAGE:
				return int(int16(mont.atk * (lv + 20) / 100))
			case EF_MAGIC:
				return int(int16(mont.magic * (lv + 15) / 100))
			case EF_PARRY:
				return mont.evasion
			case EF_RUNSPEED:
				return mont.speed
			case EF_RESIST1, EF_RESIST2, EF_RESIST3, EF_RESIST4, EF_RESISTALL:
				return mont.resist
			}
		}
		return result
	}
	if it.Index >= 3980 && it.Index <= 3999 && eff == EF_RUNSPEED {
		return 6
	}
	for i := 0; i < 3; i++ {
		if int(it.Effect[i].Index) != eff {
			continue
		}
		v := int(it.Effect[i].Value)
		if eff == EF_ATTSPEED && v == 1 {
			v = 10
		}
		result += v
	}
	if eff >= EF_RESIST1 && eff <= EF_RESIST4 {
		for _, e := range data.Effect {
			if e.Index == EF_RESISTALL {
				result += int(e.Value)
				break
			}
		}
		for i := 0; i < 3; i++ {
			if it.Effect[i].Index == EF_RESISTALL {
				result += int(it.Effect[i].Value)
				break
			}
		}
	}

	sanc := itemSanc(it)
	if it.Index <= 40 || !withSanc {
		sanc = 0
	}
	if sanc >= 9 && pos&0xF00 != 0 {
		sanc++
	}
	sanc = min(sanc, 15)
	switch eff {
	case EF_GRID, EF_CLASS, EF_POS, EF_WTYPE, EF_RANGE, EF_LEVEL, EF_REQ_STR, EF_REQ_INT, EF_REQ_DEX,
		EF_REQ_CON, EF_VOLATILE, EF_INCUBATE, EF_INCUDELAY, EF_UNKNOW1, EF_MOBTYPE:
	default:
		if sanc != 0 {
			v := sanc
			switch sanc {
			case 11:
				v = 12
			case 12:
				v = 15
			case 13:
				v = 18
			case 14:
				v = 23
			case 15:
				v = 28
			}
			result = result * (v + 10) / 10
		}
	}
	if eff == EF_RUNSPEED {
		if result >= 3 {
			result = 2
		}
		if result > 0 && sanc >= 9 {
			result++
		}
	}
	if eff == EF_GRID && (result < 0 || result > 7) {
		result = 0
	}
	return int(result)
}

// infoClass is GetInfoClass (class from the face id).
func infoClass(face int) int {
	switch {
	case face < 10:
		return 0
	case face < 20:
		return 1
	case face < 30 || face == 32:
		return 2
	case face < 40:
		return 3
	}
	return 4
}

// canEquip is CanEquip(item, mob, slot, class, packet, evolution).
func (d *Data) canEquip(it *wire.Item, mob *wire.Mob, slot int) bool {
	if it.Index == 0 || it.Index >= MaxItemList || slot == 15 {
		return false
	}
	unique := int(d.Item(it.Index).Unique)
	if slot != -1 {
		itemPos := d.itemAbility(it, EF_POS)
		if slot == 16 || slot == 17 || slot == 18 {
			return true
		}
		if (itemPos>>slot)&1 == 0 {
			return false
		}
		if slot == 6 || slot == 7 {
			other := 7
			if slot == 7 {
				other = 6
			}
			if oid := mob.Equip[other].Index; oid > 0 && oid < MaxItemList {
				otherUnique := int(d.Item(oid).Unique)
				otherPos := d.itemAbility(&mob.Equip[other], EF_POS)
				if itemPos == 64 || otherPos == 64 { // two-handed
					switch {
					case unique == 46: // throwing weapons
						if otherPos != 128 {
							return false
						}
					case otherUnique == 46:
						if itemPos != 128 {
							return false
						}
					default:
						return false
					}
				}
			}
		}
	}

	evo := int(mob.Equip[0].Effect[1].Value)
	class := infoClass(int(mob.Equip[0].Effect[1].Index))
	if (mob.Equip[0].Index >= 22 && mob.Equip[0].Index <= 25) || mob.Equip[0].Index == 32 {
		class = 2
		if evo >= evoArch {
			class = infoClass(int(mob.Equip[0].Effect[1].Index))
		}
	}
	itemClass := d.itemAbility(it, EF_CLASS)
	pos := d.itemAbility(it, EF_POS)
	if itemClass == 0 {
		return false
	}
	if evo == evoMortal {
		if (itemClass>>class)&1 == 0 {
			return false
		}
	} else if (itemClass>>class)&1 == 0 && pos <= 32 {
		return false
	}

	rLevel := d.itemAbility(it, EF_LEVEL)
	rSTR := d.itemAbility(it, EF_REQ_STR)
	rINT := d.itemAbility(it, EF_REQ_INT)
	rDEX := d.itemAbility(it, EF_REQ_DEX)
	rCON := d.itemAbility(it, EF_REQ_CON)
	wType := d.itemAbility(it, EF_WTYPE) % 10
	divWType := wType / 10 // always 0, as in the C++
	if slot == 7 && wType != 0 {
		porc := 100
		if divWType == 0 && wType > 1 {
			porc = 130
		} else if divWType == 6 && wType > 1 {
			porc = 150
		}
		rLevel, rSTR, rINT, rDEX, rCON = rLevel*porc/100, rSTR*porc/100, rINT*porc/100, rDEX*porc/100, rCON*porc/100
	}

	if mobType := d.effectValueByIndex(it.Index, EF_MOBTYPE); mobType != 0 {
		switch {
		case mobType == 2:
			if evo != evoMortal || rLevel > int(mob.BStatus.Level) {
				return false
			}
		case mobType == evoCelestial:
			return evo >= evoCelestial
		case evo != evoMortal:
			if slot == 1 {
				return it.Index >= 3500 && it.Index <= 3508
			}
		default:
			return false
		}
	}
	if evo >= evoArch {
		if slot == 1 {
			return (it.Index >= 3500 && it.Index <= 3508) || it.Index == 747
		}
		return true
	}
	s := &mob.Status
	return rSTR <= int(s.STR) && rINT <= int(s.INT) && rDEX <= int(s.DEX) && rCON <= int(s.CON) && rLevel <= int(s.Level)
}

// anctCode is GetAnctCode (the refine glow code shown to others).
func anctCode(it *wire.Item, usingCostume bool) byte {
	if it.Index >= 2360 && it.Index < 2389 {
		if it.Effect[2].Value >= 11 {
			return byte(min(it.Effect[2].Value, 255))
		}
		return 0
	}
	value := 0
	switch {
	case it.Effect[0].Index == EF_SANC:
		value = int(it.Effect[0].Value)
	case it.Effect[1].Index == EF_SANC:
		value = int(it.Effect[1].Value)
	case it.Effect[2].Index == EF_SANC:
		value = int(it.Effect[2].Value)
	default:
		for i := 0; i < 3; i++ {
			if it.Effect[i].Index >= 116 && it.Effect[i].Index <= 125 {
				if usingCostume {
					return byte(min(it.Effect[i].Value, 255))
				}
				return it.Effect[i].Index
			}
		}
	}
	if value == 0 {
		return 0
	}
	if value < 230 {
		return 43
	}
	switch value % 4 {
	case 0:
		return 0x30
	case 1:
		return 0x40
	case 2:
		return 0x10
	}
	return 0x20
}

// itemIDAndEffect is GetItemIDAndEffect: item id with the refine level in
// the top 4 bits.
func itemIDAndEffect(it *wire.Item, slot int, usingCostume bool) uint16 {
	if slot == 14 {
		return it.Index | uint16(int(it.Effect[1].Index)/10*4096)
	}
	value := 0
	colored := false
	found := false
	for i := 0; i < 3 && !found; i++ {
		if it.Effect[i].Index >= 116 && it.Effect[i].Index <= 125 {
			found = true
			if !usingCostume {
				value, colored = int(it.Effect[i].Index), true
			} else {
				value = int(it.Effect[i].Value)
			}
		}
	}
	if !found {
		for i := 0; i < 3 && !found; i++ {
			if it.Effect[i].Index == EF_SANC {
				found, value = true, int(it.Effect[i].Value)
			}
		}
	}
	if !found {
		return it.Index
	}
	if value > 9 && !colored {
		switch {
		case value < 234:
			value = 10
		case value < 238:
			value = 11
		case value < 242:
			value = 12
		case value < 246:
			value = 13
		case value < 250:
			value = 14
		case value < 254:
			value = 15
		default:
			value = 16
		}
	} else if colored && value >= 9 {
		value = 9
	}
	return it.Index | uint16(value*0x1000)
}

// mountData is mMont: physical/magic attack, evasion, resistance and run
// speed of the mounts 2360..2389.
var mountData = [30]struct{ atk, magic, evasion, resist, speed int }{
	{0, 1, 0, 0, 4}, {0, 1, 0, 0, 4}, {50, 9, 0, 0, 5}, {80, 14, 0, 0, 5}, {100, 19, 0, 0, 4},
	{150, 23, 0, 0, 5}, {250, 47, 40, 0, 6}, {300, 57, 50, 0, 6}, {350, 61, 60, 0, 6}, {400, 66, 70, 0, 6},
	{500, 80, 80, 0, 6}, {250, 47, 0, 16, 6}, {300, 57, 0, 20, 6}, {350, 61, 0, 24, 6}, {400, 66, 0, 28, 6},
	{500, 80, 0, 32, 6}, {550, 85, 0, 0, 6}, {600, 85, 0, 0, 6}, {550, 85, 0, 20, 6}, {650, 95, 60, 28, 6},
	{700, 104, 80, 32, 6}, {570, 85, 20, 16, 6}, {570, 85, 30, 8, 6}, {570, 85, 40, 12, 6}, {590, 90, 30, 20, 6},
	{600, 90, 40, 16, 6}, {600, 90, 50, 16, 6}, {600, 38, 60, 28, 6}, {300, 90, 60, 28, 6}, {150, 23, 0, 20, 6},
}
