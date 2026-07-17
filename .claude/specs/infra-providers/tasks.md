# Tasks — Providers Reais de Infraestrutura (Postgres, RabbitMQ, Redis, S3, Outbox durável)

> Documento 3 de 3. Consome `design.md` (que consome `requirements.md`,
> REQ-41..48/NFR-21..24, Marco **J**). Cada task cita o REQ que satisfaz
> `(REQ-n)` e a seção do design `(§design x)`, é verticalmente fatiada e
> independentemente verificável. **Uma PR por task de nível 2** (ex. J1.1);
> subtasks (J1.1.a) são passos internos da mesma PR, não PRs separadas.

## Como ler este plano

- `[ ]` pendente, `[x]` concluída. Ordem respeita dependências (mapa no fim).
- **Granularidade:** cada task de nível 2 é uma unidade de PR pequena e
  fechada; as subtasks `.a/.b/.c` são os passos ordenados dentro dela (o que
  escrever primeiro, o que testar), pensados para serem commit-áveis em
  sequência se preferir, mas entregues numa PR só.
- **DoD de toda subtask:** golden + smoke-compile + unit sem infra (NFR-17/24).
  Integração (`//go:build integration`) entra junto, **pulada** por default.
- **NFR-21 em toda task:** o mesmo programa sem o provider ⇒ nada em go.mod,
  nada copiado, bytes idênticos ao de hoje — asserção explícita em cada slice.
- **Riscos R1–R7** (§design 7) estão distribuídos como subtasks marcadas
  `(R#)` — são pré-condições auditadas no código, não descobertas em runtime.

## Marco J — Providers Reais de Infraestrutura

### Fase J0 — Registro de provider por categoria (peça transversal, REQ-46)

- [x] **J0.1** Tipo e mapas do registro.
  - a. `codegen/provider_registry.go` (novo): tipo `providerDep {module,
    version, minGo, adapterDir, ctor}` + mapas vazios `channelProviders`/
    `cacheProviders`/`rateLimitProviders`/`fileProviders`. (REQ-46.1, §design
    2.1).
  - b. `activeProviderDeps(prog) []providerDep`: varre o programa por categoria
    e coleta as deps ativas. (REQ-46.1).
  - c. **(R5)** Dedup por `module` (um `require`) **e** por `adapterDir` (uma
    cópia de fontes), mesmo provider aparecendo em 2 categorias (redis em
    Cache+RateLimit). Ordenação estável (NFR-23).
  - d. Teste: registro vazio ⇒ `activeProviderDeps` vazio; dois mapas com o
    MESMO módulo/dir ⇒ uma entrada só.
- [x] **J0.2** `EmitGoMod` consome o registro.
  - a. `EmitGoMod` (`project.go`) itera `activeProviderDeps` além de
    `activeSQLProviders`; `require` ordenado por módulo. (REQ-46.2, §design 2.2).
  - b. `go` diretiva sobe para `maxGoVersion` de todos os ativos (reusa helper
    de I7.0).
  - c. Teste: fixture com provider fake ⇒ require esperado; sem ⇒ inalterado
    (NFR-21).
- [x] **J0.3** Gate de cópia de fontes.
  - a. `generateCategoryRuntimeFiles(dir)` genérico (espelha
    `generateSQLRuntimeFiles`), chamado por `Generate` só quando a categoria
    tem provider ativo. (REQ-46.3, §design 2.3).
  - b. Teste: sem provider ⇒ nenhum dir copiado; projeto byte-idêntico ao atual
    (NFR-21/23).
- [ ] **J0.4** **(R10)** Vendoring + go.sum offline.
  - a. O repo domainscript passa a depender dos drivers oficiais (pgx,
    amqp091-go, go-redis, aws-sdk-go-v2) e os vendoriza/cacheia — a árvore-fonte
    de onde o gerador copia. (REQ-46.5, §design 2.2).
  - b. `EmitGoMod` emite `go.sum` (hashes fixados) e o gerador materializa
    `vendor/` com a árvore dos drivers ativos + deps transitivas, só quando há
    provider real. (REQ-46.5).
  - c. Teste: projeto com provider builda `go build -mod=vendor` **offline**;
    sem provider ⇒ sem `vendor/`/`go.sum` extra, byte-idêntico (NFR-21/24).

### Fase J1 — Postgres como Database provider real (REQ-41)

