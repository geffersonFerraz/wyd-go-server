package game

import (
	"slices"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// RequestMerchantNPC.cpp (0x28B): clicking a quest NPC. The NPC is picked
// by its Info (merchant byte), face and level; this file has the dispatch
// and the NPCs keyed by Info, merchant_quests.go the "type 100" NPCs keyed
// by their level.

const (
	opCliMerchantNPC = 0x28B
	opSrvKingdom     = 0x193 // pMsgSignal: CapeInfo

	nnGetWatchingTownMission = 3
	nnWatchingTownAwarded    = 5
	nnQest06Complete         = 6
	nnGather7Crystals        = 62
	nnEquipWeaponToEnchant   = 63
	nnNowIWillEnchant        = 65
	nnAllVillagersThank      = 66
	nnGiveYouSomeReward      = 67
	nnGuardThisVillage       = 68
	nnMonsterAttackingUs     = 69
	nnThanksForHelping       = 70
	nnNeed1MGold             = 152
	nnHurryHelping           = 153
	nnNeedPureDivine         = 155
	nnInitializeSkill        = 156
	nnNeed10Particle         = 157
	nnProcessingComplete     = 158
	nnNeed5MGold             = 166
	nnNeedDSapphire          = 177
	nnMyKingBless1           = 178
	nnMyKingBless2           = 179
	nnNeedLevel              = 180
	nnNeedMaster             = 188
	nnNeedDSapphire2         = 189
	nnNextChance             = 200
	nnCongratulations        = 201
	nnCantWithArmor          = 205
	nnZakumBy                = 222
	nnCurePrice              = 258
	nnCured                  = 259
	nnCureFailed             = 260
	nnNeed5Materials         = 291
	nnSnowimpSuccess         = 292
	nnSnowimpNeed2M          = 293
	nnSephirotCompose        = 307
	nnSephirotAreUSure       = 308
	nnSephirotEightStones    = 305
	nnSephirotNeed30M        = 306
	nnPlayerQuestEnter       = 315
	nnAlreadyALearner        = 316
	nnBringMeToLearner       = 317
	nnWelcomeLearner         = 318
	nnInitializedQuest       = 320
	nnBringMeSelado          = 323
	nnGiveMeItem             = 364
	nnReceivedMountPerzen    = 365
	nnWelcomeNipp            = 366
	nnGodContinueBlessing    = 375
	nnSorry                  = 377
	nnNewbieQuestAlready1    = 230
	nnNewbieQuestComplete1   = 232
	nnNewbieQuestReward1     = 233

	// Player.QuestInfo bits.
	pqMysticalGetQuest      = 1 << 0
	pqMysticalCanGetAmuleto = 1 << 1
	pqMysticalGetAmuleto    = 1 << 2
	pqReset50               = 1 << 3
	pqReset100              = 1 << 4
	pqMestreHab             = 1 << 5

	questUnicornio = 1 << 21 // CharInfo.Info

	itemSapphire     = 697
	itemSapphirePack = 4131 // 10 sapphires
)

// MerchantClick mirrors p28B (20 bytes).
type MerchantClick struct {
	Header wire.Header
	NPCID  uint32
	Click  uint32 // 0 = first click (ask), 1 = confirm
}

// capeIDs is g_pCapesID: blue, red and white versions of each cape.
var capeIDs = [3][10]uint16{
	{736, 1766, 1767, 1768, 543, 545, 734, 3191, 3194, 3197},
	{737, 1769, 1770, 1771, 544, 546, 735, 3192, 3195, 3198},
	{550, 572, 573, 574, 549, 548, 550, 3193, 3196, 3199},
}

// merchantKind maps the NPC's Info byte to the C++ LOCAL_526.
func merchantKind(info uint8) int {
	switch info {
	case 90:
		return 0
	case 4, 58:
		return 1
	case 10:
		return 2
	case 11:
		return 3
	case 12:
		return 4
	case 13:
		return 5
	case 14:
		return 6
	case 15:
		return 7
	case 20, 36:
		return 8
	case 19, 40:
		return 9
	case 41:
		return 10
	case 43:
		return 11
	case 26:
		return 58
	case 31:
		return 13
	case 30:
		return 14
	case 62:
		return 15
	case 8:
		return 100
	case 51:
		return 51
	}
	return -1
}

// requestMerchantNPC is CUser::RequestMerchantNPC (0x28B).
func (g *Game) requestMerchantNPC(u *User, pkt protocol.Packet) {
	var p MerchantClick
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	npc := int(p.NPCID)
	if npc < MaxPlayer || npc >= MaxMob || g.mobs[npc].mode == 0 {
		return
	}
	nm, m := &g.mobs[npc], &g.mobs[u.id]
	if distance(int(nm.target.X), int(nm.target.Y), int(m.target.X), int(m.target.Y)) > halfGrid {
		g.log.Warn("NPC clicked out of range", "slot", u.id, "npc", npc)
		return
	}
	pl, np := &m.info.Player, &nm.info.Player
	if pl.Status.CurHP <= 0 {
		return
	}
	info, face, level := np.Info, int(np.Equip[0].Index), int(np.BStatus.Level)
	kind := merchantKind(info)
	click := p.Click
	g.log.Info("NPC clicked", "slot", u.id, "npc", wire.CString(np.Name[:]), "face", face, "info", info, "level", level)

	switch {
	case level >= 700 && level <= 750:
		// NPCs configured by Quests\*.c. TODO(phase E): there are no quest
		// files in this server (only Quests/README.txt), so the C++ ran
		// these with empty conditions and gave nothing.
		g.sendEtc(u)
		g.sendScore(u.id)
		return
	case int(np.Status.Level) >= 500 && int(np.Status.Level) < 500+maxNPCEvent:
		g.npcEvent(u, npc)
		return
	case level == 63:
		g.sendSay(npc, "%s", g.arenaRegister(u, time.Now()))
		return
	}

	switch {
	case info == 4 && face == 271:
		g.npcGodGovernment(u, npc)
	case kind == 58:
		g.npcMountMaster(u, click)
	case info == 10 && face == 51:
		g.npcKibita(u)
	case kind == 14:
		g.npcZakum(u)
	case kind == 15:
		g.npcGriupan(u, npc)
	case kind == 1:
		g.npcEnchantCrystals(u, npc)
	case kind == 2:
		g.npcMysticalQuest(u, npc, click)
	case kind == 3:
		g.npcMysticalReward(u, npc)
	case kind == 4:
		g.npcJeffi(u, npc)
	case kind == 5:
		g.npcShama(u, npc)
	case kind == 7:
		g.npcKing(u, npc, click)
	case kind == 8:
		g.npcGuildMedal(u)
	case kind == 9:
		g.npcSephirot(u, npc, click)
	case kind == 100:
		g.npcQuest(u, npc, click)
	}
}

// npcEvent is NPCEvento: trade the required items (and gold) for one prize
// tier picked by Rand()%300 against the tier rates.
//
// Fixed: the C++ took the items and then lost the prize when the inventory
// was full ("caiu no chão" but nothing was created); now it asks for room
// first.
func (g *Game) npcEvent(u *User, npc int) {
	idx := int(g.mobs[npc].info.Player.Status.Level) - 500
	if idx < 0 || idx >= maxNPCEvent {
		return
	}
	ev := &g.data.NPCEvents[idx]
	pl := &g.mobs[u.id].info.Player
	if int(pl.Gold) < ev.GoldRequired {
		g.sendSay(npc, "São necessários %d de gold", ev.GoldRequired)
		return
	}
	// eventOf is the event id (effect 41) a required item asks for, -1 if any.
	eventOf := func(it *wire.Item) int {
		for e := range 3 {
			if it.Effect[e].Index == 41 {
				return int(it.Effect[e].Value)
			}
		}
		return -1
	}
	matches := func(have *wire.Item, want *wire.Item, evID int) bool {
		if have.Index != want.Index {
			return false
		}
		if evID == -1 {
			return true
		}
		for e := range 3 {
			if have.Effect[e].Index == 41 && int(have.Effect[e].Value) == evID {
				return true
			}
		}
		return false
	}
	for i := range ev.ItemRequired {
		want := &ev.ItemRequired[i]
		if want.Index == 0 || int(want.Index) >= MaxItemList {
			continue
		}
		need := ev.AmountRequired[i]
		evID := eventOf(want)
		have := 0
		for s := range invSlots {
			if matches(&pl.Inventory[s], want, evID) {
				have += itemAmount(&pl.Inventory[s])
			}
		}
		if have < need {
			g.sendSay(npc, "Traga-me [%02d] %s", need, g.itemName(want.Index))
			return
		}
	}
	roll := rnd() % 300
	tier := -1
	for i, r := range ev.Rates {
		if r >= roll {
			tier = i
			break
		}
	}
	if tier >= 0 {
		prizes := 0
		for _, it := range ev.ItemEarned[tier] {
			if it.Index > 0 && int(it.Index) < MaxItemList && it.Index != 3214 {
				prizes++
			}
		}
		if freeSlotCount(pl) < prizes {
			g.clientMessage(u, "!Falta espaço no inventario")
			return
		}
	}
	for i := range ev.ItemRequired {
		want := &ev.ItemRequired[i]
		if want.Index == 0 || int(want.Index) >= MaxItemList {
			continue
		}
		need, evID := ev.AmountRequired[i], eventOf(want)
		for s := 0; s < invSlots && need > 0; s++ {
			if !matches(&pl.Inventory[s], want, evID) {
				continue
			}
			for pl.Inventory[s].Index == want.Index && need > 0 {
				amountMinus(&pl.Inventory[s])
				need--
			}
			g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
		}
	}
	pl.Gold = max(pl.Gold-int64(ev.GoldRequired), 0)
	g.signalParm64(u, uint16(u.id), wire.OpSrvGold, pl.Gold)

	if tier >= 0 {
		for _, it := range ev.ItemEarned[tier] {
			if it.Index == 0 || int(it.Index) >= MaxItemList {
				continue
			}
			if it.Index == 3214 {
				pl.Gold = min(pl.Gold+500_000, g.cfg.Limits.MaxGold)
				g.signalParm64(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
				g.clientMessage(u, "Chegou 500.000 de gold")
				continue
			}
			g.putItem(u, &it)
			g.log.Info("NPC event prize", "slot", u.id, "npc", idx, "item", it.Index, "effects", it.Effect, "roll", roll, "rate", ev.Rates[tier])
			g.clientMessage(u, "Chegou um item: [%s]", g.itemName(it.Index))
		}
		if ev.Msg[tier] != "" {
			g.clientMessage(u, "%s", ev.Msg[tier])
		}
		if t := ev.Teleport[tier]; t[0] != 0 && t[1] != 0 {
			g.teleportar(u.id, t[0], t[1])
		}
	}
	roman := [...]string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IV", "X"}
	if ev.Name != "" {
		g.sendSay(npc, "%s", ev.Name)
	} else {
		g.sendSay(npc, "NPC %s", roman[idx])
	}
}

// npcGodGovernment (Info 4, face 271): every 100 frags become 10 fame
// (5 guild fame) and 10 CP.
func (g *Game) npcGodGovernment(u *User, npc int) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	const need = 100
	total := totKill(pl)
	if total < need {
		g.sendSay(npc, g.lang(nnNeedXXFame), need)
		return
	}
	loops := total / need
	guild := int(pl.GuildIndex)
	m.info.Fame += uint32(10 * loops)
	if guild != 0 {
		if gd := g.guilds[guild]; gd != nil {
			g.sendDB(&wire.AddGuild{Header: wire.NewHeader(wire.OpAddGuild, 0), Type: 0, Value: int32(gd.Fame + 5*loops), GuildIndex: int32(guild)})
		}
	}
	setTotKill(pl, total-loops*need)
	setPKPoint(pl, pkPoint(pl)+10*loops)
	g.refreshMob(u.id)
	g.getCurrentScore(u.id)
	g.sendScore(u.id)
	g.log.Info("frags traded for fame", "slot", u.id, "frags", total, "loops", loops, "guild", guild)
}

// npcMountMaster (Info 26): revive a dead mount for its price; it may lose
// a level above 120 (unless a Protetor Abençoado 3251 is used).
func (g *Game) npcMountMaster(u *User, click uint32) {
	pl := &g.mobs[u.id].info.Player
	mount := &pl.Equip[14]
	if mount.Index < 2330 || mount.Index >= 2390 {
		g.clientMessage(u, "Você não possui uma montaria")
		return
	}
	if hp := int16(uint16(mount.Effect[0].Index) | uint16(mount.Effect[0].Value)<<8); hp > 0 {
		g.clientMessage(u, "Sua montaria não esta morta")
		return
	}
	price := int64(g.data.Item(mount.Index).Price)
	if click == 0 {
		g.clientMessage(u, g.lang(nnCurePrice), g.itemName(mount.Index), price)
		return
	}
	if pl.Gold < price {
		g.clientMessage(u, "%s", g.lang(nnHaventMoneySoMuch))
		return
	}
	if price < 0 || price > 100_000_000 {
		return
	}
	pl.Gold -= price
	life := g.data.itemAbility(mount, EF_MOUNTLIFE) - rnd()%4
	if life <= 0 {
		g.log.Info("mount revive: died", "slot", u.id, "mount", mount.Index, "effects", mount.Effect)
		*mount = wire.Item{}
		g.clientMessage(u, "%s", g.lang(nnCureFailed))
	} else {
		if mount.Effect[1].Index > 120 {
			protector := firstSlotOf(pl, 3251)
			if protector < 0 && rnd()%100 <= 25 {
				mount.Effect[1].Index--
			}
			if protector >= 0 {
				amountMinus(&pl.Inventory[protector])
				g.sendItem(u, wire.SlotInv, protector, &pl.Inventory[protector])
			}
		}
		g.clientMessage(u, "%s", g.lang(nnCured))
		mount.Effect[1].Value = uint16(life)
		mount.Effect[0] = wire.ItemEffect{Index: 20, Value: 0} // 20 HP
		mount.Effect[2].Index = 5
	}
	g.getCurrentScore(u.id)
	g.sendItem(u, wire.SlotEquip, 14, mount)
	g.mountProcess(u.id)
	g.sendEtc(u)
}

// npcKibita (Info 10, face 51): the weekday 21h training buff for mortals
// (item 420), and the Arch soul cape (secret stone 5334-5337 at 370+).
func (g *Game) npcKibita(u *User) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	now := time.Now()
	if wd := now.Weekday(); wd != time.Saturday && wd != time.Sunday && now.Hour() == 21 && evolution(pl) == evoMortal && pl.Status.Level <= 299 {
		if slot := firstSlotOf(pl, 420); slot >= 0 {
			i := slices.IndexFunc(m.info.Affects[:], func(a wire.Affect) bool { return a.Index == 29 })
			if i < 0 {
				i = slices.IndexFunc(m.info.Affects[:], func(a wire.Affect) bool { return a.Index == 0 })
			}
			if i >= 0 {
				m.info.Affects[i] = wire.Affect{Index: 29, Value: 99, Time: 450}
				g.getCurrentScore(u.id)
				g.sendScore(u.id)
				g.sendAffect(u)
				amountMinus(&pl.Inventory[slot])
				g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
				g.teleportar(u.id, 2462, 1843)
				return
			}
		}
	}
	if pl.BStatus.Level >= 369 && evolution(pl) == evoMortal {
		stone := [4]uint16{5334, 5336, 5335, 5337}[pl.ClassInfo&3]
		slot, cape := firstSlotOf(pl, stone), pl.Equip[15].Index
		if slot >= 0 && cape != 574 && cape != 1769 && cape != 1768 {
			pl.Inventory[slot] = wire.Item{}
			pl.Equip[15] = wire.Item{}
			switch pl.CapeInfo {
			case capeBlue:
				pl.Equip[15].Index = 1768
			case 8:
				pl.Equip[15].Index = 1769
			case 0: // CAPE_WHITE
				pl.Equip[15].Index = 574
			}
			g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
			g.sendItem(u, wire.SlotEquip, 15, &pl.Equip[15])
			pl.Learn[0] |= 1 << 30
			g.log.Info("Kibita soul cape", "slot", u.id, "cape", pl.Equip[15].Index)
			charSlot := u.charSlot
			g.charLogout(u)
			g.clientMessage(u, "%s", g.lang(nnGodContinueBlessing))
			g.signalParm(u, uint16(u.id), opSrvOutSealSlot, int32(charSlot))
			return
		}
	}
	g.clientMessage(u, "%s", g.lang(nnSorry))
}

