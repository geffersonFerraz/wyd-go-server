package game

import (
	"strings"
	"testing"
	"time"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

func warClock(day time.Weekday, hour, minute int) time.Time {
	// 2026-11-08 is a Sunday.
	return time.Date(2026, 11, 8+int(day), hour, minute, 0, 0, time.Local)
}

func warGame(t *testing.T, n int) *Game {
	g := arenaGame(t, n)
	g.cfg.Wars = WarSettings{CityHour: 20, CastleHour: 21, TowerHour: 22, RvRHour: 20}
	return g
}

func setGuild(g *Game, id, guild int, leader bool) {
	pl := &g.mobs[id].info.Player
	pl.GuildIndex = uint16(guild)
	pl.GuildMemberType = 0
	if leader {
		pl.GuildMemberType = 9
	}
}

func TestWeekCycle(t *testing.T) {
	g := warGame(t, 1)
	g.war.weekMode = weekCollect
	steps := []struct {
		at   time.Time
		want int
	}{
		{warClock(time.Friday, 12, 0), weekCollect},
		{warClock(time.Saturday, 0, 0), weekBetting},
		{warClock(time.Sunday, 0, 0), weekPrepare},
		{warClock(time.Sunday, 19, 59), weekPrepare},
		{warClock(time.Sunday, 20, 0), weekWarning},
		{warClock(time.Sunday, 20, 3), weekTeleported},
		{warClock(time.Sunday, 20, 6), weekFighting},
		{warClock(time.Sunday, 20, 29), weekFighting},
		{warClock(time.Sunday, 20, 30), weekCollect},
	}
	for _, s := range steps {
		g.weekTick(s.at)
		if g.war.weekMode != s.want {
			t.Fatalf("%v: week mode %d, want %d", s.at, g.war.weekMode, s.want)
		}
	}
}

func TestChallengeTakesFameAndWin(t *testing.T) {
	g := warGame(t, 1)
	setGuild(g, 1, 7, true)
	g.guilds[7] = &Guild{Name: "Sete", Fame: 300, Wins: 2}
	g.challenge(g.users[1], 1, 0)
	if x := g.guilds[7]; x.Fame != 200 || x.Wins != 1 {
		t.Fatalf("guild after the bet: %+v", x)
	}
	if bids := g.war.cities[0].bids; len(bids) != 1 || bids[0].guild != 7 || bids[0].value != 300 {
		t.Fatalf("bids: %+v", bids)
	}
	g.guilds[7].Wins = 0
	g.challenge(g.users[1], 1, 0)
	if g.guilds[7].Fame != 200 {
		t.Fatal("a guild without a tower win could bet")
	}
}

func TestChallengeRefusedWithCityOnAnotherChannel(t *testing.T) {
	g := warGame(t, 1)
	other := g.cfg.Channel % 10 // any other channel slot
	g.chargedGuilds[other][2] = 7
	setGuild(g, 1, 7, true)
	g.guilds[7] = &Guild{Name: "Sete", Fame: 300, Wins: 2}
	g.challenge(g.users[1], 1, 0)
	if g.guilds[7].Fame != 300 || len(g.war.cities[0].bids) != 0 {
		t.Fatal("bet accepted while holding a city on another channel")
	}
}

func TestCityOwnersPerChannel(t *testing.T) {
	g := warGame(t, 1)
	ch := g.cfg.Channel
	other := ch%10 + 1
	g.data.ApplyCityOwners(ch, []store.CityOwner{{Channel: other, City: 1, GuildID: 99}})
	if g.data.Cities[1].OwnerIndex == 99 {
		t.Fatal("took the owner of another channel")
	}
	g.data.ApplyCityOwners(ch, []store.CityOwner{{Channel: ch, City: 1, GuildID: 42}})
	if g.data.Cities[1].OwnerIndex != 42 {
		t.Fatal("did not take this channel's owner")
	}
}

func TestDecideChallengers(t *testing.T) {
	g := warGame(t, 1)
	g.chargedGuilds[g.cfg.Channel-1][0] = 0
	g.chargedGuilds[g.cfg.Channel-1][1] = 50
	g.placeBid(0, 11, 100)
	g.placeBid(0, 12, 500)
	g.placeBid(0, 13, 300)
	g.placeBid(1, 21, 100)
	g.placeBid(1, 22, 200)
	g.decideChallengers()
	if c := g.war.cities[0]; c.chall1 != 12 || c.chall2 != 13 {
		t.Fatalf("city without owner: %+v", c)
	}
	if c := g.war.cities[1]; c.chall1 != 22 || c.chall2 != 0 {
		t.Fatalf("city with owner: %+v", c)
	}
}

func placeInWar(g *Game, id, city int) {
	z := &g.data.Cities[city]
	x, y := int(z.WarMinX)+id, int(z.WarMinY)+1
	g.mobs[id].target = wire.Position{X: uint16(x), Y: uint16(y)}
}

func TestDecideWinnerTakesCity(t *testing.T) {
	g := warGame(t, 3)
	ch := g.cfg.Channel - 1
	g.chargedGuilds[ch][0] = 7
	g.war.cities[0].chall1 = 8
	setGuild(g, 1, 7, true)
	setGuild(g, 2, 8, true)
	setGuild(g, 3, 8, false)
	for i := 1; i <= 3; i++ {
		placeInWar(g, i, 0)
	}
	g.decideWinner()
	if g.chargedGuilds[ch][0] != 8 {
		t.Fatalf("owner %d, want 8", g.chargedGuilds[ch][0])
	}
}

func TestDefenderWinsDraw(t *testing.T) {
	g := warGame(t, 2)
	ch := g.cfg.Channel - 1
	g.chargedGuilds[ch][0] = 7
	g.war.cities[0].chall1 = 8
	setGuild(g, 1, 7, true)
	setGuild(g, 2, 8, true)
	g.mobs[1].info.Player.Status.Level = 100
	g.mobs[2].info.Player.Status.Level = 100
	placeInWar(g, 1, 0)
	placeInWar(g, 2, 0)
	g.data.Cities[0].WinCount = 1
	g.decideWinner()
	if g.chargedGuilds[ch][0] != 7 || g.data.Cities[0].WinCount != 2 {
		t.Fatalf("owner %d, wins %d", g.chargedGuilds[ch][0], g.data.Cities[0].WinCount)
	}
}

func TestAllCitiesGiveCastle(t *testing.T) {
	g := warGame(t, 1)
	ch := g.cfg.Channel - 1
	for i := range 4 {
		g.chargedGuilds[ch][i] = 7
	}
	g.decideWinner()
	if g.chargedGuilds[ch][cityNoatun] != 7 {
		t.Fatal("the guild with every city did not get Noatun")
	}
}

func TestWarLives(t *testing.T) {
	g := warGame(t, 1)
	g.war.weekMode = weekFighting
	g.chargedGuilds[g.cfg.Channel-1][0] = 7
	setGuild(g, 1, 7, true)
	placeInWar(g, 1, 0)
	m := &g.mobs[1]
	m.lifes = 1
	m.info.Player.Status.CurHP = 0
	if !g.warLifeSaved(1) || m.info.Player.Status.CurHP != m.info.Player.Status.MaxHP || m.lifes != 0 {
		t.Fatalf("first death: hp %d lives %d", m.info.Player.Status.CurHP, m.lifes)
	}
	if g.warLifeSaved(1) {
		t.Fatal("no lives left, must die")
	}
}

func TestCollectTaxes(t *testing.T) {
	g := warGame(t, 1)
	g.war.weekMode = weekCollect
	g.chargedGuilds[g.cfg.Channel-1][0] = 7
	setGuild(g, 1, 7, true)
	g.data.Cities[0].Impost = 5000
	g.mobs[1].info.Player.Gold = 100
	npc := firstNPC + 1
	g.mobs[npc].mode = modeNPCIdle
	g.taxNPC(g.users[1], npc, 0, warClock(time.Sunday, 22, 0))
	if g.mobs[1].info.Player.Gold != 5100 || g.data.Cities[0].Impost != 0 {
		t.Fatalf("gold %d, tax left %d", g.mobs[1].info.Player.Gold, g.data.Cities[0].Impost)
	}
}

func TestGuildTaxCommand(t *testing.T) {
	g := warGame(t, 1)
	z := &g.data.Cities[0]
	g.mobs[1].target = wire.Position{X: uint16(z.CityMinX + 1), Y: uint16(z.CityMinY + 1)}
	g.chargedGuilds[g.cfg.Channel-1][0] = 7
	setGuild(g, 1, 7, true)
	g.guildTaxCommand(g.users[1], "guildtax 9")
	if z.PercImpost != 9 {
		t.Fatalf("tax %d", z.PercImpost)
	}
	g.guildTaxCommand(g.users[1], "guildtax 16")
	if z.PercImpost != 9 {
		t.Fatal("tax above 15 accepted")
	}
}

func TestAltarTakesCastle(t *testing.T) {
	g := warGame(t, 1)
	ch := g.cfg.Channel - 1
	g.chargedGuilds[ch][1] = 7
	setGuild(g, 1, 7, true)
	g.war.castleState = castleOpen
	g.clearGrid(1)
	g.mobs[1].target = wire.Position{X: altarX, Y: altarY}
	g.setCell(altarX, altarY, 1)
	for range altarSeconds + 1 {
		g.altarTick()
	}
	if g.chargedGuilds[ch][cityNoatun] != 7 || g.war.castleState != castleCollect {
		t.Fatalf("owner %d state %d", g.chargedGuilds[ch][cityNoatun], g.war.castleState)
	}
}

func TestAltarNeedsLeaderWithCity(t *testing.T) {
	g := warGame(t, 1)
	setGuild(g, 1, 7, false)
	g.war.castleState = castleOpen
	g.clearGrid(1)
	g.mobs[1].target = wire.Position{X: altarX, Y: altarY}
	g.setCell(altarX, altarY, 1)
	g.altarTick()
	if g.war.altar != 0 {
		t.Fatal("a member started charging the castle")
	}
}

func TestTowerWarGivesFameAndWin(t *testing.T) {
	g := warGame(t, 1)
	setGuild(g, 1, 7, true)
	g.guilds[7] = &Guild{Name: "Sete", Fame: 10, Wins: 1}
	g.war.towerOn, g.war.towerGuild = true, 7
	g.finishTowerWar()
	if x := g.guilds[7]; x.Fame != 110 || x.Wins != 2 || g.war.towerOn {
		t.Fatalf("after the tower: %+v on=%v", x, g.war.towerOn)
	}
}

func TestChannelStateRoundTrip(t *testing.T) {
	g := warGame(t, 1)
	st := &store.ChannelState{WeekMode: weekBetting, CastleState: castleCollect, KefraDead: true, KefraKiller: 4}
	st.Cities[2] = store.CityState{Impost: 77, Tax: 12, Wins: 3, Chall1: 5, Bids: []store.CityBid{{Guild: 9, Value: 100}}}
	g.RestoreChannelState(st)
	if g.war.weekMode != weekBetting || !g.kefra.dead || g.data.Cities[2].Impost != 77 || g.data.Cities[2].PercImpost != 12 || g.war.cities[2].chall1 != 5 || len(g.war.cities[2].bids) != 1 {
		t.Fatalf("restored: %+v %+v", g.war, g.data.Cities[2])
	}
}

func TestRvRCycle(t *testing.T) {
	g := warGame(t, 2)
	g.rvrTick(warClock(time.Monday, 20, 0))
	if !g.rvr.on {
		t.Fatal("RvR did not start")
	}
	g.mobs[1].info.Player.CapeInfo, g.mobs[2].info.Player.CapeInfo = capeBlue, capeRed
	g.mobs[1].target = wire.Position{X: 1100, Y: 2000}
	g.mobs[2].target = wire.Position{X: 1101, Y: 2000}
	if !g.rvrHitBlockedSameCape() {
		t.Fatal("same kingdom could be hit")
	}
	g.rvrKill(2, 1)
	if g.rvr.points[0] != 1 || g.mobs[1].info.RvRPoints != 1 {
		t.Fatalf("points %v", g.rvr.points)
	}
	g.rvrTick(warClock(time.Monday, 20, 30))
	if g.rvr.on || g.rvr.bonus != capeBlue {
		t.Fatalf("after the end: on=%v bonus=%d", g.rvr.on, g.rvr.bonus)
	}
	if g.kingdomRegen(capeBlue) != 2 {
		t.Fatal("the winner has no regen bonus")
	}
}

func (g *Game) rvrHitBlockedSameCape() bool {
	g.mobs[2].info.Player.CapeInfo = capeBlue
	blocked := g.rvrHitBlocked(1, 2)
	g.mobs[2].info.Player.CapeInfo = capeRed
	return blocked
}

func TestKingNeedsTowerDown(t *testing.T) {
	g := warGame(t, 1)
	id := firstNPC + 5
	g.mobs[id].mode = modeBattle
	g.mobs[id].npc.generateID = genKingBlue
	g.mobs[id].info.Player.Status.MaxHP = 500
	g.kingdom.info[0].towerAlive = true
	if !g.kingKilled(id, 1, genKingBlue) || g.mobs[id].info.Player.Status.CurHP != 500 {
		t.Fatal("king died with the tower standing")
	}
	g.kingdom.info[0].towerAlive = false
	if g.kingKilled(id, 1, genKingBlue) || g.kingdom.winner != capeRed {
		t.Fatalf("winner %d", g.kingdom.winner)
	}
}

func TestKefraDeathAndReborn(t *testing.T) {
	g := warGame(t, 1)
	setGuild(g, 1, 7, true)
	g.guilds[7] = &Guild{Name: "Sete", Fame: 10}
	id := firstNPC + 5
	g.mobs[id].mode = modeBattle
	g.mobs[id].npc.generateID = genKefra
	g.kefraKilled(id, 1)
	if !g.kefra.dead || g.kefra.killer != 7 || g.guilds[7].Fame != 110 {
		t.Fatalf("kefra %+v fame %d", g.kefra, g.guilds[7].Fame)
	}
	g.rebornKefra()
	if g.kefra.dead || g.kefra.killer != 0 {
		t.Fatalf("after reborn: %+v", g.kefra)
	}
}

func TestNightmareScrollEntersRoom(t *testing.T) {
	g := warGame(t, 1)
	z := &g.data.Cities[0]
	m := &g.mobs[1]
	m.target = wire.Position{X: uint16(z.CityMinX + 1), Y: uint16(z.CityMinY + 1)}
	m.info.Player.Equip[0].Effect[1].Value = evoMortal
	g.nightmare[nightmareM] = nightmareRoom{status: 1, timeLeft: 200}
	src := &m.info.Player.Inventory[0]
	*src = wire.Item{Index: 3000}
	if !g.useNightmareScroll(g.users[1], &wire.UseItem{}, src, 174) {
		t.Fatal("scroll not handled")
	}
	if g.nightmare[nightmareM].members[0] != 1 || src.Index != 0 {
		t.Fatalf("members %v item %d", g.nightmare[nightmareM].members[:2], src.Index)
	}
}

func TestNightmareRoomEnds(t *testing.T) {
	g := warGame(t, 1)
	g.nightmare[nightmareA] = nightmareRoom{status: 2, timeLeft: 2}
	g.nightmareTimer()
	if g.nightmare[nightmareA].status != 0 {
		t.Fatalf("room still on: %+v", g.nightmare[nightmareA].status)
	}
}

func TestWaterRoomClearedGivesScroll(t *testing.T) {
	g := warGame(t, 1)
	g.water[0][0] = waterRoom{time: 100, mode: 1, leader: 1}
	g.waterTimer()
	if g.water[0][0].mode != 2 {
		t.Fatalf("mode %d", g.water[0][0].mode)
	}
	if firstSlotOf(&g.mobs[1].info.Player, uint16(waterItemBase[0])) < 0 {
		t.Fatal("no scroll for the leader")
	}
}

func TestWaterRoomTimesOut(t *testing.T) {
	g := warGame(t, 1)
	g.genCount[waterGenBase[0]] = 5
	g.water[0][0] = waterRoom{time: 0, mode: 1, leader: 1}
	g.waterTimer()
	if g.water[0][0].time != -1 {
		t.Fatal("room not freed")
	}
}

func TestColiseumFinishes(t *testing.T) {
	g := warGame(t, 1)
	g.coliseum = coliseumState{level: 1, seconds: 1, closedWalls: true}
	g.coliseumRun()
	if g.coliseum.level != 0 {
		t.Fatal("time out did not finish it")
	}
}

func TestBossComesBackAndGivesFame(t *testing.T) {
	g := warGame(t, 1)
	setGuild(g, 1, 7, true)
	g.guilds[7] = &Guild{Name: "Sete", Fame: 1}
	g.cfg.Bosses = []BossSettings{{Fame: 50, RebornSeconds: 10, MaxSeconds: 3600, Geners: []int{1}}}
	g.bossTimer(time.Now())
	g.bosses[0].current, g.bosses[0].mob = 1, firstNPC+9
	g.mobs[firstNPC+9].mode = modeBattle
	g.bossKilled(firstNPC+9, 1)
	if g.guilds[7].Fame != 51 || g.bosses[0].current != 0 {
		t.Fatalf("fame %d boss %+v", g.guilds[7].Fame, g.bosses[0])
	}
}

func TestFormatThousands(t *testing.T) {
	if s := formatThousands(1234567); !strings.EqualFold(s, "1,234,567") {
		t.Fatal(s)
	}
}
