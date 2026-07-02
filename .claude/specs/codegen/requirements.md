# Requirements — Back-end do Transpilador DomainScript (Geração de Go)

> Documento 1 de 3 de um **novo** ciclo spec-driven (`requirements` → `design` →
> `tasks`), independente dos ciclos anteriores em `.claude/specs/` e
> `.claude/specs/type-checking/`. Define **o quê** e **por quê** desta etapa. Não
> define implementação (isso é `design.md`).
>
> Continuidade de numeração: este ciclo continua a série do projeto a partir de
> `REQ-14` e `NFR-11` (o front-end vai até `REQ-8`/`NFR-7`; a resolução de
> nomes & tipos até `REQ-13`/`NFR-10`), para um namespace de rastreabilidade único.

## 1. Introdução

### 1.1. Objetivo

Construir o **back-end** do transpilador DomainScript (spec v6.0): o estágio que
vai de um **programa já validado** (AST resolvida + tabela de símbolos + modelo de
tipos) até um **projeto Go completo, legível e compilável**. Onde o front-end
respondia "isto está correto?", o back-end responde "aqui está o código Go que
realiza isto".

```
                    ┌──────────── FRONT-END (pronto) ────────────┐
texto-fonte ──▶ LEXER ─▶ PARSER ─▶ RESOLVER ─▶ CHECKER ─▶ programa validado
                                                                 │
                                                                 ▼  (só se HasErrors() == false)
                                                          ┌── BACK-END ──┐
                                                          │  GERADOR Go  │──▶ projeto Go
                                                          └──────────────┘      (go build ✓)
```

O back-end **não é um novo compilador**: é um consumidor do que as fases
anteriores produziram. Ele não re-tokeniza, não re-parseia e não re-valida — opera
sobre `program.Program`, `symbols.SymbolTable` e `types.Model`.

### 1.2. Alinhamento filosófico com o spec

A geração encarna os mesmos princípios do §1.1 do spec da linguagem:

- **Zero infraestrutura no domínio, infraestrutura real no gerado.** O usuário
  escreveu só regras de negócio; o back-end materializa SQL/HTTP/filas/transações
  que o domínio nunca mencionou.
- **Transpilação, não interpretação.** A saída é Go idiomático que aproveita o
  ecossistema alvo, não um interpretador de DomainScript embarcado.
- **Fidelidade fail-fast.** As garantias estáticas (Regra de Ouro, exaustividade
  de `match`, `access` fechado-por-padrão) sobrevivem em runtime — o código gerado
  não pode reintroduzir o que o compilador proibiu.
- **Uma forma canônica de saída.** Para cada construto há uma única forma de Go
  gerada; regenerar o mesmo programa produz o mesmo código, byte a byte.

### 1.3. Escopo

Este ciclo cobre **a linguagem inteira**, mas entrega de forma **fatiada em
marcos** (ver §5 e `tasks.md`): o núcleo transacional roda primeiro; reações,
infraestrutura avançada e exposição gRPC/telemetria vêm em marcos seguintes.

| Em escopo | Fora de escopo |
|---|---|
| Geração de Go para **todos** os construtos `*.ds` (VO, Enum, Error, Event, Aggregate, Command, UseCase, View, Query, Projection, Policy, Worker, Saga, Notification, Adapter, Foreign, Metric, Upcast) | Otimização de performance do código gerado além de "sem O(n²) evitável" |
| Lowering de corpos executáveis (Handle, Apply, Valid, Operator, execute, coerce, source, steps de Saga) | Um runtime de DomainScript interpretado |
| Runtime de suporte **escrito à mão, vendorado, sem deps externas** | Frameworks de terceiros como base do gerado |
| Persistência: in-memory primeiro; `database/sql` plugável depois | Um ORM próprio; migrations automáticas de schema |
| Exposição HTTP via `net/http` (stdlib); gRPC/telemetria em marco posterior (dep externa isolada) | Deploy, containers, IaC |
| Canais de topologia (§11) atrás de seam: `direct`/`queue` in-memory; providers reais opt-in | Exposição TCP/UDP (§10) — sem modelo no front-end |
| Geração de testes Go a partir de `*.test.ds` | Editor/LSP, formatação de DomainScript |
| CLI `dsc gen` e API programática de geração | Retrocompilação Go → DomainScript |

**Alinhamento com o front-end (fonte de verdade da cobertura).** O gerador cobre
os construtos que o front-end (REQ-1..13) modela. Recursos que a prosa do spec v6
menciona mas o front-end **ainda não modela** ficam explicitamente fora deste
ciclo e entram quando o front-end os suportar: exposição **TCP/UDP** (§10, §14),
o receptor **`tenant`** em corpos (`tenant.id`/`tenant.tier`/`tenant.exists`,
§13.2), a built-in **`provision tenant(id)`** (§13.4) e o acesso nativo
**`events()`** em Aggregates (§4.5). Por outro lado, `tenancy: cross_tenant`
(`UseCaseDecl.Tenancy`), o bloco `storage` do Aggregate (`ast.StorageEntry`) e os
settings de `Interface` (incl. `port:`) **são** modelados e estão em escopo.