// randomAmuletEffects fills effects 1 and 2 as the Griupan and the Mystical
// amulet rolled them.
func randomAmuletEffects(it *wire.Item) {
	for e := 1; e <= 2; e++ {
		switch rnd() & 7 {
		case 0:
			it.Effect[e] = wire.ItemEffect{Index: EF_HP, Value: uint16(rnd()%41 + 20)}
		case 1:
			it.Effect[e] = wire.ItemEffect{Index: EF_DAMAGE, Value: uint16(rnd()%21 + 5)}
		case 2:
			it.Effect[e] = wire.ItemEffect{Index: EF_ATTSPEED, Value: uint16(rnd()%11 + 5)}
		case 3:
			it.Effect[e] = wire.ItemEffect{Index: EF_MP, Value: uint16(rnd()%51 + 20)}
		case 4:
			it.Effect[e] = wire.ItemEffect{Index: EF_MAGIC, Value: uint16(rnd()%7 + 2)}
		case 5:
			it.Effect[e] = wire.ItemEffect{Index: EF_STR, Value: uint16(rnd()&15 + 5)}
		case 6:
			it.Effect[e] = wire.ItemEffect{Index: EF_INT, Value: uint16(rnd()&15 + 5)}
		case 7:
			it.Effect[e] = wire.ItemEffect{Index: EF_DEX, Value: uint16(rnd()&15 + 5)}
		}
	}
}

