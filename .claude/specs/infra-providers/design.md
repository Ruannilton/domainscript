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

### 2.2. go.mod opt-in + vendoring (REQ-46.2, REQ-46.5)

`EmitGoMod` passa a iterar `activeProviderDeps` além de `activeSQLProviders`
(SQL fica separado só porque carrega `dialectCtor`; poderiam fundir-se, mas o
custo/risco não compensa neste ciclo — decisão registrada em §4). O `require`
resultante é ordenado por caminho de módulo; `minGo` eleva a diretiva `go` para
o máximo exigido por qualquer provider ativo (o `maxGoVersion` helper de I7.0 já
faz isso para SQL — reusar).

**Drivers oficiais e vendorizados (decisão do usuário).** O princípio de deps
mínimas (NFR-12/21) vale para o *núcleo* — mas onde falar com infra real de
forma correta e segura exige a lib estabelecida, **reimplementá-la seria pior**
(bugs de protocolo, brechas de segurança): nesses casos usa-se a biblioteca, e
**a oficial tem prioridade** (pgx, amqp091-go — o fork oficial da VMware/
RabbitMQ, go-redis, aws-sdk-go-v2). Para que o projeto gerado **builde offline
e reprodutível** (e o smoke-compile não dependa de rede, NFR-24), o gerador
emite o projeto **vendorizado**:

- `go.sum` é emitido junto do `go.mod` (hashes fixados dos providers ativos) —
  determinismo (NFR-23) e build verificável.
- um diretório `vendor/` com a árvore dos drivers ativos (e suas deps
  transitivas) é materializado no projeto gerado; `go build -mod=vendor` roda
  sem rede.
- **De onde vêm os bytes vendorizados:** o **próprio repositório domainscript**
  passa a depender dos drivers oficiais (em `go.mod` do compilador) e os
  vendoriza/cacheia — o gerador copia dessa árvore conhecida para o `vendor/`
  do projeto de saída, do mesmo jeito que já copia as fontes `.txt` dos
  adapters. Assim o smoke-compile de um projeto com provider builda contra bytes
  já presentes no repo, sem download (fecha o gap do ponto 2 da análise).
- Núcleo intacto: um projeto **sem** provider real não ganha `vendor/` nem
  entradas em `go.sum` — byte-idêntico ao de hoje (NFR-21/23).

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
- **Segurança de tipo do WhereEq (R7) — mesma régua do sqlite.** `payload->>'x'`
  devolve **texto**, então a igualdade só é correta para os tipos que o
  `WhereEq` já restringe no sqlite (`unsafeWhereEqPrimitives`, I7.1: só
  integer/string/boolean; decimal/datetime/duration/size/bytes ficam de fora
  porque a comparação textual mente). Essa restrição é do lowering (é o mesmo
  `Query[T].WhereEq` que desce para os dois bancos), então vale para postgres
  automaticamente — mas o `PostgresDialect` SHALL bindar o parâmetro **como
  texto** (`= $N` com o valor já stringificado, igual ao sqlite) e um teste de
  paridade garante que o conjunto de tipos seguros é idêntico nos dois dialetos
  (nunca um tipo que passa no sqlite e falha no postgres, ou vice-versa).

### 3.2. Outbox durável (REQ-42) — atômico com a tx de negócio

