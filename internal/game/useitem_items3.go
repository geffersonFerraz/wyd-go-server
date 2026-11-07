package game

import (
	"time"

	"dbsrv/internal/wire"
)

// Third batch of the item-specific branches of RequestUseItem (from
// "EXTRAÇÕES NOVAS" to the end of the file): extractions, quest compounds,
// cash coins, fairy flashes, reward boxes and books, body/class change,
// potions, replations, the mission pass and the last odds and ends.

const (
	efHPAdd2       = 69
	efMPAdd2       = 70
	nnItemsAnct    = 380
	nnItemsArch    = 381
	nnItemsSmall9  = 382
	nnItemsPlus11  = 383
	nnItemsNotAnct = 384

	questMolarGargula = 1 << 22
	questBalance      = 1 << 35
	questGodBless     = 1 << 36

	opSrvCash      = 0x3B1 // RefreshGoldPacket, used for the cash balance
	opSrvResetSoul = 0x987 // ResetSoulDelayPacket
	skillSoulLimit = 102
	learnSoulLimit = 0x40000000
	escrituraSpeed = 10 * time.Hour
)

// extractionBlocked are items extractions (3021..3026) cannot be used on:
// Balmungs, Basileus, Caliburns, Thrastys, Skytalos, Hermai, Eirenus,
// Gleipnir, Khryus and the non-anct Arch weapons.
var extractionBlocked = map[uint16]bool{}

func init() {
	for _, id := range []uint16{1656, 1506, 1221, 1356, 1657, 1507, 1222, 1357, 1658, 1508, 1223, 1358, 1659, 1509, 1224, 1359,
		2859, 2860, 2861, 2862, 2491, 2492, 2493, 2494, 2935, 2936, 2937, 2938, 2731, 2732, 2733, 2734,
		2895, 2896, 2897, 2898, 2551, 2552, 2553, 2554, 2791, 2792, 2793, 2794, 2863, 2864, 2865, 2866,
		2671, 2672, 2673, 2674, 2611, 2612, 2613, 2614, 811, 937, 871, 912, 826, 886, 903, 856, 841} {
		extractionBlocked[id] = true
	}
}

// prize is one possible reward of a box or book.
type prize struct {
	id     uint16
	amount int
	ef, ev uint8 // an effect put in Effect[0] (overrides the amount)
}

// prizeTiers picks a tier like the event books: 0..50 common, ..80
// unusual, ..95 rare, else epic; then one prize of it.
func prizeTiers(tiers [4][]prize) prize {
	r, t := rnd()%100, 3
	switch {
	case r <= 50:
		t = 0
	case r <= 80:
		t = 1
	case r <= 95:
		t = 2
	}
	return tiers[t][rnd()%len(tiers[t])]
}

// freeSlotCount is how many slots firstEmptySlot can still hand out.
func freeSlotCount(pl *wire.Mob) int {
	n := 0
	count := func(from, to int) {
		for i := from; i < to; i++ {
			if pl.Inventory[i].Index == 0 {
				n++
			}
		}
	}
	count(0, invSlots)
	return n
}

// giveBox is the delivery shared by the boxes: the first item takes the
// box's own slot when it is the last one of its stack, the rest go to free
// slots. minFree is the free-slot minimum some boxes ask for. The C++ took
// the source slot as an inventory slot even for a box used from the
// storage, and some boxes wrote a second item to Inventory[-1] when full;
// here the space is checked first.
func (g *Game) giveBox(u *User, p *wire.UseItem, src *wire.Item, items []wire.Item, minFree int) bool {
	pl := &g.mobs[u.id].info.Player
	reuse := p.SrcType == wire.SlotInv && itemAmount(src) == 1
	free := freeSlotCount(pl)
	have := free
	if reuse {
		have++
	}
	if have < len(items) || free < minFree {
		if minFree > 0 && free < minFree {
			g.clientMessage(u, "Necessário no mínimo %d slots vagos no inventário", minFree)
		} else {
			g.clientMessage(u, "Sem espaço no inventário")
		}
		g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
		return false
	}
	first := items[0]
	if reuse {
		pl.Inventory[p.SrcSlot] = first
		g.sendItem(u, wire.SlotInv, int(p.SrcSlot), &pl.Inventory[p.SrcSlot])
		items = items[1:]
	} else {
		amountMinus(src)
		g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
	}
	for i := range items {
		g.putItem(u, &items[i])
	}
	g.clientMessage(u, "Chegou um item: [%s]", g.itemName(first.Index))
	g.log.Info("box opened", "slot", u.id, "box", src.Index, "item", first.Index)
	return true
}

