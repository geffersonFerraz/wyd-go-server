package wire

// Packet IDs handled or sent by DBSRV (Dataserver/Basedef.h).
const (
	flagGame2Client = 0x0100
	flagDB2Game     = 0x0400
	flagGame2DB     = 0x0800

	// TMSRV -> DBSRV
	OpNewCharacter        = 2 | flagGame2DB  // 0x802 p20F
	OpAccountLogin        = 3 | flagGame2DB  // 0x803 p20D
	OpCharacterLogin      = 4 | flagGame2DB  // 0x804 p213
	OpNoNeedSave          = 5 | flagGame2DB  // 0x805 header only
	OpSaveMobQuit         = 6 | flagGame2DB  // 0x806 SaveCharacter
	OpSaveMob             = 7 | flagGame2DB  // 0x807 SaveCharacter
	OpDeleteCharacter     = 9 | flagGame2DB  // 0x809 p211
	OpNewArch             = 11 | flagGame2DB // 0x80B
	OpGuildZoneReport     = 13 | flagGame2DB // 0x80D
	OpReloadDonateList    = 0x80C
	OpUpdateSapphire      = 0x80E // also DB -> all TMSRV
	OpStaffMode           = 0x80F
	OpServerChange        = 0x814
	OpCreateSubCele       = 0x830
	OpBuyStoreSaveList    = 0x380
	OpBroadcastSetAvaible = 0x382 // also rebroadcast
	OpCreateGuild         = 0x310 // also rebroadcast
	OpAddGuild            = 0x313 // also rebroadcast
	OpAddSub              = 0x315 // also rebroadcast
	OpPanelGuildList      = 0x766 // request and reply
	OpBroadcast899        = 0x899 // opaque, rebroadcast
	OpServerNotice        = 0x908 // also rebroadcast
	OpNotifyKefraDeath    = 0x909
	OpBroadcastChat       = 0xD1D // opaque, rebroadcast
	OpGuildWar            = 0xE0E // also rebroadcast as Parm2
	OpGuildAlly           = 0xE12 // also rebroadcast as Parm2
	OpNoop0xFAA           = 0xFAA
	OpNumericPassword     = 0xFDE // request and confirm
	OpOutSeal             = 3132  // 0xC3C
	OpPutInSeal           = 3133  // 0xC3D
	OpSealInfo            = 3134  // 0xC3E request and reply

	// DBSRV -> TMSRV
	OpMessagePanel           = 1 | flagGame2Client // 0x101
	OpSavingQuit             = 10 | flagDB2Game    // 0x40A
	OpSaveQuitDone           = 0x40B
	OpAdditionalAccountInfo  = 0x415
	OpCNFAccountLogin        = 0x416
	OpCNFCharacterLogin      = 0x417
	OpCNFNewCharacter        = 0x418
	OpCNFDeleteCharacter     = 0x419
	OpNewCharFail            = 0x41D
	OpDeleteCharFail         = 0x41E
	OpAlreadyPlaying         = 0x41F
	OpDisconnectPrevious     = 0x420
	OpAccountNotFound        = 0x421
	OpWrongPassword          = 0x422
	OpServerHello            = 0x423 // Parm2: 1, Sapphire
	OpAccountBlocked         = 0x424
	OpAccountDisabled        = 0x425
	OpCNFChargedList         = 0x428
	OpReLoginForChange       = 0x52A // p20D with credentials
	OpBroadcastStoreBuy      = 0x381
	OpFirstKefraNotify       = 0x90A
	OpRebornKefra            = 0x90B
	OpCashDelivery           = 0xC0E
	OpItemDelivery           = 0xC0F
	OpBanNotify              = 0xC10
	OpAdminEdit              = 0xC11 // Go only: staff edit of a player in game
	OpItemDeliveryReturn     = 0xC12 // Go only: TMSRV could not place a 0xC0F item
	OpSecurityAlert          = 0xC13 // Go only: TMSRV -> DBSRV, shown in the admin panel
	OpPutInSealSuccess       = 3135  // 0xC3F
	OpCNFCharacterLoginExtra = 0x820
	OpNumericPasswordWrong   = 0xFDF
)
