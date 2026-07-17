# Requirements — Providers Reais de Infraestrutura (Postgres, RabbitMQ, Redis, S3, Outbox durável)

> Documento 1 de 3 de um **novo** ciclo spec-driven (`requirements` → `design` →
> `tasks`), continuação direta do ciclo `.claude/specs/read-side/` (REQ-33..40,
> NFR-18..20, Marco I, completo). Nasce do gap **G-4** de
> `.claude/specs/codegen/gaps.md` (registrado como **ISSUE-3** em
> `.claude/issues.md`): o sistema gerado hoje **não é implantável contra
> infraestrutura real além de sqlite** — todo provider de produção do spec v6
> (§11–§16) é rótulo decorativo ou só existe in-memory.
>
> **Escopo deste ciclo (recorte explícito do usuário):** dar suporte real
> APENAS a **Postgres** (Database), **RabbitMQ** (canal cross-service),
> **Redis** (Cache e RateLimit), **S3** (FileStorage) e **Outbox durável**. As
> demais categorias de G-4 (outros bancos, gRPC como canal, Dynamo para
> idempotência, backend `layered` de cache) ficam para ciclos futuros.
>
> Continuidade de numeração: este ciclo continua a série a partir de **REQ-41**
> e **NFR-21** (o ciclo read-side fechou em REQ-40/NFR-20), para um namespace de
> rastreabilidade único. Marco: **J** (o read-side fechou no Marco I).

## 1. Introdução

### 1.1. Objetivo

Tornar um programa DomainScript validado **implantável contra infraestrutura
real** para as cinco categorias acima, sem tocar em uma linha de domínio: o
mesmo `.ds` que hoje gera um projeto in-memory passa a gerar, quando declara um
provider real, o adapter que fala com Postgres/RabbitMQ/Redis/S3 — cada um
opt-in, isolado atrás do seam que já existe, e adicionado a `go.mod` só quando o
programa de fato o declara. O núcleo transacional in-memory continua compilando
e rodando **sem nenhuma dependência externa** (NFR-12 preservado — agora
NFR-21).

### 1.2. Baseline (o que JÁ existe — nada disso é trabalho deste ciclo)

Todo provider deste ciclo tem **o seam pronto** — o trabalho é implementar o
lado real atrás dele, não desenhá-lo:

- **Database / SQL:** seam `Dialect` + registro único de provider
  (`codegen/sqlrt/dialect.go.txt`, `codegen/sql_wiring.go:sqlProviders`,
  REQ-40, ciclo read-side). Adicionar um banco é "implementar `Dialect` +
  uma entrada no registro"; o EventStore/UnitOfWork/Collection já rodam
  inteiramente sobre `Dialect` (nenhuma string SQL específica de banco fora
  dele). O adapter sqlite (`open_sqlite.go.txt`, `eventstore.go.txt`,
  `uow.go.txt`, `twophase.go.txt`, `collection.go.txt`) é o modelo a espelhar.
