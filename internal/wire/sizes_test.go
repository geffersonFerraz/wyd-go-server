package wire

import (
	"encoding/binary"
	"reflect"
	"testing"
)

// Expected sizeof() under MSVC x64 /Zp8, after the 64-bit widening of gold,
// experience, HP, MP, attack and defense.
func TestSizes(t *testing.T) {
	cases := []struct {
		v    any
		want int
	}{
		{Header{}, 12},
		{Item{}, 14},
		{Position{}, 4},
		{Affect{}, 8},
		{ItemDate{}, 8},
		{Status{}, 104},
		{Mob{}, 1464},
		{SubInfo{}, 448},
		{DailyQuest{}, 64},
		{Contribution{}, 52},
		{FilterDrop{}, 344},
		{CharInfo{}, 2840},
		{Daily{}, 12},
		{Account{}, 14504},
		{SealInfo{}, 52},
		{CharList{}, 1688},
		{MSVCString{}, 24},
		{GuildInfo{}, 112},

		{Signal{}, 12},
		{Parm1{}, 16},
		{Parm2{}, 20},
		{Parm3{}, 24},
		{AccountLogin{}, 120},
		{CNFAccountLogin{}, 4104},
		{CharListPacket{}, 1704},
		{CharacterLogin{}, 32},
		{CNFCharacterLogin{}, 1776},
		{CNFCharacterLoginExtra{}, 2920},
		{AdditionalAccountInfo{}, 128},
		{NewCharacter{}, 36},
		{NewArch{}, 44},
		{DeleteCharacter{}, 44},
		{SaveCharacter{}, 5888},
		{SavingQuit{}, 32},
		{ServerChange{}, 20},
		{CNFRemoveServer{}, 80},
		{NumericPassword{}, 32},
		{CreateSubCele{}, 52},
		{GuildZoneReport{}, 32},
		{ChargedGuildList{}, 212},
		{CreateGuild{}, 40},
		{AddGuild{}, 24},
		{AddSub{}, 36},
		{GuildRelation{}, 20},
		{ItemDelivery{}, 48},
		{SecurityAlert{}, 132},
		{CashDelivery{}, 72},
		{BanNotify{}, 40},
		{Notice96{}, 108},
		{MessagePanel{}, 108},
		{KefraDeath{}, 28},
		{FirstKefra{}, 16},
		{StoreSlot{}, 20},
		{StoreSetAvaible{}, 24},
		{SignalValue{}, 16},
		{SignalValue64{}, 24},
		{SealInfoReply{}, 64},
		{PutOutSeal{}, 52},
		{PanelGuildList{}, 4492},

		// client packets (TMSRV)
		{Message128{}, 140},
		{Action{}, 52},
		{Chat{}, 144},
		{Command{}, 284},
		{MoveItem{}, 20},
		{ItemUpdate{}, 32},
		{Equipment{}, 72},
		{CreateMob{}, 304},
		{UpdateScore{}, 224},
		{Etc{}, 56},
		{OpenShop{}, 40},
		{ShopList{}, 400},
		{BuyShop{}, 24},
		{SellShop{}, 20},
		{CargoCoin{}, 24},
		{Repurchase{}, 252},
		{Trade{}, 256},
		{Carry{}, 920},
		{Attack{}, 288},
		{Killed{}, 40},
		{AffectList{}, 268},
		{UseItem{}, 36},
		{PartyInvite{}, 44},
		{PartyAccept{}, 32},
		{PartyMember{}, 40},
		{PartyExit{}, 16},
		{Motion{}, 20},
		{SkillBar{}, 32},
		{Compor{}, 132},
		{Extraction{}, 20},
		{ChatColor{}, 144},
		{SetHpMp{}, 48},
		{HpChange{}, 32},
	}
	for _, c := range cases {
		if got := binary.Size(c.v); got != c.want {
			t.Errorf("sizeof(%T) = %d, want %d", c.v, got, c.want)
		}
	}
}

// offsetOf returns the byte offset of a named field as encoding/binary lays
// it out (blank padding fields included).
func offsetOf(t *testing.T, v any, field string) int {
	t.Helper()
	rt := reflect.TypeOf(v)
	off := 0
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if f.Name == field {
			return off
		}
		off += binary.Size(reflect.Zero(f.Type).Interface())
	}
	t.Fatalf("%T has no field %s", v, field)
	return -1
}

