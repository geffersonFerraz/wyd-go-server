package game

import (
	"fmt"

	"dbsrv/internal/wire"
)

// Helpers for RequestMerchantNPC: the event NPCs of Data/NPCEvento.txt
// (Basedef.cpp LoadNPCEvento, RequestMerchantNPC.cpp NPCEvento), SendSay,
// the 3-argument SetAffect, Combine and SetItemBonus(item, level, sanc,
// bonus).

const maxNPCEvent = 10 // MAX_NPCEVENTO

// NPCEvent is STRUCT_NPC_EVENT: an exchange NPC (Status.Level 500..509).
type NPCEvent struct {
	ItemRequired   [10]wire.Item
	AmountRequired [10]int
	GoldRequired   int
	ItemEarned     [10][10]wire.Item
	Rates          [10]int // tier i is given when Rand()%300 <= Rates[i]
	Teleport       [10][2]int
	Msg            [10]string
	Name           string // npcName: what the NPC says at the end
}

// sendSay is SendSay: an NPC (or anyone) says a line to everyone in view.
func (g *Game) sendSay(id int, format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	p := wire.Chat{Header: wire.NewHeader(wire.OpCliChat, uint16(id))}
	wire.PutGameString(p.Text[:min(96, len(p.Text)-1)], msg)
	t := g.mobs[id].target
	g.multicast2(int(t.X), int(t.Y), &p, 0)
}

// setAffectTime is SetAffect(clientId, skillId, time): the skill's affect
// for a fixed time, value 1.
func (g *Game) setAffectTime(id, skill, ticks int) {
	if skill < 0 || skill >= len(g.data.Skills) {
		return
	}
	sd := &g.data.Skills[skill]
	slot := g.emptyAffect(id, sd.AffectType)
	if sd.AffectType <= 0 || slot < 0 || slot >= len(g.mobs[id].info.Affects) {
		return
	}
	g.mobs[id].info.Affects[slot] = wire.Affect{Index: uint8(sd.AffectType), Master: uint8(sd.AffectValue), Time: uint32(ticks), Value: 1}
	g.getCurrentScore(id)
}

// combine is Combine(clientId, searched, earned): ten of searched become
// one more of earned (stacks up to 120).
func (g *Game) combine(u *User, searched, earned uint16) {
	pl := &g.mobs[u.id].info.Player
	if inventoryAmount(pl, searched) < 10 {
		return
	}
	taken := 0
	for i := 0; i < invSlots && taken < 10; i++ {
		for pl.Inventory[i].Index == searched && taken < 10 {
			amountMinus(&pl.Inventory[i])
			taken++
		}
	}
	for i := range invSlots {
		it := &pl.Inventory[i]
		if it.Index != earned {
			continue
		}
		amount := itemAmount(it)
		if amount >= 120 {
			continue
		}
		k := -1
		for e := range 3 {
			if it.Effect[e].Index == EF_AMOUNT {
				k = e
				break
			}
		}
		if k < 0 {
			for e := range 3 {
				if it.Effect[e].Index == 0 {
					k = e
					break
				}
			}
		}
		if k < 0 {
			continue
		}
		it.Effect[k] = wire.ItemEffect{Index: EF_AMOUNT, Value: uint16(amount + 1)}
		g.sendItem(u, wire.SlotInv, i, it)
		return
	}
	if !g.putItem(u, &wire.Item{Index: earned}) {
		g.log.Warn("combine: no room", "slot", u.id, "item", earned)
	}
}

var bonusEffects = [10]int{4, 54, 11, 12, 13, 14, 7, 8, 9, 10} // dwValue

// bonusRanges is dwValue_02[10][3][2]: min/max of each bonus by tier.
var bonusRanges = [10][3][2]int{
	{{10, 30}, {20, 45}, {30, 60}},
	{{2, 5}, {4, 8}, {5, 10}},
	{{3, 10}, {7, 20}, {10, 30}},
	{{2, 8}, {5, 14}, {8, 20}},
	{{2, 8}, {5, 14}, {8, 20}},
	{{2, 8}, {5, 14}, {8, 20}},
	{{2, 6}, {4, 12}, {6, 16}},
	{{2, 6}, {4, 12}, {6, 16}},
	{{2, 6}, {4, 12}, {6, 16}},
	{{2, 6}, {4, 12}, {6, 16}},
}

