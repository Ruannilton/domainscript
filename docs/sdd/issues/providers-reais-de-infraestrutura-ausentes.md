# Providers reais de infraestrutura ausentes (G-4) (ex-ISSUE-3)
- SPEC: [codegen](../specs/codegen/requirements.md)
- TASK: [gaps.md §G-4](../specs/codegen/gaps.md) (Marcos F/G — providers
  reais de infraestrutura)
- DESCRIPTION: Tudo está atrás de seams limpos (NFR-12 respeitado), mas a única
  dependência externa real por categoria é sqlite — o sistema gerado hoje
  **não é implantável contra infraestrutura real** além disso. Categorias em
  aberto (numeração §N em v6; hoje deslocada +1 em `domainscript-spec-v7/`):
  Database (spec pede Postgres §12, hoje
  [`13-module-infra.md`](../steerings/domainscript-spec-v7/13-module-infra.md);
  só `"sqlite"` é adapter real, `"postgres"`/`"mongodb"` são rótulos
  decorativos — [sql_wiring.go](../../../codegen/sql_wiring.go));
  Canais (`grpc`/`http`/`stream` §11, hoje
  [`12-topology.md`](../steerings/domainscript-spec-v7/12-topology.md) →
  erro de geração; provider `rabbitmq` não existe, só `direct`/`queue`
  in-memory — [channel_test.go](../../../codegen/channel_test.go));
  Cache backend (`redis`/`layered` §15, hoje
  [`16-cache.md`](../steerings/domainscript-spec-v7/16-cache.md) → só
  in-memory); RateLimit backend (`redis` §16, hoje
  [`17-rate-limiting.md`](../steerings/domainscript-spec-v7/17-rate-limiting.md)
  → só in-memory); FileStorage (`"s3"` §12, hoje
  [`13-module-infra.md`](../steerings/domainscript-spec-v7/13-module-infra.md)
  → seam in-memory); Idempotency storage (`external` Redis/Dynamo §14, hoje
  [`15-idempotency.md`](../steerings/domainscript-spec-v7/15-idempotency.md)
  → só `same` in-memory,
  [`codegen/rtsrc/idempotency.go.txt`](../../../codegen/rtsrc/idempotency.go.txt));
  Outbox (durabilidade real §12, hoje `13-module-infra.md` → in-memory).
  Fechar exige um provider real por vez, opt-in e isolado (padrão já existe:
  [`codegen/sqlrt/`](../../../codegen/sqlrt), `grpcrt/`, `otelrt/`). Postgres ou
  rabbitmq primeiro (validam os seams mais centrais). Nota: o seam `Dialect`
  (REQ-40, read-side/I7.0,
  [`codegen/sqlrt/dialect.go.txt`](../../../codegen/sqlrt/dialect.go.txt)) já
  reduz o custo da parte SQL — adicionar banco vira "implementar `Dialect` +
  entrada no registro"; o restante (driver real, migrations, type mapping)
  segue aberto.

  EM ANDAMENTO (spec criada):
  [`infra-providers`](../specs/infra-providers/requirements.md) (Marco J,
  REQ-41..48 / NFR-21..24) tratou esta issue com **recorte de 5 providers** —
  Postgres, RabbitMQ, Redis (Cache+RateLimit), S3 e Outbox durável. As demais
  categorias de G-4 (outros bancos, gRPC-canal, Dynamo para idempotency
  `external`, backend `layered` de cache, GCS/Azure) ficam explicitamente fora
  do recorte, para ciclos futuros.

  FECHADA PARCIALMENTE (Marco J concluído, J7.1): as 5 categorias do recorte
  têm provider real — Postgres (J1, [`codegen/sqlrt`](../../../codegen/sqlrt) +
  [sql_wiring.go](../../../codegen/sql_wiring.go) — não há pacote `pgrt`
  separado, citado por engano ao registrar esta issue), RabbitMQ (J3,
  [channel_rabbitmq.go](../../../codegen/channel_rabbitmq.go)), Redis
  Cache+RateLimit (J4, [`codegen/redisrt`](../../../codegen/redisrt)), S3
  FileStorage (J5, [`codegen/s3rt`](../../../codegen/s3rt)), Outbox durável
  (J2, `runtime.DurableOutbox`/`sql_wiring.go:emitOutboxDatabaseWiring`) —
  todos opt-in, isolados atrás do seam existente, cobertos por golden +
  smoke compile (NFR-17) e determinismo (NFR-21, `infra_providers_
  determinism_test.go`). Ver [gaps.md](../specs/codegen/gaps.md) §G-4 para a
  tabela completa antes/depois por categoria.

  Residual que sobrou do Marco J (rastreado à parte, já resolvido): o lado
  PRODUTOR do Outbox→canal cross-service (REQ-42.6) publicava direto no
  commit em vez de enfileirar no outbox — isso foi fechado pelo Marco K
  ([`correcoes-issues-9-10-11`](../specs/correcoes-issues-9-10-11/requirements.md),
  ex-ISSUE-9, RESOLVED nos commits
  `1137ba9`/`e2f3ec9`/`9fd30f0`/`c580e1f`).

  Ainda em aberto (não fechado por nenhum ciclo até agora): a
  vendorização/build offline real (R10) nunca foi implementada — os smoke
  tests usam `go mod tidy` (rede), não `-mod=vendor` genuíno. As categorias
  explicitamente fora do recorte do Marco J (outros bancos, gRPC-canal,
  Dynamo, `layered` cache, GCS/Azure) continuam abertas para um ciclo
  futuro.
