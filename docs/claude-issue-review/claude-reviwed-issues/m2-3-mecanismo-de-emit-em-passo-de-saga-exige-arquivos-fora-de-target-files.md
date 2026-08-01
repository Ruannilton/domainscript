CODIGO: m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files
CATEGORIA: Ajuste de especificação
Issue original: [[docs/sdd/issues/m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files]]

## Resumo da issue

A task M2.3 mandava dar a passos de Saga a capacidade de `emit`, ligando-os ao `runtime.Dispatcher` do módulo do mesmo jeito que `emitPolicyWireFunc`/`emitCombinedWireFunc` fazem em `decl_policy.go`. Só que isso amarraria a Saga a existir uma Policy/UseCase no módulo (ela sozinha não tem `func Wire` nenhuma para pendurar essa fiação), e nenhuma fixture real do repositório declara um módulo só-Saga, então esse caso nunca foi decidido. A pessoa que tentou implementar parou porque tocar `decl_policy.go`/`codegen.go` estava fora de `target_files` de M2.3, e porque o design que orientava a task nunca cobriu o caso comum (Saga sem Policy no mesmo módulo).

## Evidencias

- `emitPolicyWireFunc` (`codegen/decl_policy.go:610`) e `emitCombinedWireFunc` (`codegen/decl_policy.go:648`) são as únicas funções que hoje atribuem um Dispatcher de módulo — e ficam fora de `target_files` de `[[docs/sdd/specs/correcoes-issues-6-8-12/tasks/M2.3|M2.3.md]]` (que lista só `decl_saga.go`, `lower/stmt.go`, `decl_saga_test.go`).
- `codegen/codegen.go:791-796` documenta explicitamente: "Sagas não somam a `moduleMarks`/`wireTargets`" — nenhum módulo só-Saga hoje ganha ponto de entrada em `cmd/<service>/main.go`.
- `runtime.Dispatcher` é **interface** (`codegen/rtsrc/dispatcher.go.txt:10`); um var de pacote nunca atribuído é nil, e `.Publish` sobre ele **panica** em produção — a análise corrige a issue original nesse ponto: não é só "fiação inatingível", é panic no primeiro `emit`.
- `grep -rl Saga testdata/projects/` não devolve nada — nenhum projeto real do repositório exercita este caminho hoje; toda evidência é sintética (`codegen/decl_saga_test.go:66-127`).
- `[[docs/sdd/steerings/domainscript-spec-v7/05-application-layer|05-application-layer.md]]` §5.3.2 (revisão de 2026-07-31) já normatiza que `emit <Event>` fora de Aggregate é erro de compilação e a forma legal num `up`/`down` de step é `emit <ApplicationEvent>` — a spec da linguagem decidiu esse ponto, mas `grep -rn ApplicationEvent --include=*.go .` continua devolvendo zero: nada foi implementado no front-end ainda.

## Impacto no projeto

Sem decidir a gramática de nomes de wiring, cada construto novo do back-end (Saga, e os quatro emissores futuros de `ApplicationEvent` — UseCase, Policy, Worker, Saga) volta a disputar a mesma pergunta que já travou M2.3 uma vez e já havia travado a issue irmã (colisão de `Wire`). Implementar M2.3 ao pé da letra hoje, além disso, geraria Go para uma forma (`emit <Event>` num step) que a spec agora proíbe.

## Soluçoes possíveis

### Solucão 1

Registrar a gramática de nomes de função gerada como invariante de design (não uma decisão por construto): `Wire(...)` fica congelada nas três assinaturas existentes; dependências novas ganham nome próprio pela **forma** (`Wire<Dependência>`, `Wire<Concern>`, `Start<Concern>`), nunca por struct de dependências nem por explosão posicional de `Wire`. Concretamente, um `WireDispatcher(d runtime.Dispatcher)` novo, em `codegen/wiring.go`, vira o seam único de Dispatcher do módulo — usado por Policy (substituindo o `policyDispatcher` de hoje) e, mais tarde, por Saga. Uma marca nova em `moduleMarks` (`needsDispatcherSeam`) cobre o caso só-Saga: dá a ele, pela primeira vez, um ponto de entrada real em `cmd/<service>/main.go`, evitando o panic de interface nil. É a rota que a análise da issue já detalha (seções 1-3 da "Solução proposta"), e reaproveita o precedente já existente do código (`StartWorkers`, `WireQueryCache`, `WireMetrics`, `WireOutboxStore`, `WireFileStorage` — nenhum desses colidiu).

### Solução 2

Manter a rota literal do `[[docs/sdd/specs/correcoes-issues-6-8-12/design.md|design.md]]` §4.4 original (`emitPolicyWireFunc`/`emitCombinedWireFunc` atribuindo `sagaDispatcher`). Descartada: não cobre o caso só-Saga (não há `Wire` nenhuma para estender), acopla a Saga à existência acidental de uma Policy no módulo, e foi exatamente a rota que gerou esta issue. A alternativa "tudo dentro de `decl_saga.go`, nome privado, sem tocar `codegen.go`" também foi descartada: sem call site em `main.go` o var fica nil e panica — pior que o estado atual.

## O que precisa ser resolvido antes

A decisão de nomenclatura já foi tomada pelo desenvolvedor — registrada como nota embutida na issue irmã `[[docs/sdd/issues/usecase-e-policy-no-mesmo-modulo-colisao-de-wire|usecase-e-policy-no-mesmo-modulo-colisao-de-wire]]` (as duas issues descrevem, por ângulos diferentes, a mesma lacuna de `[[docs/sdd/specs/correcoes-issues-6-8-12/design.md|design.md]]` §4.4): usar "**Nomes próprios por concern, com contrato uniforme** — a uniformidade está na forma (uma função exportada por concern, uma dependência por chamada, ordem de chamada fixa em `main.go`), não numa função única." O que falta é:

1. Reescrever `[[docs/sdd/specs/correcoes-issues-6-8-12/design.md|design.md]]` §4.4 registrando essa gramática como normativa, incluindo a decisão explícita do caso só-Saga (`needsDispatcherSeam`) — trabalho editorial, não código.
2. Refatiar `M2.3.md` com o novo `target_files` (`codegen/wiring.go`, `codegen/codegen.go`, `codegen/decl_policy.go`) e implementar W1-W3 do fatiamento sugerido na issue — isso não depende de mais nada.
3. O cliente Saga do seam (W4 — Saga efetivamente emitindo `ApplicationEvent` num step) permanece bloqueado até o front-end implementar `ApplicationEvent`, hoje só normatizado na spec (`[[docs/sdd/issues/spec-v7-metadata-implicito-de-event|spec-v7-metadata-implicito-de-event]]`) e ainda não implementado em parser/resolver/checker/codegen — isso é dependência de outro ciclo de trabalho, não uma ambiguidade de spec.
