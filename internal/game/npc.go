package game

import "dbsrv/internal/wire"

// NPC generation (CMob.cpp GenerateMob, CNPCGener::GetEmptyNPCMob and the
// generator part of ProcessMinTimer).

const (
	MaxMob      = 30000 // MAX_MOB: 0..999 players, 1000.. NPCs
	firstNPC    = MaxPlayer
	modeNPCIdle = 4
)

// npcState is the CMob part only NPCs use.
type npcState struct {
	generateID int
	leader     int
	followers  [12]int // PartyList
	formation  int
	routeType  int
	segX, segY [5]int
	segWait    [5]int
	progress   int
	direction  int
	waitSec    int
	curX, curY int // Segment.X/Y: the point being walked to
	nextX      int
	nextY      int
	summoner   int  // Summoner: the player who called this summon
	isSummon   bool // IsSummon
}

// gFormation is g_pFormation[segment][follower] = {dx, dy}.
var gFormation [5][12][2]int8

func init() {
	// The C++ initializer is a flat list of 120 chars filling [5][12][2] in
	// order; rebuild it the same way so the table matches byte for byte.
	flat := []int8{
		1, 1, -1, 1, 1, -1, -1, -1, 1, 0,
		1, 0, -1, 0, 2, 0, -2, 0, 3, 0,
		1, 1, -1, 1, 1, -1, -1, -1, 1, 0,
		1, 0, -1, 0, 2, 0, -2, 0, 3, 0,
		2, 0, 0, 2, 1, 1, 0, 1, 1, 0,
		1, 0, 0, 0, 0, 0, 0, 0, 1, 0,
		1, 0, 0, 0, 1, 0, 0, 0, 1, 0,
		0, 0, 0, 0, 1, 0, 0, 0, 1, 0,
		1, 0, 0, 0, 1, 0, 0, 0, 1, 0,
		1, 0, 0, 0, 1, 0, 0, 0, 1, 0,
		1, 0, 0, 0, 1, 0, 0, 0, 0, 0,
		1, 0, 0, 0, 1, 0, 0, 0, 1, 0,
	}
	for i, v := range flat {
		gFormation[i/24][(i/2)%12][i%2] = v
	}
}

// emptyNPCMob is GetEmptyNPCMob: rotating search over 1000..29999.
func (g *Game) emptyNPCMob() int {
	if g.npcCursor < firstNPC || g.npcCursor >= MaxMob {
		g.npcCursor = firstNPC
	}
	id := g.npcCursor
	for range MaxMob - firstNPC {
		if id >= MaxMob {
			id = firstNPC
		}
		if g.mobs[id].mode == 0 {
			g.npcCursor = id
			return id
		}
		id++
	}
	return 0
}

// skipGener lists the generators ProcessMinTimer never fires (kingdom
// battle kings/towers and the other event-driven ones).
func skipGener(i int) bool {
	switch i {
	case 0, 1, 2, 5, 6, 7:
		return true
	}
	return false
}

// minTimer is the generator part of ProcessMinTimer (every 12 s).
func (g *Game) minTimer() {
	g.minCounter++
	for i := 1; i < len(g.data.Geners); i++ {
		gen := &g.data.Geners[i]
		if skipGener(i) || gen.MinuteGenerate <= 0 {
			continue
		}
		if g.minCounter%gen.MinuteGenerate == i%gen.MinuteGenerate {
			g.generateMob(i, 0, 0)
		}
	}
	g.gateTimer()
	g.weatherTimer()
	g.zakumMinute()
	if g.minCounter%5 == 0 {
		g.saveChannelState()
		g.reportOpenStores()
	}
}

// npcFromTemplate resets mob id to a generator's leader/follower template.
func (g *Game) npcFromTemplate(id int, tmpl *wire.Mob) *Mob {
	m := &g.mobs[id]
	*m = Mob{}
	m.info.Player = *tmpl
	pl := &m.info.Player
	pl.Name[15] = 0
	pl.BStatus.Merchant &= 0x0F // Direction = 0
	for i := range 16 {
		if pl.Name[i] == '_' || pl.Name[i] == '@' {
			pl.Name[i] = ' '
		}
	}
	if pl.CapeInfo == 1 && rnd()%10 == 1 {
		pl.CapeInfo = 2
	}
	return m
}

