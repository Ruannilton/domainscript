# M2.3: mecanismo normativo de `emit` em passo de Saga (design.md §4.4, rota i) exige `decl_policy.go`/`codegen.go`, fora de `target_files`
- SPEC: correcoes-issues-6-8-12
- TASK: M2.3
- DESCRIPTION: `tasks/M2.3.md` manda implementar a rota **(i) Dispatcher
  publish-only**, decidida por M2.2 (já `completed`) e registrada como
  **normativa** em `design.md` §4.4 — a própria task diz "Não escolha uma
  rota diferente." `target_files` de M2.3 é só
  `codegen/decl_saga.go`, `codegen/lower/stmt.go`,
  `codegen/decl_saga_test.go`.

  O texto de `design.md` §4.4 (parágrafo "Decisão (M2.2): rota (i)") descreve
  o mecanismo com uma citação de função explícita, não uma analogia solta:

  > "Um var de pacote `sagaDispatcher runtime.Dispatcher`, reatribuível pelo
  > Wire do módulo — mesmo padrão de `policyDispatcher`: nasce `nil` no
  > arquivo gerado, **o Wire (`emitPolicyWireFunc`/`emitCombinedWireFunc`) o
  > atribui** ao `runtime.Dispatcher` de verdade do serviço — condicional a
  > pelo menos um passo de alguma Saga do módulo usar `emit`..."

  `emitPolicyWireFunc` e `emitCombinedWireFunc` são funções de
  `codegen/decl_policy.go`, não de `codegen/decl_saga.go`. Implementar a rota
  **como o design normativo a descreve** exige, portanto, editar
  `decl_policy.go` para que essas duas funções também emitam
  `sagaDispatcher = d` — arquivo fora de `target_files` de M2.3.

  Isso por si só já seria "arquivo fora de `target_files` que seria
  inevitável" (empecilho, conforme a definição do agente `task-implementer`).
  Mas há uma segunda lacuna, do próprio `design.md`, que reforça o bloqueio:
  o mecanismo descrito só faz sentido quando o módulo **já tem** um
  `PolicyDecl`/`UseCaseDecl` (e portanto já ganha um `func Wire` de
  `decl_policy.go`/`decl_usecase.go`). A fixture real usada pelos próprios
  testes de Saga hoje (`sagaFixtureSrc`/`sagaEmitFixtureSrc`,
  `decl_saga_test.go`) é um módulo **só-Saga** — `codegen.go` documenta
  explicitamente, na função `generateModuleFiles` (comentário acima da
  chamada de `EmitSagas`): "Sagas não somam a `moduleMarks`/`wireTargets`
  (`generateCmdMainFile`): ao contrário de UseCase/Policy/Worker, uma Saga
  não precisa de nenhum ponto de entrada injetado por
  `cmd/<service>/main.go`". Não existe, hoje, `func Wire` nenhum para
  estender num módulo desse tipo — `design.md` §4.4 não decide o que
  acontece nesse caso (o caso, aliás, mais comum: nenhuma fixture de Saga do
  repo declara Policy no mesmo módulo).

  Cobrir esse caso (dar a um módulo só-Saga um ponto de wiring de
  `runtime.Dispatcher` real, chamado por `cmd/<service>/main.go`) segue o
  precedente já existente no próprio código para exatamente este problema —
  `WireMetrics` (`codegen/decl_metric.go`), o mesmo "Dispatcher externo
  precisa alcançar um construto sem Wire próprio" que uma Metric `on Evento`
  resolveu com um nome de função PRÓPRIO (nunca "Wire", evitando a colisão
  de símbolo que ISSUE-7 fechou) e um novo campo em `moduleMarks`
  (`hasMetrics`) que `generateModuleFiles`/`generateCmdMainFile`
  (`codegen.go`) usam para chamar `WireMetrics(d)` na inicialização. Replicar
  esse padrão para Saga — a única rota que evita tocar `decl_policy.go` e
  ainda assim fia produção de verdade — exige mexer em `codegen.go`
  (`moduleMarks`, `wireTargets`, `generateCmdMainFile`), TAMBÉM fora de
  `target_files` de M2.3.

  Uma implementação inteiramente contida em `decl_saga.go` (nome de função
  próprio, nunca `"Wire"`, sem tocar `decl_policy.go`/`codegen.go`) evitaria
  colisão de símbolo em COMPILAÇÃO, mas (a) diverge do mecanismo literal que
  `design.md` §4.4 nomeia (`emitPolicyWireFunc`/`emitCombinedWireFunc`) — a
  mesma "grafia diferente do design" que a diretriz do agente proíbe adotar
  por conta própria — e (b) deixaria a fiação de produção
  (`cmd/<service>/main.go` chamando a função nova) inatingível sem tocar
  `codegen.go`, então mesmo essa alternativa não fecha o item 4 dos Passos de
  Implementação da task ("garantir que ela compõe com o Wire combinado de
  UseCase+Policy... não reintroduzir colisão de símbolo").

  Não implementei nenhuma das rotas por conta própria — seria adivinhar uma
  forma que `design.md` §4.4 não decidiu (o caso só-Saga) ou divergir do
  mecanismo que `design.md` §4.4 decidiu (o caso com Policy/UseCase).
  `design.md` §4.4 precisa decidir de novo, cobrindo explicitamente o caso
  só-Saga, e `tasks/M2.3.md` precisa ganhar `codegen/decl_policy.go` e
  `codegen/codegen.go` em `target_files` (ou uma rota alternativa que não os
  exija) antes de esta task poder ser reaberta.
- SOLVED: []
