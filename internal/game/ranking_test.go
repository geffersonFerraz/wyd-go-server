package game

import (
	"encoding/binary"
	"fmt"
	"testing"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

func TestRankPacketSizes(t *testing.T) {
	for name, c := range map[string]struct {
		v    any
		want int
	}{"p528": {wire.RankList{}, 1784}, "p487": {wire.CharProfile{}, 240}, "p292": {wire.HpMode{}, 32}} {
		if got := binary.Size(c.v); got != c.want {
			t.Errorf("%s = %d bytes, want %d", name, got, c.want)
		}
	}
}

func rankRequest(op uint16, page int32) protocol.Packet {
	return protocol.Packet{Header: protocol.Header{PacketID: op}, Raw: wire.MustMarshal(&wire.SignalValue{Header: wire.NewHeader(op, 1), Value: page})}
}

func lastRank(t *testing.T, u *User) wire.RankList {
	t.Helper()
	if len(u.out) == 0 {
		t.Fatal("no answer")
	}
	var r wire.RankList
	if err := wire.Unmarshal(u.out[len(u.out)-1], &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRankingPages(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)

	// Before the first read: an empty page, not a crash.
	g.requestRankList(u, rankRequest(opRankLevel, 1))
	if r := lastRank(t, u); r.Rank[0].Player[0] != 0 || r.Rank[0].TotalPage != 1 {
		t.Fatalf("empty ranking %+v", r.Rank[0])
	}

	var list []store.RankEntry
	for i := range 25 {
		list = append(list, store.RankEntry{Name: fmt.Sprintf("Char%02d", i), Level: 100 + i, Evolution: 1, Frags: 25 - i, Power: i * 2})
	}
	list = append(list, store.RankEntry{Name: "Staff", Level: 1011, Evolution: 3}) // staff stays out of the lists
	list = append(list, store.RankEntry{Name: "Arch", Level: 5, Evolution: 2})     // a higher evolution ranks first
	g.rankTable = newRankBoard(list)

	g.requestRankList(u, rankRequest(opRankLevel, 1))
	r := lastRank(t, u)
	if wire.GameString(r.Rank[0].Player[:]) != "Arch" || wire.GameString(r.Rank[1].Player[:]) != "Char24" || r.Rank[0].TotalPage != 3 || r.SceneIndex != 1 || r.Update != 1 {
		t.Fatalf("level page 1: %s, %s, total %d", r.Rank[0].Player, r.Rank[1].Player, r.Rank[0].TotalPage)
	}
	if r.Rank[10].Player[0] != 0 {
		t.Fatal("more than 10 rows on a page")
	}

	g.requestRankList(u, rankRequest(opRankPvP, 3))
	r = lastRank(t, u)
	if r.Header.PacketID != opRankPvP || wire.GameString(r.Rank[0].Player[:]) != "Char20" || r.Rank[6].Player[0] != 0 {
		t.Fatalf("pvp page 3: %s", r.Rank[0].Player)
	}

	// Pages out of range: page 0 and below are page 1; past the end is empty.
	g.requestRankList(u, rankRequest(opRankPower, -5))
	if r = lastRank(t, u); r.SceneIndex != 1 || wire.GameString(r.Rank[0].Player[:]) != "Char24" {
		t.Fatalf("page -5: %d %s", r.SceneIndex, r.Rank[0].Player)
	}
	g.requestRankList(u, rankRequest(opRankPower, 99))
	if r = lastRank(t, u); r.Rank[0].Player[0] != 0 {
		t.Fatal("page past the end has rows")
	}
}

func TestOverPower(t *testing.T) {
	g := worldGame(t)
	var info wire.CharInfo
	pl := &info.Player
	pl.Status.Level = 100
	pl.Equip[0].Effect[1].Value = evoCelestial
	info.Info = questElime | questNoas
	pl.Equip[1] = wire.Item{Index: 1103}
	pl.Equip[1].Effect[0] = wire.ItemEffect{Index: EF_SANC, Value: 7}
	pl.Equip[14] = wire.Item{Index: 1103}
	pl.Equip[14].Effect[1].Index = 125
	want := (100+400)*2 + 200 + 7*5 + 100 - 100 + 5*15 + 250
	if base := g.data.itemAbility(&pl.Equip[1], EF_MOBTYPE); base == 1 {
		want += 60
	}
	if got := g.overPower(&info); got != want {
		t.Fatalf("power %d, want %d", got, want)
	}
}

func TestCharProfileByName(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	e := store.RankEntry{Name: "Fulano", Level: 200, Evolution: 2, Frags: 3}
	e.Equip[1].Index = 1103
	g.rankTable = newRankBoard([]store.RankEntry{e})
	var req wire.SignalNick
	req.Header = wire.NewHeader(opCharProfileRank, 1)
	wire.PutGameString(req.Nick[:], "fulano")
	g.requestCharProfileRank(u, protocol.Packet{Raw: wire.MustMarshal(&req)})
	var p wire.CharProfile
	if err := wire.Unmarshal(u.out[len(u.out)-1], &p); err != nil {
		t.Fatal(err)
	}
	if wire.GameString(p.Name[:]) != "Fulano" || p.Level != 200 || p.Equip[1].Index != 1103 || wire.GameString(p.GuildName[:]) != "Sem Guild" {
		t.Fatalf("profile %+v", p)
	}
}

func TestHpModeWhenDead(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	g.mobs[1].info.Player.Status.CurHP = 0
	g.requestAddPoint(u, protocol.Packet{Raw: wire.MustMarshal(&AddPoint{})})
	var p wire.HpMode
	if err := wire.Unmarshal(u.out[len(u.out)-1], &p); err != nil {
		t.Fatal(err)
	}
	if p.Header.PacketID != wire.OpSrvHpMode || p.CurHP != 0 || p.Status != userPlay {
		t.Fatalf("hp mode %+v", p)
	}
}

func TestDropListPacketSizes(t *testing.T) {
	if got := binary.Size(wire.DropList{}); got != 30016 {
		t.Errorf("p2568 = %d", got)
	}
	if got := binary.Size(wire.FilterDropMsg{}); got != 216 {
		t.Errorf("filter = %d", got)
	}
}

func dropReq(region int32) protocol.Packet {
	return protocol.Packet{Raw: wire.MustMarshal(&wire.Parm2{Header: wire.NewHeader(opDropList, 1), Parm1: region})}
}

func TestDropListRegionAndSearch(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	add := func(id int, name string, x, y uint16, item uint16) {
		m := &g.mobs[id]
		m.mode = modeNPCIdle
		wire.PutCString(m.info.Player.Name[:], name)
		m.target = wire.Position{X: x, Y: y}
		m.info.Player.Inventory[3] = wire.Item{Index: item}
		m.info.Player.Inventory[4] = wire.Item{Index: 10} // a face: hidden in the region list
	}
	add(1000, "Lobo", 16*128+5, 16*128+5, 412)
	add(1001, "Lobo", 17*128, 16*128, 412)  // same name: listed once
	add(1002, "Guard", 17*128, 16*128, 412) // hidden
	add(1003, "Ogro", 100, 100, 412)        // other region
	g.mobs[1003].info.Player.Exp = 77
	g.requestDropList(u, dropReq(1))
	var p wire.DropList
	if err := wire.Unmarshal(u.out[len(u.out)-1], &p); err != nil {
		t.Fatal(err)
	}
	lobos := 0
	for i := range int(p.Amount) {
		switch wire.CString(p.Drop[i].Name[:]) {
		case "Lobo":
			lobos++
			if p.Drop[i].Carry[3].Index != 412 || p.Drop[i].Carry[4].Index != 0 {
				t.Fatalf("carry %+v", p.Drop[i].Carry[3:5])
			}
		case "Guard", "Ogro":
			t.Fatalf("listed %s", p.Drop[i].Name)
		}
	}
	if lobos != 1 {
		t.Fatalf("Lobo listed %d times", lobos)
	}
	// A second request inside a second is ignored.
	n := len(u.out)
	g.requestDropList(u, dropReq(1))
	if len(u.out) != n {
		t.Fatal("rate limit")
	}

	g.data.Items[412].Name = "Pedra Rara"
	var req wire.SignalNick
	req.Header = wire.NewHeader(opDropListSearch, 1)
	wire.PutGameString(req.Nick[:], "pedra")
	g.requestDropSearch(u, protocol.Packet{Raw: wire.MustMarshal(&req)})
	if err := wire.Unmarshal(u.out[len(u.out)-1], &p); err != nil {
		t.Fatal(err)
	}
	ogro := false
	for i := range int(p.Amount) {
		ogro = ogro || wire.CString(p.Drop[i].Name[:]) == "Ogro"
	}
	if !ogro || p.Header.PacketID != opDropList {
		t.Fatalf("search found %d, Ogro %v", p.Amount, ogro)
	}
}

func filterReq(code int32, item uint16) protocol.Packet {
	r := wire.FilterDropMsg{Header: wire.NewHeader(opFilterDrop, 1), Code: code}
	r.Items[0].Index = item
	return protocol.Packet{Raw: wire.MustMarshal(&r)}
}

func TestDropFilterWindow(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	f := &g.mobs[1].info.FilterDrop
	g.requestFilterDrop(u, filterReq(filterCodeAdd, 412))
	g.requestFilterDrop(u, filterReq(filterCodeAdd, 413))
	g.requestFilterDrop(u, filterReq(filterCodeAdd, 412))  // repeated
	g.requestFilterDrop(u, filterReq(filterCodeAdd, 9999)) // not an item
	if !f.IsEnabled || f.Items[0].Index != 412 || f.Items[1].Index != 413 || f.Items[2].Index != 0 {
		t.Fatalf("add: %+v", f.Items[:3])
	}
	g.requestFilterDrop(u, filterReq(filterCodeRemove, 412))
	if f.Items[0].Index != 413 || f.Items[1].Index != 0 {
		t.Fatalf("remove compacts: %+v", f.Items[:2])
	}
	g.requestFilterDrop(u, filterReq(filterCodeClear, 0))
	if f.IsEnabled || f.Items[0].Index != 0 {
		t.Fatal("clear")
	}
}