- [x] **J1.1** `PostgresDialect` (só strings SQL, sem driver ainda).
  - a. `dialect_postgres.go.txt` (ou struct em `dialect.go.txt`): `Placeholder`
    `$N`, `LimitOffset` `LIMIT/OFFSET`. (REQ-41.1, §design 3.1).
  - b. DDL `events`/collection com `text`/`jsonb`; `WhereEq` via
    `payload->>'campo' = $N`. (REQ-41.4).
  - c. **(R7)** Parâmetro do `WhereEq` bound como texto; teste de paridade
    garante que o conjunto de tipos seguros (`unsafeWhereEqPrimitives`) é
    idêntico entre SqliteDialect e PostgresDialect (nenhum tipo que passe num e
    falhe no outro). (§design 3.1, R7).
  - d. Teste unit do dialeto (strings esperadas), reusando o padrão de
    `sql_dialect_test.go` (o dialeto `$1` sobre sqlite já existe lá).
- [x] **J1.2** Driver + registro + go.mod.
  - a. `open_postgres.go.txt`: `Open(dsn) (*sql.DB, error)` via
    `sql.Open("pgx", dsn)`. (REQ-41.3).
  - b. **(R1-parte)** `PingContext` com `context.WithTimeout` curto
    (fail-closed, nunca `Background()`). (REQ-47.2, §design 3.1).
  - c. Entrada `sqlProviders["postgres"]` (pgx module/versão/minGo/
    `"PostgresDialect"`). (REQ-41.2).
  - d. Teste: `activeSQLProviders` reconhece postgres; `EmitGoMod` exige pgx;
    sem postgres ⇒ sem pgx (NFR-21).
- [x] **J1.3** **(R1)** Wiring lê conexão por `env(...)`, não por `db.DSN`.
  - a. O wiring de provider real (`emitXADatabaseWiring` e/ou o caminho
    single-DB) passa a lowerizar `connection: env("X")` de `db.Decl.Entries`
    via `decl_io.go:envCallGo` — **nunca** `strconv.Quote(db.DSN)` (que é `""`
    para env). (§design 3.1, R1).
  - b. Teste: fixture postgres com `connection: env("PG_URL")` ⇒ wiring emite
    `os.Getenv("PG_URL")`, não string vazia.
- [x] **J1.4** Golden + smoke + integração.
  - a. Fixture single-module `provider: "postgres"` gera; projeto builda + `go
    vet`a sobre bytes em disco (NFR-17).
  - b. Integração `//go:build integration` guardada por `PG_URL`:
    persistir+reler um Aggregate no postgres == in-memory (NFR-22/24).

### Fase J2 — Outbox durável (REQ-42) — depende de J1

- [ ] **J2.1** **(R4)** Estender o seam `runtime.Tx` para o enqueue atômico.
  - a. Método novo no seam `runtime.Tx` (ex. `EnqueueOutbox(events []Event)
    error`); `memoryTx` recebe no-op/stub documentado (sem outbox durável
    in-memory). (§design 3.2, R4).
  - b. `sqlrt.Tx.EnqueueOutbox` implementa sobre o `*sql.Tx` em mãos (mesma tx
    do `Append`). (REQ-42.1).
  - c. Teste: um `Run(fn)` que faz `Append`+`EnqueueOutbox` grava as duas
    tabelas **ou nenhuma** (rollback simulado) — atomicidade.
- [ ] **J2.2** DDL + SQL da tabela `outbox` no `Dialect`.
  - a. `InsertOutbox`/`ScanUndelivered`/`MarkDelivered`/`PurgeDelivered` como
    métodos do `Dialect`, em Sqlite e PostgresDialect. (REQ-42.4).
  - b. **(FIFO)** `ScanUndelivered` sempre com `ORDER BY id`; postgres com `FOR
    UPDATE SKIP LOCKED`, sqlite com `LIMIT` simples. (§design 3.2).
  - c. Teste unit dos dois dialetos (strings esperadas).
- [ ] **J2.3** `DurableOutbox` + relay.
  - a. `DurableOutbox` em `rtsrc/outbox.go.txt` (ao lado de `memoryOutbox`,
    MESMA interface `Outbox`): relay `Start(ctx)` com backoff, entrega, marca;
    falha ⇒ re-tenta (at-least-once). (REQ-42.2/42.3, §design 3.2).
  - b. Teste unit do relay com `*sql.DB` sqlite `:memory:` (sem infra), incl.
    crash simulado (não marca ⇒ re-entrega).
