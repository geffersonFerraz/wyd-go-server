# TMSRV port notes (C++ -> Go)

Working notes for porting `Source Servidor/Projects/Gameserver` (61k lines).
Kept in the repo so the knowledge survives between sessions.

## Phases

- **A. Core** (done, `internal/game`, `cmd/tmsrv`): client net + DB link,
  login/token/char list/create/delete, world entry, view grid, movement, chat,
  item move/equip, logout, saves. Covered by `internal/game/e2e_test.go`
  (client -> tmsrv -> dbsrv -> Postgres).
  Deliberately deferred (marked `TODO(phase X)` in code):
  - GetCurrentScore / GetMobAbility / buffs (Phase C): status stays as last saved.
  - Attribute map (newbie/guild areas) and GetRoute collision (Phase B).
  - fixBoots, mount combat stats (mMont), illusion mana cost (Phase C).
  - Slash commands 0x334, guild chat commands (Phase D/E).
  - Optional login notifications (auction, daily reward, kingdom, nightmare).
  C++ bugs not carried over: RequestAction sent `sizeof(pointer)` (4 bytes)
  when echoing a blocked move; RequestDeleteChar checked the last played
  character instead of the slot being deleted; numeric-password change was
  forwarded without having typed the current one.
- **B. World** (done; tested in the real client): `worlddata.go` loads NPCGener.txt (+ `npc/`
  templates, first 848 bytes), AttributeMap.dat, Teleport.txt, Language.txt
  and the city bounds/tax of guild_zone.txt. `npc.go`: generators fire on the
  12 s minute timer (`counter % MinuteGenerate == i % MinuteGenerate`,
  generators 0,1,2,5,6,7 skipped, >= 500 = random generator range); mobs
  1000..29999; leader + followers in g_pFormation. NPCs are visible through
  the grid (p364_Mob: Name[16] trimmed, Defense = CapeInfo != 4). Movement
  recalls players out of newbie / foreign guild areas (`movement.go`).
  Teleport 0x290, shops 0x27B/0x379/0x37A, buy-back 0x3E8, gold
  storage<->inventory 0x387/0x388. Tests: `shop_test.go`,
  `worlddata_test.go`, gold transfer in `e2e_test.go`.
  Player trade 0x383 (`trade.go`, `trade_test.go`, e2e): offers relayed
  between the two players, changes to an offered item cancel, both confirm,
  then items (largest grid first into free slots) and gold swap, 0x185 to
  both, both saved. Moving, moving items, gold transfer, logout and
  disconnect close the trade. Differences from the C++: the client's 0x384
  (cancel) is handled and RemoveTrade tells the client with 0x384 (the C++
  ignored the packet and only cleared server state); invalid trade gold
  stops the trade (the C++ cancelled and carried on).
  Still open: PvP-mode (0x399) trade block; NPCs do not move yet (mob AI, Phase C);
  NPC score is bStatus until GetCurrentScore (Phase C); "Honra"/RvR shops,
  the custom event items in RequestBuyShop, buy/sell blocks, Kefra hall /
  RvR / valley special teleports beyond the basic checks (Phase D/E).
  C++ bugs not carried over: GetEnemyFromView skipped the mob's whole row
  and column, so players in line with a mob were never aggroed (now only
  its own cell is skipped); RequestTeleport charged the fee before refusing
  the Kefra hall without entries; RequestBuyShop sold from any mob id
  (now only merchants).
