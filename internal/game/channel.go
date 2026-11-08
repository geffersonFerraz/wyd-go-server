package game

import (
	"strconv"
	"strings"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// In-game channel change. The client's channel list (TMFieldScene, control
// 12289) sends the whisper "srv507 N" after a 5 s countdown. Then:
//
//  1. TMSRV checks the request and asks DBSRV (0x814 NewServerID=N).
//  2. DBSRV answers 0x52A with "*N token" in Unknow (empty = refused).
//  3. TMSRV sends the client 0x52A (MSG_CNFRemoveServer: account + that key
//     as TID) and closes it, saving the character where it stands.
//  4. The client connects to channel N and logs in with the key instead of
//     the password; DBSRV puts the character straight into the world at
//     the saved position (0x10A + 0x114, no character list).

// changeChannelCombat: no channel change this soon after attacking.
const changeChannelCombat = 10 * time.Second

// changeChannelTimeout: a request DBSRV never answered is dropped.
const changeChannelTimeout = 10 * time.Second

// requestChangeChannel is the "srv507" command (value: channel number).
func (g *Game) requestChangeChannel(u *User, value string) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || u.status != userPlay {
		return
	}
	m := &g.mobs[u.id]
	pl := &m.info.Player
	now := time.Now()
	switch {
	case n == g.cfg.Channel:
		g.clientMessage(u, "Você já está no canal %d.", n)
		return
	case n < 1 || n > len(g.cfg.Cluster.Channels):
		g.clientMessage(u, "O canal %d não existe.", n)
		return
	case u.changeChannelTo != 0 && now.Sub(u.changeChannelAt) < changeChannelTimeout:
		return // already waiting for DBSRV
	case !u.changeChannelAt.IsZero() && now.Sub(u.changeChannelAt) < g.cfg.ChangeChannelCooldown:
		wait := g.cfg.ChangeChannelCooldown - now.Sub(u.changeChannelAt)
		g.clientMessage(u, "Aguarde %d segundo(s) para trocar de canal.", int(wait.Seconds())+1)
		return
	case pl.Status.CurHP <= 0:
		g.clientMessage(u, "Não é possível trocar de canal morto.")
		return
	case now.Sub(u.lastAttack) < changeChannelCombat:
		g.clientMessage(u, "Não é possível trocar de canal em combate.")
		return
	case inQuestArea(int(m.target.X), int(m.target.Y)):
		g.clientMessage(u, "Saia da quest para trocar de canal.")
		return
	case u.charSlot < 0 || u.charSlot > 3:
		return
	}
	u.changeChannelTo, u.changeChannelAt = n, now
	g.sendDB(&wire.ServerChange{Header: wire.NewHeader(wire.OpServerChange, uint16(u.id)),
		NewServerID: int32(n), Slot: int32(u.charSlot)})
	g.log.Info("channel change requested", "slot", u.id, "account", wire.CString(u.account.Username[:]), "to", n)
}

// channelChangeReply handles DBSRV's 0x52A.
func (g *Game) channelChangeReply(u *User, pkt protocol.Packet) {
	var p wire.AccountLogin
	if wire.Unmarshal(pkt.Raw, &p) != nil {
		return
	}
	to := u.changeChannelTo
	u.changeChannelTo = 0
	if to == 0 || u.status != userPlay || wire.CString(p.Login[:]) != wire.CString(u.account.Username[:]) {
		return
	}
	key := wire.CString(p.Unknow[:])
	if key == "" {
		g.clientMessage(u, "O canal %d está indisponível no momento.", to)
		return
	}
	r := wire.CNFRemoveServer{Header: wire.NewHeader(wire.OpReLoginForChange, uint16(u.id))}
	copy(r.AccountName[:], p.Login[:])
	wire.PutCString(r.TID[:], key)
	g.queue(u, &r)
	g.log.Info("channel change", "slot", u.id, "account", wire.CString(u.account.Username[:]), "to", to)
	// Saves the character where it stands (DBSRV hands that position to the
	// new channel) and frees the slot once DBSRV confirms.
	g.closeUser(u)
}

// inQuestArea reports whether (x, y) is inside a newbie quest area (paid
// with an item; a channel change would skip leaving it).
func inQuestArea(x, y int) bool {
	for _, a := range questArea {
		if x >= a[0] && y >= a[1] && x <= a[2] && y <= a[3] {
			return true
		}
	}
	return false
}
