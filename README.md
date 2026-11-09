# Servidor WYD 7.87 em Go

Reescrita em Go + PostgreSQL do servidor de With Your Destiny 7.87:

| Programa | O que faz | Substitui |
|---|---|---|
| `cmd/dbsrv` | Banco de dados do jogo (contas, personagens, guildas), porta 7514 | `DBsrv.exe` (C++, arquivos XML) |
| `cmd/tmsrv` | Servidor do jogo: um processo/container por canal (`-channel N`), portas 8281, 8282... | `TMsrv.exe` (C++) |
| `cmd/admin` | Painel web da equipe (contas, itens, cidades, alertas); serve ao cliente `servNN.htm` e `serverlist.bin` | ferramenta NP do C++ |
| `cmd/xml-import` | Importa as contas XML do DBsrv C++ (uma vez) | — |

O protocolo com o cliente é mantido byte a byte (structs MSVC x64, ver docs/x64-limites.md),
então o cliente x64 do `wyd-client` funciona sem adaptações. Exceções: canais fora da
porta 8281 (o `serverlist.bin` leva `ip:porta`) e a troca de canal dentro do
jogo precisam do cliente com o patch de canais (`wyd-client`).

## Requisitos

- Go (veja a versão em `go.mod`)
- Docker Desktop, para o PostgreSQL (`docker-compose.yaml`)
## Dados do jogo (`data/`)

Tudo o que o servidor lê fica dentro do repositório:

| Pasta | Conteúdo | Quem lê |
|---|---|---|
| `data/tmsrv` | `HeightMap.dat`, `AttributeMap.dat`, `guild_zone.txt`, `Language.txt`, `NPCGener.txt`, `InitItem.csv`, `npc/`, `npc_base/` | TMSRV |
| `data/tmsrv/Data` | `Missions.xml`, `DailyReward.txt`, `NPCEvento.txt` | TMSRV |
| `data/tmsrv/Conf/Roleta.txt`, `data/tmsrv/Controlador EXP/` | roleta e bônus de experiência | TMSRV |
| `data/tmsrv/Cache` | bloqueio de conta e recompensa diária por conta (criado em execução, fora do Git) | TMSRV e painel |
| `data/dbsrv` | `npc/` (modelos das classes) | DBSRV |

Os arquivos de texto estão em Windows-1252, a codificação do cliente; o
`.gitattributes` impede o Git de alterá-los. Os caminhos podem ser trocados
pelas flags `-dir` (tmsrv), `-tmsrv-dir` (admin),
`-npc` (dbsrv).

Só o `xml-import`, que migra uma vez as contas do DBsrv C++ (arquivos com
dados de jogadores), ainda aponta para a pasta antiga `../Release Servidor/DBSRV`.

## Como rodar (Windows)

```bat
servidor            :: sobe o Postgres, compila e inicia DBSRV, TMSRV e painel
servidor restart    :: depois de atualizar o código
servidor stop
servidor senha-admin
```

Um servidor novo começa sem itens e skills. Importe um arquivo gerado em
**Exportar** no painel de outro servidor (menu Exportar > Importar).

## Configuração

- `tmsrv.yaml` — **toda** a configuração da TMSRV (rede, canais, missões,
  lojinha, IA dos monstros). Todas as chaves são obrigatórias. A lista
  `channels` é lida também pelo DBSRV (reconhece cada canal pelo IP) e pelo
  painel (gera o `serverlist.bin`).
- `.env` (copie de `.env.example`) — banco, usuário e senha do painel.
  Fica fora do Git, assim como `.admin-password`.

## Canais

Cada canal é uma TMSRV com IP e porta próprios, listada em `channels` do
`tmsrv.yaml` (numerados 1..N, no máximo 10). Para adicionar um canal:

1. acrescente `{ number: 5, ip: 172.30.87.25, port: 8285 }` em `channels`;
2. copie o bloco `tmsrv4` do `docker-compose.yaml` como `tmsrv5`, com o
   mesmo IP, a mesma porta (`"8285:8285"`) e `-channel 5`;
3. `docker compose up -d` (e libere a porta no firewall);
4. distribua o novo `serverlist.bin`: `http://<host>/serverlist.bin` ou
   `go run ./cmd/admin serverlist -o serverlist.bin`.

`go test ./internal/cluster` confere que o compose e o `tmsrv.yaml` batem.
O `servNN.htm` (online por canal) já mostra todos os canais conectados ao
DBSRV.

Troca de canal dentro do jogo: o jogador escolhe o canal na lista do jogo
(comando `/canal` no cliente com o patch). A TMSRV pede ao DBSRV uma chave
de uso único, salva o personagem onde está e manda o cliente reconectar no
outro canal, onde ele entra direto no mapa, na mesma posição, sem passar
pela seleção de personagem nem pela senha numérica.

## Testes

```bat
set DBSRV_TEST_DSN=postgres://dbsrv:dbsrv@localhost:5432/dbsrv_test
go test ./... -p 1
```

O banco `dbsrv_test` é apagado a cada execução; `-p 1` é necessário porque
os pacotes compartilham esse banco.

## Documentação

- `docs/tmsrv-port.md` — notas do porte C++ → Go, fase a fase, e o que falta.
- `docs/auditoria-2026-11-07.md` — auditoria: bugs, pendências e sugestões.
