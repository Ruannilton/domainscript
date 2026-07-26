# State — Conclusão das dívidas remanescentes (ISSUE-6, ISSUE-8, ISSUE-12)

> Resumo agregado de quais tasks desta spec estão pendentes e quais estão
> bloqueadas, para retomar a execução após uma interrupção. O detalhe de cada
> task mora no próprio arquivo `tasks/<TASK-CODE>.md` (campo `status` no
> frontmatter: `pending` | `in_progress` | `completed`); este arquivo é só o
> índice agregado — atualize os dois juntos. Tasks `completed` saem das listas
> abaixo.
>
> Marco M, cinco fases **independentes entre si** (M1..M5, qualquer ordem ou
> paralelo); dentro de cada fase a ordem é linear. A lista abaixo está em ordem
> de valor decrescente — M1 fecha ISSUE-12 e desbloqueia o `pizzeria` no CI.
>
> **M1.4, M2.2 e M3.1 são tasks de design (sem código).** M2.2 e M3.1 podem
> escolher delimitar; nesse caso as tasks que dependem delas (M2.3/M2.4,
> M3.2/M3.3) são canceladas — remova-as daqui registrando o motivo, conforme o
> passo final da task de design.

## PENDING TASKS:

- M1.2 — `list <Aggregate>` sem cláusulas em EmitQuery. `depends_on:
  TASK-M1.1`, que voltou a `blocked` — não iniciar até M1.1 ser desbloqueada
  de novo (ver BLOCKED TASKS abaixo).
- M1.3 — `list <Aggregate>` com cláusulas (where/orderBy/skip/take/as) —
  mesma dependência transitiva de M1.1 via M1.2.
- M1.6 — Prova e2e do `pizzeria` e limpeza do CI
- M2.1 — `emit` em passo de Saga vira erro de geração claro
- M2.2 — (design) Decidir como um passo de Saga emite
- M2.3 — Implementar o caminho de `emit` escolhido em M2.2
- M2.4 — Asserção `emitted` no `then` de um Test de Saga
- M3.1 — (design) Contrato de resposta de Adapter
- M3.2 — Implementar `result = call Adapter(...)` (§18.2)
- M3.3 — `mock ... returns X` injeta X como retorno do stub
- M4.1 — Shrinking determinístico do contra-exemplo de `property` (§22.5)
- M4.2 — Staging na memory UoW: `rolledback` prova reversão real (§22.2)
- M5.1 — Cobertura §22.7 por ramo de `Error` em `sema`
- M5.2 — Delimitações e reclassificações em `gaps.md` e nas issues
- M5.3 — Fechamento do Marco M: revisão de DoD

## BLOCKED TASKS:

- M1.1 — Seam de enumeração de streams no runtime (StreamLister). Bloqueada
  DE NOVO: a rota decidida em `design.md` §5.1/§7.2 (thread de
  `aggregateType` via `ctx`, carimbado uma vez antes de `uow.Run`) partia da
  premissa de que uma única `Tx.Run()` nunca grava eventos de mais de um
  `aggregateType` — verificado por leitura e **refutado**
  (`sema/rules_crossfile.go:checkTransactions` só restringe por `Database`,
  nunca por tipo de Aggregate; um módulo sem `Database` ou com dois
  Aggregates no mesmo `Database` pode despachar `Handle` de tipos diferentes
  no mesmo `Run`). Issue nova (a original já está `SOLVED`):
  `.claude/issues/m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype.md`.
  `design.md` §5.1/§7.2 precisa decidir de novo antes de reabrir.