- **Tabela `outbox`** (DDL no `Dialect`): `(id, event_type, payload,
  created_at, delivered_at NULL, attempts)`. Enfileirar acontece **dentro da
  mesma tx** que grava os eventos de negócio no EventStore SQL (o UnitOfWork
  SQL já abre/commita uma tx — o INSERT no outbox entra nela: atomicidade
  store+outbox, REQ-42.1). **Isso exige estender o seam `runtime.Tx`** (hoje só
  `Append`/`Load`) com um `EnqueueOutbox` que o `sqlrt.Tx` implementa sobre seu
  `*sql.Tx` — ver R4 em §7; é a primeira subtask de J2, antes do relay.
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
  seleciona com trava por linha, **sempre com `ORDER BY id` explícito** para
  preservar a ordem FIFO de processamento (sem ele o banco devolve linhas em
  ordem arbitrária, atrasando eventos antigos): no Postgres, `SELECT ... ORDER
  BY id FOR UPDATE SKIP LOCKED LIMIT batch` (cada réplica leva um lote
  exclusivo, sem bloquear as outras); no sqlite (single-writer, sem `SKIP
  LOCKED`) a trava de escrita do próprio banco já serializa, então o `SELECT
  ... ORDER BY id LIMIT batch` simples basta. Como cada linha ainda pode ser
  entregue mais de uma vez em cenários de crash
  (at-least-once por design), o handler idempotente segue sendo a garantia
  final — `SKIP LOCKED` só elimina a duplicação *concorrente rotineira*, não a
  necessidade de idempotência.
- **Seleção:** `NewOutbox` vira `NewOutbox(dispatcher)` (memory, hoje) OU
  `NewDurableOutbox(db, dialect, dispatcher, publisher)` quando o módulo tem
  Database real (REQ-42.5). O `Subscribe` do código gerado é idêntico nos dois
  (REQ-42.3).
- **O outbox alimenta o canal cross-service (REQ-42.6, §3.3a) — não são dois
  mecanismos paralelos.** Um `PublicEvent` que cruza serviços NÃO pode ser
  publicado direto no broker pelo publisher do UnitOfWork (isso aconteceria
  *fora* da tx SQL — crash pós-commit/pré-publish perderia o evento, o buraco
  que o outbox existe pra fechar). Em vez disso: o commit grava o evento na
  tabela `outbox` (atômico), e é o **relay** que, ao entregar, publica no
  transporte certo — Dispatcher/Outbox in-process para uma Policy do mesmo
  service, **ou o `ChannelTransport` (RabbitMQ) para um destino cross-service**.
  O `publisher` injetado no `NewDurableOutbox` é justamente esse transporte de
  saída (o mesmo que o produtor montaria em `main.go`), agora acionado *de
  dentro do relay* em vez de direto no commit. Assim a entrega cross-service
  herda a durabilidade at-least-once do outbox: só marca `delivered_at` depois
  que o `Publish` no broker teve sucesso.
- **Limpeza/retenção (boas práticas):** linhas com `delivered_at` não crescem
  para sempre — um `StartOutboxCleanup(ctx)` (análogo ao
  `StartIdempotencyCleanup`, G2) apaga periodicamente as entregues mais velhas
  que uma janela de retenção (default configurável), pelo `Dialect`
  (`PurgeDelivered`). Sem isso a tabela `outbox` vira um log infinito.
- **SQL só no dialeto (REQ-42.4):** enqueue/scan/mark/purge viram métodos do
  `Dialect` (`InsertOutbox`, `ScanUndelivered` — `ORDER BY id` sempre, com
  `FOR UPDATE SKIP LOCKED` no postgres e `LIMIT` simples no sqlite —,
  `MarkDelivered`, `PurgeDelivered`) — funciona sobre sqlite e postgres sem
  string fora do dialeto.

### 3.2a. Composição outbox ↔ canal (REQ-42.6): uma entrega durável, dois destinos

O ponto de junção dos dois seams. Hoje (Marco F) o publisher da UnitOfWork
recebe o `Dispatcher`/`QueueChannel` e publica no commit. Com o outbox durável
ativo, a ordem inverte:

```
commit da tx  ─▶  INSERT outbox (mesma tx, atômico)          [REQ-42.1]
                       │
relay Start(ctx) ──────┴─▶ para cada linha não entregue (FIFO, SKIP LOCKED):
                              destino in-process?  Dispatcher.Publish
                              destino cross-service? ChannelTransport.Publish (RabbitMQ)
                              sucesso ⇒ MarkDelivered ; falha ⇒ attempts++ / re-tenta
```

