# M1.4: produtor durável escreve no banco real, mas toda Query do service lê da `store` em memória
- SPEC: correcoes-issues-6-8-12
- TASK: M1.4
- DESCRIPTION: Achado de leitura durante a task de design M1.4 (wiring de
  service com múltiplos produtores e Dispatcher, REQ-55.7/55.8) —
  explicitamente **fora** desse escopo (REQ-55.11: "bloqueio adicional e
  independente" vira issue própria, nunca amplia REQ-55 silenciosamente).

  Um módulo que é ao mesmo tempo **produtor durável** (Marco K —
  `durableProducer`, `codegen/sql_wiring.go:178`: Database real reconhecido +
  canal de saída `via: queue provider: "rabbitmq"`) e dono de Query(ies)
  sobre os MESMOS Aggregates que ele produz tem um mismatch de leitura:

  - `emitSingleDatabaseWiring` (`codegen/sql_wiring.go:388`) troca a
    `UnitOfWork` desse módulo para uma conexão SQL real
    (`sqlruntime.NewOutboxUnitOfWork`) — os Aggregates do módulo passam a
    ser persistidos **somente** no banco real.
  - `generateCmdMainFile` (`codegen/codegen.go:1304`) constrói **sempre**
    `store := runtime.NewMemoryEventStore()`, e é essa MESMA `store` em
    memória — nunca o banco real — que alimenta `newMux(store)`/
    `newGRPCServer(store)` (linhas ~1479/1487): o lado de LEITURA de TODAS
    as rotas HTTP/gRPC do service, incluindo as Query do próprio módulo
    produtor.

  Resultado: uma Query do módulo produtor durável sobre um Aggregate que ele
  mesmo escreve lê da `store` em memória, sempre vazia para esses dados —
  devolve lista vazia silenciosamente, mesmo com dados reais no banco. É
  miscompilação silenciosa de LEITURA: o Go gerado compila e roda sem erro, o
  resultado é semanticamente errado.

  **Onde reproduz.** `testdata/projects/pizzeria`: `Sales` é produtor durável
  (`Database MainDb { provider: "postgres" }` + canal `Sales -> Kitchen`
  `provider: "rabbitmq"`) e dono de `GetAvailableMenu`/`GetActiveOrders`
  (`sales/read.ds`), que leem `MenuItem`/`Order` — Aggregates que `Sales`
  grava no Postgres real via a UoW durável, nunca em `store`.

  **Por que bloqueia M1.6, não M1.4.** M1.4/M1.5 fecham as duas guardas de
  REQ-55.7/55.8 (múltiplos produtores; produtor + Dispatcher) — este achado é
  ortogonal, sobre QUAL store uma Query lê, não sobre quem publica no
  Dispatcher. Mesmo com as duas guardas fechadas, M1.6 (REQ-55.9: `dsc gen` +
  `go build`/`go vet` sobre o `pizzeria`) só prova **compilação** — não prova
  que `GetAvailableMenu`/`GetActiveOrders` devolvem dados corretos. O design
  de [design.md](../specs/correcoes-issues-9-10-11/design.md) §4.1 já documentava a `store` em
  memória como "não o banco declarado" para o produtor durável, mas nunca
  precisou lidar com uma Query do MESMO módulo lendo seu próprio estado,
  porque o recorte original de Marco K excluía Dispatcher/Query cacheada do
  módulo produtor — o `pizzeria` reintroduz exatamente essa combinação.

  **Não é decisão de design em aberto nem ambiguidade da spec da
  linguagem** — é gap de implementação em `codegen` (a variável `store` de
  `generateCmdMainFile` ignora qual módulo é produtor durável). Fica
  registrado aqui para ser resolvido antes ou junto de M1.6, sem ampliar
  REQ-55.7/55.8 (o escopo de M1.4/M1.5) por conta própria.
- SOLVED: FALSE