- **C. Combat** (in progress):
  - Slice 1 (done): `score.go` = GetCurrentScore/GetCurScore (items, mount
    table mMont, affects, class passives, Arch quest bonuses, soul, PK
    penalty, caps) + GetStatusBaseScore/GetScorePoint/SkillPoint/
    MasterPoint (Mortal/Arch). Checked against 46 real characters saved by
    the C++ (`score_real_test.go`, `SCORE_REAL_DSN`): 44 identical, the
    other 2 were saved before a buff was applied. NPCs get their score on
    spawn. `combat.go`: physical attack 0x367/0x39D/0x39E on NPCs
    (distance, double/critical via g_pHitRate, GetDamage, Hunter extra hit,
    elixirs, force damage, caps, HP items), SetBattle, relay to viewers.
    `kill.go`: MobKilled for NPCs (GetExpApply, single-member share, hold),
    p338, DeleteMob with generator count, CheckGetLevel. skilldata.csv and
    the g_pNextLevel table (`exptable.go`, generated from Basedef.h).
  - Slice 2 (done): mob AI in `ai.go`, run from the 500 ms sec timer like
    ProcessSecTimer (idle mobs id%6, fighting mobs id%4): generator routes
    (SetSegment route types 0-4/6, waits, random wander), GetRoute over the
    height map (the C++ `i < distance || i < 23` always allows 23 steps;
    kept), aggro by GetEnemyFromView + g_pClanTable, group wake-up,
    SelectTargetFromEnemyList, chase (GetTargetPos), keep-away
    (GetTargetPosDistance), line of sight (GetHitPosition), give up beyond
    30 cells of the segment, GetAttack (mastery motions, skill-bar resist,
    self/leader heal), GetParryRate, damage/HP items, relay p39D. Player
    death by mobs (p338; DeadPoint is 0 so no exp loss), resurrection 0x289
    (2 HP + recall), RegenMob HP/MP every 16 ticks (village x5, guild x10).
  - Slice 3 (done): drops (`drops.go`). The C++ MobKilled still walks the
    mob inventory with the gameconfig.txt rates but its drop call was
    removed; real drops come from `controle drop/Controle.txt`, now the
    gamedata.drops / drop_items tables (names keep trailing spaces, which
    tell monsters apart; the one exact duplicate block, "Mulher Kalintz",
    was dropped like the C++ ignored it)
    (HandleMobDrop): per player/mob/item kill counters, "every N kills"
    (reduced by fairies 3901-3915 and drop bonus), MaxDrops per kill,
    GoldDrop added to the killer (Go caps at 2 billion; the C++ did not),
    straight into the inventory with the C++ stacking rules (AgroupItem /
    AgroupItemFada, GetMaxAmountItem) and the drop filter. Counters reset
    when the character enters the world (the C++ kept them per slot id
    forever). No items are ever placed on the ground by mobs.
    gameconfig.txt note: its "Bônus experiência" and "Rates Refinação"
    headers never matched in the C++ (mis-encoded literals), so those
    values were never applied; the drop-rate table only fed the dead loop.
  - Slice 4 (done): skills (`skillcast.go`). The table is gamedata.skills
    (imported from `skilldata.csv`). Checks: class, learned bit
    (the C++ computed it but never checked: unlearned skills are refused
    now), per-skill cooldown (SkillData.Delay, -1 s with SkillDelay),
    97-102 learned bits, gold for 85, mana (GetManaSpent; 31 = all).
    Hide/Possuido removed on cast. Per target: Maxtarget, PartyCheck,
    range. Instance types 1-5 (GetSkillDamage_PvM + GetSkillDamage_2,
    holy 10% / elemental resist, IgnoreResistance, falloff for 2 and 4),
    6 heal (+class bonus), 7 calm mob, 8 cleanse, 10 taunt, 12 ethereal
    flames; 22 Exterminar damage; 47 cancel; 99 self resurrection (50%
    in place with 10% HP, else 2 HP in town). Buffs/debuffs: SetAffect
    (players only, slots < 24) and SetTick (mobs too), resist/immunity
    rules, SendEquip for transformations. ProcessAffect: timers (players
    every 16 ticks with regen, idle mobs every idle turn, fighting mobs
    every other turn), aura of life (17), poison (20), end of
    transformation (16). Mob skills from the skill bar now apply their
    affects; SendAffect 0x3B9.
    Not yet: PvP skill damage, summon player (9), evocations (11),
    knockback (6), Exterminar movement, 79/30/41/98/102 specials,
    resurrect other (31), auras 22/23, familiar auto-attack, date buffs.
  - Slice 5 (done): use item 0x373 (`useitem.go`), common checks (dead,
    slot types, trade closed). Potions (volatile 1): add to the pending
    HP/MP (Potion.CountHp/Mp, + PotionBonus), bQuaff; the sec timer then
    moves HP/MP toward it by level + 100 (+400 Celestial) once per quaff,
    as ApplyHp/ApplyMp. Affect 32 blocks potions. SendSetHpMp now sends
    the pending values in the max fields and runs SetReqHp/Mp. Also:
    healing herb (243), antidote / Kappa potions (10, affect 4), return
    scroll (11), star gem (12, saves the warp point) and teleport scroll
    (13). Every other item is given back with a message (the C++ would
    have consumed it into its 8000-line handler) and logged.
  - Slice 6 (done): parties (`party.go`): invite 0x37F (forwarded with
    partyId 1 / unk 204), accept 0x3AB, leave/kick 0x37E, party window
    0x37D (HP as %) and RemoveParty (member leaves; leader leaves -> first
    member leads, or the party ends under 2 members). Logout/disconnect
    leaves the party. Fixed C++ holes: invites trusted the client's
    leaderId, accept worked without an invite, a leader could kick anyone.
    Exp share (`shareExp`): members within half a view of the killer,
    weighted by level + 30, PARTYBONUS + g_EmptyMob - 100 (so each member
    usually gets the full base exp + 30), dead members skipped, Nightmare
    rooms ignore distance; members also get SendEtc (the C++ only updated
    the killer). The custom group finder is slice 12. Not ported: party
    HP bar refreshes.
    Review (2026-11-06): GetExpApply_2 is now complete (`expbonus.go`):
    the zone/evolution table ("Controlador EXP": ControleExp.txt gives the
    desert values, bonusmortal.txt the Água/Pesadelo ones - as in the C++,
    Água/Pesa keys written in ControleExp.txt are ignored), Mortals outside
    the exp zones get a flat 1000 and Archs/Celestials/Sub-Celestials 20, the
    killer's experience chest (affect 39) doubles and its fairy adds
    16/18/32%. CheckQuarter (HP/MP refill at 1/4, 2/4, 3/4 of a level) was
    missing. Party join: members after the first never got their own line
    in the party window (fixed). Leaving sends SendPartyEvocation(id, 0),
    and going back to the character list closes the group finder windows
    (0x99A/0x99C) and drops the listing, as in the C++.
    Fixed: when the last member leaves, the party ends (the C++ kept the
    leader in a party of one); a zone cut above 100% no longer takes exp
    away. The C++ "negative exp" on level up comes from 0x338 carrying the
    killer's exp from before the reward while CheckGetLevel's 0x337 already
    sent the new one; the Go server sends the exp after the reward.
    Fixed (client check against Source Cliente): the client reads 0x37E as
    MSG_STANDARDPARM (int Parm), but PartyExit was 14 bytes, so the next
    packet's bytes leaked into Parm and "0 = clear the party" did not clear
    the leaver's window. Packets are now padded to the C++ sizeof
    (PacketHeader aligns to 4): PartyExit 16, PartyAccept 32, PartyInvite
    44, GroupInfo 36, LockAccount 32, UnlockAccount 24 (no other Header
    struct has a size that is not a multiple of 4). The client also sends
    the whisper command "evoc2" with every invite; it now just refreshes
    the evocation list instead of answering "not connected".
    Fixed (party exp display): the client takes 0x338's Exp as its own
    when the killer is itself or a party member (OnPacketCNFMobKill ->
    SetMyHumanExp shows "Exp +N" from the difference). The C++ multicast
    the killer's exp to everyone, so members saw the killer's exp as
    theirs (huge gains, then huge negative ones - the "xp negativa").
    `multicastKill` gives each party member in view its own exp and hold
    (mob and PvP kills); 0x337 goes only to members out of view, so the
    gain still shows. Changed from the C++ (by request): the party's base
    exp is GetExpApply for each member (its own level and exp bonus), not
    the killer's, so a capped killer (level 400) no longer gives the party
    only the +30 share.
  - Slice 7 (done): PvP (`pvp.go` + `combat.go`). PK mode 0x399 (blocks
    trading; like the C++, attacks do not check it, only the map: no damage
    if either side is in a village or outside a PvP area). Physical hits on
    players: crit x1.3-1.4, defense x3, a quarter of the damage (Hunter
    extra hit kept whole), parry (GetParryRate), reflect, mount absorption,
    halved in 1664-1791 x 1537-1920, mana shield (affect 18), lifesteal
    (vampirism for magic, no /4 on players), TK aura 17, BM thorns
    reflect (can kill the attacker). Skills: GetSkillDamage_PvP, defense
    x3, holy uses the player's holy resist / 4, skill 79 defense x2;
    healing a guilty player makes you guilty. Guilty mark (8) on attackers
    outside arenas, villages and war zones, wearing off one per regen
    tick. Player kills: exp loss by level span (x5/x3 for low CP, /6,
    frag bonus, caps, safe areas), de-level, frags, CP -3 for killing a
    non-guilty player (x3 with capes 548/549). Fixed: the war zone check
    ignored max Y. Not ported: the dead "hold" exp debt, g_pPositionCP
    CP-free areas, Kefra/castle/tower-war special cases, mount HP loss.
  - Slice 8 (done): summons (`summon.go`). GenerateSummon: an NPCBase
    creature in a free NPC slot next to the owner (CapeInfo 4, route
    type 5, Summoner/Leader = owner, listed in Mob.evoca = EvocaList[36],
    name + "^"); evocations get pSummonBonus from INT x1.15 and
    BeastMaster mastery + 50 (+10.5% / +50 with the 8th skill) and live
    30 ticks (128 for 28..37) through affect 24 (0xEE1 tells the client);
    baby mounts get MountCon / EF_MOUNTSANC and the item's HP. Party
    window 0x3EA. Evocation skills 56..63 (instance type 11): count by
    mastery, only the missing ones, one kind at a time, not in the two
    blocked areas. MountProcess on entering the world, moving the mount
    slot, hatching, catalysts, feeding and revive. AI: route type 5
    follows the owner (walk beyond 2 cells, teleport from 20), vanishes
    when the owner is gone / left the party / time ran out; in battle it
    teleports back from 20 cells. Summons join their owner's (and the
    party leader's) fights: the player's attacks, mobs that find or hit
    the player, and fight back. A summon's kill counts for its owner
    (exp, drops) and trains a baby mount (+1 level every 25..100 kills,
    up to 100). LinkMountHp keeps the baby's HP on the item;
    ProcessAdultMount: adult mounts lose the HP they absorb (mob and PvP
    hits), die at 0, affect 49 protects. Summons are dismissed at logout.
    Differences: the C++ re-summoned the mount on every item move (now
    only moves of slot 14), summoned dead mounts (deleted a tick later),
    kept a summon whose owner left fighting forever (battle returned
    "keep away" instead of delete), and cleaned EvocaList lazily.
    Not yet: mount hunger in the regen timer (phase D), skill 9 (summon
    player), the group-finder "evocation allowed" flag.
  - Slice 9 (done): Celestial progression and mount upkeep
    (`celestial.go`). GetScorePoint/GetSkillPoint for Celestial and
    Sub-Celestial (creation level bits 0-4, Arch crystals, resets x200,
    the other character's level; a Sub-Celestial copies those bits from
    Sub.Info). Resets: item 3020 on a Sub-Celestial with the Arcana at
    199+ (4,000,000,000 exp, 1000 fame) sets Reset_1..3 on both
    characters, takes 40,000,000 exp (levels may drop) and up to 5 gold
    bars (the C++ check for the bars is commented out). CheckGetLevel:
    with three resets 199 and 209 block until unlocked; without them the
    exp past 199 is dropped (for every evolution, as the C++); guild cape
    at 220; Sub-Celestial Cythera bonuses at 120/150/180/190-199. Login
    and the Pedra Misteriosa re-check the 199 block. Pedra Ideal
    (volatile 211): the level 200 unlock (both characters at 199, Selo
    Real 4683, 1000 fame), Arch 355+ -> Celestial (level 400 mortal,
    kingdom, no Cythera; Cythera 3500-3502 by Arch level, cape
    3197-3199, back to level 1, relog) and Celestial 120+ with a
    sephirot -> Sub-Celestial (DBSRV 0x830). RegenMob: every 450 turns
    eggs incubate one step and living mounts lose 2 (4 for the big ones)
    feed, starving at 1 unless affect 49 is on.
    Fixed: item 3020's reset searched a "Selo Real" with GetFirstSlot(3020)
    and used up two stones of a stack; the Pedra Ideal used up two stones
    for a Sub-Celestial; a BeastMaster Celestial got the FoeMaster's base
    INT/DEX/CON; the 220 guild cape was sent as item slot 14.
    Real characters (`SCORE_REAL_DSN`): 43 of 46 match; the 3 that differ
    are Mortal/Arch attack/HP/MP (saved with a buff or debuff on).
