package game

import (
	"fmt"
	"slices"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// The guild wars (Basedef.cpp GuildProcess, DecideWinner, DecideChallenger,
// FinishCastleWar, InitializeTowerWar; RequestReqChallange.cpp).
//
// Each channel has its own wars and city owners (gamedata.city_owners, one
// row per channel and city).

const (
	weekPrepare = iota // 0: Sunday, before the war hour
	weekWarning        // 1: "wait in the city you bet on"
	weekTeleported
	weekFighting
	weekCollect // 4: until Saturday, the owners collect taxes
	weekBetting // 5: Saturday
)

const (
	castleOff = iota
	castleClosing
	castleOpen
	castleEnding
	castleCollect // the owner of Noatun may collect its taxes
)

const (
	genTowerErion  = 290
	genNoatunTower = 4815
	genNoatunGate  = 4818
	itemCrown      = 747
	itemWarRefusal = 4031
	itemWarDeclare = 4030
	cityNoatun     = 4
	warMaxTeleport = 26
	warLives       = 5
	guildFameBet   = 100
	altarX         = 1046
	altarY         = 1690
	altarSeconds   = 120
	maxCityBids    = 8

	opSrvCastleState = 0x3AC
	opSrvAltarCharge = 0x3AD
	opSrvCounterMob  = 0x3BB

	langWinnerIs      = 59
	langChampionAnd   = 57
	langNoChallenger  = 58
	langGuildMaster   = 24
	langChampionsCant = 25
	langNoMoney       = 83
	langNeedBet       = 192
	langNamedGuild    = 173
	langTaxRange      = 168
	langTaxSet        = 169
	langOnlyVillage   = 172
	langOnlyMaster    = 81
	langVillageLimit  = 265
	langCastleWarn    = 275
	langCastleOpen    = 276
	langCastleEnding  = 277
	langCastleClosed  = 278
	langCharging      = 280
	langCharged       = 281
	langWarResult     = 290
	langBattle1       = 135
	langBattle3       = 137
	langBattle4       = 138
	langBattle5       = 139
	langBattle6       = 140
)

var (
	castleArea     = mapRect{1036, 1672, 1144, 1764}
	towerWarArea   = mapRect{2447, 1857, 2545, 1919}
	guildPKZone    = mapRect{128, 128, 256, 256}
	castleOutGates = [][4]int{
		{1129, 1705, 1129, 1709}, {1116, 1705, 1116, 1709}, {1094, 1688, 1094, 1692},
		{1087, 1609, 1087, 1713}, {1050, 1690, 1050, 1690}, {1046, 1690, 1047, 1691}, {1124, 1708, 1124, 1708},
	}
)

type cityBid struct{ guild, value int32 }

type cityWar struct {
	chall1, chall2 int32
	bids           []cityBid
}

type warState struct {
	weekMode    int
	forceWeek   int // ForceWeekDay; -1 = follow the calendar
	castleState int
	capeWin     int32
	altar       int
	altarTicks  int
	towerOn     bool
	towerGuild  int32
	cities      [5]cityWar
}

func (g *Game) cityOwner(city int) int32 { return g.chargedGuilds[g.cfg.Channel-1][city] }

// reportCityOwners tells the other channels this channel's owners
// (GuildZoneReport); they are shown crowns and refuse bets with them.
func (g *Game) reportCityOwners() {
	g.sendDB(&wire.GuildZoneReport{Header: wire.NewHeader(wire.OpGuildZoneReport, uint16(g.cfg.Channel-1)), Guild: g.chargedGuilds[g.cfg.Channel-1]})
}

// setCityOwner keeps the owner and sends it to DBSRV, which saves it.
func (g *Game) setCityOwner(city int, guild int32) {
	if g.cityOwner(city) == guild {
		return
	}
	g.chargedGuilds[g.cfg.Channel-1][city] = guild
	g.data.Cities[city].OwnerIndex = guild
	g.sendDB(&wire.CityOwnerUpdate{Header: wire.NewHeader(wire.OpCityOwner, uint16(g.cfg.Channel)), City: int32(city), Guild: guild})
	g.reportCityOwners()
	g.log.Info("war: city owner changed", "city", city, "guild", guild)
}

func (g *Game) setGuildFame(guild, fame int) {
	g.guildEntry(guild).Fame = fame
	g.sendDB(&wire.AddGuild{Header: wire.NewHeader(wire.OpAddGuild, 0), Type: 0, Value: int32(fame), GuildIndex: int32(guild)})
}

func (g *Game) setGuildWins(guild, wins int) {
	g.guildEntry(guild).Wins = wins
	g.sendDB(&wire.AddGuild{Header: wire.NewHeader(wire.OpAddGuild, 0), Type: 2, Value: int32(wins), GuildIndex: int32(guild)})
}

func (g *Game) forEachPlaying(fn func(u *User)) {
	for _, u := range g.users[1:] {
		if u.status == userPlay {
			fn(u)
		}
	}
}

// initWars: WeekMode and ForceWeekDay at the start of the server.
func (g *Game) initWars() {
	g.war.weekMode, g.war.forceWeek = weekCollect, -1
}

// RestoreChannelState puts back what SaveChannelState wrote; call before Run.
func (g *Game) RestoreChannelState(st *store.ChannelState) {
	if st == nil {
		return
	}
	w := &g.war
	w.weekMode, w.castleState, w.capeWin, w.towerGuild = st.WeekMode, st.CastleState, int32(st.CapeWin), int32(st.TowerGuild)
	g.kefra.dead, g.kefra.killer = st.KefraDead, int32(st.KefraKiller)
	g.kingdom.winner, g.rvr.bonus = int32(st.KingdomWinner), int32(st.RvRBonus)
	for i := range st.Cities {
		c, zone := &st.Cities[i], &g.data.Cities[i]
		zone.Impost, zone.PercImpost, zone.WinCount = c.Impost, int32(c.Tax), int32(c.Wins)
		w.cities[i].chall1, w.cities[i].chall2 = int32(c.Chall1), int32(c.Chall2)
		w.cities[i].bids = nil
		for _, b := range c.Bids {
			w.cities[i].bids = append(w.cities[i].bids, cityBid{int32(b.Guild), int32(b.Value)})
		}
	}
	g.updateCityTowers()
}

// saveChannelState is SaveGuildZone + WriteGameConfigv2, once a minute.
func (g *Game) saveChannelState() {
	w := &g.war
	p := wire.ChannelState{
		Header: wire.NewHeader(wire.OpChannelState, uint16(g.cfg.Channel)), WeekMode: int32(w.weekMode), CastleState: int32(w.castleState),
		CapeWin: w.capeWin, TowerGuild: w.towerGuild, KefraKiller: g.kefra.killer, KingdomWinner: g.kingdom.winner, RvRBonus: g.rvr.bonus,
	}
	if g.kefra.dead {
		p.KefraDead = 1
	}
	for i := range w.cities {
		zone := &g.data.Cities[i]
		c := &p.Cities[i]
		c.Impost, c.Tax, c.Wins, c.Chall1, c.Chall2 = zone.Impost, zone.PercImpost, zone.WinCount, w.cities[i].chall1, w.cities[i].chall2
		for j, b := range w.cities[i].bids {
			if j == len(c.Bids) {
				break
			}
			c.Bids[j] = wire.CityBid{Guild: b.guild, Value: b.value}
		}
	}
	g.sendDB(&p)
}

// isWarTime is IsWarTime: the tower war, a city war or the castle war.
func (g *Game) isWarTime() bool {
	return g.war.towerOn || g.war.weekMode == weekFighting || g.war.forceWeek == weekFighting || g.war.castleState != castleOff
}

func (g *Game) guildProcessTimer() { g.guildProcess(time.Now()) }

// guildProcess is GuildProcess and the tower war of ProcessSecTimer, once a second.
func (g *Game) guildProcess(now time.Time) {
	g.coliseumTick(now)
	g.castleTick(now)
	g.weekTick(now)
	g.towerTick(now)
	g.rvrTick(now)
}

func (g *Game) warSunday(now time.Time) bool { return now.Weekday() == time.Sunday }

func (g *Game) weekTick(now time.Time) {
	w := &g.war
	warHour := g.warSunday(now) && now.Hour() >= g.cfg.Wars.CityHour
	say := func(id int) { g.notice(g.lang(id)) }
	switch w.weekMode {
	case weekPrepare:
		if warHour || w.forceWeek == weekWarning {
			say(langBattle1)
			w.weekMode = weekWarning
		}
	case weekWarning:
		if (warHour && now.Minute() == 3) || w.forceWeek == weekTeleported {
			w.weekMode = weekTeleported
			g.clearArea(guildPKZone)
			g.teleportToWars()
		}
	case weekTeleported:
		if (warHour && now.Minute() == 6) || w.forceWeek == weekFighting {
			say(langBattle3)
			w.weekMode = weekFighting
			g.setArenaDoor(gateOpen)
		}
	case weekFighting:
		if (warHour && now.Minute() >= 30) || w.forceWeek == weekCollect {
			say(langBattle4)
			w.weekMode = weekCollect
			g.decideWinner()
			g.clearArea(guildPKZone)
			g.clearChallengers()
			g.setArenaDoor(gateLocked)
			g.saveChannelState()
		}
	case weekCollect:
		if now.Weekday() == time.Saturday || w.forceWeek == weekBetting {
			say(langBattle5)
			w.weekMode = weekBetting
		}
	case weekBetting:
		if g.warSunday(now) || w.forceWeek == weekPrepare {
			g.decideChallengers()
			say(langBattle6)
			w.weekMode = weekPrepare
		}
	}
}

// setArenaDoor is SetArenaDoor: the three gates of each city's arena.
func (g *Game) setArenaDoor(mode int) {
	for i := range 4 {
		for x := range 3 {
			id := i*3 + x + 1
			gi := &g.ground[id]
			if gi.item.Index == 0 || int(gi.item.Index) >= MaxItemList {
				continue
			}
			height, _ := g.updateGroundItem(id, mode)
			g.broadcastGate(id, height)
			gi.closeArmed = false
		}
	}
}

// warEvolutionOK: each city's war is for one class. The C++ checks the
// defenders of cities 0, 1 and 3 and only city 2 for the challengers.
func warEvolutionOK(city, ev int, defender bool) bool {
	switch {
	case city == 2:
		return ev == evoMortal
	case !defender:
		return true
	case city == 3:
		return ev == evoArch
	default:
		return ev >= evoCelestial
	}
}

func (g *Game) teleportToWars() {
	for city := range 4 {
		z := &g.data.Cities[city]
		w := &g.war.cities[city]
		defender := g.cityOwner(city)
		if defender == 0 {
			defender = w.chall2
		}
		g.teleportGuildToWar(city, defender, true, int(z.GuildAWarX), int(z.GuildAWarY))
		g.teleportGuildToWar(city, w.chall1, false, int(z.GuildBWarX), int(z.GuildBWarY))
	}
}

func (g *Game) teleportGuildToWar(city int, guild int32, defender bool, x, y int) {
	if guild == 0 {
		return
	}
	total := 0
	for _, u := range g.users[1:] {
		if total >= warMaxTeleport {
			return
		}
		m := &g.mobs[u.id]
		pl := &m.info.Player
		if u.status != userPlay || m.mode == 0 || u.store.open || int32(pl.GuildIndex) != guild || g.data.village(int(m.target.X), int(m.target.Y)) != city {
			continue
		}
		if !warEvolutionOK(city, evolution(pl), defender) {
			g.clientMessage(u, "%s", g.lang(langVillageLimit))
			continue
		}
		m.lifes = warLives
		total++
		g.teleportar(u.id, x, y)
		g.sendScore(u.id)
	}
}

// warLifeSaved is the city-war part of MobKilled: a fighter with lives left
// stands up where it fell.
func (g *Game) warLifeSaved(target int) bool {
	w := &g.war
	if target >= MaxPlayer || (w.weekMode != weekFighting && w.forceWeek != weekFighting) {
		return false
	}
	m := &g.mobs[target]
	city := g.data.warArea(int(m.target.X), int(m.target.Y))
	if city < 0 || city >= len(w.cities) {
		return false
	}
	guild := int32(m.info.Player.GuildIndex)
	if guild == 0 || (guild != w.cities[city].chall1 && guild != w.cities[city].chall2 && guild != g.cityOwner(city)) {
		return false
	}
	m.lifes--
	if m.lifes < 0 {
		return false
	}
	u := g.users[target]
	s := &m.info.Player.Status
	s.CurHP, s.CurMP = s.MaxHP, s.MaxMP
	u.potionHP, u.potionMP = s.MaxHP, s.MaxMP
	g.setReqHp(u)
	g.setReqMp(u)
	if i := slices.IndexFunc(m.info.Affects[:], func(e wire.Affect) bool { return e.Index == arenaProtectAura }); i >= 0 {
		m.info.Affects[i] = wire.Affect{}
		g.sendAffect(u)
	}
	g.clientMessage(u, "Você possui %d vidas restantes", m.lifes)
	g.sendScore(target)
	return true
}

// decideWinner is DecideWinner: the side with the most levels standing in
// the war zone keeps or takes the city (the defender wins a draw).
func (g *Game) decideWinner() {
	for city := range 4 {
		zone := &g.data.Cities[city]
		w := &g.war.cities[city]
		owner, chall := g.cityOwner(city), w.chall1
		if owner == 0 {
			owner = w.chall2
		}
		if chall == 0 {
			zone.WinCount = min(zone.WinCount+1, 4)
			continue
		}
		var ownerPts, challPts int
		for _, u := range g.users[1:] {
			m := &g.mobs[u.id]
			x, y := int32(m.target.X), int32(m.target.Y)
			if u.status != userPlay || m.mode == 0 || m.info.Player.Status.CurHP <= 0 || x < zone.WarMinX || x >= zone.WarMaxX || y < zone.WarMinY || y >= zone.WarMaxY {
				continue
			}
			lv := int(m.info.Player.Status.Level) + 1
			switch int32(m.info.Player.GuildIndex) {
			case 0:
			case owner:
				ownerPts += lv
			case chall:
				challPts += lv
			}
		}
		msg := g.lang(langWarResult)
		if ownerPts >= challPts {
			g.guildNotice(int(owner), msg, g.guildName(int(owner)), g.guildName(int(chall)), ownerPts, challPts)
			g.guildNotice(int(chall), msg, g.guildName(int(owner)), g.guildName(int(chall)), ownerPts, challPts)
			if g.cityOwner(city) == 0 {
				zone.WinCount = -1
			}
			zone.WinCount = min(zone.WinCount+1, 4)
			g.setCityOwner(city, owner)
		} else {
			g.guildNotice(int(owner), msg, g.guildName(int(chall)), g.guildName(int(owner)), challPts, ownerPts)
			g.guildNotice(int(chall), msg, g.guildName(int(chall)), g.guildName(int(owner)), challPts, ownerPts)
			g.setCityOwner(city, chall)
			zone.WinCount = 0
		}
		g.log.Info("war: city war over", "city", city, "owner", owner, "owner_points", ownerPts, "challenger", chall, "challenger_points", challPts)
		w.chall1, w.chall2 = 0, 0
	}
	// A castle owner without a city loses the castle; a guild with all
	// four cities takes it.
	castle := &g.data.Cities[cityNoatun]
	if c := g.cityOwner(cityNoatun); c != 0 {
		has := false
		for i := range 4 {
			has = has || g.cityOwner(i) == c
		}
		if !has {
			g.setCityOwner(cityNoatun, 0)
			castle.WinCount = 0
		}
	}
	all := g.cityOwner(0)
	for i := 1; i < 4; i++ {
		if g.cityOwner(i) != all {
			all = -1
		}
	}
	if all >= 0 && all != g.cityOwner(cityNoatun) {
		g.setCityOwner(cityNoatun, all)
		castle.WinCount = 0
	}
	g.updateCityTowers()
}

// updateCityTowers is UpdateCityTowers: each city's tower shows its owner
// and how many wars it won.
func (g *Game) updateCityTowers() {
	first := g.firstCityTower()
	if first < 0 {
		return
	}
	for t := range 5 {
		g.setCityTower(first, t)
	}
}

func (g *Game) firstCityTower() int {
	for i := range g.ground {
		if id := g.ground[i].item.Index; id >= towerFirst && id <= towerFirst+4 {
			return i
		}
	}
	return -1
}

func (g *Game) setCityTower(first, city int) {
	if first+city >= len(g.ground) {
		return
	}
	it := &g.ground[first+city].item
	owner := g.cityOwner(city)
	it.Index = uint16(towerFirst + max(g.data.Cities[city].WinCount, 0))
	it.Effect[0].Index, it.Effect[0].Value = 56, uint16(owner/257)
	it.Effect[1].Index, it.Effect[1].Value = 57, uint16(owner)
	it.Effect[2].Index, it.Effect[2].Value = 59, uint16(rnd()%255)
}

// decideChallengers is DecideChallenger: the highest bet challenges a
// city's owner; a city without an owner takes the two highest.
func (g *Game) decideChallengers() {
	for city := range 4 {
		w := &g.war.cities[city]
		take := func() int32 {
			if len(w.bids) == 0 {
				return 0
			}
			best := 0
			for i, b := range w.bids {
				if b.value > w.bids[best].value {
					best = i
				}
			}
			guild := w.bids[best].guild
			w.bids = slices.Delete(w.bids, best, best+1)
			return guild
		}
		if g.cityOwner(city) != 0 {
			if guild := take(); guild != 0 {
				w.chall1 = guild
			}
			continue
		}
		if guild := take(); guild != 0 {
			w.chall1 = guild
		}
		if guild := take(); guild != 0 {
			w.chall2 = guild
		}
	}
}

func (g *Game) clearChallengers() {
	for i := range g.war.cities {
		g.war.cities[i] = cityWar{}
	}
}

// castleTick is the castle part of GuildProcess: Sundays at wars.castle_hour.
func (g *Game) castleTick(now time.Time) {
	w := &g.war
	if !g.warSunday(now) {
		return
	}
	hour, minute := now.Hour(), now.Minute()
	if hour == g.cfg.Wars.CastleHour+1 && minute >= 30 && w.castleState != castleOff {
		w.castleState = castleOff
	}
	if hour != g.cfg.Wars.CastleHour {
		return
	}
	switch {
	case w.castleState == castleOff && minute == 0:
		g.notice(g.lang(langCastleWarn))
		w.castleState = castleClosing
		g.broadcastCastleState(int32(w.castleState))
	case w.castleState == castleClosing && minute == 5:
		g.startCastleWar()
	case w.castleState == castleOpen && minute == 50:
		g.notice(g.lang(langCastleEnding))
		w.castleState = castleEnding
	case w.castleState == castleEnding && minute >= 55:
		g.notice(g.lang(langCastleClosed))
		g.finishCastleWar()
	}
	if (w.castleState == castleOpen || w.castleState == castleEnding) && minute < 55 {
		g.altarTick()
	}
}

func (g *Game) broadcastCastleState(v int32) {
	g.forEachPlaying(func(u *User) { g.signalParm(u, wire.ClientIDSystem, opSrvCastleState, v) })
}

func (g *Game) startCastleWar() {
	g.clearArea(castleArea)
	for _, z := range castleOutGates {
		g.clearAreaTeleport(z, 1057, 1742)
	}
	g.setDoors(33, 4, gateLocked, false)
	for i := range 3 {
		g.generateMob(genNoatunTower+i, 0, 0)
	}
	g.notice(g.lang(langCastleOpen))
	g.war.castleState = castleOpen
}

// altarTick: a guild leader who stays on the altar for two minutes takes
// the castle. The guild must hold a city.
func (g *Game) altarTick() {
	w := &g.war
	id := int(g.cell(altarX, altarY))
	if id != w.altar {
		w.altar, w.altarTicks = 0, 0
		if id > 0 && id < MaxPlayer {
			g.altarArrive(id)
		}
	}
	if w.altar == 0 {
		return
	}
	m := &g.mobs[w.altar]
	if w.altarTicks == 0 {
		g.notice(fmt.Sprintf(g.lang(langCharging), g.charName(w.altar)))
	}
	g.multicast2(int(m.target.X), int(m.target.Y), &wire.Parm1{Header: wire.NewHeader(opSrvAltarCharge, wire.ClientIDSystem), Parm1: int32(w.altar) | 1<<16}, 0)
	w.altarTicks++
	if left := altarSeconds - w.altarTicks; left > 0 && left%10 == 0 {
		g.chatAll(arenaAnnounce, fmt.Sprintf("Faltam %d segundos para a conquista do castelo!", left))
	}
	if w.altarTicks <= altarSeconds {
		return
	}
	g.notice(fmt.Sprintf(g.lang(langCharged), g.charName(w.altar)))
	g.setCityOwner(cityNoatun, int32(m.info.Player.GuildIndex))
	g.data.Cities[cityNoatun].WinCount = 0
	w.capeWin = int32(m.info.Player.CapeInfo)
	g.finishCastleWar()
	g.forEachPlaying(g.clearCrown)
}

func (g *Game) altarArrive(id int) {
	u := g.users[id]
	pl := &g.mobs[id].info.Player
	if pl.GuildMemberType != 9 {
		g.clientMessage(u, "Somente o líder da guilda pode conquistar o Castelo")
		return
	}
	guild := int32(pl.GuildIndex)
	if g.cityOwner(cityNoatun) == guild {
		g.clientMessage(u, "Você é dono do Castelo")
		return
	}
	for i := range 4 {
		if g.cityOwner(i) == guild {
			g.war.altar = id
			return
		}
	}
	g.clientMessage(u, "Para conquistar o Castelo você deve possuir pelo menos uma cidade")
}

// finishCastleWar is FinishCastleWar.
func (g *Game) finishCastleWar() {
	w := &g.war
	g.setDoors(33, 4, gateOpen, false)
	w.castleState = castleCollect
	g.forEachPlaying(func(u *User) {
		g.signal(u, wire.ClientIDSystem, opSrvCastleState)
		g.signalParm(u, wire.ClientIDSystem, opSrvCastleState, 0)
	})
	for id := firstNPC; id < MaxMob; id++ {
		if g.mobs[id].mode != 0 && g.mobs[id].info.Player.Equip[0].Index == 219 {
			g.deleteMob(id, 2)
		}
	}
	w.altar, w.altarTicks = 0, 0
	g.clearArea(castleArea)
	if first := g.firstCityTower(); first >= 0 {
		g.setCityTower(first, cityNoatun)
	}
	w.towerGuild = 0
	for id, x := range g.guilds {
		if x.Name != "" {
			g.setGuildWins(id, 0)
		}
	}
	g.saveChannelState()
}

// towerTick starts and ends the Erion tower war on weekdays.
func (g *Game) towerTick(now time.Time) {
	if wd := now.Weekday(); wd == time.Sunday || wd == time.Saturday || now.Hour() != g.cfg.Wars.TowerHour {
		return
	}
	w := &g.war
	switch {
	case now.Minute() == 0 && !w.towerOn:
		w.towerOn = true
		g.generateMob(genTowerErion, 0, 0)
		g.clearArea(towerWarArea)
		g.notice("Guerra de Torres iniciada!")
		g.chatAll(arenaAnnounce, "[SERVIDOR] Guerra de Torres iniciada")
	case now.Minute() == 30 && w.towerOn:
		g.finishTowerWar()
	}
}

func (g *Game) finishTowerWar() {
	w := &g.war
	w.towerOn = false
	for id := firstNPC; id < MaxMob; id++ {
		if g.mobs[id].mode != 0 && g.mobs[id].npc.generateID == genTowerErion {
			g.deleteMob(id, 1)
		}
	}
	if guild := int(w.towerGuild); guild != 0 {
		x := g.guild(guild)
		fame, wins := x.Fame+guildFameBet, x.Wins+1
		g.setGuildFame(guild, fame)
		g.setGuildWins(guild, wins)
		g.notice(fmt.Sprintf("Guerra de Torres Finalizada. A Guild [%s] foi a Vencedora!", g.guildName(guild)))
		g.log.Info("war: tower won", "guild", guild, "fame", fame, "wins", wins)
	} else {
		g.notice("Guerra de Torres Finalizada. Nenhuma guilda foi declarada vencedora.")
	}
	w.towerGuild = 0
	g.clearArea(towerWarArea)
}

// erionTowerKilled: the tower stands up at once, on the side of the guild
// that took it down.
func (g *Game) erionTowerKilled(target, killer int) {
	if owner := g.summonOwner(killer); owner != 0 {
		killer = owner
	}
	m := &g.mobs[target]
	m.info.Player.Status.CurHP = m.info.Player.Status.MaxHP
	if killer >= MaxPlayer || g.users[killer].status != userPlay {
		g.sendGridMob(target)
		return
	}
	guild := int32(g.mobs[killer].info.Player.GuildIndex)
	g.war.towerGuild = guild
	g.notice(fmt.Sprintf("O jogador %s da guild %s derrubou a Torre.", g.charName(killer), g.guildName(int(guild))))
	m.info.Player.GuildIndex = uint16(guild)
	g.sendGridMob(target)
	g.clearArea(towerWarArea)
}

// warHitBlocked is RequestAttack's tower and kingdom rules: nobody hits the
// tower of its own side.
func (g *Game) warHitBlocked(attacker, target int) bool {
	a, t := &g.mobs[attacker].info.Player, &g.mobs[target]
	if target >= MaxPlayer && t.npc.generateID == genTowerErion {
		guild := int(a.GuildIndex)
		if guild == 0 || int(t.info.Player.GuildIndex) == guild {
			return true
		}
		if ally := g.guildAlly[guild]; ally != 0 && ally == int(t.info.Player.GuildIndex) {
			return true
		}
	}
	return g.pistaHitBlocked(attacker, target) || g.rvrHitBlocked(attacker, target)
}

// requestChallenge is RequestReqChallange (0x28E): the tax NPC of each city.
func (g *Game) requestChallenge(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if u.status != userPlay || wire.Unmarshal(pkt.Raw, &p) != nil || int(p.Value) < firstNPC || int(p.Value) >= MaxMob {
		return
	}
	npc := int(p.Value)
	city := int(g.mobs[npc].info.Player.BStatus.Level)
	if g.mobs[npc].mode == 0 || city < 0 || city >= 5 {
		return
	}
	g.taxNPC(u, npc, city, time.Now())
}

func (g *Game) taxNPC(u *User, npc, city int, now time.Time) {
	w := &g.war
	zone := &g.data.Cities[city]
	switch now.Weekday() {
	case time.Saturday:
		if city != cityNoatun {
			g.challenge(u, npc, city)
		}
		return
	case time.Sunday:
	default:
		g.sendSay(npc, "Impostos recolhidos: %s", formatThousands(zone.Impost))
		return
	}
	owner := g.cityOwner(city)
	display := owner
	if display == 0 {
		display = w.cities[city].chall2
	}
	chall := w.cities[city].chall1
	switch {
	case w.weekMode <= weekFighting:
		switch {
		case display == 0 && chall != 0:
			g.sendSay(npc, g.lang(langWinnerIs), g.guildName(int(chall)))
		case display != 0 && chall != 0:
			g.sendSay(npc, g.lang(langChampionAnd), g.guildName(int(display)), g.guildName(int(chall)))
		default:
			g.sendSay(npc, "%s", g.lang(langNoChallenger))
		}
	case w.weekMode == weekCollect || w.castleState == castleCollect:
		pl := &g.mobs[u.id].info.Player
		if owner != 0 && owner == int32(pl.GuildIndex) && pl.GuildMemberType == 9 && g.canCollectTax(city) {
			g.collectTax(u, npc, city)
			return
		}
		if owner != 0 {
			g.sendSay(npc, g.lang(langWinnerIs), g.guildName(int(owner)))
		} else {
			g.sendSay(npc, "%s", g.lang(langNoChallenger))
		}
	case w.weekMode == weekBetting:
		g.signal(u, 0, 0x18D)
	}
}

func (g *Game) canCollectTax(city int) bool {
	if city != cityNoatun {
		return g.war.weekMode == weekCollect
	}
	return g.war.castleState == castleCollect
}

func (g *Game) collectTax(u *User, npc, city int) {
	zone := &g.data.Cities[city]
	pl := &g.mobs[u.id].info.Player
	gold := zone.Impost
	if pl.Gold+gold > g.cfg.Limits.MaxGold {
		g.sendSay(npc, "%s", g.lang(nnCantGetMoreThan2G))
		return
	}
	pl.Gold += gold
	zone.Impost = 0
	if city == cityNoatun {
		g.war.castleState = castleOff
	}
	g.log.Info("war: taxes collected", "slot", u.id, "city", city, "gold", gold)
	g.sendSay(npc, "Impostos recolhidos")
	g.sendCarry(u)
	g.sendEtc(u)
	g.saveChannelState()
}

// challenge is Challange: a leader bets 100 guild fame and one tower win.
func (g *Game) challenge(u *User, npc, city int) {
	pl := &g.mobs[u.id].info.Player
	guild := int(pl.GuildIndex)
	if pl.GuildMemberType != 9 || guild == 0 {
		g.sendSay(npc, "%s", g.lang(langGuildMaster))
		return
	}
	x := g.guild(guild)
	switch {
	case x.Name == "":
		g.sendSay(npc, "%s", g.lang(langNamedGuild))
	case x.Wins == 0:
		g.sendSay(npc, "Para apostar você deve ter conquistado a Torre de Erion uma vez pelo menos")
	case x.Fame < guildFameBet:
		g.sendSay(npc, "%s", g.lang(langNoMoney))
	case g.ownsCityElsewhere(int32(guild)):
		g.sendSay(npc, "Não é possível apostar enquanto possuir cidade em outro servidor")
	case int(g.cityOwner(city)) == guild:
		g.sendSay(npc, "%s", g.lang(langChampionsCant))
	default:
		fame := x.Fame
		g.setGuildFame(guild, fame-guildFameBet)
		g.setGuildWins(guild, x.Wins-1)
		g.clientMessage(u, "Aposta realizada.")
		g.sendEtc(u)
		g.placeBid(city, int32(guild), int32(fame))
		g.log.Info("war: bet placed", "guild", guild, "city", city, "value", fame)
	}
}

// ownsCityElsewhere: the guild holds a city on another channel.
func (g *Game) ownsCityElsewhere(guild int32) bool {
	for ch, cities := range g.chargedGuilds {
		if ch != g.cfg.Channel-1 && slices.Contains(cities[:], guild) {
			return true
		}
	}
	return false
}

func (g *Game) placeBid(city int, guild, value int32) {
	w := &g.war.cities[city]
	if i := slices.IndexFunc(w.bids, func(b cityBid) bool { return b.guild == guild }); i >= 0 {
		w.bids[i].value = value
	} else {
		w.bids = append(w.bids, cityBid{guild, value})
	}
	slices.SortStableFunc(w.bids, func(a, b cityBid) int { return int(b.value) - int(a.value) })
	if len(w.bids) > maxCityBids {
		w.bids = w.bids[:maxCityBids]
	}
}

// guildTaxCommand is the "guildtax N" chat line.
func (g *Game) guildTaxCommand(u *User, text string) {
	var taxes int
	if n, _ := fmt.Sscanf(text, "guildtax %d", &taxes); n != 1 || taxes < 0 || taxes > 15 {
		g.clientMessage(u, "%s", g.lang(langTaxRange))
		return
	}
	m := &g.mobs[u.id]
	city := g.data.village(int(m.target.X), int(m.target.Y))
	if city < 0 || city >= 5 {
		g.clientMessage(u, "%s", g.lang(langOnlyVillage))
		return
	}
	if int32(m.info.Player.GuildIndex) != g.cityOwner(city) || m.info.Player.GuildMemberType != 9 {
		g.clientMessage(u, "%s", g.lang(langOnlyMaster))
		return
	}
	g.data.Cities[city].PercImpost = int32(taxes)
	g.clientMessage(u, g.lang(langTaxSet), taxes)
}

// requestDeclareWar is RequestDeclareWar (0xED7). The C++ takes the item
// and does nothing else; so does this.
func (g *Game) requestDeclareWar(u *User, pkt protocol.Packet) {
	if u.status != userPlay {
		return
	}
	pl := &g.mobs[u.id].info.Player
	if pl.GuildMemberType != 9 || g.cityOwner(cityNoatun) != int32(pl.GuildIndex) {
		return
	}
	slot := slices.IndexFunc(pl.Inventory[:60], func(it wire.Item) bool { return it.Index == itemWarDeclare })
	if slot < 0 {
		return
	}
	amountMinus(&pl.Inventory[slot])
	g.sendItem(u, wire.SlotInv, slot, &pl.Inventory[slot])
}

// useWarRefusal is the Recusa de Guerra: only the leader of the crown's
// guild can use it. As in the C++, it has no other effect.
func (g *Game) useWarRefusal(u *User, p *wire.UseItem, src *wire.Item) bool {
	if src.Index != itemWarRefusal {
		return false
	}
	pl := &g.mobs[u.id].info.Player
	switch {
	case pl.GuildMemberType != 9:
		g.clientMessage(u, "Somente para líder de guild.")
		g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
	case g.cityOwner(cityNoatun) != int32(pl.GuildIndex):
		g.clientMessage(u, "Necessário possuir coroa.")
		g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
	default:
		amountMinus(src)
		g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
	}
	return true
}

// clearCrown is ClearCrown: the leader of the castle's guild gets the crown
// and the dragon of its kingdom; anyone else loses them.
func (g *Game) clearCrown(u *User) {
	pl := &g.mobs[u.id].info.Player
	guild := int32(pl.GuildIndex)
	if guild == 0 {
		return
	}
	isDragon := func(it wire.Item) bool { return it.Index == 3993 || it.Index == 3994 }
	isCrown := func(it wire.Item) bool { return it.Index == itemCrown }
	crown := false
	for _, cities := range g.chargedGuilds { // a castle on any channel
		crown = crown || cities[cityNoatun] == guild
	}
	if pl.GuildMemberType != 9 || !crown {
		g.removeCrownItems(u, func(it wire.Item) bool { return isCrown(it) || isDragon(it) })
		return
	}
	has := func(match func(wire.Item) bool) bool {
		return slices.ContainsFunc(pl.Equip[:], match) || slices.ContainsFunc(pl.Inventory[:], match) || slices.ContainsFunc(u.account.StorageItems[:], match)
	}
	if !has(isCrown) {
		it := wire.Item{Index: itemCrown}
		it.Effect[0].Index, it.Effect[0].Value = 43, crownRefine(g.guild(int(guild)).Fame)
		g.putItem(u, &it)
	}
	if !has(isDragon) {
		dragon := wire.Item{Index: 3994}
		if pl.CapeInfo == 8 {
			dragon.Index = 3993
		}
		g.putItem(u, &dragon)
	}
}

func (g *Game) removeCrownItems(u *User, match func(wire.Item) bool) {
	pl := &g.mobs[u.id].info.Player
	for i := range pl.Equip {
		if match(pl.Equip[i]) {
			pl.Equip[i] = wire.Item{}
			g.sendItem(u, wire.SlotEquip, i, &pl.Equip[i])
		}
	}
	for i := range pl.Inventory {
		if match(pl.Inventory[i]) {
			pl.Inventory[i] = wire.Item{}
			g.sendItem(u, wire.SlotInv, i, &pl.Inventory[i])
		}
	}
	for i := range u.account.StorageItems {
		if match(u.account.StorageItems[i]) {
			u.account.StorageItems[i] = wire.Item{}
			g.sendItem(u, wire.SlotStorage, i, &u.account.StorageItems[i])
		}
	}
}

func crownRefine(fame int) uint16 {
	switch {
	case fame < 1000:
		return 9
	case fame < 1500:
		return 233
	case fame < 3000:
		return 237
	case fame < 5000:
		return 241
	case fame < 8000:
		return 245
	case fame < 11000:
		return 249
	}
	return 253
}

func formatThousands(v int64) string {
	s := fmt.Sprint(v)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// warLogin is what SendCharToWorld tells a player about the wars.
func (g *Game) warLogin(u *User) {
	g.sendWarInfo(u)
	if g.war.castleState != castleOff {
		g.signalParm(u, wire.ClientIDSystem, opSrvCastleState, 1)
	}
	g.clearCrown(u)
	if g.kefra.killer != 0 {
		g.clientMessage(u, g.lang(langKefraGuild), g.guildName(int(g.kefra.killer)))
	} else if g.kefra.dead {
		g.clientMessage(u, "%s", g.lang(langKefraPlayer))
	}
	g.kingdomLogin(u)
	g.nightmareLogin(u)
}

// ChannelNumber is this TMSRV's channel (1..10).
func (g *Game) ChannelNumber() int { return g.cfg.Channel }
