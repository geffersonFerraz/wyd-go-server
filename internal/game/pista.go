package game

import (
	"slices"
	"time"

	"dbsrv/internal/wire"
)

// The Pista de Runas (Basedef.cpp DoRune, GiveRuna; Uxmal in
// RequestMerchantNPC.cpp): a leader registers a "Pista" item, the parties go
// in at :00, :20 and :40 and have 15 minutes.

const (
	pistaRooms    = 7
	pistaParties  = 3
	pistaLeader   = 12 // slot of the leader in a party
	pistaSeconds  = 900
	itemPista     = 5134
	itemSeedBalog = 4032

	genLich    = 175
	genTorre   = 176
	genValky   = 179
	genKalintz = 180
	genSulrang = 189
	genHell    = 191
	genHellEnd = 238 // the boss; the rooms are genHell..genHell+46
	genBarlog  = 243
	genCoelho  = 244
	hellRooms  = 47
)

var (
	pistaCoords = [pistaRooms][6]int{
		{3328, 1599, 3452, 1660, 3350, 1655}, {3328, 1539, 3452, 1595, 3393, 1585}, {3328, 1407, 3452, 1466, 3418, 1451},
		{3328, 1025, 3452, 1148, 3380, 1085}, {3328, 1280, 3454, 1406, 3444, 1396}, {3328, 1153, 3452, 1276, 3430, 1180},
		{3328, 1467, 3452, 1535, 3434, 1502},
	}
	pistaGenerators = [pistaRooms][2]int{{genLich, 1}, {genTorre, 3}, {genValky, 1}, {genSulrang, 3}}
	pistaMaxParties = [pistaRooms]int{2, 3, 3, 2, 2, 2, 2}
	pistaRunes      = [pistaRooms][]int{
		{5110, 5112, 5115, 5113, 5111}, {5114, 5113, 5117, 5111, 5115, 5112}, {5118, 5121, 5122, 5116, 5130, 5119},
		{5122, 5126, 5121, 5116, 5119}, {5125, 5126, 5124, 5127}, {5120, 5131, 5118, 5119, 5123, 5132, 5121},
		{5130, 5131, 5119, 5133, 5120, 5123, 5132, 5129, 5128},
	}
	pistaExit       = [2][2]int{{3299, 1687}, {3299, 1701}}
	pistaTowerStart = [pistaParties][2]int{{3387, 1554}, {3418, 1577}, {3358, 1578}}
	// barlogGates: maxX, maxY, minX, minY of each gate; barlogCenters: where
	// each room starts.
	barlogGates = [4][][4]int{
		{{3381, 1168, 3379, 1166}, {3381, 1180, 3379, 1178}},
		{{3342, 1225, 3340, 1223}, {3353, 1226, 3351, 1223}, {3364, 1225, 3362, 1223}},
		{{3380, 1239, 3378, 1237}, {3388, 1239, 3386, 1237}, {3396, 1239, 3394, 1237}, {3404, 1239, 3402, 1237}},
		{{3431, 1199, 3429, 1197}},
	}
	barlogCenters = [5][2]int{{3431, 1182}, {3359, 1174}, {3348, 1263}, {3422, 1232}, {3384, 1199}}
)

type pistaRoom struct {
	clients    [pistaParties][13]int
	status     bool
	timer      int
	born       bool
	points     [pistaParties]int
	room       [pistaParties]int
	roomSorted [hellRooms]bool
}

func pistaArea(sala int) mapRect {
	c := pistaCoords[sala]
	return mapRect{c[0], c[1], c[2], c[3]}
}

func (p *pistaRoom) partyOf(id int) int {
	for party := range p.clients {
		if slices.Contains(p.clients[party][:], id) {
			return party
		}
	}
	return -1
}

func (g *Game) pistaEach(sala, party int, fn func(u *User)) {
	for _, id := range g.pista[sala].clients[party] {
		if id > 0 && id < MaxPlayer && g.users[id].status == userPlay {
			fn(g.users[id])
		}
	}
}

