# Design — Providers Reais de Infraestrutura (Postgres, RabbitMQ, Redis, S3, Outbox durável)

> Documento 2 de 3. Consome `requirements.md` (REQ-41..48, NFR-21..24, Marco J)
> e traduz cada requisito numa decisão concreta sobre onde o código mora e como
> o seam existente é preenchido. **Nada aqui redesenha um seam** — todos já
> existem (read-side/codegen); o trabalho é implementar o lado real atrás deles
> e generalizar o registro de provider (REQ-46).

## 1. Visão Arquitetural

### 1.1. Onde o trabalho mora

```
codegen/
  sql_wiring.go        sqlProviders += "postgres"  (REQ-41.2)   ← 1 entrada
  project.go           EmitGoMod += providers ativos de TODAS as categorias (REQ-46.2)
  channel.go           reconhece provider "rabbitmq" no via:queue (REQ-43)
  decl_query_cache.go  seleciona backend redis quando Cache{backend:redis} (REQ-44)
  ratelimit.go         seleciona backend redis quando RateLimit{backend:redis} (REQ-44)
  decl_filestorage.go  seleciona backend s3 quando FileStorage{provider:"s3"} (REQ-45)
  provider_registry.go NOVO: registros por categoria + go.mod opt-in (REQ-46.1)

  sqlrt/               postgres: open_postgres.go.txt + dialect_postgres.go.txt
  amqprt/    NOVO      RabbitMQ ChannelTransport (REQ-43)
  redisrt/   NOVO      Redis QueryCache + Limiter (REQ-44)
  s3rt/      NOVO      S3 FileStorage (REQ-45)
  rtsrc/
    outbox.go.txt      + DurableOutbox (REQ-42) atrás da MESMA interface Outbox
    dialect: DDL/SQL da tabela outbox no Dialect (REQ-42.4)
```

O runtime núcleo (`rtsrc/`) **não ganha import externo**: os seams
`QueryCache`/`Limiter`/`FileStorage`/`Outbox`/`ChannelTransport` continuam
definidos lá (stdlib), e cada adapter real vive num pacote opt-in próprio
(`amqprt`/`redisrt`/`s3rt`/`sqlrt`) que **implementa** a interface do núcleo —
a dependência aponta do adapter para o núcleo, nunca o contrário (NFR-21).

### 1.2. Princípio central: o seam é o contrato, o provider é a implementação

Cada categoria já provou esse princípio uma vez (SQL, read-side). Este ciclo o
repete cinco vezes:

| Categoria | Interface (núcleo, stdlib) | In-memory (existe) | Real (este ciclo) |
|---|---|---|---|
| Database | `Dialect` + `EventStore`/`Collection` | sqlite | **postgres** |
| Canal | `ChannelTransport` | `queueChannel` | **rabbitmqChannel** |
| Cache | `QueryCache` | `memoryQueryCache` | **redisQueryCache** |
| RateLimit | `Limiter` | 3 algos in-mem | **redisLimiter** |
| FileStorage | `FileStorage` | `memoryFileStorage` | **s3FileStorage** |
| Outbox | `Outbox` | `memoryOutbox` | **durableOutbox** (SQL) |

O código gerado que consome cada seam (lowering, `decl_*.go`,
`cmd/*/main.go`) **não muda** — muda só qual construtor é chamado no wiring,
decidido pelo registro de provider (REQ-46).

## 2. Registro de provider por categoria (REQ-46) — a peça transversal

### 2.1. Forma

`sqlProviders` (`sql_wiring.go`) já é o modelo: `map[string]sqlProvider` com
`{driverModule, driverVersion, minGoVersion, dialectCtor}`. Generalizamos para
uma forma comum por categoria em `codegen/provider_registry.go` (novo):

```go
// providerDep é o que uma categoria precisa saber de um provider real para
// (a) exigi-lo em go.mod e (b) emitir seu wiring. Uma entrada = um provider.
type providerDep struct {
    module     string // caminho do módulo Go p/ go.mod (ex. "github.com/jackc/pgx/v5")
    version    string
    minGo      string // "" quando não eleva o mínimo
    adapterDir string // dir de fontes .txt opt-in a copiar (ex. "amqpruntime"); "" p/ SQL (já em sqlruntime)
    ctor       string // construtor do adapter no pacote gerado (ex. "NewRabbitMQChannel")
}

var channelProviders   = map[string]providerDep{ "rabbitmq": {...} }
var cacheProviders     = map[string]providerDep{ "redis": {...} }
var rateLimitProviders = map[string]providerDep{ "redis": {...} }
var fileProviders      = map[string]providerDep{ "s3": {...} }
// Database continua em sqlProviders (já tem o campo dialectCtor específico).
```

