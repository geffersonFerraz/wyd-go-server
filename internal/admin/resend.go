package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

const resendEndpoint = "https://api.resend.com/emails"

// ResendMailer sends email through the Resend API. It stays disabled until
// both the API key and the sender address are configured.
type ResendMailer struct {
	key, from, endpoint string
	client              *http.Client
}

// NewResendMailer returns a mailer; blank settings give a disabled one.
func NewResendMailer(key, from string) *ResendMailer {
	return &ResendMailer{
		key:      strings.TrimSpace(key),
		from:     strings.TrimSpace(from),
		endpoint: resendEndpoint,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled reports whether the mailer can send.
func (m *ResendMailer) Enabled() bool { return m != nil && m.key != "" && m.from != "" }

// SendWelcome tells a new player the account exists. It never carries the
// password or the token.
func (m *ResendMailer) SendWelcome(ctx context.Context, to, username string) error {
	name := html.EscapeString(username)
	return m.send(ctx, map[string]any{
		"from":    m.from,
		"to":      []string{to},
		"subject": "Sua conta foi criada",
		"html": "<p>Sua conta <b>" + name + "</b> foi criada.</p>" +
			"<p>Este email será usado para recuperar a conta.</p>" +
			"<p>Se não foi você, ignore esta mensagem.</p>",
		"text": "Sua conta " + username + " foi criada.\n\n" +
			"Este email será usado para recuperar a conta.\n\n" +
			"Se não foi você, ignore esta mensagem.\n",
	})
}

func (m *ResendMailer) send(ctx context.Context, message map[string]any) error {
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "wyd-admin")
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("resend: %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}
