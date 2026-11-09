package game

import (
	"dbsrv/internal/wire"
)

// Celestial and Sub-Celestial progression: their score/skill points
// (CMob.cpp GetScorePoint/GetSkillPoint), the three Sub-Celestial resets
// and the level 199/209 blocks (RequestUseItem.cpp item 3020, CMob.cpp
// CheckGetLevel, SendCharToWorld.cpp), the Pedra Ideal (volatile 211:
// Arch -> Celestial, Celestial -> Sub-Celestial, level 200 unlock) and the
// Sub-Celestial Cythera bonuses at 120/150/180/190; plus mount hunger and
// egg incubation of RegenMob.

// More UNION_QUESTINFO bits (CharInfo.Info / Sub.Info).
const (
	questLevel355  = 1 << 0 // the Arch level the Celestial was made at
	questLevel370  = 1 << 1
	questLevel380  = 1 << 2
	questLevel398  = 1 << 3
	questLevel399  = 1 << 4
	questLevelBit  = questLevel355 | questLevel370 | questLevel380 | questLevel398 | questLevel399
	questAdd120    = 1 << 10
	questAdd151    = 1 << 11
	questAdd180    = 1 << 12
	questAdd199    = 1 << 13
	questReset1    = 1 << 14
	questReset2    = 1 << 15
	questReset3    = 1 << 16
	questUnlock210 = 1 << 47

	itemSeloReal   = 4683
	itemPedraIdeal = 211 // EF_VOLATILE
	nnIncuProceed  = 262
	nnMountDied    = 264
	nnCantTeleNipp = 367 // _NN_CantTele_Nip ("Level inadequado")
)

// totalResets is STRUCT_CHARINFO::GetTotalResets.
func totalResets(info uint64) int {
	n := 0
	for _, b := range []uint64{questReset1, questReset2, questReset3} {
		if info&b != 0 {
			n++
		}
	}
	return n
}

// celestialScorePoints is the Celestial / Sub-Celestial branch of
// GetScorePoint. For a Sub-Celestial the C++ also copied the Celestial's
// creation-level and Arch-crystal bits from Sub.Info into Info.
func (g *Game) celestialScorePoints(id int) int {
	m := &g.mobs[id]
	pl := &m.info.Player
	level := int(pl.BStatus.Level)
	levelSub := -1
	switch evolution(pl) {
	case evoCelestial:
		if m.info.Sub.Status == 1 {
			levelSub = int(m.info.Sub.SubStatus.Level)
		}
	case evoSubCele:
		levelSub, level = level, int(m.info.Sub.SubStatus.Level)
		const copied = questLevelBit | questElime | questSylphed | questThelion | questNoas
		m.info.Info = m.info.Info&^copied | m.info.Sub.Info&copied
	}
	resets := 0
	if levelSub != -1 {
		resets = totalResets(m.info.Info | m.info.Sub.Info)
	}
	info := m.info.Info
	pnts := 1200
	switch {
	case info&questLevel355 != 0:
		pnts = 100
	case info&questLevel370 != 0:
		pnts = 300
	case info&questLevel380 != 0:
		pnts = 600
	case info&questLevel398 != 0:
		pnts = 900
	}
	pnts += resets * 200
	for _, b := range []uint64{questElime, questSylphed, questNoas, questThelion} {
		if info&b != 0 {
			pnts += 100
		}
	}
	pnts += 1001 + level*6
	if level >= 150 {
		pnts += (level - 149) * 4
	}
	if levelSub != -1 {
		pnts += levelSub * 9
		if levelSub >= 150 {
			pnts += (levelSub - 149) * 4
		}
	}
	return pnts
}

// celestialSkillPoints is the Celestial / Sub-Celestial branch of
// GetSkillPoint (before the Pílula and the learned skills).
func celestialSkillPoints(info uint64, level int) int {
	pnts := 1600
	switch {
	case info&questLevel355 != 0:
		pnts = 600
	case info&questLevel370 != 0:
		pnts = 800
	case info&questLevel380 != 0:
		pnts = 1000
	case info&questLevel398 != 0:
		pnts = 1200
	}
	return pnts + level*4
}

