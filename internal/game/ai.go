package game

import (
	"dbsrv/internal/wire"
)

// Mob AI (ProcessTimer.cpp ProcessSecTimer mob loops, CMob.cpp
// StandingByProcessor / BattleProcessor / GetNextPos / GetTargetPos /
// SetSegment / SelectTargetFromEnemyList / GetEnemyFromView, GetFunc.cpp
// GetRoute / GetHitPosition / GetAttack).

const (
	maxEnemy = 4 // MAX_ENEMY is 13 but AddEnemyList stops at 4

	// StandingByProcessor / BattleProcessor result bits.
	aiMove       = 0x1
	aiTeleport   = 0x2
	aiReturn     = 0x10
	aiSay        = 0x10
	aiDelete     = 0x20
	aiKeepAway   = 0x100
	aiRandomMove = 0x1000
	aiAttack     = 0x1000
	aiAreaAttack = 0x2000
	aiHesitate   = 0x10000
	aiFoundEnemy = 0x10000000
)

// clanTable is g_pClanTable[mob cape][other cape]: <= 0 means hostile.
var clanTable = func() (t [9][9]int) {
	flat := []int{
		1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1,
		1, 0, 1, 1, 1, 0, 0, 1, 1, 1, 0,
		1, 1, 1, 1, 1, 1, 0, 0, 1, 1, 0,
		1, 1, 1, 1, 1, 1, 1, 1, 0, 1, 1,
		1, 0, 1, 1, 0, 0, 1, 0, 0, 0, 1,
		1, 1, 1, 1, 0, 1, 1, 1, 1, 0, 1,
		1, 1, 0, 1, 1, 0, 1, 0, 1, 1, 1,
		0, 1, 0, 1,
	}
	for i, v := range flat {
		t[i/9][i%9] = v
	}
	return t
}()

// height is g_pHeightGrid[y][x] (signed).
func (d *Data) height(x, y int) int {
	if x < 0 || y < 0 || x >= gridSize || y >= gridSize {
		return 127
	}
	return int(int8(d.Height[y*gridSize+x]))
}

// moveSpeed is GetSpeed: Status.Move.Speed clamped to 1..6.
func moveSpeed(s *wire.Status) int { return min(max(int(s.Move&0x0F), 1), 6) }

// getRoute is GetRoute: walk from (x, y) toward (*tx, *ty) over walkable
// height steps (< 8), writing direction digits. The C++ loop condition
// (i < distance || i < 23) always allows 23 steps; kept as is.
func (d *Data) getRoute(x, y int, tx, ty *int, route *[24]byte) {
	*route = [24]byte{}
	lastX, lastY := x, y
	i := 0
	near := func(h, cul int) bool { return h < cul+8 && h > cul-8 }
	for ; i < 23; i++ {
		if x < 1 || y < 1 || x > gridSize-2 || y > gridSize-2 {
			break
		}
		cul := d.height(x, y)
		nw, n, ne := d.height(x-1, y-1), d.height(x, y-1), d.height(x+1, y-1)
		w, e := d.height(x-1, y), d.height(x+1, y)
		sw, s, se := d.height(x-1, y+1), d.height(x, y+1), d.height(x+1, y+1)
		X, Y := *tx, *ty
		step := func(c byte, dx, dy int) { route[i] = c; x += dx; y += dy }
		switch {
		case X == x && Y == y:
			route[i] = 0
			goto done
		case X == x && Y < y && near(n, cul):
			step('2', 0, -1)
		case X > x && Y < y && near(ne, cul):
			step('3', 1, -1)
		case X > x && Y == y && near(e, cul):
			step('6', 1, 0)
		case X > x && Y > y && near(se, cul):
			step('9', 1, 1)
		case X == x && Y > y && near(s, cul):
			step('8', 0, 1)
		case X < x && Y > y && near(sw, cul):
			step('7', -1, 1)
		case X < x && Y == y && near(w, cul):
			step('4', -1, 0)
		case X < x && Y < y && near(nw, cul):
			step('1', -1, -1)
		case X > x && Y < y && near(e, cul):
			step('6', 1, 0)
		case X > x && Y < y && near(n, cul):
			step('2', 0, -1)
		case X > x && Y > y && near(e, cul):
			step('6', 1, 0)
		case X > x && Y > y && near(s, cul):
			step('8', 0, 1)
		case X < x && Y > y && near(w, cul):
			step('4', -1, 0)
		case X < x && Y > y && near(s, cul):
			step('8', 0, 1)
		case X < x && Y < y && near(w, cul):
			step('4', -1, 0)
		case X < x && Y < y && near(n, cul):
			step('2', 0, -1)
		case X == x+1 || Y == y+1 || X == x-1 || Y == y-1:
			route[i] = 0
			goto done
		case X == x && Y > y && near(se, cul):
			step('9', 1, 1)
		case X == x && Y > y && near(sw, cul):
			step('7', -1, 1)
		case X == x && Y < y && near(ne, cul):
			step('3', 1, -1)
		case X == x && Y < y && near(nw, cul):
			step('1', -1, -1)
		case X < x && Y == y && near(sw, cul):
			step('7', -1, 1)
		case X < x && Y == y && near(nw, cul):
			step('1', -1, -1)
		case X > x && Y == y && near(se, cul):
			step('9', 1, 1)
		case X > x && Y == y && near(ne, cul):
			step('3', 1, -1)
		default:
			goto done
		}
	}
done:
	route[i] = 0
	if lastX == x && lastY == y {
		return
	}
	*tx, *ty = x, y
}

