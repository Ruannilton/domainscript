# Requirements — Correções de dívida técnica (ISSUE-9, ISSUE-10, ISSUE-11)

> Ciclo de **manutenção** (Marco K). Não é greenfield: cada requisito fecha
> uma issue já registrada em `.claude/issues.md`, encontrada durante specs
> anteriores e deixada em aberto por estar **fora do escopo** da task que a
> descobriu. O objetivo aqui é corrigir cada uma **na raiz**, com o par de
> testes positivo/negativo que o CLAUDE.md exige (NFR-4).

## 1. Introdução

### 1.1. Objetivo

Fechar três dívidas técnicas independentes, cada uma corrigida na causa-raiz
(não contornada):

- **REQ-49 (ISSUE-11, `parser/`):** o parser falha ao analisar duas atribuições
  simples consecutivas dentro do mesmo bloco (`a = load X(id)` seguido de
  `b = id`). Front-end — atravessa só o `parser/`.
- **REQ-50 (ISSUE-10, `codegen/rtsrc/`):** `memoryQueryCache.Coalesce` vaza
  goroutines e trava a chave para sempre se a função coalescida entra em pânico
  (falta o `defer` que o adapter Redis análogo já ganhou). Núcleo do runtime
  vendorizado.
- **REQ-51 (ISSUE-9, `codegen/`):** o lado **produtor** do Outbox durável nunca
  foi ligado — um módulo que emite um `PublicEvent` para um canal cross-service
  ainda publica direto no commit (fora da tx), exatamente o que REQ-42.6 proíbe.
  Back-end — o resíduo aberto do Marco J.

Os três são **ortogonais** (arquivos e pacotes distintos, nenhuma dependência
de ordem entre eles). São reunidos num único ciclo por serem todos dívida de
manutenção pequena/média rastreada; a ordem de execução (K1 → K2 → K3) põe as
correções pequenas e de baixo risco primeiro.

### 1.2. Baseline (o que já existe — não é trabalho deste ciclo)

- O parser recursivo-descendente completo (`parser/`, REQ-2/3), incluindo
  `parseSimpleStmt` (atribuição vs. expressão-statement) e `parseQueryOp`
  (operações de domínio prefixas `load`/`list`/`count`/… com binding e cláusulas
  SQL-like).
- `runtime.QueryCache`/`memoryQueryCache` (`codegen/rtsrc/querycache.go.txt`,
  Marco G3) e o seu par distribuído `redisQueryCache`
  (`codegen/redisrt/cache.go.txt`, J4.1) — este último **já corrigido** para o
  bug do `Coalesce` sob pânico (o padrão `defer` que REQ-50 vai replicar).
- Todo o mecanismo de Outbox durável do Marco J: `runtime.Tx.EnqueueOutbox`
  (J2.1), a tabela `outbox` + `Dialect` de leitura/consumo (J2.1/J2.2),
  `runtime.DurableOutbox` + relay `Start`/`Tick`/`Cleanup` (J2.3/J2.5), o
  roteamento opcional para um `publisher`/`ChannelTransport` (J2.4,
  `NewDurableOutbox(store, registry, publisher)`), e o transporte real RabbitMQ
  (`codegen/amqprt`, J3). **O seam do produtor existe e está testado
  isoladamente** (`codegen/sql_outbox_channel_test.go`) — o que falta é o
  **codegen ligar os dois lados**.
- O wiring do adapter `database/sql` para o caminho **2PC** (`emitXADatabaseWiring`,
  `sql_wiring.go`) e para o **outbox consumidor** (`emitOutboxDatabaseWiring`,
  J2.5). O que **não** existe ainda é o wiring sql para um Database **único**
  não-2PC (hoje um UseCase de banco único "degenera em commit local" sobre a
  store in-memory, §design codegen 3.8) — pré-condição de REQ-51 (ver design).

### 1.3. Escopo

Dentro:

