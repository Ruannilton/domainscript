# UseCase e Policy no mesmo módulo não geram (colisão de Wire) (ex-ISSUE-7)
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

  EM ANDAMENTO (spec criada): `.claude/specs/correcoes-issues-6-7-8/`
  (Marco L, REQ-52 / §design 2). Achado da análise de raiz: **o próprio
  código já resolve esta colisão em outros lugares** — `StartWorkers`,
  `WireQueryCache`, `WireOutboxStore`/`StartOutboxRelay` usam nome próprio
  em vez de um 2º `Wire`. Fix recomendado: um `Wire` unificado por módulo
  (`func Wire(u UnitOfWork, d Dispatcher)` no caso misto; casos puros
  byte-idênticos). O `Kitchen` do pizzeria é a fixture-âncora; ao fechar,
  `pizzeria` sai da lista `KNOWN_UNGENERATABLE` do CI
  (`.github/workflows/ci.yml`) e passa a gerar+compilar como wallet/shop.

  A task L1.1 já fechou a colisão de `Wire` em si (o escopo direto desta
  issue). **Porém não marcar esta issue como totalmente resolvida** enquanto
  a issue sobre o pizzeria bloqueado por múltiplos defeitos de codegen (o
  bloqueio real e maior de gerar o pizzeria de ponta a ponta, achado em
  L1.2) permanecer aberta — ver `.claude/specs/correcoes-issues-6-7-8/`.
- SOLVED: FALSE