// npcGriupan (Info 62, Dragão de Arzan): 18 snow-imp materials 1721-1725
// (ten 1721, two 1722, four 1723, one 1724, one 1725) and 2M gold for a
// Griupan 1726.
func (g *Game) npcGriupan(u *User, npc int) {
	pl := &g.mobs[u.id].info.Player
	if pl.Gold < 2_000_000 {
		g.sendSay(npc, "%s", g.lang(nnSnowimpNeed2M))
		return
	}
	want := [18]int{4, 3, 2, 2, 2, 2, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	var slots [18]int
	for i := range slots {
		slots[i] = -1
	}
	for s := range invSlots {
		idx := int(pl.Inventory[s].Index)
		if idx < 1721 || idx > 1725 {
			continue
		}
		for k := range slots {
			if slots[k] == -1 && want[k] == idx-1721 {
				slots[k] = s
				break
			}
		}
	}
	if slices.Contains(slots[:], -1) {
		g.sendSay(npc, "%s", g.lang(nnNeed5Materials))
		return
	}
	for _, s := range slots {
		pl.Inventory[s] = wire.Item{}
		g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
	}
	pl.Gold -= 2_000_000
	g.sendEtc(u)
	it := wire.Item{Index: 1726}
	it.Effect[0].Index = EF_SANC
	setItemSanc(&it, rnd()&3, 0)
	randomAmuletEffects(&it)
	if !g.putItem(u, &it) {
		g.createGroundItem(int(g.mobs[u.id].target.X), int(g.mobs[u.id].target.Y), it, rnd()&3, 1)
		return
	}
	g.sendSay(npc, "%s", g.lang(nnSnowimpSuccess))
	g.log.Info("Griupan made", "slot", u.id)
}

// npcEnchantCrystals (Info 4/58): the seven crystals 421-427 re-roll the
// weapon's bonuses (up to +6). The C++ first tried sServer.Treasure
// exchanges, a table nothing ever filled.
func (g *Game) npcEnchantCrystals(u *User, npc int) {
	pl := &g.mobs[u.id].info.Player
	var have [7]bool
	count := 0
	for s := range invSlots {
		if idx := pl.Inventory[s].Index; idx >= 421 && idx <= 427 {
			have[idx-421] = true
			count++
		}
	}
	if slices.Contains(have[:], false) {
		g.sendSay(npc, "%s", g.lang(nnGather7Crystals))
		return
	}
	w := &pl.Equip[6]
	if w.Index == 0 {
		g.sendSay(npc, "%s", g.lang(nnEquipWeaponToEnchant))
		return
	}
	if itemSanc(w) > 6 {
		g.sendSay(npc, "Permitido somente para armas até +6")
		return
	}
	w.Effect = [3]wire.ItemEffect{}
	g.setItemBonusLevel(w, int(g.data.Item(w.Index).Level)+count/10*25, 1, g.mobs[u.id].combat.dropBonus)
	for s := range invSlots {
		if idx := pl.Inventory[s].Index; idx >= 421 && idx <= 427 {
			pl.Inventory[s] = wire.Item{}
		}
	}
	g.sendCarry(u)
	g.sendItem(u, wire.SlotEquip, 6, w)
	g.sendSay(npc, g.lang(nnNowIWillEnchant), wire.CString(pl.Name[:]))
	g.setAffect(u.id, 44, 200, 200)
	g.sendAffect(u)
	g.refreshMob(u.id)
	if pl.Equip[0].Index/10 == 0 {
		g.sendEmotion(u.id, 0x17, 0)
	} else if pl.Equip[0].Index/10 <= 3 {
		g.sendEmotion(u.id, 0x15, 0)
	}
}

// npcMysticalQuest (Info 10): takes the Mystical amulet quest (level 60+).
func (g *Game) npcMysticalQuest(u *User, npc int, click uint32) {
	pl := &g.mobs[u.id].info.Player
	if pl.QuestInfo&pqMysticalGetQuest == 0 && pl.BStatus.Level >= 59 {
		if click == 0 {
			g.sendSay(npc, "%s", g.lang(nnMonsterAttackingUs))
			return
		}
		pl.QuestInfo |= pqMysticalGetQuest
		g.sendSay(npc, "%s", g.lang(nnGetWatchingTownMission))
		return
	}
	g.sendSay(npc, "%s", g.lang(nnHurryHelping))
}

// npcMysticalReward (Info 11): the amulet for the Mystical quest.
func (g *Game) npcMysticalReward(u *User, npc int) {
	pl := &g.mobs[u.id].info.Player
	name := wire.CString(pl.Name[:])
	if pl.QuestInfo&pqMysticalGetQuest != 0 && pl.QuestInfo&pqMysticalCanGetAmuleto == 0 {
		g.sendSay(npc, g.lang(nnAllVillagersThank), name)
		return
	}
	if pl.QuestInfo&pqMysticalGetQuest != 0 && pl.QuestInfo&pqMysticalGetAmuleto == 0 && pl.BStatus.Level >= 59 {
		base := 559
		switch lv := pl.BStatus.Level; {
		case lv < 50:
			base = 551
		case lv < 80:
			base = 555
		}
		it := wire.Item{Index: uint16(base + rnd()&3)}
		it.Effect[0].Index = EF_SANC
		randomAmuletEffects(&it)
		slot := firstCarrySlot(&pl.Inventory, it.Index)
		if slot < 0 {
			g.sendSay(npc, "%s", g.lang(nnCarryFull))
			return
		}
		pl.Inventory[slot] = it
		g.sendItem(u, wire.SlotInv, slot, &it)
		g.sendSay(npc, "%s", g.lang(nnGiveYouSomeReward))
		pl.QuestInfo |= pqMysticalGetAmuleto
		g.clientMessage(u, "%s", g.lang(nnWatchingTownAwarded))
	}
	if pl.QuestInfo&pqMysticalGetQuest == 0 {
		g.sendSay(npc, "%s", g.lang(nnGuardThisVillage))
		return
	}
	g.sendSay(npc, "%s", g.lang(nnThanksForHelping))
}

// npcJeffi (Info 12): ten Ori/Lac remains become dusts (1M gold each); or
// the Circle (447 / 692) becomes a Divine Circle.
func (g *Game) npcJeffi(u *User, npc int) {
	pl := &g.mobs[u.id].info.Player
	if c := pl.Equip[13].Index; c != 447 && c != 692 {
		ori, lac := inventoryAmount(pl, 419), inventoryAmount(pl, 420)
		if ori < 10 && lac < 10 {
			g.sendSay(npc, "%s", g.lang(nnNeed10Particle))
			return
		}
		cost := int64((ori/10 + lac/10) * 1_000_000)
		if pl.Gold < cost {
			g.sendSay(npc, "Você precisa de %dkk de ouro para compor todas as unidades disponíveis.", cost/1_000_000)
			return
		}
		for ori >= 10 || lac >= 10 {
			if ori >= 10 {
				g.combine(u, 419, 412)
				ori -= 10
			}
			if lac >= 10 {
				g.combine(u, 420, 413)
				lac -= 10
			}
		}
		pl.Gold -= cost
		g.setAffect(u.id, 44, 20, 20)
		g.sendScore(u.id)
		g.sendCarry(u)
		g.sendEtc(u)
		g.sendAffect(u)
		return
	}
	cost, base, msg := int64(1_000_000), uint16(448), nnNeed1MGold
	if pl.Equip[13].Index == 692 {
		cost, base, msg = 5_000_000, 693, nnNeed5MGold
	}
	if pl.Gold < cost {
		g.sendSay(npc, "%s", g.lang(msg))
		return
	}
	pl.Gold -= cost
	pl.Equip[13].Index = base + uint16(rnd()%3)
	g.sendItem(u, wire.SlotEquip, 13, &pl.Equip[13])
	g.sendEtc(u)
	g.sendSay(npc, "%s", g.lang(nnProcessingComplete))
	g.setAffect(u.id, 44, 20, 20)
	g.sendScore(u.id)
	g.sendAffect(u)
}

// npcShama (Info 13): a Divine Circle resets one skill tree (and up to 50
// / 100 mastery points of each tree). The 448-450 reset is once only.
func (g *Game) npcShama(u *User, npc int) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	c := int(pl.Equip[13].Index)
	var tree, kind int
	switch {
	case c >= 448 && c <= 450:
		if pl.QuestInfo&pqReset50 != 0 {
			g.sendSay(npc, "%s", g.lang(nnYouveDoneItAlready))
			return
		}
		tree, kind = c-448, 1
	case c >= 693 && c <= 695:
		tree, kind = c-693, 2
	default:
		g.sendSay(npc, "%s", g.lang(nnNeedPureDivine))
		return
	}
	limit := uint16(50)
	if kind == 2 {
		limit = 100
	}
	for i := range 4 {
		pl.BStatus.Mastery[i] -= min(pl.BStatus.Mastery[i], limit)
	}
	for i := tree * 8; i < tree*8+8; i++ {
		pl.Learn[0] &^= 1 << i
	}
	g.getCurrentScore(u.id)
	pl.Equip[13] = wire.Item{}
	g.sendItem(u, wire.SlotEquip, 13, &pl.Equip[13])
	g.sendEtc(u)
	g.sendSay(npc, "%s", g.lang(nnInitializeSkill))
	g.setAffect(u.id, 44, 20, 20)
	g.sendScore(u.id)
	g.sendAffect(u)
	if kind == 1 {
		pl.QuestInfo |= pqReset50
	} else {
		pl.QuestInfo |= pqReset100
	}
	pl.SkillBar1 = [4]uint8{255, 255, 255, 255}
	for i := range m.info.SkillBar {
		m.info.SkillBar[i] = 255
	}
	g.queue(u, &wire.SkillBar{Header: wire.NewHeader(opCliSkillBar, uint16(u.id)), SkillBar1: pl.SkillBar1, SkillBar2: m.info.SkillBar})
	g.clientMessage(u, "%s", g.lang(nnQest06Complete))
}

