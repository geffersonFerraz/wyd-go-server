package game

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The account lock (RequestBlockFunc.cpp): an 8-character password and
// eight switches that block parts of the game until the password is given
// again. The C++ kept it in <TMSRV>/Cache/LockAccount/<LOGIN>.bin
// (UserBlockData, 18 bytes); the same files are used here, so locks set on
// either server carry over.

const (
	opLockStatus  = 0xF30 // server: MSG_AccountLockInfo; client: unlock (MSG_UnlockAccount)
	opCliLock     = 0xF31 // MSG_LockAccount
	opCliBlockPwd = 0x903 // _MSG_BLOCKPASS (old item password: only answered)
)

// Lock switches (UserBlockData / User.Block.Desc order).
const (
	lockUnequip = iota // moving equipment and using items
	lockTrade
	lockBuy
	lockSell
	lockAutoTrade
	lockPremium
	lockCargo // storage
	lockCompound
)

// accountLock is User.Block.
type accountLock struct {
	pass [10]byte
	desc [8]bool
}

func (l *accountLock) set() bool { return l.pass[0] != 0 }

// LockAccount mirrors MSG_LockAccount (32 bytes).
type LockAccount struct {
	Header   wire.Header
	Password [10]byte
	Conf     [8]uint8
	_        [2]byte // sizeof = 32
}

// UnlockAccount mirrors MSG_UnlockAccount (24 bytes).
type UnlockAccount struct {
	Header   wire.Header
	Password [10]byte
	_        [2]byte // sizeof = 24
}

// LockStatus mirrors MSG_AccountLockInfo (20 bytes).
type LockStatus struct {
	Header wire.Header
	Conf   [8]uint8
}

// lockPath is the lock file of an account ("" when the server has no
// cache folder: locks then live only in memory).
func (g *Game) lockPath(u *User) string {
	login := wire.CString(u.account.Username[:])
	if g.cfg.CacheDir == "" || login == "" || filepath.Base(login) != login {
		return ""
	}
	return filepath.Join(g.cfg.CacheDir, "LockAccount", login+".bin")
}

// loadLock is LoadBlockData (on entering the world).
func (g *Game) loadLock(u *User) {
	path := g.lockPath(u)
	if path == "" {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			g.log.Warn("account lock not readable", "slot", u.id, "err", err)
		}
		return
	}
	if len(raw) < 18 {
		return
	}
	copy(u.lock.pass[:], raw[:10])
	for i := range u.lock.desc {
		u.lock.desc[i] = raw[10+i] != 0
	}
}

// saveLock is SaveUserBlockData; a cleared lock removes the file (the C++
// left it, so the lock came back at the next login).
func (g *Game) saveLock(u *User) {
	path := g.lockPath(u)
	if path == "" {
		return
	}
	if !u.lock.set() {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			g.log.Warn("account lock not removed", "slot", u.id, "err", err)
		}
		return
	}
	var b bytes.Buffer
	b.Write(u.lock.pass[:])
	for _, d := range u.lock.desc {
		if d {
			b.WriteByte(1)
		} else {
			b.WriteByte(0)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		err = os.WriteFile(path, b.Bytes(), 0o644)
		if err != nil {
			g.log.Warn("account lock not saved", "slot", u.id, "err", err)
		}
	}
}

// sendLockStatus is SendLockStatus.
func (g *Game) sendLockStatus(u *User) {
	p := LockStatus{Header: wire.NewHeader(opLockStatus, wire.ClientIDSystem)}
	for i, d := range u.lock.desc {
		if d {
			p.Conf[i] = 1
		}
	}
	g.queue(u, &p)
}

// locked reports a switch, telling the player when it is on.
func (g *Game) locked(u *User, which int, msg string) bool {
	if !u.lock.desc[which] {
		return false
	}
	g.clientMessage(u, "%s", msg)
	return true
}

// requestLock is CUser::RequestBlockFunc (0xF31): set the lock once.
func (g *Game) requestLock(u *User, pkt protocol.Packet) {
	var p LockAccount
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay || u.lock.set() {
		return
	}
	if len(wire.CString(p.Password[:])) != 8 {
		g.clientMessage(u, "Seu código deve conter 8 caracteres")
		return
	}
	u.lock.pass = [10]byte{}
	copy(u.lock.pass[:8], p.Password[:8])
	for i, c := range p.Conf {
		u.lock.desc[i] = c != 0
	}
	g.saveLock(u)
	g.sendLockStatus(u)
	g.clientMessage(u, "Bloqueio definido com sucesso!")
	g.log.Info("account lock set", "slot", u.id, "switches", u.lock.desc)
}

// requestUnlock is CUser::RequestUnlockFunc (0xF30).
func (g *Game) requestUnlock(u *User, pkt protocol.Packet) {
	var p UnlockAccount
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay || !u.lock.set() {
		return
	}
	if wire.CString(p.Password[:]) != wire.CString(u.lock.pass[:]) {
		g.clientMessage(u, "Senha inválida!")
		return
	}
	u.lock = accountLock{}
	g.saveLock(u)
	g.sendLockStatus(u)
	g.clientMessage(u, "Bloqueio removido com sucesso!")
}

// requestBlockPassword is CUser::RequestBlockItems (0x903): the C++ only
// answered and logged the password (never stored it); the password is not
// logged here.
func (g *Game) requestBlockPassword(u *User) {
	g.clientMessage(u, "Sua senha foi definida")
}