// npcUxmal is the registration, in the 4 minutes before each round.
func (g *Game) npcUxmal(u *User, npc int, now time.Time) {
	if m := now.Minute() % 20; m < 16 {
		g.sendSay(npc, "Não é possível registrar agora ...")
		return
	}
	me := &g.mobs[u.id]
	if me.npc.leader != 0 {
		g.sendSay(npc, "O registro da quest pode ser usado apenas pelos líderes do grupo")
		return
	}
	if me.npc.followers[0] == 0 {
		g.clientMessage(u, "Impossível registrar.")
		return
	}
	pl := &me.info.Player
	slot := firstSlotOf(pl, itemPista)
	if slot < 0 {
		g.sendSay(npc, "Para registrar é necessário uma Pista de Runas")
		return
	}
	ref := itemSanc(&pl.Inventory[slot])
	if ref > 6 {
		g.sendSay(npc, "Não está disponível.")
		return
	}
	for s := range g.pista {
		for party := range g.pista[s].clients {
			if g.pista[s].clients[party][pistaLeader] == u.id {
				g.sendSay(npc, "Você tem uma pista registrada.")
				return
			}
		}
	}
	p := &g.pista[ref]
	party := -1
	for i := range pistaMaxParties[ref] {
		if p.clients[i][pistaLeader] == 0 {
			party = i
			break
		}
	}
	if party < 0 {
		g.sendSay(npc, "Máximo de grupo atingido...")
		return
	}
	p.clients[party][pistaLeader] = u.id
	for x, id := range me.npc.followers {
		if x < pistaLeader && id > 0 && id < MaxPlayer && g.users[id].status == userPlay {
			p.clients[party][x] = id
		}
	}
	g.sendSay(npc, "Pista +%d registrada por %s", ref, g.charName(u.id))
	pl.Inventory[slot] = wire.Item{}
	g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
	g.log.Info("pista registered", "slot", u.id, "room", ref, "party", party)
}

// pistaUnregister: whoever leaves the game leaves the Pista.
func (g *Game) pistaUnregister(id int) {
	for s := range g.pista {
		for party := range g.pista[s].clients {
			for m, c := range g.pista[s].clients[party] {
				if c == id {
					g.pista[s].clients[party][m] = 0
					return
				}
			}
		}
	}
}

// pistaTransfer follows a change of party leader.
func (g *Game) pistaTransfer(from, to int) {
	for s := range g.pista {
		for party := range g.pista[s].clients {
			c := &g.pista[s].clients[party]
			if c[pistaLeader] != from {
				continue
			}
			c[pistaLeader] = to
			if i := slices.Index(c[:pistaLeader], to); i >= 0 {
				c[i] = from
			}
			return
		}
	}
}

// giveRuna is GiveRuna: a rune for each member in the room, at most two per
// computer, and the next Pista for the leader.
func (g *Game) giveRuna(sala, party int) {
	p := &g.pista[sala]
	area := pistaArea(sala)
	inside := func(id int) bool {
		t := g.mobs[id].target
		return area.contains(int(t.X), int(t.Y))
	}
	received := map[[8]byte]int{}
	for _, id := range p.clients[party] {
		if id <= 0 || id >= MaxPlayer || g.users[id].status != userPlay || !inside(id) || g.mobs[id].info.Player.Status.CurHP <= 0 {
			continue
		}
		u := g.users[id]
		pl := &g.mobs[id].info.Player
		if firstEmptySlot(pl) == -1 {
			g.clientMessage(u, "Sem espaço no inventário para receber a Runa")
			continue
		}
		if received[u.mac] >= 2 {
			continue
		}
		rune := wire.Item{Index: uint16(pistaRunes[sala][rnd()%len(pistaRunes[sala])])}
		g.eventItem(u, &rune, "Pista de Runas")
		received[u.mac]++
	}
	leader := p.clients[party][pistaLeader]
	if leader <= 0 || leader >= MaxPlayer || g.users[leader].status != userPlay || !inside(leader) || sala == 6 {
		return
	}
	lu := g.users[leader]
	if firstEmptySlot(&g.mobs[leader].info.Player) == -1 {
		g.clientMessage(lu, "Não foi possível receber a Pista de Runas. Falta espaço no inventário")
		return
	}
	next := wire.Item{Index: itemPista}
	next.Effect[0] = wire.ItemEffect{Index: 43, Value: uint16(sala + 1)}
	g.eventItem(lu, &next, "Pista de Runas")
}

func (g *Game) clearNPCsIn(area mapRect) {
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		if m.mode != 0 && area.contains(int(m.target.X), int(m.target.Y)) {
			g.deleteMob(id, 1)
		}
	}
}

