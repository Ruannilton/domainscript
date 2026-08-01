# M1.1: nenhuma rota leva o `aggregateType` até `EventStore.Append` dentro do escopo da task
- SPEC: [correcoes-issues-6-8-12](../specs/correcoes-issues-6-8-12/requirements.md)
- TASK: [M1.1](../specs/correcoes-issues-6-8-12/tasks/M1.1.md)
- DESCRIPTION: [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.1 (Componentes e Contratos) descreve o seam
  `StreamLister` e exige que `tenantStream` ganhe um campo `aggregateType`,
  "carimbado no primeiro `Append` do stream — pelo mesmo mecanismo e no mesmo
  ponto onde `tenantID` já é carimbado hoje", e delega a M1.1 decidir **como**
  esse tipo chega até `Append`. A própria task ([M1.1.md](../specs/correcoes-issues-6-8-12/tasks/M1.1.md), Step 2)
  prescreve duas rotas, nessa ordem:

  1. "derivá-lo do `EventType()` do primeiro evento via um **registro já
     disponível**";
  2. se isso não for possível sem tocar a assinatura de `Append` (que é
     `EventStore` e não pode mudar, NFR-32), "usar o seam que o
     `Event`/`EventMeta` **já oferece**";
  3. "Se nenhuma rota funcionar sem alterar `EventStore`, PARE e reporte —
     não altere a interface."

  Verifiquei as duas rotas por leitura de código; nenhuma existe hoje dentro
  de `target_files`
  ([`codegen/rtsrc/eventstore.go.txt`](../../../codegen/rtsrc/eventstore.go.txt),
  [rtsrc_test.go](../../../codegen/rtsrc/rtsrc_test.go)):

  **Rota 1 — "registro já disponível" não existe.**
  - `Event` ([`codegen/rtsrc/event.go.txt:6-18`](../../../codegen/rtsrc/event.go.txt#L6-L18))
    só expõe `EventType() string` e `SetMeta(EventMeta)`. `EventType()`
    devolve o nome do PRÓPRIO evento (ex. `"TicketCreated"`), não o nome do
    Aggregate — é literalmente `decl.Name` do `EventDecl`, emitido em
    [`codegen/decl_event.go:181`](../../../codegen/decl_event.go#L181)
    (`func (*%s) EventType() string { return %q }`).
    Não há convenção de prefixo (`"KitchenTicket.TicketCreated"` ou
    equivalente) que permita derivar o tipo do Aggregate a partir do nome do
    evento.
  - O único mapa "nome de evento → algo" que existe é `eventRegistry`
    (`map[string]func() runtime.Event`), gerado **por módulo**
    ([`codegen/decl_event.go:201-211`](../../../codegen/decl_event.go#L201-L211),
    exposto via `EventRegistry()`) — mapeia para um construtor de evento, não
    para um Aggregate, e vive no pacote Go do módulo declarante (ex.
    `kitchen`), não no pacote `runtime` (`codegen/rtsrc/`). Importar esse
    registro de volta para `runtime` criaria um ciclo (`kitchen` já importa
    `runtime`); hoje ele só é entregue a
    `sqlrt.NewEventStore`/`NewUnitOfWork` pelo wiring gerado
    (`cmd/<service>/main.go`, [codegen.go](../../../codegen/codegen.go)) —
    fora de `target_files` de M1.1.
  - `grep -rn "AggregateType\|aggregateType"` em todo o repositório (fora
    desta spec) não devolve nenhuma ocorrência em código Go ou `rtsrc/*.txt`
    — confirmando que esse registro não existe em lugar nenhum, não só fora
    do escopo.

  **Rota 2 — "seam que `Event`/`EventMeta` já oferece" também não existe.**
  `EventMeta` ([`codegen/rtsrc/event.go.txt:26-36`](../../../codegen/rtsrc/event.go.txt#L26-L36))
  só carrega `AggregateID`/`Sequence`/`Timestamp`; nenhum campo de tipo de
  Aggregate. E é o próprio `Append` quem CONSTRÓI o `EventMeta`
  ([`eventstore.go.txt:83-88`](../../../codegen/rtsrc/eventstore.go.txt#L83-L88))
  — não há como ele "oferecer" um dado que `Append` mesmo produz a partir do
  nada.

  **A rota análoga que existe (tenantID) está fora do escopo.** `tenantID` é
  carimbado do mesmo jeito que o [design.md](../specs/correcoes-issues-6-8-12/design.md) pede para `aggregateType`, mas
  chega via `ctx` (`WithTenant`/`TenantFrom`,
  [`codegen/rtsrc/contextkeys.go.txt`](../../../codegen/rtsrc/contextkeys.go.txt))
  — um arquivo que M1.1 não toca. Copiar esse mecanismo para `aggregateType`
  exigiria (a) um novo par `WithAggregateType`/`AggregateTypeFrom` em
  `contextkeys.go.txt`, e (b) um call site que o invoque ANTES de `Append` —
  hoje inexistente: `memoryTx.Append`
  ([`codegen/rtsrc/uow.go.txt:135-141`](../../../codegen/rtsrc/uow.go.txt#L135-L141))
  só repassa `tx.ctx`/`aggregateID`/`events`, sem tocar o tipo; quem monta
  essa chamada é
  [`codegen/lower/stmt.go:2082`](../../../codegen/lower/stmt.go#L2082)
  (`tx.Append(string(id), events)`), também fora de `target_files`.

  **Mudar a assinatura do construtor também não é opção dentro do escopo.**
  `NewMemoryEventStore()` hoje não recebe parâmetro nenhum; adicionar um
  registro/opção a ela evitaria mexer na interface `EventStore`, mas
  `NewMemoryEventStore()` é chamado em 36 arquivos (`grep -rl
  NewMemoryEventStore`), nenhum deles em `target_files` de M1.1 — mudar a
  assinatura sem tocar os call sites não compila.

  **Conclusão.** As duas rotas que [design.md](../specs/correcoes-issues-6-8-12/design.md)/[M1.1.md](../specs/correcoes-issues-6-8-12/tasks/M1.1.md) prescrevem não
  existem no código hoje, e a única rota que existe (thread via `ctx`, o
  mesmo mecanismo de `tenantID`) exige tocar arquivos fora de
  `target_files` (`contextkeys.go.txt` e o call site de `Append`
  em `uow.go.txt`/`lower/stmt.go`). Isso é exatamente a cláusula 3 da própria
  task ("Se nenhuma rota funcionar sem alterar `EventStore`, PARE e
  reporte"). **Pedido de decisão de design**, não defeito de código: [design.md](../specs/correcoes-issues-6-8-12/design.md)
  §4.1/§5.1 (e o Step 2 de [M1.1.md](../specs/correcoes-issues-6-8-12/tasks/M1.1.md)) precisam decidir e registrar
  explicitamente UMA de:

  1. Ampliar `target_files` de M1.1 para incluir `contextkeys.go.txt` e o(s)
     call site(s) de `Append` que precisam chamar `WithAggregateType` antes
     de gravar (thread via `ctx`, mesmo padrão de `tenantID`);
  2. Aceitar uma convenção de `aggregateID` prefixado
     (`"<AggregateType>:<id>"`) estampada pelo CALLER e desempacotada por
     `ListStreams`/`Append` — muda o formato do id armazenado, precisa ser
     avaliada contra REQ-55.6 (byte-identidade da saída de Queries já
     suportadas) e contra qualquer leitor existente de `aggregateID` cru;
  3. Alguma outra rota que o design ainda não considerou.

  Não implementei nenhuma das três por conta própria — escolher uma delas
  agora seria exatamente a adivinhação que o processo deste repositório
  proíbe ([CLAUDE.md](../../../CLAUDE.md), "A fronteira do spec da linguagem é parada, não
  adivinhação" — mesmo espírito aplicado aqui a uma lacuna do [design.md](../specs/correcoes-issues-6-8-12/design.md)
  desta spec, não do spec da linguagem).
- SOLVED: [decisão do usuário — opção 1 (thread via `ctx`, mesmo padrão de
  `tenantID`) — registrada em [design.md](../specs/correcoes-issues-6-8-12/design.md) §5.1/§7.2 e em [M1.1.md](../specs/correcoes-issues-6-8-12/tasks/M1.1.md)
  (`status` volta a `pending`, `target_files` ampliado com
  `contextkeys.go.txt` e [decl_usecase.go](../../../codegen/decl_usecase.go), que é o call site real de
  `uow.Run(ctx, ...)` — `lower/stmt.go`, citado nesta issue, não é. A
  implementação da rota fica condicionada a M1.1 confirmar, por leitura,
  que uma `Tx.Run()` nunca grava eventos de mais de um `aggregateType`;
  ver a nota na própria task]

# Solução proposta

> Proposta conjunta com
> [m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype](m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype.md),
> que derrubou a rota escolhida no `SOLVED` acima. As duas issues têm a mesma
> causa raiz e uma só correção; o texto abaixo é o mesmo dos dois lados, com o
> enquadramento de cada uma.

## Veredito

**Ainda real, e o `SOLVED` acima está obsoleto.** Reverificado hoje contra o
código, item por item:

- `runtime.EventStore` continua com exatamente dois métodos, ambos por
  `aggregateID`
  ([`codegen/rtsrc/eventstore.go.txt:27-38`](../../../codegen/rtsrc/eventstore.go.txt#L27-L38));
  `tenantStream` continua guardando só `tenantID`/`events` (`:45-48`), e é o
  próprio `Append` quem monta o `EventMeta` (`:81-89`).
- `grep -rn "AggregateType\|aggregateType"` sobre `*.go`/`*.txt`/`*.golden`
  (excluindo `docs/`) devolve **zero** ocorrências; `WithAggregateType`,
  **zero**. Nenhuma das duas rotas de M1.1 Step 2 ganhou existência.
- `EventType()` continua emitindo o nome **simples** do evento
  ([`codegen/decl_event.go:181`](../../../codegen/decl_event.go#L181)) e
  `eventRegistry` continua sendo `nome → construtor`, por módulo
  (`:202-212`).
- A rota decidida no `SOLVED` (carimbar `ctx` uma vez antes de `uow.Run`) é
  **insegura**: `checkTransactions`
  ([`sema/rules_crossfile.go:168-209`](../../../sema/rules_crossfile.go#L168-L209))
  restringe por `Database` e por service, nunca por tipo de Aggregate.

O que mudou desde que a issue foi escrita, e muda a análise: a
[§4.2.3](../steerings/domainscript-spec-v7/04-domain-core.md) da spec da
linguagem (revisada em 2026-07-31) agora **normatiza** o envelope e decide que
`aggregateId` é derivado do emissor, com **emissor único por `Event`** (dois
Aggregates emitindo o mesmo `Event` → erro de compilação). Isso torna o par
`(evento, tipo de Aggregate)` um fato estático do programa — e, mais
importante para esta issue, confirma que o tipo **existe e é único no ponto de
emissão**, que é exatamente onde o gerador está quando escreve o `Append`.

## Causa raiz

O tipo do Aggregate é conhecido estaticamente no **único** ponto em que o
gerador emite um `Append`
([`codegen/lower/stmt.go:2035-2085`](../../../codegen/lower/stmt.go#L2035-L2085),
onde `shape.Name` **é** o nome do Aggregate, linha 2039-2046), mas o seam
`Tx.Append(aggregateID, events)` não tem por onde carregá-lo, e o `ctx` de
`memoryTx` é fixado uma vez por `Run` — granularidade grossa demais para um
dado que varia por chamada.

## Solução proposta

Adotar a direção do dono — **passar o `aggregateType` por chamada** — com uma
peça que a torna completa: o `ctx` decidido em
[design.md](../specs/correcoes-issues-6-8-12/design.md) §5.1 **não é
descartado, é realocado** de `decl_usecase.go` (uma vez por `Run`, incorreto)
para dentro de `memoryTx.Append` (uma vez por chamada, correto). Sem isso a
proposta do dono fica pela metade: `Tx` recebe o tipo mas não tem como
entregá-lo a `EventStore.Append`, cuja assinatura não pode mudar (NFR-32).

1. **`codegen/rtsrc/uow.go.txt`** — `Tx.Append` passa a
   `Append(aggregateType, aggregateID string, events []Event) error`.
   Recomendo **terceiro parâmetro no método existente**, não um irmão
   `AppendTyped`: manter os dois deixa um caminho não-tipado que grava stream
   invisível a `ListStreams` — exatamente a classe de miscompilação silenciosa
   que NFR-33 proíbe. O custo da diferença é 16 call sites em 7 arquivos, todos
   nossos e mecânicos (contagem em "Raio de alcance").
2. **`memoryTx.Append` (mesmo arquivo, `:135-141`)** — deriva o ctx **por
   chamada**: `tx.store.Append(WithAggregateType(tx.ctx, aggregateType),
   aggregateID, events)`. `tx.ctx` continua o do `Run`; o valor do tipo nunca
   atravessa duas chamadas.
3. **`codegen/rtsrc/contextkeys.go.txt`** — o par
   `WithAggregateType`/`AggregateTypeFrom`, forma idêntica a
   `WithTenant`/`TenantFrom` (`:53-63`). Continua sendo o mecanismo que §5.1
   escolheu; muda só quem o chama — o runtime, não o código gerado.
4. **`codegen/rtsrc/eventstore.go.txt`** — `memoryEventStore.Append` lê
   `AggregateTypeFrom(ctx)` e carimba `tenantStream.aggregateType` no mesmo
   `if ts == nil` que já carimba `tenantID` (`:75-79`). Ausência de valor =
   campo vazio, nunca erro (chamador não-gerado). `StreamLister`/`ListStreams`
   seguem como o design já especificou (interface opcional, filtro
   `tenantVisible`, ids ordenados).
5. **`codegen/sqlrt/uow.go.txt`** — `sqlruntime.Tx.Append` (`:27-33`)
   acompanha a assinatura, **obrigatoriamente**: é a segunda e última
   implementação de `runtime.Tx` no repositório, e `twophase.go.txt:86` reusa o
   mesmo struct. Ignora o parâmetro com doc-comment explícito ("`sqlrt` não
   implementa `StreamLister` neste ciclo", design §4.1).
6. **`codegen/lower/stmt.go:2082`** — a única linha do gerador que emite
   `Append`, passa a emitir
   `%s.Append(%q, string(%s.id), events)` com `shape.Name`. Vale para os
   **dois** caminhos de `decl_usecase.go` — o de banco único
   ([`:345-346`](../../../codegen/decl_usecase.go#L345-L346),
   `WithHandleDispatch(aggregates, "tx")`) e o 2PC
   ([`:330`](../../../codegen/decl_usecase.go#L330),
   `WithHandleDispatchRouted`) — porque os dois passam pelo mesmo
   `handleDispatchCall`. `decl_usecase.go` **sai** de `target_files`: não
   precisa de mudança de código, só do doc-comment de `:40`.

## Alternativas descartadas

- **Rota 1 de M1.1 Step 2 — "registro já disponível".** Não existe hoje
  (verificado de novo). A §4.2.3 a torna *possível em princípio* — emissor
  único por `Event` significa que um mapa `eventType → aggregateType` é total e
  bem definido —, mas construí-lo exige (a) emitir o mapa em
  `codegen/decl_event.go`, (b) entregá-lo ao store no wiring
  (`generateCmdMainFile`), (c) que o front-end **enforce** a regra de emissor
  único da §4.2.3, que hoje não existe em `sema/`. Sem (c), um programa com
  dois emissores produziria um mapeamento arbitrário — a adivinhação que o
  processo proíbe. Perde por custo e por dependência de regra não implementada,
  não por impossibilidade. (Nota lateral: a chave desse mapa seria o nome
  simples, e a §4.2.3 exige `eventType` **qualificado** — divergência real de
  `decl_event.go:181`, matéria de issue própria.)
- **Rota 2 — "seam que `Event`/`EventMeta` já oferece".** Continua inexistente:
  `EventMeta` só tem `AggregateID`/`Sequence`/`Timestamp`, e é `Append` quem o
  constrói. Nada a oferecer.
- **Opção 1 do pedido original (`ctx` carimbado antes de `uow.Run`).** Refutada
  por [m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype](m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype.md):
  um `UseCase` pode despachar `Handle` em dois Aggregates de tipos diferentes
  no mesmo `Run` sem violar regra nenhuma do front-end.
- **Opção 2 — `aggregateID` prefixado (`"<Tipo>:<id>"`).** Muda o formato do id
  armazenado, portanto muda os bytes de toda Query já suportada (REQ-55.6) e
  todo `given Subject from [...]` de `*.test.ds`, além de vazar sintaxe de
  chave para o `Load` de qualquer leitor. Custo muito maior que a mudança de
  seam, e irreversível no dado gravado.
- **Mudar `NewMemoryEventStore()` para receber um registro.** Evitaria tocar
  `Tx`, mas o construtor aparece em **35** arquivos de código/fixture/golden
  (`grep -rl` dá 39, dos quais 4 são markdown). A rota escolhida não encosta em
  nenhum deles.

## Raio de alcance

Contagens verificadas hoje:

- **Implementações de `runtime.Tx`: exatamente 2**, ambas vendorizadas
  (`codegen/rtsrc/uow.go.txt`, `codegen/sqlrt/uow.go.txt`). Nenhum dublê de
  teste implementa `Tx` — os testes sempre usam `uow.Run(ctx, func(tx
  runtime.Tx) error)` com a implementação real. Dos seis adaptadores opt-in
  (`amqprt`, `grpcrt`, `otelrt`, `redisrt`, `s3rt`, `sqlrt`), só `sqlrt`
  menciona `runtime.Tx`.
- **Call sites de `Tx.Append` a atualizar (se a assinatura muda): 16 em 7
  arquivos** — `codegen/rtsrc/runtime_test.go.txt` (7),
  `codegen/sql_outbox_test.go` (2), `sql_outbox_relay_test.go` (1),
  `sql_outbox_cleanup_test.go` (1), `sql_outbox_channel_test.go` (1),
  `codegen/decl_aggregate_load_test.go` (2),
  `driver/generate_e2e_wallet_test.go` (2). Os quatro `sql_outbox*` são
  **strings de código Go embarcadas**, escritas em projeto temporário e
  compiladas lá — não pegam em `go build ./...`.
- **Goldens: 5 arquivos, 7 linhas** contêm `tx.Append`
  (`usecase_deposit`, `usecases_wallet` ×2, `usecase_increment_idempotent`,
  `usecase_increment_idempotent_reject`, `filestorage_usecases` ×2), de 56
  goldens no total. Mais `codegen/decl_usecase_test.go:169`, que assevera a
  string literal `"tx.Append(string(wallet.id), events)"`.
- **Fixtures de `testdata/projects/`: 3** (`wallet`, `shop`, `pizzeria`), com 9
  `UseCase` no total — todos regerados do zero pelo job `fixtures` do CI, então
  não há bytes versionados a editar ali; o que precisa continuar passando é o
  `go build`/`go vet` sobre a saída.
- **CI:** job `test` (`gofmt`/`vet`/`build`/`test`) e job `fixtures`
  (`dsc check` + `dsc gen` + `go build`/`go vet` por projeto, com `pizzeria` em
  `KNOWN_UNGENERATABLE`). Nenhum job novo.
- **Armadilha de validação:** `codegen/rtsrc/*.txt` e `codegen/sqlrt/*.txt`
  **não** são compilados por `go build ./...` (verificado: `go build ./...`
  está verde hoje e continuaria verde com `sqlruntime.Tx` quebrada). O TEST-4
  de M1.1 ("`go build ./...` limpo") **não prova** o que a task diz provar; a
  guarda real é `gentest.SmokeCompile` / `codegen/rtsrc/rtsrc_test.go`
  (`TestSourcesSmokeCompileAndVet`, `TestSourcesBehavioralTestsPass`) e os
  `sql_*_test.go`.
- **`target_files` de M1.1 está errado em dois pontos:** (a) os testes
  comportamentais do runtime moram em `codegen/rtsrc/runtime_test.go.txt`
  (1983 linhas), não em `codegen/rtsrc/rtsrc_test.go` (145 linhas, só o
  harness que materializa e roda) — TEST-1/2/3 não cabem no arquivo listado;
  (b) `codegen/decl_usecase.go` entrou por causa da rota de `ctx` e deixa de
  ser necessário.

## Bloqueios

Nenhum na **spec da linguagem** — a §4.2.3 licencia e ainda restringe o que se
emite (o valor a passar é o Aggregate emissor, único por `Event`, que é
literalmente `shape.Name` em `handleDispatchCall`). O que falta decidir é do
`design.md`/`requirements.md` **deste ciclo**:

1. **NFR-31 precisa ser emendado.** "`wallet` e `shop` permanecem
   byte-idênticos" é incompatível com **qualquer** rota que carregue o tipo a
   partir do código gerado: os 9 `UseCase` das fixtures mudam de bytes. A
   requirements.md tem de enumerar o que pode mudar (a linha de `Append` de
   todo `UseCase`, e nada mais) em vez de prometer identidade total.
2. **O escopo de NFR-32 precisa ficar explícito.** Lido ao pé da letra, ele
   fala só de `runtime.EventStore` ("`sqlrt.EventStore` e os dublês de teste em
   `codegen/`"), e a rota **não** o viola: `EventStore` fica intocada e os três
   dublês (`countingStore`, `flakyStore`, `gatedStore`) continuam compilando
   sem uma linha alterada. Mas `runtime.Tx` **é** interface e **muda** — o
   design tem de dizer isso em letras, junto com a guarda de smoke-compile que
   o TEST-4 atual não dá.
3. **Terceiro parâmetro vs. `AppendTyped` irmão** — decisão a registrar. Minha
   recomendação é o terceiro parâmetro, pelo motivo de NFR-33 acima.
4. **Semeadura de `*.test.ds`.** `emitUseCaseGiven`
   ([`codegen/gentest.go:986`](../../../codegen/gentest.go#L986)) semeia via
   `store.Append(context.Background(), id, events)`, fora de qualquer `Tx` —
   stream sem tipo, invisível a `ListStreams`. Não bloqueia M1.1, mas bloqueia
   testar `list <Aggregate>` em `*.test.ds`. O conserto é barato (o nome do
   Aggregate está no `Subject`, hoje descartado por `ucSubjectID`) e cabe numa
   fatia própria — ou vira limitação registrada.
5. **Nota para o futuro, sem ação agora:** a §5.3 (`ApplicationEvent`, evento de
   escopo de requisição, **sem** `aggregateId`/`sequence`) não pode passar por
   este caminho quando for implementada. Nada em `codegen/` a implementa hoje
   (`grep ApplicationEvent` sobre `*.go` → zero).

## Fatiamento sugerido

1. **M1.1a — `runtime.Tx` carrega o tipo por chamada.** `target_files`:
   `codegen/rtsrc/uow.go.txt`, `codegen/rtsrc/contextkeys.go.txt`,
   `codegen/sqlrt/uow.go.txt`, `codegen/rtsrc/runtime_test.go.txt`,
   `codegen/sql_outbox_test.go`, `codegen/sql_outbox_relay_test.go`,
   `codegen/sql_outbox_cleanup_test.go`, `codegen/sql_outbox_channel_test.go`,
   `codegen/decl_aggregate_load_test.go`,
   `driver/generate_e2e_wallet_test.go`. Assinatura + as 2 implementações + os
   16 call sites, no mesmo commit (senão não compila). Nada em `codegen/`
   consome ainda. DoD: `TestSourcesSmokeCompileAndVet` +
   `TestSourcesBehavioralTestsPass` verdes.
2. **M1.1b — carimbo em `tenantStream` e `StreamLister`.** `target_files`:
   `codegen/rtsrc/eventstore.go.txt`, `codegen/rtsrc/runtime_test.go.txt`.
   `Append` lê `AggregateTypeFrom(ctx)`; `ListStreams` filtra por tipo, aplica
   `tenantVisible` e ordena. É aqui que moram TEST-1/TEST-2/TEST-3 da M1.1
   atual. Depende de M1.1a.
3. **M1.1c — o gerador passa a emitir a forma nova.** `target_files`:
   `codegen/lower/stmt.go`, `codegen/decl_usecase.go` (só doc-comment),
   `codegen/decl_usecase_test.go`, `codegen/testdata/usecase_deposit.go.golden`,
   `codegen/testdata/usecases_wallet.go.golden`,
   `codegen/testdata/usecase_increment_idempotent.go.golden`,
   `codegen/testdata/usecase_increment_idempotent_reject.go.golden`,
   `codegen/testdata/filestorage_usecases.go.golden`. Guarda de vizinhança: um
   `UseCase` que toca **dois** Aggregates de tipos diferentes no mesmo `Run`
   deve gerar dois `Append` com tipos **diferentes** — o contraexemplo da issue
   irmã vira teste. Depende de M1.1b.
4. **M1.1d (opcional, habilita M1.2/M1.3 sob teste) — semeadura tipada no
   `given` de UseCase.** `target_files`: `codegen/gentest.go`,
   `codegen/testdata/tests_wallet.go.golden`. `emitUseCaseGiven` emite
   `runtime.WithAggregateType(context.Background(), "<Agg>")` usando a cabeça do
   `Subject`. Depende de M1.1b.
