# Requirements — Conclusão das dívidas remanescentes (ISSUE-6, ISSUE-8, ISSUE-12)

> Ciclo de **manutenção** (Marco M), sucessor direto do Marco L
> (`correcoes-issues-6-7-8`). O Marco L fechou ISSUE-7 (colisão de `Wire`) e
> parte de ISSUE-6, mas parou com três tasks bloqueadas e nove pendentes,
> **porque quatro das suas premissas se mostraram erradas durante a execução**
> (L1.3d, L2.1, L2.2, L2.3). Este ciclo retoma exatamente o que sobrou — com
> cada premissa **verificada no código antes de virar task**, não depois.
>
> O que o Marco L já entregou (L1.1, L1.2, L1.3a-c, L2.1) está commitado e
> **não é trabalho deste ciclo**; a spec `correcoes-issues-6-7-8` fica encerrada
> como está e esta a sucede.

## 1. Introdução

### 1.1. Objetivo

Fechar o remanescente de três issues, cada uma corrigida na causa-raiz **já
confirmada por reprodução**, até onde o escopo de codegen/runtime/sema alcança:

- **REQ-55 (ISSUE-12, `codegen/decl_query.go` + `codegen/codegen.go` +
  `codegen/rtsrc/`):** `list <Aggregate>` — enumerar instâncias de um Aggregate
  no Read Side — **não existe** no back-end, e é o que trava o `pizzeria` hoje.
  Somado a isso, o `pizzeria` bate em duas guardas de wiring de service. É o
  item de maior valor: fecha um gap de codegen genuíno *e* desbloqueia o
  exemplo inteiro (hoje em `KNOWN_UNGENERATABLE` no CI).
- **REQ-56/57/58/59 (ISSUE-6, `codegen/gentest*.go`, `codegen/decl_saga.go`,
  `codegen/lower/`, `codegen/rtsrc/`):** a fatia de semântica de teste que o
  Marco L não alcançou — `emit` em passo de Saga (hoje **miscompilação
  silenciosa**, não erro claro), `mock ... returns X` com valor efetivo,
  shrinking de `property`, e `rolledback` provando reversão real.
- **REQ-60 (ISSUE-8, `sema/rules_warnings.go`):** o refino de cobertura §22.7
  para granularidade **por ramo de `Error`** — que a análise de raiz deste ciclo
  mostrou **viável em `sema` sem re-arquitetura** — mais a reclassificação
  documentada dos itens que dependem do spec da linguagem.
- **REQ-61:** o fechamento documental — delimitações (`acesso NEGADO`,
  `released`) e atualização de `gaps.md`/`.claude/issues/`.

### 1.2. Alinhamento filosófico com o spec

- **Premissa verificada antes de virar task.** A lição do Marco L: quatro tasks
  descreviam um fix que não correspondia ao código. Toda análise de raiz aqui
  foi confirmada por leitura **e** reprodução (ver `design.md` §7); onde a
  verificação mudou o escopo, a task já nasce com o escopo real.
- **Nunca trocar um gap por uma miscompilação.** Uma forma não suportada deve
  produzir **erro de geração claro**, jamais Go que não compila (REQ-56.1) ou
  que compila com semântica errada.
- **Fix na raiz, não no fixture.** `list <Aggregate>` é um gap de codegen
  provider-agnóstico; reescrever as Queries do `pizzeria` para contorná-lo foi
  explicitamente rejeitado (`design.md` §7).
- **A fronteira do spec da linguagem é parada, não adivinhação.** O que exige
  sintaxe/semântica que o spec não define (`released`, acesso NEGADO, §4.4,
  §25) é **delimitado e documentado**, nunca inventado.

### 1.3. Escopo

| Em escopo | Fora de escopo |
|---|---|
| `list <Aggregate> [cláusulas] as <View>` em `EmitQuery` (REQ-55) | Providers reais por trás do Read Side (issue própria, G-4) |
| Seam de enumeração de streams no runtime in-memory (REQ-55) | Enumeração no adapter `database/sql` (`sqlrt`) — opt-in, ciclo futuro |
| Wiring de service com múltiplos produtores de canal + Dispatcher (REQ-55) | Reescrever a topologia do `pizzeria` para evitar as guardas |
| `emit` em passo de Saga: erro claro, depois semântica (REQ-56) | `released` (§22.3) — sem semântica definida no spec |
| `result = call Adapter(...)` (§18.2) e `mock ... returns X` (REQ-57) | Contrato de resposta de Adapter que exija nova gramática de front-end |
| Shrinking determinístico de `property` (REQ-58) | Framework de property testing genérico |
| Staging na memory UoW → `rolledback` real (REQ-59) | Staging no `sqlrt` (já transacional de verdade) |
| Cobertura §22.7 por ramo de `Error` (REQ-60) | Cobertura cruzando `*.test.ds` × ramos de `Query`/`Policy` |
| Delimitação e reclassificação documentadas (REQ-61) | Implementar §4.4 (gatilho GDPR) e §25 (agregações/FFI) |