- **Canais:** seam `runtime.ChannelTransport` (`codegen/rtsrc/channel.go.txt`)
  — `direct`/`queue` in-memory prontos; `codegen/channel.go` já lê
  `orderBy`/`workers`/`timeout`/`circuitBreaker` e monta o transporte. Um
  provider real "entra atrás desta MESMA seam: um futuro `NewXxxChannel` com o
  shape idêntico" (doc do próprio arquivo). `via: queue` + `provider:
  "rabbitmq"` hoje roda in-memory (só `via: grpc/http/stream` são erro).
- **Cache:** seam `runtime.QueryCache` (`codegen/rtsrc/querycache.go.txt`) —
  `memory` pronto, com fail-open e request-coalescing já no shape da interface
  ("a future real (networked) backend failing is modeled as Get reporting
  hit=false"). `codegen/decl_query_cache.go` dirige o seam.
- **RateLimit:** seam `runtime.Limiter`/`CheckRateLimits`
  (`codegen/rtsrc/ratelimit.go.txt`) — três algoritmos in-memory
  (token_bucket/sliding/fixed) atrás de UMA interface, transport-agnóstica.
  `codegen/ratelimit.go` compila as dimensões de rota para `[]RateLimitCheck`.
- **FileStorage:** seam `runtime.FileStorage` (`codegen/rtsrc/filestorage.go.txt`)
  — `memoryFileStorage` pronto; `store`/`signed_url`/`delete file`/`load
  File(ref)` (`codegen/lower/builtins.go`) já rodam atrás dele. "Um backend
  real (S3/GCS/Azure Blob/...) entra atrás desta MESMA interface" (doc).
- **Outbox:** seam `runtime.Outbox` (`codegen/rtsrc/outbox.go.txt`) —
  `memoryOutbox` (stopgap, **não durável**) forwarda ao Dispatcher. A própria
  doc já aponta: "replaces the implementation with a real durable/retried one
  (backed by the event store or a dedicated outbox table) behind this SAME
  interface — generated code that calls Outbox.Subscribe today does not
  change".
- **go.mod opt-in:** `EmitGoMod` (`codegen/project.go`) já exige o driver de
  cada provider SQL ativo via `activeSQLProviders` — o padrão a estender para
  as outras categorias.
- **Padrão de adapter opt-in:** `codegen/sqlrt/`, `codegen/grpcrt/`,
  `codegen/otelrt/` — fontes `.txt` embarcadas, copiadas verbatim para o
  projeto gerado só quando a feature é declarada (`embed.go`/`Sources()`).

### 1.3. Escopo

| Em escopo | Fora de escopo |
|---|---|
| **Postgres** como Database provider real: `Dialect` postgres + entrada no registro + driver em `go.mod` + type mapping do EventStore/Collection | Outros bancos (MySQL, SQL Server, Mongo, Cassandra) — cada um é uma entrada de registro futura |
| **RabbitMQ** como `ChannelTransport` real cross-process (publisher + consumidor, ack/nack, ordenação best-effort por chave) | gRPC/HTTP/stream como `via` de canal (G-4, ciclo futuro) |
| **Redis** como backend de `QueryCache` (Cache §15) **e** de `Limiter` (RateLimit §16) | Backend `layered`/`distributed` genérico de cache além de redis; Memcached |
| **S3** como backend de `FileStorage` (§2.5/§12): put/get/signed_url/delete, incl. stream | GCS/Azure Blob — entradas de registro futuras |
| **Outbox durável**: tabela transacional atômica com a tx de negócio, entrega at-least-once real com retry/backoff | Saga distribuída sobre broker; padrões de entrega exactly-once |
| Registro/seleção unificado de provider por categoria + `go.mod` opt-in por categoria | Idempotency `external` (Redis/Dynamo, §14) — seam pronto, mas fora deste recorte |
| Config por ambiente (DSN/URL via `env(...)`) + fail-closed no startup + wiring em `cmd/<service>/main.go` | Orquestração de deploy, Helm, Terraform, migrations versionadas (só o DDL mínimo do gerador) |
| Testes: golden + smoke-compile + unit de dialeto/serialização; integração real atrás de build tag opt-in | Exigir infra viva no `go test ./...` default (NFR-24) |

### 1.4. Critério-âncora (a régua deste ciclo)

Uma fixture multi-service (evolução do `shop` ou fixture dedicada) que declara,
ao mesmo tempo:

- `Database { provider: "postgres" }` em pelo menos um módulo;
- um canal de topologia `via: queue` `provider: "rabbitmq"` entre dois services;
- `Cache { backend: redis }` e `RateLimit { backend: redis }` num módulo com
  Query cacheada e rota com `rateLimit`;
- `FileStorage { provider: "s3" }` num módulo que usa `store`/`load File`;
- uma Policy `delivery AtLeastOnce` cujo Outbox é o durável.

**passa a gerar** um projeto que:

1. **compila** (`go build ./...`) e **vet-limpa** (`go vet ./...`) sobre os
   bytes escritos em disco (NFR-17), com os adapters reais presentes.
2. puxa para `go.mod` **exatamente** o módulo externo de cada provider
   declarado — e **nada** para uma categoria não declarada (o mesmo programa
   sem os providers gera `go.mod` sem nenhuma dep externa, NFR-21).
3. passa os testes de **dialeto/serialização** (unit, sem infra): o SQL
   parametrizado do dialeto postgres (`$1`), o envelope de mensagem do
   RabbitMQ, a chave/serialização do Redis, o layout de objeto do S3, o schema
   da tabela de outbox.
4. **quando** infra viva + build tag `integration` estão presentes, passa os
   testes comportamentais end-to-end contra Postgres/RabbitMQ/Redis/S3 reais —
   provando paridade com o caminho in-memory (NFR-22). Sem isso, esse passo é
   pulado, não falha (NFR-24).

---

## 2. Requisitos Funcionais

### REQ-41 — Postgres como Database provider real

**User story:** Como operador, quero declarar `Database { provider: "postgres"
}` e obter um EventStore/Collection que persiste em Postgres real, para
implantar o serviço em produção sem mudar o domínio.

**Critérios de aceitação:**

1. THE SYSTEM SHALL fornecer uma implementação de `Dialect`
   (`codegen/sqlrt`, ex. `PostgresDialect`) que encapsula tudo que varia por
   banco: placeholder posicional (`$1`, `$2`, …), DDL das tabelas `events` e da
   tabela de `Collection[T]`, e a forma de paginação (`LIMIT n OFFSET m`) —
   **nenhuma string SQL específica de Postgres fora dessa classe** (REQ-40.1).
2. THE SYSTEM SHALL acrescentar UMA entrada em `sqlProviders`
   (`codegen/sql_wiring.go`) para `"postgres"` (módulo do driver, versão,
   versão mínima de Go, construtor do `Dialect`) — e **nenhuma** outra mudança
   em lowering, `decl_*.go` ou no runtime núcleo (REQ-40.2). A partir daí,
   `activeSQLProviders`/`programNeedsSQLAdapter`/`EmitGoMod` reconhecem
   postgres automaticamente.
3. THE SYSTEM SHALL abrir a conexão real (`sqlruntime.Open` para postgres) a
   partir do DSN declarado, e montar o EventStore com o registry de eventos do
   módulo — o MESMO wiring de `emitXADatabaseWiring`/`decl_usecase.go` que hoje
   serve sqlite, parametrizado pelo `dialectCtor` do provider (já é).
4. THE SYSTEM SHALL mapear os tipos que o gerador emite (id `TEXT`, payload
   JSON) para os tipos Postgres corretos (`text`/`jsonb` ou `text`, decisão do
   design), preservando a semântica de `json_extract`/igualdade de campo do
   `WhereEq` (REQ-38) via a sintaxe JSON do Postgres.
5. WHEN o programa declara `"postgres"`, THE SYSTEM SHALL exigir o driver em
   `go.mod`; WHEN não declara, `go.mod` NÃO SHALL conter o driver (NFR-21).

### REQ-42 — Outbox durável (tabela transacional)

**User story:** Como operador, quero que uma Policy `delivery AtLeastOnce`
sobreviva a um crash entre o commit da tx de negócio e a execução do handler,
para ter a garantia at-least-once que o spec §7 promete.

**Critérios de aceitação:**

1. THE SYSTEM SHALL fornecer uma implementação de `runtime.Outbox` durável que
   grava o evento a entregar numa **tabela de outbox** dentro da MESMA
   transação que persiste os eventos de negócio (atomicidade store+outbox) —
   nunca "publica e torce", nunca perde a entrega num crash pós-commit.
2. THE SYSTEM SHALL entregar cada evento gravado ao(s) handler(s) inscrito(s)
   com **retry e backoff**, marcando a linha como entregue só após sucesso do
   handler; uma falha de handler re-tenta (at-least-once — o handler DEVE ser
   idempotente, como o spec já exige e o exemplo pizzeria demonstra).
3. THE SYSTEM SHALL preservar a assinatura `Outbox.Subscribe(eventType,
   handler)` **inalterada** — o código gerado que chama `Outbox.Subscribe`
   hoje não muda (a doc de `outbox.go.txt` já garante o contrato).
4. THE SYSTEM SHALL usar a tabela de outbox atrás do MESMO `Dialect` (REQ-41):
   o DDL e o SQL de enfileirar/varrer/marcar/**purgar** vivem no dialeto,
   funcionando sobre sqlite **e** postgres sem string específica de banco fora
   dele; a varredura SHALL usar `ORDER BY id` (FIFO) e, no postgres, `FOR UPDATE
   SKIP LOCKED` (lote exclusivo por réplica, §design 3.2).
5. WHEN o módulo não tem Database real (só in-memory), THE SYSTEM SHALL manter
   o `memoryOutbox` stopgap de hoje — o Outbox durável é opt-in, atrelado à
   presença de um Database real (NFR-21).
6. THE SYSTEM SHALL fazer o **relay do outbox alimentar o canal cross-service**:
   um `PublicEvent` com destino em outro service é publicado no
   `ChannelTransport` (REQ-43) **de dentro do relay**, não direto pelo publisher
   da tx — só assim a entrega cross-service herda a durabilidade at-least-once
   (marca `delivered_at` só após o `Publish` no broker suceder). Publicar direto
   no commit (fora da tx) é **proibido** para destino cross-service (§design
   3.2a).
7. THE SYSTEM SHALL apagar periodicamente as linhas entregues mais velhas que
   uma janela de retenção (`StartOutboxCleanup`, análogo a
   `StartIdempotencyCleanup`) — a tabela de outbox nunca cresce sem limite.

### REQ-43 — RabbitMQ como transporte de canal cross-process

**User story:** Como arquiteto, quero que um canal `via: queue provider:
"rabbitmq"` entregue eventos entre dois services em **processos separados**,
para que a topologia do spec §11 funcione num deploy real distribuído.

**Critérios de aceitação:**

1. THE SYSTEM SHALL fornecer uma implementação de `runtime.ChannelTransport`
   (ex. `NewRabbitMQChannel`) com o shape idêntico ao `QueueChannel`
   (`Subscribe`/`Publish`), que publica em / consome de uma exchange/queue
   RabbitMQ real — entrega **cross-process** de verdade (fecha a "known,
   documented Marco F limitation" de `channel.go.txt`).
2. THE SYSTEM SHALL, no lado produtor (`cmd/<service>/main.go`,
   `generateCmdMainFile`) e no consumidor (`decl_policy.go`,
   `emitPolicyWireFunc`), construir a instância RabbitMQ a partir da URL de
   conexão declarada no canal (`connection: env(...)`), mantendo o wiring de
   produtor/consumidor que já existe para `queue`.
3. THE SYSTEM SHALL preservar a semântica de ordenação do `QueueChannel`
   in-memory (NFR-22): `orderBy` declarado ⇒ **exchange consistent-hash** por
   `hash(chave)` para N filas de partição, um consumidor por partição (ordem por
   chave preservada, concorrência = `workers.concurrency` entre partições); sem
   `orderBy` ⇒ work-queue com prefetch = concurrency (sem ordem). Ordem estrita
   só por chave, nunca global (§design 3.3). `timeout`/`circuitBreaker`
   reusam a leitura de `channel.go`, sem reinterpretar o `.ds`.
4. THE SYSTEM SHALL fazer `ack` após sucesso do handler; numa falha, `nack
   requeue=false` para uma DLX+retry-queue(TTL) que reencaminha (o broker
   incrementa `x-death`), e após esgotar `circuitBreaker.threshold` a mensagem
   vai para a DLQ final — nunca `ack` antecipado nem `requeue=true` incondicional
   (poison pill; §design 3.3).
5. THE SYSTEM SHALL registrar as factories dos `PublicEvent` de `contracts/` no
   `EventRegistry` do binário **consumidor** (além do registry do módulo), para
   desserializar o envelope AMQP cross-service — senão o consumo falha em runtime
   (§design 3.3, R8).
6. THE SYSTEM SHALL reconectar automaticamente (loop com backoff sobre
   `NotifyClose`) re-estabelecendo conexão/canal/consumidores — um blip de rede
   não pode parar o consumidor em silêncio (§design 3.3).
7. WHEN o canal declara `via: queue` SEM `provider: "rabbitmq"` (ou provider
   não reconhecido), THE SYSTEM SHALL manter o `QueueChannel` in-memory de hoje
   (o provider real é opt-in, NFR-21); `via: grpc/http/stream` seguem erro de
   geração claro.

### REQ-44 — Redis como backend de Cache e de RateLimit

**User story:** Como operador de um serviço com N réplicas, quero que o cache
de Query e o rate limit sejam **compartilhados** entre réplicas via Redis, para
que a cota e a invalidação valham para o cluster inteiro, não por processo.

**Critérios de aceitação:**

1. THE SYSTEM SHALL fornecer uma implementação de `runtime.QueryCache` sobre
   Redis (Cache §15) que preserva a semântica do seam: TTL, cache de resultado
   negativo, **fail-open** (falha do Redis ⇒ `Get` reporta `hit=false`, a
   query roda de verdade — o shape já previsto na doc de `querycache.go.txt`) e
   invalidação por `InvalidateAll` da instância (namespaced por Query).
2. THE SYSTEM SHALL fornecer uma implementação de `runtime.Limiter` sobre Redis
   (RateLimit §16) com contagem **atômica** cross-réplica (ex. `INCR`+`EXPIRE`
   ou script Lua para token_bucket/sliding/fixed), preservando o contrato
   `CheckRateLimits` (AND multi-dimensão) e as dimensões
   `perIp/perUser/perTenant/perApiKey/global` que `codegen/ratelimit.go` já
   compila.
3. THE SYSTEM SHALL manter a política "retry idempotente não consome cota"
   (spec §14/§16) inalterada — a decisão continua em `codegen/http.go` (peek do
   replay antes de `CheckRateLimits`); o backend Redis só troca o `Limiter`, não
   essa lógica de borda.
4. THE SYSTEM SHALL selecionar o backend Redis quando (e só quando) `mod.ds`
   declara `Cache { backend: redis }` e/ou `RateLimit { backend: redis }` — na
   ausência, o in-memory de hoje permanece (NFR-21). A URL do Redis vem de
   `env(...)`.
5. WHEN o Redis está indisponível, THE SYSTEM SHALL degradar o **rate limit**
   para o `Limiter` in-memory local (por-réplica) — **fallback local**, não
   fail-open puro: a proteção continua ativa (por processo) durante a queda, sem
   abrir uma janela de abuso. O **cache** SHALL sempre falhar aberto (§15),
   nunca bloqueando a query por indisponibilidade de Redis (§design 3.4).

### REQ-45 — S3 como FileStorage provider real

**User story:** Como desenvolvedor, quero que `store`/`load File(ref)`/
`signed_url`/`delete file` gravem em um bucket S3 real, para que os arquivos do
domínio persistam fora do processo.

**Critérios de aceitação:**

1. THE SYSTEM SHALL fornecer uma implementação de `runtime.FileStorage` sobre
   S3 que implementa as operações que `codegen/lower/builtins.go` já emite:
   `store(File) → FileRef`, `load File(ref) → File`, `signed_url(ref, ttl) →
   string`, `delete file(ref)` — preservando as structs `File`/`FileRef`
   (metadados no objeto, bytes no corpo).
2. THE SYSTEM SHALL gerar URLs assinadas reais (presigned GET) com o TTL
   declarado, e `store` SHALL devolver um `FileRef` cujo `ID` localiza o objeto
   no bucket (key **única** por `store` — UUID v4, espelhando o
   `memoryFileStorage`; desduplicação por hash de conteúdo é outra semântica,
   fora do recorte — ver §design 3.5).
3. THE SYSTEM SHALL suportar o caminho de `FileStream` (upload/download
   chunk-a-chunk) **na medida em que** `builtins.go` o exercite hoje (G1a cobre
   só `File`/`FileRef`); ampliar para `FileStream` só se o lowering já o
   emitir, senão fica registrado como desvio.
4. THE SYSTEM SHALL selecionar o backend S3 quando (e só quando) o programa
   declara `FileStorage { provider: "s3" }` (§12); na ausência, o
   `memoryFileStorage` permanece (NFR-21). Bucket/região/credenciais vêm de
   `env(...)`/ambiente AWS padrão.

### REQ-46 — Registro de provider por categoria + go.mod opt-in

**User story:** Como mantenedor, quero que cada categoria de provider tenha o
MESMO modelo que o registro SQL (REQ-40.2): "implementar o adapter + uma entrada
no registro", com `go.mod` reagindo sozinho — para que o próximo provider (o 6º
banco, o 2º broker) seja barato e localizado.

**Critérios de aceitação:**

1. THE SYSTEM SHALL ter, para CADA categoria deste ciclo (canal, cache,
   ratelimit, filestorage), um **registro único** análogo a `sqlProviders`:
   string de provider/backend → (módulo do driver p/ `go.mod` + versão + import
   + construtor do adapter). Adicionar um provider a uma categoria = uma entrada
   + a implementação do adapter — zero mudanças em lowering/`decl_*.go`/runtime
   núcleo.
2. THE SYSTEM SHALL estender `EmitGoMod` (`codegen/project.go`) para exigir o
   módulo externo de cada provider **ativo** de qualquer categoria
   (deduplicado, ordenado — determinismo NFR-23), do mesmo jeito que
   `activeSQLProviders` já faz para SQL — e **nada** para categorias sem
   provider real declarado.
3. THE SYSTEM SHALL copiar as fontes `.txt` do adapter de cada provider ativo
   para o projeto gerado (novos dirs opt-in, ex. `codegen/amqprt/`,
   `codegen/redisrt/`, `codegen/s3rt/`, análogos a `sqlrt/`) — só quando ativo,
   via `Sources()`/`embed.go`, mantendo o padrão existente.
4. WHEN nenhuma categoria declara provider real, THE SYSTEM SHALL gerar um
   projeto **byte-idêntico** ao de hoje (NFR-21/NFR-23) — os exemplos
   wallet/shop atuais não regridem.
5. THE SYSTEM SHALL emitir o projeto **vendorizado** quando algum provider real
   está ativo: `go.sum` com os hashes fixados + um `vendor/` com a árvore dos
   drivers oficiais ativos e suas deps transitivas, de modo que `go build
   -mod=vendor` rode **offline** (sem download; smoke-compile sem rede, NFR-24).
   Os bytes vendorizados vêm da árvore de dependências do próprio repositório
   domainscript (que passa a depender dos drivers oficiais). Drivers **oficiais
   têm prioridade** (pgx, amqp091-go, go-redis, aws-sdk-go-v2): onde falar com
   infra real exige a lib estabelecida, usa-se ela — reimplementá-la seria menos
   seguro (§design 2.2). Sem provider ativo, **nenhum** `vendor/`/`go.sum` extra
   (byte-idêntico, NFR-21).

### REQ-47 — Configuração por ambiente, fail-closed no startup, e wiring

**User story:** Como operador, quero que as credenciais/URLs de infra venham do
ambiente e que o serviço **recuse subir** se um provider declarado não estiver
alcançável, para nunca rodar meio-configurado.

**Critérios de aceitação:**

1. THE SYSTEM SHALL ler DSN/URL/bucket de cada provider a partir de `env(...)`
   declarado no `.ds` (Database `connection`, Channel `connection`, Cache/
   RateLimit/FileStorage — forma de design), resolvido em runtime pela variável
   de ambiente — nunca credencial hardcoded no código gerado.
2. THE SYSTEM SHALL, no `cmd/<service>/main.go`, abrir cada conexão real no
   startup e **falhar o processo** se a conexão/ping falhar — fail-closed. Com
   múltiplos recursos abertos em sequência, o wiring usa o padrão `func run()
   error` (cada passo `return err`, `defer Close()` roda no unwind, `main()`
   faz o `log.Fatal` único) para não vazar os recursos já abertos — nunca um
   `log.Fatal` no meio que pule os `defer Close()` anteriores (ver §design
   3.6). O ping de banco usa contexto com timeout curto, nunca
   `context.Background()`.
3. THE SYSTEM SHALL fechar os recursos (conexões, canais) ordenadamente no
   shutdown do serviço (defer/Close), sem vazar conexão — na medida do wiring
   de `main.go` gerado.
4. THE SYSTEM SHALL manter o wiring in-memory intacto quando nenhum provider
   real é declarado: `main.go` continua byte-idêntico ao de hoje (NFR-21/23).

### REQ-48 — Estratégia de teste em três camadas

**User story:** Como mantenedor sem infra viva no CI default, quero provar a
correção dos adapters sem exigir Postgres/RabbitMQ/Redis/S3 rodando — e poder
rodar a prova end-to-end quando a infra existir.

**Critérios de aceitação:**

1. THE SYSTEM SHALL ter, para cada provider, **golden test** (fonte do adapter
   gerada vs. referência versionada) + **smoke-compile** (o projeto gerado com
   o adapter presente builda `go build -mod=vendor` **offline** contra o
   `vendor/` emitido e `go vet`a limpo sobre os bytes em disco) — NFR-17/24, sem
   infra e sem download.
2. THE SYSTEM SHALL ter **unit tests de dialeto/serialização** sem infra: SQL
   `$N` do PostgresDialect (reusando o padrão do "segundo dialeto de teste"
   REQ-40.3), envelope de mensagem AMQP, chave/serialização Redis, key/layout
   de objeto S3, DDL/SQL da tabela de outbox.
3. THE SYSTEM SHALL ter **testes de integração opt-in** atrás de build tag
   (`//go:build integration`) e guardados por env (URL presente): quando a
   infra está disponível, rodam o comportamento end-to-end e provam paridade
   com o in-memory (NFR-22); quando não, são pulados — **jamais** entram no
   caminho de `go test ./...` default nem quebram o CI padrão (NFR-24).
