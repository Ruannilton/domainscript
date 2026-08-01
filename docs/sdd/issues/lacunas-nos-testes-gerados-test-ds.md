# Lacunas nos testes gerados a partir de `*.test.ds` (ex-ISSUE-6)
- SPEC: [codegen](../specs/codegen/requirements.md)
- TASK: [gaps.md §G-7](../specs/codegen/gaps.md) (lacunas dos testes gerados, Marco H4)
- DESCRIPTION: `*.test.ds` → Go tests cobre o caminho feliz, mas várias formas
  do spec §22 têm semântica reduzida (cada uma registrada nas fatias de H4
  em [tasks.md](../specs/codegen/tasks.md)/[gentest.go](../../../codegen/gentest.go)): `then state { ... }` (asserção de estado
  StateStored, §22.1) → erro de geração claro; cenário de acesso NEGADO (§22)
  → não expressável (a gramática não tem "como o caller X"); `mock ... returns
  X` desviando fluxo (§22.3) → mock sempre sucede, `X` é construído mas não
  influencia; `Subject emitted`/`released` de dentro de passo de Saga (§22.3)
  → erro de geração claro; contra-exemplo **mínimo**/shrinking em property
  (§22.5) → reporta a sequência completa sem encolher; `rolledback` com
  reversão real (§22.2) → é só `err != nil`, a UnitOfWork in-memory não tem
  staging. (O item §22.4 — agrupamento por `orderId` — já foi fechado pelo
  ciclo read-side, REQ-39.1/I6.2, e não entra aqui.) Oportunista: fechar cada
  um quando o vizinho for tocado.

  EM ANDAMENTO (spec encerrada e sucedida): a spec `correcoes-issues-6-7-8`
  (Marco L, REQ-53 / §design 3) que abriu esta frente foi fechada — o que ficou
  pendente foi retomado por
  [`correcoes-issues-6-8-12`](../specs/correcoes-issues-6-8-12/requirements.md)
  (Marco M). Análise de raiz categorizou os seis
  sub-itens por tratabilidade: cinco fecham em codegen/runtime — `then
  state` (§22.1, replay+compara campos), `emitted`/`released` em Saga
  (§22.3, reusa a coleta de §22.4), `mock returns X` (§22.3, X vira o
  retorno do stub), shrinking de property (§22.5, determinístico) e
  `rolledback` real (§22.2, dar **staging** à `memoryUnitOfWork`/
  `MemoryEventStore` em `rtsrc/`). O sexto — cenário de acesso NEGADO —
  exige NOVA GRAMÁTICA ("como o caller X"), fora do escopo de codegen
  (natureza da issue de features não modeladas pelo front-end), **delimitado**
  para um ciclo de front-end: fecha só a fatia tratável e mantém esse resíduo
  apontado.

  Status conforme [state.md](../state.md): Marco L ainda **in-progress**
  (L1.1/L1.2/L1.3a/L1.3b/L1.3c/L2.1 done; L1.3d pausada por decisão do
  usuário; L1.3e/L1.3f bloqueadas em cascata — ver [a issue sobre o pizzeria
  bloqueado por múltiplos defeitos de
  codegen](pizzeria-bloqueado-por-multiplos-defeitos-de-codegen.md); próxima
  task L2.5 —
  `rolledback` com reversão real). Fecha (parcialmente) quando o Marco L
  fechar.
- SOLVED: FALSE

# Solução proposta

> Análise de 2026-07-31, contra o código de hoje (`a6b239b`) e a spec v7 já
> revisada (`d394745`). Issue irmã, mesma família de causa e proposta própria:
> [UseCase com `idempotency { required: true }` é intestável](usecase-idempotency-required-intestavel-test-ds.md).

## Veredito

**A issue continua real, mas o inventário está desatualizado nos dois sentidos.**
Um item fechou; dois deixaram de ser "sem semântica definida no spec" (a revisão
de [§9.4](../steerings/domainscript-spec-v7/09-notifications-adapters.md) e
[§5.3](../steerings/domainscript-spec-v7/05-application-layer.md) os definiu); e
a revisão da tabela [§24.7](../steerings/domainscript-spec-v7/24-testing.md)
acrescentou **três obrigações novas** que hoje ninguém implementa — nem em
`sema`, nem no gerador. Item a item:

