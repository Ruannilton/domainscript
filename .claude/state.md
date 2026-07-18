# State

Rastreio do estado de cada `spec::task`, para retomar o desenvolvimento caso
a execução seja interrompida. Atualizado ao final de **cada task concluída**
(ver `CLAUDE.md`) — nunca em lote no fim de uma spec inteira.

Convenção de status: `done` | `in-progress` | `pending` | `blocked`.

## Resumo por spec

| Spec | Diretório | Status | Próxima task |
|---|---|---|---|
| transpilador (front-end, REQ-1..8) | `.claude/specs/transpilador/` | done | — |
| type-checking (REQ-9..13) | `.claude/specs/type-checking/` | done | — |
| codegen (back-end, REQ-14..32) | `.claude/specs/codegen/` | done | — |
| read-side (REQ-33..40) | `.claude/specs/read-side/` | done | — |
| infra-providers (REQ-41..48) | `.claude/specs/infra-providers/` | in-progress | J3.3 |

## transpilador — `.claude/specs/transpilador/tasks.md`

Fases 0–11 completas (setup, léxico, parser/AST, config/test, símbolos,
agregação de programa, regras locais e cross-file, driver/CLI, robustez e
determinismo). Nenhuma task pendente.

## type-checking — `.claude/specs/type-checking/tasks.md`

Fases A–F completas (resolução de nomes em corpos, refs de config, modelo de
tipos, acesso a membro, compatibilidade de tipos, códigos de diagnóstico).
Nenhuma task pendente.

## codegen — `.claude/specs/codegen/tasks.md`

Marcos E–H completos (núcleo transacional, reações/coordenação, infra real,
exposição/observabilidade avançadas + testes). Nenhuma task pendente.
`gaps.md` documenta divergências conhecidas entre o spec da linguagem e o
que o back-end entrega — não são tasks pendentes desta spec.

## read-side — `.claude/specs/read-side/tasks.md`

Marco I ("Read Side de Verdade"), REQ-33..40. Fases I0–I6 concluídas (seam no
runtime, predicado falível, `orderBy`/`skip`/`take`, `load X(id).entries` +
`as V`, operador `in`, `join` mesmo-banco — âncora 2 `GetMyTickets` verde,
`distinct`/`sum`/`focus` (§20) e âncora 3 — fixture Policy §7
(`RefundAllOnEventCancelled`) des-adaptada para a forma canônica do spec
(3 tickets, 2 orders, `soldTickets.distinct(t => t.orderId)`, `emitted count
2`), `codegen/gentest_policy_test.go`). `orderBy`/`skip`/`take` pós-join
(I5.1) e o VO wrapper `RefundReason` no lugar do `string` cru do spec (I6.2)
ficaram fora do escopo — desvios registrados nos `tasks.md`.

Concluído: **I7.0** — seam `Dialect` (`codegen/sqlrt/dialect.go.txt`:
`Placeholder`/`CreateEventsTable`/`CreateCollectionTable`/`LimitOffset`,
`SQLiteDialect`), consumido por `eventstore.go.txt`/`uow.go.txt`/
`twophase.go.txt` (nenhuma string SQL específica de banco fora do dialeto);
registro único de provider (`sqlProviders`/`recognizedSQLProvider`,
`codegen/sql_wiring.go`) substitui as três comparações duplicadas de
`db.Provider == "sqlite"` (`programNeedsSQLAdapter`, `usecase2PCPlan` em
`decl_usecase.go`, driver/versão em `EmitGoMod`); prova de plugabilidade em
`codegen/sql_dialect_test.go` (`TestSQLEventStoreDialectPluggability`) — a
mesma suíte comportamental roda contra `SQLiteDialect` (`?`) e um dialeto de
teste posicional (`$N`, nunca registrado como provider real) sobre o mesmo
driver sqlite.

Concluído: **I7.1** — `Collection[T]` sobre tabela no adapter sqlite
(`codegen/sqlrt/collection.go.txt`, novo): tabela genérica `(id TEXT,
payload TEXT JSON)` via `Dialect.CreateCollectionTable`; `Select`/`Count`
descem `WhereEq` (`WHERE json_extract(payload,'$.<campo>') = ?`) e
delegam o resto a `runtime.SelectSlice`. Lowering nova em
`codegen/lower/whereeq.go`: popula `Query[T].WhereEq` quando o `where`
inteiro é um AND de igualdades de campo contra valor independente do item
(campo comparável — mesma régua de `in`). `orderBy`/`skip`/`take` NUNCA
descem (desvio documentado no `tasks.md`: `json_extract` compararia
datetime/decimal/duration/size incorretamente como texto). Testes pareados
em `codegen/sql_collection_test.go`, unitários em
`codegen/lower/whereeq_test.go`.

Concluído: **I8.1** — Revisão contra a DoD
(`.claude/specs/read-side/requirements.md` §5): os três exemplos-âncora
geram/compilam/se comportam corretamente (golden+smoke+comportamental), a
fixture §22.4 está des-adaptada, o in-memory segue sem deps externas com o
adapter sqlite traduzindo `where`-igualdade/`orderBy`/`skip`/`take`/`count`
via `Dialect` (testes pareados), `go build`/`go test` verdes, wallet/shop sem
regressão. `.claude/specs/codegen/gaps.md` atualizado: G-1/G-2/G-8 marcados
fechados com ponteiro para este ciclo, item §22.4 de G-7 atualizado, nota de
G-4 atualizada para o `Dialect` já entregue; `README.md`/`CLAUDE.md`
atualizados para refletir o Marco I completo. **Marco I — Read Side de
Verdade — fechado.**

## infra-providers — `.claude/specs/infra-providers/tasks.md`

Marco J ("Providers Reais de Infraestrutura"), REQ-41..48 / NFR-21..24. Nasce
do gap G-4 (`.claude/specs/codegen/gaps.md`) / ISSUE-3, com **recorte explícito
de 5 providers**: Postgres (Database), Outbox durável, RabbitMQ (canal
cross-service), Redis (Cache + RateLimit) e S3 (FileStorage). Cada categoria já
tem o seam pronto (read-side/codegen) — o trabalho é implementar o lado real
atrás dele e generalizar o registro de provider (REQ-46). J1–J5 são
independentes após J0 (exceto J2, que depende de J1). Ver `tasks.md` para o
mapa de dependências.

Concluído: **J0.1** — `codegen/provider_registry.go` (novo): tipo `providerDep
{module, version, minGo, adapterDir, ctor}` (generalização de `sqlProvider`,
I7.0, para as categorias Canal/Cache/RateLimit/FileStorage — Database
continua em `sqlProviders`, que tem `dialectCtor`, um campo específico de
SQL) + os quatro mapas do registro (`channelProviders`/`cacheProviders`/
`rateLimitProviders`/`fileProviders`), vazios nesta task (cada entrada real
chega em J1..J5). `activeProviderDeps(prog)` varre `prog.Channels`
(`provider:`), o bloco `Cache`/`RateLimit` de cada módulo (`backend:`, via
`moduleCacheBlock`/`moduleRateLimitBlock` já existentes) e cada
`mod.FileStorages` (`provider:`), resolve contra os registros e devolve a
lista deduplicada (pela `providerDep` inteira — struct totalmente comparável;
dedup por só "module" OU só "adapterDir" foi tentada e descartada na revisão
da PR #11 por descartar precocemente providers distintos que só
compartilhassem um dos dois campos, R5) e ordenada por module com
`adapterDir` como desempate (NFR-23). Testes em
`codegen/provider_registry_test.go` (`package codegen`, para mutar os mapas
no teste de dedup): registro vazio ⇒ sempre vazio mesmo com
canal/Cache/RateLimit/FileStorage declarando providers desconhecidos; dois
mapas populados com a MESMA `providerDep` ⇒ uma entrada só; duas `providerDep`
com module igual e `adapterDir` diferente ⇒ as duas sobrevivem.