`activeProviderDeps(prog)` percorre o programa e devolve, deduplicado e
ordenado (NFR-23), toda `providerDep` ativa de todas as categorias — a fonte
única que `EmitGoMod` (go.mod) e `generateXxxRuntimeFiles` (cópia de `.txt`)
consomem. É a mesma mecânica de `activeSQLProviders`, elevada a "todas as
categorias".

### 2.2. go.mod opt-in (REQ-46.2)

`EmitGoMod` passa a iterar `activeProviderDeps` além de `activeSQLProviders`
(SQL fica separado só porque carrega `dialectCtor`; poderiam fundir-se, mas o
custo/risco não compensa neste ciclo — decisão registrada em §4). O `require`
resultante é ordenado por caminho de módulo; `minGo` eleva a diretiva `go` para
o máximo exigido por qualquer provider ativo (o `maxGoVersion` helper de I7.0 já
faz isso para SQL — reusar).

### 2.3. Cópia de fontes de adapter (REQ-46.3)

Cada novo dir (`amqprt`/`redisrt`/`s3rt`) espelha `sqlrt`: fontes `.txt`
embarcadas + `embed.go` com `Sources() (map[string][]byte, error)`. `Generate`
(codegen.go) chama `generateXxxRuntimeFiles()` **só** quando aquela categoria
tem provider real ativo — mesmo gate de `programNeedsSQLAdapter`. Fontes
copiadas verbatim para `<dir>/*.go` no projeto gerado.

## 3. Decisões de Design por provider

### 3.1. Postgres (REQ-41) — mais barato dos cinco (o seam é o mais maduro)

- **Driver:** `github.com/jackc/pgx/v5/stdlib` (registra `database/sql` driver
  `"pgx"`), OU `github.com/lib/pq`. Decisão: **pgx/v5 stdlib** — mantido, jsonb
  nativo, mais ativo. Fica atrás de `database/sql` (o EventStore/UnitOfWork já
  falam `*sql.DB`), então **nenhuma** mudança nos `*.go.txt` de store/uow além
  do dialeto.
- **`open_postgres.go.txt`:** `Open(dsn) (*sql.DB, error)` que faz
  `sql.Open("pgx", dsn)` + `PingContext` (fail-closed, REQ-47.2). O ping usa um
  `context.WithTimeout` curto (default 10s, decisão de design) — **nunca**
  `context.Background()`: com o banco inalcançável (rede/firewall), o startup
  falha oportunamente em vez de travar indefinidamente.
- **`PostgresDialect`** (`dialect_postgres.go.txt` ou uma struct em
  `dialect.go.txt`): `Placeholder(n) = "$"+n`, `LimitOffset = "LIMIT %d OFFSET
  %d"`, DDL `events`/collection com tipos postgres (`text`, `jsonb`),
  `WhereEq` via `payload->>'campo' = $n` (operador JSON do postgres) no lugar do
  `json_extract` do sqlite. Toda a variação fica AQUI (REQ-40.1/41.1).
- **Registro:** `sqlProviders["postgres"] = {pgx module, versão, minGo,
  "PostgresDialect"}` (REQ-41.2). `activeSQLProviders` e o wiring reconhecem
  sozinhos.
- **Type mapping (REQ-41.4):** id `text`, payload `jsonb` (ou `text` — decisão:
  `jsonb`, para o `->>` de WhereEq ser index-friendly). O gerador só emite
  id-string + JSON-string; a diferença é DDL + operador de extração, ambos no
  dialeto.

### 3.2. Outbox durável (REQ-42) — atômico com a tx de negócio

- **Tabela `outbox`** (DDL no `Dialect`): `(id, event_type, payload,
  created_at, delivered_at NULL, attempts)`. Enfileirar acontece **dentro da
  mesma tx** que grava os eventos de negócio no EventStore SQL (o UnitOfWork
  SQL já abre/commita uma tx — o INSERT no outbox entra nela: atomicidade
  store+outbox, REQ-42.1).
- **`DurableOutbox`** (`outbox.go.txt`, ao lado de `memoryOutbox`, MESMA
  interface `Outbox`): um relay que, num loop com backoff, seleciona um lote de
  não entregues, chama o(s) handler(s) inscrito(s) por `event_type`, e num
  sucesso marca `delivered_at`; numa falha incrementa `attempts` e re-tenta
  (at-least-once, REQ-42.2). O relay roda como um Worker de serviço (mesma casa
  de `StartIdempotencyCleanup`, G2) — um `Start(ctx)` no wiring de `main.go`.
