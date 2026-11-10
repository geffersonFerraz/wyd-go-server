package game

import (
	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// Items on the ground (g_pInitItem / g_pItemGrid): the gates, barriers and
// doors of InitItem.csv, the guild towers, and what players drop or could not
// carry (see dropOwned). Basedef.cpp CreateItem / UpdateItem /
// BASE_UpdateItem3 / ProcessDecayItem / SetCastleDoor..., GetFunc.cpp
// GetCreateItem / GetEmptyItemGrid, RequestPickItem.cpp,
// RequestDropItem.cpp, RequestOpenGate.cpp.

const (
	maxGroundItem   = 4096
	groundIDBase    = 10000 // client ids of ground items: index + 10000
	opSrvCreateItem = 0x26E
	opSrvRemoveItem = 0x16F
	opGate          = 0x374 // client request and server broadcast
	opCliPickItem   = 0x270
	opCliDropItem   = 0x272
	opSrvDropItem   = 0x175

	nnYouveDoneItAlready = 71
	nnCantDropHere       = 72
	nnGuildMedalCantDrop = 73
	nnNoKey              = 79

	gateOpen   = 1 // Status: 1 open, 2/3 closed (3 = locked)
	gateLocked = 3
	towerFirst = 3145 // guild towers 3145..3149 (by win count)
	towerLast  = 3149

	dropOwnerMs     uint32 = 2 * 60 * 1000  // only the owner can pick it up
	dropLifetimeMs  uint32 = 10 * 60 * 1000 // then it is destroyed
	dropMaxDistance        = 8              // cells from the player, walking included
	dropSearchRange        = 6              // how far an item that did not fit may fall
)

// InitItemFile is one row of InitItem.csv (BASE_InitItem).
type InitItemFile struct {
	Index, X, Y, Rotate int
}

// groundItem is STRUCT_INITITEM.
type groundItem struct {
	item       wire.Item
	open       bool // slot in use
	x, y       int
	canRun     int // EF_GROUND: the ground mask
	status     int // gate state
	rotation   int
	closeArmed bool // IsOpen: a keyed gate closes on the second minute tick
	decayMark  bool // Unknow_36: removed on the next decay pass
	height     int

	playerDrop bool   // dropped by a player: follows the rules of dropOwned
	owner      string // who can pick it up before ownerUntil
	ownerUntil uint32
	expireAt   uint32
}

// CreateGroundItem mirrors p26E (32 bytes).
type CreateGroundItem struct {
	Header     wire.Header
	Pos        wire.Position
	Index      int16
	Item       wire.Item
	Rotation   uint8
	Status     uint8
	HeightGrid uint8
	Create     uint8
	Owner      uint16
	_          [2]byte
}

// RemoveGroundItem mirrors p16F / MSG_DecayItem (16 bytes).
type RemoveGroundItem struct {
	Header wire.Header
	InitID int16
	_      int16
}

// GateState mirrors p374 (20 bytes).
type GateState struct {
	Header wire.Header
	GateID int32
	Status int16
	Height int8
	_      uint8
}

// PickItem mirrors p270 (28 bytes).
type PickItem struct {
	Header           wire.Header
	InvType, InvSlot int32
	InitID           int16
	PosX, PosY       int16
	_                int16
}

// DropGroundItem mirrors p272 (32 bytes).
type DropGroundItem struct {
	Header           wire.Header
	InvType, InvSlot int32
	Rotation         int32 // Unknown1
	PosX, PosY       int16
	_                int32
}

// DropItemAck mirrors p175 (28 bytes).
type DropItemAck struct {
	Header           wire.Header
	InvType, InvSlot int32
	_                int32
	PosX, PosY       int16
}

func cellKey(x, y int) int { return y*gridSize + x }

// itemAt is g_pItemGrid[y][x].
func (g *Game) itemAt(x, y int) int { return g.itemGrid[cellKey(x, y)] }

func (g *Game) setItemAt(x, y, id int) {
	if id == 0 {
		delete(g.itemGrid, cellKey(x, y))
		return
	}
	g.itemGrid[cellKey(x, y)] = id
}

// emptyItemGrid is GetEmptyItemGrid: the cell, or one of its 8 neighbours,
// with no item and not blocked.
func (g *Game) emptyItemGrid(x, y *int) bool {
	if inMap(*x, *y) && g.itemAt(*x, *y) == 0 && g.data.height(*x, *y) != 127 {
		return true
	}
	for ny := *y - 1; ny <= *y+1; ny++ {
		for nx := *x - 1; nx <= *x+1; nx++ {
			if inMap(nx, ny) && g.itemAt(nx, ny) == 0 && g.data.height(nx, ny) != 127 {
				*x, *y = nx, ny
				return true
			}
		}
	}
	return false
}

// initGroundItems is the startup part of WindowsForm.cpp: the InitItem.csv
// gates (keyed ones locked), the guild towers, then the castle and
// coliseum doors. Rows take ids 1, 2, ... in order, which the door
// functions rely on (coliseum 13-19, castle 33-36, Zakum 24).
func (g *Game) initGroundItems() {
	for _, f := range g.data.InitItems {
		id := g.createGroundItem(f.X, f.Y, wire.Item{Index: uint16(f.Index)}, f.Rotate, gateOpen)
		if id <= 0 {
			g.log.Warn("InitItem not placed", "item", f.Index, "x", f.X, "y", f.Y)
			continue
		}
		g.initCount = id
		if key := g.data.itemAbility(&g.ground[id].item, EF_KEYID); key != 0 && key < 15 {
			g.updateGroundItem(id, gateLocked)
		}
	}
	for i, z := range g.data.Cities {
		if z.TowerX == 0 && z.TowerY == 0 {
			continue
		}
		it := wire.Item{Index: uint16(towerFirst + min(max(int(z.WinCount), 0), towerLast-towerFirst))}
		if z.OwnerIndex != 0 {
			it.Effect[0] = wire.ItemEffect{Index: EF_HWORDGUILD, Value: uint16(z.OwnerIndex / 257)}
			it.Effect[1] = wire.ItemEffect{Index: EF_LWORDGUILD, Value: uint16(uint8(z.OwnerIndex))}
			it.Effect[2] = wire.ItemEffect{Index: 59, Value: uint16(rnd() % 255)} // EF_UNIQUE
		}
		if g.createGroundItem(int(z.TowerX), int(z.TowerY), it, 3, 0) <= 0 {
			g.log.Warn("guild tower not placed", "city", i)
		}
	}
	g.setDoors(33, 4, gateOpen, false)  // SetCastleDoor(1)
	g.setDoors(13, 2, gateOpen, true)   // SetColoseumDoor(1)
	g.setDoors(15, 5, gateLocked, true) // SetColoseumDoor2(3)
}

// createGroundItem is CreateItem: put an item on (or next to) a cell and
// show it. Returns the ground id, 0 when there is no room.
func (g *Game) createGroundItem(x, y int, it wire.Item, rotation, status int) int {
	if it.Index == 0 || int(it.Index) >= MaxItemList || !g.emptyItemGrid(&x, &y) {
		return 0
	}
	id := 0
	for i := 1; i < maxGroundItem; i++ {
		if !g.ground[i].open {
			id = i
			break
		}
	}
	if id == 0 {
		return 0
	}
	g.ground[id] = groundItem{
		item: it, open: true, x: x, y: y,
		canRun:   g.data.itemAbility(&it, 34), // EF_GROUND
		status:   status,
		rotation: rotation,
		height:   g.data.height(x, y),
	}
	g.setItemAt(x, y, id)
	p := g.groundItemPacket(id)
	g.multicast2(x, y, &p, 0)
	return id
}

// timeReached compares g.now() values, which wrap around.
func timeReached(now, t uint32) bool { return int32(now-t) >= 0 }

// dropOwned puts a player's item on the ground: only the owner can pick it up
// for 2 minutes, then anyone, and after 10 minutes it is destroyed. Items that
// cannot be traded stay reserved for the owner the whole time.
func (g *Game) dropOwned(x, y int, it wire.Item, rotation int, owner string) int {
	id := g.createGroundItem(x, y, it, rotation, 1)
	if id <= 0 {
		return 0
	}
	now := g.now()
	gi := &g.ground[id]
	gi.playerDrop, gi.owner = true, owner
	gi.ownerUntil, gi.expireAt = now+dropOwnerMs, now+dropLifetimeMs
	if g.data.itemAbility(&it, EF_NOTRADE) != 0 {
		gi.ownerUntil = gi.expireAt
	}
	return id
}

// dropNearPlayer drops an item that did not fit in the inventory on the
// closest free cell around the player. Returns the ground id, 0 without room.
func (g *Game) dropNearPlayer(u *User, it wire.Item) int {
	t := g.mobs[u.id].target
	cx, cy := int(t.X), int(t.Y)
	for r := 0; r <= dropSearchRange; r++ {
		for y := cy - r; y <= cy+r; y++ {
			for x := cx - r; x <= cx+r; x++ {
				if max(abs(x-cx), abs(y-cy)) != r || !inMap(x, y) || g.itemAt(x, y) != 0 || g.data.height(x, y) == 127 {
					continue
				}
				id := g.dropOwned(x, y, it, rnd()&3, g.charName(u.id))
				if id != 0 {
					g.log.Info("item fell on the ground", "slot", u.id, "item", it.Index, "effects", it.Effect, "x", x, "y", y)
				}
				return id // 0 here means the list of ground items is full
			}
		}
	}
	return 0
}

// parkUnplacedItems handles the items a saved inventory has no room for now
// that items cover several cells: the cargo first, the ground if it is full.
func (g *Game) parkUnplacedItems(u *User, items []wire.Item) {
	cargo, ground := 0, 0
	for _, it := range items {
		if slot := firstStorageSlot(u.account.StorageItems[:], it.Index); slot >= 0 {
			u.account.StorageItems[slot] = it
			g.sendItem(u, wire.SlotStorage, slot, &u.account.StorageItems[slot])
			cargo++
			g.log.Info("item moved to cargo: inventory reorganized", "slot", u.id, "item", it.Index, "effects", it.Effect, "cargo_slot", slot)
			continue
		}
		if g.dropNearPlayer(u, it) != 0 {
			ground++
		} else {
			g.log.Warn("item lost reorganizing the inventory", "slot", u.id, "item", it.Index, "effects", it.Effect)
		}
	}
	if cargo > 0 {
		g.clientMessage(u, "Inventário reorganizado: %d item(ns) foram para o baú.", cargo)
	}
	if ground > 0 {
		g.clientMessage(u, "Inventário reorganizado: %d item(ns) no chão. Pegue em até 10 minutos.", ground)
	}
}

// groundItemPacket is GetCreateItem.
func (g *Game) groundItemPacket(id int) CreateGroundItem {
	gi := &g.ground[id]
	return CreateGroundItem{
		Header:     wire.NewHeader(opSrvCreateItem, wire.ClientIDSystem),
		Pos:        wire.Position{X: uint16(gi.x), Y: uint16(gi.y)},
		Index:      int16(id + groundIDBase),
		Item:       gi.item,
		Rotation:   uint8(gi.rotation),
		Status:     uint8(gi.status),
		HeightGrid: uint8(gi.height),
	}
}

// sendCreateGroundItem is SendCreateItem.
func (g *Game) sendCreateGroundItem(u *User, id int) {
	if u == nil || u.status != userPlay {
		return
	}
	p := g.groundItemPacket(id)
	g.queue(u, &p)
}

// sendRemoveGroundItem is SendRemoveItem.
func (g *Game) sendRemoveGroundItem(u *User, id int) {
	if u == nil {
		return
	}
	g.queue(u, &RemoveGroundItem{Header: wire.NewHeader(opSrvRemoveItem, wire.ClientIDSystem), InitID: int16(id + groundIDBase)})
}

// removeGroundItem takes an item off the ground and tells everyone near.
func (g *Game) removeGroundItem(id int) {
	gi := &g.ground[id]
	if g.itemAt(gi.x, gi.y) == id {
		g.setItemAt(gi.x, gi.y, 0)
	}
	x, y := gi.x, gi.y
	*gi = groundItem{}
	g.multicast2(x, y, &RemoveGroundItem{Header: wire.NewHeader(opSrvRemoveItem, wire.ClientIDSystem), InitID: int16(id + groundIDBase)}, 0)
}

// sendGroundItemsInView sends the items around (x, y) to a player
// (SendGridMob / GridMulticast's item part).
func (g *Game) sendGroundItemsInView(u *User, x, y int) {
	if u == nil || len(g.itemGrid) == 0 {
		return
	}
	minX, minY, maxX, maxY := viewRect(x, y)
	for id := 1; id < maxGroundItem; id++ {
		gi := &g.ground[id]
		if gi.open && gi.x >= minX && gi.x < maxX && gi.y >= minY && gi.y < maxY {
			g.sendCreateGroundItem(u, id)
		}
	}
}

// groundItemsMoved is the item part of GridMulticast when a player moves
// from (ox, oy) to (nx, ny): remove what left the view, show what entered.
func (g *Game) groundItemsMoved(u *User, ox, oy, nx, ny int) {
	if u == nil || len(g.itemGrid) == 0 {
		return
	}
	minX, minY, maxX, maxY := viewRect(ox, oy)
	dminX, dminY, dmaxX, dmaxY := viewRect(nx, ny)
	for id := 1; id < maxGroundItem; id++ {
		gi := &g.ground[id]
		if !gi.open {
			continue
		}
		inOld := gi.x >= minX && gi.x < maxX && gi.y >= minY && gi.y < maxY
		inNew := gi.x >= dminX && gi.x < dmaxX && gi.y >= dminY && gi.y < dmaxY
		switch {
		case inOld && !inNew:
			g.sendRemoveGroundItem(u, id)
		case inNew && !inOld:
			g.sendCreateGroundItem(u, id)
		}
	}
}

// updateGroundHeight is BASE_UpdateItem3: opening (closed -> 1) lowers and
// closing (1 -> 2/3) raises the cells of the item's ground mask. Returns
// the last height written.
func (g *Game) updateGroundHeight(mask, cur, next, x, y, rot int) (int, bool) {
	// The C++ accepted masks up to 9 and read past its 6-row table.
	if mask < 0 || mask >= 6 || rot < 0 || rot >= 4 {
		return 0, false
	}
	sign := 0
	if cur == 1 && (next == 3 || next == 2) {
		sign = 1
	}
	if next == 1 && (cur == 3 || cur == 2) {
		sign = -1
	}
	if sign == 0 {
		return 0, false
	}
	height := 0
	for dy := 0; dy <= 5; dy++ {
		for dx := 0; dx <= 5; dx++ {
			v := int(groundMask[((mask*4+rot)*6+dy)*6+dx])
			if v == 0 {
				continue
			}
			xp, yp := x+dx-2, y+dy-2
			if xp < 1 || yp < 1 || xp > gridSize-2 || yp > gridSize-2 {
				break // as the C++: skips the rest of the row
			}
			h := min(max(g.data.height(xp, yp)+v*sign, -128), 127)
			height = h
			g.data.Height[yp*gridSize+xp] = byte(int8(h))
		}
	}
	return height, true
}

// updateGroundItem is UpdateItem: change a gate's state, moving the ground
// heights. Returns the new height and whether anything changed.
func (g *Game) updateGroundItem(id, state int) (int, bool) {
	gi := &g.ground[id]
	height, ok := g.updateGroundHeight(gi.canRun, gi.status, state, gi.x, gi.y, gi.rotation)
	if !ok {
		return 0, false
	}
	gi.height = height
	gi.closeArmed = false
	if gi.status != state {
		// Mobs under the gate: summoned gate guards (face 220) go away;
		// everyone else is re-sent where they stand (GetEmptyMobGrid keeps
		// a mob on its own cell, so the C++ never actually moved anyone).
		for dy := 0; dy <= 4; dy++ {
			for dx := 0; dx <= 4; dx++ {
				if groundMask[((gi.canRun*4+gi.rotation)*6+dy)*6+dx] == 0 {
					continue
				}
				x, y := gi.x+dx-2, gi.y+dy-2
				if x < 1 || y < 1 || x > gridSize-2 || y > gridSize-2 {
					continue
				}
				mob := int(g.cell(x, y))
				if mob == 0 || mob >= MaxMob || g.mobs[mob].mode == 0 {
					continue
				}
				if g.mobs[mob].info.Player.Equip[0].Index == 220 {
					g.deleteMob(mob, 3)
					continue
				}
				if g.emptyMobGrid(mob, &x, &y) {
					p := g.action(mob, x, y)
					p.MoveSpeed, p.MoveType = 20, 0
					g.multicast2(x, y, &p, 0)
				}
			}
		}
	}
	gi.status = state
	return height, true
}

// gatePacket is the 0x374 broadcast of a gate's state.
func (g *Game) broadcastGate(id, height int) {
	gi := &g.ground[id]
	g.multicast2(gi.x, gi.y, &GateState{
		Header: wire.NewHeader(opGate, wire.ClientIDSystem),
		GateID: int32(id + groundIDBase),
		Status: int16(gi.status),
		Height: int8(height),
	}, 0)
}

// setDoors is SetCastleDoor / SetColoseumDoor / SetColoseumDoor2: put n
// gates from first into a state. fixedHeight sends 18/0 like the
// coliseum versions instead of the height UpdateItem computed.
func (g *Game) setDoors(first, n, mode int, fixedHeight bool) {
	for id := first; id < first+n && id < maxGroundItem; id++ {
		gi := &g.ground[id]
		if gi.item.Index == 0 || int(gi.item.Index) >= MaxItemList || gi.status == mode {
			continue
		}
		height, _ := g.updateGroundItem(id, mode)
		if fixedHeight {
			height = 0
			if mode == gateLocked {
				height = 18
			}
		}
		g.broadcastGate(id, height)
		gi.closeArmed = false
	}
}

// requestOpenGate is CUser::RequestOpenGate (0x374): open or close a gate;
// locked gates take the matching key (EF_KEYID) from the inventory.
func (g *Game) requestOpenGate(u *User, pkt protocol.Packet) {
	var p GateState
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	pl := &g.mobs[u.id].info.Player
	if u.status != userPlay || pl.Status.CurHP == 0 {
		g.sendHpMode(u)
		return
	}
	id := int(p.GateID) - groundIDBase
	if id < 0 || id >= maxGroundItem {
		return
	}
	if id == 24 {
		g.clientMessage(u, "Monte o seu grupo e utilize o Zakum para ter acesso a quest.")
		return
	}
	gi := &g.ground[id]
	// Fixed: the C++ acted on any slot, even one with nothing on it.
	if !gi.open || gi.playerDrop {
		return
	}
	want, was := int(p.Status), gi.status
	if gi.item.Index == 773 {
		// 773 doors move first and then go through the key check below
		// (the C++ updated them twice; the second update is a no-op).
		if h, ok := g.updateGroundItem(id, want); ok {
			p.Height = int8(h)
			g.multicast2(gi.x, gi.y, &p, 0)
		}
	}
	if was == gateLocked || want == gateLocked {
		if key := g.data.itemAbility(&gi.item, EF_KEYID); key != 0 {
			found := false
			for i := range pl.Inventory {
				if g.data.itemAbility(&pl.Inventory[i], EF_KEYID) == key {
					pl.Inventory[i] = wire.Item{}
					g.sendItem(u, wire.SlotInv, i, &pl.Inventory[i])
					found = true
					break
				}
			}
			if !found {
				if gi.item.Index != 773 {
					g.clientMessage(u, "%s", g.lang(nnNoKey))
				}
				return
			}
		}
	}
	h, ok := g.updateGroundItem(id, want)
	if !ok {
		return
	}
	p.Height = int8(h)
	p.Header.ClientID = uint16(u.id)
	g.multicast2(gi.x, gi.y, &p, 0)
}

// gateTimer is the gate part of ProcessMinTimer: an open keyed gate (key
// 1..14) is armed on one minute tick and closes (locks) on the next.
func (g *Game) gateTimer() {
	for id := 20; id <= g.initCount; id++ {
		gi := &g.ground[id]
		if !gi.open || gi.status != gateOpen {
			continue
		}
		if key := g.data.itemAbility(&gi.item, EF_KEYID); key == 0 || key >= 15 {
			continue
		}
		if !gi.closeArmed {
			gi.closeArmed = true
			continue
		}
		h, _ := g.updateGroundItem(id, gateLocked)
		g.broadcastGate(id, h)
		gi.closeArmed = false
	}
}

// noDecay are the ground items ProcessDecayItem leaves alone.
func noDecay(id uint16) bool {
	switch {
	case id == 1727, id >= towerFirst && id <= towerLast, id >= 794 && id <= 798,
		id == 4922, id == 4923, id == 4143, id == 471:
		return true
	}
	return false
}

// decayGroundItems is ProcessDecayItem (every sec tick): 8 slots per tick
// round-robin past the InitItem gates; an item is marked on one pass and
// removed on the next (a few minutes on the ground).
func (g *Game) decayGroundItems() {
	for range 8 {
		g.decayCursor++
		if g.decayCursor >= maxGroundItem {
			g.decayCursor = g.initCount + 1
		}
		gi := &g.ground[g.decayCursor]
		// The C++ cursor started at 0 and only skipped the gates by
		// marking them on its first pass; here they are never touched.
		if !gi.open || g.decayCursor <= g.initCount || gi.playerDrop || noDecay(gi.item.Index) {
			continue
		}
		if !gi.decayMark {
			gi.decayMark = true
			continue
		}
		g.removeGroundItem(g.decayCursor)
	}
}

// expirePlayerDrops destroys the player drops whose time on the ground is over.
func (g *Game) expirePlayerDrops() {
	now := g.now()
	for id := g.initCount + 1; id < maxGroundItem; id++ {
		gi := &g.ground[id]
		if gi.open && gi.playerDrop && timeReached(now, gi.expireAt) {
			g.log.Info("dropped item destroyed", "item", gi.item.Index, "effects", gi.item.Effect, "owner", gi.owner, "x", gi.x, "y", gi.y)
			g.removeGroundItem(id)
		}
	}
}

// pickBlocked are items RequestPickItem never lets anyone pick up.
var pickBlocked = map[uint16]bool{1727: true, 4700: true, 4701: true, 794: true, 795: true, 796: true, 797: true, 798: true, 4703: true, 4705: true, 4704: true}

// requestPickItem is CUser::RequestPickItem (0x270).
//
// Fixed: the C++ let a player pick up the InitItem gates and the guild
// towers (only a short list was blocked); they stay on the ground now.
func (g *Game) requestPickItem(u *User, pkt protocol.Packet) {
	var p PickItem
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if u.status != userPlay || pl.Status.CurHP <= 0 || p.InvType != wire.SlotInv {
		return
	}
	id := int(p.InitID) - groundIDBase
	if id < 0 || id >= maxGroundItem {
		return
	}
	gi := &g.ground[id]
	gone := func() {
		g.queue(u, &RemoveGroundItem{Header: wire.NewHeader(opSrvRemoveItem, wire.ClientIDSystem), InitID: p.InitID})
	}
	if !gi.open {
		gone()
		return
	}
	tx, ty := int(m.target.X), int(m.target.Y)
	if tx < gi.x-3 || ty < gi.y-3 || tx > gi.x+3 || ty > gi.y+3 {
		g.log.Warn("pick item too far", "slot", u.id, "item", gi.item.Index)
		return
	}
	idx := gi.item.Index
	if pickBlocked[idx] || idx == 0 || int(idx) >= MaxItemList || id <= g.initCount || (idx >= towerFirst && idx <= towerLast) {
		return
	}
	if gi.playerDrop && !timeReached(g.now(), gi.ownerUntil) && gi.owner != g.charName(u.id) {
		g.clientMessage(u, "Este item ainda está reservado para %s.", gi.owner)
		return
	}
	switch {
	case idx == 470: // Pílula: the skill-point quest, on the spot
		if m.info.Info&questPilula != 0 {
			g.clientMessage(u, "%s", g.lang(nnYouveDoneItAlready))
			return
		}
		g.clientMessage(u, "%s", g.lang(1)) // _NN_Get_Skill_Point
		m.info.Info |= questPilula
		g.removeGroundItem(id)
		g.sendEmotion(u.id, 14, 3)
		return
	case idx >= 490 && idx <= 500:
		// The C++ took these off the ground without giving them.
		g.removeGroundItem(id)
		return
	}
	if g.itemAt(gi.x, gi.y) != id || int(p.PosX) != gi.x || int(p.PosY) != gi.y {
		gone()
		return
	}
	slot := firstCarrySlot(&pl.Inventory, idx)
	if slot < 0 {
		g.clientMessage(u, "Sem espaço no inventario")
		return
	}
	if g.data.itemAbility(&gi.item, EF_VOLATILE) != 2 { // gold is not picked up (none falls any more)
		pl.Inventory[slot] = gi.item
		g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
		g.log.Info("picked item", "slot", u.id, "item", gi.item.Index, "effects", gi.item.Effect)
	}
	g.removeGroundItem(id)
}

// guildMedal reports the items RequestDropItem refuses to drop.
func guildMedal(id uint16) bool {
	return id == 0x1FC || id == 0x1FD || id == 0x20A || (id >= 0x20E && id < 0x219) || id == 0x1BE || id == 0x2EB
}

// playerCantDrop reports the items only a GM can put on the ground: what the
// trade rules protect and what requestPickItem would never give back.
func (g *Game) playerCantDrop(it *wire.Item) bool {
	id := it.Index
	return id == 747 || id == 3993 || id == 3994 || id == 470 || (id >= 490 && id <= 500) || pickBlocked[id] ||
		(id >= towerFirst && id <= towerLast) ||
		g.data.itemAbility(it, EF_NOTRADE) != 0 || g.data.itemAbility(it, EF_VOLATILE) == 2
}

// requestDropItem is CUser::RequestDropItem (0x272). Changed from the C++,
// which was GM only: players drop from the inventory (see dropOwned).
func (g *Game) requestDropItem(u *User, pkt protocol.Packet) {
	var p DropGroundItem
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if u.status != userPlay || pl.Status.CurHP <= 0 {
		g.sendHpMode(u)
		return
	}
	if other := int(u.trade.ClientID); other > 0 && other < MaxPlayer {
		g.removeTrade(other)
		return
	}
	isPlayer := u.accessLevel == 0
	if isPlayer && p.InvType != wire.SlotInv {
		return
	}
	x, y := int(p.PosX), int(p.PosY)
	if !inMap(x, y) {
		return
	}
	if isPlayer && max(abs(x-int(m.target.X)), abs(y-int(m.target.Y))) > dropMaxDistance {
		return
	}
	if dx, dy := x, y; !g.emptyItemGrid(&dx, &dy) {
		g.clientMessage(u, "%s", g.lang(nnCantDropHere))
		return
	}
	slot := int(p.InvSlot)
	var it *wire.Item
	refreshEquip := false
	switch p.InvType {
	case wire.SlotEquip:
		if slot < 0 || slot >= 18 {
			return
		}
		it, refreshEquip = &pl.Equip[slot], slot < 8
	case wire.SlotInv:
		if slot < 0 || slot >= invSlots {
			return
		}
		it = &pl.Inventory[slot]
	case wire.SlotStorage:
		if slot < 0 || slot >= 160 {
			return
		}
		it = &u.account.StorageItems[slot]
	default:
		return
	}
	if it.Index == 0 || int(it.Index) >= MaxItemList {
		return
	}
	if guildMedal(it.Index) {
		g.clientMessage(u, "%s", g.lang(nnGuildMedalCantDrop))
		return
	}
	if isPlayer {
		if g.playerCantDrop(it) {
			g.clientMessage(u, "Este item não pode ser largado.")
			return
		}
		if g.locked(u, lockTrade, "Bloqueio de troca ativo!") {
			return
		}
	}
	if g.dropOwned(x, y, *it, int(p.Rotation), g.charName(u.id)) <= 0 {
		g.clientMessage(u, "Can't create object(item)")
		return
	}
	g.log.Info("dropped item", "slot", u.id, "gm", !isPlayer, "item", it.Index, "effects", it.Effect, "x", x, "y", y, "inv_type", p.InvType, "inv_slot", slot)
	*it = wire.Item{}
	g.queue(u, &DropItemAck{Header: wire.NewHeader(opSrvDropItem, 0xCCCC), InvType: p.InvType, InvSlot: p.InvSlot, PosX: p.PosX, PosY: p.PosY})
	// Dropping the main weapon moves the off-hand one into its place
	// (the C++ meant to; it sent the wrong packet).
	if pl.Equip[6].Index == 0 && pl.Equip[7].Index != 0 && g.data.itemAbility(&pl.Equip[7], EF_POS) != 128 {
		pl.Equip[6], pl.Equip[7] = pl.Equip[7], wire.Item{}
		g.sendItem(u, wire.SlotEquip, 6, &pl.Equip[6])
		g.sendItem(u, wire.SlotEquip, 7, &pl.Equip[7])
		refreshEquip = true
	}
	if p.InvType == wire.SlotEquip {
		if refreshEquip {
			g.sendEquip(u)
		}
		g.getCurrentScore(u.id)
		g.sendScore(u.id)
	}
	g.saveUser(u, 0) // an item on the ground must not still be in the saved character
}