// block199 is the check of SendCharToWorld and the Pedra Misteriosa: a
// Celestial+ at 199 with the three resets and no unlock is held there.
func (g *Game) block199(u *User) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	ev := evolution(pl)
	if ev == evoSubCele && totalResets(m.info.Info) == 3 && m.info.Info&questUnlock200 != 0 {
		m.info.Sub.Info |= questUnlock200
	}
	if ev >= evoCelestial && pl.BStatus.Level == 199 && m.info.Info&(questUnlock200|questLvBlocked) == 0 && totalResets(m.info.Info) == 3 {
		m.info.Info |= questLvBlocked
		pl.Exp = nextLevel[ev][198]
		g.log.Info("level 200 blocked", "slot", u.id)
		g.sendEtc(u)
	}
}

// subCythera is the CheckGetLevel bonus of a Sub-Celestial wearing a
// Cythera (3500..3507) at levels 120, 150, 180 and 190/199: HP and
// defense, once each.
func (g *Game) subCythera(u *User) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if evolution(pl) != evoSubCele {
		return
	}
	adds := [4][2]uint8{{30, 40}, {35, 50}, {45, 60}, {50, 70}} // {AC, HP}
	flags := [4]uint64{questAdd120, questAdd151, questAdd180, questAdd199}
	i := -1
	switch pl.BStatus.Level {
	case 120:
		i = 0
	case 150:
		i = 1
	case 180:
		i = 2
	case 190, 199:
		i = 3
	}
	if i < 0 || m.info.Info&flags[i] != 0 {
		return
	}
	cyt := &pl.Equip[1]
	if cyt.Index < 3500 || cyt.Index > 3507 {
		return
	}
	var keep wire.ItemEffect
	for _, e := range cyt.Effect {
		if e.Index >= 43 || (e.Index >= 116 && e.Index <= 125) {
			keep = e
			break
		}
	}
	cyt.Effect = [3]wire.ItemEffect{keep, {Index: EF_HP, Value: uint16(adds[i][1])}, {Index: EF_AC, Value: uint16(adds[i][0])}}
	m.info.Info |= flags[i]
	g.sendItem(u, wire.SlotEquip, 1, cyt)
}

// subCeleReset is the Sub-Celestial reset of item 3020 (Arcana done,
// level 199+, 4,000,000,000 exp, 1000 fame): one of the three resets,
// -40,000,000 exp (levels can drop) and up to 5 gold bars 4011.
//
// The C++ also looked for a "Selo Real" with GetFirstSlot(3020) - finding
// the very stone being used - and then used up both, so a stack lost two
// stones; one is used here.
func (g *Game) subCeleReset(u *User, src *wire.Item, resend func()) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if pl.Exp < 4_000_000_000 {
		g.clientMessage(u, "%s", g.lang(nnCantTeleNipp))
		resend()
		return
	}
	if m.info.Fame < 1000 {
		g.clientMessage(u, g.lang(nnNeedXXFame), 1000)
		resend()
		return
	}
	var bit uint64
	switch {
	case m.info.Info&questReset1 == 0:
		bit = questReset1
	case m.info.Info&questReset2 == 0:
		bit = questReset2
	case m.info.Info&questReset3 == 0:
		bit = questReset3
	default:
		g.clientMessage(u, "%s", g.lang(nnDoneAlready))
		resend()
		return
	}
	m.info.Info |= bit
	m.info.Sub.Info |= bit
	g.removeAmount(u, 4011, min(5, inventoryAmount(pl, 4011)))
	m.info.Fame -= 1000
	amountMinus(src)
	resend()
	pl.Exp -= 40_000_000
	ev := evolution(pl)
	for pl.BStatus.Level > 0 && pl.Exp < nextLevel[ev][pl.BStatus.Level-1] {
		pl.BStatus.Level--
	}
	g.log.Info("Sub-Celestial reset", "slot", u.id, "resets", totalResets(m.info.Info))
	g.getCurrentScore(u.id)
	g.sendScore(u.id)
	g.sendEtc(u)
	g.clientMessage(u, "%s", g.lang(nnSuccessComp))
}

