# Design — Correções de dívida técnica (ISSUE-9, ISSUE-10, ISSUE-11)

Marco K. Três correções ortogonais. Cada seção abaixo faz a **análise de raiz**
(o que realmente causa o bug, confirmado por leitura de código), decide o
**fix na raiz**, lista os **arquivos tocados** e o **par de testes** (NFR-4).

---

## 1. Visão geral e ordem

Os três problemas não se tocam (parser / runtime vendorizado / wiring de
codegen). Executamos na ordem **K1 (REQ-49) → K2 (REQ-50) → K3 (REQ-51)**: as
duas primeiras são pequenas, de baixo risco e sem pré-condições; a terceira é a
maior (o resíduo do Marco J) e ganha várias subtasks. Nenhuma depende do
resultado da outra — a ordem é só de risco crescente.

---

## 2. REQ-49 — Parser: duas atribuições consecutivas (ISSUE-11)

### 2.1. Análise de raiz (corrige a hipótese da issue)

A ISSUE-11 especulava que a causa estaria em `synchronize`/`expect` decidindo
onde um statement termina. **A leitura do código mostra outra coisa** — o culpado
é o **binding opcional de `parseQueryOp`**, não a recuperação de erro.

Trace de `order = load Bar(id)` / `x = id` (dois statements, um por linha):

1. `parseBlock` → `parseStmt` → `parseSimpleStmt` (`parse_stmt.go:138`):
   - `x := p.parseExpr()` lê `order` (Ident). Cursor em `=`.
   - `p.at(ASSIGN) && !p.atFatArrow()` → verdadeiro. Consome `=`.
   - `val := p.parseExpr()` para o RHS `load Bar(id)` → cai em `parseQueryOp`.
2. Em `parseQueryOp` (`parse_query.go:31`):
   ```go
   op := p.advance().Lit          // "load"
   target := p.parsePostfix()     // Bar(id)   — cursor agora em `x` (linha nova)
   if p.at(token.IDENT) && !isClauseKw(p.cur().Lit) {
       binding = p.advance().Lit   // <-- consome "x" como se fosse o binding!
   }
   ```
   O `x` do **segundo** statement está numa linha nova, mas o parser não olha
   linha: vê um `IDENT` que não é palavra-chave de cláusula e o engole como
   binding da query (a forma legítima de `list Ticket t`). Cursor vai para `=`.
3. `parseQueryClauses` não encontra cláusula (`=` não abre nenhuma) → volta.
   O 1º `AssignStmt` termina como `order = load Bar(id) [binding=x]`.
4. O loop de `parseBlock` chama `parseStmt` de novo. Cursor em `=`.
   `parsePrimary` (`parse_expr.go:149`) cai no `default` → **"esperava uma
   expressão, encontrei ="**. Esse é o erro que a issue observou.

Por que uma statement intermediária evita o bug (observação da issue): se entre
as duas atribuições houver, p.ex., um `ensure … else …`, então após
`load Bar(id)` o próximo token **não** é um `IDENT` (é `ensure`/`}`/etc.), logo
o `if p.at(token.IDENT)` de (2) é falso e o binding não é roubado. Confirma a
raiz: **o binding é ganancioso e cruza a fronteira de statement/linha.**

O **mesmo padrão latente** existe no alias de `join` (`parseOneClause`,
`parse_query.go:66`): `join Foo` seguido, na próxima linha, de um `IDENT` que
inicia outro statement seria consumido como alias. REQ-49.3 pede cobrir isso.

### 2.2. Fix na raiz: fronteira de linha nos identificadores opcionais

DomainScript separa statements por **quebra de linha** (todos os corpos nos
fixtures são um-statement-por-linha; não há `;` terminador). O parser já carrega
`token.Pos{Line, Col}` em cada token e `p.lastPos` (o fim do último token
consumido, `parser.go:16/72`). A correção é **localizada e mínima**: um binding/
alias opcional só é consumido se o `IDENT` candidato está na **mesma linha** que
o token que o precede (o alvo/fonte já consumido). Se está numa linha nova, é o
começo de outro statement — não um binding.

- Novo helper de cursor (p.ex. `sameLineAsPrev()` em `parser.go`): compara
  `p.cur().Pos.Line` com `p.lastPos.Line`.
