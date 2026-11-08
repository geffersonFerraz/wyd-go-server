package game

import (
	"time"

	"dbsrv/internal/wire"
)

// Second batch of the item-specific branches of RequestUseItem (from
// "GEMA ANCT" to "RESTAURADORES"): anct gems, Lac 100, quest exp boxes,
// Pedra Misteriosa, Poção do Vigor, jewels, Paclac, Selo do Guerreiro,
// stat resets, Honra coins, Arch crystals and the mount items.

const (
	nnCantRefineItemCele = 311
	nnLevelNotAllowed    = 313
	nnOnlyMortal         = 319
	capeRed              = 8
)

// questLevel is g_pQuestLevel: the level range of the quest boxes 4117..4121.
var questLevel = [5][2]int{{39, 114}, {115, 189}, {190, 264}, {265, 319}, {320, 349}}

// mountRest is g_pItemRest: the adult mounts each catalyst (3344+n) and
// restorer (3351+n) works on.
var mountRest = [7][6]uint16{
	{2363, 2364, 2365},
	{2366, 2367, 2368, 2372, 2373, 2371},
	{2369, 2370, 2374, 2375, 2387},
	{2377, 2376, 2378},
	{2381, 2382, 2383, 2388},
	{2384, 2385, 2386},
	{2379, 2380},
}

func mountRestHas(n int, id uint16, offset uint16) bool {
	for _, r := range mountRest[n] {
		if r != 0 && r-offset == id {
			return true
		}
	}
	return false
}

// refineEffect is the effect slot holding the refine (43 or a colored one).
func refineEffect(it *wire.Item) int {
	for i := range 3 {
		if e := it.Effect[i].Index; e == EF_SANC || (e >= 116 && e <= 125) {
			return i
		}
	}
	return -1
}

// anctBase is the first refine value of the +10.. band an anct value is in.
func anctBase(v int) int {
	switch {
	case v < 234:
		return 230
	case v < 238:
		return 234
	case v < 242:
		return 238
	case v < 246:
		return 242
	case v < 250:
		return 246
	}
	return 250
}

// lac100Allowed says where the Lactolerium 100 (4141) works. The C++ only
// took the earrings 591..594 (Hércules 595 was forgotten). As in the
// official game it is the sure version of the Lac roll where failing costs
// the item or its refine:
//   - earrings 591..595 up to +11 (the Lac roll there is 10%/5% and
//     destroys the earring);
//   - planet amulets 762..768 / 788..791 at +9 and +10 (the Lac roll there
//     resets them to +0);
//   - any other refinable gear at +10, to +11 (the Lac roll is 4%).
//
// Below those levels the common Lac already goes up to +9, so the 100 is
// refused there instead of being wasted.
func (g *Game) lac100Allowed(p *wire.UseItem, d *wire.Item, sanc int) bool {
	idx := int(d.Index)
	switch {
	case idx >= 591 && idx <= 595:
		return sanc < 11
	case (idx >= 762 && idx <= 768) || (idx >= 788 && idx <= 791):
		return sanc == 9 || sanc == 10
	}
	if sanc != 10 || refineEffect(d) < 0 {
		return false
	}
	if p.DstType == wire.SlotEquip && (p.DstSlot == 0 || p.DstSlot == 11 || p.DstSlot >= 12) {
		return false // face, stone, mount/cape slots
	}
	if idx == 3993 || idx == 3994 || idx == 3508 || idx == nucleoLac {
		return false
	}
	it := g.data.Item(d.Index)
	return it.Pos != 0 && g.data.effectValueByIndex(d.Index, 125) != 1
}