### 1.4. Pré-condição e baseline

O back-end **só gera código para um programa sem erros** (REQ-14). O front-end
completo e a resolução de nomes/tipos (REQ-1..13) são a baseline: a geração
assume uma AST cujos nomes já foram **validados** — mas **sem** anotação de símbolo
por nó (a AST não guarda o `*symbols.Symbol` resolvido) — mais a `symbols.SymbolTable`
coletada e um `types.Model` construído sobre ela. Como a AST não é anotada e a
inferência do front-end não cobre locais de `load`/`list`/`match`/`lambda`, o gerador
**reconsulta** a tabela e **reconstrói** o tipo de cada nome/local num ambiente de
tipos próprio (design §3.6a). Um programa com qualquer diagnóstico de severidade
`error` não é gerado.

### 1.5. Glossário (incremental)

| Termo | Definição |
|---|---|
| Emissor (emitter) | Componente que escreve texto Go garantidamente formatado (gofmt) e válido |
| Lowering | Tradução de um construto DomainScript (expr/stmt/decl) para sua forma Go |
| Runtime de suporte | Pacote Go escrito à mão, sem deps externas, vendorado no projeto gerado (event store, dispatcher, unit of work, …) |
| Vendorar | Emitir o código-fonte do runtime dentro do projeto gerado, sem exigir `go get` |
| Golden test | Teste que compara a saída gerada com um artefato Go de referência versionado |
| Smoke compile | Verificação de que o Go gerado de fato compila (`go build`) e passa testes de fumaça |
| Seam de dependência | Ponto (interface) onde uma dep externa opcional (driver DB, gRPC, OTel) pode ser plugada sem tocar o núcleo |
| Projeto gerado | O módulo Go de saída: `go.mod`, `runtime/`, um pacote por módulo de domínio, `cmd/` por service |

---

## 2. Requisitos Funcionais

> Formato EARS (**WHEN/WHILE/IF … THE SYSTEM SHALL …**). "O SISTEMA" = o back-end
> gerador. "O gerado" = o projeto Go produzido.

### REQ-14 — Orquestração da Geração e Pré-condições

**User story:** Como usuário, quero que o gerador só rode sobre um programa
válido e produza um projeto Go completo de forma determinística, para que o código
gerado seja confiável e versionável.

**Critérios de aceitação:**

1. WHEN o `DiagnosticBag` do programa contém ao menos um diagnóstico de severidade
   `error`, THE SYSTEM SHALL **não** gerar código e sinalizar a recusa ao chamador.
2. WHEN o programa é válido, THE SYSTEM SHALL consumir `program.Program`,
   `symbols.SymbolTable` e `types.Model` (nunca re-parsear) e produzir um conjunto
   de arquivos Go.
3. THE SYSTEM SHALL produzir saída **determinística**: a mesma entrada gera
   exatamente os mesmos arquivos, com o mesmo conteúdo byte a byte, em toda
   execução (ordenação estável de declarações, imports e arquivos).
4. THE SYSTEM SHALL pular subárvores marcadas com nós de erro sem gerar código para
   elas (consistente com a pré-condição de programa válido, defensivo contra AST
   parcial).
5. THE SYSTEM SHALL organizar a saída como um projeto Go: um `go.mod`, um pacote de
   runtime vendorado, um pacote Go por módulo de domínio e um `cmd/` por service.

### REQ-15 — Emissão de Go Legível e Válido

**User story:** Como usuário, quero que o Go gerado pareça escrito por um humano e
compile sem ajustes, para poder ler, revisar e versionar o resultado.

**Critérios de aceitação:**

1. THE SYSTEM SHALL emitir apenas Go **sintaticamente válido e formatado** por
   `gofmt` (via `go/format`, stdlib), de modo que `gofmt -l` não liste nenhum
   arquivo gerado.
2. THE SYSTEM SHALL gerar identificadores Go idiomáticos a partir dos nomes
   DomainScript (tipos e métodos exportados em PascalCase; campos exportados com
   tag `json` preservando o nome original; locais em camelCase), com uma estratégia
   determinística de desambiguação para colisões com palavras-reservadas Go.
3. THE SYSTEM SHALL emitir imports mínimos e ordenados; nenhum import não usado.
4. WHEN um construto tem um comentário de doc natural (o nome e o papel do
   construto), THE SYSTEM SHALL emitir um comentário de doc Go conciso, sem poluir
   com metadados de máquina.
