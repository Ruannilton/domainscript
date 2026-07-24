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
| infra-providers (REQ-41..48) | `.claude/specs/infra-providers/` | done (recorte de 5 fechado; residual REQ-42.6 registrado) | — |
| correcoes-issues-9-10-11 (REQ-49..51) | `.claude/specs/correcoes-issues-9-10-11/` | done | — |
| correcoes-issues-6-7-8 (REQ-52..54) | `.claude/specs/correcoes-issues-6-7-8/` | in-progress (L1.1/L1.2 done; tasks.md gained L1.3a-L1.3f to resolve ISSUE-12 before the final pizzeria proof) | L1.3a |

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

**Revisão da PR #25** corrigiu três bugs de corrida encontrados após o
primeiro push: (1) `supervise()` agora checa `r.conn == nil` antes de
chamar `NotifyClose` — evita um nil pointer dereference quando
`reconnectLoop` retorna sem reconectar (`Close()` concorrente durante a
janela de reconexão); (2) `reconnectLoop` fecha explicitamente a conexão
antiga (não só os canais), liberando recursos do lado do cliente; (3)
`reconnectLoop` checa `r.stopCh` sob `connMu.Lock` ANTES de salvar uma
conexão nova recém-aberta — se `Close()` rodou enquanto `dialAndSetup()`
estava em andamento, a conexão/canais novos são fechados imediatamente em
vez de vazarem.

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

Concluído: **J4.1** — adapter `redisQueryCache` (Cache §15, REQ-44.1/44.5,
§design infra-providers 3.4), abrindo a Fase J4. Novo pacote
`codegen/redisrt/` (mesmo padrão de `amqprt`/`sqlrt`/`grpcrt`: `doc.go` +
`embed.go` (`Sources()`/`Names()` via `//go:embed *.go.txt`) +
`cache.go.txt`, `package redisruntime`, driver
`github.com/redis/go-redis/v9`).

`redisQueryCache` implementa `runtime.QueryCache`
(`codegen/rtsrc/querycache.go.txt`) sobre um `redisCmdable` (interface
interna própria, só `Get`/`Set`/`Incr` — as 3 únicas operações usadas, com as
MESMAS assinaturas dos métodos promovidos por `*redis.Client`; existir como
interface, e não campo `*redis.Client` concreto, é o que permite ao teste
injetar um client fake sem nenhuma conexão real, já que `*redis.Client` é
uma struct em go-redis/v9, não uma interface).

