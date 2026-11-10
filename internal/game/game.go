// Package game is the TMSRV (game server) port.
//
// All world state lives in Game and is touched only by the loop goroutine
// (Run), mirroring the single Win32 message loop of the C++ server. Network
// goroutines only decode/encode packets and post events.
package game

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

const (
	MaxPlayer = 1000 // MAX_PLAYER: client slots 1..999; 0 is unused
	viewGrid  = 33   // VIEWGRIDX/Y
	halfGrid  = 16   // HALFGRIDX/Y
)

// User status (CUser.h).
const (
	userEmpty       = 0
	userAccept      = 1
	userLogin       = 2
	userSelChar     = 11
	userCharWait    = 12
	userCreWait     = 13
	userDelWait     = 14
	userPlay        = 22
	userSaving4Quit = 24
)

// User is CUser: one client slot.
type User struct {
	id     int
	conn   *clientConn
	status int
	ip     string

	account       wire.Account // the TMSRV copy of STRUCT_ACCOUNT (User)
	charList      wire.CharList
	keys          [16]byte
	tokenOK       bool
	numericFail   int
	accessLevel   int32
	lastGMMessage time.Time // /gm cooldown
	lastStack     time.Time // delayMoveItem
	crackCount    int       // CrackCount
	hitEventCount int       // AttackEventCount of the hit event
	isBanned      bool
	mac           [8]byte
	charSlot      int

	out [][]byte // queued plaintext packets, flushed by the send timer

	movementPacket uint16
	movementTS     uint32
	lastReceiveSec int
	lastPenalty    [4]int64 // RequestCreateChar -> 2h delete penalty

	whisperOff, guildChatOff, citizenChatOff, kingdomChatOff bool

	repurchase     [10]wire.Item // last items sold to NPCs
	repurchaseLast int

	trade                        wire.Trade // own offer; trade.ClientID = partner, 0 = none
	attackRange                  int
	dropKills                    map[string]map[int]int // kill counters of the drop table
	lastAttack                   time.Time
	lastDropList, lastDropSearch time.Time
	lastUsedItem                 time.Time
	honorMarket                  bool // MarketHonra: buying goes to the Honor Market
	cashShopOpens                time.Time
	// Roulette (CMob.DropRoll): the prize of the last spin and the bar of each box.
	rouletteReward wire.Item
	rouletteEvol   [maxRouletteBox]int16
	skillUsed      [256]time.Time // last use per skill (cooldowns)

	potionHP, potionMP int64 // Potion.CountHp/CountMp: pending HP/MP
	quaff              bool  // Potion.bQuaff: apply on the next tick
	returnX, returnY   int   // return scroll point (nTargetX/Y)

	partyInvite   int // leader whose invite is pending
	partyInviteAt time.Time
	pkMode        bool // AllStatus.PK: PK mode on
	lastTrade     time.Time

	guildInvite   guildInvite // recruit offer waiting for an answer
	lastWhisper   int         // LastWhisper (for "/r")
	citizenChatAt time.Time   // last kingdom/citizen chat
	subSwapAt     time.Time   // last Pedra Misteriosa (5 s delay)
	lastDelete    time.Time   // Times.LastDeletedItem (logged when < 400 ms)
	lastMoveItem  time.Time   // delayMoveItem (stack split, 700 ms)
	questAccess   int         // QuestAccess: newbie quest entered (1..5)
	questTime     int         // CheckQuestTime: seconds left in a Lanhouse
	captchaCode   string      // passCaptcha: the digits being asked
	captchaTime   int         // TimeCaptcha: seconds until the window closes
	captchaTicket uint16      // QuestsCaptcha: the Lanhouse ticket being used
	arenaTeam     int         // Arena.GroupIndex + 1; 0 = not fighting
	arenaKills    int         // Arena.Kills
	regenTurns    int         // CharLoginTime: RegenMob turns since login
	lock          accountLock // User.Block
	challengeTo   int         // Challenger.Index: the player invited to a duel
	challengeType int         // Challenger.Type
	lastDuel      time.Time
	store         autoTrade // AutoTrade: the auction store
	lastAuction   time.Time // delayLeilaoBusca

	changeChannelTo int       // channel asked of DBSRV (0 = none pending)
	changeChannelAt time.Time // last channel change request

	snd           string // SNDMessage: the "/snd" away message shown to whisperers
	summonedUser  int    // SummonedUser: who this player called with "/summon"
	partyPass     string // PartyPassword (group finder)
	reconnect     bool   // CMob.Reconnect (0x99F): the client reconnects to its group
	follow        int    // CMob.Follow: auto-follow the party leader ("/atvfll")
	waterCount    [3]int // CMob.WaterCount (N, M, A rooms cleared)
	lastGroupFind time.Time
}

