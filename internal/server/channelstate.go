package server

import (
	"context"
	"fmt"

	"dbsrv/internal/protocol"
	"dbsrv/internal/store"
	"dbsrv/internal/wire"
)

// handleChannelState saves a TMSRV's war and tax state.
func (s *Server) handleChannelState(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.ChannelState
	if !decode(ch, pkt, &p) || p.Header.ClientID == 0 {
		return
	}
	st := &store.ChannelState{
		WeekMode: int(p.WeekMode), CastleState: int(p.CastleState), CapeWin: int(p.CapeWin), TowerGuild: int(p.TowerGuild),
		KefraDead: p.KefraDead != 0, KefraKiller: int(p.KefraKiller), KingdomWinner: int(p.KingdomWinner), RvRBonus: int(p.RvRBonus),
	}
	for i, c := range p.Cities {
		out := store.CityState{Impost: c.Impost, Tax: int(c.Tax), Wins: int(c.Wins), Chall1: int(c.Chall1), Chall2: int(c.Chall2)}
		for _, b := range c.Bids {
			if b.Guild != 0 {
				out.Bids = append(out.Bids, store.CityBid{Guild: int(b.Guild), Value: int(b.Value)})
			}
		}
		st.Cities[i] = out
	}
	if err := s.st.SaveChannelState(ctx, int(p.Header.ClientID), st); err != nil {
		ch.log.Error("save channel state", "channel", p.Header.ClientID, "err", err)
	}
}

// handleCityOwner saves the owner a war decided. The gamedata notification
// then gives it to every channel.
func (s *Server) handleCityOwner(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.CityOwnerUpdate
	if !decode(ch, pkt, &p) {
		return
	}
	by := fmt.Sprintf("guerra (canal %d)", p.Header.ClientID)
	if err := s.st.SetCityOwner(ctx, int(p.Header.ClientID), int(p.City), int(p.Guild), by); err != nil {
		ch.log.Error("save city owner", "city", p.City, "guild", p.Guild, "err", err)
		return
	}
	ch.log.Info("city owner saved", "city", p.City, "guild", p.Guild, "by", by)
}

// handleMacBan saves a +banmac.
func (s *Server) handleMacBan(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.MacBan
	if !decode(ch, pkt, &p) {
		return
	}
	if err := s.st.SetMacBan(ctx, p.Mac, p.Type == 1); err != nil {
		ch.log.Error("save mac ban", "err", err)
	}
}

// handleGMMessage stores what a player wrote with /gm for the panel.
func (s *Server) handleGMMessage(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.GMMessage
	if !decode(ch, pkt, &p) {
		return
	}
	m := store.GMMessage{Channel: int(p.Channel), Username: wire.CString(p.Username[:]), CharName: wire.GameString(p.CharName[:]), Text: wire.GameString(p.Text[:])}
	if err := s.st.AddGMMessage(ctx, m); err != nil {
		ch.log.Error("gm message NOT stored", "account", m.Username, "char", m.CharName, "text", m.Text, "err", err)
	}
}

// handlePanelCommandResult keeps what a GM command answered.
func (s *Server) handlePanelCommandResult(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.PanelCommandResult
	if !decode(ch, pkt, &p) {
		return
	}
	if err := s.st.SetGMCommandOutput(ctx, p.Delivery, wire.GameString(p.Text[:])); err != nil {
		ch.log.Error("gm command output", "delivery", p.Delivery, "err", err)
	}
}

// deliverGMCommand hands a GM command from the panel to its channel. An
// offline channel is an answer, not something to retry later.
func (s *Server) deliverGMCommand(id int64, channel int, char, command string) string {
	if channel < 1 || channel > maxServerGroup || command == "" {
		return resInvalid + ": command"
	}
	c := s.channel(channel - 1)
	if c == nil {
		return resNotOnline
	}
	out := wire.PanelCommand{Header: wire.NewHeader(wire.OpPanelCommand, 0), Delivery: id}
	wire.PutGameString(out.CharName[:15], char)
	wire.PutGameString(out.Command[:127], command)
	c.send(&out)
	return resSentOnline
}

// handleOpenStore keeps the panel's list of open stores.
func (s *Server) handleOpenStore(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.OpenStore
	if !decode(ch, pkt, &p) {
		return
	}
	if err := s.st.TouchOpenStore(ctx, int(p.Channel), wire.GameString(p.Owner[:]), wire.GameString(p.Name[:]), int(p.Items)); err != nil {
		ch.log.Error("open store", "err", err)
	}
}

// handleStoreSale keeps the record of an auction sale.
func (s *Server) handleStoreSale(ctx context.Context, ch *channel, pkt protocol.Packet) {
	var p wire.StoreSale
	if !decode(ch, pkt, &p) {
		return
	}
	sale := store.StoreSale{Channel: int(p.Channel), Buyer: wire.GameString(p.Buyer[:]), Seller: wire.GameString(p.Seller[:]),
		Item: int(p.Item.Index), Price: p.Price, Tax: p.Tax, Category: int(p.Category)}
	for i, e := range p.Item.Effect {
		sale.Effects[i] = store.ItemEffect{Effect: int(e.Index), Value: int(e.Value)}
	}
	if err := s.st.AddStoreSale(ctx, sale); err != nil {
		ch.log.Error("store sale NOT stored", "buyer", sale.Buyer, "seller", sale.Seller, "item", sale.Item, "err", err)
	}
}
