package game

import (
	"strings"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The drop window: the monsters of a region (0xA08) or carrying an item
// (0xA09), and the drop filter (0x506D). RequestDropList.cpp, RequestFiltroDrop.cpp.

const (
	opDropList       = 0xA08
	opDropListSearch = 0xA09
	opFilterDrop     = 0x506D

	maxDropListMobs   = 50
	maxDropSearchMobs = 30
	dropRegionDuelist = 10 // the generators of the Carta de Duelo

	// The Carta de Duelo monsters: mGener.pList[46..83].
	dropDuelistFirst = 46
	dropDuelistCount = 38
)

// dropCell is a map block (Target / 128).
type dropCell struct{ x, y int }

// dropRegions are the map blocks of each region of the window.
var dropRegions = map[int][]dropCell{
	1: {{16, 16}, {17, 16}, {18, 16}},                                                             // Armia
	2: {{19, 13}, {18, 13}, {16, 12}, {17, 13}, {17, 12}, {17, 11}, {15, 12}, {17, 10}, {14, 13}}, // Azran
	3: {{19, 14}, {19, 15}, {20, 15}, {20, 16}, {19, 16}},                                         // Erion
	4: {{28, 23}, {28, 22}, {28, 21}, {29, 22}, {29, 23}, {27, 23}, {27, 22}, {27, 21}},           // Karden
	5: {{12, 13}, {11, 13}, {10, 13}, {9, 13}, {8, 13}, {10, 14}},                                 // Deserto
	6: {{9, 31}, {10, 31}, {11, 31}, {10, 29}, {11, 29}},                                          // Submundo
	7: {{1, 29}, {2, 29}, {3, 29}, {4, 29}, {5, 29}, {7, 31}, {6, 31}, {5, 31}},                   // Dungeon
	8: {{18, 31}, {17, 31}, {17, 30}, {18, 30}, {19, 30}, {19, 31}},                               // Kefra
	9: {{29, 27}, {28, 28}, {30, 28}},                                                             // Lan House
}

// dropListHidden are the monsters the window leaves out.
var dropListHidden = map[string]bool{"Baleia": true, "Town Watcher": true, "Town Watcher ": true,
	"K's GuardChief": true, "Wizard": true, "Compounder": true, "Gate Keeper": true, "Guard": true}

func inDropRegion(cells []dropCell, m *Mob) bool {
	c := dropCell{int(m.target.X) / 128, int(m.target.Y) / 128}
	for _, r := range cells {
		if r == c {
			return true
		}
	}
	return false
}

// dropListAdd appends a monster unless the window is full or already has
// that name. merchants never appear.
func dropListAdd(p *wire.DropList, limit int, mob *wire.Mob, x, y uint16, all bool) {
	if mob.Status.Merchant&0x0F != 0 || int(p.Amount) >= limit {
		return
	}
	name := wire.CString(mob.Name[:])
	for i := range int(p.Amount) {
		if wire.CString(p.Drop[i].Name[:]) == name {
			return
		}
	}
	d := &p.Drop[p.Amount]
	copy(d.Name[:], mob.Name[:])
	d.X, d.Y = int16(x), int16(y)
	d.Exp, d.Gold = int64(mob.Exp), mob.Gold
	for i := range d.Carry {
		if it := mob.Inventory[i]; all || it.Index > 40 {
			d.Carry[i] = it
		}
	}
	p.Amount++
}

func dropListMobAlive(m *Mob) bool { return m.mode == modeNPCIdle || m.mode == modeBattle }

// requestDropList is 0xA08 (SendDropList): at most once a second.
func (g *Game) requestDropList(u *User, pkt protocol.Packet) {
	var req wire.Parm2
	if wire.Unmarshal(pkt.Raw, &req) != nil || u.status != userPlay {
		return
	}
	now := time.Now()
	if now.Sub(u.lastDropList) < time.Second {
		return
	}
	u.lastDropList = now
	region := int(req.Parm1)
	p := wire.DropList{Header: wire.NewHeader(opDropList, uint16(u.id))}
	if cells := dropRegions[region]; cells != nil {
		for id := MaxPlayer; id < MaxMob; id++ {
			m := &g.mobs[id]
			if dropListMobAlive(m) && !dropListHidden[wire.CString(m.info.Player.Name[:])] && inDropRegion(cells, m) {
				dropListAdd(&p, maxDropListMobs, &m.info.Player, m.target.X, m.target.Y, false)
			}
		}
	}
	if region == dropRegionDuelist {
		for i := dropDuelistFirst; i < dropDuelistFirst+dropDuelistCount && i < len(g.data.Geners); i++ {
			dropListAdd(&p, maxDropListMobs, &g.data.Geners[i].Leader, 0, 0, false)
		}
	}
	g.queue(u, &p)
}

// requestDropSearch is 0xA09 (RequestSearchDropList): the monsters carrying
// an item with this text in the name, at most once every 2 seconds.
func (g *Game) requestDropSearch(u *User, pkt protocol.Packet) {
	var req wire.SignalNick
	if wire.Unmarshal(pkt.Raw, &req) != nil || u.status != userPlay {
		return
	}
	now := time.Now()
	if now.Sub(u.lastDropSearch) < 2*time.Second {
		return
	}
	u.lastDropSearch = now
	text := strings.ToLower(wire.GameString(req.Nick[:]))
	p := wire.DropList{Header: wire.NewHeader(opDropList, uint16(u.id))}
	for id := MaxPlayer; id < MaxMob && int(p.Amount) < maxDropSearchMobs; id++ {
		m := &g.mobs[id]
		if dropListMobAlive(m) && g.carriesItemNamed(&m.info.Player, text) {
			dropListAdd(&p, maxDropSearchMobs, &m.info.Player, m.target.X, m.target.Y, true)
		}
	}
	g.queue(u, &p)
}

func (g *Game) carriesItemNamed(mob *wire.Mob, text string) bool {
	for i := range mob.Inventory {
		if id := mob.Inventory[i].Index; id != 0 && strings.Contains(strings.ToLower(g.data.Item(id).Name), text) {
			return true
		}
	}
	return false
}

// Filter window answers (MSG_pFilterDrop.Code).
const (
	filterCodeClear  = 0
	filterCodeAdd    = 1
	filterCodeEnable = 2
	filterCodeLoad   = 5
	filterCodeRemove = 7
	filterCodeType   = 9
	filterCodeShow   = 10 // leaves the drop list and shows the filter
)

// requestFilterDrop is 0x506D (Msg_FilterDrop).
func (g *Game) requestFilterDrop(u *User, pkt protocol.Packet) {
	var req wire.FilterDropMsg
	if wire.Unmarshal(pkt.Raw, &req) != nil || u.status != userPlay {
		return
	}
	m := &g.mobs[u.id]
	if m.info.Player.Status.CurHP <= 0 {
		return
	}
	f := &m.info.FilterDrop
	// The header goes without the client id, as in the C++.
	p := wire.FilterDropMsg{Header: wire.NewHeader(opFilterDrop, 0), Code: filterCodeShow}
	switch req.Code {
	case filterCodeClear:
		f.Items = [24]wire.Item{}
		f.IsEnabled = false
		g.queue(u, &p)
		g.clientMessage(u, "O seu filtro de drop foi limpo.")

	case filterCodeAdd:
		// Only the id counts; what the client claims about the item is not stored.
		id := req.Items[0].Index
		if id == 0 || int(id) >= MaxItemList {
			return
		}
		free := -1
		for i := range f.Items {
			if f.Items[i].Index == id {
				g.clientMessage(u, "Este item já existe no filtro.")
				return
			}
			if free < 0 && f.Items[i].Index == 0 {
				free = i
			}
		}
		if free < 0 {
			g.clientMessage(u, "Não há mais espaço para por no filtro.")
			return
		}
		f.Items[free] = wire.Item{Index: id}
		p.Items = f.Items
		if f.IsEnabled {
			g.clientMessage(u, "O item foi adicionado no filtro.")
		} else {
			f.IsEnabled = true
			g.clientMessage(u, "O item foi adicionado no filtro. Filtro ativado!")
		}
		g.queue(u, &p)

	case filterCodeEnable:
		f.IsEnabled = req.IsEnabled != 0
		if f.IsEnabled {
			g.clientMessage(u, "Habilitado.")
		} else {
			g.clientMessage(u, "Desabilitado.")
		}

	case filterCodeLoad:
		p.Code = 0
		p.Items = f.Items
		g.queue(u, &p)

	case filterCodeRemove:
		id, next := req.Items[0].Index, 0
		var kept [24]wire.Item
		for _, it := range f.Items {
			if it.Index != 0 && it.Index != id {
				kept[next] = it
				next++
			}
		}
		f.Items, p.Items = kept, kept
		g.queue(u, &p)
		g.clientMessage(u, "Item removido com sucesso.")

	case filterCodeType:
		f.Type = req.Type
	}
}
