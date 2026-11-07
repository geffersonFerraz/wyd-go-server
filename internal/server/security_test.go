package server

import (
	"context"
	"testing"

	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// A TMSRV alert (0xC13) is stored for the admin panel.
func TestSecurityAlertStored(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	tm := h.connect()
	p := wire.SecurityAlert{Header: wire.NewHeader(wire.OpSecurityAlert, 3), Kind: wire.AlertWPE, Channel: 1}
	wire.PutCString(p.Username[:], "TESTER")
	wire.PutCString(p.CharName[:], "Heroi")
	wire.PutCString(p.IP[:], "10.0.0.7")
	wire.PutGameString(p.Detail[:], "detectou o WPE")
	tm.send(&p)
	var got []store.Alert
	waitFor(t, func() bool {
		got, _ = h.st.Alerts(ctx, true, 10)
		return len(got) == 1
	})
	a := got[0]
	if a.Kind != store.AlertKindWPE || a.Username != "TESTER" || a.CharName != "Heroi" || a.IP != "10.0.0.7" || a.Detail != "detectou o WPE" {
		t.Fatalf("alert %+v", a)
	}
	if err := h.st.MarkAlertsSeen(ctx, 0, "test"); err != nil {
		t.Fatal(err)
	}
	if n, _ := h.st.UnseenAlerts(ctx); n != 0 {
		t.Fatalf("%d unseen after marking all", n)
	}
}