// Mob is CMob for players (index = user id). NPCs come in Phase B.
type Mob struct {
	mode         int
	info         wire.CharInfo // Mobs
	target, last wire.Position
	lastTime     uint32
	lastSpeed    uint32
	route        [24]byte
	motion       int
	spawnType    int
	lifes        int32
	guildDisable bool
	tab          [26]byte
	npc          npcState // NPCs only (id >= 1000)
	combat       combatState
	evoca        [maxEvoca]int // EvocaList: this player's summons
	storeOwner   int           // auction store NPC: the player selling
}

// Game is the server.
type Game struct {
	cfg  Config
	data *Data
	log  *slog.Logger

	users [MaxPlayer]*User
	mobs  []Mob    // MaxMob: players 1..999, NPCs 1000..
	grid  []uint16 // g_pMobGrid [y*4096+x]

	events chan event
	db     *dbLink
	start  time.Time

	chargedGuilds [10][5]int32
	griffinNPC    int                      // cached id of the Mestre Grifo
	rankTable     *rankBoard               // nil until the first read from the database
	sealCache     map[int32]sealCacheEntry // sServer.SealCache
	guilds        map[int]*Guild           // g_pGuild (SetGuilds, then DBSRV broadcasts)
	guildAlly     map[int]int              // g_pGuildAlly
	guildWar      map[int]int              // g_pGuildWar
	sapphire      int32
	secCounter    int
	saveCursor    int

	genCount               []int // MobCount per generator
	duel                   duelLetter
	arena                  arenaState
	war                    warState
	kefra                  kefraState
	kingdom                kingdomState
	rvr                    rvrState
	coliseum               coliseumState
	nightmare              [3]nightmareRoom
	water                  [3][9]waterRoom
	nightmareLastMin       int
	weather, forceWeather  int
	zakum                  zakumState
	bosses                 []bossState
	bigCuboCount           int
	uniCount, phoenixCount int // sServer.dropuni / dropfenix
	pista                  [pistaRooms]pistaRoom
	dropAll                int      // +drop: one rate for every slot (0 = tmsrv.yaml)
	adminOut               []string // answers of the GM command being run
	expRate                int      // +bonusexp (sServer.BonusEXP)
	macBans                map[[8]byte]bool
	minCounter             int
	npcCursor              int

	ground      [maxGroundItem]groundItem // g_pInitItem
	itemGrid    map[int]int               // g_pItemGrid, cellKey -> ground id
	initCount   int                       // last ground id taken by InitItem.csv
	decayCursor int                       // sServer.ItemCount

	ranking rankingState // sServer.Challanger: the arena duel
	groups  []groupEntry // ListGrupoSexy: the group finder list
	dbUp    bool         // connected to DBSRV
}

type event struct {
	kind eventKind
	conn *clientConn
	pkt  protocol.Packet
	gd   *store.GameData
	rank *rankBoard
	macs [][8]byte
}

type eventKind int

const (
	evAccept eventKind = iota
	evClientPacket
	evClientClosed
	evDBPacket
	evDBUp
	evDBDown
	evGameData
	evRanking
	evMacBans
)

