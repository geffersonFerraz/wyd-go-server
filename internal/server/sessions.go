package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"strings"
	"sync"
	"time"

	"dbsrv/internal/store"
)

// sessionKey identifies a logged-in account the way the C++ index did
// (server*MAX_PLAYER + clientId).
type sessionKey struct {
	Channel int
	Client  uint16
}

// session is an account online on some channel. The C++ Account[] entry.
// Fields other than key/username are only touched by the owning client's
// lane goroutine, under mu.
type session struct {
	key      sessionKey
	username string

	mu     sync.Mutex
	rec    *store.AccountRecord
	slot   int  // selected character, -1 = character selection screen
	master bool // logged in with the master password (C++ "IsBanned")
}

// changeToken lets an account log into another channel without its
// password, straight into the world (0x814 / 0x52A).
type changeToken struct {
	token   string
	channel int // index of the channel it is valid on
	slot    int // character to enter the world with
	expires time.Time
}

// sessions is the cross-channel table of online accounts.
type sessions struct {
	mu     sync.Mutex
	byKey  map[sessionKey]*session
	byUser map[string]*session
	tokens map[string]changeToken // username -> pending 0x52A token
	busy   map[string]bool        // offline accounts being written by a delivery
}

func newSessions() *sessions {
	return &sessions{
		byKey:  map[sessionKey]*session{},
		byUser: map[string]*session{},
		tokens: map[string]changeToken{},
		busy:   map[string]bool{},
	}
}

// claim registers s unless the account is already online. It returns the
// existing session in that case (C++ GetIndex(accountName) != 0), or nil
// with ok=false while a delivery is writing the offline account.
func (m *sessions) claim(s *session) (existing *session, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.byUser[s.username]; e != nil {
		return e, false
	}
	if m.busy[s.username] {
		return nil, false
	}
	if e := m.byKey[s.key]; e != nil {
		// Same client id reused without a logout: the old login is gone.
		delete(m.byUser, e.username)
	}
	m.byKey[s.key] = s
	m.byUser[s.username] = s
	return nil, true
}

// withOffline runs fn while username cannot log in, if it is offline now.
// It returns the online session instead (fn not run) when there is one.
func (m *sessions) withOffline(username string, fn func()) *session {
	m.mu.Lock()
	if s := m.byUser[username]; s != nil {
		m.mu.Unlock()
		return s
	}
	m.busy[username] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.busy, username)
		m.mu.Unlock()
	}()
	fn()
	return nil
}

func (m *sessions) get(k sessionKey) *session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byKey[k]
}

func (m *sessions) byName(username string) *session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byUser[strings.ToUpper(username)]
}

// remove drops s if it is still the registered session for its key.
func (m *sessions) remove(s *session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byKey[s.key] == s {
		delete(m.byKey, s.key)
	}
	if m.byUser[s.username] == s {
		delete(m.byUser, s.username)
	}
}

// dropChannel removes every session of a channel whose TMSRV went away.
// The C++ server left them logged in forever, locking those accounts out.
func (m *sessions) dropChannel(ch int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k, s := range m.byKey {
		if k.Channel == ch {
			delete(m.byKey, k)
			if m.byUser[s.username] == s {
				delete(m.byUser, s.username)
			}
			n++
		}
	}
	return n
}

func (m *sessions) count(ch int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for k := range m.byKey {
		if k.Channel == ch {
			n++
		}
	}
	return n
}

// issueToken creates the single-use key of a channel change to channel
// (index) with character slot, replacing any pending one of the account.
func (m *sessions) issueToken(username string, channel, slot int, ttl time.Duration) string {
	var b [20]byte
	_, _ = rand.Read(b[:])
	tok := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b[:]))
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[username] = changeToken{token: tok, channel: channel, slot: slot, expires: time.Now().Add(ttl)}
	return tok
}

// consumeToken checks tok against the account's pending channel change
// arriving on channel (index) and returns its slot. A presented token is
// removed whether or not it matched, so it cannot be guessed at.
func (m *sessions) consumeToken(username, tok string, channel int) (slot int, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, found := m.tokens[username]
	if !found {
		return -1, false
	}
	delete(m.tokens, username)
	if tok == "" || subtle.ConstantTimeCompare([]byte(t.token), []byte(tok)) != 1 ||
		t.channel != channel || !time.Now().Before(t.expires) {
		return -1, false
	}
	return t.slot, true
}