- `parseQueryOp`: trocar
  `if p.at(token.IDENT) && !isClauseKw(p.cur().Lit)`
  por `if p.at(token.IDENT) && !isClauseKw(p.cur().Lit) && p.sameLineAsPrev()`.
- `parseOneClause`/`case "join"`: mesma guarda no consumo do alias.

Nota de robustez complementar: mesmo se um binding legítimo pudesse ficar numa
nova linha (não é o estilo da linguagem), essa forma nunca aparece nos exemplos
nem nas specs; a leitura por linha é a régua correta. Alternativa considerada e
**rejeitada**: um lookahead "não consuma o IDENT se o token seguinte for `=`"
— resolve só o caso de atribuição, deixa aberto `join Foo` / `x.doStuff()` e
outros começos de statement; a régua de linha é mais geral e mais barata.

### 2.3. Arquivos e testes

- `parser/parser.go` — helper `sameLineAsPrev()`.
- `parser/parse_query.go` — guarda de linha no binding e no alias de `join`.
- Testes (`parser/…_test.go`, par NFR-4):
  - **positivo/regressão-do-bug:** duas atribuições consecutivas
    (`order = load Bar(id)` / `x = id`) parseiam sem diagnóstico e produzem dois
    `AssignStmt`; variante com RHS literal simples na 2ª (`x = 1`) — o caso
    isolado que a issue reproduziu.
    Uma 3ª variante com `join` (`… join Foo` seguido de `x = …` na linha
    seguinte) confirmando REQ-49.3.
  - **preserva binding legítimo:** `list Ticket t where t.active` na mesma linha
    continua produzindo `binding == "t"` (o binding não foi quebrado pela nova
    guarda); `join Bar b` na mesma linha continua com alias `b`.

---

## 3. REQ-50 — `memoryQueryCache.Coalesce` à prova de pânico (ISSUE-10)

### 3.1. Análise de raiz

`memoryQueryCache.Coalesce` (`codegen/rtsrc/querycache.go.txt:170`) hoje:

```go
fl := &queryCacheFlight{done: make(chan struct{})}
c.flights[key] = fl
c.mu.Unlock()

fl.value, fl.err = fn()   // <-- se fn() panica, nada abaixo executa
close(fl.done)

c.mu.Lock()
delete(c.flights, key)
c.mu.Unlock()
```

Se `fn()` entra em pânico, `close(fl.done)` e `delete(c.flights, key)` nunca
rodam. Consequências (idênticas ao que a revisão da PR #26 apontou no
`redisQueryCache`, já corrigido): toda goroutine concorrente parada em
`<-fl.done` (o ramo `inFlight` no topo) **trava para sempre** (vazamento de
goroutine), e a chave fica presa em `c.flights` — **nunca mais coalesce**.

O adapter Redis (`codegen/redisrt/cache.go.txt:326`) já usa o padrão certo — um
`defer` que limpa `c.flights` e fecha `fl.done` **antes** de `fn()` rodar, sem
`recover` (o pânico segue propagando). `memoryQueryCache`, o backend em produção
desde G3, ficou com a versão original.

### 3.2. Fix na raiz: `defer` com flag + erro-sentinela aos esperadores

