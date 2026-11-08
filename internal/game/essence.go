package game

import (
	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// opEssenceUse is UseEssencePacket: feeding âmagos to the mount in one go
// (RequestEssenceUse.cpp). opEchoMoveItem is answered with itself.
const (
	opEssenceUse   = 0x473
	opEchoMoveItem = 0x3776
)

var (
	essenceLevelMax = [7]int{25, 35, 45, 55, 65, 75, 100}
	essenceRate     = [2][12]int{
		{100, 100, 95, 85, 75, 65, 55, 45, 35, 25, 10, 3},
		{95, 85, 75, 65, 55, 45, 35, 25, 15, 7, 5, 3},
	}
)

func (g *Game) requestEssenceUse(u *User, pkt protocol.Packet) {
	var p wire.EssencePacket
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	if p.Slot < 0 || p.Slot >= 60 {
		g.clientMessage(u, "%s", g.lang(nnMountNotMatch))
		return
	}
	pl := &g.mobs[u.id].info.Player
	src, mountItem := &pl.Inventory[p.Slot], &pl.Equip[14]
	refuse := func(format string, args ...any) {
		g.clientMessage(u, format, args...)
		g.sendItem(u, wire.SlotInv, int(p.Slot), src)
	}
	mount := int(mountItem.Index)
	if mount < 2330 || mount > 2389 {
		refuse("%s", g.lang(nnMountNotMatch))
		return
	}
	index := 0
	if mount >= 2360 {
		index = mount - 2360
	} else if mount <= 2358 {
		index = mount - 2330
	}
	essence := 2390 + index
	if mount == 2387 || mount == 2388 {
		essence = 2417 + (mount - 2387)
	}
	if essence != int(src.Index) {
		refuse("%s", g.lang(nnMountNotMatch))
		return
	}
	if amount := itemAmount(src); int(p.Amount) > amount || p.Amount < 1 || p.Amount > 120 {
		g.log.Warn("essence amount", "slot", u.id, "have", amount, "sent", p.Amount)
		refuse("Valor incorreto")
		return
	}
	if mountItem.Effect[1].Value <= 5 {
		refuse("Vitalidade mínima para aumentar o nível da montaria é 6.")
		return
	}
	success, fail := 0, 0
	start := int(mountItem.Effect[1].Index)
loop:
	for range p.Amount {
		level := &mountItem.Effect[1].Index
		switch {
		case mount < 2360: // an egg still growing
			*level++
			success++
			if int(*level) >= essenceLevelMax[min(index, 6)] {
				mountItem.Index = uint16(2360 + (mount - 2330))
				mountItem.Effect[0] = wire.ItemEffect{Index: 0x88, Value: 0x13} // 5000 as a short
				*level = 1
				mountItem.Effect[1].Value = uint8(min(int(mountItem.Effect[1].Value)+rnd()%16+5, 255))
				mountItem.Effect[2] = wire.ItemEffect{Index: 100}
				g.clientMessage(u, "%s", g.lang(nnIncubated))
				g.log.Info("mount grew", "slot", u.id, "item", mountItem.Index)
				g.mountProcess(u.id)
				break loop // as in the C++, the last essence is kept
			}
		case *level >= 120:
			g.clientMessage(u, "Level maximo.")
			break loop
		default:
			table := 0
			if index >= 17 {
				table = 1
			}
			rate := essenceRate[table][min(int(*level)/10, 11)]
			for _, e := range src.Effect {
				if e.Index == 210 {
					rate += int(e.Value)
					break
				}
			}
			mountItem.Effect[2].Index = 100
			if rnd()%100 > rate {
				if rnd()%3 == 0 && *level%2 == 0 && *level != 0 {
					*level--
				}
				fail++
			} else {
				*level++
				success++
			}
		}
		amountMinus(src)
	}
	g.sendItem(u, wire.SlotEquip, 14, mountItem)
	g.sendItem(u, wire.SlotInv, int(p.Slot), src)
	g.clientMessage(u, "Âmagos usados: %d. Com sucesso: %d. Com falha: %d Leveis ganhos: %d", success+fail, success, fail, int(mountItem.Effect[1].Index)-start)
	g.sendEmotion(u.id, 14, 3)
	g.log.Info("essence", "slot", u.id, "essence", essence, "used", success+fail, "ok", success, "level", mountItem.Effect[1].Index)
}