**Fora, com motivo (não reabrir estas issues por eles):**

- **Cenário de acesso NEGADO em teste** (ISSUE-6): a gramática de §22 não tem
  "como o caller X". Exige um ciclo de front-end (léxico→parser→resolver→sema),
  mesma natureza de ISSUE-2.
- **`released` (§22.3)** (ISSUE-6): aparece **uma única vez** em todo o spec
  (dentro de um exemplo) e não tem definição operacional em lugar nenhum;
  `grep '"released"'` no código devolve zero ocorrências. Não é implementável
  sem antes DEFINIR a semântica no spec da linguagem.
- **Gatilho de redação GDPR (§4.4)** e **agregações/aritmética/FFI de §25**
  (ISSUE-8 a/c): o próprio spec os marca "em evolução / a definir".

### 1.4. Pré-condição e baseline

O que já existe e **não** é trabalho deste ciclo:

- **Marco L, fatia entregue:** `Wire` combinado para módulo com UseCase+Policy
  (`emitCombinedWireFunc`), o call site em `generateCmdMainFile`,
  `lowerAccessCondition` com `caller.hasRole(...)` puro, `emitApply` com
  `BuiltinLowerer`, e `then state { ... }` de Aggregate ponta a ponta
  (`ast/test.go` → `parser/parse_testfile.go` → `codegen/gentest.go`).
- **Read Side (Marco I):** `tryEmitListVO` (`list <VO>` correlacionado a um
  campo `AppendList<VO>` de um Aggregate), `emitLoadAsView`,
  `emitHoistedQueryReturn` e `emitHoistedJoinReturn` — o caminho de cláusulas
  (`where`/`orderBy`/`skip`/`take`/`as`) **já existe e é reusável**.
- **Runtime in-memory:** `EventStore` com `Append`/`Load` por `aggregateID`,
  tagueado por tenant (§13.2); `memoryUnitOfWork` cujo commit/rollback são
  no-op documentado; `runtime.Query[T]`/`collection.go.txt`.
- **Geração de testes `*.test.ds`** (Marco H4 + L2.1), cobrindo §22.1–22.6 no
  caminho feliz.
- **`checkHandleErrorCoverage`** (`sema/rules_warnings.go`, REQ-5.22) na
  granularidade **por Handle**, com `handleRaisesError` já percorrendo cada
  `ensure … else <Error>` e `testedErrorHandles` já lendo `ThenClause.Error`.

### 1.5. Glossário (incremental)

| Termo | Definição |
|---|---|
| **Seam de enumeração** | Interface opcional do runtime que lista os `aggregateID` de um tipo de Aggregate, descoberta por type assertion sobre `EventStore` — nunca um método novo na interface `EventStore` (quebraria `sqlrt` e os dublês de teste). |
| **`list <Aggregate>`** | Forma de Query que enumera **todas as instâncias** de um Aggregate (`list KitchenTicket t where … as V`), distinta de `list <VO>`, que lista os itens de um campo `AppendList<VO>` de **uma** instância. |
| **Miscompilação silenciosa** | Geração que sai com código 0 e produz Go que não compila (ou compila com semântica errada) — pior que erro de geração, porque só falha no `go build` do usuário. |
| **Ramo de erro** | Um `ensure <cond> else <Error>` no corpo de um Handle; a unidade de cobertura que §22.7 pede, mais fina que "o Handle tem algum cenário de erro". |
| **Delimitar** | Documentar explicitamente que um item fica fora, com o motivo estrutural (falta de gramática ou de definição no spec), em vez de implementá-lo por adivinhação. |

---

## 2. Requisitos Funcionais

> Formato EARS. "THE SYSTEM" = o back-end (`codegen` + `runtime` vendorizado) e,
> em REQ-60, o checker semântico (`sema`).