Concluído: **J0.2** — `EmitGoMod` (`codegen/project.go`) ganha um parâmetro
`providerDeps []providerDep`: cada dep ativa vira uma linha `require` no MESMO
bloco que os providers SQL/grpc/OTel já usam (ordenado por módulo,
determinismo NFR-13) e eleva o default de versão de Go para `dep.minGo`
quando não-vazio — mesma mecânica de `sqlProviderKeys`/`sqlProviders.minGoVersion`,
generalizada (REQ-46.2). `Generate` (`codegen.go`) passa
`activeProviderDeps(prog)`. Registros ainda vazios (J1..J5 não populados) ⇒
go.mod byte-idêntico ao de antes (NFR-21) — provado por
`TestEmitGoModNoProviderDepsUnchanged`; `TestEmitGoModWithProviderDepsAddsRequireAndBumpsVersion`/
`TestEmitGoModWithProviderDepsOrderedWithSQL` provam a adição/ordenação com uma
dep fake. Sem regressão nos guardas SQL existentes (`TestLedgerGoMod...`,
`TestWalletGoModStaysDependencyFreeAfterG1`) nem no smoke/comportamental do
ledger. Próxima: **J0.3** (gate de cópia de fontes por categoria).

Concluído: **J0.3** — `codegen/provider_runtime.go` (novo):
`generateProviderRuntimeFiles(deps []providerDep) ([]File, error)`, a versão
genérica de `generateSQLRuntimeFiles` (`sql_wiring.go`) para as categorias
Canal/Cache/RateLimit/FileStorage (Database continua à parte, em
`generateSQLRuntimeFiles`/`sqlProviders`). Para cada dep ativa, busca uma
função de fontes em `providerSources` (novo mapa em
`codegen/provider_registry.go`, chave `dep.adapterDir`) e copia cada arquivo
devolvido para `<adapterDir>/<nome>`, ordenado por caminho (NFR-13). `providerDep`
NÃO ganhou um campo `sources func(...)`: isso quebraria a comparabilidade
(`==`/chave de map) que `activeProviderDeps` usa para a dedup por struct
inteira (R5) — por isso `providerSources` é um registro à parte, indexado por
`adapterDir`, populado por cada categoria quando implementa seu adapter real
(J1..J5), sem exigir mudança em `generateProviderRuntimeFiles` nem em
`activeProviderDeps`. Uma dep cujo `adapterDir` não está em `providerSources`
é ignorada em silêncio (o caso de hoje: mapa vazio, nenhuma entrada de
J1..J5 ainda). `Generate` (`codegen.go`) chama
`generateProviderRuntimeFiles(activeProviders)` incondicionalmente, logo
após o bloco de `generateSQLRuntimeFiles` — hoje é sempre um no-op porque
`providerSources` está vazio. Testes em `codegen/provider_runtime_test.go`:
registro vazio ⇒ nenhum arquivo mesmo com deps ativas declaradas (deps
fake para amqp/redis); deps nil ⇒ nenhum arquivo; uma fonte fake registrada
⇒ copia os arquivos ordenados por nome; `adapterDir` não registrado ⇒
ignorado sem erro; erro de `sources()` ⇒ propagado (`errors.Is`). Sem
regressão: `TestWallet*`/`TestLedger*` (`codegen`) e `TestGenerate*`
(`driver`) continuam verdes, incluindo os guardas de determinismo/byte-
identidade (NFR-13/21) — `providerSources` vazio não altera nenhum projeto
gerado. Próxima: **J1.1** (dialeto Postgres, primeira subtask da Fase J1).

Concluído: **J1.1** — `PostgresDialect` (`codegen/sqlrt/dialect_postgres.go.txt`,
novo): implementa `Dialect` (REQ-41.1) com `Placeholder` posicional (`$N`),
`CreateEventsTable`/`CreateCollectionTable` com os tipos Postgres (`BIGINT`
para `sequence`, `JSONB` para `payload`, `TIMESTAMPTZ` para `recorded_at` —
decisão documentada no arquivo: preserva o fuso UTC que o valor Go já carrega,
ao contrário do `TIMESTAMP` naive do sqlite) e `LimitOffset` idêntico ao
sqlite (LIMIT/OFFSET é ANSI). Só o dialeto — nenhum driver `pgx`, nenhuma
entrada em `sqlProviders`, nenhuma mudança em `go.mod` (isso é J1.2). A
interface `Dialect` (`codegen/sqlrt/dialect.go.txt`) ganhou um QUINTO método,
`JSONFieldEq(field, placeholder string) string`, porque `whereEqClause`
(`codegen/sqlrt/collection.go.txt`) hardcodava a sintaxe `json_extract(...)`
do sqlite fora de qualquer `Dialect` — uma violação de REQ-40.1/41.1 que só
apareceria quando um segundo banco tentasse descer `WhereEq` (REQ-38). O
método agora encapsula a extração de campo JSON por banco: `sqliteDialect`
devolve `json_extract(payload,'$.<campo>') = <placeholder>` (string idêntica
à de antes — behavior-preserving, provado por
`TestSQLiteDialectJSONFieldEqUnchanged`); `postgresDialect` devolve
`payload->>'<campo>' = <placeholder>` (o operador de extração-como-texto do
Postgres, R7: o parâmetro do lado do chamador já é o valor Go stringificado,
igual ao sqlite, e `unsafeWhereEqPrimitives`
(`codegen/lower/whereeq.go`) continua uma lista ÚNICA dialeto-agnóstica —
confirmado por leitura, nenhuma mudança necessária nesse arquivo — então o
conjunto de tipos seguros é idêntico nos dois dialetos por construção).
Testes novos em `codegen/sql_postgres_dialect_test.go`
(`TestPostgresDialectSQLStrings`, que roda `TestPostgresDialectStrings` e
`TestSQLiteDialectJSONFieldEqUnchanged` de verdade num projeto Go efêmero via
`gentest.WriteFiles`/`RunTests`, mesmo padrão de
`TestSQLEventStoreDialectPluggability`) — puras asserções de string, nenhuma
conexão Postgres real aberta ou necessária. Sem regressão:
`codegen/sql_collection_test.go`/`sql_dialect_test.go`/`sql_tenancy_test.go`
(`go test ./codegen/ -run TestSQL`) e `driver` `TestGenerate*` (wallet/shop
e2e, NFR-19) continuam verdes — o refactor de `whereEqClause` não muda
nenhum byte gerado para sqlite. Próxima: **J1.2** (driver `pgx` real +
entrada em `sqlProviders` + `go.mod` opt-in).

