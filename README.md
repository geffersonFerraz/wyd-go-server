# Servidor WYD 7.87 em Go

Reescrita em Go + PostgreSQL do servidor de With Your Destiny 7.87. Substitui
o `DBsrv.exe`, o `TMsrv.exe` e a ferramenta NP do servidor C++ original, e
fala com o cliente x64 do repositório `wyd-client`.

- [Visão geral](#visão-geral)
- [O que foi criado e modificado](#o-que-foi-criado-e-modificado)
- [Decisões técnicas](#decisões-técnicas)
- [Como rodar](#como-rodar)
- [Dados do jogo](#dados-do-jogo)
- [Canais](#canais)
- [Arquivos para o cliente e o launcher](#arquivos-para-o-cliente-e-o-launcher)
- [Testes](#testes)
- [Documentação](#documentação)

## Visão geral

| Programa | O que faz | Substitui |
|---|---|---|
| `cmd/dbsrv` | Banco do jogo (contas, personagens, guildas), porta 7514 | `DBsrv.exe` (C++, arquivos XML) |
| `cmd/tmsrv` | Servidor do jogo: um processo por canal (`-channel N`), portas 8281, 8282… | `TMsrv.exe` (C++) |
| `cmd/admin` | Painel web da equipe e porta pública para o cliente e o launcher | ferramenta NP do C++ |
| `cmd/xml-import` | Importa uma vez as contas XML do DBsrv C++ | — |

```mermaid
flowchart LR
    subgraph Jogador
        L[Launcher<br/>JogarWYD.exe / WYD-Linux]
        C[Cliente<br/>WYD.exe x64]
    end

    subgraph Host["Servidor (docker compose, rede 172.30.87.0/24)"]
        direction TB
        PUB["admin: porta pública :80<br/>manifest.json · serverlist.bin · itemlist.bin<br/>servNN.htm · /launcher/ · /register · marcas de guilda"]
        PAN["admin: painel :8080<br/>só em 127.0.0.1"]
        T1[tmsrv1 :8281]
        T2[tmsrv2 :8282]
        TN[tmsrvN …]
        DB[dbsrv :7514<br/>não publicada]
        PG[(PostgreSQL<br/>contas + schema gamedata)]
        BK[backup<br/>pg_dump diário]
    end

    L -- HTTP --> PUB
    C -- "pacotes WYD cifrados (TCP)" --> T1
    C -.-> T2
    T1 & T2 & TN -- "protocolo DB" --> DB
    DB --> PG
    T1 & T2 & TN -- "LISTEN gamedata_changed" --> PG
    PAN --> PG
    PUB --> PG
    BK --> PG
    Equipe([Equipe]) -- "túnel SSH" --> PAN
```

Os pacotes do jogo são mantidos byte a byte (structs MSVC x64, ver
`docs/x64-limites.md`), mas viajam em um transporte cifrado próprio (ver
`docs/transporte-seguro.md`). O cliente original de 32 bits **não** funciona:
os limites de 64 bits, o item de 14 bytes e o transporte mudaram o protocolo.

## O que foi criado e modificado

### Portado do C++

- **DBSRV**: login, criação e exclusão de personagem, baú, guildas, ranking,
  senha numérica e bloqueio por tentativas, com as contas no PostgreSQL em vez
  de um XML por conta.
- **TMSRV**: movimento, combate e skills, IA dos monstros, drops, grupo, troca,
  lojinha (autotrade), leilão, guildas e guerras (cidades, torres, RvR, Kefra),
  arena, duelo, montarias e evocações, refinação e os compositores, missões,
  Pista de Runas, Água, eventos de NPC, loja de cash, mercado de honra, roleta
  e recompensa diária. O que ficou de fora por não ter efeito no C++ está
  listado em `docs/auditoria-2026-11-07.md`.

### Novo neste servidor

- **Vários canais, um container cada**, com troca de canal dentro do jogo:
  a TMSRV pede ao DBSRV uma chave de uso único, salva o personagem e o cliente
  reconecta no outro canal já no mapa, na mesma posição, sem passar pela
  seleção de personagem.
- **Transporte seguro**: a conexão do jogo usa X25519 + ChaCha20-Poly1305 por
  conexão no lugar da tabela fixa do WYD, e o cliente só aceita o servidor que
  tem a chave privada (`docs/transporte-seguro.md`).
- **Limites em 64 bits**: gold, experiência, dano, HP, MP, ataque, defesa e
  atributos são `int64`; o efeito de item tem 16 bits (item de 14 bytes). Os
  tetos ficam em `limits` no `tmsrv.yaml`.
- **Dados do jogo no banco com recarga ao vivo**: itens, skills, drops,
  teleportes, NPCs, geradores e regra de experiência são editados no painel e
  valem nos canais em cerca de um segundo, sem reiniciar.
- **Drops mistos**: cada monstro tem uma lista fixa (a cada X mortes) e uma por
  chance, mais as taxas dos 64 slots.
- **Inventário em grade**: o item ocupa o tamanho real dele (`carrygrid.go`),
  e o cliente tem botões para destruir tudo, ordenar e largar tudo (0xF40).
- **Drop direto no inventário** (`drop_to_inventory`) e fogos coloridos.
- **Painel web** no lugar dos comandos de GM dentro do jogo:

  | Menu | O que faz |
  |---|---|
  | Contas | Criar, banir, trocar senha, editar personagem (gold, nível, equipamento, inventário) e baú, entregar item e cash |
  | Dados do jogo | Itens, skills, drops, taxas, teleportes, NPCs, geradores e experiência, cada um com histórico |
  | Guildas | Guildas, donos de cidade por canal e marcas |
  | Aviso global · Comandos GM · Mensagens | Aviso para todos, comandos por canal e as mensagens `/gm` dos jogadores |
  | Lojas · Rastreio | Lojinhas abertas, registro de vendas e o caminho de um item entre contas |
  | Exportar | Exporta e importa a configuração inteira do servidor em um JSON |
  | Alertas | Pacotes impossíveis e outros avisos de segurança |

- **Porta pública** para o cliente: contagem de online (`servNN.htm`),
  `serverlist.bin` gerado do `tmsrv.yaml`, `ItemList.bin` gerado do banco,
  página do launcher, cadastro de conta com e-mail de boas-vindas (Resend) e
  o **manifesto com hashes** (ver abaixo).
- **Operação**: log diário por programa em `logs/` (90 dias), `pg_dump` diário
  em `backups/`, migrações automáticas ao subir o DBSRV.

### Corrigido em relação ao C++

A auditoria achou 12 bugs herdados ou do porte, todos corrigidos menos um
operacional. Os mais graves: a lojinha criava barras de ouro com preço 0, item
entregue com o baú cheio sumia, e qualquer personagem de nível 1000+ virava GM.

## Decisões técnicas

| Decisão | Por quê |
|---|---|
| **Um único goroutine dono do estado do jogo.** A rede só decodifica pacotes e posta eventos. | É a mesma ordem do loop de mensagens do C++, sem locks. Um bug de concorrência em regra de jogo vira duplicação de item. |
| **Layout dos pacotes conferido por teste** (`internal/wire/sizes_test.go`). | O cliente é descompilado: um byte de padding errado desloca todo o resto. Os offsets seguem MSVC x64 com `/Zp8`. |
| **PostgreSQL no lugar de XML e arquivos.** | Transação ao salvar conta, consulta para o painel e backup de verdade. |
| **Recarga por `LISTEN/NOTIFY`.** Gatilhos avisam `gamedata_changed`; a TMSRV recarrega fora do loop e troca as tabelas entre dois pacotes. | Editar no painel sem reiniciar canal. Uma carga que falha na validação mantém as tabelas atuais. |
| **Um processo por canal, identificado pelo IP de origem.** | Era assim no C++ (`serverlist.txt`). Um canal travado não derruba os outros. `internal/cluster` testa que o compose e o yaml batem. |
| **`tmsrv.yaml` com todas as chaves obrigatórias e chave desconhecida como erro.** | Erro de digitação na configuração aparece ao subir, não em produção. |
| **Tabelas pequenas no yaml, tabelas grandes no banco.** | O yaml é revisável em diff; 6.500 itens não são. NPCs são exceção: ficam no banco por causa do editor do painel. |
| **Edições do painel passam por uma fila de entregas.** Cada edição leva o item que o admin viu e é recusada se o slot mudou. | O personagem em jogo vive na memória da TMSRV; gravar direto no banco seria sobrescrito no próximo save. |
| **Senhas em argon2id** (o C++ guardava em claro). | — |
| **Cifra por conexão sobre o TCP existente, em vez de TLS ou QUIC.** | O cliente usa Winsock assíncrono preso à janela: trocar o transporte seria reescrever a rede dele. Cifrar cada pacote mexe em dois pontos do `CPSock` e mantém os pacotes iguais por dentro. |
| **Chave pública do servidor em arquivo (`serverkey.txt`), não baixada.** | Baixada por HTTP, quem intercepta a rede trocaria a chave junto. |
| **Painel e porta pública em listeners separados.** | O cliente busca arquivos por HTTP de qualquer lugar; o painel só escuta em `127.0.0.1`. |
| **GM só pelo painel.** O `/gm` do jogo virou mensagem para a equipe. | Comando de GM dentro do jogo era o caminho do bug "nível 1000 é GM". |

Fluxo de uma edição feita no painel:

```mermaid
sequenceDiagram
    participant A as Painel
    participant PG as PostgreSQL
    participant T as TMSRV (cada canal)
    A->>PG: UPDATE gamedata.items (+ change_log)
    PG-->>T: NOTIFY gamedata_changed
    T->>PG: lê as tabelas (snapshot)
    T->>T: valida; se falhar, mantém as atuais
    T->>T: troca as tabelas entre dois pacotes
    Note over A,T: cerca de 1 segundo, sem reiniciar
```

## Como rodar

Requisitos: Docker com compose. Para desenvolver, Go (versão no `go.mod`).

```sh
cp .env.example .env            # defina POSTGRES_PASSWORD
go run ./cmd/admin transport-key -env .env -pub "../wyd-others/Release Cliente/serverkey.txt"
docker compose up -d --build    # postgres, dbsrv, canais, painel e backup
docker compose logs -f tmsrv1
docker compose down             # para e mantém os dados (down -v APAGA tudo)
```

No Windows, sem Docker para os programas: `servidor`, `servidor restart`,
`servidor stop`, `servidor senha-admin` (`servidor.bat`).

Um servidor novo começa com o banco vazio: o DBSRV e os canais não sobem até
receberem os dados. Abra o painel (`http://127.0.0.1:8080`, por túnel SSH se
for remoto) e importe um arquivo gerado em **Exportar** no painel de outro
servidor.

Portas no host:

| Porta | Serviço | Aberta para |
|---|---|---|
| 8281, 8282… | canais (TMSRV) | todos |
| 80 (`PUBLIC_PORT`) | arquivos do cliente, launcher e cadastro | todos |
| 8080 (`ADMIN_LISTEN`) | painel | `127.0.0.1` |
| 5432 | PostgreSQL | `127.0.0.1` |
| 7514 | DBSRV | não publicada |

Configuração:

- `tmsrv.yaml` — toda a configuração da TMSRV. A lista `channels` também é
  lida pelo DBSRV (reconhece cada canal pelo IP) e pelo painel (gera o
  `serverlist.bin`).
- `.env` — banco, usuário e senha do painel, chave do Resend e a chave privada
  do transporte (`TMSRV_TRANSPORT_KEY`, obrigatória). Fora do Git, assim como
  `.admin-state/`.

## Dados do jogo

| Onde | Conteúdo | Quem lê |
|---|---|---|
| `tmsrv.yaml` | opções e tabelas pequenas: limites, canais, cidades, guerras, chefes, arena, recompensa diária, roleta, bônus de experiência | TMSRV; DBSRV e painel em parte |
| `config/` | `language.yaml`, `missions.yaml` | TMSRV e painel |
| Banco, schema `gamedata` | itens, skills, drops, teleportes, NPCs e geradores, regra de experiência, mapas, `ItemList.bin` do cliente | TMSRV, DBSRV e painel |
| Banco, tabela `account_locks` | bloqueio de conta | TMSRV e painel |

Os NPCs são editados em **Dados do jogo**: *NPCs* (status, experiência,
equipamento e loja de cada modelo) e *Geradores* (onde, quando e em que grupo
nascem). Os já nascidos continuam como estão até renascerem. O
`config/npc_generators.yaml` só é lido uma vez, para preencher a tabela vazia.

A experiência segue **Dados do jogo > Experiência**: base do monstro ×
diferença de nível × evolução do personagem × multiplicador global. A tela
simula o resultado. Com "Usar esta regra" desmarcado, vale o cálculo do C++.

Para trocar um binário inteiro:

```sh
go run ./cmd/admin world-import -itemlist ItemList.bin
go run ./cmd/admin world-import -heightmap HeightMap.dat -attributemap AttributeMap.dat -npc npc -npc-base npc_base -class classes
```

`ItemList.bin` e modelos de NPC valem em segundos. Mapas valem quando a TMSRV
reiniciar, e modelos de classe quando o DBSRV reiniciar.

## Canais

Cada canal é uma TMSRV com IP e porta próprios em `channels` do `tmsrv.yaml`
(1..N, no máximo 10). Para adicionar:

1. acrescente `{ number: 5, ip: 172.30.87.25, port: 8285 }` em `channels`;
2. copie o bloco `tmsrv4` do `docker-compose.yaml` como `tmsrv5`, com o mesmo
   IP, a mesma porta e `-channel 5`;
3. `docker compose up -d` e libere a porta no firewall.

O `serverlist.bin` novo chega aos jogadores pelo launcher, sozinho.
`go test ./internal/cluster` confere que o compose e o yaml batem.

```mermaid
sequenceDiagram
    participant C as Cliente
    participant A as TMSRV canal 1
    participant D as DBSRV
    participant B as TMSRV canal 2
    C->>A: escolhe o canal 2 (/canal)
    A->>D: pede chave de uso único
    D-->>A: chave
    A->>D: salva o personagem
    A-->>C: reconecte no canal 2 com a chave
    C->>B: conecta com a chave
    B->>D: valida a chave (vale uma vez)
    B-->>C: entra no mapa, na mesma posição
```

## Arquivos para o cliente e o launcher

A porta pública do painel (`-public-listen`) serve:

| Rota | Conteúdo |
|---|---|
| `/manifest.json` | nome, tamanho e SHA-256 dos arquivos abaixo |
| `/serverlist.bin` | lista de canais, gerada do `tmsrv.yaml` |
| `/itemlist.bin` | `ItemList.bin`, gerado do banco |
| `/servNN.htm` | jogadores online por canal |
| `/launcher/` | página mostrada dentro do launcher |
| `/register` | cadastro de conta |
| marcas de guilda | imagens enviadas pelo painel |

O launcher lê o manifesto e só baixa o arquivo cujo hash difere do que tem em
disco:

```json
{"version":1,"files":[
  {"name":"serverlist.bin","path":"/serverlist.bin","size":7040,"sha256":"…"},
  {"name":"ItemList.bin","path":"/itemlist.bin","size":1014000,"sha256":"…"}
]}
```

O manifesto é recalculado no máximo a cada 5 segundos. Um arquivo indisponível
fica fora da lista, e o launcher mantém a cópia local dele. Launchers
anteriores ao manifesto continuam funcionando: eles baixam tudo. O manifesto
confere integridade, não origem: sem HTTPS, quem intercepta a rede troca os
dois (ver o relatório, V2).

## Testes

```sh
export DBSRV_TEST_DSN=postgres://dbsrv:dbsrv@localhost:5432/dbsrv_test
go test ./... -p 1
```

O banco `dbsrv_test` é **apagado** a cada execução; `-p 1` é necessário porque
os pacotes compartilham esse banco.

## Documentação

- `docs/relatorio-2026-11-12.md` — organização dos três projetos, problemas,
  vulnerabilidades e melhorias sugeridas.
- `docs/transporte-seguro.md` — a cifra da conexão do jogo, as chaves e como
  implantar.
- `docs/x64-limites.md` — structs e pacotes que mudaram com os 64 bits; vale
  para o servidor e para o cliente.
- `docs/auditoria-2026-11-07.md` — auditoria do porte: bugs, o que não foi
  portado e por quê.
- `docs/tmsrv-port.md` — notas do porte C++ → Go, fase a fase (histórico).