- Correção de raiz das três issues, com testes pareados (NFR-4).
- Para REQ-51, o **recorte mínimo** que fecha REQ-42.6 de verdade: **um** módulo
  produtor com **um** Database real e **um** canal de saída `via: queue provider:
  "rabbitmq"` — exatamente a forma do `shop/Orders` e da fixture-âncora de J6.
  Fechar isso exige a pré-condição do UnitOfWork sql de banco único (§design 3.1).

Fora (registrado, para ciclos futuros — não reabrir estas issues por eles):

- Qualquer categoria de provider fora do recorte de 5 do Marco J (ISSUE-3 residual:
  outros bancos, gRPC/HTTP/stream como canal, Idempotency `external`, Cache
  `layered`, GCS/Azure).
- Múltiplos canais de saída por módulo produtor, ou um módulo que combine
  Dispatcher local (Policy/Query cacheada) **e** canal de saída no mesmo service
  (limitação pré-existente de `generateCmdMainFile`, F5/G3 — fora do escopo).
- Vendorização/build offline real (`-mod=vendor`, R10 do Marco J) — resíduo
  separado, não coberto aqui.

## 2. Requisitos Funcionais

### REQ-49 — Parser: duas atribuições simples consecutivas (ISSUE-11)

**User story:** Como autor de DomainScript, quero escrever duas atribuições em
linhas consecutivas dentro do mesmo bloco (`order = load Bar(id)` seguido de
`x = id`) sem que o parser produza um erro de sintaxe espúrio, porque as duas
são gramaticalmente válidas (dois `AssignStmt` independentes).

1. THE SYSTEM SHALL parsear, dentro de um mesmo bloco de statements, um
   `AssignStmt` cujo RHS é uma operação de domínio (`order = load Bar(id)`)
   seguido imediatamente por um segundo `AssignStmt` (`x = id`), produzindo
   **dois** `ast.AssignStmt` distintos — sem erro de sintaxe, sem consumir o
   identificador do 2º statement como parte do 1º.
2. THE SYSTEM SHALL preservar a semântica atual do **binding** de operação de
   domínio quando ele é legítimo (`list Ticket t where …`) — a correção não pode
   quebrar `parseQueryOp`/`parseOneClause` para o caso em que o identificador
   opcional realmente é um binding/alias na MESMA linha.
3. THE SYSTEM SHALL manter a mesma garantia para o **alias de `join`**
   (`join Foo f` — o outro ponto que consome um identificador opcional com a
   mesma heurística), se a análise de raiz confirmar que ele partilha a mesma
   causa (ver design §2.1).
4. WHERE o segundo statement começa numa nova linha, THE SYSTEM SHALL tratar essa
   quebra de linha como fim do statement anterior para fins da decisão do binding
   opcional (a causa-raiz identificada: DomainScript separa statements por linha,
   mas o parser não usava essa informação nesses pontos ambíguos).
5. THE SYSTEM SHALL cobrir a correção com um teste **positivo** (o programa das
   duas atribuições consecutivas parseia limpo, sem diagnóstico) e um teste que
   **preserva o binding legítimo** (`list X x where …` continua produzindo o
   binding) — NFR-4.

### REQ-50 — Runtime: `memoryQueryCache.Coalesce` à prova de pânico (ISSUE-10)

**User story:** Como operador de um serviço gerado, quero que um pânico dentro de
uma Query coalescida não trave para sempre toda goroutine concorrente esperando
o resultado, nem impeça aquela chave de voltar a coalescer.

1. THE SYSTEM SHALL garantir que, se `fn()` entra em pânico dentro de
   `memoryQueryCache.Coalesce` (`codegen/rtsrc/querycache.go.txt`), o
   `close(fl.done)` e a remoção da chave de `c.flights` **ainda executam** — toda
   goroutine bloqueada em `<-fl.done` é liberada e a chave volta a poder coalescer.
2. THE SYSTEM SHALL deixar o pânico original **continuar propagando** normalmente
   após a limpeza (nenhum `recover` que engula o pânico) — o comportamento fica
   idêntico ao do adapter Redis já corrigido (`redisQueryCache.Coalesce`, J4.1):
   um `defer` que fecha o canal e remove a chave, sem recuperar o pânico.
