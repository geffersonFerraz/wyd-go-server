package game

import (
	"time"

	"dbsrv/internal/wire"
)

// The "type 100" NPCs of RequestMerchantNPC (Info 8): what each one does
// is picked by its Status.Level.

// Newbie quests (Coveiro, Jardim, Kaizen, Hidra, Elfo): g_pQuestLevel,
// MaxMinCoordsQuest and g_pQuestTele.
var (
	questArea = [5][4]int{
		{2379, 2078, 2425, 2131},
		{2229, 1700, 2257, 1728},
		{460, 3888, 497, 3916},
		{658, 3728, 703, 3762},
		{1313, 4027, 1348, 4055},
	}
	questTele  = [5][2]int{{2397, 2105}, {2234, 1714}, {464, 3902}, {668, 3755}, {1326, 4043}}
	questNames = [5]string{"Coveiro", "Jardim", "Kaizen", "Hidra", "Elfo"}
)

// oneAccountIn reports another account of the same computer playing inside
// the rectangle.
func (g *Game) oneAccountIn(u *User, minX, minY, maxX, maxY int) bool {
	for _, o := range g.users[1:] {
		if o.status != userPlay || o.mac != u.mac {
			continue
		}
		t := g.mobs[o.id].target
		if int(t.X) >= minX && int(t.X) <= maxX && int(t.Y) >= minY && int(t.Y) <= maxY {
			return true
		}
	}
	return false
}