- **Lote exclusivo entre réplicas (múltiplas instâncias do mesmo service):** a
  seleção do lote **não** pode ser um `SELECT ... WHERE delivered_at IS NULL`
  nu — duas réplicas rodando o relay pegariam as MESMAS linhas e entregariam
  em duplicidade sem necessidade. O `ScanUndelivered` do `Dialect` (abaixo)
  seleciona com trava por linha: no Postgres, `SELECT ... FOR UPDATE SKIP
  LOCKED LIMIT batch` (cada réplica leva um lote exclusivo, sem bloquear as
  outras); no sqlite (single-writer, sem `SKIP LOCKED`) a trava de escrita do
  próprio banco já serializa, então o `SELECT ... LIMIT batch` simples basta.
  Como cada linha ainda pode ser entregue mais de uma vez em cenários de crash
  (at-least-once por design), o handler idempotente segue sendo a garantia
  final — `SKIP LOCKED` só elimina a duplicação *concorrente rotineira*, não a
  necessidade de idempotência.
- **Seleção:** `NewOutbox` vira `NewOutbox(dispatcher)` (memory, hoje) OU
  `NewDurableOutbox(db, dialect, dispatcher)` quando o módulo tem Database real
  (REQ-42.5). O `Subscribe` do código gerado é idêntico nos dois (REQ-42.3).
- **SQL só no dialeto (REQ-42.4):** enqueue/scan/mark viram métodos do
  `Dialect` (ex. `InsertOutbox`, `ScanUndelivered` — com `FOR UPDATE SKIP
  LOCKED` no postgres, `LIMIT` simples no sqlite —, `MarkDelivered`) —
  funciona sobre sqlite e postgres sem string fora do dialeto.

### 3.3. RabbitMQ (REQ-43) — fecha a limitação single-process do Marco F

- **Driver:** `github.com/rabbitmq/amqp091-go` (o fork oficial mantido).
- **`amqprt/rabbitmq.go.txt`:** `rabbitmqChannel` implementa `ChannelTransport`
  (`Subscribe`/`Publish`). `Publish` publica numa exchange (topic) com routing
  key derivada de `EventType()` (+ chave de `orderBy` como parte da routing/
  consumer key para ordenação best-effort por partição, REQ-43.3). `Subscribe`
  declara queue durável + binding, consome com prefetch = `workers.concurrency`,
  e para cada mensagem chama o handler; `ack` no sucesso, `nack` na falha
  (REQ-43.4).
- **Poison pill (falha permanente):** um `nack`+requeue incondicional vira um
  loop imediato quando o erro é permanente (payload que não desserializa, bug
  de lógica no handler) — sobrecarrega broker e consumidor sem nunca progredir.
  A queue durável é declarada com uma **Dead Letter Exchange** e o consumidor
  requeue só até um limite de tentativas (o `circuitBreaker.threshold`
  declarado, ou um default); esgotado o limite, a mensagem é `nack`ada **sem
  requeue** e cai na DLQ para inspeção manual, em vez de girar para sempre. A
  contagem de tentativas viaja no header `x-death`/num header próprio do
  envelope.
- **Envelope:** JSON `{eventType, payload}` — o mesmo shape que o Dispatcher
  já move em memória, serializado (o `Event` gerado já é JSON-serializável,
  convenção de E4.2). Deserialização no consumidor reconstrói via o
  `EventRegistry` do módulo (já existe, decl_event.go).
- **Wiring:** produtor (`generateCmdMainFile`) e consumidor
  (`emitPolicyWireFunc`) trocam `NewQueueChannel(cfg, keyFunc)` por
  `NewRabbitMQChannel(url, cfg, keyFunc)` quando o canal declara `provider:
  "rabbitmq"` (REQ-43.2/5). `channel.go` decide via um `channelProviderKind(ch)`
  novo, análogo a `channelViaKind`, consultando `channelProviders`. `via:
  queue` sem provider reconhecido segue in-memory; `grpc/http/stream` seguem
  `unsupportedChannelKindError`.

### 3.4. Redis — Cache e RateLimit (REQ-44)