// routeTo is the shared loop of GetNextPos/GetTargetPos: route toward
// (nx, ny), backing off to a free cell. ok=false means "stay".
func (g *Game) routeTo(id, nx, ny, steps int, freeFirst bool) (int, int, bool) {
	m := &g.mobs[id]
	lx, ly := int(m.target.X), int(m.target.Y)
	m.last = m.target
	if freeFirst {
		g.emptyMobGrid(0, &nx, &ny)
	}
	k := steps
	for ; k >= 0; k-- {
		g.data.getRoute(lx, ly, &nx, &ny, &m.route)
		if inMap(nx, ny) && g.cell(nx, ny) == 0 {
			break
		}
		if k != steps {
			g.emptyMobGrid(0, &nx, &ny)
			g.data.getRoute(lx, ly, &nx, &ny, &m.route)
			if inMap(nx, ny) && g.cell(nx, ny) == 0 {
				break
			}
		}
	}
	if k < 0 || m.route[0] == 0 {
		m.route[0] = 0
		return lx, ly, false
	}
	return nx, ny, true
}

func (m *Mob) routeSteps() int { return min(moveSpeed(&m.info.Player.Status)*8/4, 23) }

// nextPos is CMob::GetNextPos: toward the current segment point.
func (g *Game) nextPos(id int, battle bool) (int, int, bool) {
	m := &g.mobs[id]
	pl := &m.info.Player
	if pl.BStatus.Move&0x0F == 0 || pl.Equip[0].Index == 219 || pl.Equip[0].Index == 220 {
		return int(m.target.X), int(m.target.Y), false
	}
	steps := m.routeSteps()
	if battle {
		steps = min(steps, (int(pl.BStatus.STR)+1)>>1)
	} else {
		steps = min(steps, int(pl.BStatus.STR))
	}
	return g.routeTo(id, m.npc.curX, m.npc.curY, steps, false)
}

// targetPos is CMob::GetTargetPos: toward another mob.
func (g *Game) targetPos(id, target int) (int, int, bool) {
	m := &g.mobs[id]
	if m.info.Player.BStatus.Move&0x0F == 0 {
		return int(m.target.X), int(m.target.Y), false
	}
	t := g.mobs[target].target
	return g.routeTo(id, int(t.X), int(t.Y), m.routeSteps(), true)
}

// targetPosDistance is CMob::GetTargetPosDistance: step past the target
// in the direction it came from (keep-away / reposition).
func (g *Game) targetPosDistance(id, target int) (int, int, bool) {
	m := &g.mobs[id]
	if m.info.Player.BStatus.Move&0x0F == 0 {
		return int(m.target.X), int(m.target.Y), false
	}
	t := &g.mobs[target]
	nx, ny := int(t.target.X), int(t.target.Y)
	switch {
	case int(t.last.X) > nx:
		nx -= 1 + rnd()%2
	case int(t.last.X) < nx:
		nx += 1 + rnd()%2
	}
	switch {
	case int(t.last.Y) > ny:
		ny -= 1 + rnd()%2
	case int(t.last.Y) < ny:
		ny += 1 + rnd()%2
	}
	return g.routeTo(id, nx, ny, m.routeSteps(), true)
}

// randomPos is CMob::GetRandomPos: wander up to 3 cells.
func (g *Game) randomPos(id int) (int, int, bool) {
	m := &g.mobs[id]
	if m.info.Player.BStatus.Move&0x0F == 0 {
		return int(m.target.X), int(m.target.Y), false
	}
	nx, ny := int(m.target.X)+rnd()%7-3, int(m.target.Y)+rnd()%7-3
	return g.routeTo(id, nx, ny, m.routeSteps(), true)
}