func (g *Game) useItemByID2(u *User, p *wire.UseItem, src *wire.Item) bool {
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
	case id >= 3386 && id <= 3389: // Gemas Anct: change the gem of a +10.. item
		g.useAnctGem(u, p, src)

	case id == 3220 || id == 3265: // stat resets: up to 100 points of each stat back
		g.useStatReset(u, src, id == 3220)

	case id == 4141: // Lactolerium 100: a sure +1 where a Lac roll is risky
		d := dst()
		if d == nil || d.Index == 0 {
			resend()
			return true
		}
		if g.data.effectValueByIndex(d.Index, EF_MOBTYPE) == 3 {
			refuse("%s", g.lang(nnCantRefineItemCele))
			return true
		}
		sanc := itemSanc(d)
		if !g.lac100Allowed(p, d, sanc) {
			refuse("%s", g.lang(nnCantRefine))
			return true
		}
		setItemSanc(d, sanc+1, 0)
		g.clientMessage(u, "%s", g.lang(nnRefineSuccess))
		g.log.Info("lac 100", "slot", u.id, "item", d.Index, "to", sanc+1)
		g.sendItem(u, int(p.DstType), int(p.DstSlot), d)
		amountMinus(src)
		g.sendEmotion(u.id, 14, 3)
		if p.DstType == wire.SlotEquip {
			g.getCurrentScore(u.id)
			g.sendScore(u.id)
			g.sendEquip(u)
		}

	case id >= 4117 && id <= 4121: // quest exp boxes (mortals)
		g.useQuestBox(u, p, src)

	case id == 4148: // Pedra Misteriosa: swap Celestial <-> Sub-Celestial
		g.useMysteryStone(u, src, resend)

	case id == 3313: // Poção do Vigor: affect 35/2 for 900 ticks
		slot := -1
		for i, a := range m.info.Affects {
			if a.Index == 35 && a.Value == 2 {
				slot = i
				break
			}
		}
		// The C++ also took the Poção Saúde's affect 35 (value 1) and added
		// 50 to the base defense for good on every use; neither here.
		if slot == -1 {
			slot = affectSlot(m, 0)
		}
		if slot == -1 {
			resend()
			return true
		}
		m.info.Affects[slot] = wire.Affect{Index: 35, Value: 2, Time: 900}
		g.getCurrentScore(u.id)
		g.sendAffect(u)
		g.sendScore(u.id)
		g.refreshMob(u.id)
		amountMinus(src)

	case id >= 3200 && id <= 3209: // jewels: bits of affect 8
		n := id - 3200
		bits := [10]uint16{1, 2, 4, 0, 8, 16, 32, 0, 64, 128}[n]
		if bits == 0 {
			if n == 3 { // Jóia da Recuperação: removes the "cancel" affect 32
				found := false
				for i := range m.info.Affects {
					if m.info.Affects[i].Index == 32 {
						m.info.Affects[i] = wire.Affect{}
						found = true
						break
					}
				}
				if !found {
					resend()
					return true
				}
				g.getCurrentScore(u.id)
				g.sendAffect(u)
				g.refreshMob(u.id)
			}
			amountMinus(src) // the storage jewel (3207) does nothing
			return true
		}
		slot := g.emptyAffect(u.id, 8)
		if slot == -1 {
			resend()
			return true
		}
		a := &m.info.Affects[slot]
		a.Index, a.Time = 8, 450
		a.Value |= bits
		g.getCurrentScore(u.id)
		g.sendAffect(u)
		g.sendScore(u.id)
		g.refreshMob(u.id)
		amountMinus(src)

	case id == 3447 || id == 3448: // Paclac / Pacori: open into single Lac / Ori
		single := 412 + id - 3447
		for n := itemAmount(src); n > 0; n-- {
			slot := firstEmptySlot(pl)
			if slot == -1 {
				g.clientMessage(u, "%s", g.lang(nnYouHaveNoSpaceToTrade))
				break
			}
			pl.Inventory[slot] = wire.Item{Index: single}
			g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
			amountMinus(src)
		}
		resend()

	case id == 4146: // Selo do Guerreiro: +10 fame, kingdom cape for mortals 370+
		m.info.Fame += 10
		g.clientMessage(u, "+ 10 Pontos de Fame Adicionado - [Saldo Fame] : %d", m.info.Fame)
		cape := uint16(3193)
		switch pl.CapeInfo {
		case capeBlue:
			cape = 3191
		case capeRed:
			cape = 3192
		}
		// The C++ also replaced a white cape 3193 by a new +0 one; the cape
		// is only handed out when the player does not wear it yet.
		if evolution(pl) == evoMortal && pl.BStatus.Level >= 370 && pl.Equip[15].Index != 3191 &&
			pl.Equip[15].Index != 3192 && pl.Equip[15].Index != cape {
			pl.Equip[15] = wire.Item{Index: cape}
			g.sendItem(u, wire.SlotEquip, 15, &pl.Equip[15])
			g.sendEquip(u)
		}
		amountMinus(src)
		g.sendEmotion(u.id, 14, 3)

	case id >= 3288 && id <= 3290: // Moedas de Honra
		add := [3]uint32{1, 50, 100}[id-3288]
		u.account.Honra += add
		g.clientMessage(u, "+ %d Pontos de Honra Adicionado - [Saldo Honra] : %d", add, u.account.Honra)
		amountMinus(src)

	case id >= 4106 && id <= 4109: // Cristais Arch: Elime, Sylphed, Thelion, Noas
		g.useArchCrystal(u, src, int(id-4106), refuse)

	case id == 3438: // Acelerador de Nascimento: hatch an egg or shorten it
		d := dst()
		if d == nil || d.Index < 2300 || d.Index >= 2330 {
			resend()
			return true
		}
		left := g.data.itemAbility(d, EF_INCUBATE) - 1
		if left <= 0 {
			d.Index = 2330 + d.Index - 2300
			setMountHP(d, 5000)
			d.Effect[1] = wire.ItemEffect{Index: 1, Value: uint8(rnd()%20 + 10)}
			d.Effect[2] = wire.ItemEffect{Index: 30, Value: 1}
			g.clientMessage(u, "%s", g.lang(nnIncubated))
			g.mountProcess(u.id)
		} else {
			for i := range 3 {
				if d.Effect[i].Index == EF_INCUBATE {
					d.Effect[i].Value = uint8(left)
					break
				}
			}
			g.clientMessage(u, "A incubação foi reduzida")
		}
		// The C++ sent Equip[14] here whatever slot the egg was in.
		g.sendItem(u, int(p.DstType), int(p.DstSlot), d)
		amountMinus(src)

	case id >= 3344 && id <= 3350: // catalysts: baby mount -> adult
		// The C++ read the target from the packet but wrote Equip[14], so a
		// baby in the inventory became a second adult mount; only the mount
		// slot here.
		if p.DstType != wire.SlotEquip || p.DstSlot != 14 {
			refuse("%s", g.lang(nnMountNotMatch))
			return true
		}
		d := &pl.Equip[14]
		if !mountRestHas(int(id-3344), d.Index, 30) {
			refuse("%s", g.lang(nnMountNotMatch))
			return true
		}
		d.Index += 30
		setMountHP(d, 5000)
		d.Effect[1].Index = 1
		d.Effect[1].Value = uint8(min(int(d.Effect[1].Value)+rnd()%16+5, 255))
		d.Effect[2] = wire.ItemEffect{Index: 100}
		g.mountProcess(u.id)
		g.clientMessage(u, "%s", g.lang(nnIncubated))
		g.sendItem(u, wire.SlotEquip, 14, d)
		g.sendEmotion(u.id, 14, 3)
		amountMinus(src)

	case id >= 3351 && id <= 3357: // restorers: +1..3 vitality, up to 60
		d := dst()
		if d == nil || d.Index <= 2360 || d.Index >= 2390 {
			resend()
			return true
		}
		if d.Effect[1].Value >= 60 {
			refuse("Limite de 60 pontos de vitalidade")
			return true
		}
		if !mountRestHas(int(id-3351), d.Index, 0) {
			refuse("%s", g.lang(nnMountNotMatch))
			return true
		}
		d.Effect[1].Value = uint8(min(int(d.Effect[1].Value)+rnd()%3+1, 60))
		g.sendItem(u, int(p.DstType), int(p.DstSlot), d)
		amountMinus(src)
		g.sendEmotion(u.id, 14, 3)

	default:
		return g.useItemByID3(u, p, src)
	}
	return true
}

