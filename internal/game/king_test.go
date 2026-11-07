package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func kingGame(t *testing.T) (*Game, *User, *wire.Mob, int) {
	g := worldGame(t)
	u := placePlayer(g, 2112, 2042, 0)
	listen(u)
	npc := questNPC(g, 15, 0, 0)
	g.mobs[npc].info.Player.CapeInfo = capeRed
	return g, u, &g.mobs[1].info.Player, npc
}

// A mortal at 300 without a cape, in a guild of the other kingdom, wearing
// the Pedra Eterna and a sephirot becomes Arch.
func TestKingArchWithoutCape(t *testing.T) {
	g, u, pl, npc := kingGame(t)
	pl.Status.Level, pl.BStatus.Level = 299, 299
	pl.Equip[0].Index = 1
	pl.Equip[10] = wire.Item{Index: 1742}
	pl.Equip[11] = wire.Item{Index: 1761}
	pl.GuildIndex = 77
	g.guilds[77] = &Guild{Name: "Azuis", Kingdom: capeBlue}
	g.npcKing(u, npc, 1)
	if pl.Equip[10].Index != 0 || pl.Equip[11].Index != 0 {
		t.Fatalf("no Arch: equip 10 %d, 11 %d", pl.Equip[10].Index, pl.Equip[11].Index)
	}
}

// A guild leader takes the guild to the king's kingdom; a member of a guild
// of the other kingdom is still refused.
func TestKingGuildLeaderChangesKingdom(t *testing.T) {
	g, u, pl, npc := kingGame(t)
	pl.Status.Level, pl.BStatus.Level = 250, 250
	pl.GuildIndex = 77
	g.guilds[77] = &Guild{Name: "Azuis", Kingdom: capeBlue}
	pl.Inventory[0] = wire.Item{Index: itemSapphirePack}
	pl.Inventory[0].Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: 5}

	g.npcKing(u, npc, 1)
	if pl.Equip[15].Index != 0 {
		t.Fatal("member of a blue guild got a red cape")
	}
	pl.GuildMemberType = 9
	g.npcKing(u, npc, 1)
	if pl.Equip[15].Index != 546 {
		t.Fatalf("leader cape %d", pl.Equip[15].Index)
	}

	// A neutral guild does not hold its members back.
	pl.Equip[15] = wire.Item{}
	pl.CapeInfo = 0
	pl.GuildMemberType = 0
	g.guilds[77].Kingdom = 0
	g.npcKing(u, npc, 1)
	if pl.Equip[15].Index != 546 {
		t.Fatalf("neutral guild member cape %d", pl.Equip[15].Index)
	}
}