- **D. Items/special NPCs** (in progress): RequestUseItem, compounders,
  seals, guilds.
  - Slice 1 (done): refining (`refine.go`, volatile 4 Oriharucon / 5
    Lactolerium): Rate[dust][EF_UNKNOW1 row][sanc] (Arch row for
    EF_MOBTYPE 1), caps +6 / +9, SetItemSanc (failure count in the tens,
    +10.. as 230 + 4n keeping the gem bonus), equipped +10 -> +11 at 4%,
    Celestial items to +15 (RateCele, failure -1), Cythera 3508, items
    762-768/788-791 at +9/+10 (announced), earrings 591-595 from +9 with
    the Emblema da Proteção 4611, Lac core 4521, egg hatching in the mount
    slot, sealed stones 1752-1759, 3397-3406 upgrade. Emote 0x36A
    (RequestMotion relay, SendEmotion). Kept from the C++: the failure
    bonus Fail[sanc] x fails is also stored as the new failure count, so
    an item that never failed keeps 0. Fixed: 3993/3994 got the "can't
    refine" message and were refined anyway; the Celestial check read
    Equip[DstSlot] even for inventory items.
    Other items: 6 Pílula do Poder Mágico (quest bit, wasted when already
    taken, like the C++), 7 Poeira da Fada (+300M exp), 8 Olho Crescente
    (+3000 exp), 9 pedras LE 575-579 (50% to ItemList.Extreme; fixed: the
    C++ mapped Unique 10/20/30/40 to stone 3, so 579 never worked), 15
    mount food (+5000 HP up to npc_base max, +2 feed; `npc_base/` is now
    loaded as Data.NPCBase). 14 and 17 are empty in the C++ (item kept).
    Not yet: mount re-summon after hatching/feeding (MountProcess), the
    water scrolls (21-30/131-140/161-170) and the per-item handlers
    (Nightmare, boxes, quests, ...).
  - Slice 2 (done): compounders (`compounder.go`), all on the pCompor
    window (84 bytes: 8 items + 8 slots): Anct 0x3A6 (GetMatchCombine,
    Escritura 4529, +7 Ancient, notices to all channels via 0x908 and
    the colored 0xD1D line), Lindy 0x2C3 (Arch 354/369 unlock + cape),
    Shany 0x2C4 (Pedra Espiritual [F]), Odin 0x2D2 (+11..+15, extraction
    on break, Celestial 40 unlock, Fúria, cape refine, runes, Kersef,
    secret stones), Ehre 0x2D3 ([F] bonuses, blessed refining, Ímago,
    Pedra Misteriosa, mount costumes, 4860, earring dust, 4683, soul
    limit), Agatha 0x3BA, Aylin 0x3B5, Tiny 0x3C0, Alchemy 0x2E1,
    0x2FD1 and Extraction 0x2D4. Every place is checked against the
    inventory (`readCompor`), the trade is closed, the character saved.
    Level blocks (`checkGetLevel`, `expApply2`): Arch 354/369 (the C++
    tests bloqueia354/369, which nothing sets, so every level-up into
    them blocks) and Celestial 39/89; a blocked character (or a blocked
    killer) gets no exp. Fixed C++ holes: Shany never checked its 4
    Lactolerium against the inventory; Ehre's [F] and earring recipes
    accepted empty places (wiping "Inventory[-1]") and the earring check
    let any item through; the soul recipe said "incorrect" after the
    last combination succeeded; Extraction's failure cleared 4 bytes of
    the item; Odin's ReturnChance read past its table for items with a
    failure count; Odin's Kersef did not update the client's gold.
    Not yet: the 199/209 blocks of the reset system, LevelItem, the
    guild cape at 219.
  - Slice 3 (done): soul seals (`seal.go`): using an empty Selo da Alma
    (3443) seals an Arch+ character (bare, no gold, not a guild leader,
    blue/red capes become neutral) -> PutInSeal 3133; DBSRV answers
    PutInSealSuccess (the in-memory character is wiped so no later save
    brings it back) and delivers the filled seal into storage (0xC0F,
    saved at the character list). 0x3CC opens a filled seal from level 280
    as a new character (OutSeal 3132, 0x3B4 with the new slot), 0x2CD
    shows a seal (5-minute cache, client 0xDC3). The C++ closed the
    connection when asked about a seal that does not exist; no answer now.
    e2e: TestSealFlow (seal, storage, open from another character).
  - Slice 4 (done): guilds (`guild.go`, `command.go`). The guild table
    (g_pGuild, alliances, wars) is read from Postgres at startup
    (`cmd/tmsrv` loadGuilds -> SetGuilds; the C++ read ..\DBsrv\guilds.txt)
    and kept current by the DBSRV rebroadcasts (0x310, 0x313, 0x315,
    0xE12, 0xE0E). Slash commands 0x334: /criar|/create (100M or ticket
    4614), /subcreate, /expulsar, /handover, /fimirma, /guild,
    /guildfame, /kingdom, /cp, /nt, /time, /day, the town teleports
    (/armia ...), and the chats: "@" kingdom, "@@" citizen (5 s limit),
    "=" party, "-" guild, "--" guild + ally, whispers ("/nick text",
    "/r"). The other commands are slice 12. Recruit 0x3D5 /
    accept 0x681, kick 0x28C, alliance 0xE12 (DoAlly, guild notices),
    war info 0x3A8 on entry, guild list on the "guild" chat word, kingdom
    cape fix-up on entry. Fixed: recruiting put the player in the guild
    and charged at once (no consent; the accept then always failed), a
    sub-leader could kick the leader, kicks and alliances did not check
    the target was online / the leader's own guild, empty guild names
    were accepted, /subcreate refreshed the view at (X, X).
    Known: two channels creating a guild in the same second can pick the
    same id (DBSRV keeps the first; the same race existed in the C++).
    Not yet: guild war declaration 0xED7 (the C++ only took the item),
    /summonguild, city taxes (/guildtax), war time (castle/tower).
    e2e: TestGuildCreatePersists.
  - Slice 5 (done): item-specific use, first batch (`useitem_items.go`):
    colored refines 3407-3416 / remover 3417 (volatile 186), Replations
    4016-4020 (SetItemBonus), Bênção de Ímago 3250, Mythril bars
    (volatile 235-238), Bolsa do Andarilho 3467 (30 days), Caixa de Jóia
    4528, Pedra da Fúria 3020 (Cythera Arcana at 199, Celestial 90
    unlock), hunting scrolls 3432-3437, Pergaminho do Perdão 3343,
    fireworks 1728, exp chests 4140/4548/4549 (affect 39), chocolates
    1739/4145, escrituras 3445/3446, frango / poções da coragem
    (affect 30), Sephira books 667-671, date buffs (Revigorante
    3210-3213, Divina 3379-3381, Sephira 3361-3363, Saúde 3364-3366),
    Escritura de Pesadelo 5137 and gold bars 4010/4011/4026-4029. Date
    buffs: SendAffect and the affect tick keep them at 999 until their
    date and the client gets the days left (as SendAffect did). Fixed:
    a Replation lost a refine held in effect 1 or 2 (now moved to effect
    0); the Mythril bar could raise an empty effect slot and crashed on
    a missing target; the Revigorante was re-applied as affect 51.
    Not yet: the Sub-Celestial resets of 3020, PAC items (210), Pedra
    Ideal (211) and the Nightmare tickets.
  - Slice 6 (done): item-specific use, second batch (`useitem_items2.go`,
    RequestUseItem.cpp 3569-4999): Gemas Anct 3386-3389, stat resets
    3220/3265, Lactolerium 100 4141, quest exp boxes 4117-4121, Pedra
    Misteriosa 4148 (Celestial <-> Sub-Celestial swap, sends the skill bar
    0x378), Poção do Vigor 3313, jewels 3200-3209 (affect 8), Paclac /
    Pacori 3447/3448, Selo do Guerreiro 4146, Moedas de Honra 3288-3290,
    Cristais Arch 4106-4109, Acelerador de Nascimento 3438, catalysts
    3344-3350 and restorers 3351-3357. Fixed: the Gema read Equip[DstSlot]
    before checking the slot type (out of bounds); the Vigor potion added
    50 base defense for good on every use and could take the Saúde
    affect; the Selo do Guerreiro reset a worn white cape to +0; quest
    boxes used up the whole stack even when the level range ended after a
    few (now only the boxes needed); the Acelerador sent Equip[14]
    whatever slot the egg was in; catalysts wrote Equip[14] whatever the
    target was (a baby in the inventory became a second adult mount), now
    mount slot only; restorers resent the wrong slot on refusal. Skipped:
    the masmorra tickets 4111-4113 (need the ReCaptcha quest system,
    phase E); the 199 block after 4148 (no reset system yet).
  - Slice 7 (done): item-specific use, third batch (`useitem_items3.go`,
    RequestUseItem.cpp 5002 to the end): extractions 3021-3026, Compostos
    do Equilíbrio 4126 / de Chance 4124, Molar de Gárgula 4122, cash coins
    4533-4535 (balance sent as 0x3B1), fairy flashes 4688/3451/3452, Baú
    Mercantil 4545, Troca de Corpo 4649 and Troca de Classe 3486 (back to
    the list and 0x3B4 into the same slot), mount suit remover 4595,
    Poções Gelada 4624 / Annubis 4668 / Panqueca 4639, reward boxes and
    books (4541, 4623, 4647, 4657, 4667, 4715, 4724, 4633, 4631, 4632,
    4634, eggs 4615/4642, runes 4665, secretas 4676) through one helper
    (`giveBox`), replations 4686/4687 and 4610, Passe de Missões
    4616-4618/4621, Compactador de Gold 4572, Pó de Brinco 4719, Jóia da
    Alma 3214 (resets skill 102, 0x987), Ímago Abençoado 3230-3244,
    Fumaça Cósmica 792 and Acelerador de Escritura 4682. Fixed: boxes used
    from the storage wrote the prize over Inventory[SrcSlot]; the Bone
    Demon book could write its second 3338 to Inventory[-1]; the Baú
    Mercantil and the Compactador took the key/gold and the box with no
    room for the prize; the compounds hid the item on refusal; the
    Imp/Griupan replation stored the refine level as the raw value (+10
    and up broke); the escritura accelerator did the date math by hand
    (dias_mes[0] in January); the extraction could feed the failure count
    of a +0 item into SetItemSanc. Not ported: Recusa de Guerra 4031
    (guild war), duel letters 3171/3172/1731 (event system), the masmorra
    tickets 4111-4113, SendMissionInfo and SendClientPacket (client info
    window).
    Note: many of these ids exist only in the C++ code, which came from a
    newer server than this item list. None of ItemList.csv,
    TMSRV/ItemList.bin (same content) or the client's Itemlist.bin has
    4572, 4595, 4610, 4615-4618, 4621, 4623, 4624, 4631-4634, 4639, 4642,
    4647, 4649, 4657, 4665, 4667, 4668, 4676, 4682, 4686-4688, 4715, 4719,
    4724, 3244, 3289 or 3290, so they cannot drop or be delivered; the
    handlers stay ready for when the items are added (gamedata + client).
    The client list (2023) is also older than the server's: it lacks
    4533-4535, 4545/4546, 3214, 3220, 3231-3243, 3486 and 4541, and names
    3265/3288 that the server does not have.
  - Slice 8 (done): character upkeep (`points.go`): 0x277 AddPoint
    (status points, 100 at a time above 200; mastery capped at
    (level+1)*3/2 below Celestial and at 200, or 255 with that tree's 8th
    skill; mode 2 learns a skill from its book 5000-5095: class, skill
    points, level unless Celestial+, mastery INT/DEX/CON of the book, 8th
    skill needs the whole tree, one 8th per character, 5M/10M/50M gold).
    Points are derived by GetCurrentScore, so spending just moves base
    stats. 0x378 skill bar (unlearned skills become 248, special-skill
    bits wrap at 32 like the x86 shift), 0x2E4 delete in the bin (509
    refused), 0x2E5 stack split (fixed: the C++ duplicated single items
    without EF_AMOUNT and left empty stacks), 0x291 home city.
    Ground items (`ground.go`, `groundmask.go`): InitItem.csv is loaded
    (rows take ground ids 1.. in order; the door helpers rely on that:
    coliseum 13-19, castle 33-36, Zakum 24), keyed gates start locked,
    guild towers 3145+win_count, then SetCastleDoor(1)/SetColoseumDoor(1)/
    SetColoseumDoor2(3). UpdateItem/BASE_UpdateItem3 raise/lower the
    HeightMap with g_pGroundMask (+18), so GetRoute and mobs respect
    closed gates. 0x374 open gate (key EF_KEYID consumed, 773 doors),
    minute timer re-locks keyed gates on the second tick, 0x26E/0x16F
    sent on SendGridMob and when moving across the view, ProcessDecayItem
    (8 slots per sec tick, marked then removed). 0x270 pick (3 cells,
    470 Pílula quest, 490-500 vanish), 0x272 drop (GMs only, as the C++).
    Fixed: gates and towers could be picked up; RequestOpenGate acted on
    empty slots; the weapon swap after dropping Equip[6] sent the wrong
    packet; masks 6-9 read past the table. Kept: UpdateItem "moves" mobs
    under a closing gate to the cell they already stand on.
  - Slice 9 (done): quest NPCs, RequestMerchantNPC 0x28B (`merchant.go`,
    `merchant_quests.go`, `npcevent.go`). Dispatch by the NPC's Info byte
    (LOCAL_526), face and level, within half a view. Event NPCs
    (Status.Level 500-509) from `Data/NPCEvento.txt` (items with event id
    41, gold, Rand()%300 tiers, 3214 = 500k gold, teleport/messages);
    frags -> fame (Info 4 face 271); mount master (Info 26); Kibita
    (21h weekday buff, Arch soul cape); Griupan (Info 62); seven crystals
    weapon enchant (Info 4/58, SetItemBonus(item, level, sanc, bonus)
    ported as setItemBonusLevel); Mystical amulet quest (10/11); Jeffi
    (dusts, Divine Circle); Shama (tree reset); kingdom kings (capes for
    sapphires with the 0x80E price swing, emblem 4081, Arch creation via
    DBSRV 0x80B, Pedra Ideal); Sephirot (19/40); type 100 NPCs by level:
    newbie quest entrances 5-9, Perzen, Juli, green cape 20/21,
    Sobrevivente (hall entries), Molar, event tp 50, Guarda Real 35,
    trainers 40-43, Carbuncle 44-48, Urnammu, Amelia, Equilíbrio,
    kingdom broker 374, Jester, Kruno, Unicórnio 55-58, Oráculo 59,
    BigCubo 64, Mestre das Habilidades 130 (`stat_sapphire` in
    tmsrv.yaml, 30 like gameconfig.txt). CAPE_WHITE is 0 in the C++.
    Fixed: a stack of sapphire packs paid only 10 (one pack per slot);
    NPCEvento took the items and lost the prize with a full inventory;
    the guild medal NPC took 10 sapphires for nothing (upgrade commented
    out); Kruno never took the Pedaço de Chance; the Oráculo and Amelia
    lost the reward with a full inventory.
    Not ported (TODO(phase E)): Quests\*.c NPCs (levels 700-750; the
    server ships only the README, so the C++ gave nothing), Odin arena
    (level 63), Zakum castle (Info 30), Uxmal Pista de Runas (33), zombie
    dungeon (60), Yugi (62); MountProcess after a revive (summons).
  - Slice 10 (done): account lock and arena duels. Lock (`lock.go`,
    RequestBlockFunc.cpp): 0xF31 sets an 8-character password and eight
    switches once, 0xF30 removes it with the password, 0xF30 status on
    entering the world. Stored like the C++ in
    `<TMSRV>/Cache/LockAccount/<LOGIN>.bin` (18 bytes), so both servers
    share it. Switches enforced: equipment moves and item use (except
    volatile 2/15), trade offers, NPC buy, NPC sell, storage moves, the
    five compounders of PacketControl (Anct, Lindy, Shany, Odin, Ehre);
    auto-trade and premium shop are stored for when those systems come.
    0x903 only answers, like the C++. Fixed: unlocking left the file, so
    the lock came back at the next login; the C++ logged the passwords.
    Duels (`duel.go`, RequestDuel.cpp / DoRanking / ProcessRanking): 0x39F
    invite (type 0 1x1, 1 5x5 and 2 10x10 guilds from the lobby
    2564-2579 x 1688-1714, 3 guild leaders always refused), 2 s between
    invites, whisper-off refuses; type 4 accepts (DoRanking(type,
    acceptor, inviter)). One battle per channel: teleports to 147/189 x
    4045 with the 0x3A1 timer, every 2 s a turn; every 5 turns a side
    with nobody alive in the arena ends it; below 180/120/60 turns the
    arena edges hurt (SendDamage: to 25% HP, at most -2000) with 0x3A2
    effects; the result goes to the arena and the lobby, everyone is sent
    to 2572x1752 (the dead with 1 HP).
  - Slice 11 (done): the auction ("leilão", `auction.go`,
    RequestLeilaoStore.cpp). 0x397 opens a store in town (one per
    computer, named by the client): a still store NPC (npc/Base, face
    230) is left where the seller stands and shows as 0x363 (StoreName).
    0x397C lists a storage item (slot < 128, the 8-byte item must match)
    priced in donate (cash), gold or gold bars (up to 100); medals and
    no-trade items refused. 0xAF1 lists all open stores by currency, 48
    per page (-1 = own store), 0xAFC searches by item name, 0x39A (store
    NPC) / 0xAF2 (owner id) open one store, 0xBAF2 takes a listing down,
    0x398 buys from anywhere while the seller is online and in town: the
    item goes from the seller's storage to the buyer's inventory; gold
    goes to the seller's storage (less the city tax from 100,000, added
    to the city's impost; prices up to 1000 are "demonstration"), bars
    to a storage slot, cash between the accounts; both saved. 0xBF1
    funds, 0x3F1 store timer: 1 honor every 10 minutes open.
    "/fecharloja", "/hideleilao". Logout closes the store; a listed
    storage slot cannot be moved; the account lock's auto-trade switch
    blocks it all but the browse list (as the C++).
    Fixed: unknown currencies handed the item over for free (the item
    moved before the payment), one storage slot could be listed twice,
    the seller's bars were lost with a full storage, a sold listing could
    stay up, a seller could buy from themselves, pages were sent with a
    size past the packet (now the whole 2516-byte struct).
  - Slice 12 (done): every slash command of RequestCommand.cpp is now
    handled (none answers "not available" any more).
    - Player (`cmdplayer.go`): /daily, /roleta, /summonguild, /spk
      (/gritar, Trombeta 3330 to every channel through the DB 0xD1D),
      /passe, /attpass, /attrank, /novato (once per account: 30 min of
      buffs 30 and 39), /honra, /quest, /evoc, /sagua /sarmia /sgelo
      /serion /sazran /skefra /spilar, /tab, /nig, /snd (shown to
      whisperers), /relo (portal scroll; party/guild member, and for
      non-ClassInfo-1 only after that player's /summon), /summon (0x3B2;
      the summon skill, instance type 9, sends it too), /registerbattle
      (answers "event not found", the arena is not ported), /srv507.
    - Group finder (`groupfinder.go`): passwords (/passgrupo,
      /definirsenha, /group atived|desatived, /lgetin), listing 0xCDB1 and
      search 0xCDB6 (10 per page, 700 ms between searches), /atvgrupo
      (public/private), /atvevoc (members' evocations; listed groups start
      with them off, as in the C++), /atvfll (auto-follow the leader
      outside towns: walk, teleport beyond 30 cells), /entrar lider senha
      and /mgetin (password join), /transferirgrupo, /countwater, 0x99B
      group info, 0x2132 client info, 0x99F reconnect flag. Changed from
      the C++ (by request): forming a party no longer generates a
      password ("Senha gerada"); the leader sets one with /passgrupo,
      /definirsenha or /group atived, and joining members learn it. A party
      without password is not listed in the group finder.
    - Admin (`admin.go`): /gm and /sgm with +item +frag +move +hpmob
      +snoop +dcall +on +exp +generate +gate +cash +set mission +level
      +equip +limparinv +limparbau +guildowner +resetcities +fame +special
      +remove +removeall +buff +nobuff +summonserver +summon +kick
      +teleport +relo +ban +coin +not +reloadcash +status +learn. The rest
      (week mode, Kefra, weather, drop/exp rates, ban lists, C++ file
      reloads, +loja, +create, +name, ...) answer "not supported".
    - Data (`daily.go`): Data/DailyReward.txt with the 0x671 info and the
      0x672 collect (state in Cache/Daily/<LOGIN>.bin, shared with the
      C++), Data/Missions.xml for SendMissionInfo 0x347, ../Conf/Roleta.txt
      for the /roleta box list 0xF05.
    Fixed: "/@9638" (level 1010 for anyone, i.e. admin) is not ported;
    "/sgm" had no check and "/gm" spawned three bosses for anyone; admin
    argument parsing (several commands could never match); "/spk" sent a
    blank line to the other channels; "/relo" accepted anyone's summon; a
    password shorter than 4 ("0" = disabled) opened parties; "/mgetin"
    looked the leader up by the word "mgetin"; "/countwater" repeated
    itself per member; the daily collect wrote Affects[-1] with no free
    slot; /roleta read past the box table.
