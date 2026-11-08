package game

import (
	"cmp"
	"slices"
	"strings"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// The ranking window (0x528 level, 0x529 PvP, 0x530 power) and the character
// profile. The C++ kept Ranking.txt and Rank\<name>.bin per channel; here
// DBSRV stores what every channel sends, and each TMSRV reads the same list.

const (
	opRankLevel       = 0x528
	opRankPvP         = 0x529
	opRankPower       = 0x530
	opCharProfile     = 0x487
	opCharProfileRank = 0x912A

	rankPerPage = 10 // MAX_RANK of the client
	// rankMaxLevel: above this (as shown) a character is staff, kept out of
	// the lists (the C++ skipped bStatus.Level >= 401).
	rankMaxLevel = 401
)

// rankBoard is the ranking snapshot: three orders and the lookup by name.
type rankBoard struct {
	level, pvp, power []*store.RankEntry
	byName            map[string]*store.RankEntry
}

func newRankBoard(list []store.RankEntry) *rankBoard {
	b := &rankBoard{byName: make(map[string]*store.RankEntry, len(list))}
	for i := range list {
		e := &list[i]
		b.byName[strings.ToLower(e.Name)] = e
		if e.Level <= rankMaxLevel {
			b.level = append(b.level, e)
		}
	}
	b.pvp, b.power = slices.Clone(b.level), slices.Clone(b.level)
	// The C++ sorted this list by Exp but never filled Exp, so its order
	// was arbitrary. Evolution, level, then exp is what "Level" means.
	slices.SortStableFunc(b.level, func(x, y *store.RankEntry) int {
		return cmp.Or(cmp.Compare(y.Evolution, x.Evolution), cmp.Compare(y.Level, x.Level), cmp.Compare(y.Exp, x.Exp), cmp.Compare(x.Name, y.Name))
	})
	slices.SortStableFunc(b.pvp, func(x, y *store.RankEntry) int {
		return cmp.Or(cmp.Compare(y.Frags, x.Frags), cmp.Compare(x.Name, y.Name))
	})
	slices.SortStableFunc(b.power, func(x, y *store.RankEntry) int {
		return cmp.Or(cmp.Compare(y.Power, x.Power), cmp.Compare(x.Name, y.Name))
	})
	return b
}

// SetRanking swaps in a new ranking (safe from any goroutine).
func (g *Game) SetRanking(list []store.RankEntry) {
	g.post(event{kind: evRanking, rank: newRankBoard(list)})
}

// overPower is GetOverPower: the "power" column.
func (g *Game) overPower(info *wire.CharInfo) int {
	pl := &info.Player
	ev := evolution(pl)
	level := int(pl.Status.Level)
	if ev >= evoCelestial {
		level += 400
	}
	points := level * 2
	if ev >= evoArch {
		for _, q := range []uint64{questElime, questThelion, questNoas, questSylphed} {
			if info.Info&q != 0 {
				points += 100
			}
		}
	}
	for i := 1; i < 16; i++ {
		it := &pl.Equip[i]
		if it.Index == 0 || int(it.Index) >= MaxItemList {
			continue
		}
		if g.data.itemAbility(it, EF_MOBTYPE) == 1 {
			points += 30
		}
		if i != 14 {
			points += itemSanc(it) * 5
			continue
		}
		mount := int(it.Effect[1].Index)
		if mount == 120 {
			points += 100
		}
		if mount > 120 {
			points += (mount - 120) * 15
		}
		points += mount * 2
	}
	return points
}

// sendRankUpdate is SetRankTopFrag + SaveRankingToBin: this character's row
// goes to DBSRV.
func (g *Game) sendRankUpdate(u *User) {
	if u.charSlot < 0 || u.charSlot > 3 {
		return
	}
	info := &g.mobs[u.id].info
	pl := &info.Player
	if pl.Name[0] == 0 {
		return
	}
	p := wire.RankUpdate{Header: wire.NewHeader(wire.OpRankUpdate, uint16(u.id)), Name: pl.Name,
		Level: int32(pl.Status.Level) + 1, Class: int32(pl.ClassInfo), Evolution: int32(evolution(pl)),
		Guild: int32(pl.GuildIndex), Patent: int32(pl.GuildMemberType), Kingdom: int32(pl.CapeInfo),
		Frags: int32(totKill(pl)), Power: int32(g.overPower(info)), Fame: int32(info.Fame), Citizen: int32(info.Citizen),
		PvPKill: info.Counters.PvPKill, PvMKill: info.Counters.PvMMob, Mantle: int32(pl.Equip[15].Index),
		Exp: int64(pl.Exp)}
	copy(p.Equip[:], pl.Equip[:])
	g.sendDB(&p)
}

// requestRankList answers 0x528, 0x529 and 0x530 with one page of 10.
func (g *Game) requestRankList(u *User, pkt protocol.Packet) {
	var req wire.SignalValue
	if wire.Unmarshal(pkt.Raw, &req) != nil || u.status != userPlay {
		return
	}
	page := max(int(req.Value), 1) // the C++ indexed the list with page-1 unchecked
	out := wire.RankList{Header: wire.NewHeader(pkt.Header.PacketID, uint16(u.id)), SceneIndex: int32(page), Update: 1}
	var list []*store.RankEntry
	if g.rankTable != nil {
		switch pkt.Header.PacketID {
		case opRankLevel:
			list = g.rankTable.level
		case opRankPvP:
			list = g.rankTable.pvp
		default:
			list = g.rankTable.power
		}
	}
	total := max((len(list)+rankPerPage-1)/rankPerPage, 1)
	for i := range out.Rank {
		out.Rank[i] = wire.RankRow{Class: -1, Kingdom: -1, CurrentPage: int32(page), TotalPage: int32(total)}
	}
	if page <= total {
		start := (page - 1) * rankPerPage
		for i, e := range list[start:min(start+rankPerPage, len(list))] {
			r := &out.Rank[i]
			wire.PutGameString(r.Player[:], e.Name)
			r.Level, r.Class, r.Evolution, r.Guild, r.Patent = int32(e.Level), int32(e.Class), int32(e.Evolution), int32(e.Guild), int32(e.Patent)
			r.Kingdom, r.Frags, r.Exp, r.Power = int32(e.Kingdom), int32(e.Frags), e.Exp, int32(e.Power)
			if e.Guild != 0 {
				wire.PutGameString(r.GuildName[:], g.guildName(e.Guild))
			}
		}
	}
	g.queue(u, &out)
}

func (g *Game) profileGuildName(guild int) string {
	if guild != 0 {
		return g.guildName(guild)
	}
	return "Sem Guild"
}

// requestCharProfile is Msg_CharProfile: the profile of a player nearby.
func (g *Game) requestCharProfile(u *User, pkt protocol.Packet) {
	var req wire.SignalValue
	if wire.Unmarshal(pkt.Raw, &req) != nil || u.status != userPlay {
		return
	}
	target := int(req.Value)
	if target <= 0 || target >= MaxPlayer || target == u.id {
		return
	}
	if g.users[target].status != userPlay {
		g.clientMessage(u, "Este jogador não está conectado.")
		return
	}
	info := &g.mobs[target].info
	pl := &info.Player
	p := wire.CharProfile{Header: wire.NewHeader(opCharProfile, uint16(u.id)), Name: pl.Name, Evolution: int32(evolution(pl)),
		Level: int32(pl.Status.Level) + 1, GuildIndex: int32(pl.GuildIndex), Citizen: int32(info.Citizen), Fame: int32(info.Fame),
		PvPKill: info.Counters.PvPKill, PvMKill: info.Counters.PvMMob, Mantle: int32(pl.Equip[15].Index)}
	wire.PutGameString(p.GuildName[:], g.profileGuildName(int(pl.GuildIndex)))
	copy(p.Equip[:], pl.Equip[:])
	g.queue(u, &p)
}

// requestCharProfileRank is Msg_CharProfileRank: a profile by name, from
// the ranking.
func (g *Game) requestCharProfileRank(u *User, pkt protocol.Packet) {
	var req wire.SignalNick
	if wire.Unmarshal(pkt.Raw, &req) != nil || u.status != userPlay || g.rankTable == nil {
		return
	}
	e := g.rankTable.byName[strings.ToLower(wire.GameString(req.Nick[:]))]
	if e == nil {
		g.clientMessage(u, "Personagem não encontrado no ranking.")
		return
	}
	p := wire.CharProfile{Header: wire.NewHeader(opCharProfile, uint16(u.id)), Evolution: int32(e.Evolution), Level: int32(e.Level),
		GuildIndex: int32(e.Guild), Citizen: int32(e.Citizen), Fame: int32(e.Fame), PvPKill: int32(e.PvPKill), PvMKill: int32(e.PvMKill),
		Mantle: int32(e.Mantle), Equip: e.Equip}
	wire.PutGameString(p.Name[:], e.Name)
	wire.PutGameString(p.GuildName[:], g.profileGuildName(e.Guild))
	g.queue(u, &p)
}
