package game

import (
	"strings"
	"time"

	"dbsrv/internal/protocol"
	"dbsrv/internal/wire"
)

// The auction ("leilão", RequestLeilaoStore.cpp): a player in town opens a
// store (0x397), which leaves a store NPC where they stood, and lists
// items from their storage (0x397C) priced in donate (cash), gold or gold
// bars (4011). Everyone online can browse all stores page by page (0xAF1),
// search them by name (0xAFC), open one store (0x39A / 0xAF2) and buy from
// anywhere (0x398) while the seller is online and in town. The item goes
// from the seller's storage into the buyer's inventory; gold goes to the
// seller's storage (less the city tax from 100,000 up), bars to a storage
// slot, cash to the seller's account. A store gives 1 honor every 10
// minutes it stays open. "/fecharloja" closes it, "/hideleilao" hides
// your own listings from your searches.

const (
	opCliAuctionList   = 0xAF1  // MSG_GetLeilaoStores
	opCliAuctionCreate = 0x397  // MSG_LeilaoStore
	opCliAuctionOpen   = 0x39A  // pMsgSignal: store NPC id
	opCliAuctionAdd    = 0x397C // pAddItemLeilaoStore
	opCliAuctionOwner  = 0xAF2  // pMsgSignal: store owner id
	opCliAuctionBuy    = 0x398  // pStartNegotiation
	opCliAuctionSearch = 0xAFC  // STRUCT_BUSCA
	opCliAuctionRemove = 0xBAF2 // pMsgSignal2: category, list slot
	opSrvAuctionPage   = 0x397B
	opSrvAuctionStatus = 0x3972 // pLeilaoStatus
	opSrvAuctionHidden = 0x3973 // pHideLeilaoStatus
	opSrvStoreTimer    = 0x3F1  // seconds to the next honor point
	opSrvAuctionFunds  = 0xBF1
	opSrvStoreMob      = 0x363

	maxListings    = 48  // MAX_SLOT_LEILAO_AUTOTRADE
	maxStorageSlot = 128 // the C++ accepted 168 when listing, 128 when buying

	auctionDonate = 1
	auctionGold   = 2
	auctionBars   = 3

	nnOnlyVillage = 172
)

// Listing mirrors LeilaoStoreAux (64 bytes).
type Listing struct {
	Name     [24]byte // store name
	Price    int64    // 24
	OwnerID  int32
	SlotBank int32 // storage slot of the item
	Category int32 // 1 donate, 2 gold, 3 bars
	TaxRate  int32
	Item     wire.Item // 48
	_        [2]byte
}

// AuctionPage mirrors MSG_LeilaoStore (3096 bytes), both the client's
// "open my store" and the server's list pages.
type AuctionPage struct {
	Header      wire.Header
	TotalPages  int32
	CurrentPage int32
	_           [4]byte
	List        [maxListings]Listing // 24
}

// AuctionQuery mirrors MSG_GetLeilaoStores (24 bytes).
type AuctionQuery struct {
	Header    wire.Header
	ItemType  int32 // -1 own store, 0 all, 1..3 by currency
	Page      int32
	TotalPage int32
}

// AuctionSearch mirrors STRUCT_BUSCA (48 bytes).
type AuctionSearch struct {
	Header    wire.Header
	ItemType  int32
	Page      int32
	TotalPage int32
	Name      [24]byte
}

// AuctionAdd mirrors pAddItemLeilaoStore (72 bytes).
type AuctionAdd struct {
	Header   wire.Header
	ItemType int32
	SlotBank int32
	_        [4]byte
	Price    int64 // 24
	Name     [24]byte
	Item     wire.Item
	_        [2]byte
}

// AuctionBuy mirrors pStartNegotiation (80 bytes).
type AuctionBuy struct {
	Header   wire.Header
	ItemType int32
	SlotBank int32
	ItemSlot int32
	Price    int64 // 24
	Tax      int32
	Seller   int32
	Name     [24]byte
	Item     wire.Item
	_        [2]byte
}

// AuctionFunds mirrors st_SaldoLeilao (32 bytes).
type AuctionFunds struct {
	Header wire.Header
	Donate int32
	Gold   int64 // 16
	Bars   int32
	_      [4]byte
}