// stackOf builds a prize item: a stack, or a single item with its effect.
func stackOf(pz prize, noTrade bool) wire.Item {
	it := wire.Item{Index: pz.id}
	if pz.amount > 1 {
		it.Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: uint8(pz.amount)}
		if noTrade {
			it.Effect[1] = wire.ItemEffect{Index: EF_NOTRADE, Value: 1}
		}
	} else if noTrade {
		it.Effect[0] = wire.ItemEffect{Index: EF_NOTRADE, Value: 1}
	}
	if pz.ef != 0 {
		it.Effect[0] = wire.ItemEffect{Index: pz.ef, Value: pz.ev}
	}
	return it
}

func (g *Game) useItemByID3(u *User, p *wire.UseItem, src *wire.Item) bool {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	id := src.Index
	resend := func() { g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src) }
	refuse := func(msg string, args ...any) {
		g.clientMessage(u, msg, args...)
		resend()
	}
	dst := func() *wire.Item { return g.slotItemAny(u, int(p.DstType), int(p.DstSlot)) }
	box := func(items ...wire.Item) { g.giveBox(u, p, src, items, 0) }

	switch {
	case id >= 3021 && id <= 3026: // extractions: the stored bonuses onto Equip[1..6]
		g.useExtraction(u, p, src, int(id-3020), refuse)

	case id == 4126 || id == 4124: // Compostos do Equilíbrio / de Chance (mortal quests)
		lo, bit := 119, uint64(questBalance)
		if id == 4124 {
			lo, bit = 69, questGodBless
		}
		switch {
		case int(pl.BStatus.Level) < lo || int(pl.BStatus.Level) > lo+5:
			refuse("Level inadequado.") // the C++ did not give the item back
			return true
		case evolution(pl) != evoMortal:
			refuse("Somente para mortais.")
			return true
		case m.info.Info&bit != 0:
			refuse("Quest realizada.")
			return true
		}
		// Equilíbrio: all masteries back; Chance: 50 off each mastery of 50+.
		// GetCurrentScore gives the points back from the level.
		for i := range pl.BStatus.Mastery {
			switch {
			case id == 4126:
				pl.BStatus.Mastery[i] = 0
			case pl.BStatus.Mastery[i] >= 50:
				pl.BStatus.Mastery[i] -= 50
			}
		}
		pl.Learn[0] &^= 0xFFFFFF // the class skills
		m.info.Info |= bit
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.sendAffect(u)
		g.sendEtc(u)
		amountMinus(src)
		resend()
		g.clientMessage(u, "Processo concluído.")

	case id == 4122: // Molar de Gárgula: +4 (up to +6) on the armor up to +5
		if pl.BStatus.Level < 199 || pl.BStatus.Level > 254 {
			refuse(g.lang(nnLevelNotAllowed), 200, 255)
			return true
		}
		if m.info.Info&questMolarGargula != 0 {
			refuse("%s", g.lang(nnDoneAlready))
			return true
		}
		m.info.Info |= questMolarGargula
		for i := 1; i < 6; i++ {
			it := &pl.Equip[i]
			if it.Index == 0 || int(it.Index) >= MaxItemList {
				continue
			}
			if s := itemSanc(it); s < 6 {
				setItemSanc(it, min(s+4, 6), 0)
				g.sendItem(u, wire.SlotEquip, i, it)
			}
		}
		amountMinus(src)
		g.getCurrentScore(u.id)
		g.sendScore(u.id)

	case id >= 4533 && id <= 4535: // cash coins
		add := [3]uint32{200, 500, 1000}[id-4533]
		u.account.Cash += add
		g.clientMessage(u, "Foram adicionados %d Cash em sua conta. Saldo total: %d", add, u.account.Cash)
		amountMinus(src)
		g.signalParm(u, wire.ClientIDSystem, opSrvCash, int32(u.account.Cash))

	case id == 4688 || id == 3451 || id == 3452: // fairy flashes
		fairy := &pl.Equip[13]
		to := uint16(3917)
		switch {
		case id == 4688 && fairy.Index != 3916:
			refuse("Somente disponível para Fada do Vale")
			return true
		case id != 4688 && fairy.Index != 3902 && fairy.Index != 3905 && fairy.Index != 3908:
			refuse("Somente disponível para Fada Vermelha")
			return true
		case id == 3451:
			to = 3914
		case id == 3452:
			to = 3915
		}
		fairy.Index = to
		g.sendItem(u, wire.SlotEquip, 13, fairy)
		amountMinus(src)
		g.getCurrentScore(u.id)
		g.sendScore(u.id)

	case id == 4545: // Baú Mercantil: needs the key 4546 or 10,000,000 gold
		key := firstSlotOf(pl, 4546)
		if key == -1 && pl.Gold < 10000000 {
			refuse("Necessário 10 milhões de gold ou Chave Mercantil!")
			return true
		}
		var it wire.Item
		switch r := rnd() % 100; {
		case r <= 50:
			it = []wire.Item{{Index: 4547}, stackOf(prize{id: 412, amount: 5}, false)}[rnd()%2]
		case r <= 90:
			it = stackOf([]prize{{id: 413, amount: 2}, {id: 404, amount: 50}, {id: 409, amount: 50}}[rnd()%3], false)
		default:
			it = stackOf(prize{id: 4547, amount: 3}, false)
		}
		// The C++ took the key or gold and the box even with no room for
		// the prize; the room is checked first here.
		if !g.giveBox(u, p, src, []wire.Item{it}, 0) {
			return true
		}
		if key != -1 {
			amountMinus(&pl.Inventory[key])
			g.sendItem(u, wire.SlotInv, key, &pl.Inventory[key])
		} else {
			pl.Gold -= 10000000
			g.signalParm(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
		}

	case id == 4649: // Troca de Corpo: new body from the sephirot (Celestial+)
		g.changeBody(u, src, refuse)

	case id == 3486: // Troca de Classe: new class from the sephirot (Arch+)
		g.changeClass(u, src, refuse)

	case id == 4595: // remover de traje de montaria
		mount := &pl.Equip[14]
		traje := int(mount.Effect[2].Value)
		if traje < 11 || mount.Index < 2360 || mount.Index >= 2390 {
			refuse("Utilize com uma montaria utilizando traje")
			return true
		}
		switch traje {
		case 12, 13, 14, 15, 16, 20:
		default:
			refuse("Você não pode remover este traje")
			return true
		}
		suit := wire.Item{Index: uint16(4190 + traje - 11)}
		if !g.giveBox(u, p, src, []wire.Item{suit}, 0) {
			return true
		}
		mount.Effect[2].Value = 0
		g.sendItem(u, wire.SlotEquip, 14, mount)

	case id == 4624 || id == 4668 || id == 4639: // Poção Gelada, Annubis, Panqueca
		idx, dur := uint8(54), uint32(2700)
		switch id {
		case 4668:
			idx = 59
		case 4639:
			idx, dur = 55, 900
		}
		slot := affectSlot(m, idx)
		if slot == -1 {
			resend()
			return true
		}
		m.info.Affects[slot] = wire.Affect{Index: idx, Value: 1, Time: dur}
		g.getCurrentScore(u.id)
		g.refreshMob(u.id)
		g.sendAffect(u)
		g.sendScore(u.id)
		amountMinus(src)

	case id == 4541: // Baú Congelado (3 free slots)
		prizes := []prize{{id: 4542, amount: 1}, {id: 4140, amount: 5}, {id: 3314, amount: 3}, {id: 4538, amount: 1}, {id: 4522, amount: 3}, {id: 4028, amount: 1}}
		pz := prizes[rnd()%len(prizes)]
		var items []wire.Item
		switch {
		case pz.id == 4522: // three separate items
			for range 3 {
				items = append(items, wire.Item{Index: 4522, Effect: [3]wire.ItemEffect{{Index: EF_NOTRADE, Value: 1}}})
			}
		case pz.id == 4542:
			items = []wire.Item{{Index: 4542}}
		default:
			items = []wire.Item{stackOf(pz, true)}
		}
		g.giveBox(u, p, src, items, 3)

	case id == 4623 || id == 4647 || id == 4657: // Livro Amaldiçoado, Baú do Sukur, Livro Annubis
		prizes := map[uint16][]prize{
			4623: {{4522, 3, 0, 0}, {4140, 5, 0, 0}, {3314, 3, 0, 0}, {4627, 1, 0, 0}, {4624, 1, 0, 0}, {777, 5, 0, 0}, {4028, 1, 0, 0}, {4633, 1, 0, 0}},
			4647: {{2411, 20, 0, 0}, {2412, 20, 0, 0}, {2413, 20, 0, 0}, {3182, 10, 0, 0}, {3182, 10, 0, 0}, {3326, 1, 0, 0}, {4140, 10, 0, 0}, {4140, 10, 0, 0}, {671, 1, 0, 0}, {4633, 1, 0, 0}},
			4657: {{2411, 30, 0, 0}, {2412, 30, 0, 0}, {2413, 30, 0, 0}, {3326, 1, 0, 0}, {4633, 2, 0, 0}, {4140, 10, 0, 0}, {4140, 10, 0, 0}, {4854, 1, 0, 0}, {3210, 1, 0, 0}, {4665, 10, 0, 0}, {633, 1, 0, 0}},
		}[id]
		box(stackOf(prizes[rnd()%len(prizes)], true))

	case id == 4667: // Livro Poseidon
		box(stackOf(prizeTiers([4][]prize{
			{{4633, 3, 0, 0}, {2411, 30, 0, 0}, {2412, 30, 0, 0}, {2413, 30, 0, 0}, {4672, 1, 0, 0}},
			{{2414, 15, 0, 0}, {2415, 15, 0, 0}, {2416, 15, 0, 0}, {4665, 10, 0, 0}},
			{{4679, 1, 0, 0}, {633, 1, 0, 0}, {671, 1, 0, 0}, {4673, 1, 0, 0}},
			{{4854, 1, 0, 0}},
		}), false))

	case id == 4715: // Livro Junino
		box(stackOf(prizeTiers([4][]prize{
			{{2414, 30, 0, 0}, {2415, 30, 0, 0}, {2416, 30, 0, 0}, {4708, 1, 0, 0}},
			{{4679, 1, 0, 0}, {4851, 1, 0, 0}, {3338, 1, 0, 0}, {4665, 10, 0, 0}},
			{{633, 1, efHPAdd2, 8}, {633, 1, efMPAdd2, 8}, {633, 1, EF_CRITICAL, 40}, {4709, 1, 0, 0}},
			{{4854, 1, 0, 0}},
		}), false))

	case id == 4724: // Livro do Bone Demon (2 free slots)
		pz := prizeTiers([4][]prize{
			{{2414, 30, 0, 0}, {2415, 30, 0, 0}, {2416, 30, 0, 0}, {1731, 1, 0, 0}, {4727, 1, 0, 0}},
			{{3338, 1, EF_SANC, 1}, {4665, 10, 0, 0}, {4864, 1, EF_SANC, 1}, {4679, 1, 0, 0}, {3182, 15, 0, 0}},
			{{633, 1, efHPAdd2, 8}, {633, 1, efMPAdd2, 8}, {633, 1, EF_CRITICAL, 40}, {4728, 1, 0, 0}},
			{{3338, 1, EF_SANC, 9}, {3338, 2, EF_SANC, 4}},
		})
		items := []wire.Item{stackOf(pz, false)}
		if pz.id == 3338 { // each 3338 is its own item
			items = nil
			for range pz.amount {
				items = append(items, wire.Item{Index: 3338, Effect: [3]wire.ItemEffect{{Index: pz.ef, Value: pz.ev}}})
			}
		}
		g.giveBox(u, p, src, items, 2)

	case id == 4633: // Caixa Mytril: a Mythril bar 3027..3030
		box(wire.Item{Index: uint16(3027 + rnd()%4)})

	case id == 4631 || id == 4632: // Caixas de Armas D / E
		dmgW := []uint16{824, 825, 839, 840, 870, 884, 885, 911, 809, 810}
		magW := []uint16{854, 855, 902, 900}
		dmgV := []uint8{54, 54, 54, 54, 63, 63, 63, 72}
		magV := []uint8{24, 24, 24, 24, 28, 28, 28, 32}
		if id == 4632 {
			dmgW = []uint16{3551, 3556, 3561, 3571, 3576, 3591, 3596}
			magW = []uint16{3566, 3581, 3582}
			dmgV = append(dmgV, 72, 72)
			magV = append(magV, 32, 32)
		}
		it := wire.Item{Effect: [3]wire.ItemEffect{{Index: EF_SANC}}}
		if rnd()%50 <= 25 {
			it.Effect[1] = wire.ItemEffect{Index: EF_DAMAGE, Value: dmgV[rnd()%len(dmgV)]}
			it.Index = dmgW[rnd()%len(dmgW)]
		} else {
			it.Effect[1] = wire.ItemEffect{Index: EF_MAGIC, Value: magV[rnd()%len(magV)]}
			it.Index = magW[rnd()%len(magW)]
		}
		box(it)

	case id == 4634: // Caixa Amuleto Arcano
		it := wire.Item{Index: uint16(567 + rnd()%4), Effect: [3]wire.ItemEffect{{Index: EF_SANC}}}
		switch rnd() % 3 {
		case 0:
			it.Effect[1], it.Effect[2] = wire.ItemEffect{Index: efHPAdd2, Value: 10}, wire.ItemEffect{Index: EF_MAGIC, Value: 16}
		case 1:
			it.Effect[1], it.Effect[2] = wire.ItemEffect{Index: efMPAdd2, Value: 10}, wire.ItemEffect{Index: EF_MAGIC, Value: 16}
		default:
			it.Effect[1], it.Effect[2] = wire.ItemEffect{Index: efHPAdd2, Value: 10}, wire.ItemEffect{Index: EF_DAMAGE, Value: 40}
		}
		box(it)

	case id == 4615 || id == 4642: // Ovos da Sorte: an adult mount
		mount := uint16(2360 + rnd()%6)
		if id == 4642 {
			mount = []uint16{2366, 2367, 2368, 2371, 2372, 2373}[rnd()%6]
		}
		it := wire.Item{Index: mount}
		setMountHP(&it, 5000)
		it.Effect[1] = wire.ItemEffect{Index: 120, Value: 10}
		it.Effect[2] = wire.ItemEffect{Index: 100}
		box(it)

	case id == 4665: // Baú de Runas
		box(wire.Item{Index: uint16(5110 + rnd()%24)})

	case id == 4676: // Caixa de Secretas
		box(wire.Item{Index: uint16(5334 + rnd()%4)})

	case id == 4572: // Compactador de Gold: 1,000,000,000 gold -> bar 4011
		if pl.Gold < 1000000000 {
			refuse("O gold mínimo para compactar é de 1 bilhão")
			return true
		}
		if g.giveBox(u, p, src, []wire.Item{{Index: 4011}}, 0) {
			pl.Gold -= 1000000000
			g.signalParm(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
		}

	case id == 4719: // Pó de Brinco: 120 + 100,000,000 gold -> Emblema da Proteção 4611
		if itemAmount(src) != 120 {
			refuse("Necessário 120 unidades de Pó de Brinco")
			return true
		}
		if pl.Gold < 100000000 {
			refuse("Necessário 100 milhões de gold")
			return true
		}
		pl.Gold -= 100000000
		g.signalParm(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
		*src = wire.Item{Index: 4611}
		resend()
		g.clientMessage(u, "Chegou um item: [%s]", g.itemName(4611))

	case id == 4686 || id == 4687: // Replations Imp (753) / Griupan (1726)
		d := dst()
		if p.DstType != wire.SlotInv {
			refuse("%s", g.lang(nnOnlyToEquips))
			return true
		}
		if d == nil || d.Index == 0 {
			resend()
			return true
		}
		if id == 4686 && d.Index != 753 {
			refuse("Somente utilizado em auxiliares Imp")
			return true
		}
		if id == 4687 && d.Index != 1726 {
			refuse("Somente utilizado em auxiliares Griupan")
			return true
		}
		var add wire.ItemEffect
		if id == 4686 {
			if rnd()%100 < 50 {
				add = wire.ItemEffect{Index: EF_DAMAGE, Value: uint8(27 + rnd()%7*3)}
			} else {
				add = wire.ItemEffect{Index: EF_MAGIC, Value: uint8(5 + rnd()%6)}
			}
		} else {
			switch rnd() % 4 {
			case 0:
				add = wire.ItemEffect{Index: EF_AC, Value: uint8(70 + rnd()%4*10)}
			case 1:
				add = wire.ItemEffect{Index: EF_RESISTALL, Value: 10}
				if rnd()%11 <= 5 {
					add.Value = 8
				}
			case 2:
				add = wire.ItemEffect{Index: efHPAdd2, Value: uint8(4 + rnd()%3*2)}
			default:
				add = wire.ItemEffect{Index: efMPAdd2, Value: uint8(4 + rnd()%3*2)}
			}
		}
		sanc := itemSanc(d)
		*d = wire.Item{Index: d.Index, Effect: [3]wire.ItemEffect{{Index: EF_SANC}, add, {Index: EF_NOTRADE, Value: 1}}}
		// The C++ stored the refine level as the raw value, so a +10 or more
		// came back as a broken +10; SetItemSanc encodes it.
		setItemSanc(d, sanc, 0)
		g.sendItem(u, int(p.DstType), int(p.DstSlot), d)
		amountMinus(src)

	case id == 4610: // Replation de Brinco (590..595 up to +9)
		d := dst()
		if p.DstType != wire.SlotInv {
			refuse("%s", g.lang(nnOnlyToEquips))
			return true
		}
		if d == nil || d.Index == 0 {
			resend()
			return true
		}
		if d.Index < 590 || d.Index > 595 {
			refuse("Somente utilizado em Brincos")
			return true
		}
		if itemSanc(d) > 9 {
			refuse("Somente utilizado em Brincos igual ou inferior a +9")
			return true
		}
		if d.Effect[0].Index != EF_SANC {
			d.Effect[0] = wire.ItemEffect{}
		}
		var add wire.ItemEffect
		switch r := rnd() % 100; {
		case r <= 25:
			add = wire.ItemEffect{Index: efHPAdd2, Value: uint8(rnd()%4 + 3)}
		case r <= 50:
			add = wire.ItemEffect{Index: efMPAdd2, Value: uint8(rnd()%4 + 3)}
		case r <= 75:
			add = wire.ItemEffect{Index: EF_DAMAGE, Value: uint8(rnd()%26 + 15)}
		default:
			add = wire.ItemEffect{Index: EF_MAGIC, Value: uint8(rnd()%7 + 4)}
		}
		for i := 1; i < 3; i++ {
			if d.Effect[i].Index != EF_SANC {
				d.Effect[i] = add
				break
			}
		}
		g.sendItem(u, int(p.DstType), int(p.DstSlot), d)
		amountMinus(src)

	case id == 4616 || id == 4617 || id == 4618 || id == 4621: // Passe de Missões
		pass := &m.info.DailyQuest.BattlePass
		if dateRemaining(*pass) > 0 {
			refuse("Você já possui um Passe de Missões ativo. Espere o mesmo acabar para ativar outro")
			return true
		}
		days := map[uint16]int{4616: 30, 4617: 7, 4618: 15, 4621: 1}[id]
		end := time.Now().AddDate(0, 0, days)
		*pass = itemDate(end)
		g.clientMessage(u, "Seu Passe de Missões foi ativado até %s", end.Format("02/01/2006 às 15:04:05"))
		amountMinus(src)
		resend()
		// TODO(phase E): SendMissionInfo (the daily mission window).

	case id == 3214: // Jóia da Alma: resets the Limite da Alma cooldown
		if pl.Learn[0]&learnSoulLimit == 0 {
			refuse("Você não possui a habilidade Limite de Alma")
			return true
		}
		u.skillUsed[skillSoulLimit] = time.Time{}
		g.signal(u, wire.ClientIDSystem, opSrvResetSoul)
		amountMinus(src)

	case id >= 3230 && id <= 3244: // Ímago Abençoado: mount level 120..129 -> +1
		g.useBlessedImago(u, src, refuse)

	case id == 792: // Fumaça Cósmica: planet +11 -> its next form
		planet := &pl.Equip[11]
		switch {
		case planet.Index < 762 || planet.Index > 765:
			refuse("Equipe um planeta no slot adequado")
		case itemSanc(planet) != 11:
			refuse("O planeta precisa estar +11 para composição")
		case m.info.Fame < 500:
			refuse("Você precisa de 500 de fama para composição")
		case inventoryAmount(pl, 4127) < 10:
			refuse("Você precisa de 10 Pergaminho Selado")
		default:
			for range 10 {
				if s := firstSlotOf(pl, 4127); s != -1 {
					pl.Inventory[s] = wire.Item{}
					g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
				}
			}
			m.info.Fame -= 500
			amountMinus(src)
			planet.Index += 26
			setItemSanc(planet, 0, 0)
			g.sendItem(u, wire.SlotEquip, 11, planet)
			g.getCurrentScore(u.id)
			g.sendScore(u.id)
		}

	case id == 4682: // Acelerador de Escritura: the Escritura de Pesadelo wait -10 h
		if evolution(pl) <= evoArch {
			refuse("Disponível apenas para celestiais ou superior")
			return true
		}
		e := m.info.Escritura
		if e.Ano == 0 {
			refuse("Você não está com tempo de espera para usar uma próxima Escritura de Pesadelo")
			return true
		}
		last := time.Date(int(e.Ano), time.Month(e.Mes), int(e.Dia), int(e.Hora), int(e.Minuto), int(e.Segundo), 0, time.Local)
		if time.Since(last) >= nightmareScrollDelay {
			refuse("Você não está com tempo de espera para usar uma próxima Escritura de Pesadelo")
			return true
		}
		// The C++ did the date math by hand (and read dias_mes[0] in January).
		m.info.Escritura = itemDate(last.Add(-escrituraSpeed))
		amountMinus(src)
		resend()

	default:
		return false
	}
	return true
}

// useExtraction is items 3021..3026: the bonuses kept in the extraction go
// onto the equipment in slot n, keeping its refine. Effect 87 holds the
// extraction type: 0..4 sets up to +9, 8/9 non-anct Arch weapons (+9 or
// less / +11), 10/11 anct Arch weapons (+9 or less / +11).
func (g *Game) useExtraction(u *User, p *wire.UseItem, src *wire.Item, n int, refuse func(string, ...any)) {
	pl := &g.mobs[u.id].info.Player
	d := &pl.Equip[n]
	if extractionBlocked[d.Index] {
		refuse("Não é possível utilizar extração neste item.")
		return
	}
	if d.Index == 0 || int(d.Index) >= MaxItemList {
		refuse("Equipe o item que vai receber a extração")
		return
	}
	typ := -1
	for i := range 3 {
		if src.Effect[i].Index == EF_UNKNOW1 {
			typ = int(src.Effect[i].Value)
			break
		}
	}
	sanc := itemSanc(d)
	mobType := g.data.effectValueByIndex(d.Index, EF_MOBTYPE)
	grade := int(g.data.Item(d.Index).Grade)
	anct := grade >= 5 && grade <= 8
	switch {
	case typ >= 0 && typ <= 4:
		if sanc > 9 {
			refuse("%s", g.lang(nnItemsSmall9))
			return
		}
	case typ == 10 || typ == 11:
		switch {
		case !anct:
			refuse("%s", g.lang(nnItemsAnct))
			return
		case mobType != 1:
			refuse("%s", g.lang(nnItemsArch))
			return
		case typ == 10 && sanc > 9:
			refuse("%s", g.lang(nnItemsSmall9))
			return
		case typ == 11 && sanc != 11:
			refuse("%s", g.lang(nnItemsPlus11))
			return
		}
	case typ == 8 || typ == 9:
		switch {
		case anct:
			refuse("%s", g.lang(nnItemsNotAnct))
			return
		case mobType != 1:
			refuse("%s", g.lang(nnItemsArch))
			return
		case typ == 9 && sanc != 11:
			refuse("%s", g.lang(nnItemsSmall9))
			return
		case typ == 8 && sanc > 9:
			refuse("%s", g.lang(nnItemsSmall9))
			return
		}
	}
	ref := uint8(0)
	if e := refineEffect(d); e != -1 {
		ref = d.Effect[e].Index // keeps a colored refine
	}
	out := *src
	for i := range 3 {
		if out.Effect[i].Index == EF_UNKNOW1 {
			out.Effect[i] = wire.ItemEffect{Index: ref}
			break
		}
	}
	// The C++ fell back to the raw refine value for a +0 item, which could
	// carry the failure count into SetItemSanc; the level is used here.
	setItemSanc(&out, sanc, 0)
	out.Index = d.Index
	g.log.Info("extraction", "slot", u.id, "item", d.Index, "before", d.Effect, "after", out.Effect)
	*d = out
	amountMinus(src)
	g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
	g.sendItem(u, wire.SlotEquip, n, d)
	g.getCurrentScore(u.id)
	g.sendScore(u.id)
	g.sendEquip(u)
}

// sephirotClass is the class (0..3) of the sephirot in Equip[11], or -1.
func sephirotClass(pl *wire.Mob) int {
	if s := int(pl.Equip[11].Index); s >= 1760 && s <= 1763 {
		return s - 1760
	}
	return -1
}

// faceClass is the class of a face (Equip[0].EF2).
func faceClass(face int) int {
	switch {
	case face < 10:
		return face - 6
	case face < 20:
		return face - 16
	case face < 30:
		return face - 26
	case face < 40:
		return face - 36
	}
	return 0
}

// relog ends a body/class change: back to the character list and straight
// into the same character (0x3B4).
func (g *Game) relog(u *User, src *wire.Item) {
	pl := &g.mobs[u.id].info.Player
	amountMinus(src)
	pl.Equip[11] = wire.Item{}
	g.clientMessage(u, "Que os Deuses continuem te abençoando")
	slot := u.charSlot
	g.charLogout(u)
	g.signalParm(u, uint16(u.id), opSrvOutSealSlot, int32(slot))
}

// changeBody is item 4649: the body of the sephirot's class, keeping the
// class (and the Sub-Celestial's).
func (g *Game) changeBody(u *User, src *wire.Item, refuse func(string, ...any)) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	for i := 1; i < 8; i++ {
		if pl.Equip[i].Index != 0 {
			refuse("Necessário desequipar todos os seus itens")
			return
		}
	}
	if evolution(pl) <= evoArch {
		refuse("Disponível apenas para celestiais ou superior")
		return
	}
	seph := sephirotClass(pl)
	if seph == -1 {
		refuse("Necessário a sephirot da classe que deseja o novo corpo")
		return
	}
	face := seph*10 + int(pl.ClassInfo) + 6
	if face == int(pl.Equip[0].Index) {
		refuse("Não é possível trocar para a mesma classe")
		return
	}
	pl.Equip[0].Index, pl.Equip[0].Effect[1].Index = uint16(face), uint8(face)
	if m.info.Sub.Status == 1 || evolution(pl) == evoSubCele {
		sub := &m.info.Sub.Equip[0]
		sf := seph*10 + faceClass(int(sub.Effect[1].Index)) + 6
		sub.Index, sub.Effect[1].Index = uint16(sf), uint8(sf)
	}
	g.log.Info("body changed", "slot", u.id, "face", face)
	g.relog(u, src)
}

// changeClass is item 3486: the class of the sephirot, on the same body.
// The learned skills are kept as bits, as in the C++.
func (g *Game) changeClass(u *User, src *wire.Item, refuse func(string, ...any)) {
	pl := &g.mobs[u.id].info.Player
	if evolution(pl) < evoArch {
		refuse("Não disponível para mortais!")
		return
	}
	seph := sephirotClass(pl)
	if seph == -1 {
		refuse("Necessário o sephirot da classe que deseja.")
		return
	}
	base := int(pl.Equip[0].Effect[1].Index) - int(pl.ClassInfo)
	if base != 6 && base != 16 && base != 26 && base != 36 {
		refuse("Incorreto. Contate a administração #0001")
		return
	}
	face := base + seph
	if face == int(pl.Equip[0].Index) {
		refuse("Não é possível trocar para a mesma classe")
		return
	}
	pl.ClassInfo = uint8(seph)
	pl.Equip[0].Index, pl.Equip[0].Effect[1].Index = uint16(face), uint8(face)
	g.log.Info("class changed", "slot", u.id, "class", seph, "face", face)
	g.relog(u, src)
}

// blessedImago maps an adult mount to its Ímago Abençoado (3230..3243).
var blessedImago = map[uint16]uint16{2370: 3230, 2375: 3231, 2376: 3232, 2377: 3233, 2379: 3234, 2380: 3235,
	2381: 3236, 2382: 3237, 2383: 3238, 2384: 3239, 2385: 3240, 2386: 3241, 2387: 3242, 2388: 3243}

// useBlessedImago is items 3230..3244: a mount of level 120..129 gains a
// level, with 50% minus 2% per level above 120.
func (g *Game) useBlessedImago(u *User, src *wire.Item, refuse func(string, ...any)) {
	mount := &g.mobs[u.id].info.Player.Equip[14]
	if mount.Index < 2330 || mount.Index > 2389 || blessedImago[mount.Index] != src.Index {
		refuse("%s", g.lang(nnMountNotMatch))
		return
	}
	level := int(mount.Effect[1].Index)
	if level < 120 {
		refuse("Necessário montaria nível 120")
		return
	}
	if level >= 130 {
		refuse("O nível máximo é 130")
		return
	}
	r, rate := rnd()%100, 50-(level-120)*2
	if r <= rate {
		mount.Effect[1].Index++
		g.sendItem(u, wire.SlotEquip, 14, mount)
		g.clientMessage(u, "Sucesso na refinação (%d/%d)", r, rate)
	} else {
		g.clientMessage(u, "Falha na refinação (%d/%d)", r, rate)
	}
	amountMinus(src)
	for i := range g.mobs[u.id].info.Player.Inventory {
		if &g.mobs[u.id].info.Player.Inventory[i] == src {
			g.sendItem(u, wire.SlotInv, i, src)
		}
	}
}