### REQ-55 — `list <Aggregate>` no Read Side e desbloqueio do `pizzeria` (ISSUE-12)

**User story:** Como autor de DomainScript, quero escrever
`Query GetBoardTickets() -> List<KitchenTicketVW> { return list KitchenTicket t
where … orderBy … as KitchenTicketVW }` e ter o back-end gerado, em vez de
`dsc gen` falhar com "forma não suportada por EmitQuery".

**Critérios de aceitação:**

1. THE SYSTEM SHALL expor, no runtime vendorizado, um **seam de enumeração**
   que lista os `aggregateID` já escritos para um tipo de Aggregate, respeitando
   o filtro de tenant de §13.2 que `Load` já aplica, implementado pela store
   in-memory.
2. THE SYSTEM SHALL descobrir esse seam por **type assertion sobre
   `runtime.EventStore`**, nunca por um método novo na interface `EventStore` —
   `sqlrt.EventStore` e os dublês de teste de `codegen/` continuam satisfazendo
   `EventStore` sem alteração (NFR-32).
3. WHEN uma Query devolve `list <Aggregate>` **sem cláusula nenhuma**, THE
   SYSTEM SHALL emitir Go que enumera os ids, carrega e reconstrói cada
   instância via o `Load<Agg>` já gerado, e devolve a projeção declarada.
4. WHEN uma Query devolve `list <Aggregate>` **com cláusulas**
   (`where`/`orderBy`/`skip`/`take`/`as`), THE SYSTEM SHALL reusar o caminho de
   cláusulas já existente (`emitHoistedQueryReturn`, Marco I), aplicando-o sobre
   a coleção enumerada — sem duplicar a lógica de filtro/ordenação/projeção.
5. IF o seam de enumeração não estiver disponível na store configurada, THE
   SYSTEM SHALL produzir um **erro claro em tempo de execução ou de geração**,
   nomeando o Aggregate e a Query — jamais Go que não compila nem uma lista
   silenciosamente vazia.
6. THE SYSTEM SHALL manter **byte-idêntica** a saída de toda Query já suportada
   hoje (`load … as V`, `list <VO>` com e sem cláusulas, join) — a mudança
   afeta exclusivamente a forma `list <Aggregate>`, hoje um erro (NFR-31).
7. THE SYSTEM SHALL suportar, em `generateCmdMainFile`, um service com **mais de
   um módulo produtor de canal de saída "queue"** — hoje um erro de geração
   explícito (`codegen/codegen.go`, guarda F5).
8. THE SYSTEM SHALL suportar, no MESMO service, um módulo produtor de canal de
   saída **e** um módulo que precisa de `Dispatcher` (Policy local, Query
   cacheada ou Metric) — hoje um erro de geração explícito (guarda F5/G3).
9. THE SYSTEM SHALL provar o fechamento com o exemplo **`pizzeria`**:
   `dsc gen docs/examples/pizzeria` sai com código 0 e o projeto Go gerado
   **compila** (`go build`/`go vet`), com teste e2e no padrão
   `driver.TestGenerate*`.
10. THE SYSTEM SHALL remover `pizzeria` da lista `KNOWN_UNGENERATABLE`
    (`.github/workflows/ci.yml`, job `examples`).
11. WHERE a geração do `pizzeria` revelar um bloqueio **adicional e
    independente** dos mapeados aqui, THE SYSTEM SHALL registrá-lo como nova
    issue (skill `issue-generator`) em vez de ampliar silenciosamente REQ-55.

### REQ-56 — `emit` em passo de Saga: da miscompilação silenciosa à semântica definida (ISSUE-6)

**User story:** Como autor de DomainScript, quero que um `emit` dentro de
`up`/`down`/`onInfraError` de uma Saga ou funcione, ou falhe com uma mensagem
clara — nunca gere Go quebrado.

**Critérios de aceitação:**

1. WHEN o corpo de um passo de Saga contém `emit <Evento>(...)`, THE SYSTEM
   SHALL falhar a geração com mensagem clara, em vez de emitir Go que
   referencia uma variável `events` inexistente no escopo do passo — fechando a
   miscompilação silenciosa reproduzida em `design.md` §7.
2. THE SYSTEM SHALL registrar em `design.md`, **antes de qualquer código de
   semântica**, a rota escolhida para um passo de Saga emitir (dispatcher
   publish-only, `Tx` no passo, ou delimitação), com o motivo das descartadas.