5. THE SYSTEM SHALL depender, no código gerado, **apenas** da stdlib Go e do
   runtime vendorado (REQ-16); qualquer outra dependência é opt-in e isolada
   (REQ-26/29/30, NFR-12).
6. THE SYSTEM SHALL mapear cada tipo primitivo do spec (`integer`, `decimal`,
   `string`, `boolean`, `datetime`, `bytes`, os literais `duration`/`size`/`rate`, e
   `File`/`FileStream`/`FileRef`) a um **único** tipo Go documentado (design §3.3),
   com `decimal` num tipo **exato** do runtime (nunca `float64`) e os tipos de `File`
   emitidos de um template fixo do runtime (o `types.Model` os trata como opacos).

### REQ-16 — Runtime de Suporte Vendorado

**User story:** Como usuário, quero que o scaffolding repetitivo (event store,
dispatcher, unit of work) venha de um runtime enxuto e legível vendorado no
projeto, para não ter cada módulo reimplementando o mesmo e para não depender de
`go get`.

**Critérios de aceitação:**

1. THE SYSTEM SHALL emitir, no projeto gerado, um pacote `runtime/` cujo
   código-fonte é escrito à mão, versionado com o compilador e copiado verbatim.
2. THE SYSTEM SHALL fazer o runtime depender **apenas** da stdlib Go.
3. THE SYSTEM SHALL prover no runtime, no mínimo: contrato e implementação
   in-memory de **event store**, **repositório de aggregate**, **dispatcher de
   eventos**, **unit of work**, e **idempotency store**, todos atrás de interfaces
   (seam para implementações concretas posteriores).
4. WHEN o runtime é regenerado, THE SYSTEM SHALL produzir bytes idênticos (o
   runtime não varia por programa; é template estável).

### REQ-17 — ValueObjects e Enums

**User story:** Como usuário, quero que meus ValueObjects e Enums virem tipos Go
que preservam a validação e o comportamento, para que a Regra de Ouro tenha efeito
em runtime.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar cada `ValueObject` **wrapper** (`Email(string)`) como um
   tipo Go que embrulha o base, com um construtor `NewX` que executa o bloco
   `Valid` e retorna `(X, error)`.
2. THE SYSTEM SHALL gerar cada `ValueObject` **composto** (`Money`) como um struct
   imutável de campos, com construtor `NewX` que executa `Valid` e retorna
   `(X, error)`, sem setters exportados (imutabilidade).
3. THE SYSTEM SHALL gerar cada `Operator` de um VO como um método Go
   (`+`→`Add`, `-`→`Sub`, `>=`→`Gte`, …) com a assinatura correspondente ao spec,
   incluindo o corpo (lowering via REQ-22). O corpo enxerga os **receptores de VO do
   front-end** (`value` + campos por nome nu; **não** `self`), e o sentinela `ok` em
   `Valid` significa "validação passa" (design §3.3). Uma operação aritmética/
   relacional sobre um VO que **não** declara o operador correspondente não é
   gerável — o exemplo/programa deve declará-lo (design §7).
4. THE SYSTEM SHALL gerar cada `Enum` como um tipo Go nomeado sobre o base
   (`string`/`integer`/VO) com constantes para cada membro e uma função de coerção
   (`ParseX`) que reflete a coerção implícita (valor desconhecido → erro) ou o
   bloco `coerce` explícito quando presente.

### REQ-18 — Errors e Events

**User story:** Como usuário, quero que Errors virem valores de erro Go e Events
virem structs serializáveis e reproduzíveis, para que negócio e event sourcing
funcionem no gerado.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar cada `Error` de negócio como um valor de erro Go estável
   (implementando `error`), com a mensagem declarada, distinguível por igualdade/
   `errors.Is`.
2. THE SYSTEM SHALL gerar cada `Event`/`PublicEvent` como um struct de campos, com
   o metadata implícito (`timestamp`, `sequence`, `aggregateId`, `eventType`)
   acessível, e registrá-lo num **registry de eventos** para (de)serialização
   estável (nome do evento → tipo).
3. THE SYSTEM SHALL emitir (de)serialização determinística de eventos usando
   `encoding/json` (stdlib), com tags preservando os nomes originais dos campos.
4. WHEN um campo de evento declara `default`, THE SYSTEM SHALL aplicar o default na
   desserialização de eventos antigos que não o contêm (versionamento por default).
5. WHEN existe um `Upcast Event vN -> vN+1`, THE SYSTEM SHALL gerar a função de
   upcast e aplicá-la no caminho de replay/leitura ao carregar eventos da versão
   anterior.
6. WHEN um campo é `redactable`, THE SYSTEM SHALL gerar o suporte a substituí-lo por
   um placeholder tipado sem quebrar (de)serialização/replay.

