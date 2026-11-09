package game

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The informational systems behind /daily, /attpass and /roleta:
//   - daily reward (RequestCollectDaily.cpp SendDailyRewardInfo 0x671 and
//     RequestCollectDaily 0x672, ReadDailyReward Data/DailyReward.txt; the
//     per-account state lives in Cache/Daily/<LOGIN>.bin, shared with the C++);
//   - SendMissionInfo 0x347 (ReadMissions Data/Missions.xml);
//   - the roulette box list of /roleta 0xF05 (LoadRoleta ../Conf/Roleta.txt).
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

// loadDailyReward is ReadDailyReward: "day line item ef1 efv1 ef2 efv2 ef3 efv3".
func (d *Data) loadDailyReward(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if line == "" || line[0] == '#' {
			continue
		}
		f := atoiFields(line)
		if len(f) < 3 || f[0] < 0 || f[0] >= 7 || f[1] < 0 || f[1] >= 4 { // the C++ wrote out of bounds
			continue
		}
		it := wire.Item{Index: uint16(f[2])}
		for k := 0; k < 3 && 4+2*k < len(f); k++ {
			it.Effect[k] = wire.ItemEffect{Index: uint8(f[3+2*k]), Value: uint16(f[4+2*k])}
		}
		d.DailyReward[f[0]][f[1]] = it
	}
	return sc.Err()
}

// atoiFields parses the leading integers of a line (sscanf "%d %d ...").
func atoiFields(line string) []int {
	var out []int
	for _, s := range strings.Fields(line) {
		v, err := strconv.Atoi(s)
		if err != nil {
			break
		}
		out = append(out, v)
	}
	return out
}

type xmlMissions struct {
	Missions []struct {
		ID   string `xml:"id"`
		Name string `xml:"name"`
		Reqs struct {
			Classes  []string `xml:"classes>class"`
			MinLevel string   `xml:"minLevel"`
			MaxLevel string   `xml:"maxLevel"`
		} `xml:"reqs"`
		Quest struct {
			Mobs []struct {
				Index int    `xml:"index,attr"`
				Name  string `xml:"name,attr"`
				Total int    `xml:"total,attr"`
			} `xml:"mob"`
			Drops []struct {
				Index int `xml:"index,attr"`
				ID    int `xml:"id,attr"`
				Total int `xml:"total,attr"`
			} `xml:"drop"`
		} `xml:"quest"`
		Rewards struct {
			Gold string        `xml:"gold"`
			Exp  string        `xml:"exp"`
			Free []xmlMissItem `xml:"free>items>item"`
			Pass []xmlMissItem `xml:"pass>items>item"`
		} `xml:"rewards"`
	} `xml:"mission"`
}

type xmlMissItem struct {
	Index  int  `xml:"index,attr"`
	Amount *int `xml:"amount,attr"`
	ItemID int  `xml:"itemId,attr"`
	Ef0    int  `xml:"ef0,attr"`
	Efv0   int  `xml:"efv0,attr"`
	Ef1    int  `xml:"ef1,attr"`
	Efv1   int  `xml:"efv1,attr"`
	Ef2    int  `xml:"ef2,attr"`
	Efv2   int  `xml:"efv2,attr"`
}

// loadMissions is ReadMissions.
func (d *Data) loadMissions(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var x xmlMissions
	// The file is Windows-1252 (the game charset) with no encoding declared.
	if err := xml.Unmarshal([]byte(wire.DecodeGame(string(raw))), &x); err != nil {
		return err
	}
	d.Missions = d.Missions[:0]
	for _, xm := range x.Missions {
		var m Mission
		m.ID, _ = strconv.Atoi(strings.TrimSpace(xm.ID))
		m.Name = xm.Name
		for _, c := range xm.Reqs.Classes {
			if v, err := strconv.Atoi(strings.TrimSpace(c)); err == nil {
				m.Classes = append(m.Classes, v)
			}
		}
		minS, maxS := strings.TrimSpace(xm.Reqs.MinLevel), strings.TrimSpace(xm.Reqs.MaxLevel)
		if minS != "" && maxS != "" {
			m.MinLevel, _ = strconv.Atoi(minS)
			m.MaxLevel, _ = strconv.Atoi(maxS)
			m.HasLevel = true
		}
		for _, mob := range xm.Quest.Mobs {
			if mob.Index >= 0 && mob.Index < 5 {
				m.MobName[mob.Index], m.MobAmount[mob.Index] = mob.Name, mob.Total
			}
		}
		for _, dr := range xm.Quest.Drops {
			if dr.Index >= 0 && dr.Index < 5 {
				m.DropItem[dr.Index], m.DropAmount[dr.Index] = dr.ID, dr.Total
			}
		}
		read := func(src []xmlMissItem, dst *[6]wire.Item, amount *[6]int) {
			for _, it := range src {
				if it.Index < 0 || it.Index >= 6 || it.ItemID <= 0 || it.ItemID >= MaxItemList {
					continue
				}
				amount[it.Index] = 1
				if it.Amount != nil {
					amount[it.Index] = min(max(*it.Amount, 0), 120)
				}
				dst[it.Index] = wire.Item{Index: uint16(it.ItemID), Effect: [3]wire.ItemEffect{
					{Index: uint8(it.Ef0), Value: uint16(it.Efv0)},
					{Index: uint8(it.Ef1), Value: uint16(it.Efv1)},
					{Index: uint8(it.Ef2), Value: uint16(it.Efv2)},
				}}
			}
		}
		read(xm.Rewards.Free, &m.Free, &m.FreeAmount)
		read(xm.Rewards.Pass, &m.Pass, &m.PassAmount)
		m.Gold, _ = strconv.Atoi(strings.TrimSpace(xm.Rewards.Gold))
		m.Exp, _ = strconv.Atoi(strings.TrimSpace(xm.Rewards.Exp))
		d.Missions = append(d.Missions, m)
	}
	return nil
}