4. THE SYSTEM SHALL des-adaptar/estender as fixtures e a doc dos exemplos que
   hoje documentam esses providers como decorativos (ex. `docs/examples/
   pizzeria` diz "postgres/rabbitmq/mongodb são rótulos decorativos") — quando
   o provider passar a ser real, a nota vira "real, opt-in" (ou o exemplo migra
   para o provider real), sem regressão dos demais (NFR-19/22).

---

## 3. Requisitos Não-Funcionais (incrementais)

> NFR-1..20 dos ciclos anteriores continuam valendo — em particular NFR-11
> (Go idiomático), NFR-12 (deps mínimas do núcleo), NFR-13 (determinismo
> byte-idêntico), NFR-14 (correção por construção), NFR-17 (golden + smoke
> pareados), NFR-18 (semântica única entre backends).

### NFR-21 — Núcleo sem deps externas, provider opt-in por categoria
O núcleo transacional in-memory (event store, dispatcher, uow, HTTP, cache/
ratelimit/filestorage/outbox in-memory) continua compilando e rodando com
**Go stdlib + `runtime/` vendorizado apenas**. Cada driver externo (postgres,
rabbitmq, redis, s3) entra em `go.mod`/`go.sum`/`vendor/` **exclusivamente**
quando o programa declara aquele provider — categoria não declarada não puxa
nada. Onde um provider real é declarado, o driver **oficial** é a escolha
(prioridade sobre reimplementar protocolo, que seria menos seguro) e é
**vendorizado** para build offline (REQ-46.5). Extensão direta de NFR-12: deps
mínimas no núcleo, deps reais e auditadas onde falar com infra as exige.

