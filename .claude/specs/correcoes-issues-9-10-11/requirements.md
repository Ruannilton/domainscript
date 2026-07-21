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

### REQ-50 — Runtime: `Coalesce` à prova de pânico, sem retorno nulo silencioso (ISSUE-10)

**User story:** Como operador de um serviço gerado, quero que um pânico dentro de
uma Query coalescida não trave para sempre as goroutines concorrentes esperando
o resultado, não impeça aquela chave de voltar a coalescer, e **não devolva um
resultado nulo silencioso** aos esperadores (que causaria um segundo pânico no
wrapper gerado).

> **Refinamento pós-revisão (PR #37, Gemini Code Assist + validação própria):**
> a versão original deste requisito pedia só "mirror do `defer` do Redis". A
> validação contra o wrapper gerado (`codegen/decl_query_cache.go:491-504`:
> `result, err := Coalesce(...); if err != nil { return zero, err }; return
> result.(T), nil`) mostrou que, num pânico do líder, o esperador recebe
> `(nil, nil)` e cai em `nil.(T)` — **um segundo pânico** para qualquer tipo de
> valor. Pior ainda: o adapter Redis "já corrigido" tem o MESMO defeito (o fix
> da PR #26 fechou o vazamento de goroutine, mas ainda entrega `(nil, nil)` ao
> esperador). Logo o fix de raiz **injeta um erro** e vale para os **dois**
> backends (ver REQ-50.5).

1. THE SYSTEM SHALL garantir que, se `fn()` entra em pânico dentro de
   `memoryQueryCache.Coalesce` (`codegen/rtsrc/querycache.go.txt`), o
   `close(fl.done)` e a remoção da chave de `c.flights` **ainda executam** — toda
   goroutine bloqueada em `<-fl.done` é liberada e a chave volta a poder coalescer.
2. THE SYSTEM SHALL fazer com que os esperadores liberados recebam um **erro
   apropriado** (nunca `(nil, nil)` silencioso): um `defer` com uma flag de
   controle (ex. `completed`) que, quando o corpo NÃO chegou ao fim (pânico),
   atribui um erro-sentinela a `fl.err` **antes** de `close(fl.done)` — assim o
   wrapper gerado entra no ramo `if err != nil` e nunca executa `result.(T)`
   sobre um valor nulo. (Solução sugerida pela revisão da PR #37 e validada
   contra o wrapper gerado.)
3. THE SYSTEM SHALL deixar o pânico original do **líder** continuar propagando
   normalmente (nenhum `recover` que o engula) — a flag e o erro-sentinela
   protegem só os esperadores; a goroutine que panica segue desenrolando a pilha.
4. THE SYSTEM SHALL manter o caminho **sem pânico** byte-idêntico em
   comportamento ao atual: `completed = true` após `fn()` retornar preserva
   `fl.err` como o erro que `fn` de fato devolveu (um erro de negócio legítimo
   NÃO é sobrescrito pelo sentinela — o `defer` só injeta quando `!completed`).
5. THE SYSTEM SHALL aplicar o **mesmo** endurecimento (flag + erro-sentinela a
   esperadores no pânico) a `redisQueryCache.Coalesce`
   (`codegen/redisrt/cache.go.txt`), que hoje tem só o `defer` de limpeza
   (fecha/remove) mas ainda devolve `(nil, nil)` ao esperador sob pânico — os
   dois backends compartilham o wrapper gerado e o mesmo defeito, então a
   correção mantém-nos consistentes E corretos.
6. THE SYSTEM SHALL cobrir cada backend com um par de testes (NFR-4): **negativo**
   (uma `fn` que entra em pânico — uma 2ª goroutine no mesmo voo é liberada, não
   trava, e recebe um **erro não-nulo**; a MESMA chave coalesce de novo depois) e
   **positivo** de não-regressão (coalescência normal serve o mesmo resultado a
   todos os esperadores; `fn` roda uma vez; um erro de negócio legítimo continua
   sendo propagado como antes).

### REQ-51 — Codegen: produtor Outbox → canal cross-service (ISSUE-9, REQ-42.6)

**User story:** Como arquiteto de um sistema distribuído, quero que um
`PublicEvent` emitido por um módulo produtor e destinado a outro service **por um
canal com transporte real (rabbitmq)** seja **enfileirado no outbox durável
dentro da transação de negócio** e publicado no canal **pelo relay**, não direto
no commit — só assim a entrega cross-service herda a durabilidade at-least-once
que REQ-42.6 promete.

> **Condição de ativação (validada na revisão da PR #37 — corrige a versão
> original):** o caminho produtor-durável ativa **somente** quando o módulo tem
> (a) um Database real (`recognizedSQLProvider`) **E** (b) um canal de saída com
> **provider real** (`via: queue provider: "rabbitmq"`). A durabilidade só
> existe com transporte real (§design infra-providers 3.2a: "A durabilidade
> cross-service só existe quando há Database real *e* canal com provider real").
> Consequência importante: o exemplo `shop` produz para um canal `via: queue`
> **sem** `provider:` (a `QueueChannel` in-memory, mesmo processo) — logo **não
> ativa** o caminho e permanece **byte-idêntico** (corrige a alegação anterior de
> que o `shop` mudaria). O exerciser real é uma fixture com `provider:
> "rabbitmq"` (a âncora de J6, cujo `AnchorOrders` = postgres + canal rabbitmq,
> já se encaixa).

1. THE SYSTEM SHALL, para um módulo produtor que satisfaz a condição de ativação
   acima, **enfileirar** o(s) `PublicEvent` carregado(s) pelo canal via
   `tx.EnqueueOutbox` **atomicamente** com a transação de negócio (mesma tx do
   `Append`), em vez de depender da publicação direta do publisher da UnitOfWork
   no commit.
2. THE SYSTEM SHALL construir, para esse módulo produtor, um
   `runtime.DurableOutbox` com o `ChannelTransport` do canal como `publisher`
   (o 3º argumento de `NewDurableOutbox`, já suportado desde J2.4), e iniciar o
   seu relay (`Start`/`StartOutboxRelay`) e o cleanup (`Cleanup`/
   `StartOutboxCleanup`) em `cmd/<service>/main.go`.
3. THE SYSTEM SHALL **deixar de passar o canal** como publisher da UnitOfWork do
   produtor (`generateCmdMainFile` hoje emite `runtime.NewUnitOfWork(store,
   <canal>)`) quando o caminho produtor-durável está ativo — para que nenhum
   evento seja publicado **duas vezes** (uma inline no commit, outra pelo relay).
4. THE SYSTEM SHALL **filtrar** o que é enfileirado para o outbox aos tipos de
   evento que o canal de saída de fato **carrega** (os `PublicEvent` do módulo,
   `buckets[module].pubEvents`), nunca todo evento apensado indistintamente — um
   evento de domínio interno não-`PublicEvent` (se houver) não deve atravessar o
   canal cross-service. No recorte (um canal de saída por módulo, garantido por
   `producerChannelFor`), todo evento enfileirado vai para esse único canal.
5. THE SYSTEM SHALL, como **pré-condição** de (1), passar a wirar o adapter
   `database/sql` para o módulo produtor de **Database único** não-2PC (hoje só
   o caminho 2PC de 2+ Databases ganha a store SQL; um banco único degenera para
   a store in-memory, onde `Tx.EnqueueOutbox` é um no-op e nada persiste) — ver
   §design 4.1/4.2-P1. Sem essa pré-condição, o enqueue de (1) não seria durável.
6. WHERE um módulo produtor **não** satisfaz a condição de ativação (sem Database
   real, OU canal in-memory sem provider real como o do `shop`, OU sem canal de
   saída como o `wallet`), THE SYSTEM SHALL manter o comportamento de hoje
   byte-idêntico (`shop`: publish in-memory direto no commit; `wallet`: nenhuma
   mudança) — NFR-25.
7. THE SYSTEM SHALL provar o fluxo fim-a-fim com uma fixture (dedicada + a âncora
   de J6, cujo `AnchorOrders` passa a ativar o caminho) que combine Database real
   + canal `rabbitmq` no produtor: um "crash simulado" entre o commit e a
   publicação no canal **não perde** o evento — a linha fica no outbox, não
   entregue (`attempts++`), e o próximo `Tick` a publica (teste comportamental
   sobre sqlite real + `fakePublisher`, mesmo padrão de
   `sql_outbox_channel_test.go`, exercitando o **caminho gerado do produtor**); e
   um teste de **wiring** confirmando que `main.go`/o código gerado passam a
   enfileirar+relay em vez de publicar direto (NFR-4).

## 3. Requisitos Não-Funcionais

### NFR-25 — Sem regressão; determinismo; byte-identidade dos exemplos reais

- Um programa que **não** exercita nenhuma das três correções gera projeto
  byte-idêntico ao de hoje (determinismo NFR-13/21 do Marco J preservado).
- **`wallet` E `shop` permanecem byte-idênticos** (corrige a versão anterior
  desta spec, que previa mudança no `shop`): `wallet` não tem canal de saída;
  `shop/Orders` produz para um canal `via: queue` **sem** provider real (a
  `QueueChannel` in-memory), então não satisfaz a condição de ativação de REQ-51
  e mantém `runtime.NewUnitOfWork(store, ordersChannel)` como hoje.
  `driver.TestGenerateWalletE2E*`/`TestGenerateShopE2E*` seguem sem regressão.
- **Atualização esperada de fixture de TESTE (não de exemplo real):** a
  fixture-âncora de J6 (`codegen/anchor_fixture_test.go`), cujo `AnchorOrders` é
  postgres + canal `rabbitmq`, **passa a ativar** o caminho produtor-durável —
  suas asserções de wiring de `AnchorOrders` são atualizadas deliberadamente
  (é o exerciser pretendido, não uma regressão de exemplo publicado).
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
2. Um pânico em `fn()` dentro de `memoryQueryCache.Coalesce` **e** de
   `redisQueryCache.Coalesce` não vaza goroutine nem trava a chave; os
   esperadores recebem um **erro** (nunca `nil, nil`); o pânico do líder ainda
   propaga; a coalescência normal e um erro de negócio legítimo não regridem
   (REQ-50) — par de testes verde por backend.
3. Um módulo produtor com Database real + canal `rabbitmq` enfileira o
   `PublicEvent` cross-service no outbox atômico e o relay publica no canal (nunca
   o commit direto); um crash simulado entre commit e publish não perde o evento
   (REQ-51) — fixture dedicada + âncora de J6 + teste de wiring verdes. Um
   produtor sem a condição de ativação permanece byte-idêntico.
4. `go build ./...` / `go vet ./...` / `gofmt -l .` limpos; os testes de escopo de
   cada task verdes; `wallet` E `shop` byte-idênticos (sem regeneração de
   golden dos exemplos reais); as asserções da fixture-âncora de J6 atualizadas
   deliberadamente.
5. `.claude/issues.md` marca ISSUE-9/10/11 como `RESOLVED (commit <hash>)`;
   `.claude/state.md` reflete o Marco K como `done`; `.claude/specs/codegen/gaps.md`
   §G-4 "Residual aberto" atualizado (o item produtor→outbox→canal deixa de estar
   aberto).