func (g *Game) npcQuest(u *User, npc int, click uint32) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	typ := int(g.mobs[npc].info.Player.Status.Level)
	level := int(pl.BStatus.Level)
	evo := evolution(pl)
	name := wire.CString(pl.Name[:])
	mortalOnly := func() bool {
		if evo != evoMortal {
			g.clientMessage(u, "Somente para mortais.")
			return false
		}
		return true
	}
	levelIn := func(lo, hi int) bool {
		if level < lo || level > hi {
			g.clientMessage(u, "Level inadequado.")
			return false
		}
		return true
	}

	switch {
	case typ >= 5 && typ <= 9: // newbie quest entrances
		q := typ - 5
		if evo >= evoArch {
			g.sendSay(npc, "%s", g.lang(nnOnlyMortal))
			return
		}
		lo, hi := questLevel[q][0], questLevel[q][1]
		if level < lo || level > hi {
			g.sendSay(npc, g.lang(nnLevelNotAllowed), lo+1, hi+1)
			return
		}
		a, t := questArea[q], m.target
		if int(t.X) >= a[0] && int(t.Y) >= a[1] && int(t.X) <= a[2] && int(t.Y) <= a[3] {
			g.clientMessage(u, "Já esta dentro da quest!")
			return
		}
		slot := -1
		for s := range 60 {
			if int(pl.Inventory[s].Index) == 4038+q {
				slot = s
				break
			}
		}
		if slot < 0 {
			g.clientMessage(u, "Você não possui o item necessário para entrar nesta quest.")
			return
		}
		pl.Inventory[slot] = wire.Item{}
		g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
		g.teleportar(u.id, questTele[q][0], questTele[q][1])
		g.sendSay(npc, g.lang(nnPlayerQuestEnter), name)
		g.log.Info("entered quest", "slot", u.id, "quest", questNames[q])
		u.questAccess = q + 1

	case typ >= 11 && typ <= 13: // Perzen: mount eggs
		k := typ - 11
		want := uint16(4128 + k)
		slot := firstSlotOf(pl, want)
		if slot < 0 {
			g.sendSay(npc, g.lang(nnGiveMeItem), g.data.Item(want).Name)
			return
		}
		pl.Inventory[slot] = wire.Item{Index: uint16(3986 + k)}
		g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
		g.sendSay(npc, g.lang(nnReceivedMountPerzen), g.data.Item(uint16(3986+k)).Name)

	case typ == 14: // Juli: to and from Nippleheim
		if g.data.village(int(m.target.X), int(m.target.Y)) == 3 {
			g.teleportar(u.id, 2482, 1652)
			return
		}
		if evo == evoMortal && pl.Status.Level < 255 {
			g.sendSay(npc, "%s", g.lang(nnCantTeleNip))
			return
		}
		g.sendSay(npc, "%s", g.lang(nnWelcomeNipp))
		g.teleportar(u.id, 3649, 3140)

	case typ == 20: // green cape quest: entrance
		if level < 99 || level > 149 {
			g.sendSay(npc, g.lang(nnLevelNotAllowed), 100, 150)
			return
		}
		if evo != evoMortal {
			g.sendSay(npc, "Somente para personagens mortais")
			return
		}
		g.teleportar(u.id, 2242, 1577)
		lines := [4]string{
			"Tenha cuidado, futuro aprendiz",
			"Você é corajoso por querer entrar.",
			"Sua coragem sera recompensada!",
			"Você é um dos grandes guerreiros de Midgard?",
		}
		g.sendSay(npc, "%s", lines[rnd()%4])
		u.questAccess = 5

	case typ == 21 && click == 0: // green cape quest: the cape
		if level < 99 || level > 149 {
			g.sendSay(npc, g.lang(nnLevelNotAllowed), 100, 150)
			return
		}
		if evo != evoMortal {
			g.sendSay(npc, "Somente para personagens mortais")
			return
		}
		if pl.Equip[15].Index == 4006 {
			g.sendSay(npc, "%s", g.lang(nnAlreadyALearner))
			return
		}
		if pl.Equip[13].Index != 4080 {
			g.sendSay(npc, "%s", g.lang(nnBringMeToLearner))
			return
		}
		pl.Equip[15], pl.Equip[13] = wire.Item{Index: 4006}, wire.Item{}
		g.sendItem(u, wire.SlotEquip, 13, &pl.Equip[13])
		g.sendItem(u, wire.SlotEquip, 15, &pl.Equip[15])
		g.sendSay(npc, g.lang(nnWelcomeLearner), name)
		g.getCurrentScore(u.id)
		g.sendScore(u.id)

	case typ == 22: // Sobrevivente: Kefra hall entries
		slot := firstSlotOf(pl, 4127)
		if slot < 0 {
			g.sendSay(npc, "%s", g.lang(nnBringMeSelado))
			return
		}
		m.info.HallEnter += 25
		g.sendSay(npc, g.lang(nnHallEnter), m.info.HallEnter)
		pl.Inventory[slot] = wire.Item{}
		g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
		switch r := rnd() % 100; {
		case r < 20:
			g.sendSay(npc, "Boa sorte, meu jovem!")
		case r < 40:
			g.sendSay(npc, "Que os Deuses te ajude, guerreiro!")
		case r < 60:
			g.sendSay(npc, "Você vai precisar de muita sorte, %s", name)
		default:
			g.sendSay(npc, "Vai precisar de uma equipe aí dentro!")
		}

	case typ == 30: // Molar de Gárgula
		if level < 199 || level > 254 {
			g.sendSay(npc, g.lang(nnLevelNotAllowed), 200, 255)
			return
		}
		if g.oneAccountIn(u, 793, 4046, 828, 4080) {
			g.clientMessage(u, "Somente uma conta por computador nesta area")
			return
		}
		g.teleportar(u.id, 811, 4055)

	case typ == 50: // event area
		if g.oneAccountIn(u, 1304, 1438, 1379, 1473) {
			g.clientMessage(u, "Disponivel apenas uma conta por computador")
			return
		}
		g.teleportar(u.id, 1349, 1464)

	case typ == 35: // Guarda Real: kingdom knight quest
		if evo != evoMortal {
			g.sendSay(npc, "%s", g.lang(nnOnlyMortal))
			return
		}
		if level < 219 || level > 250 {
			g.sendSay(npc, g.lang(nnLevelNotAllowed), 220, 250)
			return
		}
		g.sendSay(npc, g.lang(nnInitializedQuest), "Cavaleiro do Reino")
		g.teleportar(u.id, 1727, 1727)

	case typ >= 40 && typ <= 43: // training area quests
		g.npcTrainer(u, npc, typ-40)

	case typ >= 44 && typ <= 48: // Carbuncle buffs
		if evo != evoMortal {
			g.sendSay(npc, "%s", g.lang(nnOnlyMortal))
			return
		}
		if level >= 255 {
			g.sendSay(npc, "Você é forte! não precisa mais de ajuda.")
			return
		}
		if typ == 48 {
			for _, s := range []int{41, 43, 44, 45} {
				g.setAffectTime(u.id, s, 25)
			}
		} else {
			g.setAffectTime(u.id, []int{41, 43, 44, 45}[typ-44], 50)
		}
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.sendSay(npc, "Sente-se mais forte agora, %s?", name)

	case typ == 49: // Urnammu: Sub-Celestial class change
		g.npcUrnammu(u)

	case typ == 51: // Sacerdotisa Amelia
		slot := firstSlotOf(pl, 4125)
		if !levelIn(119, 124) || !mortalOnly() {
			return
		}
		if slot < 0 {
			g.clientMessage(u, "Onde esta o Pedaço do Equilíbrio?.")
			return
		}
		if firstEmptySlot(pl) < 0 && itemAmount(&pl.Inventory[slot]) > 1 {
			g.clientMessage(u, "%s", g.lang(nnCarryFull))
			return
		}
		amountMinus(&pl.Inventory[slot])
		g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
		g.putItem(u, &wire.Item{Index: 4126})
		g.clientMessage(u, "Recebeu sua recompensa.")

	case typ == 52: // Guarda Equilíbrio da Força
		if levelIn(119, 124) && mortalOnly() {
			g.teleportar(u.id, 1962, 1598)
		}

	case typ == 374: // kingdom broker: kingdom cape -> white
		g.npcKingdomBroker(u, npc)

	case typ == 53: // Sacerdote Jester
		if levelIn(69, 74) && mortalOnly() {
			g.teleportar(u.id, 2655, 1981)
		}

	case typ == 54: // Kruno
		if !levelIn(69, 74) || !mortalOnly() {
			return
		}
		slot := firstSlotOf(pl, 4123)
		if slot < 0 {
			g.clientMessage(u, "Onde esta o Pedaço de Chance?.")
			return
		}
		// Fixed: the C++ gave the reward without taking the piece, so it
		// could be clicked again and again.
		if firstEmptySlot(pl) < 0 && itemAmount(&pl.Inventory[slot]) > 1 {
			g.clientMessage(u, "%s", g.lang(nnCarryFull))
			return
		}
		amountMinus(&pl.Inventory[slot])
		g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
		g.putItem(u, &wire.Item{Index: 4124})
		g.clientMessage(u, "Recebeu sua recompensa.")

	case typ >= 55 && typ <= 58: // Unicórnio: +6 weapon swap, once
		k := typ - 55
		from := [4]uint16{917, 923, 918, 917}[k]
		to := [4]uint16{939, 943, 940, 941}[k]
		w := &pl.Equip[6]
		if w.Index != from {
			g.sendSay(npc, "Traga-me o %s para que eu possa recompensá-lo", g.data.Item(from).Name)
			return
		}
		if itemSanc(w) != 6 {
			g.sendSay(npc, "O item deve estar +6 para efetuar a troca")
			return
		}
		if m.info.Info&questUnicornio != 0 {
			g.sendSay(npc, "Só posso ajudá-lo uma vez :(")
			return
		}
		*w = wire.Item{Index: to}
		w.Effect[0] = wire.ItemEffect{Index: EF_SANC, Value: 3}
		g.sendItem(u, wire.SlotEquip, 6, w)
		g.sendSay(npc, "Agora sua jornada vai começar de verdade!")
		m.info.Info |= questUnicornio

	case typ == 59: // Oráculo: souls and the Pedra Eterna
		g.npcOracle(u, npc)

	case typ == 64: // BigCubo
		if g.oneAccountIn(u, 1292, 1475, 1392, 1525) {
			g.clientMessage(u, "Somente uma conta por computador")
			return
		}
		if evo >= evoCelestial || (evo <= evoArch && level >= 20) {
			g.clientMessage(u, "Somente mortais até nível 20")
			return
		}
		g.teleportar(u.id, 1340, 1507)
		g.clientMessage(u, "Bem vindo ao BigCubo")

	case typ == 130: // Mestre das Habilidades: stat reset
		g.npcSkillMaster(u, npc)

	case typ == 33:
		g.npcUxmal(u, npc, time.Now())

	case typ == 62:
		g.hitEventEntry(u)

	case typ == 60: // the C++ only records a list nothing reads
		g.sendSay(npc, "%s", g.lang(nnSorry))
	}
}

