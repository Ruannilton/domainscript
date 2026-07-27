# M1.1: uma `Tx.Run()` PODE gravar eventos de mais de um `aggregateType` — a rota "thread via `ctx`" não serve
- SPEC: correcoes-issues-6-8-12
- TASK: M1.1
- DESCRIPTION: [design.md](../specs/correcoes-issues-6-8-12/design.md) §5.1 ("Como `aggregateType` chega a `Append`
  (decisão de M1.1)") registra a decisão do usuário — carimbar `ctx` UMA vez,
  em `codegen/decl_usecase.go`, imediatamente antes de `uow.Run(ctx, ...)` —
  mas condiciona explicitamente essa rota a uma premissa: "Isso só é seguro
  se uma única `Tx.Run()` **nunca** grava eventos de mais de um
  `aggregateType`... **M1.1 deve confirmar essa premissa por leitura**... Se
  a premissa **não** se confirmar (uma `Run()` mistura tipos), essa rota não
  serve — M1.1 para e reporta, não adivinha um fallback." A própria task
  ([M1.1.md](../specs/correcoes-issues-6-8-12/tasks/M1.1.md), banner "Desbloqueada" + Step 2.2) repete a mesma condição.

  Verifiquei por leitura e a premissa é **falsa**: o front-end permite, e o
  codegen hoje gera corretamente, um `UseCase.execute` que dispara `Handle`
  em **dois Aggregates de tipos diferentes**, ambos indo para o **mesmo**
  `tx`/`Run()` — seja porque não há `Database` declarado (caso comum,
  in-memory, como `wallet`), seja porque os dois Aggregates compartilham o
  MESMO `Database` (commit local, sem 2PC).

  **1. A única regra transacional do front-end é por `Database`, nunca por
  tipo de Aggregate.** `sema/rules_crossfile.go:161-198`
  (`checkTransactions`, REQ-5.9) só soma os `Database` distintos tocados por
  um `UseCase` e erra quando são `>1` sem XA universal
  (`len(distinct(dbs)) > 1 && !allXA`). Não existe nenhuma verificação sobre
  QUANTOS TIPOS de Aggregate distintos um `UseCase` toca — a variável `aggs`
  (linha 175, `c.referencedAggregates(uc.Execute)`) já é heterogênea por
  natureza (nomes de Aggregate, não tipos de Database). E a própria função
  documenta, na linha 186, o caso comum como **fora do alcance da regra**:

  ```go
  db := c.prog.DatabaseOfAggregate(agg)
  if db == nil {
      continue // aggregate sem banco declarado: fora do alcance da regra
  }
  ```

  Ou seja: um módulo SEM `Database` declarado (o padrão de `wallet`/`shop`
  hoje, in-memory) nunca aciona a regra cross-database, não importa quantos
  Aggregates DIFERENTES o `UseCase` toque.

  **2. A própria spec da linguagem exemplifica "mesmo Database → commit
  local" sem restringir o tipo de Aggregate.**
  `../steerings/domainscript-spec-v7/19-transactions-sagas.md:7`: "Mesmo
  `Database` | Commit local" — a tabela normativa condiciona só ao `Database`,
  nunca ao tipo. O exemplo do próprio spec/design (`PerformTransfer`, dois
  `load Wallet(...)` + dois dispatches) só usa o MESMO tipo (`Wallet`) duas
  vezes, mas nada na regra exige homogeneidade de tipo — é só a fixture
  ilustrativa que não cobre o caso heterogêneo.

  **3. O codegen já suporta, hoje, dispatch de Handle em Aggregates
  DIFERENTES dentro do mesmo `Run()`.** `decl_usecase.go:427-486`
  (`touchedAggregates`) varre `execute.Stmts` reconhecendo `load Agg(...)` +
  dispatch subsequente para QUALQUER Aggregate do mapa `aggregates` (todos os
  Aggregates do módulo, não um só) — usado por `usecase2PCPlan` exatamente
  para DETECTAR quando 2+ Aggregates de tipos diferentes são tocados. Quando
  esses tipos compartilham o MESMO `Database` (ou nenhum `Database` existe),
  `usecase2PCPlan` devolve `ok=false` (precisa de `len(seen) >= 2` bancos
  DISTINTOS, `decl_usecase.go:399-401`) e o caminho gerado é o de banco único
  — `emitUseCaseDecl:345-346`, um `uow.Run(ctx, func(tx runtime.Tx) error
  {...})` só, onde `l.WithHandleDispatch(aggregates, "tx")`
  (`lower/stmt.go`) despacha CADA `Handle` reconhecido, de QUALQUER
  Aggregate do módulo, no MESMO `tx`.

  **Contraexemplo concreto (não precisei escrever/rodar — é o caminho já
  citado acima ponto a ponto):** um módulo sem `Database` declarado, com dois
  Aggregates `A` e `B`, e

  ```
  UseCase DoBoth handles DoBothCmd {
      execute {
          a = load A(cmd.aId)
          a.SomeHandle(...)
          b = load B(cmd.bId)
          b.OtherHandle(...)
      }
  }
  ```

  - `checkTransactions` não erra (nenhum `Database`, "fora do alcance da
    regra").
  - `usecase2PCPlan` devolve `(nil, false)` (nenhum `Database` XA a somar).
  - `emitUseCaseDecl` emite UM `uow.Run(ctx, func(tx runtime.Tx) error {...
    })`, com dois `tx.Append` dentro — um para o stream de `A`, outro para o
    de `B`.

  Carimbar `ctx = runtime.WithAggregateType(ctx, "<Tipo>")` UMA vez, antes
  desse `Run`, gravaria — incorretamente — `"A"` (ou `"B"`, dependendo de
  qual nome a task escolhesse emitir) no `tenantStream.aggregateType` de
  AMBOS os streams, porque `AggregateTypeFrom(ctx)` dentro de
  `memoryEventStore.Append` não tem como saber qual dos dois `tx.Append`
  daquele mesmo `Run()` está em curso — o `ctx` é o MESMO objeto imutável
  para as duas chamadas. `ListStreams(ctx, "B")` deixaria de enxergar o
  stream de `B` (carimbado como `"A"`), e vice-versa — exatamente o cenário
  que [design.md](../specs/correcoes-issues-6-8-12/design.md) §5.1 identificou como inseguro.

  **4. O caminho 2PC tem o MESMO problema, latente.** `usecase2PCPlan` só
  exige `len(seen) >= 2` `Database` DISTINTOS entre os Aggregates tocados —
  nada impede 3 Aggregates de tipos diferentes onde 2 compartilham o MESMO
  `Database` (colapsando num único `txs[db.Name]`) e o terceiro está em
  outro. O `Tx` desse `Database` compartilhado teria a mesma mistura de
  tipos dentro do mesmo `Run` por-banco.

  **Conclusão.** A rota decidida (`ctx` carimbado uma vez, antes de
  `uow.Run`) só seria segura se o front-end garantisse "um `UseCase` toca no
  máximo um tipo de Aggregate por transação local" — e ele não garante; a
  única fronteira que o front-end (REQ-5.9) e a spec (§19,
  "Mesmo Database → commit local") reconhecem é o `Database`, não o tipo de
  Aggregate. Não implementei a rota: seria exatamente a "adivinhação"
  (aceitar a mistura e carimbar mesmo assim, ou inventar um fallback não
  registrado no design) que o processo deste repositório proíbe. [design.md](../specs/correcoes-issues-6-8-12/design.md)
  §5.1/§7.2 precisa decidir de novo — algumas rotas que preservariam a
  interface `EventStore` intocada (NFR-32) e não exigiriam re-arquitetar
  `Tx`:

  1. Passar o `aggregateType` por CHAMADA, não por `ctx` de todo o `Run` —
     exigiria mudar `Tx.Append(aggregateID string, events []Event)` para
     aceitar um terceiro parâmetro (ou uma variante
     `Tx.AppendTyped(aggregateType, aggregateID string, events []Event)`),
     tocando `runtime.Tx` (`uow.go.txt`) e o call site real em
     `lower/stmt.go:handleDispatchCall` — ambos fora de `target_files` de
     M1.1 hoje.
  2. Restringir a regra do front-end (`checkTransactions`, REQ-5.9, `sema/`)
     para também barrar/isentar o caso heterogêneo — mudaria semântica da
     linguagem (o que hoje compila deixaria de compilar), fora do escopo
     desta spec de correções e do pacote `codegen`.
  3. Alguma outra rota que o design ainda não considerou (ex.: derivar o tipo
     no ponto de `Append` a partir de um registro `aggregateID -> tipo`
     construído no wiring, nunca do `ctx` da chamada).

  Nenhuma das três está em `target_files` de M1.1 nem foi escolhida por
  conta própria.
- SOLVED: []

# Solução sugerida

Passar o `aggregateType` por CHAMADA, não por `ctx` de todo o `Run` — exigiria mudar `Tx.Append(aggregateID string, events []Event)` para
aceitar um terceiro parâmetro (ou uma variante
`Tx.AppendTyped(aggregateType, aggregateID string, events []Event)`),
tocando `runtime.Tx` (`uow.go.txt`) e o call site real em
`lower/stmt.go:handleDispatchCall` — ambos fora de `target_files` de
M1.1 hoje.