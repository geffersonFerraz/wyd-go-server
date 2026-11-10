package game

import (
	"strconv"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The informational systems behind /daily, /attpass and /roleta:
//   - daily reward (RequestCollectDaily.cpp SendDailyRewardInfo 0x671 and
//     RequestCollectDaily 0x672; the items are daily_reward of tmsrv.yaml and
//     the per-account state is saved with the account);
//   - SendMissionInfo 0x347 (config/missions.yaml);
//   - the roulette box list of /roleta 0xF05 (roulette of tmsrv.yaml).
// The mission event itself (accept 0x349, collect 0x348, kill counters)
// lives in missions.go; the roulette spin is still to port.

const (
	opDailyRewardInfo    = 0x671
	opDailyRewardCollect = 0x672
	opMissionInfo        = 0x347
	opMissionTimes       = 0xCF30
	opRollInfo           = 0xF05
	efInformation        = 203
	maxRouletteBox       = 100 // MAX_ROULETTEPACK
	maxRouletteItem      = 50  // MAX_ROULETTEITEM
)

// DailyRewardInfo is MSG_DAILYREWARDINFO (248 bytes).
type DailyRewardInfo struct {
	Header   wire.Header
	Item     [7][4]wire.Item
	Received [7]bool
	_        [1]byte
	Day      int32
}

type missionMob struct {
	Name          [16]byte
	Total, Killed int32
}

type missionItem struct {
	ItemID, Total, Dropped int32
}

// MissionInfo is MSG_MISSIONINFO (352 bytes).
type MissionInfo struct {
	Header           wire.Header
	IsAccepted       bool
	Name             [32]byte
	_                [3]byte
	Mob              [5]missionMob
	Item             [5]missionItem
	FreeReward       [6]wire.Item
	BattlePassReward [6]wire.Item
	_                [4]byte
	Gold             int64 // 328
	Exp              uint64
	BattlePassValid  wire.ItemDate
	LastUpdate       wire.ItemDate
}

// MissionTimes is the 0xCF30 packet of /attpass (524 bytes).
type MissionTimes struct {
	Header         wire.Header
	LastMission    [256]byte
	CurrentMission [256]byte
}

// RollInfo is MSG_SendRollInfo (104 bytes).
type RollInfo struct {
	Header wire.Header
	Evol   int16
	BoxID  int8
	KeyID  int8
	Item   [11]wire.Item
	_      [2]byte
}

// Mission is TOD_MissionInfo (the parts SendMissionInfo shows).
type Mission struct {
	ID         int
	Name       string
	MobName    [5]string
	MobAmount  [5]int
	DropItem   [5]int
	DropAmount [5]int
	Free, Pass [6]wire.Item
	// FreeAmount/PassAmount are how many of each reward item (attribute
	// "amount", 1 when missing).
	FreeAmount, PassAmount [6]int
	Gold, Exp              int
	// Classes are the evolutions (1 Mortal, 2 Arch, 3 Celestial, 4
	// Sub-Celestial) that can get it; MinLevel/MaxLevel limit mortals.
	Classes            []int
	MinLevel, MaxLevel int
	HasLevel           bool
}

// RouletteBox is ItemRoll_.
type RouletteBox struct {
	Name         string
	Status       bool
	HasSuperDrop bool
	KeyID        int8
	ShowSlots    [11]int8
	Roll         [maxRouletteItem]wire.Item
	Meta         [maxRouletteItem]RollMeta // odds and flags of each Roll
}

// RollMeta is the rest of a roulette line: chance, announce to the server,
// rarity shown, is a prize of the super drop, bar gained.
type RollMeta struct {
	Chance               int
	SendMsg, Rare, Super int
	EvolBar              int
}

// ---- loaders ----

// ---- daily reward ----

// weekOfYear is getWeek: the ISO week number.
func weekOfYear(t time.Time) int32 {
	_, w := t.ISOWeek()
	return int32(w)
}

// dailyNewWeek clears the week's marks when the week changed.
func dailyNewWeek(d *wire.Daily, now time.Time) {
	if w := weekOfYear(now); w != d.WeekYear {
		d.Received = [7]bool{}
		d.WeekYear = w
	}
}

// sendDailyRewardInfo is SendDailyRewardInfo ("/daily").
func (g *Game) sendDailyRewardInfo(u *User) {
	now := time.Now()
	dailyNewWeek(&u.account.Daily, now)
	p := DailyRewardInfo{Header: wire.NewHeader(opDailyRewardInfo, uint16(u.id)),
		Item: g.data.DailyReward, Received: u.account.Daily.Received, Day: int32(now.Weekday())}
	g.queue(u, &p)
}

var weekNames = [7]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"}

// requestCollectDaily is RequestCollectDaily (0x672): once per weekday,
// frango (30) and exp (39) buffs plus the day's items.
// Fixed: the C++ indexed Affects[-1] when no affect slot was free.
func (g *Game) requestCollectDaily(u *User, _ protocol.Packet) {
	if u.status != userPlay {
		return
	}
	now := time.Now()
	d := &u.account.Daily
	dailyNewWeek(d, now)
	day := int(now.Weekday())
	if d.Received[day] {
		g.clientMessage(u, "Você já recebeu esta premiação!")
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	if inventoryAmount(pl, 0) < 2 {
		g.clientMessage(u, "Não possui espaço no inventário")
		return
	}
	g.addAffectTime(u.id, 30, 1, 450*2)
	g.addAffectTime(u.id, 39, 0, 450*2)

	items := []wire.Item{{Index: 419}, {Index: 420}}
	if day == 0 || day == 6 {
		items = []wire.Item{{Index: 3322}, {Index: 3323}}
		for i := range items {
			items[i].Effect[0] = wire.ItemEffect{Index: 61, Value: 250}
		}
	} else {
		for i := range items {
			items[i].Effect[0] = wire.ItemEffect{Index: 61, Value: uint16(day)}
		}
	}
	for i := range items {
		if g.eventItem(u, &items[i], "Quest diária") {
			g.clientMessage(u, "Chegou um item: [%s]", g.itemName(items[i].Index))
		} else {
			g.clientMessage(u, "Falha ao receber %s", g.itemName(items[i].Index))
		}
	}
	g.sendAffect(u)
	g.refreshMob(u.id)
	g.log.Info("daily reward", "slot", u.id, "day", weekNames[day])
	g.clientMessage(u, "Coletado com sucesso referente ao dia de %s!!", weekNames[day])
	d.Received[day] = true
	g.sendDailyRewardInfo(u)
	g.getCurrentScore(u.id)
	g.sendEtc(u)
	g.sendScore(u.id)
}

// addAffectTime gives affect index (value) for seconds/… ticks, adding the
// time when it is already on (the /novato and daily buffs).
func (g *Game) addAffectTime(id, index, value int, ticks uint32) {
	slot := g.emptyAffect(id, index)
	if slot < 0 {
		return
	}
	a := &g.mobs[id].info.Affects[slot]
	if int(a.Index) == index {
		a.Time += ticks
		a.Value = uint16(value)
		return
	}
	*a = wire.Affect{Index: uint8(index), Value: uint16(value), Time: ticks}
}

// ---- missions ----

// sendMissionInfo is SendMissionInfo: the player's current daily mission.
func (g *Game) sendMissionInfo(u *User) {
	if u == nil || u.status != userPlay {
		return
	}
	m := &g.mobs[u.id].info
	daily := &m.DailyQuest
	p := MissionInfo{Header: wire.NewHeader(opMissionInfo, 0)}
	if daily.QuestID == -1 {
		g.queue(u, &p)
		return
	}
	var quest *Mission
	for i := range g.data.Missions {
		if g.data.Missions[i].ID == int(daily.QuestID) {
			quest = &g.data.Missions[i]
			break
		}
	}
	if quest == nil {
		return
	}
	p.IsAccepted = daily.IsAccepted
	wire.PutGameString(p.Name[:31], quest.Name)
	exp := int64(quest.Exp)
	pl := &m.Player
	ev, level := evolution(pl), int(pl.Status.Level)
	if (ev <= evoArch && level == maxLevel) || (ev >= evoCelestial && level >= maxLevelCelestial) {
		exp = 0
	}
	if ev != evoMortal {
		exp = g.expApply2(exp, u.id, u.id, false)
	}
	p.Exp = uint64(max(exp, 0))
	p.Gold = int64(quest.Gold)
	p.FreeReward, p.BattlePassReward = quest.Free, quest.Pass
	for i := range 5 {
		wire.PutGameString(p.Mob[i].Name[:15], quest.MobName[i])
		p.Mob[i].Total = int32(quest.MobAmount[i])
		p.Mob[i].Killed = int32(daily.MobCount[i])
		p.Item[i] = missionItem{ItemID: int32(quest.DropItem[i]), Total: int32(quest.DropAmount[i]), Dropped: int32(daily.ItemCount[i])}
	}
	p.LastUpdate, p.BattlePassValid = daily.LastUpdate, daily.BattlePass
	g.queue(u, &p)
}

// attPass is "/attpass": send the last/next mission times and the mission.
// The C++ also stamped LastUpdate with "now", which pushed the next mission
// (or the deadline of an accepted one) back every time the window was
// opened; the clock is only read here.
func (g *Game) attPass(u *User) {
	now := time.Now()
	daily := &g.mobs[u.id].info.DailyQuest
	p := MissionTimes{Header: wire.NewHeader(opMissionTimes, uint16(u.id))}
	lu := daily.LastUpdate
	if lu.Ano == 0 {
		lu = itemDate(now)
	}
	next := missionDate(lu).Add(g.missionNextDelay())
	if daily.IsAccepted {
		next = missionDate(lu).Add(g.missionCompleteTime())
	}
	if next.Before(now) {
		next = now
	}
	wire.PutCString(p.LastMission[:], twoDigits(int(lu.Hora), int(lu.Minuto), int(lu.Segundo)))
	wire.PutCString(p.CurrentMission[:], twoDigits(next.Hour(), next.Minute(), next.Second()))
	g.queue(u, &p)
	g.sendMissionInfo(u)
}

func twoDigits(h, m, s int) string {
	pad := func(v int) string {
		if v < 10 {
			return "0" + strconv.Itoa(v)
		}
		return strconv.Itoa(v)
	}
	return pad(h) + ":" + pad(m) + ":" + pad(s)
}

// ---- roulette ----

// roletaCommand is "/roleta": the first box (an inventory item with
// EF_INFORMATION) shows its prize list.
// Fixed: the C++ indexed ItemRoll with any EF_INFORMATION value (out of
// bounds above 99).
func (g *Game) roletaCommand(u *User) {
	pl := &g.mobs[u.id].info.Player
	for i := range 64 {
		it := &pl.Inventory[i]
		if it.Index == 0 {
			continue
		}
		box := g.data.itemAbility(it, efInformation)
		if box <= 0 {
			continue
		}
		g.openRoulette(u, i)
		return
	}
}

// openRoulette is RequestOpenRoulette: shows the box in inventory slot i.
// Fixed: the C++ indexed ItemRoll with any EF_INFORMATION value.
func (g *Game) openRoulette(u *User, i int) {
	m := &g.mobs[u.id].info
	it := &m.Player.Inventory[i]
	box := g.data.itemAbility(it, efInformation)
	g.sendItem(u, wire.SlotInv, i, it)
	if box < 0 || box >= maxRouletteBox || !g.data.Roulette[box].Status {
		g.clientMessage(u, "Roleta desativada temporariamente")
		return
	}
	if u.rouletteReward.Index != 0 {
		g.getRouletteReward(u)
	}
	b := &g.data.Roulette[box]
	p := RollInfo{Header: wire.NewHeader(opRollInfo, wire.ClientIDSystem), BoxID: int8(box), KeyID: b.KeyID, Evol: -1}
	if b.HasSuperDrop {
		p.Evol = u.rouletteEvol[box]
	}
	for k, s := range b.ShowSlots {
		switch {
		case s == -1:
		case s == -2:
			p.Item[k].Index = 5900
		case s == -3:
			p.Item[k].Index = 5901
		case s == -4:
			p.Item[k].Index = 5902
		case s >= 0 && int(s) < maxRouletteItem:
			p.Item[k] = b.Roll[s]
		}
	}
	g.queue(u, &p)
	m.Roleta.BauSlot = int8(i)
}
