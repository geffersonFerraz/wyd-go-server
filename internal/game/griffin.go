package game

import (
	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The Mestre Grifo (RequestGriffinMaster.cpp) and the costume tickets it
// redeems (RequestRedeemGriffin.cpp).

const (
	opGriffinMaster = 0xAD9
	opRedeemGriffin = 0x632
	opRedeemClose   = 0x633

	griffinMasterName  = "Mestre Grifo"
	griffinMaxDistance = 15
)

// griffinWarps are the destinations of the Mestre Grifo.
var griffinWarps = []struct {
	name string
	x, y int
}{
	{"Campo de Treino", 2112, 2051},
	{"Defensor da alma", 2372, 2099},
	{"Jardim de Deus", 2220, 1714},
	{"Dungeon", 2365, 2249},
	{"Submundo", 1826, 1771},
}

// griffinTickets: the ticket of each kind and the costumes it can become.
var griffinTickets = []struct {
	ticket   uint16
	costumes []uint16
}{
	{4716, []uint16{4208, 5711, 4209, 5710, 4207}},
	{4731, []uint16{5715, 5714}},
}

// griffinMaster finds the NPC, keeping the id while it is still the master.
func (g *Game) griffinMaster() int {
	isMaster := func(id int) bool {
		return id >= firstNPC && id < MaxMob && g.mobs[id].mode != 0 &&
			wire.CString(g.mobs[id].info.Player.Name[:]) == griffinMasterName
	}
	if isMaster(g.griffinNPC) {
		return g.griffinNPC
	}
	for id := firstNPC; id < MaxMob; id++ {
		if isMaster(id) {
			g.griffinNPC = id
			return id
		}
	}
	return 0
}

// requestGriffinMaster is 0xAD9: Type 1 opens the flight, Type 2 lands.
func (g *Game) requestGriffinMaster(u *User, pkt protocol.Packet) {
	var p wire.Parm2 // Warp, Type
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay || g.mobs[u.id].info.Player.Status.CurHP <= 0 {
		return
	}
	if p.Parm2 != 1 && p.Parm2 != 2 {
		return
	}
	npc := g.griffinMaster()
	if npc == 0 {
		g.log.Warn("Mestre Grifo used but not alive", "slot", u.id)
		return
	}
	a, b := g.mobs[npc].target, g.mobs[u.id].target
	if distance(int(a.X), int(a.Y), int(b.X), int(b.Y)) > griffinMaxDistance {
		g.log.Warn("Mestre Grifo used from far", "slot", u.id, "x", b.X, "y", b.Y)
		return
	}
	if p.Parm2 != 2 || p.Parm1 < 0 || int(p.Parm1) >= len(griffinWarps) {
		return
	}
	w := griffinWarps[p.Parm1]
	g.clientMessage(u, "Chegou no destino [%s].", w.name)
	g.teleportar(u.id, w.x, w.y)
}

// requestRedeemGriffin is 0x632: Parm1 the ticket kind, Parm2 its slot,
// Parm3 the costume chosen. The C++ wrote item -1 for an unknown kind.
func (g *Game) requestRedeemGriffin(u *User, pkt protocol.Packet) {
	var p wire.Parm3
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	if p.Parm1 < 0 || int(p.Parm1) >= len(griffinTickets) || p.Parm2 < 0 || p.Parm2 >= 60 {
		return
	}
	kind := &griffinTickets[p.Parm1]
	pl := &g.mobs[u.id].info.Player
	slot := int(p.Parm2)
	ticket := &pl.Inventory[slot]
	if ticket.Index != kind.ticket {
		return
	}
	if p.Parm3 < 0 || int(p.Parm3) >= len(kind.costumes) {
		g.clientMessage(u, "Escolha incorreta")
		return
	}
	costume := wire.Item{Index: kind.costumes[p.Parm3]}
	if itemAmount(ticket) == 1 {
		g.replaceCarry(u, slot, costume)
	} else {
		free := firstCarrySlot(&pl.Inventory, costume.Index)
		if free < 0 {
			g.clientMessage(u, "Sem espaço livre no inventario")
			return
		}
		pl.Inventory[free] = costume
		g.sendItem(u, wire.SlotInv, free, &pl.Inventory[free])
		amountMinus(ticket)
		g.sendItem(u, wire.SlotInv, slot, ticket)
	}
	g.log.Info("griffin ticket redeemed", "slot", u.id, "item", costume.Index)
	g.clientMessage(u, "Resgatado com sucesso")
	g.signal(u, uint16(u.id), opRedeemClose)
}
