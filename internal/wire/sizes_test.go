package wire

import (
	"encoding/binary"
	"reflect"
	"testing"
)

// Expected sizeof() under MSVC x86 /Zp8, derived field by field from
// Common/stBase.h and Dataserver/Basedef.h. STRUCT_MOB = 848 is independently
// confirmed by the size of Release Servidor/DBSRV/npc/{TK,FM,BM,HT}.
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
		{Status{}, 48},
		{Mob{}, 848},
		{SubInfo{}, 376},
		{DailyQuest{}, 64},
		{Contribution{}, 52},
		{FilterDrop{}, 200},
		{CharInfo{}, 2008},
		{Daily{}, 12},
		{Account{}, 10168},
		{SealInfo{}, 44},
		{CharList{}, 968},
		{MSVCString{}, 24},
		{GuildInfo{}, 112},

		{Signal{}, 12},
		{Parm1{}, 16},
		{Parm2{}, 20},
		{Parm3{}, 24},
		{AccountLogin{}, 120},
		{CNFAccountLogin{}, 2376},
		{CharListPacket{}, 984},
		{CharacterLogin{}, 32},
		{CNFCharacterLogin{}, 1152},
		{CNFCharacterLoginExtra{}, 2088},
		{AdditionalAccountInfo{}, 128},
		{NewCharacter{}, 36},
		{NewArch{}, 44},
		{DeleteCharacter{}, 44},
		{SaveCharacter{}, 4048},
		{SavingQuit{}, 32},
		{ServerChange{}, 20},
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
		{CreateMob{}, 248},
		{UpdateScore{}, 152},
		{Etc{}, 48},
		{OpenShop{}, 40},
		{ShopList{}, 236},
		{BuyShop{}, 24},
		{SellShop{}, 20},
		{CargoCoin{}, 16},
		{Repurchase{}, 172},
		{Trade{}, 156},
		{Carry{}, 528},
		{Attack{}, 168},
		{Killed{}, 32},
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
		{SetHpMp{}, 28},
		{HpChange{}, 20},
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
		{Status{}, "Defense", 4},
		{Status{}, "MaxHP", 16},
		{Status{}, "STR", 32},
		{Mob{}, "Exp", 32},
		{Mob{}, "Last", 40},
		{Mob{}, "BStatus", 44},
		{Mob{}, "Equip", 140},
		{Mob{}, "Inventory", 300},
		{Mob{}, "Learn", 812},
		{Mob{}, "SkillBar1", 828},
		{Mob{}, "MagicIncrement", 834},
		{SubInfo{}, "Exp", 88},
		{SubInfo{}, "Info", 360},
		{CharInfo{}, "Affects", 848},
		{CharInfo{}, "Hold", 1128},
		{CharInfo{}, "Sub", 1200},
		{CharInfo{}, "Insignias01", 1656},
		{CharInfo{}, "DailyQuest", 1672},
		{CharInfo{}, "FilterDrop", 1788},
		{CharInfo{}, "Roleta", 2000},
		{Account{}, "StorageItems", 108},
		{Account{}, "Mob", 1480},
		{Account{}, "Friends", 9512},
		{Account{}, "Insignias", 9992},
		{Account{}, "Unique", 10064},
		{Account{}, "Daily", 10080},
		{Account{}, "SingleGift", 10128},
		{Account{}, "LastPenalty", 10144},
		{Account{}, "PassBlock", 10152},
		{CharList{}, "Status", 80},
		{CharList{}, "Exp", 936},
		{AccountLogin{}, "Login", 24},
		{AccountLogin{}, "Unknow84", 96},
		{CNFAccountLogin{}, "CharList", 32},
		{CNFAccountLogin{}, "UserName", 2348},
		{CharListPacket{}, "CharList", 16},
		{CNFCharacterLogin{}, "Mob", 16},
		{CNFCharacterLogin{}, "Affect", 892},
		{CNFCharacterLoginExtra{}, "Mob", 16},
		{CNFCharacterLoginExtra{}, "Cash", 2024},
		{AdditionalAccountInfo{}, "SingleGift", 64},
		{AdditionalAccountInfo{}, "PassBlock", 100},
		{SaveCharacter{}, "Mob", 16},
		{SaveCharacter{}, "Storage", 2024},
		{SaveCharacter{}, "User", 3392},
		{SaveCharacter{}, "Insignia", 3928},
		{SaveCharacter{}, "Unique", 3960},
		{SaveCharacter{}, "SingleGift", 4008},
		{SaveCharacter{}, "PassBlock", 4016},
		{GuildInfo{}, "Fame", 100},
		{CreateMob{}, "ItemRefine", 34},
		{CreateMob{}, "Affect", 74},
		{CreateMob{}, "GuildIndex", 138},
		{CreateMob{}, "Status", 144},
		{CreateMob{}, "SpawnType", 192},
		{CreateMob{}, "Life", 244},
		{UpdateScore{}, "Guild", 126},
		{UpdateScore{}, "ReqHP", 136},
		{UpdateScore{}, "LearnedSkill", 148},
		{Etc{}, "Exp", 16},
		{Etc{}, "Coin", 40},
		{Action{}, "Destiny", 48},
		{Trade{}, "Gold", 148},
		{Attack{}, "AttackerID", 42},
		{Attack{}, "CurrentMP", 52},
		{Attack{}, "Target", 60},
		{Killed{}, "Exp", 24},
		{PartyInvite{}, "TargetID", 40},
		{PartyMember{}, "ID", 38},
		{Trade{}, "ClientID", 154},
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
	if got := offsetOf(t, SaveCharacter{}, "TypeBlock"); got != 4032 {
		t.Errorf("TMSRV STRUCT_SAVECHARACTER is 4032 bytes, DBSRV TypeBlock at %d", got)
	}
	if got := offsetOf(t, AdditionalAccountInfo{}, "TypeBlock"); got != 116 {
		t.Errorf("TMSRV p415 ends at 116 (+4 pad), DBSRV TypeBlock at %d", got)
	}
}
