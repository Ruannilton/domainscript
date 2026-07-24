# Issues

Registro de erros encontrados pelos agentes durante a execução das specs que
**não pertencem ao escopo da task/spec em andamento** (erros do escopo atual
são corrigidos na hora, sem entrar aqui — ver `CLAUDE.md`).

## Como registrar

Cada issue é um bloco novo, nesta forma:

```
## ISSUE-<numero>
- SPEC: <nome-da-spec>
- TASK: <numero-da-task>
- DESCRIPTION: <descrição do erro encontrado, contexto e impacto>
```

- `<numero>` é sequencial, nunca reaproveitado.
- `SPEC`/`TASK` identificam onde o erro foi **encontrado** (não necessariamente
  onde ele deveria ser corrigido).
- Issues aqui ficam pendentes até serem resolvidas em uma task futura; ao
  resolver, marque com `RESOLVED (commit <hash>)` ao final do bloco em vez de
  apagar o registro.

---

## ISSUE-1
- SPEC: read-side
- TASK: I5.1
- DESCRIPTION: `emitQueryJoinCollectionVars` (`codegen/decl_query.go`) gera
  variáveis de pacote como `var ticketCollection = runtime.NewMemoryCollection[Ticket]()`
  no arquivo `queries.go`. Se o MESMO tipo (ex. `Ticket`) também for
  referenciado num `list`/`count` dentro de uma Policy do MESMO módulo,
  `emitPolicyCollectionVars` (`codegen/decl_policy.go`) gera a MESMA variável,
  com o MESMO nome (`policyCollectionVarName`/convenção `<tipo>Collection`),
  em `policies.go` — os dois arquivos compartilham o MESMO pacote Go, então o
  compilador falha com "redeclared in this block". Nenhum exemplo real hoje
  exercita essa combinação (nenhum módulo tem Query com join E Policy com
  list/count sobre o mesmo tipo), por isso não foi pego pelos testes
  existentes. Correção sugerida: centralizar a declaração dessas
  Collection[T] var num único arquivo por módulo (ex. `collections.go`),
  compartilhado entre `EmitQueries`/`EmitPolicies`, em vez de cada emissor
  declarar as suas independentemente.
- RESOLVED (commit `3a22df3`): `codegen/decl_collections.go` (novo) calcula
  a interseção entre os tipos usados por join de Query e por list/count de
  Policy do módulo; quando não vazia, `generateModuleFiles` emite um único
  `collections.go` com esses vars e repassa o mapa tipo->var a
  `EmitQueries`/`EmitPolicies`, que passam a reusar o var compartilhado em
  vez de declarar o seu. Vazio no caso comum (Go byte-idêntico a antes).
  Fixture de regressão em `codegen/decl_collections_test.go`.

---

## ISSUE-2
- SPEC: codegen
- TASK: gaps.md §G-3 (exclusões de `requirements.md` §1.3)
- DESCRIPTION: Features do spec v6 que o **front-end nunca modelou** — não são
  gaps só do codegen: parser/resolver/checker não as reconhecem, então fechar
  qualquer uma começa por um ciclo novo de front-end (parser → resolver → sema
  → types) e só depois codegen. São quatro:
  (a) **Exposição TCP/UDP** (spec §10/§14) — `interface.ds` só modela HTTP e
  GRPC.
  (b) **Receptor `tenant.*` em corpos** (`tenant.id`/`tenant.tier`/
  `tenant.exists`, spec §13.2) — a tenancy row_level funciona no runtime
  (filtro, cross_tenant, fail-closed 400), mas o domínio não consegue LER o
  tenant corrente de dentro de um Handle/UseCase.
  (c) **Built-in `provision tenant(id)`** (spec §13.4) — sem ela o fluxo de
  provisionamento de tenant do spec não é expressável.
  (d) **Acesso nativo `events()` em Aggregates** (spec §4.5).
  Impacto: cada uma é um ciclo de spec próprio (as mais caras do inventário,
  atravessam o pipeline inteiro); abrir só quando houver demanda real.

## ISSUE-3
- SPEC: codegen
- TASK: gaps.md §G-4 (Marcos F/G — providers reais de infraestrutura)
- DESCRIPTION: Tudo está atrás de seams limpos (NFR-12 respeitado), mas a única
  dependência externa real por categoria é sqlite — o sistema gerado hoje
  **não é implantável contra infraestrutura real** além disso. Categorias em
  aberto: Database (spec pede Postgres §12; só `"sqlite"` é adapter real,
  `"postgres"`/`"mongodb"` são rótulos decorativos — `codegen/sql_wiring.go`);
  Canais (`grpc`/`http`/`stream` §11 → erro de geração; provider `rabbitmq`
  não existe, só `direct`/`queue` in-memory — `codegen/channel_test.go`);
  Cache backend (`redis`/`layered` §15 → só in-memory); RateLimit backend
  (`redis` §16 → só in-memory); FileStorage (`"s3"` §12 → seam in-memory);
  Idempotency storage (`external` Redis/Dynamo §14 → só `same` in-memory,
  `codegen/rtsrc/idempotency.go.txt`); Outbox (durabilidade real §12 → in-
  memory). Fechar exige um provider real por vez, opt-in e isolado (padrão já
  existe: `codegen/sqlrt/`, `grpcrt/`, `otelrt/`). Postgres ou rabbitmq
  primeiro (validam os seams mais centrais). Nota: o seam `Dialect` (REQ-40,
  read-side/I7.0, `codegen/sqlrt/dialect.go.txt`) já reduz o custo da parte
  SQL — adicionar banco vira "implementar `Dialect` + entrada no registro"; o
  restante (driver real, migrations, type mapping) segue aberto.