// npcTrainer is the training-area NPCs 40..43: one reward each, for
// characters up to level 41 with the quest key (451-453, 524).
func (g *Game) npcTrainer(u *User, npc, k int) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if pl.BStatus.Level > 40 && pl.BStatus.Level < 1000 {
		return
	}
	bit := uint16(1) << (6 + k)
	if pl.QuestInfo&bit != 0 {
		g.sendSay(npc, "%s", g.lang(nnNewbieQuestAlready1+k*4))
		return
	}
	key := uint16(451 + k)
	if key == 454 {
		key = 524
	}
	keySlot := -1
	for s := range 60 {
		if pl.Inventory[s].Index == key {
			keySlot = s
			break
		}
	}
	if keySlot < 0 {
		g.sendSay(npc, "%s", g.lang(nnNewbieQuestAlready1+k*4))
		return
	}
	g.sendSay(npc, "%s", g.lang(nnNewbieQuestComplete1+k*4))
	g.clientMessage(u, "%s", g.lang(nnNewbieQuestReward1+k*4))
	pl.QuestInfo |= bit
	potions := wire.Item{Index: 682}
	potions.Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 20}
	switch k {
	case 0:
		g.putItem(u, &potions)
	case 1: // re-roll a low weapon's bonuses, keeping its refine
		w := &pl.Equip[6]
		if w.Index == 0 || int(w.Index) >= MaxItemList {
			break
		}
		lv := int(g.data.Item(w.Index).Level)
		if lv > 32 && lv < 1000 {
			return
		}
		keep := sancEffect(w)
		if keep < 0 {
			keep = 0
		}
		saved := w.Effect[keep]
		w.Effect = [3]wire.ItemEffect{}
		g.setItemBonusLevel(w, lv+50, 1, 0)
		i := sancEffect(w)
		if i < 0 {
			for e := range 3 {
				if w.Effect[e].Index == 0 {
					i = e
					break
				}
			}
		}
		if i >= 0 {
			w.Effect[i] = saved
		}
		g.sendItem(u, wire.SlotEquip, 6, w)
	case 2: // +1..+4 on the armor (max +6) and 5 Replations
		for s := 1; s < 8; s++ {
			it := &pl.Equip[s]
			if it.Index < 500 || int(it.Index) > MaxItemList {
				continue
			}
			sanc := itemSanc(it)
			if sanc > 6 {
				continue
			}
			if s < 6 {
				sanc += rnd()%4 + 1
			} else {
				sanc += rnd()%3 + 1
			}
			setItemSanc(it, min(sanc, 6), 0)
			g.sendItem(u, wire.SlotEquip, s, it)
		}
		rep := wire.Item{Index: 4016}
		rep.Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 5}
		if !g.putItem(u, &rep) {
			g.clientMessage(u, "Sem espaço no inventario")
		}
	case 3:
		switch rnd() % 3 {
		case 0:
			g.putItem(u, &potions)
		case 1:
			g.putItem(u, &wire.Item{Index: 481})
		case 2:
			g.putItem(u, &wire.Item{Index: 652})
		}
	}
	g.setAffect(u.id, 44, 200, 200)
	g.sendAffect(u)
	g.refreshMob(u.id)
	g.getCurrentScore(u.id)
	g.sendScore(u.id)
	if k == 3 { // only the last key is taken
		pl.Inventory[keySlot] = wire.Item{}
		g.sendItem(u, wire.SlotInv, keySlot, &pl.Inventory[keySlot])
	}
}