// setItemBonusLevel is SetItemBonus(item, level, sanc, bonus): rolls the
// three effects of an item as if it dropped from a level `level` mob.
func (g *Game) setItemBonusLevel(it *wire.Item, level, sanc, bonus int) {
	extra := min(max(bonus>>3, 0), 2)
	gradeTier, gradeSanc := -1, -1
	if e := it.Effect[0].Index; e >= 100 && e <= 105 {
		gradeTier, gradeSanc = int(e)-100, int(it.Effect[0].Value)
		it.Effect[0] = wire.ItemEffect{}
	}
	if sanc == 0 && level >= 210 {
		level -= 47
	}
	d := g.data.Item(it.Index)
	unique, pos := int(d.Unique), int(d.Pos)
	tier := (level - int(d.Level)) / 25
	if gradeTier != -1 {
		tier = gradeTier
	}
	high := tier >= 4
	tier = min(max(tier, 0), 3)
	mobType := g.data.itemAbility(it, EF_MOBTYPE)
	if pos&254 == 0 || it.Effect[0].Index != 0 || pos == 128 {
		return
	}
	if extra == 0 {
		extra = 1
	}
	r := rnd() % 101
	if r == 0 {
		r = 1
	}
	t1 := 100
	switch {
	case sanc != 0:
		t1 = r % 3
	case tier == 0:
		t1 = r % (7 - extra)
	case tier == 1, tier == 2:
		t1 = r % (5 - extra)
	default:
		t1 = r & 3
	}
	ef1, base1, mod1 := EF_UNIQUE, 0, 0
	switch {
	case pos == 2:
		switch t1 {
		case 0:
			ef1, base1 = EF_ATTSPEED, 3
		case 1:
			ef1, base1 = EF_MAGIC, 2
		}
	case pos == 4 || pos == 8:
		ef1, base1, mod1 = EF_CRITICAL2, 10, 1
		if t1 == 2 {
			ef1, base1, mod1 = EF_AC, 5, 0
		}
	case pos == 16:
		ef1, base1 = EF_ACADD2, 5
	case pos == 32:
		ef1, base1, mod1 = EF_DAMAGE2, 6, -1
	case pos == 64 || pos == 192:
		switch {
		case unique == 44 || unique == 47:
			switch t1 {
			case 0:
				ef1, base1, mod1 = EF_MAGIC, 4, -1
			case 1:
				ef1, base1, mod1 = EF_SPECIALALL, 3, -1
			}
		case t1 == 0:
			ef1, base1, mod1 = EF_ATTSPEED, 3, 1
		case t1 == 1:
			ef1, base1, mod1 = EF_DAMAGE, 9, -1
		case t1 == 2:
			ef1, base1 = EF_SPECIALALL, 3
		}
	}

	r2 := rnd() % 100
	if sanc != 0 {
		r2 = (r2 << 1) / 3
	}
	t2 := 100
	switch {
	case sanc != 0:
		t2 = r2 & 3
	case tier == 0:
		t2 = r2 & 7
	case tier == 1 || tier == 2:
		t2 = r2 % 6
	default:
		t2 = r2 & 3
	}
	ef2, base2, mod2 := EF_UNIQUE, 0, 0
	switch pos {
	case 2:
		switch t2 {
		case 0:
			ef2, base2 = EF_HP, 10
		case 1:
			ef2, base2, mod2 = EF_AC, 5, -1
		}
	case 4, 8:
		switch t2 {
		case 2:
			ef2, base2, mod2 = EF_AC, 5, -1
		case 0:
			ef2, base2, mod2 = EF_MAGIC, 2, -1
		case 1:
			ef2, base2, mod2 = EF_DAMAGE, 6, -1
		}
	case 16:
		switch t2 {
		case 0:
			ef2, base2, mod2 = EF_MAGIC, 2, -1
		case 1:
			ef2, base2, mod2 = EF_DAMAGE, 6, -1
		case 2:
			ef2, base2 = EF_SPECIALALL, 3
		case 3:
			ef2, base2 = EF_RESISTALL, 3
		}
	case 32:
		switch t2 {
		case 0:
			ef2, base2, mod2 = EF_MAGIC, 2, -1
		case 1:
			ef2, base2 = EF_SPECIALALL, 3
		}
	case 64, 128, 192:
		if unique == 44 || unique == 47 {
			switch t2 {
			case 0:
				ef2, base2, mod2 = EF_MAGIC, 4, -1
			case 1:
				ef2, base2, mod2 = EF_SPECIALALL, 3, 1
			}
		} else {
			switch t2 {
			case 0:
				ef2, base2, mod2 = EF_ATTSPEED, 3, 1
			case 1:
				ef2, base2, mod2 = EF_DAMAGE, 9, -1
			case 2:
				ef2, base2, mod2 = EF_SPECIALALL, 3, 1
			}
		}
	}

	roll := rnd() % 100
	v1 := 0
	switch tier {
	case 0:
		v1 = pick(roll, []int{2, 6, 24, 55}, []int{4, 3, 2, 1}, 0)
	case 1:
		v1 = pick(roll, []int{3, 12, 24, 65}, []int{5, 4, 3, 2}, 1)
	case 2:
		v1 = pick(roll, []int{5, 20, 60}, []int{5, 4, 3}, 2)
	case 3:
		v1 = pick(roll, []int{16, 40, 80}, []int{6, 5, 4}, 3) // "<= 15" is "< 16"
	}
	// The C++ also has tables for tier >= 4, but tier was already clamped
	// to 3; only this floor is left of them.
	if high && v1 < 4 {
		v1 = 4
	}
	v1 += mod1
	if sanc != 0 && v1 == 0 {
		v1 = 1
	}
	if mobType == 1 && v1 >= 3 {
		v1 = 2
	}
	switch {
	case it.Effect[1].Index == 0 && v1 > 0:
		it.Effect[1] = wire.ItemEffect{Index: uint8(ef1), Value: uint16(v1 * base1)}
	case it.Effect[1].Index == 0 && pos == 32:
		it.Effect[1] = wire.ItemEffect{Index: uint8(ef1)}
	case it.Effect[1].Index == 0:
		it.Effect[1] = wire.ItemEffect{Index: EF_UNIQUE, Value: uint16(rnd() % 127)}
	}

	roll2 := rnd() % 100
	v2 := 0
	switch tier {
	case 0:
		v2 = pick(roll2, []int{2, 6, 24, 55}, []int{4, 3, 2, 1}, 0)
	case 1:
		v2 = pick(roll2, []int{3, 12, 24, 65}, []int{5, 4, 3, 2}, 1)
	case 2:
		v2 = pick(roll2, []int{7, 20, 60}, []int{5, 4, 3}, 2)
	case 3:
		// The C++ tests the first roll (LOCAL_13) for the third step.
		switch {
		case roll2 <= 15:
			v2 = 6
		case roll2 < 40:
			v2 = 5
		case roll < 80:
			v2 = 4
		default:
			v2 = 3
		}
	}
	if high && v2 < 3 {
		v2 = 3
	}
	if sanc != 0 && v2 >= 5 {
		v2 = 4
	}
	v2 += mod2
	if extra != 0 && v2 == 0 {
		v2 = extra
	}
	if sanc != 0 && v2 == 0 {
		v2 = 1
	}
	if mobType == 1 && v2 >= 3 {
		v2 = 2
	}
	// Caps the damage/magic pair at 45 + 27 on weapons.
	if pos >= 64 && (ef2 == EF_DAMAGE || ef2 == EF_MAGIC) && ((v2 >= 4 && v1 >= 5) || (v2 >= 5 && v1 >= 4)) {
		v2 = 3
	}
	switch {
	case v2 > 0 && it.Effect[2].Index == 0:
		it.Effect[2] = wire.ItemEffect{Index: uint8(ef2), Value: uint16(v2 * base2)}
	case it.Effect[2].Index == 0:
		it.Effect[2] = wire.ItemEffect{Index: EF_UNIQUE, Value: uint16(rnd() % 127)}
	}

	if it.Effect[0].Index != 0 {
		return
	}
	r3 := rnd() % 100
	if sanc != 0 {
		r3 >>= 1
	}
	s1, s2, s3, s4 := 1, 12, 45, 70
	if tier >= 2 {
		s1, s2, s3, s4 = 6, 35, 85, 100
	}
	if tier <= 1 {
		s1, s2, s3, s4 = 6, 2, 75, 90
	}
	switch {
	case r3 < s1:
		it.Effect[0] = wire.ItemEffect{Index: EF_SANC, Value: 2}
		if gradeSanc > 2 {
			r := rnd() % 100
			steps := map[int][][2]int{
				3: {{30, 3}},
				4: {{10, 4}, {40, 3}},
				5: {{10, 5}, {30, 4}, {60, 3}},
				6: {{10, 6}, {20, 5}, {30, 4}, {60, 3}},
				7: {{4, 7}, {10, 6}, {20, 5}, {35, 4}, {60, 3}},
			}[gradeSanc]
			for _, s := range steps {
				if r < s[0] {
					it.Effect[0].Value = uint16(s[1])
					break
				}
			}
		}
	case r3 < s2:
		it.Effect[0] = wire.ItemEffect{Index: EF_SANC, Value: 1}
	case r3 < s3:
		it.Effect[0] = wire.ItemEffect{Index: EF_SANC, Value: 0}
	case r3 < s4:
		k := rnd() % 10
		rg := bonusRanges[k][min(tier, 2)]
		span := max(rg[1]+1-rg[0], 1)
		it.Effect[0] = wire.ItemEffect{Index: uint8(bonusEffects[k]), Value: uint16(rnd()%span + rg[0])}
	default:
		it.Effect[0] = wire.ItemEffect{Index: EF_UNIQUE, Value: uint16(rnd() % 127)}
	}
}

// pick returns vals[i] for the first limit with roll < limits[i], else def.
func pick(roll int, limits, vals []int, def int) int {
	for i, l := range limits {
		if roll < l {
			return vals[i]
		}
	}
	return def
}
