package game

import (
	"reflect"
	"testing"

	"dbsrv/internal/wire/wiretest"
)

func TestNaturalAlignment(t *testing.T) {
	for _, v := range []any{Listing{}, AuctionPage{}, AuctionQuery{}, AuctionSearch{}, AuctionAdd{}, AuctionBuy{}, AuctionFunds{}, StoreMob{}, DailyRewardInfo{}, missionMob{}, missionItem{}, MissionInfo{}, MissionTimes{}, RollInfo{}, Duel{}, EnvEffect{}, CreateGroundItem{}, RemoveGroundItem{}, GateState{}, PickItem{}, DropGroundItem{}, DropItemAck{}, GroupList{}, GroupSearch{}, GroupInfo{}, ClientInfo{}, SummonCall{}, RecruitRequest{}, LockAccount{}, UnlockAccount{}, LockStatus{}, MerchantClick{}, AddPoint{}, DeleteItem{}, UngroupItem{}, PartyEvocation{}} {
		if m := wiretest.Misaligned(reflect.TypeOf(v)); len(m) > 0 {
			t.Errorf("%T: %v", v, m)
		}
	}
}
