# Limites em 64 bits (servidor e client)

Gold, XP (incluindo `Hold`), dano, HP, MP, Ataque e Defesa passaram para
`int64` / `long long`. O valor dos efeitos de item passou de 1 para 2 bytes
(até 65.535), e o item foi de 8 para 14 bytes. Esta lista é a mesma nos dois repositórios: qualquer
mudança em um lado precisa da mudança correspondente no outro, senão o pacote
é lido com deslocamento errado.

Os layouts seguem MSVC x64 com `/Zp8`. Offsets conferidos em
`internal/wire/sizes_test.go` (servidor).

## Implantação

O protocolo mudou: client antigo não funciona com servidor novo, nem o inverso.

1. Faça backup do banco. A migração `0015_x64_limits` roda ao iniciar o DBSRV.
2. Reconstrua e reinicie juntos DBSRV, todos os canais TMSRV e o admin.
3. Distribua o client novo ao mesmo tempo.

A conversão para 14 bytes é uma quebra de protocolo à parte: exige novo build
do client e novo deploy.

## Tetos (`limits` no `tmsrv.yaml`)

| Chave | Valor atual | Antes |
|---|---|---|
| `max_gold` | 1.000.000.000.000.000 | 2.000.000.000 |
| `max_damage` (golpe em mob) | 1.000.000.000.000.000 | 64.000 (62.000 no crítico) |
| `max_hp_mp` | 1.000.000.000.000.000 | 200.000 |
| `max_attack_defense` | 1.000.000.000.000.000 | 100.000 |

O máximo aceito em qualquer chave é 1 quatrilhão (`wire.MaxLimit`).

## Structs base

| Client (`Basedef.h`) | Servidor (`internal/wire`) | sizeof | Campos em 64 bits |
|---|---|---|---|
| `STRUCT_SCORE` | `Status` | 48 → 80 | `Ac`, `Damage`, `MaxHp`, `MaxMp`, `Hp`, `Mp` |
| `STRUCT_DAM` | `AttackTarget` | 8 → 16 | `Damage` (offset 8) |
| `STRUCT_ITEM` | `Item` | 8 → 14 | `STRUCT_BONUSEFFECT`: `cValue` de 16 bits no offset 2 |
| `STRUCT_MOB` | `Mob` | 848 → 1416 | `Coin` (24); `BaseScore` em 48, `CurrentScore` em 128 |
| `STRUCT_SELCHAR` | `CharList` | 968 → 1592 | `Coin[4]` (1528) |

## Pacotes