// StoreMob mirrors p363 (304 bytes): a store NPC as seen by the client.
type StoreMob struct {
	Header      wire.Header
	CurrentX    int16
	CurrentY    int16
	Index       int16
	Name        [12]byte
	ChaosPoints uint8
	CurrentKill uint8
	TotalKill   uint16
	ItemRefine  [20]uint16
	Affect      [32]wire.AffectBrief
	GuildIndex  uint16
	Unknow      uint8
	_           [3]byte
	Status      wire.Status
	Spawn       uint16
	AnctCode    [20]byte
	Tab         [26]byte
	StoreName   [27]byte
	_           [5]byte
}

// autoTrade is CMob::AutoTrade for a player.
type autoTrade struct {
	open  bool
	mob   int // the store NPC
	list  [maxListings]Listing
	timer int // AutotradeTimer
	hide  bool
}

// storeFunds is AtualizarSaldoLeilao (0xBF1): cash, gold and bars held.
func (g *Game) storeFunds(u *User) {
	pl := &g.mobs[u.id].info.Player
	g.queue(u, &AuctionFunds{Header: wire.NewHeader(opSrvAuctionFunds, uint16(u.id)),
		Donate: int32(u.account.Cash), Gold: pl.Gold, Bars: int32(inventoryAmount(pl, 4011))})
}

// closeStore is LimparInfoLeilao / ResetLeilao / RemoveCloneTrade.
func (g *Game) closeStore(u *User) {
	t := &u.store
	if t.mob >= MaxPlayer && t.mob < MaxMob && g.mobs[t.mob].storeOwner == u.id {
		g.mobs[t.mob].storeOwner = 0
		g.deleteMob(t.mob, 3)
	}
	was := t.open
	*t = autoTrade{hide: t.hide}
	if was && u.status == userPlay {
		g.signalParm(u, uint16(u.id), opSrvStoreTimer, 0)
	}
}

// pageOf builds a 0x397B page (the C++ sent a size past the struct; the
// whole struct is sent here).
func pageOf(id int, list []Listing, page, total int) AuctionPage {
	p := AuctionPage{Header: wire.NewHeader(opSrvAuctionPage, uint16(id)), TotalPages: int32(total), CurrentPage: int32(page)}
	copy(p.List[:], list)
	return p
}

// sendStoreOf is SendLeilaoStoreMyInfos / SendLeilaoStoreAlvoInfos: one
// store's whole list.
func (g *Game) sendStoreOf(u *User, owner int) {
	o := g.users[owner]
	list := make([]Listing, 0, maxListings)
	for _, l := range o.store.list {
		l.OwnerID = int32(owner)
		list = append(list, l)
	}
	p := pageOf(u.id, list, 1, 1)
	g.queue(u, &p)
}

// listings collects the open stores' listings (category 0 = all), with
// an optional lower-case name filter.
func (g *Game) listings(u *User, category int, name string) []Listing {
	var out []Listing
	for _, o := range g.users[1:] {
		if o.status != userPlay || !o.store.open {
			continue
		}
		for _, l := range o.store.list {
			if l.Item.Index == 0 || (category != 0 && int(l.Category) != category) {
				continue
			}
			if o.id == u.id && u.store.hide {
				continue
			}
			if name != "" {
				itemName := strings.ToLower(g.data.Item(l.Item.Index).Name)
				if !strings.Contains(itemName, name) {
					continue
				}
				l.Name = [24]byte{}
				copy(l.Name[:23], itemName)
			}
			l.OwnerID = int32(o.id)
			out = append(out, l)
		}
	}
	return out
}

// sendPage is SendLeilaoStoreAllInfos: one page of maxListings.
func (g *Game) sendPage(u *User, list []Listing, page int) {
	total := (len(list) + maxListings - 1) / maxListings
	if page < 1 || page > total {
		page = 1
	}
	start := (page - 1) * maxListings
	end := min(start+maxListings, len(list))
	if start > end {
		start = end
	}
	p := pageOf(u.id, list[start:end], page, total)
	g.queue(u, &p)
}

func (g *Game) auctionReady(u *User) bool {
	if u.status != userPlay || g.mobs[u.id].info.Player.Status.CurHP <= 0 {
		return false
	}
	if time.Since(u.lastAuction) < 500*time.Millisecond {
		return false
	}
	u.lastAuction = time.Now()
	return true
}