- EM ANDAMENTO (spec criada): `.claude/specs/infra-providers/` (Marco J,
  REQ-41..48 / NFR-21..24) trata esta issue com **recorte de 5 providers** —
  Postgres, RabbitMQ, Redis (Cache+RateLimit), S3 e Outbox durável. As demais
  categorias de G-4 (outros bancos, gRPC-canal, Dynamo para idempotency
  `external`, backend `layered` de cache, GCS/Azure) ficam explicitamente fora
  do recorte, para ciclos futuros. Fecha PARCIALMENTE quando o Marco J fechar.
- FECHADA PARCIALMENTE (Marco J concluído, J7.1): as 5 categorias do recorte
  têm provider real — Postgres (J1, `codegen/pgrt` + `sql_wiring.go`),
  RabbitMQ (J3, `codegen/channel_rabbitmq.go`), Redis Cache+RateLimit (J4,
  `codegen/redisrt`), S3 FileStorage (J5, `codegen/s3rt`), Outbox durável
  (J2, `runtime.DurableOutbox`/`sql_wiring.go:emitOutboxDatabaseWiring`) —
  todos opt-in, isolados atrás do seam existente, cobertos por golden +
  smoke compile (NFR-17) e determinismo (NFR-21, `infra_providers_
  determinism_test.go`). Ver `.claude/specs/codegen/gaps.md` §G-4 para a
  tabela completa antes/depois por categoria. **Residual aberto** (não
  fechado por Marco J, ver ISSUE-9 e `gaps.md` "Residual aberto"): o lado
  PRODUTOR do Outbox→canal cross-service (REQ-42.6) segue publicando direto
  no commit em vez de enfileirar no outbox — só o lado consumidor (Policy
  local com Database real) ganhou `DurableOutbox` de verdade; e a
  vendorização/build offline real (R10) nunca foi implementada — os smoke
  tests usam `go mod tidy` (rede), não `-mod=vendor` genuíno. As categorias
  explicitamente fora do recorte (outros bancos, gRPC-canal, Dynamo,
  `layered` cache, GCS/Azure) continuam abertas para um ciclo futuro. Não
  reabrir esta issue para o residual — ele está registrado em ISSUE-9 e em
  `gaps.md`; um ciclo futuro pode referenciar os dois diretamente.

## ISSUE-4
- SPEC: codegen
- TASK: gaps.md §G-5 (Field-Level Security de View)
- DESCRIPTION: O bloco `visibility` de View (spec §6.2) é **parseado**
  (`ast.ViewDecl.Visibility`, `parser/parse_decl.go`) mas **nenhum arquivo do
  codegen consome `Visibility`** — a omissão condicional de campos na
  serialização não acontece. É a lacuna "silenciosa" mais arriscada do
  inventário (cheiro de segurança que falha em silêncio): o programa compila,
  o bloco é aceito e ignorado. O exemplo `docs/examples/pizzeria`
  (`sales/read.ds`, `OrderVW`) exercita e documenta essa limitação. Atenuantes:
  o spec marca a feature como "em evolução" (§25) e wallet/shop não a usam.
  Fechar exige decidir a semântica de serialização condicional por caller na
  borda HTTP/gRPC (o `runtime.Caller` já circula até lá) e emitir a filtragem
  no encode das Views. Paliativo imediato defensável: **warning de geração**
  ("visibility declarado e ignorado") para tirar o silêncio.

## ISSUE-5
- SPEC: codegen
- TASK: gaps.md §G-6 (Observabilidade OTel parcial, Marcos H2/H3)
- DESCRIPTION: Traces OTel reais e opt-in via `Telemetry` (H2) funcionam, mas o
  adapter **não exporta métricas nem logs OTel**: `Metric` vive num registry
  in-memory próprio (`rtsrc/metrics.go.txt`, H3) e logs são `slog` com trace
  id, não OTLP. Documentado no cabeçalho de `codegen/decl_telemetry.go`. O spec
  (§21/§1.1) promete "instrumentação OpenTelemetry automática" para os três
  sinais. Oportunista: fechar quando telemetria for tocada de novo.

