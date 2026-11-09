package game

import "dbsrv/internal/wire"

// reportOpenStores tells DBSRV which stores are open, for the panel.
func (g *Game) reportOpenStores() {
	g.forEachPlaying(func(u *User) {
		if !u.store.open {
			return
		}
		p := wire.OpenStore{Header: wire.NewHeader(wire.OpOpenStore, uint16(u.id)), Channel: int32(g.cfg.Channel)}
		copy(p.Owner[:15], g.mobs[u.id].info.Player.Name[:15])
		for i := range u.store.list {
			if l := &u.store.list[i]; l.Item.Index != 0 {
				p.Items++
				if p.Name[0] == 0 {
					copy(p.Name[:23], l.Name[:23])
				}
			}
		}
		g.sendDB(&p)
	})
}