**Serialização type-preserving via `encoding/gob`, não `encoding/json`**: o
wrapper gerado (`emitQueryCacheWrapper`, `codegen/decl_query_cache.go`) faz
`v.(<TipoDeRetorno>)` — um type assertion DIRETO e sem checagem sobre o que
`Get` devolve como `value any`. `encoding/json` num alvo `any` reconstruiria
só `map[string]interface{}`/`float64`/etc, o que faria esse assertion
PANICAR; `encoding/gob`, ao contrário, preserva o tipo concreto através de um
campo de interface — `cachePayload{Value any; Err error; HasErr bool}`, o
envelope gravado no Redis — DESDE QUE o tipo concreto já tenha sido
registrado via `gob.Register` antes do primeiro `Decode`. Essa chamada de
`gob.Register(<TipoDeRetornoDaQuery>{})` (e, quando a Query usa
`negativeCacheTtl`, dos tipos de erro de negócio cacheados por `SetErr`) é
documentada no arquivo como responsabilidade do wiring que seleciona este
backend para uma Query específica — **não desta task**: J4.3 ("Seleção +
wiring") ainda não existe; `Get`/`decodePayload` já tratam um tipo não
registrado (ou qualquer stream gob malformado) como um MISS silencioso
(fail-open), nunca um pânico.

**Invalidação por geração-no-prefixo (item b)**: cada instância (um
namespace explícito por construtor, `NewRedisQueryCache(client, namespace)`
— uma instância por Query cacheada, mesma convenção já documentada em
`querycache.go.txt`) mantém um contador em `<ns>:gen` (via `INCR`,
`redisQueryCache.InvalidateAll` = um único `INCR`, O(1), NUNCA `SCAN`). Toda
chave de DADOS é escrita como `<ns>:<geração-na-escrita>:<sha256(key)>`
sempre COM o `ttl`/`negativeCacheTtl` recebido de `Set`/`SetErr` (ttl<=0
nunca cacheia, mesma regra do `memoryQueryCache`) — nunca sem expiração. A
chave de geração em si NUNCA recebe TTL (persistente por design: se
expirasse, o contador reiniciaria em 0 e invalidaria acidentalmente todo o
cache vivo daquele namespace). `InvalidateAll` não apaga as chaves de dados
da geração antiga — elas só deixam de ser lidas e expiram sozinhas via seu
próprio TTL (custo aceito por design, documentado no arquivo).

**Fail-open total (REQ-44.1/44.5)**: `Get` nunca devolve algo diferente de
`hit=false` sob qualquer falha (Redis indisponível, `redis.Nil`/chave
ausente, `gob.Decode` malformado) — nunca propaga erro, nunca panica.
`Set`/`SetErr`/`InvalidateAll` também nunca retornam erro (mesma forma da
interface): uma falha aí só é logada via `log/slog` (mesma convenção de
`amqprt/rabbitmq.go.txt`) e a chamada retorna — uma escrita de cache que
falha não é fatal. Coalescing (stampede protection) continua local por
processo (mesmo mecanismo single-flight de `memoryQueryCache.Coalesce`,
copiado dentro de `redisQueryCache`) — cruzar réplicas não é exigido (spec
§15).

Registro (mirror exato de J3.1/`channelProviders["rabbitmq"]`):
`codegen/project.go` ganhou as constantes
`redisDriverModule`/`redisDriverVersion` (`github.com/redis/go-redis/v9
v9.21.0`, confirmada estável via `go list -m -versions`) e
`redisMinGoVersion` ("1.24", do próprio `go.mod` do driver — abaixo de
`sqliteMinGoVersion`/`postgresMinGoVersion`, então não eleva o default além
do que Postgres/sqlite já exigiriam); `codegen/provider_registry.go` ganhou
`cacheProviders["redis"] = {module: redisDriverModule, version:
redisDriverVersion, minGo: redisMinGoVersion, adapterDir: "redisruntime",
ctor: "NewRedisQueryCache"}` e `providerSources["redisruntime"] =
redisrt.Sources` — `activeProviderDeps`/`EmitGoMod`/
`generateProviderRuntimeFiles` (todos genéricos desde J0) já passam a
reconhecer `Cache { backend: "redis" }` sem nenhuma mudança adicional
(REQ-46.1/46.2/46.3). Este é o PRIMEIRO backend real de `cacheProviders`
(antes só `channelProviders["rabbitmq"]` era real) — `TestActiveProviderDepsUnrecognizedProvidersAreNoOp`
(`codegen/provider_registry_test.go`) usava `"redis"` como exemplo de
backend de Cache NÃO reconhecido; atualizado para `"memcached"` (RateLimit
continua usando `"redis"` como exemplo de backend ainda não reconhecido —
`rateLimitProviders` fica vazio até J4.2).

**Ainda fora do orçamento (J4.2/J4.3)**: `redisLimiter` (RateLimit sobre
Redis, script Lua atômico, §design infra-providers 3.4) é J4.2; a seleção
real — o wiring gerado (`decl_query_cache.go`) emitindo
`redisruntime.NewRedisQueryCache(...)` em vez de
`runtime.NewMemoryQueryCache()` quando `backend: "redis"`, incluindo as
chamadas `gob.Register` necessárias — é J4.3. Nenhum dos dois foi tocado
aqui; a task J4.1 é só o adapter.

Testes novos (item c): `codegen/redis_cache_test.go` (`package codegen`,
mesmo padrão de `amqp_envelope_test.go` — lê `cacheProviders["redis"]`
direto ao montar o `go.mod` da fixture, roda um teste embutido `package
redisruntime` white-box via `gentest.WriteFiles`/`RunTests` sobre um
projeto Go efêmero real, SEM nenhuma conexão Redis, usando um `fakeCmdable`
em memória injetado via `newRedisQueryCache` — o construtor interno usado
tanto pelo fake quanto por `NewRedisQueryCache` real):
`TestRedisQueryCacheRoundTripPreservesConcreteType` (Set→Get com um struct
próprio, prova que o tipo concreto sobrevive ao gob, não vira
`map[string]interface{}`), `TestRedisQueryCacheSetErrRoundTrip` (negativo,
`SetErr`), `TestRedisQueryCacheInvalidateAllMissesEvenWithinTTL` (Get após
`InvalidateAll` vira miss mesmo dentro do TTL original — prova geração, não
expiração — e confirma que a chave de geração nunca é apagada),
`TestRedisQueryCacheGetFailsOpenOnRedisError`/
`TestRedisQueryCacheGetFailsOpenOnUnregisteredType` (fail-open, cliente fake
com erro injetado / payload gob corrompido) e
`TestRedisQueryCacheSetSkipsNonPositiveTTL`.

Sem regressão: `go build ./...`/`gofmt -l .`/`go vet ./...` limpos; `go test
./codegen/ -run "TestSQL|TestPostgres|TestWallet|TestLedger|
TestActiveSQLProviders|TestEmitGoMod|TestPolicy|TestOutbox|TestGentest|
TestProviderRegistry|TestActiveProviderDeps|
TestGenerateProviderRuntimeFiles|TestAMQP"` e `go test ./driver/... -run
TestGenerate` (wallet/shop e2e, zero bytes mudaram, NFR-21) verdes; `go test
./...` (suíte inteira) verde.

**Reconciliação** (J3.3 e J4.1 foram desenvolvidas em paralelo, em branches
separadas — ver a nota na PR de cada uma): ambos os parágrafos acima foram
mesclados nesta atualização; nenhum outro arquivo divergiu (`tasks.md` fez
merge automático, os dois só marcavam checkboxes independentes).

Concluído: **J3.4** — Seleção + wiring + integração do canal RabbitMQ,
fecha a Fase J3 (REQ-43 completo). Novo `codegen/channel_rabbitmq.go`:
`channelProvider(ch)` lê `provider` de `ch.Decl.Entries` (R2, mesmo helper
`configStringLitEntry` que `activeProviderDeps` já usa para
`channelProviders["rabbitmq"]`); `channelProviderKind(ch)` normaliza contra
o registro real — `""` (in-memory de sempre) quando ausente OU não
reconhecido (ex. `"kafka"`, ainda não implementado — nunca um erro de
geração por um rótulo que o front-end já aceita livremente), `"rabbitmq"`
quando reconhecido. `channelConnectionGo(e, ch)` traduz `connection`/`url`
de `ch.Decl.Entries` (R1, mesmo padrão de `databaseConnectionGo`,
`sql_wiring.go`, J1.3): `env("VAR")` vira `os.Getenv("VAR")`, um literal
string vira ele mesmo; sem nenhuma das duas chaves, `""` (sem campo DSN
estático pra cair de volta, `program.Channel` nunca teve um).

`emitChannelTransportVar` (novo) substitui as duas chamadas diretas a
`emitChannelQueueVar` (`decl_policy.go:emitPolicyWireFunc`, lado
consumidor; `codegen.go:generateCmdMainFile`, lado produtor) — despacha
para `emitChannelQueueVar` de sempre (byte-idêntico, NFR-21) quando
`channelProviderKind` não é `"rabbitmq"`, ou para `emitRabbitMQChannelVar`
(novo) quando é. `emitRabbitMQChannelVar` monta
`amqpruntime.NewRabbitMQChannel(<connection>, amqpruntime.RabbitMQConfig{
Exchange: "<From>-<To>", Queue: "<From>-<To>", Concurrency: <workers.
concurrency>, MaxAttempts: <circuitBreaker.threshold>, RetryTTL:
<circuitBreaker.cooldown>, KeyFunc: <mesmo switch de orderBy que o caminho
in-memory já usa>}, <registry>)` + `if err != nil { log.Fatal(err) }`
(fail-closed na inicialização — mesma postura de `emitXADatabaseWiring`);
`workers.maxRate`/`.batchSize` são específicos de `QueueChannelConfig`
in-memory, sem equivalente em `RabbitMQConfig` — silenciosamente ignorados
(nenhuma mentira: REQ-43 nunca promete os dois pro provider real). O
registry (R8/REQ-43.5) é montado INLINE a partir dos `candidates` já
resolvidos pelo CHAMADOR (o Policy.On no consumidor, todo PublicEvent do
módulo produtor no produtor) — nunca via `contracts.EventRegistry()`/
`EventRegistry()`: o tipo já é conhecido ESTATICAMENTE aqui, mesmo
raciocínio que `emitDurableOutboxConstruction` (`decl_policy.go`, J2.5) já
documenta para o outbox durável.

**Achado desta task, corrigido dentro do orçamento (erro pertence ao
escopo, CLAUDE.md):** `RabbitMQConfig.ConsumeDisabled` (novo campo,
`amqprt/rabbitmq.go.txt`) — o lado PRODUTOR só chama `Publish`, nunca
`Subscribe`, mas `NewRabbitMQChannel` sempre declarava fila(s) e subia
consumidores de verdade na construção; como a fila é um recurso
COMPARTILHADO no broker (ao contrário do `QueueChannel` in-memory, onde
cada processo tem sua PRÓPRIA cópia isolada), um consumidor espúrio do lado
produtor competiria pelas mensagens com o consumidor real do outro service
e as descartaria em silêncio (`deliver` roda zero handlers ⇒ sucesso
vacuamente ⇒ `ack` — a mensagem nunca chegaria no handler de verdade).
`ConsumeDisabled: true` faz `declareTopology` só declarar a exchange
principal (Publish precisa dela existir — idempotente, o lado consumidor
pode declará-la de novo sem erro), sem fila/DLX/retry/DLQ nem consumidor
nenhum; `dialAndSetup` então não abre nenhum `consumeChan` para essa
instância. `emitRabbitMQChannelVar` passa `consumeDisabled=true` só na
chamada de `generateCmdMainFile`.

Testes novos: `codegen/channel_rabbitmq_test.go` (reusa a fixture
multi-service de F5, `channel_test.go` — `parseChannelFixture`/
`channelFixtureAlphaModDs`/`channelFixtureBetaPolicyDs` — só acrescentando
`provider: "rabbitmq"` + `connection: env("AMQP_URL")` ao canal):
`TestEmitPoliciesRabbitMQChannelGolden` (lado consumidor: `NewRabbitMQChannel`
com a config/registry esperados, SEM `ConsumeDisabled`, nunca mais
`runtime.NewQueueChannel`), `TestGenerateRabbitMQChannelFixtureProducer
AndConsumerCompile` (lado produtor: `ConsumeDisabled: true`, `uow :=
runtime.NewUnitOfWork(store, alphaChannel)` — MESMA forma de wiring que o
caminho in-memory já usava, só troca o construtor — smoke compile real,
importa `amqp091-go` de verdade) e
`TestChannelRabbitMQUnrecognizedProviderStaysInMemory` (NFR-21: `"kafka"`
não reconhecido ⇒ `runtime.NewQueueChannel` de sempre, nenhuma referência a
`amqpruntime`). `codegen/channel_rabbitmq_integration_test.go` (`//go:build
integration`, mesmo padrão de `sql_postgres_integration_test.go`, J1.4.b):
`TestRabbitMQIntegration` gera o projeto de verdade e roda, via
`gentest.RunTests`, um teste embutido que constrói DUAS instâncias
SEPARADAS de `rabbitmqChannel` (produtora `ConsumeDisabled: true`,
consumidora `ConsumeDisabled: false` com um handler `Subscribe`) contra o
MESMO exchange/queue — `Publish` na produtora chega no handler da
consumidora via um broker real (a prova de NFR-22, publicar→consumir
cross-process == in-process); pula (`t.Skip`, nunca falha) sem `AMQP_URL`
(REQ-48.3/NFR-24) — confirmado compilando com `-tags=integration` e pulando
sem a env var, sem broker algum disponível neste ambiente.

Sem regressão: `go build ./...`/`go build -tags=integration ./...`/
`gofmt -l .`/`go vet ./...`/`go vet -tags=integration ./...` limpos; `go
test ./...` (suíte inteira) verde, incluindo TODOS os testes de
`channel_test.go` (F5, a fixture SEM provider — a prova viva de NFR-21: o
mesmo programa sem provider continua byte-idêntico, nem uma linha mudou) e
`TestGenerate*` (`driver`, wallet/shop e2e — nenhum dos dois declara canal
`provider: "rabbitmq"`, nenhum byte mudou).

Concluído: **J4.2** — `redisLimiter` (RateLimit §16) + fallback local
(REQ-44.2/44.3/44.5). Novo `codegen/redisrt/ratelimit.go.txt` implementa
`runtime.Limiter` sobre `github.com/redis/go-redis/v9` — o contraparte
distribuído dos três algoritmos in-memory de `rtsrc/ratelimit.go.txt`
(`token_bucket`/`sliding_window`/`fixed_window`, que só contam DENTRO do
mesmo processo: cada réplica teria seu próprio balde, nunca uma cota
compartilhada de verdade). `CheckRateLimits`/`RateLimitCheck`
(`rtsrc/ratelimit.go.txt`) reusados SEM NENHUMA mudança (REQ-44.2 pedia
exatamente isso) — `redisLimiter` só implementa a MESMA interface
`runtime.Limiter`.

Atomicidade cross-réplica via script Lua (REQ-44.2): três scripts
(`tokenBucketScript`/`slidingWindowScript`/`fixedWindowScript`) fazem o
read-modify-write inteiro num único `EVAL` (Redis executa um script Lua
atomicamente, single-threaded — o mesmo papel que o mutex por-instância de
cada Limiter in-memory já cumpria localmente, agora coordenando entre
processos via o servidor). Cada script replica a fórmula EXATA do seu
equivalente in-memory (mesmo refill contínuo capado em burst pro
token_bucket; mesmo ZSET aparado por `period` pro sliding_window; mesmo
"NUNCA incrementa quando nega" pro fixed_window — `if allowed { e.count++
}`, nunca incondicional) — replicar fielmente é o que torna REQ-44.3 (mesma
semântica, backend diferente) verdade. `EVAL`, não `EVALSHA+SCRIPT LOAD`
(decisão documentada no arquivo): mais simples, sem o caminho `NOSCRIPT`
que `EVALSHA` exigiria tratar — Redis já cacheia o script internamente por
hash de conteúdo entre chamadas `EVAL` idênticas.

Fallback local, não fail-open total (REQ-44.5, ponto 6): `redisLimiter`
compõe um `runtime.Limiter` in-memory (o MESMO `NewLimiter` que o caminho
de sempre usa, construído uma vez com a MESMA config) — `Allow` tenta o
Redis primeiro; QUALQUER falha (erro de rede OU resposta malformada do
`EVAL`) roteia IMEDIATAMENTE pro Limiter local em vez de propagar o erro —
a proteção continua ativa, agora só por processo, em vez de desligada (um
fail-open total abriria uma janela real de abuso bem na hora em que a
proteção mais importa). Quando o Redis volta, a PRÓXIMA chamada já tenta
ele de novo — nenhum estado "modo degradado" persistido, a contagem global
retoma sozinha.

Registro: `codegen/provider_registry.go` ganha `rateLimitProviders["redis"]`
— MESMO `module`/`adapterDir` de `cacheProviders["redis"]` (J4.1): a dedup
por struct inteira de `activeProviderDeps` (R5) já colapsa as duas
categorias numa única entrada de go.mod/cópia de fontes quando um programa
usa redis nas duas pontas, sem nenhuma mudança adicional. Um efeito
colateral (não uma regressão): `TestActiveProviderDepsUnrecognizedProviders
AreNoOp` (`provider_registry_test.go`) usava `"redis"` como exemplo de
backend de RateLimit NÃO reconhecido — agora que `rateLimitProviders["redis"]`
é real, trocado para `"memcached"` (mesmo exemplo que Cache já usava desde
J4.1).

Testes novos (item c): `codegen/redis_ratelimit_test.go` (`package
codegen`, mesmo padrão de `redis_cache_test.go`/J4.1.c — `gentest.WriteFiles`/
`RunTests` sobre um `fakeScripter` em memória, SEM nenhuma conexão Redis
real; go-redis/v9's `*redis.Client` é uma struct concreta, então só um
campo de interface — `redisScripter`, `Eval` — permite o dublê de teste):
`TestRedisLimiterSelectsScriptAndKeyPerAlgorithm` (script Lua certo por
algoritmo, incl. o default `token_bucket` pra um valor desconhecido, e a
chave namespaced `"ratelimit:<namespace>:<key>"`),
`TestRedisLimiterParsesResultFields`, e o par que prova REQ-44.5:
`TestRedisLimiterFallsBackToLocalOnRedisError`/`TestRedisLimiterFallsBackTo
LocalOnMalformedResponse` (o fallback local NEGA depois de esgotar a cota
configurada — nunca um "libera tudo" disfarçado). `codegen/
redis_ratelimit_integration_test.go` (`//go:build integration`, guardado
por `REDIS_URL`, mesmo padrão de `sql_postgres_integration_test.go`/
`channel_rabbitmq_integration_test.go`): os três algoritmos admitindo até a
cota e negando de verdade contra um Redis vivo, mais um teste de isolamento
entre chaves distintas sob o mesmo namespace; pula sem `REDIS_URL`
(REQ-48.3/NFR-24) — confirmado compilando com `-tags=integration` e
pulando sem Redis disponível neste ambiente.

Sem regressão: `go build ./...`/`go build -tags=integration ./...`/
`gofmt -l .`/`go vet ./...`/`go vet -tags=integration ./...` limpos; `go
test ./...` (suíte inteira) verde; `TestGenerate*` (`driver`, wallet/shop
e2e) — nenhum byte mudou (nenhum dos dois declara `RateLimit { backend:
"redis" }`).

Concluído: **J4.3** — **(R2/R3)** Seleção + wiring, fechando a Fase J4
(REQ-44 completo). `codegen/decl_query_cache.go`: `cacheBackendKind(mod)`
lê `Cache.backend` do mod.ds via `configStringLitEntry` (mesma chave/forma
que `activeProviderDeps` já lia desde J4.1 — nenhuma reinvenção) contra
`cacheProviders` (J4.1); `cacheConnectionGo(e, mod)` traduz
`connection`/`url` para `os.Getenv(...)` (`env(...)`, R1) ou um literal —
mesmo padrão de `channelConnectionGo`/`databaseConnectionGo`.
`emitQueryCacheVar` agora recebe `mod`+`returnGoType`: sem `Cache {
backend: "redis" }` continua emitindo exatamente
`runtime.NewMemoryQueryCache()` (byte-idêntico, NFR-21); com o backend
selecionado, cada Query cacheada abre sua PRÓPRIA conexão
(`redisruntime.OpenClient(os.Getenv(...))` — novo `redisrt/open.go.txt`,
`ParseURL`+`NewClient`+`Ping` com timeout, mesmo padrão de fail-closed no
startup que `sqlruntime.OpenPostgres` já estabeleceu; nunca compartilhada
entre Queries — generaliza o princípio "1 instância por Query" que já
regia o backend in-memory), um `func init()` que faz `panic` se a conexão
falhar e chama `gob.Register(<TipoDeRetorno>{})` (a responsabilidade que
J4.1 documentou como pertencente a esta task), e
`redisruntime.NewRedisQueryCache(client, "<NomeDaQuery>")` no lugar do
in-memory. `codegen/ratelimit.go` ganhou o mesmo tratamento, generalizado
para `routeRateLimitPlan.backend` (resolvido em `planRouteRateLimit` via
`rateLimitBackendKind(modBlock)`) e `pendingRateLimitPlan.modBlock` (para
`rateLimitConnectionGo` resolver a URL só na hora da emissão, já que
`planRouteRateLimit` não tem `*emit.Emitter` em mãos): cada var de
`Limiter` (flat ou por-tier) ganha sua própria conexão Redis do mesmo
jeito, e `emitRateLimitLimiterVar` passa a emitir
`redisruntime.NewRedisLimiter(client, <varName>, algoritmo, limit, período,
burst)` no lugar de `runtime.NewLimiter(...)` quando `backend == "redis"`.
`emitRouteRateLimitChecks` (e portanto `generateCmdMainFile`, que já
propagava erro) passou a devolver `error` — a resolução de `connection`
pode falhar de forma clara (fail-closed) quando `backend: "redis"` está
declarado sem `connection`/`url`.

**(R3)** `TestCacheModuleBlockAcceptsEnvConnection`
(`codegen/redis_provider_wiring_test.go`, novo arquivo): confirma
diretamente, inspecionando o AST devolvido por `driver.CheckProject`, que
`connection: env("REDIS_URL")` dentro de `Cache { ... }` chega em
`ConfigBlock.Entries` como um `*ast.CallExpr` de `env` com 1 argumento
STRING — nenhuma mudança de front-end foi necessária (o parser já aceita
config de módulo livre, confirmando a pré-condição do §design 7). Golden:
`TestEmitQueryCacheRedisBackendGolden` (as 2 Queries cacheadas da fixture
Widget — reaproveitada de J3/`decl_query_cache_test.go` — trocam para o
backend redis, com `OpenClient`/`gob.Register`/`NewRedisQueryCache` por
Query e nenhum `NewMemoryQueryCache()` residual) e
`TestGenerateRateLimitRedisBackendGolden` (nova fixture mínima Orders/
PlaceOrder, 1 rota `perIp`, `RateLimit { backend: "redis" }` — mesma
`FailOpen`/headers/checks que o caminho in-memory já provava, só o
construtor do `Limiter` muda). Smoke:
`TestGenerateCacheRedisBackendSmokeCompile`/
`TestGenerateRateLimitRedisBackendSmokeCompile` rodam o pipeline
`codegen.Generate` completo (não uma `EmitX` isolada) + `gentest.
SmokeCompile` sobre os bytes de fato escritos — prova que `go.mod`/
`redisruntime/*.go` vendorados por `activeProviderDeps`/J0 (nenhuma mudança
necessária ali — já eram genéricos desde J4.1/J4.2) mais o wiring novo
compilam de verdade contra `github.com/redis/go-redis/v9`, sem abrir
conexão nenhuma.

Sem regressão: `go build ./...`/`go build -tags=integration ./...`/
`gofmt -l .`/`go vet ./...`/`go vet -tags=integration ./...` limpos; a
suíte completa de `decl_query_cache_test.go`/`ratelimit_test.go` (golden,
determinismo, smoke, comportamental) continua verde SEM NENHUMA alteração
— a prova viva de NFR-21/23 (nenhum programa sem `backend: "redis"` muda
1 byte); `TestGenerate*` (`driver`, wallet/shop e2e) idem; `go test ./...`
(suíte inteira) verde.

Concluído: **J5.1** — `s3FileStorage`, abrindo a Fase J5 (REQ-45). Novo
pacote `codegen/s3rt` (mesmo padrão `.go.txt` embutido de
sqlrt/amqprt/redisrt): `s3rt/filestorage.go.txt` implementa
`runtime.FileStorage` sobre `github.com/aws/aws-sdk-go-v2` — `Store`:
`PutObject` com key ÚNICA (`runtime.UUID()`, nunca hash de conteúdo —
§design infra-providers 3.5 corrige explicitamente essa nuance vs. uma
leitura anterior de "determinística"), `Name`/`Metadata` preservados como
object metadata do S3 sob uma chave reservada (`ds-name`,
`mergeMetadataWithName`/`splitMetadataName`); `Load`: `GetObject`,
reconstruindo `Name`/`ContentType`/`Size`/`Metadata` da resposta de
VERDADE do S3 (nunca dos campos do `FileRef` de entrada — S3 é a fonte da
verdade, diferente de `memoryFileStorage`, que guarda o `File` inteiro),
`types.NoSuchKey` mapeado para `runtime.ErrNotFound`; `SignedURL`:
presigned GET real (`s3.PresignClient`, REQ-45.2) — sem `error` (contrato
do seam), uma falha de presign é logada (`log/slog`) e devolve `""`;
`Delete`: `DeleteObject`, idempotente de graça (S3 não erra numa key
ausente). `NewS3FileStorage(ctx, bucket, region)` resolve credenciais pela
cadeia padrão do AWS SDK (`config.LoadDefaultConfig`, REQ-47.1 — nunca
hardcoded). `s3PutObjectAPI`/`s3GetObjectAPI`/`s3DeleteObjectAPI`/
`s3PresignAPI` isolam a superfície de chamadas usada (mesmo padrão
`redisCmdable`/`redisScripter` de J4.1/J4.2, já que `*s3.Client` é uma
struct concreta) — `newS3FileStorage` (construtor não-exportado) é o ponto
de injeção de dublês nos testes.

**(b) FileStream — desvio documentado (REQ-45.3):** nenhum método/adapter
para `runtime.FileStream` — `codegen/lower/builtins.go` não emite hoje
nenhuma operação sobre ele (G1a cobre só `File`/`FileRef` inteiros em
memória), então não haveria corpo Go gerado capaz de exercitá-lo; registrado
como trabalho futuro condicionado a `builtins.go` emitir essas ops primeiro.

**(c)** `codegen/s3_filestorage_test.go` (novo, `package codegen` — mesmo
padrão de `redis_cache_test.go`/J4.1.c): um `package s3runtime` white-box
embutido como string, compilado e RODADO de verdade (`gentest.WriteFiles`/
`RunTests`) dentro de um módulo Go efêmero (`rtsrc.Sources()` +
`s3rt.Sources()`) — `fakeS3` (mapa + mutex, sem rede) prova round-trip
Store→Load (metadata/Name preservados, chave reservada nunca vaza de
volta), key ÚNICA em 2 `Store` com bytes idênticos, `NoSuchKey` →
`ErrNotFound`, `Delete` idempotente, e o par de `SignedURL` (caminho feliz
+ falha do presigner devolvendo `""`, nunca propagando erro).

Registro: `codegen/project.go` ganhou `awsS3Module`/`awsS3Version` +
`awsConfigModule`/`awsConfigVersion` (S3 é o PRIMEIRO provider desta Fase J
que precisa de DOIS módulos Go diretamente importados pelo adapter, não só
um — `EmitGoMod` acrescenta a segunda linha `require` explicitamente quando
`fileProviders["s3"]` está ativo, mesma técnica que o bloco `otelAdapter`
já usa para seus 4 módulos) e `s3MinGoVersion = "1.24"`.
`codegen/provider_registry.go` ganhou `fileProviders["s3"]` e
`providerSources["s3runtime"]` — `activeProviderDeps`/
`generateProviderRuntimeFiles` já eram genéricos o bastante desde J0 para
não precisar de nenhuma mudança. `provider_registry_test.go` trocou seu
exemplo de provider de `FileStorage` NÃO reconhecido de `"s3"` (agora real)
para `"gcs"` (continua fora de escopo, §design infra-providers §5).

Sem regressão: `go build ./...`/`go build -tags=integration ./...`/
`gofmt -l .`/`go vet ./...`/`go vet -tags=integration ./...` limpos; `go
test ./codegen/... ./driver/...` verde (inclui `TestActiveProviderDeps*`
ajustado); nenhum programa sem `FileStorage { provider: "s3" }` muda 1
byte (NFR-21 — nenhuma fixture existente declara S3).

Concluído: **J5.2** — **(R2)** Seleção + wiring, fechando a Fase J5
(REQ-45 completo) e o 5-provider slice inteiro de Marco J (Postgres/
Outbox/RabbitMQ/Redis/S3). `codegen/decl_filestorage.go` ganhou
`fileStorageProvider(fs)` (lê `provider` de `fs.Decl.Entries` via
`configStringLitEntry`, R2 — mesmo padrão de `channelProvider`),
`fileStorageProviderKind(fs)` (normaliza contra `fileProviders`, "" quando
ausente/não reconhecido — NFR-21) e `fileStorageConfigGo(e, fs, key)`
(traduz `bucket`/`region` via `env(...)` ou literal, mesmo padrão de
`cacheConnectionGo`/`rateLimitConnectionGo`/`channelConnectionGo` — chave
ausente é erro de geração claro, fail-closed). `codegen.go`: o loop `for
_, name := range wt.fileStorages` (dentro de `generateCmdMainFile`, ainda
`func main()` direto — sem `run() error`, isso é J6.2) agora resolve
`fileStorageProviderKind` por FileStorage; sem `"s3"` continua emitindo
exatamente `<pkg>.WireFileStorage(name, runtime.NewMemoryFileStorage())`
(byte-idêntico); com `"s3"`, emite `<var>, err :=
s3runtime.NewS3FileStorage(context.Background(), <bucketGo>, <regionGo>)`
+ `if err != nil { log.Fatal(err) }` (mesmo padrão fail-closed de
`emitXADatabaseWiring`/`sql_wiring.go`, único resource aberto aqui — o
padrão multi-resource `run() error` do §design 3.6 é escopo de J6.2) + `
<pkg>.WireFileStorage(name, <var>)`.

Testes novos: `codegen/s3_filestorage_wiring_test.go`
(`TestGenerateFileStorageS3BackendGolden`,
`TestGenerateFileStorageS3BackendSmokeCompile`,
`TestFileStorageUnrecognizedProviderStaysInMemory` — mesmo padrão de
`channel_rabbitmq_test.go`/J3.4: reaproveita a fixture Docs de
`filestorage_test.go` acrescentando só `provider: "s3"` +
`bucket/region: env(...)` ao bloco `FileStorage{}` do mod.ds, prova que os
DOIS caminhos leem a MESMA declaração). `codegen/
s3_filestorage_integration_test.go` (`//go:build integration`, guardado
por `S3_BUCKET` — região fixa `"us-east-1"` em vez de `env(...)` no
fixture de integração especificamente, para que só `S3_BUCKET` precise
estar no ambiente, mesmo espírito dos demais integration tests):
`TestS3FileStorageIntegrationParityWithMemory` roda Store->Load->
SignedURL->Delete->Load contra `runtime.NewMemoryFileStorage()` e contra
um bucket S3 real, comparando Name/ContentType/Buffer/Metadata — a prova
de NFR-22; confirmado compilando com `-tags=integration` e pulando sem
`S3_BUCKET` neste ambiente.

Sem regressão: `go build ./...`/`go build -tags=integration ./...`/
`gofmt -l .`/`go vet ./...`/`go vet -tags=integration ./...` limpos; a
suíte completa de `filestorage_test.go` (golden, determinismo, smoke,
comportamental) continua verde SEM NENHUMA alteração — a prova viva de
NFR-21/23 (nenhum programa sem `FileStorage{provider:"s3"}` muda 1 byte);
`go test ./...` (suíte inteira) verde.

**Follow-up (PR #32):** o Gemini Code Assist apontou, já depois da PR #31
mesclada, que `fsVar := strings.ToLower(name[:1]) + name[1:] + "FS"`
colidiria em Go se DOIS módulos DISTINTOS do mesmo grupo de serviços
declarassem uma FileStorage de MESMO nome (ex. "ContentStorage") — duas
vars `:=` no MESMO escopo de `func main()`. Corrigido prefixando pelo
pacote do módulo: `fsVar := wt.pkg + name + "FS"`. PR pequena e separada
(#32, já mesclada), já que #31 mesclou antes do fix ficar pronto.

Concluído: **J6.1** — **(R7)** Fixture-âncora multi-service, abrindo a
Fase J6 (REQ-47/48) — o fechamento de Marco J. Novo
`codegen/anchor_fixture_test.go`: 3 services, 5 módulos, os CINCO
providers reais ativos ao mesmo tempo no MESMO programa —
`AnchorOrdersSvc{AnchorOrders}` (Database postgres decorativo + emite o
PublicEvent `AnchorOrderPlaced` cross-service via RabbitMQ),
`AnchorCatalogSvc{AnchorCatalog}` (Cache+RateLimit redis numa Query
cacheada + rota com rateLimit, FileStorage s3 via `store`/`load File`),
`AnchorBillingSvc{AnchorBilling, AnchorInvoice, AnchorNotify}`
(`AnchorBilling` consome `AnchorOrderPlaced` via RabbitMQ;
`AnchorInvoice`/`AnchorNotify` — UseCase+Policy LOCAIS, mesmo service, sem
canal — provam a Policy `AtLeastOnce` com Outbox durável, `AnchorNotify`
com Database postgres própria).

**Dois achados arquiteturais pré-existentes (fora do escopo de
infra-providers, contornados na fixture, registrados como desvio/issue):**
1. Uma Policy cross-service (consumindo via canal `queue`) NUNCA é
   elegível ao Outbox durável no código atual — `emitPolicyWireFunc`
   (decl_policy.go, J2.5) só promove `o` a `*runtime.DurableOutbox` para
   Policies de alvo LOCAL (`info.channel == nil`); uma Policy cross-service
   sempre usa `runtime.NewOutbox(<canal>)`, mesmo com Database real +
   `delivery AtLeastOnce`. A durabilidade cross-service do REQ-42.6 é sobre
   o outbox do PRODUTOR alimentando o canal ao publicar — um mecanismo
   distinto de "promover o Outbox local do CONSUMIDOR". Por isso a
   fixture prova as duas pontas SEPARADAMENTE (`AnchorBilling` prova o
   canal; `AnchorInvoice`/`AnchorNotify`, local, provam o Outbox durável) —
   nenhuma mudança de código foi necessária, só de estrutura da fixture.
2. `generateCmdMainFile` (F5/G3, ANTES de Marco J) recusa combinar, no
   MESMO service, um módulo PRODUTOR de canal de saída com um módulo que
   precisa de `runtime.Dispatcher` (Policy local OU Query cacheada) — as
   duas formas de construir a `UnitOfWork` do service são incompatíveis.
   Por isso Cache/RateLimit/FileStorage (REQ-44/45) vivem em
   `AnchorCatalog`, um 3º service SEM canal nenhum, separado de
   `AnchorOrders` (o produtor).
3. **(ISSUE-11, novo)** o parser falha em analisar duas atribuições
   simples CONSECUTIVAS (`x = load Y(id)` seguido de `y = ...`) — erro de
   SINTAXE no `=` da segunda, mesmo sendo gramática válida; uma "ensure"
   (ou qualquer outro statement) entre as duas evita o bug. Fora do escopo
   de infra-providers (é `parser/`) — contornado na fixture inserindo um
   "ensure ... exists else ..." entre os dois `load`; registrado em
   `.claude/issues.md` para uma task dedicada no `parser/`.

**Desvio registrado (R10, item c) — build offline com `vendor/` real não
implementado nesta task.** O critério pede `go build -mod=vendor` contra
um `vendor/` materializado a partir da árvore vendorizada do PRÓPRIO
repositório domainscript (§design infra-providers §2.2) — isso exigiria o
compilador (este módulo) passar a depender de verdade dos 4 drivers
(pgx/amqp091-go/go-redis/aws-sdk-go-v2) só para vendorizá-los, e uma
função nova de `codegen` para copiar o subconjunto ativo para o `vendor/`
do projeto GERADO — uma mudança de escopo maior (dezenas de MB no repo,
mecanismo nunca usado aqui antes) do que cabe nesta task isoladamente;
levantado explicitamente ao usuário antes de decidir (a pergunta não
recebeu resposta síncrona — decisão tomada: interim, sem bloquear a
task). Interim: `TestAnchorFixtureSmokeCompile` prova o MESMO smoke
(build+vet reais, sobre os bytes escritos em disco, NFR-17) via `go mod
tidy` — a mesma técnica que TODA a suíte de J1-J5 já usa — em vez de
`-mod=vendor`. A vendorização de verdade fica como follow-up explícito
(não um `ISSUE-N` — é o próprio critério R10/c da task, registrado aqui
e em `tasks.md`).

Sem regressão: `go build ./...`/`gofmt -l .`/`go vet ./...` limpos; `go
test ./codegen/... ./driver/...` verde; `go test ./...` (suíte inteira)
verde.

Concluído: **J6.2** — **(R1)** Wiring multi-recurso fail-closed com `run()
error` (REQ-47.2/47.3, §design infra-providers 3.6). Novo
`codegen/run_error.go`: `emitFailFast`/`emitFailFastBlock` (a checagem de
erro de UM passo fallível — `runMode==false`, o caso comum, preserva
EXATAMENTE `log.Fatal(errVar)` inline, byte-idêntico a antes desta task;
`runMode==true` troca para `return errVar`), `emitDeferClose` (`defer
<dbVar>.Close()` para um recurso `*sql.DB`), `emitDeferChannelClose`
(`defer <var>.(interface{ Close() error }).Close()` para o canal
produtor rabbitmq — o var tem tipo estático `runtime.ChannelTransport`,
sem `Close()` na interface; a asserção de tipo nunca falha porque só é
emitida quando o provider já é "rabbitmq"). `codegen.go`
(`generateCmdMainFile`) ganhou o cálculo de `runMode`: conta os recursos
reais fallíveis do service (canal produtor rabbitmq, Database do outbox
por módulo, Database XA por módulo — tratada como UMA unidade mesmo
abrindo db+store, mesmo enquadramento do "sqlite de hoje" no §design
3.6 —, FileStorage s3 por módulo); com 2+ no MESMO service, o corpo
inteiro emitido antes como `func main()` passa a ser `func run() error`,
e um `func main()` novo e curto chama `run()` e faz o `log.Fatal` ÚNICO;
com 0 ou 1 (wallet, shop, e toda fixture de J1-J5 hoje — nenhuma combina
2+ num mesmo service), `runMode` fica `false` e absolutamente NADA muda.
`emitXADatabaseWiring`/`emitOutboxDatabaseWiring`
(sql_wiring.go)/`emitChannelTransportVar`/`emitRabbitMQChannelVar`
(channel_rabbitmq.go) ganharam o parâmetro `runMode` (o lado CONSUMIDOR
de canal, chamado por `emitPolicyWireFunc`/decl_policy.go dentro de
`Wire(d)` — uma função diferente, sem retorno de erro — sempre passa
`runMode=false`, preservando seu comportamento de sempre).

**(b)** Novo `codegen/run_error_test.go`: fixture sintética de 3 módulos
sem `topology.ds` (1 grupo default "app") — `RunOrders` (UseCase,
`PublicEvent RunOrderPlaced`), `RunShipping` (Policy-only, `delivery
AtLeastOnce` local + Database postgres própria → 1º recurso, o Outbox
durável), `RunDocs` (UseCase-only, `FileStorage { provider: "s3" }` → 2º
recurso) — nenhum dos dois módulos produz canal nem precisa de Dispatcher
junto de um produtor (evita a limitação pré-existente já documentada em
J6.1). `TestGenerateRunErrorMainUsesRunErrorForm` prova a forma completa:
`func run() error` com `runShippingOutboxDB, err :=
sqlruntime.OpenPostgres(...)` + `defer runShippingOutboxDB.Close()` +
`return err`, `rundocsRunDocsStorageFS, err :=
s3runtime.NewS3FileStorage(...)`, terminando em `return
server.ListenAndServe()`; e um `func main()` CURTO (só `if err := run();
err != nil { log.Fatal(err) }`, sem nenhum wiring direto).
`TestGenerateRunErrorSmokeCompile` prova que o projeto INTEIRO, com essa
forma ativa, compila e `go vet`-limpa de verdade.

Sem regressão: `go build ./...`/`go build -tags=integration ./...`/
`gofmt -l .`/`go vet ./...`/`go vet -tags=integration ./...` limpos;
TODA a suíte existente (`go test ./codegen/... ./driver/...`) continua
verde SEM NENHUMA alteração — incl. `decl_policy_outbox_test.go`
(1 recurso — outbox — continua gerando `log.Fatal` inline, sem
`run()`), `channel_rabbitmq_test.go` (1 recurso — canal — idem),
`s3_filestorage_wiring_test.go` (1 recurso — S3 — idem) e o
`anchor_fixture_test.go` de J6.1 (cada um dos 3 services tem só 1
recurso PRÓPRIO — `runMode` fica `false` nos três) — a prova viva de
NFR-21/23; `go test ./...` (suíte inteira) verde.

Concluído: **J6.3** — Determinismo + NFR-21 consolidado, fechando a Fase
J6 (REQ-47/48). Novo `codegen/infra_providers_determinism_test.go`:

**(a)** `TestAnchorFixtureDeterministic` — regenera a fixture-âncora de
J6.1 (os cinco providers ativos ao mesmo tempo, 3 services) duas vezes e
compara TODOS os arquivos byte a byte (`gentest.Deterministic`, mesma
técnica de `TestFileStorageDeterministic`/
`TestSharedCollectionTypeDeterministic`: concatena `Path+"\x00"+Content`
de cada arquivo). Cobre `go.mod`, imports, cada `main.go` de service, e as
fontes de adapter copiadas (redisrt/s3rt/amqprt/sqlrt) — **não** cobre
`go.sum`/`vendor/`, que nunca chegaram a existir neste ciclo (o desvio de
vendoring real, R10, registrado em J6.1, continua em aberto — ver abaixo).

**(b)** `TestNoProviderDeclaredMeansCoreOnly` — uma fixture nova, mínima
(`Baseline`, 1 módulo, 1 Aggregate/UseCase, SEM Database/Cache/RateLimit/
FileStorage/topology.ds nenhum — a prova mais simples possível de "zero
dos cinco providers") gera `go.mod` **exatamente**
`"module domainscript/generated\n\ngo 1.22\n"` (sem bloco `require`
nenhum) e **nenhum** arquivo sob `sqlruntime/`/`amqpruntime/`/
`redisruntime/`/`s3runtime/` — consolida, numa única fixture, a mesma
prova que cada task individual (J1-J5) já fazia por categoria.
**Achado incidental (não é um bug, registrado só para contexto):**
`docs/examples/wallet`/`shop`/`pizzeria` já declaram `provider: "postgres"`/
`"rabbitmq"` como rótulos que ERAM decorativos quando escritos — como
"postgres" (J1.2) e "rabbitmq" (J3.1) agora são reconhecidos de verdade,
esses exemplos deixaram de ser um baseline "zero provider" — por isso
J6.3.b precisou de uma fixture NOVA em vez de reusar wallet/shop; a
atualização da documentação desses exemplos (postgres/rabbitmq deixam de
ser "só rótulo") já é o item J7.1.b, não tocado aqui.

Sem regressão: `go build ./...`/`go build -tags=integration ./...`/
`gofmt -l .`/`go vet ./...` limpos; `go test ./codegen/... ./driver/...`
verde; `go test ./...` (suíte inteira) verde.

Concluído: **J7.1** — Revisão contra a DoD + atualização de docs (REQ-48.4),
fechando o plano infra-providers inteiro (REQ-41..48). Revisão sistemática
da DoD (`requirements.md` §5, 6 critérios):

1. Fixture-âncora compila/vet limpo (NFR-17) — **satisfeito**
   (`anchor_fixture_test.go`, J6.1).
2. Outbox durável alimenta o canal cross-service (REQ-42.1/42.6) —
   **satisfeito só PARCIALMENTE**: o lado consumidor (Policy local +
   Database real → `DurableOutbox`) está de verdade (J2.5); o lado
   produtor (`emitDurableOutboxConstruction`, `codegen/decl_policy.go`)
   NUNCA passa um `publisher` para `NewDurableOutbox`, e
   `generateCmdMainFile` continua publicando direto no commit para um
   módulo produtor de canal — nenhum emissor de UseCase/Handle chama
   `tx.EnqueueOutbox`. O runtime seam suporta e testa isso isoladamente
   (`codegen/sql_outbox_channel_test.go`), mas o codegen nunca liga os dois
   lados. Este é o achado mais significativo da revisão — documentado sem
   suavização em ISSUE-9 (status final) e em `gaps.md` §G-4 ("Residual
   aberto"), e referenciado de volta por ISSUE-3.
3. `go.mod` exato / NFR-21/23 — **satisfeito**, exceto que `go.sum`/
   `vendor/` reais nunca existiram (desvio R10, já registrado em J6.1,
   permanece aberto).
4. Três camadas de teste (golden + smoke compile + unit de dialeto,
   integração pulada sem infra real, NFR-24) — **satisfeito**, com a mesma
   ressalva do item 3: os smoke tests usam `go mod tidy` (rede), não
   `-mod=vendor` genuíno offline.
5. wallet/shop sem regressão (NFR-19) / `go build`/`go test` — **satisfeito**
   (confirmado por toda a suíte verde ao longo de J1-J6, sem alteração
   byte a byte nos exemplos existentes).
6. Cinco categorias opt-in e isoladas atrás do seam — **satisfeito**
   (Postgres/RabbitMQ/Redis-Cache/Redis-RateLimit/S3, cada um só ativado
   quando o `.ds` declara o provider correspondente).

Atualização de docs (item b da task): `docs/examples/pizzeria/sales/mod.ds`,
`docs/examples/pizzeria/kitchen/mod.ds` e `docs/examples/pizzeria/README.md`
("Adaptações" itens #3/#4) — removida a alegação, agora falsa, de que
`"postgres"`/`"rabbitmq"` são "só rótulos decorativos"; substituída por uma
descrição precisa: ambos são providers REAIS desde Marco J (J1.2/J3.1),
mas a geração do pizzeria continua bloqueada por uma limitação PRÉ-EXISTENTE
e não relacionada (ISSUE-7, UseCase+Policy no mesmo módulo) antes de
alcançar qualquer wiring de provider; MongoDB (Kitchen) permanece decorativo,
fora do recorte de 5 providers de Marco J.

`.claude/specs/codegen/gaps.md` §G-4 reescrito: nova tabela por categoria
(antes/depois) e nova seção "Residual aberto" documentando com precisão de
código o gap do item 2 acima, o desvio de vendoring (R10), e as categorias
explicitamente fora de escopo (outros bancos, gRPC-canal, Dynamo,
`layered` cache, GCS/Azure). `.claude/issues.md`: ISSUE-3 ganhou nota de
fechamento parcial (recorte de 5 fechado, residual apontado para ISSUE-9);
ISSUE-9 ganhou nota de status final confirmando que nem J3.4 nem J6
fecharam o lado produtor — continua ABERTA.

Sem regressão: `go build ./...`/`go build -tags=integration ./...`/
`gofmt -l .`/`go vet ./...` limpos; `./dsc docs/examples/pizzeria` continua
validando limpo (mudança só em comentários); `go test ./...` verde.

Marco J (infra-providers) está **fechado** com este task: o recorte
deliberado de 5 providers (Postgres/Outbox/RabbitMQ/Redis/S3) está
implementado, testado e documentado; o residual (produtor→outbox→canal,
vendoring real, e as categorias fora de escopo) fica registrado para um
ciclo futuro, não reaberto aqui.

## correcoes-issues-9-10-11 — `.claude/specs/correcoes-issues-9-10-11/tasks.md`

Marco K ("Correções de dívida técnica"), REQ-49..51 — ciclo de manutenção que
fecha três issues em aberto na raiz: ISSUE-11 (parser, duas atribuições
consecutivas — causa-raiz: binding ganancioso de `parseQueryOp`), ISSUE-10
(`memoryQueryCache.Coalesce` sem `defer`, vaza goroutine sob pânico) e ISSUE-9
(produtor Outbox→canal cross-service, resíduo do Marco J / REQ-42.6). Três
fases independentes (K1/K2/K3), executadas em ordem de risco crescente.

Concluído: **K1.1** — guarda de fim-de-linha no **binding** de operação de
domínio (REQ-49.1/49.2/49.4, ISSUE-11). Helper `sameLineAsPrev()`
(`parser/parser.go`) compara `p.cur().Pos.Line` com `p.lastPos.Line` (o fim do
último token consumido); `parseQueryOp` (`parser/parse_query.go`) ganhou
`&& p.sameLineAsPrev()` na guarda do binding opcional — sem ela, após
`order = load Bar(id)` o parser engolia o `x` da linha seguinte (de `x = id`)
como binding, deixando o `=` órfão (erro "esperava uma expressão, encontrei
="). O alias de `join` (mesma heurística gananciosa) fica para **K1.2** —
intocado nesta task. Testes pareados (NFR-4) em `parser/parse_query_test.go`:
`TestConsecutiveAssignsDoNotStealBinding` (duas atribuições consecutivas,
variantes `x = id` e `x = 1` → dois `AssignStmt`, zero diagnóstico) e
`TestLegitimateBindingPreserved` (`list Ticket t where …` na mesma linha
mantém o binding `t`). Suíte inteira do `parser/` verde; `go build ./...`
limpo; `gofmt -l` sem apontar os arquivos tocados. Próxima task: **K1.2**
(mesma guarda no alias de `join` em `parseOneClause`).

Concluído: **K1.2** — mesma guarda de fim-de-linha no **alias de `join`**
(REQ-49.3, ISSUE-11). `parseOneClause` (`parser/parse_query.go`, `case
"join":`) ganhou `&& p.sameLineAsPrev()` na guarda do alias opcional,
reusando o helper `sameLineAsPrev()` já criado em K1.1 — sem ela, um `join
Foo` seguido na linha seguinte por um statement começando com identificador
(ex. `x = id`) engolia esse identificador como alias, deixando o `=` órfão.
Testes pareados (NFR-4) em `parser/parse_query_test.go`:
`TestConsecutiveAssignsDoNotStealJoinAlias` (`join Foo` numa linha + `x = id`/
`x = 1` na seguinte → alias vazio, `join` e o statement seguinte parseiam
como dois nós independentes, zero diagnóstico) e
`TestLegitimateJoinAliasPreserved` (`join Order o` na MESMA linha do alvo
mantém o alias `o` intacto). Fase K1 (ISSUE-11) está **completa**. Suíte
inteira do `parser/` verde; `go build ./...` limpo; `go vet ./...` limpo;
`gofmt -l` sem apontar os arquivos tocados. Próxima task: **K2.1** (fase K2,
`memoryQueryCache.Coalesce` — flag + erro-sentinela aos esperadores,
ISSUE-10).

Concluído: **K2.1** — `memoryQueryCache.Coalesce` à prova de pânico do líder
(REQ-50.1/50.2/50.3/50.4, ISSUE-10, backend memory). `codegen/rtsrc/
querycache.go.txt` ganhou o sentinela de pacote `errCoalescedPanic =
errors.New("coalesced function panicked")` (import `errors` adicionado) e,
antes de `fn()`, um `defer` que — sob lock — faz `delete(c.flights, key)`, e
se a flag local `completed` ainda for `false` (ou seja, `fn()` nunca
terminou, o líder panicou no meio) força `fl.err = errCoalescedPanic` antes
de `close(fl.done)`; `completed = true` roda logo após `fn()` retornar
normalmente. **Sem `recover`** em lugar nenhum — o pânico do líder continua
propagando para quem chamou `Coalesce`; o `defer` só protege quem está
esperando o mesmo voo. Comentário no código aponta a paridade com
`redisQueryCache.Coalesce` (`codegen/redisrt/cache.go.txt`, que já tinha o
`defer` de limpeza da PR #26 mas NÃO a flag/sentinela — isso é o escopo de
**K2.2**, não tocado aqui). Causa-raiz confirmada por leitura do wrapper
gerado (`codegen/decl_query_cache.go`): sem o sentinela, um esperador
liberado por um pânico do líder receberia `(nil, nil)` e o `result.(T)` do
wrapper panicaria uma SEGUNDA vez para tipos de valor — daí o fix ser um erro
não-nil aos esperadores, não só um `defer` de limpeza. Testes pareados
(NFR-4) adicionados a `codegen/decl_query_cache_test.go`, todos batendo
DIRETO em `runtime.NewMemoryQueryCache()` (sem passar por uma Query cacheada
gerada — testando o primitivo, não o wiring), embutidos como um novo arquivo
Go gerado (`widgets/coalesce_panic_sentinel_test.go`) dentro do mesmo projeto
sintético que `TestQueryCacheBehavior` já monta e roda via
`gentest`/`runGeneratedTests`:
`TestCoalescePanicPropagatesToLeaderAndReleasesWaiterWithError` (negativo: um
`fn` que panica é `recover()`ado pela goroutine líder do teste — Coalesce não
recupera sozinho —; uma 2ª goroutine no mesmo voo, liberada sob timeout de
2s, recebe um erro NÃO-nil em vez de travar; a MESMA key coalesce de novo
depois com um `fn` de sucesso, provando que não ficou presa em `c.flights`),
`TestCoalesceSingleFlightSameResultNonRegression` (positivo: 8 goroutines na
mesma key veem o mesmo resultado e `fn` roda exatamente 1 vez — mesmo idioma
de `TestStampedeProtectionSingleFlight`, mas direto no primitivo) e
`TestCoalesceBusinessErrorPropagatesNotSentinel` (um `fn` com erro de negócio
propaga ESSE erro, via `errors.Is`, a todos os esperadores — nunca o
sentinela de pânico). `go test ./codegen/ -run TestQueryCacheBehavior` verde
(2.4s) — os 3 novos `TestCoalesce*` rodam dentro dele, no `go test ./...` do
projeto gerado; sanity rodando também `TestEmitQueryCache*`
(golden/determinístico/smoke) sem regressão. `go build ./...` limpo; `go vet
./...` limpo; `gofmt -l` sem apontar `codegen/decl_query_cache_test.go` nem
`codegen/rtsrc/querycache.go.txt`. `codegen/redisrt/cache.go.txt`
NÃO tocado (fica para K2.2). Próxima task: **K2.2** (mesmo endurecimento —
flag + sentinela — no backend `redisQueryCache`, REQ-50.5, mesma fase K2).

Concluído: **K2.2** — `redisQueryCache.Coalesce` à prova de pânico do líder
(REQ-50.5, ISSUE-10, backend redis). Mesmo endurecimento de K2.1, mirrorado
no adapter distribuído: `codegen/redisrt/cache.go.txt` ganhou o sentinela de
pacote `errCoalescedPanic = errors.New("coalesced function panicked")` (MESMA
mensagem que o sentinela do backend memory, para grepabilidade — dois `var`
não-exportados distintos, um por pacote, sem conflito de compilação; `errors`
já estava importado, nenhum import novo) e, dentro de `Coalesce`, o `defer`
de limpeza da PR #26 ganhou a flag `completed` + `if !completed { fl.err =
errCoalescedPanic }` antes do `close(fl.done)`, com `completed = true` logo
após `fn()` retornar normalmente — **sem `recover`**, mesma invariante: o
pânico do líder continua propagando para quem chamou `Coalesce`, o `defer` só
protege quem está esperando o mesmo voo. Comentário do arquivo atualizado
para refletir o novo comportamento (flag+sentinela, não só limpeza),
cross-referenciando `memoryQueryCache.Coalesce` (`rtsrc/querycache.go.txt`)
para a nota de paridade. Testes pareados (NFR-4) adicionados dentro do string
Go embutido `redisCacheTest` (`codegen/redis_cache_test.go`) — que já roda,
via `TestRedisQueryCacheAdapter`, como `package redisruntime` (white-box)
compilado e testado de verdade num projeto Go sintético efêmero
(`gentest.WriteFiles`/`RunTests`), já que `cache.go.txt` não é compilado
diretamente por este módulo. As 3 novas funções (renomeadas com prefixo
`TestRedis` para não colidir semanticamente com as equivalentes do backend
memory, embora vivam em pacotes diferentes) constroem `*redisQueryCache` via
`newRedisQueryCache(newFakeCmdable(), ns)` — `Coalesce` nunca toca `c.client`,
então um `fakeCmdable` vazio (nenhum Get/Set/Incr configurado) já basta, zero
conexão real: `TestRedisCoalescePanicPropagatesToLeaderAndReleasesWaiterWithError`
(negativo: um `fn` que panica é `recover()`ado pela goroutine líder do teste;
uma 2ª goroutine no mesmo voo, liberada sob timeout de 2s, recebe um erro
NÃO-nil em vez de travar; a MESMA key coalesce de novo depois com um `fn` de
sucesso), `TestRedisCoalesceSingleFlightSameResultNonRegression` (positivo: 8
goroutines na mesma key veem o mesmo resultado e `fn` roda exatamente 1 vez)
e `TestRedisCoalesceBusinessErrorPropagatesNotSentinel` (um `fn` com erro de
negócio propaga ESSE erro, via `errors.Is`, a todos os esperadores — nunca o
sentinela de pânico). Import `sync/atomic` adicionado ao string embutido
(necessário para os novos testes; `sync` já estava presente). Fase K2
(ISSUE-10) está **completa** nos DOIS backends. `go test ./codegen/ -run
TestRedisQueryCacheAdapter` verde (~37s, roda `go mod tidy` + `go test ./...`
do projeto sintético, incluindo os 3 novos testes); sanity re-rodando `go
test ./codegen/ -run TestRedisQueryCache` sem regressão. `go build ./...`
limpo; `go vet ./...` limpo; `gofmt -l codegen/redis_cache_test.go
codegen/redisrt/cache.go.txt` sem saída.

Concluído: **K3.1** — detecção do produtor durável (predicado puro, sem
emissão; REQ-51 condição de ativação, ISSUE-9, §design
correcoes-issues-9-10-11 4.1). Nova função `durableProducer(prog *program.Program,
module string) (bool, error)` em `codegen/sql_wiring.go` (ao lado de
`recognizedSQLProvider`/`moduleOutboxDatabaseName`, que ela reusa junto com
`producerChannelFor`/`channelProviderKind` de `channel.go`/
`channel_rabbitmq.go` — cross-cutting entre banco e canal, cabe melhor aqui
que em `codegen.go`, que não tem hoje um lugar natural de "predicados de
módulo" fora de `moduleMarks`, propositalmente não tocado nesta task).
Condição de ativação, as DUAS precisam valer: (1) o módulo tem **exatamente
1** Database real (`recognizedSQLProvider`, contagem sobre
`mod.Databases`); (2) o módulo tem um canal de saída
(`producerChannelFor`) cujo `channelProviderKind == "rabbitmq"` — um
`via: queue` sem `provider:` real (a `QueueChannel` in-memory, a forma do
`shop`) ou um provider declarado mas não reconhecido (ex. `"kafka"`) ambos
resolvem `channelProviderKind` para `""`, portanto `false`. Erro de
`producerChannelFor` (mais de um canal de saída via queue — guarda F5
pré-existente) é propagado ao chamador, não mascarado como `false` — segue a
mesma convenção dos chamadores existentes de `producerChannelFor`
(`generateCmdMainFile`). Decisão registrada em comentário no código: **2+
Database reais no mesmo módulo devolve `false`**, não `true` — a leitura do
design (§4.1 "Sem 2PC", §4.2-P1 "banco único, não-2PC") e de
`usecase2PCPlan`/`emitXADatabaseWiring` (`decl_usecase.go`/`sql_wiring.go`)
mostra que 2+ Database reais já disparam o caminho XA/2PC pré-existente,
ortogonal ao produtor de banco único que REQ-51 endereça; `durableProducer`
não deve colidir com/duplicar esse reconhecimento. Testes unitários leves
(construção direta de `*program.Program`/`*program.Module`/
`*program.Channel`, mesmo padrão de `sql_wiring_test.go` — sem passar pelo
driver/parser), em `codegen/durable_producer_test.go`:
`TestDurableProducerPostgresPlusRabbitMQ` (positivo: postgres + canal
`provider:"rabbitmq"` → true), `TestDurableProducerInMemoryChannelIsNotDurable`
(o gotcha central: postgres + canal `via: queue` SEM `provider:`, a forma do
`shop` → false), `TestDurableProducerUnrecognizedChannelProviderIsNotDurable`
(provider declarado mas não reconhecido, ex. `"kafka"` → false),
`TestDurableProducerNoRealDatabase` (sub-testes sem-database/
provider-não-reconhecido, ex. `"mongodb"`, mesmo com canal rabbitmq → false),
`TestDurableProducerNoOutboundChannel` (postgres sem nenhum canal de saída,
a forma do `wallet` → false), `TestDurableProducerTwoRealDatabasesIsNotSingleDatabaseProducer`
(2 Databases reais + canal rabbitmq → false, documentando a decisão acima).
Task **puramente aditiva**: nenhuma chamada nova a `durableProducer` em
`generateCmdMainFile` nem em nenhum outro ponto de emissão — função ainda
não consumida (dead code do ponto de vista do gerador), então nenhuma saída
gerada muda (wallet/shop/fixture-âncora seguem byte-idênticos, confirmado
rodando a suíte mais ampla abaixo, sem nenhuma golden/smoke precisar
atualizar). `go test ./codegen/ -run TestDurableProducer` verde (7 testes/
sub-testes, <10ms); `go test ./codegen/... ./driver/...` completo (mais
amplo que o exigido, para provar "zero mudança de saída gerada") verde,
~169s, sem regressão em nenhum golden/smoke/e2e existente. `go build ./...`
limpo; `go vet ./...` limpo; `gofmt -l codegen/sql_wiring.go
codegen/durable_producer_test.go` sem saída. Próxima task: **K3.2**
(`emitSingleDatabaseWiring` — store `database/sql` real para o produtor de
banco único, publisher ainda inalterado; REQ-51.5, pré-condição do resto de
K3).

Concluído: **K3.2** — `emitSingleDatabaseWiring`: store `database/sql` real
para o produtor de banco único, publisher (canal) inalterado (REQ-51.5,
ISSUE-9, §design correcoes-issues-9-10-11 4.2-P1). Nova função
`emitSingleDatabaseWiring(e *emit.Emitter, prog *program.Program, moduleName,
pkgAlias, dbName, channelVarName string, runMode bool) error` em
`codegen/sql_wiring.go`, ao lado de `emitOutboxDatabaseWiring` (mesmo estilo
de emissão — `databaseConnectionGo`/`provider.openFunc`, `emitFailFast`,
`emitDeferClose`) mas SEM 2PC e SEM `*sqlruntime.EventStore` intermediário:
ao contrário de `emitXADatabaseWiring`, `sqlruntime.NewUnitOfWork(db *sql.DB,
registry, dialect, publisher ...runtime.Publisher)` (confirmado em
`sqlrt/uow.go.txt:68`) já recebe o `*sql.DB` direto e abre sua própria
`*sql.Tx` a cada `Run` — a linha final emitida é
`uow := sqlruntime.NewUnitOfWork(<db>, <pkg>.EventRegistry(),
sqlruntime.<Dialect>(), <canal>)`, com o CANAL como publisher, exatamente
como o caminho in-memory que substitui (a troca de publisher/enqueue no
outbox durável continua K3.3, fora deste escopo). `durableProducer` (K3.1)
**manteve sua assinatura** `(prog, module) (bool, error)` — não foi alterada;
o nome do único Database real do produtor é obtido reusando
`moduleOutboxDatabaseName(prog, producerModule)` (que já devolve "o primeiro
Database real em ordem alfabética" — como `durableProducer` garante
exatamente 1, esse primeiro É o único), evitando duplicar a iteração sobre
`mod.Databases` e evitando qualquer mudança de assinatura em K3.1.

`codegen/codegen.go`, `generateCmdMainFile`: `producerDurable`/
`producerPkgAlias` resolvidos logo após a guarda F5/G3 pré-existente
(produtor+Dispatcher no mesmo service continua barrado, então nunca
precisam coexistir); o switch que decide como construir `uow` ganhou um novo
`case producerChannel != nil && producerDurable` ANTES do `case
producerChannel != nil` genérico, chamando `emitSingleDatabaseWiring` em vez
de `runtime.NewUnitOfWork(store, <canal>)`. Decisão registrada em comentário
de código: a linha `store := runtime.NewMemoryEventStore()` continua sendo
emitida INCONDICIONALMENTE (não é removida no caso do produtor durável) —
achado desta task, `store` é uma variável de escopo de SERVIÇO passada
sempre a `newMux(store)`/`newGRPCServer(store)` (rotas de Query via
`<pkg>.<Query>(ctx, store, ...)`), ortogonal ao `uow` de escrita; removê-la
quebraria a compilação de qualquer rota de Query do service (mesmo de outro
módulo do mesmo grupo) sem nenhum ganho — a troca do produtor é só de QUAL
`uow` se constrói. `resourceCount` (runMode, J6.2) ganhou mais um incremento
quando `producerDurable` é true (abrir o Database do produtor é mais um
recurso fallível em sequência, ao lado do canal) — decisão consistente com a
convenção já existente (cada abertura conta 1).

**Efeito colateral achado e corrigido nos testes:** `durableProducer`
qualifica qualquer módulo com 1 Database real + canal `provider:"rabbitmq"`
— não só a fixture-âncora de J6. A fixture sintética de
`channel_rabbitmq_test.go` (`Alpha`, `Database MainDb { provider: "postgres"
}` + canal rabbitmq) também qualifica, então
`TestGenerateRabbitMQChannelFixtureProducerAndConsumerCompile` precisou da
MESMA atualização deliberada de asserção (documentada em comentário no
próprio teste). Atualizado: `TestAnchorFixtureOrdersMainWiresRabbitMQProducer`
(`anchor_fixture_test.go`) agora também exige
`sqlruntime.OpenPostgres(os.Getenv("PG_URL"))` e
`uow := sqlruntime.NewUnitOfWork(anchorOrdersDB, anchororders.EventRegistry(),
sqlruntime.PostgresDialect(), anchorOrdersChannel)`, com uma asserção
negativa provando que `runtime.NewUnitOfWork(store, anchorOrdersChannel)` NÃO
aparece mais; `TestGenerateRabbitMQChannelFixtureProducerAndConsumerCompile`
(`channel_rabbitmq_test.go`) espelha a mesma troca para `alphaDB`/`alpha`.
`TestAnchorFixtureSmokeCompile`/`TestAnchorFixtureDeterministic` seguem
verdes sem mudança (smoke/determinismo são indiferentes à FORMA do wiring,
só exigem Go válido/estável). `wallet` e `shop` confirmados byte-idênticos
(nenhum dos dois ativa `durableProducer`: `wallet` não tem canal de saída;
`shop/Orders` tem canal `via: queue` SEM `provider:` — a `QueueChannel`
in-memory, não um transporte real) — `driver.TestGenerateWalletE2E*`/
`TestGenerateShopE2E*` verdes sem regeneração de golden.

Testes novos: `codegen/single_database_wiring_test.go`
(`TestEmitSingleDatabaseWiringShape`, `runMode=false` — abre a conexão,
`uow := sqlruntime.NewUnitOfWork(...)` com o canal, sem defer/EventStore
intermediário; `TestEmitSingleDatabaseWiringRunModeEmitsFailFastAndDeferClose`,
`runMode=true` — `return err`/`defer Close()` em vez de `log.Fatal` inline;
`TestEmitSingleDatabaseWiringUnknownDatabaseIsGenerationBug` — guarda de bug
de geração, mesma convenção de `emitXADatabaseWiring`/
`emitOutboxDatabaseWiring`) e `codegen/sql_producer_parity_test.go`
(`TestLedgerSingleDatabaseProducerParity`/
`TestSQLUnitOfWorkPublishesAfterCommitLikeMemoryUnitOfWork` — paridade
comportamental NFR-22: reusa a fixture Ledger/sqlite já provada por
`TestLedgerSingleDatabaseBehavior`, injeta um `fakeChannelPublisher` no 4º
argumento de `sqlruntime.NewUnitOfWork` e roda `PerformDebit` de verdade via
`gentest.RunTests` — confirma que o evento apensado é publicado LOGO APÓS o
commit, na mesma ordem, exatamente como `runtime.NewUnitOfWork(store,
publisher)` em memória já fazia, e que o Append continua acontecendo
independente do publisher).

`go test ./codegen/ -run "TestEmitSingleDatabaseWiring|TestLedgerSingleDatabaseProducerParity|TestAnchorFixture|TestGenerateRabbitMQChannelFixture"`
verde; `go test ./codegen/... ./driver/...` completo verde (~151s, sem
regressão em nenhum golden/smoke/e2e existente, incluindo wallet/shop). `go
build ./...` limpo; `go vet ./...` limpo; `gofmt -l` sem apontar nenhum dos
arquivos tocados (`codegen/sql_wiring.go`, `codegen/codegen.go`,
`codegen/anchor_fixture_test.go`, `codegen/channel_rabbitmq_test.go`,
`codegen/single_database_wiring_test.go`,
`codegen/sql_producer_parity_test.go`). Próxima task: **K3.3** (troca
atômica — irredutível: enqueue in-tx via `tx.EnqueueOutbox` antes do commit,
trocar o publisher da UoW do produtor do canal para o `DurableOutbox`, e
subir `StartOutboxRelay`/`StartOutboxCleanup` do produtor; REQ-51.1/.2/.3/.4).

Ver `tasks.md` para o mapa de dependências (K3.1 é pré-condição do fluxo do
produtor; K3.2 é a pré-condição de armazenamento que K3.3 precisa para ter
onde persistir a linha do outbox).

Concluído: **K3.3** — troca atômica (irredutível) do produtor Outbox → canal:
enqueue-in-tx + relay + troca do publisher da UoW, as três metades juntas
(REQ-51.1/51.2/51.3/51.4, ISSUE-9, §design correcoes-issues-9-10-11
4.2-P2/P3/P4). Fecha, no nível de codegen, o resíduo aberto do Marco J
(REQ-42.6): um módulo produtor com Database real + canal `provider:"rabbitmq"`
agora enfileira o `PublicEvent` cross-service no outbox durável DENTRO da tx e
o relay publica no canal — nunca mais o publish-direto-no-commit. A
consolidação final (docs/gaps.md/issues.md) é K3.5.

As quatro peças:
- **P2 (runtime, `codegen/sqlrt/uow.go.txt`):** novo construtor DISTINTO
  `NewOutboxUnitOfWork(db, registry, dialect, outboxEventTypes map[string]bool)`
  ao lado de `NewUnitOfWork` (campo novo `outboxEventTypes` na struct
  `UnitOfWork`, reusa o mesmo `Run`). No `Run`, ANTES do `Commit`, filtra os
  apensados cujo `EventType()` está em `outboxEventTypes` e chama
  `tx.EnqueueOutbox(<filtrados>)` na MESMA `*sql.Tx` do `Append` (atômico,
  REQ-51.1/51.4); publisher fica nil de propósito — NÃO publica pós-commit.
  **Decisão de assinatura:** construtor distinto, não um parâmetro a mais em
  `NewUnitOfWork` — mantém `NewUnitOfWork(...,publisher)` byte-idêntico para
  todo caller existente (o seam de `sql_outbox_channel_test.go`, o 2PC), e o
  nome deixa explícito que este caminho não publica. `rtsrc/uow.go.txt` (a
  INTERFACE `runtime.Tx`/`UnitOfWork`) **NÃO foi tocado** — `EnqueueOutbox` já
  está na interface `Tx`; o produtor só usa a UoW sql.
- **P3 (`codegen/sql_wiring.go`/`codegen.go`):** `emitSingleDatabaseWiring`
  troca `channelVarName` por `outboxEventTypes []string` na assinatura e emite
  `uow := sqlruntime.NewOutboxUnitOfWork(<db>, EventRegistry(), dialect,
  map[string]bool{...})` — o canal deixa de ser o 4º argumento (publisher) da
  UoW.
- **P4 (`codegen/sql_wiring.go`/`codegen.go`):** novo helper
  `emitProducerOutboxRelay` monta, em `main.go`, `<mod>OutboxStore :=
  sqlruntime.NewOutboxStore(<mod>DB, dialect)` sobre a MESMA conexão que a UoW
  já abriu (uma só conexão), `<mod>Outbox := runtime.NewDurableOutbox(<store>,
  map[string]runtime.EventFactory{...contracts...}, <canal>)` e sobe
  `go <mod>Outbox.Start(workerCtx)` + uma goroutine de cleanup periódico
  (ticker + `<mod>Outbox.Cleanup`, mesma cadência/retenção de
  `outboxCleanupTickInterval`/`outboxCleanupRetention` do lado consumidor).
- **Wiring (`generateCmdMainFile`):** `producerOutboxEventTypes` (nomes dos
  `PublicEvent` do produtor, `buckets[producerModule].pubEvents`, ordenado
  NFR-13) computado junto de `producerDurable`; alimenta P3 e P4. Os gates de
  `context`/`workerCtx` ganharam `|| producerDurable` (o produtor durável passa
  a precisar de `workerCtx` para o relay/cleanup — sem isso, referência a var
  não declarada). O relay é emitido DEPOIS de `workerCtx :=` e de `Wire`.

**Decisão arquitetural A-vs-B (a chamada de fundo de K3.3):** o
DurableOutbox + relay + cleanup do produtor moram INLINE em `main.go` (rota
A), **não** como funções `StartOutboxRelay`/`WireOutboxStore` no pacote do
módulo (a forma do CONSUMIDOR, `decl_policy.go`). Razão forçada: o canal
(`<mod>Channel`) é uma var LOCAL de `main()`/`run()` (construída por
`emitChannelTransportVar`), e o `DurableOutbox` precisa dele como publisher —
só `main.go` tem o canal em escopo. Um módulo produtor é UseCase-only (wirado
por `emitUOWWireFunc`, que não conhece canal), então emitir funções de pacote
como o consumidor exigiria empurrar o canal para dentro do pacote do módulo —
mudança maior e desnecessária. A rota A é também o que o design pede
literalmente (§4.2-P4 "em `main.go`, montar o `OutboxStore`, construir
`runtime.NewDurableOutbox(...)`"), deixa o pacote do módulo produtor
byte-idêntico, e concentra tudo em `main.go`. Documentada em comentário
proeminente em `emitProducerOutboxRelay`.

**Discrepância registrada (fonte vs. prompt/design):** o design diz "registry
… via `contracts.EventRegistry()`" — mas `contracts.EventRegistry()` **NÃO
existe**: o pacote `contracts/` só tem ALIASES de tipo por PublicEvent
(`type X = <módulo>.X`), sem registry próprio (decl_event.go documenta isso; e
`channel_rabbitmq.go`/`emitDurableOutboxConstruction` já montam registry
INLINE pela mesma razão). Segui o padrão correto/existente: registry INLINE
`map[string]runtime.EventFactory{ "Evt": func() runtime.Event { return
&contracts.Evt{} } }` sobre `buckets[producerModule].pubEvents`, chaveado
exatamente pelo `event_type` que o outbox grava (REQ-51.4 routing-by-type).

**runMode (J6.2):** o produtor durável já contava 2 recursos fallíveis (canal
rabbitmq + Database, K3.2) ⇒ `runMode=true`; o `OutboxStore` reusa a conexão
`<mod>DB` (`NewOutboxStore` é infalível, não abre conexão) ⇒ nenhum novo
recurso, nenhuma mudança na contagem. fail-fast/defer-close seguem corretos.

**Testes:** `TestAnchorFixtureOrdersMainWiresRabbitMQProducer`
(`anchor_fixture_test.go`) e `TestGenerateRabbitMQChannelFixtureProducerAnd
ConsumerCompile` (`channel_rabbitmq_test.go`) atualizados para exigir
`NewOutboxUnitOfWork(... map[string]bool{"<Evt>": true})`, `NewOutboxStore`,
`NewDurableOutbox(..., <canal>)` e `go <mod>Outbox.Start(workerCtx)`, com
prova negativa de que a forma K3.2 (`sqlruntime.NewUnitOfWork(<db>, ...,
<canal>)`) sumiu (o canal só entra no DurableOutbox agora); o segundo continua
com smoke compile real (o novo wiring do DurableOutbox/relay compila+vet com
amqp091-go). `single_database_wiring_test.go` atualizado para a nova
assinatura (`[]string` de event_type em vez de `channelVarName`) e a linha
`NewOutboxUnitOfWork`. `sql_producer_parity_test.go` **reescrito** (a premissa
K3.2 "canal = publisher da UoW que publica pós-commit" é exatamente o que K3.3
remove): agora `TestLedgerSingleDatabaseProducerOutbox` prova, sobre o Ledger
gerado + sqlite real, que `NewOutboxUnitOfWork` enfileira o evento no outbox na
MESMA tx do Append (linha em `outbox` E `events`, `delivered_at` NULL — não
publicado) e FILTRA (REQ-51.4) um evento cujo tipo não está no conjunto do
canal (apensado ao stream, nunca ao outbox). `sql_outbox_channel_test.go`
inalterado (usa a forma 3-arg de `NewUnitOfWork`, preservada).

`wallet` e `shop` confirmados byte-idênticos (nenhum ativa `producerDurable`:
`wallet` sem canal; `shop/Orders` com canal `via: queue` SEM `provider:` real
⇒ cai no `case producerChannel != nil` inalterado) — `driver.TestGenerate
WalletE2E*`/`TestGenerateShopE2E*` verdes (layout, smoke compile,
`RegenTwoDirsByteIdentical`), nenhum golden regenerado. `go test ./codegen/...
./driver/...` COMPLETO verde (~226s codegen, ~10s driver). `go build ./...`
limpo; `go vet ./codegen/...` limpo; `gofmt -l` sem apontar nenhum dos 7
arquivos tocados (`codegen/sqlrt/uow.go.txt`, `codegen/sql_wiring.go`,
`codegen/codegen.go`, `codegen/single_database_wiring_test.go`,
`codegen/anchor_fixture_test.go`, `codegen/channel_rabbitmq_test.go`,
`codegen/sql_producer_parity_test.go`). `lower/stmt.go` (emit) e o corpo do
UseCase **não** tocados (rota (b), §design 4.2-P2). Próxima task: **K3.4**
(fixture dedicada `producer_outbox_test.go` + comportamental de crash simulado
fim-a-fim: relay re-publica após falha, nenhum evento perdido — REQ-51.7).

Concluído: **K3.4** — fixture dedicada `codegen/producer_outbox_test.go`
(ISSUE-9/REQ-51.7, §design correcoes-issues-9-10-11 4.4/4.5), espelhando como
o lado consumidor ganhou `decl_policy_outbox_test.go` (J2.5): 1 módulo
produtor "Orders" (`Database MainDb provider:"sqlite"`, DSN de arquivo real —
deliberadamente sqlite, não postgres como a âncora/Alpha, porque
`recognizedSQLProvider` reconhece os dois e sqlite evita a decolagem
"gera-com-postgres/testa-com-sqlite" que outras fixtures precisam) + canal de
saída `queue provider:"rabbitmq"` para 1 módulo consumidor "Shipping" (uma
Policy `AtLeastOnce` simples, sem Database próprio — só para o canal ter dois
lados reais, sem exercitar durabilidade do consumidor, já coberta). O
Aggregate `Order` tem DOIS `Handle`: `Place` emite o `PublicEvent`
`OrderPlaced` (o único tipo que o canal carrega) e `Touch` emite o `Event`
interno `OrderTouched` (nunca cross-service) — para o filtro REQ-51.4 ser
provado também sobre o conjunto de event_type que vem do canal REAL da
fixture na geração, não de um mapa escrito à mão como em
`sql_producer_parity_test.go` (K3.3).

Dois testes novos:
- `TestProducerOutboxFixtureWiringAndSmokeCompile` — wiring do produtor
  durável sobre esta fixture dedicada: `cmd/orderssvc/main.go` abre
  `sqlruntime.Open`, constrói `uow := sqlruntime.NewOutboxUnitOfWork(ordersDB,
  orders.EventRegistry(), sqlruntime.SQLiteDialect(),
  map[string]bool{"OrderPlaced": true})` (só `OrderPlaced` — `OrderTouched`
  nunca aparece em `main.go`, confirmando REQ-51.4 já na geração), monta
  `ordersOutboxStore`/`ordersOutbox := runtime.NewDurableOutbox(...,
  ordersChannel)` e sobe `go ordersOutbox.Start(workerCtx)`; provas negativas
  de que as formas antigas (`NewUnitOfWork(store, ordersChannel)`,
  `sqlruntime.NewUnitOfWork(ordersDB, ...)` com o canal como publisher)
  sumiram. Fecha com `gentest.SmokeCompile` (DoD "smoke compile limpo").
- `TestProducerOutboxDurableRelayRetriesAfterCrash` — o teste comportamental
  que era o gap genuíno: gera o projeto, embute um arquivo `package orders`
  (white-box, precisa da var de pacote não-exportada `uow`) que (1) chama
  `sqlruntime.NewOutboxUnitOfWork` com o MESMO conjunto de event_type do
  canal real e invoca `PlaceOrderUseCase`/`TouchOrderUseCase` de verdade —
  confirma outbox+events na mesma tx para `OrderPlaced` e o filtro REQ-51.4
  para `OrderTouched` (nenhuma linha de outbox) — e (2), o ponto novo,
  constrói manualmente um `runtime.NewDurableOutbox(store, registry, pub)`
  (mesma forma que `emitProducerOutboxRelay` monta em `main.go`, mas
  orquestrado no teste via `Tick` em vez de `Start` para controlar o
  timing) com um `producerOutboxFakePublisher` (idioma de `fakePublisher`,
  `sql_outbox_channel_test.go`, redeclarado localmente — pacote gerado
  diferente) que falha na 1ª tentativa: 1º `Tick` processa a linha que
  `PlaceOrderUseCase` enfileirou de verdade, `Publish` falha, `attempts`
  sobe para 1, `delivered_at` continua NULL; 2º `Tick` re-escaneia a MESMA
  linha, `Publish` sucede, `delivered_at` é marcado, nenhum evento
  cross-service perdido. Achado de implementação: como `contracts.<Evt>` é
  um ALIAS DE TIPO para o tipo do módulo de origem (`contracts` importa
  `orders`, nunca o inverso, `decl_event.go`), o teste embutido em `package
  orders` referencia `OrderPlaced`/`Touch...` SEM qualificador `contracts.` —
  importar `domainscript/generated/contracts` dali causaria um ciclo de
  import (`go test` falhou com exatamente esse erro na primeira tentativa,
  corrigido removendo o import e usando os tipos locais do próprio pacote).

O que K3.3 já cobria e este teste REAFIRMA (não duplica como se fosse gap):
`sql_producer_parity_test.go` (`TestLedgerSingleDatabaseProducerOutbox`) já
prova enqueue-in-tx + filtro REQ-51.4 sobre o Ledger; `channel_rabbitmq_test.go`
(`TestGenerateRabbitMQChannelFixtureProducerAndConsumerCompile`) já prova a
MESMA forma de wiring sobre Alpha/Beta com smoke compile real. O que era
GENUINAMENTE novo e é o que este teste fecha: o crash simulado (`Publish`
falhando 1x, retry no `Tick` seguinte, nenhum evento perdido) rodando sobre o
**caminho gerado do produtor** (`NewOutboxUnitOfWork`/`EventRegistry()` reais,
alimentados por um UseCase de verdade) — `sql_outbox_channel_test.go` já
provava o MESMO mecanismo de retry, mas só via o seam manual
(`tx.EnqueueOutbox` chamado à mão), nunca através de código gerado.

Verificação: `go test ./codegen/ -run
"TestProducerOutboxFixtureWiringAndSmokeCompile|TestProducerOutboxDurableRelayRetriesAfterCrash"`
verde; `go test ./codegen/... ./driver/...` completo sem regressão; `go build
./...` limpo; `go vet ./...` limpo; `gofmt -l .` sem apontar
`codegen/producer_outbox_test.go` (único arquivo tocado — nenhuma mudança em
`codegen`/`sqlrt`/`rtsrc` de produção, task TEST-ONLY conforme o escopo de
K3.4). Próxima task: **K3.5** (docs/consolidação — `gaps.md` §G-4 remove o
item produtor→outbox→canal, `.claude/issues.md` marca ISSUE-9
`RESOLVED`, `CLAUDE.md`/`README.md` atualizados — a última task de Marco K,
fecha REQ-51 formalmente).

Concluído: **K3.5** — docs + consolidação, fechando REQ-51 formalmente
(§design 4.4, NFR-25). Task só-docs, nenhum código tocado:

- `.claude/specs/codegen/gaps.md` §G-4: a linha "Outbox" da tabela por
  categoria atualizada para refletir o fechamento (produtor→canal
  cross-service agora real, não só a tabela SQL); o item "Outbox → canal
  cross-service (REQ-42.6, ISSUE-9)" da subseção "Residual aberto" riscado
  e substituído por uma nota de fechamento (K3.1-K3.4, com o fluxo completo
  descrito: `durableProducer` → `emitSingleDatabaseWiring` →
  `NewOutboxUnitOfWork` (enqueue in-tx) → `NewDurableOutbox` com o canal
  como publisher + relay/cleanup → prova comportamental de crash simulado
  sobre o caminho gerado). O parágrafo "Fechar o residual exige" reescrito
  para "Fechar o restante exige" (só sobra o item (b), categorias fora do
  recorte de Marco J — o item (a), específico do produtor→outbox, foi
  removido por não haver mais nada a fechar ali).
- `.claude/issues.md`: ISSUE-9 ganhou uma entrada `RESOLVED (commits
  1137ba9/e2f3ec9/9fd30f0/c580e1f, K3.1-K3.4)` — os 4 hashes de merge das
  PRs #47-#50 — resumindo o fluxo completo implementado e apontando de
  volta para `gaps.md` §G-4. ISSUE-10/ISSUE-11 **não** tocadas aqui —
  ficam para **K.fim** (a task de fechamento do Marco K inteiro,
  explicitamente pede marcar as três issues juntas).
- `CLAUDE.md`: dois pontos. (1) O parágrafo "Current state" (linha ~20)
  reescrito — a frase que descrevia o residual como sobrevivendo à
  fechada de Marco J foi substituída por uma descrição do fechamento via
  Marco K; a lista de spec sets ganhou a 6ª entrada
  (`correcoes-issues-9-10-11`, antes ausente — corrigido um descuido de
  J7.1/tasks anteriores que nunca a listaram apesar da spec já existir).
  (2) O parágrafo "Infra providers" no bloco de milestones (linha ~280)
  também reescrito — mantém o relato histórico de que J7.1 encontrou o
  residual (não reescreve a história), mas agora aponta para o fechamento
  por Marco K logo em seguida, com um novo parágrafo "Correções de dívida
  técnica" descrevendo K1/K2/K3 (K1/K2 done, K3.1-K3.4 done, K3.5/K.fim
  restantes).
- `README.md` (raiz): conferido — não faz nenhuma afirmação sobre o
  produtor publicar direto no commit; nada a mudar ali.
- Confirmação (sem `go test ./...` — CI roda a suíte na PR, CLAUDE.md):
  `go build ./...`/`go vet ./...`/`gofmt -l .` limpos; nenhuma mudança de
  código-fonte nesta task, então `wallet`/`shop`/a âncora seguem
  byte-idênticos por construção (nada foi tocado que pudesse afetá-los).

Próxima task: **K.fim** — revisão de DoD do Marco K inteiro
(`requirements.md` §5): as três issues fechadas com par de testes (REQ-50
nos DOIS backends); `wallet`/`shop` byte-idênticos; âncora de J6 atualizada;
`.claude/issues.md` marca ISSUE-9/10/11 `RESOLVED` (ISSUE-9 já está, falta
10/11); `.claude/state.md` marca o Marco K `done`. Última task do plano
inteiro.

Concluído: **K.fim** — revisão de DoD do Marco K inteiro
(`requirements.md` §5, 5 critérios), fechando o plano
`correcoes-issues-9-10-11` por completo. Revisão item a item:

1. Duas atribuições consecutivas parseiam limpo; binding/alias legítimos
   preservados (REQ-49) — **satisfeito** (K1.1/K1.2, par de testes por
   ponto, suíte inteira do `parser/` verde).
2. Pânico em `fn()` dentro de `Coalesce` nos DOIS backends não vaza
   goroutine nem trava a chave; esperadores recebem erro, nunca
   `(nil, nil)`; pânico do líder ainda propaga; coalescência normal e erro
   de negócio legítimo não regridem (REQ-50) — **satisfeito** (K2.1/K2.2,
   par de testes por backend).
3. Produtor com Database real + canal rabbitmq enfileira o `PublicEvent`
   cross-service no outbox atômico e o relay publica (nunca o commit
   direto); crash simulado entre commit e publish não perde o evento
   (REQ-51) — **satisfeito** (K3.1-K3.4: fixture dedicada + âncora de J6 +
   teste de wiring + comportamental fim-a-fim sobre o caminho GERADO do
   produtor, não só o seam manual). Produtor sem a condição de ativação
   permanece byte-idêntico (`shop/Orders`, canal sem provider real).
4. `go build`/`go vet`/`gofmt -l` limpos em toda task; testes de escopo de
   cada task verdes; `wallet` E `shop` byte-idênticos em toda a fase K3
   (confirmado repetidamente via `driver.TestGenerate{Wallet,Shop}E2E*`,
   nenhuma regeneração de golden); asserções da âncora de J6
   (`AnchorOrders`) atualizadas deliberadamente em K3.2 e de novo em K3.3
   (a natureza incremental do wiring do produtor) — **satisfeito**.
5. `.claude/issues.md` marca as três issues `RESOLVED`: ISSUE-9 já estava
   (K3.5); esta task adiciona as entradas `RESOLVED` de **ISSUE-10**
   (commits `9d5fe16`/`bc6df20`, K2.1/K2.2) e **ISSUE-11** (commits
   `3a7437e`/`2abce08`, K1.1/K1.2), cada uma resumindo o fix de raiz e
   confirmando que a causa-raiz analisada no registro original bateu com
   a implementação real. `.claude/specs/codegen/gaps.md` §G-4 já refletia
   o fechamento desde K3.5 — **satisfeito**.

Sem `go test ./...` local neste fechamento (CI roda a suíte completa nas
PRs, conforme CLAUDE.md) — só `go build ./...`/`go vet ./...`/`gofmt -l .`
confirmados limpos; nenhuma mudança de código-fonte nesta task (só
`.claude/issues.md`/`.claude/state.md`), então nada a regenerar.

**Marco K (`correcoes-issues-9-10-11`, REQ-49..51) está FECHADO** — as
três issues (ISSUE-9/10/11) resolvidas na raiz, cada uma com par de
testes, `wallet`/`shop` sem regressão em nenhuma das 9 tasks (K1.1, K1.2,
K2.1, K2.2, K3.1-K3.5, K.fim).

## correcoes-issues-6-7-8 — `.claude/specs/correcoes-issues-6-7-8/tasks.md`

Marco L ("Correções de dívida técnica"), REQ-52..54 — segundo ciclo de
manutenção, fecha três dívidas do back-end na raiz até onde codegen/runtime/sema
alcançam: ISSUE-7 (Wire unificado p/ UseCase+Policy no mesmo módulo — desbloqueia
o `pizzeria`, remove-o do `KNOWN_UNGENERATABLE` do CI), ISSUE-6 (semântica plena
dos testes gerados — fatia tratável de 5 itens; acesso NEGADO delimitado por
exigir nova gramática) e ISSUE-8 (refino §22.7 em `sema` + reclassificação de
§4.4/§25 como "aguardando spec"). Três fases independentes (L1/L2/L3). Ver
`tasks.md` para o mapa de dependências.

Concluído: **L1.1** (emissão do `Wire` combinado no módulo misto,
REQ-52.1/52.2/52.3, §design 2.2). Raiz: `generateModuleFiles`
(`codegen/codegen.go`) recusava de propósito um módulo com UseCase E Policy,
porque `emitUOWWireFunc` (usecases.go → `func Wire(u UnitOfWork)`) e
`emitPolicyWireFunc` (policies.go → `func Wire(d Dispatcher)`) emitiam duas
`func Wire` no mesmo pacote Go (colisão "redeclared in this block"). Estratégia
de extração escolhida — **variantes internas, sem mudar a assinatura pública de
`EmitUseCases`/`EmitPolicies`** (25 call sites de teste seguem intactos, e os
casos puros byte-idênticos por construção, pois seguem literalmente o mesmo
caminho de antes): (1) `EmitUseCases` passou a delegar a `emitUseCasesBytes(...,
emitWire bool)`, que só pula a chamada de `emitUOWWireFunc` quando `emitWire=false`
(caso misto); (2) `EmitPolicies` foi fatiada em `emitPolicyDeclsAndVars` (tudo
menos a `func Wire`, reusável) + a Wire; `emitPolicyWireFunc` foi decomposta em
`emitPolicyWirePreamble` (declarações de pacote pré-Wire) + `emitPolicyWireBody`
(corpo do Wire, posicionado dentro do bloco pelo chamador), ambos reusados
idênticos; (3) o novo `emitCombinedWireFunc` reusa preamble+body e só troca a
assinatura para `func Wire(u UnitOfWork, d Dispatcher)` prependendo `uow = u`;
(4) o novo `emitPoliciesCombinedBytes` monta policies.go do módulo misto
(decls/vars + Wire combinado). `generateModuleFiles` removeu a guarda, calcula
`mixed := hasUseCases && hasPolicies` e roteia: usecases.go via
`emitUseCasesBytes(..., !mixed)` e policies.go via `emitPoliciesCombinedBytes`
quando misto (senão os emissores públicos de sempre). O call site em
`generateCmdMainFile` (`codegen.go`, ~1343-1352) **já** montava `Wire(uow,
dispatcher)` corretamente para o módulo misto (append de `"uow"` quando
`hasUseCases`, `"dispatcher"` quando `hasPolicies`) — nada a mudar ali nesta
task (é a próxima, L1.2, que basicamente escreve o teste que prova isso).
Testes (par NFR-4, `codegen/mixed_wire_test.go`, novo):
`TestGenerateMixedModuleWiresCombinedWireAndCompiles` (fixture sintética Kitchen
UseCase+Policy local → gera, exatamente 1 `func Wire(` no pacote, assinatura
combinada, `uow = u`, `kitchen.Wire(uow, dispatcher)` em main.go,
`gentest.SmokeCompile` verde) +
`TestGeneratePureUseCaseModuleKeepsSingleArgWire` /
`TestGeneratePurePolicyModuleKeepsSingleArgWire` (guardas de byte-identidade dos
casos puros — Wire single-arg preservada). Verificação: `go build ./...`,
`go vet ./codegen/...`, `gofmt -l` limpos; `go test ./codegen/... ./driver/...`
verde (232s, zero regressões — wallet/shop golden/e2e intactos).

Concluído: **L1.2** (call site em `main.go` para o módulo misto, REQ-52.4,
§design 2.2). Confirmado por leitura: `generateCmdMainFile`
(`codegen/codegen.go`, ~1364-1373) **já** monta `args` corretamente para
QUALQUER `wireTarget` — `"uow"` quando `wt.hasUseCases`, `"dispatcher"` quando
`wt.hasPolicies` — produzindo `<pkg>.Wire(uow, dispatcher)` para o caso misto
(ordem batendo com a assinatura combinada `func Wire(u UnitOfWork, d
Dispatcher)` que `emitCombinedWireFunc`/L1.1 emite) e `<pkg>.Wire(uow)`/
`<pkg>.Wire(dispatcher)` para os casos puros, sem NENHUMA mudança de código
necessária nesta task — L1.1 já tinha deixado o call site certo. Teste
DEDICADO adicionado (`codegen/mixed_wire_maincall_test.go`, novo, distinto do
teste de L1.1 que prova isso só incidentalmente via SmokeCompile):
`TestGenerateMixedModuleMainCallSiteWiresBothArgs` (módulo misto →
`kitchen.Wire(uow, dispatcher)`, guarda negativa contra formas de 1 argumento
ou ordem trocada) + `TestGeneratePureModulesMainCallSiteUnchanged`
(sub-testes só-UseCase/só-Policy → `orders.Wire(uow)`/`shipping.Wire(dispatcher)`
inalterados, sem o combinado).

**Achado central da task (Parte 2, REQ-52.7):** a instrução da própria task
("o `Kitchen` do pizzeria não esbarra na guarda F5/G3... é UseCase+Policy
local, sem canal próprio") estava **ERRADA** — confirmado por leitura de
`docs/examples/pizzeria/{topology.ds,kitchen/domain.ds,kitchen/policy.ds}` e
por reprodução empírica parcial (`dsc gen docs/examples/pizzeria`, e uma cópia
de trabalho isolada usada só para diagnóstico, nunca commitada). O `Kitchen`
do pizzeria EMITE `TicketFinished` (`PublicEvent`) sobre o canal `Kitchen ->
Sales` (queue/rabbitmq) que existe no MESMO service `PizzeriaMonolith` que
também roda a Policy local `CreateTicketOnOrderPaid` de Kitchen — exatamente
`producerChannel != nil && needsDispatcher`, a guarda F5/G3
(`codegen/codegen.go:1143`). **Mas** não foi possível provar isso rodando o
`pizzeria` real ponta a ponta, porque QUATRO outros bloqueios independentes e
mais cedo no pipeline impedem a geração de sequer chegar a
`generateCmdMainFile`: (1) `access { ... requires caller.hasRole(...) }`
sozinho (sem `&&`/`||`/`==`) não é suportado por `lowerAccessCondition`
(`codegen/decl_aggregate.go`); (2) `emitApply` (mesmo arquivo, ~274) nunca
anexa um `BuiltinLowerer` (`.WithBuiltins`), então `now()`/builtins dentro de
`Apply` falham; (3) `kitchen/domain.ds` declara `items List<TicketItem>` mas
chama `.add(...)` nele — só suportado para `AppendList<T>` (provável typo do
próprio fixture, não gap de codegen); (4) o Read Side de Kitchen
(`Database MainDb { provider: "mongodb" }`, decorativo) não tem provider real
por trás, e o seam in-memory de Query não cobre "list <Aggregate>" direto
sem correlação a um campo `AppendList` conhecido. Registrado como **ISSUE-12**
(`.claude/issues.md`), com os cinco pontos (F5/G3 + os quatro acima)
detalhados, os arquivos/linhas exatos e o texto exato dos erros reproduzidos.
Por REQ-52.7, o escopo de L1.2 **não foi ampliado** para tentar corrigir
nenhum deles — a task fecha no seu próprio escopo (call site + esta
confirmação). **L1.3 fica BLOQUEADA**: a task original ("prova com o
pizzeria + limpeza do CI") não pode prosseguir como planejada até ISSUE-12
fechar (ou uma decisão explícita de trocar a fixture-âncora de L1.3) —
provavelmente precisa de um recorte novo, maior que REQ-52 sozinho.
Verificação: `go build ./...`, `go vet ./...`, `gofmt -l` limpos sobre os
arquivos tocados; `go test ./codegen/... ./driver/...` verde.

Re-escopo (decisão do usuário: tacklear ISSUE-12 agora, em vez de pular para
L2/L3): `.claude/specs/correcoes-issues-6-7-8/tasks.md` ganhou **L1.3a-L1.3f**
entre L1.2 e a prova final, uma task por defeito de ISSUE-12, em ordem
crescente de risco — L1.3a (typo do fixture, `items List<TicketItem>` →
`AppendList<TicketItem>`), L1.3b (`lowerAccessCondition` para
`caller.hasRole(...)` isolado), L1.3c (`emitApply` sem `BuiltinLowerer`),
L1.3d (Read Side de Kitchen sem provider real — decisão entre estender o
seam ou ajustar o fixture), L1.3e (a guarda F5/G3 em si — o bloqueio
arquitetural central, exige desenho antes de implementar), e L1.3f (a prova
final com `pizzeria` + limpeza do CI, antiga L1.3, só depois dos cinco
fecharem). Tabela de rastreabilidade e mapa de dependências do `tasks.md`
atualizados de acordo. **Próxima task: L1.3a.**

## Issues em aberto

Ver `.claude/issues.md`. ISSUE-1 (read-side/I5.1) **RESOLVIDA** (commit
`3a22df3`): `codegen/decl_collections.go` centraliza a declaração de
`Collection[T]` var disputado entre `EmitQueries`/`EmitPolicies` num único
`collections.go` por módulo. ISSUE-9/10/11 têm **spec de correção criada**
(`.claude/specs/correcoes-issues-9-10-11/`, Marco K) e ISSUE-6/7/8 também
(`.claude/specs/correcoes-issues-6-7-8/`, Marco L) — todas ainda abertas até a
execução dos respectivos Marcos fechá-las. ISSUE-2/3/4/5 seguem abertas sem spec
dedicada (itens maiores / front-end / spec-da-linguagem).
