# M1.4: produtor durável escreve no banco real, mas toda Query do service lê da `store` em memória
- SPEC: [correcoes-issues-6-8-12](../specs/correcoes-issues-6-8-12/requirements.md)
- TASK: [M1.4](../specs/correcoes-issues-6-8-12/tasks/M1.4.md)
- DESCRIPTION: Achado de leitura durante a task de design M1.4 (wiring de
  service com múltiplos produtores e Dispatcher,
  [REQ-55.7/55.8](../specs/correcoes-issues-6-8-12/requirements.md)) —
  explicitamente **fora** desse escopo (REQ-55.11: "bloqueio adicional e
  independente" vira issue própria, nunca amplia REQ-55 silenciosamente).

  Um módulo que é ao mesmo tempo **produtor durável** (Marco K —
  `durableProducer`, [`codegen/sql_wiring.go`](../../../codegen/sql_wiring.go#L178):
  Database real reconhecido + canal de saída `via: queue provider:
  "rabbitmq"`) e dono de Query(ies) sobre os MESMOS Aggregates que ele
  produz tem um mismatch de leitura:

  - `emitSingleDatabaseWiring`
    ([`codegen/sql_wiring.go:388`](../../../codegen/sql_wiring.go#L388)) troca a
    `UnitOfWork` desse módulo para uma conexão SQL real
    (`sqlruntime.NewOutboxUnitOfWork`) — os Aggregates do módulo passam a
    ser persistidos **somente** no banco real.
  - `generateCmdMainFile`
    ([`codegen/codegen.go:1384`](../../../codegen/codegen.go#L1384) — linha 1304
    à época do registro, deslocada desde então) constrói **sempre**
    `store := runtime.NewMemoryEventStore()`, e é essa MESMA `store` em
    memória — nunca o banco real — que alimenta
    [`newGRPCServer(store)`](../../../codegen/codegen.go#L1583)/
    [`newMux(store)`](../../../codegen/codegen.go#L1591) (linhas ~1479/1487 à
    época do registro): o lado de LEITURA de TODAS as rotas HTTP/gRPC do
    service, incluindo as Query do próprio módulo produtor.

  Resultado: uma Query do módulo produtor durável sobre um Aggregate que ele
  mesmo escreve lê da `store` em memória, sempre vazia para esses dados —
  devolve lista vazia silenciosamente, mesmo com dados reais no banco. É
  miscompilação silenciosa de LEITURA: o Go gerado compila e roda sem erro, o
  resultado é semanticamente errado.

  **Onde reproduz.** [`testdata/projects/pizzeria`](../../../testdata/projects/pizzeria):
  `Sales` é produtor durável (`Database MainDb { provider: "postgres" }` +
  canal `Sales -> Kitchen` `provider: "rabbitmq"`) e dono de
  `GetAvailableMenu`/`GetActiveOrders` (`sales/read.ds`), que leem
  `MenuItem`/`Order` — Aggregates que `Sales` grava no Postgres real via a
  UoW durável, nunca em `store`.

  **Por que bloqueia [M1.6](../specs/correcoes-issues-6-8-12/tasks/M1.6.md),
  não M1.4.** M1.4/[M1.5](../specs/correcoes-issues-6-8-12/tasks/M1.5.md)
  fecham as duas guardas de REQ-55.7/55.8 (múltiplos produtores; produtor +
  Dispatcher) — este achado é ortogonal, sobre QUAL store uma Query lê, não
  sobre quem publica no Dispatcher. Mesmo com as duas guardas fechadas, M1.6
  (REQ-55.9: `dsc gen` + `go build`/`go vet` sobre o `pizzeria`) só prova
  **compilação** — não prova que `GetAvailableMenu`/`GetActiveOrders`
  devolvem dados corretos. O design de
  [design.md](../specs/correcoes-issues-9-10-11/design.md) §4.1 já
  documentava a `store` em memória como "não o banco declarado" para o
  produtor durável, mas nunca precisou lidar com uma Query do MESMO módulo
  lendo seu próprio estado, porque o recorte original de Marco K excluía
  Dispatcher/Query cacheada do módulo produtor — o `pizzeria` reintroduz
  exatamente essa combinação.

  **Não é decisão de design em aberto nem ambiguidade da spec da
  linguagem** — é gap de implementação em `codegen` (a variável `store` de
  `generateCmdMainFile` ignora qual módulo é produtor durável). Fica
  registrado aqui para ser resolvido antes ou junto de M1.6, sem ampliar
  REQ-55.7/55.8 (o escopo de M1.4/M1.5) por conta própria.
- SOLVED: FALSE

# Solução proposta

## Veredito

**Real, e pior do que a issue descreve.** Reverificado hoje, linha a linha — os
números que a issue cita continuam corretos:

- [`codegen.go:1384`](../../../codegen/codegen.go#L1384) `store :=
  runtime.NewMemoryEventStore()`, incondicional, dentro de `func main()`/`func
  run()`; [`:1583`](../../../codegen/codegen.go#L1583) `newGRPCServer(store)`;
  [`:1591`](../../../codegen/codegen.go#L1591) `newMux(store)`;
  [`:1627`](../../../codegen/codegen.go#L1627) `func newMux(store
  runtime.EventStore)` — **uma** variável de leitura por SERVICE.
- [`sql_wiring.go:415`](../../../codegen/sql_wiring.go#L415)
  (`emitSingleDatabaseWiring`, [`:388`](../../../codegen/sql_wiring.go#L388))
  emite `uow := sqlruntime.NewOutboxUnitOfWork(<mod>DB, ...)`. A própria
  doc-comment da função,
  [`:381-387`](../../../codegen/sql_wiring.go#L381-L387), já **documenta** o
  mismatch como intencional ("nenhuma rota de Query do módulo produtor muda de
  comportamento aqui").
- Toda Query gerada recebe a store como **parâmetro**:
  [`decl_query.go:245`](../../../codegen/decl_query.go#L245) `store
  %s.EventStore`, idem a Query cacheada
  ([`decl_query_cache.go:495`](../../../codegen/decl_query_cache.go#L495)); a
  rota HTTP monta a chamada em
  [`http.go:960`](../../../codegen/http.go#L960)
  (`callArgs := append([]string{"ctx", "store"}, ...)`).
- `durableProducer` ([`sql_wiring.go:178-207`](../../../codegen/sql_wiring.go#L178-L207))
  e o `pizzeria` batem: `Sales` tem `Database MainDb { provider: "postgres" }`
  ([`sales/mod.ds`](../../../testdata/projects/pizzeria/sales/mod.ds)) + canal
  `Sales -> Kitchen` `provider: "rabbitmq"`
  ([`topology.ds`](../../../testdata/projects/pizzeria/topology.ds)), e é dono
  de `GetAvailableMenu`/`GetActiveOrders`
  ([`sales/read.ds`](../../../testdata/projects/pizzeria/sales/read.ds)) sobre
  `MenuItem`/`Order`. `Kitchen` NÃO é produtor durável (`provider: "mongodb"`
  não é reconhecido), então só `Sales` é afetado.

**Correção de fato:** o `pizzeria` mora em
[`testdata/projects/pizzeria`](../../../testdata/projects/pizzeria), **não** em
`docs/examples/pizzeria` — não existe pizzeria em `docs/examples/`. Os Steps 1
e 2 de [M1.6](../specs/correcoes-issues-6-8-12/tasks/M1.6.md) apontam para um
caminho inexistente.

### Consequência observável — três formas, e a pior não é lista vazia

1. **`load <Agg>(id) as <View>` devolve dado FABRICADO, com HTTP 200.**
   `memoryEventStore.Load` de um id desconhecido devolve `([]Event{}, nil)`, não
   `ErrNotFound` ([`rtsrc/eventstore.go.txt:102-105`](../../../codegen/rtsrc/eventstore.go.txt#L102-L105)),
   e o `Load<Agg>` gerado **não trata stream vazio** — itera zero eventos e
   devolve o agregado zero-value sem erro
   ([`codegen/testdata/aggregate_wallet_load.go.golden:14-33`](../../../codegen/testdata/aggregate_wallet_load.go.golden#L14-L33)).
   Uma `Query GetOrder(id) -> OrderVW` de um módulo produtor durável responde
   **200 com a View inteiramente zerada** (id vazio, `Money` 0, status zero) —
   não 404, não lista vazia: um registro inventado. É a forma mais grave e a
   issue não a menciona.
2. **`list <Aggregate>` devolve lista vazia silenciosa** — a forma que a issue
   descreve, e a que o `pizzeria` usa. Vale a ressalva de que ela ainda não
   existe no gerador (M1.2/M1.3 pendentes).
3. **A escrita pode nem chegar lá: o caminho durável nunca cria o schema.**
   `ensureSchema` (o DDL idempotente de `events` + `outbox`) é chamada de **um
   único lugar**, `NewEventStore`
   ([`sqlrt/eventstore.go.txt:49`](../../../codegen/sqlrt/eventstore.go.txt#L49),
   corpo em [`:74-82`](../../../codegen/sqlrt/eventstore.go.txt#L74-L82)) — e o
   wiring do produtor durável emite só `Open*` + `NewOutboxUnitOfWork` +
   `NewOutboxStore`, nenhum dos quais a chama. Contra um Postgres virgem, o
   primeiro UseCase falha com `relation "events" does not exist`. Verificado por
   leitura; **não** rodei contra um Postgres real.

**Prioridade: alta, mas não urgente — hoje é latente.** Nenhum projeto que GERA
exercita o defeito: a fixture-âncora `AnchorOrders` é produtora durável mas não
tem `read.ds` nem `interface.ds`
([`anchor_fixture_test.go:370-384`](../../../codegen/anchor_fixture_test.go#L370-L384)),
e o `pizzeria` não gera — rodei `go run ./cmd/dsc gen testdata/projects/pizzeria`
hoje e ele para antes, em `Query GetBoardTickets` ("`list ...` em posição de
expressão pura não é suportado por `Lowerer.Expr`"), que é o gap de M1.2/M1.3.
O defeito vira observável no minuto em que M1.2/M1.3 caírem — ou seja, é
pré-condição de [M1.6](../specs/correcoes-issues-6-8-12/tasks/M1.6.md) poder
afirmar o que promete, não um incêndio em produção.

## Causa raiz

`generateCmdMainFile` já roteia a **escrita** por módulo (`uowVarByModule`,
[`codegen.go:1254-1262`](../../../codegen/codegen.go#L1254-L1262)) mas mantém a
**leitura** com uma única variável por service (`store`); quando
`emitSingleDatabaseWiring` troca a UoW de um módulo para SQL, não existe a noção
"store de leitura DESTE módulo" para acompanhar.

## Solução proposta

Simetria com o que a escrita já faz: um `readStoreByModule` ao lado de
`uowVarByModule`.

1. **`emitSingleDatabaseWiring`** ([`sql_wiring.go:388`](../../../codegen/sql_wiring.go#L388))
   emite **uma linha a mais**, na forma exata que `emitXADatabaseWiring` já usa
   ([`:295`](../../../codegen/sql_wiring.go#L295)):
   `salesReadStore, err := sqlruntime.NewEventStore(context.Background(),
   salesDB, sales.EventRegistry(), sqlruntime.PostgresDialect())` + `emitFailFast`.
   Mesma conexão `<mod>DB` que a UoW já abriu; mesma tabela `events`
   (`appendWithinTx`/`loadWithinQuerier`,
   [`sqlrt/eventstore.go.txt:153`](../../../codegen/sqlrt/eventstore.go.txt#L153)/[`:209`](../../../codegen/sqlrt/eventstore.go.txt#L209))
   — o que a UoW grava é exatamente o que este store lê, sem nenhuma
   coordenação nova. `*sqlruntime.EventStore` satisfaz `runtime.EventStore` sem
   conversão (só `Append`/`Load`,
   [`rtsrc/eventstore.go.txt:27-38`](../../../codegen/rtsrc/eventstore.go.txt#L27-L38)).
   **Efeito colateral desejado:** `ensureSchema` finalmente roda neste caminho,
   fechando o item 3 do veredito de graça.
2. **`newMux`/`newGRPCServer` ganham um parâmetro por módulo com store própria** —
   `func newMux(store runtime.EventStore, salesStore runtime.EventStore)`,
   chamado `newMux(store, salesReadStore)`. Emitido **só** quando o grupo tem
   produtor durável **com ao menos uma Query roteada**; sem isso, assinatura e
   chamada ficam byte a byte como hoje.
3. **A rota escolhe o var pelo módulo da Query.**
   [`emitQueryRoute`](../../../codegen/http.go#L866) já recebe `qModule`: uma
   linha em [`http.go:960`](../../../codegen/http.go#L960) troca `"store"` por
   `readStoreVar[qModule]` (default `"store"`), e o handler gRPC equivalente em
   [`grpc.go:249`](../../../codegen/grpc.go#L249) faz o mesmo. Nada em
   `decl_query.go` muda: a Query gerada continua recebendo a store por
   parâmetro, agnóstica de qual é.
4. **`list <Aggregate>` sobre módulo com store SQL cai no erro claro de
   REQ-55.5**, porque `*sqlruntime.EventStore` não implementa `StreamLister`
   ([design.md](../specs/correcoes-issues-6-8-12/design.md) §4.1, decisão
   explícita do ciclo) — em vez de devolver lista vazia. É exatamente a troca
   que NFR-33 pede: de miscompilação silenciosa para gap declarado. Fazer
   `GetAvailableMenu`/`GetActiveOrders` devolverem **dados** exige `StreamLister`
   no `sqlrt`, hoje fora de escopo
   ([requirements.md](../specs/correcoes-issues-6-8-12/requirements.md) §1.3) —
   ver Bloqueios 2 e o fatiamento.

## Alternativas descartadas

- **Trocar a `store` do service inteiro pela store SQL do produtor.** Quebra
  todo módulo in-memory do MESMO service: no `pizzeria`, `Kitchen`
  (`GetBoardTickets`) passaria a ler um Postgres que nunca recebeu um evento
  dela. O exemplo é justamente um monólito com os dois no mesmo processo.
- **`newMux(stores map[string]runtime.EventStore)`.** Uniformiza, mas reescreve
  os 6 goldens de `newMux` e a assertiva literal de
  [`http_test.go:53`](../../../codegen/http_test.go#L53) por um caso que
  nenhuma fixture atual exercita — perde byte-identidade (NFR-31) sem ganho.
- **Espelhar as escritas SQL na `store` em memória (dual write).** Duas fontes
  de verdade sem transação comum: é a classe de bug que o outbox existe para
  evitar, e o relay já entrega pós-commit.
- **Esperar `StreamLister` no `sqlrt` e resolver tudo de uma vez.** Deixa a
  leitura FABRICADA de `load ... as V` (item 1) de pé — que é pior que a lista
  vazia e não depende de enumeração nenhuma.
- **Fazer a Query recusar-se a gerar quando o módulo tem Database real.** Troca
  um dado errado por um gap total: `pizzeria` deixaria de gerar por outra razão,
  e REQ-55.9 ficaria inalcançável.

## Raio de alcance

- **Goldens: zero mudanças.** Os 6 arquivos com `func newMux`
  (`cmd_{app_ratelimit,billing,grpcdemo,notes,telemetrydemo,wallet}_main.go.golden`)
  ficam byte-idênticos — `grep NewOutboxUnitOfWork` em `codegen/testdata/` e
  `testdata/` devolve **zero**: nenhum golden tem produtor durável.
  [`http_test.go:53`](../../../codegen/http_test.go#L53) continua válido.
- **Fixtures:** `wallet`/`shop` intocados; a fixture-âncora tem produtor durável
  **sem Query**, então `TestAnchorFixtureOrdersMainWiresRabbitMQProducer`
  ([`anchor_fixture_test.go:438`](../../../codegen/anchor_fixture_test.go#L438))
  segue verde sem uma linha alterada. O único exerciser real é o `pizzeria`, que
  não gera — logo o par NFR-4 precisa de **fixture sintética nova** em
  `codegen/codegen_test.go`, no molde de `m15DurableSelf*`
  ([`:723-770`](../../../codegen/codegen_test.go#L723-L770)).
- **CI:** nenhum job novo; `examples`/`fixtures` só mudam quando o `pizzeria`
  sair de `KNOWN_UNGENERATABLE` ([`ci.yml:69`](../../../.github/workflows/ci.yml#L69)),
  o que é trabalho de M1.6.
- **NFR-13:** a linha emitida é fixa e o DDL é idempotente — geração
  determinística e regeneração byte-idêntica. O determinismo do **resultado** de
  um `list` sobre SQL depende de `ORDER BY` explícito e é problema da fatia 3,
  não desta.
- **Sobreposição com [m1-1](m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype.md):
  COMPÕE, não colide — e m1-1 vem primeiro.** Na parte de roteamento de leitura
  não há um arquivo em comum: m1-1 mexe em `codegen/rtsrc/{uow,contextkeys,eventstore}.go.txt`,
  em `codegen/sqlrt/uow.go.txt` (assinatura de `Tx.Append`) e em
  `codegen/lower/stmt.go:2082`; esta rota mexe em `codegen/sql_wiring.go`,
  `codegen/codegen.go`, `codegen/http.go`, `codegen/grpc.go`. Os dois tocam
  `codegen/sqlrt/`, mas em arquivos e tipos disjuntos (`Tx` vs. `EventStore`) —
  podem ir em paralelo. **A dependência aparece na peça seguinte:** dar
  `StreamLister` ao `sqlrt` exige saber o tipo de Aggregate de cada stream, e a
  tabela `events` **não tem coluna `aggregate_type`** (DDL em
  [`sqlrt/dialect.go.txt:103`](../../../codegen/sqlrt/dialect.go.txt#L103) e
  [`dialect_postgres.go.txt:48`](../../../codegen/sqlrt/dialect_postgres.go.txt#L48);
  INSERT em [`eventstore.go.txt:166-171`](../../../codegen/sqlrt/eventstore.go.txt#L166-L171)).
  Quem leva o tipo até o `Append` é exatamente a fatia M1.1a de m1-1
  (`Tx.Append(aggregateType, aggregateID, events)`). Ordem: **m1-1a/b/c →
  coluna `aggregate_type` + `ListStreams` no `sqlrt`**. Inverter significaria
  inventar uma segunda via para o tipo — a adivinhação que m1-1 já derrubou.

## Bloqueios

1. **[M1.6](../specs/correcoes-issues-6-8-12/tasks/M1.6.md) tem OUTRO bloqueio,
   independente desta issue e ainda sem issue própria.**
   [`codegen.go:1215-1219`](../../../codegen/codegen.go#L1215-L1219) continua
   recusando um produtor durável que TAMBÉM precise de Dispatcher **no mesmo
   módulo** — e `Sales` é precisamente isso: Policy
   `MarkOrderReadyOnTicketFinished`
   ([`sales/policy.ds`](../../../testdata/projects/pizzeria/sales/policy.ds)) e
   Query cacheada `GetAvailableMenu` (`cache { ttl: 1h }`). M1.5 está
   `completed` e fechou REQ-55.8 só no caso "módulos DIFERENTES";
   [`codegen_test.go:723-730`](../../../codegen/codegen_test.go#L723-L730)
   registra a combinação restante como negativa deliberada, e
   [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.3 item 4 exige
   estender `NewOutboxUnitOfWork` com Publisher opcional — não implementado
   ([`sqlrt/uow.go.txt:107`](../../../codegen/sqlrt/uow.go.txt#L107)). Por
   REQ-55.11 isso merece issue própria.
2. **Decisão de escopo do ciclo, em `requirements.md`/`design.md`.** A rota
   acima entrega leitura **correta** para `load ... as V`/join e **erro claro**
   para `list` — não faz as duas Queries do `pizzeria` devolverem dados. Se
   REQ-55.9 for lida como "o projeto compila", basta; se for "as Queries do
   exemplo funcionam", `StreamLister` no `sqlrt` precisa entrar no escopo
   ([requirements.md](../specs/correcoes-issues-6-8-12/requirements.md) §1.3 o
   exclui hoje, e [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.1
   confirma). Não é decisão de uma task.
3. **Módulo com 2+ Database reais (caminho XA).** O mesmo mismatch existe lá —
   `Wire2PC` recebe stores SQL
   ([`sql_wiring.go:299`](../../../codegen/sql_wiring.go#L299)) enquanto
   `Wire(uow)` e as Queries continuam na memória — mas "qual das duas stores
   serve a leitura" não tem resposta óbvia. Fica fora desta proposta; precisa de
   decisão de design própria.
4. **Nada na spec da linguagem bloqueia.** Roteamento de leitura é decisão de
   wiring do back-end; a spec só diz que um `Database ... manages: [X]`
   ([§13](../steerings/domainscript-spec-v7/13-module-infra.md)) é onde `X`
   vive, o que não admite ler `X` de outro lugar. Nenhuma das seções revisadas
   em 2026-07-31 (§2.7/2.8/4.2.3/4.3.1/5.3/9.4/19.3) trata de qual store
   alimenta uma Query — a §4.2.3 reforça só o lado da escrita, que é assunto de
   m1-1.

## Fatiamento sugerido

1. **Mx.1 — store de leitura do módulo produtor durável.** `target_files`:
   `codegen/sql_wiring.go`, `codegen/codegen.go`, `codegen/http.go`,
   `codegen/grpc.go`, `codegen/codegen_test.go`. Emite `<mod>ReadStore`,
   acrescenta o parâmetro a `newMux`/`newGRPCServer` e seleciona o var por
   `qModule`. **Par NFR-4:** fixture sintética no molde de `m15DurableSelf*`
   (Database `postgres` + canal `rabbitmq` + `Interface HTTP` com uma
   `Query ... { return load Ticket(id) as TicketView }`) — positivo: o main
   gerado contém `sqlruntime.NewEventStore(` e a rota chama
   `<pkg>.GetTicket(ctx, durableSelfReadStore, ...)`; negativo: um service sem
   produtor durável mantém `newMux(store)` e os 6 goldens byte-idênticos.
2. **Mx.2 — guarda do schema no caminho durável.** `target_files`:
   `codegen/sql_wiring_test.go` (ou o teste da fixture-âncora). Asserção de que
   o `main.go` de um produtor durável contém `sqlruntime.NewEventStore(` — hoje
   não contém, e nenhuma tabela é criada. Absorvível por Mx.1 se o revisor
   preferir; separada, vira regressão permanente do item 3 do veredito.
3. **Mx.3 — `ListStreams` no `sqlrt` (depende de M1.1a de m1-1 e da decisão do
   Bloqueio 2).** `target_files`: `codegen/sqlrt/eventstore.go.txt`,
   `codegen/sqlrt/dialect.go.txt`, `codegen/sqlrt/dialect_postgres.go.txt`,
   `codegen/sql_outbox_test.go`. Coluna `aggregate_type` no DDL e no INSERT de
   `appendWithinTx` (valor vindo do `Tx.Append` tipado de M1.1a) +
   `ListStreams` com `SELECT DISTINCT aggregate_id ... ORDER BY aggregate_id` e
   o mesmo filtro de tenant de `loadWithinQuerier`. Só depois desta fatia
   `GetAvailableMenu`/`GetActiveOrders` devolvem dados.
4. **Mx.4 — e2e do `pizzeria` (= M1.6 reescrita).** Depende de Mx.1, de
   M1.2/M1.3 e do Bloqueio 1. Corrigir em
   [M1.6.md](../specs/correcoes-issues-6-8-12/tasks/M1.6.md) o caminho
   `docs/examples/pizzeria` → `testdata/projects/pizzeria` antes de executá-la.