3. WHERE a rota escolhida em REQ-56.2 for implementável neste ciclo, THE SYSTEM
   SHALL emitir o evento pelo canal escolhido e o projeto gerado SHALL compilar.
4. WHERE REQ-56.3 for entregue, THE SYSTEM SHALL passar a aceitar
   `<Subject> emitted <Evento>(...)` no `then` de um Test de Saga (§22.3), hoje
   rejeitado pelo `default` de `emitSagaThenAssert`.
5. THE SYSTEM SHALL manter **byte-idêntica** a saída de qualquer Saga que não
   use `emit` em passo (wallet/shop e as fixtures de `gentest_saga_test.go`).

### REQ-57 — `mock ... returns X` com valor efetivo (ISSUE-6)

**User story:** Como autor de `*.test.ds`, quero que `mock PaymentRequest
returns PaymentResult(...)` influencie o fluxo do passo seguinte, em vez de o
valor ser construído e descartado.

**Critérios de aceitação:**

1. THE SYSTEM SHALL registrar em `design.md`, antes de código, o **contrato de
   resposta de um Adapter** — hoje inexistente: `Call<Nome>` devolve só `error`
   e nenhuma seção do spec define tipo de resposta para `Adapter`/`Notification`.
2. WHERE o contrato de REQ-57.1 for implementável sem nova gramática de
   front-end, THE SYSTEM SHALL suportar `result = call <Adapter>(...)` (§18.2,
   a forma do próprio exemplo de Saga do spec), hoje um erro explícito em
   `codegen/lower/builtins.go` (`QueryExpr.Op "call"`).
3. WHERE REQ-57.2 for entregue, THE SYSTEM SHALL fazer o valor `X` de
   `mock <Target> returns X` ser o **retorno efetivo** do alvo mockado,
   substituindo o `_ = <expr>` de hoje em `emitSagaMock`.
4. IF o contrato de REQ-57.1 exigir gramática nova, THE SYSTEM SHALL delimitar
   REQ-57.2/57.3 com o motivo registrado, entregando apenas o que couber.
5. THE SYSTEM SHALL manter **byte-idêntica** a saída de um passo que chama o
   Adapter como statement (a forma de hoje) e de todo cenário sem `mock`.

### REQ-58 — Shrinking do contra-exemplo de `property` (ISSUE-6)

**User story:** Como autor de `*.test.ds`, quero que uma `property` que falha
reporte a **menor** sequência que ainda reproduz a falha, não a sequência
completa.

**Critérios de aceitação:**

1. WHEN uma `property` (§22.5) falha, THE SYSTEM SHALL encolher a sequência de
   passos (remoção/bissecção, re-testando cada candidata) até o mínimo que ainda
   viola a invariante, e reportar **essa** sequência como contra-exemplo.
2. THE SYSTEM SHALL manter o shrinking **determinístico** (NFR-13): o mesmo
   programa e a mesma falha produzem o mesmo contra-exemplo mínimo, sobre a
   semente fixa derivada de `(Test.Name, Property.Name)` que
   `gentest_property.go` já usa — nunca `time.Now`.
3. WHEN uma `property` passa, THE SYSTEM SHALL não executar shrinking algum
   (sem custo no caminho verde).
4. THE SYSTEM SHALL manter a saída gerada de uma `property` byte-idêntica no que
   não for o caminho de falha (NFR-31).

### REQ-59 — `rolledback` com reversão real: staging na memory UoW (ISSUE-6)

**User story:** Como autor de `*.test.ds`, quero que `then { … rolledback }`
prove que **nada foi escrito**, não apenas que houve erro.

**Critérios de aceitação:**

1. THE SYSTEM SHALL dar **staging** a `memoryTx`: os eventos de `Append` durante
   um `Run` ficam num buffer e só são aplicados à `EventStore` quando `fn`
   retorna `nil`; num erro, são descartados.
2. THE SYSTEM SHALL preservar **read-your-writes** dentro do mesmo `Run`: um
   `Tx.Load` posterior a um `Tx.Append` no mesmo `Run` enxerga os eventos
   staged, na ordem, concatenados ao que já está na store.
3. THE SYSTEM SHALL preservar a semântica de publicação pós-commit: os eventos
   continuam entregues a `Publisher.Publish` na ordem de append, só depois de
   `fn` retornar `nil`.
4. THE SYSTEM SHALL fazer `rolledback` (§22.2) afirmar que a store ficou
   **intacta**, não só que `err != nil`.