## ISSUE-6
- SPEC: codegen
- TASK: gaps.md §G-7 (lacunas dos testes gerados, Marco H4)
- DESCRIPTION: `*.test.ds` → Go tests cobre o caminho feliz, mas várias formas
  do spec §22 têm semântica reduzida (cada uma registrada nas fatias de H4
  em `tasks.md`/`codegen/gentest.go`): `then state { ... }` (asserção de estado
  StateStored, §22.1) → erro de geração claro; cenário de acesso NEGADO (§22)
  → não expressável (a gramática não tem "como o caller X"); `mock ... returns
  X` desviando fluxo (§22.3) → mock sempre sucede, `X` é construído mas não
  influencia; `Subject emitted`/`released` de dentro de passo de Saga (§22.3)
  → erro de geração claro; contra-exemplo **mínimo**/shrinking em property
  (§22.5) → reporta a sequência completa sem encolher; `rolledback` com
  reversão real (§22.2) → é só `err != nil`, a UnitOfWork in-memory não tem
  staging. (O item §22.4 — agrupamento por `orderId` — JÁ foi fechado pelo
  ciclo read-side, REQ-39.1/I6.2, e não entra aqui.) Oportunista: fechar cada
  um quando o vizinho for tocado.
- EM ANDAMENTO (spec criada): `.claude/specs/correcoes-issues-6-7-8/`
  (Marco L, REQ-53 / §design 3). Análise de raiz categorizou os seis
  sub-itens por tratabilidade: cinco fecham em codegen/runtime — `then
  state` (§22.1, replay+compara campos), `emitted`/`released` em Saga
  (§22.3, reusa a coleta de §22.4), `mock returns X` (§22.3, X vira o
  retorno do stub), shrinking de property (§22.5, determinístico) e
  `rolledback` real (§22.2, dar **staging** à `memoryUnitOfWork`/
  `MemoryEventStore` em `rtsrc/`). O sexto — cenário de acesso NEGADO —
  exige NOVA GRAMÁTICA ("como o caller X"), fora do escopo de codegen
  (natureza de ISSUE-2), **delimitado** para um ciclo de front-end: ISSUE-6
  fecha só a fatia tratável e mantém esse resíduo apontado. Fecha
  (parcialmente) quando o Marco L fechar.

## ISSUE-7
- SPEC: codegen
- TASK: descoberto em `docs/examples/pizzeria` (não estava no `gaps.md`)
- DESCRIPTION: Um módulo que combina **`UseCase` E `Policy` no mesmo módulo**
  ainda não gera — `dsc gen` falha com "UseCase e Policy no mesmo módulo ainda
  não têm wiring combinado suportado (cada um gera seu próprio Wire —
  colidiriam); ver a doc de decl_policy.go". `generateModuleFiles`
  (`codegen/codegen.go`) emitiria dois `func Wire(...)` no mesmo pacote Go
  (um de `emitUOWWireFunc`, outro de `emitPolicyWireFunc`), que colidem. Nem
  wallet nem shop exercitavam essa combinação; o módulo `Kitchen` do exemplo
  pizzeria (Claim/Finish via HTTP **e** criação reativa via Policy sobre
  `OrderPaid`) é o primeiro caso real — o próprio comentário no código já
  previa "fica para quando um exemplo real precisar disso". Bloqueia a geração
  do back-end do exemplo pizzeria (o front-end valida limpo). Fechar exige
  unificar o wiring: um único `Wire(...)` por módulo que registre tanto os
  UseCases (dispatcher/UoW) quanto as Policies (assinaturas de evento).
- EM ANDAMENTO (spec criada): `.claude/specs/correcoes-issues-6-7-8/`
  (Marco L, REQ-52 / §design 2). Achado da análise de raiz: **o próprio
  código já resolve esta colisão em outros lugares** — `StartWorkers`,
  `WireQueryCache`, `WireOutboxStore`/`StartOutboxRelay` usam nome próprio
  em vez de um 2º `Wire`. Fix recomendado: um `Wire` unificado por módulo
  (`func Wire(u UnitOfWork, d Dispatcher)` no caso misto; casos puros
  byte-idênticos). O `Kitchen` do pizzeria é a fixture-âncora; ao fechar,
  `pizzeria` sai da lista `KNOWN_UNGENERATABLE` do CI
  (`.github/workflows/ci.yml`) e passa a gerar+compilar como wallet/shop.
  Fecha quando o Marco L fechar (a task L1.3 registra como issue nova
  qualquer bloqueio ADICIONAL do pizzeria fora da colisão de Wire).