Concluído: **J1.2** — driver Postgres real + registro + `go.mod` opt-in
(REQ-41.2/41.3). `codegen/sqlrt/open_postgres.go.txt` (novo):
`OpenPostgres(dsn) (*sql.DB, error)` via `sql.Open("pgx", dsn)` (driver
`github.com/jackc/pgx/v5/stdlib`) + `PingContext` fail-closed com
`context.WithTimeout(10s)` — nunca `context.Background()` (REQ-47.2): um
Postgres inalcançável falha o startup oportunamente. Nome **`OpenPostgres`**,
não `Open` — achado desta task: `generateSQLRuntimeFiles` (codegen.go) copia
TODO `sqlrt.Sources()` sempre que qualquer provider real está ativo (não
filtra por provider individual), então um projeto com Database "sqlite" E
"postgres" ativos ao mesmo tempo teria os dois `open_*.go.txt` no MESMO
pacote `sqlruntime` gerado — dois `func Open` colidiriam. `sqlProvider`
(`codegen/sql_wiring.go`) ganhou o campo `openFunc` (mesmo padrão de
`dialectCtor`): `"Open"` para sqlite (inalterado, zero regressão),
`"OpenPostgres"` para postgres; `emitXADatabaseWiring` usa
`provider.openFunc` em vez do literal `.Open(` que tinha antes (sqlite
continua emitindo `.Open(` byte-a-byte — só postgres muda). Registro:
`sqlProviders["postgres"] = {driverModule: "github.com/jackc/pgx/v5",
driverVersion: "v5.10.0", minGoVersion: "1.25", dialectCtor:
"PostgresDialect", openFunc: "OpenPostgres"}` (`project.go` ganhou as
constantes `postgresDriverModule`/`postgresDriverVersion`/
`postgresMinGoVersion` — v5.10.0 confirmada via `go mod download` exigir
`go >= 1.25.0`, mesma versão mínima que sqlite já exige, então
`maxGoVersion` não muda o default). `EmitGoMod`/`activeSQLProviders` já
eram genéricos (nenhuma mudança — só a entrada no registro basta,
REQ-40.2). Testes novos `codegen/sql_wiring_test.go` (pacote interno
`codegen`, J1.2.d): `TestActiveSQLProvidersRecognizesPostgres`
(case-insensitive), `TestActiveSQLProvidersUnrecognizedProviderIsNFR21NoOp`
(um provider não reconhecido, ex. `"pg"`, não ativa nada),
`TestEmitGoModRequiresPgxForPostgres`, `TestEmitGoModWithoutPostgresHasNoPgx`
(NFR-21).