// npcUrnammu (49): a Celestial wearing a sephirot changes the class of its
// Sub-Celestial for 2000 fame.
func (g *Game) npcUrnammu(u *User) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if pl.Equip[11].Index < 1760 || pl.Equip[11].Index > 1763 {
		return
	}
	if m.info.Sub.Status == 0 {
		g.clientMessage(u, "%s", g.lang(nnSorry))
		return
	}
	if evolution(pl) == evoSubCele {
		g.clientMessage(u, "É necessario estar no personagem celestial.")
		return
	}
	if m.info.Fame < 2000 {
		g.clientMessage(u, g.lang(nnNeedXXFame), 2000)
		return
	}
	if face := pl.Equip[0].Index; (face >= 22 && face <= 25) || face == 32 {
		g.clientMessage(u, "Não é possível trocar de classe transformado.")
		return
	}
	base := infoClass(int(pl.Equip[0].Index))
	if base == 4 {
		return
	}
	m.info.Fame -= 2000
	class := int(pl.Equip[11].Index) - 1760
	face := base*10 + 6 + class
	g.log.Info("Sub-Celestial class changed", "slot", u.id, "old_face", m.info.Sub.Equip[0].Index, "new_face", face)
	m.info.Sub.Equip[0].Index = uint16(face)
	m.info.Sub.Equip[0].Effect[1].Index = uint8(face)
	for i := range 4 { // memset(Sub.Affect, 0, 32): the first four
		m.info.Sub.Affect[i] = wire.Affect{}
	}
	pl.Equip[11] = wire.Item{}
	g.sendItem(u, wire.SlotEquip, 11, &pl.Equip[11])
	g.clientMessage(u, "%s", g.lang(nnGodContinueBlessing))
}

