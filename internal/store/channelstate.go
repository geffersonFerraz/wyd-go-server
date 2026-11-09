package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CityBid is a guild's bet on a city.
type CityBid struct {
	Guild int `json:"guild"`
	Value int `json:"value"`
}

// CityState is what changes in a city while a channel runs.
type CityState struct {
	Impost int64     `json:"impost"`
	Tax    int       `json:"tax"`
	Wins   int       `json:"wins"`
	Chall1 int       `json:"chall1"`
	Chall2 int       `json:"chall2"`
	Bids   []CityBid `json:"bids,omitempty"`
}

// ChannelState is a channel's saved war state.
type ChannelState struct {
	WeekMode      int          `json:"week_mode"`
	CastleState   int          `json:"castle_state"`
	CapeWin       int          `json:"cape_win"`
	TowerGuild    int          `json:"tower_guild"`
	KefraDead     bool         `json:"kefra_dead"`
	KefraKiller   int          `json:"kefra_killer"`
	KingdomWinner int          `json:"kingdom_winner"`
	RvRBonus      int          `json:"rvr_bonus"`
	Cities        [5]CityState `json:"cities"`
}

// SaveChannelState writes a channel's state.
func (s *Store) SaveChannelState(ctx context.Context, channel int, st *ChannelState) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("channel state: %w", err)
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO channel_state (channel, state) VALUES ($1, $2)
		ON CONFLICT (channel) DO UPDATE SET state = EXCLUDED.state, updated_at = now()`, channel, raw)
	return err
}

// LoadChannelState returns a channel's state, or nil if it was never saved.
func (s *Store) LoadChannelState(ctx context.Context, channel int) (*ChannelState, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT state FROM channel_state WHERE channel = $1`, channel).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st := &ChannelState{}
	if err := json.Unmarshal(raw, st); err != nil {
		return nil, fmt.Errorf("channel %d state: %w", channel, err)
	}
	return st, nil
}