- SOLVED: FALSE

# Solução proposta

## Veredito

Real, mas o resíduo de hoje não é o que o texto acima descreve. As 5 categorias do
recorte do Marco J estão de fato fechadas — verificado hoje: `sqlProviders` tem
`"sqlite"` e `"postgres"` reais ([sql_wiring.go](../../../codegen/sql_wiring.go):47-68),
com `PostgresDialect` completo ([dialect_postgres.go.txt](../../../codegen/sqlrt/dialect_postgres.go.txt):23-80)
e `OpenPostgres` atrás de pgx ([open_postgres.go.txt](../../../codegen/sqlrt/open_postgres.go.txt):1-30);
os quatro registros de categoria têm uma entrada real cada
([provider_registry.go](../../../codegen/provider_registry.go):69-107);
`wallet/mod.ds`:22-40 e `shop/orders/mod.ds`:4-7 já declaram `provider: "postgres"`,
ou seja as fixtures exercitam o caminho real. O que sobrou são **três lacunas de
catálogo, um defeito de isolamento não registrado, e uma exigência nova que a
revisão de 2026-07-31 acabou de criar**.

**nota do desenvolvedor:** vamos utilizar o GORM internamente para diminuir a complexidade de desenvolvimento e ao mesmo tempo ter suporte a vários bancos de dados

**Inventário verificado (2026-07-31).**

