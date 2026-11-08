package game

import (
	"fmt"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The Lanhouse dungeons (RequestUseItem.cpp, RequestRecaptcha.cpp and the
// "Reset das lan" block of ProcessTimer.cpp). The item shows a captcha; the
// right code warps the player in for 10 minutes.

const (
	opRecaptcha = 5269
	opQuestTime = 0x3A1 // pMsgSignal: seconds left in the quest area

	captchaAsk     = 0
	captchaConfirm = 1
	captchaClose   = 2

	captchaSeconds  = 30
	lanhouseSeconds = 600
)

type mapRect struct{ minX, minY, maxX, maxY int }

func (r mapRect) contains(x, y int) bool {
	return x >= r.minX && x <= r.maxX && y >= r.minY && y <= r.maxY
}

var (
	lanAreaN = mapRect{3600, 3600, 3700, 3700}
	lanAreaM = mapRect{3713, 3459, 3838, 3582}
	lanAreaA = mapRect{3840, 3584, 3967, 3712}
	// lanAreaMCore is the smaller rectangle the C++ uses when an Arch enters.
	lanAreaMCore = mapRect{3732, 3476, 3816, 3562}

	lanhouseAreas = [...]mapRect{lanAreaN, lanAreaM, lanAreaA}
)

// lanhouseEntry is one ticket. occupied lists where another account of the
// same computer blocks the entry: the C++ checks differ for each ticket.
type lanhouseEntry struct {
	warpX, warpY int
	cityOnly     bool
	allowed      func(evolution uint8) bool
	refusal      string
	occupied     []mapRect
}

var lanhouseEntries = map[uint16]lanhouseEntry{
	4111: {3655, 3640, false, func(e uint8) bool { return e == evoMortal },
		"Disponível apenas para mortais", []mapRect{lanAreaN, lanAreaA}},
	4112: {3768, 3513, true, func(e uint8) bool { return e == evoArch },
		"Disponível apenas para archs", []mapRect{lanAreaMCore, lanAreaA}},
	4113: {3896, 3654, true, func(e uint8) bool { return e > evoArch },
		"Disponível apenas para CELESTIAL", []mapRect{lanAreaM, lanAreaN, lanAreaA}},
}

// useLanhouseTicket answers the use of a ticket and reports whether src is one.
func (g *Game) useLanhouseTicket(u *User, p *wire.UseItem, src *wire.Item) bool {
	entry, ok := lanhouseEntries[src.Index]
	if !ok {
		return false
	}
	refuse := func(msg string) {
		g.clientMessage(u, "%s", msg)
		g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
	}
	m := &g.mobs[u.id]
	if entry.cityOnly && g.data.village(int(m.target.X), int(m.target.Y)) == 5 {
		refuse("Somente utilize em uma cidade!")
		return true
	}
	if !entry.allowed(m.info.Player.Equip[0].Effect[1].Value) {
		refuse(entry.refusal)
		return true
	}
	for _, r := range entry.occupied {
		if g.oneAccountIn(u, r.minX, r.minY, r.maxX, r.maxY) {
			refuse("Disponível apenas uma conta por computador na área")
			return true
		}
	}
	// A second ticket while the captcha is open would just be lost.
	if u.captchaTime > 0 {
		g.sendItem(u, int(p.SrcType), int(p.SrcSlot), src)
		return true
	}
	u.captchaTicket = src.Index
	g.sendCaptcha(u)
	amountMinus(src)
	return true
}

// sendCaptcha is SendReCaptcha: the client draws the digits it receives.
func (g *Game) sendCaptcha(u *User) {
	u.captchaCode = fmt.Sprintf("%04d", 1001+rnd()%8999)
	u.captchaTime = captchaSeconds
	p := wire.Recaptcha{Header: wire.NewHeader(opRecaptcha, uint16(u.id)), Code: captchaAsk}
	copy(p.Pass[:], u.captchaCode)
	g.queue(u, &p)
}

func (g *Game) closeCaptcha(u *User) {
	u.captchaCode, u.captchaTime, u.captchaTicket = "", 0, 0
	g.queue(u, &wire.Recaptcha{Header: wire.NewHeader(opRecaptcha, uint16(u.id)), Code: captchaClose})
}

// requestRecaptcha is Msg_reCaptcha. Only an open captcha counts: the C++
// still accepted the code after the window had timed out.
func (g *Game) requestRecaptcha(u *User, pkt protocol.Packet) {
	var p wire.Recaptcha
	if u.status != userPlay || wire.Unmarshal(pkt.Raw, &p) != nil || p.Code != captchaConfirm || u.captchaTime <= 0 {
		return
	}
	if wire.CString(p.Pass[:]) != u.captchaCode {
		g.sendCaptcha(u)
		g.clientMessage(u, "O reCaptcha está incorreto..")
		return
	}
	entry := lanhouseEntries[u.captchaTicket]
	g.closeCaptcha(u)
	g.teleportar(u.id, entry.warpX, entry.warpY)
	u.questTime = lanhouseSeconds
	g.log.Info("lanhouse entered", "slot", u.id, "x", entry.warpX, "y", entry.warpY)
}

// clearArea is ClearArea: everyone in the rectangle goes back to town, the
// dead with 1 HP.
func (g *Game) clearArea(r mapRect) {
	for _, u := range g.users[1:] {
		m := &g.mobs[u.id]
		if u.status != userPlay || m.mode == 0 || !r.contains(int(m.target.X), int(m.target.Y)) {
			continue
		}
		if m.info.Player.Status.CurHP <= 0 {
			m.info.Player.Status.CurHP = 1
			g.sendScore(u.id)
		}
		g.doRecall(u.id)
	}
}

// clearQuestTime stops the quest countdown and its display on the client.
func (g *Game) clearQuestTime(u *User) {
	if u.questTime > 0 {
		u.questTime = 0
		g.signalParm(u, uint16(u.id), opQuestTime, 0)
	}
}

// lanhouseTimer runs once a second: the captcha and quest countdowns, and
// the removal of whoever has no time left or shares a computer in the area.
func (g *Game) lanhouseTimer() {
	var seen [len(lanhouseAreas)]map[[8]byte]bool
	for _, u := range g.users[1:] {
		m := &g.mobs[u.id]
		if u.status != userPlay || m.mode == 0 {
			continue
		}
		if u.captchaTime > 0 {
			if u.captchaTime--; u.captchaTime == 0 {
				g.closeCaptcha(u)
			}
		}
		if u.questTime > 0 {
			u.questTime--
			g.signalParm(u, uint16(u.id), opQuestTime, int32(u.questTime))
		}
		x, y := int(m.target.X), int(m.target.Y)
		for i, area := range lanhouseAreas {
			if !area.contains(x, y) {
				continue
			}
			if seen[i] == nil {
				seen[i] = map[[8]byte]bool{}
			}
			// GMs may stay without a ticket; the C++ tested level < 1000.
			if seen[i][u.mac] || (u.questTime <= 0 && u.accessLevel <= 0) {
				g.log.Info("removed from lanhouse", "slot", u.id, "x", x, "y", y, "same_computer", seen[i][u.mac])
				g.doRecall(u.id)
			} else {
				seen[i][u.mac] = true
			}
			break
		}
	}
}
