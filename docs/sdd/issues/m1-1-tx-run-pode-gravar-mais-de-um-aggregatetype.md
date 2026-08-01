# M1.1: uma `Tx.Run()` PODE gravar eventos de mais de um `aggregateType` — a rota "thread via `ctx`" não serve
- SPEC: [correcoes-issues-6-8-12](../specs/correcoes-issues-6-8-12/requirements.md)
- TASK: [M1.1](../specs/correcoes-issues-6-8-12/tasks/M1.1.md)
- DESCRIPTION: [design.md](../specs/correcoes-issues-6-8-12/design.md) §5.1 ("Como `aggregateType` chega a `Append`
  (decisão de M1.1)") registra a decisão do usuário — carimbar `ctx` UMA vez,
  em [decl_usecase.go](../../../codegen/decl_usecase.go), imediatamente antes de `uow.Run(ctx, ...)` —
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
  tipo de Aggregate.**
  [`sema/rules_crossfile.go:161-198`](../../../sema/rules_crossfile.go#L161-L198)
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
  [`19-transactions-sagas.md:7`](../steerings/domainscript-spec-v7/19-transactions-sagas.md):
  "Mesmo `Database` | Commit local" — a tabela normativa condiciona só ao `Database`,
  nunca ao tipo. O exemplo do próprio spec/design (`PerformTransfer`, dois
  `load Wallet(...)` + dois dispatches) só usa o MESMO tipo (`Wallet`) duas
  vezes, mas nada na regra exige homogeneidade de tipo — é só a fixture
  ilustrativa que não cobre o caso heterogêneo.

  **3. O codegen já suporta, hoje, dispatch de Handle em Aggregates
  DIFERENTES dentro do mesmo `Run()`.**
  [`decl_usecase.go:427-486`](../../../codegen/decl_usecase.go#L427-L486)
  (`touchedAggregates`) varre `execute.Stmts` reconhecendo `load Agg(...)` +
  dispatch subsequente para QUALQUER Aggregate do mapa `aggregates` (todos os
  Aggregates do módulo, não um só) — usado por `usecase2PCPlan` exatamente
  para DETECTAR quando 2+ Aggregates de tipos diferentes são tocados. Quando
  esses tipos compartilham o MESMO `Database` (ou nenhum `Database` existe),
  `usecase2PCPlan` devolve `ok=false` (precisa de `len(seen) >= 2` bancos
  DISTINTOS,
  [`decl_usecase.go:399-401`](../../../codegen/decl_usecase.go#L399-L401)) e o
  caminho gerado é o de banco único —
  [`emitUseCaseDecl:345-346`](../../../codegen/decl_usecase.go#L345-L346), um
  `uow.Run(ctx, func(tx runtime.Tx) error {...})` só, onde
  `l.WithHandleDispatch(aggregates, "tx")`
  ([`lower/stmt.go`](../../../codegen/lower/stmt.go)) despacha CADA `Handle`
  reconhecido, de QUALQUER Aggregate do módulo, no MESMO `tx`.

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
     tocando `runtime.Tx` ([`uow.go.txt`](../../../codegen/rtsrc/uow.go.txt)) e o
     call site real em
     [`lower/stmt.go:handleDispatchCall`](../../../codegen/lower/stmt.go#L2035)
     — ambos fora de `target_files` de M1.1 hoje.
  2. Restringir a regra do front-end (`checkTransactions`, REQ-5.9,
     [`sema/`](../../../sema)) para também barrar/isentar o caso heterogêneo —
     mudaria semântica da
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

# Solução proposta

> Detalhamento da `# Solução sugerida` acima, validado contra o código de hoje.
> Proposta conjunta com
> [m1-1-aggregatetype-nao-chega-a-eventstore-append](m1-1-aggregatetype-nao-chega-a-eventstore-append.md),
> que registra a mesma causa raiz pelo outro lado (nenhuma rota leva o tipo até
> `Append`); o texto essencial é o mesmo dos dois lados.

## Veredito

**Confirmada, e mais forte do que a issue afirma.** Reverificado hoje:

- `checkTransactions`
  ([`sema/rules_crossfile.go:168-209`](../../../sema/rules_crossfile.go#L168-L209)
  — a issue cita 161-198, o corpo deslocou algumas linhas mas o conteúdo é o
  mesmo) só computa `distinct(dbs) > 1 && !allXA` e `distinct(svcs) > 1`. A
  linha 186 continua sendo literalmente `continue // aggregate sem banco
  declarado: fora do alcance da regra`. **Nenhuma** regra do front-end conta
  tipos de Aggregate.
- `emitUseCaseDecl` continua com os dois caminhos:
  [`decl_usecase.go:345-346`](../../../codegen/decl_usecase.go#L345-L346)
  (banco único, `WithHandleDispatch(aggregates, "tx")`) e
  [`:330`](../../../codegen/decl_usecase.go#L330) (2PC,
  `WithHandleDispatchRouted`). Os dois desembocam no **mesmo**
  `handleDispatchCall`
  ([`lower/stmt.go:2035-2085`](../../../codegen/lower/stmt.go#L2035-L2085)),
  que emite `%s.Append(string(%s.id), events)` (`:2082`) para **qualquer**
  Aggregate do mapa — `aggregates` é o mapa do módulo inteiro.
- `usecase2PCPlan` continua exigindo `len(seen) >= 2` **bancos** distintos
  (`:399-401`), então três Aggregates com dois deles no mesmo `Database`
  colapsam num `txs[db]` só — o ponto 4 da issue.
- `grep` de `aggregateType`/`WithAggregateType` sobre `*.go`/`*.txt`/`*.golden`
  (fora de `docs/`): **zero** ocorrências. Nada foi implementado.

Uma precisão que a issue não faz e vale registrar: **nenhuma fixture exercita
hoje o caso heterogêneo**. Os 9 `UseCase` de `testdata/projects/` despacham
`Handle` num tipo só cada — `PlaceOrder` de `pizzeria/sales`
([`application.ds:61-93`](../../../testdata/projects/pizzeria/sales/application.ds#L61-L93))
chega perto (`load MenuItem` + `load Order`), mas só **escreve** em `Order`. O
defeito é latente e permitido, não observado — o que não muda o veredito: a
rota do `ctx` é insegura por construção, e implementá-la seria gravar dado
errado esperando que nenhum programa futuro use uma forma que a linguagem
permite.

**Contexto novo, favorável à sua direção:** a
[§4.2.3](../steerings/domainscript-spec-v7/04-domain-core.md) (spec revisada em
2026-07-31) normatiza que `aggregateId` é derivado do **emissor**, com
**emissor único por `Event`**. Isto é, o tipo é propriedade da emissão —
granularidade de chamada, nunca de transação. A spec da linguagem passou a
dizer, em termos normativos, exatamente o que esta issue provou por leitura do
código: `Run` é o escopo errado.

## Causa raiz

O `ctx` de `memoryTx` é fixado uma única vez, em
`memoryUnitOfWork.Run(ctx, fn)`
([`codegen/rtsrc/uow.go.txt:109-110`](../../../codegen/rtsrc/uow.go.txt#L109-L110)),
enquanto o `aggregateType` varia por `Append` — carimbar no `ctx` do `Run`
casa um dado de granularidade de chamada num carregador de granularidade de
transação.

## Solução proposta

Sua direção — tipo por chamada — está correta e é a única que fecha as duas
issues. Falta-lhe uma peça: `Tx` receber o tipo não basta, porque quem grava é
`EventStore.Append`, cuja assinatura não pode mudar (NFR-32). A peça é
**realocar** o `ctx` que o [design.md](../specs/correcoes-issues-6-8-12/design.md)
§5.1 já decidiu: ele sai de `decl_usecase.go` (uma vez por `Run`, incorreto) e
entra em `memoryTx.Append` (uma vez por chamada, correto). O mecanismo de §5.1
não é descartado — muda só quem o invoca, do código gerado para o runtime.

1. **`codegen/rtsrc/uow.go.txt`** — `Tx.Append` passa a
   `Append(aggregateType, aggregateID string, events []Event) error`. Entre as
   duas formas que sua `# Solução sugerida` oferece, recomendo o **terceiro
   parâmetro no método existente** em vez de um irmão `AppendTyped`: manter os
   dois deixa de pé um caminho não-tipado, que grava stream invisível a
   `ListStreams` — a classe de miscompilação silenciosa que NFR-33 proíbe. A
   diferença de custo é 16 call sites, todos nossos e mecânicos (contagem
   abaixo).
2. **`memoryTx.Append` (`:135-141`)** — deriva o ctx **por chamada**:
   `tx.store.Append(WithAggregateType(tx.ctx, aggregateType), aggregateID,
   events)`. `tx.ctx` continua o do `Run`; o valor do tipo nunca atravessa duas
   chamadas, e o contraexemplo de dois Aggregates no mesmo `Run` passa a gravar
   dois streams com tipos distintos.
3. **`codegen/rtsrc/contextkeys.go.txt`** — o par
   `WithAggregateType`/`AggregateTypeFrom`, forma idêntica a
   `WithTenant`/`TenantFrom` (`:53-63`), como §5.1 já previa.
4. **`codegen/rtsrc/eventstore.go.txt`** — `memoryEventStore.Append` lê
   `AggregateTypeFrom(ctx)` e carimba `tenantStream.aggregateType` no mesmo
   `if ts == nil` que já carimba `tenantID` (`:75-79`); ausência de valor =
   campo vazio, nunca erro. `StreamLister`/`ListStreams` seguem como o design
   já especificou.
5. **`codegen/sqlrt/uow.go.txt`** — `sqlruntime.Tx.Append` (`:27-33`)
   acompanha a assinatura, obrigatoriamente: é a segunda e última
   implementação de `runtime.Tx` do repositório, e `twophase.go.txt:86` reusa o
   mesmo struct. Ignora o parâmetro com doc-comment explícito (`sqlrt` não
   implementa `StreamLister` neste ciclo, design §4.1).
6. **`codegen/lower/stmt.go:2082`** — a única linha do gerador que emite
   `Append` passa a `%s.Append(%q, string(%s.id), events)`, com `shape.Name`
   (já em mãos, `:2039-2046`). Como os dois caminhos de `decl_usecase.go`
   compartilham `handleDispatchCall`, **uma** linha corrige o caminho de banco
   único **e** o 2PC — inclusive o cenário do ponto 4 desta issue, em que dois
   Aggregates de tipos diferentes compartilham o mesmo `txs[db]`.
   `decl_usecase.go` **sai** de `target_files`: só o doc-comment de `:40`
   precisa acompanhar.

## Alternativas descartadas

- **Opção 1 desta issue (o que já está proposto acima), sem a realocação do
  `ctx`.** Incompleta: `Tx` receberia o tipo e não teria como entregá-lo a
  `EventStore.Append` sem mudar `EventStore` (proibido por NFR-32) ou
  type-assertar o store dentro do `Tx` (dobra a superfície sem ganho sobre o
  ctx derivado).
- **Opção 2 — restringir `checkTransactions` para barrar o caso
  heterogêneo.** Mudaria a semântica da linguagem: o que hoje compila deixaria
  de compilar, e a §19 condiciona commit local ao `Database`, nunca ao tipo. Um
  ciclo de correções de `codegen` não decide isso, e a §4.2.3 acabou de
  confirmar que a linguagem **espera** múltiplos emissores por transação (é o
  `Event` que tem emissor único, não a transação).
- **Opção 3 — registro `aggregateID → tipo` construído no wiring.** Não é
  necessário; e a variante mais próxima disso que a §4.2.3 agora licencia é um
  mapa `eventType → aggregateType` (emissor único torna a função total). Perde
  mesmo assim: exige emitir o mapa em `codegen/decl_event.go`, entregá-lo ao
  store em `generateCmdMainFile`, e depende de o front-end **enforçar** a regra
  de emissor único da §4.2.3 — regra que não existe em `sema/` hoje. Sem ela, o
  mapeamento seria arbitrário para um programa com dois emissores: adivinhação.
- **Carimbar `ctx` uma vez antes de `uow.Run` (a rota que esta issue derrubou).**
  Mantida como descartada.
- **`aggregateID` prefixado (`"<Tipo>:<id>"`).** Muda o formato do id
  armazenado — quebra REQ-55.6 (byte-identidade de Query já suportada) e todo
  `given Subject from [...]`, e vaza sintaxe de chave para o `Load`.
- **Mudar `NewMemoryEventStore()` para receber um registro.** O construtor
  aparece em 35 arquivos de código/fixture/golden (`grep -rl` dá 39, dos quais
  4 são markdown). A rota escolhida não encosta em nenhum deles — este é o
  ganho principal dela.

## Raio de alcance

- **Implementações de `runtime.Tx`: exatamente 2**, ambas vendorizadas
  (`codegen/rtsrc/uow.go.txt`, `codegen/sqlrt/uow.go.txt`). Nenhum dublê de
  teste implementa `Tx`. Dos seis adaptadores opt-in (`amqprt`, `grpcrt`,
  `otelrt`, `redisrt`, `s3rt`, `sqlrt`), só `sqlrt` menciona `runtime.Tx`.
- **Call sites de `Tx.Append` a atualizar: 16 em 7 arquivos** —
  `codegen/rtsrc/runtime_test.go.txt` (7), `codegen/sql_outbox_test.go` (2),
  `sql_outbox_relay_test.go` (1), `sql_outbox_cleanup_test.go` (1),
  `sql_outbox_channel_test.go` (1), `codegen/decl_aggregate_load_test.go` (2),
  `driver/generate_e2e_wallet_test.go` (2). Os quatro `sql_outbox*` são
  **strings de Go embarcadas**, compiladas em projeto temporário — invisíveis a
  `go build ./...`.
- **Goldens: 5 arquivos, 7 linhas** com `tx.Append` (`usecase_deposit`,
  `usecases_wallet` ×2, `usecase_increment_idempotent`,
  `usecase_increment_idempotent_reject`, `filestorage_usecases` ×2), de 56
  goldens. Mais `codegen/decl_usecase_test.go:169`, que assevera a string
  literal `"tx.Append(string(wallet.id), events)"`.
- **Fixtures: 3** (`wallet`, `shop`, `pizzeria`), 9 `UseCase` no total,
  regerados do zero pelo job `fixtures` — nenhum byte versionado a editar ali,
  só o `go build`/`go vet` da saída a manter verde.
- **CI:** jobs `test` e `fixtures` (com `pizzeria` em `KNOWN_UNGENERATABLE`).
  Nenhum job novo.
- **Armadilha de validação:** `codegen/rtsrc/*.txt` e `codegen/sqlrt/*.txt`
  **não** entram em `go build ./...` (verificado hoje: build verde, e
  continuaria verde com `sqlruntime.Tx` quebrada). O TEST-4 de M1.1 não prova o
  que promete; a guarda real é `codegen/rtsrc/rtsrc_test.go`
  (`TestSourcesSmokeCompileAndVet`, `TestSourcesBehavioralTestsPass`) mais os
  `sql_*_test.go`.
- **`target_files` de M1.1 está errado em dois pontos:** os testes
  comportamentais do runtime moram em `codegen/rtsrc/runtime_test.go.txt`
  (1983 linhas), não em `codegen/rtsrc/rtsrc_test.go` (145, só o harness que
  materializa e roda) — TEST-1/2/3 não cabem no arquivo listado; e
  `codegen/decl_usecase.go` deixa de ser necessário.

## Bloqueios

Nenhum na **spec da linguagem**: a §4.2.3 licencia a rota e ainda fixa qual
valor emitir (o Aggregate emissor, único por `Event` — literalmente
`shape.Name` em `handleDispatchCall`). O que falta é decisão do
`design.md`/`requirements.md` **deste ciclo**:

1. **NFR-31 precisa ser emendado.** "`wallet` e `shop` permanecem
   byte-idênticos" é incompatível com **qualquer** rota que carregue o tipo a
   partir do código gerado — inclusive a do `ctx` antes do `Run`, que também
   acrescentaria linha. Os 9 `UseCase` das fixtures mudam de bytes; a
   requirements.md precisa enumerar o que pode mudar (a linha de `Append` de
   todo `UseCase`) em vez de prometer identidade total.
2. **O escopo de NFR-32 precisa ficar explícito.** Ao pé da letra ele fala só
   de `runtime.EventStore` ("`sqlrt.EventStore` e os dublês de teste em
   `codegen/`"), e a rota **não** o viola: `EventStore` fica intocada, e
   `countingStore`/`flakyStore`/`gatedStore` continuam compilando sem uma linha
   alterada. Mas `runtime.Tx` **é** interface e **muda** — o design tem de
   registrar isso, junto com a guarda de smoke-compile que substitui o TEST-4.
3. **Terceiro parâmetro vs. `AppendTyped` irmão** — sua `# Solução sugerida`
   deixa as duas em aberto; a decisão precisa ficar escrita. Recomendo o
   terceiro parâmetro, por NFR-33.
4. **Semeadura de `*.test.ds`.** `emitUseCaseGiven`
   ([`codegen/gentest.go:986`](../../../codegen/gentest.go#L986)) semeia com
   `store.Append(context.Background(), id, events)`, fora de qualquer `Tx` —
   stream sem tipo, invisível a `ListStreams`. Não bloqueia M1.1, mas bloqueia
   testar `list <Aggregate>` em `*.test.ds`. Conserto barato (o nome do
   Aggregate está no `Subject`, hoje descartado por `ucSubjectID`), em fatia
   própria — ou limitação registrada.
5. **Acoplamento com REQ-59 (staging, M4.2).** M4.2 fará `memoryTx.Append`
   bufferizar e só aplicar no commit; o registro bufferizado precisa carregar o
   `aggregateType` junto do `aggregateID`, senão o carimbo se perde no flush.
   Qualquer que seja a ordem das duas tasks, a que vier depois tem de saber
   disso.
6. **Nota para o futuro, sem ação agora:** a §5.3 (`ApplicationEvent` — evento
   de escopo de requisição, **sem** `aggregateId`/`sequence`) não pode passar
   por este caminho quando for implementada. Nada em `codegen/` a implementa
   hoje (`grep ApplicationEvent` sobre `*.go` → zero).

## Fatiamento sugerido

1. **M1.1a — `runtime.Tx` carrega o tipo por chamada.** `target_files`:
   `codegen/rtsrc/uow.go.txt`, `codegen/rtsrc/contextkeys.go.txt`,
   `codegen/sqlrt/uow.go.txt`, `codegen/rtsrc/runtime_test.go.txt`,
   `codegen/sql_outbox_test.go`, `codegen/sql_outbox_relay_test.go`,
   `codegen/sql_outbox_cleanup_test.go`, `codegen/sql_outbox_channel_test.go`,
   `codegen/decl_aggregate_load_test.go`,
   `driver/generate_e2e_wallet_test.go`. Assinatura + as 2 implementações + os
   16 call sites no mesmo commit (senão não compila). Nada em `codegen/`
   consome ainda. DoD: `TestSourcesSmokeCompileAndVet` +
   `TestSourcesBehavioralTestsPass`.
2. **M1.1b — carimbo em `tenantStream` e `StreamLister`.** `target_files`:
   `codegen/rtsrc/eventstore.go.txt`, `codegen/rtsrc/runtime_test.go.txt`.
   `Append` lê `AggregateTypeFrom(ctx)`; `ListStreams` filtra por tipo, aplica
   `tenantVisible` e ordena. TEST-1/TEST-2/TEST-3 da M1.1 atual moram aqui.
   Depende de M1.1a.
3. **M1.1c — o gerador emite a forma nova.** `target_files`:
   `codegen/lower/stmt.go`, `codegen/decl_usecase.go` (só doc-comment),
   `codegen/decl_usecase_test.go`, e os 5 goldens
   (`usecase_deposit`, `usecases_wallet`, `usecase_increment_idempotent`,
   `usecase_increment_idempotent_reject`, `filestorage_usecases`). **Par
   NFR-4 desta issue:** o teste positivo é um `UseCase` que despacha `Handle`
   em dois Aggregates de tipos diferentes no mesmo `Run` e produz dois `Append`
   com tipos **distintos** — o contraexemplo desta issue vira regressão
   permanente. Depende de M1.1b.
4. **M1.1d (opcional, habilita M1.2/M1.3 sob teste) — semeadura tipada no
   `given` de UseCase.** `target_files`: `codegen/gentest.go`,
   `codegen/testdata/tests_wallet.go.golden`. `emitUseCaseGiven` passa a
   embrulhar o ctx com o tipo tirado da cabeça do `Subject`. Depende de M1.1b.