| Categoria / rótulo | Estado | Evidência |
|---|---|---|
| Database `sqlite` | real | [sql_wiring.go](../../../codegen/sql_wiring.go):48-54; `sqlite`Dialect em [dialect.go.txt](../../../codegen/sqlrt/dialect.go.txt):94-152 |
| Database `postgres` | real | [sql_wiring.go](../../../codegen/sql_wiring.go):61-67; [dialect_postgres.go.txt](../../../codegen/sqlrt/dialect_postgres.go.txt) |
| Database `mysql`/`sqlserver`/`mongodb`/`cassandra` | **ausente e SILENCIOSO** — rótulo não reconhecido faz `programNeedsSQLAdapter` devolver false e o projeto inteiro cair em memória, sem diagnóstico | [sql_wiring.go](../../../codegen/sql_wiring.go):113-115; `pizzeria/kitchen/mod.ds`:10 (`provider: "mongodb"`); zero regras de provider em `sema/`/`resolver/` (grep só bate em `_test.go`) |
| Canal `direct` | in-memory, não precisa provider | [channel.go](../../../codegen/channel.go):26 |
| Canal `queue` + `rabbitmq` | real | [provider_registry.go](../../../codegen/provider_registry.go):69-71; [channel_rabbitmq.go](../../../codegen/channel_rabbitmq.go); `amqprt/rabbitmq.go.txt` |
| Canal `grpc`/`http`/`stream` | ausente, **erro de geração alto** (a única categoria que falha em voz alta) | [channel.go](../../../codegen/channel.go):66-72, 288-308 |
| Cache Redis | adapter real, mas **selecionado por um rótulo que a spec não enumera**: o gerador casa `backend == "redis"`; [16-cache.md](../steerings/domainscript-spec-v7/16-cache.md):18 lista `memory`/`distributed`/`layered`, e [13-module-infra.md](../steerings/domainscript-spec-v7/13-module-infra.md):32-40 só mostra `redis` como `type` de uma **layer** dentro de `backend: layered` | [decl_query_cache.go](../../../codegen/decl_query_cache.go):309-317, 412-425; `redisrt/cache.go.txt` |
| Cache `layered` / `distributed` | ausente | [16-cache.md](../steerings/domainscript-spec-v7/16-cache.md):18 |
| RateLimit Redis | real, com fallback local | [ratelimit.go](../../../codegen/ratelimit.go):223-231, 634-642 |
| FileStorage `s3` | real; rótulo desconhecido → memória silenciosa | [provider_registry.go](../../../codegen/provider_registry.go):105-107; [decl_filestorage.go](../../../codegen/decl_filestorage.go):84 |
| FileStorage GCS/Azure | ausente | — |
| Idempotency `same` | in-memory real | `rtsrc/idempotency.go.txt` |
| Idempotency `external` | ausente — e o **bloco `Idempotency` do `mod.ds` nunca é lido pelo codegen** (só `uc.Idempotency` do UseCase, [codegen.go](../../../codegen/codegen.go):619): `storage: external` é aceito e descartado sem ruído | [usecase_idempotency.go](../../../codegen/usecase_idempotency.go):241 |
| Outbox durável | real | [sql_wiring.go](../../../codegen/sql_wiring.go):307-351, 436-520 |
| Vendorização offline (R10) | ausente | [ci.yml](../../../.github/workflows/ci.yml):83-87 (`go mod tidy && go build`, com rede) |

**Defeito novo, não registrado em lugar nenhum — NFR-12 quebrado no eixo Database.**
`generateSQLRuntimeFiles()` não recebe nenhum parâmetro e copia **todos** os
`*.go.txt` de `sqlrt.Sources()` sempre que *qualquer* provider SQL está ativo
([sql_wiring.go](../../../codegen/sql_wiring.go):212-228; `embed.go`:24-42 não filtra).
`sqlruntime/open_postgres.go` importa `github.com/jackc/pgx/v5/stdlib`
([open_postgres.go.txt](../../../codegen/sqlrt/open_postgres.go.txt):16), mas `EmitGoMod`
só emite `require` para os providers **ativos** ([project.go](../../../codegen/project.go):226).
Logo, um projeto que declare **só** `provider: "sqlite"` emite um arquivo que
importa pgx sem o `require` correspondente: `go build` puro quebra, e o
`go mod tidy` que o job `fixtures` roda antes do build mascara isso puxando pgx —
uma dependência externa que o programa nunca declarou, exatamente o que NFR-12
proíbe. As demais categorias **filtram** por `adapterDir`
([provider_runtime.go](../../../codegen/provider_runtime.go):37-48); só Database não.
Sem cobertura: [sql_adapter_test.go](../../../codegen/sql_adapter_test.go):221-243
afirma a *presença* de `modernc.org/sqlite` e nunca a *ausência* de pgx. O
comentário de `openFunc` ([sql_wiring.go](../../../codegen/sql_wiring.go):31-38)
documenta a não-filtragem como premissa de design, mas não sua consequência em
`go.mod`. Este é o item mais barato e mais grave do resíduo.