### REQ-19 — Aggregates

**User story:** Como usuário, quero que cada Aggregate vire um tipo Go com estado,
handles e reconstrução, respeitando StateStored vs EventSourced e o `access`, para
que a fronteira transacional e o controle de acesso valham em runtime.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar o `state` do Aggregate como um struct de campos e um tipo
   de aggregate que o contém, com um identificador estável.
2. THE SYSTEM SHALL gerar cada `Handle` como um método que valida o `access`
   correspondente (checa `caller`), executa o corpo (lowering) e retorna os eventos
   produzidos por `emit` mais um `error` de negócio.
3. THE SYSTEM SHALL gerar cada `Apply` como um método que muta o `state` a partir
   do `event` (lowering), usado tanto no comando quanto na reconstrução.
4. WHEN a estratégia é `EventSourced`, THE SYSTEM SHALL gerar a reconstrução do
   aggregate a partir do stream de eventos (aplicando `Apply` em ordem), com suporte
   a snapshot quando `snapshot every N events` está declarado.
5. WHEN a estratégia é `StateStored` (padrão), THE SYSTEM SHALL gerar a persistência
   direta do `state` (sem replay), mantendo os mesmos `Handle`/`Apply`.
6. THE SYSTEM SHALL emitir, para cada Handle, a checagem de `access`
   fechado-por-padrão: um Handle sem entrada em `access` já foi barrado no
   front-end, então o gerado sempre tem a regra correspondente.

> Nota: o acesso nativo via `events()` (§4.5 do spec) não é modelado pelo
> front-end e fica fora deste ciclo (ver §1.3).

### REQ-20 — Commands e UseCases

**User story:** Como usuário, quero que Commands virem DTOs e UseCases virem
handlers transacionais (Unit of Work), para que a orquestração de aplicação
aconteça com consistência.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar cada `Command` como um struct DTO, com o metadata de
   idempotência implícito modelado no runtime (não como campo declarado pelo dev).
2. THE SYSTEM SHALL gerar cada `UseCase` como uma função/handler que abre uma
   **unit of work**, executa o corpo (`load`/`ensure`/chamadas de Handle) e faz
   commit atômico ao final, ou rollback em erro.
3. WHEN o UseCase declara `timeout` — ou, na ausência, WHEN o `Module` declara um
   timeout de módulo (herança módulo → UseCase/Saga, §18.2) — THE SYSTEM SHALL
   propagar o timeout via `context.Context` (stdlib) na execução.
4. WHEN o UseCase declara `idempotency`, THE SYSTEM SHALL gerar a checagem de chave
   de idempotência contra o idempotency store do runtime conforme §14: cache de
   sucesso e de erro de negócio (erro de infra permite retry); mesma chave com
   command diferente → `IdempotencyKeyConflict` (422); corrida da mesma chave →
   `wait`/`reject` conforme `concurrentRetry`; worker de limpeza de chaves
   expiradas gerado automaticamente; para Sagas, a chave mapeia de forma estável
   para o `sagaId`.
5. THE SYSTEM SHALL mapear a inferência transacional do §18.1 nos seus três casos:
   mesmo Database → commit local; Databases distintos **todos com `supportsXA`** →
   transação distribuída (2PC) sobre o seam de persistência (efetivada no marco
   `database/sql`, REQ-26.2; o in-memory usa um único store e degenera em commit
   local); cross-database sem XA universal e cross-service sem Saga **não chegam
   aqui** (barrados no front-end) — o fluxo cross-service válido é Saga (REQ-24).

### REQ-21 — Read Side: Views, Queries, Projections

**User story:** Como usuário, quero que Views/Queries/Projections virem structs de
leitura e funções de consulta, para servir leituras sem tocar o Write Side.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar cada `View` como um struct de campos de leitura (incl.
   `From Aggregate`, projetando os campos), com suporte ao bloco `visibility`
   (omissão de campos não autorizados na serialização).
2. THE SYSTEM SHALL gerar cada `Query` como uma função que recebe os parâmetros,
   executa a operação de leitura (`load`/`list`/`count` com `where`/`join`/
   `orderBy`/`skip`/`take`/`as`) e retorna o tipo declarado.
3. WHEN uma Query declara `cache`, THE SYSTEM SHALL gerar a consulta ao cache do
   runtime com a política do §15: `ttl`, invalidação por evento (inferida dos
   aggregates tocados, com override `invalidateOn`; in-process imediata após
   `emit`, antes da fila externa), `negativeCacheTtl`, proteção a stampede
   (request coalescing), fail-open na falha do backend, bypass via
   `Cache-Control: no-cache` e tenant incluído na chave.
4. THE SYSTEM SHALL gerar cada `Projection` como uma view materializada
   cross-aggregate, atualizada nos eventos de `refreshOn`.
