package game

import (
	"dbsrv/internal/wire"
)

// opCliWPEWarn is WPEOnWarnPacket (0x634): the client's protection
// (JProtectDll) found WPE, a packet editor. The C++ closed the connection;
// the Go server also records it for the admin panel ("Alertas").
const opCliWPEWarn = 0x634

// requestWPEWarn closes the player and raises an alert.
func (g *Game) requestWPEWarn(u *User) {
	login := wire.CString(u.account.Username[:])
	char := ""
	if u.status == userPlay {
		char = wire.CString(g.mobs[u.id].info.Player.Name[:])
	}
	g.log.Warn("WPE detected by the client, disconnecting", "slot", u.id, "account", login, "char", char, "ip", u.ip)
	g.securityAlert(u, wire.AlertWPE, "Proteção do cliente detectou o WPE (editor de pacotes)")
	if u.status == userPlay {
		g.saveUser(u, 0)
	}
	g.closeUser(u)
}

// securityAlert sends an alert to DBSRV, which stores it for the panel.
func (g *Game) securityAlert(u *User, kind int32, detail string) {
	p := wire.SecurityAlert{Header: wire.NewHeader(wire.OpSecurityAlert, uint16(u.id)), Kind: kind, Channel: int32(g.cfg.Channel)}
	copy(p.Username[:15], u.account.Username[:15])
	if u.status == userPlay {
		copy(p.CharName[:15], g.mobs[u.id].info.Player.Name[:15])
	}
	wire.PutCString(p.IP[:], u.ip)
	wire.PutGameString(p.Detail[:], detail)
	g.sendDB(&p)
}
