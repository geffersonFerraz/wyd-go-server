package admin

import (
	"context"
	_ "embed"
	"html/template"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"

	"dbsrv/internal/auth"
	"dbsrv/internal/server"
)

// RegisterPath is the public account registration page, opened by the game
// client.
const RegisterPath = "/registro"

const (
	registerWindowLen   = time.Hour
	registerMaxAttempts = 30 // per address and window
	registerMaxCreated  = 5  // per address and window
	registerMaxBody     = 4096
	registerSendTimeout = 15 * time.Second
)

// The limits are the game client's: its login box takes 4 to 12 characters
// of ID and 4 to 10 of password, in ASCII.
var (
	registerUserRe     = regexp.MustCompile(`^[A-Za-z0-9]{4,12}$`)
	registerPasswordRe = regexp.MustCompile(`^[\x21-\x7e]{4,10}$`)
)

//go:embed register/page.html
var registerPageSource string

var registerPage = template.Must(template.New("register").Parse(registerPageSource))

// RegisterStore is what the registration page needs from the database.
type RegisterStore interface {
	CreateAccountWithEmail(ctx context.Context, username, passwordHash string, numericHash, email *string) (bool, error)
}

type registerForm struct {
	Action, Username, Email, Error string
	Done                           bool
}

type registerHandler struct {
	st     RegisterStore
	mailer *ResendMailer
	log    *slog.Logger
	limit  *registerLimiter
}

// RegisterHandler serves the registration page. mailer may be disabled: the
// address is kept either way and only the welcome message is skipped.
func RegisterHandler(st RegisterStore, mailer *ResendMailer, log *slog.Logger) http.Handler {
	return &registerHandler{st: st, mailer: mailer, log: log, limit: newRegisterLimiter()}
}

func (h *registerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		h.render(w, http.StatusOK, registerForm{})
	case http.MethodPost:
		h.submit(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *registerHandler) render(w http.ResponseWriter, code int, form registerForm) {
	form.Action = RegisterPath
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := registerPage.Execute(w, form); err != nil {
		h.log.Warn("registration page", "err", err)
	}
}

func (h *registerHandler) submit(w http.ResponseWriter, r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if !h.limit.allow(ip) {
		h.render(w, http.StatusTooManyRequests, registerForm{Error: "Muitas tentativas. Tente de novo mais tarde."})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, registerMaxBody)
	if err := r.ParseForm(); err != nil {
		h.render(w, http.StatusBadRequest, registerForm{Error: "Formulário inválido."})
		return
	}

	user := strings.ToUpper(strings.TrimSpace(r.PostFormValue("username")))
	pw, pw2 := r.PostFormValue("password"), r.PostFormValue("password2")
	token := strings.TrimSpace(r.PostFormValue("token"))
	email := strings.TrimSpace(r.PostFormValue("email"))
	form := registerForm{Username: user, Email: email}
	invalid := func(msg string) {
		form.Error = msg
		h.render(w, http.StatusUnprocessableEntity, form)
	}

	switch {
	case !registerUserRe.MatchString(user) || server.IsDeviceName(user):
		invalid("O login deve ter de 4 a 12 letras ou números.")
		return
	case !registerPasswordRe.MatchString(pw):
		invalid("A senha deve ter de 4 a 10 caracteres, sem espaços ou acentos.")
		return
	case pw != pw2:
		invalid("As senhas não conferem.")
		return
	case !server.ValidNumericPassword(token):
		invalid("O token deve ter de 1 a 6 dígitos.")
		return
	case email != "" && !validEmail(email):
		invalid("Esse email não é válido.")
		return
	}

	failed := func(err error) {
		h.log.Error("registration", "account", user, "err", err)
		form.Error = "Não foi possível criar a conta agora. Tente de novo."
		h.render(w, http.StatusInternalServerError, form)
	}
	passwordHash, err := auth.Hash(pw)
	if err != nil {
		failed(err)
		return
	}
	numericHash, err := auth.Hash(token)
	if err != nil {
		failed(err)
		return
	}
	var emailValue *string
	if email != "" {
		emailValue = &email
	}
	created, err := h.st.CreateAccountWithEmail(r.Context(), user, passwordHash, &numericHash, emailValue)
	if err != nil {
		failed(err)
		return
	}
	if !created {
		invalid("Já existe uma conta com esse login.")
		return
	}

	h.limit.created(ip)
	h.log.Info("account registered", "account", user, "with_email", email != "")
	if email != "" && h.mailer.Enabled() {
		go h.sendWelcome(email, user)
	}
	h.render(w, http.StatusOK, registerForm{Username: user, Done: true})
}

func (h *registerHandler) sendWelcome(to, user string) {
	ctx, cancel := context.WithTimeout(context.Background(), registerSendTimeout)
	defer cancel()
	if err := h.mailer.SendWelcome(ctx, to, user); err != nil {
		h.log.Warn("welcome email not sent", "account", user, "err", err)
	}
}

func validEmail(s string) bool {
	if len(s) > 254 {
		return false
	}
	a, err := mail.ParseAddress(s)
	return err == nil && a.Address == s
}

// registerLimiter counts form submissions per address: the page has no login
// and every valid submission costs two password hashes.
type registerLimiter struct {
	mu   sync.Mutex
	now  func() time.Time
	seen map[string]*registerWindow
}

type registerWindow struct {
	start             time.Time
	attempts, created int
}

func newRegisterLimiter() *registerLimiter {
	return &registerLimiter{now: time.Now, seen: map[string]*registerWindow{}}
}

func (l *registerLimiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if len(l.seen) >= 4096 {
		for k, v := range l.seen {
			if now.Sub(v.start) >= registerWindowLen {
				delete(l.seen, k)
			}
		}
	}
	win := l.seen[ip]
	if win == nil || now.Sub(win.start) >= registerWindowLen {
		win = &registerWindow{start: now}
		l.seen[ip] = win
	}
	if win.attempts >= registerMaxAttempts || win.created >= registerMaxCreated {
		return false
	}
	win.attempts++
	return true
}

func (l *registerLimiter) created(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if win := l.seen[ip]; win != nil {
		win.created++
	}
}
