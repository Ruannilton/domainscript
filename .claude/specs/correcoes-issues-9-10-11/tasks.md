# Tasks — Correções de dívida técnica (ISSUE-9, ISSUE-10, ISSUE-11)

## Como ler este plano

Marco K, manutenção. Três fases independentes (K1/K2/K3), uma por issue. Cada
task referencia o REQ que satisfaz `(REQ-n)` e a seção de design `(§design x)`.
Regras de execução do CLAUDE.md valem: **uma task por vez**, commit atômico com
a árvore verde no escopo da task, par de testes positivo/negativo (NFR-4), e
uma PR por task concluída.

Ordem: **K1 → K2 → K3** (risco crescente). K1 e K2 são pequenas e sem
pré-condições; K3 é o resíduo do Marco J e traz subtasks. As três fases não
dependem umas das outras — a ordem é só de conveniência (fechar barato primeiro).

Convenção de commit (Conventional Commits em PT imperativo, CLAUDE.md):
`fix(parser): …`, `fix(codegen): …`.

---

## Fase K1 — Parser: duas atribuições consecutivas (ISSUE-11, REQ-49)

- [ ] **K1.1** Guarda de fim-de-linha no binding/alias opcional de operação de
  domínio. (REQ-49.1/49.2/49.3/49.4, §design 2.2)
  - `parser/parser.go`: helper `sameLineAsPrev()` — compara `p.cur().Pos.Line`
    com `p.lastPos.Line`.
  - `parser/parse_query.go`: adicionar `&& p.sameLineAsPrev()` à guarda do
    `binding` em `parseQueryOp` e à guarda do `alias` no `case "join"` de
    `parseOneClause`.
  - Comentário curto no código apontando a causa-raiz (ISSUE-11: DomainScript
    separa statements por linha; o identificador opcional não pode cruzar essa
    fronteira).
  - **Testes pareados (NFR-4)**, `parser/…_test.go`:
    - positivo/regressão: `order = load Bar(id)` + `x = id` (e variante
      `x = 1`) no mesmo bloco → dois `AssignStmt`, **zero** diagnóstico; variante
      com `join` numa linha e `x = …` na seguinte (REQ-49.3).
    - preserva binding legítimo: `list Ticket t where …` (mesma linha) → binding
      `t`; `join Bar b` (mesma linha) → alias `b`.
  - DoD: `go test ./parser/ -run <novos>` verde; `go build ./...` limpo.

---

## Fase K2 — Runtime: `Coalesce` à prova de pânico (ISSUE-10, REQ-50)