// sapphiresHeld counts loose sapphires plus packs of ten.
func sapphiresHeld(pl *wire.Mob) int {
	return inventoryAmount(pl, itemSapphire) + inventoryAmount(pl, itemSapphirePack)*10
}

// takeSapphires takes `need` sapphires, loose ones and packs (a pack
// counts 10, whatever is left of it), slot by slot as the C++ did.
func (g *Game) takeSapphires(u *User, need int) {
	pl := &g.mobs[u.id].info.Player
	for s := 0; s < invSlots && need > 0; s++ {
		if pl.Inventory[s].Index == itemSapphire {
			for pl.Inventory[s].Index == itemSapphire && need > 0 {
				amountMinus(&pl.Inventory[s])
				need--
			}
			g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
		}
		// Fixed: the C++ took one pack per slot, so a stack of packs paid
		// for only 10 and the rest of the price was never taken.
		if need > 0 && pl.Inventory[s].Index == itemSapphirePack {
			for pl.Inventory[s].Index == itemSapphirePack && need > 0 {
				amountMinus(&pl.Inventory[s])
				need -= 10
			}
			g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
		}
	}
}

// npcKing (Info 15, the kingdom kings): kingdom capes for sapphires, the
// emblem 4081 cape, Arch creation (sephirot + Pedra Eterna at 300) and the
// Pedra Ideal (Arch+).
func (g *Game) npcKing(u *User, npc int, click uint32) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	np := &g.mobs[npc].info.Player
	kingdom, mine := int(np.CapeInfo), int(pl.CapeInfo)
	level := int(pl.BStatus.Level)
	cape := pl.Equip[15].Index
	if cape == 543 || cape == 545 {
		mine = 7
	}
	if cape == 544 || cape == 546 {
		mine = 8
	}
	capeKind := 0
	switch cape {
	case 543, 544, 1768, 1769, 574, 3191, 3192, 3193, 3197, 3198, 3199:
		capeKind = 2
	case 545, 546, 549:
		capeKind = 1
	}
	if mine == 0 && cape != 0 && cape != 4081 && cape != 4006 { // CAPE_WHITE is 0
		capeKind = 3
	}
	// Arch creation goes first: a mortal at 300 wearing the Pedra Eterna and a
	// sephirot becomes Arch with any cape (or none). The C++ only tried it
	// with a kingdom cape on, so a character without cape got the cape
	// purchase answers ("guild of another kingdom", sapphires) instead.
	if evolution(pl) == evoMortal && pl.Status.Level >= 299 && pl.Equip[10].Index == 1742 &&
		pl.Equip[11].Index >= 1760 && pl.Equip[11].Index <= 1763 {
		g.createArch(u)
		return
	}
	if mine != 0 && mine != kingdom {
		// The C++ said nothing here. A guild leader trying to take the
		// guild to this kingdom needs to know the cape is in the way.
		if guild := int(pl.GuildIndex); guild != 0 && pl.GuildMemberType == 9 {
			if gd := g.guilds[guild]; gd != nil && gd.Kingdom != kingdom {
				g.sendSay(npc, "Tire a capa do outro reino para mudar sua guild de reino")
			}
		}
		return
	}
	if g.sapphire == 0 {
		g.sapphire = 4
	}
	price := int(g.sapphire)
	if kingdom == 8 {
		switch g.sapphire {
		case 1, 2:
			price = 20
		case 4:
			price = 16
		case 16:
			price = 4
		case 20:
			price = 1 // the C++ set 2 and then 1
		}
	}

	if pl.Equip[13].Index == 4081 && pl.Status.Level >= 219 && pl.Status.Level < 250 && pl.CapeInfo == 0 {
		pl.Equip[13] = wire.Item{}
		newCape := uint16(0)
		switch np.Equip[0].Index {
		case 303:
			newCape = 545
		case 304:
			newCape = 546
		}
		if newCape != 0 {
			pl.Equip[15] = wire.Item{Index: newCape}
			g.sendItem(u, wire.SlotEquip, 15, &pl.Equip[15])
			g.clientMessage(u, "Bem vindo, novo aventureiro")
		}
		g.sendItem(u, wire.SlotEquip, 13, &pl.Equip[13])
		return
	}

	if capeKind == 2 && evolution(pl) == evoMortal && pl.Status.Level >= 299 {
		g.createArch(u)
		return
	}

	if capeKind == 2 && evolution(pl) >= evoArch && pl.Equip[10].Index == 1742 && pl.Equip[11].Index >= 1760 && pl.Equip[11].Index <= 1763 {
		if g.data.itemAbility(&pl.Equip[10], EF_NOTRADE) != 0 || g.data.itemAbility(&pl.Equip[11], EF_NOTRADE) != 0 {
			g.clientMessage(u, "%s", g.lang(nnMyKingBless1))
			return
		}
		var secret [4]int
		for i := range secret {
			secret[i] = firstSlotOf(pl, uint16(5334+i))
		}
		if slices.Contains(secret[:], -1) {
			g.clientMessage(u, "Necessario as 4 Pedras Secretas")
			return
		}
		for _, s := range secret {
			pl.Inventory[s] = wire.Item{}
			g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
		}
		pl.Equip[10], pl.Equip[11] = wire.Item{}, wire.Item{}
		g.sendItem(u, wire.SlotEquip, 10, &pl.Equip[10])
		g.sendItem(u, wire.SlotEquip, 11, &pl.Equip[11])
		pl.Inventory[secret[0]].Index = 5338
		g.sendItem(u, wire.SlotInv, secret[0], &pl.Inventory[secret[0]])
		g.notice(wire.CString(pl.Name[:]) + " compôs com sucesso a Pedra Ideal")
		g.clientMessage(u, "%s", g.lang(nnMyKingBless1))
		return
	}

	if click == 0 {
		g.clientMessage(u, g.lang(nnNeedDSapphire), price)
		return
	}
	bless := nnMyKingBless2
	if kingdom == capeBlue {
		bless = nnMyKingBless1
	}
	if capeKind == 2 {
		g.clientMessage(u, "%s", g.lang(bless))
		return
	}
	if level < 219 && capeKind == 0 {
		g.clientMessage(u, "%s", g.lang(nnNeedLevel))
		return
	}
	// A guild member joins the guild's kingdom. The guild leader may take
	// the guild to this kingdom (the guild changes with them), and a
	// neutral guild (no kingdom yet) does not hold anyone back; the C++
	// refused both.
	moveGuild := 0
	if guild := int(pl.GuildIndex); guild != 0 && pl.CapeInfo == 0 {
		if gd := g.guilds[guild]; gd != nil && gd.Kingdom != kingdom {
			switch {
			case pl.GuildMemberType == 9:
				moveGuild = guild
			case gd.Kingdom != capeBlue && gd.Kingdom != capeRed:
			default:
				g.sendSay(npc, "Você não pode ser recrutado aqui porque sua guild é de outro reino")
				return
			}
		}
	}
	if capeKind == 1 && level < 255 {
		g.clientMessage(u, "%s", g.lang(nnNeedLevel))
		return
	}
	if sapphiresHeld(pl) < price {
		g.clientMessage(u, g.lang(nnNeedDSapphire), price)
		return
	}
	// The sapphire price doubles or halves on every channel (DBSRV
	// rebroadcasts 0x80E).
	blue := int32(0)
	if kingdom == capeBlue {
		blue = 1
	}
	g.sendDB(&wire.SignalValue{Header: wire.NewHeader(wire.OpUpdateSapphire, 0), Value: blue})
	g.takeSapphires(u, price)
	g.log.Info("sapphires for a cape", "slot", u.id, "sapphires", price)
	switch capeKind {
	case 1:
		pl.Equip[15].Index = 544
		if kingdom == capeBlue {
			pl.Equip[15].Index = 543
		}
	case 3:
		i := slices.Index(capeIDs[2][:], cape)
		if i < 0 {
			g.log.Error("white cape not in the cape table", "slot", u.id, "cape", cape)
			g.clientMessage(u, "Contate a administração")
			return
		}
		pl.Equip[15].Index = capeIDs[1][i]
		if kingdom == capeBlue {
			pl.Equip[15].Index = capeIDs[0][i]
		}
	default:
		pl.Equip[15].Index = 546
		if kingdom == capeBlue {
			pl.Equip[15].Index = 545
		}
	}
	g.sendItem(u, wire.SlotEquip, 15, &pl.Equip[15])
	g.clientMessage(u, "%s", g.lang(bless))
	g.getCurrentScore(u.id)
	g.refreshMob(u.id)
	if moveGuild != 0 {
		g.log.Info("guild changed kingdom", "slot", u.id, "guild", moveGuild, "kingdom", kingdom)
		g.sendDB(&wire.AddGuild{Header: wire.NewHeader(wire.OpAddGuild, 0), Type: guildInfoKingdom, Value: int32(kingdom), GuildIndex: int32(moveGuild)})
		g.endAlliances(moveGuild)
		g.clientMessage(u, "Sua guild agora pertence a este reino.")
	}
}

