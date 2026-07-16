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
| read-side (REQ-33..40) | `.claude/specs/read-side/` | in-progress | I8.1 |

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

Pendente, na ordem do plano:

- [ ] **I8.1** — Revisão contra a DoD (`.claude/specs/read-side/requirements.md` §5); atualizar
      documentação de fechamento.

## Issues em aberto

Ver `.claude/issues.md`. ISSUE-1 (read-side/I5.1): colisão potencial de
`<tipo>Collection` var entre `EmitQueries` e `EmitPolicies` quando o mesmo
tipo aparece num join de Query e num list/count de Policy do mesmo módulo —
nenhum exemplo real exercita a combinação ainda.