- [ ] **J2.4** **(R9)** Relay alimenta o canal cross-service.
  - a. `NewDurableOutbox(..., publisher)` recebe o transporte de saída; o relay
    roteia por `event_type`: destino cross-service (a topologia tem canal de
    saída, `producerChannelFor`) ⇒ `ChannelTransport.Publish`; senão ⇒
    in-process. Só marca `delivered_at` após o `Publish` suceder. (REQ-42.6,
    §design 3.2a).
  - b. **Proibir** publish direto no commit para destino cross-service (o
    publisher da uow deixa de receber o canal quando o outbox durável está
    ativo). Teste: crash entre commit e publish ⇒ evento re-entregue (não
    perdido).
- [ ] **J2.5** Cleanup + seleção/wiring.
  - a. `StartOutboxCleanup(ctx)` (análogo a `StartIdempotencyCleanup`) purga
    entregues além da janela de retenção via `PurgeDelivered`. (REQ-42.7).
  - b. `NewDurableOutbox(db, dialect, dispatcher, publisher)` quando o módulo
    tem Database real, senão `NewOutbox(dispatcher)` de hoje; `Start(ctx)` do
    relay e do cleanup no `main.go`. (REQ-42.5).
  - c. Golden + smoke; sem Database real ⇒ wiring byte-idêntico (NFR-21/23).

### Fase J3 — RabbitMQ como transporte de canal cross-process (REQ-43)

- [ ] **J3.1** Adapter `amqprt` + envelope + registro de contracts.
  - a. `codegen/amqprt/` (novo, espelha `sqlrt/`): `rabbitmq.go.txt` com
    `rabbitmqChannel` implementando `ChannelTransport` (`Subscribe`/`Publish`).
    `embed.go`/`Sources()`. (REQ-43.1).
  - b. Envelope JSON `{eventType, payload}`; consumidor reconstrói via
    `EventRegistry`. (§design 3.3).
  - c. **(R8)** O wiring do consumidor registra `contracts.EventRegistry()` (as
    factories dos PublicEvent) além do registry do módulo — senão desserializa
    errado cross-service. (REQ-43.5, §design 3.3, R8).
  - d. Entrada `channelProviders["rabbitmq"]` (amqp091-go).
  - e. Teste unit do envelope (serialize→deserialize round-trip via contracts),
    sem infra.
- [ ] **J3.2** **(R6)** Ordenação por partição + poison pill.
  - a. `orderBy` declarado ⇒ **exchange consistent-hash** por `hash(chave)` →
    N filas de partição, um consumidor por partição (ordem por chave, paralelo
    entre chaves = `workers.concurrency`); sem `orderBy` ⇒ work-queue prefetch =
    concurrency. Paridade com o in-memory (ordem só por chave). (REQ-43.3, R6).
  - b. `ack` no sucesso; falha ⇒ `nack requeue=false` → DLX+retry-queue(TTL) (o
    broker incrementa `x-death`); esgotado `circuitBreaker.threshold` ⇒ DLQ
    final. (REQ-43.4, §design 3.3).
  - c. Teste unit da montagem de exchange consistent-hash/DLX/binding.
- [ ] **J3.3** Reconexão.
  - a. Supervisão de `Connection.NotifyClose`/`Channel.NotifyClose` com loop de
    reconnect+backoff, re-declarando exchanges/filas/consumidores; publish na
    janela de reconexão retorna erro (o relay do outbox re-tenta). (REQ-43.6,
    §design 3.3).
  - b. Teste unit: fechar o canal fake ⇒ o supervisor tenta reabrir.
- [ ] **J3.4** Seleção + wiring + integração.
  - a. `channel.go`: `channelProvider(ch)` lê `provider` de `ch.Decl.Entries`
    (R2); produtor/consumidor trocam `NewQueueChannel` por
    `NewRabbitMQChannel(url, cfg, keyFunc)` quando `"rabbitmq"`; sem provider ⇒
    in-memory; grpc/http/stream ⇒ erro de sempre. (REQ-43.2/43.7, R2).
  - b. **(R1)** URL de `connection: env(...)` via `envCallGo`.
  - c. Golden + smoke sobre fixture multi-service; integração `//go:build
    integration` guardada por `AMQP_URL` (publicar→consumir cross-process ==
    in-process, NFR-22/24).

### Fase J4 — Redis como backend de Cache e RateLimit (REQ-44)