// npcKingdomBroker (374): 30 sapphires turn a kingdom cape white.
func (g *Game) npcKingdomBroker(u *User, npc int) {
	pl := &g.mobs[u.id].info.Player
	if pl.GuildIndex != 0 {
		g.clientMessage(u, "Membros de guild não podem alterar a capa")
		return
	}
	kingdom := int(pl.CapeInfo)
	if kingdom != capeBlue && kingdom != 8 {
		g.clientMessage(u, "Necessario ter um reino!")
		return
	}
	if c := pl.Equip[15].Index; c == 0 || c == 4006 {
		g.clientMessage(u, "Necessario ter um reino!")
		return
	}
	if sapphiresHeld(pl) < 30 {
		g.clientMessage(u, g.lang(nnNeedDSapphire), 30)
		return
	}
	g.takeSapphires(u, 30)
	row := kingdom - capeBlue
	for i, c := range capeIDs[row] {
		if pl.Equip[15].Index == c {
			pl.Equip[15].Index = capeIDs[2][i]
			g.sendItem(u, wire.SlotEquip, 15, &pl.Equip[15])
			break
		}
	}
	g.refreshMob(u.id)
	g.getCurrentScore(u.id)
	g.sendScore(u.id)
	g.sendSay(npc, "Troca realizada com sucesso.")
}