5. THE SYSTEM SHALL traduzir as cláusulas SQL-like para operações do
   repositório/query do runtime (no marco in-memory, sobre coleções; no marco
   `database/sql`, sobre SQL) — o mesmo lowering, back-ends distintos.

### REQ-22 — Lowering de Expressões e Statements

**User story:** Como autor do compilador, quero uma tradução fiel e única de cada
expressão e statement DomainScript para Go, porque é o coração da geração dos
corpos.

**Critérios de aceitação:**

1. THE SYSTEM SHALL traduzir `ensure Cond else Ação` para um `if !Cond { … }` cuja
   ação reflete o contexto: `Error` → `return …, Err`; `Nop` → sem-op de laço;
   `break`/`break all`/`continue` → o controle de laço Go correspondente.
2. THE SYSTEM SHALL traduzir `match` (statement e expressão) para um `switch`
   exaustivo em Go, preservando braços de valor, guards (`when`) e wildcard `_`,
   sem reintroduzir não-exaustividade.
3. THE SYSTEM SHALL traduzir `for Var in Iter` para `for … range …` em Go, com
   `continue`/`break`/`break all` mapeados (o último via label).
4. THE SYSTEM SHALL traduzir `emit Evento(args)` para a construção do evento e sua
   coleta na lista de eventos do Handle/contexto.
5. THE SYSTEM SHALL traduzir operações aritméticas/relacionais sobre VOs para as
   chamadas de método de operador correspondentes (`a + b` → `a.Add(b)`), e sobre
   primitivos para os operadores Go nativos.
6. THE SYSTEM SHALL traduzir acesso a membro, construção de VO/Event/Command,
   indexação, range e lambdas para as formas Go equivalentes, usando o **ambiente de
   tipos local** (design §3.6a) — que estende o `types.Model` com o tipo dos locais
   introduzidos por `load`/`list`/`store`/`match`/`lambda` — para saber a forma do
   receptor.
7. THE SYSTEM SHALL traduzir as built-ins para chamadas ao runtime, em duas
   famílias: (a) **núcleo, sem dep externa** — `now()`, `uuid()`, `random`,
   `random_str`, e as operações de domínio `load`/`list`/`count`/`exists` (§2.6); (b)
   **ops de arquivo** — `store`, `signed_url`, `delete file`, `load File(ref)` (§2.5),
   que dependem do seam `FileStorage` do `mod.ds` e portanto entram com REQ-26 (marco
   posterior), não no núcleo transacional.
8. THE SYSTEM SHALL traduzir `log Level Msg { campos }` para uma chamada de log do
   runtime com trace context anexado.
9. WHEN uma operação de erro de negócio pode ocorrer (operador de VO que faz
   `ensure`, coerção que falha), THE SYSTEM SHALL propagar o `error` idiomaticamente
   (`if err != nil { return … }`), sem panics no caminho de negócio.

### REQ-23 — Policies e Workers

**User story:** Como usuário, quero que Policies reajam a eventos e Workers rodem
em background, para que os fluxos assíncronos do domínio funcionem.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar cada `Policy` como um subscriber registrado no dispatcher
   do runtime para o `Event`/`PublicEvent` de `on`, com a garantia de entrega
   declarada (`BestEffort`/`AtLeastOnce`).
2. THE SYSTEM SHALL gerar cada `Worker` como uma goroutine agendada conforme o
   modo: `every` (ticker), `cron` (agenda), `continuous` (loop com `source`,
   `concurrency`, `batchSize`, `maxRate`).
3. WHEN um Worker declara `onError { retry … }`, THE SYSTEM SHALL gerar a política
   de retry/backoff no runtime.
4. WHEN um Worker declara `scope: per_tenant`/`global`, THE SYSTEM SHALL gerar o
   escopo de execução correspondente (REQ-27).
5. THE SYSTEM SHALL usar o mesmo lowering de corpos (REQ-22) para `execute`/`source`
   de Policies e Workers.

### REQ-24 — Sagas e Transações Distribuídas

**User story:** Como usuário, quero que Sagas virem state machines com compensação,
para coordenar transações que cruzam banco/serviço.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar cada `Saga` como uma state machine com `state`, executando
   os `step` em ordem, cada um com `up` (ação), `down` (compensação) e
   `onInfraError` (política de infra).
2. WHEN um step falha após steps já executados, THE SYSTEM SHALL gerar a execução
   dos `down` em ordem reversa (compensação), respeitando `down { unrecoverable }`.
3. WHEN o modo é `async`, THE SYSTEM SHALL gerar o retorno de `sagaId` e um
   `SagaStatus` consultável; WHEN `await`, a execução bloqueante com timeout.
