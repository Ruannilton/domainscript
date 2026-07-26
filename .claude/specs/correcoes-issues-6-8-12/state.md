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

- M1.2 — `list <Aggregate>` sem cláusulas em EmitQuery
- M1.3 — `list <Aggregate>` com cláusulas (where/orderBy/skip/take/as)
- M1.5 — Implementar o wiring: remover as guardas F5 e F5/G3
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

- M1.1 — Seam de enumeração de streams no runtime (StreamLister). Step 2 pede
  para decidir como `aggregateType` chega até `Append` (registro já
  disponível, ou o seam que `Event`/`EventMeta` já oferece) e manda parar se
  nenhuma rota funcionar sem alterar `EventStore`. Verificado por leitura:
  nenhuma das duas rotas existe hoje dentro de `target_files`, e a rota que
  existe (thread via `ctx`, mesmo mecanismo de `tenantID`) exige tocar
  `codegen/rtsrc/contextkeys.go.txt` e o call site de `Append`
  (`codegen/rtsrc/uow.go.txt`/`codegen/lower/stmt.go`), fora do escopo desta
  task. Pedido de decisão de `design.md` §4.1/§5.1, registrado em
  `.claude/issues/m1-1-aggregatetype-nao-chega-a-eventstore-append.md`. M1.2
  e M1.3 dependem de M1.1 (`depends_on`) e ficam transitivamente travadas até
  a decisão — seguem listadas em PENDING acima porque nenhuma execução
  chegou a tentá-las ainda; quem as pegar vai encontrar a dependência não
  concluída no pré-voo e parar do mesmo jeito.
