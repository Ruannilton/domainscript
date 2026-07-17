# Tasks — Providers Reais de Infraestrutura (Postgres, RabbitMQ, Redis, S3, Outbox durável)

> Documento 3 de 3. Consome `design.md` (que consome `requirements.md`,
> REQ-41..48/NFR-21..24, Marco **J**). Cada task cita o REQ que satisfaz
> `(REQ-n)` e a seção do design `(§design x)`, é verticalmente fatiada
> (adapter → registro → wiring → teste de UMA categoria de ponta a ponta) e
> independentemente verificável — **uma PR por task concluída** (CLAUDE.md).

## Como ler este plano

- `[ ]` pendente, `[x]` concluída. Ordem respeita dependências (ver o mapa no
  fim). Cada categoria é um vertical slice fechado: dá para parar depois de
  qualquer Jn com o repositório verde e um provider real a mais.
- **Teste por task:** golden + smoke-compile + unit de dialeto/serialização
  (sem infra) são a Definition of Done de cada task (NFR-17/24). O teste de
  integração (build tag `integration`) entra junto mas **pulado** por default.
- **NFR-21 em toda task:** o exemplo sem o provider tem que continuar
  byte-idêntico — todo slice inclui a asserção "categoria não declarada ⇒ nada
  em go.mod, nada copiado".

## Marco J — Providers Reais de Infraestrutura

### Fase J0 — Registro de provider por categoria (peça transversal, REQ-46)

- [ ] **J0.1** `codegen/provider_registry.go` (novo): tipo `providerDep`
  `{module, version, minGo, adapterDir, ctor}` e os mapas vazios-por-enquanto
  `channelProviders`/`cacheProviders`/`rateLimitProviders`/`fileProviders`;
  `activeProviderDeps(prog) []providerDep` que varre o programa e devolve as
  deps ativas dedup+ordenadas (NFR-23) — espelhando `activeSQLProviders`.
  (REQ-46.1, §design 2.1). Teste: registro vazio ⇒ `activeProviderDeps` vazio
  ⇒ go.mod sem deps (NFR-21).
- [ ] **J0.2** `EmitGoMod` (`codegen/project.go`) passa a exigir cada
  `providerDep` ativa de todas as categorias, além de `activeSQLProviders`;
  `go` diretiva sobe para `maxGoVersion` de todos os providers ativos (reusa o
  helper de I7.0). (REQ-46.2, §design 2.2). Teste: fixture com um provider fake
  registrado ⇒ require esperado, ordenado; sem ⇒ inalterado.
- [ ] **J0.3** Gate de cópia de fontes: `generateCategoryRuntimeFiles(dir)`
  genérico (espelha `generateSQLRuntimeFiles`) chamado por `Generate` só quando
  a categoria tem provider ativo. (REQ-46.3, §design 2.3). Teste: sem provider
  ativo ⇒ nenhum dir copiado; projeto byte-idêntico ao atual (NFR-21/23).

### Fase J1 — Postgres como Database provider real (REQ-41)

- [ ] **J1.1** `PostgresDialect` em `codegen/sqlrt` (`dialect_postgres.go.txt`
  ou struct em `dialect.go.txt`): `Placeholder` `$N`, `LimitOffset` `LIMIT/
  OFFSET`, DDL `events`/collection com `text`/`jsonb`, `WhereEq` via
  `payload->>'campo' = $N`. (REQ-41.1/41.4, §design 3.1). Teste: unit do dialeto
  (strings SQL esperadas) reusando o padrão de `sql_dialect_test.go`.
- [ ] **J1.2** `open_postgres.go.txt`: `Open(dsn) (*sql.DB, error)` via
  `sql.Open("pgx", dsn)` + `PingContext` (fail-closed). Entrada
  `sqlProviders["postgres"]` (pgx module/versão/minGo/`"PostgresDialect"`).
  (REQ-41.2/41.3/41.5, §design 3.1). Teste: `activeSQLProviders` reconhece
  postgres; `EmitGoMod` exige pgx; sem postgres ⇒ sem pgx (NFR-21).
- [ ] **J1.3** Golden + smoke: fixture single-module com `provider: "postgres"`
  gera, o projeto com `sqlruntime` presente builda + `go vet`a (REQ-41,
  NFR-17). Teste de integração `//go:build integration` guardado por `PG_URL`:
  persistir+reler um Aggregate no postgres == in-memory (NFR-22/24).

### Fase J2 — Outbox durável (REQ-42) — depende de J1 (usa o Dialect SQL)

