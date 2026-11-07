package game

import (
	"testing"
	"time"

	"dbsrv/internal/wire"
)

func TestMissionFlow(t *testing.T) {
	g := worldGame(t)
	if len(g.data.Missions) == 0 {
		t.Skip("no Missions.xml")
	}
	u := placePlayer(g, 2100, 2100, 0)
	m := &g.mobs[1]
	d := &m.info.DailyQuest
	m.info.Player.Status.Level = 10

	// A fresh character gets a mission on the next timer tick.
	g.missionTimer()
	q := g.findMission(d.QuestID)
	if q == nil || d.IsAccepted {
		t.Fatalf("no mission offered: %+v", d)
	}
	found := false
	for _, c := range q.Classes {
		found = found || c == evoMortal
	}
	if !found {
		t.Fatalf("mission %d is not for mortals", q.ID)
	}

	// Collecting before accepting is refused.
	g.requestCollectMission(u, packet(&wire.SignalValue{Header: wire.NewHeader(opMissionCollect, 1)}))
	if d.QuestID != int32(q.ID) {
		t.Fatal("collect without accepting closed the mission")
	}
	g.requestAcceptMission(u, packet(&wire.SignalValue{Header: wire.NewHeader(opMissionAccept, 1), Value: 1}))
	if !d.IsAccepted {
		t.Fatal("not accepted")
	}

	// Not finished yet.
	g.requestCollectMission(u, packet(&wire.SignalValue{Header: wire.NewHeader(opMissionCollect, 1)}))
	if !d.IsAccepted {
		t.Fatal("collected an unfinished mission")
	}

	// Kill the goal mobs: put an NPC with the right name at each kill.
	npc := MaxPlayer + 5
	g.mobs[npc].mode = 1
	g.mobs[npc].target = m.target
	for i := range 5 {
		if q.MobName[i] == "" {
			continue
		}
		wire.PutGameString(g.mobs[npc].info.Player.Name[:15], q.MobName[i])
		for range q.MobAmount[i] + 2 {
			g.missionMobKilled(1, npc)
		}
		if int(d.MobCount[i]) != q.MobAmount[i] {
			t.Fatalf("goal %d: %d/%d", i, d.MobCount[i], q.MobAmount[i])
		}
	}

	exp := m.info.Player.Exp
	g.requestCollectMission(u, packet(&wire.SignalValue{Header: wire.NewHeader(opMissionCollect, 1)}))
	if d.QuestID != -1 || d.IsAccepted {
		t.Fatalf("not collected: %+v", d)
	}
	if q.Exp > 0 && m.info.Player.Exp <= exp {
		t.Fatal("no exp")
	}
	if q.Free[0].Index != 0 && firstSlotOf(&m.info.Player, q.Free[0].Index) < 0 {
		t.Fatal("no free reward")
	}
	if m.info.Player.Gold != 0 {
		t.Fatal("gold without the mission pass")
	}

	// The next one only after the delay.
	g.missionTimer()
	if d.QuestID != -1 {
		t.Fatal("new mission right away")
	}
	d.LastUpdate = itemDate(time.Now().Add(-g.missionNextDelay() - time.Minute))
	g.missionTimer()
	if d.QuestID == -1 {
		t.Fatal("no new mission after the delay")
	}

	// An accepted mission expires.
	g.requestAcceptMission(u, packet(&wire.SignalValue{Header: wire.NewHeader(opMissionAccept, 1), Value: 1}))
	d.LastUpdate = itemDate(time.Now().Add(-g.missionCompleteTime() - time.Minute))
	g.missionTimer()
	if d.QuestID != -1 || d.IsAccepted {
		t.Fatalf("did not expire: %+v", d)
	}
}

func TestLac100(t *testing.T) {
	g, u, pl := useItemGame(t)
	id := refineTarget(t, g)

	// Plain gear: only +10 -> +11.
	pl.Inventory[0] = wire.Item{Index: 4141}
	pl.Inventory[5] = sanced(id, 5)
	useOn(g, u, 0, wire.SlotInv, 5)
	if itemSanc(&pl.Inventory[5]) != 5 || pl.Inventory[0].Index != 4141 {
		t.Fatal("lac 100 used on a +5")
	}
	pl.Inventory[5] = sanced(id, 10)
	useOn(g, u, 0, wire.SlotInv, 5)
	if itemSanc(&pl.Inventory[5]) != 11 || pl.Inventory[0].Index != 0 {
		t.Fatalf("+10 gear: sanc %d", itemSanc(&pl.Inventory[5]))
	}

	// Earrings, Hércules included.
	pl.Inventory[0] = wire.Item{Index: 4141}
	pl.Inventory[6] = sanced(595, 9)
	useOn(g, u, 0, wire.SlotInv, 6)
	if itemSanc(&pl.Inventory[6]) != 10 {
		t.Fatalf("earring: sanc %d", itemSanc(&pl.Inventory[6]))
	}

	// Planet amulets at +9.
	pl.Inventory[0] = wire.Item{Index: 4141}
	pl.Inventory[7] = sanced(762, 9)
	useOn(g, u, 0, wire.SlotInv, 7)
	if itemSanc(&pl.Inventory[7]) != 10 {
		t.Fatalf("amulet: sanc %d", itemSanc(&pl.Inventory[7]))
	}
}