// requestAuctionList is CUser::RequestLeilaoStore (0xAF1).
func (g *Game) requestAuctionList(u *User, pkt protocol.Packet) {
	var p AuctionQuery
	if wire.Unmarshal(pkt.Raw, &p) != nil || !g.auctionReady(u) {
		return
	}
	if p.ItemType == -1 {
		if !u.store.open {
			g.clientMessage(u, "Sua loja esta fechada!")
			return
		}
		g.sendStoreOf(u, u.id)
	} else {
		g.sendPage(u, g.listings(u, int(p.ItemType), ""), int(p.Page))
	}
	g.storeFunds(u)
}

// requestAuctionSearch is CUser::RequestSearchLeilaoStore (0xAFC).
func (g *Game) requestAuctionSearch(u *User, pkt protocol.Packet) {
	var p AuctionSearch
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay || g.mobs[u.id].info.Player.Status.CurHP <= 0 {
		return
	}
	if g.locked(u, lockAutoTrade, "Bloqueio de auto troca ativo!") {
		return
	}
	name := strings.ToLower(wire.GameString(p.Name[:]))
	if p.ItemType == -1 {
		var mine []Listing
		for _, l := range u.store.list {
			if l.Item.Index == 0 {
				continue
			}
			itemName := strings.ToLower(g.data.Item(l.Item.Index).Name)
			if strings.Contains(itemName, name) {
				l.OwnerID = int32(u.id)
				l.Name = [24]byte{}
				copy(l.Name[:23], itemName)
				mine = append(mine, l)
			}
		}
		pg := pageOf(u.id, mine, 1, 1)
		g.queue(u, &pg)
		return
	}
	g.sendPage(u, g.listings(u, int(p.ItemType), name), int(p.Page))
}

// requestAuctionCreate is CUser::RequestCreateMobLeilao (0x397): open the
// store in town, named by the client (List[i].Name).
func (g *Game) requestAuctionCreate(u *User, pkt protocol.Packet) {
	var p AuctionPage
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	if g.locked(u, lockAutoTrade, "Bloqueio de auto troca ativo!") {
		return
	}
	for _, o := range g.users[1:] {
		if o.status == userPlay && o.store.open && o.mac == u.mac {
			g.clientMessage(u, "Você tem loja ativa.")
			return
		}
	}
	m := &g.mobs[u.id]
	if m.info.Player.Status.CurHP <= 0 {
		return
	}
	if u.trade.ClientID != 0 {
		g.removeTrade(u.id)
		return
	}
	g.closeStore(u)
	x, y := int(m.target.X), int(m.target.Y)
	if v := g.data.village(x, y); v < 0 || v >= 5 {
		g.clientMessage(u, "%s", g.lang(nnOnlyVillage))
		return
	}
	mob := g.createStoreMob(u, x, y)
	if mob <= 0 {
		g.clientMessage(u, "Não foi possivel abrir a loja.")
		return
	}
	name := p.List[0].Name
	name[22], name[23] = 0, 0
	for i := range u.store.list {
		u.store.list[i].Name = name
		u.store.list[i].OwnerID = int32(u.id)
	}
	u.store.open, u.store.mob, u.store.timer = true, mob, 1
	g.signalParm(u, uint16(u.id), opSrvStoreTimer, int32(g.cfg.Store.HonorEverySeconds))
	sm := g.storeMobPacket(mob)
	g.multicast2(x, y, &sm, 0)
	g.signalParm(u, wire.ClientIDSystem, opSrvAuctionStatus, 1)
	g.sendStoreOf(u, u.id)
	g.clientMessage(u, "Sua lojinha foi criada com sucesso.")
}

// createStoreMob is CriarMobLojinhaLeilaoStore: a still NPC named after
// the seller (face 230) where the seller stands.
func (g *Game) createStoreMob(u *User, x, y int) int {
	if g.data.StoreBase == nil {
		return 0
	}
	id := g.emptyNPCMob()
	if id == 0 || !g.emptyMobGrid(id, &x, &y) {
		return 0
	}
	m := &g.mobs[id]
	*m = Mob{}
	pl := &m.info.Player
	*pl = *g.data.StoreBase
	pl.Name = [16]byte{}
	copy(pl.Name[:15], g.mobs[u.id].info.Player.Name[:15])
	for i := range pl.Name {
		if pl.Name[i] == '_' || pl.Name[i] == '@' {
			pl.Name[i] = ' '
		}
	}
	pl.Equip[0].Index = 230
	pl.Equip[6], pl.Equip[7], pl.Equip[14] = wire.Item{}, wire.Item{}, wire.Item{}
	pl.Status.CON = 600
	m.npc.generateID = -1
	g.getCurrentScore(id)
	pl.Status.CurHP = pl.Status.MaxHP
	m.storeOwner = u.id
	g.placeNPC(id, x, y)
	m.mode = 1 // the C++ "locks" it: the AI never moves it
	return id
}

