package game

import (
	"fmt"
	"slices"
	"time"

	"dbsrv/internal/wire"
)

// The rules of RequestMoveItem besides the swap itself (RequestMoveItem.cpp,
// Basedef.cpp EnergizeEmptyRune).

const (
	itemEmptyRune       = 4854
	runeFirst           = 5110
	runeLast            = 5133
	costumeFirst        = 4190
	costumeLast         = 4299
	moveStackDelay      = 700 * time.Millisecond
	langBadPackets      = 23
	crackLimit          = 30
	efAmount            = 61
	opSrvDailyAvailable = 0xC671
)

// addCrack is AddCrackError: a client that keeps sending impossible packets
// goes back to the character list.
func (g *Game) addCrack(u *User, points int, reason string) {
	g.log.Warn("bad packet", "slot", u.id, "points", points, "reason", reason)
	u.crackCount += points
	if u.crackCount < crackLimit {
		return
	}
	u.crackCount = 0
	g.clientMessage(u, "%s", g.lang(langBadPackets))
	if u.status == userPlay {
		g.log.Warn("sent back to the character list: too many bad packets", "slot", u.id)
		g.charLogout(u)
	}
}

// energizeRune is EnergizeEmptyRune: a rune feeds an empty rune, once each.
func (g *Game) energizeRune(u *User, srcType, srcSlot, dstType, dstSlot int, src, dst *wire.Item) {
	bit := uint32(1) << uint(src.Index-runeFirst+1)
	// The C++ kept the bits in the bytes of effects 1 and 2.
	learn := uint32(dst.Effect[1].Index) | uint32(dst.Effect[1].Value)<<16 | uint32(dst.Effect[2].Index)<<24 | uint32(dst.Effect[2].Value)<<8
	if learn&bit != 0 {
		g.clientMessage(u, "Esta Runa foi utilizada para energização")
		return
	}
	energy := 0
	for _, e := range dst.Effect {
		if e.Index == efAmount {
			energy = int(e.Value)
			break
		}
	}
	setAmount(dst, energy+5)
	learn |= bit
	dst.Effect[1].Index, dst.Effect[1].Value = uint8(learn), uint16(learn>>16)
	dst.Effect[2].Index, dst.Effect[2].Value = uint8(learn>>24), uint16(learn>>8)&0xFF
	*src = wire.Item{}
	g.sendItem(u, srcType, srcSlot, src)
	g.sendItem(u, dstType, dstSlot, dst)
}

// setAmount is SetItemAmount on effect 0.
func setAmount(it *wire.Item, amount int) {
	for i := range it.Effect {
		if it.Effect[i].Index == efAmount {
			it.Effect[i].Value = uint16(amount)
			return
		}
	}
	it.Effect[0] = wire.ItemEffect{Index: efAmount, Value: uint16(amount)}
}

func isCrownItem(id uint16) bool { return id == itemCrown || id == 3993 || id == 3994 }

// crownMoveRefused: the crown and the dragon only go in or out of the
// storage for a guild that holds Noatun on some channel.
func (g *Game) crownMoveRefused(u *User, src, dst int, srcItem, dstItem *wire.Item) bool {
	if (!isCrownItem(srcItem.Index) && !isCrownItem(dstItem.Index)) || (src != wire.SlotStorage && dst != wire.SlotStorage) {
		return false
	}
	guild := int32(g.mobs[u.id].info.Player.GuildIndex)
	if guild <= 0 {
		return true
	}
	for _, cities := range g.chargedGuilds {
		if cities[cityNoatun] == guild {
			return false
		}
	}
	return true
}

// storageAllowedHere: outside a city the storage only opens in Armia's and
// the Kefra hall's training grounds.
func (g *Game) storageAllowedHere(t wire.Position) bool {
	x, y := int(t.X), int(t.Y)
	return g.data.village(x, y) != 5 || (mapRect{2085, 2016, 2150, 2051}).contains(x, y) || (mapRect{3209, 1672, 3273, 1712}).contains(x, y)
}

// itemDurations is the table of GiveTime: days of validity once equipped.
var itemDurations = []struct {
	days, from, to int
}{
	{1, 3916, 3917}, {3, 3900, 3902}, {3, 3980, 3982}, {3, 3998, 3998}, {5, 3903, 3905}, {5, 3990, 3990},
	{7, 3906, 3908}, {7, 3911, 3911}, {7, 3997, 3997}, {15, 3912, 3912}, {15, 3983, 3985}, {15, 3996, 3996},
	{30, 3913, 3913}, {30, 3986, 3989}, {30, 3995, 3995}, {30, 4150, 4189}, {30, 4224, 4227}, {30, 4229, 4231},
	{30, 4233, 4233}, {30, 4235, 4237}, {30, 4240, 4247}, {30, 4300, 4416},
}

func itemDays(id int) int {
	i := slices.IndexFunc(itemDurations, func(d struct{ days, from, to int }) bool { return id >= d.from && id <= d.to })
	if i < 0 {
		return 0
	}
	return itemDurations[i].days
}

