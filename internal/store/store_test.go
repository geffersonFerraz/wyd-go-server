package store

import (
	"bytes"
	"context"
	"os"
	"testing"

	"dbsrv/internal/wire"
)

// Integration tests need a throwaway database:
//
//	DBSRV_TEST_DSN=postgres://user:pass@localhost:5432/dbsrv_test go test ./internal/store/
//
// The database is wiped (public schema dropped) before running.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("DBSRV_TEST_DSN")
	if dsn == "" {
		t.Skip("DBSRV_TEST_DSN not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, dsn, testLoc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.ResetForTests(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err != nil { // idempotent
		t.Fatal(err)
	}
	return s
}

func TestStoreAccountRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	in := sampleAccount(t)

	if err := s.SaveAccount(ctx, in); err != nil {
		t.Fatal(err)
	}
	// Second save is an update, not a duplicate.
	in.Account.Cash = 777
	if err := s.SaveAccount(ctx, in); err != nil {
		t.Fatal(err)
	}

	out, err := s.LoadAccount(ctx, "anderson")
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != in.ID || out.Account.Cash != 777 {
		t.Errorf("id/cash: %d/%d", out.ID, out.Account.Cash)
	}
	if !bytes.Equal(wire.MustMarshal(&in.Account), wire.MustMarshal(&out.Account)) {
		t.Error("account changed through Postgres")
	}

	// Columns are authoritative: an operator edit shows up on load.
	if _, err := s.pool.Exec(ctx, `UPDATE accounts SET cash = 5 WHERE id = $1`, in.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `UPDATE items SET item_id = 999 WHERE account_id = $1 AND char_slot = -1 AND slot = 39`, in.ID); err != nil {
		t.Fatal(err)
	}
	out, _ = s.LoadAccount(ctx, "ANDERSON")
	if out.Account.Cash != 5 {
		t.Errorf("cash column not applied: %d", out.Account.Cash)
	}
	if wire.CString(out.Account.Username[:]) == "ANDERSON" && out.Account.StorageItems[39].Index != 999 && in.Account.StorageItems[39].Index != 0 {
		t.Errorf("item row not applied: %d", out.Account.StorageItems[39].Index)
	}

	if _, err := s.LoadAccount(ctx, "NOBODY"); err != ErrNotFound {
		t.Errorf("missing account: %v", err)
	}
}

func TestReserveName(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	a, b := sampleAccount(t), sampleAccount(t)
	wire.PutCString(b.Account.Username[:], "OTHER")
	if err := s.SaveAccount(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveAccount(ctx, b); err != nil {
		t.Fatal(err)
	}

	if ok, err := s.ReserveName(ctx, a.ID, "Hero", false); !ok || err != nil {
		t.Fatalf("first reserve: %v %v", ok, err)
	}
	if ok, _ := s.ReserveName(ctx, a.ID, "HERO", true); !ok {
		t.Error("owner re-reserving (arch creation) must succeed")
	}
	if ok, _ := s.ReserveName(ctx, a.ID, "hero", false); ok {
		t.Error("new character must not reuse an existing name, even the account's own")
	}
	if ok, _ := s.ReserveName(ctx, b.ID, "hero", true); ok {
		t.Error("other account took a reserved name")
	}
	if owner, _ := s.NameOwner(ctx, "hErO"); owner != "ANDERSON" {
		t.Errorf("owner = %q", owner)
	}
	if err := s.ReleaseName(ctx, b.ID, "hero"); err != nil {
		t.Fatal(err)
	}
	if owner, _ := s.NameOwner(ctx, "hero"); owner != "ANDERSON" {
		t.Error("non-owner released the name")
	}
	_ = s.ReleaseName(ctx, a.ID, "hero")
	if ok, _ := s.ReserveName(ctx, b.ID, "hero", false); !ok {
		t.Error("released name not reusable")
	}
}

func TestSealSequence(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.AdvanceSealSeq(ctx, 2); err != nil {
		t.Fatal(err)
	}
	var next int
	if err := s.pool.QueryRow(ctx, `SELECT nextval('seal_id_seq')`).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if next != 3 {
		t.Errorf("next seal id %d, want 3", next)
	}
}
