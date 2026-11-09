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

**Sem efeito hoje, de propósito:** 0x655/0x656 só respondem a uma pergunta
que o servidor faz ao cliente (anti-hack), e o Go não pergunta. O 0x1495
(reCaptcha) passou a valer em 08/11, na entrada das Lanhouse.

Mestre Grifo (0xAD9, 0x632) feito em 08/11.

Desafio da cidade (0x28E), declarar guerra (0xED7) e Pesadelo (0x631) feitos em
08/11. O 0x3A0 não faz nada no C++ (`break` direto). A declaração de guerra só
consome o item 4030, como no C++, que nunca terminou esse sistema.

**Quiz (0x2C7): não será portado.** No C++ nada registra jogador no `CQuiz`:
os tickets das Lanhouse, que o usavam, passaram para o reCaptcha. Sem jogador
registrado, nenhuma pergunta é enviada, e o `Quiz.txt` não tem uso.

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
| ~~Pacotes de itens (volátil 210)~~ | **Sem dados:** o `PacItem.txt` não existe e nenhum item do `ItemList.csv` tem volátil 210 |
| ~~Pergaminhos de Pesadelo (voláteis 173 a 175)~~ | **Feito (08/11).** O 173 (Pesadelo N) sempre recusa: no C++ essa sala fica desligada |
| ~~Roleta (5381)~~ | **Feito (08/11)** |
| ~~Masmorras Lanhouse (4111, 4112, 4113)~~ | **Feito (08/11)** |
| ~~Recusa de Guerra (4031)~~ | **Feito (08/11).** Só consome o item, como no C++ |
| ~~Pergaminhos da Água (voláteis 21 a 30, 131 a 140, 161 a 170)~~ | **Feito (08/11)** |
| ~~Cartas de Duelo (3172, 1731, 3171)~~ | **Feito (08/11)** |

As demais regiões ativas do C++ já existem no Go.

Decisões que diferem do C++ nas Lanhouse:

- O código do reCaptcha só vale com a janela aberta. No C++ ele ainda era
  aceito depois dos 30 s.