- E. Events & extras.
  - 2026-11-07: daily missions (missions.go: offer every 1152 s, accept
    0x349, collect 0x348, party kill counters; /attpass no longer resets the
    clock); Lactolerium 100 (4141) also on +10 gear and planet amulets;
    account lock reload from the admin panel (acctedit OpLock); city owners
    in gamedata.city_owners (panel, live reload); auction buy from the
    all-stores page (the client sends the page position); kings: Arch
    creation without a kingdom cape, guild leaders move their guild's
    kingdom (AddGuild type 3, Go only).
  - 2026-11-07 (2): every TMSRV option comes from tmsrv.yaml (all keys
    required, network included); city owners only in the panel (GM
    +guildowner / +resetcities removed, guild_zone.txt owners seeded once);
    the 60 inventory slots are open to everyone (permanent bags in 60/61);
    WPE warning 0x634 closes the player and raises an alert (0xC13 -> DBSRV
    -> alerts table -> panel 'Alertas'); undeliverable items go back to
    DBSRV (0xC12); GM needs account access (no more level >= 1000).
  - 2026-11-07 (3): the data files the servers read live in `data/` in
    this repository (`data/tmsrv`, `data/dbsrv`); only `xml-import` still
    reads `../Release Servidor/DBSRV`. `Roleta.txt` moved from `../Conf` to
    `data/tmsrv/Conf`.
  - 2026-11-08: multiple channels. tmsrv.yaml lists every channel
    (`serverlist`, `channels`; `network.listen_*` and `channel` are gone)
    and each TMSRV picks one with -channel; DBSRV reads the same list
    (serverlist.txt removed) and accepts 10 channels (C++ MAX_SERVER 3);
    the admin panel serves the client's serverlist.bin
    (internal/cluster). In-game channel change ("srv507 N"): TMSRV ->
    0x814 -> DBSRV single-use key "*N token" bound to channel and slot ->
    0x52A -> client reconnects -> DBSRV logs it straight into the world at
    the saved position (0x417 WorldPos). The C++ TempKey path is gone.