// storeMobPacket is GetCreateMobTrade.
func (g *Game) storeMobPacket(id int) StoreMob {
	m := &g.mobs[id]
	pl := &m.info.Player
	p := StoreMob{Header: wire.NewHeader(opSrvStoreMob, wire.ClientIDSystem),
		CurrentX: int16(m.target.X), CurrentY: int16(m.target.Y), Index: int16(id),
		Status: pl.Status, GuildIndex: pl.GuildIndex, Unknow: uint8(pl.GuildMemberType), Spawn: uint16(uint8(m.spawnType))}
	copy(p.Name[:], pl.Name[:12])
	for i, a := range m.info.Affects {
		if a.Index != 0 && a.Time > 0 {
			p.Affect[i] = wire.AffectBrief{Index: a.Index, Time: a.Index} // the C++ sent the index as the time
		}
	}
	if pl.Info&1 != 0 {
		p.Status.Merchant = p.Status.Merchant&^0x0F | 1
	}
	copy(p.Tab[:], m.tab[:])
	if o := m.storeOwner; o > 0 && o < MaxPlayer {
		copy(p.StoreName[:24], g.users[o].store.list[0].Name[:])
	}
	return p
}

// requestAuctionOpen is CUser::RequestOpenMobLeilao (0x39A): clicking a
// store NPC shows its owner's list.
func (g *Game) requestAuctionOpen(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay || g.mobs[u.id].info.Player.Status.CurHP <= 0 {
		return
	}
	if g.locked(u, lockAutoTrade, "Bloqueio de auto troca ativo!") {
		return
	}
	id := int(p.Value)
	if id < MaxPlayer || id >= MaxMob || g.mobs[id].mode == 0 {
		return
	}
	owner := g.mobs[id].storeOwner
	if owner <= 0 || owner >= MaxPlayer || g.users[owner].status != userPlay || !g.users[owner].store.open {
		return
	}
	g.storeFunds(u)
	g.sendStoreOf(u, owner)
}

// requestAuctionOwner is the 0xAF2 branch: a store by its owner's id.
func (g *Game) requestAuctionOwner(u *User, pkt protocol.Packet) {
	var p wire.SignalValue
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	if g.locked(u, lockAutoTrade, "Bloqueio de auto troca ativo!") {
		return
	}
	if o := int(p.Value); o > 0 && o < MaxPlayer && g.users[o].status == userPlay {
		g.sendStoreOf(u, o)
	}
}

// listingOf finds a storage slot already listed.
func (t *autoTrade) listingOf(slot int) int {
	for i, l := range t.list {
		if l.Item.Index != 0 && int(l.SlotBank) == slot {
			return i
		}
	}
	return -1
}

