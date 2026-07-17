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
| infra-providers (REQ-41..48) | `.claude/specs/infra-providers/` | in-progress | J0.3 |

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

## Issues em aberto

Ver `.claude/issues.md`. ISSUE-1 (read-side/I5.1) **RESOLVIDA** (commit
`3a22df3`): `codegen/decl_collections.go` centraliza a declaração de
`Collection[T]` var disputado entre `EmitQueries`/`EmitPolicies` num único
`collections.go` por módulo — nenhuma issue em aberto no momento.