## What is still missing (2026-11-06)

Client packets the C++ handles and Go does not yet (PacketControl.cpp):
- Gameplay: the event-driven NPCs of 0x28B (see slice 9), 0x28E city
  tax/challenge NPC (needs the guild war week: WeekMode, CastleState),
  0xAD9 griffin, 0xED7 guild war.
- Systems: premium/donate shops (0xAF3, 0xF03, 0xAAF1 honra market),
  rankings (0x528-0x530, 0x487, 0x912A), drop list (0xA08/0xA09), filter
  drop 0x506D, roulette spin 0xF06/0xF07 (the /roleta list is done),
  mission drop counters (Missions.xml has none; the C++ never counted), quiz 0x2C7, reCaptcha 5269 (CheckQuestTime),
  client info 0x655/0x656, reconnect 0xAF5, guild panel list, essence
  packet, 0x3776.
- Inside ported handlers (TODO(phase X) in code): PvP skill specials
  and auras, SendHpMode, LevelItem rewards, city taxes, castle
  / tower / RvR / Kefra / Nightmare wars and areas, the mission event
  timer, the admin commands that need those systems (week mode, Kefra,
  weather, rates, ban lists), arena (/registerbattle), water (Water.cpp,
  feeds /countwater), clue/pista leaders in /transferirgrupo, duel
  letters.

