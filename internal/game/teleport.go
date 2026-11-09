package game

import "dbsrv/internal/wire"

const (
	nnOnlySameKingdom       = 282
	nnNotEnoughGoldTeleport = 297
	nnHallEnter             = 324
)

// requestTeleport is CUser::RequestTeleport (0x290): use the teleport the
// player stands on (±3 cells of a Teleport.txt source), paying its price.
func (g *Game) requestTeleport(u *User) {
	if u.status != userPlay {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	x, y := int(m.target.X), int(m.target.Y)
	if x >= 2452 && x <= 2455 && y >= 1988 && y <= 1991 { // Erion box
		g.teleportar(u.id, 2050, 1605)
		return
	}
	for i, t := range g.data.Teleports {
		if i >= 44 && i <= 47 {
			continue // event teleports, handled elsewhere in the C++
		}
		if x < t.SrcX-3 || x > t.SrcX+3 || y < t.SrcY-3 || y > t.SrcY+3 {
			continue
		}
		// Unlike the C++, refuse the special cases before charging the fee.
		switch i {
		case 14: // Kefra hall
			if m.info.HallEnter == 0 {
				g.clientMessage(u, "Você não possui entradas")
				return
			}
		case 43: // fairy valley
			if e := pl.Equip[13].Index; e != 3916 && e != 3917 {
				g.clientMessage(u, "Necessario a Fada do Vale para ser teleportado")
				return
			}
			for _, o := range g.users[1:] {
				if o != u && o.status == userPlay && o.mac == u.mac && insideValley(g.mobs[o.id].target) {
					g.clientMessage(u, "Somente uma conta por computador")
					return
				}
			}
		}
		if t.Price != 0 {
			if pl.Gold < int64(t.Price) {
				g.clientMessage(u, "%s", g.lang(nnNotEnoughGoldTeleport))
				return
			}
			if city := g.data.village(t.DestX, t.DestY); city != 5 {
				g.data.Cities[city].Impost += int64(t.Price)
			}
			pl.Gold -= int64(t.Price)
			g.signalParm64(u, uint16(u.id), wire.OpSrvGold, pl.Gold)
		}
		switch i {
		case 14:
			m.info.HallEnter--
			g.clientMessage(u, cFormat(g.lang(nnHallEnter)), m.info.HallEnter)
		case 6:
			g.rvrPortal(u)
			return
		case 43:
			g.teleportar(u.id, 2284+rnd()%2, 3688+rnd()%2)
			return
		}
		g.teleportar(u.id, t.DestX+rnd()%2, t.DestY+rnd()%2)
		g.sendScore(u.id)
		return
	}
}

// cFormat adapts a printf format from Language.txt for fmt (%u -> %d).
func cFormat(s string) string {
	out := []byte(s)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == '%' && out[i+1] == 'u' {
			out[i+1] = 'd'
		}
	}
	return string(out)
}

// insideValley is CMob::IsInsideValley.
func insideValley(t wire.Position) bool {
	return t.X >= 2177 && t.X <= 2304 && t.Y >= 3584 && t.Y <= 3839
}