5. THE SYSTEM SHALL manter o núcleo do runtime em **stdlib pura** — nenhuma
   dependência nova, nenhum import de `database/sql` (NFR-32).
6. THE SYSTEM SHALL manter verdes os testes comportamentais existentes da memory
   UoW/EventStore (commit continua durável) e byte-idêntica a saída de wallet e
   shop (NFR-31).

### REQ-60 — Cobertura §22.7 por ramo de `Error` (ISSUE-8)

**User story:** Como mantenedor, quero que o aviso de cobertura aponte **qual
ramo de regra de negócio** não tem cenário de erro, não apenas que o Handle não
tem nenhum.

**Critérios de aceitação:**

1. THE SYSTEM SHALL coletar, por Handle, o **conjunto de `Error` levantados**
   pelos seus `ensure … else <Error>` — informação que `handleRaisesError` já
   percorre hoje, mas reduz a um booleano.
2. THE SYSTEM SHALL coletar, por Handle sob teste, o **conjunto de `Error`
   asseverados** pelos cenários `then error <Nome>` — informação que
   `ThenClause.Error` já carrega como nome, mas `testedErrorHandles` reduz a um
   booleano.
3. WHEN um Handle sob teste levanta um `Error` que nenhum cenário assevera, THE
   SYSTEM SHALL emitir um warning **nomeando esse `Error`**, não só o Handle.
4. WHEN todos os `Error` levantados por um Handle têm cenário correspondente,
   THE SYSTEM SHALL permanecer em silêncio.
5. THE SYSTEM SHALL manter o comportamento de hoje para Aggregates sem nenhum
   `Test` (ausência total de teste não é falta de cobertura de ramo) e a
   severidade `Warning` (nunca `Error`) — o programa continua compilando.
6. THE SYSTEM SHALL preservar o determinismo dos diagnósticos (NFR-3): a ordem
   dos warnings não depende de iteração de mapa.

### REQ-61 — Fechamento documental: delimitações e reclassificações

**User story:** Como mantenedor, quero que cada issue toque este ciclo com um
estado preciso — resolvida, parcialmente resolvida com resíduo apontado, ou
reclassificada — para nenhuma delas continuar sendo um saco indefinido.

**Critérios de aceitação:**

1. THE SYSTEM SHALL documentar em `.claude/specs/codegen/gaps.md` e na issue de
   ISSUE-6 que o **cenário de acesso NEGADO** e o verbo **`released`** exigem
   definição no spec da linguagem / nova gramática, apontados para um ciclo de
   front-end.
2. THE SYSTEM SHALL **reclassificar** o gatilho de redação GDPR (§4.4) e os
   itens de §25 (avg/min/max/group by, aritmética estendida, marshalling FFI)
   de "dívida de codegen" para **"aguardando definição no spec da linguagem"**,
   em `gaps.md` e na issue de ISSUE-8.
3. THE SYSTEM SHALL atualizar `.claude/issues/open-issues.md` e cada arquivo de
   issue tocado com o estado final, e o ponteiro de `.claude/state.md` conforme
   as regras do `CLAUDE.md`.

---

## 3. Requisitos Não-Funcionais (incrementais)

> Os NFRs dos ciclos anteriores continuam valendo (NFR-1..30). Abaixo, os deste
> ciclo.

### NFR-31 — Não-regressão e byte-identidade dos exemplos reais

`wallet` e `shop` permanecem **byte-idênticos** (determinismo NFR-13/21), assim
como toda forma de Query, Saga e `*.test.ds` que já gerava. `pizzeria` **passa a
gerar** — é a mudança de comportamento esperada; sua saída é cobertura nova, não
regressão de golden. Cada task que altera um emissor entrega, junto do par
NFR-4, a guarda de byte-identidade da forma vizinha que ela **não** deve tocar.

### NFR-32 — Núcleo do runtime sem deps externas e sem quebra de interface

O seam de enumeração (REQ-55) e o staging (REQ-59) são `codegen/rtsrc/` puro,
stdlib apenas. Nenhum dos dois **altera a interface `runtime.EventStore`**: a
enumeração entra como interface **opcional**, descoberta por type assertion, de
modo que `sqlrt.EventStore` e os dublês de teste em `codegen/` continuam
compilando sem alteração. Reafirma NFR-12/NFR-30.

### NFR-33 — Par positivo/negativo por correção, e nenhuma miscompilação silenciosa