## Game data in Postgres

Items (`ItemList.csv` + `ItemEffect.h` names), skills (`skilldata.csv`),
drops (`controle drop/Controle.txt`) and teleports (`Teleport.txt`) live in
the `gamedata` schema (migration 0006). `cmd/gamedata-import -dir ... -dsn ...`
reads the C++ files once and replaces the tables in one transaction (then
reads them back and checks the server can use them). The TMSRV loads them at
startup with a read-only snapshot (`-dsn` / `DATABASE_URL`); after that the
files are not used. Names are UTF-8 in the database and Windows-1252 in
memory. Still files: guild_zone.txt (city bounds; owners live in gamedata.city_owners), Language.txt,
NPCGener.txt + npc/, HeightMap.dat, AttributeMap.dat, InitItem.csv.

Live reload (migration 0007): statement triggers on every gamedata table
`pg_notify('gamedata_changed', table)`; the TMSRV keeps its store open, LISTENs
(`store.ListenGameData`, reconnects, collapses bursts within 1 s), reloads
the tables in a goroutine and hands them to the game loop (`evGameData`),
which swaps them between packets. A reload that fails validation (e.g. no
items) keeps the current tables. `gamedata.change_log` records who changed
what (admin panel saves, imports).

Admin panel: "Dados do jogo" screens for items (search, edit, 12 effects with
named dropdown), skills (all fields with help), drops (per monster, item lines,
create/rename/delete; trailing spaces shown as ␣) and teleports, each with its
change history. Next: exporting the client ItemList.bin from the database.