- **Driver:** `github.com/redis/go-redis/v9`.
- **`redisrt/cache.go.txt`:** `redisQueryCache` implementa `QueryCache`. Chave
  = `<queryNamespace>:<argHash>`; `Get`/`Set` via `GET`/`SETEX` com o TTL;
  resultado negativo com `negativeCacheTtl`; **fail-open** — erro de Redis ⇒
  `Get` devolve `hit=false` (REQ-44.1/5, o shape já previsto). `InvalidateAll`
  = `DEL` por prefixo de namespace (SCAN+DEL, ou um contador de geração no key
  prefix para evitar SCAN — decisão: **geração no prefixo**, O(1) e sem SCAN).
  **Custo da geração-no-prefixo:** as chaves das gerações antigas não são
  apagadas no `InvalidateAll` (só deixam de ser lidas) — ficam no Redis até
  expirar. Para não virar bloat de memória, **toda** chave de cache é escrita
  com TTL (o `ttl` declarado no bloco `cache {}`; nunca sem expiração), e a
  geração (um inteiro sob `<queryNamespace>:gen`) também expira/reinicia com
  folga — o pior caso é uma geração de chaves órfãs vivendo no máximo um `ttl`,
  não acumulando indefinidamente. Se um dia um `cache {}` sem `ttl` for
  permitido, este adapter impõe um TTL-teto default em vez de gravar sem
  expiração.
  Coalescing continua local por processo (o seam já o faz); cross-réplica não é
  exigido para stampede (§15 não pede).
- **`redisrt/ratelimit.go.txt`:** `redisLimiter` implementa `Limiter` com
  contagem atômica: `token_bucket`/`sliding`/`fixed` via script Lua (atômico
  cross-réplica, REQ-44.2). `CheckRateLimits` (AND multi-dimensão,
  `ratelimit.go`) é reusado como está — ele só compõe `Limiter.Allow`.
- **Fail semantics (REQ-44.5):** cache fail-**open** sempre; rate limit —
  decisão: **fail-open** por default (indisponibilidade de Redis não derruba
  tráfego), documentado; um modo fail-closed fica fora do recorte.
- **Seleção:** `decl_query_cache.go`/`ratelimit.go` consultam
  `cacheProviders`/`rateLimitProviders` para trocar o construtor
  `NewMemoryQueryCache`/in-mem-limiter por `NewRedisQueryCache(url)`/
  `NewRedisLimiter(url)` quando `backend: redis`. URL de `env(...)`.

### 3.5. S3 — FileStorage (REQ-45)

- **Driver:** `github.com/aws/aws-sdk-go-v2` (`config`, `service/s3`,
  `service/s3/presign`).
- **`s3rt/filestorage.go.txt`:** `s3FileStorage` implementa `FileStorage`.
  `Store(File) → FileRef`: `PutObject` com key **única** (UUID v4 gerado por
  `store`, guardado no `FileRef.ID` — cada `store` é um objeto novo, espelhando
  o `memoryFileStorage`), metadados como object metadata. `Load(FileRef) →
  File`: `GetObject`. `SignedURL(ref, ttl)`: presigned GET real (REQ-45.2).
  `Delete(ref)`: `DeleteObject`. **Não** é uma key determinística por conteúdo:
  desduplicação de arquivos idênticos (key = hash SHA-256 do conteúdo) seria
  outra semântica — mudaria o comportamento observável vs. o in-memory
  (NFR-22) e não é pedida pelo spec; fica fora do recorte. A palavra
  "determinística" era imprecisa e foi corrigida para "única".
- **Config:** bucket/região de `env(...)`; credenciais via cadeia padrão do
  AWS SDK (`config.LoadDefaultConfig`) — nunca hardcoded (REQ-47.1).
- **FileStream (REQ-45.3):** só se `builtins.go` já emitir ops sobre
  `FileStream` hoje (não emite — G1a cobre só File/FileRef); então fica
  registrado como desvio, o adapter cobre File/FileRef igual ao in-memory.

### 3.6. Config por ambiente + fail-closed (REQ-47)

Padrão único: cada construtor de adapter recebe a URL/DSN como string, resolvida
por `os.Getenv` no `main.go` gerado (o lowering de `env("X")` já produz
`os.Getenv("X")`). O startup abre cada conexão e **falha fechado** se qualquer
uma falhar.