// pistaTimer is DoRune, every 2 seconds.
func (g *Game) pistaTimer(now time.Time) {
	for i := range g.pista {
		p := &g.pista[i]
		if !p.status {
			continue
		}
		if p.timer-2 <= 0 {
			g.finishPista(i)
			continue
		}
		p.timer -= 2
		switch i {
		case 2, 6:
			if p.timer <= 600 && !p.born {
				gen := genValky
				if i == 6 {
					gen = genCoelho
				}
				g.generateMob(gen, 0, 0)
				p.born = true
				for party := range p.clients {
					g.pistaEach(i, party, func(u *User) { g.clientMessage(u, "O Boss nasceu.") })
				}
			}
		case 5:
			g.barlogGates(p)
		}
	}
	if now.Minute()%20 != 0 {
		return
	}
	for i := range g.pista {
		if !g.pista[i].status {
			g.startPista(i)
		}
	}
}

func (g *Game) finishPista(i int) {
	p := &g.pista[i]
	switch i {
	case 1: // each party whose tower is standing
		for x := range pistaParties {
			if g.genAlive(genTorre+x) != 0 {
				g.giveRuna(i, x)
			}
		}
	case 3: // the party with the most points
		best, most := -1, -1
		for x, pts := range p.points {
			if pts > most {
				best, most = x, pts
			}
		}
		if best != -1 {
			g.giveRuna(i, best)
		}
	}
	area := pistaArea(i)
	g.clearNPCsIn(area)
	sent := 0
	for party := range p.clients {
		for _, id := range p.clients[party] {
			if id <= 0 || id >= MaxPlayer || g.users[id].status != userPlay {
				continue
			}
			t := g.mobs[id].target
			if area.contains(int(t.X), int(t.Y)) {
				exit := pistaExit[sent/30%len(pistaExit)]
				g.teleportar(id, exit[0], exit[1])
				sent++
			}
		}
	}
	g.clearAreaTeleport([4]int{area.minX, area.minY, area.maxX, area.maxY}, pistaExit[0][0], pistaExit[0][1])
	*p = pistaRoom{}
}

func (g *Game) startPista(i int) {
	p := &g.pista[i]
	used := false
	for party := range p.clients {
		used = used || p.clients[party][pistaLeader] > 0
	}
	if !used {
		return
	}
	g.clearNPCsIn(pistaArea(i))
	if i == 4 {
		for gen := genHell; gen < genHell+hellRooms; gen++ {
			if gen != genHellEnd {
				g.generateMob(gen, 0, 0)
			}
		}
		room := rnd() % hellRooms
		if genHell+room == genHellEnd {
			room = 30
		}
		p.roomSorted[room] = true
	}
	if i != 2 && i != 4 && i != 5 && i != 6 {
		for x := range pistaGenerators[i][1] {
			g.generateMob(pistaGenerators[i][0]+x, 0, 0)
		}
	}
	for party := range p.clients {
		g.pistaEach(i, party, func(u *User) {
			x, y := pistaCoords[i][4], pistaCoords[i][5]
			if i == 1 {
				x, y = pistaTowerStart[party][0], pistaTowerStart[party][1]
			}
			g.teleportar(u.id, x, y)
			g.signalParm(u, uint16(u.id), opSrvQuestSeconds, pistaSeconds)
		})
	}
	p.timer, p.status = pistaSeconds, true
	g.log.Info("pista started", "room", i)
}

// barlogGates: the leader standing on a gate spends a seed and the party
// moves forward (70%), back (20%) or stays.
func (g *Game) barlogGates(p *pistaRoom) {
	for room := range barlogGates {
		for _, gate := range barlogGates[room] {
			for y := gate[3]; y <= gate[1]; y++ {
				for x := gate[2]; x <= gate[0]; x++ {
					id := int(g.cell(x, y))
					if id <= 0 || id >= MaxPlayer {
						continue
					}
					party := -1
					for i := range p.clients {
						if p.clients[i][pistaLeader] == id {
							party = i
							break
						}
					}
					if party >= 0 {
						g.barlogGate(p, party, id)
					}
				}
			}
		}
	}
}

func (g *Game) barlogGate(p *pistaRoom, party, leader int) {
	u := g.users[leader]
	pl := &g.mobs[leader].info.Player
	seed := firstSlotOf(pl, itemSeedBalog)
	if seed < 0 {
		g.clientMessage(u, "Necessário Semente de Cristal para avançar a sala")
		return
	}
	move := func(room int) {
		c := barlogCenters[room]
		for _, id := range p.clients[party] {
			if id > 0 && id < MaxPlayer && g.users[id].status == userPlay {
				g.teleportar(id, c[0], c[1])
			}
		}
	}
	switch roll := rnd() % 100; {
	case roll <= 70 && p.room[party] < 4:
		move(p.room[party] + 1)
		p.room[party]++
		if p.room[party] == 4 {
			g.generateMob(genBarlog, 0, 0)
		}
	case roll > 70 && roll <= 90 && p.room[party] > 0:
		move(p.room[party] - 1)
		p.room[party]--
	default:
		g.clientMessage(u, "Nada aconteceu, tente novamente...")
	}
	pl.Inventory[seed] = wire.Item{}
	g.sendItem(u, wire.SlotInv, seed, &pl.Inventory[seed])
}

