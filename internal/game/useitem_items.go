package game

import (
	"time"

	"dbsrv/internal/wire"
)

// Item-specific branches of RequestUseItem (the part after the volatile
// switch): colors, replations, Mythril bars, bags, boxes, Pedra da Fúria,
// hunting scrolls, pardon, exp chests, food, Sephira books, date buffs,
// escrituras and gold bars.

// Language.txt indexes (Basedef.h).
const (
	nnOnlyToEquips       = 74
	nnCarryFull          = 149
	nnCantResetChaos     = 302
	nnNeedCelestialSub   = 309
	nnMax36Enter         = 310
	nnNeedXXFame         = 351
	nnNoSpaceItem        = 352
	nnCantTeleNip        = 367
	nnHasColor           = 368
	nnNoColor            = 369
	nnItemNotMatch       = 370
	questArcana          = 1 << 9
	nightmareScrollDelay = 20 * time.Hour // ScrollWaitTime
)

// huntingScrolls is HuntingScrolls[town][warp] for items 3432..3437.
var huntingScrolls = [6][10][2]int{
	{{2370, 2106}, {2508, 2101}, {2526, 2009}, {2529, 1882}, {2126, 1600}, {2005, 1617}, {2241, 1474}, {1858, 1721}, {2250, 1316}, {1989, 1755}},
	{{290, 3799}, {724, 3781}, {481, 4062}, {876, 4058}, {855, 3922}, {808, 3876}, {959, 3813}, {926, 3750}, {1096, 3730}, {1132, 3800}},
	{{1242, 4035}, {1264, 4017}, {1333, 3994}, {1358, 4041}, {1462, 4033}, {1326, 3788}, {1493, 3777}, {1437, 3741}, {1389, 3740}, {1422, 3810}},
	{{1376, 1722}, {1426, 1686}, {1381, 1861}, {1326, 1896}, {1510, 1723}, {1543, 1726}, {1580, 1758}, {1182, 1714}, {1634, 1727}, {1237, 1764}},
	{{2367, 4024}, {2236, 4044}, {2236, 3993}, {2209, 3989}, {2453, 4067}, {2485, 4043}, {2534, 3897}, {2489, 3919}, {2269, 3910}, {2202, 3866}},
	{{3664, 3024}, {3582, 3007}, {3514, 3008}, {3819, 2977}, {3517, 2889}, {3745, 2977}, {3639, 2877}, {3650, 2727}, {3660, 2773}, {3746, 2879}},
}

// itemDate stores t as STRUCT_ITEMDATE.
func itemDate(t time.Time) wire.ItemDate {
	return wire.ItemDate{Dia: uint8(t.Day()), Mes: uint8(t.Month()), Ano: uint16(t.Year()),
		Hora: uint8(t.Hour()), Minuto: uint8(t.Minute()), Segundo: uint8(t.Second())}
}

// dateRemaining is TimeRemaining(STRUCT_ITEMDATE) in days (<= 0 = over).
func dateRemaining(d wire.ItemDate) float64 {
	if d.Ano == 0 {
		return 0
	}
	end := time.Date(int(d.Ano), time.Month(d.Mes), int(d.Dia), int(d.Hora), int(d.Minuto), int(d.Segundo), 0, time.Local)
	return time.Until(end).Hours() / 24
}

// affectSlot finds the affect idx, or a free one.
func affectSlot(m *Mob, idx uint8) int {
	for i := range m.info.Affects {
		if m.info.Affects[i].Index == idx {
			return i
		}
	}
	for i := range m.info.Affects {
		if m.info.Affects[i].Index == 0 {
			return i
		}
	}
	return -1
}

// dateBuffs are the potions whose affect lasts until a date.
type dateBuff struct {
	idx   uint8
	value uint16
	match func(a *wire.Affect) bool
	date  func(u *User, m *Mob) *wire.ItemDate
}

var dateBuffs = []dateBuff{
	{34, 0, func(a *wire.Affect) bool { return true }, func(u *User, _ *Mob) *wire.ItemDate { return &u.account.Divina }},
	{4, 4, func(a *wire.Affect) bool { return a.Value != 6 && a.Value != 7 && a.Value != 8 }, func(u *User, _ *Mob) *wire.ItemDate { return &u.account.Sephira }},
	{35, 1, func(a *wire.Affect) bool { return a.Value == 1 }, func(_ *User, m *Mob) *wire.ItemDate { return &m.info.Saude }},
	{49, 0, func(a *wire.Affect) bool { return true }, func(_ *User, m *Mob) *wire.ItemDate { return &m.info.Revigorante }},
}