// npcOracle (59): up to ten Pedaços da Alma 4522 for a chance at a soul
// (1740/1741); or both souls side by side and 10 sapphires make the Pedra
// Eterna 1742.
func (g *Game) npcOracle(u *User, npc int) {
	pl := &g.mobs[u.id].info.Player
	slot := firstSlotOf(pl, 1740)
	if slot < 0 {
		pieces := inventoryAmount(pl, 4522)
		if pieces <= 0 {
			g.sendSay(npc, "Traga-me as duas almas para fazer a Pedra da Imortalidade")
			return
		}
		pieces = min(pieces, 10)
		if firstEmptySlot(pl) < 0 {
			// Fixed: the C++ took the pieces and lost the soul when the
			// inventory was full.
			g.sendSay(npc, "%s", g.lang(nnCarryFull))
			return
		}
		g.removeAmount(u, 4522, pieces)
		rate := [11]int{0, 5, 10, 15, 25, 30, 40, 50, 60, 75, 100}[pieces]
		name := wire.CString(pl.Name[:])
		if roll := rnd() % 100; roll <= rate {
			soul := uint16(1740)
			if rnd()%100 >= 50 {
				soul = 1741
			}
			g.putItem(u, &wire.Item{Index: soul})
			g.notice(name + " compôs com sucesso a " + g.data.Item(soul).Name + " usando Pedaço da Alma")
		} else {
			g.notice(name + " falhou na composição usando Pedaço da Alma")
		}
		return
	}
	if slot+1 >= 60 || pl.Inventory[slot+1].Index != 1741 {
		g.sendSay(npc, "Deixe as duas almas juntas para a composição.")
		return
	}
	if inventoryAmount(pl, itemSapphire) >= 10 {
		g.removeAmount(u, itemSapphire, 10)
	} else {
		pack := firstSlotOf(pl, itemSapphirePack)
		if pack < 0 {
			g.sendSay(npc, "É necessario 10 safiras para continuar.")
			return
		}
		amountMinus(&pl.Inventory[pack])
	}
	amountMinus(&pl.Inventory[slot])
	amountMinus(&pl.Inventory[slot+1])
	pl.Inventory[slot] = wire.Item{Index: 1742}
	g.sendCarry(u)
	g.sendSay(npc, "Pedra Eterna foi produzida. Equipe-a e procure seu rei.")
}

// npcSkillMaster (130): gives back up to 100 points of each stat (2000
// with a Pergaminho do Retorno 3336) for StatSapphire sapphires (none
// with the scroll). Naked characters only.
func (g *Game) npcSkillMaster(u *User, npc int) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	price := g.cfg.StatSapphire
	// MestreHab is never set by the C++, so this never triggers.
	if pl.QuestInfo&pqMestreHab != 0 && price < 5 {
		g.sendSay(npc, "%s", g.lang(nnYouveDoneItAlready))
		return
	}
	for s := 1; s < 8; s++ {
		if pl.Equip[s].Index != 0 {
			g.sendSay(npc, "%s", g.lang(nnCantWithArmor))
			return
		}
	}
	scroll := firstSlotOf(pl, 3336)
	if scroll < 0 {
		loose, packs := inventoryAmount(pl, itemSapphire), inventoryAmount(pl, itemSapphirePack)
		if loose < price {
			if loose+10*packs < price {
				g.clientMessage(u, g.lang(nnNeedDSapphire2), price)
				return
			}
			packs = min(packs, 3)
			g.removeAmount(u, itemSapphirePack, packs)
			price -= packs * 10
		}
		if price > 0 {
			g.removeAmount(u, itemSapphire, min(price, inventoryAmount(pl, itemSapphire)))
		}
	}
	cls := int(pl.ClassInfo)
	if cls > 3 {
		return
	}
	limit := 100
	if scroll >= 0 {
		limit = 2000
	}
	b := &pl.BStatus
	for i, st := range []*uint16{&b.STR, &b.INT, &b.DEX, &b.CON} {
		take := min(max(int(*st)-baseSIDCHM[cls][i], 0), limit)
		*st -= uint16(take)
	}
	if scroll >= 0 {
		amountMinus(&pl.Inventory[scroll])
		g.sendItem(u, wire.SlotInv, scroll, &pl.Inventory[scroll])
	}
	g.getCurrentScore(u.id)
	g.setAffect(u.id, 44, 20, 20)
	g.sendScore(u.id)
	g.sendEtc(u)
	g.refreshMob(u.id)
}