Roteamento de destino: o relay sabe, por `event_type`, se aquele PublicEvent
tem um canal de saída (a topologia já diz — `producerChannelFor`, channel.go);
se tem, publica no `ChannelTransport` daquele canal; senão, entrega in-process.
Quando o módulo **não** tem Database real, nada disso muda — segue o publisher
direto de hoje (memory), sem durabilidade (o mesmo trade-off já documentado do
Marco F). A durabilidade cross-service só existe quando há Database real *e*
canal com provider real — exatamente o cenário da fixture-âncora.

### 3.3. RabbitMQ (REQ-43) — fecha a limitação single-process do Marco F

- **Driver:** `github.com/rabbitmq/amqp091-go` (o fork oficial mantido).
- **`amqprt/rabbitmq.go.txt`:** `rabbitmqChannel` implementa `ChannelTransport`
  (`Subscribe`/`Publish`). `Publish` publica numa exchange com routing key
  derivada de `EventType()` + a chave de `orderBy`. `Subscribe` declara queue
  durável + binding, consome e para cada mensagem chama o handler; `ack` no
  sucesso, tratamento de falha abaixo.
- **Ordenação por chave × concorrência (R6) — resolvido, não hand-waved.**
  Numa fila única os dois objetivos são mutuamente exclusivos: um consumidor
  ativo dá ordem mas mata a concorrência; prefetch>1 dá concorrência mas
  embaralha a ordem por chave. A regra:
  - **`orderBy` declarado ⇒ particionamento por chave.** Uma **exchange
    consistent-hash** (`x-consistent-hash`) roteia por `hash(chave de orderBy)`
    para *N* filas de partição; cada partição tem **um** consumidor (prefetch 1
    naquela fila) — ordem preservada *dentro* da partição, e a concorrência
    (`workers.concurrency`) é o número de partições/consumidores, espalhando a
    carga entre chaves. É o mesmo contrato do `QueueChannel` in-memory
    (ordenado por chave, paralelo entre chaves), agora cross-process (NFR-22).
  - **Sem `orderBy` ⇒ work-queue simples** com prefetch = `workers.concurrency`
    (sem garantia de ordem — não havia nenhuma a preservar). REQ-5.16 já avisa
    quando um canal de fila não declara `orderBy`.
  A ordem entre partições nunca foi prometida por nenhum dos dois lados
  (best-effort global, ordem estrita só por chave — igual ao in-memory).
- **Reconexão (boas práticas).** O `amqp091-go` não reconecta sozinho: um blip
  de rede fecha a conexão/canal e o consumidor pararia em silêncio. O
  `rabbitmqChannel` supervisiona `Connection.NotifyClose`/`Channel.NotifyClose`
  e re-estabelece conexão, canal, exchanges/filas e consumidores num loop com
  backoff — publicações durante a janela de reconexão são bufferizadas/retornam
  erro para o chamador (que, no caminho do relay do outbox, simplesmente
  re-tenta a linha não entregue: durabilidade preservada).
- **Poison pill (falha permanente):** um `nack`+`requeue=true` incondicional
  vira um loop imediato quando o erro é permanente (payload que não
  desserializa, bug de lógica no handler) — sobrecarrega broker e consumidor
  sem nunca progredir. **E `requeue=true` nativo NÃO altera os headers da
  mensagem** (ela volta ao topo da fila intacta), então nem `x-death` nem um
  contador próprio seriam incrementados por esse caminho. Por isso o controle
  de tentativas usa um dos dois fluxos padrão do RabbitMQ: (a) `nack`
  **`requeue=false`** para uma **DLX** ligada a uma *retry queue* com TTL, que
  ao expirar reencaminha a mensagem de volta à fila principal — o broker
  incrementa `x-death` a cada passagem, e o consumidor lê a contagem dali; ou
  (b) o consumidor **republica manualmente** a mensagem com um header de
  contador incrementado antes de dar `ack` na original. Decisão: fluxo (a)
  (DLX+TTL, menos código no consumidor). Esgotado o limite de tentativas (o
  `circuitBreaker.threshold` declarado, ou um default), a mensagem vai para a
  **DLQ final** (`nack requeue=false` sem reencaminhamento) para inspeção
  manual, em vez de girar para sempre.