- [ ] **K2.1** `defer` de limpeza em `memoryQueryCache.Coalesce`.
  (REQ-50.1/50.2/50.3, §design 3.2)
  - `codegen/rtsrc/querycache.go.txt`: instalar, antes de `fn()`, o `defer` que
    faz `delete(c.flights, key)` sob lock e `close(fl.done)` — idêntico ao padrão
    já corrigido em `redisQueryCache.Coalesce` (`codegen/redisrt/cache.go.txt`,
    revisão da PR #26). Sem `recover` (o pânico segue propagando).
  - Comentário apontando a paridade com o adapter Redis (para os dois não voltarem
    a divergir).
  - **Testes pareados (NFR-4)**, `codegen/…_test.go` via
    `gentest.WriteFiles`/`RunTests` sobre o pacote runtime gerado:
    - negativo (pânico): `fn` que panica; uma 2ª goroutine no mesmo voo é
      liberada (não trava — `recover` no teste + timeout/WaitGroup); a MESMA
      chave coalesce de novo depois.
    - positivo (não-regressão): N goroutines na mesma chave recebem o mesmo
      resultado, `fn` roda uma vez.
  - DoD: `go test ./codegen/ -run <novos>` verde; `go build ./...` limpo.

---

## Fase K3 — Produtor Outbox → canal cross-service (ISSUE-9, REQ-51)

> O resíduo do Marco J. Subdividido para que cada peça seja verificável
> isoladamente. K3.1 (pré-condição) precede as demais; K3.2–K3.4 constroem o
> fluxo; K3.5 é a fixture-âncora fim-a-fim; K3.6 fecha o golden do `shop`.

- [ ] **K3.1** **(pré-condição)** Wiring do UnitOfWork `database/sql` de
  **Database único** não-2PC para um módulo produtor. (REQ-51.5, §design 4.1/4.2-P1)
  - `codegen/codegen.go`: nova marca `moduleMarks.singleDatabase` (módulo produtor
    com exatamente 1 Database real reconhecido + canal de saída `queue`, sem 2PC).
  - `codegen/sql_wiring.go`: `emitSingleDatabaseWiring` (análogo a
    `emitXADatabaseWiring`) — abre a conexão (`databaseConnectionGo`/
    `provider.openFunc`), monta o `EventStore` sql, wira
    `sqlruntime.NewUnitOfWork(db, <pkg>.EventRegistry(), dialect)`.
  - Nesta task ainda **sem** enqueue/relay: a store passa a ser sql real, mas o
    publisher continua o de hoje — provar que só a troca de store não muda o
    comportamento observável (paridade in-memory ↔ sql, NFR-22).
  - **Testes pareados:** fixture sintética (produtor 1 Database real) gera
    `NewUnitOfWork` sql + smoke compile; um produtor sem Database real
    (`"pg"` decorativo) permanece com a store in-memory, byte-idêntico (NFR-25).
  - DoD: escopo verde; `wallet` byte-idêntico (sem Database de saída); `go build`/
    `go vet`/`gofmt` limpos.

- [ ] **K3.2** Enfileirar o `PublicEvent` cross-service via `tx.EnqueueOutbox`.
  (REQ-51.1, §design 4.2-P2)
  - Auditar o lowering de `emit`/o wrapper do UseCase e **decidir** entre a rota
    (a) lowering roteia por tipo, ou (b) a UoW do produtor enfileira
    automaticamente os eventos apensados cujo `event_type` tem canal de saída
    (preferência do design se a informação puder ser passada na construção da UoW).
  - Implementar a rota escolhida: para um evento cujo tipo é carregado pelo canal
    de saída do módulo, `tx.EnqueueOutbox([]Event{ev})` dentro da tx (atômico com
    `Append`); eventos locais inalterados.
  - **Testes pareados** (comportamental, sqlite real via
    `gentest.WriteFiles`/`RunTests`): um `emit` de evento com canal grava linha na
    tabela `outbox` **e** em `events` na mesma tx (rollback desfaz as duas); um
    `emit` de evento **local** não grava no outbox.
  - DoD: escopo verde; `go build`/`go vet`/`gofmt` limpos.

- [ ] **K3.3** Trocar o publisher da UoW do produtor + construir o DurableOutbox
  do produtor com o canal como `publisher`. (REQ-51.2/51.3, §design 4.2-P3/P4)
  - `codegen/codegen.go` (`generateCmdMainFile`): quando o produtor tem outbox
    durável ativo, **não** passar o canal para `NewUnitOfWork`; construir
    `runtime.NewDurableOutbox(outboxStore, <registry-dos-PublicEvent-do-canal>,
    <canalTransport>)` e emitir `StartOutboxRelay(ctx)`/`StartOutboxCleanup(ctx)`.
  - Registry montado com as factories dos `PublicEvent` que o canal carrega
    (`contracts.EventRegistry()` mesclado — mecanismo de R8/J3.1).
  - **Testes pareados (wiring):** `main.go` do produtor durável constrói
    `NewDurableOutbox(…, <canal>)`, **não** passa o canal para `NewUnitOfWork`, e
    sobe relay/cleanup; produtor sem Database real segue `NewUnitOfWork(store,
    <canal>)` direto (byte-idêntico, NFR-25).
  - DoD: escopo verde; `go build`/`go vet`/`gofmt` limpos.

- [ ] **K3.4** Roteamento do relay por `event_type` para o canal (recorte: 1
  canal). (REQ-51.4, §design 4.2-P4)
  - Confirmar (e cobrir por teste) que, com `publisher != nil`, o relay do
    produtor roteia toda linha entregue para o `ChannelTransport` do canal — no
    recorte de um único canal de saída, sem lógica de dispatch por tipo adicional.
    (Se a auditoria de K3.2/K3.3 já cobrir isto por construção, esta task vira só
    a asserção de teste que fixa o invariante.)
  - **Teste:** evento enfileirado é publicado no canal (`fakePublisher`), não em
    handler local; um 2º tipo de evento local (se houver) **não** vaza para o
    canal.
  - DoD: escopo verde.

- [ ] **K3.5** Fixture-âncora fim-a-fim: crash simulado não perde o evento.
  (REQ-51.7, §design 4.4/4.5)
  - Fixture sintética (ou extensão da âncora de J6) com produtor Database real +
    canal `queue provider:"rabbitmq"`: sobre **sqlite** real + `fakePublisher`
    (sem broker vivo), um `emit` enfileira atômico; `Publish` que falha na 1ª
    tentativa deixa a linha não entregue (`attempts++`); o `Tick` seguinte
    re-publica — nenhum evento perdido. Exercita o **caminho gerado do produtor**
    (não só o seam manual de `sql_outbox_channel_test.go`).
  - DoD: escopo verde; smoke compile do projeto gerado limpo.

- [ ] **K3.6** Atualizar o golden/e2e do `shop` (mudança deliberada) + docs.
  (REQ-51 fechamento, §design 4.4, NFR-25)
  - Regenerar o golden do `shop` e ajustar `driver.TestGenerateShopE2E*`: o
    `cmd/sales/main.go` agora abre o Database, monta o OutboxStore, enfileira e
    sobe o relay em vez de `NewUnitOfWork(store, ordersChannel)`. Diff justificado
    no commit (enquadramento do ripple de J1.2). `wallet` byte-idêntico.
  - Garantir `ensureModTidyIfNeeded`/`t.Skip` onde o e2e precisar (o relay não
    abre broker no build/vet).
  - Docs: `.claude/specs/codegen/gaps.md` §G-4 "Residual aberto" — remover o item
    produtor→outbox→canal (agora fechado); `.claude/issues.md` — ISSUE-9
    `RESOLVED (commit <hash>)`; `CLAUDE.md`/`README.md` se afirmarem que o
    produtor publica direto no commit.
  - DoD: `go build`/`go vet`/`gofmt` limpos; e2e do `shop` verde com o novo
    golden; `wallet` sem regressão.

---

## Fechamento do Marco K

- [ ] **K.fim** Revisão de DoD (requirements §5): as três issues fechadas com par
  de testes; golden do `shop` atualizado e justificado; `wallet` byte-idêntico;
  `.claude/issues.md` marca ISSUE-9/10/11 `RESOLVED`; `.claude/state.md` marca o
  Marco K `done`; `gaps.md` §G-4 atualizado. (Sem `go test ./...` local no
  fechamento — CI roda a suíte nas PRs, CLAUDE.md.)

---

## Rastreabilidade REQ → Tasks

| REQ | Tasks | Issue |
|---|---|---|
| REQ-49 | K1.1 | ISSUE-11 |
| REQ-50 | K2.1 | ISSUE-10 |
| REQ-51.5 (pré-condição) | K3.1 | ISSUE-9 |
| REQ-51.1 | K3.2 | ISSUE-9 |
| REQ-51.2/51.3 | K3.3 | ISSUE-9 |
| REQ-51.4 | K3.4 | ISSUE-9 |
| REQ-51.7 | K3.5 | ISSUE-9 |
| REQ-51.6 / NFR-25 (fechamento) | K3.1/K3.3/K3.6 | ISSUE-9 |

## Mapa de dependências

```
K1.1   (independente)
K2.1   (independente)
K3.1 ──▶ K3.2 ──▶ K3.3 ──▶ K3.4 ──▶ K3.5 ──▶ K3.6 ──▶ K.fim
       (K3.1 é pré-condição de todo o fluxo do produtor)
```