func New(cfg Config, data *Data, log *slog.Logger) *Game {
	cfg.fillDefaults()
	g := &Game{
		cfg:      cfg,
		data:     data,
		log:      log,
		grid:     make([]uint16, gridSize*gridSize),
		mobs:     make([]Mob, MaxMob),
		genCount: make([]int, len(data.Geners)),
		events:   make(chan event, 4096),
		start:    time.Now(),

		sealCache: map[int32]sealCacheEntry{},
		guilds:    map[int]*Guild{},
		guildAlly: map[int]int{},
		guildWar:  map[int]int{},
		itemGrid:  map[int]int{},
	}
	for i := range g.users {
		g.users[i] = &User{id: i}
	}
	for i, c := range data.Cities {
		g.chargedGuilds[cfg.Channel-1][i] = c.OwnerIndex
	}
	g.initGroundItems()
	g.initWars()
	g.initWater()
	g.nightmareLastMin, g.forceWeather = -1, -1
	g.macBans = map[[8]byte]bool{}
	return g
}

// now is CurrentTime: milliseconds on the server clock, stamped in every
// outgoing header.
func (g *Game) now() uint32 { return uint32(time.Since(g.start).Milliseconds()) }

// rnd is Rand(): non-negative pseudo-random int.
// It is a variable so tests can fix the rolls.
var rnd = func() int { return rand.IntN(0x8000) }

// Run is the main loop (WndProc + timers). It returns when ctx ends.
func (g *Game) Run(ctx context.Context) {
	send := time.NewTicker(100 * time.Millisecond) // TIMER_SEND
	sec := time.NewTicker(500 * time.Millisecond)  // TIMER_SEC (500 ms in the C++)
	minute := time.NewTicker(12 * time.Second)     // TIMER_MIN
	defer minute.Stop()
	defer send.Stop()
	defer sec.Stop()
	for {
		select {
		case <-ctx.Done():
			g.shutdown()
			return
		case ev := <-g.events:
			g.handleEvent(ev)
		case <-send.C:
			g.flushAll()
		case <-sec.C:
			g.secTimer()
		case <-minute.C:
			g.minTimer()
		}
	}
}

func (g *Game) post(ev event) { g.events <- ev }

// ReloadGameData hands freshly loaded game data to the game loop, which
// swaps it in between packets (safe from any goroutine).
func (g *Game) ReloadGameData(gd *store.GameData) { g.post(event{kind: evGameData, gd: gd}) }

func (g *Game) handleEvent(ev event) {
	switch ev.kind {
	case evAccept:
		g.accept(ev.conn)
	case evClientPacket:
		if u := g.userOf(ev.conn); u != nil {
			g.handleClient(u, ev.pkt)
		}
	case evClientClosed:
		if u := g.userOf(ev.conn); u != nil {
			if u.status == userLogin {
				// Waiting for DBSRV: the client gave up (or dropped) mid-login.
				g.log.Info("disconnected during login", "slot", u.id, "account", wire.CString(u.account.Username[:]))
			}
			g.closeUser(u)
		}
	case evDBPacket:
		g.handleDB(ev.pkt)
	case evDBUp:
		g.dbUp = true
		g.log.Info("connected to DBSRV")
		g.reportCityOwners()
	case evDBDown:
		g.dbUp = false
		g.log.Error("lost DBSRV connection")
	case evRanking:
		g.rankTable = ev.rank
	case evMacBans:
		g.macBans = make(map[[8]byte]bool, len(ev.macs))
		for _, m := range ev.macs {
			g.macBans[m] = true
		}
	case evGameData:
		if err := g.data.ApplyGameData(ev.gd); err != nil {
			g.log.Error("game data reload refused, keeping the current tables", "err", err)
			return
		}
		g.data.ApplyCityOwners(g.cfg.Channel, ev.gd.CityOwners)
		changed := false
		for _, c := range ev.gd.CityOwners {
			if c.Channel == g.cfg.Channel && c.City >= 0 && c.City < len(g.data.Cities) && g.chargedGuilds[g.cfg.Channel-1][c.City] != int32(c.GuildID) {
				g.chargedGuilds[g.cfg.Channel-1][c.City] = int32(c.GuildID)
				changed = true
				g.log.Info("city owner changed", "city", c.City, "guild", c.GuildID, "by", c.ChangedBy)
			}
		}
		if changed {
			g.reportCityOwners()
		}
		g.log.Info("game data reloaded", "items", len(ev.gd.Items), "skills", len(ev.gd.Skills), "drop_mobs", len(ev.gd.Drops), "teleports", len(ev.gd.Teleports))
	}
}

