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
| infra-providers (REQ-41..48) | `.claude/specs/infra-providers/` | in-progress | J1.2 |

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
Próxima: **J1.3** — `(R1)` wiring lê conexão por `env(...)`, não por
`db.DSN` (`emitXADatabaseWiring` hoje faz `strconv.Quote(db.DSN)`, que é
`""` para `connection: env(...)`; precisa lowerizar via
`decl_io.go:envCallGo`).

## Issues em aberto

Ver `.claude/issues.md`. ISSUE-1 (read-side/I5.1) **RESOLVIDA** (commit
`3a22df3`): `codegen/decl_collections.go` centraliza a declaração de
`Collection[T]` var disputado entre `EmitQueries`/`EmitPolicies` num único
`collections.go` por módulo — nenhuma issue em aberto no momento.