**1. `then state { ... }` (§24.1) — ✅ FECHADO.** `emitAggregateThenState`
([gentest.go:438](../../../codegen/gentest.go)) compara campo a campo após o
replay, com `emitStateOverlay` (`gentest.go:553`) do lado do `given`; golden em
`codegen/testdata/tests_thenstate_counter.go.golden` e o par NFR-4 em
`codegen/gentest_thenstate_test.go:193` (`RunGreen`) / `:202`
(`RunRedOnDivergence`). Sai do inventário — foi L2.1 do Marco L.

**2. Cenário de acesso NEGADO (§24) — ABERTO, inalterado.** Toda chamada gerada
ainda fixa um caller autenticado: `gentest.go:405`
(`runtime.NewTestCaller(string(w.state.Id))`, Aggregate), `gentest.go:915`
(`runtime.WithCaller(context.Background(), runtime.NewTestCaller("test-caller"))`,
UseCase) e `gentest_property.go:319`. A revisão de §24 **não** acrescentou
gramática de caller — a tabela §24.7 continua sem qualquer linha sobre acesso.
Segue como resíduo de front-end, e ganhou uma irmã exata: a issue de
idempotência acima é o **mesmo** buraco (o cenário descreve estado e ação, nunca
o contexto de chamada).

**3. `mock ... returns X` desviando fluxo (§24.3) — ABERTO, mas a premissa que o
delimitou CAIU.** O código não mudou: `emitSagaMock` ainda constrói `X` e emite
`_ = <expr>` (`gentest.go:1344-1354`), e `result = call Adapter(...)` ainda é
erro explícito (`codegen/lower/builtins.go:340-341`). O que mudou é a spec: §9.4
passou a definir o contrato de resposta (`Notification X { … } -> T`, bloco
`response { }` no Adapter Nível 1, `function "F" -> T throws E` no Nível 2),
§9.4.5 declara `PaymentResult`/`PaymentStatus` normativamente, §9.4.6 liga
`mock X returns V` ao `-> T` com semântica de **shape parcial**, e §24.7 ganhou
"Mock com retorno de tipo errado, **ou em Notification sem contrato de
resposta**" e "Cenário atinge `call X` sem `mock X returns …` → ❌ Falha em
execução". Ou seja: M3.1 delimitou ([state.md](../specs/correcoes-issues-6-8-12/state.md),
CANCELLED TASKS) sobre a premissa "nenhuma seção do spec define tipo de resposta
para Adapter/Notification" — **falsa desde 2026-07-31**. O item deixa de ser
"aguardando decisão de linguagem" e vira "aguardando ciclo de front-end", com
texto normativo pronto.

**4. `Subject emitted` / `released` de dentro de passo de Saga (§24.3) — CINDIDO
em dois, com estados diferentes.**
 - A miscompilação silenciosa fechou: `checkNoEmitInSagaStepBlock`
   ([decl_saga.go:210-233](../../../codegen/decl_saga.go)) hoje dá erro claro
   para `emit` em `up`/`down`, e `emitSagaThenAssert` rejeita `emitted` no
   `default` (`gentest.go:1444`). Continua sem semântica **executável**.
 - A semântica que faltava agora existe: §5.3.7 fixa que a asserção de um
   `ApplicationEvent` é `emitted X(...)` **sem instância**, que a forma
   qualificada sobre ApplicationEvent é ❌ erro, e o próprio exemplo de §5.3.8
   mostra um `down` de step fazendo `order = load Order(state.orderId);
   order.Cancel(...)` seguido de `emit PurchaseAbandoned(...)` — isto é: o passo
   **tem** acesso a store, e `Order emitted OrderCancelled` (§24.3) é evento de
   domínio no stream de `Order`, não um evento do passo. A pergunta que
   [design.md §4.4](../specs/correcoes-issues-6-8-12/design.md) não conseguiu
   responder (que rota o `emit` de um passo toma) tem resposta normativa agora.
 - `released` continua **indefinido**: uma única ocorrência em toda a v7
   (`24-testing.md:68`, dentro de um exemplo) e zero em código (`grep '"released"'`
   sobre `*.go` devolve nada hoje). Resíduo legítimo, inalterado.