// refreshDateBuffs is the date part of SendAffect: a buff whose date is
// still ahead is (re)applied with time 999, an expired one is removed.
// The C++ re-applied the Revigorante as affect 51 by mistake; 49 here.
// It returns the days left per affect index for the client.
func (g *Game) refreshDateBuffs(u *User) map[uint8]float64 {
	m := &g.mobs[u.id]
	left := map[uint8]float64{}
	for _, b := range dateBuffs {
		days := dateRemaining(*b.date(u, m))
		found := false
		for i := range m.info.Affects {
			a := &m.info.Affects[i]
			if a.Index != b.idx || !b.match(a) {
				continue
			}
			found = true
			if days <= 0 {
				*a = wire.Affect{}
				continue
			}
			a.Time = 999
		}
		if days > 0 {
			left[b.idx] = days
			if !found {
				if s := affectSlot(m, 0); s != -1 {
					m.info.Affects[s] = wire.Affect{Index: b.idx, Master: 1, Value: b.value, Time: 999}
				}
			}
		}
	}
	return left
}

// applyDateBuff is the shared body of the Divina/Sephira/Saúde/Revigorante
// items: set the end date, apply the affect, tell everyone.
func (g *Game) applyDateBuff(u *User, src *wire.Item, idx uint8, value uint16, date *wire.ItemDate, days int) bool {
	m := &g.mobs[u.id]
	slot := affectSlot(m, idx)
	if slot == -1 {
		return false
	}
	*date = itemDate(time.Now().AddDate(0, 0, days))
	m.info.Affects[slot] = wire.Affect{Index: idx, Master: 1, Value: value, Time: 999}
	g.getCurrentScore(u.id)
	g.refreshMob(u.id)
	g.sendAffect(u)
	g.sendScore(u.id)
	amountMinus(src)
	return true
}

