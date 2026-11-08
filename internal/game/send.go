package game

import (
	"fmt"

	"dbsrv/internal/wire"
)

// Senders from SendFunc.cpp / Basedef.cpp.

func (g *Game) clientMessage(u *User, format string, args ...any) {
	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}
	p := wire.Message128{Header: wire.NewHeader(wire.OpSrvMessage, 0)}
	wire.PutGameString(p.Msg[:], msg)
	g.queue(u, &p)
}

// signal is SendSignal (header-only packet).
func (g *Game) signal(u *User, clientID, op uint16) {
	g.queue(u, &wire.Signal{Header: wire.NewHeader(op, clientID)})
}

// signalParm is SendSignalParm (pMsgSignal).
func (g *Game) signalParm(u *User, clientID, op uint16, value int32) {
	g.queue(u, &wire.SignalValue{Header: wire.NewHeader(op, clientID), Value: value})
}

// createMobPacket is GetCreateMob for a player (index < 1000).
func (g *Game) createMobPacket(id int) wire.CreateMob {
	m := &g.mobs[id]
	pl := &m.info.Player
	p := wire.CreateMob{Header: wire.NewHeader(wire.OpSrvCreateMob, wire.ClientIDSystem)}
	p.Status = pl.Status

	npc := isNPC(id)
	if npc {
		// p364_Mob: Name[16] over Name + the kill counters, trailing
		// spaces/underscores trimmed.
		name := pl.Name
		n := 0
		for n < 16 && name[n] != 0 {
			n++
		}
		if n >= 16 {
			n = 15
		}
		for i := n; i > 0; i-- {
			if name[i] != ' ' && name[i] != '_' && name[i] != 0 {
				break
			}
			name[i] = 0
		}
		name[n] = 0
		copy(p.Name[:], name[:12])
		p.ChaosPoints, p.CurrentKill = name[12], name[13]
		p.TotalKill = uint16(name[14]) | uint16(name[15])<<8
		if pl.CapeInfo == 4 {
			p.Status.Defense = 0
		} else {
			p.Status.Defense = 1
		}
	} else {
		// Players: Name[12] (names of 12+ chars spill into the kill
		// counters, which are written right after, as in the C++).
		copy(p.Name[:], pl.Name[:12])
	}
	p.SpawnType = uint16(m.spawnType)
	if m.motion != 0 {
		p.SpawnType |= uint16(m.motion << 4)
	}
	p.Index = int16(id)
	p.CurrentX, p.CurrentY = int16(m.target.X), int16(m.target.Y)

	usingCostume := pl.Equip[12].Index != 0
	for i := range 20 {
		item := pl.Equip[i]
		if i == 14 && item.Index >= 2360 && item.Index <= 2390 && (item.Effect[0].Index == 0 && item.Effect[0].Value == 0) {
			continue
		}
		if i == 0 && pl.Equip[12].Index != 0 && pl.ClassInfo == 2 {
			item.Index = uint16(item.Effect[1].Index)
		}
		p.ItemRefine[i] = itemIDAndEffect(&item, i, usingCostume)
		p.AnctCode[i] = anctCode(&item, usingCostume)
	}
	for i, a := range m.info.Affects {
		if a.Index == 0 || a.Time == 0 {
			continue
		}
		p.Affect[i] = wire.AffectBrief{Index: a.Index, Time: uint8(a.Time)}
	}
	p.GuildIndex = pl.GuildIndex
	p.GuildMemberType = pl.GuildMemberType
	if m.guildDisable {
		p.GuildIndex, p.GuildMemberType = 0, 0
	}
	switch {
	case pl.GuildMemberType == 9:
		p.SpawnType |= 0x80
	case pl.GuildMemberType >= 6:
		p.SpawnType |= 0x40
	}

	if !npc {
		inv63 := &pl.Inventory[63]
		p.ChaosPoints = inv63.Effect[0].Index // GetPKPoint
		p.CurrentKill = inv63.Effect[0].Value // GetCurKill
		p.TotalKill = uint16(inv63.Effect[1].Value) | uint16(inv63.Effect[2].Value)<<8
		if g.guilty(id) > 0 {
			p.ChaosPoints = 0
		}
		if pl.Info&1 != 0 { // Info.Merchant & 1
			p.Status.Merchant = p.Status.Merchant&^0x0F | 1
		}
		p.Life = m.lifes
	}

	evo := pl.Equip[0].Effect[1].Value
	if (pl.Status.Level >= 69 && evo == evoMortal) || evo >= evoArch {
		p.Tab = m.tab
	}
	if tag, ok := g.arenaTag(id); ok {
		p.GuildIndex, p.GuildMemberType = 0, 0
		p.ItemRefine[15], p.AnctCode[15] = 0, 0
		p.Tab = [26]byte{}
		wire.PutGameString(p.Tab[:], tag)
	}
	// Arena of the guild war (2604x1708 - 2648x1744) hides guild and cape.
	if m.target.X >= 2604 && m.target.Y >= 1708 && m.target.X <= 2648 && m.target.Y <= 1744 {
		p.GuildIndex, p.GuildMemberType = 0, 0
		p.ItemRefine[15], p.AnctCode[15] = 0, 0
	}
	return p
}

