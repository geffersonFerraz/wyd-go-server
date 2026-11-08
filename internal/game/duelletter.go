package game

import (
	"slices"

	"dbsrv/internal/wire"
)

// The Carta de Duelo (CDuelLetter.cpp): a party dungeon of four rooms and a
// boss, one party at a time per channel. Each room lasts duelLetterSeconds
// and opens the next when its monsters are gone. The fourth room needs the
// Símbolo de Coragem; the boss needs the three runes among those generated.

const (
	duelLetterSeconds = 220
	opMobsLeft        = 0x3B0 // pMsgSignal: monsters left in the room

	itemCourageSymbol = 1732
	duelSymbolSlot    = 10
	firstDuelRune     = 1733
	lastDuelRune      = 1735
	wildDuelRune      = 1736

	duelFirstGener = 46 // DuelLetter_Normal_Index
	duelLastGener  = 83 // DuelLetter_Arcane_Index + 12

	duelRoomFourth = 3
	duelRoomBoss   = 4
)

var (
	duelAltar = mapRect{1045, 1689, 1047, 1691}
	duelArea  = mapRect{782, 3598, 886, 3695}
	// duelRooms is cleared at the end, on top of removing the party.
	duelRooms = [...]mapRect{
		{786, 3656, 826, 3695}, {782, 3599, 830, 3643},
		{841, 3655, 886, 3695}, {841, 3598, 886, 3639},
	}
	duelRoomStart = [...][2]int{{786, 3680}, {844, 3688}, {844, 3632}, {786, 3639}}
	duelRunePlace = [...][][2]int{
		{{787, 3676}},
		{{845, 3676}, {844, 3676}},
		{{845, 3620}, {844, 3621}, {845, 3621}},
		{{787, 3634}, {786, 3634}, {787, 3635}, {786, 3635}},
	}
)

// duelLetterKind holds, per room, where its generators start and how many
// there are, counted from the first generator of the letter.
type duelLetterKind struct {
	firstGen int
	offset   [duelRoomBoss + 1]int
	count    [duelRoomBoss + 1]int
}

var duelLetterKinds = map[uint16]*duelLetterKind{
	3172: {46, [5]int{0, 3, 5, 8, 11}, [5]int{3, 2, 3, 2, 1}}, // N
	3171: {58, [5]int{0, 3, 6, 9, 12}, [5]int{3, 3, 3, 2, 1}}, // M
	1731: {71, [5]int{0, 3, 6, 9, 12}, [5]int{3, 3, 3, 2, 1}}, // A
}

// duelLetter is the running dungeon.
type duelLetter struct {
	active  bool
	kind    *duelLetterKind
	room    int
	timer   int
	courage bool
	users   []int
	runes   []uint16 // the rune chosen for each room so far
	ground  []int    // ground items to take back at the end
}

// useDuelLetter answers the use of a letter and reports whether src is one.
func (g *Game) useDuelLetter(u *User, p *wire.UseItem, src *wire.Item) bool {
	kind, ok := duelLetterKinds[src.Index]
	if !ok {
		return false
	}
	refuse := func(msg string) {
		g.clientMessage(u, "%s", msg)
		g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
	}
	m := &g.mobs[u.id]
	switch {
	case !duelAltar.contains(int(m.target.X), int(m.target.Y)):
		refuse("Utilize no Altar de Thor!")
	case g.duel.active:
		refuse("Quest ocupada")
	case m.npc.leader != 0:
		refuse("Somente o líder do grupo pode utilizar")
	default:
		g.startDuelLetter(u, kind)
		amountMinus(src)
	}
	return true
}

func (g *Game) startDuelLetter(u *User, kind *duelLetterKind) {
	g.finishDuelLetter(false) // leftovers of a previous run
	d := &g.duel
	m := &g.mobs[u.id]
	d.kind, d.room, d.timer = kind, 0, duelLetterSeconds
	d.users = append(d.users, u.id)
	for _, id := range m.npc.followers {
		if id > 0 && id < MaxPlayer {
			d.users = append(d.users, id)
		}
	}
	if symbol := &m.info.Player.Equip[duelSymbolSlot]; symbol.Index == itemCourageSymbol {
		d.courage = true
		*symbol = wire.Item{}
		g.sendItem(u, wire.SlotEquip, duelSymbolSlot, symbol)
	}
	g.generateDuelRoom()
	g.moveDuelParty(d.room)
	d.active = true
	g.log.Info("duel letter started", "slot", u.id, "gener", kind.firstGen, "party", len(d.users), "courage", d.courage)
}