func isCostumeItem(id uint16) bool {
	return (id >= 4210 && id <= 4229) || (id >= 4230 && id <= 4247) || (id >= 4301 && id <= 4411)
}

// giveTime is GiveTime: equipping a sphere, a costume or a fairy starts its
// validity.
func (g *Game) giveTime(u *User, srcType, srcSlot, dstType, dstSlot int, now time.Time) {
	if srcType != wire.SlotEquip && dstType != wire.SlotEquip {
		return
	}
	var it *wire.Item
	typ, slot := srcType, srcSlot
	if dstType == wire.SlotEquip {
		it = g.slotItem(u, dstType, dstSlot)
	} else {
		it = g.slotItem(u, srcType, srcSlot)
		typ, slot = dstType, dstSlot
	}
	if it == nil {
		return
	}
	days := itemDays(int(it.Index))
	if days == 0 {
		return
	}
	costume := isCostumeItem(it.Index)
	id := it.Index
	dated := it.Effect[0].Index == 106 && it.Effect[1].Index == 110 && it.Effect[2].Index == 109
	switch {
	case (id >= 3980 && id <= 3999) || (costume && !(dstType == wire.SlotEquip && dstSlot == 18)):
		if dated || (costume && it.Effect[0].Value != 0 && it.Effect[1].Value != 0 && it.Effect[2].Value != 0) {
			return
		}
	case (id >= 3900 && id <= 3908) || id == 3916 || id == 3917:
		if it.Effect[0].Index != 0 {
			return
		}
		it.Effect[0] = wire.ItemEffect{Index: 106, Value: uint16(days)}
		g.sendItem(u, typ, slot, it)
		return
	}
	end := now.AddDate(0, 0, days)
	it.Effect[0] = wire.ItemEffect{Index: 106, Value: uint16(end.Day())}
	it.Effect[1] = wire.ItemEffect{Index: 110, Value: uint16(end.Month())}
	it.Effect[2] = wire.ItemEffect{Index: 109, Value: uint16(end.Year() - 2000)}
	g.sendItem(u, typ, slot, it)
}

// loginExtras is the rest of SendCharToWorld.
func (g *Game) loginExtras(u *User) {
	m := &g.mobs[u.id]
	pl := &m.info.Player
	now := time.Now()
	g.signalParm(u, wire.ClientIDSystem, opSrvAuctionStatus, 0)
	g.signalParm(u, uint16(u.id), opSrvStoreTimer, 0)
	g.sendClientInfo(u)
	g.sendDailyRewardInfo(u)
	available := int32(1)
	if u.account.Daily.Received[int(now.Weekday())] {
		available = 0
	}
	g.signalParm(u, wire.ClientIDSystem, opSrvDailyAvailable, available)
	if pl.Equip[6].Index == 877 {
		pl.Equip[6] = wire.Item{}
		g.sendItem(u, wire.SlotEquip, 6, &pl.Equip[6])
	}
	if pl.BStatus.Level < 999 {
		g.chatAll(arenaAnnounce, fmt.Sprintf("[%s] se conectou no continente Kerserf!", g.charName(u.id)))
	}
	if u.account.Water.Day != int32(now.YearDay()-1) {
		u.account.Water.Day, u.account.Water.Total = int32(now.YearDay()-1), 0
	}
}

// fairyMinuteTimer counts down the fairies being worn, once a minute. The
// valley fairies only run inside the valley.
func (g *Game) fairyMinuteTimer() {
	g.forEachPlaying(func(u *User) {
		m := &g.mobs[u.id]
		it := &m.info.Player.Equip[13]
		id := it.Index
		if (id < 3900 || id > 3908) && id != 3914 && id != 3915 && id != 3916 && id != 3917 {
			return
		}
		valley := id == 3916 || id == 3917
		if valley && !insideValley(m.target) {
			return
		}
		days, hours, mins := int(it.Effect[0].Value), int(it.Effect[1].Value), int(it.Effect[2].Value)
		mins--
		if mins <= 0 && (hours > 0 || days > 0) {
			hours--
			mins = 59
		}
		if hours <= 0 && days > 0 {
			days--
			hours = 23
		}
		days = max(days, 0)
		if days == 0 && hours <= 0 && mins <= 0 {
			g.clientMessage(u, "Sua fada expirou.")
			*it = wire.Item{}
			g.sendItem(u, wire.SlotEquip, 13, it)
			g.getCurrentScore(u.id)
			g.sendScore(u.id)
			if valley {
				g.doRecall(u.id)
			}
			return
		}
		it.Effect[0] = wire.ItemEffect{Index: 106, Value: uint16(days)}
		it.Effect[1] = wire.ItemEffect{Index: 107, Value: uint16(hours)}
		it.Effect[2] = wire.ItemEffect{Index: 108, Value: uint16(mins)}
		g.sendItem(u, wire.SlotEquip, 13, it)
	})
}
