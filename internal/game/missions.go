package game

import (
	"strings"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The daily mission event (UOD_Mission.cpp TOD_Mission,
// RequestAcceptRejectMission.cpp, RequestCollectMission.cpp):
//   - every playing character without an accepted mission gets a random one
//     of Data/Missions.xml for its evolution (mortals also by level) once
//     missions.offer_every_seconds passed since the last change;
//   - 0x349 accepts (Value 1) or refuses it; an accepted mission must be
//     finished within missions.time_to_complete_seconds;
//   - NPC kills by the character, its summons or its party (within half a
//     grid of the killer) count toward the mob goals;
//   - 0x348 hands the rewards over when every goal is met: exp, the free
//     items and, with an active mission pass, the pass items and the gold.
// The state is the character's DailyQuest, saved with the character.
//
// Fixed from the C++: "/attpass" no longer restarts the mission clock; a
// mission without <minLevel>/<maxLevel> is open to every level (the C++
// compared mortals against 0..0); rewards that do not fit stay pending
// instead of being lost (the mission is only closed when everything was
// delivered).

const (
	opMissionCollect = opMissionInfo + 1 // 0x348
	opMissionAccept  = opMissionInfo + 2 // 0x349

)

// missionNextDelay / missionCompleteTime: tmsrv.yaml missions.* (C++ 1152 s).
func (g *Game) missionNextDelay() time.Duration {
	return time.Duration(g.cfg.Missions.OfferEverySeconds) * time.Second
}

func (g *Game) missionCompleteTime() time.Duration {
	return time.Duration(g.cfg.Missions.TimeToCompleteSeconds) * time.Second
}

func missionDate(d wire.ItemDate) time.Time {
	return time.Date(int(d.Ano), time.Month(d.Mes), int(d.Dia), int(d.Hora), int(d.Minuto), int(d.Segundo), 0, time.Local)
}

func (g *Game) findMission(id int32) *Mission {
	for i := range g.data.Missions {
		if g.data.Missions[i].ID == int(id) {
			return &g.data.Missions[i]
		}
	}
	return nil
}

// missionTimer is TOD_Mission::Work (the C++ ran it every 800 ms).
func (g *Game) missionTimer() {
	if len(g.data.Missions) == 0 {
		return
	}
	now := time.Now()
	for _, u := range g.users[1:] {
		if u.status != userPlay {
			continue
		}
		d := &g.mobs[u.id].info.DailyQuest
		next, complete := g.missionNextDelay(), g.missionCompleteTime()
		elapsed := next + time.Second
		if d.LastUpdate.Ano != 0 {
			elapsed = now.Sub(missionDate(d.LastUpdate))
		}
		if !d.IsAccepted {
			if elapsed > next {
				g.nextMission(u)
			}
		} else if elapsed > complete {
			g.clientMessage(u, "Você não completou sua missão a tempo.")
			g.log.Info("mission expired", "slot", u.id, "quest", d.QuestID)
			g.resetMission(u)
			g.sendMissionInfo(u)
		}
	}
}

// nextMission is SetNextDailyQuest.
func (g *Game) nextMission(u *User) {
	pl := &g.mobs[u.id].info.Player
	ev, level := evolution(pl), int(pl.Status.Level)
	var avail []*Mission
	for i := range g.data.Missions {
		q := &g.data.Missions[i]
		ok := false
		for _, c := range q.Classes {
			if c == ev {
				ok = true
				break
			}
		}
		if !ok {
			continue
		}
		if ev == evoMortal && q.HasLevel && (level < q.MinLevel || level > q.MaxLevel) {
			continue
		}
		avail = append(avail, q)
	}
	d := &g.mobs[u.id].info.DailyQuest
	d.LastUpdate = itemDate(time.Now())
	if len(avail) == 0 {
		return
	}
	q := avail[rnd()%len(avail)]
	d.QuestID = int32(q.ID)
	d.IsAccepted = false
	d.MobCount, d.ItemCount = [5]uint32{}, [5]uint32{}
	g.sendMissionInfo(u)
	g.clientMessage(u, "Chegou uma nova missão!")
	g.log.Info("mission offered", "slot", u.id, "quest", q.ID)
}

// resetMission is TOD_Mission::Reset.
func (g *Game) resetMission(u *User) {
	d := &g.mobs[u.id].info.DailyQuest
	d.LastUpdate = itemDate(time.Now())
	d.IsAccepted = false
	d.QuestID = -1
	d.MobCount, d.ItemCount = [5]uint32{}, [5]uint32{}
}

// requestAcceptMission is RequestAcceptRejectMission (0x349).
func (g *Game) requestAcceptMission(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if u.status != userPlay || wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	d := &g.mobs[u.id].info.DailyQuest
	if d.QuestID == -1 || g.findMission(d.QuestID) == nil {
		g.clientMessage(u, "Você precisa aguardar o tempo para aceitar ou recusar a próxima missão!")
		return
	}
	if p.Value == 1 {
		if d.IsAccepted {
			g.sendMissionInfo(u)
			return
		}
		d.IsAccepted = true
		d.LastUpdate = itemDate(time.Now())
		g.clientMessage(u, "Você aceitou a missão. Você tem 20 minutos para concluí-la")
		g.log.Info("mission accepted", "slot", u.id, "quest", d.QuestID)
	} else {
		g.clientMessage(u, "Você rejeitou a missão")
		g.log.Info("mission refused", "slot", u.id, "quest", d.QuestID)
		g.resetMission(u)
	}
	g.sendMissionInfo(u)
}

// requestCollectMission is RequestCollectMission (0x348).
func (g *Game) requestCollectMission(u *User, _ protocol.Packet) {
	if u.status != userPlay {
		return
	}
	m := &g.mobs[u.id]
	d := &m.info.DailyQuest
	if d.QuestID == -1 || !d.IsAccepted {
		g.clientMessage(u, "Você não tem uma missão em andamento para coletar")
		return
	}
	q := g.findMission(d.QuestID)
	if q == nil {
		g.clientMessage(u, "Falha ao recolher a recompensa")
		g.resetMission(u)
		g.sendMissionInfo(u)
		return
	}
	for i := range 5 {
		if (q.MobAmount[i] > 0 && int(d.MobCount[i]) < q.MobAmount[i]) ||
			(q.DropAmount[i] > 0 && int(d.ItemCount[i]) < q.DropAmount[i]) {
			g.clientMessage(u, "Você ainda não completou sua missão")
			return
		}
	}

	pl := &m.info.Player
	pass := dateRemaining(d.BattlePass) > 0
	// Every reward item must fit before anything is given (the C++ gave what
	// fitted and closed the mission, losing the rest).
	need := 0
	for i := range 6 {
		if q.Free[i].Index != 0 {
			need += q.FreeAmount[i]
		}
		if pass && q.Pass[i].Index != 0 {
			need += q.PassAmount[i]
		}
	}
	if need > 0 && inventoryAmount(pl, 0) < need {
		g.clientMessage(u, "Falta espaço no inventário (%d espaços livres necessários)", need)
		return
	}

	exp := int64(q.Exp)
	ev, level := evolution(pl), int(pl.Status.Level)
	if (ev <= evoArch && level == maxLevel) || (ev >= evoCelestial && level >= maxLevelCelestial) {
		exp = 0
	}
	if ev != evoMortal {
		exp = g.expApply2(exp, u.id, u.id, false)
	}
	if exp > 0 {
		pl.Exp += exp
	}
	give := func(items *[6]wire.Item, amount *[6]int) {
		for i := range 6 {
			if items[i].Index == 0 {
				continue
			}
			for range amount[i] {
				it := items[i]
				if !g.putItem(u, &it) {
					g.clientMessage(u, "Falta espaço no inventário")
					return
				}
				g.clientMessage(u, "Chegou um item: [%s]", g.itemName(it.Index))
			}
		}
	}
	give(&q.Free, &q.FreeAmount)
	if pass {
		give(&q.Pass, &q.PassAmount)
		pl.Gold = min(pl.Gold+int64(q.Gold), g.cfg.Limits.MaxGold)
	}
	g.log.Info("mission collected", "slot", u.id, "quest", q.ID, "exp", exp, "pass", pass)
	g.clientMessage(u, "Recompensa coletada com sucesso")
	g.resetMission(u)
	g.checkGetLevel(u)
	g.sendEtc(u)
	g.sendMissionInfo(u)
}

// missionMobKilled is TOD_Mission::MobKilled: killer is the player (a
// summon's kill was already given to its owner).
func (g *Game) missionMobKilled(killer, killed int) {
	if killed < MaxPlayer || killer <= 0 || killer >= MaxPlayer || len(g.data.Missions) == 0 {
		return
	}
	name := strings.TrimSpace(wire.DecodeGame(mobName(&g.mobs[killed].info.Player)))
	if name == "" {
		return
	}
	k := &g.mobs[killer]
	for _, id := range g.partyMembers(killer) {
		mu := g.player(id)
		if mu == nil || mu.status != userPlay {
			continue
		}
		mm := &g.mobs[id]
		d := &mm.info.DailyQuest
		if d.QuestID == -1 || !d.IsAccepted {
			continue
		}
		if id != killer && distance(int(mm.target.X), int(mm.target.Y), int(k.target.X), int(k.target.Y)) > halfGrid {
			continue
		}
		q := g.findMission(d.QuestID)
		if q == nil {
			g.resetMission(mu)
			continue
		}
		changed := false
		for i := range 5 {
			if q.MobName[i] == "" || !strings.EqualFold(strings.TrimSpace(q.MobName[i]), name) {
				continue
			}
			if int(d.MobCount[i]) >= q.MobAmount[i] {
				continue
			}
			d.MobCount[i]++
			changed = true
		}
		if changed {
			g.sendMissionInfo(mu)
		}
	}
}