**5. Contra-exemplo mínimo / shrinking (§24.5) — ABERTO, inalterado.**
`gentest_property.go:132-138` documenta explicitamente que não há shrinking; §24.5
continua exigindo "contra-exemplo mínimo". O bloqueio de M4.1 é **processual**
(`target_files` não inclui `codegen/testdata/tests_wallet.go.golden` nem
`codegen/gentest_test.go`), não semântico.

**6. `rolledback` com reversão real (§24.2) — ABERTO, inalterado.**
`gentest.go:1011-1015` emite só `if err == nil { t.Fatalf(...) }`, e
`memoryUnitOfWork.Run` (`codegen/rtsrc/uow.go.txt:109-122`) grava direto: o
próprio tipo documenta commit/rollback como no-op. É a task M4.2, pendente e
**sem** dependências. Nota da revisão: §5.3.7 acrescenta "`emitted X(...)` num
cenário que termina `rolledback` → falha do teste — rollback não publica", o que
o staging já satisfaz de graça (a publicação só ocorre depois de `fn` devolver
`nil`).

**Itens NOVOS que a revisão de §24.7 acrescentou ao inventário** (nenhum
implementado; `sema/rules_test_files.go:19-65` valida apenas **existência** de
símbolo):
 - **Metadata de evento em `given`/`when`/`then` → ❌ Erro** (§4.2.3). Pior que
   ausente: `gentest.go:1057` zera a metadata (`SetMeta(runtime.EventMeta{})`)
   antes do `reflect.DeepEqual`, então um `then` que nomeie `sequence` ou
   `timestamp` compara contra o zero e pode **passar em silêncio** — falso
   positivo, não só regra faltante. Além disso `EventMeta`
   (`codegen/rtsrc/event.go.txt:26-36`) tem 3 dos 5 campos que §4.2.3 fixa (sem
   `eventId`, sem `eventType`).
 - **`emitted <ApplicationEvent>` qualificado por instância → ❌ Erro** (§5.3.7).
   Não implementável hoje: `ApplicationEvent` tem **zero ocorrências** em todo o
   código Go e no runtime vendorizado. E há divergência simétrica: a forma
   canônica de §5.3.8 (`emitted TransferCompleted(...)` no `then` de um Test de
   UseCase) é rejeitada pelo `default` de `emitUseCaseThenAssert`
   (`gentest.go:1064`), porque o ramo de `emitted` exige `a.Subject != nil`
   (`gentest.go:1017`); o `emitted` não qualificado só existe no caminho de
   Policy (`gentest.go:1739`).
 - **`compensate` de `Error` inexistente → ❌ Erro** (§19.3.2). Não implementável
   hoje: `compensate` não existe no front-end — `grep` em `token/ lexer/ parser/
   ast/ sema/` só encontra o verbo de asserção `compensated`
   (`parser/parse_testfile.go:240`). *(Colateral da mesma revisão, fora desta
   issue mas na mesma vizinhança: §19.3 aboliu `onInfraError` — "construto
   inexistente, ❌ erro de compilação" — e o front-end ainda o modela
   (`ast/decl.go:374`, `parser/parse_decl.go:489`) e o codegen ainda o emite.)*

## Causa raiz

Duas, e o inventário as mistura: **(i)** o cenário de §24 só sabe descrever
*estado* e *ação* — nunca o *contexto de chamada* (caller, chave de
idempotência) nem o *retorno* de um seam externo — porque até a revisão de
2026-07-31 nem a gramática nem o spec tinham onde pendurá-los; **(ii)** a
validação estática de `*.test.ds` parou na existência de símbolos
(`sema/rules_test_files.go`), então toda regra ❌ de §24.7 que dependa de tipo,
envelope ou qualificação é hoje silêncio (às vezes com falso positivo) ou erro
tardio de geração.

## Solução proposta

Reescrever o inventário em **três frentes disjuntas**, e tratar esta issue como
índice delas em vez de saco único:

**Frente A — `sema` fecha o que §24.7 tornou estático** (sem gramática nova,
maior valor por custo). `checkTestFile` (`sema/rules_test_files.go:19`) ganha um
walk de `given`/`when`/`then` que rejeita os cinco nomes de envelope de §4.2.3
(`eventId`, `eventType`, `timestamp`, `sequence`, `aggregateId`) como argumento
nomeado ou campo asserido — hoje é a única linha nova de §24.7 implementável
integralmente com o que o front-end já modela. No mesmo passe, `gentest.go:1057`
deixa de ser a única defesa contra metadata em `then` (continua zerando, mas
agora nenhum cenário chega lá com envelope nomeado).

**Frente B — `codegen`/runtime fecha o que já está especificado.** M4.2 (staging
em `memoryTx`/`memoryUnitOfWork.Run`, `rtsrc/uow.go.txt`, + `rolledback` compara
a store antes/depois em `gentest.go:1011`) e M4.1 (shrinking em
`gentest_property.go`) são as duas fatias sem nenhuma pergunta de linguagem em
aberto; M4.1 só precisa de `target_files` corrigido.

**Frente C — ciclo de front-end, agora com texto normativo.** `-> T` em
`Notification` + `response { }`/`expect status` no Adapter Nível 1 + `function
"F" -> T throws E` no Nível 2 (§9.4) atravessando lexer→parser→resolver→sema →
destrava `result = call` (`lower/builtins.go:340`) e o `mock returns X` efetivo
(`gentest.go:1353`); `ApplicationEvent`/`PublicApplicationEvent` (§5.3) →
destrava `emitted` não qualificado em UseCase/Saga e a regra de qualificação
indevida; `compensate <Error>` (§19.3.2) → destrava a última linha nova de
§24.7. Contexto de chamada (caller e chave de idempotência) fica **fora**: ver a
issue irmã, porque §24 ainda não decidiu (é o único bloqueio de spec restante).

`released` sai do inventário para uma linha só de resíduo: **não é gap de
implementação, é ausência de definição na linguagem** — não reabrir sem revisão
de §24.3.

## Alternativas descartadas

- **Manter os seis itens como um item só de "oportunista, fechar quando o
  vizinho for tocado"** — foi o que produziu dois Marcos (L e M) com metade das
  tasks bloqueadas: os itens têm naturezas incompatíveis (um é sema puro, dois
  são runtime, três exigem gramática) e nenhum critério de pronto comum.
- **Implementar `mock returns X` só no codegen, inferindo o tipo de resposta do
  próprio literal do `mock`** — é exatamente a adivinhação que M3.1 recusou, e
  agora seria pior: contradiz §9.4, que ancora a resposta na **Notification**
  (`-> T`), não no Adapter nem no call site.
- **Dar staging ao `sqlrt` junto com a memory UoW** — `sqlrt` já é transacional
  de verdade; duplicaria semântica e ampliaria o raio sem fechar nada de §24.
- **Fechar a linha de `ApplicationEvent` só no gerador de testes** (aceitar
  `emitted X(...)` sem Subject num Test de UseCase, mapeando para o dispatcher) —
  geraria asserção sobre um conceito que o resto do pipeline não conhece: sem
  declaração, sem envelope `procedureName`, sem regra de "ApplicationEvent em
  `given` é erro". Behaviour fora do spec, proibido pelo [CLAUDE.md](../../../CLAUDE.md).

## Raio de alcance

- **Frente A:** nenhum golden. Só diagnósticos novos (`sema`), portanto pares
  NFR-4 em `sema/` e o risco de quebrar fixtures que hoje nomeiem envelope —
  verificado: `testdata/projects/wallet/wallet.test.ds` é o **único** `*.test.ds`
  de `testdata/` (`shop` não tem nenhum) e não nomeia nenhum dos cinco campos; o
  único casamento textual é `forall sequence of [Deposit, Withdraw]` (linha 53),
  palavra-chave de §24.5, não campo — a checagem precisa distinguir os dois. O
  sweep `fixtures` do CI segue verde. Em `docs/examples/` (não validado por CI)
  a regra passaria a rejeitar `ticketing.test.ds:17`
  (`TicketCreated(id: "T1", eventId: "E1")` — campo de payload de `Event`
  colidindo com o envelope), enquanto o `given tickets [ Ticket("T1") { eventId:
  "E1", … } ]` do próprio §24.4 continua legal (é `state` de Aggregate, e a
  proibição de nome de §4.2.3 alcança só `Event`/`PublicEvent`). Essa assimetria
  é o ponto fino da fatia — e sinal de que o exemplo de §24.4 e a regra de
  §4.2.3 merecem uma leitura conjunta antes de codificar.