3. THE SYSTEM SHALL manter o caminho **sem pânico** byte-idêntico em
   comportamento ao atual (uma chamada normal coalesce e limpa exatamente como
   antes) — a mudança é só de robustez sob falha.
4. THE SYSTEM SHALL cobrir com um teste **negativo** (uma `fn` que entra em
   pânico: uma 2ª goroutine esperando o mesmo voo não trava — o teste termina; a
   MESMA chave coalesce de novo depois) e um teste **positivo** de não-regressão
   (coalescência normal continua servindo o mesmo resultado a todos os
   esperadores) — NFR-4.

### REQ-51 — Codegen: produtor Outbox → canal cross-service (ISSUE-9, REQ-42.6)

**User story:** Como arquiteto de um sistema distribuído, quero que um
`PublicEvent` emitido por um módulo produtor e destinado a outro service seja
**enfileirado no outbox durável dentro da transação de negócio** e publicado no
canal cross-service **pelo relay**, não direto no commit — só assim a entrega
cross-service herda a durabilidade at-least-once que REQ-42.6 promete.

1. THE SYSTEM SHALL, para um módulo produtor que (a) possui um Database real
   (`recognizedSQLProvider`) e (b) produz um `PublicEvent` que atravessa um canal
   de saída `via: queue` com provider real, **enfileirar** esse evento via
   `tx.EnqueueOutbox` **atomicamente** com a transação de negócio (mesma tx do
   `Append`), em vez de depender da publicação direta do publisher da
   UnitOfWork no commit.
2. THE SYSTEM SHALL construir, para esse módulo produtor, um
   `runtime.DurableOutbox` com o `ChannelTransport` do canal como `publisher`
   (o 3º argumento de `NewDurableOutbox`, já suportado desde J2.4), e iniciar o
   seu relay (`Start`/`StartOutboxRelay`) e o cleanup (`Cleanup`/
   `StartOutboxCleanup`) em `cmd/<service>/main.go`.
3. THE SYSTEM SHALL **deixar de passar o canal** como publisher da UnitOfWork do
   produtor (`generateCmdMainFile` hoje emite `runtime.NewUnitOfWork(store,
   <canal>)`) quando o outbox durável do produtor está ativo — para que nenhum
   evento seja publicado **duas vezes** (uma inline no commit, outra pelo relay).
4. THE SYSTEM SHALL rotear, no relay, cada linha entregue para o
   `ChannelTransport` correto por `event_type` (§design 3.2a): no recorte deste
   ciclo, um único canal de saída por módulo produtor, então todo evento
   enfileirado para o outbox do produtor vai para esse canal.
5. THE SYSTEM SHALL, como **pré-condição** de (1), passar a wirar o adapter
   `database/sql` para um módulo produtor de **Database único** não-2PC (hoje só
   o caminho 2PC de 2+ Databases ganha a store SQL; um banco único degenera para
   a store in-memory, onde `Tx.EnqueueOutbox` é um no-op e nada persiste) — ver
   §design 3.1. Sem essa pré-condição, o enqueue de (1) não seria durável.
6. WHERE um módulo produtor **não** tem Database real, THE SYSTEM SHALL manter o
   comportamento de hoje (publish direto no commit sobre a store in-memory, sem
   durabilidade) — exatamente o mesmo trade-off documentado do Marco F, byte-
   idêntico (NFR-25).
7. THE SYSTEM SHALL provar o fluxo fim-a-fim com uma fixture-âncora que combine
   Database real + canal `rabbitmq` no produtor: um "crash simulado" entre o
   commit e a publicação no canal **não perde** o evento — a linha fica no
   outbox, não entregue, e o próximo `Tick` a publica (teste comportamental sobre
   sqlite real, mesmo padrão de `sql_outbox_channel_test.go`); e um teste de
   **wiring** confirmando que `main.go`/`policies.go`/o código gerado do UseCase
   passam a enfileirar+relay em vez de publicar direto (NFR-4).