// guilty is GetGuilty (Inventory[63].EF2, reset above 50).
func (g *Game) guilty(id int) int {
	it := &g.mobs[id].info.Player.Inventory[63]
	v := int(it.Effect[1].Index)
	if v > 50 {
		it.Effect[1].Index = 0
		v = 0
	}
	return v
}

// sendCreateMob is SendCreateMob(to, who).
func (g *Game) sendCreateMob(to *User, who int) {
	if to == nil || to.id <= 0 || to.id >= MaxPlayer || to.status != userPlay {
		return
	}
	if who > 0 && who < MaxPlayer && g.users[who].isBanned {
		return
	}
	if who >= MaxPlayer && who < MaxMob && g.mobs[who].storeOwner != 0 {
		sm := g.storeMobPacket(who) // an auction store shows as 0x363
		g.queue(to, &sm)
		return
	}
	p := g.createMobPacket(who)
	g.queue(to, &p)
}

// sendRemoveMob is SendRemoveMob(to, who, type).
func (g *Game) sendRemoveMob(to *User, who int, kind int32) {
	if to == nil {
		return
	}
	g.queue(to, &wire.SignalValue{Header: wire.NewHeader(wire.OpSrvRemoveMob, uint16(who)), Value: kind})
}

func (g *Game) sendItem(u *User, slotType, slot int, item *wire.Item) {
	g.queue(u, &wire.ItemUpdate{Header: wire.NewHeader(wire.OpSrvItem, uint16(u.id)),
		InvType: int16(slotType), InvSlot: int16(slot), Item: *item})
}

// sendScore is SendScore: multicast to the view grid, self included.
func (g *Game) sendScore(id int) {
	m := &g.mobs[id]
	pl := &m.info.Player
	p := wire.UpdateScore{Header: wire.NewHeader(wire.OpSrvScore, uint16(id))}
	p.ReqHP, p.ReqMP = pl.Status.CurHP, pl.Status.CurMP
	p.Guild = pl.GuildIndex
	p.GuildLevel = uint16(uint8(pl.GuildMemberType))
	if m.guildDisable {
		p.Guild = 0
	}
	p.Magician = uint16(m.info.MagicIncrement)
	p.Rsv = uint16(m.info.AffectInfo)
	p.SaveMana = int8(pl.RegenMP)
	p.Critical = int8(pl.Critical)
	for i := range 4 {
		p.Resist[i] = int8(pl.Resist[i])
	}
	p.Score = pl.Status
	for i, a := range m.info.Affects {
		t := min(a.Time, 255)
		p.Affect[i] = uint16(a.Index)<<8 | uint16(t&0xFF)
	}
	if pl.Info&1 != 0 {
		p.Score.Merchant = p.Score.Merchant&^0x0F | 1
	}
	g.multicast2(int(m.target.X), int(m.target.Y), &p, 0)
	// The C++ SendScore ended with SendAffect: the client counts the buff
	// timers down from the 0x3B9 list, so without it an expired buff stays
	// on screen with a negative time.
	if u := g.player(id); u != nil {
		g.sendAffect(u)
	}
}

// sendEtc is SendEtc: gold, exp, learned skills, points.
func (g *Game) sendEtc(u *User) {
	if u.status != userPlay {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	g.queue(u, &wire.Etc{
		Header:       wire.NewHeader(wire.OpSrvEtc, uint16(u.id)),
		Hold:         int32(m.info.Hold),
		Exp:          pl.Exp,
		LearnedSkill: pl.Learn,
		ScoreBonus:   int16(pl.StatusPoint),
		SpecialBonus: int16(pl.MasterPoint),
		SkillBonus:   int16(pl.SkillPoint),
		Coin:         pl.Gold,
	})
}

// sendEquip is SendEquip: the equipment others see.
func (g *Game) sendEquip(u *User) {
	if u.status != userPlay {
		return
	}
	pl := &g.mobs[u.id].info.Player
	p := wire.Equipment{Header: wire.NewHeader(wire.OpSrvEquip, uint16(u.id))}
	usingCostume := pl.Equip[12].Index != 0
	for i := range 20 {
		item := pl.Equip[i]
		if i == 14 && item.Index >= 2360 && item.Index <= 2389 && item.Effect[0].Index == 0 && item.Effect[0].Value == 0 {
			continue
		}
		if i == 0 && pl.Equip[12].Index != 0 && pl.ClassInfo == 2 {
			item.Index = uint16(item.Effect[1].Index)
		}
		p.ItemEff[i] = itemIDAndEffect(&item, i, usingCostume)
		p.AnctCode[i] = anctCode(&item, usingCostume)
	}
	g.multicast2(int(g.mobs[u.id].target.X), int(g.mobs[u.id].target.Y), &p, 0)
}