## ISSUE-8
- SPEC: codegen
- TASK: gaps.md §G-baixo (§25 — em evolução no próprio spec)
- DESCRIPTION: Divergências menores, a maioria marcada como planejada/"em
  evolução" pelo próprio spec (§25) — registradas para rastreio, sem ação
  urgente: (a) **Redação GDPR** (§4.4) — placeholder tipado implementado
  (E4.3), mas o *gatilho* de redação não (spec o marca como em evolução);
  (b) **Cobertura semântica** (§22.7) — o warning "Handle sem cenário de erro
  testado" existe (`sema/rules_warnings.go:checkHandleErrorCoverage`,
  REQ-5.22), mas o relatório fino "por ramo de regra de negócio" fica na
  granularidade por Handle; (c) **itens §25** (avg/min/max/group by, aritmética
  estendida, marshalling FFI detalhado) — declarados planejado/a definir pelo
  spec, sem ação pendente deste lado.
- EM ANDAMENTO (spec criada): `.claude/specs/correcoes-issues-6-7-8/`
  (Marco L, REQ-54 / §design 4). Decisão por item: (b) cobertura §22.7 —
  a task L3.1 começa pela análise de raiz de `checkHandleErrorCoverage`; se
  o checker consegue cruzar os ramos `ensure ... else Error` com os cenários
  de erro testados, refina o warning para o ramo específico (fecha em
  `sema`); senão, mantém por-Handle e reclassifica como ciclo de sema
  dedicado, com o motivo. (a) redação GDPR (§4.4) e (c) §25 (agregações/
  aritmética/FFI) — **reclassificados** de "dívida de codegen" para
  "aguardando definição no spec da linguagem" (exigem sintaxe nova não
  definida; não há ação de codegen pendente). Fecha (b) e reclassifica
  (a)/(c) quando o Marco L fechar.

## ISSUE-9
- SPEC: infra-providers
- TASK: J2.5
- DESCRIPTION: REQ-42.6/§design infra-providers 3.2a exige que "publicar
  direto no commit (fora da tx) é PROIBIDO para destino cross-service" —
  item reclassificado de J2.4 (mecanismo do relay) para J2.5 (decisão de
  wiring). J2.5 fechou o lado CONSUMIDOR (uma Policy AtLeastOnce local com
  Database real ganha DurableOutbox de verdade em vez de memoryOutbox — ver
  `codegen/decl_policy.go:emitPolicyWireFunc`/`codegen/sql_wiring.go:
  emitOutboxDatabaseWiring`). O lado PRODUTOR não foi tocado:
  `generateCmdMainFile` (codegen.go) ainda constrói
  `runtime.NewUnitOfWork(store, <canal>)` quando um módulo produz
  PublicEvent para um canal "queue" (`producerChannel != nil`) — publish
  direto no commit, exatamente o padrão que REQ-42.6 proíbe para um destino
  cross-service. O exemplo real shop/Orders EXERCITA esse caminho hoje
  (`Database MainDb { provider: "postgres" }` + `UseCase PlaceOrder` +
  canal `Orders -> Shipping` via queue em topology.ds) — mas trocar esse
  wiring por um outbox durável exigiria primeiro que o código gerado de
  UseCase/Handle chamasse `tx.EnqueueOutbox` (nenhum emissor chama isso
  hoje — só os testes de J2.1-J2.4/J2.5 o exercitam manualmente), uma peça
  que a doc de design (`design.md`, seção sobre a fixture-âncora) já
  reconhece como só fechando de verdade em **J6** ("a durabilidade
  cross-service do outbox só se prova end-to-end com o transporte real de
  J3 presente... por isso a fixture-âncora (J6) combina outbox durável +
  rabbitmq"). Não bloqueou J2.5 (nenhum critério de aceite da task pede essa
  mudança no lado produtor — só `NewDurableOutbox(...)`/`Start(ctx)` do lado
  Policy) nem quebra NFR-21 (shop continua byte-idêntico, confirmado por
  `driver.TestGenerateShopE2E*`/golden tests). Fica para J3.4 (RabbitMQ, R2)
  ou J6 fechar de verdade.
- STATUS FINAL (J7.1, revisão de DoD): **nem J3.4 nem J6 fecharam isto** —
  confirmado por inspeção de código durante a revisão de DoD de J7.1.
  `emitDurableOutboxConstruction` (`codegen/decl_policy.go`, ~linha 692)
  ainda chama `NewDurableOutbox(outboxStore, map[...]{...})` sem 3º
  argumento (`publisher`) em NENHUM caminho — nem mesmo na fixture-âncora
  de J6.1 (`AnchorNotify`), que prova só o lado consumidor local (Policy +
  Database própria, sem canal). `generateCmdMainFile` continua publicando
  direto no commit (`runtime.NewUnitOfWork(store, <canal>)`) para todo
  módulo produtor de canal — nenhum emissor de UseCase/Handle chama
  `tx.EnqueueOutbox`. O runtime seam suporta e testa isso isoladamente
  (`codegen/sql_outbox_channel_test.go`), mas o codegen nunca conecta os
  dois lados. Registrado também em `.claude/specs/codegen/gaps.md` §G-4
  ("Residual aberto") e em ISSUE-3. Continua ABERTA — não fechar sem
  implementar o wiring produtor→outbox→relay de verdade.
