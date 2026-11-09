package game

import (
	"encoding/binary"
	"slices"
	"testing"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

func TestManaShieldAgainstMobs(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	m := &g.mobs[1]
	m.info.Player.Status.MaxMP, m.info.Player.Status.CurMP = 1000, 1000
	m.info.Affects[3] = wire.Affect{Index: 18, Time: 100}
	left, shielded, absorbed := g.manaShield(1, 100)
	if left != 20 || !shielded || !absorbed || m.info.Player.Status.CurMP != 920 {
		t.Fatalf("left %d shield %v absorbed %v mp %d", left, shielded, absorbed, m.info.Player.Status.CurMP)
	}
	// It would leave less than 300 MP: the hit goes through in full.
	m.info.Player.Status.CurMP = 350
	if left, _, absorbed = g.manaShield(1, 100); left != 100 || absorbed || m.info.Player.Status.CurMP != 350 {
		t.Fatalf("low mp: left %d absorbed %v", left, absorbed)
	}
	m.info.Affects[3] = wire.Affect{}
	if left, shielded, _ = g.manaShield(1, 100); left != 100 || shielded {
		t.Fatal("no shield")
	}
}

func TestLevelItems(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	pl := &g.mobs[1].info.Player
	pl.Equip[0].Effect[1].Value = evoMortal
	pl.ClassInfo = 1
	pl.Status.Level = 29
	pl.Status.INT, pl.Status.STR = 100, 10
	g.cfg.LevelItems = []LevelItem{
		{Evolution: 1, Level: 29, Class: 1, Type: 2, Item: wire.Item{Index: 412}},  // mage build: yes
		{Evolution: 1, Level: 29, Class: 1, Type: 1, Item: wire.Item{Index: 413}},  // strength build: no
		{Evolution: 1, Level: 29, Class: 0, Type: 0, Item: wire.Item{Index: 414}},  // other class: no
		{Evolution: 1, Level: 29, Class: 0, Type: -1, Item: wire.Item{Index: 415}}, // anyone: yes
		{Evolution: 2, Level: 29, Class: 1, Type: -1, Item: wire.Item{Index: 416}}, // other evolution: no
		{Evolution: 1, Level: 30, Class: 1, Type: -1, Item: wire.Item{Index: 417}}, // other level: no
	}
	g.giveLevelItems(u)
	for id, want := range map[uint16]bool{412: true, 413: false, 414: false, 415: true, 416: false, 417: false} {
		if got := firstSlotOf(pl, id) >= 0; got != want {
			t.Errorf("item %d: %v, want %v", id, got, want)
		}
	}
	// Full inventory: the storage.
	for i := range invSlots {
		pl.Inventory[i] = wire.Item{Index: 1}
	}
	g.cfg.LevelItems = []LevelItem{{Evolution: 1, Level: 29, Class: 1, Type: -1, Item: wire.Item{Index: 412}}}
	u.account.StorageItems = [168]wire.Item{}
	g.giveLevelItems(u)
	if u.account.StorageItems[0].Index != 412 {
		t.Fatalf("storage %+v", u.account.StorageItems[0])
	}
}

func TestNoCPArea(t *testing.T) {
	g := worldGame(t)
	g.cfg.NoCPAreas = []NoCPArea{{MinX: 10, MinY: 20, MaxX: 30, MaxY: 40}}
	if !g.noCPArea(10, 40) || g.noCPArea(9, 30) || g.noCPArea(20, 41) {
		t.Fatal("area limits")
	}
}

func TestShippedLevelSettings(t *testing.T) {
	s, err := LoadSettings("../../tmsrv.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.NoCPAreas) != 4 || len(s.LevelItems) != 181 {
		t.Fatalf("%d areas, %d items", len(s.NoCPAreas), len(s.LevelItems))
	}
	first := s.levelItems()[0]
	if first.Evolution != 1 || first.Level != 140 || first.Type != -1 || first.Item.Index != 4615 {
		t.Fatalf("first %+v", first)
	}
}

func TestFixBootsCapsDamage(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	it := wire.Item{Index: 1147}
	it.Effect[1] = wire.ItemEffect{Index: EF_DAMAGE, Value: 90}
	if !g.fixBoots(u, &it) || it.Effect[1].Index != EF_DAMAGE2 || it.Effect[1].Value != 30 && it.Effect[1].Value != 42 {
		t.Fatalf("boots %+v", it.Effect[1])
	}
	if g.fixBoots(u, &it) && it.Effect[1].Index == EF_DAMAGE {
		t.Fatal("changed again")
	}
}

func TestIllusionCostsMana(t *testing.T) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	m := &g.mobs[1]
	m.info.Player.ClassInfo = 3
	m.info.Player.Learn[0] = 2
	mana := int64(g.data.Skills[73].Mana)
	m.info.Player.Status.MaxMP, m.info.Player.Status.CurMP = mana+10, mana+10
	p := wire.Action{Header: wire.NewHeader(wire.OpCliMoveIllus, 1)}
	p.Header.TimeStamp = g.now()
	p.Destiny = wire.Position{X: m.target.X + 2, Y: m.target.Y}
	send := func() {
		g.requestAction(u, protocol.Packet{Header: protocol.Header{PacketID: wire.OpCliMoveIllus}, Raw: wire.MustMarshal(&p)})
	}
	send()
	if m.info.Player.Status.CurMP != 10 {
		t.Fatalf("mp %d", m.info.Player.Status.CurMP)
	}
	if mana > 10 {
		before := m.target
		p.Destiny = wire.Position{X: m.target.X + 3, Y: m.target.Y}
		send()
		if m.target != before || m.info.Player.Status.CurMP != 10 {
			t.Fatal("moved without mana")
		}
	}
	// Not a hunter with the skill.
	m.info.Player.ClassInfo = 0
	m.info.Player.Status.MaxMP, m.info.Player.Status.CurMP = mana+10, mana+10
	send()
	if m.info.Player.Status.CurMP != mana+10 {
		t.Fatal("illusion without the skill")
	}
}

// Every item of the client's ItemList.bin must exist on the server.
func TestClientItemsExistOnServer(t *testing.T) {
	raw := WorldFixture(t).Asset(store.AssetItemList)
	const size = 156
	if len(raw) != MaxItemList*size {
		t.Fatalf("%d bytes", len(raw))
	}
	g := worldGame(t)
	missing := 0
	for id := 1; id < MaxItemList; id++ {
		rec := raw[id*size : (id+1)*size]
		if !slices.ContainsFunc(rec, func(b byte) bool { return b != 0x5A }) {
			continue
		}
		if g.data.Items[id].Name == "" {
			missing++
			t.Errorf("item %d is only in the client", id)
		}
		pos := int32(binary.LittleEndian.Uint32(rec[136:])) ^ 0x5A5A5A5A
		if id == 5715 && (g.data.Items[id].FullPos != pos || pos != 131072 || g.data.Items[id].Pos != 0) {
			t.Errorf("belt position: full %d, game %d, client %d", g.data.Items[id].FullPos, g.data.Items[id].Pos, pos)
		}
	}
	if missing == 0 && g.data.Items[5765].FullPos != 65536 {
		t.Errorf("necklace position %d", g.data.Items[5765].FullPos)
	}
	// They go to the accessory slots like the other items of the family.
	belt := wire.Item{Index: 5718}
	if !g.data.canEquip(&belt, &wire.Mob{}, 17) {
		t.Error("belt refused in slot 17")
	}
}