// setSegment is CMob::SetSegment: advance along the generator route.
// Returns 1 = arrived for good (route type 0), 2 = remove the mob.
func (g *Game) setSegment(id int) int {
	m := &g.mobs[id]
	n := &m.npc
	if n.routeType == 6 {
		n.progress, n.direction = 0, 0
		n.curX, n.curY, n.waitSec = n.segX[0], n.segY[0], 0
		return 0
	}
	if n.routeType < 0 || n.routeType > 4 {
		return 0
	}
	ret := 0
loop:
	for range 1000 {
		if n.direction == 0 {
			n.progress++
		} else {
			n.progress--
		}
		switch {
		case n.progress <= -1:
			switch n.routeType {
			case 0:
				n.progress, n.direction, ret = 0, 0, 2
			case 1, 2:
				n.progress, n.direction = 0, 0
			case 3:
				ret = 2
			}
			break loop
		case n.progress >= 5:
			switch n.routeType {
			case 0:
				n.progress = 4
				m.mode = modeNPCIdle
				pl := &m.info.Player
				pl.BStatus.Merchant = pl.BStatus.Merchant&0xF0 | pl.Info&0x0F
				if l := routeLen(&m.route); l > 0 {
					if dir := m.route[l-1] - '0'; dir >= 1 && dir <= 9 {
						pl.BStatus.Merchant |= dir << 4
					}
				}
				g.getCurrentScore(id)
				ret = 1
			case 1:
				ret = 2
			case 2, 3:
				n.progress, n.direction = 4, 1
			case 4:
				n.progress = -1
			}
			break loop
		default:
			if n.segX[n.progress] == 0 {
				continue
			}
			break loop
		}
	}
	if n.progress >= 0 && n.progress < 5 {
		n.curX, n.curY = n.segX[n.progress], n.segY[n.progress]
	}
	n.waitSec = 0
	return ret
}

func routeLen(r *[24]byte) int {
	n := 0
	for n < len(r) && r[n] != 0 {
		n++
	}
	return n
}

// enemyFromView is CMob::GetEnemyFromView: the first hostile mob in a
// 9x9 box (16x16 for kingdom guards).
func (g *Game) enemyFromView(id int) int {
	m := &g.mobs[id]
	pl := &m.info.Player
	w, h := 9, 9
	x0, y0 := int(m.target.X)-4, int(m.target.Y)-4
	if pl.CapeInfo == 7 || pl.CapeInfo == 8 {
		w, h = 16, 16
		x0, y0 = int(m.target.X)-6, int(m.target.Y)-6
	}
	for y := y0; y < y0+h; y++ {
		for x := x0; x < x0+w; x++ {
			// The C++ skipped the whole row and column of the mob
			// (x == X || y == Y), so players in line with it were never
			// seen; only the mob's own cell is skipped here.
			if (x == int(m.target.X) && y == int(m.target.Y)) || !inMap(x, y) {
				continue
			}
			other := int(g.cell(x, y))
			if other == 0 || other >= MaxMob {
				continue
			}
			o := &g.mobs[other]
			if o.info.Player.Status.CurHP == 0 || o.mode == 0 {
				continue
			}
			if other < MaxPlayer && (o.info.AffectInfo&0x40 != 0 || pl.Info&1 != 0) {
				continue
			}
			mc, oc := int(pl.CapeInfo), int(o.info.Player.CapeInfo)
			if mc < 0 || mc >= 9 || oc < 0 || oc >= 9 {
				return 0
			}
			if clanTable[mc][oc] <= 0 {
				return other
			}
		}
	}
	return 0
}

// selectTarget is CMob::SelectTargetFromEnemyList: the closest valid enemy.
func (g *Game) selectTarget(id int) {
	m := &g.mobs[id]
	c := &m.combat
	c.currentTarget = 0
	rng := 6
	if cape := m.info.Player.CapeInfo; cape == 4 || cape == 7 || cape == 8 || m.info.Player.Equip[0].Index == 362 {
		rng = 12
	}
	if m.target.X>>7 < 12 && int(m.target.Y)<<7 > 25 {
		rng = 8
	}
	best, bestDist := 0, MaxPlayer
	for i, e := range c.enemies {
		if e <= 0 || e >= MaxMob {
			continue
		}
		o := &g.mobs[e]
		drop := o.mode == 0 || o.info.Player.Status.CurHP == 0
		if !drop && e < MaxPlayer {
			drop = o.info.AffectInfo&0x40 != 0 || (o.info.Player.Status.Level >= 400 && o.info.Player.AffectInfo&affSlowMov != 0)
		}
		if !drop && (int(o.target.X) < int(m.target.X)-rng || int(o.target.X) > int(m.target.X)+rng ||
			int(o.target.Y) < int(m.target.Y)-rng || int(o.target.Y) > int(m.target.Y)+rng) {
			drop = true
		}
		if drop {
			c.enemies[i] = 0
			continue
		}
		d := distance(int(m.target.X), int(m.target.Y), int(o.target.X), int(o.target.Y))
		if e >= MaxPlayer {
			d += 2
		}
		if d < bestDist {
			best, bestDist = e, d
		}
	}
	c.currentTarget = best
}

func (m *Mob) removeEnemy(id int) {
	for i, e := range m.combat.enemies {
		if e == id {
			m.combat.enemies[i] = 0
			return
		}
	}
}