**Padrão `run() error` em vez de `log.Fatal` direto (múltiplos recursos).** O
sqlite de hoje (`emitXADatabaseWiring`) usa `log.Fatal` porque abre **um** só
recurso — não há nada aberto antes que precise ser fechado. Com os cinco
providers, o startup abre vários recursos em sequência (DB, canal AMQP, cliente
Redis, cliente S3); um `log.Fatal` no meio chama `os.Exit(1)` e **pula todos os
`defer Close()`** dos recursos já abertos, vazando conexões. Por isso o
`main.go`-âncora gera o corpo num `func run() error` — cada passo faz `if err
!= nil { return err }` (os `defer Close()` acima dele rodam no unwind), e o
`main()` fica `if err := run(); err != nil { log.Fatal(err) }` (o `Fatal`
único, depois que todo `defer` já executou). Fail-closed preservado
(REQ-47.2), cleanup ordenado garantido (REQ-47.3). O caminho single-sqlite
existente pode continuar como está (um recurso, sem `defer` anterior a
proteger) — o padrão `run() error` entra no wiring novo multi-recurso.

## 4. Alternativas Rejeitadas

- **Fundir todos os registros num só `map[categoria]map[provider]dep`.**
  Rejeitado: SQL carrega `dialectCtor` (não os outros), e as categorias têm
  gates de ativação diferentes (Database por `mod.Databases`, canal por
  `prog.Channels`, etc.). Mapas separados + um `activeProviderDeps` que os
  varre é mais claro e menos acoplado. Reavaliar se surgir a 3ª categoria com
  a mesma forma exata.
- **Exigir infra viva no CI (testcontainers no `go test ./...` default).**
  Rejeitado por NFR-24: quebraria o build de quem não tem Docker/rede. Infra
  real fica atrás de build tag `integration` + env-guard.
- **Outbox durável como tabela separada com seu próprio commit.** Rejeitado:
  perderia a atomicidade store+outbox (REQ-42.1). O INSERT no outbox tem que
  entrar na tx do UnitOfWork SQL — por isso o SQL de enqueue vira método do
  `Dialect` chamado de dentro do commit da uow, não um relay separado.
- **RabbitMQ com ordering total.** Rejeitado: o spec (§11) e o `QueueChannel`
  in-memory já são ordenados **por chave** (partição), não total; RabbitMQ
  espelha isso (routing/consumer key), mantendo NFR-22.

## 5. Fora de Escopo Registrado (para o próximo gaps)

- Outros bancos (MySQL, SQL Server, Mongo), gRPC/HTTP/stream como `via` de
  canal, Dynamo para idempotency `external`, backend `layered` de cache,
  GCS/Azure para FileStorage — cada um é uma entrada de registro + adapter
  futuros, baratos pelo modelo de REQ-46, mas fora do recorte deste ciclo.
- Migrations versionadas (só o DDL mínimo do gerador roda), pooling avançado,
  observabilidade dos adapters (liga com G-6, telemetria).
- `FileStream` real (depende de o lowering de `builtins.go` emitir ops de
  stream — hoje não emite).

## 6. Estratégia de Testes (NFR-17, NFR-22, NFR-24)

Três camadas, por provider:

1. **Golden + smoke (sem infra, NFR-17):** a fonte do adapter gerada bate com
   a referência; a fixture-âncora (§1.4 do requirements) gera, e o projeto com
   os adapters presentes builda + `go vet`a sobre os bytes em disco. Prova que
   o wiring e as fontes compilam — não que falam com infra.
2. **Unit de dialeto/serialização (sem infra, NFR-22 parcial):** PostgresDialect
   emite `$1`/`jsonb`/`->>'campo'` corretos (reusa o padrão do segundo dialeto
   de teste, REQ-40.3 — o dialeto `$1` sobre sqlite já existe em
   `sql_dialect_test.go`); envelope AMQP round-trip (serialize→deserialize via
   EventRegistry); chave/geração Redis; key/layout S3; DDL/SQL da tabela outbox
   em ambos os dialetos.
3. **Integração opt-in (com infra, NFR-22 total / NFR-24):** arquivos
   `//go:build integration`, guardados por env (`PG_URL`/`AMQP_URL`/`REDIS_URL`/
   `S3_BUCKET` presentes ⇒ roda; ausentes ⇒ `t.Skip`). Rodam o comportamento
   end-to-end (persistir+reler no postgres, publicar+consumir no rabbitmq,
   cachear+invalidar no redis, put+get+presign no s3, crash-recover no outbox)
   e comparam com o resultado in-memory (paridade). **Nunca** no `go test ./...`
   default.

Determinismo (NFR-23): regenerar a fixture-âncora duas vezes ⇒ bytes idênticos
(go.mod, imports, fontes de adapter, main.go) — um teste dedicado por analogia
a `TestSharedCollectionTypeDeterministic`.