4. THE SYSTEM SHALL usar canais da topologia (REQ-26.5) para os passos cross-service.

### REQ-25 — Notifications, Adapters e Foreign (FFI)

**User story:** Como usuário, quero que Notifications e Adapters virem chamadas de
saída reais e que Foreign chame código Go que escrevi, para integrar com o mundo
externo.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar cada `Notification` como um contrato de saída e cada
   `Adapter` HTTP declarativo como uma chamada `net/http` (stdlib) com método, URL,
   headers e body mapeados (`env(...)` resolvido do ambiente).
2. THE SYSTEM SHALL gerar cada `Adapter` FFI e cada bloco `Foreign` como uma chamada
   à função Go correspondente em `adapters/`, com marshalling dos argumentos/retorno.
3. THE SYSTEM SHALL distinguir `notify` (async, via outbox/dispatcher) de `call`
   (sync) no código gerado.

### REQ-26 — Infraestrutura de Módulo (mod.ds)

**User story:** Como usuário, quero que os blocos de `mod.ds` configurem a
infraestrutura real do gerado, com persistência plugável, para ligar o domínio ao
mundo sem tocar o domínio.

**Critérios de aceitação:**

1. THE SYSTEM SHALL, no marco in-memory, ligar `Database`/`Cache`/`Idempotency`/
   `RateLimit`/`Outbox` a implementações in-memory do runtime, sem dep externa.
2. THE SYSTEM SHALL, num marco posterior, gerar um adapter de persistência sobre
   `database/sql` (stdlib) atrás da interface de repositório do runtime; o driver
   concreto (ex.: Postgres) é a **única** dep externa, isolada e opt-in.
3. THE SYSTEM SHALL respeitar `supportsXA`, `retry`, `circuitBreaker` e `tenancy`
   dos blocos `Database` ao configurar o adapter.
4. THE SYSTEM SHALL gerar a wiring (composição) que injeta as implementações
   configuradas nos UseCases/Queries/Policies/Workers, a partir do `mod.ds`.
5. THE SYSTEM SHALL materializar os canais da topologia (`topology.ds`, §11) atrás
   de um seam do runtime: `direct` → despacho in-process (default); `queue` → fila
   in-memory no núcleo, respeitando `orderBy` e os settings de `workers`
   (`concurrency`/`maxRate`/`batchSize`); `grpc`/`http` → chamada à borda
   correspondente, com `timeout`/`circuitBreaker` configurando o cliente.
   Providers reais (ex.: `rabbitmq`) são dep externa opt-in e isolada (NFR-12),
   ausentes quando não usados.

### REQ-27 — Multi-tenancy

**User story:** Como usuário, quero que o tenant seja contexto ambiente com filtro
automático, para isolar dados sem passar tenant como parâmetro.

**Critérios de aceitação:**

1. THE SYSTEM SHALL propagar o tenant como contexto ambiente (via `context.Context`),
   injetado na borda (interface.ds) e nunca como parâmetro explícito de domínio.
2. THE SYSTEM SHALL injetar o filtro de tenant em toda query/carregamento conforme a
   estratégia (`row_level`/`schema_per_tenant`/`database_per_tenant`); acesso a
   aggregate de outro tenant → 404.
3. WHEN um UseCase declara `tenancy: cross_tenant`, THE SYSTEM SHALL gerar o caminho
   sem filtro de tenant, exigindo a role privilegiada e emitindo trilha de auditoria.
4. WHEN uma rota com tenancy não consegue resolver o tenant na borda, THE SYSTEM
   SHALL falhar fechado (HTTP 400, §13.4).

> Nota: o receptor `tenant` em corpos (`tenant.id`/`tier`/`exists`, §13.2) e a
> built-in `provision tenant(id)` (§13.4) não são modelados pelo front-end e ficam
> fora deste ciclo (ver §1.3). O filtro ambiente deste REQ é injetado pelo gerador
> na camada de persistência/borda, não escrito pelo dev no domínio.

### REQ-28 — Exposição HTTP (interface.ds)

**User story:** Como usuário, quero que `interface.ds HTTP` vire um servidor
`net/http` que roteia para meus UseCases/Queries, para expor o sistema sem escrever
handlers à mão.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar um servidor `net/http` (stdlib) que registra cada `Route`
   (método + path → UseCase/Query), mapeando path params → parâmetros/`ref`, query
   string → parâmetros, body → JSON.
2. THE SYSTEM SHALL mapear os resultados a status codes: `200`/`201` sucesso;
   `400` tenant ausente (fail-closed, §13.4); `403` acesso negado pelo `access`;
   `404` não-encontrado (incl. aggregate de outro tenant, §13.2); `410` versão
   após `sunset` (§17); `422` erro de negócio (incl. coerção de Enum desconhecida
   e `IdempotencyKeyConflict`); `429` rate-limit; `503` erro de infraestrutura.