// standingBy is CMob::StandingByProcessor.
func (g *Game) standingBy(id int) int {
	m := &g.mobs[id]
	n := &m.npc
	if n.routeType == 5 {
		return g.summonStandingBy(id)
	}
	ai := &g.cfg.MobAI
	if n.leader == 0 || ai.FollowersLookForPlayers {
		near := int(m.target.X) < n.curX+12 && int(m.target.X) > n.curX-12 &&
			int(m.target.Y) < n.curY+12 && int(m.target.Y) > n.curY-12
		if e := g.enemyFromView(id); e != 0 && (near || ai.AggroWhileWalking) {
			return e | aiFoundEnemy
		}
	}
	if n.routeType == 6 && int(m.target.X) == n.curX && int(m.target.Y) == n.curY {
		return 0
	}
	if n.curX == int(m.target.X) && n.curY == int(m.target.Y) {
		// Waits are counted in the C++ units (6 per 3 s idle turn).
		step := ticks(ai.IdleThinkSeconds)
		if n.waitSec > 0 && n.waitSec != step {
			n.waitSec -= step
			if n.waitSec > 0 {
				if n.routeType == 0 && int(m.target.X) == n.segX[0] && int(m.target.Y) == n.segY[0] {
					return 0
				}
				if m.info.Player.Status.Move&0x0F == 0 {
					return 0
				}
				return aiRandomMove
			}
			n.waitSec = 0
		} else if n.progress >= 0 && n.progress < 5 && n.segWait[n.progress] > 0 {
			n.waitSec = n.segWait[n.progress]
			return 0
		}
		switch g.setSegment(id) {
		case 1:
			return 0
		case 2:
			return aiKeepAway // 256: delete
		}
	}
	nx, ny, ok := g.nextPos(id, false)
	if !ok || (nx == int(m.target.X) && ny == int(m.target.Y)) {
		g.setSegment(id)
		return 0
	}
	n.nextX, n.nextY = nx, ny
	return aiMove
}

// battle is CMob::BattleProcessor.
func (g *Game) battle(id int) int {
	m := &g.mobs[id]
	pl := &m.info.Player
	if pl.Equip[0].Index == 362 {
		return aiAreaAttack
	}
	g.selectTarget(id)
	tgt := m.combat.currentTarget
	if tgt == 0 {
		m.mode = modeNPCIdle
		return 0
	}
	if m.npc.routeType == 5 {
		if r := g.summonBattle(id); r != 0 {
			return r
		}
	}
	if max(int(pl.BStatus.INT), g.cfg.MobAI.MinAttackChance) < rnd()%100 {
		return aiHesitate
	}
	tx, ty := int(g.mobs[tgt].target.X), int(g.mobs[tgt].target.Y)
	rng := g.data.mobAbility(pl, EF_RANGE)
	dist := distance(int(m.target.X), int(m.target.Y), tx, ty)
	n := &m.npc
	if n.routeType != 5 && (int(m.target.X) > n.curX+30 || int(m.target.X) < n.curX-30 ||
		int(m.target.Y) > n.curY+30 || int(m.target.Y) < n.curY-30) {
		m.combat.currentTarget = 0
		m.combat.enemies = [4]int{}
		m.mode = modeNPCIdle
		nx, ny, ok := g.nextPos(id, true)
		if !ok || (nx == int(m.target.X) && ny == int(m.target.Y)) {
			return 0
		}
		n.nextX, n.nextY = nx, ny
		return aiReturn
	}
	if dist <= rng {
		if rng >= 4 && dist <= 2 && rnd()%100 > int(pl.BStatus.DEX) {
			return aiKeepAway
		}
		hx, hy := tx, ty
		g.hitPosition(int(m.target.X), int(m.target.Y), &hx, &hy)
		if hx != tx || hy != ty {
			return aiKeepAway
		}
		return aiAttack
	}
	return aiMove
}

