// Package admin is a small web panel for staff: create accounts and change
// passwords and numeric passwords ("token").
//
// It never writes accounts directly. Every change goes through the
// deliveries queue, which DBSRV applies (waiting for the player to log out
// when needed), so the panel can never race with a player's save.
// Passwords are hashed here, so no plain text is stored anywhere.
package admin

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/server"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

//go:embed templates/*.html static/*
var assets embed.FS

const (
	cookieName     = "dbsrv_admin"
	sessionTTL     = 8 * time.Hour
	maxFailures    = 5
	lockoutWindow  = 15 * time.Minute
	requestTimeout = 10 * time.Second
)

// Config for the admin panel.
type Config struct {
	// Users maps admin login -> argon2id hash.
	Users map[string]string
	// SecureCookie sets the Secure flag (enable when served over HTTPS).
	SecureCookie bool
	// GuildMarkDir is where guild marks are saved ("" disables the upload).
	GuildMarkDir string
	// Channels are the channel numbers of tmsrv.yaml (none = just channel 1).
	Channels []int
	// SettingsPath (tmsrv.yaml), ConfigDir (the config folder) and StateDir
	// (the panel's own files) are used by the server export and import.
	SettingsPath, ConfigDir, StateDir string
}

type adminSession struct {
	user    string
	csrf    string
	expires time.Time
}

type failure struct {
	count int
	first time.Time
}

// Server is the admin HTTP handler.
type Server struct {
	cfg  Config
	st   *store.Store
	log  *slog.Logger
	tmpl map[string]*template.Template

	mu       sync.Mutex
	sessions map[string]*adminSession
	failures map[string]*failure
	pending  *pendingImport
}

func New(cfg Config, st *store.Store, log *slog.Logger) (*Server, error) {
	s := &Server{cfg: cfg, st: st, log: log, sessions: map[string]*adminSession{}, failures: map[string]*failure{}}
	funcs := template.FuncMap{
		"status":    statusLabel,
		"evolution": evolutionLabel,
		"result":    func(d store.DeliveryInfo) string { l, _ := resultLabel(d); return l },
		"tone":      func(d store.DeliveryInfo) string { _, t := resultLabel(d); return t },
		"kind":      kindLabel,
		"itemlabel": func(d store.DeliveryInfo) string { return itemLabel(d) + editLabel(d) + banLabel(d) + cashLabel(d) },
		"bantype":   func(t int32) string { return banTypeLabel(int(t)) },
		"pending":   func(d store.DeliveryInfo) bool { return d.ProcessedAt == nil },
		"datetime":  func(t time.Time) string { return t.Local().Format("02/01/2006 15:04:05") },
		"dateptr": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Local().Format("02/01/2006 15:04")
		},
		"join":       strings.Join,
		"pathescape": url.PathEscape,
	}
	s.tmpl = map[string]*template.Template{}
	funcs["add1"] = func(i int) int { return i + 1 }
	funcs["mul2"] = func(i int) int { return i * 2 }
	funcs["alertkind"] = alertKindLabel
	funcs["dict"] = func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	funcs["effidx"] = func(it wire.Item, k int) int { return int(it.Effect[k].Index) }
	funcs["effval"] = func(it wire.Item, k int) int { return int(it.Effect[k].Value) }
	funcs["slice3"] = func() []int { return []int{0, 1, 2} }
	funcs["spaces"] = visibleSpaces
	funcs["dropurl"] = func(name string) string { return dropURL(name, "") }
	funcs["class"] = func(id int) string {
		if id < 96 {
			return []string{"TransKnight", "Foema", "BeastMaster", "Hunter"}[id/24]
		}
		return "especial"
	}
	for _, page := range []string{"login", "accounts", "account", "items", "item", "skills", "skill", "drops", "drop", "teleports", "character", "storage", "guilds", "alerts", "notice", "gm", "messages", "stores", "droprates", "server", "npcs", "npc", "generators", "generator", "exprule", "tracks", "track"} {
		files := []string{"templates/layout.html", "templates/" + page + ".html"}
		if page == "character" || page == "storage" {
			files = append(files, "templates/editparts.html")
		} else if page != "login" && page != "accounts" && page != "account" && page != "guilds" && page != "alerts" && page != "notice" && page != "gm" && page != "messages" && page != "stores" && page != "server" && page != "tracks" && page != "track" {
			files = append(files, "templates/gamedata_common.html")
		}
		t, err := template.New("layout.html").Funcs(funcs).ParseFS(assets, files...)
		if err != nil {
			return nil, err
		}
		s.tmpl[page] = t
	}
	return s, nil
}