// useAnctGem is items 3386..3389 (Garnet, Coral, ...): the anct refine of a
// +10.. set or weapon is moved to the gem's color; an ancient weapon also
// becomes the matching item.
func (g *Game) useAnctGem(u *User, p *wire.UseItem, src *wire.Item) {
	resend := func() { g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src) }
	pl := &g.mobs[u.id].info.Player
	// The C++ read Equip[DstSlot] before checking the slot type.
	if p.DstType != wire.SlotEquip || int(p.DstSlot) >= len(pl.Equip) {
		resend()
		return
	}
	d := &pl.Equip[p.DstSlot]
	data := g.data.Item(d.Index)
	pos, grade := int(data.Pos), int(data.Grade)
	weapon := pos >= 64 && pos <= 2048
	stone := int(src.Index) - 3386
	sanc := itemSanc(d)
	switch {
	case d.Index == 0 || pos == 0:
		resend()
		return
	case weapon && (grade < 5 || grade > 8) && sanc < 10:
		resend()
		return
	case pos <= 32:
		if sanc < 10 {
			resend()
			return
		}
		if e := refineEffect(d); e != -1 && (int(d.Effect[e].Value)-230)%4 == stone {
			resend()
			return
		}
	case !weapon:
		resend()
		return
	case stone == grade-5:
		resend()
		return
	}
	if e := refineEffect(d); e != -1 && (pos <= 32 || d.Effect[e].Value > 9) {
		v := int(d.Effect[e].Value)
		d.Effect[e].Value = uint8(v + stone - (v - anctBase(v)))
	}
	if weapon && pos != 128 && grade >= 5 && grade <= 8 {
		d.Index = uint16(int(d.Index) + stone - (grade - 5))
	}
	g.sendItem(u, wire.SlotEquip, int(p.DstSlot), d)
	amountMinus(src)
	g.log.Info("anct gem", "slot", u.id, "item", d.Index, "gem", src.Index)
}