**Exigência nova de §2.7 (revisão de hoje).** [02-type-system.md](../steerings/domainscript-spec-v7/02-type-system.md):256
define mapeamento de storage por representação de identidade — `uuid` → tipo
nativo quando o provider suporta, senão `char(36)`; `string` → `varchar(256)`;
`integer` → 64 bits **com sequence quando `generation: system`** (:246, :254,
que ainda exige o `Database` de `storage.state`). O seam `Dialect` de hoje
([dialect.go.txt](../../../codegen/sqlrt/dialect.go.txt):13-90) não tem superfície
alguma para isso: `aggregate_id` é `TEXT` fixo nos dois dialetos (:106 e
[dialect_postgres.go.txt](../../../codegen/sqlrt/dialect_postgres.go.txt):51). Nada
disso é implementável hoje — `identity`, `ref T` e `new_ref` não existem em
nenhum ponto do código (grep vazio em `parser/`, `ast/`, `program/`, `types/`,
`sema/`, `codegen/`): é ciclo de front-end primeiro. O impacto **sobre esta
issue**, porém, é imediato e de projeto: "provider real" deixa de ser um
predicado booleano e passa a ser um **conjunto de capacidades** (tipo `uuid`
nativo? sequence monotônica?). O `Dialect` vai ganhar métodos, e todo dialeto
escrito antes disso terá de ser reaberto.

## Causa raiz

O registro de provider do Marco J (REQ-46) generalizou as duas mecânicas
*mecânicas* — `require` em `go.mod` e cópia de fontes — mas não generalizou nem
a **validação do rótulo** (desconhecido cai em silêncio para in-memory em todas
as categorias exceto canal) nem o **filtro por provider no eixo Database**; e o
vocabulário de seleção que o gerador adotou (`backend: "redis"`,
`provider: "mongodb"`) nunca foi confrontado com o catálogo da spec.

## Priorização

Critério, nesta ordem: **(1)** risco de perda silenciosa de dado em produção >
**(2)** superfície da spec desbloqueada por unidade de trabalho > **(3)** custo
marginal do próximo provider. Deliberadamente **não** priorizo "mais um banco":
o seam `Dialect` já foi provado duas vezes, nenhum exemplo ou fixture pede um
terceiro, e §2.7 acaba de mudar o preço da interface.

- **P0 — nenhum provider novo: parar de falhar em silêncio.** (a) diagnóstico
  para rótulo desconhecido; (b) filtro por provider em `sqlrt`. Remove a classe
  inteira de falha silenciosa e impede que o custo do provider nº 3 seja pago
  por *todos* os projetos. Critério (1) e (3).
- **P1 — alinhar o vocabulário de seleção ao catálogo da spec (Cache,
  RateLimit).** Custo zero de driver: o adapter Redis real já existe e hoje é
  **inalcançável a partir de fonte conforme à spec**. Maior desbloqueio por
  unidade de trabalho de todo o inventário. Critério (2). *Bloqueado por decisão
  de spec — ver abaixo.*
- **P2 — Cache `layered`.** É o único backend de cache que a spec de fato
  descreve ([13-module-infra.md](../steerings/domainscript-spec-v7/13-module-infra.md):32-40)
  e que não existe; reusa o `redisruntime` já pronto como camada, sem driver novo.
- **P3 — Idempotency `external`.** Mesmo `redisruntime`; fecha §15 e, de quebra,
  faz o bloco `Idempotency` do `mod.ds` deixar de ser lido-e-descartado.
- **P4 — inventário, sem rota proposta aqui:** MySQL/SQL Server (`Dialect` +
  entrada de registro — barato, mas espere §2.7), gRPC/HTTP/stream como canal,
  Mongo/Cassandra (**não cabem no seam `Dialect`**, ver alternativas),
  GCS/Azure Blob, e R10 (vendorização offline, ortogonal a providers).

## Solução proposta