// usePedraIdeal is volatile 211.
func (g *Game) usePedraIdeal(u *User, p *wire.UseItem, src *wire.Item) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	resend := func() { g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src) }
	ev := evolution(pl)
	refuse := func(msg string) {
		g.clientMessage(u, "%s", msg)
		resend()
	}

	// The level 200 unlock: both characters at 199, a Selo Real, 1000 fame.
	if ev >= evoCelestial && totalResets(m.info.Info) == 3 && pl.BStatus.Level >= 199 && m.info.Info&questLvBlocked != 0 {
		if pl.BStatus.Level != 199 || m.info.Sub.SubStatus.Level != 199 {
			resend()
			return
		}
		seal := firstSlotOf(pl, itemSeloReal)
		if seal < 0 {
			refuse("Necessario Selo Real para destravar o nivel")
			return
		}
		if m.info.Fame < 1000 {
			refuse("Necessario 1000 pontos de fama para destravar o nível")
			return
		}
		pl.Inventory[seal] = wire.Item{}
		g.sendItem(u, wire.SlotInv, seal, &pl.Inventory[seal])
		m.info.Fame -= 1000
		m.info.Info = m.info.Info&^questLvBlocked | questUnlock200
		m.info.Sub.Info = m.info.Sub.Info&^questLvBlocked | questUnlock200
		g.clientMessage(u, "Nivel desbloqueado")
		amountMinus(src)
		resend()
		return
	}

	switch ev {
	case evoArch:
		g.makeCelestial(u, src, refuse)
	case evoCelestial:
		g.makeSubCelestial(u, src, refuse)
	default:
		resend()
	}
}

// makeCelestial is the Arch -> Celestial branch of the Pedra Ideal: from
// Arch 355 with a level 400 Mortal, a kingdom and no Cythera. The
// character starts over at level 1 with a Cythera by its Arch level.
func (g *Game) makeCelestial(u *User, src *wire.Item, refuse func(string)) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	level := int(pl.Status.Level)
	if level < 354 {
		refuse("Função ainda não implementada!")
		return
	}
	var cythera uint16
	var bit uint64
	switch {
	case level <= 369:
		cythera, bit = 3500, questLevel355
	case level <= 379:
		cythera, bit = 3500, questLevel370
	case level <= 397:
		cythera, bit = 3501, questLevel380
	case level == 398:
		cythera, bit = 3501, questLevel398
	case level == 399:
		cythera, bit = 3502, questLevel399
	default:
		refuse("Level inadequado!")
		return
	}
	if level == 354 { // 354 passes the first check but has no row
		refuse("Level inadequado!")
		return
	}
	var cape uint16
	switch pl.CapeInfo {
	case capeBlue:
		cape = 3197
	case 8:
		cape = 3198
	case 0: // CAPE_WHITE
		cape = 3199
	}
	if cape == 0 {
		refuse("Necessario possuir um Reino para criação do celestial")
		return
	}
	if g.mortalLevel(u.id) != 399 {
		refuse("Necessario que o Mortal seja nível 400")
		return
	}
	if pl.Equip[1].Index != 0 {
		refuse("Desequipe sua cythera para continuar")
		return
	}
	// Base stats by class (from the Arch face kept in EF2). Fixed: the
	// C++ gave a BeastMaster the FoeMaster's INT/DEX/CON.
	stats := map[int][4]uint16{0: {8, 4, 7, 6}, 1: {5, 8, 5, 5}, 2: {6, 6, 9, 5}, 3: {8, 9, 13, 6}}
	face := int(pl.Equip[0].Effect[1].Index)
	cls := -1
	switch {
	case face > 31:
		cls = 3
	case face > 21:
		cls = 2
	case face > 11:
		cls = 1
	case face > 1:
		cls = 0
	}
	if s, ok := stats[cls]; ok {
		pl.BStatus.STR, pl.BStatus.INT, pl.BStatus.DEX, pl.BStatus.CON = s[0], s[1], s[2], s[3]
	}
	m.info.Info |= bit
	name := wire.CString(pl.Name[:])
	g.log.Info("Celestial created", "slot", u.id, "arch_level", level, "cythera", cythera, "cape", cape)
	g.serverNotice("Parabens " + name + " pela criação do seu Celestial")
	pl.SkillBar1 = [4]uint8{255, 255, 255, 255}
	for i := range m.info.SkillBar {
		m.info.SkillBar[i] = 255
	}
	pl.Learn[0] = 1 << 30
	pl.Equip[0].Effect[1].Value = evoCelestial
	pl.Equip[1] = wire.Item{Index: cythera}
	pl.BStatus.Level, pl.Status.Level, pl.Exp = 0, 0, 0
	pl.BStatus.Mastery = [4]uint16{}
	pl.Equip[15] = wire.Item{Index: cape}
	g.sendItem(u, wire.SlotEquip, 15, &pl.Equip[15])
	g.sendItem(u, wire.SlotEquip, 1, &pl.Equip[1])
	g.sendScore(u.id)
	amountMinus(src)
	slot := u.charSlot
	g.charLogout(u)
	g.signalParm(u, uint16(u.id), opSrvOutSealSlot, int32(slot))
	g.clientMessage(u, "Que Deus abençoe seu caminho!")
}

