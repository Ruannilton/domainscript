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
  - **CORREÇÃO (achado durante a tentativa de L1.3d — a premissa acima do
    item 4 estava ERRADA):** a task L1.3d tentou a rota (b) — dar a Kitchen
    um provider real (`"sqlite"`) — presumindo que o provider fosse a única
    diferença entre a Query de Kitchen (falha) e a de Sales (funciona).
    **Não é.** Trocar o provider de Kitchen NÃO mudou o erro (confirmado
    empiricamente, inclusive numa fixture mínima isolada com `"sqlite"` E
    `"postgres"`). A causa raiz de verdade, lida em `codegen/decl_query.go`
    (`tryEmitListVO`, ~linha 461): `if _, ok :=
    qc.env.TypeOfName(voName).(*types.VOType); !ok { return false, nil }` —
    a função SÓ reconhece `list <nome>` quando `<nome>` resolve a um
    `*types.VOType` correlacionado via campo `AppendList<VO>` de um
    Aggregate conhecido (o padrão de `wallet`, `list StatementEntry`). Um
    NOME DE AGGREGATE (`KitchenTicket`, `MenuItem`) resolve a
    `*types.ShapeType`, não `*types.VOType` — a checagem falha SEMPRE,
    **independente de provider**. É um gap de codegen genuíno e
    provider-agnóstico (a forma "`list <Aggregate> ... as View`" nunca foi
    implementada), não um problema do `pizzeria`. **Achado ainda mais
    importante:** `sales/read.ds`'s `GetAvailableMenu`/`GetActiveOrders` têm
    a MESMA forma (`list MenuItem`/`list Order`, o próprio Aggregate, sem
    correlação via `AppendList`) e NUNCA foram de fato exercitadas — a
    geração do `pizzeria` sempre falha em Kitchen primeiro (ordem
    alfabética de módulos), então ninguém tinha confirmado que a Query de
    Sales realmente funciona. É plausível que ela tropece no MESMO erro se
    a geração algum dia chegar lá. **Decisão do usuário (não perseguir agora):**
    em vez de (a) estender `tryEmitListVO`/`EmitQuery` para suportar `list
    <Aggregate>` de verdade (a rota antes rejeitada como grande demais, mas
    que parece inevitável), ou (b') reescrever as Queries de Kitchen/Sales
    para a forma já suportada, o usuário optou por **parar aqui, registrar
    e seguir para as Fases L2/L3** (independentes de L1) em vez de perseguir
    o fechamento completo de `pizzeria` agora. L1.3d/L1.3e/L1.3f ficam
    **BLOQUEADAS**, sem tentativa adicional neste ciclo — ver
    `.claude/state.md` para o registro completo da decisão.
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

## ISSUE-13
- SPEC: (nenhuma — achado ao atualizar `docs/examples/wallet` para infra real)
- TASK: atualização do exemplo wallet (Postgres + Redis)
- DESCRIPTION: um `UseCase` com `idempotency { required: true, ... }` (§14)
  fica **intestável pelo formato nativo `*.test.ds`**: a gramática de cenário
  não tem como fornecer uma `Idempotency-Key`, então todo cenário que exercita
  esse UseCase falha com "an Idempotency-Key is required for this operation"
  em vez do comportamento de negócio que queria provar. Reproduzido ao pôr
  `required: true` nos dois UseCases do wallet: 3 cenários gerados quebraram
  (`TestPerformDeposit_DepositoBemSucedidoCommita`,
  `TestPerformDeposit_CarteiraNuncaCriadaFalhaENaoCommita`,
  `TestE2ESeedGivenEventThenPerformDepositWiresThroughRealStore`).
  O RUNTIME já sabe receber a chave — `runtime.WithIdempotencyKey(ctx, ...)`
  existe e é testada (`rtsrc/runtime_test.go.txt:TestWithIdempotencyKeyFrom`);
  quem não sabe EMITI-LA é o gerador de testes (`codegen/gentest.go`), que
  monta o ctx do cenário sem nenhuma chave. É a MESMA natureza do gap de
  "cenário de acesso NEGADO" (L2.6/REQ-53.7) e do `given caller` inexistente:
  o cenário não consegue descrever o *contexto de chamada*, só o estado e a
  ação. Duas rotas para fechar: (a) gramática nova no `.test.ds` (ex. `given
  idempotencyKey "k1"`), que é ciclo de front-end como ISSUE-2; ou (b) o
  gerador injetar uma chave sintética determinística por cenário quando o
  UseCase exigir uma — mais barato, e suficiente para o cenário provar o
  comportamento de negócio (a idempotência em si continuaria coberta pelos
  testes de runtime). Contornado no wallet declarando `idempotency { window:
  48h }` sem `required` — que também é o que APIs de pagamento reais fazem
  (o Stripe recomenda o header, não o exige), então o exemplo não perde
  realismo.