// useStatReset is items 3220 and 3265: each stat above its class base gives
// back up to 100 points. 3220 needs a stat with 100+ points, 3265 one with
// 5+. The C++ added the points up but never stored them; GetCurrentScore
// gives them back from the level for Mortal/Arch, and they are stored here
// for the rest.
func (g *Game) useStatReset(u *User, src *wire.Item, big bool) {
	pl := &g.mobs[u.id].info.Player
	cls := int(pl.ClassInfo)
	resend := func() {
		for i := range pl.Inventory {
			if &pl.Inventory[i] == src {
				g.sendItem(u, wire.SlotInv, i, src)
			}
		}
	}
	if cls > 3 {
		resend()
		return
	}
	stats := [4]*uint16{&pl.BStatus.STR, &pl.BStatus.INT, &pl.BStatus.DEX, &pl.BStatus.CON}
	need := 5
	if big {
		need = 100
	}
	enough := false
	for i, s := range stats {
		if int(*s)-baseSIDCHM[cls][i] >= need {
			enough = true
		}
	}
	if !enough {
		g.clientMessage(u, "Pontos insuficientes para resetar")
		resend()
		return
	}
	points := int(pl.StatusPoint)
	for i, s := range stats {
		back := min(max(int(*s)-baseSIDCHM[cls][i], 0), 100)
		*s -= uint16(back)
		points += back
	}
	pl.StatusPoint = uint16(min(points, maxPoints))
	g.getCurrentScore(u.id)
	g.setAffect(u.id, 0x2C, 20, 20)
	g.sendScore(u.id)
	g.sendEtc(u)
	amountMinus(src)
}