Account editor (admin panel, migration 0008, delivery protocol 4): from an
account page, each character opens `/accounts/{user}/chars/{slot}` (gold,
level as shown in game, equipment slots 1-15 and inventory 0-62, each slot
with edit/remove) and `/accounts/{user}/storage` (128 storage slots and the
storage gold); the account page sets the GM access level. Every change is an
"edit" delivery (`internal/acctedit`): an item edit carries the item the
admin saw ("expect") and is refused if the slot changed meanwhile. DBSRV
applies it to the stored account when the player is offline; while online
on another character or at character selection, a character edit goes into
DBSRV's copy (TMSRV only saves the character in play) and a storage edit
waits for the player to enter the game; in game it sends 0xC11 AdminEdit to
the TMSRV, which applies it to the live character (client updated, saved at
once). The access level is always written to the database and DBSRV's copy,
and sent to TMSRV in game (/gm, /sgm work at once). 0xC11 is Go-only: a C++
TMSRV ignores it. The Go TMSRV now also handles 0xC0E (cash delivery) and
0xC10 (ban, disconnects). Level edits put the exp at the start of the level
(the in-game "/gm +level" did the same fix: the C++ gave the next level's
exp). The items shown are those of the last save.

## Settings (Go only)

`tmsrv.yaml` (flag `-config`, default `tmsrv.yaml` in the working folder)
holds the Go server's own options: `channel`, `newbie_zone` and `mob_ai`
(idle/battle think intervals, aggro while walking, followers looking for
players, minimum attack chance). Defaults = C++ behavior; unknown keys are
an error. The Go server no longer reads the C++ `gameconfig.txt`; its
other values (drop rates, exp bonus, refine rates, ...) move to
`tmsrv.yaml` as the features that use them are ported.

