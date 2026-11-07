package admin

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"dbsrv/internal/acctedit"
)

// The in-game account lock (TMSRV, RequestBlockFunc.cpp): an 8-character
// password and eight switches, kept by TMSRV in
// Cache/LockAccount/<LOGIN>.bin (10 bytes of password, 8 switch bytes).
// Players who forget the password have no way out in game, so the panel can
// remove the lock (password and switches) or give it a new password,
// keeping the switches. Afterwards an "edit" delivery (acctedit.OpLock)
// makes TMSRV reload it when the player is in game.

const (
	lockFileSize = 18
	lockPassLen  = 8
)

var lockSwitchNames = [8]string{"Equipar/usar itens", "Troca", "Comprar em NPC", "Vender em NPC",
	"Lojinha (leilão)", "Loja premium", "Baú", "Composições"}

// LockInfo is what the account page shows.
type LockInfo struct {
	Enabled  bool // the panel knows the TMSRV folder
	Set      bool
	Switches []string
	Error    string
}

func (s *Server) lockFile(user string) string {
	user = strings.ToUpper(user)
	if s.cfg.LockDir == "" || !usernameRe.MatchString(user) {
		return ""
	}
	return filepath.Join(s.cfg.LockDir, user+".bin")
}

func (s *Server) lockInfo(user string) LockInfo {
	path := s.lockFile(user)
	if path == "" {
		return LockInfo{}
	}
	info := LockInfo{Enabled: true}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return info
	case err != nil:
		info.Error = err.Error()
		return info
	case len(raw) < lockFileSize || raw[0] == 0:
		return info
	}
	info.Set = true
	for i, name := range lockSwitchNames {
		if raw[10+i] != 0 {
			info.Switches = append(info.Switches, name)
		}
	}
	return info
}

// removeLock deletes the lock (password and switches).
func (s *Server) removeLock(w http.ResponseWriter, r *http.Request) {
	user := strings.ToUpper(r.PathValue("user"))
	path := s.lockFile(user)
	if path == "" {
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Lock", "A pasta da TMSRV não está configurada (-tmsrv-dir).")
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.fail(w, err)
		return
	}
	s.log.Info("account lock removed", "admin", sessionOf(r).user, "account", user)
	s.notifyLock(w, r, user)
}

// resetLockPassword gives the lock a new password, keeping its switches.
func (s *Server) resetLockPassword(w http.ResponseWriter, r *http.Request) {
	user := strings.ToUpper(r.PathValue("user"))
	path := s.lockFile(user)
	pw, pw2 := r.PostFormValue("lockpass"), r.PostFormValue("lockpass2")
	switch {
	case path == "":
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Lock", "A pasta da TMSRV não está configurada (-tmsrv-dir).")
		return
	case !validLockPassword(pw):
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Lock", "A senha de bloqueio deve ter exatamente 8 letras ou números.")
		return
	case pw != pw2:
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Lock", "As senhas não conferem.")
		return
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) || (err == nil && (len(raw) < lockFileSize || raw[0] == 0)) {
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Lock", "Esta conta não tem bloqueio. O jogador define o bloqueio no jogo.")
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]byte, lockFileSize)
	copy(out[:lockPassLen], pw)
	copy(out[10:], raw[10:lockFileSize])
	if err := os.WriteFile(path, out, 0o644); err != nil {
		s.fail(w, err)
		return
	}
	s.log.Info("account lock password reset", "admin", sessionOf(r).user, "account", user)
	s.notifyLock(w, r, user)
}

// notifyLock queues the reload in TMSRV (when the player is in game).
func (s *Server) notifyLock(w http.ResponseWriter, r *http.Request, user string) {
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		// The file already changed; it takes effect at the next login.
		s.renderAccount(w, r, http.StatusOK, "Lock", "Bloqueio alterado. "+msg+" Se o jogador estiver online, vale no próximo login.")
		return
	}
	s.enqueue(w, r, "edit", user, acctedit.Edit{Op: acctedit.OpLock})
}

func validLockPassword(pw string) bool {
	if len(pw) != lockPassLen {
		return false
	}
	for _, c := range pw {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}