// generateMob is GenerateMob(arg1, arg2, arg3).
func (g *Game) generateMob(genID, atX, atY int) {
	if genID <= 0 || genID >= len(g.data.Geners) {
		return
	}
	if !g.kingdomCanSpawn(genID) {
		return
	}
	src := genID
	gen := &g.data.Geners[genID]
	if gen.MinuteGenerate >= 500 {
		// A "random" generator: Segment[0] holds a generator id range.
		span := gen.SegmentY[0] - gen.SegmentX[0]
		if span <= 0 {
			return
		}
		genID = gen.SegmentX[0] + rnd()%span
		if genID <= 0 || genID >= len(g.data.Geners) {
			return
		}
	}
	seg := &g.data.Geners[genID] // segments come from the relocated generator
	count := gen.MinGroup + rnd()%(max(gen.MaxGroup-gen.MinGroup+1, 1))
	if g.genCount[src] >= gen.MaxNumMob || !gen.HasLeader {
		return
	}
	leader := g.emptyNPCMob()
	if leader == 0 {
		return
	}
	m := g.npcFromTemplate(leader, &gen.Leader)
	for i := range 5 {
		if seg.SegmentX[i] == 0 {
			continue
		}
		if r := seg.SegmentRange[i]; r != 0 {
			m.npc.segX[i] = seg.SegmentX[i] - r + rnd()%(r+1)*2
			m.npc.segY[i] = seg.SegmentY[i] - r + rnd()%(r+1)*2
		} else {
			m.npc.segX[i], m.npc.segY[i] = seg.SegmentX[i], seg.SegmentY[i]
		}
		m.npc.segWait[i] = seg.SegmentWait[i]
	}
	if atX != 0 && atY != 0 {
		for i := range 5 {
			m.npc.segX[i] = atX + rnd()%5 - 2
			m.npc.segY[i] = atY + rnd()%5 - 2
		}
	}
	m.npc.generateID = genID
	m.npc.formation = gen.Formation
	m.npc.routeType = gen.RouteType
	m.npc.waitSec = m.npc.segWait[0]
	g.getCurrentScore(leader)
	m.info.Player.Status.CurHP = m.info.Player.Status.MaxHP
	m.lastTime = g.now()
	x, y := m.npc.segX[0], m.npc.segY[0]
	if !g.emptyMobGrid(leader, &x, &y) {
		*m = Mob{}
		return
	}
	g.placeNPC(leader, x, y)
	g.genCount[src] = max(g.genCount[src], 0) + 1
	g.kingdomSpawned(genID, leader)
	g.bossSpawned(genID, leader)
	// TODO(phase C): Leader.bStatus.maxMP is an affect applied on spawn.

	if !gen.HasFollower {
		return
	}
	for i := 0; i < count && i < len(m.npc.followers); i++ {
		f := g.emptyNPCMob()
		if f == 0 {
			continue
		}
		fm := g.npcFromTemplate(f, &gen.Follower)
		g.mobs[leader].npc.followers[i] = f
		lead := &g.mobs[leader].npc
		for s := range 5 {
			if seg.SegmentX[s] == 0 {
				continue
			}
			fm.npc.segX[s] = lead.segX[s] + int(gFormation[s][i][0])
			fm.npc.segY[s] = lead.segY[s] + int(gFormation[s][i][1])
			fm.npc.segWait[s] = gen.SegmentWait[s]
		}
		if atX != 0 && atY != 0 {
			for s := range 5 {
				fm.npc.segX[s] = atX + rnd()%5 - 2
				fm.npc.segY[s] = atY + rnd()%5 - 2
			}
		}
		fm.npc.generateID = genID
		fm.npc.formation = gen.Formation
		fm.npc.routeType = gen.RouteType
		fm.npc.leader = leader
		fm.npc.waitSec = fm.npc.segWait[0]
		g.getCurrentScore(f)
		fm.info.Player.Status.CurHP = fm.info.Player.Status.MaxHP
		fx, fy := fm.npc.segX[0], fm.npc.segY[0]
		if !g.emptyMobGrid(f, &fx, &fy) {
			*fm = Mob{}
			g.mobs[leader].npc.followers[i] = 0
			continue
		}
		g.placeNPC(f, fx, fy)
		g.genCount[src] = max(g.genCount[src], 0) + 1
	}
}

// placeNPC puts a freshly generated NPC on the grid and shows it to the
// players around (SpawnType 2 = "being born" animation).
func (g *Game) placeNPC(id, x, y int) {
	m := &g.mobs[id]
	m.mode = modeNPCIdle
	m.target = wire.Position{X: uint16(x), Y: uint16(y)}
	m.last = m.target
	m.npc.curX, m.npc.curY = x, y
	g.setCell(x, y, uint16(id))
	m.spawnType = 2
	g.sendGridMob(id)
	m.spawnType = 0
}

// isNPC reports whether a mob id is in the NPC range.
func isNPC(id int) bool { return id >= firstNPC && id < MaxMob }
