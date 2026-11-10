package game

import (
	"time"

	"strings"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// enterWorld is CUser::SendCharToWorld (DB 0x417 -> client 0x114), without
// the optional event notifications of later phases.
func (g *Game) enterWorld(u *User, pkt protocol.Packet) {
	u.trade = emptyTrade()
	u.dropKills = nil
	u.partyInvite = 0
	u.pkMode = false
	g.clearQuestTime(u)
	u.captchaCode, u.captchaTime, u.captchaTicket = "", 0, 0
	g.mobs[u.id].npc = npcState{} // no party carried over from a previous session
	var p wire.CNFCharacterLogin
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	g.fixItemsOnLogin(u, p.Mob.Equip[:])
	g.fixItemsOnLogin(u, p.Mob.Inventory[:])
	for i := range p.Mob.Inventory {
		if id := p.Mob.Inventory[i].Index; id == 470 || id == 500 {
			p.Mob.Inventory[i].Index = 0
		}
	}

	u.tokenOK = true
	m := &g.mobs[u.id]
	m.info.Player = p.Mob
	m.mode = 2
	pl := &m.info.Player

	if pl.Equip[0].Effect[1].Value >= evoCelestial {
		pl.Learn[0] |= 0x40000000
	}
	if pl.Inventory[63].Index == 0 {
		// The PK/kill counter item; GetPKPoint reads EF1, so 75 = neutral.
		pl.Inventory[63] = wire.Item{Index: 547, Effect: [3]wire.ItemEffect{{Index: 75}, {Index: 76}, {Index: 77}}}
	}
	if pl.Status.CurHP <= 0 {
		pl.Status.CurHP = 2
	}
	if pl.ClassInfo > 3 {
		g.log.Warn("login with undefined class", "slot", u.id, "class", pl.ClassInfo)
		g.closeUser(u)
		return
	}
	u.charSlot = int(p.SlotIndex)
	u.account.CharSlot = p.SlotIndex
	u.movementPacket = wire.OpCliMoveStop
	u.movementTS = 0xE0A1ACA

	// Spawn point: where the character stood on its previous channel (a
	// channel change, if this channel lets it be there), else its city,
	// the guild area of a city its guild holds, the training area for low
	// mortals, or Armia.
	x, y := int(p.WorldPos.X), int(p.WorldPos.Y)
	if x == 0 && y == 0 || x < 128 || x >= gridSize || y < 128 || y >= gridSize || g.areaRule(pl, x, y) != 0 {
		city := int(pl.Info >> 6) // Info.CityID
		c := g.data.Cities[city]
		x, y = int(c.CityX)+rnd()%14, int(c.CityY)+rnd()%14
		if gi := int32(pl.GuildIndex); gi != 0 {
			for zone := range 5 {
				if gi == g.chargedGuilds[g.cfg.Channel-1][zone] {
					x, y = int(g.data.Cities[zone].AreaGuildX), int(g.data.Cities[zone].AreaGuildY)
					break
				}
			}
		}
		if pl.Equip[0].Effect[1].Value == evoMortal && int(pl.BStatus.Level) < g.cfg.NewbieZone {
			x, y = 2112, 2042
		}
	}
	if x < 128 || x >= gridSize || y < 128 || y >= gridSize {
		x, y = 2100+rnd()%14, 2100+rnd()%14
		pl.Info &^= 0xC0
	}
	if !g.emptyMobGrid(u.id, &x, &y) {
		g.log.Warn("no free cell to spawn", "slot", u.id, "x", x, "y", y)
		g.closeUser(u)
		return
	}
	m.target = wire.Position{X: uint16(x), Y: uint16(y)}
	m.last = m.target
	m.lastTime = g.now()
	u.status = userPlay
	m.info.FilterDrop.IsEnabled = false // SendCharToWorld: the filter starts off
	u.rouletteReward, u.rouletteEvol = wire.Item{}, [maxRouletteBox]int16{}
	u.honorMarket = false
	g.sendRankUpdate(u)
	m.lifes = 0
	g.getCurrentScore(u.id)
	u.potionHP, u.potionMP = pl.Status.CurHP, pl.Status.CurMP
	u.quaff, u.returnX, u.returnY = false, 0, 0

	g.guildKingdomCape(pl)
	openBags(pl) // the inventory always has its 60 slots (sent in p.Mob)
	unplaced := settleCarry(&pl.Inventory)
	u.guildInvite = guildInvite{}

	p.Header = wire.NewHeader(wire.OpSrvEnterWorld, wire.ClientIDCharList)
	p.WorldPos = m.target
	p.Mob = *pl
	p.ClientIndex = int16(u.id)
	g.queue(u, &p)

	m.spawnType = 2
	spawn := g.createMobPacket(u.id)
	m.spawnType = 0
	g.setCell(x, y, uint16(u.id))
	if !u.isBanned {
		g.multicast2(x, y, &spawn, 0)
	} else {
		g.queue(u, &spawn)
	}
	g.sendGridMob(u.id)
	g.sendScore(u.id)
	g.sendEtc(u)
	g.warLogin(u)
	g.loginExtras(u)
	g.mountProcess(u.id)
	g.block199(u)
	g.fixStoredBoots(u)
	g.parkUnplacedItems(u, unplaced)
	g.loadLock(u)
	g.sendLockStatus(u)
	g.log.Info("entered world", "slot", u.id, "account", wire.CString(u.account.Username[:]),
		"char", wire.GameString(pl.Name[:]), "x", x, "y", y)
}

// fixBoots caps the damage bonus of boots (30, or 42 for an arch item) on
// the first EF_DAMAGE effect, which becomes EF_DAMAGE2; it reports a change.
func (g *Game) fixBoots(u *User, it *wire.Item) bool {
	total := g.data.itemAbilityNoSanc(it, EF_DAMAGE) + g.data.itemAbilityNoSanc(it, EF_DAMAGE2)
	limit := 30
	if g.data.itemAbility(it, EF_MOBTYPE) == 1 {
		limit = 42
	}
	if total <= limit {
		return false
	}
	for i := range it.Effect {
		if it.Effect[i].Index == EF_DAMAGE {
			it.Effect[i] = wire.ItemEffect{Index: EF_DAMAGE2, Value: uint16(limit)}
			g.log.Info("boots fixed", "slot", u.id, "item", it.Index, "bonus", total)
			return true
		}
	}
	return false
}

// fixStoredBoots is the storage part of that clean-up.
func (g *Game) fixStoredBoots(u *User) {
	for i := range u.account.StorageItems {
		it := &u.account.StorageItems[i]
		if it.Index != 0 && int(it.Index) < MaxItemList && g.data.Item(it.Index).Pos == 32 && g.fixBoots(u, it) {
			g.sendItem(u, wire.SlotStorage, i, it)
		}
	}
}

// fixItemsOnLogin is the item clean-up at the top of SendCharToWorld.
func (g *Game) fixItemsOnLogin(u *User, items []wire.Item) {
	for i := range items {
		it := &items[i]
		if it.Index == 0 || it.Index >= MaxItemList {
			continue
		}
		pos := g.data.Item(it.Index).Pos
		if pos == 64 || pos == 192 { // weapons: DAMAGE2/DAMAGEADD -> DAMAGE
			for e := range 3 {
				if it.Effect[e].Index == EF_DAMAGE2 || it.Effect[e].Index == EF_DAMAGEADD {
					it.Effect[e].Index = EF_DAMAGE
				}
			}
		}
		if pos <= 32 { // armor: CRITICAL -> CRITICAL2 including the base value
			for e := range 3 {
				if it.Effect[e].Index == EF_CRITICAL {
					it.Effect[e].Index = EF_CRITICAL2
					it.Effect[e].Value += uint16(g.data.effectValueByIndex(it.Index, EF_CRITICAL))
				}
			}
		}
		// Costumes lose their expiry date effects.
		if costume := (it.Index >= 4151 && it.Index <= 4189) || (it.Index >= 4210 && it.Index <= 4241) || (it.Index >= 4301 && it.Index <= 4411); costume &&
			it.Effect[0].Index == 106 && it.Effect[1].Index == 110 && it.Effect[2].Index == 109 {
			it.Effect[0].Index, it.Effect[1].Index, it.Effect[2].Index = 0, 0, 0
		}
		if pos == 32 {
			g.fixBoots(u, it)
		}
	}
}

// requestAction is CUser::RequestAction (walk / stop / illusion).
func (g *Game) requestAction(u *User, pkt protocol.Packet) {
	m := &g.mobs[u.id]
	if u.status != userPlay || m.info.Player.Status.CurHP == 0 {
		g.sendHpMode(u)
		return
	}
	g.cancelTrade(u) // moving closes the trade window on both sides
	var p wire.Action
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	if g.pistaMoved(u, int(m.target.X), int(m.target.Y)) {
		return
	}
	if g.rvrWallBlocked(int(m.info.Player.CapeInfo), int(p.Destiny.X), int(p.Destiny.Y), int(m.target.Y)) {
		stop := g.action(u.id, int(m.target.X), int(m.target.Y))
		stop.Header.PacketID = wire.OpCliMoveStop
		stop.MoveSpeed = p.MoveSpeed
		g.queue(u, &stop)
		return
	}
	now := g.now()
	check := uint32(0)
	if now >= 120000 {
		check = now - 120000
	}
	ts := p.Header.TimeStamp
	m.motion = 0
	switch p.Header.PacketID {
	case wire.OpCliMoveIllus:
		pl := &m.info.Player
		if (pl.ClassInfo != 3 || pl.Learn[0]&2 == 0) && pl.BStatus.Level < 400 {
			g.addCrack(u, 10, "illusion without the skill")
			return
		}
		mana := int64(g.data.Skills[73].Mana)
		if pl.Status.CurMP < mana {
			g.sendSetHpMp(u)
			return
		}
		pl.Status.CurMP -= mana
		u.potionMP = max(u.potionMP-mana, 0)
		if ts > now+15000 || ts < check {
			g.log.Debug("illusion timestamp out of range", "slot", u.id, "ts", ts, "now", now)
			return
		}
	case wire.OpCliMove:
		if u.movementTS != 0xE0A1ACA && ts < u.movementTS+900 {
			g.log.Debug("movement faster than 900 ms", "slot", u.id)
			return
		}
		if ts > now+15000 || ts < check {
			g.log.Debug("movement timestamp out of range", "slot", u.id, "ts", ts, "now", now)
			return
		}
		u.movementTS = ts
	case wire.OpCliMoveStop:
		if u.movementPacket == wire.OpCliMoveStop {
			return // C++: "can't send MSG_Stop continuously"
		}
	}
	if speed := uint32(m.info.Player.Status.Move & 0x0F); p.MoveSpeed > speed {
		p.MoveSpeed = speed
	}

	tx, ty := int(m.target.X), int(m.target.Y)
	dx, dy := int(p.Destiny.X), int(p.Destiny.Y)
	if dx < tx-viewGrid || dx > tx+viewGrid || dy < ty-viewGrid || dy > ty+viewGrid {
		if dx < tx-2*viewGrid || dx > tx+2*viewGrid || dy < ty-2*viewGrid || dy > ty+2*viewGrid {
			sm := g.action(u.id, tx, ty)
			sm.Header.PacketID = wire.OpCliMoveIllus
			sm.MoveSpeed = 6
			g.queue(u, &sm)
			g.flush(u)
		}
		g.addCrack(u, 1, "moved outside the view")
		return
	}
	if dx <= 0 || dx >= gridSize || dy <= 0 || dy >= gridSize {
		return
	}
	if dx == tx && dy == ty {
		return
	}
	if g.areaDenied(u, dx, dy) {
		return
	}
	// An occupied destination moves the player to the nearest free cell
	// below. The C++ tried GetRoute first, but its loop tested the unchanged
	// destination, so only the "stay where you are" outcome ever happened.

	u.movementPacket = p.Header.PacketID
	if c := g.cell(dx, dy); c != 0 && c != uint16(u.id) {
		if !g.emptyMobGrid(u.id, &dx, &dy) {
			p.Destiny = m.target
			p.Command[0] = 0
			g.queue(u, &p) // the C++ sent sizeof(pointer) = 4 bytes here
			return
		}
		p.Destiny = wire.Position{X: uint16(dx), Y: uint16(dy)}
		g.queue(u, &p)
	}
	m.route = p.Command
	g.multicastMove(u.id, dx, dy, &p)
	if p.Header.PacketID == wire.OpCliMoveIllus {
		g.queue(u, &p)
		g.sendSetHpMp(u)
	}

	// Facing direction from the last route step, kept in Merchant's top bits.
	route := wire.CString(m.route[:])
	dir := uint8(0)
	if n := len(route); n > 0 && n < 24 {
		if d := route[n-1] - '0'; d >= 1 && d <= 9 {
			dir = d << 4
		}
	}
	st := &m.info.Player.Status
	st.Merchant |= dir & 0xF0
	m.info.Player.BStatus.Merchant = st.Merchant
}

// action is GetAction: a movement packet for mob id towards (x, y).
func (g *Game) action(id, x, y int) wire.Action {
	m := &g.mobs[id]
	return wire.Action{
		Header:    wire.NewHeader(wire.OpCliMove, uint16(id)),
		LastPos:   m.target,
		Destiny:   wire.Position{X: uint16(x), Y: uint16(y)},
		MoveSpeed: uint32(m.info.Player.Status.Move & 0x0F),
		Command:   m.route,
	}
}

// requestChat is CUser::RequestChat.
func (g *Game) requestChat(u *User, pkt protocol.Packet) {
	if u.status != userPlay {
		return
	}
	var p wire.Chat
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	p.Text[95] = 0
	m := &g.mobs[u.id]
	switch wire.CString(p.Text[:]) {
	case "guildoff":
		m.guildDisable = true
		g.sendScore(u.id)
		return
	case "guildon":
		m.guildDisable = false
		g.sendScore(u.id)
		return
	case "whisper":
		u.whisperOff = !u.whisperOff
		g.clientMessage(u, "Whisper : %s", onOff(u.whisperOff))
		return
	case "partychat":
		u.citizenChatOff = !u.citizenChatOff
		g.clientMessage(u, "Citizen Chatting : %s", onOff(u.citizenChatOff))
		return
	case "guildchat":
		u.guildChatOff = !u.guildChatOff
		g.clientMessage(u, "Guild Chatting : %s", onOff(u.guildChatOff))
		return
	case "kingdomchat":
		u.kingdomChatOff = !u.kingdomChatOff
		g.clientMessage(u, "Kingdom Chatting : %s", onOff(u.kingdomChatOff))
		return
	}
	if t := wire.CString(p.Text[:]); t == "guild" {
		g.sendGuildList(u)
		return
	} else if strings.HasPrefix(t, "guildtax") {
		g.guildTaxCommand(u, t)
		return
	}
	g.multicast2(int(m.target.X), int(m.target.Y), &p, u.id)
}

func onOff(off bool) string {
	if off {
		return "Off"
	}
	return "On"
}

// requestMobInfo is case 0x369: the client asks again about a mob it sees.
func (g *Game) requestMobInfo(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	id := int(p.Value)
	if id <= 0 || id >= MaxMob {
		return
	}
	if g.mobs[id].mode == 0 || (id < MaxPlayer && g.users[id].status != userPlay) || !g.inHalf(id, u.id) {
		g.sendRemoveMob(u, id, 0)
		return
	}
	g.sendCreateMob(u, id)
}

// slotItem returns the item a (type, slot) refers to, nil when invalid.
func (g *Game) slotItem(u *User, typ, slot int) *wire.Item {
	pl := &g.mobs[u.id].info.Player
	switch typ {
	case wire.SlotEquip:
		if slot > 0 && slot <= 19 {
			return &pl.Equip[slot]
		}
	case wire.SlotInv:
		if slot >= 0 && slot < 60 {
			return &pl.Inventory[slot]
		}
	case wire.SlotStorage:
		if slot >= 0 && slot < 160 {
			return &u.account.StorageItems[slot]
		}
	}
	return nil
}

// requestMoveItem is CUser::RequestMoveItem.
func (g *Game) requestMoveItem(u *User, pkt protocol.Packet) {
	m := &g.mobs[u.id]
	if u.status != userPlay || m.info.Player.Status.CurHP == 0 {
		g.sendHpMode(u)
		g.addCrack(u, 1, "move item while dead or not playing")
		return
	}
	g.cancelTrade(u)
	var p wire.MoveItem
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	src, dst := int(p.SrcType), int(p.DstType)
	ss, ds := int(p.SrcSlot), int(p.DstSlot)
	if (dst == wire.SlotStorage && src == wire.SlotEquip) || (dst == wire.SlotEquip && src == wire.SlotStorage) {
		return
	}
	if (dst == wire.SlotEquip && ds == 15) || (src == wire.SlotEquip && ss == 15) {
		g.addCrack(u, 2, "tried to move the cape")
		return
	}
	srcItem, dstItem := g.slotItem(u, src, ss), g.slotItem(u, dst, ds)
	if srcItem == nil || dstItem == nil {
		g.addCrack(u, 2, "invalid item move")
		return
	}
	pl := &m.info.Player
	if dst == wire.SlotEquip && ds == 18 && pl.Equip[14].Index == 0 {
		g.clientMessage(u, "Equipe sua montaria antes de equipar o traje de montaria!")
		return
	}
	if src == wire.SlotEquip && ss == 14 && pl.Equip[18].Index != 0 {
		g.clientMessage(u, "Retire seu traje de montaria antes de movimentar a montaria!")
		return
	}
	if src == wire.SlotEquip && ss == 18 && dst == wire.SlotInv && dstItem.Index != 0 && (dstItem.Index < costumeFirst || dstItem.Index > costumeLast) {
		return // the slot of the mount costume takes only costumes
	}
	if srcItem.Index != 0 && srcItem.Index == dstItem.Index && time.Since(u.lastStack) >= moveStackDelay {
		u.lastStack = time.Now()
		if agroupItem(srcItem, dstItem) {
			g.sendItem(u, src, ss, srcItem)
			g.sendItem(u, dst, ds, dstItem)
			return
		}
	} else if srcItem.Index != 0 && srcItem.Index == dstItem.Index {
		return
	}
	if srcItem.Index >= runeFirst && srcItem.Index <= runeLast && dstItem.Index == itemEmptyRune {
		g.energizeRune(u, src, ss, dst, ds, srcItem, dstItem)
		return
	}
	if srcItem.Index >= costumeFirst && srcItem.Index <= costumeLast && dst == wire.SlotEquip && ds == 18 && pl.Equip[14].Index != 0 {
		pl.Equip[14].Effect[2].Value = srcItem.Index - costumeFirst + 11
		g.sendItem(u, wire.SlotEquip, 14, &pl.Equip[14])
	}
	if g.crownMoveRefused(u, src, dst, srcItem, dstItem) {
		return
	}
	if dstItem.Index == 877 || srcItem.Index == 877 {
		return
	}
	if (src == wire.SlotEquip || dst == wire.SlotEquip) && g.locked(u, lockUnequip, "Desbloqueie seus itens para poder movimentá-los") {
		return
	}
	if (src == wire.SlotStorage || dst == wire.SlotStorage) && g.locked(u, lockCargo, "Há um bloqueio ativo nesta conta") {
		return
	}
	if (src == wire.SlotStorage && u.listedSlot(ss)) || (dst == wire.SlotStorage && u.listedSlot(ds)) {
		g.clientMessage(u, "Item do leilão não é possivel movimenta-lo")
		return
	}

	newSrc, newDst := *srcItem, *dstItem // what ends up at src / dst
	*srcItem, *dstItem = wire.Item{}, wire.Item{}
	ok := true
	if newDst.Index != 0 && src == wire.SlotEquip { // dst item goes into an equip slot
		ok = g.data.canEquip(&newDst, pl, ss)
	}
	if newSrc.Index != 0 && dst == wire.SlotEquip {
		ok = ok && g.data.canEquip(&newSrc, pl, ds)
	}
	if ok && (src == wire.SlotStorage || dst == wire.SlotStorage) && !g.storageAllowedHere(m.target) {
		ok = false
	}
	fits := !ok || carryMoveFits(pl, src, ss, dst, ds, &newSrc, &newDst)
	if !ok || !fits {
		*srcItem, *dstItem = newSrc, newDst
		if !fits {
			g.clientMessage(u, "Não há espaço para este item nesse local.")
			g.sendItem(u, src, ss, srcItem)
			g.sendItem(u, dst, ds, dstItem)
		}
		return
	}
	*srcItem, *dstItem = newDst, newSrc
	g.giveTime(u, src, ss, dst, ds, time.Now())
	if srcItem.Index <= 40 {
		*srcItem = wire.Item{}
	}
	if dstItem.Index <= 40 {
		*dstItem = wire.Item{}
	}
	g.queue(u, &p)

	equipChanged := src == wire.SlotEquip || dst == wire.SlotEquip
	if pl.Equip[6].Index == 0 && pl.Equip[7].Index != 0 && g.data.itemAbility(&pl.Equip[7], EF_POS) != 128 {
		// A weapon left alone in the off hand moves to the main hand.
		g.queue(u, &wire.MoveItem{Header: p.Header, DstType: wire.SlotEquip, DstSlot: 6, SrcType: wire.SlotEquip, SrcSlot: 7})
		pl.Equip[6], pl.Equip[7] = pl.Equip[7], wire.Item{}
	}
	if equipChanged {
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
		g.sendEquip(u)
	}
	// The C++ ran MountProcess after every move (re-summoning the mount
	// each time); only moves that touch the mount slot do here.
	if (src == wire.SlotEquip && ss == 14) || (dst == wire.SlotEquip && ds == 14) {
		g.mountProcess(u.id)
	}
}

// charLogout is CharLogOut (0x215): back to the character list.
func (g *Game) charLogout(u *User) {
	g.cancelTrade(u)
	g.arenaUnregister(u)
	g.zakumUnregister(u.id)
	g.pistaUnregister(u.id)
	if u.status == userPlay {
		g.removeParty(u.id)
	}
	if u.status != userPlay {
		g.signal(u, uint16(u.id), wire.OpSrvBackToList)
		return
	}
	g.signalParm(u, uint16(u.id), 0x99A, 0) // group finder windows closed
	g.signalParm(u, uint16(u.id), opGroupFail, 0)
	g.removeGroup(g.charName(u.id))
	u.follow = 0
	g.settleRouletteReward(u)
	u.account.Position = wire.Position{}
	u.account.CharSlot = -1
	g.saveUser(u, 1)
	g.closeStore(u)
	g.dismissSummons(u.id)
	g.deleteMob(u.id, 2)
	u.status = userSelChar
	g.mobs[u.id].mode = 0
	g.signal(u, uint16(u.id), wire.OpSrvBackToList)
}

// saveCharacter builds STRUCT_SAVECHARACTER like SaveUser / CloseUser.
func (g *Game) saveCharacter(u *User, op uint16, quit bool) wire.SaveCharacter {
	m := &g.mobs[u.id]
	a := &u.account
	p := wire.SaveCharacter{
		Header:   wire.NewHeader(op, uint16(u.id)),
		CharSlot: uint32(u.charSlot),
		Mob:      m.info,
		Storage:  a.StorageItems,
		Coin:     int64(a.StorageCoin),
		Cash:     int32(a.Cash), Honra: int32(a.Honra), Novato: int32(a.Novato),
		PassBlock:  a.PassBlock,
		BanType:    a.BanType,
		Ban:        a.Ban,
		Insignia:   int64(a.Insignias),
		Position:   m.target,
		Unique:     int64(a.Unique),
		Daily:      a.Daily,
		Water:      a.Water,
		Divina:     a.Divina,
		Sephira:    a.Sephira,
		SingleGift: a.SingleGift,
		Slot:       a.CharSlot,
	}
	copy(p.User[:], a.Username[:])
	copy(p.SkillBar[0:4], m.info.Player.SkillBar1[:])
	copy(p.SkillBar[4:], m.info.SkillBar[:])
	if quit {
		p.Friends = a.Friends
		p.Slot = -1
	}
	g.sendRankUpdate(u)
	return p
}

// saveUser is SaveUser (0x807).
func (g *Game) saveUser(u *User, arg2 uint32) {
	if u.charSlot < 0 || u.charSlot > 3 {
		return
	}
	p := g.saveCharacter(u, wire.OpSaveMob, false)
	p.Arg2 = arg2
	g.sendDB(&p)
}

// closeUser is CloseUser: disconnect, saving the character if playing.
// A playing slot stays reserved (USER_SAVING4QUIT) until DBSRV confirms
// the save with 0x40B.
func (g *Game) closeUser(u *User) {
	g.arenaUnregister(u)
	g.zakumUnregister(u.id)
	g.pistaUnregister(u.id)
	if u.status == userPlay {
		g.settleRouletteReward(u)
		g.cancelTrade(u)
		g.removeParty(u.id)
		g.closeStore(u)
		g.dismissSummons(u.id)
		g.clearGrid(u.id)
	}
	u.tokenOK = false
	if u.conn != nil {
		g.flush(u)
		u.conn.close()
		u.conn = nil
	}
	switch u.status {
	case userEmpty, userAccept:
		g.releaseSlot(u)
	case userPlay:
		if u.charSlot >= 0 && u.charSlot <= 3 {
			p := g.saveCharacter(u, wire.OpSaveMobQuit, true)
			g.sendDB(&p)
			u.status = userSaving4Quit
			g.deleteMob(u.id, 2)
			g.log.Info("disconnected while playing", "slot", u.id, "account", wire.CString(u.account.Username[:]))
			return
		}
		g.deleteMob(u.id, 2)
		g.sendDB(&wire.Signal{Header: wire.NewHeader(wire.OpNoNeedSave, uint16(u.id))})
		g.releaseSlot(u)
	case userSaving4Quit:
		// Waiting for 0x40B; nothing to do.
	default:
		g.sendDB(&wire.Signal{Header: wire.NewHeader(wire.OpNoNeedSave, uint16(u.id))})
		g.mobs[u.id].mode = 0
		g.releaseSlot(u)
	}
}

// releaseSlot is CloseUser_OL1: the slot becomes free.
func (g *Game) releaseSlot(u *User) {
	if u.conn != nil {
		g.flush(u)
		u.conn.close()
	}
	g.mobs[u.id] = Mob{}
	*u = User{id: u.id}
}