- [ ] **J2.1** DDL + SQL da tabela `outbox` como métodos do `Dialect`
  (`InsertOutbox`/`ScanUndelivered`/`MarkDelivered`), implementados em
  Sqlite e PostgresDialect. (REQ-42.4, §design 3.2). Teste: unit dos dois
  dialetos.
- [ ] **J2.2** `DurableOutbox` em `rtsrc/outbox.go.txt` (ao lado de
  `memoryOutbox`, MESMA interface `Outbox`): enqueue dentro da tx do UnitOfWork
  SQL (atômico store+outbox), relay `Start(ctx)` com backoff que varre não
  entregues, chama handler, marca entregue; falha ⇒ re-tenta (at-least-once).
  (REQ-42.1/42.2/42.3, §design 3.2). Teste: unit do relay com um `*sql.DB`
  sqlite em memória (`:memory:` — sem infra externa), incl. crash simulado
  (não marca ⇒ re-entrega).
- [ ] **J2.3** Seleção do Outbox no wiring: `NewDurableOutbox(db, dialect,
  dispatcher)` quando o módulo tem Database real, senão `NewOutbox(dispatcher)`
  de hoje (REQ-42.5). `Start(ctx)` do relay no `main.go` gerado. Golden +
  smoke; sem Database real ⇒ wiring byte-idêntico ao de hoje (NFR-21/23).

### Fase J3 — RabbitMQ como transporte de canal cross-process (REQ-43)

- [ ] **J3.1** `codegen/amqprt/` (novo, espelha `sqlrt/`): `rabbitmq.go.txt`
  com `rabbitmqChannel` implementando `ChannelTransport` (`Subscribe`/`Publish`,
  exchange topic, routing/consumer key de `EventType`+orderBy, prefetch =
  concurrency, ack/nack). `embed.go`/`Sources()`. Entrada
  `channelProviders["rabbitmq"]` (amqp091-go). (REQ-43.1/43.3/43.4, §design 3.3).
  Teste: unit do envelope (serialize→deserialize via EventRegistry), sem infra.
- [ ] **J3.2** `channel.go`: `channelProviderKind(ch)` consulta
  `channelProviders`; produtor (`generateCmdMainFile`) e consumidor
  (`emitPolicyWireFunc`) trocam `NewQueueChannel` por `NewRabbitMQChannel(url,
  cfg, keyFunc)` quando `provider: "rabbitmq"`; `via: queue` sem provider ⇒
  in-memory; `grpc/http/stream` ⇒ erro de sempre. (REQ-43.2/43.5, §design 3.3).
  Golden + smoke sobre fixture multi-service com o canal rabbitmq.
- [ ] **J3.3** Integração `//go:build integration` guardada por `AMQP_URL`:
  publicar num service e consumir noutro processo == entrega in-process
  (paridade cross-process, NFR-22/24).

### Fase J4 — Redis como backend de Cache e RateLimit (REQ-44)

- [ ] **J4.1** `codegen/redisrt/` (novo): `cache.go.txt` com `redisQueryCache`
  implementando `QueryCache` (GET/SETEX, TTL, negativo, fail-open, invalidação
  por geração-no-prefixo). Entrada `cacheProviders["redis"]` (go-redis/v9).
  (REQ-44.1/44.5, §design 3.4). Teste: unit com um fake/miniredis? — não;
  unit da montagem de chave/serialização + fail-open (erro ⇒ hit=false) por
  injeção de um client fake. Integração `REDIS_URL`.
- [ ] **J4.2** `redisrt/ratelimit.go.txt`: `redisLimiter` implementando
  `Limiter` com script Lua atômico (token_bucket/sliding/fixed). Entrada
  `rateLimitProviders["redis"]`. `CheckRateLimits` reusado inalterado.
  (REQ-44.2/44.3, §design 3.4). Teste: unit do script/chave; integração
  `REDIS_URL` (cota cross-"réplica" == in-memory numa réplica).
- [ ] **J4.3** Seleção no wiring: `decl_query_cache.go`/`ratelimit.go` trocam
  o construtor in-memory pelo redis quando `Cache{backend:redis}`/`RateLimit
  {backend:redis}`; URL de `env(...)`. (REQ-44.4). Golden + smoke; sem
  `backend: redis` ⇒ byte-idêntico (NFR-21/23).

### Fase J5 — S3 como FileStorage provider real (REQ-45)