// moveDuelParty warps the party to a room and shows its timer and monsters.
func (g *Game) moveDuelParty(room int) {
	d := &g.duel
	left := int32(g.duelMobsLeft())
	for _, id := range d.users {
		g.signalParm(g.users[id], wire.ClientIDSystem, opQuestTime, int32(d.timer))
		g.signalParm(g.users[id], wire.ClientIDSystem, opMobsLeft, left)
		g.teleportar(id, duelRoomStart[room][0], duelRoomStart[room][1])
	}
}

func (g *Game) duelMobsLeft() int {
	d := &g.duel
	total := 0
	first := d.kind.firstGen + d.kind.offset[d.room]
	for i := range d.kind.count[d.room] {
		total += max(g.genCount[first+i], 0)
	}
	return total
}

func (g *Game) generateDuelRoom() {
	d := &g.duel
	first := d.kind.firstGen + d.kind.offset[d.room]
	if d.room == duelRoomBoss {
		if g.duelBossUnlocked() {
			g.generateMob(first, 0, 0)
		}
		return
	}
	for i := range d.kind.count[d.room] {
		g.generateMob(first+i, 0, 0)
	}
	g.generateDuelRune()
}

// generateDuelRune chooses this room's rune and shows, in the room, the
// runes of every room so far.
func (g *Game) generateDuelRune() {
	d := &g.duel
	var r uint16
	if rnd()%15 == 0 {
		r = wildDuelRune
	}
	if rnd()%6 == 0 && len(d.runes) != 0 {
		r = d.runes[0]
	} else {
		if len(d.runes) != 0 {
			for i := uint16(firstDuelRune); i <= lastDuelRune; i++ {
				if !slices.Contains(d.runes, i) {
					r = i
				}
			}
		}
		if r == 0 {
			r = uint16(firstDuelRune + rnd()%3)
		}
	}
	d.runes = append(d.runes, r)
	for i, at := range duelRunePlace[d.room] {
		if id := g.createGroundItem(at[0], at[1], wire.Item{Index: d.runes[i]}, 0, 0); id != 0 {
			d.ground = append(d.ground, id)
		}
	}
}

// duelBossUnlocked reports the three runes among those generated.
func (g *Game) duelBossUnlocked() bool {
	for r := uint16(firstDuelRune); r <= lastDuelRune; r++ {
		if !slices.Contains(g.duel.runes, r) {
			return false
		}
	}
	return true
}

// duelLetterTimer is CDuelLetter::Work, once a second.
func (g *Game) duelLetterTimer() {
	d := &g.duel
	if !d.active {
		return
	}
	if d.timer--; d.timer <= 0 {
		g.finishDuelLetter(true)
		return
	}
	g.checkDuelLetterUsers()
	if len(d.users) == 0 {
		g.finishDuelLetter(false)
		return
	}
	if left := g.duelMobsLeft(); left != 0 {
		for _, id := range d.users {
			g.signalParm(g.users[id], wire.ClientIDSystem, opMobsLeft, int32(left))
		}
		return
	}
	if d.room == duelRoomBoss || (d.room+1 == duelRoomFourth && !d.courage) {
		return
	}
	d.room++
	if d.room == duelRoomBoss {
		g.generateDuelRoom()
		return
	}
	d.timer = duelLetterSeconds
	g.generateDuelRoom()
	g.moveDuelParty(d.room)
}

// checkDuelLetterUsers drops those who left and recalls those who do not
// belong to the party.
func (g *Game) checkDuelLetterUsers() {
	d := &g.duel
	inArea := func(id int) bool {
		t := g.mobs[id].target
		return duelArea.contains(int(t.X), int(t.Y))
	}
	d.users = slices.DeleteFunc(d.users, func(id int) bool {
		return g.users[id].status != userPlay || !inArea(id)
	})
	for _, u := range g.users[1:] {
		if u.status == userPlay && g.mobs[u.id].mode != 0 && inArea(u.id) && !slices.Contains(d.users, u.id) {
			g.log.Info("in the duel letter without being in the party", "slot", u.id)
			g.doRecall(u.id)
		}
	}
}

func (g *Game) finishDuelLetter(outOfTime bool) {
	d := &g.duel
	for id := firstNPC; id < MaxMob; id++ {
		if gid := g.mobs[id].npc.generateID; g.mobs[id].mode != 0 && gid >= duelFirstGener && gid <= duelLastGener {
			g.deleteMob(id, 1)
		}
	}
	for _, id := range d.users {
		if u := g.users[id]; u.status == userPlay {
			g.doRecall(id)
			if outOfTime {
				g.clientMessage(u, "Tempo esgotado")
			}
		}
	}
	for _, r := range duelRooms {
		g.clearArea(r)
	}
	// A rune may have decayed and its slot gone to some other item.
	for _, id := range d.ground {
		if gi := &g.ground[id]; gi.open && gi.item.Index >= firstDuelRune && gi.item.Index <= wildDuelRune {
			g.removeGroundItem(id)
		}
	}
	*d = duelLetter{}
}