> **Refinado após a revisão da PR #37.** A versão inicial deste design pedia só
> "espelhar o `defer` do Redis". Validando contra o **wrapper gerado**
> (`codegen/decl_query_cache.go:491-504`):
> ```go
> result, err := cache.Coalesce(key, func() (any, error) { return runFn(...) })
> if err != nil { var zero T; return zero, err }
> return result.(T), nil   // <-- nil.(T) PANICA se result==nil e T é tipo de valor
> ```
> Num pânico do líder, o esperador recebe `(nil, nil)` (a atribuição
> `fl.value, fl.err = fn()` nunca completa), passa pelo `if err != nil`, e
> executa `result.(T)` sobre `nil` — **um segundo pânico**. O `defer` do Redis
> (PR #26) fechou o vazamento de goroutine mas **não** este ponto: ele também
> devolve `(nil, nil)`. Logo o fix de raiz injeta um erro e vale para os dois
> backends.

Reescrever o trecho pós-registro (idêntico em `memoryQueryCache` e
`redisQueryCache`):

```go
fl := &queryCacheFlight{done: make(chan struct{})}
c.flights[key] = fl
c.mu.Unlock()

completed := false
defer func() {
    c.mu.Lock()
    delete(c.flights, key)
    c.mu.Unlock()
    if !completed {
        fl.err = errCoalescedPanic // sentinela de pacote; nunca (nil, nil)
    }
    close(fl.done)
}()

fl.value, fl.err = fn()
completed = true
return fl.value, fl.err
```

Pontos validados:
- **Sem `recover`** (REQ-50.3): o pânico do líder segue desenrolando a pilha; a
  flag só protege os esperadores.
- **Erro de negócio legítimo preservado** (REQ-50.4): no sucesso, `completed =
  true` roda, então o `if !completed` do `defer` é falso e **não** sobrescreve o
  `fl.err` que `fn` devolveu.
- **Data-race-free:** o `defer` escreve `fl.err` **antes** de `close(fl.done)`; o
  esperador lê `fl.err` **depois** de `<-fl.done`. O fechamento do canal
  estabelece o happens-before.
- **Sentinela:** `var errCoalescedPanic = errors.New("coalesced function
  panicked")` no pacote. `errors` já é importado no adapter Redis; adicionar o
  import em `querycache.go.txt`.
- Comentário nos dois arquivos apontando a paridade (para não voltarem a
  divergir).

### 3.3. Arquivos e testes

- `codegen/rtsrc/querycache.go.txt` — flag + sentinela + import `errors` (REQ-50.1-4).
- `codegen/redisrt/cache.go.txt` — o MESMO endurecimento (REQ-50.5): hoje só tem
  o `defer` de limpeza da PR #26; ganha a flag + o sentinela.
- Testes (`codegen/…_test.go`, via `gentest.WriteFiles`/`RunTests` sobre o pacote
  gerado, mesmo padrão dos testes de cache existentes; par NFR-4 **por backend**):
  - **negativo (pânico):** uma `fn` que panica (com `recover` no teste do líder
    para não derrubar o processo); uma 2ª goroutine no mesmo voo é liberada sob
    timeout/`WaitGroup` (não trava) **e recebe um erro não-nil**; depois, a MESMA
    chave coalesce de novo (não ficou presa em `c.flights`).
  - **positivo (não-regressão):** N goroutines na mesma chave recebem o mesmo
    resultado, `fn` roda **uma** vez; um `fn` que devolve um erro de negócio
    propaga esse erro (não o sentinela) a todos os esperadores.

---

## 4. REQ-51 — Produtor Outbox → canal cross-service (ISSUE-9, REQ-42.6)

O maior dos três — o resíduo aberto do Marco J. O design segue o alvo já
desenhado em §3.2a de `infra-providers/design.md`, traduzido para os pontos de
codegen concretos.

### 4.1. Condição de ativação, análise de raiz e pré-condição

**Condição de ativação (validada na revisão da PR #37):** o caminho
produtor-durável ativa só quando o módulo tem **Database real**
(`recognizedSQLProvider`) **E** um canal de saída com **provider real**
(`channelProviderKind(ch) == "rabbitmq"` — não a `QueueChannel` in-memory de um
`via: queue` sem `provider:`). A durabilidade só faz sentido com transporte real
(§design infra-providers 3.2a). Recorte, isolando as fronteiras pré-existentes:

- `wallet` — sem canal de saída ⇒ nunca ativa ⇒ byte-idêntico.
- `shop/Orders` — Database postgres **real**, mas canal `Orders -> Shipping {
  via: queue orderBy: id }` **sem `provider:`** (a `QueueChannel` in-memory,
  mesmo processo) ⇒ **não ativa** ⇒ **byte-idêntico**. (Corrige a versão inicial
  deste design, que dizia que o `shop` mudaria — validação de código: seu canal
  não tem transporte real, então a durabilidade não se aplica; segue no
  publish-direto in-memory documentado do Marco F.)
- Fixture-âncora de J6 (`AnchorOrders`) — postgres + canal `queue provider:
  "rabbitmq"` ⇒ **ativa** ⇒ é o exerciser pretendido (suas asserções de teste
  atualizam).

Hoje, para `AnchorOrders`/`shop/Orders`, `generateCmdMainFile` (`codegen.go:1254`)
emite:

```go
store := runtime.NewMemoryEventStore()
uow := runtime.NewUnitOfWork(store, ordersChannel)   // publisher = canal
```

Dois problemas encadeados (relevantes só sob a condição de ativação):

1. **A store é in-memory, não o Database declarado.** O adapter `database/sql`
   só é wirado hoje no caminho **2PC** (`usecase2PCPlan` exige 2+ Databases XA,
   `decl_usecase.go:360`). Um UseCase de **banco único** "degenera em commit
   local" (§design codegen 3.8) sobre a store in-memory. Nessa store,
   `Tx.EnqueueOutbox` é um **no-op** documentado (`rtsrc/uow.go.txt:153`) — logo
   não há onde persistir a linha do outbox. **Pré-condição de REQ-51:** wirar o
   `sqlruntime.NewUnitOfWork` (banco único, não-2PC) para o produtor, de modo que
   a store — e a tabela `outbox` — sejam reais.
2. **O publisher é o canal, direto no commit.** Mesmo com store real, passar o
   canal como publisher da UoW faz o `Publish` acontecer logo após o commit
   (`sqlrt/uow.go.txt:90`), fora de qualquer garantia de durabilidade — um crash
   entre commit e `Publish` perde o evento. É exatamente o padrão que REQ-42.6
   proíbe.

### 4.2. Fix na raiz: enfileirar na tx, publicar pelo relay

O fluxo-alvo (§3.2a), por peça de codegen:

```
UseCase/Handle emite PublicEvent cross-service
        │
        ▼
tx.EnqueueOutbox(ev)   ── atômico com Append, na MESMA *sql.Tx  [REQ-51.1]
        │  commit
        ▼
tabela outbox (durável)
        │
relay DurableOutbox.Start(ctx)  ── publisher = ChannelTransport(rabbitmq)
        │                                                        [REQ-51.2/4]
        ▼
Publish no broker  →  MarkDelivered  (falha ⇒ attempts++, re-tenta)
```

Peças concretas:

**(P1) UnitOfWork sql de banco único para o produtor** (pré-condição, REQ-51.5).
Generalizar o wiring sql para reconhecer um módulo produtor de banco único: abrir
a conexão real (mesma `databaseConnectionGo`/`provider.openFunc` de
`emitXADatabaseWiring`), montar o `EventStore` sql, e construir
`sqlruntime.NewUnitOfWork(db, EventRegistry(), dialect, …)` em vez de
`runtime.NewUnitOfWork(store)`. Isolar num helper análogo a `emitXADatabaseWiring`
(p.ex. `emitSingleDatabaseWiring` em `sql_wiring.go`), acionado por uma nova
marca de módulo (`moduleMarks.singleDatabase`, calculada em `codegen.go` quando o
módulo produtor tem exatamente 1 Database real e um canal de saída — sem 2PC).

**(P2) Enfileirar o PublicEvent cross-service via `tx.EnqueueOutbox`**
(REQ-51.1/51.4). **Rota resolvida na revisão da PR #37 — é a (b), no wiring da
UoW, sem tocar o corpo gerado.** Auditoria do lowering de `emit`
(`codegen/lower/stmt.go:1904-1919`): dentro de um Handle, `emit X(...)` vira
`events = append(events, &X)` — acumula numa slice `events []runtime.Event` que
o UseCase apensa (`Append`) atomicamente; o publisher da UoW então publica os
apensados **após** o commit (`rtsrc/uow.go.txt:114-120` /
`sqlrt/uow.go.txt:90-96`). Confirmado no `shop/Orders`: `Handle Place { emit
OrderPlaced(...) }` + `Apply OrderPlaced` — o `PublicEvent` É apensado ao stream
e está em `tx.appended`.

Logo o enqueue não precisa mudar o lowering nem o corpo do UseCase: basta a UoW
do produtor, **antes** do commit, enfileirar via `tx.EnqueueOutbox` os eventos
apensados **cujo tipo o canal de saída carrega** — e **deixar de publicá-los**
pós-commit (a troca de publisher é P3). Concretamente, `sqlruntime.NewUnitOfWork`
ganha um parâmetro opcional "conjunto de `event_type` carregados pelo canal"
(os `PublicEvent` do módulo, `buckets[module].pubEvents`, REQ-51.4): no `Run`,
após `fn` e **antes** de `Commit`, `tx.EnqueueOutbox(<apensados desse conjunto>)`;
os demais apensados (eventos de domínio internos) seguem só no stream, nunca no
outbox nem no canal. Eventos locais de módulos sem canal seguem 100% inalterados.
  - Vantagem da rota (b): o corpo gerado do UseCase/Handle fica byte-idêntico —
    a mudança é isolada na construção da UoW e no `main.go` (P1/P3/P4).
  - Filtro (REQ-51.4): sem ele, um evento de domínio interno apensado junto
    (não-`PublicEvent`) seria serializado e enviado cross-process à toa. O
    publisher in-memory de hoje podia publicar tudo (o consumidor filtra por
    assinatura); o outbox→broker precisa ser preciso.

**(P3) Trocar o publisher da UoW do produtor** (REQ-51.3). Em
`generateCmdMainFile`, quando o produtor tem outbox durável ativo (P1),
**não** passar o canal para `NewUnitOfWork` — o canal vira o `publisher` do
`DurableOutbox` (P4), não da UoW. Elimina a publicação dupla.

**(P4) DurableOutbox do produtor com o canal como publisher + relay/cleanup**
(REQ-51.2/4). Construir `runtime.NewDurableOutbox(outboxStore, registry,
channelTransport)` para o produtor (o 3º argumento já existe desde J2.4 e está
testado por `sql_outbox_channel_test.go`) e emitir `StartOutboxRelay(ctx)` /
`StartOutboxCleanup(ctx)` em `main.go` (mesma mecânica de J2.5, hoje só usada
pelo lado consumidor). O `registry` é montado com as factories dos `PublicEvent`
que o canal carrega (via `contracts.EventRegistry()` mesclado, o mecanismo de
R8/J3.1). Roteamento por `event_type` (REQ-51.4): no recorte (um canal de saída
por produtor), todo evento enfileirado vai para esse único canal — o relay com
`publisher != nil` já roteia toda linha entregue para `publisher.Publish`
(`rtsrc/outbox.go.txt:312`).

### 4.3. Interação com limitações pré-existentes (fronteiras do recorte)

- `generateCmdMainFile` recusa combinar, no mesmo service, um módulo com
  Dispatcher (Policy local / Query cacheada) e um módulo produtor de canal
  (F5/G3, `codegen.go:1121`). Fora do escopo — o recorte é o produtor "puro"
  (UseCase + canal), como `shop/Orders`.
- `producerChannelFor` já garante **no máximo um** canal de saída `queue` por
  módulo — o recorte de REQ-51.4 (um canal) é o que o código já permite.
- A Policy **consumidora** cross-service (info.channel != nil) continua usando
  `NewOutbox(<canal>)`, **não** o DurableOutbox (anotado em
  `anchor_fixture_test.go`): REQ-51 é sobre o **produtor** alimentar o canal, não
  sobre o consumidor. As duas metades permanecem provadas separadamente, mas
  agora o produtor ganha a durabilidade que faltava.

### 4.4. Impacto nos exemplos e no exerciser (NFR-25, corrigido)

**`wallet` e `shop` permanecem byte-idênticos** (correção da validação da PR
#37):

- `wallet` — sem canal de saída ⇒ nunca ativa.
- `shop/Orders` — Database postgres real, mas canal `via: queue` **sem
  `provider:`** (`QueueChannel` in-memory) ⇒ não satisfaz a condição de ativação
  (§4.1) ⇒ `cmd/sales/main.go` continua `NewUnitOfWork(store, ordersChannel)`,
  byte-idêntico. O `shop` é um demo single-process; seu canal in-memory não é um
  transporte real, então a durabilidade cross-service não se aplica (limitação
  documentada do Marco F, inalterada). `driver.TestGenerateShopE2E*` sem
  regressão, **sem** regeneração de golden.

**Exerciser = fixture com `provider: "rabbitmq"`:**

- A **fixture-âncora de J6** (`codegen/anchor_fixture_test.go`), cujo
  `AnchorOrders` é postgres + canal `queue provider: "rabbitmq"`, **passa a
  ativar** o caminho: suas asserções de wiring de `AnchorOrders` mudam
  deliberadamente (abre o Database, monta o OutboxStore, enfileira, sobe o
  relay em vez de `NewUnitOfWork(store, canal)`). É o exerciser pretendido, não
  uma regressão de exemplo publicado.
- Uma **fixture sintética dedicada** (nova, `codegen/producer_outbox_test.go`,
  espelhando como o lado consumidor ganhou `decl_policy_outbox_test.go`): 1
  módulo produtor (postgres + canal rabbitmq) + 1 consumidor — foco no wiring do
  produtor, isolado da complexidade multi-provider da âncora.
- O teste comportamental fim-a-fim do "crash simulado" roda sobre **sqlite** real
  + um `fakePublisher` (mesmo padrão de `sql_outbox_channel_test.go`), sem
  RabbitMQ vivo: o relay não abre broker em `go build`/`go vet` (só em runtime),
  e o `Publish` é substituído pelo `fakePublisher` que falha na 1ª tentativa.

### 4.5. Arquivos e testes

Arquivos (rota (b) resolvida — corpo gerado do UseCase/Handle **não** muda):
- `codegen/codegen.go` — detecção do produtor durável (`durableProducer`, §4.1:
  Database real + canal `provider:"rabbitmq"`); trocar o publisher da UoW; emitir
  o relay/cleanup do produtor em `generateCmdMainFile`.
- `codegen/sql_wiring.go` — `emitSingleDatabaseWiring` (P1) + `OutboxStore` do
  produtor + resolução do conjunto de `event_type` carregados pelo canal.
- `codegen/sqlrt/uow.go.txt` (+ `rtsrc/uow.go.txt` só para o parâmetro na
  interface, se necessário) — `NewUnitOfWork` do produtor recebe o conjunto de
  `event_type` do canal e, no `Run`, enfileira esses apensados via
  `tx.EnqueueOutbox` **antes** do `Commit`, em vez de publicá-los pós-commit
  (P2/REQ-51.4). O caminho sem esse conjunto (todos os outros módulos) fica
  byte-idêntico.
- **NÃO tocados:** `codegen/lower/stmt.go` (emit) e o corpo do UseCase — a rota
  (b) os deixa intactos; golden/e2e de `wallet` e `shop` — byte-idênticos (§4.4).
- Fixtures de teste atualizadas: `codegen/anchor_fixture_test.go` (asserções de
  `AnchorOrders`, exerciser real) + nova `codegen/producer_outbox_test.go`.

Testes (par NFR-4):
- **wiring (positivo do produtor durável):** fixture sintética dedicada (produtor
  Database real + canal `queue provider:"rabbitmq"`) — `main.go` abre a conexão,
  monta o OutboxStore, constrói `NewDurableOutbox(store, registry, <canal>)`,
  **não** passa o canal para `NewUnitOfWork`, e sobe `StartOutboxRelay`/
  `StartOutboxCleanup`; a UoW do produtor recebe o conjunto de `event_type` do
  canal.
- **wiring (byte-identidade / negativo):** um produtor sem a condição de ativação
  — canal in-memory sem provider (a forma do `shop`) OU sem Database real —
  continua com `NewUnitOfWork(store, <canal>)`/`NewUnitOfWork(store)` direto,
  byte-idêntico (REQ-51.6, NFR-25).
- **comportamental (crash simulado):** sobre sqlite real + `fakePublisher`, um
  evento emitido é enfileirado atômico (linha em `outbox` E `events` na mesma tx;
  um evento de domínio interno NÃO vai ao outbox — REQ-51.4); um `Publish` que
  falha na 1ª tentativa deixa a linha não entregue (`attempts++`); o `Tick`
  seguinte re-publica — nenhum evento cross-service perdido (estende
  `sql_outbox_channel_test.go` para o caminho gerado do produtor, não só o seam
  manual).

### 4.6. Alternativa rejeitada

**Manter a publicação direta no commit e só "tentar de novo" em memória** —
rejeitada: não sobrevive a um crash do processo entre commit e publish (a
memória some), que é exatamente a janela que REQ-42.6 existe para fechar. Só a
tabela outbox durável (linha persistida na mesma tx) fecha a janela.

---

## 5. Estratégia de testes (consolidada, NFR-4/NFR-26)

- Cada REQ entrega o par positivo/negativo descrito na sua seção.
- REQ-49/REQ-50 rodam no escopo do seu pacote (`go test ./parser/…` /
  `go test ./codegen/ -run …Cache…`).
- REQ-51 combina teste de wiring (asserção de string sobre o gerado, sem infra
  viva) + comportamental sobre sqlite real (`gentest.WriteFiles`/`RunTests`,
  sem broker) + atualização de golden e2e do `shop`.
- Fechamento: `.claude/issues.md` marca as três `RESOLVED`; `gaps.md` §G-4
  "Residual aberto" perde o item do produtor→outbox→canal; `state.md` marca o
  Marco K `done`.