// Every 8-byte member must land on an 8-aligned offset and every 4-byte one
// on a 4-aligned offset, as MSVC would place them. These are the offsets the
// padding fields are there to produce.
func TestOffsets(t *testing.T) {
	cases := []struct {
		v     any
		field string
		want  int
	}{
		{Status{}, "Defense", 8},
		{Status{}, "MaxHP", 32},
		{Status{}, "STR", 64},
		{Mob{}, "Exp", 32},
		{Mob{}, "Last", 40},
		{Mob{}, "Gold", 24},
		{Mob{}, "BStatus", 48},
		{Mob{}, "Equip", 256},
		{Mob{}, "Inventory", 536},
		{Mob{}, "Learn", 1432},
		{Mob{}, "SkillBar1", 1448},
		{Mob{}, "MagicIncrement", 1454},
		{SubInfo{}, "Exp", 160},
		{SubInfo{}, "Info", 432},
		{CharInfo{}, "Affects", 1464},
		{CharInfo{}, "Hold", 1744},
		{CharInfo{}, "Sub", 1816},
		{CharInfo{}, "Insignias01", 2344},
		{CharInfo{}, "DailyQuest", 2360},
		{CharInfo{}, "FilterDrop", 2476},
		{CharInfo{}, "Roleta", 2832},
		{Account{}, "StorageCoin", 104},
		{Account{}, "StorageItems", 112},
		{Account{}, "Mob", 2488},
		{Account{}, "Friends", 13848},
		{Account{}, "Insignias", 14328},
		{Account{}, "Unique", 14400},
		{Account{}, "Daily", 14416},
		{Account{}, "SingleGift", 14464},
		{Account{}, "LastPenalty", 14480},
		{Account{}, "PassBlock", 14488},
		{CharList{}, "Status", 80},
		{CharList{}, "Gold", 1624},
		{CharList{}, "Exp", 1656},
		{AccountLogin{}, "Login", 24},
		{AccountLogin{}, "Unknow84", 96},
		{CNFAccountLogin{}, "CharList", 32},
		{CNFAccountLogin{}, "GoldStorage", 4072},
		{CNFAccountLogin{}, "UserName", 4080},
		{CharListPacket{}, "CharList", 16},
		{CNFCharacterLogin{}, "Mob", 16},
		{CNFCharacterLogin{}, "Affect", 1508},
		{CNFCharacterLogin{}, "Hold", 1768},
		{CNFCharacterLoginExtra{}, "Mob", 16},
		{CNFCharacterLoginExtra{}, "Cash", 2856},
		{AdditionalAccountInfo{}, "SingleGift", 64},
		{AdditionalAccountInfo{}, "PassBlock", 100},
		{SaveCharacter{}, "Mob", 16},
		{SaveCharacter{}, "Storage", 2856},
		{SaveCharacter{}, "Coin", 5208},
		{SaveCharacter{}, "User", 5236},
		{SaveCharacter{}, "Insignia", 5768},
		{SaveCharacter{}, "Unique", 5800},
		{SaveCharacter{}, "SingleGift", 5848},
		{SaveCharacter{}, "PassBlock", 5856},
		{GuildInfo{}, "Fame", 100},
		{CreateMob{}, "ItemRefine", 34},
		{CreateMob{}, "Affect", 74},
		{CreateMob{}, "GuildIndex", 138},
		{CreateMob{}, "Status", 144},
		{CreateMob{}, "SpawnType", 248},
		{CreateMob{}, "Life", 300},
		{UpdateScore{}, "Score", 16},
		{UpdateScore{}, "Guild", 186},
		{UpdateScore{}, "ReqHP", 200},
		{UpdateScore{}, "LearnedSkill", 220},
		{Etc{}, "Hold", 16},
		{Etc{}, "Exp", 24},
		{Etc{}, "Coin", 48},
		{Action{}, "Destiny", 48},
		{Trade{}, "Gold", 240},
		{Attack{}, "CurrentExp", 32},
		{Attack{}, "AttackerID", 50},
		{Attack{}, "CurrentMP", 64},
		{Attack{}, "SkillID", 72},
		{Attack{}, "Target", 80},
		{Killed{}, "Hold", 16},
		{Killed{}, "Exp", 32},
		{PartyInvite{}, "TargetID", 40},
		{PartyMember{}, "ID", 38},
		{Trade{}, "ClientID", 250},
		{Carry{}, "Gold", 912},
		{CargoCoin{}, "Gold", 16},
		{SetHpMp{}, "CurHP", 16},
		{HpChange{}, "Incress", 24},
		{HpMode{}, "Status", 24},
		{DropMob{}, "Exp", 72},
		{DropMob{}, "Carry", 88},
		{SignalValue64{}, "Value", 16},
	}
	for _, c := range cases {
		if got := offsetOf(t, c.v, c.field); got != c.want {
			t.Errorf("offsetof(%T, %s) = %d, want %d", c.v, c.field, got, c.want)
		}
	}
}

// TMSRV's copies in Common/Struct.h end before TypeBlock; the fields they do
// have must sit at the same offsets.
func TestTMSRVPrefixes(t *testing.T) {
	if got := offsetOf(t, SaveCharacter{}, "TypeBlock"); got != 5872 {
		t.Errorf("TMSRV STRUCT_SAVECHARACTER is 5872 bytes, DBSRV TypeBlock at %d", got)
	}
	if got := offsetOf(t, AdditionalAccountInfo{}, "TypeBlock"); got != 116 {
		t.Errorf("TMSRV p415 ends at 116 (+4 pad), DBSRV TypeBlock at %d", got)
	}
}