// pistaHitBlocked: a party cannot hit its own tower (room 1).
func (g *Game) pistaHitBlocked(attacker, target int) bool {
	gen := g.mobs[target].npc.generateID
	if target < MaxPlayer || gen < genTorre || gen >= genTorre+pistaParties {
		return false
	}
	p := &g.pista[1]
	return !p.status || slices.Contains(p.clients[gen-genTorre][:], attacker)
}

// pistaMoved is the room 2 rule of RequestAction: outside the event nobody
// stays there, and only leaders stay with the boss alone.
func (g *Game) pistaMoved(u *User, x, y int) bool {
	if x < 3329 || x > 3452 || y < 1408 || y > 1462 {
		return false
	}
	p := &g.pista[2]
	if !p.status {
		g.doRecall(u.id)
		return true
	}
	if !p.born || g.mobs[u.id].npc.leader == 0 {
		return false
	}
	area := mapRect{3340, 1412, 3449, 1463}
	count := 0
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		if (m.mode == modeNPCIdle || m.mode == modeBattle) && area.contains(int(m.target.X), int(m.target.Y)) {
			count++
		}
	}
	if count == 1 {
		g.teleportar(u.id, 3368, 1452)
		return true
	}
	return false
}

// pistaMobKilled runs the deaths that matter to a room.
func (g *Game) pistaMobKilled(gen, killer int) {
	if owner := g.summonOwner(killer); owner != 0 {
		killer = owner
	}
	switch {
	case gen == genValky || gen == genHellEnd || gen == genCoelho || gen == genBarlog:
		sala := map[int]int{genValky: 2, genHellEnd: 4, genCoelho: 6, genBarlog: 5}[gen]
		p := &g.pista[sala]
		party := p.partyOf(killer)
		if party < 0 {
			return
		}
		g.giveRuna(sala, party)
		for other := range p.clients {
			if other != party {
				g.pistaEach(sala, other, func(u *User) { g.clientMessage(u, "O boss foi derrotado por outro grupo.") })
			}
		}
	case gen >= genKalintz && gen <= genKalintz+7:
		p := &g.pista[3]
		if party := p.partyOf(killer); party >= 0 {
			p.points[party]++
			g.generateMob(genSulrang+rnd()%2, 0, 0)
		}
	case gen >= genSulrang && gen < genSulrang+2:
		g.generateMob(genKalintz+rnd()%8, 0, 0)
	case gen == genLich:
		reborn := true
		if party := g.pista[0].partyOf(killer); party >= 0 && rnd()%100 <= 30 {
			g.giveRuna(0, party)
			reborn = false
		}
		for id := firstNPC; id < MaxMob; id++ {
			if g.mobs[id].mode != 0 && g.mobs[id].npc.generateID == genLich && g.mobs[id].info.Player.Status.CurHP > 0 {
				g.deleteMob(id, 1)
			}
		}
		if reborn {
			g.generateMob(genLich, 0, 0)
		}
	case gen >= genTorre && gen < genTorre+pistaParties:
		g.pistaEach(1, gen-genTorre, func(u *User) { g.clientMessage(u, "Você não defendeu sua Torre.") })
	case gen >= genHell && gen < genHell+hellRooms && gen != genHellEnd:
		g.hellRoomKilled(gen-genHell, killer, gen)
	}
}

func (g *Game) hellRoomKilled(room, killer, gen int) {
	p := &g.pista[4]
	party := p.partyOf(killer)
	if party < 0 || !p.roomSorted[room] || g.genAlive(gen) > 1 {
		return
	}
	area := pistaArea(4)
	g.pistaEach(4, party, func(u *User) {
		t := g.mobs[u.id].target
		if area.contains(int(t.X), int(t.Y)) {
			g.teleportar(u.id, 3358, 1340)
			g.clientMessage(u, "Teleportado para a sala do Boss")
		}
	})
	for other := range p.clients {
		if other != party {
			g.pistaEach(4, other, func(u *User) {
				g.clientMessage(u, "A sala sorteada foi derrotada por outro grupo. Você falhou na refinação!")
			})
		}
	}
	if !p.born {
		g.generateMob(genHellEnd, 0, 0)
		p.born = true
	}
}
