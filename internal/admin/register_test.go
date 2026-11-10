package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"dbsrv/internal/auth"
)

type fakeRegisterStore struct {
	mu       sync.Mutex
	accounts map[string][3]string // password hash, numeric hash, email
	err      error
}

func (f *fakeRegisterStore) CreateAccountWithEmail(_ context.Context, user, hash string, num, email *string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return false, f.err
	}
	if _, ok := f.accounts[user]; ok {
		return false, nil
	}
	rec := [3]string{hash, *num, ""}
	if email != nil {
		rec[2] = *email
	}
	f.accounts[user] = rec
	return true, nil
}

func registerServer(t *testing.T, mailer *ResendMailer) (*fakeRegisterStore, *registerHandler, *httptest.Server) {
	t.Helper()
	st := &fakeRegisterStore{accounts: map[string][3]string{}}
	h := RegisterHandler(st, mailer, slog.New(slog.NewTextHandler(io.Discard, nil))).(*registerHandler)
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return st, h, ts
}

func registerValues(user, pw, pw2, token, email string) url.Values {
	return url.Values{"username": {user}, "password": {pw}, "password2": {pw2}, "token": {token}, "email": {email}}
}

func postRegister(t *testing.T, ts *httptest.Server, form url.Values) (int, string) {
	t.Helper()
	resp, err := http.PostForm(ts.URL, form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestRegisterPageShowsFormAndEmailNotice(t *testing.T) {
	_, _, ts := registerServer(t, nil)
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	page := string(b)
	for _, want := range []string{`name="username"`, `name="password"`, `name="password2"`, `name="token"`, `name="email"`,
		"O email é opcional, mas é ele que será usado para recuperar a conta."} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestRegisterValidation(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"short login", registerValues("abc", "senha1", "senha1", "123", ""), "login deve ter"},
		{"long login", registerValues("abcdefghijklm", "senha1", "senha1", "123", ""), "login deve ter"},
		{"device name", registerValues("COM1", "senha1", "senha1", "123", ""), "login deve ter"},
		{"short password", registerValues("NOVA", "abc", "abc", "123", ""), "senha deve ter"},
		{"long password", registerValues("NOVA", "12345678901", "12345678901", "123", ""), "senha deve ter"},
		{"accented password", registerValues("NOVA", "senhá1", "senhá1", "123", ""), "senha deve ter"},
		{"mismatch", registerValues("NOVA", "senha1", "senha2", "123", ""), "não conferem"},
		{"empty token", registerValues("NOVA", "senha1", "senha1", "", ""), "1 a 6 dígitos"},
		{"long token", registerValues("NOVA", "senha1", "senha1", "1234567", ""), "1 a 6 dígitos"},
		{"letters token", registerValues("NOVA", "senha1", "senha1", "12ab", ""), "1 a 6 dígitos"},
		{"bad email", registerValues("NOVA", "senha1", "senha1", "123", "nao-e-email"), "não é válido"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st, _, ts := registerServer(t, nil)
			code, body := postRegister(t, ts, c.form)
			if code != http.StatusUnprocessableEntity || !strings.Contains(body, c.want) {
				t.Errorf("code %d, want the error %q", code, c.want)
			}
			if len(st.accounts) != 0 {
				t.Error("account created")
			}
		})
	}
}

func TestRegisterCreatesAccountWithoutMailer(t *testing.T) {
	st, _, ts := registerServer(t, NewResendMailer("", ""))
	code, body := postRegister(t, ts, registerValues("nova", "senha1", "senha1", "123456", "a@example.com"))
	if code != http.StatusOK || !strings.Contains(body, "NOVA") || !strings.Contains(body, "criada") {
		t.Fatalf("code %d", code)
	}
	rec, ok := st.accounts["NOVA"]
	if !ok {
		t.Fatal("account not saved, or not in upper case")
	}
	if good, _ := auth.Verify(rec[0], "senha1"); !good {
		t.Error("password hash does not verify")
	}
	if good, _ := auth.Verify(rec[1], "123456"); !good {
		t.Error("token hash does not verify")
	}
	if rec[2] != "a@example.com" {
		t.Errorf("email %q", rec[2])
	}
	if strings.Contains(body, "senha1") {
		t.Error("the page echoes the password")
	}

	code, body = postRegister(t, ts, registerValues("NOVA", "outra1", "outra1", "1", ""))
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Já existe") {
		t.Errorf("duplicate: %d", code)
	}
}