**P0.a — rótulo desconhecido deixa de ser silencioso.** `Generate` já recebe o
`*diag.DiagnosticBag` ([codegen.go](../../../codegen/codegen.go):81-83) e hoje só o
consulta; o canal para um **warning de geração** existe estruturalmente e nunca
foi usado. Emitir um warning por rótulo não reconhecido em Database, FileStorage,
Cache, RateLimit e `Idempotency.storage` — "provider `X` não reconhecido: o
projeto gerado persiste em memória". Warning, não erro, como primeiro passo: um
erro faria `dsc gen pizzeria` falhar por um motivo novo (`provider: "mongodb"`,
`pizzeria/kitchen/mod.ds`:10) e obrigaria a mexer no `KNOWN_UNGENERATABLE` de
[ci.yml](../../../.github/workflows/ci.yml):66-69, que exige uma issue por entrada.
Promover a erro quando o catálogo estiver estabilizado. Note que a decisão mora
no **back-end**, não no checker: §13 não fecha o catálogo de `provider:`, e o
gerador é quem sabe o que sabe montar.

**P0.b — filtrar as fontes de `sqlrt` por provider ativo.** `activeSQL` já está
calculado em `Generate` ([codegen.go](../../../codegen/codegen.go):89) e a chamada de
`generateSQLRuntimeFiles()` está na linha 105: passá-lo é trivial. A separação
natural é por sufixo de arquivo, auto-mantida e sem campo novo no registro:
`dialect_<p>.go.txt` e `open_<p>.go.txt` entram só para os providers ativos; o
núcleo comum (`eventstore`, `uow`, `collection`, `twophase`, `outbox`) entra
sempre — **mesma mecânica que `generateProviderRuntimeFiles` já usa por
`adapterDir`** ([provider_runtime.go](../../../codegen/provider_runtime.go):37-48).
Pré-requisito obrigatório: [dialect.go.txt](../../../codegen/sqlrt/dialect.go.txt)
hoje mistura a **interface** `Dialect` (:13-90) com a **implementação**
`sqliteDialect` (:94-152) no mesmo arquivo — sem separá-los antes, um projeto
postgres-only deixa de compilar. Essa separação é uma task por si.

**P1 — seletor de backend distribuído.** Manter o adapter Redis intacto e mudar
só o **seletor**: `backend: distributed` + `connection: env(...)` resolve para o
adapter distribuído (hoje o único é Redis), e `backend: layered` com
`layers: [{ type: memory, ... }, { type: redis, ... }]` resolve o Redis como
camada. NFR-12 permanece intacto — o driver continua entrando em `go.mod` só
quando o programa o declara, porque `activeProviderDeps`
([provider_registry.go](../../../codegen/provider_registry.go):129-205) continua sendo a
fonte única; muda apenas *de onde* ele lê o rótulo. Não implementável sem a
decisão de spec listada em Bloqueios.

## Alternativas descartadas

- **Adicionar MySQL/SQL Server agora** ("o seam existe, é barato"). Perde por
  pagar o custo antes do benefício: nenhum exemplo pede, o seam já foi provado
  duas vezes, e §2.7 vai reabrir a interface `Dialect` com
  `IdentityColumnType`/sequence — um terceiro dialeto escrito hoje é reescrito
  depois.