### NFR-22 — Paridade comportamental in-memory ↔ provider real
Para toda operação que existe nos dois lados (persistência, entrega de canal,
cache, rate limit, arquivo), o resultado observável pelo domínio é o MESMO no
caminho in-memory e no provider real. A prova é o teste de integração pareado
(REQ-48.3) quando há infra; sem infra, o unit de dialeto/serialização
(REQ-48.2) prova a equivalência da superfície. Extensão de NFR-18 para além do
SQL.

### NFR-23 — Determinismo com provider ativo
Regenerar o mesmo programa com provider(es) real(is) ativo(s) produz saída
byte-idêntica: ordenação estável de entradas de `go.mod`, imports, fontes de
adapter e wiring em `main.go`. Extensão de NFR-13.

### NFR-24 — Integração real nunca no caminho default
`go build ./...` e `go test ./...` do compilador — e o smoke-compile dos
exemplos — **jamais** exigem Postgres/RabbitMQ/Redis/S3 vivos. Todo teste que
precise de infra é opt-in por build tag `integration` + env-guarded, e sua
ausência é *skip*, nunca *fail*.

---

## 4. Rastreabilidade

| Requisito | Tema | Gap de origem |
|---|---|---|
| REQ-41 | Postgres (Database) | G-4 (Database) |
| REQ-42 | Outbox durável | G-4 (Outbox) |
| REQ-43 | RabbitMQ (canal) | G-4 (Canais) |
| REQ-44 | Redis (Cache + RateLimit) | G-4 (Cache/RateLimit) |
| REQ-45 | S3 (FileStorage) | G-4 (FileStorage) |
| REQ-46 | Registro por categoria + go.mod opt-in | G-4 (transversal) |
| REQ-47 | Config por env + fail-closed + wiring | G-4 (transversal) |
| REQ-48 | Estratégia de teste em 3 camadas | NFR-17/24 |
| NFR-21..24 | transversais | — |

