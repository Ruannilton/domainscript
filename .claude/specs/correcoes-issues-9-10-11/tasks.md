# Tasks — Correções de dívida técnica (ISSUE-9, ISSUE-10, ISSUE-11)

## Como ler este plano

Marco K, manutenção. Três fases independentes (K1/K2/K3), uma por issue. Cada
task referencia o REQ que satisfaz `(REQ-n)` e a seção de design `(§design x)`.
Regras de execução do CLAUDE.md valem: **uma task por vez**, commit atômico com
a árvore verde no escopo da task, par de testes positivo/negativo (NFR-4), e
uma PR por task concluída.

Ordem: **K1 → K2 → K3** (risco crescente). As três fases não dependem umas das
outras — a ordem é só de conveniência (fechar barato primeiro). Dentro de cada
fase há dependência linear (ver o mapa no fim).

> **Nota de granularidade (revisão da PR #37):** as tasks foram refinadas para
> serem o mais independentes/pequenas possível. A única que resiste a ser
> quebrada é **K3.3** (a troca de publisher + enqueue + relay): dividi-la
> produziria um gerador incoerente (um projeto que enfileira mas nunca faz
> relay, ou que para de publicar sem ter relay) — irredutível por corretude, não
> por tamanho. Ver a nota na própria task.

Convenção de commit (Conventional Commits em PT imperativo, CLAUDE.md):
`fix(parser): …`, `fix(codegen): …`.

---

## Fase K1 — Parser: duas atribuições consecutivas (ISSUE-11, REQ-49)

- [x] **K1.1** Guarda de fim-de-linha no **binding** de operação de domínio.
  (REQ-49.1/49.2/49.4, §design 2.2)
  - `parser/parser.go`: helper `sameLineAsPrev()` — compara `p.cur().Pos.Line`
    com `p.lastPos.Line` (o último token consumido; validado que após
    `parsePostfix` do alvo, `lastPos` é o fim do alvo — ex. o `)` de `Bar(id)`).
  - `parser/parse_query.go`: adicionar `&& p.sameLineAsPrev()` à guarda do
    `binding` em `parseQueryOp`. Comentário apontando a causa-raiz (ISSUE-11:
    DomainScript separa statements por linha; o binding opcional não pode cruzar
    essa fronteira e roubar o identificador do statement seguinte).
  - **Testes pareados (NFR-4)**, `parser/…_test.go`:
    - positivo/regressão: `order = load Bar(id)` + `x = id` (e variante
      `x = 1`) no mesmo bloco → dois `AssignStmt`, **zero** diagnóstico.
    - preserva binding legítimo: `list Ticket t where …` (mesma linha) → binding
      `t` intacto.
  - DoD: `go test ./parser/ -run <novos>` verde + **suíte inteira do `parser/`**
    verde (o guarda não pode regredir nenhum fixture de binding existente);
    `go build ./...` limpo.

- [x] **K1.2** Mesma guarda no **alias de `join`**. (REQ-49.3, §design 2.1/2.2)
  - `parser/parse_query.go`: adicionar `&& p.sameLineAsPrev()` à guarda do
    `alias` no `case "join"` de `parseOneClause` (o outro ponto com a mesma
    heurística gananciosa de identificador opcional).
  - **Testes pareados:** `… join Foo` numa linha + `x = …` na seguinte → não
    consome `x` como alias; `join Bar b` (mesma linha) → alias `b` intacto.
  - DoD: escopo + suíte do `parser/` verdes; `go build ./...` limpo.

---

## Fase K2 — Runtime: `Coalesce` à prova de pânico (ISSUE-10, REQ-50)

- [x] **K2.1** `memoryQueryCache.Coalesce`: flag + erro-sentinela aos esperadores.
  (REQ-50.1/50.2/50.3/50.4, §design 3.2)
  - `codegen/rtsrc/querycache.go.txt`: `var errCoalescedPanic = errors.New(...)`
    de pacote; instalar, antes de `fn()`, o `defer` que faz `delete(c.flights,
    key)` sob lock, `if !completed { fl.err = errCoalescedPanic }` e
    `close(fl.done)`; `completed = true` após `fn()`. Sem `recover`. Adicionar o
    import `errors`. Comentário apontando a paridade com `redisQueryCache`.
  - **Testes pareados (NFR-4)**, via `gentest.WriteFiles`/`RunTests`:
    - negativo (pânico): `fn` que panica (com `recover` no líder do teste); uma
      2ª goroutine no mesmo voo é liberada sob timeout **e recebe erro não-nil**;
      a MESMA chave coalesce de novo depois.
    - positivo (não-regressão): N goroutines na mesma chave → mesmo resultado,
      `fn` roda uma vez; um `fn` com erro de negócio propaga esse erro (não o
      sentinela).
  - DoD: `go test ./codegen/ -run <novos>` verde; `go build ./...` limpo.

- [x] **K2.2** `redisQueryCache.Coalesce`: o **mesmo** endurecimento. (REQ-50.5,
  §design 3.2/3.3)
  - `codegen/redisrt/cache.go.txt`: hoje tem só o `defer` de limpeza (PR #26);
    adicionar a flag `completed` + `if !completed { fl.err = <sentinela> }` antes
    do `close(fl.done)` (o adapter já importa `errors`). Mantém os dois backends
    consistentes E corretos.
  - **Testes pareados:** mesmo par de K2.1, agora sobre o backend redis (com o
    `fakeRedis`/client injetável que os testes de J4.1 já usam — nenhuma conexão
    real).
  - DoD: escopo verde; `go build ./...` limpo.

---

## Fase K3 — Produtor Outbox → canal cross-service (ISSUE-9, REQ-51)

> O resíduo do Marco J. Ativa só sob **Database real + canal com provider real
> (`rabbitmq`)** (§design 4.1) — `wallet`/`shop` **não** ativam (byte-idênticos);
> o exerciser é a âncora de J6 (`AnchorOrders`) + uma fixture dedicada.
> **Rota (b) resolvida** (§design 4.2-P2): o enqueue mora na construção da UoW,
> o corpo gerado do UseCase/Handle **não** muda.

- [x] **K3.1** Detecção do produtor durável (predicado puro, sem emissão).
  (REQ-51 condição de ativação, §design 4.1)
  - `codegen/codegen.go` (ou `sql_wiring.go`): função `durableProducer(prog,
    module)` — true sse o módulo tem 1 Database real (`recognizedSQLProvider`) E
    um canal de saída com `channelProviderKind(ch) == "rabbitmq"`
    (`producerChannelFor`). Sem 2PC, sem Dispatcher local no mesmo service
    (respeita a guarda F5/G3 pré-existente — fora do recorte).
  - **Testes pareados (unit):** produtor postgres+rabbitmq → true; produtor
    postgres + canal in-memory sem provider (a forma do `shop`) → false; sem
    Database real → false; sem canal → false.
  - DoD: escopo verde; nenhuma mudança em projeto gerado (só um predicado ainda
    não consumido) — `wallet`/`shop`/âncora byte-idênticos.

- [x] **K3.2** `emitSingleDatabaseWiring`: store `database/sql` para o produtor de
  banco único (só a troca de store; publisher inalterado). (REQ-51.5,
  §design 4.2-P1)
  - `codegen/sql_wiring.go`: `emitSingleDatabaseWiring` (análogo a
    `emitXADatabaseWiring`, sem 2PC) — abre a conexão
    (`databaseConnectionGo`/`provider.openFunc`), monta o `EventStore` sql, wira
    `sqlruntime.NewUnitOfWork(db, <pkg>.EventRegistry(), dialect, <canal>)`
    (publisher = canal, **como hoje** — a troca de publisher é K3.3).
  - `codegen/codegen.go`: `generateCmdMainFile` usa `durableProducer` (K3.1) para
    escolher `emitSingleDatabaseWiring` em vez de `NewUnitOfWork(store, canal)`.
  - **Testes pareados:** paridade comportamental (in-memory ↔ sql, NFR-22) sobre
    a fixture do produtor; um produtor não-qualificante segue in-memory,
    byte-idêntico (NFR-25). Atualizar as asserções da âncora de J6 (`AnchorOrders`
    agora abre o Database no `main.go`) — mudança deliberada de fixture de teste.
  - DoD: escopo verde; `wallet`/`shop` byte-idênticos; `go build`/`go vet`/`gofmt`
    limpos.

- [x] **K3.3** **(troca atômica — irredutível)** Enqueue in-tx + relay + trocar o
    publisher da UoW. (REQ-51.1/51.2/51.3/51.4, §design 4.2-P2/P3/P4)
  > **Por que não quebrar:** "enfileirar no outbox", "parar de publicar direto no
  > commit" e "subir o relay que publica" precisam valer juntos. Qualquer metade
  > isolada gera um projeto incoerente — enfileira sem entregar, ou para de
  > publicar sem ter quem publique. É atômica por corretude, não por tamanho.
  - `codegen/sqlrt/uow.go.txt` (+ `rtsrc/uow.go.txt` se a interface precisar):
    `NewUnitOfWork` do produtor recebe o conjunto de `event_type` carregados pelo
    canal (os `PublicEvent` do módulo, `buckets[module].pubEvents`); no `Run`,
    **antes** do `Commit`, `tx.EnqueueOutbox(<apensados desse conjunto>)` e
    **deixa de publicá-los** pós-commit (os demais apensados, se houver, seguem
    só no stream). Filtro de REQ-51.4.
  - `codegen/codegen.go`/`sql_wiring.go`: em `main.go`, montar o `OutboxStore`,
    construir `runtime.NewDurableOutbox(outboxStore, <registry dos PublicEvent do
    canal, via contracts.EventRegistry()>, <canalTransport>)`, **não** passar o
    canal para `NewUnitOfWork`, e emitir `StartOutboxRelay(ctx)`/
    `StartOutboxCleanup(ctx)` do produtor.
  - **Testes pareados (wiring):** `main.go` do produtor durável constrói
    `NewDurableOutbox(…, <canal>)`, não passa o canal para `NewUnitOfWork`, e sobe
    relay/cleanup; produtor não-qualificante byte-idêntico (NFR-25). Atualizar
    novamente as asserções de `AnchorOrders`.
  - DoD: escopo verde; `wallet`/`shop` byte-idênticos; `go build`/`go vet`/`gofmt`
    limpos + smoke compile da fixture do produtor.

- [ ] **K3.4** Fixture dedicada + comportamental de crash simulado fim-a-fim.
  (REQ-51.7, §design 4.4/4.5)
  - Nova `codegen/producer_outbox_test.go` (fixture sintética mínima: 1 produtor
    postgres + canal rabbitmq + 1 consumidor), espelhando `decl_policy_outbox_
    test.go` do lado consumidor.
  - **Teste comportamental** sobre **sqlite** real + `fakePublisher` (sem broker):
    um `emit` do produtor grava linha em `outbox` E `events` na mesma tx (um
    evento de domínio interno NÃO vai ao outbox — REQ-51.4); `Publish` que falha
    na 1ª tentativa deixa a linha não entregue (`attempts++`); o `Tick` seguinte
    re-publica — nenhum evento perdido. Exercita o **caminho gerado do produtor**,
    não só o seam manual de `sql_outbox_channel_test.go`.
  - DoD: escopo verde; smoke compile limpo.

- [ ] **K3.5** Docs + consolidação (fechamento de REQ-51). (§design 4.4, NFR-25)
  - `.claude/specs/codegen/gaps.md` §G-4 "Residual aberto": remover o item
    produtor→outbox→canal (agora fechado); manter os demais residuais (vendoring
    R10, categorias fora de escopo).
  - `.claude/issues.md`: ISSUE-9 → `RESOLVED (commit <hash>)`.
  - `CLAUDE.md`/`README.md`: onde afirmam que o produtor publica direto no commit
    (ISSUE-9), atualizar para "produtor com Database real + canal rabbitmq
    enfileira no outbox durável e o relay publica".
  - Confirmar `wallet`/`shop` byte-idênticos (`driver.TestGenerate*`), âncora
    atualizada e verde.
  - DoD: `go build`/`go vet`/`gofmt` limpos; e2e de `wallet`/`shop` sem
    regressão.

---

## Fechamento do Marco K

- [ ] **K.fim** Revisão de DoD (requirements §5): as três issues fechadas com par
  de testes (REQ-50 nos DOIS backends); `wallet`/`shop` byte-idênticos; âncora de
  J6 atualizada; `.claude/issues.md` marca ISSUE-9/10/11 `RESOLVED`;
  `.claude/state.md` marca o Marco K `done`; `gaps.md` §G-4 atualizado. (Sem
  `go test ./...` local no fechamento — CI roda a suíte nas PRs, CLAUDE.md.)

---

## Rastreabilidade REQ → Tasks

| REQ | Tasks | Issue |
|---|---|---|
| REQ-49 (binding) | K1.1 | ISSUE-11 |
| REQ-49 (join alias) | K1.2 | ISSUE-11 |
| REQ-50.1-4 (memory) | K2.1 | ISSUE-10 |
| REQ-50.5 (redis) | K2.2 | ISSUE-10 |
| REQ-51 ativação (predicado) | K3.1 | ISSUE-9 |
| REQ-51.5 (pré-condição store sql) | K3.2 | ISSUE-9 |
| REQ-51.1/.2/.3/.4 (troca atômica) | K3.3 | ISSUE-9 |
| REQ-51.7 (crash fim-a-fim) | K3.4 | ISSUE-9 |
| REQ-51.6 / NFR-25 / docs | K3.1/K3.2/K3.3/K3.5 | ISSUE-9 |

## Mapa de dependências

```
K1.1 ──▶ K1.2                          (parser; K1.2 reusa sameLineAsPrev de K1.1)
K2.1 ──▶ K2.2                          (cache; mesmo padrão, memory depois redis)
K3.1 ──▶ K3.2 ──▶ K3.3 ──▶ K3.4 ──▶ K3.5 ──▶ K.fim
        (K3.1 predicado; K3.2 store; K3.3 troca atômica; K3.4 prova; K3.5 docs)

Entre fases: K1, K2, K3 são independentes (podem ir em qualquer ordem/paralelo).
```