// requestAuctionAdd is CUser::RequestAddItemLeilaoStore (0x397C).
//
// Fixed: the C++ took any category number (a listing with category 4+
// was handed to the buyer for free) and let one storage slot be listed
// twice.
func (g *Game) requestAuctionAdd(u *User, pkt protocol.Packet) {
	var p AuctionAdd
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay || p.Item.Index == 0 {
		return
	}
	if g.locked(u, lockAutoTrade, "Bloqueio de auto troca ativo!") {
		return
	}
	slot, price, cat := int(p.SlotBank), p.Price, int(p.ItemType)
	// Only gold is 64-bit: donate is paid from a uint32 balance.
	maxPrice := int64(1_999_999_999)
	if cat == auctionGold {
		maxPrice = g.cfg.Limits.MaxGold
	}
	if price < 0 || price > maxPrice || slot < 0 || slot >= maxStorageSlot {
		return
	}
	stored := &u.account.StorageItems[slot]
	resend := func() { g.sendItem(u, wire.SlotStorage, slot, stored) }
	if cat == auctionBars && price > 100 {
		g.clientMessage(u, "Limite de 100 bi para cada 1 item!")
		return
	}
	// Fixed: a price of 0 bars was accepted, and the sale then handed the
	// seller a 4011 with amount 0, which counts as one bar: a bar out of
	// nothing on every sale. Bars and donate need at least 1.
	if price < 1 && (cat == auctionBars || cat == auctionDonate) {
		g.clientMessage(u, "O preço mínimo é 1.")
		resend()
		return
	}
	switch id := p.Item.Index; {
	case id == 508 || id == 509 || id == 522 || (id >= 526 && id <= 531) || id == 446 || id == 3993 || id == 3994:
		resend()
		return
	}
	if g.data.itemAbility(&p.Item, EF_NOTRADE) != 0 {
		g.clientMessage(u, "Este item não é negociavel.")
		resend()
		return
	}
	if !u.store.open {
		g.clientMessage(u, "Você precisa criar uma lojinha para anunciar itens!")
		resend()
		return
	}
	if *stored != p.Item {
		g.clientMessage(u, "O item mudou no baú. Tente de novo.")
		resend()
		return
	}
	if cat < auctionDonate || cat > auctionBars {
		g.clientMessage(u, "%s esta sem definição de valor. Processo reiniciado!", g.itemName(p.Item.Index))
		resend()
		return
	}
	if u.store.listingOf(slot) >= 0 {
		g.clientMessage(u, "Este item já está anunciado.")
		return
	}
	t := g.mobs[u.id].target
	v := g.data.village(int(t.X), int(t.Y))
	if v < 0 || v >= 5 {
		g.clientMessage(u, "%s", g.lang(nnOnlyVillage))
		resend()
		return
	}
	for i := range u.store.list {
		l := &u.store.list[i]
		if l.Item.Index != 0 {
			continue
		}
		l.OwnerID, l.TaxRate, l.Item = int32(u.id), g.data.Cities[v].PercImpost, p.Item
		l.Category, l.SlotBank, l.Price = int32(cat), int32(slot), price
		g.clientMessage(u, "Item '%s' anunciado por %d em %s.", g.itemName(p.Item.Index), price, [4]string{"NA", "donate", "gold", "barras de 1b"}[cat])
		g.sendStoreOf(u, u.id)
		return
	}
	g.clientMessage(u, "Não há vagas disponíveis no AutoTrade.")
}

// removeListing drops list slot i, moving the rest up.
func (t *autoTrade) removeListing(i int) {
	name := t.list[i].Name
	copy(t.list[i:], t.list[i+1:])
	t.list[maxListings-1] = Listing{Name: name}
}

// requestAuctionRemove is the 0xBAF2 branch: take one listing down.
func (g *Game) requestAuctionRemove(u *User, pkt protocol.Packet) {
	var p wire.Parm2
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay || !u.store.open {
		return
	}
	slot := int(p.Parm2)
	if slot < 0 || slot >= maxListings {
		return
	}
	if l := &u.store.list[slot]; l.Item.Index > 0 && l.Category == p.Parm1 {
		u.store.removeListing(slot)
		g.clientMessage(u, "Item foi retirado do leilao com sucesso !")
		g.sendStoreOf(u, u.id)
	}
}

