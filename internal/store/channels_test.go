package store

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestChannelStatus(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	online, age, err := s.ChannelStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(online) != 0 || age != -1 {
		t.Fatalf("fresh database: online %v age %v, want [] -1", online, age)
	}

	if err := s.Heartbeat(ctx, DeliveryProtocol); err != nil {
		t.Fatal(err)
	}
	want := []int{3, 0, -1, -1, -1, -1, -1, -1, -1, -1}
	if err := s.ReportChannels(ctx, want); err != nil {
		t.Fatal(err)
	}
	online, age, err = s.ChannelStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(online) != fmt.Sprint(want) {
		t.Errorf("online %v, want %v", online, want)
	}
	if age < 0 || age > 10*time.Second {
		t.Errorf("age %v right after a heartbeat", age)
	}

	// The admin panel's unprivileged role must be able to read it.
	u, err := OpenUnprivileged(ctx, s.pool.Config().ConnString(), testLoc)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if online, _, err = u.ChannelStatus(ctx); err != nil || fmt.Sprint(online) != fmt.Sprint(want) {
		t.Errorf("unprivileged read: %v %v", online, err)
	}
}
