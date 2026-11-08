package game

import (
	"testing"

	"dbsrv/internal/wire"
)

func useTicket(g *Game, u *User, slot uint32) {
	g.requestUseItem(u, packet(&wire.UseItem{Header: wire.NewHeader(wire.OpCliUseItem, uint16(u.id)), SrcType: wire.SlotInv, SrcSlot: slot, PosX: 2100, PosY: 2100}))
}

func answerCaptcha(g *Game, u *User, code string) {
	p := wire.Recaptcha{Header: wire.NewHeader(opRecaptcha, uint16(u.id)), Code: captchaConfirm}
	copy(p.Pass[:], code)
	g.requestRecaptcha(u, packet(&p))
}

func lanhouseGame(t *testing.T) (*Game, *User, *wire.Mob) {
	g := worldGame(t)
	u := placePlayer(g, 2100, 2100, 0)
	listen(u)
	pl := &g.mobs[1].info.Player
	pl.Inventory[3] = wire.Item{Index: 4111}
	return g, u, pl
}

func inLanhouse(m *Mob) bool {
	return lanAreaN.contains(int(m.target.X), int(m.target.Y))
}

func TestLanhouseEntry(t *testing.T) {
	g, u, pl := lanhouseGame(t)

	pl.Equip[0].Effect[1].Value = evoArch
	useTicket(g, u, 3)
	if pl.Inventory[3].Index != 4111 || u.captchaTime != 0 {
		t.Fatal("an Arch used the mortal ticket")
	}

	pl.Equip[0].Effect[1].Value = evoMortal
	useTicket(g, u, 3)
	if pl.Inventory[3].Index != 0 || u.captchaTime != captchaSeconds {
		t.Fatalf("ticket not spent or no captcha: %+v, %d", pl.Inventory[3], u.captchaTime)
	}
	if len(sent(u, opRecaptcha)) != 1 || len(u.captchaCode) != 4 {
		t.Fatalf("captcha not sent, code %q", u.captchaCode)
	}

	answerCaptcha(g, u, "0000") // the codes start at 1001
	if inLanhouse(&g.mobs[1]) {
		t.Fatal("wrong code accepted")
	}

	answerCaptcha(g, u, u.captchaCode)
	if !inLanhouse(&g.mobs[1]) || u.questTime != lanhouseSeconds || u.captchaTime != 0 {
		t.Fatalf("not warped: %+v, time %d", g.mobs[1].target, u.questTime)
	}
}

func TestLanhouseCaptchaExpires(t *testing.T) {
	g, u, _ := lanhouseGame(t)
	useTicket(g, u, 3)
	code := u.captchaCode
	for range captchaSeconds {
		g.lanhouseTimer()
	}
	answerCaptcha(g, u, code)
	if inLanhouse(&g.mobs[1]) || u.questTime != 0 {
		t.Fatal("captcha accepted after it expired")
	}
}

func TestLanhouseTimeRunsOut(t *testing.T) {
	g, u, _ := lanhouseGame(t)
	g.teleportar(1, 3655, 3640)
	u.questTime = 2
	g.lanhouseTimer()
	if !inLanhouse(&g.mobs[1]) || u.questTime != 1 {
		t.Fatalf("left too soon, time %d", u.questTime)
	}
	g.lanhouseTimer()
	if inLanhouse(&g.mobs[1]) {
		t.Fatal("still in the area with no time left")
	}
}

func TestLanhouseWithoutTicketRemoved(t *testing.T) {
	g, u, _ := lanhouseGame(t)
	g.teleportar(1, 3655, 3640)
	u.accessLevel = 1
	g.lanhouseTimer()
	if !inLanhouse(&g.mobs[1]) {
		t.Fatal("GM removed")
	}
	u.accessLevel = 0
	g.lanhouseTimer()
	if inLanhouse(&g.mobs[1]) {
		t.Fatal("player without time stayed")
	}
}

func TestLanhouseOneAccountPerComputer(t *testing.T) {
	g, u, _ := lanhouseGame(t)
	g.teleportar(1, 3655, 3640)
	u.questTime = 100

	o := g.users[2]
	o.status, o.mac, o.questTime = userPlay, u.mac, 100
	listen(o)
	g.mobs[2].mode = 1
	g.mobs[2].info.Player.Status.CurHP = 100
	g.mobs[2].target = wire.Position{X: 3660, Y: 3640}
	g.setCell(3660, 3640, 2)

	g.lanhouseTimer()
	if !inLanhouse(&g.mobs[1]) {
		t.Fatal("first account removed")
	}
	if inLanhouse(&g.mobs[2]) {
		t.Fatal("second account of the computer stayed")
	}
	if o.questTime != 0 {
		t.Fatalf("recall kept the countdown: %d", o.questTime)
	}
}
