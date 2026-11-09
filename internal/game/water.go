package game

import "dbsrv/internal/wire"

// The water rooms (Water.cpp): nine rooms in three difficulties. The item
// opens a room, the party is taken there, and clearing the monsters gives
// the leader the scroll of the next room.

const (
	waterGenN        = 22
	waterGenM        = 10
	waterGenA        = 34
	waterRoomSeconds = 120
	waterExitX       = 1965
	waterExitY       = 1770
)

var (
	waterItemBase = [3]int{3174, 778, 3183}
	waterGenBase  = [3]int{waterGenN, waterGenM, waterGenA}
	waterAreas    = [3][9]mapRect{
		{{1040, 3545, 1055, 3560}, {1040, 3510, 1055, 3525}, {1040, 3475, 1055, 3490}, {1080, 3475, 1095, 3490}, {1115, 3475, 1130, 3490}, {1115, 3510, 1130, 3525}, {1115, 3545, 1130, 3560}, {1075, 3545, 1096, 3560}, {1075, 3505, 1095, 3530}},
		{{1170, 3675, 1185, 3690}, {1170, 3635, 1185, 3655}, {1170, 3600, 1185, 3620}, {1205, 3600, 1220, 3620}, {1240, 3600, 1255, 3620}, {1240, 3640, 1255, 3655}, {1240, 3675, 1255, 3690}, {1205, 3675, 1220, 3690}, {1205, 3630, 1225, 3655}},
		{{1295, 3545, 1315, 3565}, {1295, 3510, 1315, 3525}, {1300, 3470, 1315, 3490}, {1335, 3475, 1350, 3490}, {1370, 3475, 1385, 3490}, {1370, 3510, 1385, 3525}, {1365, 3545, 1385, 3565}, {1330, 3545, 1350, 3565}, {1330, 3505, 1355, 3530}},
	}
	waterPlaces = [3][9][2]int{
		{{1050, 3555}, {1050, 3518}, {1050, 3482}, {1086, 3482}, {1122, 3482}, {1122, 3518}, {1122, 3555}, {1086, 3555}, {1086, 3518}},
		{{1178, 3682}, {1178, 3647}, {1178, 3610}, {1214, 3610}, {1250, 3610}, {1250, 3647}, {1250, 3683}, {1215, 3683}, {1215, 3647}},
		{{1306, 3554}, {1306, 3518}, {1306, 3482}, {1342, 3482}, {1378, 3482}, {1378, 3518}, {1377, 3554}, {1342, 3554}, {1342, 3517}},
	}
)

type waterRoom struct {
	time   int // -1: free
	mode   int // 1: fighting, 2: cleared, waiting to leave
	leader int
}

func (g *Game) initWater() {
	for t := range g.water {
		for r := range g.water[t] {
			g.water[t][r].time = -1
		}
	}
}

func (g *Game) waterMobs(typ, room int) int {
	if room == 8 {
		n := 0
		for i := 8; i < 12; i++ {
			n += g.genAlive(waterGenBase[typ] + i)
		}
		return n
	}
	return g.genAlive(waterGenBase[typ] + room)
}

func (g *Game) waterKillRoom(typ, room int) {
	for id := firstNPC; id < MaxMob; id++ {
		m := &g.mobs[id]
		if m.mode != 0 && !m.npc.isSummon && waterAreas[typ][room].contains(int(m.target.X), int(m.target.Y)) {
			g.deleteMob(id, 1)
		}
	}
}

func (g *Game) waterEvict(typ, room int) {
	a := waterAreas[typ][room]
	g.clearAreaTeleport([4]int{a.minX, a.minY, a.maxX, a.maxY}, waterExitX, waterExitY)
}

func (g *Game) waterParty(leader int, fn func(u *User)) {
	if leader <= 0 || leader >= MaxPlayer || g.users[leader].status != userPlay {
		return
	}
	fn(g.users[leader])
	for _, id := range g.partyMembers(leader) {
		if id != leader && id > 0 && id < MaxPlayer && g.users[id].status == userPlay {
			fn(g.users[id])
		}
	}
}

