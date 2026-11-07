package game

import (
	"fmt"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// PvP: PK mode (0x399), the guilty mark, arenas and war zones, PvP skill
// damage (GetSkillDamage_PvP), pet damage absorption and the player-kill
// rules of MobKilled (exp loss, CP and frags).
//
// The C++ RequestAttack never checks PK mode: the client only sends attacks
// on players in PK mode, and the server only checks the map (no damage in
// villages or outside PvP areas). PK mode itself blocks trading.

const OpCliPKMode = 0x399

// arenaAreas is g_pArenaArea (GetArena checks the first 4).
var arenaAreas = [4][4]int{
	{2052, 2171, 2052, 2163},
	{2432, 2675, 1672, 1767},
	{2448, 2476, 1966, 2024},
	{1070, 1110, 1130, 1158},
}

// arena is GetArena: 0..3, or 5 for none.
func arena(x, y int) int {
	for i, a := range arenaAreas {
		if x >= a[0] && x <= a[1] && y >= a[2] && y <= a[3] {
			return i
		}
	}
	return 5
}

// warArea returns the city whose war zone contains (x, y), or -1. The C++
// never tested the maximum Y (the zone ran to the bottom of the map); the
// full rectangle is checked here.
func (d *Data) warArea(x, y int) int {
	for i, c := range d.Cities {
		if int32(x) >= c.WarMinX && int32(x) <= c.WarMaxX && int32(y) >= c.WarMinY && int32(y) <= c.WarMaxY {
			return i
		}
	}
	return -1
}

// pkPoint is GetPKPoint (75 = neutral, CP = pkPoint - 75).
func pkPoint(pl *wire.Mob) int { return int(pl.Inventory[63].Effect[0].Index) }

// setPKPoint is SetPKPoint (1..150).
func setPKPoint(pl *wire.Mob, v int) { pl.Inventory[63].Effect[0].Index = uint8(min(max(v, 1), 150)) }

// setGuilty is SetGuilty (0..50).
func setGuilty(pl *wire.Mob, v int) { pl.Inventory[63].Effect[1].Index = uint8(min(max(v, 0), 50)) }

func curKill(pl *wire.Mob) int { return int(pl.Inventory[63].Effect[0].Value) }

func setCurKill(pl *wire.Mob, v int) { pl.Inventory[63].Effect[0].Value = uint8(min(max(v, 0), 200)) }

func totKill(pl *wire.Mob) int {
	return int(pl.Inventory[63].Effect[1].Value) | int(pl.Inventory[63].Effect[2].Value)<<8
}

func setTotKill(pl *wire.Mob, v int) {
	v = min(max(v, 0), 32767)
	pl.Inventory[63].Effect[1].Value = uint8(v % 256)
	pl.Inventory[63].Effect[2].Value = uint8(v >> 8)
}

// markGuilty sets the guilty mark (8) on an attacker outside arenas,
// villages and war zones; others see the new name color.
func (g *Game) markGuilty(id int) {
	m := &g.mobs[id]
	x, y := int(m.target.X), int(m.target.Y)
	if arena(x, y) != 5 || g.data.village(x, y) != 5 || g.data.warArea(x, y) != -1 {
		return
	}
	was := g.guilty(id)
	setGuilty(&m.info.Player, 8)
	if was == 0 {
		p := g.createMobPacket(id)
		g.multicast2(x, y, &p, 0)
	}
}

// requestPKMode is the 0x399 handler: toggle PK mode; it ends a trade.
func (g *Game) requestPKMode(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	u.pkMode = p.Value != 0
	if other := int(u.trade.ClientID); u.pkMode && other > 0 && other < MaxPlayer {
		if o := g.users[other]; o.status == userPlay && int(o.trade.ClientID) == u.id {
			g.clientMessage(u, "Não é possivel trocar com o modo PvP ativo")
			g.clientMessage(o, "O outro jogador esta com o modo PvP ativo")
			g.removeTrade(other)
			g.removeTrade(u.id)
		}
	}
}

// absorbByPet is AbsorveDamageByPet: a living mount takes part of the hit
// (the callers pass the difference to processAdultMount).
func absorbByPet(pl *wire.Mob, dam int) int {
	if !petAlive(pl) {
		return dam
	}
	perc := 25
	switch it := pl.Equip[14].Index; {
	case it == 2376:
		perc = 27
	case it == 2377:
		perc = 34
	case it == 2379, it == 2380:
		perc = 35
	case it >= 2381 && it <= 2383:
		perc = 29
	case it >= 2384 && it <= 2386:
		perc = 31
	case it == 2387, it == 2388:
		perc = 28
	}
	if lv := int(pl.Equip[14].Effect[1].Index); lv >= 120 {
		perc += lv - 120
	}
	return max(dam-dam*perc/100, 1)
}

// skillDamagePvP is GetSkillDamage_PvP (the PvP balance of this server).
func (g *Game) skillDamagePvP(skill, id, weather int) int {
	m := &g.mobs[id]
	mob := &m.info.Player
	sd := &g.data.Skills[skill]
	typ := sd.InstanceType
	kind := (skill%24)>>3 + 1
	level := min(max(int(mob.Status.Level)+map[bool]int{true: 400}[evolution(mob) >= evoCelestial], 0), 600)
	mastery := int(mob.Status.Mastery[kind])
	base, affectV := sd.InstanceValue, sd.AffectValue
	weapon := float64(m.combat.weaponDamage)
	intel, learn := float64(mob.Status.INT), mob.Learn[0]
	ret := 0
	switch {
	case typ == 0:
		switch skill {
		case 11:
			ret = mastery/10 + affectV
		case 13:
			ret = mastery*3>>2 + affectV
		case 41:
			ret = mastery/25 + 2
		case 43:
			ret = mastery/3 + affectV
		case 44:
			ret = (mastery*3/20 + affectV) << 1
		case 45:
			ret = mastery/10 + affectV
		}
	case typ >= 1 && typ <= 5:
		group := skill >> 3
		b := base + mastery + level>>1
		switch {
		case skill == 97:
			ret = level*15 + base
		case mob.ClassInfo == 0 && group == 1:
			ret = int(float64(b) + float64(mob.Status.DEX)*2 + weapon*2.3)
		case mob.ClassInfo == 0:
			ret = b + int(weapon) + int(intel)/2
			if skill == 7 || (skill >= 17 && skill <= 23 && learn&0x800000 != 0) {
				ret = b + int(weapon) + int(intel)/2*110/100
			}
			if skill == 7 {
				ret += ret * 10 / 100
			}
		case mob.ClassInfo == 1:
			ret = b + int(intel)
			if skill == 28 {
				ret += int(mob.Status.CurHP) * 10 / 100
			}
		case mob.ClassInfo == 2:
			ret = int(float64(b) + intel/0.7)
			if skill == 48 || skill == 50 || skill == 52 || (skill == 55 && learn&0x80 != 0) {
				ret = b + int(weapon) + int(intel)/2*108/100
			}
			ret += ret * 5 / 100
		case mob.ClassInfo == 3:
			ret = b + int(mob.Status.STR)*3 + int(weapon)*3
		}
		if evolution(mob) == evoArch {
			switch mob.ClassInfo {
			case 0, 1:
				ret = int((float64(b) + intel*4.3) * 0.3)
			case 2:
				ret = int(float64(b) + intel*0.9)
			}
		}
		if ev := evolution(mob); ev == evoCelestial || ev == evoSubCele {
			switch mob.ClassInfo {
			case 1:
				ret = int(float64(b) + intel/2.1)
			case 0:
				ret = int(float64(b) + intel/1.9)
			case 2:
				ret = int(float64(b) + intel/2.3)
			}
		}
		switch {
		case weather == 1 && (skill == 48 || skill == 17):
			ret = ret * 90 / 100
		case weather == 1 && skill == 33:
			ret = ret * 150 / 100
		case weather == 2 && typ == 3:
			ret = ret * 120 / 100
		}
		switch {
		case skill == 97:
			return ret
		case mob.ClassInfo != 0 || group != 1:
			if mob.ClassInfo == 3 {
				ret = ret * 5 / 4
			} else {
				ret = (m.combat.magicIncrement*2 + 100) * ret / 100
				ret += ret * 10 / 100
			}
		default:
			return ret * 5 / 4
		}
	case typ == 6:
		ret = mastery*3 + base
	case typ == 11:
		return base
	default:
		ret = m.combat.magicIncrement
	}
	if skill == 79 {
		ret = int(mob.Status.Attack) * 95 / 100
	}
	return ret
}

// pvpExpLoss is the MobKilled exp penalty for a player killed by a player.
// The C++ also has a "hold" (exp debt) branch that a leftover condition
// always zeroes, so it is not ported.
func (g *Game) pvpDeath(target, killer int) {
	dead, k := &g.mobs[target], &g.mobs[killer]
	dpl, kpl := &dead.info.Player, &k.info.Player
	du := g.player(target)
	ku := g.player(killer)
	level := int(dpl.BStatus.Level)
	ev := evolution(dpl)
	if level < 0 || level > 399 || ev < 0 || ev >= len(nextLevel) {
		return
	}
	x, y := int(dead.target.X), int(dead.target.Y)
	war := g.data.warArea(x, y)
	frag := (dpl.CapeInfo == 7 && kpl.CapeInfo == 8) || (dpl.CapeInfo == 8 && kpl.CapeInfo == 7)

	if arena(x, y) != 5 || g.data.village(x, y) != 5 || (x>>7 == 0 && y>>7 == 31) {
		g.clientMessage(du, "Você não pode perder experiancia nesta area")
		return
	}
	// Exp loss: a share of the current level's span.
	var loss int64
	if level+1 < 400 {
		span := nextLevel[ev][level+1] - nextLevel[ev][level]
		div := int64(20)
		for _, s := range []struct {
			lv  int
			div int64
		}{{30, 22}, {40, 25}, {50, 30}, {60, 35}, {70, 40}, {80, 45}, {90, 50}, {100, 55}, {150, 70}, {200, 85}, {250, 100}} {
			if level >= s.lv {
				div = s.div
			}
		}
		loss = min(max(span/div, 0), 30000)
	}
	switch pk := pkPoint(dpl); {
	case pk <= 0:
		loss *= 5
	case pk <= 25:
		loss *= 3
	}
	loss /= 6
	if frag {
		loss += loss * int64(curKill(dpl)) >> 2
	}
	loss = min(max(loss, 0), 150000)
	if (x >= 2447 && x <= 2545 && y >= 1851 && y <= 1924) || (x >= 1041 && x <= 1248 && y >= 1950 && y <= 2158) {
		loss = 0
	}
	// TODO(phase E): Kefra area while Kefra lives, castle war, level blocks.
	if level >= 20 || ev >= evoArch {
		dpl.Exp = max(dpl.Exp-loss, 0)
		g.clientMessage(du, "Você perdeu %d de experiancia", loss)
		for dpl.BStatus.Level > 0 && dpl.Exp < nextLevel[ev][dpl.BStatus.Level-1] {
			dpl.BStatus.Level--
		}
		g.sendEtc(du)
	} else {
		g.clientMessage(du, "Level abaixo de 20 não perde experiancia.")
	}

	// CP (PK points) and frag counters. TODO(phase E): the CP-free areas of
	// g_pPositionCP, tower war.
	if war != -1 {
		return
	}
	kpk, victimGuilty := pkPoint(kpl), g.guilty(target)
	if frag {
		setCurKill(kpl, curKill(kpl)+1)
		setTotKill(kpl, totKill(kpl)+1)
		if ku != nil {
			g.sendItem(ku, wire.SlotInv, 63, &kpl.Inventory[63])
		}
		if victimGuilty == 0 {
			delta := min(max(-(pkPoint(dpl)/20)*3, -3), 0)
			setPKPoint(kpl, kpk+delta)
			g.clientMessage(ku, "CP %d diminuiu em %d", kpk+delta-75, delta)
		}
		return
	}
	setCurKill(kpl, curKill(kpl)+1)
	setTotKill(kpl, totKill(kpl)+1)
	if ku != nil {
		g.sendItem(ku, wire.SlotInv, 63, &kpl.Inventory[63])
	}
	delta := min(max(-(pkPoint(dpl)/25)*3, -3), 0)
	if victimGuilty > 0 {
		delta = 0
	}
	if c := dpl.Equip[15].Index; c == 548 || c == 549 || kpl.Equip[15].Index == 548 || kpl.Equip[15].Index == 549 {
		delta *= 3
	}
	if x >= 2180 && x <= 2541 && y >= 3858 && y <= 4051 {
		delta = 0
	}
	setPKPoint(kpl, kpk+delta)
	if delta != 0 {
		g.clientMessage(ku, "%s", fmt.Sprintf("CP %d diminuiu em %d", kpk+delta-75, -delta))
	}
	p := g.createMobPacket(killer)
	g.multicast2(int(k.target.X), int(k.target.Y), &p, 0)
}

// markGuiltyAnywhere is the heal branch's SetGuilty(8): no area check.
func (g *Game) markGuiltyAnywhere(id int) {
	m := &g.mobs[id]
	was := g.guilty(id)
	setGuilty(&m.info.Player, 8)
	if was == 0 {
		p := g.createMobPacket(id)
		g.multicast2(int(m.target.X), int(m.target.Y), &p, 0)
	}
}