- [ ] **J4.1** `redisQueryCache` (Cache §15).
  - a. `codegen/redisrt/cache.go.txt`: implementa `QueryCache` (GET/SETEX, TTL,
    negativo, fail-open). Entrada `cacheProviders["redis"]` (go-redis/v9).
    (REQ-44.1/44.5, §design 3.4).
  - b. Invalidação por geração-no-prefixo: chaves de dados COM TTL, chave de
    geração `<ns>:gen` **sem** expiração. (§design 3.4).
  - c. Teste unit: montagem de chave/serialização + fail-open (erro ⇒
    hit=false) por client fake injetado.
- [ ] **J4.2** `redisLimiter` (RateLimit §16) + fallback local.
  - a. `redisrt/ratelimit.go.txt`: implementa `Limiter` via script Lua atômico
    (token_bucket/sliding/fixed). Entrada `rateLimitProviders["redis"]`.
    `CheckRateLimits` reusado inalterado. (REQ-44.2/44.3).
  - b. **(ponto 6)** Compõe um `Limiter` in-memory como **fallback local**: erro
    de Redis ⇒ roteia para o local (proteção por-réplica ativa, não fail-open
    total); Redis volta ⇒ retoma a contagem global. (REQ-44.5, §design 3.4).
  - c. Teste unit do script/chave + fallback (Redis fake com erro ⇒ usa local,
    não libera tudo); integração `REDIS_URL`.
- [ ] **J4.3** **(R2/R3)** Seleção + wiring.
  - a. `decl_query_cache.go`/`ratelimit.go`: trocam construtor in-memory pelo
    redis quando `Cache{backend:redis}`/`RateLimit{backend:redis}` (lidos dos
    ConfigBlocks de módulo); URL de `env(...)`. (REQ-44.4, R1).
  - b. **(R3)** Teste de fixture confirma que `url:/connection: env(...)` nos
    blocos `Cache{}`/`RateLimit{}` chega em `Decl.Entries` (se não chegar:
    único ponto que tocaria o front-end ⇒ registrar desvio).
  - c. Golden + smoke; sem `backend: redis` ⇒ byte-idêntico (NFR-21/23).

### Fase J5 — S3 como FileStorage provider real (REQ-45)

- [ ] **J5.1** `s3FileStorage`.
  - a. `codegen/s3rt/filestorage.go.txt`: implementa `FileStorage`
    (`Store`→PutObject key UUID + metadata, `Load`→GetObject,
    `SignedURL`→presign GET real, `Delete`→DeleteObject). Entrada
    `fileProviders["s3"]` (aws-sdk-go-v2 config/s3/presign). (REQ-45.1/45.2,
    §design 3.5).
  - b. `FileStream` fica como desvio documentado (lowering não emite hoje,
    REQ-45.3).
  - c. Teste unit da montagem de key/metadata/layout, sem infra.
- [ ] **J5.2** **(R2)** Seleção + wiring.
  - a. `decl_filestorage.go`: `fileStorageProvider(fs)` lê `provider` de
    `fs.Decl.Entries` (R2); troca `NewMemoryFileStorage` por
    `NewS3FileStorage(bucket, region)` quando `"s3"`; bucket/região de
    `env(...)`, credenciais pela cadeia AWS padrão. (REQ-45.4, R1/R2).
  - b. Golden + smoke; sem s3 ⇒ byte-idêntico. Integração `//go:build
    integration` guardada por `S3_BUCKET` (put+get+presign+delete == in-memory,
    NFR-22/24).

### Fase J6 — Fixture-âncora, fail-closed e determinismo (REQ-47, REQ-48)

- [ ] **J6.1** **(R7)** Fixture-âncora multi-service.
  - a. Postgres + canal rabbitmq + cache/ratelimit redis + filestorage s3 +
    Policy AtLeastOnce sobre o Outbox durável, tudo com `connection: env(...)`.
    (§1.4 requirements).
  - b. **(R7)** Estruturar cada módulo para NÃO combinar UseCase + Policy no
    mesmo módulo (evita ISSUE-7, alheia a este ciclo — cada módulo é de UseCase
    OU de Policy, como shop).
  - c. Gera, builda **offline** (`go build -mod=vendor`) + `go vet`a com os
    cinco adapters e o `vendor/` presentes (golden + smoke, REQ-48.1, R10).