**Ripple grande, não previsto no orçamento original da task, corrigido
dentro dela por ser causado diretamente por ela** (regra de "erro no escopo
da task atual"): `wallet`/`shop` (`docs/examples/`) declaram `provider:
"postgres"` DE VERDADE em `mod.ds` — antes de J1.2 isso era decorativo (só
"sqlite" era reconhecido), documentado explicitamente em vários comentários
como invariante ("wallet/shop nunca disparam esse caminho"). Registrar
"postgres" em `sqlProviders` torna essa suposição falsa: os dois exemplos
passam a ser programas SQL reais (ganham `sqlruntime/*` + `require
github.com/jackc/pgx/v5` em go.mod), o que quebrava testes que dependiam do
invariante antigo. Corrigido:
- `driver/generate_e2e_wallet_test.go`/`generate_e2e_shop_test.go`: novo
  helper `ensureModTidyIfNeeded` (roda `go mod tidy` quando go.mod tem
  `require`, mesma detecção de `gentest.needsModTidy`) chamado antes de
  `go build`/`go vet`/`go test` sobre a saída em disco — sem isso,
  `missing go.sum entry` falha o build. `TestGenerateWalletE2EGoModHasNoExternalRequire`/
  `TestGenerateShopE2EGoModHasNoExternalRequire` viraram
  `TestGenerateWalletE2EGoModRequiresPostgresDriver`/
  `TestGenerateShopE2EGoModRequiresPostgresDriver` (a asserção invertida é o
  comportamento correto agora, não uma regressão).
- `codegen/decl_aggregate_load_test.go:runGeneratedTests` (helper
  compartilhado por ~10 testes comportamentais do pacote, incl. os que usam
  o wallet real via `generateWalletProject`): mesmo fix de `go mod tidy`
  condicional, num único ponto.
- `codegen/sql_adapter_test.go:TestWalletGoModStaysDependencyFreeAfterG1` →
  `TestWalletGoModRequiresOnlyPostgresDriverAfterG1`: a asserção "NFR-12: sem
  require" não é mais verdadeira para o wallet real; reescrita para provar
  que exige EXATAMENTE `github.com/jackc/pgx/v5` (nenhum `modernc.org/sqlite`)
  e gera `sqlruntime/eventstore.go`.
- `codegen/decl_metric_test.go`/`ratelimit_test.go`/`tenancy_test.go`/
  `versioning_test.go`: 6 fixtures SINTÉTICAS (não wallet/shop) usavam
  `provider: "postgres"` só como rótulo inerte de propósito, para testar algo
  não relacionado (Metric/RateLimit/tenancy in-memory/versioning de evento)
  sem acionar o adapter SQL — renomeadas para `provider: "pg"` (a mesma
  convenção já usada em `sema/rules_audit_test.go`), restaurando a intenção
  original sem herdar uma dependência pgx irrelevante ao que cada teste
  prova.
- `program/graph.go` (doc de `Database.Provider`) e `CLAUDE.md` (invariante
  "Core vs. opt-in dependencies", NFR-12): atualizados — "postgres" não é
  mais decorativo, é o segundo provider real.
- Não tocado (fora do orçamento, passam mesmo assim via `gentest.SmokeCompile`,
  que já faz `go mod tidy` condicional): `channel_test.go`/`grpc_test.go`/
  `otel_test.go`/`idempotency_test.go`/`filestorage_test.go`/
  `decl_query_cache_test.go`/`decl_usecase_test.go`/`decl_query_test.go`/
  `decl_projection_test.go`/`decl_aggregate_load_test.go` (uma 2ª fixture)
  ainda usam `provider: "postgres"` decorativo — passam, mas puxam pgx à toa
  via `go mod tidy` nesses smoke tests; não é uma falha, só um desperdício
  de rede/tempo de teste. Candidato a limpeza futura (mesmo padrão da
  renomeação para "pg" acima), não registrado como issue por não ser um
  bug.
`go build ./...`, `go vet ./...`, `gofmt -l .` limpos; `go test ./...`
(suíte inteira, não só o escopo da task, por causa do ripple acima) verde.

Concluído: **J1.3** — `(R1)` `codegen/sql_wiring.go` ganhou
`databaseConnectionGo(e, db)`, o único ponto que resolve a connection
string de um `Database` real no wiring 2PC (`emitXADatabaseWiring`) — NUNCA
mais `strconv.Quote(db.DSN)` direto (esse campo só é populado a partir do
literal estático `"dsn:"`, `program/graph.go`, e fica `""` para
`env(...)`). `databaseConnectionGo` lê a `Expr` crua de `db.Decl.Entries`
(mesmo padrão de `telemetryEndpointGo`, `decl_telemetry.go`, reusando
`findConfigEntryExpr`/`envCallKey`): a chave `"connection"` (spec §12, a
forma canônica — `connection: env("DB_URL")`) tem prioridade; `"dsn"` é
aceita como sinônimo histórico (o mesmo campo semântico, nome usado pela
fixture sqlite `ledgerModDs`, `sql_adapter_test.go`, com um caminho de
arquivo literal). Qualquer uma das duas resolve por FORMA — `env(KEY)` vira
`os.Getenv(KEY)`, um literal STRING vira ele mesmo entre aspas Go — nunca
mais um valor Go nativo silenciosamente vazio. Nenhuma das duas chaves
presente cai no comportamento histórico (`strconv.Quote(db.DSN)` ==
`""`), preservando o default de antes desta task. Teste novo
`codegen/sql_wiring_connection_test.go` (`TestLedgerMainWiresConnectionFromEnv`):
reusa a fixture Ledger (domínio/aplicação/read de `sql_adapter_test.go`)
com um `mod.ds` alternativo — `provider: "postgres"` +
`connection: env("LEDGER_MAIN_PG_URL")`/`env("LEDGER_SIDE_PG_URL")` em vez
de `provider: "sqlite"` + `dsn: <caminho>` — e confirma que o `main.go`
gerado chama `sqlruntime.OpenPostgres(os.Getenv("LEDGER_MAIN_PG_URL"))`,
nunca `OpenPostgres("")`. Sem regressão: a fixture sqlite original
(`dsn:` literal) continua produzindo a MESMA string quotada de antes —
`TestLedgerMainWiresXADatabases`/`TestLedgerSmokeCompile`/
`TestLedgerSingleDatabaseBehavior`/`TestLedgerTwoPCBehavior` e os testes
J1.2 (`TestWallet*`, `TestActiveSQLProviders*`, `TestEmitGoMod*`) seguem
verdes, assim como `TestGenerate*` (`driver`, wallet/shop e2e).
`go build ./...`/`gofmt -l .`/`go vet ./...` limpos. Próxima: **J1.4**
(golden + smoke + teste de integração opt-in guardado por `PG_URL`).

Concluído: **J1.4** — Golden + smoke + integração, fechando a Fase J1
(REQ-41, NFR-17/22/24). Item (a) (fixture single-module `provider:
"postgres"` gera + builda + `go vet`a sobre bytes em disco) já era coberto
sem trabalho novo: `TestGenerateWalletSmokeCompile` (`codegen/
codegen_test.go`) gera o wallet real — que declara `Database MainDb {
provider: "postgres" }` desde sempre — e roda `gentest.SmokeCompile` sobre
os bytes escritos em disco; isso só passou a exercitar o adapter real a
partir de J1.2 (antes, "postgres" era decorativo). Item (b) (novo,
`codegen/sql_postgres_integration_test.go`, `//go:build integration`):
reusa a fixture Ledger (domínio/aplicação/read de `sql_adapter_test.go`)
com um `mod.ds` de UM Database (`MainDb`, `provider: "postgres"`,
`connection: env("PG_URL")`, `manages: [Account, Journal]` — sem
`supportsXA`/2PC, já coberto contra sqlite por `TestLedgerTwoPCBehavior`)
e roda o MESMO par UseCase/Query (`PerformDebit`/`GetAccount`) duas vezes
dentro do pacote `ledger` gerado: uma contra `runtime.NewMemoryEventStore()`,
outra contra `sqlruntime.NewEventStore(..., PostgresDialect())` sobre
`sqlruntime.OpenPostgres(os.Getenv("PG_URL"))` — compara o `balance` lido de
volta pelos dois caminhos (paridade, NFR-22). Guarda dupla contra NFR-24: a
build tag `integration` (o arquivo nem compila em `go test ./...` default)
MAIS um `t.Skip` em runtime quando `PG_URL` está vazia — tanto no teste
externo (`TestPostgresIntegrationParity`, evita gerar/rodar o subprocesso à
toa) quanto no teste comportamental gerado dentro do pacote `ledger`
(segunda camada de guarda, caso alguém rode o pacote gerado isolado). ID de
conta único por execução (`fmt.Sprintf("acc-pg-%d", time.Now().UnixNano())`)
evita colisão com uma tabela `events` persistente entre corridas — o mesmo
cuidado que os testes sqlite já não precisam (usam arquivo `:memory:`/
`t.TempDir()` descartável). Verificado manualmente (sem Postgres vivo neste
ambiente): `go build ./...`/`go vet ./...`/`gofmt -l .` default continuam
limpos (o arquivo novo é invisível sem `-tags=integration`); com
`-tags=integration` (ainda sem `PG_URL`), o arquivo compila e
`TestPostgresIntegrationParity` faz *skip* corretamente; a fixture gera com
sucesso (`generateLedgerPostgresIntegrationProject`) e o `behavior_test.go`
gerado passa por `go vet`/`go test` (skip) dentro do projeto Go de verdade
escrito em disco — provando que o caminho compila e só falta infra viva
para exercitar o corpo real do teste. Fecha a Fase J1 (REQ-41 completo:
J1.1–J1.4).

Concluído: **J2.1** — `(R4)` o seam `runtime.Tx` (`codegen/rtsrc/uow.go.txt`)
ganhou `EnqueueOutbox(events []Event) error` (REQ-42.1): `memoryTx`
implementa como no-op documentado (nenhum outbox durável in-memory —
`memoryOutbox`, `outbox.go.txt`, só encaminha ao Dispatcher); `sqlrt.Tx`
(`codegen/sqlrt/uow.go.txt`) implementa sobre o `*sql.Tx` em mãos, a MESMA
tx de `Append`, via `enqueueOutboxWithinTx` (novo, `eventstore.go.txt`, ao
lado de `appendWithinTx`) — atomicidade store+outbox por construção
(commit grava as duas tabelas, rollback desfaz as duas). `twophase.go.txt`
herda o método de graça (reusa a MESMA struct `Tx`). **Desvio de escopo
necessário** (documentado em `tasks.md`): o teste de atomicidade exige uma
tabela `outbox` de verdade, então `Dialect.CreateOutboxTable()` (novo
método na interface, Sqlite E Postgres) e o INSERT vieram para cá em vez de
esperar por J2.2 — que fica só com o lado de leitura/consumo
(`ScanUndelivered`/`MarkDelivered`/`PurgeDelivered`, FIFO+`SKIP LOCKED`).
`id` da tabela outbox é auto-incrementado pelo próprio banco (`INTEGER
PRIMARY KEY AUTOINCREMENT` no sqlite, `BIGSERIAL` no postgres) — nunca um
UUID, já que só um contador monotônico de inserção garante a ordem FIFO que
o relay (J2.3) vai exigir (`ORDER BY id`). `EventStore.ensureSchema` passou
a criar "outbox" ao lado de "events" (mesmo dono de schema — evita uma
corrida de "tabela não existe" se `EnqueueOutbox` for chamado antes de
qualquer `DurableOutbox` ser construído, J2.3). Teste novo
`codegen/sql_outbox_test.go` (`TestSQLOutboxAtomicity`, via
`gentest.WriteFiles`/`RunTests` sobre sqlite real — a atomicidade é
propriedade de `*sql.Tx`, a mesma para os dois dialetos): `Run` bem-sucedido
grava 1 linha em `events` E 1 em `outbox`; `Run` que devolve erro não
adiciona NENHUMA linha em NENHUMA das duas (rollback simulado). Sem
regressão: `TestSQL*`/`TestPostgres*`/`TestWallet*`/`TestLedger*`
(`codegen`, incl. o teste de integração Postgres sob `-tags=integration`,
que segue compilando e pulando sem `PG_URL`) e `TestGenerate*` (`driver`)
seguem verdes — a interface `Dialect` estendida não quebra nenhum
consumidor existente. `go build ./...`/`gofmt -l .`/`go vet ./...` limpos.
Próxima: **J2.2** (leitura/consumo da tabela outbox: `ScanUndelivered`/
`MarkDelivered`/`PurgeDelivered`, FIFO+`SKIP LOCKED`).

Concluído: **J2.2** — leitura/consumo da tabela `outbox` no `Dialect`
(REQ-42.4): três métodos novos na interface (`codegen/sqlrt/dialect.go.txt`)
— `ScanUndelivered(batch int) string` (SELECT em ordem FIFO, `ORDER BY id`
sempre; Postgres acrescenta `FOR UPDATE SKIP LOCKED` para que réplicas do
relay levem lotes exclusivos sem duplicar entrega à toa; sqlite usa `LIMIT`
simples — single-writer, a trava de escrita do próprio banco já serializa),
`MarkDelivered(idPlaceholder, timePlaceholder string) string` e
`PurgeDelivered(cutoffPlaceholder string) string` (idênticos nos dois
dialetos — SQL ANSI simples, sem sintaxe específica de banco; implementados
em ambos só para satisfazer `Dialect`). Mesma convenção de
`JSONFieldEq`/`LimitOffset`: os placeholders chegam prontos (o resultado de
`Placeholder(n)`, decidido pelo CHAMADOR — J2.3), `batch` é inlined como
inteiro Go direto (mesma convenção de `LimitOffset`, não parametrizado).
Teste novo `codegen/sql_outbox_dialect_test.go`
(`TestSQLOutboxDialectStrings`, via `gentest.WriteFiles`/`RunTests` — mesmo
padrão de `TestPostgresDialectSQLStrings`): confirma as strings exatas dos
dois dialetos, incl. que sqlite NUNCA contém `SKIP LOCKED` e que Postgres
sempre contém `FOR UPDATE SKIP LOCKED` + `ORDER BY id`. Sem regressão:
`TestSQL*`/`TestPostgres*`/`TestWallet*`/`TestLedger*` (`codegen`, incl. o
teste de integração Postgres sob `-tags=integration`, que segue compilando
e pulando sem `PG_URL`) e `TestGenerate*` (`driver`) seguem verdes — a
interface `Dialect` estendida (agora com 8 métodos) não quebra nenhum
consumidor existente. `go build ./...`/`gofmt -l .`/`go vet ./...` limpos.
Concluído: **J2.3** — `DurableOutbox` + relay (REQ-42.2/42.3). Seam novo em
`codegen/rtsrc/outbox.go.txt` (ao lado de `memoryOutbox`, MESMA interface
`Outbox`): `OutboxRow`/`OutboxStore` (interface — `ProcessBatch(ctx, batch,
deliver func(OutboxRow) error) (processed int, err error)`, dialeto-agnóstica
de propósito, para o núcleo `runtime` nunca importar `database/sql` —
NFR-12/21) e `EventFactory` (alias `func() Event`, mesma forma de
`sqlruntime.EventFactory` — zero conversão ao passar `EventRegistry()`).
`DurableOutbox` implementa `Subscribe` (mesmo formato de `memoryOutbox`) +
`Start(ctx)` (loop ticker/select, mesma forma de um worker periódico gerado
como `StartIdempotencyCleanup`) + `Tick(ctx)` (exportado, um lote só — o
gancho de teste direto sem esperar o ticker). `deliver` decodifica o
payload via a registry e roda os handlers assinados; falha de decode/tipo
desconhecido é tratada como PERMANENTE (marca entregue mesmo assim — nunca
entra num retry infinito por uma linha malformada, mesmo espírito do
poison-pill do RabbitMQ, §design 3.3); falha de HANDLER é retentável
(propaga, `ProcessBatch` incrementa `attempts` em vez de marcar).

`codegen/sqlrt/outbox.go.txt` (novo): `outboxStore`/`NewOutboxStore(db,
dialect) runtime.OutboxStore` — a implementação real de `ProcessBatch`:
abre UMA `*sql.Tx` para o lote inteiro (Scan + todo Mark/Increment +
Commit), porque a trava de linha do Postgres (`FOR UPDATE SKIP LOCKED`,
J2.2) só vale enquanto ESSA tx está aberta — comitar o SELECT sozinho cedo
demais liberaria a trava antes da entrega terminar, permitindo que outra
réplica pegasse o MESMO lote. **Achado sutil (documentado no arquivo):**
`Dialect.MarkDelivered(idPlaceholder, timePlaceholder)` monta o texto
SEMPRE como `"...delivered_at = <timePlaceholder> WHERE id = <idPlaceholder>"`
(timePlaceholder aparece PRIMEIRO no texto, apesar de vir depois na lista
de parâmetros) — para bater com isso nos dois esquemas de binding
(Postgres numerado por `$N`, sqlite posicional por ordem textual de `?`),
o chamador precisa passar `Placeholder(1)` como `timePlaceholder`
(aparece primeiro) e `Placeholder(2)` como `idPlaceholder` (aparece
segundo), e executar com os argumentos NA MESMA ordem (`now, id`) — inverter
essa ordem quebraria silenciosamente o sqlite (que não tem `$N` nomeado,
só posição textual). Incremento de `attempts` é SQL ANSI simples, sem
método próprio no `Dialect` (só `ScanUndelivered`/`MarkDelivered` têm forma
que difere por banco).

Testes novos (`codegen/sql_outbox_relay_test.go`, via
`gentest.WriteFiles`/`RunTests` sobre sqlite real):
`TestDurableOutboxDeliversAndMarks` (entrega ao handler assinado,
`delivered_at` deixa de ser NULL, um 2º `Tick` não re-entrega a linha já
marcada) e `TestDurableOutboxRetriesOnHandlerFailure` (o "crash simulado":
handler falha na 1ª tentativa ⇒ `delivered_at` continua NULL, `attempts`
sobe para 1, a MESMA linha é re-escaneada no próximo `Tick` ⇒ handler
sucede na 2ª tentativa ⇒ entrega, at-least-once cumprido). Sem regressão:
`TestSQL*`/`TestPostgres*`/`TestWallet*`/`TestLedger*`/`TestPolicy*`/
`TestOutbox*`/`TestGentest*` (`codegen`, incl. integração Postgres sob
`-tags=integration`, segue compilando/pulando) e `TestGenerate*` (`driver`)
seguem verdes. `go build ./...`/`gofmt -l .`/`go vet ./...` limpos.
Concluído: **J2.4** — `(R9)` o relay alimenta o canal cross-service
(REQ-42.6). `NewDurableOutbox(store, registry, publisher ...Publisher)`
ganha um `publisher` opcional (mesma convenção variádica de
`NewUnitOfWork`): quando presente, `deliver` roteia TODA linha entregue por
`publisher.Publish` em vez de rodar os handlers localmente assinados via
`Subscribe` — a decisão é tomada UMA vez por instância de `DurableOutbox`
(não por `event_type` dinamicamente), espelhando a MESMA exclusividade
mútua que `codegen.go` já impõe hoje entre dispatcher/canal por módulo
(`NewUnitOfWork(store, dispatcher)` OU `NewUnitOfWork(store, canal)`, nunca
os dois — comentário em `codegen.go` sobre "wiring combinado ainda não
suportado"). `Publisher` (`Dispatcher`/`ChannelTransport` já satisfazem a
MESMA assinatura `Publish(ctx, ev) error`) é o único contrato exigido — o
mecanismo de roteamento não conhece nem depende de qual transporte concreto
está por trás (RabbitMQ, Marco J3, ainda não existe; os testes usam um
`fakePublisher` local). Uma falha de `Publish` é retentável exatamente como
falha de handler: `ProcessBatch` incrementa `attempts` em vez de marcar
entregue, e `ORDER BY attempts ASC, id ASC` (revisão da PR #20) garante que
a mesma linha volta a ser escaneada num `Tick` seguinte.

**Desvio de escopo (reclassificação):** o item (b) da task original
("proibir publish direto no commit... o publisher da uow deixa de receber
o canal quando o outbox durável está ativo") é uma decisão de WIRING — qual
valor `codegen.go` passa para `NewUnitOfWork`/qual `main.go` gerado
constrói — não do mecanismo do `DurableOutbox` em si; reclassificado para
**J2.5** (a task de seleção/wiring), documentado em `tasks.md`. A garantia
comportamental que o item (b) pedia ("crash entre commit e publish ⇒ evento
re-entregue") já está provada nesta task, no nível do relay, independente
de qualquer decisão de wiring futura.

Testes novos (`codegen/sql_outbox_channel_test.go`, via
`gentest.WriteFiles`/`RunTests` sobre sqlite real):
`TestDurableOutboxRoutesToPublisherInsteadOfLocalHandlers` (com
`publisher` configurado, `deliver` chama `Publish` e NUNCA roda o handler
local — mesmo quando esse handler está assinado para o mesmo `event_type`)
e `TestDurableOutboxRetriesOnPublishFailure` (o "crash simulado" do lado do
publisher: `Publish` falha na 1ª tentativa ⇒ `attempts` sobe,
`delivered_at` continua NULL; a MESMA linha é re-escaneada no próximo
`Tick` ⇒ `Publish` sucede na 2ª tentativa ⇒ entrega — nenhum evento
cross-service é perdido). Sem regressão:
`TestSQL*`/`TestPostgres*`/`TestWallet*`/`TestLedger*`/`TestPolicy*`/
`TestOutbox*`/`TestGentest*` (`codegen`, incl. integração Postgres sob
`-tags=integration`) e `TestGenerate*` (`driver`) seguem verdes.
`go build ./...`/`gofmt -l .`/`go vet ./...` limpos.

Concluído: **J2.5** — cleanup + seleção/wiring, fecha a Fase J2 (REQ-42
completo). `runtime.OutboxStore` (`rtsrc/outbox.go.txt`) ganha um QUINTO
método, `PurgeDelivered(ctx, cutoff) (int, error)` (apaga linhas entregues
mais velhas que `cutoff`, sem transação própria — nada mais escreve na
`outbox` durante a chamada), implementado em `sqlrt/outbox.go.txt:
outboxStore.PurgeDelivered` sobre `Dialect.PurgeDelivered` (já existia desde
J2.2, nunca consumido até agora). `DurableOutbox` ganha `Cleanup(ctx,
retention) (int, error)` (delega a `store.PurgeDelivered`, mesmo espírito de
`StartIdempotencyCleanup`/`Cleanup` do idempotency store).

Seleção (REQ-42.5): `codegen/sql_wiring.go:moduleOutboxDatabaseName(prog,
module)` devolve o Database real (primeiro em ordem alfabética,
`recognizedSQLProvider`) do módulo, ou `""` — nenhum caso comum muda
(wallet/shop/shipping incluídos: `shipping` tem Policy AtLeastOnce mas NENHUM
Database próprio, então nunca dispara este caminho — achado desta task que
zera o risco de regressão em cima do shop real que a task anterior tinha
levantado por engano). `codegen/decl_policy.go:emitPolicyWireFunc` ganha o
parâmetro `outboxDBName`: quando `""` (o caso comum), Go idêntico a antes
desta task — `o := runtime.NewOutbox(d)` continua var LOCAL de `Wire`
(NFR-21/23). Quando não-vazio E ao menos uma Policy AtLeastOnce local ("d",
nunca uma cross-service via canal "queue" — fora do orçamento desta task,
ver ISSUE-9) disputa o outbox: `o` vira var de PACOTE, tipo concreto
`*runtime.DurableOutbox` (não a interface `Outbox` — só o tipo concreto
expõe `Start`/`Cleanup`, que `StartOutboxRelay`/`StartOutboxCleanup`, novos,
chamam); um novo par `var outboxStore runtime.OutboxStore` +
`func WireOutboxStore(store runtime.OutboxStore)` (populado por
`cmd/<service>/main.go` ANTES de `Wire`) alimenta
`runtime.NewDurableOutbox(outboxStore, registry)` dentro de `Wire` — o
`registry` é montado INLINE a partir só dos eventos que as Policies
AtLeastOnce locais deste módulo consomem (nunca `EventRegistry()`, que só
cobre Event/PublicEvent DECLARADOS pelo módulo — uma Policy cross-módulo
como `NotifyShipping`, reagindo a um PublicEvent de Orders, não teria a
entrada ali).

Wiring em `main.go` (REQ-42.5): `codegen.go` ganha `moduleMarks.
outboxDatabase` (calculado com `moduleNeedsDurableOutbox`, que reusa
`resolvePolicyWireInfos` para bater EXATAMENTE a mesma decisão de
`emitPolicyWireFunc` — uma divergência quebraria a compilação num lado ou no
outro) e `modulesOutboxDatabase map[string]string` repassado a
`generateCmdMainFile`. Um `wireTarget` com `outboxDatabase != ""` ganha, em
`func main()`: `sqlruntime.Open<Provider>(...)` (mesma resolução de
connection string de `emitXADatabaseWiring`, `databaseConnectionGo`, J1.3) +
`sqlruntime.NewOutboxStore(db, dialect)` + `<pkg>.WireOutboxStore(...)`
SEMPRE antes de `<pkg>.Wire(...)` (`Wire` lê `outboxStore` ao construir o
`DurableOutbox`) + `go <pkg>.StartOutboxRelay(workerCtx)`/`go <pkg>.
StartOutboxCleanup(workerCtx)`, nomes próprios ao lado de
`StartWorkers`/`StartIdempotencyCleanup` (`emitOutboxDatabaseWiring`, novo,
`sql_wiring.go`).

**Desvio de escopo, registrado como ISSUE-9:** o item reclassificado de
J2.4.b ("o publisher da uow deixa de receber o canal direto quando o outbox
durável está ativo" — o lado PRODUTOR, `NewUnitOfWork(store, canal)` em
`generateCmdMainFile`) NÃO foi fechado nesta task — só o lado CONSUMIDOR
(Policy). Fechar o lado produtor exige que UseCase/Handle gerados chamem
`tx.EnqueueOutbox` (nenhum emissor chama isso hoje — só os testes manuais de
J2.1-J2.5 o exercitam), peça que `design.md` já reconhece como fechando de
verdade só na fixture-âncora (J6, quando o transporte real de J3 estiver
presente). shop/Orders EXERCITARIA esse caminho (Database postgres real +
UseCase produzindo para o canal Orders→Shipping) mas seu `main.go` continua
byte-idêntico a antes desta task (confirmado por `driver.TestGenerate*`
sem nenhuma regeneração de golden) — não é uma regressão, é escopo ainda não
fechado.

Testes novos: `codegen/decl_policy_outbox_test.go` (fixture sintética nova,
2 módulos sem topology.ds — Orders com UseCase+Database postgres decorativo,
Shipping com Policy AtLeastOnce + Database próprio, provider variável)
— `TestPolicyOutboxDurableWhenModuleHasRealDatabase` (provider `"sqlite"`
reconhecido: policies.go tem `NewDurableOutbox`/`WireOutboxStore`/
`StartOutboxRelay`/`StartOutboxCleanup`; `cmd/app/main.go` abre a conexão,
chama `WireOutboxStore` ANTES de `Wire` — checado por índice de string — e
sobe as duas goroutines; `gentest.SmokeCompile` builda de verdade) e
`TestPolicyOutboxMemoryWhenNoRealDatabase` (provider `"pg"`, decorativo/não
reconhecido: nenhum dos símbolos novos aparece em lugar nenhum, `o :=
runtime.NewOutbox(d)` local intacto — prova NFR-21/23 nesta combinação nova).
`codegen/sql_outbox_cleanup_test.go` (via `gentest.WriteFiles`/`RunTests`
sobre sqlite real): `TestDurableOutboxCleanupPurgesOnlyOldDelivered` — três
linhas (entregue há 10 dias, entregue agora, nunca entregue) — `Cleanup(ctx,
7*24h)` apaga só a 1ª; uma 2ª chamada imediata não apaga nada a mais.

Sem regressão: `go build ./...`/`gofmt -l .`/`go vet ./...` limpos; `go test
./...` (suíte inteira) verde, incluindo `TestSQL*`/`TestPostgres*`/
`TestWallet*`/`TestLedger*`/`TestActiveSQLProviders*`/`TestEmitGoMod*`/
`TestPolicy*`/`TestOutbox*`/`TestGentest*` (`codegen`), a integração Postgres
sob `-tags=integration` (compila e pula sem `PG_URL`) e `TestGenerate*`
(`driver`, wallet/shop e2e — nenhum byte mudou, confirmando NFR-21/23 para
os dois exemplos reais).

Concluído: **J3.1** — adapter `amqprt` + envelope + registro de contracts,
abre a Fase J3 (REQ-43). `codegen/amqprt/` (novo, espelha `sqlrt/`/`grpcrt/`
— `doc.go`/`embed.go` com `Sources()`/`Names()` via `//go:embed *.go.txt`):
`rabbitmq.go.txt` declara `package amqpruntime` (mesma convenção de
`sqlruntime`/`grpcedge` — nome distinto de "runtime", o núcleo). `driver:
github.com/rabbitmq/amqp091-go` (o fork oficial mantido, `streadway/amqp`
está arquivado), `v1.12.0` fixo (determinismo, NFR-13; seu próprio go.mod
exige só `go 1.20` — nenhuma constante `minGoVersion` própria precisou
subir o default "1.22").

`rabbitmqChannel` implementa `runtime.ChannelTransport` (`Subscribe`/
`Publish`) sobre uma exchange fanout + UMA queue durável — Subscribe
registra handlers locais, Publish serializa no envelope e publica,
`consume` (goroutine própria, subida por `NewRabbitMQChannel`) decodifica
cada mensagem, entrega, e faz `ack`/`nack(requeue=false)`. Documentado
explicitamente no arquivo o que ESTA versão NÃO cobre ainda (cada um com a
task própria que fecha): ordenação por partição via exchange
consistent-hash (**J3.2**, REQ-43.3/R6 — hoje uma única queue não preserva
"ordem por chave" do jeito que o `QueueChannel` in-memory promete), DLX+
retry-queue no lugar do `nack` direto (**J3.2**, REQ-43.4), e supervisão de
reconexão sobre `NotifyClose` (**J3.3**, REQ-43.6).

Envelope (REQ-42.1.b/§design 3.3): JSON `{eventType, payload}` — mesmo
shape que o Dispatcher já move em memória. `encodeEnvelope`/`decodeEnvelope`
são funções livres (não métodos), deliberadamente NÃO exportadas — só o
wiring gerado (dentro do MESMO pacote `amqpruntime`) as chama de verdade.
Registry (R8/REQ-43.5): `decodeEnvelope` recebe um `map[string]EventFactory`
já pronto e não distingue de onde vem cada factory — o mecanismo que
permite ao wiring do CONSUMIDOR (task J3.4, fora do orçamento desta task)
mesclar `contracts.EventRegistry()` (as factories dos PublicEvent) ao
`EventRegistry()` do próprio módulo antes de construir o canal; um
`eventType` fora do registry é tratado como falha PERMANENTE ("poison
pill", mesma classificação de `DurableOutbox.deliver`, Marco J) — erro
claro, nunca um pânico nem uma decodificação silenciosamente errada.

Registro: `codegen/project.go` ganha as constantes `amqpDriverModule`/
`amqpDriverVersion`; `codegen/provider_registry.go` ganha
`channelProviders["rabbitmq"]` (a PRIMEIRA entrada real desse registro,
criado vazio em J0.1) e `providerSources["amqpruntime"] = amqprt.Sources`
— a partir daqui `activeProviderDeps`/`generateProviderRuntimeFiles`/
`EmitGoMod` (mecânica genérica de J0.1-J0.3) já resolvem um canal
`provider: "rabbitmq"` sozinhos, sem nenhuma mudança adicional nelas.
Nenhuma mudança em `channel.go`/`decl_policy.go`/`generateCmdMainFile`
ainda: NENHUM `.ds` seleciona `rabbitmqChannel` de verdade nesta task —
isso é **J3.4** (o item (c) do design, "wiring do consumidor registra
contracts.EventRegistry()", fecha no nível de MECANISMO aqui; o CALL SITE
que de fato monta e passa o registry mesclado é wiring, escopo de J3.4).

**Ripple pequeno, corrigido dentro do orçamento desta task:** dois testes
de J0.1/J0.3 assumiam os quatro registros (`channelProviders` incluído)
vazios — `TestActiveProviderDepsEmptyRegistry` (usava `"rabbitmq"` como
exemplo de provider de canal NÃO reconhecido) virou
`TestActiveProviderDepsUnrecognizedProvidersAreNoOp` (troca para
`"kafka"`, ainda não implementado neste ciclo);
`TestGenerateProviderRuntimeFilesEmptyRegistryIsNoop` (usava `"amqpruntime"`
como exemplo de `adapterDir` NÃO registrado em `providerSources`) ganhou um
monkey-patch explícito de `providerSources` para vazio, e um teste NOVO
(`TestGenerateProviderRuntimeFilesCopiesRealAMQPRuntimeSources`) prova o
caminho positivo real (sem monkey-patch) — `channelProviders["rabbitmq"]`
resolve contra `providerSources` de verdade e copia `amqpruntime/rabbitmq.go`.

Testes novos: `codegen/amqp_envelope_test.go` (`package codegen`, interno —
acessa `channelProviders["rabbitmq"]` direto para montar o go.mod da
fixture, mesmo padrão de `provider_registry_test.go`) —
`TestAMQPEnvelopeRoundTrip` roda, via `gentest.WriteFiles`/`RunTests` (só
`go mod tidy` resolvendo o driver amqp091-go — nenhuma conexão AMQP real
aberta), um teste `package amqpruntime` (white-box, acessa
`encodeEnvelope`/`decodeEnvelope` direto) que prova
`TestEnvelopeRoundTripLocalAndContractsEvents` (um registry MESCLADO
local+contracts decodifica os dois tipos a partir do MESMO map — a prova
de R8/REQ-43.5 no nível de mecanismo) e
`TestEnvelopeUnknownEventTypeIsPermanentError` (erro claro, nunca pânico).

Sem regressão: `go build ./...`/`gofmt -l .`/`go vet ./...` limpos; `go
test ./...` (suíte inteira) verde, incluindo `TestActiveProviderDeps*`/
`TestGenerateProviderRuntimeFiles*`/`TestEmitGoModWithProviderDeps*`
(`codegen`) e `TestGenerate*` (`driver`, wallet/shop e2e — nenhum byte
mudou, NFR-21/23 intactos: nenhum dos dois declara canal `provider:
"rabbitmq"`).

Concluído: **J3.2** — **(R6)** Ordenação por partição + poison pill
(REQ-43.3/43.4). `RabbitMQConfig` ganha `KeyFunc runtime.KeyFunc` (mesmo
tipo exportado de `rtsrc/channel.go.txt`, reusado direto — nil = sem
`orderBy`), `Concurrency`/`MaxAttempts`/`RetryTTL` (com defaults via
`effectiveConcurrency`/`effectiveMaxAttempts`/`effectiveRetryTTL`, cada uma
uma função pura clampando `<1`/`<=0`).

Ordenação (REQ-43.3, R6, mesma regra do `QueueChannel` in-memory,
`rtsrc/channel.go.txt`, agora cross-process): `KeyFunc != nil` ⇒ a exchange
principal é declarada `"x-consistent-hash"` em vez de `"fanout"`,
`Concurrency` filas de partição (`partitionQueueNames`, `"<base>-p0"..
"<base>-p(n-1)"`), cada uma ligada com a MESMA `consistentHashBindingKey()`
(peso uniforme "10") e consumida por exatamente UM `*amqp.Channel`/goroutine
próprios (`Qos(1)`) — `Publish` roteia com routing key = `KeyFunc(ev)`,
que a exchange consistent-hash hasheia deterministicamente pra sempre cair
na MESMA partição (ordem preservada). `KeyFunc == nil` ⇒ exchange `fanout`
de sempre, UMA fila, mas `Concurrency` consumidores DISTINTOS (canais
próprios) competindo pela MESMA fila ("competing consumers" do RabbitMQ) —
**desvio documentado** do literal "um consumidor com `Qos(prefetch=
Concurrency)`" do design: mesmo teto de mensagens em voo, sem precisar de
mutex extra pra Ack/Nack concorrente sobre um canal compartilhado (cada
canal AMQP é dono de exatamente uma goroutine — nem Subscribe/Publish nem
Ack/Nack cruzam goroutines).

Poison pill / DLX+retry+DLQ final (REQ-43.4, substitui o
`nack(requeue=false)`-descarta-direto de J3.1): TODA fila principal
(partição ou única) declara `x-dead-letter-exchange` apontando pra uma DLX
de retry PRÓPRIA do canal (`retryExchangeName`) — `nack(requeue=false)`
agora roteia pra lá em vez de descartar. A DLX de retry alimenta UMA retry
queue (`retryQueueName`) com `x-message-ttl` (`effectiveRetryTTL`) +
`x-dead-letter-exchange` apontando de VOLTA pra exchange ORIGINAL: ao
expirar o TTL (sem consumidor nenhum na retry queue — ela só segura a
mensagem), o broker reencaminha pra exchange original preservando a routing
key (nem a fila principal nem a retry queue setam
`x-dead-letter-routing-key`), então uma mensagem particionada volta pra
MESMA partição depois de reencaminhada — e incrementa `x-death` a cada
passagem. `xDeathCount` (função pura sobre `amqp.Table`, soma o campo
`"count"` de cada entrada do header `x-death`) diz ao consumidor quantas
vezes a mensagem já deu a volta; ao atingir
`effectiveMaxAttempts(cfg.MaxAttempts)` — REQ-43.4 reusa o CAMPO
`circuitBreaker.threshold` do canal como contador de tentativas, deliberada
e documentadamente DIFERENTE da máquina de estados open/half-open/closed de
`runtime.CircuitBreaker` (que resolve um problema diferente: um breaker
por-canal global guardando TODA entrega, não um contador por-mensagem) —, o
consumidor publica o corpo direto na DLQ final (`dlqName`, via exchange
default `""`, routing direto por nome de fila) e dá `ack` na mensagem
original, removendo-a do ciclo. Uma falha de DECODIFICAÇÃO (evento
desconhecido/payload malformado) passa pelo MESMO ciclo — nenhum caminho
especial "permanente direto pra DLQ": mais simples, e ainda limitado (nunca
gira pra sempre, `effectiveMaxAttempts` sempre a manda pra DLQ eventualmente).

`Close()` (da revisão da PR #23) passou a liberar TODOS os canais de
consumo abertos (um por partição/consumidor), não só o de publish.

Testes novos: `codegen/amqp_topology_test.go` (mesmo padrão de
`amqp_envelope_test.go`/J3.1.e — `package codegen`, fixture via
`gentest.WriteFiles`/`RunTests` sobre `buildAMQPRuntimeProjectFiles`, teste
embutido `package amqpruntime` white-box, SEM nenhuma conexão AMQP real):
`TestAMQPTopologyAssembly` roda `TestPartitionQueueNames`,
`TestRetryTopologyNames` (os três recursos extras não colidem entre si nem
com a exchange original), `TestMainQueueArgsPointsToRetryExchange`,
`TestRetryQueueArgsPointsBackToMainExchangeWithTTL` (TTL em milissegundos,
`x-dead-letter-exchange` aponta pra exchange ORIGINAL, nunca de volta pra
DLX de retry — senão giraria em círculo sem nunca voltar à fila principal),
`TestConsistentHashBindingKeyIsNumeric`, `TestXDeathCountSumsAcrossEntries`
e `TestEffectiveMaxAttemptsAndRetryTTLDefaults` (clamping `<1`/`<=0`).

Sem regressão: `go build ./...`/`gofmt -l .`/`go vet ./...` limpos; `go
test ./...` (suíte inteira) verde, incluindo `TestAMQPEnvelopeRoundTrip`
(J3.1, ainda passa sobre o rabbitmq.go.txt reescrito) e `TestGenerate*`
(`driver`, wallet/shop e2e — nenhum byte mudou).

Próxima: **J3.3** — Reconexão: supervisão de
`Connection.NotifyClose`/`Channel.NotifyClose` com loop de
reconnect+backoff, re-declarando toda a topologia (exchanges/filas/
consumidores) desta task; `Publish` na janela de reconexão retorna erro (o
relay do outbox re-tenta). Teste unit: fechar o canal fake ⇒ o supervisor
tenta reabrir.

Concluído: **J3.3** — Reconexão (REQ-43.6). `rabbitmqChannel` ganha um
supervisor (`supervise`, uma goroutine própria disparada por
`NewRabbitMQChannel`) que observa `Connection.NotifyClose` da conexão
ATUAL e, ao detectar um fechamento que não veio de `Close()` (a checagem de
`r.stopCh` logo após o disparo de `NotifyClose` distingue as duas causas —
`Close()` sempre fecha `stopCh` ANTES de fechar a conexão de verdade),
aciona `reconnectLoop`: redial + re-declaração da topologia INTEIRA +
reabertura de todos os consumidores, com backoff exponencial
(`nextReconnectBackoff`, mesmo idioma de `DurableOutbox.Start`,
`rtsrc/outbox.go.txt` — dobra a cada falha, capado em
`reconnectMaxBackoff` = 30s, partindo de `reconnectInitialBackoff` = 1s).

Refatoração pré-requisito: a lógica de declaração de topologia (antes só
inline em `NewRabbitMQChannel`) virou o método `declareTopology`, e o dial +
declare + abertura de consumidores virou `dialAndSetup` — as DUAS reusadas
por `NewRabbitMQChannel` (construção) e `reconnectLoop` (reconexão), nunca
duplicadas.

`conn`/`ch`/`consumeChans` deixaram de ser write-once na construção — agora
protegidos por `connMu` (`sync.RWMutex`, campo NOVO desta task): `Publish`
lê `r.ch` sob `RLock` e devolve erro imediatamente se for `nil` (janela de
reconexão), nunca bloqueia nem desreferencia nil; `pubMu` continua existindo,
mas agora só serializa a chamada de I/O (`PublishWithContext`) em si.
`Subscribe` não muda (só popula um map protegido por outro mutex, nunca toca
conn/ch). `Close()` fecha `stopCh` (via `closeOnce`, então chamar `Close()`
mais de uma vez nunca panica) ANTES de fechar conexão/canais — é essa ordem
que permite ao supervisor distinguir "fechamento deliberado" de "blip de
rede" sem nenhuma outra sincronização extra.

Teste novo: `codegen/amqp_reconnect_test.go` (mesmo padrão white-box de
`amqp_topology_test.go`/`amqp_envelope_test.go` — `package codegen`, fixture
via `gentest.WriteFiles`/`RunTests` sobre `buildAMQPRuntimeProjectFiles`,
teste embutido `package amqpruntime`): `TestAMQPReconnectBackoffSequence`
roda `TestNextReconnectBackoffDoublesAndCaps` (a sequência dobra e capa em
`reconnectMaxBackoff`) e `TestNextReconnectBackoffNeverExceedsCapFromLargeInput`
(clamping defensivo contra overflow de `time.Duration`). **Desvio do item
b da task** ("fechar o canal fake ⇒ o supervisor tenta reabrir" literal):
`*amqp.Connection`/`*amqp.Channel` (`github.com/rabbitmq/amqp091-go`) são
STRUCTS concretos, não interfaces — não há como fabricar um `NotifyClose`
"fake" sem abrir um socket de verdade contra um broker real (documentado no
topo de `rabbitmq.go.txt`, seção "Reconexão"). Testado, então, o que É
puro/testável sem broker (a sequência de backoff); o teste de integração
de verdade (gated por uma env var tipo `AMQP_URL`) fica fora do orçamento
desta task — pertence a J3.4/J6.

Sem regressão: `go build ./...`/`gofmt -l .`/`go vet ./...` limpos; `go
test ./...` (suíte inteira) verde, incluindo `TestAMQPEnvelopeRoundTrip`/
`TestAMQPTopologyAssembly` (J3.1/J3.2, ainda passam sobre o
`rabbitmq.go.txt` reescrito) e `TestGenerate*` (`driver`, wallet/shop e2e —
nenhum byte mudou, NFR-21/23 intactos: nenhum dos dois declara canal
`provider: "rabbitmq"`).

## Issues em aberto

Ver `.claude/issues.md`. ISSUE-1 (read-side/I5.1) **RESOLVIDA** (commit
`3a22df3`): `codegen/decl_collections.go` centraliza a declaração de
`Collection[T]` var disputado entre `EmitQueries`/`EmitPolicies` num único
`collections.go` por módulo — nenhuma issue em aberto no momento.
