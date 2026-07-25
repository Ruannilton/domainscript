# Lacunas nos testes gerados a partir de `*.test.ds` (ex-ISSUE-6)
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
  staging. (O item §22.4 — agrupamento por `orderId` — já foi fechado pelo
  ciclo read-side, REQ-39.1/I6.2, e não entra aqui.) Oportunista: fechar cada
  um quando o vizinho for tocado.

  EM ANDAMENTO (spec criada): `.claude/specs/correcoes-issues-6-7-8/`
  (Marco L, REQ-53 / §design 3). Análise de raiz categorizou os seis
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

  Status conforme `.claude/state.md`: Marco L ainda **in-progress**
  (L1.1/L1.2/L1.3a/L1.3b/L1.3c/L2.1 done; L1.3d pausada por decisão do
  usuário; L1.3e/L1.3f bloqueadas em cascata — ver a issue sobre o pizzeria
  bloqueado por múltiplos defeitos de codegen; próxima task L2.5 —
  `rolledback` com reversão real). Fecha (parcialmente) quando o Marco L
  fechar.
- SOLVED: FALSE