- **Envelope:** JSON `{eventType, payload}` — o mesmo shape que o Dispatcher
  já move em memória, serializado (o `Event` gerado já é JSON-serializável,
  convenção de E4.2). Deserialização no consumidor reconstrói via o
  `EventRegistry`.
- **Registro de contracts no consumidor (R8) — o ponto fácil de esquecer.** O
  serviço que consome roda num binário DIFERENTE do produtor; para
  desserializar o envelope de um `PublicEvent`, o `EventRegistry` do consumidor
  precisa conter a factory daquele tipo. PublicEvents vivem no pacote
  `contracts/` (E4.2). O wiring do consumidor (`emitPolicyWireFunc` /
  `cmd/<service>/main.go` do lado que recebe) SHALL registrar
  `contracts.EventRegistry()` (as factories dos PublicEvents que ele consome)
  além do `EventRegistry` do próprio módulo — senão o consumo cross-service
  falha em runtime com "tipo de evento desconhecido". Subtask explícita em J3.
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
  **Custo da geração-no-prefixo:** as chaves de *dados* das gerações antigas
  não são apagadas no `InvalidateAll` (só deixam de ser lidas) — ficam no Redis
  até expirar. Para não virar bloat de memória, **toda chave de dados** é
  escrita com TTL (o `ttl` declarado no bloco `cache {}`; nunca sem expiração —
  se um dia um `cache {}` sem `ttl` for permitido, este adapter impõe um
  TTL-teto default). **A chave de geração** (um inteiro sob
  `<queryNamespace>:gen`), ao contrário, **NÃO expira**: é uma única chave por
  namespace (cardinalidade mínima, zero risco de bloat), e se ela expirasse o
  contador reiniciaria — invalidando *acidentalmente* todo o cache ativo
  daquela Query (as chaves de dados vivas passariam a ter um prefixo de geração
  incompatível). Persistente por design, portanto.
  Coalescing continua local por processo (o seam já o faz); cross-réplica não é
  exigido para stampede (§15 não pede).
- **`redisrt/ratelimit.go.txt`:** `redisLimiter` implementa `Limiter` com
  contagem atômica: `token_bucket`/`sliding`/`fixed` via script Lua (atômico
  cross-réplica, REQ-44.2). `CheckRateLimits` (AND multi-dimensão,
  `ratelimit.go`) é reusado como está — ele só compõe `Limiter.Allow`.
- **Fail semantics (REQ-44.5):** cache fail-**open** sempre (§15). Rate limit —
  decisão: **fallback local**, não fail-open puro. Quando o Redis está
  indisponível, o `redisLimiter` degrada para o `Limiter` in-memory local
  (por-réplica) em vez de deixar passar tudo: a proteção continua ativa (agora
  por processo, não global) durante a queda, evitando a janela de abuso/
  sobrecarga que um fail-open total abriria. O `redisLimiter` compõe um
  `Limiter` in-memory como fallback e roteia para ele ao detectar erro de
  Redis; quando o Redis volta, retoma a contagem global. Melhor esforço, nunca
  desligado.
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
- Migrations versionadas (só o DDL mínimo do gerador roda; schema evolution
  fica para um ciclo dedicado), pooling avançado de conexão
  (`SetMaxOpenConns`/`MaxIdleConns`/`ConnMaxLifetime` — o adapter usa defaults
  sãos, tuning fino fora do recorte), observabilidade dos adapters (liga com
  G-6, telemetria — métricas de retry/reconexão/latência de driver).
- `FileStream` real (depende de o lowering de `builtins.go` emitir ops de
  stream — hoje não emite).