- EM ANDAMENTO (spec criada): `.claude/specs/correcoes-issues-9-10-11/`
  (Marco K, REQ-51 / §design 4) trata esta issue na raiz — a análise de
  raiz confirma que fechar exige a pré-condição do UnitOfWork `database/sql`
  de banco único para o produtor (hoje um Database único degenera para a
  store in-memory, onde `Tx.EnqueueOutbox` é no-op), depois enfileirar o
  `PublicEvent` cross-service na tx (`tx.EnqueueOutbox`), trocar o publisher
  da UoW pelo canal-como-publisher do `DurableOutbox`, e subir o relay.
  **Condição de ativação (validada na revisão da PR #37):** Database real E
  canal com provider REAL (`rabbitmq`) — não a `QueueChannel` in-memory. Por
  isso o `shop/Orders` (postgres + canal `via: queue` SEM `provider:`) NÃO
  ativa e fica byte-idêntico (correção: o registro original desta issue
  sugeria que o shop mudaria). O exerciser é a âncora de J6 (`AnchorOrders`
  = postgres + rabbitmq) + uma fixture dedicada. Rota do enqueue resolvida:
  na construção da UoW (o corpo gerado do UseCase/Handle não muda). Fecha
  quando o Marco K fechar.
- RESOLVED (commits `1137ba9`/`e2f3ec9`/`9fd30f0`/`c580e1f`, K3.1-K3.4): a
  raiz analisada acima está implementada de ponta a ponta. `durableProducer`
  (K3.1, `codegen/sql_wiring.go`) detecta a condição de ativação (Database
  real + canal `provider:"rabbitmq"`, sem 2PC/Dispatcher combinado — guarda
  F5/G3 pré-existente); `emitSingleDatabaseWiring` (K3.2) abre a conexão real
  em vez de degenerar para a store in-memory; a UoW do produtor passa a ser
  `sqlruntime.NewOutboxUnitOfWork` (K3.3, construtor DISTINTO de
  `NewUnitOfWork` — mantém todo caller existente byte-idêntico), que
  enfileira o `PublicEvent` cross-service no outbox ANTES do `Commit`, na
  MESMA `*sql.Tx` do `Append` (REQ-51.1/51.4), e NÃO publica mais nada
  pós-commit; `generateCmdMainFile` monta `runtime.NewDurableOutbox(store,
  registry, <canal>)` com o canal como `publisher` (inline em `main.go`/
  `run()` — o produtor é UseCase-only, o canal só existe nesse escopo) e sobe
  o relay/cleanup (K3.3, REQ-51.2/51.3). Provado por fixtures dedicadas +
  smoke compile (K3.2/K3.3, incl. a âncora de J6 `AnchorOrders` e a fixture
  sintética `Alpha`/`Beta`) e por um teste comportamental fim-a-fim de crash
  simulado sobre o CAMINHO GERADO do produtor — não só o seam manual de
  `sql_outbox_channel_test.go` (K3.4, `codegen/producer_outbox_test.go`,
  REQ-51.7): `Publish` falha na 1ª tentativa, a linha fica undelivered
  (`attempts++`), o `Tick` seguinte re-publica — nenhum evento cross-service
  perdido. `wallet`/`shop` confirmados byte-idênticos em toda task (nenhum
  dos dois satisfaz a condição de ativação). Ver
  `.claude/specs/codegen/gaps.md` §G-4 (item removido da lista de residuais).

## ISSUE-10
- SPEC: infra-providers
- TASK: J4.1 (achado durante a revisão da PR #26)
- DESCRIPTION: `memoryQueryCache.Coalesce` (`codegen/rtsrc/querycache.go.txt`,
  Marco G3) tem o MESMO bug que a revisão do Gemini Code Assist apontou em
  `redisQueryCache.Coalesce` (`codegen/redisrt/cache.go.txt`, task J4.1,
  copiado do padrão de `querycache.go.txt`): se `fn()` panica, `close(
  fl.done)` e a remoção de `key` de `c.flights` nunca rodam (não há
  `defer`) — toda goroutine concorrente bloqueada em `<-fl.done` trava para
  sempre (vazamento de goroutine), e a MESMA chave nunca mais coalesce de
  novo (fica presa em `c.flights` indefinidamente). `redisQueryCache` já foi
  corrigido (commit da revisão da PR #26, `defer` fechando/limpando mesmo
  sob panic) — `memoryQueryCache`, o backend em produção desde G3 (Marco
  E/F, todo módulo com Query cacheada usa isto hoje, incl. potencialmente
  wallet/shop se algum dia declararem `cache {}`), continua com o bug
  original. Fora do escopo de J4.1 (é `rtsrc/`, núcleo, não o adapter redis
  que a task realmente toca) — registrado aqui em vez de corrigido
  silenciosamente numa PR que não é sobre isso. Fix sugerido: mesmo padrão
  de `defer` que `redisQueryCache.Coalesce` já usa. Baixo risco prático (um
  handler de Query gerado só panica sobre um bug de geração ou um builtin
  malformado — não exercitado por nenhum teste comportamental hoje), mas
  vale uma task pequena e dedicada (fora de Marco J, é `rtsrc/` puro) para
  fechar.
- EM ANDAMENTO (spec criada): `.claude/specs/correcoes-issues-9-10-11/`
  (Marco K, REQ-50 / §design 3, tasks K2.1/K2.2). A revisão da PR #37
  (Gemini Code Assist + validação contra o wrapper gerado em
  `decl_query_cache.go:491-504`) mostrou que o fix é MAIOR que "espelhar o
  `defer` do Redis": num pânico do líder, o esperador recebe `(nil, nil)` e
  cai em `result.(T)` → um SEGUNDO pânico. E o `redisQueryCache` tem o MESMO
  defeito (o fix da PR #26 fechou só o vazamento). Fix de raiz: flag
  `completed` + erro-sentinela a `fl.err` no `defer` (sem `recover`),
  aplicado aos DOIS backends (K2.1 memory, K2.2 redis). Fecha quando o
  Marco K fechar.
- RESOLVED (commits `9d5fe16`/`bc6df20`, K2.1/K2.2): fix de raiz aplicado
  aos DOIS backends, exatamente como analisado acima. `memoryQueryCache.
  Coalesce`/`redisQueryCache.Coalesce` (`codegen/rtsrc/querycache.go.txt`/
  `codegen/redisrt/cache.go.txt`) ganharam um sentinela de pacote
  (`errCoalescedPanic = errors.New("coalesced function panicked")`) e uma
  flag `completed`: o `defer` (instalado ANTES de `fn()` rodar) sempre
  remove a chave de `c.flights` e fecha `fl.done`; se `completed` continua
  `false` quando o `defer` roda (o líder panicou, `fn()` nunca retornou),
  `fl.err` é forçado ao sentinela ANTES do `close` — nenhum esperador
  recebe `(nil, nil)` nunca mais. Sem `recover()`: o pânico do líder segue
  propagando normalmente. Par de testes por backend (NFR-4): pânico do
  líder libera o esperador com erro não-nil sob timeout e a MESMA chave
  coalesce de novo depois (negativo); N goroutines concorrentes recebem o
  mesmo resultado com `fn` rodando uma única vez, e um erro de negócio
  legítimo propaga como está, nunca o sentinela (positivo/não-regressão).
- SPEC: infra-providers
- TASK: J6.1 (fixture-âncora multi-service)
- DESCRIPTION: o **parser** (`parser/parse_stmt.go`) falha em analisar DUAS
  atribuições simples consecutivas dentro do mesmo bloco de statements — ex.:
  ```
  order = load Bar(id)
  x = id
  ```
  A SEGUNDA atribuição ("x = id") produz um erro de SINTAXE ("esperava uma
  expressão, encontrei =") no "=" da segunda linha, mesmo sendo gramática
  válida (duas AssignStmt seguidas) — reproduzido isolado, sem relação com
  `load File`/FileStorage (testado trocando o RHS da 2ª atribuição por um
  literal simples, mesmo erro). Uma "ensure ... exists else ..." (ou
  qualquer outro tipo de statement) ENTRE as duas atribuições evita o bug —
  sugere que o parser de statements trata "AssignStmt seguido de AssignStmt"
  como uma continuação de expressão em vez de dois statements
  independentes, possivelmente relacionado a como `synchronize`/`expect`
  decide onde um statement termina. Fora do escopo de J6.1 (é
  `parser/`, front-end — a spec infra-providers não toca lá) — contornado
  na fixture-âncora reescrevendo para "return load File(...)" direto (sem a
  2ª atribuição intermediária), sem mudar a cobertura pretendida da task.
  Vale uma task pequena e dedicada no `parser/` (fora de Marco J) para
  isolar a causa raiz e cobrir com um teste positivo/negativo (NFR-4).
- EM ANDAMENTO (spec criada): `.claude/specs/correcoes-issues-9-10-11/`
  (Marco K, REQ-49 / §design 2, task K1.1). Causa-raiz ISOLADA e ela **não**
  é `synchronize`/`expect` como este registro especulava: é o **binding
  opcional de `parseQueryOp`** (`parser/parse_query.go`) — depois de
  `load Bar(id)`, o `x` da linha seguinte é consumido como binding da query
  (a forma de `list Ticket t`) porque a guarda `p.at(token.IDENT) &&
  !isClauseKw(...)` não olha a quebra de linha; o `=` órfão vira o erro. Uma
  statement intermediária evita o bug porque o token após `load Bar(id)`
  deixa de ser um IDENT. Fix: guardar o binding (e o alias de `join`, mesmo
  padrão latente) por `sameLineAsPrev()`. Fecha quando o Marco K fechar.
- RESOLVED (commits `3a7437e`/`2abce08`, K1.1/K1.2): fix de raiz aplicado
  nos DOIS pontos que compartilhavam a mesma heurística gananciosa de
  identificador opcional. Novo helper `sameLineAsPrev()`
  (`parser/parser.go`) compara `p.cur().Pos.Line` com `p.lastPos.Line` (o
  fim do último token consumido); a guarda do **binding** opcional em
  `parseQueryOp` (K1.1) e a guarda do **alias** opcional de `join` em
  `parseOneClause` (K1.2, `parser/parse_query.go`) ganharam `&&
  p.sameLineAsPrev()`. Par de testes por ponto (NFR-4): duas atribuições
  consecutivas (`x = id`/`x = 1`) não roubam mais o identificador da 2ª
  como binding/alias, zero diagnóstico (negativo/regressão); um binding
  (`list Ticket t where ...`) ou alias (`join Order o`) legítimo na MESMA
  linha do alvo continua intacto (positivo). Suíte inteira do `parser/`
  verde em ambas as tasks — nenhum fixture de binding/alias existente
  regrediu.

## ISSUE-12
- SPEC: correcoes-issues-6-7-8
- TASK: L1.2 (achado ao provar `docs/examples/pizzeria` fim-a-fim, REQ-52.4/
  52.7)
- DESCRIPTION: L1.2 pedia para confirmar que o `Kitchen` do `pizzeria` não
  esbarra na guarda F5/G3 pré-existente (`codegen/codegen.go:1143`:
  `"codegen: cmd/%s/main.go: módulo com Policy/Query cacheada E módulo
  produtor de canal de saída no mesmo service ainda não têm wiring combinado
  suportado (F5/G3)"`, disparada quando `producerChannel != nil &&
  needsDispatcher`). **A leitura confirma que ele ESBARRA, sim** — ao
  contrário do que a task text presumia ("é UseCase+Policy local, sem canal
  próprio"):
  - `docs/examples/pizzeria/kitchen/domain.ds`: `Handle Finish` faz `emit
    TicketFinished(self.orderRef)`, e `TicketFinished` é um `PublicEvent`.
  - `docs/examples/pizzeria/topology.ds`: o canal `Kitchen -> Sales` (`via:
    queue`, `provider: "rabbitmq"`, `orderBy: orderRef`) existe DENTRO do
    MESMO service `PizzeriaMonolith { modules: [Sales, Kitchen] }` que
    `Sales -> Kitchen`. Logo `producerChannelFor(prog, "Kitchen")` resolve
    esse canal: Kitchen **é** produtor de canal de saída.
  - `docs/examples/pizzeria/kitchen/policy.ds`: `Policy
    CreateTicketOnOrderPaid on OrderPaid` é uma Policy LOCAL do módulo
    Kitchen, que força `needsDispatcher = true` para o grupo
    `PizzeriaMonolith` (`codegen.go:1089`).
  - `producerChannel != nil && needsDispatcher` é exatamente essa combinação
    → a guarda F5/G3 dispara para o `pizzeria`.
  - Confirmado por leitura estática (ver acima) e por reprodução empírica
    parcial (abaixo) — não foi possível chegar ao PONTO exato do erro F5/G3
    rodando o `pizzeria` real porque **outros bloqueios independentes, mais
    cedo no pipeline, impedem a geração de chegar a `generateCmdMainFile`**
    (onde a guarda mora). Rodando `dsc gen docs/examples/pizzeria` hoje
    (pós-L1.1) o erro real é:
    ```
    dsc: codegen: módulo Kitchen: aggregate_kitchen_ticket.go: codegen:
    Aggregate KitchenTicket: Handle Create: access: codegen: CallExpr com Fn
    *ast.MemberExpr não suportado em Lowerer.Expr (só construção de tipo via
    identificador nu; chamada de método/built-in é E5.3/E6+)
    ```
    Investigando numa cópia de trabalho isolada (nunca commitada, só para
    diagnóstico — o fixture real do `pizzeria` NÃO foi alterado), contornando
    esse erro e os seguintes um a um, aparecem em sequência MAIS QUATRO
    bloqueios independentes, cada um ortogonal a REQ-52/F5-G3:
    1. `access { Create requires caller.hasRole("system_sales") }`
       (`kitchen/domain.ds:90-93`): `lowerAccessCondition`
       (`codegen/decl_aggregate.go:341`) só trata condições que são um
       `BinaryExpr` (`&&`/`||`/`==`/`!=` com "caller.X == VOWrapper") ou caem
       no fallback genérico `l.Expr(cond)` — uma condição que é SÓ
       `caller.hasRole(...)` (um `CallExpr` puro, sem `&&`/`||`/`==`) não é
       nenhuma das formas tratadas por `lowerCallerVOEquality` e cai no
       `Lowerer.call` genérico, que rejeita qualquer `CallExpr` cujo `Fn` não
       seja "construção de tipo via identificador nu" — daí o erro acima.
       `wallet`/`shop` nunca exercitam essa forma (só usam `caller.
       authenticated`), então nunca foi pega.
    2. `Apply TicketCreated { state.createdAt = CreatedAt(now()) }`
       (`kitchen/domain.ds:104`, também `:125`): `emitApply`
       (`codegen/decl_aggregate.go:274`) constrói o `Lowerer` com
       `lower.NewLowerer(env, reg, runtimeAlias)` e NUNCA chama
       `.WithBuiltins(...)` (ao contrário de `emitUseCasesBytes`/
       `emitPolicyExecute`/Saga/Query, que sempre anexam um
       `BuiltinLowerer`) — qualquer builtin de função (`now()`/`uuid()`/
       `random(...)`) usado dentro de um corpo de `Apply` falha com "CallExpr
       sobre \"now\" não é construção de VO/Event/Command conhecida".
       `wallet`/`shop` nunca chamam nenhum builtin de função dentro de um
       `Apply`, então nunca foi pego.
    3. `Apply TicketItemAdded { state.items.add(event.item) }`
       (`kitchen/domain.ds:112`): `.add(...)` só está mapeado
       (`codegen/goname/types.go:111`, `BuiltinMethod{Receiver:
       "AppendList", Method: "add"}`) para um campo `AppendList<T>`; Kitchen
       declara `items List<TicketItem>` (List comum), não `AppendList` — ao
       que tudo indica um TYPO/bug do PRÓPRIO fixture `pizzeria` (deveria
       ser `AppendList<TicketItem>`, o padrão que
       `wallet/domain.ds:88` — `entries AppendList<StatementEntry>` — já
       usa), não necessariamente um gap de codegen. Sinalizado aqui porque
       bloqueia a geração de qualquer forma, mas provavelmente se resolve
       ajustando o `.ds`, não o back-end.
    4. `Query GetBoardTickets() -> List<KitchenTicketVW> { return list
       KitchenTicket t where t.status in [...] orderBy t.createdAt ascending
       as KitchenTicketVW }` (`kitchen/read.ds:15-20`): mesmo reduzindo para
       a forma mínima "`return list KitchenTicket where ... as
       KitchenTicketVW`" (idêntica em espírito à
       `sales/read.ds:20` que FUNCIONA sobre o Aggregate `MenuItem`), a
       geração falha com "list ... em posição de expressão pura não é
       suportado por Lowerer.Expr". A diferença de fato entre os dois é que
       `Sales.MainDb` usa `provider: "postgres"` (REAL, Marco J) e
       `Kitchen.MainDb` usa `provider: "mongodb"` (DECORATIVO,
       `kitchen/mod.ds:9-13`) — sem um provider real, o Read Side de Kitchen
       cai no seam in-memory (`runtime.Query[T]`), cujo suporte de "list
       <VO/Aggregate>" (E8.1, `codegen/decl_query.go` cabeçalho) exige
       correlacionar o VO a um campo `AppendList<VO>` de um ÚNICO Aggregate
       conhecido — listar o PRÓPRIO Aggregate (`KitchenTicket`) diretamente,
       sem provider real por trás, não é uma forma coberta.
  - **Conclusão:** o `pizzeria` está bloqueado por PELO MENOS CINCO defeitos
    independentes (F5/G3 + os quatro acima), nenhum deles dentro do escopo de
    REQ-52 (Wire unificado) — REQ-52.7 pede exatamente isto: registrar como
    issue nova em vez de ampliar o escopo da task. **L1.2 fecha normalmente**
    (seu próprio escopo — o call site de `main.go` + esta confirmação — está
    completo); **L1.3 fica BLOQUEADA** até esta issue (ou uma investigação
    dedicada) resolver os cinco pontos, e provavelmente precisa de um recorte
    NOVO (talvez até uma fixture-alvo diferente de `pizzeria`, ou correções
    em `pizzeria` + em pelo menos dois pacotes de codegen distintos:
    `decl_aggregate.go`/`lower/` para os itens 1/2, `decl_query.go`/E8.1 para
    o item 4, e a guarda F5/G3 em si). Este registro é INDEPENDENTE de
    ISSUE-7/REQ-52 (que L1.1 já fechou — a colisão de `Wire`); não fechar
    ISSUE-7 como totalmente resolvida enquanto ISSUE-12 (o bloqueio real e
    maior de `pizzeria`) permanecer aberta — ver `.claude/specs/
    correcoes-issues-6-7-8/`.