// waterTimer is CWaterScroll::Process, every 2 seconds.
func (g *Game) waterTimer() {
	for t := range g.water {
		for r := range g.water[t] {
			w := &g.water[t][r]
			timer := w.time
			if timer == -1 {
				continue
			}
			if timer <= 0 && w.mode != 2 {
				g.waterKillRoom(t, r)
				g.waterEvict(t, r)
				*w = waterRoom{time: -1}
				continue
			}
			if timer <= 0 {
				g.waterEvict(t, r)
				*w = waterRoom{time: -1}
				continue
			}
			mobs := g.waterMobs(t, r)
			if mobs <= 1 && w.mode == 1 {
				w.mode = 2
				timer = 18
				g.waterParty(w.leader, func(u *User) { g.signalParm(u, wire.ClientIDSystem, opSrvQuestSeconds, 15) })
				if w.leader > 0 && w.leader < MaxPlayer && g.users[w.leader].status == userPlay && r != 8 {
					it := wire.Item{Index: uint16(waterItemBase[t] + r)}
					if lu := g.users[w.leader]; !g.putItem(lu, &it) {
						g.clientMessage(lu, "Sem espaço para o pergaminho da próxima sala.")
					}
				}
			} else {
				g.waterParty(w.leader, func(u *User) { g.signalParm(u, wire.ClientIDSystem, opSrvMobsLeft, int32(mobs)) })
			}
			w.time = timer - 3
		}
	}
}

// useWaterScroll is CWaterScroll::Use_Item.
func (g *Game) useWaterScroll(u *User, p *wire.UseItem, src *wire.Item, vol int) bool {
	typ, minus := 2, 3182
	switch {
	case vol >= 21 && vol <= 30:
		typ, minus = 1, 777
	case vol >= 131 && vol <= 140:
		typ, minus = 0, 3173
	case vol >= 161 && vol <= 170:
	default:
		return false
	}
	resend := func() { g.sendItem(u, wire.SlotInv, int(p.SrcSlot), src) }
	refuse := func(format string, args ...any) bool {
		g.clientMessage(u, format, args...)
		resend()
		return true
	}
	room := int(src.Index) - minus
	if room < 0 || room >= 9 {
		resend()
		return true
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if w := &g.water[typ][room]; w.time != -1 {
		return refuse("Sala ocupada por %s e seu grupo. Tempo restante: %d seg.", g.charName(w.leader), w.time)
	}
	if m.npc.leader != 0 {
		return refuse("Apenas o líder do grupo pode utilizar!")
	}
	ev := evolution(pl)
	if typ == 0 && ev > evoArch {
		return refuse("Somente personagem MORTAL/ARCH pode entrar nesta Quest.")
	}
	if typ == 0 && ev == evoArch && pl.Status.Level > 350 {
		return refuse("Nível não permitido para Arch.")
	}
	x, y := int(m.target.X), int(m.target.Y)
	cur := 9
	for i, a := range waterAreas[typ] {
		if a.contains(x, y) {
			cur = i
			break
		}
	}
	atEntry := x > 1961 && x < 1970 && y > 1770 && y < 1778
	if !atEntry && (cur == 9 || g.water[typ][cur].mode != 2) {
		return refuse("Use na Zona Elemental da Água!")
	}
	if cur == room {
		resend()
		return true
	}
	if cur != 9 && g.water[typ][cur].mode == 2 {
		g.waterKillRoom(typ, cur)
		g.water[typ][cur].time, g.water[typ][cur].mode = -1, 0
	}
	switch vol {
	case 21:
		u.waterCount[1]++
	case 131:
		u.waterCount[0]++
	case 161:
		u.waterCount[2]++
	}
	gen := waterGenBase[typ] + room
	if room == 8 {
		switch chance := rnd() % 100; {
		case chance >= 75:
		case chance >= 50:
			gen++
		case chance >= 25:
			gen += 2
		default:
			gen += 3
		}
	}
	for id := firstNPC; id < MaxMob; id++ {
		if g.mobs[id].mode != 0 && g.mobs[id].npc.generateID == gen {
			g.deleteMob(id, 1)
		}
	}
	g.generateMob(gen, 0, 0)
	place := waterPlaces[typ][room]
	members := []int{u.id}
	for _, id := range g.partyMembers(u.id) {
		if id != u.id {
			members = append(members, id)
		}
	}
	for _, id := range members {
		if id <= 0 || id >= MaxPlayer || g.users[id].status != userPlay {
			continue
		}
		g.teleportar(id, place[0], place[1])
		g.signalParm(g.users[id], uint16(id), opSrvMobsLeft, int32(gen))
		g.signalParm(g.users[id], wire.ClientIDSystem, opSrvQuestSeconds, waterRoomSeconds)
	}
	g.water[typ][room] = waterRoom{time: waterRoomSeconds, mode: 1, leader: u.id}
	amountMinus(src)
	resend()
	return true
}