// createArch is the Arch creation of the kingdom kings: a mortal at 300
// wearing the Pedra Eterna 1742 and a sephirot 1760-1763 gets an Arch in a
// free character slot (DBSRV 0x80B) and enters it.
func (g *Game) createArch(u *User) {
	pl := &g.mobs[u.id].info.Player
	if pl.Equip[10].Index != 1742 || pl.Equip[11].Index < 1760 || pl.Equip[11].Index > 1763 {
		return
	}
	face := int(pl.Equip[0].Index)
	if (face >= 22 && face <= 25) || face == 32 {
		face = 21
	}
	seph := int(pl.Equip[11].Index) - 1760
	switch face {
	case 1, 11, 21, 31:
		face += 5 + seph
	default:
		g.clientMessage(u, "Contate a administração!")
		return
	}
	if face == int(pl.Equip[0].Index) {
		g.clientMessage(u, "Contate a administração!")
		return
	}
	slot := -1
	for i := range u.charList.Name {
		if u.charList.Name[i][0] == 0 {
			slot = i
			break
		}
	}
	if slot < 0 {
		g.clientMessage(u, "Você não possui espaço para criação do personagem arch")
		return
	}
	name := wire.CString(pl.Name[:])
	g.log.Info("Arch created", "slot", u.id, "name", name, "sephirot", seph, "face", face, "mortal_slot", u.charSlot, "arch_slot", slot)
	g.notice("Parabens " + name + " pela criação do seu personagem Arch")
	p := wire.NewArch{Header: wire.NewHeader(wire.OpNewArch, uint16(u.id)), PosID: int32(slot), ClassID: int32(face), ClassInfo: int32(seph), MortalSlot: int32(u.charSlot)}
	copy(p.Name[:12], pl.Name[:12])
	g.sendDB(&p)
	pl.Equip[10], pl.Equip[11] = wire.Item{}, wire.Item{}
	g.charLogout(u)
	g.signalParm(u, uint16(u.id), opSrvOutSealSlot, int32(slot))
}

