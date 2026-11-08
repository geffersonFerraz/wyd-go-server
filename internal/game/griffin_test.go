package game

import (
	"testing"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

func griffinGame(t *testing.T) (*Game, *User, int) {
	g := worldGame(t)
	u := placePlayer(g, 2300, 2300, 0)
	listen(u)
	npc := 1500
	g.mobs[npc] = Mob{mode: modeNPCIdle}
	wire.PutCString(g.mobs[npc].info.Player.Name[:], griffinMasterName)
	g.mobs[npc].target = wire.Position{X: 2305, Y: 2300}
	return g, u, npc
}

func TestGriffinMasterWarps(t *testing.T) {
	g, u, npc := griffinGame(t)
	send := func(warp, typ int32) {
		g.requestGriffinMaster(u, protocol.Packet{Raw: wire.MustMarshal(&wire.Parm2{Header: wire.NewHeader(opGriffinMaster, 1), Parm1: warp, Parm2: typ})})
	}
	start := g.mobs[1].target
	send(1, 1) // opening the window does not move
	send(9, 2) // no such destination
	send(1, 3) // bad type
	if g.mobs[1].target != start {
		t.Fatal("moved without a valid landing")
	}
	send(1, 2)
	if d := distance(int(g.mobs[1].target.X), int(g.mobs[1].target.Y), 2372, 2099); d > 3 {
		t.Fatalf("landed at %+v", g.mobs[1].target)
	}
	// Far from the master: nothing.
	at := g.mobs[1].target
	g.mobs[npc].target = wire.Position{X: 100, Y: 100}
	send(0, 2)
	if g.mobs[1].target != at {
		t.Fatal("used from far")
	}
}

func TestRedeemGriffinTicket(t *testing.T) {
	g, u, _ := griffinGame(t)
	pl := &g.mobs[1].info.Player
	send := func(kind, slot, choice int32) {
		g.requestRedeemGriffin(u, protocol.Packet{Raw: wire.MustMarshal(&wire.Parm3{Header: wire.NewHeader(opRedeemGriffin, 1), Parm1: kind, Parm2: slot, Parm3: choice})})
	}
	pl.Inventory[2] = wire.Item{Index: 4716}
	pl.Inventory[2].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 2}
	send(5, 2, 0) // unknown kind
	send(1, 2, 0) // wrong ticket for the kind
	send(0, 2, 9) // no such costume
	if itemAmount(&pl.Inventory[2]) != 2 {
		t.Fatal("ticket spent")
	}
	send(0, 2, 2) // stack of two: one left, the costume in a free slot
	if itemAmount(&pl.Inventory[2]) != 1 || firstSlotOf(pl, 4209) < 0 {
		t.Fatalf("stack: %+v", pl.Inventory[2])
	}
	send(0, 2, 4) // the last one becomes the costume
	if pl.Inventory[2].Index != 4207 {
		t.Fatalf("last: %+v", pl.Inventory[2])
	}
}

// The shipped generators spawn the Mestre Grifo and keep the Teleporte_Quest.
func TestShippedGeneratorSpawnsGriffinMaster(t *testing.T) {
	g := worldGame(t)
	idx, teleport := -1, false
	for i := range g.data.Geners {
		switch wire.CString(g.data.Geners[i].Leader.Name[:]) {
		case "Mestre_Grifo":
			idx = i
		case "Teleporte_Quest":
			teleport = true
		}
	}
	if idx < 0 || !teleport {
		t.Fatalf("generator %d, Teleporte_Quest %v", idx, teleport)
	}
	g.generateMob(idx, 0, 0)
	npc := g.griffinMaster()
	if npc == 0 {
		t.Fatal("no Mestre Grifo after spawning")
	}
	if at := g.mobs[npc].target; distance(int(at.X), int(at.Y), 2118, 2080) > 2 {
		t.Fatalf("spawned at %+v", at)
	}
}