// hitPosition is GetHitPosition: where a straight shot from (x, y) toward
// (*tx, *ty) is stopped by terrain; (0, 0) when it hits a wall.
func (g *Game) hitPosition(x, y int, tx, ty *int) {
	if (x == *tx && y == *ty) || x == 0 || y == 0 || *tx == 0 || *ty == 0 {
		return
	}
	dx, dy := abs(x-*tx), abs(y-*ty)
	if distance(x, y, *tx, *ty) <= 1 {
		return
	}
	h := g.data.height
	if dx > dy {
		if *tx == x {
			return
		}
		slope := (*ty - y) * 1000 / (*tx - x)
		base := y*1000 - slope*x
		dir := -1
		if x < *tx {
			dir = 1
		}
		x += dir
		if dx == dy {
			if y < *ty {
				y++
			} else {
				y--
			}
		}
		cur := h(x, (slope*x+base)/1000)
		if cur == 127 {
			*tx, *ty = 0, 0
			return
		}
		left := dx
		for cx := x; cx != *tx; cx += dir {
			if cx == x {
				if left--; left < 1 {
					break
				}
				continue
			}
			cy := (slope*cx + base) / 1000
			prev := cur
			cur = h(cx, cy)
			if cur == 127 {
				*tx, *ty = 0, 0
				return
			}
			if cur > prev+8 || cur < prev-8 {
				*tx, *ty = cx, cy
				return
			}
			if left--; left < 1 {
				break
			}
		}
		return
	}
	if *ty == y {
		return
	}
	slope := (*tx - x) * 1000 / (*ty - y)
	base := x*1000 - slope*y
	dir := -1
	if y < *ty {
		dir = 1
	}
	y += dir
	if dx == dy {
		if x < *tx {
			x++
		} else {
			x--
		}
	}
	cur := h((slope*y+base)/1000, y)
	if cur == 127 {
		*tx, *ty = 0, 0
		return
	}
	left := dy
	for cy := y; cy != *ty; cy += dir {
		if cy == y {
			if left--; left < 1 {
				break
			}
			continue
		}
		cx := (slope*cy + base) / 1000
		prev := cur
		cur = h(cx, cy)
		if cur == 127 {
			*tx, *ty = 0, 0
			return
		}
		if cur > prev+8 || cur < prev-8 {
			*tx, *ty = cx, cy
			return
		}
		if left--; left < 1 {
			break
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// moveMob sends a mob walking to (x, y) (GetAction + GridMulticast).
func (g *Game) moveMob(id, x, y int, keepRoute bool) {
	m := &g.mobs[id]
	if !keepRoute {
		m.route[0] = 0
	}
	p := g.action(id, x, y)
	p.MoveSpeed = uint32(moveSpeed(&m.info.Player.Status))
	g.multicastMove(id, x, y, &p)
}

// wakeGroup is the SetBattle fan-out over a mob group (PartyList).
func (g *Game) wakeGroup(id, enemy int) {
	leader := id
	if l := g.mobs[id].npc.leader; l != 0 {
		leader = l
	}
	if leader < MaxPlayer {
		return
	}
	for i, f := range g.mobs[leader].npc.followers {
		if f < MaxPlayer {
			continue
		}
		if g.mobs[f].mode == 0 || g.mobs[f].info.Player.Status.CurHP == 0 {
			g.mobs[leader].npc.followers[i] = 0
			continue
		}
		g.setBattle(f, enemy)
	}
	g.setBattle(leader, enemy)
}

// mobTimer is the mob part of ProcessSecTimer: idle mobs every 6 ticks,
// fighting mobs every 4 ticks (each id on its own phase); tmsrv.yaml can
// change both.
func (g *Game) mobTimer() {
	counter := g.secCounter
	idle, fight := ticks(g.cfg.MobAI.IdleThinkSeconds), ticks(g.cfg.MobAI.BattleThinkSeconds)
	for id := MaxPlayer + counter%idle; id < MaxMob; id += idle {
		m := &g.mobs[id]
		if m.mode != modeNPCIdle {
			continue
		}
		if m.info.Player.Status.CurHP == 0 {
			g.deleteMob(id, 1)
			continue
		}
		g.processAffect(id)
		before := m.npc.progress
		r := g.standingBy(id)
		if r&aiFoundEnemy != 0 {
			enemy := r & 0x0FFFFFFF
			g.wakeGroup(id, enemy)
			if enemy < MaxPlayer { // the player's summons answer
				g.summonsFight(enemy, id, false)
			}
			continue
		}
		if r&aiMove != 0 {
			g.moveMob(id, m.npc.nextX, m.npc.nextY, true)
			if p := m.npc.progress; p != before && p >= 0 && p < 5 {
				g.mobSay(id, p)
			}
		}
		if r&aiKeepAway != 0 {
			g.deleteMob(id, 3)
			continue
		}
		if r&aiRandomMove != 0 {
			if nx, ny, ok := g.randomPos(id); ok && (nx != int(m.target.X) || ny != int(m.target.Y)) {
				g.moveMob(id, nx, ny, true)
			}
		}
		if r&aiTeleport != 0 {
			g.teleportar(id, m.npc.nextX, m.npc.nextY)
		}
	}
	for id := MaxPlayer + counter%fight; id < MaxMob; id += fight {
		m := &g.mobs[id]
		if m.mode != modeBattle {
			continue
		}
		if m.info.Player.Status.CurHP == 0 {
			g.deleteMob(id, 1)
			continue
		}
		if counter%fight == counter%(fight*2) {
			g.processAffect(id)
		}
		if t := m.combat.currentTarget; t > 0 && t < MaxMob {
			lead, tlead := id, t
			if m.npc.leader != 0 {
				lead = m.npc.leader
			}
			if g.mobs[t].npc.leader != 0 {
				tlead = g.mobs[t].npc.leader
			}
			gi, tgi := int(m.info.Player.GuildIndex), int(g.mobs[t].info.Player.GuildIndex)
			if gi == 0 && tgi == 0 {
				gi = -1
			}
			if lead == tlead || gi == tgi {
				m.removeEnemy(t)
			}
		}
		r := g.battle(id)
		switch {
		case r&aiDelete != 0:
			g.deleteMob(id, 35)
			continue
		case r&aiAreaAttack != 0:
			continue // TODO(phase E): Kefra area attack
		}
		if r&aiAttack != 0 {
			g.mobAttack(id)
		}
		if r&aiKeepAway != 0 {
			if nx, ny, ok := g.targetPosDistance(id, m.combat.currentTarget); ok && (nx != int(m.target.X) || ny != int(m.target.Y)) {
				g.moveMob(id, nx, ny, false)
			}
		}
		if r&aiMove != 0 && r&aiHesitate == 0 {
			g.getCurrentScore(id)
			if t := m.combat.currentTarget; t != 0 {
				if nx, ny, ok := g.targetPos(id, t); ok && (nx != int(m.target.X) || ny != int(m.target.Y)) {
					g.moveMob(id, nx, ny, true)
				}
			}
		}
		if r&aiTeleport != 0 {
			g.teleportar(id, m.npc.nextX, m.npc.nextY)
		}
		if r&aiReturn != 0 {
			g.moveMob(id, m.npc.nextX, m.npc.nextY, true)
		}
	}
}

// mobSay is SendSay for a generator segment action line.
func (g *Game) mobSay(id, progress int) {
	gid := g.mobs[id].npc.generateID
	if gid < 0 || gid >= len(g.data.Geners) {
		return
	}
	text := g.data.Geners[gid].SegmentAction[progress]
	if text == "" {
		return
	}
	p := wire.Chat{Header: wire.NewHeader(wire.OpCliChat, uint16(id))}
	copy(p.Text[:len(p.Text)-1], text)
	t := g.mobs[id].target
	g.multicast2(int(t.X), int(t.Y), &p, 0)
}

// mobAttack is the 0x1000 branch of the battle loop: GetAttack, parry,
// damage, relay, death.
func (g *Game) mobAttack(id int) {
	m := &g.mobs[id]
	tgt := m.combat.currentTarget
	if tgt <= 0 || tgt >= MaxMob {
		return
	}
	p := g.getAttack(id, tgt)
	tgt = int(p.Target[0].Index) & 0xFFFF
	if tgt <= 0 || tgt >= MaxMob {
		return
	}
	if sk := int(p.SkillID); sk >= 0 && sk < 96 && p.SkillParm == 0 {
		if sk == 33 {
			p.Motion = 0xFD
		}
		mastery := int(m.info.Player.Status.Mastery[(sk%24)>>3+1])
		a := g.setAffect(tgt, sk, 100, mastery)
		t := g.setTick(tgt, sk, 100, mastery)
		if a || t {
			g.sendScore(tgt)
		}
		g.sendAffect(g.player(tgt))
	}
	t := &g.mobs[tgt]
	dam := int(p.Target[0].Damage)
	if tgt < MaxPlayer && dam > 0 {
		if rnd()%1000 < g.parryRate(id, tgt, -2) {
			dam = -3
			if t.info.Player.AffectInfo&affResist != 0 {
				dam = -4
			}
		}
	}
	if tgt < MaxPlayer && dam > 0 {
		if m.info.Player.CapeInfo == 4 { // summons hit players at 40%
			dam = (dam << 1) / 5
		}
		left, shielded, _ := g.manaShield(tgt, dam)
		dam = left
		if tu := g.player(tgt); shielded && tu != nil {
			g.sendSetHpMp(tu)
		}
	}
	if dam > 0 {
		lead := tgt
		if t.npc.leader != 0 {
			lead = t.npc.leader
		}
		g.setBattle(lead, id)
		if m.info.Player.CapeInfo != 4 {
			g.setBattle(id, lead)
		}
		if lead < MaxPlayer { // the victim's (party's) summons join in
			g.summonsFight(lead, id, true)
		}
	}
	petDamage := 0
	if tgt < MaxPlayer && dam > 0 && petAlive(&t.info.Player) && t.info.Player.Equip[14].Index >= 2360 && t.info.Player.Equip[14].Index <= 2389 {
		left := absorbByPet(&t.info.Player, dam)
		petDamage, dam = dam-left, left
	}
	if dam > 0 || dam <= -5 {
		if hp := t.info.Player.Equip[13].Index; hp == 786 || hp == 1936 || hp == 1937 {
			mult := max(itemSanc(&t.info.Player.Equip[13]), 2)
			switch hp {
			case 1936:
				mult *= 10
			case 1937:
				mult *= 1000
			}
			t.info.Player.Status.CurHP -= int32(dam / mult)
		} else {
			if dam > int(t.info.Player.Status.CurHP) {
				dam = int(t.info.Player.Status.CurHP)
			}
			t.info.Player.Status.CurHP -= int32(dam)
		}
		if t.info.Player.Status.CurHP > t.info.Player.Status.MaxHP {
			t.info.Player.Status.CurHP = t.info.Player.Status.MaxHP
		}
		if tgt >= MaxPlayer {
			g.linkMountHP(tgt)
		}
		if petDamage > 0 {
			g.processAdultMount(tgt, petDamage)
		}
	}
	p.Target[0].Damage = int32(dam)
	raw, err := wire.Marshal(&p)
	if err != nil {
		return
	}
	g.multicastRaw(int(t.target.X), int(t.target.Y), raw[:attackSize1])
	if t.info.Player.Status.CurHP <= 0 {
		t.info.Player.Status.CurHP = 0
		g.mobKilled(tgt, id)
	}
}

// getAttack is GetAttack for a mob attacker: skill/motion from its
// masteries and skill bar, damage from GetDamage and the target's resist.
func (g *Game) getAttack(id, tgt int) wire.Attack {
	m, t := &g.mobs[id], &g.mobs[tgt]
	pl := &m.info.Player
	p := wire.Attack{Header: wire.NewHeader(wire.OpCliAttackOne, wire.ClientIDCharList)}
	p.Header.Size = attackSize1
	p.Header.TimeStamp = g.now()
	p.AttackerID = int16(id)
	p.AttackerPos, p.TargetPos = m.target, t.target
	p.CurrentMP, p.CurrentExp = -1, -1
	p.Target[0].Index = int32(tgt)
	p.SkillID = -1
	resistIdx := -1
	dist := distance(int(m.target.X), int(m.target.Y), int(t.target.X), int(t.target.Y))
	a, b := int(pl.Status.Mastery[0]), int(pl.Status.Mastery[1])
	if dist >= 3 {
		a, b = int(pl.Status.Mastery[2]), int(pl.Status.Mastery[3])
	}
	if a&0xFF == 0xFF {
		a = 0xFF
	}
	switch a {
	case 0x79:
		p.SkillID, p.SkillParm = 103, 5
	case 0x7A:
		p.SkillID, p.SkillParm = 0x69, 1
	case 0x7B:
		p.SkillID, p.SkillParm = 0x65, 1
	case 0x7C:
		p.SkillID, p.SkillParm = 0x65, 2
	case 0x7D:
		p.SkillID, p.SkillParm = 0x28, 1
	case 0x7E:
		p.SkillID, p.SkillParm = 0x28, 2
	case 0x7F:
		p.SkillID, p.SkillParm = 0x28, 3
	case 0x80:
		p.SkillID, p.SkillParm, p.Motion = 0x21, 0xFC, 0xFC
	default:
		p.SkillID = int16(a)
	}
	if b != 0 {
		p.Motion = 4
		r := rnd() & 3
		switch b {
		case 3:
			if r <= 1 {
				p.Motion++
			}
		case 6:
			if r <= 1 {
				p.Motion++
			} else {
				p.Motion += 2
			}
		case 7:
			if r == 1 {
				p.Motion++
			} else {
				p.Motion += 2
			}
		case 15:
			p.Motion += uint8(r)
		case 0x18:
			if r <= 1 {
				p.Motion += 3
			} else {
				p.Motion += 4
			}
		case 0x1B:
			switch r {
			case 1:
				p.Motion++
			case 2:
				p.Motion += 3
			case 3:
				p.Motion += 4
			}
		case 0x17:
			switch r {
			case 1:
				p.Motion++
			case 2:
				p.Motion += 2
			case 3:
				p.Motion += 4
			}
		}
	}
	bar := pl.SkillBar1
	roll := rnd() % 100
	skill := func(s uint8) int { return g.data.Skills[s].InstanceType }
	if bar[3] != 0xFF && roll >= 25 && roll <= 40 && skill(bar[3]) == 6 {
		// Heal: self or the group leader, whichever is lower.
		lead := id
		if m.npc.leader != 0 {
			lead = m.npc.leader
		}
		mine := int(pl.Status.CurHP) * 10 / max(int(pl.Status.MaxHP), 1)
		theirs := int(g.mobs[lead].info.Player.Status.CurHP) * 10 / max(int(pl.Status.MaxHP), 1)
		if mine <= 8 && theirs <= 8 {
			p.SkillID = int16(bar[3])
			who := id
			if mine > theirs {
				who = lead
			}
			p.Target[0] = wire.AttackTarget{Index: int32(who), Damage: -(g.mobs[who].info.Player.Status.CurHP / 10)}
			return p
		}
	}
	switch {
	case bar[0] != 0xFF && roll <= 25:
		p.SkillID, resistIdx = int16(bar[0]), skill(bar[0])-2
	case bar[1] != 0xFF && roll >= 26 && roll <= 50:
		p.SkillID, resistIdx = int16(bar[1]), skill(bar[1])-2
	case bar[2] != 0xFF && roll >= 51 && roll <= 75:
		p.SkillID, resistIdx = int16(bar[2]), skill(bar[2])-2
	}
	dam := getDamage(int(pl.Status.Attack), int(t.info.Player.Status.Defense), 0)
	if resistIdx >= 0 && resistIdx <= 3 {
		dam = dam * (100 - int(t.info.Player.Resist[resistIdx])) / 100
	}
	if dam > 0 && tgt < MaxPlayer && t.combat.reflectDamage > 0 {
		dam = max(dam-t.combat.reflectDamage, 0)
	}
	if dam > 0 {
		lead, tlead := id, tgt
		if m.npc.leader != 0 {
			lead = m.npc.leader
		}
		if t.npc.leader != 0 {
			tlead = t.npc.leader
		}
		gi, tgi := int(pl.GuildIndex), int(t.info.Player.GuildIndex)
		if gi == 0 && tgi == 0 {
			gi = -1
		}
		if tgt < MaxPlayer && t.guildDisable {
			tgi = -2
		}
		if lead == tlead || gi == tgi {
			dam = 0
			m.combat.currentTarget = 0
		}
	}
	p.Target[0].Damage = int32(dam)
	return p
}

// parryRate is GetParryRate(attacker, defender, type).
func (g *Game) parryRate(att, def, typ int) int {
	a, d := &g.mobs[att], &g.mobs[def]
	dex := int(a.info.Player.Status.DEX) / 5
	if a.info.Player.AffectInfo&affEvasion != 0 {
		dex += 150
	}
	if typ == -2 {
		dex += 300
	}
	if a.info.Player.AffectInfo&affSpeedMov != 0 {
		dex += 50
	}
	td := int(d.info.Player.Status.DEX)
	c1 := min(max(td-700, 0), 1000)
	c2 := max(td-2000, 0)
	parry := min(td, 700)>>1 + c1>>2 + c2>>3 - dex
	esfera := [20]int{0, 10, 0, 0, 10, 0, 0, 10, 0, 16, 10, 0, 0, 60, 60, 50, 60, 0, 0, 0}
	if e := d.info.Player.Equip[14].Index; e >= 3980 && e <= 3999 {
		parry += esfera[e-3980]
	}
	if e := a.info.Player.Equip[14].Index; e >= 3980 && e <= 3999 {
		parry += esfera[e-3980]
	}
	parry = min(parry, 750)
	learn := a.info.Player.Learn[0]
	if a.info.Player.ClassInfo == 1 && learn&(1<<(47%24)) != 0 && typ == -2 {
		parry -= 50
	}
	if a.info.Player.ClassInfo == 0 && (learn&(1<<7) != 0 || learn&(1<<23) != 0) && typ == -1 {
		parry -= 75
	}
	parry += d.combat.parry
	if learn&(1<<28) != 0 {
		parry -= 75
	}
	parry -= a.combat.hitRate
	return max(parry, 1)
}

// regenTimer is RegenMob, each player every 16 ticks: mount hunger and
// incubation (mountTimer), the guilty mark, HP/MP regen and affects.
func (g *Game) regenTimer() {
	phase := g.secCounter % 16
	for id := phase; id < MaxPlayer; id += 16 {
		u := g.users[id]
		if id == 0 || u.status != userPlay {
			continue
		}
		g.mountTimer(u)
		m := &g.mobs[id]
		s := &m.info.Player.Status
		if m.mode == 0 || s.CurHP == 0 {
			continue
		}
		if gl := g.guilty(id); gl > 0 { // the guilty mark wears off
			setGuilty(&m.info.Player, gl-1)
			if gl == 1 {
				p := g.createMobPacket(id)
				g.multicast2(int(m.target.X), int(m.target.Y), &p, 0)
			}
		}
		base := 100
		attr := g.data.attribute(int(m.target.X), int(m.target.Y))
		if attr&attrVillage != 0 {
			base = 500
		}
		if attr&attrGuild != 0 {
			base = 1000
		}
		s.CurHP = min(s.CurHP+int32((base+m.combat.regenHP)*5), s.MaxHP)
		s.CurMP = min(s.CurMP+int32((base+m.combat.regenMP)*5), s.MaxMP)
		g.sendSetHpMp(u)
		g.processAffect(id)
	}
}

// requestResurrect is RequestRessuctPlayer (0x289): back to town with 2 HP.
func (g *Game) requestResurrect(u *User) {
	m := &g.mobs[u.id]
	if u.status != userPlay || m.info.Player.Status.CurHP > 0 {
		g.sendHpMode(u)
		return
	}
	m.info.Player.Status.CurHP = 2
	g.sendScore(u.id)
	g.sendSetHpMp(u)
	g.doRecall(u.id)
	g.sendEtc(u)
}