## Architecture (C++)

- Single Win32 message loop (`MainWndProc.cpp`): WSA async sockets + timers
  TIMER_SEC / TIMER_MIN / TIMER_HOUR / TIMER_SEND. All state is global:
  `pUser[1000]` (CUser), `pMob[MAX_MOB]` (CMob; 0..999 = players), `sServer`,
  `g_pMobGrid[4096][4096]`, `g_pItemGrid`, `ItemList[]`.
- Go plan: one game-loop goroutine owns all state; network goroutines only
  decode/encode and post events. Same ordering as the C++ loop, no locks.

## Client protocol

- Listen address and DBSRV address from `tmsrv.yaml` (`network.*`; the C++ read `serverip.txt` / `dataserver.txt`). Max packet 8096 bytes.
- Same framing/cipher as the DB link (`internal/protocol`): INITCODE hello
  `0x1F11F311`, 12-byte header, KeyTable cipher keyed by header byte 2,
  random key on send (`Rand() % 256`). `CUser::Keys[16]`/`GetHashKey` exist
  but the active AddMessage does not use them; the client still receives
  16 random KeyTable bytes in 0x10A `Keys`.
- `CheckPacket(Header)` validates sizes per packet id (except 0x334).
- Incoming `Header.ClientId` is overwritten with the slot id unless the
  timestamp is the magic 0x0E0A1ACA.
- Server->client "system" client ids: 0x7530 (30000), 0x7531, 0x7532.

### Client -> TMSRV (Phase A)

| id | handler | notes |
|---|---|---|
| 0x20D | RequestLogin | p20D; requires Status USER_ACCEPT; MAC limit 3 accounts; uppercases login, strips ' and space; fail-count >= 3 blocks; forwards to DB as 0x803; Status = USER_LOGIN |
| 0xFDE | RequestNumeric | pFDE (32 bytes); Status must be USER_SELCHAR(11); forwards to DB |
| 0x20F | RequestCreateChar | p20F; class 0..3, slot 0..3, Status SELCHAR; Nick[12]=0; -> 0x802; Status USER_CREWAIT |
| 0x211 | RequestDeleteChar | p211; Status SELCHAR; (C++ checks equip/gold of pMob = last played char, a bug); 2h penalty after create; -> 0x809; Status USER_DELWAIT |
| 0x213 | char login | requires TokenOk (numeric pass) else signal 0xFDF + msg; Status SELCHAR; -> 0x804 |
| 0x215 | CharLogOut | back to char list |
| 0x366/0x368/0x36C | RequestAction | movement |
| 0x333 | RequestChat | |
| 0x334 | RequestCommand | slash commands |
| 0x376 | RequestMoveItem | |
| 0x369 | request mob info | resend create mob if in half view, else remove |
| 0x36A | RequestMotion | |
| 0x2CB, 0x3AE | ignored | |

### DB -> TMSRV replies

| id | action |
|---|---|
| 0x416 | SendCharList: check UserName; store CharList+storage; 16 random keys; send as 0x10A clientId 0x7532; Status SELCHAR |
| 0x415 | store access, pass block, water, divina/sephira, ban, IsBanned, cash/honra/novato |
| 0x418 | ResendCharList: send as 0x110 clientId 0x7531; Status SELCHAR |
| 0x419 | send as 0x112 clientId 0x7531; Status SELCHAR |
| 0x41D / 0x41E | signal 0x11A / 0x11B (create/delete fail); Status SELCHAR |
| 0x41F | msg wrong password; Status SELCHAR |
| 0x420 | signal 0x11D (clientId 0x7532), close |
| 0x421 / 0x422 | msg + signal 0xF22, close (0x422 also counts a failed login) |
| 0x424 / 0x425 / 0x426 | msg blocked / disabled / maintenance, close |
| 0xFDE | SendNumericToken: TokenOk = true; echo pFDE clientId 0 |
| 0xFDF | wrong numeric: count, close at 4; signal 0xF23 + echo |
| 0x820 | GetCharInfo: pMob.Mobs = p.Mob (full CHARINFO) |
| 0x417 | SendCharToWorld (see below) |
| 0x40A | SavingQuit: if not playing send 0x805; message by mode; CloseUser |
| 0x40B | save-quit done: CloseUser |
| 0x423 | Parm1==1 -> sapphire = Parm2; ==2 -> FirstKefra |
| 0x80E | sapphire <<= / >>= 1, clamp 1..35 |
| 0x101, 0x908 | SendNotice |

### SendCharToWorld (0x417 -> client 0x114)

Fix-ups on p114.Mob equip/inventory (weapons Pos 64/192: EF_DAMAGE2/ADD -> EF_DAMAGE;
armor Pos<=32: EF_CRITICAL -> EF_CRITICAL2 + base; boots damage cap 30/42; costume
time effects stripped); drop items 470/500 from inventory; TokenOk=true;
pMob.Player = Mob; Mode=2; spawn at city (g_pCityZone[CityID] from guild_zone.txt
+ rand 14), guild zone if charged guild, training area 2112x2042 for low mortals,
fallback 2100x2100; GetEmptyMobGrid; Status=USER_PLAY; GetCurrentScore;
send 0x114 (clientId 0x7531, ClientIndex = slot); build p364 (GetCreateMob,
SpawnType 2) and GridMulticast_2 to neighbours; SendGridMob; SendScore; SendEtc;
plus many optional notifications (auction, daily reward, kingdom, nightmare).
Inventory[63] defaults to item 547 (EF 43/76/77) and PK point 75.