- **Oportunidade adjacente barata (registrada, não neste ciclo):** com Postgres
  presente **e** o seam `runtime.Tx` já estendido para o outbox (R4/J2.1),
  Idempotency `storage: same` **atômico com a tx de negócio** (spec §14) fica
  quase de graça — o mesmo `EnqueueOutbox` abre caminho para um
  `RecordIdempotency` na mesma tx, fechando parte de G-4 (idempotency) sem o
  Redis/Dynamo `external`. Fora do recorte de 5 providers, mas o próximo passo
  natural mais barato depois deste ciclo.

## 6. Estratégia de Testes (NFR-17, NFR-22, NFR-24)

Três camadas, por provider:

1. **Golden + smoke (sem infra, NFR-17):** a fonte do adapter gerada bate com
   a referência; a fixture-âncora (§1.4 do requirements) gera, e o projeto com
   os adapters presentes builda + `go vet`a sobre os bytes em disco. O build é
   **offline** — `go build -mod=vendor` contra o `vendor/` emitido (§2.2), com
   os drivers oficiais vindos da árvore vendorizada do próprio repo; nenhum
   download de módulo, nenhuma infra. Prova que o wiring e as fontes compilam
   contra os drivers reais — não que falam com infra.
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

## 7. Riscos e pré-condições de implementação (auditados no código antes de fatiar)

Levantados cruzando o design com o código real (`program/graph.go`,
`codegen/*.go`, `codegen/sqlrt/*`) — cada um vira uma subtask explícita em
`tasks.md` para não ser descoberto no meio da implementação:

- **R1 — `Database.DSN` é `""` quando a conexão é `env(...)`.**
  `program.Database.DSN` só carrega literais estáticos (doc do próprio campo);
  `emitXADatabaseWiring` (`sql_wiring.go`) faz `strconv.Quote(db.DSN)`. Para
  Postgres com `connection: env("PG_URL")` isso emitiria uma **string vazia**.
  O wiring de provider real tem que **lowerizar `env(...)` a partir do
  `Decl.Entries`** (não usar `db.DSN`). Já existe o helper de forma:
  `decl_io.go:envCallGo`/`envCallKey` produz `os.Getenv("X")` — reusar. Sem
  isso, todo provider com conexão por env sobe com string vazia (falha só em
  runtime). **Pré-condição transversal de J1/J3/J4/J5.**

- **R2 — `Channel` e `FileStorage` não têm `Provider`/`Connection` no modelo.**
  `program.Channel` só tem `From/To/Via/Decl`; `program.FileStorage` só tem
  `Name/Module/Decl`. O provider (`"rabbitmq"`/`"s3"`) e a `connection`/bucket
  vivem em `Decl.Entries` — acessíveis (é o que `channelOrderByField` já faz),
  mas **não** como campo tipado. Decisão: ler de `Decl.Entries` via helpers
  novos (`channelProvider(ch)`, `fileStorageProvider(fs)`), sem tocar o
  front-end (config é free-form, nunca validada contra enum — mesma postura de
  `Database.Provider`). **Nenhuma mudança de front-end** (mantém o escopo).

- **R3 — Config `env(...)` em Cache/RateLimit/FileStorage: bloco aceita a
  chave?** Os blocos de módulo são `*ast.ConfigBlock` free-form
  (`moduleRateLimitBlock` lê `b.Kind == "RateLimit"`; o `Cache{}` de módulo já
  é lido para o fallback de ttl em `decl_query.go`). Uma `url:`/`connection:
  env(...)` a mais nesses blocos é só mais uma entry — o parser a aceita (config
  é genérica). **Pré-condição:** confirmar num teste de fixture que a entry
  chega em `Decl.Entries` (deve chegar); se não chegar, é o ÚNICO ponto que
  tocaria o front-end e vira desvio registrado. Subtask de verificação em J4/J5.