- **Mongo/Cassandra atrás do mesmo `Dialect`.** Perde porque `Dialect` é uma
  interface de **strings SQL** (`Placeholder`, `CreateEventsTable`,
  `JSONFieldEq` — [dialect.go.txt](../../../codegen/sqlrt/dialect.go.txt):13-90): um
  document store não implementa `CreateEventsTable() string` sem farsa. Precisa
  de um seam irmão no nível de `EventStore`/`Collection`, não de uma entrada de
  registro. A frase de [gaps.md](../specs/codegen/gaps.md):151-155 ("estruturalmente
  o mesmo trabalho") vale só para bancos SQL e deve ser corrigida quando esta
  issue for fechada.
- **Fazer o `sema` recusar provider desconhecido.** Perde por NFR-6 e por §13: o
  catálogo de providers é propriedade do back-end; codificá-lo no checker faria
  a *validade* de um programa depender da versão do gerador.
- **Deixar o rótulo decorativo como está e só documentar.** Perde porque o modo
  de falha é perda de dado em produção com o serviço "no ar" — mesmo perfil de
  risco de G-5, e mais provável, porque `provider: "mongodb"` parece
  deliberadamente configurado.
- **Resolver o vazamento de pgx elevando pgx a dependência fixa do
  `sqlruntime`.** Perde por NFR-12, textualmente.

## Raio de alcance

- **Goldens.** P0.b muda a *lista de arquivos* de todo projeto com Database
  sqlite (some `sqlruntime/open_postgres.go` e `dialect_postgres.go`) e de todo
  projeto postgres-only (some `open_sqlite.go`, e `dialect.go` se dividido).
- **Fixtures.** `wallet`, `shop` e `pizzeria` são **todos** postgres — nenhuma é
  sqlite-only, então seus goldens não mudam e a byte-identidade (NFR-13) fica
  preservada nos três. Quem muda são os testes sintéticos de sqlite
  ([sql_adapter_test.go](../../../codegen/sql_adapter_test.go), projeto `ledger`).
- **`go.mod` do projeto gerado.** Só-sqlite deixa de arrastar pgx; wallet/shop
  inalterados. P0.a e P1 não mexem em `go.mod` de nada que já funcione.
- **Job `fixtures`.** P0.a como *warning* não muda o job. Como *erro*, exigiria
  atualizar o comentário de `KNOWN_UNGENERATABLE`
  ([ci.yml](../../../.github/workflows/ci.yml):64-69).
- **NFR-13.** Nenhuma rota introduz não-determinismo: `activeSQLProviders` e
  `generateProviderRuntimeFiles` já ordenam.

**Como testar um provider real em CI sem infra externa.** O padrão vigente é o
certo e deve ser mantido: teste de integração guardado por variável de ambiente,
`t.Skip` quando ausente, nunca falha
([channel_rabbitmq_integration_test.go](../../../codegen/channel_rabbitmq_integration_test.go):84,
[redis_ratelimit_integration_test.go](../../../codegen/redis_ratelimit_integration_test.go):43) —
o que sustenta a CI de verdade é o par golden + smoke compile (NFR-17) sobre os
bytes efetivamente escritos. Três reforços possíveis, em ordem de custo:

1. **sqlite como banco de prova de conformidade de `Dialect`** — driver puro-Go,
   sem processo externo. Um teste de conformidade que roda o *mesmo* roteiro de
   operações contra qualquer implementação de `Dialect` cobre a semântica
   compartilhada; o dialeto remoto só precisa do teste guardado por env para o
   que é genuinamente específico (`FOR UPDATE SKIP LOCKED`, `JSONB`).
2. **`services:` do runner do GitHub Actions** — sobe postgres/redis/rabbitmq em
   container, sem infra própria e sem segredo nenhum. É a rota que promove os
   testes hoje **pulados** a executados, e custa apenas editar `ci.yml`. É a
   resposta canônica para "provider real testado em CI".
3. **Fake em processo do protocolo** (RESP é simples; AMQP não é) — traz
   dependência de teste ao `go.mod` do *compilador*, nunca ao do artefato
   gerado, o que é aceitável. Não vale a pena para Postgres.

R10 (build offline com `-mod=vendor`) é ortogonal: o que ele testa é
hermeticidade, não o provider — resolvível materializando um `vendor/` a partir
do cache de módulos e removendo a rede da fase de build.

## Bloqueios

1. **(crítico, bloqueia P1)** §16 × §13 × gerador divergem sobre o **rótulo** do
   backend distribuído de Cache: [16-cache.md](../steerings/domainscript-spec-v7/16-cache.md):18
   enumera `memory`/`distributed`/`layered`; [13-module-infra.md](../steerings/domainscript-spec-v7/13-module-infra.md):32-40
   usa `layered` com `layers[].type: redis`; o gerador casa `backend == "redis"`.
   A spec precisa decidir se `distributed` ganha uma chave de provider, ou se
   §16 passa a nomear o backend concreto alinhando-se a §13. A issue vizinha
   [cache-ratelimit-backend-exige-string-contra-spec](cache-ratelimit-backend-exige-string-contra-spec.md)
   cobre a **forma** do valor (identificador nu × literal string); o **rótulo**
   é a metade não coberta e merece issue de revisão de spec própria.
2. **(bloqueia P1 parcialmente)** [17-rate-limiting.md](../steerings/domainscript-spec-v7/17-rate-limiting.md)
   não enumera backends em lugar nenhum — só o exemplo de §13:42-47 mostra
   `backend: redis`. Decidir se o catálogo é o mesmo de Cache.
3. **(bloqueia P4 e o desenho definitivo do `Dialect`)** §2.7 exige decidir
   (a) quais capacidades definem um provider apto — tipo `uuid` nativo? sequence
   monotônica? — e (b) o que acontece quando o provider declarado não as tem.
   §2.7:256 dá a degradação para `uuid` ("senão `char(36)`"), mas nada diz para
   `integer` + `generation: system` num provider sem sequence: erro de
   compilação, erro de geração, ou degradação. Sem isso, todo dialeto novo
   nasce com data de reabertura.
4. **(bloqueia P3)** [15-idempotency.md](../steerings/domainscript-spec-v7/15-idempotency.md):11
   diz "`external` (Redis/Dynamo)" em prosa e não define **qual chave do `mod.ds`
   escolhe entre os dois**.

## Fatiamento sugerido

**T1 — `sqlruntime`: separar interface de implementação.** Extrair
`sqliteDialect` de `dialect.go.txt` para `dialect_sqlite.go.txt`, deixando em
`dialect.go.txt` só a interface `Dialect`. Puramente mecânico, sem mudança de
comportamento; habilita T2. `target_files`: `codegen/sqlrt/dialect.go.txt`,
`codegen/sqlrt/dialect_sqlite.go.txt` (novo), `codegen/sql_wiring.go` (doc de
`openFunc`), `codegen/sql_adapter_test.go`.

**T2 — filtrar `sqlrt` por provider ativo (fecha o vazamento de pgx, NFR-12).**
`generateSQLRuntimeFiles(activeSQL)`; `dialect_<p>`/`open_<p>` só para os
ativos; teste negativo obrigatório (projeto sqlite-only **não** emite
`open_postgres.go` e seu `go.mod` **não** menciona `jackc/pgx`) ao lado do
positivo já existente. `target_files`: `codegen/sql_wiring.go`,
`codegen/codegen.go`, `codegen/sqlrt/embed.go`, `codegen/sql_adapter_test.go`,
`codegen/provider_registry_gomod_test.go`.

**T3 — warning de geração para rótulo de provider não reconhecido.** Database,
FileStorage, Cache, RateLimit e `Idempotency.storage`, via o `*diag.DiagnosticBag`
que `Generate` já recebe. Um par positivo/negativo por categoria.
`target_files`: `codegen/codegen.go`, `codegen/sql_wiring.go`,
`codegen/decl_filestorage.go`, `codegen/decl_query_cache.go`,
`codegen/ratelimit.go`, `codegen/usecase_idempotency.go`,
`codegen/provider_registry_test.go`.

**T4 — promover os testes de integração hoje pulados a executados em CI.**
`services:` (postgres, redis, rabbitmq) num job novo que exporta
`DATABASE_URL`/`REDIS_URL`/`AMQP_URL`; os `t.Skip` existentes passam a rodar sem
nenhuma mudança de código de teste. `target_files`:
`.github/workflows/ci.yml`.

**T5 — (só após o Bloqueio 1 ser decidido) alinhar o seletor de Cache/RateLimit
ao catálogo da spec.** Adapter Redis inalterado; muda `cacheBackendKind`/
`rateLimitBackendKind` e o `mod.ds` de `wallet`. `target_files`:
`codegen/decl_query_cache.go`, `codegen/ratelimit.go`,
`codegen/provider_registry.go`, `testdata/projects/wallet/mod.ds`,
`codegen/redis_cache_test.go`, `codegen/redis_ratelimit_test.go`.