3. THE SYSTEM SHALL gerar a resolução de tenant da borda (`subdomain`/`header`/
   `jwt_claim`/`path`) e a chave de idempotência (header `Idempotency-Key`).
4. WHEN há `versioning`/`rateLimit`, THE SYSTEM SHALL gerar o roteamento por versão
   (§17: headers `Deprecation`/`Sunset` após `deprecated`; `410 Gone` após
   `sunset`; endpoints inalterados passam direto) e os hooks de rate limit na
   borda conforme §16: múltiplas dimensões → todas precisam passar; `429` com
   `Retry-After` e headers `X-RateLimit-*` (gRPC: `RESOURCE_EXHAUSTED`); `byTier`
   resolvido de `tenant.tier`; `onBackendFailure: open/closed` com override por
   endpoint; rotas sem tenant usam só `perIp`; retry idempotente não consome cota
   (integra REQ-20.4).

### REQ-29 — Exposição gRPC

**User story:** Como usuário, quero expor via gRPC quando declaro `Interface GRPC`,
ciente de que isso puxa a dependência de gRPC.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar o `.proto` a partir de `Interface GRPC` (services e rpc →
   UseCase/Query).
2. THE SYSTEM SHALL gerar os stubs de servidor gRPC, isolando a dependência externa
   `google.golang.org/grpc` num pacote de borda — **exceção documentada** à NFR-12,
   opt-in e ausente quando não há `Interface GRPC`.
3. THE SYSTEM SHALL manter o domínio inalterado: a borda gRPC chama os mesmos
   UseCases/Queries que a borda HTTP.

### REQ-30 — Observabilidade (Telemetry e Metric)

**User story:** Como usuário, quero traces/metrics/logs automáticos e minhas
`Metric` de negócio, para observar o sistema.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar, por padrão, instrumentação de logs estruturados via
   stdlib (`log/slog`), sem dep externa, com trace context propagado.
2. WHEN `Telemetry` (OpenTelemetry) é declarado, THE SYSTEM SHALL gerar um adapter
   OTel atrás de uma interface de observabilidade — **exceção documentada** à
   NFR-12, opt-in e isolada.
3. THE SYSTEM SHALL gerar cada `Metric` de negócio (counter/histogram) atualizada no
   gatilho `on Evento`/`on X.completed`, com labels.

### REQ-31 — Testes Gerados (*.test.ds)

**User story:** Como usuário, quero que meus testes declarativos virem testes Go
executáveis, para rodar `go test` sobre o gerado.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar, de cada `Test`/`scenario`, um teste Go (`testing`) que
   monta o `given`, executa o `when` e verifica o `then` (eventos emitidos, erro,
   estado, commit/rollback).
2. THE SYSTEM SHALL gerar mocks de Adapters a partir de `mock … returns …` e injetar
   falhas de step a partir de `fail step X with InfraError`.
3. WHEN há `property`, THE SYSTEM SHALL gerar um teste baseado em propriedades que
   gera sequências de comandos e checa a invariante, reportando o contra-exemplo.
4. THE SYSTEM SHALL gerar `Fixture`s como helpers reutilizáveis nos testes.

### REQ-32 — CLI e API de Geração

**User story:** Como integrador, quero gerar o projeto por linha de comando e
programaticamente, para usar em CI e em ferramentas.

**Critérios de aceitação:**

1. THE SYSTEM SHALL expor uma função programática que recebe um diretório (ou um
   `program.Program` já construído) e um diretório de saída, e escreve o projeto Go.
2. THE SYSTEM SHALL prover um subcomando de CLI (ex.: `dsc gen <dir> -o <out>`) que
   valida (REQ-14) e, se válido, gera; WHEN há erros, imprime os diagnósticos e
   retorna exit code ≠ 0 sem gerar.
3. WHEN a saída já existe de uma geração anterior, THE SYSTEM SHALL sobrescrever de
   forma idempotente (mesma entrada → mesma saída), sem deixar arquivos órfãos de
   declarações removidas.

---

## 3. Requisitos Não-Funcionais (incrementais)

> Os NFR-1..10 dos ciclos anteriores continuam valendo. Abaixo, os adicionais do
> back-end.

### NFR-11 — Legibilidade e Idiomaticidade
O Go gerado deve parecer escrito à mão: nomes idiomáticos, sem prefixos de máquina
gratuitos, comentários úteis e escassos, estrutura de pacotes clara. Um revisor Go
deve conseguir ler e entender o resultado sem conhecer DomainScript.

### NFR-12 — Dependências Mínimas
O núcleo do gerado depende **só** da stdlib Go e do runtime vendorado. Toda dep
externa (driver de banco, gRPC, OTel) é **opt-in**, **isolada atrás de interface** e
**ausente** quando o recurso não é usado. O núcleo transacional compila e roda sem
nenhum módulo externo (`go build`/`go run` só com o toolchain).

