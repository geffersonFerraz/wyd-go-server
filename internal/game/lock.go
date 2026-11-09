package game

import (
	"bytes"
	"context"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The account lock (RequestBlockFunc.cpp): an 8-character password and
// eight switches that block parts of the game until the password is given
// again. The C++ kept it in <TMSRV>/Cache/LockAccount/<LOGIN>.bin
// (UserBlockData, 18 bytes); here the same 18 bytes are in the database.

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

// LockStore keeps the account locks (internal/store implements it).
type LockStore interface {
	AccountLock(ctx context.Context, username string) ([]byte, error)
	SetAccountLock(ctx context.Context, username string, data []byte) error
	DeleteAccountLock(ctx context.Context, username string) error
}

// lockTimeout bounds the database calls made from the game loop.
const lockTimeout = 2 * time.Second

// lockLogin is the key of an account's lock ("" when locks live only in
// memory).
func (g *Game) lockLogin(u *User) string {
	if g.cfg.Locks == nil {
		return ""
	}
	return wire.CString(u.account.Username[:])
}

// loadLock is LoadBlockData (on entering the world).
func (g *Game) loadLock(u *User) {
	login := g.lockLogin(u)
	if login == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	raw, err := g.cfg.Locks.AccountLock(ctx, login)
	if err != nil {
		g.log.Warn("account lock not readable", "slot", u.id, "err", err)
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

// saveLock is SaveUserBlockData; a cleared lock removes the row (the C++
// left its file, so the lock came back at the next login).
func (g *Game) saveLock(u *User) {
	login := g.lockLogin(u)
	if login == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()
	if !u.lock.set() {
		if err := g.cfg.Locks.DeleteAccountLock(ctx, login); err != nil {
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
	if err := g.cfg.Locks.SetAccountLock(ctx, login, b.Bytes()); err != nil {
		g.log.Warn("account lock not saved", "slot", u.id, "err", err)
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