// Handler returns the routed handler with security headers.
func (s *Server) Handler() http.Handler {
	static, _ := fs.Sub(assets, "static")
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.loginSubmit)
	mux.HandleFunc("POST /logout", s.auth(s.logout))
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/accounts", http.StatusSeeOther) })
	mux.HandleFunc("GET /accounts", s.auth(s.accountsPage))
	mux.HandleFunc("POST /accounts", s.auth(s.createAccount))
	mux.HandleFunc("GET /accounts/{user}", s.auth(s.accountPage))
	mux.HandleFunc("POST /accounts/{user}/password", s.auth(s.changePassword))
	mux.HandleFunc("POST /accounts/{user}/token", s.auth(s.changeToken))
	mux.HandleFunc("POST /accounts/{user}/kick", s.auth(s.kick))
	mux.HandleFunc("POST /accounts/{user}/login-unblock", s.auth(s.loginUnblock))
	mux.HandleFunc("POST /accounts/{user}/item", s.auth(s.deliverItem))
	mux.HandleFunc("POST /accounts/{user}/lock/remove", s.auth(s.removeLock))
	mux.HandleFunc("POST /accounts/{user}/lock/password", s.auth(s.resetLockPassword))
	mux.HandleFunc("GET /api/deliveries/{id}", s.auth(s.deliveryStatus))
	s.gamedataRoutes(mux)
	s.npcRoutes(mux)
	s.expRuleRoutes(mux)
	s.trackRoutes(mux)
	s.editRoutes(mux)
	s.guildRoutes(mux)
	s.alertRoutes(mux)
	s.gmRoutes(mux)
	s.serverRoutes(mux)
	s.accountRoutes(mux)
	s.noticeRoutes(mux)
	return securityHeaders(mux)
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; form-action 'self'; base-uri 'none'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "same-origin")
		hd.Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

// ---- sessions & auth ----

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

type ctxKey struct{}

func (s *Server) currentSession(r *http.Request) *adminSession {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[c.Value]
	if sess == nil || time.Now().After(sess.expires) {
		delete(s.sessions, c.Value)
		return nil
	}
	sess.expires = time.Now().Add(sessionTTL)
	return sess
}

// auth requires a session and, for POST, a valid CSRF token.
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.currentSession(r)
		if sess == nil {
			if strings.HasPrefix(r.URL.Path, "/api/") {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if r.Method == http.MethodPost {
			if subtle.ConstantTimeCompare([]byte(r.PostFormValue("csrf")), []byte(sess.csrf)) != 1 {
				http.Error(w, "sessão expirada, recarregue a página", http.StatusForbidden)
				return
			}
		}
		ctx, cancel := context.WithTimeout(context.WithValue(r.Context(), ctxKey{}, sess), requestTimeout)
		defer cancel()
		next(w, r.WithContext(ctx))
	}
}

func sessionOf(r *http.Request) *adminSession { return r.Context().Value(ctxKey{}).(*adminSession) }

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) lockedOut(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[ip]
	if f == nil {
		return false
	}
	if time.Since(f.first) > lockoutWindow {
		delete(s.failures, ip)
		return false
	}
	return f.count >= maxFailures
}

func (s *Server) recordFailure(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f := s.failures[ip]
	if f == nil || time.Since(f.first) > lockoutWindow {
		f = &failure{first: time.Now()}
		s.failures[ip] = f
	}
	f.count++
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	if s.currentSession(r) != nil {
		http.Redirect(w, r, "/accounts", http.StatusSeeOther)
		return
	}
	s.render(w, "login", map[string]any{"Title": "Entrar"})
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if s.lockedOut(ip) {
		s.renderStatus(w, http.StatusTooManyRequests, "login", map[string]any{"Title": "Entrar",
			"Error": "Muitas tentativas. Aguarde 15 minutos."})
		return
	}
	user := strings.TrimSpace(r.PostFormValue("user"))
	pw := r.PostFormValue("password")
	hash, known := s.cfg.Users[user]
	if !known {
		hash = dummyHash // spend the same time for unknown users
	}
	ok, _ := auth.Verify(hash, pw)
	if !known || !ok {
		s.recordFailure(ip)
		s.log.Warn("admin login failed", "user", user, "ip", ip)
		s.renderStatus(w, http.StatusUnauthorized, "login", map[string]any{"Title": "Entrar", "Error": "Usuário ou senha incorretos.", "User": user})
		return
	}
	token := randomToken()
	s.mu.Lock()
	s.sessions[token] = &adminSession{user: user, csrf: randomToken(), expires: time.Now().Add(sessionTTL)}
	delete(s.failures, ip)
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: s.cfg.SecureCookie, MaxAge: int(sessionTTL.Seconds())})
	s.log.Info("admin login", "user", user, "ip", ip)
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

