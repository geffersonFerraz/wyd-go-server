package store

import (
	"context"
	"log/slog"
	"time"
)

// GameDataChannel is the NOTIFY channel of migration 0007.
const GameDataChannel = "gamedata_changed"

// ListenGameData calls onChange (from its own goroutine) after gamedata
// changes, at most once per quiet period: bursts of notifications (an
// import, several saves) collapse into one call. It reconnects on errors
// and returns when ctx ends.
func (s *Store) ListenGameData(ctx context.Context, quiet time.Duration, log *slog.Logger, onChange func()) {
	changed := make(chan struct{}, 1)
	go func() {
		var timer *time.Timer
		var fire <-chan time.Time
		for {
			select {
			case <-ctx.Done():
				return
			case <-changed:
				if timer == nil {
					timer = time.NewTimer(quiet)
				} else {
					timer.Reset(quiet)
				}
				fire = timer.C
			case <-fire:
				fire = nil
				onChange()
			}
		}
	}()

	for ctx.Err() == nil {
		err := s.listenOnce(ctx, changed)
		if ctx.Err() != nil {
			return
		}
		log.Warn("gamedata listener lost, retrying", "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Store) listenOnce(ctx context.Context, changed chan<- struct{}) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+GameDataChannel); err != nil {
		return err
	}
	// Anything changed while we were not listening is caught by one reload.
	select {
	case changed <- struct{}{}:
	default:
	}
	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			conn.Conn().Close(context.Background()) //nolint:errcheck // do not return a LISTENing conn to the pool
			return err
		}
		select {
		case changed <- struct{}{}:
		default:
		}
	}
}