- **R4 — Atomicidade store+outbox exige estender o seam `runtime.Tx`.** O
  `sqlrt.UnitOfWork.Run(ctx, fn)` (`sqlrt/uow.go.txt`) abre um `*sql.Tx` real e
  passa um `runtime.Tx` cujo contrato hoje é só `Append`/`Load` — **não** há
  como um outbox INSERT entrar na MESMA tx pela interface atual. Fechar REQ-42.1
  exige um método novo no seam `runtime.Tx` (ex. `EnqueueOutbox(events
  []Event)`) implementado pelo `sqlrt.Tx` (que tem o `*sql.Tx` em mãos) e
  chamado pelo caminho de commit da uow, dentro do `fn`. O `memoryTx` recebe um
  no-op/stub (sem outbox durável in-memory). **Subtask dedicada em J2, antes do
  relay** — é a peça que garante "atômico com a tx de negócio", não um detalhe.

- **R5 — Registro compartilhado entre categorias (redis em Cache e RateLimit).**
  `cacheProviders["redis"]` e `rateLimitProviders["redis"]` apontam para o
  MESMO módulo (`go-redis/v9`) e o MESMO `adapterDir` (`redisruntime`).
  `activeProviderDeps` tem que **deduplicar por módulo** (um require só) **e por
  adapterDir** (copiar as fontes uma vez), mesmo o provider aparecendo em duas
  categorias. Subtask de dedup explícita em J0, com fixture que usa redis nas
  duas pontas.

- **R6 — Ordenação por chave no RabbitMQ vs. `QueueChannel` in-memory (NFR-22).**
  **Resolvido em §3.3:** `orderBy` declarado ⇒ exchange consistent-hash + N
  filas de partição, um consumidor por partição (ordem por chave preservada,
  concorrência entre chaves = `workers.concurrency`) — o MESMO contrato do
  in-memory, agora cross-process. Sem `orderBy` ⇒ work-queue com prefetch =
  concurrency (nenhuma ordem a preservar). Ordem estrita só por chave, nunca
  global (igual ao in-memory).

- **R7 — Fixture-âncora precisa combinar UseCase + Policy no mesmo módulo?**
  ISSUE-7 (`.claude/issues.md`) já mostrou que um módulo com UseCase E Policy
  colide no wiring (`Wire` duplicado). A fixture-âncora deste ciclo deve
  **evitar** essa combinação por módulo (senão o `dsc gen` falha por um motivo
  alheio a este ciclo) — ou ISSUE-7 vira pré-requisito. Decisão: estruturar a
  fixture para NÃO acionar ISSUE-7 (cada módulo é ou de UseCase ou de Policy,
  como shop já faz), mantendo este ciclo independente. Subtask de validação em
  J6.1.

- **R8 — Registro de `contracts` no consumidor cross-service (§3.3).** O
  binário consumidor precisa registrar as factories dos `PublicEvent` do pacote
  `contracts/` no seu `EventRegistry`, senão a desserialização do envelope AMQP
  falha em runtime ("tipo desconhecido"). Subtask explícita no wiring do
  consumidor em J3.

- **R9 — Durabilidade cross-service exige outbox→canal (§3.2a).** Publicar no
  broker direto do publisher do UnitOfWork acontece fora da tx SQL — crash
  pós-commit perde o evento. **Resolvido:** o relay do outbox é quem publica no
  `ChannelTransport` (RabbitMQ) para destinos cross-service; a entrega
  cross-service herda a durabilidade at-least-once do outbox (só marca
  `delivered_at` após o `Publish` no broker suceder). Subtask em J2/J3.

- **R10 — Smoke-compile de projeto com provider precisa dos drivers sem rede.**
  `go build` da fixture-âncora importa pgx/amqp/redis/aws-sdk. **Resolvido
  (§2.2):** o projeto é emitido **vendorizado** (`vendor/` + `go.sum`), os bytes
  dos drivers oficiais vêm da árvore vendorizada do próprio repo domainscript, e
  o smoke roda `go build -mod=vendor` offline. Subtask de vendoring em J0/J6.