// makeSubCelestial is the Celestial -> Sub-Celestial branch: a Celestial
// 120+ wearing a sephirot gets a Sub-Celestial of that class (DBSRV 0x830
// writes it into the account).
//
// Fixed: the C++ used up two stones from a stack (AmountMinus twice).
func (g *Game) makeSubCelestial(u *User, src *wire.Item, refuse func(string)) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if m.info.Sub.Status == 1 {
		refuse("Você possui subcelestial")
		return
	}
	if pl.Status.Level < 119 {
		refuse("Crie Subcelestial partir do level 120")
		return
	}
	seph := int(pl.Equip[11].Index) - 1760
	if seph < 0 || seph > 3 {
		refuse("É necessario uma Sephirot para a criação do Sub Celestial")
		return
	}
	face := int(pl.Equip[0].Effect[1].Index)
	cape := uint32(3199)
	switch pl.CapeInfo {
	case capeBlue:
		cape = 3197
	case 8:
		cape = 3198
	}
	p := wire.CreateSubCele{Header: wire.NewHeader(wire.OpCreateSubCele, uint16(u.id)),
		CharPos: uint32(u.charSlot), ClassInfo: uint32(seph), Learn: pl.Learn[0], Face: uint32(face), Mantle: cape}
	copy(p.Name[:12], pl.Name[:12])
	amountMinus(src)
	pl.Equip[11] = wire.Item{}

	var learn uint32 = 1 << 30
	for i := 24; i < 32; i++ {
		if i != 30 && pl.Learn[0]&(1<<i) != 0 {
			learn |= 1 << i
		}
	}
	base := 36
	switch {
	case face < 10:
		base = 6
	case face < 20:
		base = 16
	case face < 30:
		base = 26
	}
	sub := &m.info.Sub
	*sub = wire.SubInfo{}
	sub.Equip[0] = wire.Item{Index: uint16(base + seph)}
	sub.Equip[0].Effect[1] = wire.ItemEffect{Index: uint8(base + seph), Value: evoSubCele}
	sub.Learn = learn
	sub.Status = 1
	for i := range sub.SkillBar {
		sub.SkillBar[i] = 255
	}
	g.serverNotice("Parabens " + wire.CString(pl.Name[:]) + " pela criação do seu SubCelestial")
	g.log.Info("Sub-Celestial created", "slot", u.id, "class", seph, "level", pl.Status.Level)
	slot := u.charSlot
	g.charLogout(u)
	g.sendDB(&p)
	g.signalParm(u, uint16(u.id), opSrvOutSealSlot, int32(slot))
	g.clientMessage(u, "Que Deus abençoe seu caminho!")
}

// mountTimer is the RegenMob part run every 450 regen turns (about an
// hour): eggs incubate one step, living mounts get hungry (2 feed, 4 for
// the bigger ones) and starve at 1, unless the Poção Revigorante
// (affect 49) is on.
func (g *Game) mountTimer(u *User) {
	m := &g.mobs[u.id]
	u.regenTurns++
	if u.regenTurns%450 != 0 {
		return
	}
	mount := &m.info.Player.Equip[14]
	if mount.Index >= 2300 && mount.Index < 2330 {
		if d := g.data.itemAbility(mount, EF_INCUDELAY); d > 0 {
			mount.Effect[2].Value = uint16(d - 1) // where the C++ kept it
			g.sendItem(u, wire.SlotEquip, 14, mount)
			g.clientMessage(u, "%s", g.lang(nnIncuProceed))
		}
	}
	if mount.Index < 2330 || mount.Index >= 2390 || mountHP(mount) <= 0 {
		return
	}
	feed := int(mount.Effect[2].Index)
	if (int(mount.Index)-2330)%30 <= 15 {
		feed -= 2
	} else {
		feed -= 4
	}
	for _, a := range m.info.Affects {
		if a.Index == 49 {
			g.sendItem(u, wire.SlotEquip, 14, mount)
			return
		}
	}
	if feed > 1 {
		mount.Effect[2].Index = uint8(feed)
		g.sendItem(u, wire.SlotEquip, 14, mount)
		return
	}
	setMountHP(mount, 0)
	mount.Effect[2].Index = 4
	g.clientMessage(u, "%s", g.lang(nnMountDied))
	g.log.Info("mount starved", "slot", u.id, "mount", mount.Index)
	if mount.Index >= 2360 {
		g.processAdultMount(u.id, 0)
		g.getCurrentScore(u.id)
	} else {
		g.mountProcess(u.id)
	}
	g.sendItem(u, wire.SlotEquip, 14, mount)
}