### NFR-13 — Determinismo e Idempotência de Saída
Regenerar o mesmo programa produz bytes idênticos. Ordenação estável de
declarações, imports, membros de map e arquivos. Isso permite versionar o código
gerado e revisar diffs limpos.

### NFR-14 — Correção por Construção
Todo Go gerado a partir de um programa válido **compila** (`go build ./...` verde) e
passa `go vet`. O back-end nunca emite código que não compila; um bug de geração é
uma falha, não um aviso.

### NFR-15 — Fidelidade Semântica
O comportamento do gerado reflete as regras estáticas do domínio: exaustividade de
`match` vira `switch` completo; a Regra de Ouro não é reintroduzida; `access`
fechado-por-padrão é aplicado; imutabilidade de VO é preservada.

### NFR-16 — Extensibilidade
Adicionar suporte a um construto novo exige um emissor localizado (uma função de
geração + um golden test), sem reescrever os emissores existentes (espelha NFR-5).

### NFR-17 — Testabilidade e Cobertura Pareada
Cada emissor tem (a) um **golden test** comparando a saída com um artefato de
referência e (b) participa de um **smoke compile**: os exemplos gerados
(`docs/examples/wallet`, `docs/examples/shop`) compilam e passam testes de fumaça.
Espelha NFR-4/NFR-10.

---

## 4. Rastreabilidade

| Requisito | Tema | Marco (tasks.md) |
|---|---|---|
| REQ-14 | Orquestração + pré-condições | E |
| REQ-15 | Emissor de Go legível | E |
| REQ-16 | Runtime vendorado | E |
| REQ-17 | ValueObjects e Enums | E |
| REQ-18 | Errors e Events | E |
| REQ-19 | Aggregates | E |
| REQ-20 | Commands e UseCases | E / G (2PC, idempotência) |
| REQ-21 | Read Side | E |
| REQ-22 | Lowering de corpos | E (núcleo) / G (ops de arquivo) |
| REQ-23 | Policies e Workers | F |
| REQ-24 | Sagas | F |
| REQ-25 | Notifications/Adapters/Foreign | F / G (arquivo) |
| REQ-26 | Infra de módulo (persistência, canais, FileStorage) | F (canais) / G |
| REQ-27 | Multi-tenancy | G |
| REQ-28 | Exposição HTTP | E (básico) / G (avançado) |
| REQ-29 | Exposição gRPC | H |
| REQ-30 | Observabilidade | H |
| REQ-31 | Testes gerados | H |
| REQ-32 | CLI/API de geração | E |

---

## 5. Critérios de Pronto (Definition of Done)

O back-end está completo quando:

1. Todo construto do spec v6 **modelado pelo front-end** é gerado em Go idiomático
   (REQ-17..25, 28..31); as exceções ficam explícitas em §1.3 (TCP/UDP, receptor
   `tenant`, `provision tenant`, `events()`).
2. O gerado a partir de `docs/examples/wallet` e `docs/examples/shop` **compila**
   (`go build ./...`) e passa os testes de fumaça (NFR-14/17).
3. A saída é determinística: dois runs produzem bytes idênticos (NFR-13).
4. O núcleo transacional compila e roda **sem nenhuma dep externa** (NFR-12).
5. Cada emissor tem golden test; deps externas (DB, gRPC, OTel) estão isoladas
   atrás de interfaces e ausentes quando não usadas.
6. A CLI `dsc gen` gera um projeto válido e recusa (exit ≠ 0) programas com erros.
7. `go build ./...` e `go test ./...` do **compilador** permanecem verdes.

### Entrega incremental (marcos)

Continuando os marcos do projeto (front-end fechou no Marco D):

- **Marco E — "gera e roda o núcleo transacional"**: VO/Enum/Error/Event/Aggregate/
  Command/UseCase/Query + lowering + runtime in-memory + HTTP básico + CLI `gen`. O
  Wallet gerado compila e passa smoke test com `go run`, zero deps externas.
- **Marco F — "reações e coordenação"**: Policy, Worker, Saga, dispatcher, outbox,
  Notifications/Adapters/Foreign.
- **Marco G — "infraestrutura real"**: adapter `database/sql` plugável, `FileStorage`
  e ops de arquivo (`store`/`signed_url`/`delete`/`load File`), idempotência, cache,
  rate limit, multi-tenancy, HTTP avançado (versioning, rateLimit).
- **Marco H — "exposição e observabilidade avançadas + testes"**: gRPC, telemetria
  OTel, `Metric`, geração de testes `*.test.ds`, fechamento (determinismo,
  idempotência, docs).
