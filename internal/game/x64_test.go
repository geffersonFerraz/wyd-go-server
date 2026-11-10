package game

import (
	"log/slog"
	"testing"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

const beyond32Bits = int64(5_000_000_000)

func x64Game(limit int64) (*Game, *User) {
	g := New(Config{Channel: 1, Limits: LimitSettings{MaxGold: limit, MaxDamage: limit, MaxAttackDefense: limit}},
		&Data{}, slog.New(slog.DiscardHandler))
	u := g.users[1]
	u.status = userPlay
	g.mobs[1].info.Player.Status.CurHP = 100
	return g, u
}

func goldPacket(op uint16, v int64) protocol.Packet {
	return protocol.Packet{Raw: wire.MustMarshal(&wire.SignalValue64{Header: wire.NewHeader(op, 1), Value: v})}
}

func TestStorageMovesGoldBeyond32Bits(t *testing.T) {
	g, u := x64Game(wire.MaxLimit)
	pl := &g.mobs[1].info.Player
	pl.Gold = 2 * beyond32Bits

	g.requestGoldToStorage(u, goldPacket(wire.OpCliGoldToStorage, beyond32Bits))
	if pl.Gold != beyond32Bits || u.account.StorageCoin != beyond32Bits {
		t.Fatalf("deposit: bag %d, storage %d", pl.Gold, u.account.StorageCoin)
	}
	g.requestGoldToInv(u, goldPacket(wire.OpCliGoldToInv, beyond32Bits))
	if pl.Gold != 2*beyond32Bits || u.account.StorageCoin != 0 {
		t.Fatalf("withdraw: bag %d, storage %d", pl.Gold, u.account.StorageCoin)
	}
}

func TestStorageRejectsNegativeAndOverLimitGold(t *testing.T) {
	g, u := x64Game(beyond32Bits)
	pl := &g.mobs[1].info.Player
	pl.Gold, u.account.StorageCoin = beyond32Bits, beyond32Bits

	g.requestGoldToInv(u, goldPacket(wire.OpCliGoldToInv, -1))
	g.requestGoldToStorage(u, goldPacket(wire.OpCliGoldToStorage, -1))
	g.requestGoldToInv(u, goldPacket(wire.OpCliGoldToInv, 1))
	g.requestGoldToStorage(u, goldPacket(wire.OpCliGoldToStorage, 1))
	if pl.Gold != beyond32Bits || u.account.StorageCoin != beyond32Bits {
		t.Fatalf("bag %d, storage %d", pl.Gold, u.account.StorageCoin)
	}
}

func TestLimitsAreValidated(t *testing.T) {
	ok := LimitSettings{MaxGold: 1, MaxDamage: 1, MaxAttackDefense: wire.MaxLimit}
	if err := ok.validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []LimitSettings{
		{MaxGold: 0, MaxDamage: 1, MaxAttackDefense: 1},
		{MaxGold: 1, MaxDamage: wire.MaxLimit + 1, MaxAttackDefense: 1},
	} {
		if bad.validate() == nil {
			t.Errorf("%+v was accepted", bad)
		}
	}
}