| Opcode | Client | Servidor | sizeof | Campos em 64 bits |
|---|---|---|---|---|
| 0x10A / 0x416 | `MSG_CNFAccountLogin` | `CNFAccountLogin` | 2376 → 4008 | `Coin` |
| 0x110 / 0x112 | `SelChar` | `CharListPacket` | 984 → 1608 | via `STRUCT_SELCHAR` |
| 0x114 | `p114` | `CNFCharacterLogin` | 1152 → 1728 | `Hold` (1720), via `STRUCT_MOB` |
| 0x181 | `MSG_SetHpMp` | `SetHpMp` | 28 → 48 | `Hp`, `Mp`, `ReqHp`, `ReqMp` |
| 0x18A | `MSG_SetHpDam` | `HpChange` | 20 → 32 | `Hp`, `Dam` |
| 0x185 | `MSG_Carry` | `Carry` | 528 → 920 | `Coin` (912) |
| 0x292 | `MSG_SetHpMode` | `HpMode` | 20 → 32 | `Hp` |
| 0x336 | `MSG_UpdateScore` | `UpdateScore` | 152 → 200 | `ReqHp`, `ReqMp`, via `STRUCT_SCORE` |
| 0x337 | `p337` | `Etc` | 48 → 56 | `Hold`, `Coin` |
| 0x338 | `MSG_CNFMobKill` | `Killed` | 32 → 40 | `FakeExp` (`Hold`) |
| 0x339 | `MSG_STANDARDPARM64` | `CargoCoin` | 16 → 24 | `Parm` (16) |
| 0x363 | `MSG_CreateMobTrade` | `StoreMob` | 268 → 304 | via `STRUCT_SCORE` |
| 0x364 | `MSG_CreateMob` | `CreateMob` | 248 → 280 | via `STRUCT_SCORE` |
| 0x367 | `MSG_Attack` | `Attack` | 168 → 288 | `FakeExp`, `ReqMp`, `CurrentMp`, `Dam[13]` |
| 0x39D | `MSG_AttackOne` | `Attack` (1 alvo) | 72 → 96 | idem |
| 0x39E | `MSG_AttackTwo` | `Attack` (2 alvos) | 80 → 112 | idem |
| 0x383 | `MSG_Trade` | `Trade` | 156 → 256 | `TradeMoney` (240) |
| 0x387 / 0x388 | `MSG_STANDARDPARM64` | `SignalValue64` | 16 → 24 | `Parm` (16), nos dois sentidos |
| 0x3AF | `MSG_STANDARDPARM64` | `SignalValue64` | 16 → 24 | `Parm` (16) |
| 0xA08 | `MSG_DropList` | `DropList` | 29416 → 49216 | `exp`, `gold` |
| 0x397 / 0x397B | `MSG_LeilaoStore` | `AuctionPage` | 2516 → 3096 | `Price` de cada item |
| 0x397C | `pAddItemAutoTrade` | `AuctionAdd` | 56 → 72 | `itemPrice` |
| 0x398 | `pStartNegotiation` | `AuctionBuy` | 68 → 80 | `itemPrice` |
| saldo do leilão | `st_SaldoLeilao` | `AuctionFunds` | 24 → 32 | `Gold` |
| missão | `MSG_MISSIONINFO` | `MissionInfo` | 352 → 432 | `Gold` |
| 0x17C | `MSG_ShopList` | `ShopList` | 236 → 400 | via `STRUCT_ITEM` |
| 0x3A6 e demais compositores | `MSG_CombineItem` | `Compor` | 84 → 132 | via `STRUCT_ITEM` |
| 0xAF1 | premium store | `CashShopInfo` | 1096 → 1488 | via `STRUCT_ITEM` |

## Client: além das structs

- `sprintf` de gold, HP, MP, dano e defesa usam `%lld`.
- Removidos os tetos de exibição: dano acima de 1.000.000 virava 0, e o HP
  pendente parava em 100.000.
- Removidos os cortes para `short` e `unsigned short` em `m_BigHp` e `m_nReqHP`.
- Viraram `long long`: `m_nCargoCoin`, `m_nFakeExp`, `m_nReqHP`, `m_nReqMP`,
  `m_Coin`, `m_nTradeMoney`, `m_BigHp`, `m_MaxBigHp`, `m_BigMp`, `m_MaxBigMp`,
  o evento de golpe (`nDamage`), `Punched`, `SetMyHumanExp`,
  `BASE_GetSkillDamage` (a sobrecarga de 5 argumentos), `FormatPrice`,
  `FormatPriceInKKK` e `SProgressBar`.
- O limite de preço digitado no leilão subiu para 1 quatrilhão.

## Item com valor de 16 bits

- Todo pacote com `STRUCT_ITEM` mudou de tamanho; o padding de cada um é
  conferido por `TestNaturalAlignment`.
- O HP da montaria continua guardado em índice + valor do efeito 0
  (`LegacyShort` no client).
- Migração `0016_item_effect_16bit`: colunas `efv` e valor do drop viram
  `integer`. O equipamento salvo do ranking no formato antigo ainda é lido.
- Os arquivos do client (`STRUCT_MYITEM`, ItemList) e os NPCs em `data/`
  mantêm o formato de 1 byte.

## O que continua em 32 bits

- Preços de catálogo (`Item.Price`, teleporte, `SellPrice` de recompra): vêm
  de arquivos binários de 32 bits. O total da compra é calculado em 64 bits.
- Cash, Honra e Novato não são gold. No leilão, preço em donate continua
  limitado a 1.999.999.999.
- Janela de grupo (0x37D, 0x37F): o HP vai em porcentagem.
- Arquivos NPC em `data/`: continuam no layout antigo e são convertidos ao
  carregar (`wire.UnmarshalLegacyMob`).
