package store

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestGameDataNotifiesListeners(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.ImportGameData(ctx, &GameData{Items: []Item{{ID: 1, Name: "a"}}}, "test"); err != nil {
		t.Fatal(err)
	}
	reloads := make(chan struct{}, 10)
	go s.ListenGameData(ctx, 100*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)), func() { reloads <- struct{}{} })
	// The listener reports once right after connecting.
	select {
	case <-reloads:
	case <-time.After(5 * time.Second):
		t.Fatal("no initial reload")
	}
	// Several saves in a burst collapse into one reload.
	for i := range 3 {
		if err := s.SaveItem(ctx, &Item{ID: 1, Name: "a", Price: i}, "test"); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-reloads:
	case <-time.After(5 * time.Second):
		t.Fatal("save did not notify")
	}
	select {
	case <-reloads:
		t.Fatal("burst was not collapsed")
	case <-time.After(400 * time.Millisecond):
	}
	changes, err := s.RecentChanges(ctx, "item", "1", 10)
	if err != nil || len(changes) != 3 || changes[0].By != "test" {
		t.Fatalf("change log: %+v %v", changes, err)
	}
}