var dummyHash, _ = auth.Hash(randomToken())

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---- pages ----

var usernameRe = regexp.MustCompile(`^[A-Za-z0-9]{3,15}$`)

func (s *Server) accountsPage(w http.ResponseWriter, r *http.Request) {
	s.renderAccounts(w, r, http.StatusOK, nil)
}

func (s *Server) renderAccounts(w http.ResponseWriter, r *http.Request, code int, form map[string]any) {
	q := r.URL.Query().Get("q")
	list, err := s.st.SearchAccounts(r.Context(), q, 100)
	if err != nil {
		s.fail(w, err)
		return
	}
	recent, err := s.st.RecentDeliveries(r.Context(), "", 15)
	if err != nil {
		s.fail(w, err)
		return
	}
	data := map[string]any{"Title": "Contas", "Q": q, "Accounts": list, "Recent": recent, "Form": form}
	s.page(w, r, code, "accounts", data)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	user := strings.ToUpper(strings.TrimSpace(r.PostFormValue("username")))
	pw, pw2 := r.PostFormValue("password"), r.PostFormValue("password2")
	token := strings.TrimSpace(r.PostFormValue("token"))
	form := map[string]any{"Username": user, "Token": token}
	formErr := func(msg string) {
		form["Error"] = msg
		s.renderAccounts(w, r, http.StatusUnprocessableEntity, form)
	}

	switch {
	case !usernameRe.MatchString(user) || server.IsDeviceName(user):
		formErr("Login deve ter de 3 a 15 letras ou números.")
		return
	case !server.ValidPassword(pw):
		formErr("A senha deve ter de 1 a 11 caracteres (limite do cliente).")
		return
	case pw != pw2:
		formErr("As senhas não conferem.")
		return
	case token != "" && !server.ValidNumericPassword(token):
		formErr("O token deve ter de 1 a 6 dígitos.")
		return
	}
	exists, err := s.st.AccountExists(r.Context(), user)
	if err != nil {
		s.fail(w, err)
		return
	}
	if exists {
		formErr("Já existe uma conta com esse login.")
		return
	}
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		form["Error"] = msg
		s.renderAccounts(w, r, http.StatusServiceUnavailable, form)
		return
	}

	payload := map[string]any{}
	if payload["password_hash"], err = auth.Hash(pw); err != nil {
		s.fail(w, err)
		return
	}
	if token != "" {
		if payload["numeric_hash"], err = auth.Hash(token); err != nil {
			s.fail(w, err)
			return
		}
	}
	s.enqueue(w, r, "create_account", user, payload)
}

func (s *Server) accountPage(w http.ResponseWriter, r *http.Request) {
	s.renderAccount(w, r, http.StatusOK, "", "")
}

func (s *Server) renderAccount(w http.ResponseWriter, r *http.Request, code int, which, msg string) {
	user := strings.ToUpper(r.PathValue("user"))
	acc, chars, err := s.st.AccountDetail(r.Context(), user)
	if errors.Is(err, store.ErrNotFound) {
		// Maybe it is still being created.
		recent, _ := s.st.RecentDeliveries(r.Context(), user, 20)
		if len(recent) == 0 {
			http.NotFound(w, r)
			return
		}
		s.page(w, r, http.StatusOK, "account", map[string]any{"Title": user, "Username": user, "Missing": true, "Deliveries": recent})
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	deliveries, err := s.st.RecentDeliveries(r.Context(), user, 20)
	if err != nil {
		s.fail(w, err)
		return
	}
	online, err := s.st.Online(r.Context(), user)
	if err != nil {
		s.fail(w, err)
		return
	}
	names, err := s.effectNames(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	data := map[string]any{"Title": user, "Username": user, "Account": acc, "Characters": chars,
		"Deliveries": deliveries, "Online": online, "EffectNames": names, "Lock": s.lockInfo(r.Context(), user),
		"ItemForm": url.Values{"amount": {"1"}, "copies": {"1"}, "sanc": {"0"}}}
	if acc.BanType != 0 && acc.BanUntil != nil {
		data["BanActive"] = time.Now().Before(*acc.BanUntil)
		data["BanExpired"] = !time.Now().Before(*acc.BanUntil)
	}
	if which != "" {
		data[which+"Error"] = msg
	}
	if which == "Item" {
		data["ItemForm"] = r.PostForm // keep what was typed
	}
	if id := r.URL.Query().Get("req"); id != "" {
		data["Requested"] = id
	}
	if msg := r.URL.Query().Get("err"); msg != "" && which == "" {
		data["AccessError"] = msg // from the edit action
	}
	s.page(w, r, code, "account", data)
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	pw, pw2 := r.PostFormValue("password"), r.PostFormValue("password2")
	switch {
	case !server.ValidPassword(pw):
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Password", "A senha deve ter de 1 a 11 caracteres.")
		return
	case pw != pw2:
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Password", "As senhas não conferem.")
		return
	}
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		s.renderAccount(w, r, http.StatusServiceUnavailable, "Password", msg)
		return
	}
	hash, err := auth.Hash(pw)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.enqueue(w, r, "password", r.PathValue("user"), map[string]any{"password_hash": hash})
}

func (s *Server) changeToken(w http.ResponseWriter, r *http.Request) {
	tok, tok2 := strings.TrimSpace(r.PostFormValue("token")), strings.TrimSpace(r.PostFormValue("token2"))
	switch {
	case !server.ValidNumericPassword(tok):
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Token", "O token deve ter de 1 a 6 dígitos.")
		return
	case tok != tok2:
		s.renderAccount(w, r, http.StatusUnprocessableEntity, "Token", "Os tokens não conferem.")
		return
	}
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		s.renderAccount(w, r, http.StatusServiceUnavailable, "Token", msg)
		return
	}
	hash, err := auth.Hash(tok)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.enqueue(w, r, "password", r.PathValue("user"), map[string]any{"numeric_hash": hash})
}