- [ ] **J5.1** `codegen/s3rt/` (novo): `filestorage.go.txt` com `s3FileStorage`
  implementando `FileStorage` (`Store`→PutObject key UUID + metadata,
  `Load`→GetObject, `SignedURL`→presign GET real, `Delete`→DeleteObject).
  Entrada `fileProviders["s3"]` (aws-sdk-go-v2 config/s3/presign). (REQ-45.1/
  45.2, §design 3.5). Teste: unit da montagem de key/metadata/layout, sem
  infra.
- [ ] **J5.2** Seleção no wiring: `decl_filestorage.go` troca
  `NewMemoryFileStorage` por `NewS3FileStorage(bucket, region)` quando
  `FileStorage{provider:"s3"}`; bucket/região de `env(...)`, credenciais pela
  cadeia AWS padrão. (REQ-45.4, §design 3.5/3.6). `FileStream` fica como desvio
  documentado (lowering não emite, REQ-45.3). Golden + smoke; sem s3 ⇒
  byte-idêntico. Integração `//go:build integration` guardada por `S3_BUCKET`
  (put+get+presign+delete == in-memory, NFR-22/24).

### Fase J6 — Fixture-âncora, config/fail-closed e determinismo (REQ-47, REQ-48)

- [ ] **J6.1** Fixture-âncora multi-service (§1.4 requirements): postgres +
  canal rabbitmq + cache/ratelimit redis + filestorage s3 + Policy AtLeastOnce
  sobre o Outbox durável, tudo com `connection/env(...)`. Gera, builda, `go
  vet`a com os cinco adapters presentes (golden + smoke, REQ-48.1). Wiring
  abre cada conexão com fail-closed (`log.Fatal`) e `defer Close()` (REQ-47).
- [ ] **J6.2** Teste de determinismo (NFR-23): regenerar a fixture-âncora duas
  vezes ⇒ bytes idênticos (go.mod, imports, fontes de adapter, main.go) — por
  analogia a `TestSharedCollectionTypeDeterministic`. Teste "categoria não
  declarada ⇒ nada" consolidado (NFR-21).

### Fase J7 — Fechamento do ciclo (REQ-48.4, DoD)

- [ ] **J7.1** Revisão contra a DoD (requirements §5): os cinco providers reais
  e opt-in, go.mod exato, wallet/shop sem regressão (NFR-19), três camadas de
  teste no lugar (integração pulada sem infra, NFR-24). Atualizar a doc dos
  exemplos que marcavam esses providers como decorativos (ex.
  `docs/examples/pizzeria` README — postgres/rabbitmq deixam de ser "só
  rótulo"). Atualizar `.claude/specs/codegen/gaps.md` (G-4 parcialmente fechado
  — cinco categorias do recorte), `.claude/issues.md` (ISSUE-3 idem, com o
  restante de G-4 ainda aberto), `.claude/state.md`, `README.md`/`CLAUDE.md`.
  **Marco J — Providers Reais — fechado (recorte de 5).**

## Mapa de Dependências

```
J0 (registro transversal) ──┬─▶ J1 (postgres) ──▶ J2 (outbox durável, usa Dialect+uow SQL)
                            ├─▶ J3 (rabbitmq)
                            ├─▶ J4 (redis cache/ratelimit)
                            └─▶ J5 (s3)
J1..J5 ──▶ J6 (fixture-âncora + determinismo) ──▶ J7 (fechamento)
```

J1–J5 são independentes entre si depois de J0 (podem ser feitas/PR-adas em
qualquer ordem), exceto J2 que depende de J1 (o Outbox durável reusa o Dialect
SQL e a tx do UnitOfWork SQL). J6 exige todas; J7 fecha.

## Rastreabilidade REQ → Tasks

| REQ | Tema | Tasks |
|---|---|---|
| REQ-41 | Postgres (Database) | J1.1, J1.2, J1.3 |
| REQ-42 | Outbox durável | J2.1, J2.2, J2.3 |
| REQ-43 | RabbitMQ (canal) | J3.1, J3.2, J3.3 |
| REQ-44 | Redis (Cache+RateLimit) | J4.1, J4.2, J4.3 |
| REQ-45 | S3 (FileStorage) | J5.1, J5.2 |
| REQ-46 | Registro + go.mod opt-in | J0.1, J0.2, J0.3 |
| REQ-47 | Config env + fail-closed + wiring | J1.2, J2.3, J3.2, J4.3, J5.2, J6.1 |
| REQ-48 | Teste em 3 camadas | cada Jn.\* + J6, J7 |
| NFR-21..24 | transversais | todas |
