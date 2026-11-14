package admin

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/store"
)

// Needs DBSRV_TEST_DSN (wiped), like the store and server tests.

type panelHarness struct {
	t     *testing.T
	panel *Server
	st    *store.Store
	ts    *httptest.Server
	c     *http.Client
	ctx   context.Context
}

func newPanel(t *testing.T) *panelHarness {
	t.Helper()
	dsn := os.Getenv("DBSRV_TEST_DSN")
	if dsn == "" {
		t.Skip("DBSRV_TEST_DSN not set")
	}
	ctx := context.Background()
	st, err := store.OpenUnprivileged(ctx, dsn, time.Local) // as cmd/admin does
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := st.ResetForTests(ctx); err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	// Pretend a current dbsrv is running.
	if err := st.Heartbeat(ctx, store.DeliveryProtocol); err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.Hash("adminpw")
	accHash, _ := auth.Hash("x")
	if ok, err := st.CreateAccount(ctx, "EXISTING", accHash, nil, 0); !ok || err != nil {
		t.Fatal(err)
	}
	p, err := New(Config{Users: map[string]string{"admin": hash}}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(p.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &panelHarness{t: t, panel: p, st: st, ts: ts, c: c, ctx: ctx}
}

func (h *panelHarness) get(path string) (int, string, http.Header) {
	h.t.Helper()
	resp, err := h.c.Get(h.ts.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func (h *panelHarness) post(path string, form url.Values) (int, string, http.Header) {
	h.t.Helper()
	resp, err := h.c.PostForm(h.ts.URL+path, form)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

func (h *panelHarness) postRaw(path, contentType string, body io.Reader) (int, string, http.Header) {
	h.t.Helper()
	resp, err := h.c.Post(h.ts.URL+path, contentType, body)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (h *panelHarness) login() string {
	h.t.Helper()
	code, _, hdr := h.post("/login", url.Values{"user": {"admin"}, "password": {"adminpw"}})
	if code != http.StatusSeeOther || hdr.Get("Location") != "/accounts" {
		h.t.Fatalf("login: %d %s", code, hdr.Get("Location"))
	}
	_, body, _ := h.get("/accounts")
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		h.t.Fatal("no csrf token on page")
	}
	return m[1]
}

// lastPayload returns the newest delivery row as stored.
func (h *panelHarness) lastPayload() (kind, user, requestedBy string, payload map[string]any) {
	h.t.Helper()
	list, err := h.st.PendingDeliveries(h.ctx, 100)
	if err != nil || len(list) == 0 {
		h.t.Fatalf("no pending delivery: %v", err)
	}
	d := list[len(list)-1]
	_ = json.Unmarshal(d.Payload, &payload)
	recent, _ := h.st.RecentDeliveries(h.ctx, "", 1)
	return d.Kind, d.Username, recent[0].RequestedBy, payload
}

func TestAuth(t *testing.T) {
	h := newPanel(t)

	if code, _, hdr := h.get("/accounts"); code != http.StatusSeeOther || hdr.Get("Location") != "/login" {
		t.Fatalf("unauthenticated: %d %s", code, hdr.Get("Location"))
	}
	if code, _, _ := h.get("/api/deliveries/1"); code != http.StatusUnauthorized {
		t.Errorf("api unauthenticated: %d", code)
	}
	if code, body, _ := h.post("/login", url.Values{"user": {"admin"}, "password": {"wrong"}}); code != http.StatusUnauthorized || !strings.Contains(body, "incorretos") {
		t.Errorf("bad password: %d", code)
	}
	if _, _, hdr := h.get("/login"); !strings.Contains(hdr.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("missing CSP header")
	}

	csrf := h.login()
	// POST without the CSRF token is refused.
	if code, _, _ := h.post("/accounts", url.Values{"username": {"NOCSRF"}, "password": {"a"}, "password2": {"a"}}); code != http.StatusForbidden {
		t.Errorf("missing csrf: %d", code)
	}
	if code, _, _ := h.post("/logout", url.Values{"csrf": {csrf}}); code != http.StatusSeeOther {
		t.Errorf("logout: %d", code)
	}
	if code, _, _ := h.get("/accounts"); code != http.StatusSeeOther {
		t.Error("session survived logout")
	}
}

func TestLoginLockout(t *testing.T) {
	h := newPanel(t)
	for i := 0; i < maxFailures; i++ {
		h.post("/login", url.Values{"user": {"admin"}, "password": {"nope"}})
	}
	if code, _, _ := h.post("/login", url.Values{"user": {"admin"}, "password": {"adminpw"}}); code != http.StatusTooManyRequests {
		t.Errorf("after %d failures: %d, want 429", maxFailures, code)
	}
}

func TestCreateAccount(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	form := func(user, pw, pw2, tok string) url.Values {
		return url.Values{"csrf": {csrf}, "username": {user}, "password": {pw}, "password2": {pw2}, "token": {tok}}
	}

	for _, c := range []struct {
		name string
		f    url.Values
		msg  string
	}{
		{"short login", form("ab", "senha1", "senha1", ""), "3 a 15"},
		{"symbols", form("bad name", "senha1", "senha1", ""), "3 a 15"},
		{"device name", form("COM1", "senha1", "senha1", ""), "3 a 15"},
		{"long password", form("NOVA", "123456789012", "123456789012", ""), "1 a 11"},
		{"mismatch", form("NOVA", "senha1", "senha2", ""), "não conferem"},
		{"bad token", form("NOVA", "senha1", "senha1", "12ab"), "1 a 6 dígitos"},
		{"exists", form("existing", "senha1", "senha1", ""), "Já existe"},
	} {
		code, body, _ := h.post("/accounts", c.f)
		if code != http.StatusUnprocessableEntity || !strings.Contains(body, c.msg) {
			t.Errorf("%s: %d, want 422 with %q", c.name, code, c.msg)
		}
	}
	if list, _ := h.st.PendingDeliveries(h.ctx, 10); len(list) != 0 {
		t.Fatalf("invalid forms queued %d deliveries", len(list))
	}

	code, _, hdr := h.post("/accounts", form("nova", "senha1", "senha1", "1234"))
	if code != http.StatusSeeOther || !strings.HasPrefix(hdr.Get("Location"), "/accounts/NOVA?req=") {
		t.Fatalf("create: %d %s", code, hdr.Get("Location"))
	}
	kind, user, by, payload := h.lastPayload()
	if kind != "create_account" || user != "NOVA" || by != "admin" {
		t.Errorf("delivery %s %s by %q", kind, user, by)
	}
	raw, _ := json.Marshal(payload)
	if strings.Contains(string(raw), "senha1") || strings.Contains(string(raw), `"1234"`) {
		t.Fatalf("plain secret stored in the queue: %s", raw)
	}
	if ok, _ := auth.Verify(payload["password_hash"].(string), "senha1"); !ok {
		t.Error("password_hash does not verify")
	}
	if ok, _ := auth.Verify(payload["numeric_hash"].(string), "1234"); !ok {
		t.Error("numeric_hash does not verify")
	}

	// Before DBSRV processes it the account page explains the wait.
	if code, body, _ := h.get(hdr.Get("Location")); code != http.StatusOK || !strings.Contains(body, "ainda não existe") {
		t.Errorf("pending account page: %d", code)
	}
}

func TestChangePasswordAndToken(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()

	if code, body, _ := h.get("/accounts/existing"); code != http.StatusOK || !strings.Contains(body, "EXISTING") {
		t.Fatalf("account page: %d", code)
	}
	if code, _, _ := h.get("/accounts/NOPE"); code != http.StatusNotFound {
		t.Errorf("unknown account: %d", code)
	}

	code, body, _ := h.post("/accounts/EXISTING/password", url.Values{"csrf": {csrf}, "password": {"a"}, "password2": {"b"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "não conferem") {
		t.Errorf("mismatch: %d", code)
	}
	code, _, _ = h.post("/accounts/EXISTING/password", url.Values{"csrf": {csrf}, "password": {"nova123"}, "password2": {"nova123"}})
	if code != http.StatusSeeOther {
		t.Fatalf("password: %d", code)
	}
	kind, user, _, payload := h.lastPayload()
	if kind != "password" || user != "EXISTING" || payload["numeric_hash"] != nil {
		t.Errorf("password delivery %s %s %v", kind, user, payload)
	}
	if ok, _ := auth.Verify(payload["password_hash"].(string), "nova123"); !ok {
		t.Error("password hash wrong")
	}

	code, _, _ = h.post("/accounts/EXISTING/token", url.Values{"csrf": {csrf}, "token": {"9876"}, "token2": {"9876"}})
	if code != http.StatusSeeOther {
		t.Fatalf("token: %d", code)
	}
	_, _, _, payload = h.lastPayload()
	if payload["password_hash"] != nil {
		t.Error("token change must not touch the password")
	}
	if ok, _ := auth.Verify(payload["numeric_hash"].(string), "9876"); !ok {
		t.Error("token hash wrong")
	}

	// Status API.
	list, _ := h.st.PendingDeliveries(h.ctx, 10)
	_, body, _ = h.get("/api/deliveries/" + itoa(list[0].ID))
	var st struct {
		Done  bool
		Label string
	}
	_ = json.Unmarshal([]byte(body), &st)
	if st.Done || st.Label == "" {
		t.Errorf("status api: %s", body)
	}
}

// An outdated or stopped dbsrv must not receive requests it would reject
// (this is how GEFF and ABOBRINHA failed: the old dbsrv rejected hashes).
func TestRefusesWhenDBSRVOutdatedOrDown(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	create := url.Values{"csrf": {csrf}, "username": {"GEFF"}, "password": {"senha1"}, "password2": {"senha1"}}

	if err := h.st.Heartbeat(h.ctx, store.DeliveryProtocol-1); err != nil {
		t.Fatal(err)
	}
	code, body, _ := h.post("/accounts", create)
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "desatualizado") {
		t.Errorf("old dbsrv: %d", code)
	}
	if _, body, _ := h.get("/accounts/EXISTING"); !strings.Contains(body, "desatualizado") {
		t.Error("no banner for outdated dbsrv")
	}

	if _, err := h.st.Pool().Exec(h.ctx, `UPDATE server_status SET dbsrv_protocol = $1, dbsrv_seen_at = now() - interval '1 minute'`, store.DeliveryProtocol); err != nil {
		t.Fatal(err)
	}
	code, body, _ = h.post("/accounts/EXISTING/token", url.Values{"csrf": {csrf}, "token": {"1"}, "token2": {"1"}})
	if code != http.StatusServiceUnavailable || !strings.Contains(body, "não está rodando") {
		t.Errorf("stale heartbeat: %d", code)
	}
	if list, _ := h.st.PendingDeliveries(h.ctx, 10); len(list) != 0 {
		t.Fatalf("queued %d deliveries while dbsrv was unusable", len(list))
	}

	_ = h.st.Heartbeat(h.ctx, store.DeliveryProtocol)
	if code, _, _ := h.post("/accounts", create); code != http.StatusSeeOther {
		t.Errorf("after restart: %d", code)
	}
}

func TestKickButton(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	if _, body, _ := h.get("/accounts/EXISTING"); strings.Contains(body, "Desconectar do jogo</button>") {
		t.Error("kick button shown for an offline account")
	}
	if ok, _, err := h.st.ClaimOnline(h.ctx, "EXISTING", 1, 42); err != nil || !ok {
		t.Fatal(err)
	}
	_, body, _ := h.get("/accounts/EXISTING")
	if !strings.Contains(body, "Desconectar do jogo</button>") || !strings.Contains(body, "canal 1") {
		t.Fatal("online account page lacks the kick button")
	}
	if _, list, _ := h.get("/accounts"); !strings.Contains(list, `pill info">online`) {
		t.Error("no online badge in the list")
	}
	if code, _, _ := h.post("/accounts/EXISTING/kick", url.Values{"csrf": {csrf}}); code != http.StatusSeeOther {
		t.Fatalf("kick: %d", code)
	}
	if kind, user, _, _ := h.lastPayload(); kind != "kick" || user != "EXISTING" {
		t.Errorf("queued %s for %s", kind, user)
	}
}

func TestLoginUnblock(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()
	if _, body, _ := h.get("/accounts/EXISTING"); !strings.Contains(body, "Liberar login agora</button>") {
		t.Fatal("no login unblock button")
	}
	if code, _, _ := h.post("/accounts/existing/login-unblock", url.Values{"csrf": {csrf}}); code != http.StatusSeeOther {
		t.Fatalf("login unblock: %d", code)
	}
	if kind, user, _, _ := h.lastPayload(); kind != "login_unblock" || user != "EXISTING" {
		t.Errorf("queued %s for %s", kind, user)
	}
}

func TestSearch(t *testing.T) {
	h := newPanel(t)
	h.login()
	if _, body, _ := h.get("/accounts?q=xist"); !strings.Contains(body, "EXISTING") {
		t.Error("search by substring failed")
	}
	if _, body, _ := h.get("/accounts?q=zzz%25"); strings.Contains(body, "EXISTING") {
		t.Error("LIKE wildcard not escaped")
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