// kick asks dbsrv to have TMSRV save and disconnect the player, after which
// the account can be edited directly.
func (s *Server) kick(w http.ResponseWriter, r *http.Request) {
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		s.renderAccount(w, r, http.StatusServiceUnavailable, "Kick", msg)
		return
	}
	s.enqueue(w, r, "kick", r.PathValue("user"), map[string]any{})
}

// loginUnblock clears the "3 wrong passwords" lock the channels keep in
// memory, so the player can try again at once.
func (s *Server) loginUnblock(w http.ResponseWriter, r *http.Request) {
	if msg := s.dbsrvProblem(r.Context()); msg != "" {
		s.renderAccount(w, r, http.StatusServiceUnavailable, "LoginUnblock", msg)
		return
	}
	s.enqueue(w, r, "login_unblock", r.PathValue("user"), map[string]any{})
}

func (s *Server) enqueue(w http.ResponseWriter, r *http.Request, kind, user string, payload any) {
	user = strings.ToUpper(user)
	sess := sessionOf(r)
	id, err := s.st.Enqueue(r.Context(), kind, user, payload, sess.user)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.log.Info("admin request", "admin", sess.user, "kind", kind, "account", user, "delivery", id)
	http.Redirect(w, r, "/accounts/"+url.PathEscape(user)+"?req="+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) deliveryStatus(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d, err := s.st.DeliveryByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	label, tone := resultLabel(d)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"done": d.ProcessedAt != nil, "label": label, "tone": tone})
}

// heartbeatMaxAge: dbsrv beats every 5s; allow a couple of missed beats.
const heartbeatMaxAge = 15 * time.Second

// dbsrvProblem explains why queued requests would not be applied right now
// ("" when dbsrv is up and speaks the payloads this panel sends). Without
// this, an outdated dbsrv rejected hashed passwords as invalid.
func (s *Server) dbsrvProblem(ctx context.Context) string {
	proto, seen, age, err := s.st.DBSRVStatus(ctx)
	switch {
	case err != nil:
		s.log.Error("dbsrv status", "err", err)
		return "Não foi possível verificar o DBSRV."
	case seen == nil || age > heartbeatMaxAge:
		return "O DBSRV não está rodando (ou é uma versão antiga sem heartbeat). Inicie o dbsrv atualizado e tente de novo."
	case proto < store.DeliveryProtocol:
		return "O DBSRV em execução está desatualizado. Reinicie o dbsrv com o código atual e tente de novo."
	}
	return ""
}

// ---- rendering ----

func (s *Server) page(w http.ResponseWriter, r *http.Request, code int, name string, data map[string]any) {
	sess := sessionOf(r)
	data["Admin"] = sess.user
	data["CSRF"] = sess.csrf
	data["DBSRVWarning"] = s.dbsrvProblem(r.Context())
	if online, age, err := s.st.ChannelStatus(r.Context()); err == nil && age >= 0 && age <= heartbeatMaxAge {
		data["Channels"] = channelRows(online)
	}
	if n, err := s.st.UnseenAlerts(r.Context()); err == nil {
		data["UnseenAlerts"] = n
	}
	if n, err := s.st.UnseenGMMessages(r.Context()); err == nil {
		data["UnseenGM"] = n
	}
	s.renderStatus(w, code, name, data)
}

