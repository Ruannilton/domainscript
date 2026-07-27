# M1.1: nenhuma rota leva o `aggregateType` até `EventStore.Append` dentro do escopo da task
- SPEC: correcoes-issues-6-8-12
- TASK: M1.1
- DESCRIPTION: `design.md` §4.1 (Componentes e Contratos) descreve o seam
  `StreamLister` e exige que `tenantStream` ganhe um campo `aggregateType`,
  "carimbado no primeiro `Append` do stream — pelo mesmo mecanismo e no mesmo
  ponto onde `tenantID` já é carimbado hoje", e delega a M1.1 decidir **como**
  esse tipo chega até `Append`. A própria task (`tasks/M1.1.md`, Step 2)
  prescreve duas rotas, nessa ordem:

  1. "derivá-lo do `EventType()` do primeiro evento via um **registro já
     disponível**";
  2. se isso não for possível sem tocar a assinatura de `Append` (que é
     `EventStore` e não pode mudar, NFR-32), "usar o seam que o
     `Event`/`EventMeta` **já oferece**";
  3. "Se nenhuma rota funcionar sem alterar `EventStore`, PARE e reporte —
     não altere a interface."

  Verifiquei as duas rotas por leitura de código; nenhuma existe hoje dentro
  de `target_files` (`codegen/rtsrc/eventstore.go.txt`,
  `codegen/rtsrc/rtsrc_test.go`):

  **Rota 1 — "registro já disponível" não existe.**
  - `Event` (`codegen/rtsrc/event.go.txt:6-18`) só expõe `EventType() string`
    e `SetMeta(EventMeta)`. `EventType()` devolve o nome do PRÓPRIO evento
    (ex. `"TicketCreated"`), não o nome do Aggregate — é literalmente
    `decl.Name` do `EventDecl`, emitido em
    `codegen/decl_event.go:181` (`func (*%s) EventType() string { return %q }`).
    Não há convenção de prefixo (`"KitchenTicket.TicketCreated"` ou
    equivalente) que permita derivar o tipo do Aggregate a partir do nome do
    evento.
  - O único mapa "nome de evento → algo" que existe é `eventRegistry`
    (`map[string]func() runtime.Event`), gerado **por módulo**
    (`codegen/decl_event.go:201-211`, exposto via `EventRegistry()`) — mapeia
    para um construtor de evento, não para um Aggregate, e vive no pacote Go
    do módulo declarante (ex. `kitchen`), não no pacote `runtime`
    (`codegen/rtsrc/`). Importar esse registro de volta para `runtime`
    criaria um ciclo (`kitchen` já importa `runtime`); hoje ele só é
    entregue a `sqlrt.NewEventStore`/`NewUnitOfWork` pelo wiring gerado
    (`cmd/<service>/main.go`, `codegen/codegen.go`) — fora de
    `target_files` de M1.1.
  - `grep -rn "AggregateType\|aggregateType"` em todo o repositório (fora
    desta spec) não devolve nenhuma ocorrência em código Go ou `rtsrc/*.txt`
    — confirmando que esse registro não existe em lugar nenhum, não só fora
    do escopo.

  **Rota 2 — "seam que `Event`/`EventMeta` já oferece" também não existe.**
  `EventMeta` (`codegen/rtsrc/event.go.txt:26-36`) só carrega
  `AggregateID`/`Sequence`/`Timestamp`; nenhum campo de tipo de Aggregate. E é
  o próprio `Append` quem CONSTRÓI o `EventMeta` (`eventstore.go.txt:83-88`)
  — não há como ele "oferecer" um dado que `Append` mesmo produz a partir do
  nada.

  **A rota análoga que existe (tenantID) está fora do escopo.** `tenantID` é
  carimbado do mesmo jeito que o `design.md` pede para `aggregateType`, mas
  chega via `ctx` (`WithTenant`/`TenantFrom`,
  `codegen/rtsrc/contextkeys.go.txt`) — um arquivo que M1.1 não toca. Copiar
  esse mecanismo para `aggregateType` exigiria (a) um novo par
  `WithAggregateType`/`AggregateTypeFrom` em `contextkeys.go.txt`, e (b) um
  call site que o invoque ANTES de `Append` — hoje inexistente:
  `memoryTx.Append` (`codegen/rtsrc/uow.go.txt:135-141`) só repassa
  `tx.ctx`/`aggregateID`/`events`, sem tocar o tipo; quem monta essa chamada é
  `codegen/lower/stmt.go:2082` (`tx.Append(string(id), events)`), também fora
  de `target_files`.

  **Mudar a assinatura do construtor também não é opção dentro do escopo.**
  `NewMemoryEventStore()` hoje não recebe parâmetro nenhum; adicionar um
  registro/opção a ela evitaria mexer na interface `EventStore`, mas
  `NewMemoryEventStore()` é chamado em 36 arquivos (`grep -rl
  NewMemoryEventStore`), nenhum deles em `target_files` de M1.1 — mudar a
  assinatura sem tocar os call sites não compila.

  **Conclusão.** As duas rotas que `design.md`/`M1.1.md` prescrevem não
  existem no código hoje, e a única rota que existe (thread via `ctx`, o
  mesmo mecanismo de `tenantID`) exige tocar arquivos fora de
  `target_files` (`contextkeys.go.txt` e o call site de `Append`
  em `uow.go.txt`/`lower/stmt.go`). Isso é exatamente a cláusula 3 da própria
  task ("Se nenhuma rota funcionar sem alterar `EventStore`, PARE e
  reporte"). **Pedido de decisão de design**, não defeito de código: `design.md`
  §4.1/§5.1 (e o Step 2 de `tasks/M1.1.md`) precisam decidir e registrar
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
  proíbe (`CLAUDE.md`, "A fronteira do spec da linguagem é parada, não
  adivinhação" — mesmo espírito aplicado aqui a uma lacuna do `design.md`
  desta spec, não do spec da linguagem).
- SOLVED: [decisão do usuário — opção 1 (thread via `ctx`, mesmo padrão de
  `tenantID`) — registrada em `design.md` §5.1/§7.2 e em `tasks/M1.1.md`
  (`status` volta a `pending`, `target_files` ampliado com
  `contextkeys.go.txt` e `decl_usecase.go`, que é o call site real de
  `uow.Run(ctx, ...)` — `lower/stmt.go`, citado nesta issue, não é. A
  implementação da rota fica condicionada a M1.1 confirmar, por leitura,
  que uma `Tx.Run()` nunca grava eventos de mais de um `aggregateType`;
  ver a nota na própria task]
