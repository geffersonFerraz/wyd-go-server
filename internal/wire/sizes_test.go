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
		{Item{}, 8},
		{Position{}, 4},
		{Affect{}, 8},
		{ItemDate{}, 8},
		{Status{}, 80},
		{Mob{}, 912},
		{SubInfo{}, 416},
		{DailyQuest{}, 64},
		{Contribution{}, 52},
		{FilterDrop{}, 200},
		{CharInfo{}, 2112},
		{Daily{}, 12},
		{Account{}, 10584},
		{SealInfo{}, 44},
		{CharList{}, 1112},
		{MSVCString{}, 24},
		{GuildInfo{}, 112},

		{Signal{}, 12},
		{Parm1{}, 16},
		{Parm2{}, 20},
		{Parm3{}, 24},
		{AccountLogin{}, 120},
		{CNFAccountLogin{}, 2520},
		{CharListPacket{}, 1128},
		{CharacterLogin{}, 32},
		{CNFCharacterLogin{}, 1224},
		{CNFCharacterLoginExtra{}, 2192},
		{AdditionalAccountInfo{}, 128},
		{NewCharacter{}, 36},
		{NewArch{}, 44},
		{DeleteCharacter{}, 44},
		{SaveCharacter{}, 4152},
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
		{ItemDelivery{}, 40},
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
		{SealInfoReply{}, 56},
		{PutOutSeal{}, 52},
		{PanelGuildList{}, 4492},

		// client packets (TMSRV)
		{Message128{}, 140},
		{Action{}, 52},
		{Chat{}, 144},
		{Command{}, 284},
		{MoveItem{}, 20},
		{ItemUpdate{}, 24},
		{Equipment{}, 72},
		{CreateMob{}, 280},
		{UpdateScore{}, 200},
		{Etc{}, 56},
		{OpenShop{}, 40},
		{ShopList{}, 236},
		{BuyShop{}, 24},
		{SellShop{}, 20},
		{CargoCoin{}, 24},
		{Repurchase{}, 172},
		{Trade{}, 168},
		{Carry{}, 536},
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
		{Compor{}, 84},
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
		{Mob{}, "Equip", 208},
		{Mob{}, "Inventory", 368},
		{Mob{}, "Learn", 880},
		{Mob{}, "SkillBar1", 896},
		{Mob{}, "MagicIncrement", 902},
		{SubInfo{}, "Exp", 128},
		{SubInfo{}, "Info", 400},
		{CharInfo{}, "Affects", 912},
		{CharInfo{}, "Hold", 1192},
		{CharInfo{}, "Sub", 1264},
		{CharInfo{}, "Insignias01", 1760},
		{CharInfo{}, "DailyQuest", 1776},
		{CharInfo{}, "FilterDrop", 1892},
		{CharInfo{}, "Roleta", 2104},
		{Account{}, "StorageCoin", 104},
		{Account{}, "StorageItems", 112},
		{Account{}, "Mob", 1480},
		{Account{}, "Friends", 9928},
		{Account{}, "Insignias", 10408},
		{Account{}, "Unique", 10480},
		{Account{}, "Daily", 10496},
		{Account{}, "SingleGift", 10544},
		{Account{}, "LastPenalty", 10560},
		{Account{}, "PassBlock", 10568},
		{CharList{}, "Status", 80},
		{CharList{}, "Gold", 1048},
		{CharList{}, "Exp", 1080},
		{AccountLogin{}, "Login", 24},
		{AccountLogin{}, "Unknow84", 96},
		{CNFAccountLogin{}, "CharList", 32},
		{CNFAccountLogin{}, "GoldStorage", 2488},
		{CNFAccountLogin{}, "UserName", 2496},
		{CharListPacket{}, "CharList", 16},
		{CNFCharacterLogin{}, "Mob", 16},
		{CNFCharacterLogin{}, "Affect", 956},
		{CNFCharacterLogin{}, "Hold", 1216},
		{CNFCharacterLoginExtra{}, "Mob", 16},
		{CNFCharacterLoginExtra{}, "Cash", 2128},
		{AdditionalAccountInfo{}, "SingleGift", 64},
		{AdditionalAccountInfo{}, "PassBlock", 100},
		{SaveCharacter{}, "Mob", 16},
		{SaveCharacter{}, "Storage", 2128},
		{SaveCharacter{}, "Coin", 3472},
		{SaveCharacter{}, "User", 3500},
		{SaveCharacter{}, "Insignia", 4032},
		{SaveCharacter{}, "Unique", 4064},
		{SaveCharacter{}, "SingleGift", 4112},
		{SaveCharacter{}, "PassBlock", 4120},
		{GuildInfo{}, "Fame", 100},
		{CreateMob{}, "ItemRefine", 34},
		{CreateMob{}, "Affect", 74},
		{CreateMob{}, "GuildIndex", 138},
		{CreateMob{}, "Status", 144},
		{CreateMob{}, "SpawnType", 224},
		{CreateMob{}, "Life", 276},
		{UpdateScore{}, "Score", 16},
		{UpdateScore{}, "Guild", 162},
		{UpdateScore{}, "ReqHP", 176},
		{UpdateScore{}, "LearnedSkill", 196},
		{Etc{}, "Hold", 16},
		{Etc{}, "Exp", 24},
		{Etc{}, "Coin", 48},
		{Action{}, "Destiny", 48},
		{Trade{}, "Gold", 152},
		{Attack{}, "CurrentExp", 32},
		{Attack{}, "AttackerID", 50},
		{Attack{}, "CurrentMP", 64},
		{Attack{}, "SkillID", 72},
		{Attack{}, "Target", 80},
		{Killed{}, "Hold", 16},
		{Killed{}, "Exp", 32},
		{PartyInvite{}, "TargetID", 40},
		{PartyMember{}, "ID", 38},
		{Trade{}, "ClientID", 162},
		{Carry{}, "Gold", 528},
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
	if got := offsetOf(t, SaveCharacter{}, "TypeBlock"); got != 4136 {
		t.Errorf("TMSRV STRUCT_SAVECHARACTER is 4136 bytes, DBSRV TypeBlock at %d", got)
	}
	if got := offsetOf(t, AdditionalAccountInfo{}, "TypeBlock"); got != 116 {
		t.Errorf("TMSRV p415 ends at 116 (+4 pad), DBSRV TypeBlock at %d", got)
	}
}
