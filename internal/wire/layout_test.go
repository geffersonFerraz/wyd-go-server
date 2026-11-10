package wire

import (
	"reflect"
	"testing"

	"dbsrv/internal/wire/wiretest"
)

// Every padding byte MSVC inserts must be spelled out as a blank field.
func TestNaturalAlignment(t *testing.T) {
	for _, v := range []any{Item{}, Mob{}, SubInfo{}, FilterDrop{}, CharInfo{}, Account{}, CharList{},
		CNFAccountLogin{}, CharListPacket{}, CNFCharacterLogin{}, CNFCharacterLoginExtra{}, SaveCharacter{},
		ItemDelivery{}, AdminEdit{}, AdminStats{}, LoginUnblock{}, RankUpdate{}, CharProfile{}, DropMob{}, DropList{}, FilterDropMsg{}, RollItem{},
		ItemTrackEvent{}, ItemTrackPlace{}, ItemTrackPlaces{},
		CashShopInfo{}, CashShopBuy{}, HonorShopList{}, ShopList{}, Repurchase{}, RepurchaseItem{}, Compor{},
		Trade{}, Carry{}, ItemUpdate{}, Attack{}, UpdateScore{}, CreateMob{}, Etc{}, HpMode{}} {
		if m := wiretest.Misaligned(reflect.TypeOf(v)); len(m) > 0 {
			t.Errorf("%T: %v", v, m)
		}
	}
}
