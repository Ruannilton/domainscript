# M2.3: mecanismo normativo de `emit` em passo de Saga (design.md §4.4, rota i) exige [decl_policy.go](../../../codegen/decl_policy.go)/[codegen.go](../../../codegen/codegen.go), fora de `target_files`
- SPEC: [correcoes-issues-6-8-12](../specs/correcoes-issues-6-8-12/requirements.md)
- TASK: [M2.3](../specs/correcoes-issues-6-8-12/tasks/M2.3.md)
- DESCRIPTION: [M2.3.md](../specs/correcoes-issues-6-8-12/tasks/M2.3.md) manda implementar a rota **(i) Dispatcher
  publish-only**, decidida por [M2.2](../specs/correcoes-issues-6-8-12/tasks/M2.2.md) (já `completed`) e registrada como
  **normativa** em [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 — a própria task diz "Não escolha uma
  rota diferente." `target_files` de M2.3 é só
  [decl_saga.go](../../../codegen/decl_saga.go), [stmt.go](../../../codegen/lower/stmt.go),
  [decl_saga_test.go](../../../codegen/decl_saga_test.go).

  O texto de [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 (parágrafo "Decisão (M2.2): rota (i)") descreve
  o mecanismo com uma citação de função explícita, não uma analogia solta:

  > "Um var de pacote `sagaDispatcher runtime.Dispatcher`, reatribuível pelo
  > Wire do módulo — mesmo padrão de `policyDispatcher`: nasce `nil` no
  > arquivo gerado, **o Wire (`emitPolicyWireFunc`/`emitCombinedWireFunc`) o
  > atribui** ao `runtime.Dispatcher` de verdade do serviço — condicional a
  > pelo menos um passo de alguma Saga do módulo usar `emit`..."

  `emitPolicyWireFunc` e `emitCombinedWireFunc` são funções de
  [decl_policy.go](../../../codegen/decl_policy.go), não de [decl_saga.go](../../../codegen/decl_saga.go). Implementar a rota
  **como o design normativo a descreve** exige, portanto, editar
  [decl_policy.go](../../../codegen/decl_policy.go) para que essas duas funções também emitam
  `sagaDispatcher = d` — arquivo fora de `target_files` de M2.3.

  Isso por si só já seria "arquivo fora de `target_files` que seria
  inevitável" (empecilho, conforme a definição do agente `task-implementer`).
  Mas há uma segunda lacuna, do próprio [design.md](../specs/correcoes-issues-6-8-12/design.md), que reforça o bloqueio:
  o mecanismo descrito só faz sentido quando o módulo **já tem** um
  `PolicyDecl`/`UseCaseDecl` (e portanto já ganha um `func Wire` de
  [decl_policy.go](../../../codegen/decl_policy.go)/[decl_usecase.go](../../../codegen/decl_usecase.go)). A fixture real usada pelos próprios
  testes de Saga hoje (`sagaFixtureSrc`/`sagaEmitFixtureSrc`,
  [decl_saga_test.go](../../../codegen/decl_saga_test.go)) é um módulo **só-Saga** — [codegen.go](../../../codegen/codegen.go) documenta
  explicitamente, na função `generateModuleFiles` (comentário acima da
  chamada de `EmitSagas`): "Sagas não somam a `moduleMarks`/`wireTargets`
  (`generateCmdMainFile`): ao contrário de UseCase/Policy/Worker, uma Saga
  não precisa de nenhum ponto de entrada injetado por
  `cmd/<service>/main.go`". Não existe, hoje, `func Wire` nenhum para
  estender num módulo desse tipo — [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 não decide o que
  acontece nesse caso (o caso, aliás, mais comum: nenhuma fixture de Saga do
  repo declara Policy no mesmo módulo).

  Cobrir esse caso (dar a um módulo só-Saga um ponto de wiring de
  `runtime.Dispatcher` real, chamado por `cmd/<service>/main.go`) segue o
  precedente já existente no próprio código para exatamente este problema —
  `WireMetrics` ([decl_metric.go](../../../codegen/decl_metric.go)), o mesmo "Dispatcher externo
  precisa alcançar um construto sem Wire próprio" que uma Metric `on Evento`
  resolveu com um nome de função PRÓPRIO (nunca "Wire", evitando a colisão
  de símbolo que ISSUE-7 fechou) e um novo campo em `moduleMarks`
  (`hasMetrics`) que `generateModuleFiles`/`generateCmdMainFile`
  ([codegen.go](../../../codegen/codegen.go)) usam para chamar `WireMetrics(d)` na inicialização. Replicar
  esse padrão para Saga — a única rota que evita tocar [decl_policy.go](../../../codegen/decl_policy.go) e
  ainda assim fia produção de verdade — exige mexer em [codegen.go](../../../codegen/codegen.go)
  (`moduleMarks`, `wireTargets`, `generateCmdMainFile`), TAMBÉM fora de
  `target_files` de M2.3.

  Uma implementação inteiramente contida em [decl_saga.go](../../../codegen/decl_saga.go) (nome de função
  próprio, nunca `"Wire"`, sem tocar [decl_policy.go](../../../codegen/decl_policy.go)/[codegen.go](../../../codegen/codegen.go)) evitaria
  colisão de símbolo em COMPILAÇÃO, mas (a) diverge do mecanismo literal que
  [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 nomeia (`emitPolicyWireFunc`/`emitCombinedWireFunc`) — a
  mesma "grafia diferente do design" que a diretriz do agente proíbe adotar
  por conta própria — e (b) deixaria a fiação de produção
  (`cmd/<service>/main.go` chamando a função nova) inatingível sem tocar
  [codegen.go](../../../codegen/codegen.go), então mesmo essa alternativa não fecha o item 4 dos Passos de
  Implementação da task ("garantir que ela compõe com o Wire combinado de
  UseCase+Policy... não reintroduzir colisão de símbolo").

  Não implementei nenhuma das rotas por conta própria — seria adivinhar uma
  forma que [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 não decidiu (o caso só-Saga) ou divergir do
  mecanismo que [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 decidiu (o caso com Policy/UseCase).
  [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 precisa decidir de novo, cobrindo explicitamente o caso
  só-Saga, e [M2.3.md](../specs/correcoes-issues-6-8-12/tasks/M2.3.md) precisa ganhar [decl_policy.go](../../../codegen/decl_policy.go) e
  [codegen.go](../../../codegen/codegen.go) em `target_files` (ou uma rota alternativa que não os
  exija) antes de esta task poder ser reaberta.
- SOLVED: []

# Solução proposta

> Análise de 2026-07-31 sobre `main` @ a6b239b. Desenho compartilhado com
> [a issue da colisão de `Wire`](usecase-e-policy-no-mesmo-modulo-colisao-de-wire.md)
> — as duas perguntam a mesma coisa por ângulos diferentes.

## Veredito

**Real, e todas as afirmações de fato conferem hoje.** Verificado arquivo a
arquivo:

- `emitPolicyWireFunc` ([decl_policy.go:610](../../../codegen/decl_policy.go)) e
  `emitCombinedWireFunc` ([decl_policy.go:648](../../../codegen/decl_policy.go))
  estão mesmo em [decl_policy.go](../../../codegen/decl_policy.go), e
  [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 (linhas 381-386)
  segue nomeando as duas como quem atribui `sagaDispatcher = d`.
  `target_files` de [M2.3.md](../specs/correcoes-issues-6-8-12/tasks/M2.3.md)
  (linhas 10-13) continua sendo só `decl_saga.go`/`lower/stmt.go`/
  `decl_saga_test.go`.
- O caso só-Saga segue não decidido e segue sendo o único caso real: **nenhum
  projeto de [`testdata/projects/`](../../../testdata/projects) declara Saga**
  (`grep -rl Saga testdata/projects/` não devolve nada), então toda evidência
  de Saga do repo é sintética, e a fixture âncora
  ([decl_saga_test.go:66-127](../../../codegen/decl_saga_test.go)) é
  `Module Tickets { }` com VO+Error+Command+Saga e mais nada.
  [codegen.go:791-796](../../../codegen/codegen.go) mantém o comentário "Sagas
  não somam a `moduleMarks`/`wireTargets`", e
  `TestGenerateSagaFixtureCompilesWithoutWireCollision`
  ([decl_saga_test.go:342-363](../../../codegen/decl_saga_test.go)) *assevera*
  que `cmd/tickets/main.go` não menciona a Saga.
- O precedente `WireMetrics` está exatamente onde a issue diz
  ([decl_metric.go:444](../../../codegen/decl_metric.go), marca em
  [codegen.go:452-460](../../../codegen/codegen.go), campo de `wireTarget` em
  [codegen.go:1091](../../../codegen/codegen.go), `needsDispatcher` em
  [codegen.go:1121-1123](../../../codegen/codegen.go), call site em
  [codegen.go:1484-1489](../../../codegen/codegen.go)).

Duas correções ao enunciado, uma delas grave:

1. `runtime.Dispatcher` é **interface**
   ([rtsrc/dispatcher.go.txt:10](../../../codegen/rtsrc/dispatcher.go.txt)).
   Um var de pacote nunca atribuído é interface nil e `.Publish` **panica**. A
   rota "contida em [decl_saga.go](../../../codegen/decl_saga.go), sem tocar
   [codegen.go](../../../codegen/codegen.go)" não é só "fiação inatingível":
   é um módulo só-Saga que compila, passa nos testes gerados (que reatribuem o
   var) e **panica no primeiro `emit` em produção**. Isso encerra a discussão
   sobre cobrir ou não o caso só-Saga.
2. **A revisão da spec da linguagem de 2026-07-31 mudou o alvo de M2.3.**
   [§5.3.2](../steerings/domainscript-spec-v7/05-application-layer.md) fixa
   que `emit <Event>` fora de um Aggregate é **erro de compilação**, e que a
   forma legal num `up`/`down` de step é `emit <ApplicationEvent>`. A rota (i)
   sobrevive intacta como *mecanismo* (§5.3.4 diz literalmente que
   `ApplicationEvent` não vai ao event store e que a publicação de um step
   ocorre "ao fim do `up`/`down` que contém o `emit`" — é publish-only, é o
   Dispatcher), mas o **construto emitido** não existe: `grep -rn
   ApplicationEvent --include=*.go .` devolve zero. Implementar M2.3 ao pé da
   letra hoje geraria Go para uma forma que a spec proíbe.

## Causa raiz

`Wire` foi projetada como *a* superfície do módulo e sua assinatura depende do
conteúdo declarado (`Wire(u)`, `Wire(d)`, `Wire(u, d)` — montada
posicionalmente em [codegen.go:1467-1476](../../../codegen/codegen.go)), então
todo construto novo que precise de uma dependência do serviço ou muda a aridade
de `Wire` (explosão 2^n de assinaturas, quebrando todo golden existente) ou
inventa um nome. Cinco construtos já inventaram (`StartWorkers`,
`WireQueryCache`, `WireMetrics`, `WireOutboxStore`, `WireFileStorage`) e nenhum
colidiu; `Wire` foi a única que colidiu. Ninguém escreveu a regra, então M2.2
descreveu o mecanismo de Saga pendurando-o na `Wire` de Policy — e um módulo
só-Saga não tem `Wire` nenhuma para pendurar.

## Solução proposta

### 1. A regra (o que falta em [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4)

Escrever como normativa a gramática que o código já pratica. A superfície de
wiring de um módulo gerado tem exatamente quatro formas:

| Forma | Papel | Precedentes |
|---|---|---|
| `Wire(...)` | **congelada** nas três assinaturas que L1.1 fixou; nunca ganha parâmetro novo | [decl_usecase.go:89](../../../codegen/decl_usecase.go), [decl_policy.go:623](../../../codegen/decl_policy.go), [decl_policy.go:661](../../../codegen/decl_policy.go) |
| `Wire<Dependência>(dep)` | injeção pura de um seam do serviço | `WireOutboxStore`, `WireFileStorage` |
| `Wire<Concern>(dep)` | injeção **e** registro (Subscribe) | `WireQueryCache`, `WireMetrics` |
| `Start<Concern>(ctx)` | goroutine de fundo | `StartWorkers`, `StartIdempotencyCleanup`, `StartOutboxRelay/Cleanup` |

Ordem de chamada em `cmd/<service>/main.go`, estável (determinismo, NFR-13):
`WireOutboxStore` → `Wire` → `WireQueryCache` → `WireMetrics` →
`WireDispatcher` → `Wire2PC` → `WireFileStorage` → `Start*`. Só a primeira
seta é uma dependência de verdade (`Wire` lê `outboxStore`); o resto é ordem
fixa por determinismo, e por isso **inserir um item novo nunca reordena os
existentes**.

### 2. O seam concreto: um Dispatcher por MÓDULO, não por construto

Nome pela **dependência**, não pelo construto: `WireDispatcher(d
runtime.Dispatcher)`. A razão é [§5.3.2](../steerings/domainscript-spec-v7/05-application-layer.md):
quando `ApplicationEvent` chegar, `emit` passa a ser legal em UseCase
`execute`, Policy `execute`, Worker `execute` **e** step de Saga — quatro
emissores, quatro arquivos, **uma única dependência**. `WireSagas`/
`WireUseCaseEvents`/`WireWorkerEvents` seriam quatro setters para o mesmo
objeto.

- Um novo `codegen/wiring.go` emite `<pkg>/wiring.go` com `var
  moduleDispatcher runtime.Dispatcher` + `func WireDispatcher(d
  runtime.Dispatcher) { moduleDispatcher = d }`. Arquivo próprio porque o var
  é disputado por dois emissores (sagas.go e policies.go) — **exatamente o
  precedente de `collections.go`/`EmitCollections`**
  ([codegen.go:682-690](../../../codegen/codegen.go)), criado para o mesmo
  problema (um var de pacote que dois emissores querem declarar).
- `emitSagaStepPhaseFunc` ([decl_saga.go:255](../../../codegen/decl_saga.go))
  anexa `.WithEmitDispatch("moduleDispatcher", "ctx")` ao `StmtLowerer` — o
  mecanismo já existe em [lower/stmt.go:182](../../../codegen/lower/stmt.go),
  nenhuma linha nova ali. `checkNoEmitInSagaStepBlock`
  ([decl_saga.go:223](../../../codegen/decl_saga.go)) deixa de barrar a forma
  coberta e mantém o erro para o resto (REQ-56.5).
- `policyDispatcher` ([decl_policy.go:378](../../../codegen/decl_policy.go))
  migra para o mesmo var; `emitPolicyWireBody`
  ([decl_policy.go:754-761](../../../codegen/decl_policy.go)) para de fazer
  `policyDispatcher = d`. É o que dispensa
  [decl_policy.go](../../../codegen/decl_policy.go) de `target_files` de M2.3:
  a Saga não passa a depender de a Policy existir, e a Policy passa a usar o
  seam do módulo.

### 3. O caso só-Saga (a lacuna que §4.4 não decidiu)

`moduleMarks` ([codegen.go:415](../../../codegen/codegen.go)) ganha
`needsDispatcherSeam bool` — true quando algum corpo emissor do módulo usa
`emit` (hoje: alguma Policy, ou algum `up`/`down` de algum step). Em
`generateCmdMainFile`:

- entra no predicado de salto de
  [codegen.go:1109](../../../codegen/codegen.go)
  (`if !hu && !hp && !hw && !hc && !hm && len(fsNames) == 0 { continue }`), de
  forma que um módulo só-Saga vire `wireTarget`;
- força `needsDispatcher = true`
  ([codegen.go:1115-1123](../../../codegen/codegen.go)), do mesmo jeito que
  `hasCachedQueries`/`hasMetrics` já fazem;
- emite `<pkg>.WireDispatcher(dispatcher)` ao lado de
  `<pkg>.WireMetrics(dispatcher)`
  ([codegen.go:1484-1489](../../../codegen/codegen.go)).

Para o `Tickets` da fixture (só-Saga, **sem** `emit`) a marca é falsa e nada
muda — `TestGenerateSagaFixtureCompilesWithoutWireCollision` continua
passando literalmente. Para um só-Saga **com** `emit`, o `main.go` gerado
constrói o `dispatcher`, chama `WireDispatcher` e cai no ramo já existente
`_ = uow // nenhum módulo deste service declara UseCase`
([codegen.go:1549-1551](../../../codegen/codegen.go)) — nenhum caminho novo em
main.go. Honestidade sobre o alcance: num módulo só-Saga o Dispatcher não tem
assinante local, então a publicação é inerte até alguém assinar (uma Policy do
mesmo módulo, ou o dispatcher de teste de M2.4). É exatamente a semântica de
[§5.3.3](../steerings/domainscript-spec-v7/05-application-layer.md) ("Policy é
o consumidor canônico") — e é infinitamente melhor que o panic de interface
nil da rota sem wiring.

### 4. Limpeza de plumbing (opcional, mas paga por si)

`generateCmdMainFile` já recebe **seis** `map[string]bool` + dois outros mapas
([codegen.go:1075](../../../codegen/codegen.go)) e `Generate` mantém seis mapas
paralelos ([codegen.go:165-194](../../../codegen/codegen.go)) só para
repassá-los. Trocar tudo por um `map[string]moduleMarks` deixa o custo de cada
construto futuro (incluindo os quatro emissores de `ApplicationEvent`) em **um
campo de struct**, e é byte-idêntico por construção.

## Alternativas descartadas

- **Estender `Wire` com mais um parâmetro** (`Wire(u, d)` → `Wire(u, d, x)`):
  a aridade depende da combinação declarada e main.go a monta posicionalmente
  ([codegen.go:1467-1476](../../../codegen/codegen.go)); n construtos ⇒ 2^n
  assinaturas, e cada construto novo reescreve todo golden de `Wire` existente.
  É literalmente o defeito que L1.1 teve de consertar.
- **[design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 ao pé da
  letra** (`emitPolicyWireFunc`/`emitCombinedWireFunc` atribuem
  `sagaDispatcher`): não cobre o caso só-Saga (não há `Wire` a estender),
  acopla Saga a [decl_policy.go](../../../codegen/decl_policy.go) e faz a
  fiação de uma Saga depender de o módulo por acaso declarar uma Policy. É a
  rota que gerou esta issue.
- **Auto-inicializar o var** (`var sagaDispatcher = runtime.NewDispatcher()`,
  no espírito do `SagaStore` de
  [decl_saga.go:463](../../../codegen/decl_saga.go)): zero mudança em
  [codegen.go](../../../codegen/codegen.go), compila, não panica — e publica
  num Dispatcher **privado** que nenhuma Policy do serviço assina. Entrega
  silenciosamente inerte é pior que o panic: o panic aparece; isto não.
- **Tudo dentro de [decl_saga.go](../../../codegen/decl_saga.go) com nome
  privado**: o item (b) da própria issue — sem call site em main.go o var fica
  nil (panic) e o item 4 dos Passos de Implementação de M2.3 não fecha.
- **`WireSagas(d)`** (nome por construto, cópia literal de `WireMetrics`):
  funciona e é o menor delta possível — mas
  [§5.3](../steerings/domainscript-spec-v7/05-application-layer.md) traz mais
  três emissores de `emit`, e o resultado seria quatro setters exportados para
  injetar o mesmo objeto. Perde só por isso; se o ciclo quiser o mínimo
  absoluto, é o fallback aceitável.

## Raio de alcance

- **Goldens de [codegen/testdata/](../../../codegen/testdata) (56 arquivos):**
  o seam de Saga é gated por `emit` em step, que **nenhuma** fixture atual usa
  → 0 goldens quebram por causa da Saga. A migração de `policyDispatcher` →
  `moduleDispatcher` quebra **4**: `policy_pricecheck`,
  `policy_ranking_orderby`, `policy_ticketsales_smartpartial`,
  `tests_policy_refunds` (renomeação mecânica), mais
  [gentest.go](../../../codegen/gentest.go) (que emite o nome do var nos testes
  gerados) e 4 arquivos de teste que reatribuem o var por nome
  (`gentest_policy_test.go`, `gentest_policy_predicate_test.go`,
  `gentest_policy_orderby_test.go`, `gentest_policy_smartpartial_test.go`).
- **Goldens de `cmd/<service>/main.go` (6):** 0 quebram. As linhas novas só
  aparecem sob marca verdadeira, e nenhum dos seis módulos-golden tem Policy
  com `emit` nem Saga.
- **[testdata/projects/](../../../testdata/projects) (wallet, shop,
  pizzeria):** 0 mudanças. Nenhum declara Saga; a única Policy real
  (`shop/shipping/policy.ds`) é `execute { return }`, sem `emit`.
- **`KNOWN_UNGENERATABLE` no [ci.yml](../../../.github/workflows/ci.yml):**
  inalterado. `pizzeria` não tem Saga; verificado hoje que seu bloqueio atual é
  outro (ver a issue-irmã).
- **NFR-13/NFR-31:** preservados por construção — toda emissão nova é gated
  por marca falsa em todo módulo existente, e o refactor de plumbing não muda
  nenhum byte.
- **Cobertura de CI:** vale registrar que o job `fixtures` **nunca** exercitou
  Saga (nenhum projeto em `testdata/projects/` a declara). Um projeto-fixture
  com Saga seria o primeiro `dsc gen` + `go build` de Saga em CI.

## Bloqueios

1. **Spec da linguagem,
   [§5.3.2](../steerings/domainscript-spec-v7/05-application-layer.md)
   (revisão de 2026-07-31) — o mais grave.** A forma que M2.3 mandaria gerar
   (`emit <Event>` num step) é agora **erro de compilação**; a forma legal
   (`emit <ApplicationEvent>`) não existe em lugar nenhum da implementação.
   O teste positivo TEST-1/TEST-2 de
   [M2.3.md](../specs/correcoes-issues-6-8-12/tasks/M2.3.md) não tem como ser
   escrito de forma conformante hoje. Consequência: **o seam de wiring pode e
   deve ser entregue agora** (é estrutura do Go gerado, matéria que a spec da
   linguagem não descreve — decisão de
   [design.md](../specs/correcoes-issues-6-8-12/design.md), não da spec), mas o
   **cliente Saga do seam fica bloqueado** até o front-end ter
   `ApplicationEvent`. M2.3 precisa ser fatiada nessa junta.
2. **[design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 precisa ser
   reescrita** — decisão do dono do ciclo, não de quem implementa: (a) trocar
   `emitPolicyWireFunc`/`emitCombinedWireFunc` por `WireDispatcher` como
   atribuidor; (b) decidir explicitamente o caso só-Saga; (c) registrar a
   gramática de nomes da seção 1 acima como invariante.
3. **Fora do meu escopo, mas cai no mesmo arquivo:**
   [§19.2/§19.3](../steerings/domainscript-spec-v7/19-transactions-sagas.md) da
   revisão de 2026-07-31 eliminou `onInfraError` ("não existe bloco
   `onInfraError`… → erro de compilação") e criou `retry:`/`compensate`.
   [decl_saga.go](../../../codegen/decl_saga.go) emite `onInfraError` hoje
   ([decl_saga.go:315-320](../../../codegen/decl_saga.go)). Quem for reabrir
   M2.3 vai disputar o mesmo arquivo com essa correção de conformidade; não
   registrei issue por não ser meu escopo.

## Fatiamento sugerido

1. **W1 — reescrever [design.md](../specs/correcoes-issues-6-8-12/design.md)
   §4.4** com a gramática de nomes, a ordem de chamada e a decisão explícita do
   caso só-Saga; reescrever
   [M2.3.md](../specs/correcoes-issues-6-8-12/tasks/M2.3.md) partido em W3/W4 e
   com o novo `target_files`. *(spec-writer; nenhum código)*
   `target_files`: `docs/sdd/specs/correcoes-issues-6-8-12/design.md`,
   `docs/sdd/specs/correcoes-issues-6-8-12/tasks/M2.3.md`,
   `docs/sdd/specs/correcoes-issues-6-8-12/state.md`.
2. **W2 — plumbing:** `Generate`/`generateCmdMainFile` passam a trocar
   `map[string]moduleMarks` no lugar dos seis mapas paralelos. Saída
   byte-idêntica (é o critério de conclusão).
   `target_files`: `codegen/codegen.go`, `codegen/codegen_test.go`.
3. **W3 — o seam:** novo `codegen/wiring.go` (`moduleDispatcher` +
   `WireDispatcher`), `moduleMarks.needsDispatcherSeam`, entrada em
   `wireTargets`/`needsDispatcher`/call site, e migração de `policyDispatcher`
   para o seam do módulo (primeiro cliente real, com cobertura existente).
   Não depende de §5.3.
   `target_files`: `codegen/wiring.go` (novo), `codegen/codegen.go`,
   `codegen/decl_policy.go`, `codegen/gentest.go`,
   `codegen/testdata/policy_*.go.golden`,
   `codegen/testdata/tests_policy_refunds.go.golden`,
   `codegen/gentest_policy*_test.go`.
4. **W4 — Saga pluga no seam** *(bloqueada até `ApplicationEvent` existir no
   front-end)*: `emitSagaStepPhaseFunc` anexa `WithEmitDispatch`,
   `checkNoEmitInSagaStepBlock` estreita para o que a rota não cobre, marca de
   módulo só-Saga, goldens novos, teste comportamental sobre o projeto gerado.
   `target_files`: `codegen/decl_saga.go`, `codegen/codegen.go`,
   `codegen/decl_saga_test.go`, `codegen/testdata/saga_*_emit.go.golden` (novo).
5. **W5 (opcional) — fixture de CI:** um projeto em
   [testdata/projects/](../../../testdata/projects) com Saga, para o job
   `fixtures` cobrir `dsc gen` + `go build` de Saga pela primeira vez. Depende
   de W4.
   `target_files`: `testdata/projects/<novo>/**`.