// userOf maps a connection to its slot, ignoring stale events from a
// connection whose slot was already released.
func (g *Game) userOf(c *clientConn) *User {
	if c == nil || c.slot <= 0 || c.slot >= MaxPlayer {
		return nil
	}
	u := g.users[c.slot]
	if u.conn != c {
		return nil
	}
	return u
}

// accept is WSA_ACCEPT + GetEmptyUser.
func (g *Game) accept(c *clientConn) {
	slot := 0
	for i := 1; i < MaxPlayer; i++ {
		if g.users[i].status == userEmpty && g.users[i].conn == nil {
			slot = i
			break
		}
	}
	if slot == 0 {
		g.log.Warn("no free player slot")
		c.close()
		return
	}
	u := g.users[slot]
	*u = User{id: slot, conn: c, status: userAccept, ip: c.ip, lastReceiveSec: g.secCounter}
	c.slot = slot
	if slot >= MaxPlayer-10 {
		g.clientMessage(u, "Servidor cheio. Tente novamente mais tarde")
		g.flush(u)
		g.releaseSlot(u)
		return
	}
	c.start(g)
}

// secTimer is the part of ProcessSecTimer Phase A needs: the round-robin
// autosave (one playing user per tick).
func (g *Game) secTimer() {
	g.secCounter++
	g.serverAreaTimer()
	if g.secCounter%32 == 0 {
		g.idleTimer()
	}
	g.potionTimer()
	g.mobTimer()
	g.regenTimer()
	g.decayGroundItems()
	g.expirePlayerDrops()
	g.followTimer()
	if g.secCounter%4 == 0 {
		g.rankingTimer()
	}
	if g.secCounter%2 == 0 {
		g.storeTimer()
		g.missionTimer()
		g.lanhouseTimer()
		g.duelLetterTimer()
		g.arenaTimer()
		g.guildProcessTimer()
		g.zakumTimer()
		g.bossTimer(time.Now())
		g.nightmareStart(time.Now())
	}
	if g.secCounter%4 == 0 {
		g.nightmareTimer()
		g.waterTimer()
		g.pistaTimer(time.Now())
	}
	for range MaxPlayer {
		g.saveCursor++
		if g.saveCursor >= MaxPlayer {
			g.saveCursor = 1
		}
		u := g.users[g.saveCursor]
		if u.status == userPlay {
			g.saveUser(u, 0)
			break
		}
	}
}

func (g *Game) shutdown() {
	for _, u := range g.users[1:] {
		if u.status != userEmpty {
			g.closeUser(u)
		}
	}
	g.flushAll()
}

// ---- output ----

// queue is AddMessage: stamps nothing yet (the connection encrypts and
// stamps on write), just copies the packet.
func (g *Game) queue(u *User, v any) {
	if u == nil || u.conn == nil {
		return
	}
	b, err := wire.Marshal(v)
	if err != nil {
		g.log.Error("marshal", "err", err)
		return
	}
	u.out = append(u.out, b)
}

func (g *Game) queueRaw(u *User, raw []byte) {
	if u == nil || u.conn == nil {
		return
	}
	u.out = append(u.out, append([]byte(nil), raw...))
}

// flush is SendMessageA for one user.
func (g *Game) flush(u *User) {
	if len(u.out) == 0 || u.conn == nil {
		return
	}
	batch := u.out
	u.out = nil
	if !u.conn.enqueue(batch) {
		g.log.Warn("client send buffer full, disconnecting", "slot", u.id)
		// Detach the connection first: closeUser queues more packets (trade,
		// store, party) and flushed them into the same full buffer, calling
		// closeUser again without end until the TMSRV crashed.
		c := u.conn
		u.conn = nil
		c.close()
		g.closeUser(u)
	}
}

func (g *Game) flushAll() {
	for _, u := range g.users[1:] {
		if len(u.out) > 0 {
			g.flush(u)
		}
	}
}

func (g *Game) sendDB(v any) {
	if g.db != nil {
		g.db.send(v)
	}
}