// useQuestBox is items 4117..4121: exp and gold for mortals in the box's
// level range, the whole stack at once, up to the end of the range. The C++
// used up the whole stack even when the range ended after a few boxes; only
// the boxes needed are used here.
func (g *Game) useQuestBox(u *User, p *wire.UseItem, src *wire.Item) {
	expValue := [5]int64{50000, 100000, 200000, 300000, 600000}
	goldValue := [5]int64{6000, 10000, 14000, 10000, 15000}
	resend := func() { g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src) }
	pl := &g.mobs[u.id].info.Player
	q := int(src.Index) - 4117
	level := int(pl.BStatus.Level)
	if level < questLevel[q][0] || level >= questLevel[q][1] {
		g.clientMessage(u, g.lang(nnLevelNotAllowed), questLevel[q][0]+1, questLevel[q][1]+1)
		resend()
		return
	}
	if evolution(pl) != evoMortal {
		g.clientMessage(u, "%s", g.lang(nnOnlyMortal))
		resend()
		return
	}
	// The C++ walked the levels from Level+1, so the cap is the start of
	// the range's last level (or one more level when already next to it).
	limit := nextLevel[evoMortal][max(questLevel[q][1]-1, level+1)]
	room := limit - pl.Exp
	if room <= 0 {
		resend()
		return
	}
	have := int64(itemAmount(src))
	used := min(have, (room+expValue[q]-1)/expValue[q])
	exp := min(used*expValue[q], room)
	pl.Exp += exp
	pl.Gold = int32(min(int64(pl.Gold)+goldValue[q]*used, 2000000000))
	for range used {
		amountMinus(src)
	}
	g.getCurrentScore(u.id)
	g.checkGetLevel(u)
	g.sendScore(u.id)
	g.sendEtc(u)
	g.sendEmotion(u.id, 14, 3)
	g.signalParm(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
	g.clientMessage(u, ".+++ %d de Experiência +++", exp)
	resend()
	g.shareQuestBoxExp(u, q, exp)
}

// shareQuestBoxExp gives the party members near the user the same exp the
// quest box gave, under the box's own rules: online, alive, mortal, inside
// the box's level range, and capped at the end of that range.
func (g *Game) shareQuestBoxExp(u *User, q int, exp int64) {
	if exp <= 0 {
		return
	}
	pos := g.mobs[u.id].target
	for _, id := range g.partyMembers(u.id) {
		if id == u.id {
			continue
		}
		mu := g.player(id)
		if mu == nil || mu.status != userPlay {
			continue
		}
		m := &g.mobs[id]
		pl := &m.info.Player
		if pl.Status.CurHP == 0 || evolution(pl) != evoMortal {
			continue
		}
		p := m.target
		if int(pos.X) < int(p.X)-halfGrid || int(pos.X) > int(p.X)+halfGrid ||
			int(pos.Y) < int(p.Y)-halfGrid || int(pos.Y) > int(p.Y)+halfGrid {
			continue
		}
		level := int(pl.BStatus.Level)
		if level < questLevel[q][0] || level >= questLevel[q][1] {
			continue
		}
		limit := nextLevel[evoMortal][max(questLevel[q][1]-1, level+1)]
		gain := min(exp, limit-pl.Exp)
		if gain <= 0 {
			continue
		}
		pl.Exp += gain
		g.getCurrentScore(id)
		g.checkGetLevel(mu)
		g.sendScore(id)
		g.sendEtc(mu)
		g.sendEmotion(id, 14, 3)
		g.clientMessage(mu, ".+++ %d de Experiência (grupo) +++", gain)
	}
}

