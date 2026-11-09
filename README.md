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
## Dados do jogo

| Onde | Conteúdo | Quem lê |
|---|---|---|
| `tmsrv.yaml` | opções e tabelas pequenas: cidades, portões, NPCs de troca, recompensa diária, roleta, bônus de experiência | TMSRV (e DBSRV e painel, em parte) |
| `config/` | `language.yaml`, `missions.yaml`, `npc_generators.yaml` | TMSRV e painel |
| Banco, schema `gamedata` | itens, skills, drops, teleportes, mapas (`HeightMap.dat`, `AttributeMap.dat`), `ItemList.bin` do cliente e modelos de NPC e de classe | TMSRV, DBSRV e painel |
| Banco, tabela `account_locks` | bloqueio de conta | TMSRV e painel |

A recompensa diária de cada conta é salva junto com a conta. A pasta de
`config/` pode ser trocada pela flag `-config-dir` (tmsrv e admin).

Para substituir um binário (um `ItemList.bin` novo, modelos saídos de um editor
de NPC), use `admin world-import`; só o que for informado é alterado:

```bat
go run ./cmd/admin world-import -itemlist ItemList.bin
go run ./cmd/admin world-import -heightmap HeightMap.dat -attributemap AttributeMap.dat -npc npc -npc-base npc_base -class classes
```

O `ItemList.bin` vale em alguns segundos. Mapas e modelos valem quando a TMSRV
(e, para as classes, o DBSRV) reiniciar.

Só o `xml-import`, que migra uma vez as contas do DBsrv C++ (arquivos com
dados de jogadores), ainda aponta para a pasta antiga `../Release Servidor/DBSRV`.

## Como rodar (Windows)

```bat
servidor            :: sobe o Postgres, compila e inicia DBSRV, TMSRV e painel
servidor restart    :: depois de atualizar o código
servidor stop
servidor senha-admin
```

Um servidor novo começa com o banco vazio: o DBSRV e os canais não sobem até
receberem os dados. Abra o painel, que sobe sozinho, e importe um arquivo
gerado em **Exportar** no painel de outro servidor (menu Exportar > Importar).
Exportações feitas antes da mudança para YAML (versão 1) não são aceitas.

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