// setItemBonus is SetItemBonus(item): new random bonuses in effects 1 and 2
// (the Replations).
func (g *Game) setItemBonus(it *wire.Item) {
	d := g.data.Item(it.Index)
	unique, pos := int(d.Unique), int(d.Pos)
	mobType := g.data.itemAbility(it, EF_MOBTYPE)
	if pos&254 == 0 || pos == 128 {
		return
	}
	ef1, mod1, base1 := EF_UNIQUE, 0, 0
	t1 := (rnd() % 101) % 3
	switch {
	case pos == 2:
		ef1, base1 = EF_ATTSPEED, 3
		if t1 == 1 {
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
		default:
			ef1, base1 = EF_SPECIALALL, 3
		}
	}

	ef2, base2, mod2 := EF_UNIQUE, 0, 0
	t2 := (rnd() % 50) % 3
	switch pos {
	case 2:
		ef2, base2 = EF_HP, 10
		if t2 == 1 {
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
		switch rnd() % 4 {
		case 0:
			ef2, base2, mod2 = EF_MAGIC, 2, -1
		case 1:
			ef2, base2, mod2 = EF_DAMAGE, 6, -1
		case 2:
			ef2, base2 = EF_SPECIALALL, 3
		case 3:
			ef2, base2, mod2 = EF_RESISTALL, 3, 1
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

	// rnd()%50 never reaches 60, so the first bonus is always x6 or x5
	// (as in the C++).
	tier := func(r int) int {
		switch {
		case r <= 20:
			return 6
		case r < 60:
			return 5
		case r < 80:
			return 4
		}
		return 3
	}
	v1 := tier(rnd()%50) + mod1
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
	v2 := tier(rnd()%100) + mod2
	if mobType == 1 && v2 >= 3 {
		v2 = 2
	}
	// Caps the weapon damage/magic bonus pair (45 + 27).
	if pos >= 64 && (ef2 == EF_DAMAGE || ef2 == EF_MAGIC) && ((v2 >= 4 && v1 >= 5) || (v2 >= 5 && v1 >= 4)) {
		v2 = 3
	}
	if v2 > 0 && it.Effect[2].Index == 0 {
		v := v2 * base2
		if pos == 16 && v > 18 {
			v = 18
		}
		it.Effect[2] = wire.ItemEffect{Index: uint8(ef2), Value: uint16(v)}
	}
}

// mythrilAdd is g_pAddMyt: max/min/step for sets and weapons and the
// effects a Mythril bar (volatile 235..238) raises.
var mythrilAdd = [4]struct {
	maxSet, maxWeapon, minSet, minWeapon, stepSet, stepWeapon int
	effects                                                   []uint8
}{
	{42, 81, 0, 0, 6, 9, []uint8{EF_DAMAGE, EF_DAMAGE2}},
	{14, 36, 0, 0, 2, 4, []uint8{EF_MAGIC}},
	{40, 0, 0, 0, 5, 0, []uint8{EF_AC, EF_ACADD, EF_ACADD2}},
	{90, 0, 0, 0, 10, 0, []uint8{EF_CRITICAL, EF_CRITICAL2}},
}

// useItemByID handles the item-specific branches; it reports whether the
// item was one of them.
func (g *Game) useItemByID(u *User, p *wire.UseItem, src *wire.Item, vol int) bool {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	id := src.Index
	resend := func() { g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src) }
	refuse := func(msg string, args ...any) {
		g.clientMessage(u, msg, args...)
		resend()
	}
	dst := func() *wire.Item { return g.slotItemAny(u, int(p.DstType), int(p.DstSlot)) }

	switch {
	case vol == 186: // colored refine (3407..3416) / color remover (3417)
		d := dst()
		if d == nil || d.Index == 0 {
			resend()
			return true
		}
		changed := false
		for i := range 3 {
			e := d.Effect[i].Index
			if id != 3417 && e == EF_SANC {
				d.Effect[i].Index = uint8(116 + int(id) - 3407)
				changed = true
				break
			}
			if id == 3417 && e >= 116 && e <= 125 {
				d.Effect[i].Index = EF_SANC
				changed = true
				break
			}
		}
		if !changed {
			msg := nnHasColor
			if id == 3417 {
				msg = nnNoColor
			}
			refuse("%s", g.lang(msg))
			return true
		}
		g.clientMessage(u, "%s", g.lang(nnRefineSuccess))
		amountMinus(src)
		g.sendItem(u, int(p.DstType), int(p.DstSlot), d) // the C++ only sent SendEquip
		g.sendEquip(u)
		g.sendEmotion(u.id, 14, 3)

	case id >= 4016 && id <= 4020: // Replations [A]..[E]: new bonuses
		if p.DstType != wire.SlotInv {
			refuse("%s", g.lang(nnOnlyToEquips))
			return true
		}
		d := dst()
		if d == nil || d.Index == 0 {
			resend()
			return true
		}
		if g.data.effectValueByIndex(d.Index, EF_UNKNOW1) != int(id)-4015 || g.data.effectValueByIndex(d.Index, EF_MOBTYPE) == 1 ||
			g.data.Item(d.Index).Pos > 32 {
			refuse("%s", g.lang(nnItemNotMatch))
			return true
		}
		if itemSanc(d) > 6 {
			refuse("Somente em itens menor ou igual a +6.")
			return true
		}
		if d.Effect[0].Index != 0 && d.Effect[1].Index != 0 && d.Effect[2].Index != 0 && sancEffect(d) == -1 {
			d.Effect = [3]wire.ItemEffect{}
		}
		// The new bonuses go into effects 1 and 2, so the refine must sit in
		// effect 0. The C++ swapped 1 and 2 and then cleared both, losing a
		// refine kept in either; it is moved to effect 0 here.
		if s := sancEffect(d); s > 0 {
			d.Effect[0] = d.Effect[s]
		}
		d.Effect[1], d.Effect[2] = wire.ItemEffect{}, wire.ItemEffect{}
		g.setItemBonus(d)
		amountMinus(src)
		g.sendItem(u, int(p.DstType), int(p.DstSlot), d)
		g.sendEmotion(u.id, 14, 3)

	case id == 3250: // Bênção de Ímago: 120 essences -> a blessed one
		d := dst()
		if d == nil {
			resend()
			return true
		}
		blessed := map[uint16]uint16{2400: 3230, 2405: 3231, 2406: 3232, 2407: 3233, 2409: 3234, 2410: 3235,
			2411: 3236, 2412: 3237, 2413: 3238, 2414: 3239, 2415: 3240, 2416: 3241, 2417: 3242, 2418: 3243}
		to := blessed[d.Index]
		if to == 0 || itemAmount(d) != 120 {
			refuse("%s", g.lang(nnMountNotMatch))
			return true
		}
		*d = wire.Item{Index: to}
		amountMinus(src)
		g.sendItem(u, int(p.DstType), int(p.DstSlot), d)

	case vol >= 235 && vol <= 238: // Mythril bars: +/- an Arch +11 bonus
		g.useMythrilBar(u, p, src, vol-235)

	case id == itemBag: // Bolsa do Andarilho: the bags are open to everyone
		// The C++ gave 30 days of an extra bag; here every character has
		// both bags for good (openBags), so the item is not used up.
		if openBags(pl) {
			g.sendItem(u, wire.SlotInv, 60, &pl.Inventory[60])
			g.sendItem(u, wire.SlotInv, 61, &pl.Inventory[61])
		}
		refuse("As bolsas já estão liberadas para todos os personagens.")

	case id == 4528: // Caixa de Jóia: a random gem 2441..2444
		slot, last := int(p.SrcSlot), itemAmount(src) == 1
		if !last {
			slot = firstEmptySlot(pl)
		}
		if slot == -1 || p.SrcType != wire.SlotInv {
			refuse("Sem espaço no inventário")
			return true
		}
		if !last {
			amountMinus(src)
			g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
		}
		pl.Inventory[slot] = wire.Item{Index: uint16(2441 + rnd()%4)}
		g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
		g.clientMessage(u, "Chegou um item [%s]", g.itemName(pl.Inventory[slot].Index))

	case id == 3020: // Pedra da Fúria
		g.useFuryStone(u, src)

	case id >= 3432 && id <= 3437: // hunting scrolls: warp 1..10
		w := int(p.Warp)
		if w < 1 || w > 10 {
			return true
		}
		pos := huntingScrolls[id-3432][w-1]
		amountMinus(src)
		g.teleportar(u.id, pos[0], pos[1])

	case id == 3343: // Pergaminho do Perdão: +75 CP up to 60
		if pkPoint(pl)-75 > 60 {
			refuse("%s", g.lang(nnCantResetChaos))
			return true
		}
		setPKPoint(pl, pkPoint(pl)+75)
		g.clientMessage(u, g.lang(nnCPPoint), pkPoint(pl)-75)
		g.refreshMob(u.id)
		amountMinus(src)

	case id == 1728: // fireworks
		g.sendEmotion(u.id, 100, rnd()%8)
		amountMinus(src)

	case id == 4140 || id == 4548 || id == 4549: // exp chests: affect 39
		slot := affectSlot(m, 39)
		if slot == -1 {
			resend()
			return true
		}
		add := uint32(900)
		switch id {
		case 4548:
			add = 225
		case 4549:
			add = 450
		}
		a := &m.info.Affects[slot]
		a.Index = 39
		a.Time += add
		g.sendAffect(u)
		g.refreshMob(u.id)
		amountMinus(src)

	case id == 1739 || id == 4145: // chocolates
		skills := [2]int{44, 43}
		if id == 4145 {
			skills = [2]int{41, 45}
		}
		g.setAffect(u.id, skills[0], 500, 125)
		g.setAffect(u.id, skills[1], 500, 125)
		g.sendAffect(u)
		amountMinus(src)

	case id == 3445 || id == 3446: // escrituras: up to 10 Ori/Lac into a pack
		want := uint16(412 + id - 3445)
		total := min(inventoryAmount(pl, want), 10)
		if total == 0 {
			refuse("Não há %s para juntar", g.itemName(want))
			return true
		}
		slot := firstEmptySlot(pl)
		if slot == -1 {
			refuse("%s", g.lang(nnCarryFull))
			return true
		}
		left := total
		for i := 0; i < 60 && left > 0; i++ {
			for pl.Inventory[i].Index == want && left > 0 {
				amountMinus(&pl.Inventory[i])
				left--
			}
		}
		pl.Inventory[slot] = wire.Item{Index: 3447 + id - 3445, Effect: [3]wire.ItemEffect{{Index: EF_AMOUNT, Value: uint16(total)}}}
		amountMinus(src)
		g.sendCarry(u)

	case id == 3314 || id == 646 || id == 647: // frango / poções da coragem: affect 30
		slot := affectSlot(m, 30)
		if slot == -1 {
			resend()
			return true
		}
		value, dur := uint16(1), uint32(1800)
		if id != 3314 {
			value, dur = 2+id-646, 450
		}
		m.info.Affects[slot] = wire.Affect{Index: 30, Value: value, Time: dur}
		g.getCurrentScore(u.id)
		g.sendAffect(u)
		g.sendScore(u.id)
		g.refreshMob(u.id)
		amountMinus(src)

	case id >= 667 && id <= 671: // Sephira skill books
		cost := [5]int{0, 36, 35, 40, 39}
		n := int(id) - 666
		bit := uint32(1) << uint(24+n)
		switch {
		case pl.Learn[0]&bit != 0:
			refuse("Você aprendeu esta skill")
		case int(pl.SkillPoint) < cost[n-1]:
			refuse("Você não tem pontos de Skill suficiente")
		case evolution(pl) == evoMortal && pl.BStatus.Level < 255:
			refuse("Level inadequado")
		default:
			pl.SkillPoint -= uint16(cost[n-1])
			pl.Learn[0] |= bit
			g.getCurrentScore(u.id)
			g.sendScore(u.id)
			g.sendEtc(u)
			amountMinus(src)
		}

	case id >= 3210 && id <= 3213: // Poção Revigorante / montaria imortal
		days := [4]int{1, 3, 7, 30}[id-3210]
		if !g.applyDateBuff(u, src, 49, 0, &m.info.Revigorante, days) {
			resend()
		}

	case id >= 3379 && id <= 3381: // Poção Divina
		days := [3]int{7, 15, 30}[id-3379]
		if !g.applyDateBuff(u, src, 34, 0, &u.account.Divina, days) {
			resend()
		}

	case id >= 3361 && id <= 3363: // Poção Sephira
		days := [3]int{7, 15, 30}[id-3361]
		if !g.applyDateBuff(u, src, 4, 4, &u.account.Sephira, days) {
			resend()
		}

	case id >= 3364 && id <= 3366: // Poção Saúde
		days := [3]int{7, 15, 30}[id-3364]
		if !g.applyDateBuff(u, src, 35, 1, &m.info.Saude, days) {
			resend()
		}

	case id == 5137: // Escritura de Pesadelo: +12 Nightmare entries, once per 20 h
		if evolution(pl) <= evoArch {
			refuse("%s", g.lang(nnNeedCelestialSub))
			return true
		}
		if e := m.info.Escritura; e.Ano != 0 {
			last := time.Date(int(e.Ano), time.Month(e.Mes), int(e.Dia), int(e.Hora), int(e.Minuto), int(e.Segundo), 0, time.Local)
			if wait := nightmareScrollDelay - time.Since(last); wait > 0 {
				s := int(wait.Seconds())
				refuse("Você precisa aguardar %02d horas %02d minutos %02d segundos até o próximo uso", s/3600%24, s%3600/60, s%60)
				return true
			}
		}
		if m.info.PesaEnter >= 36 {
			refuse("%s", g.lang(nnMax36Enter))
			return true
		}
		m.info.Escritura = itemDate(time.Now())
		m.info.PesaEnter = min(m.info.PesaEnter+12, 36)
		amountMinus(src)

	case id == 4010 || id == 4011 || (id >= 4026 && id <= 4029): // gold bars
		add := map[uint16]int64{4010: 100000000, 4011: 1000000000, 4026: 1000000, 4027: 5000000, 4028: 10000000, 4029: 50000000}[id]
		if pl.Gold+add > g.cfg.Limits.MaxGold {
			refuse("%s", g.lang(nnCantGetMoreThan2G))
			return true
		}
		pl.Gold += int64(add)
		g.signalParm64(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
		amountMinus(src)

	default:
		return g.useItemByID2(u, p, src)
	}
	return true
}

// useMythrilBar is volatile 235..238: on an Arch item from +11, 53% to
// raise one bonus by a step, 14% to lower it.
func (g *Game) useMythrilBar(u *User, p *wire.UseItem, src *wire.Item, typ int) {
	d := g.slotItemAny(u, int(p.DstType), int(p.DstSlot))
	resend := func() { g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src) }
	if d == nil || d.Index == 0 || itemSanc(d) < 11 || g.data.effectValueByIndex(d.Index, EF_MOBTYPE) != 1 {
		resend()
		return
	}
	t := mythrilAdd[typ]
	is := func(e uint8) bool {
		for _, x := range t.effects {
			if e == x {
				return true
			}
		}
		return false
	}
	// The C++ also counted empty effects (its "!= 0" applied to the first
	// comparison only), which could raise an empty slot; only real ones here.
	total, found := 0, uint8(0)
	for i := range 3 {
		if e := d.Effect[i].Index; e != 0 && is(e) {
			total += int(d.Effect[i].Value)
			found = e
		}
	}
	switch {
	case t.effects[0] == EF_CRITICAL:
		total += g.data.effectValueByIndex(d.Index, EF_CRITICAL)
		for i := range 3 {
			if d.Effect[i].Index == EF_CRITICAL2 {
				total = int(d.Effect[i].Value)
				break
			}
		}
	case len(t.effects) > 1 && t.effects[1] == EF_ACADD && found != EF_ACADD2:
		total += g.data.effectValueByIndex(d.Index, EF_ACADD)
	}
	pos := int(g.data.Item(d.Index).Pos)
	maxAdd, minAdd, step := t.maxSet, t.minSet, t.stepSet
	if pos > 32 {
		maxAdd, minAdd, step = t.maxWeapon, t.minWeapon, t.stepWeapon
	}
	if pos == 16 && typ == 2 {
		maxAdd = 70
	}
	if total <= minAdd || total >= maxAdd {
		resend()
		return
	}
	roll := rnd() % 150
	for i := range 3 {
		e := d.Effect[i].Index
		if e == 0 || !is(e) {
			continue
		}
		switch {
		case roll >= 70:
			d.Effect[i].Value += uint16(step)
			g.clientMessage(u, "Adicional aumentou")
		case roll <= 20:
			d.Effect[i].Value = uint16(max(int(d.Effect[i].Value)-step, 0))
			g.clientMessage(u, "Adicional reduziu")
		}
		break
	}
	g.sendItem(u, int(p.DstType), int(p.DstSlot), d)
	amountMinus(src)
}

// useFuryStone is item 3020: the Cythera Arcana (level 199 with Cythera
// 3502, the 4 secret stones and 500 fame) or the Celestial 90 unlock (500
// fame, 95%); for a Sub-Celestial with the Arcana at 199+, one of the
// three resets (subCeleReset).
func (g *Game) useFuryStone(u *User, src *wire.Item) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	ev, level := evolution(pl), int(pl.BStatus.Level)
	resend := func() {
		for i := range pl.Inventory {
			if &pl.Inventory[i] == src {
				g.sendItem(u, wire.SlotInv, i, src)
			}
		}
	}
	if ev == evoSubCele && (m.info.Info|m.info.Sub.Info)&questArcana != 0 && level >= 199 {
		g.subCeleReset(u, src, resend)
		return
	}
	if (ev == evoSubCele || ev == evoCelestial) && level == 199 && pl.Equip[1].Index == 3502 {
		if m.info.Info&questArcana != 0 || m.info.Sub.Info&questArcana != 0 {
			g.clientMessage(u, "%s", g.lang(nnDoneAlready))
			resend()
			return
		}
		if m.info.Fame < 500 {
			g.clientMessage(u, g.lang(nnNeedXXFame), 500)
			resend()
			return
		}
		var stones [4]int
		for i := range stones {
			if stones[i] = firstSlotOf(pl, uint16(5334+i)); stones[i] == -1 {
				resend()
				return
			}
		}
		for _, s := range stones {
			pl.Inventory[s] = wire.Item{}
			g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
		}
		m.info.Fame -= 500
		amountMinus(src)
		r := rnd() % 100
		if add := rnd() % 50; r+add > 100 {
			r += add - 50
		}
		name := g.charName(u.id)
		if r < 50 {
			pl.Equip[1].Index = 3507
			g.sendItem(u, wire.SlotEquip, 1, &pl.Equip[1])
			g.clientMessage(u, "%s", g.lang(nnSuccessComp))
			m.info.Info |= questArcana
			m.info.Sub.Info |= questArcana
			g.notice("O jogador " + name + " concluiu com sucesso a criação da Cythera Arcana")
		} else {
			g.clientMessage(u, "%s", g.lang(nnCombineFailed))
			g.notice("O jogador " + name + " falhou a criação da Cythera Arcana")
		}
		return
	}
	q := m.info.Info
	if ev != evoCelestial || level != 89 || q&questLvBlocked == 0 || q&questUnlock89 != 0 {
		resend()
		return
	}
	if m.info.Fame < 500 {
		g.clientMessage(u, g.lang(nnNeedXXFame), 500)
		resend()
		return
	}
	m.info.Fame -= 500
	if r := rnd() % 101; r <= 95 {
		g.clientMessage(u, "%s", g.lang(nnSuccessComp))
		g.sendEmotion(u.id, 14, 3)
		if s := firstEmptySlot(pl); s == -1 {
			g.clientMessage(u, g.lang(nnNoSpaceItem), g.itemName(3502))
		} else {
			pl.Inventory[s] = wire.Item{Index: 3502}
			g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
		}
		m.info.Info = m.info.Info&^questLvBlocked | questUnlock89
	} else {
		g.clientMessage(u, "Houve falha no destravamento do nível %d/95", r)
	}
	amountMinus(src)
}
