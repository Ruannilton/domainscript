# `Cache`/`RateLimit` `backend:` exige string literal, contra a forma da §13
- SPEC: [codegen](../specs/codegen/requirements.md)
- TASK: descoberto ao auditar `docs/examples/` contra a v7
  ([review-v7.md §A-8](../steerings/review-v7.md))
- DESCRIPTION: A [§13](../steerings/domainscript-spec-v7/13-module-infra.md)
  (`mod.ds`) escreve o valor de `backend:` como **identificador nu**, não
  como string: `Cache { backend: layered }`, `RateLimit { backend: redis }`
  — coerente com o resto do bloco, que só aspa valores opacos
  (`provider: "Postgres"`, `backoff: "exponential"`, `exporter: "otlp"`) e
  deixa nus os valores enumerados (`storage: same`, `algorithm:
  token_bucket`, `onBackendFailure: open`, `strategy: row_level`). A
  [§16](../steerings/domainscript-spec-v7/16-cache.md) reforça, listando os
  backends como `memory`, `distributed`, `layered`.
  O gerador aceita **apenas** a forma com aspas e falha na forma do spec:

  ```
  $ dsc gen <mod com "Cache { backend: memory }"> -o out
  dsc: codegen: módulo M: queries.go: codegen: Query Q:
       mod.ds Cache.backend: backend: esperava um literal string, veio *ast.Ident

  $ dsc gen <mod com "RateLimit { backend: redis }"> -o out
  dsc: codegen: cmd/m: cmd/m/main.go: rota POST "/w" -> U: UseCase U:
       mod.ds RateLimit.backend: backend: esperava um literal string, veio *ast.Ident
  ```

  Reproduzido com o `dsc` do HEAD em dois módulos mínimos (um com Query com
  bloco `cache`, outro com rota sob `rateLimit`); os demais campos enumerados
  do mesmo bloco (`algorithm`, `onBackendFailure`, `storage`, `strategy`)
  aceitam identificador nu normalmente — o defeito é específico de `backend`.
  Onde mora: os leitores de config de [decl_query_cache.go](../../../codegen/decl_query_cache.go)
  (`cacheBackendKind` e vizinhança, ~l.295-352) e [ratelimit.go](../../../codegen/ratelimit.go)
  (~l.264), que exigem `*ast.Literal` de kind STRING.

  **Impacto nos exemplos.** `testdata/projects/wallet/mod.ds` usa a forma com
  aspas (`backend: "redis"`) — gera, mas diverge do spec.
  `testdata/projects/pizzeria/sales/mod.ds` usa a forma do spec
  (`backend: memory`) — está correto e é **latentemente quebrado**: hoje o
  erro fica mascarado porque a geração do pizzeria já falha antes, na Query
  `GetBoardTickets`. Ou seja: os dois exemplos não podem estar simultaneamente
  conformes e funcionais enquanto isto não fechar, e por isso nenhum dos dois
  foi "corrigido" na auditoria dos exemplos.

  **Não é lacuna do spec** — o spec é claro e internamente consistente aqui.
  É defeito de código: a correção é aceitar o identificador nu (e, se quiser
  tolerância, aceitar as duas formas **só** se o spec passar a descrever as
  duas — hoje ele descreve uma).
- SOLVED: FALSE