func TestRegisterEmailIsOptional(t *testing.T) {
	st, _, ts := registerServer(t, nil)
	if code, _ := postRegister(t, ts, registerValues("NOVA", "senha1", "senha1", "1", "")); code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if st.accounts["NOVA"][2] != "" {
		t.Error("email should be empty")
	}
}

func TestRegisterStoreFailureHidesDetails(t *testing.T) {
	st, _, ts := registerServer(t, nil)
	st.err = errors.New("pq: secret detail")
	code, body := postRegister(t, ts, registerValues("NOVA", "senha1", "senha1", "1", ""))
	if code != http.StatusInternalServerError || strings.Contains(body, "secret detail") {
		t.Errorf("code %d", code)
	}
}

func TestRegisterLimits(t *testing.T) {
	_, h, ts := registerServer(t, nil)
	clock := time.Unix(1000, 0)
	h.limit.now = func() time.Time { return clock }
	var code int
	for _, user := range []string{"CONTA1", "CONTA2", "CONTA3", "CONTA4", "CONTA5"} {
		if code, _ = postRegister(t, ts, registerValues(user, "senha1", "senha1", "1", "")); code != http.StatusOK {
			t.Fatalf("%s: %d", user, code)
		}
	}
	if code, _ = postRegister(t, ts, registerValues("CONTA6", "senha1", "senha1", "1", "")); code != http.StatusTooManyRequests {
		t.Fatalf("sixth account: %d", code)
	}
	clock = clock.Add(registerWindowLen)
	if code, _ = postRegister(t, ts, registerValues("CONTA6", "senha1", "senha1", "1", "")); code != http.StatusOK {
		t.Fatalf("after the window: %d", code)
	}
}

func TestRegisterSendsWelcomeThroughResend(t *testing.T) {
	type sent struct {
		auth string
		body map[string]any
	}
	got := make(chan sent, 1)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- sent{r.Header.Get("Authorization"), body}
		w.Write([]byte(`{"id":"1"}`))
	}))
	defer api.Close()
	mailer := NewResendMailer("re_chave", "WYD <wyd@example.com>")
	mailer.endpoint = api.URL
	_, _, ts := registerServer(t, mailer)

	if code, _ := postRegister(t, ts, registerValues("NOVA", "senha1", "senha1", "654321", "a@example.com")); code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	select {
	case s := <-got:
		if s.auth != "Bearer re_chave" || s.body["from"] != "WYD <wyd@example.com>" {
			t.Errorf("request: %+v", s)
		}
		raw, _ := json.Marshal(s.body)
		if !strings.Contains(string(raw), "a@example.com") || !strings.Contains(string(raw), "NOVA") {
			t.Errorf("message: %s", raw)
		}
		if strings.Contains(string(raw), "senha1") || strings.Contains(string(raw), "654321") {
			t.Error("the email carries a secret")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing sent")
	}
}

func TestResendDisabledWithoutSettings(t *testing.T) {
	var nilMailer *ResendMailer
	for name, m := range map[string]*ResendMailer{"nil": nilMailer, "blank": NewResendMailer(" ", ""), "no sender": NewResendMailer("re_x", ""), "no key": NewResendMailer("", "a@b.c")} {
		if m.Enabled() {
			t.Errorf("%s: enabled", name)
		}
	}
	if !NewResendMailer("re_x", "a@b.c").Enabled() {
		t.Error("configured mailer is disabled")
	}
}

func TestResendReportsAPIError(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"domain not verified"}`, http.StatusForbidden)
	}))
	defer api.Close()
	m := NewResendMailer("re_chave", "a@b.c")
	m.endpoint = api.URL
	err := m.SendWelcome(context.Background(), "x@y.z", "NOVA")
	if err == nil || !strings.Contains(err.Error(), "domain not verified") || strings.Contains(err.Error(), "re_chave") {
		t.Errorf("err = %v", err)
	}
}