// useMysteryStone is item 4148: in town, swap the Celestial and the
// Sub-Celestial (status, face, exp, skills, soul, quests, affects and the
// skill bar), once every 5 seconds.
func (g *Game) useMysteryStone(u *User, src *wire.Item, resend func()) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	sub := &m.info.Sub
	if time.Since(u.subSwapAt) < 5*time.Second {
		g.clientMessage(u, "Aguarde alguns segundos para utilizar novamente...")
		resend()
		return
	}
	if g.data.attribute(int(m.target.X), int(m.target.Y))&attrVillage == 0 {
		g.clientMessage(u, "Não é possível utilizar fora da cidade")
		resend()
		return
	}
	ev := evolution(pl)
	if !(ev == evoCelestial && sub.Status == 1) && ev != evoSubCele {
		resend()
		return
	}
	if ev == evoCelestial {
		g.clientMessage(u, "Sua classe foi alterada para Subcelestial!")
	} else {
		g.clientMessage(u, "Sua classe foi alterada para Celestial!")
	}
	pl.BStatus, sub.SubStatus = sub.SubStatus, pl.BStatus
	pl.Equip[0], sub.Equip[0] = sub.Equip[0], pl.Equip[0]
	pl.Exp, sub.Exp = sub.Exp, pl.Exp
	switch face := int(pl.Equip[0].Effect[1].Index); {
	case face < 10:
		pl.ClassInfo = uint8(face - 6)
	case face < 20:
		pl.ClassInfo = uint8(face - 16)
	case face < 30:
		pl.ClassInfo = uint8(face - 26)
	case face < 40:
		pl.ClassInfo = uint8(face - 36)
	default:
		pl.ClassInfo = 0
	}
	pl.Learn[0], sub.Learn = sub.Learn, pl.Learn[0]
	pl.Learn[1], sub.SecLearn = sub.SecLearn, pl.Learn[1]
	m.info.Soul, sub.Soul = sub.Soul, m.info.Soul
	m.info.Info, sub.Info = sub.Info, m.info.Info
	m.info.Affects, sub.Affect = sub.Affect, m.info.Affects
	var bar [20]uint8
	copy(bar[:4], pl.SkillBar1[:])
	copy(bar[4:], m.info.SkillBar[:])
	copy(pl.SkillBar1[:], sub.SkillBar[:4])
	copy(m.info.SkillBar[:], sub.SkillBar[4:])
	sub.SkillBar = bar
	sub.Status = 1
	if pl.Equip[0].Effect[1].Value != evoCelestial {
		pl.Equip[0].Effect[1].Value = evoSubCele
	}
	g.getCurrentScore(u.id)
	g.block199(u)
	g.refreshMob(u.id)
	g.sendItem(u, wire.SlotEquip, 0, &pl.Equip[0])
	g.sendScore(u.id)
	g.sendEtc(u)
	g.sendAffect(u)
	sb := wire.SkillBar{Header: wire.NewHeader(0x378, uint16(u.id))}
	sb.SkillBar1 = pl.SkillBar1
	sb.SkillBar2 = m.info.SkillBar
	g.queue(u, &sb)
	amountMinus(src)
	resend()
	g.saveUser(u, 0)
	u.subSwapAt = time.Now()
}

// useArchCrystal is items 4106..4109: the four crystal quests, in order,
// for 100,000,000 exp (Arch 380+, or Celestial 40+ with 100 fame). Losing
// the exp can take levels away.
func (g *Game) useArchCrystal(u *User, src *wire.Item, n int, refuse func(string, ...any)) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	bits := [4]uint64{questElime, questSylphed, questThelion, questNoas}
	ev, level := evolution(pl), int(pl.BStatus.Level)
	if pl.Exp < 100000000 {
		refuse("Experiência insuficiente.")
		return
	}
	if m.info.Info&bits[n] != 0 {
		refuse("%s", g.lang(nnDoneAlready))
		return
	}
	for i := range n {
		if m.info.Info&bits[i] == 0 {
			refuse("Conclua a quest anterior para continuar")
			return
		}
	}
	switch ev {
	case evoArch:
		if level < 379 {
			refuse("Você só pode fazer a quest dos cristais 380 +")
			return
		}
	case evoCelestial:
		if m.info.Fame < 100 {
			refuse(g.lang(nnNeedXXFame), 100)
			return
		}
		if level < 39 {
			refuse("%s", g.lang(nnCantTeleNip))
			return
		}
	default:
		refuse("Disponível apenas para Arch e Celestial")
		return
	}
	m.info.Info |= bits[n]
	pl.Exp -= 100000000
	for pl.BStatus.Level > 0 && pl.Exp < nextLevel[ev][pl.BStatus.Level-1] {
		pl.BStatus.Level--
	}
	g.getCurrentScore(u.id)
	g.sendScore(u.id)
	g.sendEtc(u)
	amountMinus(src)
	g.sendEmotion(u.id, 14, 3)
}