// npcGuildMedal (Info 20/36): the guild medal upgrade.
//
// Fixed: the C++ checked the medal and the guild master, took 10
// sapphires and then did nothing (the upgrade was commented out). The
// checks are kept; nothing is taken.
func (g *Game) npcGuildMedal(u *User) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	medal := pl.Equip[12].Index
	if medal != 508 && (medal < 526 || medal > 528) && (medal < 532 || medal > 534) {
		g.clientMessage(u, "%s", g.lang(nnNeedGuildMedal))
		return
	}
	leader := m.npc.leader
	lu := g.player(leader)
	if lu == nil || lu.status != userPlay || g.mobs[leader].info.Player.GuildIndex != pl.GuildIndex || g.mobs[leader].info.Player.GuildMemberType == 0 {
		g.clientMessage(u, "%s", g.lang(nnNeedMaster))
		return
	}
	if u.trade.ClientID != 0 {
		g.removeTrade(u.id)
		return
	}
	g.clientMessage(u, "%s", g.lang(nnSorry))
}

// npcSephirot (Info 19/40): eight stones 1744-1751 and 30M gold, wearing
// the Pedra Eterna, make the sephirot of this NPC (its Exp is 0..3).
func (g *Game) npcSephirot(u *User, npc int, click uint32) {
	if click != 1 {
		g.sendSay(npc, "%s", g.lang(nnSephirotAreUSure))
		return
	}
	pl := &g.mobs[u.id].info.Player
	if pl.Equip[10].Index != 1742 {
		return
	}
	var stones [8]int
	for i := range stones {
		if stones[i] = firstSlotOf(pl, uint16(1744+i)); stones[i] < 0 {
			g.sendSay(npc, "%s", g.lang(nnSephirotEightStones))
			return
		}
	}
	if pl.Gold < 30_000_000 {
		g.sendSay(npc, "%s", g.lang(nnSephirotNeed30M))
		return
	}
	class := g.mobs[npc].info.Player.Exp
	if class < 0 || class > 3 {
		return // the C++ trusted the NPC file
	}
	pl.Gold -= 30_000_000
	pl.Inventory[stones[0]] = wire.Item{Index: uint16(1760 + class)}
	for _, s := range stones[1:] {
		pl.Inventory[s] = wire.Item{}
		g.sendItem(u, wire.SlotInv, s, &pl.Inventory[s])
	}
	g.sendItem(u, wire.SlotInv, stones[0], &pl.Inventory[stones[0]])
	g.signalParm64(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
	g.sendSay(npc, "%s", g.lang(nnSephirotCompose))
}
