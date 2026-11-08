package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBanAndCash(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()

	if _, body, _ := h.get("/accounts/EXISTING"); !strings.Contains(body, "Banir conta") || !strings.Contains(body, "Entregar cash") {
		t.Fatal("no ban or cash form on the account page")
	}

	// Without days nor permanent: refused.
	code, body, _ := h.post("/accounts/EXISTING/ban", url.Values{"csrf": {csrf}, "type": {"1"}, "days": {"0"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "Informe os dias") {
		t.Fatalf("ban without days: %d", code)
	}

	code, _, _ = h.post("/accounts/EXISTING/ban", url.Values{"csrf": {csrf}, "type": {"2"}, "days": {"7"}})
	if code != http.StatusSeeOther {
		t.Fatalf("ban: %d", code)
	}
	kind, user, _, payload := h.lastPayload()
	until, _ := time.Parse(time.RFC3339, payload["until"].(string))
	if kind != "ban" || user != "EXISTING" || payload["type"] != float64(2) || time.Until(until) < 6*24*time.Hour {
		t.Fatalf("ban payload %s %s %v", kind, user, payload)
	}

	code, _, _ = h.post("/accounts/EXISTING/ban", url.Values{"csrf": {csrf}, "type": {"1"}, "days": {""}, "permanent": {"1"}})
	if _, _, _, payload = h.lastPayload(); code != http.StatusSeeOther || !strings.HasPrefix(payload["until"].(string), "2099-") {
		t.Fatalf("permanent ban: %d %v", code, payload)
	}

	code, _, _ = h.post("/accounts/EXISTING/unban", url.Values{"csrf": {csrf}})
	if kind, _, _, payload = h.lastPayload(); code != http.StatusSeeOther || kind != "ban" || payload["type"] != float64(0) {
		t.Fatalf("unban: %d %s %v", code, kind, payload)
	}

	code, body, _ = h.post("/accounts/EXISTING/cash", url.Values{"csrf": {csrf}, "amount": {"0"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "O cash") {
		t.Fatalf("cash 0: %d", code)
	}
	code, _, _ = h.post("/accounts/EXISTING/cash", url.Values{"csrf": {csrf}, "amount": {"500"}})
	if kind, _, _, payload = h.lastPayload(); code != http.StatusSeeOther || kind != "cash" || payload["amount"] != float64(500) {
		t.Fatalf("cash: %d %s %v", code, kind, payload)
	}
	// The request list shows the amount and the ban.
	if _, body, _ := h.get("/accounts/EXISTING"); !strings.Contains(body, "Cash: 500") || !strings.Contains(body, "retirar banimento") {
		t.Fatal("cash or ban missing from the request list")
	}

	if code, _, _ := h.post("/accounts/NOBODY/cash", url.Values{"csrf": {csrf}, "amount": {"5"}}); code != http.StatusNotFound {
		t.Fatalf("unknown account: %d", code)
	}
}

func TestNotice(t *testing.T) {
	h := newPanel(t)
	csrf := h.login()

	if code, body, _ := h.get("/notice"); code != http.StatusOK || !strings.Contains(body, "Enviar a todos") {
		t.Fatalf("notice page: %d", code)
	}
	code, body, _ := h.post("/notice", url.Values{"csrf": {csrf}, "text": {strings.Repeat("a", 96)}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "95 caracteres") {
		t.Fatalf("long notice: %d", code)
	}
	code, body, _ = h.post("/notice", url.Values{"csrf": {csrf}, "text": {"Olá 🙂"}})
	if code != http.StatusUnprocessableEntity || !strings.Contains(body, "não mostra") {
		t.Fatalf("emoji notice: %d", code)
	}
	code, _, hdr := h.post("/notice", url.Values{"csrf": {csrf}, "text": {" Manutenção às 22h "}})
	if code != http.StatusSeeOther || !strings.HasPrefix(hdr.Get("Location"), "/notice?req=") {
		t.Fatalf("notice: %d", code)
	}
	kind, user, _, payload := h.lastPayload()
	if kind != "notice" || user != "*" || payload["text"] != "Manutenção às 22h" {
		t.Fatalf("payload %s %s %v", kind, user, payload)
	}
	if _, body, _ := h.get("/notice"); !strings.Contains(body, "Manutenção às 22h") {
		t.Fatal("notice not listed")
	}
}