func (s *Server) render(w http.ResponseWriter, name string, data map[string]any) {
	s.renderStatus(w, http.StatusOK, name, data)
}

func (s *Server) renderStatus(w http.ResponseWriter, code int, name string, data map[string]any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	if err := s.tmpl[name].Execute(w, data); err != nil {
		s.log.Error("render", "page", name, "err", err)
	}
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	s.log.Error("admin request failed", "err", err)
	http.Error(w, "Erro interno. Veja o log do painel.", http.StatusInternalServerError)
}

// ---- labels ----

func statusLabel(s int16) string {
	switch auth.AccountStatus(s) {
	case auth.StatusBlocked:
		return "Bloqueada"
	case auth.StatusDefective:
		return "Defeituosa"
	case auth.StatusDisabled:
		return "Desativada"
	}
	return "Ativa"
}

func evolutionLabel(e int16) string {
	switch e {
	case 1:
		return "Mortal"
	case 2:
		return "Arch"
	case 3:
		return "Celestial"
	case 4:
		return "Sub-celestial"
	}
	return "?"
}

func kindLabel(k string) string {
	switch k {
	case "create_account":
		return "Criar conta"
	case "password":
		return "Senha / token"
	case "ban":
		return "Banimento"
	case "item":
		return "Item"
	case "cash":
		return "Cash"
	case "kick":
		return "Desconectar do jogo"
	case "edit":
		return "Edição"
	case "lock":
		return "Bloqueio da conta"
	case "notice":
		return "Aviso global"
	case "login_unblock":
		return "Liberar login (senha errada)"
	}
	return k
}

// resultLabel turns a delivery state into text and a tone (ok, wait, bad).
func resultLabel(d store.DeliveryInfo) (string, string) {
	r := d.Result
	if d.ProcessedAt == nil {
		switch {
		case r == "waiting_offline":
			return "Aguardando o jogador sair do jogo", "wait"
		case r == "waiting_character":
			return "Aguardando o jogador entrar com um personagem", "wait"
		case strings.HasPrefix(r, "error"):
			return "Erro, tentando de novo: " + strings.TrimPrefix(r, "error: "), "bad"
		case time.Since(d.CreatedAt) > 10*time.Second:
			return "Na fila há mais de 10s — o DBSRV está rodando?", "bad"
		}
		return "Na fila", "wait"
	}
	switch {
	case r == "ok" || strings.HasPrefix(r, "ok:"):
		return "Concluído", "ok"
	case r == "sent_online" && d.Kind == "kick":
		return "Jogador desconectado", "ok"
	case r == "sent_online":
		return "Enviado ao jogo", "ok"
	case d.Kind == "login_unblock" && strings.HasPrefix(r, "sent_online: "):
		return "Login liberado em " + strings.TrimPrefix(r, "sent_online: ") + " canal(is)", "ok"
	case r == "not_online" && d.Kind == "login_unblock":
		return "Nenhum canal aberto (o bloqueio some ao reiniciar o canal)", "ok"
	case d.Kind == "notice" && strings.HasPrefix(r, "sent_online: "):
		return "Enviado a " + strings.TrimPrefix(r, "sent_online: ") + " canal(is)", "ok"
	case r == "not_online" && d.Kind == "gm_command":
		return "Canal desligado", "bad"
	case r == "not_online" && d.Kind == "notice":
		return "Nenhum canal aberto", "bad"
	case r == "not_online":
		return "Jogador não estava online", "ok"
	case r == "exists":
		return "Conta já existe", "bad"
	case r == "not_found":
		return "Conta não encontrada", "bad"
	case r == "changed":
		return "O slot mudou antes de aplicar; confira e tente de novo", "bad"
	case r == "no_character":
		return "Personagem não existe", "bad"
	case r == "no_space":
		return "Sem espaço no baú", "bad"
	case strings.HasPrefix(r, "invalid"):
		return "Inválido: " + strings.TrimPrefix(strings.TrimPrefix(r, "invalid"), ": "), "bad"
	}
	return r, "bad"
}

// channelRow is one channel in the panel's status line.
type channelRow struct {
	Number, Online int
	Up             bool
}

// channelRows lists the channels DBSRV reported; -1 means its TMSRV is off.
func channelRows(online []int) []channelRow {
	var rows []channelRow
	for i, n := range online {
		rows = append(rows, channelRow{Number: i + 1, Online: max(n, 0), Up: n >= 0})
	}
	return rows
}