- Um segundo ticket com o reCaptcha aberto é recusado, sem gastar o item.
- Fica na área sem tempo restante só quem tem nível de acesso de GM. O C++
  isentava por nível 1000, a mesma regra retirada no [B3](#b3).

### 3.3 Eventos e sistemas inteiros

- Eventos do `CEventManager`: todos resolvidos. **Missão diária**, **Carta de
  Duelo** e **Arena** foram portadas; o **Quiz** não tem uso (ver 3.1).
**Feito (08/11), com testes:** semana de guerra e guerra de cidades (com 5 vidas),
apostas, castelo de Noatun (altar de 2 minutos), Torre de Erion, RvR, batalha
de reinos (torre guardiã e rei), Kefra (morte, bônus de 50% de exp, renascimento
de quarta), Coliseu, Pesadelo M e A, Água, Castelo de Zakum, impostos
(`guildtax` e NPC de imposto), clima, chefes por tempo (seção `bosses`).
O estado de cada canal fica na tabela `channel_state`, gravada a cada minuto.

Decisões que diferem do C++:

- **Donos das cidades são os mesmos em todos os canais** (como o painel já
  fazia). Apostas, guerras de cidade e Noatun acontecem só em `wars.channel`.
  A Torre de Erion, a RvR e os impostos continuam por canal.
- O resultado da guerra é gravado em `gamedata.city_owners` (autor "guerra
  (canal N)"), então a recarga do gamedata não desfaz mais o resultado.
- Imposto recolhido vai todo para o gold: o limite aqui é `limits.max_gold`, e
  as barras do C++ existiam por causa do teto de 2 bilhões.
- RvR: no C++ o fim estava comentado e a guerra nunca acabava. Usei o bloco
  comentado (fim aos 30 minutos, vencedor, bônus, loja). Vale de segunda a
  sábado no horário `wars.rvr_hour`. O bônus do vencedor fica por canal.
- As apostas sobrevivem a um reinício (o C++ perdia); guardo as 8 maiores por
  cidade.
- No altar, líder sem cidade não herda mais o contador do líder anterior.
- Pesadelo M também aceita voltar após desconectar (no C++ só o A).

**Ainda não portado:**

- Pista de Runas (Uxmal, ~1500 linhas com regras próprias por sala).
- Evento do NPC Yugi (`CEventHit`).
- Ataque em área do Kefra (`aiAreaAttack`).
- Loja da RvR: o NPC nasce, mas a lista de itens nunca era preenchida no C++.
- Itens especiais de evento nas lojas de NPC.

**Não será portado:** calabouço zumbi (no C++ o NPC só guarda o registro, nada
usa essa lista). `Quests\*.c` não existem no servidor.

**Arena (08/11):** registro com o Odin ou `/registerbattle`, 4 equipes por
poder, até 15 minutos, meta de 15 pontos mais 10 a cada 4 jogadores acima de
12. Horários e prêmios ficam na seção `arena` do `tmsrv.yaml`, porque o
`arenaconfig.xml` não existe; o C++ tinha os horários fixos no código. O
`docker-compose` passou a definir `TZ` nos canais, senão os horários seriam
UTC.

- O cliente não trata os pacotes de placar 0x690/0x691, então eles não são
  enviados. A pontuação aparece na etiqueta "Grupo X (kills)" sobre o jogador.
- O prêmio do grupo vencedor do C++ (item 4132, quantidade 2) nunca era
  entregue: a variável que o liberava jamais recebia valor. Por isso
  `winner_items` começa vazio.
- Diferenças do C++: empate no top kill não tira o prêmio de participante do
  primeiro empatado; a guilda volta a aparecer no fim da luta, sem precisar
  reconectar; morte causada por monstro não soma ponto (o C++ lia fora da
  tabela de usuários); só quem está na luta acumula kills.
- Prêmio com o inventário cheio se perde, como no C++, com aviso ao jogador e
  registro no log.

### 3.4 Arquivos de dados do C++ que o Go não lê

- `ReadBoss`: **feito (08/11)**, seção `bosses` (começa vazia: o `Boss.xml` não
  existe).
- `ReadBanList`: **feito (08/11)**, tabela `mac_bans`, comando `+banmac`.
  Para desbanir: `DELETE FROM mac_bans`, ainda sem tela no painel.
- `ReadRanking`: substituído pela tabela `ranking`.
- `ReadScheduled`: não será portado, a execução está comentada no C++.
- `ReadBlockedItem`: **feito**, `drops.blocked_items`. Texto antigo: o Go não
  tem (só a tabela de drops do painel). Ver [seção 6](#6-decisão-pendente).
- `ReadPacItens` e `LoadQuiz` não serão portados (ver 3.1 e 3.2).

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

**Feito (08/11):** `+weekmode`, `+createkefra`, `+kill kefra`, `+weather`,
`+bonusexp`, `+banmac`, `+reloadban`, `+create`.

Continuam sem suporte: `+drop` (depende do drop por inventário), `+name` (troca
o nome sem checar duplicidade no banco), `+loja` (loja falsa), `+reload*` (os
dados vêm do banco e do yaml).

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
2. **`servidor.bat`**: **feito (08/11)**, comando `atualizar` (parar, compilar,
   iniciar). Evita o B4.
3. **Confirmação de entrega** TMSRV → DBSRV para itens, cash e edições
   (resolve o B2 e deixa o status do painel fiel).
4. **GM só por nível de acesso** (B3), com registro de cada comando de GM.
5. **Painel**: status dos canais **feito (08/11)**, no topo de cada página. Falta
   a lista de lojas abertas.
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
7. **Lanhouse**: mortal usa o ticket 4111 em qualquer lugar, confere o
   reCaptcha, entra e vê a contagem de 10 minutos; ao zerar, volta à cidade.
   Testar também a segunda conta do mesmo computador na área.
8. **Carta de Duelo**: líder do grupo no Altar de Thor (1046, 1690) usa a
   carta; matar as salas e conferir a troca de sala, o contador de monstros e
   as runas no chão. Sem o Símbolo de Coragem o grupo para na terceira sala.
9. **Guerras** (use `+weekmode` para adiantar as etapas): aposta no sábado,
   teleporte e 5 vidas no domingo, vencedor, imposto no NPC, altar de Noatun.
10. **RvR, Kefra, Coliseu, Pesadelo e Água**: conferir contadores e janelas no
    cliente. Os pacotes 0x637, 0x3AD e 0x3BB seguem o C++ e nunca foram vistos
    no cliente real.
11. **Arena**: com 4 contas em computadores diferentes, registrar no Odin
   (2122, 2149) nos 5 primeiros minutos de uma hora da seção `arena`. No
   minuto 5 conferir equipes, grupo, etiqueta "Grupo X (kills)", respawn ao
   morrer e o fim com prêmios.

---

## 6. Ajustes pedidos depois da auditoria (09/11)

- **Guerras e donos de cidade por canal.** `gamedata.city_owners` tem uma linha
  por canal e cidade; a tela Guildas > Cidades escolhe o canal. O que a seção
  3.3 diz sobre "canal único" e `wars.channel` não vale mais. Uma guilda que
  tem cidade em outro canal não aposta; a coroa vale para o dono de Noatun em
  qualquer canal, como no C++.
- **Drops mistos, geridos só pelo painel.** Cada monstro tem duas listas em
  Dados do jogo > Drops: **fixos** (a cada X mortes, a tabela que já existia) e
  **por chance** (slot, item, efeitos e chance). Os dois valem juntos. A aba
  **Taxas de drop** guarda a base e a taxa dos 64 slots e os itens bloqueados.
  Uma linha com chance 0 usa o slot; com chance N é 1 em N, e os bônus de fada e
  de drop continuam valendo. A seção `drops` saiu do `tmsrv.yaml`.
  Na primeira subida depois da atualização, o painel copia os inventários
  dos arquivos `npc/` para a lista por chance (4.230 linhas com os dados atuais;
  a tabela fica com 368 monstros, 91 deles com as duas listas), uma única vez. Depois disso os arquivos não são lidos para drops. Lojas de NPC
  continuam vendendo o inventário do arquivo. O gold por chance vem do campo
  gold de cada monstro.
- **GM só no painel.** Menu **Comandos GM**: canal, personagem, comando e a
  resposta do canal. O `/gm` do jogo agora é uma mensagem para a equipe, lida
  em **Mensagens**. O `/sgm` não existe mais.

## 7. Continuação da auditoria (09/11)

**Feito, com testes:** Pista de Runas (registro no Uxmal, as 7 salas, runas e
próxima Pista para o líder), evento do NPC Yugi (HIT-HERE), ataque em área do
Kefra, itens especiais das lojas (Teleporte Quest, baús de evento, Loja Arena),
reset das quests a cada 10 minutos, regras do Vale (fada e uma conta por
computador), prêmio de 250 mortes no BigCubo, desconexão por inatividade, lista
de lojas abertas (menu **Lojas**).

Correção no prêmio do BigCubo: o C++ passava as coordenadas trocadas para
`SendPremioArea`; aqui vale a área de fato usada na contagem de mortes.

**Sem efeito no C++, não portados:** `BossEvent`, `DropArea`, `Kingdom1Clear` e
`Kingdom2Clear` (nada liga esses estados), `Scheduled` (execução comentada),
calabouço zumbi (só guarda um registro), poção do `NewbieHour` (gera um monstro
de sala da Água sem jogador).

**Substituídos:** `AutoDonate` (arquivo por conta) pela entrega de cash do painel.

**Limitações conhecidas:**

- O ataque em área do Kefra sai como um golpe por alvo, não como um único
  pacote 0x367 com vários alvos. O dano é o mesmo; a animação no cliente pode
  mudar.
- A loja da RvR abre como loja comum do NPC: a lista de itens própria nunca foi
  preenchida no C++.
- Falta conferir no cliente real os pacotes das guerras e da Pista.
