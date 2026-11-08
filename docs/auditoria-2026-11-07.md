# Auditoria do servidor WYD 7.87 em Go — 07/11/2026

Levantamento do que falta, dos bugs encontrados e de sugestões de melhoria
no servidor Go (`dbsrv-go`: DBSRV, TMSRV e painel admin), comparando com o
servidor C++ original (`Source Servidor`) e com o cliente (`Source Cliente`).

## Resumo

| | Quantidade |
|---|---|
| Bugs encontrados | 12 (1 crítico, 3 altos, 2 médios, 6 baixos) |
| Corrigidos | B1, B2, B3, B5, B6, B8, B9, B10, B12 em 07/11; B7 e B11 em 08/11 (falta só B4, operacional) |
| Pacotes do cliente sem tratamento no Go | 28 de 104 |
| Itens usáveis do C++ sem tratamento no Go | 6 grupos (8 das 98 regiões) |
| Arquivos de dados do C++ que o Go não lê | 12 |
| Sistemas/eventos inteiros não portados | ~15 |

Os mais urgentes (**B1–B3 já corrigidos**, ver cada item):

1. **Crítico — lojinha cria barras de ouro do nada** (anúncio em "barras"
   com preço 0). Ver [B1](#b1).
2. **Alto — item entregue com o baú cheio some** (painel, site de cash).
   Ver [B2](#b2).
3. **Alto — qualquer personagem com nível 1000+ é GM**; existem 3 assim no
   banco, em contas sem acesso de GM. Ver [B3](#b3).

---

## 1. Como foi validado

- **Compilação e análise**: `go build ./...` e `go vet ./...` sem erros.
  `gofmt` aponta só espaçamento em 8 arquivos antigos (os arquivos mexidos
  nesta sessão foram formatados).
- **Testes automáticos** (`go test ./... -p 1` com o banco de teste):
  - passam: `acctedit`, `admin`, `auth`, `legacy`, `protocol`, `store`,
    `wire` e todos os testes da TMSRV menos um;
  - falha `TestShippedSettingsAreCppDefaults` (ver [B8](#b8)), que não vem
    das mudanças desta sessão;
  - `internal/server` (DBSRV) não rodou na última rodada: o **Controle de
    Aplicativos do Windows bloqueou o executável de teste**
    (`server.test.exe`), mesmo compilado em outra pasta. Ele tinha passado
    duas vezes mais cedo hoje, depois das mudanças no DBSRV; desde então só
    mudou a constante `DeliveryProtocol` (4 → 5).
- **Comparação com o C++**: os 104 pacotes que o `CUser::PacketControl` do
  C++ trata contra os 81 do `handleClient` do Go; os pacotes DBSRV ↔ TMSRV;
  os carregadores de dados do `WindowsForm.cpp`; os eventos do
  `CEventManager`; as 98 regiões do `RequestUseItem.cpp`.
- **Painel**: subi uma cópia do painel novo na porta 8099, apontando para o
  banco real, só para olhar (nenhum formulário enviado; a cópia já foi
  encerrada e apagada). As telas **Guildas → Cidades** e **Bloqueio da
  conta** aparecem certas. O banco mostra que você já usou as duas (Armia
  para a guilda "abobrinha" às 01:16; bloqueio da ASDF às 01:18 — ver
  [B4](#b4)).
- **Não validado**: nada foi testado no cliente do jogo. O roteiro manual
  está na [seção 5](#5-roteiro-de-teste-no-jogo).

---

## 2. Bugs encontrados

### B1
**Crítico — Lojinha (leilão): anúncio em "barras" com preço 0 cria uma barra
de ouro a cada venda.**

- Onde: [auction.go:475](../internal/game/auction.go) aceita `price >= 0`
  (só barra `price > 100` para barras); na compra,
  [auction.go:627](../internal/game/auction.go) aceita qualquer pilha de
  barras com quantidade `>= 0`, desconta 0 e
  [auction.go:708](../internal/game/auction.go) entrega ao vendedor um item
  4011 com `EF_AMOUNT 0`, que o jogo conta como **1 barra** (`itemAmount`
  trata 0 como 1).
- Como acontece: duas contas combinam; uma anuncia qualquer item por
  0 barras, a outra (com pelo menos uma barra no inventário) compra. O
  comprador não paga nada e o vendedor ganha 1 barra (1 bilhão) por venda.
- Correção: recusar preço menor que 1 para barras (e para donate) ao
  anunciar e ao comprar; nunca criar pacote de barras com quantidade 0.
  Acrescentar um caso no `TestAuctionStore`.
- **Corrigido (07/11):** o anúncio com preço 0 em barras ou donate é
  recusado ("O preço mínimo é 1."), e a compra também recusa anúncios
  antigos assim. Teste em `TestAuctionStore`.

### B2
**Alto — Item entregue com o jogador online e o baú cheio é perdido.**

- Onde: [seal.go:199](../internal/game/seal.go) (`itemDelivery`, 0xC0F).
  Com o baú cheio ele só grava `item delivery: storage full` no console
  ([seal.go:221](../internal/game/seal.go)). O DBSRV já marcou o pedido como
  concluído ("Enviado ao jogo") ao mandar o pacote.
- Afeta: itens do painel e do site (compras de cash). Também se perde se o
  jogador desconectar entre o envio e a chegada.
- Correção: a TMSRV responder ao DBSRV se entregou ou não (o DBSRV deixa o
  pedido pendente e tenta de novo), ou colocar no inventário quando o baú
  estiver cheio.
- **Corrigido (07/11):** com o baú cheio o item vai para o inventário; se o
  inventário também estiver cheio (ou o jogador já tiver saído), a TMSRV
  devolve o item ao DBSRV (pacote novo 0xC12), que cria um pedido novo
  ("tmsrv (baú cheio)") para colocar no baú quando o jogador sair. Se nem
  assim couber, o painel mostra "Sem espaço no baú" e o pedido fica
  registrado. Testes em `TestItemDeliveryFullStorage` e
  `TestItemDeliveryReturn`.

### B3
**Alto (segurança) — Personagem com nível 1000 ou mais tem todos os
comandos de GM.**

- Onde: [admin.go:31](../internal/game/admin.go):
  `if u.accessLevel <= 0 && pl.BStatus.Level < 1000 { negar }`.
- No banco hoje, em contas com `access_level = 0`:

  | Conta | Personagem | Nível |
  |---|---|---|
  | ASDF | Teste-BM101 (mortal) | 1010 |
  | ASDF | [ADM] (celestial) | 1009 |
  | ASDF2 | Teste-BM21 (mortal) | 1010 |

  O nível 1010 vem do comando secreto `/@9638` do C++ (que o Go não
  portou), então personagens criados assim continuam GM.
- Correção: usar só o nível de acesso da conta (definido no painel) e
  baixar o nível desses personagens.
- **Corrigido (07/11):** os comandos de GM exigem nível de acesso maior que
  0 na conta; uma tentativa de personagem 1000+ sem acesso fica registrada
  no log. Teste em `TestAdminNeedsAccessLevel`. Os níveis dos 3
  personagens **não** foram alterados: para usá-los como GM, dê acesso à
  conta pelo painel; para tirar o nível, use a edição de personagem.

### B4
**Médio — O pedido de bloqueio #131 (conta ASDF, 07/11 01:18) falhou com
"unknown op".**

- O painel já era o novo, mas o DBSRV em execução era o antigo (último sinal
  de vida às 01:19 com protocolo 4). O arquivo de bloqueio foi apagado mesmo
  assim, então a conta ficou desbloqueada no login seguinte; só o aviso ao
  jogo em tempo real falhou.
- Causa: o `servidor.bat start` não reinicia o que já está rodando
  ([servidor.bat:60–67](../servidor.bat)) e o `build` mantém o `.exe` antigo
  quando ele está em uso ([servidor.bat:109](../servidor.bat), "mantido o
  atual").
- Já mitigado: o protocolo de pedidos subiu para 5, então o painel novo
  avisa "DBSRV desatualizado" em vez de enfileirar.
- Recomendação: depois de atualizar, sempre `servidor restart`. Ver também a
  sugestão 2 da [seção 4](#4-sugestões-de-melhoria).

### B5
**Médio — `+guildowner` e `+resetcities` (GM) não persistem e brigam com o
painel.**

- Onde: [admin.go:264](../internal/game/admin.go) e
  [admin.go:274](../internal/game/admin.go) mudam só a memória do canal.
- A próxima recarga do gamedata (qualquer edição de item, drop ou teleporte
  no painel) devolve o dono salvo no painel (`gamedata.city_owners`); um
  reinício devolve o do `guild_zone.txt`.
- Correção: gravar em `gamedata.city_owners` (via DBSRV) ou retirar os
  comandos e usar só o painel.
- **Corrigido (07/11):** comandos retirados (respondem que a gestão é pelo
  painel). Os donos ficam só no banco: na primeira subida do painel os donos
  do `guild_zone.txt` são copiados para as cidades ainda sem registro, e a
  tela Guildas → Cidades ganhou **Resetar todas as cidades**.

### B6
**Baixo — Slots das bolsas contados sem conferir a bolsa.**

- `inventoryAmount` ([compounder.go:181](../internal/game/compounder.go)) e
  `invSlotUsable` ([shop.go:79](../internal/game/shop.go)) aceitam os slots
  30–59 se o item dos slots 60/61 tiver uma "data" futura nos efeitos, sem
  checar se é a bolsa 3467; `firstEmptySlot`
  ([shop.go:90](../internal/game/shop.go)) checa. Risco baixo (depende do
  que pode ficar nesses slots), mas vale unificar.
- **Resolvido de outro jeito (07/11), a seu pedido:** as bolsas foram
  liberadas para todos. O inventário tem sempre os 60 slots, e ao entrar no
  jogo cada personagem recebe bolsas permanentes nos slots 60/61 (o cliente
  só mostra as páginas 3 e 4 se houver o item 3467 ali). A Bolsa do
  Andarilho deixou de ser consumida (responde que as bolsas já estão
  liberadas).

### B7
**Baixo — Ativar o Passe de Missões não atualiza a janela de missão.**

- Onde: [useitem_items3.go:560](../internal/game/useitem_items3.go) ainda
  tem `TODO: SendMissionInfo`; a função agora existe, basta chamá-la.
- **Corrigido (08/11):** ativar o Passe envia a janela da missão na hora.

### B8
**Baixo — `TestShippedSettingsAreCppDefaults` falha.**

- O `tmsrv.yaml` tem `aggro_while_walking: true`,
  `followers_look_for_players: true` e `min_attack_chance: 80`, mas o
  comentário do próprio arquivo diz "Os valores abaixo reproduzem o servidor
  C++", e o teste exige isso.
- Correção: decidir qual vale. Se os valores novos são intencionais, ajustar
  o comentário e o teste (por exemplo, testar só que o arquivo é válido).
- **Corrigido (07/11):** o `tmsrv.yaml` é a única fonte de configuração:
  todas as chaves são obrigatórias (o servidor não inicia e diz qual falta),
  e saíram do código para o arquivo a rede (antes `serverip.txt` /
  `dataserver.txt`), a pasta de cache, o limite de contas por computador,
  os tempos da missão diária e o intervalo de honra da lojinha. O teste agora
  só exige que o arquivo seja válido e completo.

### B9
**Baixo — Mensagens de GM com acentos quebrados.**

- Onde: 10 mensagens em [admin.go](../internal/game/admin.go) (linhas 120,
  140, 218, 241, 306, 324, 374, 376, 404, 484) estão gravadas com o texto
  já corrompido (`nÃ£o`, `manutenÃ§Ã£o`, `invÃ¡lido`...); aparecem assim no
  jogo.
- Correção: regravar essas strings em UTF-8 correto.
- **Corrigido (07/11):** as 10 mensagens foram regravadas (e corrigido o
  erro "encontado"). Uma varredura dos 194 arquivos de texto do
  `dbsrv-go` não achou mais nada fora de UTF-8. Os arquivos de dados do C++
  (`Release Servidor`) continuam em Windows-1252 de propósito: é o que o
  cliente lê.

### B10
**Baixo (segurança) — Aviso de WPE do cliente é ignorado.**

- O cliente manda 0x634 (`WPEOnWarnPacket`) quando detecta o WPE (editor de
  pacotes); o C++ desconectava o jogador, o Go só registra "unhandled" no
  modo debug.
- Correção: fechar a conexão, como o C++.
- **Corrigido (07/11):** a TMSRV salva o personagem, fecha a conexão e envia
  um alerta ao DBSRV (pacote novo 0xC13), que grava na tabela `alerts`. O
  painel ganhou a página **Alertas** (conta, personagem, IP, canal, horário;
  marcar como visto, um ou todos) e um contador de alertas não vistos no
  menu.

### B11
**Baixo — Mudança de reino da guilda deixa alianças incoerentes.**

- Quando o líder leva a guilda para o outro reino (novo nesta sessão), a
  aliança com guildas do reino antigo continua. Ao criar aliança o jogo
  exige o mesmo reino ([guild.go](../internal/game/guild.go), `doAlly`).
- Sugestão: desfazer a aliança ao mudar de reino.
- Relacionado: um líder que **já tem capa** do outro reino não consegue
  migrar; o Rei não responde nada ([merchant.go:823](../internal/game/merchant.go)).
  Uma mensagem ("tire a capa para mudar de reino") ajudaria.
- **Corrigido (08/11):** ao levar a guilda para o outro reino, a aliança
  dela e as alianças declaradas com ela são desfeitas (pelo DBSRV, em todos
  os canais). O líder com a capa do outro reino ouve do Rei "Tire a capa do
  outro reino para mudar sua guild de reino". Testes em
  `TestKingKingdomChangeEndsAlliances` e `TestKingLeaderWithOtherCape`.

### Observações de migração e configuração

- **Passe de missões do C++**: o C++ lia `TMSRV/Cache/MissionPass/<nome>.bin`
  ao entrar no mundo (o arquivo valia mais que o banco). O Go usa só o banco.
  Há 9 arquivos (o mais novo de 06/11/2026). Se algum jogador disser que
  perdeu o passe, é daí.
- **Rede**: a TMSRV Go agora lê o endereço do `tmsrv.yaml`
  (`network.listen_ip` / `listen_port` / `dbsrv_ip` / `dbsrv_port`), não
  mais do `serverip.txt`. Está em `127.0.0.1` (só conexões desta máquina);
  para jogadores de fora, coloque o IP da máquina ou `0.0.0.0`. O
  `AutoIP.bat` só mexe no `serverip.txt` do C++, então não vale mais para a
  TMSRV Go.

### B12 (achado durante as correções)
**Alto — TMSRV caía com o buffer de envio de um jogador cheio.**

- Quando o buffer de um cliente lento enchia, `flush` chamava `closeUser`,
  que enfileirava mais pacotes (troca, lojinha, grupo) e chamava `flush` de
  novo no mesmo buffer cheio — recursão sem fim até estourar a pilha e
  derrubar o canal inteiro.
- **Corrigido (07/11):** a conexão é desligada antes de `closeUser`. Teste
  em `TestFullSendBufferDisconnectsOnce`.

---

## 3. O que falta portar

### 3.1 Pacotes do cliente sem tratamento (28)

Hoje o Go ignora esses pacotes (só registra "unhandled" com `-debug`).

**Atualização de 08/11.** Implementados: ranking (0x528/0x529/0x530), perfil
(0x487, 0x912A), lista e busca de drops (0xA08/0xA09), filtro de drop
(0x506D), roleta (0xF06/0xF07), loja premium (0xAF3, 0xF03), Mercado de Honra
(0xAAF1), painel de guildas (0x766), essência (0x473), reconexão (0xAF5) e o
eco 0x3776. O aviso de WPE (0x634) já tinha sido feito no B10.

**Sem efeito hoje, de propósito:** 0x655/0x656 e 0x1495 só respondem a uma
pergunta que o servidor faz ao cliente (anti-hack e reCaptcha), e o Go não
pergunta.

Mestre Grifo (0xAD9, 0x632) feito em 08/11.

**Ainda faltam:** desafio da cidade (0x28E, 0x3A0), declarar guerra (0xED7),
Pesadelo (0x631) e Quiz (0x2C7).

Decisões que diferem do C++:

- O ranking fica no banco (tabela `ranking`, pacote novo 0xC14), igual em
  todos os canais e atualizado a cada 30 s; o C++ tinha um arquivo por canal.
  A lista "Level" ordena por evolução, nível e exp (o C++ ordenava por um
  campo Exp que nunca preenchia).
- Loja premium: recusa quantidade zero ou negativa, e o item de presente
  (EF_GIFTID) sai uma vez por conta; no C++ ele saía de graça sem limite.
- Roleta: uma caixa sem prêmios não consome a caixa nem a chave. Prêmio
  pendente ao sair vai para o inventário ou, se cheio, para o baú. A barra do
  super prêmio volta a zero ao entrar no jogo, como no C++ (ficava só na
  memória).
- Reconexão: as áreas de quest foram corrigidas. No C++ as colunas estavam
  trocadas: a maioria nunca valia e a da Kaizen cobria quase o mapa inteiro.

**Lista de itens (08/11):** os 15 itens que só existiam no `ItemList.bin` do
cliente foram para o `ItemList.csv` e, no banco em uso, pela migração `0014`.
O servidor guarda a posição do cliente (colares 65536, cintos 131072), mas as
regras do jogo continuam vendo 16 bits, como no C++. O arquivo do cliente
fica em `data/client/ItemList.bin` e sai em `/itemlist.bin` na porta pública.
Restam diferenças de campo em 294 itens e 189 itens só no servidor.

Os ids 5706 a 5725 são cintos, então deixaram de valer como traje de montaria
na composição da Ehre. O Mestre Grifo ganhou gerador próprio em 2118, 2080, no
fim do `NPCGener.txt`. Os tickets 4716 e 4731 não existem nas listas de itens,
e o 4731 entregaria 5715 ou 5714, que são cintos.

**Dados das lojas:** convertidos para o `tmsrv.yaml`, seções `cash_shop` (8
abas, 92 itens, de `Data/CashShop.xml`) e `honor_market` (20 itens, de
`MarketHonra.txt`). O servidor Go não lê mais os originais; para mudar uma
loja, edite o yaml e reinicie os canais.

| Opcode | No C++ | Efeito para o jogador hoje |
|---|---|---|
| 0x528 / 0x529 / 0x530 | Ranking de nível, PvP e pontuação | Ranking não abre |
| 0x487, 0x912A | Perfil do personagem / posição no ranking | Perfil não abre |
| 0xA08 / 0xA09 | Lista de drops do monstro | Janela de drop vazia |
| 0x506D | Filtro de drop | Filtro não funciona |
| 0xF06 / 0xF07 | Girar a roleta / pegar prêmio | Roleta só mostra a lista (`/roleta`) |
| 0xAF3, 0xF03 | Loja premium (donate) | Loja de cash não funciona |
| 0xAAF1 | Mercado de Honra | Não funciona |
| 0x28E, 0x3A0 | Desafio da cidade (`RequestReqChallange`) | Não funciona |
| 0xED7 | Declarar guerra de guilda | Não funciona |
| 0x766 | Lista do painel de guildas | Painel de guildas vazio |
| 0xAD9, 0x632 | Mestre Grifo / resgatar grifo | Não funciona |
| 0x631 | Aceitar Pesadelo (Nightmare) | Não funciona |
| 0x2C7 | Resposta do Quiz | Evento Quiz inexistente |
| 0x473 | Usar essência | Não funciona |
| 0x655 / 0x656 | Informações do cliente | Ignorado |
| 0xAF5 | Reconexão (`MSG_Reconect`) | Ignorado |
| 0x1495 (5269) | reCaptcha anti-bot | Sem verificação anti-bot |
| 0x3776 | Eco de `p376` para o cliente | Ignorado |
| 0x634 | Aviso de WPE | Jogador não é desconectado (B10) |

### 3.2 Itens usáveis que o Go ainda devolve

O jogador recebe "Este item ainda não pode ser usado neste servidor." e o
item volta ([useitem.go:319](../internal/game/useitem.go)).

| Item | Região no C++ (`RequestUseItem.cpp`) |
|---|---|
| Pacotes de itens (volátil 210) | `PAC ITENS` — depende de `ReadPacItens` |
| Pergaminhos de Pesadelo (volátil 173) | `PESADELOS` — depende do sistema de Pesadelo |
| ~~Roleta (5381)~~ | **Feito (08/11)** |
| Masmorras Lanhouse (4111, 4112, 4113) | `LANHOUSE (MASMORRA N/M/A)` |
| Recusa de Guerra (4031) | `RECUSA DE GUERRA` — depende das guerras |
| Cartas de Duelo (3172, 1731, 3171) | `CARTA DE DUELO` — evento `DuelLetter` |

As demais regiões ativas do C++ já existem no Go.

### 3.3 Eventos e sistemas inteiros

- Eventos do `CEventManager`: **Quiz**, **Arena** (Odin, `/registerbattle`)
  e **Carta de Duelo**. (A **Missão diária** foi portada nesta sessão.)
- Guerras: torre, castelo, RvR, Kefra (área, bônus, renascimento), Coliseu,
  Pesadelo (Nightmare), Água (`Water.cpp`, alimenta `/countwater`); semana
  de guerra (WeekMode / CastleState); `isWarTime` hoje é sempre falso.
- Impostos da cidade para o dono (`/guildtax`,
  [world.go:302](../internal/game/world.go)) e prêmios das guerras.
- Quests de NPC: Zakum, Uxmal (Pista de Runas), dungeon zumbi, Yugi;
  `Quests\*.c` (o servidor não traz os arquivos).
- ~~Recompensas por nível (`LevelItem.txt`)~~ **Feito (08/11).**
- Lojas "Honra" e de RvR, itens especiais de evento nas lojas de NPC.

### 3.4 Arquivos de dados do C++ que o Go não lê

`ReadBlockedItem`, `ReadPacItens`,
`ReadRanking`, `ReadBoss` (chefes:
renascimento e fama), `ReadArenaConfig`, `ReadBanList` (banimento por
IP/MAC), `ReadScheduled` (tarefas agendadas), `LoadQuiz`.

### 3.5 Combate e detalhes pendentes (TODO no código)

**Feito (08/11), com testes:**

- Escudo de mana contra golpes de monstros, e dano de evocações em jogadores
  a 40%.
- Familiar (754, 769, 1726: ataque automático; 753/1726 perdem refino ao
  morrer) e auras 22 e 23.
- Skills 6 (puxão), 30, 41, 79, 98 e 102. A 9 e a 22 já existiam. O movimento
  do Exterminar não foi portado: no C++ a condição só vale para o gerador 374.
- Ilusão custa mana e exige a skill; botas com dano acima do limite são
  corrigidas ao entrar no mundo.
- Colisão de rota: mantido o desvio para a célula livre mais próxima. O laço
  `GetRoute` do C++ testava sempre a mesma posição e não fazia nada.
- Recompensas por nível e áreas sem perda de CP: seções `level_items` (181)
  e `no_cp_areas` (4) do `tmsrv.yaml`.
- Uso de itens rápido demais (169 ms) só registra no log, como no C++.

### 3.6 Comandos de GM ainda sem suporte

Week mode, Kefra, clima, taxas de drop/exp, listas de ban, recarga dos
arquivos do C++, `+loja`, `+create`, `+name` (respondem "Comando ADM não
suportado").

### 3.7 Painel admin

O DBSRV já sabe aplicar **banimento** e **entrega de cash**, mas o painel
não tem tela para isso. Também não há **aviso global** (o C++ tinha pelo
programa NP).

**Feito (08/11):** a página da conta ganhou **Cash** (soma ao saldo; chega
na hora se o jogador estiver no jogo) e **Banimento** (bloqueada ou
desativada, por N dias ou permanente; retirar banimento). Desbanir com o
jogador online não o derruba mais. O menu ganhou **Aviso global**: o texto
(até 95 caracteres, só caracteres que o jogo mostra) vai para todos os
canais abertos, e a página lista os avisos enviados. Protocolo de pedidos
subiu para 6 (o painel avisa se o DBSRV estiver desatualizado). Testes em
`TestBanAndCash`, `TestNotice` e `TestNoticeDelivery`.

---

## 4. Sugestões de melhoria

1. **Logs em arquivo (prioridade alta).** **Feito (08/11):** DBSRV,
   TMSRV (um arquivo por canal) e painel gravam, além do console, um
   arquivo por dia em `logs/` (`dbsrv-`, `tmsrvN-`, `admin-AAAA-MM-DD.log`),
   guardando 90 dias (`-log-dir`, `-log-days`, `LOG_DIR`). O
   docker-compose monta `./logs`. As trocas, vendas da lojinha, entregas,
   comandos de GM e refinações já eram registrados no log; agora ficam no
   arquivo. Texto original: TMSRV, DBSRV e painel só escrevem
   no console; ao fechar a janela, tudo some. O C++ gravava
   `Logs/Servidor_1/*.txt` (criação de itens, trocas). Gravar em arquivo
   por dia (ou no Postgres) as trocas, vendas da lojinha, entregas, comandos
   de GM e refinações — é o que permite investigar duplicação de itens.
2. **`servidor.bat`**: um comando `atualizar` (parar, compilar, iniciar), ou
   fazer o `start` avisar quando o processo em execução é mais antigo que o
   `.exe` novo. Evita o B4.
3. **Confirmação de entrega** TMSRV → DBSRV para itens, cash e edições
   (resolve o B2 e deixa o status do painel fiel).
4. **GM só por nível de acesso** (B3), com registro de cada comando de GM.
5. **Painel**: banir/desbanir, entregar cash, aviso global, lista de lojas
   abertas, status dos canais (online, jogadores conectados) — hoje só o
   DBSRV aparece.
6. **Lojinha**: preço mínimo por moeda (B1), registro das vendas numa tabela
   para auditoria e um teste de ponta a ponta com os pacotes exatamente como
   o cliente monta.
7. **Backup do banco**: não há rotina de `pg_dump`. Agendar um backup diário
   do volume `dbsrv-pgdata` (o `docker compose down -v` apaga tudo).
   **Feito (08/11):** serviço `backup` no docker-compose grava um
   `pg_dump` por dia em `./backups` (como restaurar está no comentário do
   serviço). Os backups antigos **não** são apagados automaticamente.
8. **Senha do Postgres**: o padrão é `dbsrv`/`dbsrv`. A porta só escuta em
   127.0.0.1 (risco baixo), mas vale definir outra no `.env`.
9. **Windows**: o Controle de Aplicativos está bloqueando executáveis de
   teste do Go. Liberar a pasta de build do Go ou desligar o Smart App
   Control na máquina de desenvolvimento.
10. **`gofmt`** nos 8 arquivos antigos e checagem `gofmt -l` no `build`.
    (Em 08/11 o `gofmt -l` já não aponta nenhum arquivo.)
11. **Documentação**: atualizar o comentário do `tmsrv.yaml` (B8) e manter
    este relatório junto do `docs/tmsrv-port.md`.

---

## 5. Roteiro de teste no jogo

Depois de `servidor restart`:

1. **Lac 100%**: arma/armadura +10 → +11; brinco (incluindo Hércules) até
   +11; amuleto de planeta +9 → +10. Em item +5 deve recusar sem gastar.
2. **Missão diária**: entrar com um mortal, esperar a missão (até ~19 min),
   aceitar, matar os monstros pedidos (também em grupo, perto), coletar.
   Com Passe ativo, conferir gold e itens extras; recusar uma missão e ver
   o tempo para a próxima no `/attpass`.
3. **Bloqueio**: criar um bloqueio no jogo; pelo painel, redefinir a senha
   com o jogador online (deve avisar no jogo) e depois desbloquear.
4. **Cidades**: dar uma cidade a uma guilda no painel; um membro deve
   voltar para a área da guilda no recall/teleporte da cidade.
5. **Lojinha**: duas lojas abertas em contas diferentes; comprar pela lista
   geral, pela busca e clicando no NPC da loja, em gold, donate e barras.
   (A compra pela lista geral e pela busca é a correção desta sessão: antes
   ela usava a posição do item na página e falhava sem mensagem.)
6. **Rei**: mortal 300 sem capa, com Pedra da Imortalidade e Sephirot →
   vira Arch. Líder de guilda sem capa → pega a capa do outro reino e a
   guilda muda junto (membros online trocam de capa).
