package game

import (
	"dbsrv/internal/wire"
)

// The roulette spin (RequestRoleta.cpp: GirarRoleta, GetRoletaReward).
// Opening a box and its list are in daily.go.

const (
	opRouletteSpin  = 0xF06
	opRouletteClaim = 0xF07

	itemRouletteBox = 5381
	itemRouletteKey = 5382

	rouletteWheelSize = 20
	rouletteBarFull   = 100
)

// rollPool lists the prizes a spin can give and the sum of their chances.
// The C++ summed lines that could never be picked, so a spin could give
// nothing and still eat the box.
func rollPool(b *RouletteBox, super bool) (pool []int, total int) {
	for i := range b.Roll {
		if b.Roll[i].Index != 0 && b.Meta[i].Chance > 0 && (!super || b.Meta[i].Super != 0) {
			pool = append(pool, i)
			total += b.Meta[i].Chance
		}
	}
	return pool, total
}

// rollOne picks a line of the pool by its chance.
func rollOne(b *RouletteBox, pool []int, total int) int {
	r := 1 + rnd()%total
	for _, i := range pool {
		if r -= b.Meta[i].Chance; r <= 0 {
			return i
		}
	}
	return pool[len(pool)-1]
}

// spinRoulette is GirarRoleta (0xF06): takes the key and the box, keeps the
// prize until the wheel stops (0xF07).
func (g *Game) spinRoulette(u *User) {
	if u.status != userPlay {
		return
	}
	info := &g.mobs[u.id].info
	pl := &info.Player
	slot := int(info.Roleta.BauSlot)
	if slot < 0 || slot >= 60 || pl.Inventory[slot].Index != itemRouletteBox {
		g.clientMessage(u, "Selecione uma caixa para ser aberta")
		return
	}
	id := g.data.itemAbility(&pl.Inventory[slot], efInformation)
	if id < 0 || id >= maxRouletteBox || !g.data.Roulette[id].Status {
		g.clientMessage(u, "Roleta desativada temporariamente")
		return
	}
	b := &g.data.Roulette[id]
	if u.rouletteReward.Index != 0 {
		g.clientMessage(u, "Retire sua recompensa para usar novamente")
		return
	}
	if firstEmptySlot(pl) < 0 {
		g.clientMessage(u, "Não possui espaço para negociar")
		return
	}
	super := b.HasSuperDrop && u.rouletteEvol[id] >= rouletteBarFull
	pool, total := rollPool(b, super)
	if len(pool) == 0 {
		g.log.Warn("roulette box without prizes", "box", id, "super", super)
		g.clientMessage(u, "Roleta desativada temporariamente")
		return
	}
	if b.KeyID >= 0 {
		key := -1
		for i := range 64 {
			it := &pl.Inventory[i]
			if it.Index != itemRouletteKey {
				continue
			}
			for _, e := range it.Effect {
				if e.Index == efInformation && int8(e.Value) == b.KeyID {
					key = i
				}
			}
			if key >= 0 {
				break
			}
		}
		if key < 0 {
			g.clientMessage(u, "Não possui a chave para esta caixa")
			return
		}
		amountMinus(&pl.Inventory[key])
		g.sendItem(u, wire.SlotInv, key, &pl.Inventory[key])
	}

	p := wire.RollItem{Header: wire.NewHeader(opRouletteSpin, wire.ClientIDSystem), Level: int16(id), Evol: -1}
	if b.HasSuperDrop {
		p.Evol = u.rouletteEvol[id]
	}
	p.Slot = int16(rnd() % rouletteWheelSize)
	prize := rollOne(b, pool, total)
	for a := range p.Item {
		line := prize
		if a != int(p.Slot) {
			line = rollOne(b, pool, total)
		}
		p.Item[a] = b.Roll[line]
		p.Rare[a] = int16(b.Meta[line].Rare)
	}
	u.rouletteReward = b.Roll[prize]
	info.Roleta.DropMsg = int8(b.Meta[prize].SendMsg)
	info.Roleta.BauID = int8(id)
	if super {
		u.rouletteEvol[id] = 0
	} else if b.HasSuperDrop {
		// The C++ left a negative bar in place.
		u.rouletteEvol[id] = int16(min(max(int(u.rouletteEvol[id])+b.Meta[prize].EvolBar, 0), rouletteBarFull))
	}
	g.queue(u, &p)
	amountMinus(&pl.Inventory[slot])
	g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
	g.log.Info("roulette", "slot", u.id, "char", g.charName(u.id), "box", id, "prize", u.rouletteReward.Index, "super", super)
}

// getRouletteReward is GetRoletaReward (0xF07): the prize goes to the
// inventory, and it waits when the inventory is full.
func (g *Game) getRouletteReward(u *User) {
	if u.rouletteReward.Index == 0 {
		return
	}
	info := &g.mobs[u.id].info
	if firstEmptySlot(&info.Player) < 0 {
		g.clientMessage(u, "Não possui espaço para negociar")
		return
	}
	g.clientMessage(u, "Recompensa retirada com sucesso")
	if info.Roleta.DropMsg == 1 {
		g.notice("[Servidor] " + g.charName(u.id) + " obteve " + g.data.Item(u.rouletteReward.Index).Name + " na roleta")
	}
	g.putItem(u, &u.rouletteReward)
	u.rouletteReward = wire.Item{}
}

// settleRouletteReward runs when the player leaves with a prize pending:
// the inventory if it fits, otherwise the storage through DBSRV (the C++
// lost it).
func (g *Game) settleRouletteReward(u *User) {
	if u.rouletteReward.Index == 0 {
		return
	}
	if firstEmptySlot(&g.mobs[u.id].info.Player) >= 0 {
		g.getRouletteReward(u)
		return
	}
	p := wire.ItemDelivery{Item: u.rouletteReward}
	copy(p.Username[:], u.account.Username[:])
	g.returnDelivery(&p)
	u.rouletteReward = wire.Item{}
}