- **Frente B (M4.2):** muda `codegen/rtsrc/uow.go.txt` (runtime vendorizado — o
  arquivo é copiado para **todo** projeto gerado, logo todo golden que contenha
  `runtime/uow.go` muda) e a asserção de `rolledback` em `gentest.go:1011`, que
  reescreve `codegen/testdata/tests_wallet.go.golden` (cenário "carteira nunca
  criada falha e não commita") e o teste de substring
  `codegen/gentest_test.go:109`. `tests_saga_purchasetickets.go.golden`,
  `tests_policy_refunds.go.golden` e `tests_thenstate_counter.go.golden` não têm
  `rolledback` — devem permanecer byte-idênticos, e essa é a guarda NFR-13/31 da
  task.
- **Frente B (M4.1):** `tests_wallet.go.golden` (é o único com `property`) +
  `gentest_test.go`. Nenhum outro.
- **Frente C:** ciclo próprio, com raio de pipeline inteiro (token→codegen) e
  goldens de Saga/UseCase; fora do orçamento de qualquer task de manutenção.

## Bloqueios

- **`released` (§24.3):** único item genuinamente bloqueado em decisão de
  linguagem. §24 precisa dizer o que o verbo assere (liberação de recurso
  reservado por um passo compensado? asserção sobre `state` da Saga? sobre
  eventos de um Aggregate tocado pelo `down`?), sobre qual sujeito, e o que a
  ausência de compensação implica. Sem isso, não implementar.
- **Contexto de chamada** (caller/idempotência): bloqueado, detalhado na issue
  irmã.
- **Não bloqueia mais:** contrato de resposta de Adapter (§9.4 decidiu),
  semântica de `emit` fora de Aggregate (§5.3 decidiu), qualificação de
  `emitted` (§5.3.7 decidiu), envelope em teste (§4.2.3 decidiu). As tasks M3.2/
  M3.3 (CANCELADAS) e M2.3 (bloqueada) devem ser **reavaliadas contra o texto
  novo** antes de qualquer implementação — mas como ciclo de front-end, não
  como retomada de tasks de codegen.

## Fatiamento sugerido

1. **Envelope em `*.test.ds` → erro estático** (§24.7 / §4.2.3). `target_files`:
   `sema/rules_test_files.go`, `sema/rules_test_files_test.go`. Par NFR-4: um
   cenário com `given [ E(sequence: 2) ]` e outro com `then [ E(id: "W1") ]` —
   erro no primeiro, silêncio no segundo. Sem dependências, sem golden.
2. **Staging na memory UoW + `rolledback` real** (§24.2) — é a
   [M4.2](../specs/correcoes-issues-6-8-12/tasks/M4.2.md) como está, já com
   `target_files` correto (`codegen/rtsrc/uow.go.txt`,
   `codegen/rtsrc/rtsrc_test.go`, `codegen/gentest.go`, `codegen/gentest_test.go`);
   acrescentar `codegen/testdata/tests_wallet.go.golden`, que a asserção nova
   necessariamente reescreve.
3. **Shrinking determinístico de `property`** (§24.5) — reabrir
   [M4.1](../specs/correcoes-issues-6-8-12/tasks/M4.1.md) com `target_files`
   ampliado para `codegen/gentest_property.go`,
   `codegen/gentest_property_test.go`, `codegen/testdata/tests_wallet.go.golden`,
   `codegen/gentest_test.go`. Depende de (2) só para evitar dois rewrites
   concorrentes do mesmo golden.
4. **Reescrita desta issue e de [gaps.md §G-7](../specs/codegen/gaps.md)** nas
   três frentes acima, com `released` isolado como resíduo de linguagem e os
   três itens novos de §24.7 registrados. `target_files`:
   `docs/sdd/issues/lacunas-nos-testes-gerados-test-ds.md`,
   `docs/sdd/specs/codegen/gaps.md`, `docs/sdd/issues/open-issues.md`. Sem
   código — é o fechamento documental que REQ-61 já previa, agora com o
   inventário certo.
