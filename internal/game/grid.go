package game

import (
	"dbsrv/internal/wire"
)

// The mob grid (g_pMobGrid) and visibility (Basedef.cpp GridMulticast*,
// SendFunc.cpp SendGridMob, GetFunc.cpp GetEmptyMobGrid).

func (g *Game) cell(x, y int) uint16 { return g.grid[y*gridSize+x] }

func (g *Game) setCell(x, y int, id uint16) { g.grid[y*gridSize+x] = id }

func inMap(x, y int) bool { return x >= 0 && y >= 0 && x < gridSize && y < gridSize }

// clearGrid frees the cell a player stands on, if it is still theirs.
func (g *Game) clearGrid(id int) {
	t := g.mobs[id].target
	if inMap(int(t.X), int(t.Y)) && g.cell(int(t.X), int(t.Y)) == uint16(id) {
		g.setCell(int(t.X), int(t.Y), 0)
	}
}

// viewRect returns the 33x33 view window around (x, y), clipped to the map
// the way the C++ computed it.
func viewRect(x, y int) (minX, minY, maxX, maxY int) {
	visX, visY := viewGrid, viewGrid
	minX, minY = x-halfGrid, y-halfGrid
	if minX+visX >= gridSize {
		visX -= visX + minX - gridSize
	}
	if minY+visY >= gridSize {
		visY -= visY + minY - gridSize
	}
	if minX < 0 {
		minX = 0
	}
	if minY < 0 {
		minY = 0
	}
	return minX, minY, minX + visX, minY + visY
}

// playerAt returns the playing user standing in a cell, or nil.
func (g *Game) playerAt(x, y int) *User {
	id := int(g.cell(x, y))
	if id <= 0 || id >= MaxPlayer {
		return nil
	}
	if g.mobs[id].mode == 0 {
		return nil
	}
	return g.users[id]
}

// multicast2 is GridMulticast_2: send v to every player in view of (x, y)
// except `except` (0 = nobody excluded, so the sender gets it too).
func (g *Game) multicast2(x, y int, v any, except int) {
	b, err := wire.Marshal(v)
	if err != nil {
		g.log.Error("marshal", "err", err)
		return
	}
	minX, minY, maxX, maxY := viewRect(x, y)
	for ny := minY; ny < maxY; ny++ {
		for nx := minX; nx < maxX; nx++ {
			id := int(g.cell(nx, ny))
			if id <= 0 || id == except || id >= MaxPlayer || g.mobs[id].mode == 0 {
				continue
			}
			g.queueRaw(g.users[id], b)
		}
	}
}

// sendGridMob is SendGridMob: exchange spawn packets with everyone in view.
func (g *Game) sendGridMob(id int) {
	t := g.mobs[id].target
	minX, minY, maxX, maxY := viewRect(int(t.X), int(t.Y))
	for ny := minY; ny < maxY; ny++ {
		for nx := minX; nx < maxX; nx++ {
			other := int(g.cell(nx, ny))
			if other == 0 || other == id || other >= MaxMob || g.mobs[other].mode == 0 {
				continue
			}
			g.sendCreateMob(g.player(other), id)
			g.sendCreateMob(g.player(id), other)
		}
	}
	if u := g.player(id); u != nil && u.status == userPlay {
		g.sendGroundItemsInView(u, int(t.X), int(t.Y))
	}
}

// emptyMobGrid is GetEmptyMobGrid: the cell itself if free and walkable,
// else the nearest one within 5 cells.
func (g *Game) emptyMobGrid(id int, x, y *int) bool {
	if !inMap(*x, *y) {
		return false
	}
	cur := int(g.cell(*x, *y))
	if cur == id {
		return true
	}
	if cur >= MaxMob || g.mobs[cur].mode == 0 {
		g.setCell(*x, *y, 0)
		cur = 0
	}
	if cur == 0 && !g.data.Blocked(*x, *y) {
		return true
	}
	for r := 1; r <= 5; r++ {
		for ny := *y - r; ny <= *y+r; ny++ {
			for nx := *x - r; nx <= *x+r; nx++ {
				if !inMap(nx, ny) {
					continue
				}
				if g.cell(nx, ny) == 0 && !g.data.Blocked(nx, ny) {
					*x, *y = nx, ny
					return true
				}
			}
		}
	}
	return false
}

// inHalf is GetInHalf: within half a view window.
func (g *Game) inHalf(a, b int) bool {
	ta, tb := g.mobs[a].target, g.mobs[b].target
	dx, dy := int(ta.X)-int(tb.X), int(ta.Y)-int(tb.Y)
	return dx >= -halfGrid && dx <= halfGrid && dy >= -halfGrid && dy <= halfGrid
}

// multicastMove is GridMulticast(Index, posX, posY, buf): move a player to
// (x, y), relay the movement packet to everyone who sees either position,
// and send spawn/remove packets to those entering or leaving the view.
func (g *Game) multicastMove(id int, x, y int, move *wire.Action) {
	m := &g.mobs[id]
	if m.target.X == 0 {
		return
	}
	if g.cell(int(m.target.X), int(m.target.Y)) == uint16(id) {
		g.setCell(int(m.target.X), int(m.target.Y), 0)
	}
	if c := g.cell(x, y); c != uint16(id) && c != 0 {
		g.emptyMobGrid(id, &x, &y)
	}
	g.setCell(x, y, uint16(id))

	raw, _ := wire.Marshal(move)
	minX, minY, maxX, maxY := viewRect(int(m.target.X), int(m.target.Y))
	dminX, dminY, dmaxX, dmaxY := viewRect(x, y)
	inNew := func(nx, ny int) bool { return nx >= dminX && nx < dmaxX && ny >= dminY && ny < dmaxY }
	inOld := func(nx, ny int) bool { return nx >= minX && nx < maxX && ny >= minY && ny < maxY }

	for ny := minY; ny < maxY; ny++ {
		for nx := minX; nx < maxX; nx++ {
			other := int(g.cell(nx, ny))
			if other <= 0 || other == id || other >= MaxMob {
				continue
			}
			if u := g.player(other); u != nil {
				g.queueRaw(u, raw)
			}
			if !inNew(nx, ny) {
				g.sendRemoveMob(g.player(other), id, 0)
				g.sendRemoveMob(g.player(id), other, 0)
			}
		}
	}
	for ny := dminY; ny < dmaxY; ny++ {
		for nx := dminX; nx < dmaxX; nx++ {
			if inOld(nx, ny) {
				continue
			}
			other := int(g.cell(nx, ny))
			if other <= 0 || other == id || other >= MaxMob {
				continue
			}
			if g.mobs[other].mode == 0 {
				g.setCell(nx, ny, 0)
				continue
			}
			g.sendCreateMob(g.player(id), other)
			g.sendCreateMob(g.player(other), id)
			if u := g.player(other); u != nil {
				g.queueRaw(u, raw)
			}
		}
	}

	g.groundItemsMoved(g.player(id), int(m.target.X), int(m.target.Y), x, y)

	m.lastTime = move.Header.TimeStamp
	m.lastSpeed = move.MoveSpeed
	m.last = move.LastPos
	m.target = wire.Position{X: uint16(x), Y: uint16(y)}
}

// player returns the user of a mob id, nil for NPCs and empty slots.
func (g *Game) player(id int) *User {
	if id <= 0 || id >= MaxPlayer {
		return nil
	}
	return g.users[id]
}