- [ ] **J6.2** **(R1)** Wiring multi-recurso fail-closed com `run() error`.
  - a. O `main.go`-âncora gera o corpo num `func run() error` (cada passo
    `return err`, `defer Close()` no unwind, `main()` faz o `log.Fatal` único) —
    não vaza recurso já aberto se o próximo falhar. (REQ-47.2/47.3, §design 3.6).
  - b. Teste: smoke confirma a forma `run() error` + `defer Close()` por
    recurso.
- [ ] **J6.3** Determinismo + NFR-21 consolidado.
  - a. Regenerar a fixture-âncora 2x ⇒ bytes idênticos (go.mod, go.sum, imports,
    fontes de adapter, `vendor/`, main.go) — por analogia a
    `TestSharedCollectionTypeDeterministic`. (NFR-23).
  - b. Teste "categoria não declarada ⇒ nada em go.mod/go.sum/vendor, nada
    copiado" (NFR-21).

### Fase J7 — Fechamento do ciclo (REQ-48.4, DoD)

- [ ] **J7.1** Revisão contra a DoD + atualização de docs.
  - a. Conferir DoD (requirements §5): cinco providers reais e opt-in, go.mod
    exato, wallet/shop sem regressão (NFR-19), três camadas de teste
    (integração pulada sem infra, NFR-24).
  - b. Atualizar a doc dos exemplos que marcavam esses providers como
    decorativos (`docs/examples/pizzeria` README — postgres/rabbitmq deixam de
    ser "só rótulo").
  - c. Atualizar `.claude/specs/codegen/gaps.md` (G-4 parcialmente fechado — 5
    categorias), `.claude/issues.md` (ISSUE-3 idem, restante de G-4 aberto),
    `.claude/state.md`, `README.md`/`CLAUDE.md`.
  - **Marco J — Providers Reais — fechado (recorte de 5).**

## Mapa de Dependências

```
J0 (registro transversal) ──┬─▶ J1 (postgres) ──▶ J2 (outbox durável, usa Dialect+Tx SQL)
                            ├─▶ J3 (rabbitmq)
                            ├─▶ J4 (redis cache/ratelimit)
                            └─▶ J5 (s3)
J1..J5 ──▶ J6 (fixture-âncora + fail-closed + determinismo) ──▶ J7 (fechamento)
```

J1–J5 são independentes entre si depois de J0 (qualquer ordem / PRs paralelas),
com duas dependências reais: **J2 depende de J1** (o Outbox durável reusa o
Dialect SQL, a tx do UnitOfWork SQL e a extensão do seam `Tx`, J2.1); e a
durabilidade cross-service do outbox (**J2.4**, relay→canal) só se prova
end-to-end com o transporte real de **J3** presente — por isso a fixture-âncora
(J6) combina outbox durável + rabbitmq. O roteamento do relay para
`ChannelTransport` (J2.4) é genérico (funciona sobre o `QueueChannel` in-memory
também), então J2 pode fechar antes de J3; só o cenário durável cross-service
(J6) exige os dois. J6 exige todas; J7 fecha. Dentro de cada Jn.x, as subtasks
`.a/.b/.c` são ordenadas (implementar antes de testar; seam antes de wiring).

## Rastreabilidade REQ → Tasks

| REQ | Tema | Tasks |
|---|---|---|
| REQ-41 | Postgres (Database) | J1.1, J1.2, J1.3, J1.4 |
| REQ-42 | Outbox durável (incl. relay→canal, cleanup) | J2.1, J2.2, J2.3, J2.4, J2.5 |
| REQ-43 | RabbitMQ (canal, ordenação, reconexão, contracts) | J3.1, J3.2, J3.3, J3.4 |
| REQ-44 | Redis (Cache+RateLimit, fallback local) | J4.1, J4.2, J4.3 |
| REQ-45 | S3 (FileStorage) | J5.1, J5.2 |
| REQ-46 | Registro + go.mod opt-in + vendoring | J0.1, J0.2, J0.3, J0.4 |
| REQ-47 | Config env + fail-closed + wiring | J1.2, J1.3, J2.5, J3.4, J4.3, J5.2, J6.1, J6.2 |
| REQ-48 | Teste em 3 camadas (smoke offline) | cada Jn.\* + J6.3, J7.1 |
| NFR-21..24 | transversais | todas |
| R1..R10 | riscos auditados (§design 7) | R1:J1.3/J3.4 · R2:J3.4/J5.2 · R3:J4.3 · R4:J2.1 · R5:J0.1 · R6:J3.2 · R7:J1.1 · R8:J3.1 · R9:J2.4 · R10:J0.4/J6 |