## 3. Requisitos Não-Funcionais

### NFR-25 — Sem regressão; determinismo; byte-identidade onde aplicável

- Um programa que **não** exercita nenhuma das três correções gera projeto
  byte-idêntico ao de hoje (determinismo NFR-13/21 do Marco J preservado).
- **Exceção deliberada e esperada** (REQ-51): o exemplo real `shop` **muda** de
  saída gerada — `shop/Orders` é o exerciser real do produtor→outbox→canal
  (Database postgres + UseCase `PlaceOrder` + canal `Orders -> Shipping` via
  queue). Essa mudança é o **comportamento correto novo**, não uma regressão:
  o golden/`driver.TestGenerateShopE2E*` é atualizado deliberadamente, com o
  diff justificado (mesmo enquadramento do ripple postgres de J1.2). `wallet`
  (sem canal de saída) permanece byte-idêntico.
- REQ-49 e REQ-50 não alteram nenhum byte de projeto gerado para os exemplos
  existentes (são correções de parser e de runtime vendorizado sob falha).

### NFR-26 — Par de testes positivo/negativo por correção (reafirma NFR-4)

Cada uma das três correções entrega o par exigido pelo CLAUDE.md: um teste que
exercita o caminho corrigido e um teste que prova que o caminho legítimo/feliz
não regrediu. Escopo de teste por task (não a suíte inteira) — CI roda o resto.

### NFR-27 — Núcleo do runtime sem deps externas (reafirma NFR-12)

A correção de REQ-50 é `rtsrc/` puro (stdlib `sync`/`time` já importados) — não
introduz nenhuma dependência nova. A de REQ-51 mantém toda dep externa (driver
sql, amqp) atrás do seam opt-in já existente; o núcleo `runtime/` não ganha
import de `database/sql` nem de amqp.

## 4. Rastreabilidade

| Issue | REQ | Pacote/arquivo-raiz | Marco de origem |
|---|---|---|---|
| ISSUE-11 | REQ-49 | `parser/parse_query.go` (+ `parse_stmt.go`) | transpilador (front-end) |
| ISSUE-10 | REQ-50 | `codegen/rtsrc/querycache.go.txt` | codegen G3 |
| ISSUE-9  | REQ-51 | `codegen/codegen.go`, `sql_wiring.go`, `decl_policy.go`, lowering de `emit` | infra-providers (Marco J, resíduo) |

## 5. Critérios de Pronto (Definition of Done)

O ciclo está completo quando:

1. `a = load X(id)` seguido de `b = id` no mesmo bloco parseia limpo; o binding
   legítimo (`list X x`) e o alias de `join` continuam funcionando (REQ-49) —
   par de testes verde.
2. Um pânico em `fn()` dentro de `memoryQueryCache.Coalesce` não vaza goroutine
   nem trava a chave; o pânico ainda propaga; a coalescência normal não regrediu
   (REQ-50) — par de testes verde.
3. Um módulo produtor com Database real + canal `rabbitmq` enfileira o
   `PublicEvent` cross-service no outbox atômico e o relay publica no canal (nunca
   o commit direto); um crash simulado entre commit e publish não perde o evento
   (REQ-51) — fixture-âncora + teste de wiring verdes. O produtor sem Database
   real permanece byte-idêntico.
4. `go build ./...` / `go vet ./...` / `gofmt -l .` limpos; os testes de escopo de
   cada task verdes; o golden do `shop` atualizado deliberadamente com diff
   justificado; `wallet` sem regressão.
5. `.claude/issues.md` marca ISSUE-9/10/11 como `RESOLVED (commit <hash>)`;
   `.claude/state.md` reflete o Marco K como `done`; `.claude/specs/codegen/gaps.md`
   §G-4 "Residual aberto" atualizado (o item produtor→outbox→canal deixa de estar
   aberto).