// requestAuctionBuy is CUser::RequestBuyItemLeilaoStore (0x398).
//
// Fixed: the C++ moved the item before taking the payment (an unknown
// category gave it away), lost the seller's bars when their storage was
// full, could leave a sold listing up when the packet's category did not
// match, and let a seller buy from their own store.
func (g *Game) requestAuctionBuy(u *User, pkt protocol.Packet) {
	var p AuctionBuy
	if wire.Unmarshal(pkt.Raw, &p) != nil || u.status != userPlay {
		return
	}
	if g.locked(u, lockAutoTrade, "Bloqueio de auto troca ativo!") {
		return
	}
	me := &g.mobs[u.id].info.Player
	seller, slot, bank, price := int(p.Seller), int(p.ItemSlot), int(p.SlotBank), p.Price
	if me.Status.CurHP <= 0 || seller <= 0 || seller >= MaxPlayer || seller == u.id {
		return
	}
	s := g.users[seller]
	sp := &g.mobs[seller].info.Player
	if s.status != userPlay || sp.Status.CurHP <= 0 || !s.store.open {
		return
	}
	if u.trade.ClientID != 0 {
		g.removeTrade(u.id)
		return
	}
	if bank < 0 || bank >= maxStorageSlot {
		return
	}
	// The client sends the item's position in the page it showed, which is
	// the seller's list slot only in the seller's own store; in the "all
	// stores" and search pages it is not. The C++ took it as the list slot,
	// so buying from those pages failed silently. The listing is found by
	// its storage slot (one listing per slot) instead.
	if slot < 0 || slot >= maxListings || int(s.store.list[slot].SlotBank) != bank || s.store.list[slot].Item.Index == 0 {
		slot = s.store.listingOf(bank)
	}
	if slot < 0 {
		g.clientMessage(u, "Este item não está mais à venda.")
		g.sendStoreOf(u, seller)
		return
	}
	l := &s.store.list[slot]
	if p.Tax != l.TaxRate || price != l.Price || p.Item != l.Item || s.account.StorageItems[bank] != l.Item {
		g.clientMessage(u, "O anúncio mudou. Confira o item e o preço e tente de novo.")
		g.sendStoreOf(u, seller)
		return
	}
	cat := int(l.Category)
	if price < 1 && cat != auctionGold { // see requestAuctionAdd
		g.clientMessage(u, "Este item esta em modo demonstração")
		return
	}
	barSlot := -1
	switch cat {
	case auctionDonate:
		if int64(u.account.Cash) < int64(price) {
			g.clientMessage(u, "Não possui a quantia de cash!")
			return
		}
	case auctionGold:
		if me.Gold < price {
			g.clientMessage(u, "Não possui a quantia de gold!")
			return
		}
		if price <= 1000 {
			g.clientMessage(u, "Este item esta em modo demonstração")
			return
		}
	case auctionBars:
		for i := 0; i < invSlots; i++ {
			if me.Inventory[i].Index == 4011 && itemAmount(&me.Inventory[i]) >= int(price) && invSlotUsable(me, i) {
				barSlot = i
				break
			}
		}
		if barSlot < 0 {
			g.clientMessage(u, "Não possui a quantia de Bis(pack)!")
			return
		}
		if itemAmount(&me.Inventory[barSlot]) > 100 {
			g.clientMessage(u, "Adicione um Pack de até 100 unidades no primeiro slot.")
			return
		}
	default:
		g.clientMessage(u, "Invalido!")
		return
	}
	free := firstCarrySlot(&me.Inventory, l.Item.Index)
	if free < 0 {
		g.clientMessage(u, "%s", g.lang(nnYouHaveNoSpaceToTrade))
		return
	}
	st := g.mobs[seller].target
	v := g.data.village(int(st.X), int(st.Y))
	if v < 0 || v >= 5 {
		g.clientMessage(u, "%s", g.lang(nnOnlyVillage))
		return
	}
	net, impost := price, int64(0)
	if cat == auctionGold && price >= 100000 {
		impost = price / 100 * int64(g.data.Cities[v].PercImpost)
		net = price - impost
	}
	if cat == auctionGold && s.account.StorageCoin+net > g.cfg.Limits.MaxGold {
		g.clientMessage(u, "%s", g.lang(nnCantGetMoreThan2G))
		return
	}
	sellerBarSlot := -1
	if cat == auctionBars {
		for i := 0; i < storageVisible; i++ {
			if i != bank && s.account.StorageItems[i].Index == 0 && storageFits(s.account.StorageItems[:], i, 4011) {
				sellerBarSlot = i
				break
			}
		}
		if sellerBarSlot < 0 {
			sellerBarSlot = bank // the sold item's slot frees up
		}
	}

	// Pay.
	switch cat {
	case auctionDonate:
		u.account.Cash -= uint32(price)
		s.account.Cash += uint32(price)
	case auctionGold:
		me.Gold -= price
		g.sendCarry(u)
		s.account.StorageCoin += int64(net)
		g.data.Cities[v].Impost += int64(impost)
		g.sendCargoCoin(s)
	case auctionBars:
		bar := &me.Inventory[barSlot]
		if left := itemAmount(bar) - int(price); left <= 0 {
			*bar = wire.Item{}
		} else {
			for e := range bar.Effect {
				if bar.Effect[e].Index == EF_AMOUNT {
					bar.Effect[e].Value = uint16(left)
				}
			}
		}
		g.sendItem(u, wire.SlotInv, barSlot, bar)
	}
	// Deliver.
	item := l.Item
	ref := g.takeTrack(s, wire.SlotStorage, bank)
	me.Inventory[free] = item
	g.sendItem(u, wire.SlotInv, free, &item)
	s.account.StorageItems[bank] = wire.Item{}
	if ref != nil {
		g.giveTrack(u, ref, wire.SlotInv, free)
		g.trackEvent(wire.TrackStore, ref.id, item, g.partyOf(s), g.partyOf(u), int64(price))
	}
	if cat == auctionBars {
		pack := wire.Item{Index: 4011}
		pack.Effect[0] = wire.ItemEffect{Index: EF_AMOUNT, Value: uint16(price)}
		s.account.StorageItems[sellerBarSlot] = pack
		g.sendItem(s, wire.SlotStorage, sellerBarSlot, &pack)
	}
	g.sendItem(s, wire.SlotStorage, bank, &s.account.StorageItems[bank])
	s.store.removeListing(slot)

	itemName := g.itemName(item.Index)
	sale := wire.StoreSale{Header: wire.NewHeader(wire.OpStoreSale, uint16(u.id)), Channel: int32(g.cfg.Channel), Category: int32(cat), Price: int64(price), Tax: int64(impost), Item: item}
	copy(sale.Buyer[:15], me.Name[:15])
	copy(sale.Seller[:15], sp.Name[:15])
	g.sendDB(&sale)
	g.log.Info("auction sale", "buyer", u.id, "seller", seller, "item", item.Index, "effects", item.Effect, "price", price, "category", cat, "tax", impost)
	g.clientMessage(s, "O personagem [%s] comprou o item %s por %d", wire.CString(me.Name[:]), itemName, price)
	g.clientMessage(u, "Comprou o item %s de %s por %d", itemName, wire.CString(sp.Name[:]), price)
	g.clientMessage(s, "Vendeu o item %s de %s por %d. %d de impostos", itemName, wire.CString(me.Name[:]), price, impost)
	g.storeFunds(s)
	g.storeFunds(u)
	g.saveUser(u, 0)
	g.saveUser(s, 0)
	g.sendStoreOf(u, seller)
	g.sendStoreOf(s, seller)
}