// loadRoulette is LoadRoleta: "[Caixa] n" starts box n-1; Name, Status,
// SuperDrop, KeyId, ShowSlots a.b.c...; then "item.ef.efv.ef.efv.ef.efv
// chance msg rare super evol" lines (commas count as spaces).
func (d *Data) loadRoulette(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	d.Roulette = [maxRouletteBox]RouletteBox{}
	box, last := -1, 0
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.ReplaceAll(sc.Text(), ",", " ")
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if len(f) >= 2 && f[0] == "[Caixa]" {
			n, _ := strconv.Atoi(f[1])
			box, last = n-1, 0
			continue
		}
		if box < 0 || box >= 20 {
			continue
		}
		b := &d.Roulette[box]
		val := func() int {
			if len(f) < 2 {
				return 0
			}
			v, _ := strconv.Atoi(f[1])
			return v
		}
		switch f[0] {
		case "Name":
			if len(f) >= 2 {
				b.Name = f[1]
			}
		case "Status":
			b.Status = val() != 0
		case "SuperDrop":
			b.HasSuperDrop = val() != 0
		case "KeyId":
			b.KeyID = int8(val())
		case "ShowSlots":
			if len(f) >= 2 {
				for i, s := range strings.Split(f[1], ".") {
					if i >= 11 {
						break
					}
					v, _ := strconv.Atoi(s)
					b.ShowSlots[i] = int8(v - 1)
				}
			}
		default:
			parts := strings.Split(f[0], ".")
			if len(parts) != 7 || len(f) != 6 || last >= maxRouletteItem {
				continue
			}
			var v [7]int
			ok := true
			for i, s := range parts {
				if v[i], err = strconv.Atoi(s); err != nil {
					ok = false
				}
			}
			if !ok {
				continue
			}
			b.Roll[last] = wire.Item{Index: uint16(v[0]), Effect: [3]wire.ItemEffect{
				{Index: uint8(v[1]), Value: uint16(v[2])}, {Index: uint8(v[3]), Value: uint16(v[4])}, {Index: uint8(v[5]), Value: uint16(v[6])},
			}}
			var m [5]int
			for i := range m {
				if m[i], err = strconv.Atoi(f[i+1]); err != nil {
					ok = false
				}
			}
			if !ok {
				b.Roll[last] = wire.Item{}
				continue
			}
			b.Meta[last] = RollMeta{Chance: m[0], SendMsg: m[1], Rare: m[2], Super: m[3], EvolBar: m[4]}
			last++
		}
	}
	return sc.Err()
}

// loadOptional runs a loader whose file may be missing (as in the C++,
// which skipped absent files).
func loadOptional(load func(string) error, path string) error {
	if err := load(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// ---- daily reward ----

// weekOfYear is getWeek: the ISO week number.
func weekOfYear(t time.Time) int32 {
	_, w := t.ISOWeek()
	return int32(w)
}

func (g *Game) dailyPath(u *User) string {
	login := wire.CString(u.account.Username[:])
	if g.cfg.CacheDir == "" || login == "" || filepath.Base(login) != login {
		return ""
	}
	return filepath.Join(g.cfg.CacheDir, "Daily", login+".bin")
}

// loadDaily is LoadDailyFromBin: the file wins over the account copy (the
// C++ kept the state only there).
func (g *Game) loadDaily(u *User) {
	path := g.dailyPath(u)
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) < binary.Size(wire.Daily{}) {
		return
	}
	var d wire.Daily
	if binary.Read(bytes.NewReader(raw), binary.LittleEndian, &d) == nil {
		u.account.Daily = d
	}
}

// saveDaily is SaveDailyToBin.
func (g *Game) saveDaily(u *User) {
	path := g.dailyPath(u)
	if path == "" {
		return
	}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, &u.account.Daily)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		err = os.WriteFile(path, buf.Bytes(), 0o644)
		if err != nil {
			g.log.Warn("daily reward not saved", "slot", u.id, "err", err)
		}
	}
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
	g.loadDaily(u)
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
	g.loadDaily(u)
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
		if g.putItem(u, &items[i]) {
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
	g.saveDaily(u)
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