---

## 5. Critérios de Pronto (Definition of Done)

O ciclo está completo quando:

1. A fixture-âncora (§1.4) gera um projeto que compila e vet-limpa com os cinco
   providers reais presentes (golden + smoke, NFR-17).
2. Postgres é um adapter real (REQ-41); o Outbox durável grava atômico com a tx
   de negócio **e alimenta o canal cross-service** (o relay publica no broker,
   não o commit — REQ-42.1/42.6), com cleanup de retenção (REQ-42.7); o canal
   RabbitMQ entrega cross-process com ordem por chave (consistent-hash),
   reconexão e DLQ (REQ-43); Redis respalda Cache e RateLimit **com fallback
   local** no rate limit (REQ-44); e S3 respalda FileStorage (REQ-45) — cada um
   opt-in e isolado atrás do seam existente.
3. `go.mod`/`go.sum`/`vendor/` refletem exatamente os providers declarados e
   **nada** a mais; o mesmo programa sem providers gera projeto byte-idêntico ao
   de hoje (NFR-21/23); wallet/shop sem regressão (NFR-19).
4. Cada provider tem golden + smoke (build **offline** `-mod=vendor`) + unit de
   dialeto/serialização passando **sem infra e sem download** (REQ-48.1/2); os
   testes de integração existem, rodam quando há infra + build tag, e são
   pulados (não falham) sem ela (REQ-48.3, NFR-24).
5. `go build ./...` / `go test ./...` do compilador verdes; a doc dos exemplos
   que marcava esses providers como decorativos é atualizada (REQ-48.4).
6. `.claude/specs/codegen/gaps.md` e `.claude/issues.md` atualizados: G-4/
   ISSUE-3 marcados como **parcialmente fechados por este ciclo** (as cinco
   categorias no recorte), com ponteiro para cá e o restante de G-4 (outros
   bancos, gRPC-canal, Dynamo, layered) explicitamente ainda aberto.
