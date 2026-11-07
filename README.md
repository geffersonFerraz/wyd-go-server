# Servidor WYD 7.87 em Go

Reescrita em Go + PostgreSQL do servidor de With Your Destiny 7.87:

| Programa | O que faz | Substitui |
|---|---|---|
| `cmd/dbsrv` | Banco de dados do jogo (contas, personagens, guildas), porta 7514 | `DBsrv.exe` (C++, arquivos XML) |
| `cmd/tmsrv` | Servidor do jogo (canal), porta 8281 | `TMsrv.exe` (C++) |
| `cmd/admin` | Painel web da equipe (contas, itens, cidades, alertas) | ferramenta NP do C++ |
| `cmd/gamedata-import` | Importa itens, skills, drops e teleportes para o Postgres | — |
| `cmd/xml-import` | Importa as contas XML do DBsrv C++ (uma vez) | — |

O protocolo com o cliente original é mantido byte a byte (structs MSVC x86),
então o cliente do jogo funciona sem alterações.

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
| `data/tmsrv` | `ItemList.csv`, `ItemEffect.h`, `skilldata.csv`, `controle drop/Controle.txt`, `Teleport.txt` | `gamedata-import` (depois tudo fica no Postgres) |
| `data/tmsrv/Cache` | bloqueio de conta e recompensa diária por conta (criado em execução, fora do Git) | TMSRV e painel |
| `data/dbsrv` | `serverlist.txt` (IPs dos canais) e `npc/` (modelos das classes) | DBSRV |

Os arquivos de texto estão em Windows-1252, a codificação do cliente; o
`.gitattributes` impede o Git de alterá-los. Os caminhos podem ser trocados
pelas flags `-dir` (tmsrv, gamedata-import), `-tmsrv-dir` (admin),
`-serverlist` e `-npc` (dbsrv).

Só o `xml-import`, que migra uma vez as contas do DBsrv C++ (arquivos com
dados de jogadores), ainda aponta para a pasta antiga `../Release Servidor/DBSRV`.

## Como rodar (Windows)

```bat
servidor            :: sobe o Postgres, compila e inicia DBSRV, TMSRV e painel
servidor restart    :: depois de atualizar o código
servidor stop
servidor senha-admin
```

Na primeira vez, importe os dados do jogo:

```bat
go run ./cmd/gamedata-import -dsn postgres://dbsrv:dbsrv@localhost:5432/dbsrv
```

## Configuração

- `tmsrv.yaml` — **toda** a configuração da TMSRV (rede, canal, missões,
  lojinha, IA dos monstros). Todas as chaves são obrigatórias.
- `.env` (copie de `.env.example`) — banco, usuário e senha do painel.
  Fica fora do Git, assim como `.admin-password`.

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