Cada sub-parte fechada entrega o par exigido pelo `CLAUDE.md` (NFR-4): um
programa que viola a regra (esperando o diagnóstico exato) e um correto
(esperando silêncio). Adicionalmente, **toda forma não suportada que este ciclo
descobrir deve produzir erro de geração claro** — nunca Go que não compila. O
escopo de teste é o da task, não a suíte inteira; CI roda o resto.

---

## 4. Rastreabilidade

| Requisito | Tema | Issue | Pacote/arquivo-raiz | Marco |
|---|---|---|---|---|
| REQ-55 | `list <Aggregate>` + wiring de service + `pizzeria` | ISSUE-12 | `codegen/decl_query.go`, `codegen/codegen.go`, `codegen/rtsrc/eventstore.go.txt` | M1 |
| REQ-56 | `emit` em passo de Saga | ISSUE-6 | `codegen/decl_saga.go`, `codegen/lower/stmt.go`, `codegen/gentest.go` | M2 |
| REQ-57 | `mock … returns X` com valor efetivo | ISSUE-6 | `codegen/lower/builtins.go`, `codegen/decl_io.go`, `codegen/gentest.go` | M3 |
| REQ-58 | Shrinking de `property` | ISSUE-6 | `codegen/gentest_property.go` | M4 |
| REQ-59 | `rolledback` com staging | ISSUE-6 | `codegen/rtsrc/uow.go.txt`, `codegen/gentest.go` | M4 |
| REQ-60 | Cobertura §22.7 por ramo de `Error` | ISSUE-8 | `sema/rules_warnings.go` | M5 |
| REQ-61 | Delimitações e reclassificações | ISSUE-6, ISSUE-8, ISSUE-12 | `.claude/specs/codegen/gaps.md`, `.claude/issues/` | M5 |

---

## 5. Critérios de Pronto (Definition of Done)

O ciclo está completo quando:

1. `list <Aggregate>` gera, com e sem cláusulas, sobre o seam de enumeração;
   `dsc gen docs/examples/pizzeria` sai 0 e o projeto Go **compila**; `pizzeria`
   sai de `KNOWN_UNGENERATABLE`; wallet/shop byte-idênticos (REQ-55).
2. `emit` em passo de Saga não miscompila mais — ou funciona, ou dá erro claro —
   com a rota registrada em `design.md` (REQ-56).
3. `mock … returns X` influencia o fluxo, ou está delimitado com o contrato de
   resposta de Adapter registrado em `design.md` (REQ-57).
4. `property` que falha reporta contra-exemplo **mínimo e determinístico**
   (REQ-58); `rolledback` prova store intacta, com read-your-writes preservado
   (REQ-59).
5. O warning de §22.7 nomeia o `Error` não coberto, com par NFR-4 (REQ-60).
6. `go build ./...` / `go vet ./...` / `gofmt -l .` limpos; testes de escopo de
   cada task verdes; CI verde na PR da spec.
7. `gaps.md`, `.claude/issues/` e `.claude/state.md` refletem o estado final:
   **ISSUE-12** → `RESOLVED`; **ISSUE-6** → resolvida na fatia fechada, com
   `acesso NEGADO` e `released` apontados para um ciclo de front-end;
   **ISSUE-8** → fechada em (b) e reclassificada em (a)/(c) (REQ-61).

### Entrega incremental (marcos)

Todo o ciclo é o **Marco M**, em cinco fases independentes entre si (qualquer
ordem ou paralelo; dentro de cada fase há dependência linear):

- **Fase M1 — "Read Side e o desbloqueio do `pizzeria`" (REQ-55):** seam de
  enumeração, `list <Aggregate>` sem e com cláusulas, as duas guardas de wiring
  de service, prova e2e e limpeza do CI. É a fase de maior valor e a única que
  toca o CI.
- **Fase M2 — "`emit` em passo de Saga" (REQ-56):** erro claro primeiro
  (independente e imediato), depois design e implementação da semântica.
- **Fase M3 — "`mock … returns X`" (REQ-57):** contrato de resposta de Adapter,
  `result = call …` (§18.2), e o mock com valor efetivo.
- **Fase M4 — "Semântica dos testes gerados" (REQ-58, REQ-59):** shrinking de
  `property` e staging da memory UoW — as duas independentes entre si.
- **Fase M5 — "`sema` e fechamento" (REQ-60, REQ-61):** cobertura §22.7 por
  ramo, reclassificações e o fechamento documental do Marco.