// listedSlot reports a storage slot held by a listing (RequestMoveItem
// refused moving onto it; moving it away is refused too).
func (u *User) listedSlot(slot int) bool {
	if !u.store.open || slot < 0 || slot >= maxStorageSlot {
		return false
	}
	i := u.store.listingOf(slot)
	return i >= 0 && u.store.list[i].Item == u.account.StorageItems[slot]
}

// storeTimer is the store part of the 1 s user loop: an open store earns
// 1 honor every store.honor_every_seconds (tmsrv.yaml).
func (g *Game) storeTimer() {
	for _, u := range g.users[1:] {
		t := &u.store
		if u.status != userPlay || !t.open {
			continue
		}
		if t.timer > 0 {
			t.timer++
		}
		if t.timer >= g.cfg.Store.HonorEverySeconds {
			u.account.Honra++
			g.clientMessage(u, "Você recebeu +1 ponto de honra")
			t.timer = 1
			g.sendEtc(u)
			g.signalParm(u, uint16(u.id), opSrvStoreTimer, int32(g.cfg.Store.HonorEverySeconds))
		}
	}
}

// storeCommand handles "/fecharloja" and "/hideleilao".
func (g *Game) storeCommand(u *User, cmd string) bool {
	switch cmd {
	case "fecharloja":
		g.closeStore(u)
		g.sendStoreOf(u, u.id)
		g.clientMessage(u, "Sua lojinha foi fechada com sucesso.")
		g.signalParm(u, uint16(u.id), opSrvStoreTimer, 0)
		g.signalParm(u, wire.ClientIDSystem, opSrvAuctionStatus, 0)
	case "hideleilao":
		u.store.hide = !u.store.hide
		v := int32(0)
		if u.store.hide {
			v = 1
			g.clientMessage(u, "Seus anuncios foram ocultados!")
		} else {
			g.clientMessage(u, "Seus anuncios foram recuperados para visualizacao!")
		}
		g.signalParm(u, wire.ClientIDSystem, opSrvAuctionHidden, v)
		g.sendPage(u, g.listings(u, 0, ""), 1)
	default:
		return false
	}
	return true
}
