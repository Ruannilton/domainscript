# Tasks — Back-end do Transpilador DomainScript (Geração de Go)

> Documento 3 de 3. Plano executável para `requirements.md` (REQ-14..32) via
> `design.md`. Mesmas convenções dos `tasks.md` anteriores: a ordem respeita
> dependências, fatiar **verticalmente** (um construto: emissor → lowering →
> golden test → smoke compile, antes de alargar), cada task tem **critério de
> conclusão** verificável e fecha em **commit** atômico (Conventional Commits,
> português imperativo).

## Como ler este plano

- Todas as tarefas começam `[ ]` (pendentes). **Nada do back-end está
  implementado** — o front-end e a resolução de nomes/tipos (REQ-1..13) são a
  baseline pronta e **verde**.
- `(REQ-n)` = requisito atendido; `(§design x)` = seção de design correspondente.
- Cada task lista **Toca** (pacotes/arquivos e os nós de AST/tipos concretos),
  **Conclusão** (critério verificável) e **Commit**. **Depende** aparece quando a
  ordem não é óbvia pela numeração.
- **Regra de verde dupla:** só commitar com (a) a árvore do **compilador** verde
  (`go build ./...` e `go test ./...`) **e** (b) o Go **gerado** dos exemplos
  compilando (`go build` sobre a saída — smoke compile, NFR-14). Um commit por task.
- **Escopos de commit novos:** `codegen`, `emit`, `runtime`. Reusa os existentes
  (`cli`, `driver`, `repo`, `docs`).
- **Fatias ancoradas nos exemplos reais.** O Marco E gera `docs/examples/wallet`;
  os Marcos F+ usam `docs/examples/shop` (dois services, `Policy` cross-service via
  canal `queue`). As tasks referenciam os construtos que esses arquivos de fato
  contêm — não hipóteses.

---

## Marco E — Núcleo Transacional Runnable

> Ao fim do Marco E, `dsc gen docs/examples/wallet -o <tmp>` produz um projeto Go
> que **compila e roda com `go run`, sem nenhuma dependência externa**. O smoke
> **automatizado** é comportamental in-memory (semeia estado via `given` ou construção
> completa; **sem subir socket** — §design 5/6).
>
> Inventário do wallet (o que o Marco E precisa cobrir): VOs wrapper
> (`WalletId`/`HolderName`/`TransactionDescription`/`ActiveStatus`) e compostos
> (`Money`/`StatementEntry`); `Enum TransactionType`; `Error`s
> `InactiveWallet`/`InsufficientBalance`; `Event`s `WalletCreated`/
> `DepositPerformed`/`WithdrawalPerformed`; `Aggregate Wallet` **EventSourced** com
> `access` e pares Handle/Apply; `Command`s `Deposit`/`Withdraw` (campos `ref
> Wallet`); `UseCase`s `PerformDeposit`/`PerformWithdrawal` (`timeout 5s`, `load` +
> dispatch de Handle); `View WalletView`; `Query`s `GetWallet` (`… as WalletView`) e
> `ListEntries` (`list StatementEntry`); `mod.ds` (`Module Wallet`, `Database MainDb
> manages [Wallet]`); `interface.ds` HTTP (4 rotas). Sem Policy/Worker/Saga/
> Notification/Adapter/Metric/Projection (Marcos F+); `wallet.test.ds` fica p/ H.

### Fase E0 — Setup do gerador e prep do exemplo

- [x] **E0.1** Scaffold dos pacotes: `codegen/`, `codegen/emit/`, `codegen/lower/`,
  `codegen/rtsrc/`, cada um com `doc.go`. Adicionar `driver.GenerateProject(dir, out,
  opts)` (stub que só chama `CheckProject` e recusa se `HasErrors`, imprimindo o bag)
  e reestruturar `cmd/dsc` para **subcomandos**: `dsc check <path>` (comportamento
  atual, default quando o 1º arg não é subcomando conhecido — retrocompatível) e
  `dsc gen <dir> -o <out>` (stub). _(REQ-14, REQ-32, §design 2/3.15)_
  **Toca:** `codegen/*`, `driver/driver.go` (+`GenerateProject`), `cmd/dsc/main.go`
  (`run` vira dispatcher de subcomando + parse de `-o`; hoje aceita só 1 arg
  posicional e sempre valida).
  **Conclusão:** `go build ./...` compila os pacotes vazios; `dsc gen <projeto-com-
  erro>` imprime diagnósticos e sai com **exit 1** sem gerar; `dsc check` e `dsc
  <path>` preservam o comportamento atual (exit 0/1/2).
  **Commit:** `chore(codegen): scaffold do gerador e API GenerateProject`

- [x] **E0.2** Infra de teste: helper de **golden test** (gera → compara com artefato
  versionado; roda **duas vezes** e exige bytes idênticos p/ determinismo) e helper de
  **smoke compile** (escreve o gerado num tmp, roda `go build ./...` e `go vet ./...`
  sobre a saída). _(NFR-13/14/17, §design 5)_
  **Conclusão:** ambos os helpers rodam (ainda sem emissores) sobre um caso trivial
  (um arquivo Go fixo) — golden compara, smoke compila.
  **Commit:** `test(codegen): helpers de golden test e smoke compile`

- [x] **E0.3** **Prep do exemplo wallet (pré-requisito do smoke do Marco E).**
  Declarados em `Money` os operadores que o domínio já usa mas não
  definia — `Operator +`, `Operator -`, `Operator >=` (spec §2.2) — pois `Apply` faz
  `state.balance +/- event.amount` e `Handle Withdraw` faz `ensure state.balance >=
  amount`. Sem eles o Go gerado não compilaria (design §4.2 ramo (d); §7). Os
  operadores usam a **convenção real do front-end** (campos por nome nu `amount`/
  `currency` + parâmetro `other`, **não** `self`, que o resolver não semeia em VO); e
  foram declarados os `Error`s `CurrencyMismatch`/`NegativeResult` referenciados no
  `else`. _(design §7, DoD §5.2)_
  **Toca:** `docs/examples/wallet/domain.ds`.
  **Conclusão:** ✅ `go build ./...` e `go test ./...` verdes; `TestWalletExampleClean`
  (zero diagnósticos) e `TestWalletThreeBugsRegression` (linhas intactas) passam.
  **Commit:** feito em `5421153` ("nova especificação").

### Fase E1 — Emissor, nomes e mapa de tipos

- [x] **E1.1** Pacote `emit`: `Emitter` com buffer, indentação, **imports geridos**
  (`Import(path) string` devolve o alias usável) e `Bytes()` que monta header+imports+
  corpo e roda `go/format.Source`. _(REQ-15.1/15.3, §design 3.2)_
  **Conclusão:** teste que emite um arquivo com 2 imports (um usado, verificar que um
  **não** usado faz `format.Source` falhar → apanhado como erro), confere saída
  gofmt-ada e **byte-idêntica** entre duas emissões.
  **Commit:** `feat(emit): Emitter com imports geridos e go/format`

- [x] **E1.2** `names.go`: mapeamento de identificadores — tipo (PascalCase), campo
  (exporta + tag `json` com nome original), `Operator`→método (`+`→`Add`, `-`→`Sub`,
  `*`→`Mul`, `/`→`Div`, `>=`→`Gte`, `<=`→`Lte`, `>`→`Gt`, `<`→`Lt`, `==`→`Eq`,
  `!=`→`Neq`), membro de Enum (`Tipo`+`Membro`), pacote (nome do módulo, minúsculo),
  desambiguação de **keyword Go** (sufixo `_`) e de colisão de dois símbolos no mesmo
  pacote (**sufixo numérico determinístico** na ordem de declaração), e **refs
  qualificadas por pacote** (integração com `emit.Import` p/ `contracts/` e
  cross-módulo). _(REQ-15.2, §design 3.3)_
  **Conclusão:** tabela de testes cobrindo keyword (`type`→`type_`), cada operador, o
  caso `Module Wallet` + `Aggregate Wallet` (pacote `wallet`, tipo `Wallet`, sem
  colisão) e uma ref cross-pacote (`contracts.OrderPlaced`).
  **Commit:** `feat(codegen): mapeamento de identificadores idiomáticos`

- [x] **E1.3** **Mapa de tipos primitivos → Go** e tabela de **métodos embutidos**.
  Primitivos: `integer`→`int64`, `decimal`→`runtime.Decimal` (exato, ver E3.1),
  `string`→`string`, `boolean`→`bool`, `datetime`→`time.Time`, `bytes`→`[]byte`,
  `duration`→`time.Duration`, `size`→`int64`, coleções `List<T>`→`[]T`,
  `Set<T>`→`map[T]struct{}`, `AppendList<T>`→`runtime.AppendList[T]`, `Map<K,V>`→
  `map[K]V`; `File`/`FileRef`/`FileStream`→structs do runtime. Métodos:
  `string.length()`→`len`, `AppendList.add(x)`→`.Add(x)`; `list.distinct(f)`/`sum(f)`/
  `focus(id)` (§20) **entram no Marco F** (shop). Um par ausente é **erro de geração**,
  não Go arbitrário.
  _(REQ-15.6, §design 3.3/3.6)_
  **Conclusão:** teste tabelado dos primitivos e coleções; método desconhecido →
  erro claro.
  **Commit:** `feat(codegen): mapa de tipos primitivos e métodos embutidos`

### Fase E2 — Runtime in-memory vendorado

- [x] **E2.1** `rtsrc/` com o núcleo do runtime, como `.go.txt` embutidos via
  `//go:embed`; `codegen` emite `runtime/*.go` verbatim. Conteúdo mínimo: **event
  store** (interface + in-memory, chaveado por `aggregateId`), **repositório de
  aggregate**, **dispatcher de eventos**, **unit of work** (`Run(ctx, func(Tx) error)`
  + `Tx.Append`), **idempotency store**, o contrato **`Caller`** (`Authenticated()`,
  `ID()`, `HasRole(string)` — usado pelo `access`), o tipo **`BusinessError`**
  (`Code`=nome do `Error`, comparável, `errors.Is`; `ErrForbidden`/`ErrNotFound`
  reservados), o tipo **`Decimal`** de escala fixa (`math/big.Int`, escala 4,
  half-even), **chaves tipadas de contexto** (caller/tenant/idempotency/trace, no-op
  até F/G/H), `Now(ctx)`/`UUID()` e `AppendList[T]`. Tudo atrás de interfaces (seam),
  **só stdlib**. _(REQ-16, §design 3.1/3.1a/3.7)_
  **Conclusão:** teste do compilador copia `rtsrc/`, renomeia p/ `.go`, roda `go
  build` **e** `go vet` isolados (runtime compila e é limpo); emissão byte-idêntica
  entre runs; `go.mod` do runtime sem `require`.
  **Commit:** `feat(runtime): runtime in-memory vendorado (store, uow, dispatcher, caller)`

### Fase E3 — ValueObjects e Enums

- [x] **E3.1** VO wrapper e composto → tipo/struct + `NewX` que roda `Valid` e devolve
  `(X, error)`; **sem setters** (imutável). Fixar `decimal`→`runtime.Decimal`. Semear
  no corpo de `Valid` os receptores do front-end: `value` (valor embrulhado) e `ok`
  ("validação passa" → `NewX` sem erro); no composto, campos por nome nu. _(REQ-17.1/
  17.2, §design 3.3/3.5)_
  **Conclusão (golden + smoke):** `WalletId`/`ActiveStatus`/`Money`/`StatementEntry`
  geram Go que compila; `NewWalletId("")` falha em `value.length() > 0`;
  `NewActiveStatus(false)` **não** falha (`Valid { ok }`); `NewMoney` roda `amount >=
  0`.
  **Commit:** `feat(codegen): ValueObject wrapper e composto`

- [x] **E3.2** `Operator` de VO → método (`Add`/`Sub`/`Gte`/…), corpo via lowering
  mínimo (`ensure … else Error`/`return`/aritmética sobre `value`+campos nus). VO
  `==`/`!=` **sem** operador declarado → comparação Go nativa; aritmético/relacional
  sem operador → erro de geração (§design 4.2). _(REQ-17.3, REQ-22.1/5/9, §design
  3.6/4.2)_ **Depende:** E0.3 (Money ganha os operadores).
  **Conclusão (golden + smoke):** `Money.Add/Sub/Gte` compilam e propagam `error`;
  `state.active == ActiveStatus(true)` (sem operador) vira `==` nativo.
  **Commit:** `feat(codegen): operadores de ValueObject`

- [x] **E3.3** `Enum` → tipo nomeado sobre o base + `const` por membro + `ParseX`. Sem
  `coerce`: coerção implícita (valor desconhecido → erro). Com `coerce`: corpo do
  `Parse` via lowering de `match` (E5.2). _(REQ-17.4, §design 3.5)_
  **Conclusão (golden + smoke):** `TransactionType` gera tipo+consts+`ParseTransaction
  Type`; um Enum com `coerce` (ex. `PaymentMethod` do spec, em fixture de teste) vira
  `switch`.
  **Commit:** `feat(codegen): Enum com coerção`

### Fase E4 — Errors e Events

- [x] **E4.1** `Error` → `var ErrX = runtime.BusinessError{Code, Msg}` (comparável,
  `errors.Is`); `Code` derivado do nome, `Msg` do literal de `message` (`ErrorTypeDecl.
  Message` é `Expr`). _(REQ-18.1, §design 3.5)_
  **Conclusão (golden + smoke):** `ErrInactiveWallet`/`ErrInsufficientBalance`
  compilam; `errors.Is` distingue.
  **Commit:** `feat(codegen): Errors de negócio`

- [x] **E4.2** `Event` (privado, no pacote do módulo) / `PublicEvent` (no pacote
  compartilhado **`contracts/`**, que quebra ciclos de import) → struct dos campos +
  metadata implícito (`timestamp`/`sequence`/`aggregateId`/`eventType`, via embed
  `runtime.EventMeta`, atribuído no `append`; a chave de stream é o **id do aggregate
  emissor**, não o campo `id` do payload) + `EventType() string` + registro num
  **registry** (nome→tipo). (De)serialização `encoding/json` determinística com tags de
  nome original. _(REQ-18.2/18.3, §design 3.4/3.5/3.7)_
  **Conclusão (golden + smoke):** `WalletCreated`/`DepositPerformed`/`Withdrawal
  Performed` compilam e fazem round-trip JSON estável (mesma ordem de campos).
  **Commit:** `feat(codegen): Events, registry e serialização`

- [x] **E4.3** Versionamento de evento: `Field.Default` aplicado na desserialização de
  eventos antigos; `UpcastDecl` (`Event vN -> vN+1`) → função de upcast aplicada no
  replay. `Field.Redactable` → suporte a placeholder tipado sem quebrar round-trip.
  _(REQ-18.4/18.5/18.6)_ *(Nenhum evento do wallet usa `default`/`Upcast`/`redactable`;
  cobrir por fixture sintética.)*
  **Commit:** `feat(codegen): defaults, upcast e redação de eventos`

### Fase E5 — Lowering do núcleo de corpos

> Cross-cutting: base para Aggregate/UseCase/Query. **E5.0 vem primeiro** — sem o
> ambiente de tipos, o dispatch de operador e o acesso a membro não sabem a forma Go.

- [x] **E5.0** **`lower/env.go` — `TypeEnv` (ambiente de tipos local).** Implementa
  `types.Scope`; semeia receptores por construto (espelhando `resolver/receivers.go`)
  e parâmetros (`Model.TypeOfRef`); ao percorrer o corpo, **estende o escopo em cada
  `AssignStmt` de alvo nu** inferindo o RHS, **incluindo o que `types.Infer` não
  cobre**: `load T(id)`→tipo de `T`; `list T … as V`→`List<V>` (sem `as`: `List<T>`);
  `count`→`integer`; `store f`→`FileRef`; lambda→tipo do corpo; `match`→tipo do 1º
  braço. Abre escopos-filho em `for`/`match`/`lambda`/alias de `list`/`join`.
  _(REQ-22.6, §design 3.6a)_ **Depende:** E1.3.
  **Conclusão:** testes de inferência sobre corpos sintéticos — `wallet = load
  Wallet(id)` ⇒ `wallet: Wallet`; iterador de `for e in state.entries` ⇒ `e:
  StatementEntry`; `x = count …` ⇒ `integer`. Nó realmente desconhecido → falha
  explícita.
  **Commit:** `feat(codegen): ambiente de tipos local do lowering`

- [x] **E5.1** `lower/expr.go`: literais (INT/FLOAT/STRING/DURATION/SIZE/bool),
  idents/receptores (via `TypeEnv`), `MemberExpr` (campo exportado vs método),
  construção de VO/Event/Command (`CallExpr` cujo `Fn` é tipo → `NewT`/literal de
  struct com campos nomeados), `BinaryExpr` (dispatch §4.2), `IndexExpr`, `RangeExpr`,
  `LambdaExpr` (closure Go). _(REQ-22.5/22.6, §design 3.6/4.2)_ **Depende:** E5.0.
  **Conclusão:** testes de lowering por família de expressão; `state.balance +
  event.amount` → `state.Balance.Add(ev.Amount)`.
  **Commit:** `feat(codegen): lowering de expressões`

- [x] **E5.2** `lower/stmt.go`: `EnsureStmt` por contexto (`else Error`→`return zero,
  ErrX`; `Nop` (Ident) → no-op de laço; `break`/`break all`(`BreakStmt.All`)/`continue`
  → controle de laço, `break all` via **label**), `MatchStmt`/`MatchExpr` exaustivo →
  `switch` (Enum coberto → `case`s sem `default`; com guard → `switch { case cond
  }`+`default` do `_`), `ForStmt`→`for … range`/`for i := lo; i <= hi; i++`, `EmitStmt`
  → constrói evento + `events = append(events, ev)`, `ReturnStmt`/`AssignStmt`/`LogStmt`
  (`slog` com trace context). _(REQ-22.1/2/3/4/8, §design 3.6/4.3)_ **Depende:** E5.0.
  **Conclusão:** `match` sobre Enum sem `default`; `break all` com label; `ensure …
  else InactiveWallet` vira `return … , ErrInactiveWallet`.
  **Commit:** `feat(codegen): lowering de statements e controle de fluxo`

- [x] **E5.3** `lower/builtins.go` **(núcleo, sem dep externa):** `now()`/`uuid()`/
  `random`/`random_str` → runtime; `load T(id)`→`repo.Load`, `list … [as V]`→query
  in-memory, `count`→`repo.Count`, `exists` (QueryExpr pós-fixo em `ensure x exists`)
  → checagem do `load`. **Ops de arquivo ficam para G1a** (dependem de `FileStorage`).
  _(REQ-22.7 (a), §design 3.6)_ **Depende:** E5.0.
  **Commit:** `feat(codegen): lowering de built-ins do núcleo`

### Fase E6 — Aggregates

- [x] **E6.1** `state`→struct (`walletState`); tipo do aggregate (`id`+`version`+
  `state`); cada `Handle`→método `(w *Wallet) Name(caller runtime.Caller, params…)
  ([]runtime.Event, error)` que (1) checa o `access` correspondente (`AccessRule.
  Condition` lowerizada sobre `caller`/`self`, senão `runtime.ErrForbidden`), (2)
  executa o corpo, (3) devolve os `emit`. Cada `Apply`→método privado
  `applyX(ev)` que muta `w.state`. _(REQ-19.1/2/3/6, §design 3.7)_ **Depende:** E5.*.
  **Conclusão (golden + smoke):** `Wallet` compila; `Deposit` checa `caller.
  authenticated`, faz `ensure state.active == ActiveStatus(true)` e emite
  `DepositPerformed`; `applyDepositPerformed` faz `state.Balance =
  state.Balance.Add(ev.Amount)` e `state.Entries.Add(...)`.
  **Commit:** `feat(codegen): Aggregate — state, Handle e Apply`

- [x] **E6.2** Reconstrução sobre o seam de store: `EventSourced` (`LoadWallet` lê o
  stream **por `aggregateId`** e aplica `applyX` em ordem; snapshot quando `snapshot
  every N` — o wallet **não** usa snapshot, cobrir o caminho sem snapshot + fixture com
  snapshot) e `StateStored` (persistência direta do `state`, mesmos Handle/Apply).
  _(REQ-19.4/19.5, §design 3.7)_
  **Conclusão:** `LoadWallet` reconstrói por replay; smoke compila ambos os modos.
  **Commit:** `feat(codegen): reconstrução EventSourced e StateStored`

### Fase E7 — Commands e UseCases

- [x] **E7.1** `Command`→struct DTO; campo `ref Aggregate` → tipo do `id` do state do
  aggregate (design §3.8; `Deposit.walletId` → `WalletId`), idempotência **não** vira
  campo (modelada no runtime). _(REQ-20.1, §design 3.5/3.8)_
  **Commit:** `feat(codegen): Commands`

- [x] **E7.2** `UseCase`→função `func Name(ctx, cmd T) error` que abre `uow.Run`,
  executa o corpo (`load`/`ensure`/dispatch de Handle) e faz commit/rollback;
  `timeout` (`UseCaseDecl.Timeout`) → `context.WithTimeout`. O corpo semeia `cmd` e
  `caller` (receptores do front-end). _(REQ-20.2/20.3, §design 3.8)_ **Depende:** E6.
  **Conclusão (golden + smoke):** `PerformDeposit(ctx, cmd Deposit)` abre `uow.Run`,
  faz `wallet, err := LoadWallet(tx, cmd.WalletId)`, chama `wallet.Deposit(...)`, faz
  `tx.Append`; compila. *(idempotência real e o `ensure exists` são G2.)*
  **Commit:** `feat(codegen): UseCase e unit of work`

### Fase E8 — Read Side

- [x] **E8.1** `View`→struct de leitura (campos próprios; `From Aggregate`→projeta os
  campos do state); `Query`→função com parâmetros e corpo. Cláusulas SQL-like
  (`QueryClause`: `where`/`orderBy`/`skip`/`take`/`as`) sobre o runtime in-memory
  (filter/sort/paginate); `… as V`→`map` para o struct da View. **Definir a semântica
  de `list <VO>`** (ex.: `ListEntries` faz `list StatementEntry`, um VO sem aggregate
  de origem — no in-memory, materializa as `entries` dos aggregates; documentar o
  suporte mínimo exercido pelo exemplo). _(REQ-21.1/2/5, §design 3.9)_ **Depende:** E5.*.
  **Conclusão (golden + smoke):** `GetWallet` (`load Wallet(id) as WalletView`) e
  `ListEntries` (`List<StatementEntry>`) compilam e retornam o tipo declarado.
  **Commit:** `feat(codegen): Views e Queries (read side in-memory)`

- [x] **E8.2** `Projection`→view materializada cross-aggregate atualizada nos eventos
  de `refreshOn` (`ProjectionDecl.Sources`/`Map`/`RefreshOn`). _(REQ-21.4)_ *(Sem
  Projection no wallet; cobrir por fixture — ex. `InvoiceWithHolderVW` do spec §6.4.)*
  **Commit:** `feat(codegen): Projections`

### Fase E9 — Exposição HTTP básica e wiring

- [x] **E9.1** `go.mod` (module `opts.ModulePath` — default derivado do dir de saída;
  `go opts.GoVersion` default `1.22`, **sem `require`**) + layout do projeto (inclui o
  pacote **`contracts/`** dos `PublicEvent`) + `cmd/<service>/main.go` por service, com
  wiring in-memory a partir de `mod.ds`/topologia (`prog.Services`/`ServiceOfModule`;
  monólito ⇒ um `cmd/`); porta HTTP do setting `port:` da `Interface` quando
  declarado (§10), fallback `8080` (o wallet não declara porta). _(REQ-14.5,
  REQ-26.4, §design 3.4/3.11/3.12)_
  **Commit:** `feat(codegen): layout de projeto, go.mod e wiring in-memory`

- [x] **E9.2** HTTP `net/http`: cada `ast.Route` → `mux.HandleFunc("METHOD /path/
  {param}", …)` (ServeMux de Go 1.22+); handler decodifica path/query/body, popula um
  **caller de dev** a partir do header `X-Caller-Id` (placeholder até auth real), chama
  o UseCase/Query e mapeia o resultado a status **por `errors.As(&BusinessError)`**
  (negócio→422; `ErrForbidden`→403; `ErrNotFound`→404; infra→503; sucesso→200/201).
  Ler `Idempotency-Key` (repassar ao runtime, efetivado em G2). _(REQ-28.1/28.2,
  §design 3.12)_ **Depende:** E7/E8.
  **Conclusão (golden + smoke):** o router do wallet compila; `POST /wallets/{id}/
  deposit` roteia para `PerformDeposit`, `GET /wallets/{id}` para `GetWallet`.
  **Commit:** `feat(codegen): borda HTTP com net/http`

### Fase E10 — CLI e fechamento do Marco E

- [x] **E10.1** `dsc gen <dir> -o <out>` completo + escrita **idempotente** da saída
  (mesma entrada → mesmos bytes; remove artefatos órfãos de declarações removidas).
  _(REQ-32.1/32.2/32.3, §design 3.15/4.1)_ **Depende:** E9.
  **Commit:** `feat(cli): subcomando gen`

- [x] **E10.2** Smoke end-to-end do wallet: gerar, `go build ./...`, `go vet ./...` e
  um **teste comportamental in-memory** verdes (sem subir socket) — semeia estado via
  eventos `given` (evento isolado) ou construção completa (fluxo), executa um UseCase e
  confere o evento emitido; **`go.mod` gerado sem `require` externo**. Regen
  byte-idêntico (dois runs). O exemplo wallet **não é fonte de verdade** (§design 6): o
  teste não depende de ele ser um domínio canônico. _(DoD §5.2/5.3/5.4, NFR-12/13/14)_
  **Conclusão:** teste end-to-end que gera o wallet, compila e roda o teste
  comportamental, conferindo ausência de dep externa e determinismo.
  **Commit:** `test(codegen): smoke end-to-end do wallet (Marco E)`

---

## Marco F — Reações e Coordenação

> Passa a exercitar `docs/examples/shop`: `Policy NotifyShipping` cross-service
> (canal `queue` Orders→Shipping). Fila/dispatcher/outbox e IO entram aqui.

- [x] **F1** `Policy`→subscriber no `runtime.Dispatcher` para o tipo do evento de `on`
  (`PolicyDecl.On`), com a garantia de `delivery` (`BestEffort`→in-process;
  `AtLeastOnce`→via outbox). Corpo via lowering (`event`/`caller` semeados). _(REQ-23.1/
  23.5, §design 3.10)_
  **Conclusão (golden + smoke):** `NotifyShipping on OrderPlaced` registra o subscriber
  e compila.
  **Commit:** `feat(codegen): Policies e dispatcher`

- [x] **F2** `Worker`→job agendado por `WorkerDecl.Schedule` (`every`→ticker,
  `cron`→agenda do runtime, `continuous`→loop consumindo `Source`), com
  `concurrency`/`batchSize`/`maxRate` (de `Settings`), `ExecuteParam` como item da
  fonte, e `onError.retry`→backoff. _(REQ-23.2/23.3, §design 3.10)_ *(Fixture; sem
  Worker no shop mínimo.)*
  **Commit:** `feat(codegen): Workers`

- [x] **F3** `Saga`→state machine com `state` e `Steps` (`up`/`down`/`onInfraError`);
  falha após N steps → `down` em ordem reversa (respeitando `down { unrecoverable }`);
  `async`→`sagaId`+`SagaStatus`, `await`→bloqueante com `timeout`. Steps semeiam
  `state`. _(REQ-24, §design 3.10)_
  **Conclusão (golden + smoke):** um exemplo de Saga (fixture PurchaseTickets do spec)
  compila; teste de compensação executa `down` em ordem reversa.
  **Commit:** `feat(codegen): Sagas e compensação`

- [x] **F4** `Notification`→contrato de saída; `Adapter` HTTP declarativo→cliente
  `net/http` (método/URL/headers/body de `AdapterDecl`, `env(...)` do ambiente);
  `Adapter` FFI + bloco `Foreign`→chamada a `adapters/` com marshalling; distinguir
  `notify` (async) de `call` (sync). _(REQ-25, §design 3.13)_
  **Commit:** `feat(codegen): Notifications, Adapters e FFI`

- [x] **F5** Outbox in-memory ligando `emit`→dispatcher/`notify` + **canais da
  topologia** (REQ-26.5): `direct`→despacho in-process; `queue`→fila in-memory do
  runtime respeitando `orderBy` e `workers{concurrency,maxRate,batchSize}`;
  `timeout`/`circuitBreaker` no cliente de canal; provider real (`rabbitmq`) fica
  atrás do seam como opt-in de marco posterior. **Fechamento do marco:** o shop
  (dois services, Policy cross-service via `Orders -> Shipping { via: queue
  orderBy: id }`) gera e compila. _(REQ-25.3, REQ-26.1/26.5, §design 3.11)_
  **Commit:** `feat(codegen): outbox e canais de entrega de eventos`

---

## Marco G — Infraestrutura Real

- [x] **G1** Adapter de persistência `database/sql` (stdlib) atrás de `runtime.Store`;
  driver concreto isolado e opt-in (única dep externa). O **mesmo** lowering de Query
  (E8) passa a gerar SQL parametrizado; o núcleo in-memory continua compilando **sem**
  dep externa. `supportsXA`/`retry`/`circuitBreaker`/`tenancy` (do `Database`)
  configuram o adapter; UseCase cross-database com **todos os bancos XA** (válido no
  front-end) → **2PC** no `uow.Run` (prepare/commit em duas fases, §design 3.8);
  Smart Partial Loading (§20) pode descer `focus`/`sum`/paginação para SQL sem mudar
  o lowering. _(REQ-20.5, REQ-26.2/26.3, NFR-12, §design 3.8/3.9/3.11/4.4)_
  **Commit:** `feat(codegen): adapter database/sql plugável`

- [x] **G1a** **Ops de arquivo (adiadas de E5.3):** seam `FileStorage` do `mod.ds` +
  lowering de `store`/`signed_url`/`delete file`/`load File(ref)` e as structs
  built-in `File`/`FileRef`/`FileStream` do runtime; o bloco `storage` do Aggregate
  (`ast.StorageEntry`, spec §2.5) roteia cada campo `FileRef` para o `FileStorage`
  declarado (e `state:` para o `Database`). _(REQ-22.7 (b), REQ-25, spec §2.5)_
  **Commit:** `feat(codegen): FileStorage e operações de arquivo`

- [x] **G2** Idempotência real de Command (spec §14): chave do cliente, cache de
  sucesso/erro de negócio (erro de infra permite retry), conflito mesma chave +
  command diferente → `IdempotencyKeyConflict` (422), corrida da mesma chave →
  `wait`/`reject` conforme `concurrentRetry`, worker de limpeza de chaves expiradas
  gerado automaticamente, mapeamento estável Idempotency-Key → `sagaId` (Sagas de
  F3) + o `ensure cmd/agg exists` adiado de E7.2. _(REQ-20.4, REQ-26, spec §14)_
  **Commit:** `feat(codegen): idempotência de Commands`

- [x] **G3** Cache de Query (`QueryDecl.Cache`, spec §15): `ttl`, invalidação por
  evento (inferida dos aggregates tocados; override `invalidateOn`; in-process
  imediata após `emit`, antes da fila externa), `negativeCacheTtl`, stampede
  protection (request coalescing), fail-open na falha do backend, bypass
  `Cache-Control: no-cache`, tenant na chave. _(REQ-21.3, spec §15)_
  **Commit:** `feat(codegen): cache de Queries`

- [x] **G4** Rate limiting na borda (spec §16): dimensões `perIp`/`perUser`/
  `perTenant`/`perApiKey`/`global` (múltiplas → todas precisam passar), algoritmos
  (`token_bucket` com `burst`, `sliding_window`, `fixed_window`), tiers de
  `RateLimitTier` via `byTier`/`tenant.tier`, resposta `429` + `Retry-After` +
  headers `X-RateLimit-*` (gRPC `RESOURCE_EXHAUSTED` entra com H1),
  `onBackendFailure: open/closed` com override por endpoint, rotas sem tenant só
  `perIp`, retry idempotente não consome cota (integra G2). _(REQ-28.4, spec §16)_
  **Commit:** `feat(codegen): rate limiting`

- [x] **G5** Multi-tenancy: `tenant` em `context.Context`, filtro automático por
  estratégia (`row_level`/`schema`/`database`), acesso a aggregate de outro tenant
  → 404, tenant ausente na borda → fail-closed **400**, `cross_tenant` opt-in
  (`UseCaseDecl.Tenancy`) + auditoria. *(O receptor `tenant.*` em corpos e
  `provision tenant(id)` (§13.4) não são modelados pelo front-end — fora deste
  ciclo, ver req. §1.3.)* _(REQ-27, spec §13)_
  **Commit:** `feat(codegen): multi-tenancy`

- [x] **G6** HTTP avançado: `versioning` + `versions/*.ds` (`VersionUpcast`/
  `VersionDowncast`/`VersionRoute`); após `deprecated` → headers `Deprecation` +
  `Sunset`; após `sunset` → **`410 Gone`**; endpoints inalterados passam direto
  (versionamento esparso). _(REQ-28.4, spec §17)_
  **Commit:** `feat(codegen): versionamento de API na borda`

---

## Marco H — Exposição/Observabilidade Avançadas e Testes

- [x] **H1** gRPC: `.proto` (de `InterfaceDecl` `GRPC` + `GrpcService`/`GrpcRPC`) +
  stubs, dep `google.golang.org/grpc` **isolada** num pacote de borda, **ausente** sem
  `Interface GRPC`. _(REQ-29, NFR-12, §design 3.12)_
  **Conclusão:** `.proto` textual (`grpc_proto.go`) + servidor gRPC real sem
  protoc/`*.pb.go` (`grpc.ServiceDesc`/`MethodDesc` manuais + `encoding.Codec`
  JSON, `grpc.go`); dep isolada em `grpcedge/` (vendorado, `codegen/grpcrt`),
  ausente sem `Interface GRPC` (go.mod, golden, smoke via `go mod tidy` real, e
  round-trip comportamental via `bufconn` — fixture sintética `GrpcDemo`,
  `grpc_test.go`).
  **Commit:** `feat(codegen): exposição gRPC (dep isolada)`

- [x] **H2** Observabilidade: `log/slog` (stdlib) por padrão com trace context; adapter
  OTel atrás de `runtime.Observer`, opt-in quando `Telemetry` é declarado.
  _(REQ-30.1/30.2, §design 3.13)_
  **Conclusão:** trace id stdlib (`runtime.WithTrace`/`NewTraceID`, hex de 128
  bits, mintado na borda HTTP/gRPC) propagado a todo `log` cujo corpo tem
  `ctx` em escopo (`StmtContext.CtxVar`, UseCase/Policy/Query/Worker/passo de
  Saga — Handle/Apply ficam de fora, limitação preexistente documentada);
  seam `runtime.Observer` (`RecordSpan`/`TraceID`, no-op default,
  `rtsrc/observer.go.txt`) ligado na borda HTTP/gRPC (span por despacho de
  UseCase/Query) e no `Dispatcher` núcleo (span por invocação de Policy,
  cobre BestEffort+AtLeastOnce de graça); adapter OTel real isolado em
  `codegen/otelrt` (OTLP sobre HTTP, `v1.44.0`, opt-in via `Telemetry` no
  mod.ds — `go.mod`/`otelruntime/*.go` ausentes sem ela); fixture sintética
  `TelemetryDemo` com golden/determinismo/smoke-compile (`go mod tidy` real)
  e dois testes comportamentais (`RunTests` sobre o projeto gerado inteiro,
  incl. `otelruntime/observer_test.go` embutido sobre `tracetest.
  SpanRecorder`; e um `runtime.Observer` fake instalado provando que a borda
  HTTP de fato chama `RecordSpan`); regressão: wallet/shop sem `Telemetry`
  continuam sem nenhum artefato `otelruntime/*`/`require` OTel em `go.mod`.
  **Commit:** `feat(codegen): observabilidade (slog + OTel opt-in)`

- [x] **H3** `Metric` de negócio (`MetricDecl`: counter/histogram no gatilho `on`,
  `buckets`, `labels`). _(REQ-30.3)_
  **Conclusão:** registry em memória sempre presente, stdlib-only, no runtime
  núcleo (`Counter`/`Histogram`, `rtsrc/metrics.go.txt` — sem opt-in, ao
  contrário do adapter OTel de H2, que documenta métricas como fora de
  escopo). Dois gatilhos: `on Evento` vira subscriber no
  `runtime.Dispatcher` (`WireMetrics`, `decl_metric.go`, mesmo padrão de
  `decl_policy.go`; `needsDispatcher` como `hasCachedQueries`); `on
  Saga.completed` vira hook direto no código gerado da própria Saga
  (`decl_saga.go` — sem Dispatcher, já que uma Saga não publica nada ao
  concluir), atualizado logo após sucesso com a duração
  (`time.Since(start).Seconds()`) quando um histogram não declara `value`.
  Fecha um gap do front-end: `MetricDecl` não passa por nenhuma resolução de
  nomes em REQ-9 (confirmado empiricamente — `resolveMetricOn` é a ÚNICA
  validação do gatilho `on`; `value`/`labels` são cruzados direto contra
  `types.Model` em `decl_metric.go` antes de aceitar qualquer texto Go).
  Buckets de DURATION materializados em segundos em tempo de geração
  (`lower.DurationLiteralSeconds`). Fixture sintética `MetricsDemo`
  (counter `DepositVolume` sobre o wallet real + histogram
  `PurchaseLatency` reaproveitando a Saga `PurchaseTickets` de F3) com
  golden/determinismo/smoke-compile e dois testes comportamentais
  (`Dispatcher.Publish` atualiza o Counter; a Saga completando com sucesso
  observa uma amostra no Histogram); regressão: wallet/shop sem `Metric`
  continuam sem `<módulo>/metrics.go`/`WireMetrics`.
  **Commit:** `feat(codegen): métricas de negócio`

- [x] **H4** Testes gerados de `*.test.ds`: `given`/`when`/`then` (`ThenClause`/
  `ThenAssert`), `mock … returns`, `fail step X with InfraError`, `property`,
  `Fixture` → testes Go (`testing`). _(REQ-31, §design 3.14)_
  **Progresso parcial (1ª fatia, `gentest.go`):** cenário de Aggregate (§22.1) —
  um `Test` cujo `Name` resolve a um `*ast.AggregateDecl` do módulo (mesmo
  casamento por nome que `sema/rules_test_files.go:sagaSteps` já faz para
  Saga) vira `func TestX(t *testing.T)`: `given [eventos]`/`given state{...}`
  (qualquer nº, aplicados em ordem — a 2ª `given` de "carteira inativa"
  sobrescreve `active` depois da 1ª) semeia o Aggregate direto (Apply real
  quando existe; seed campo-a-campo por nome quando não, ex. `WalletCreated`
  antes desta task — nunca via `EventStore`+`LoadX`, que quebraria no bootstrap
  de um VO com Operator como `Money`, ver a doc de `gentest.go`); `when
  Action(...)` despacha o Handle de mesmo nome (nunca o Command homônimo,
  convenção Command↔Handle do wallet); `then [eventos]` (via
  `reflect.DeepEqual`)/`then error Name` (via `errors.Is`) verifica o
  resultado; todo caller gerado é `runtime.NewTestCaller(id do aggregate)`
  (rtsrc/caller.go.txt) — a gramática de §22 não tem forma de expressar "como
  o caller X", então acesso NEGADO não é testável nesta fase. Corrigido de
  quebra: `docs/examples/wallet/domain.ds` ganhou `Apply WalletCreated`
  (seedava nada antes — `WalletCreated` nunca tinha `Apply`, gap preexistente
  que só este `given` expôs) e `codegen/lower` ganhou coerção de literal
  INT→`runtime.NewDecimalFromInt` em argumento de construção de VO composto
  (`hoistVOConstruct`, mesma lacuna que `vobody.go:lowerDecimalOperand` já
  fechava para corpo de VO, nunca fechada para Handle/Apply/UseCase por falta
  de caso de uso — `Money(0, "BRL")` foi o primeiro). `wallet.test.ds` (real,
  com um 2º `given state { active: false }` acrescentado para a carteira
  inativa) gera `wallet_test.go` que roda `go test` verde sobre o projeto
  gerado inteiro (`TestEmitTestsWalletRunsGreen`) — fidelidade semântica
  (NFR-15) sobre o alvo de conclusão nomeado por esta task.
  **Progresso parcial (2ª fatia):** cenário de UseCase (§22.2) — um `Test`
  cujo `Name` resolve a um `*ast.UseCaseDecl` do módulo (checado depois de
  Aggregate, mesmo mapa nome→decl) vira `func TestX(t *testing.T)` que
  invoca a função gerada do UseCase como CAIXA-PRETA (`PerformDeposit(ctx,
  cmd)`, decl_usecase.go), MECANISMO ESTRUTURALMENTE DIFERENTE de §22.1: já
  que o UseCase carrega o Aggregate de dentro do próprio corpo ("load
  Wallet(cmd.walletId)"), `given Subject from [eventos]` (ex. `Wallet("W1")
  from [...]`) semeia um `runtime.EventStore` de verdade (`store.Append`) em
  vez de construir o Aggregate direto — `LoadWallet`, já testado, faz o
  replay quando o UseCase chamar `load`. `then { ... }`: `Subject emitted
  Evento` resolve por ÍNDICE ESTÁTICO (quantos eventos aquele Subject já
  tinha ANTES, fixado em tempo de geração, mais quantas asserções `emitted`
  já foram consumidas para o MESMO Subject — `ucSubject`/`ucSubjects`) e
  zera a `EventMeta` do evento persistido antes do `reflect.DeepEqual`
  (`Event.SetMeta`, já que `AggregateID`/`Sequence`/`Timestamp` são
  carimbados por `store.Append`, nunca conhecidos pelo `.test.ds`);
  `committed`/`rolledback` são só `err == nil`/`err != nil` —
  `rtsrc/uow.go.txt` documenta que a UnitOfWork em memória não tem stage
  nenhum (Append já é durável no instante em que retorna), então rollback de
  verdade não existe para desfazer-se-precisar-verificar; caller é sempre
  `runtime.NewTestCaller("test-caller")` (fixo — um cenário de UseCase pode
  envolver MAIS de um Aggregate, não há um "self" único para bater
  `caller.id`). `wallet.test.ds` ganhou `Test PerformDeposit` (2 cenários:
  sucesso com `committed` + evento persistido, e carteira nunca criada com
  `error InactiveWallet, rolledback`) — passa por `TestEmitTestsWalletRunsGreen`
  junto do cenário de Aggregate, no projeto wallet gerado INTEIRO (a prova
  agora usa `generateWalletProject`, não mais um subconjunto de arquivos, já
  que exercita `usecases.go`/`Wire` de verdade).
  **Progresso parcial (3ª fatia):** `Fixture` como helper reusável (§22.6) —
  "Fixture Nome { Subject from [eventos] }" vira "func fixture<Nome>(t
  *testing.T) *<AggType>" no MESMO "<pkg>_test.go" (`EmitTests` passa a
  receber também `[]*ast.FixtureDecl`; `emitFixtureDecl`/`emitFixtureBody`/
  `resolveFixtureAggregate`, `gentest.go`): reusa a MESMA máquina de given de
  §22.1 (`emitAggregateGivenEntity` — Apply real quando existe, seed
  campo-a-campo quando não, ver a doc do arquivo sobre "seed direto, não
  replay de EventStore") porque a gramática não tem NENHUMA forma de ligar
  uma Fixture a um Test (confirmado: nem parser nem sema referenciam
  `FixtureDecl` fora de coletar) — o helper gerado não tem chamador dentro do
  projeto gerado, e Go não recusa uma func de topo não usada; sua corretude é
  provada por um teste hand-written que a CHAMA
  (`TestEmitFixturesWalletBehavior`, gentest_test.go, mesmo espírito dos
  testes comportamentais hand-written já existentes do pacote). Escopo
  restrito, documentado (não esquecimento): só "Subject from [eventos]" (a
  forma do próprio exemplo do spec) é suportada — uma Fixture sem Subject
  (lista de eventos ambígua, qual Aggregate?), com "state {...}"/"binding
  [...]" (StateStored/Policy), ou referenciando mais de um Aggregate (um
  helper multi-Subject precisaria devolver vários valores/um struct) são erro
  de geração claro, nunca geradas silenciosamente erradas
  (`resolveFixtureAggregate`). `wallet.test.ds` ganhou `Fixture
  activeWallet` (§22.6, o mesmo exemplo do spec, adaptado ao wallet real).
  **Progresso parcial (4ª fatia):** cenário de Saga com `mock`/`fail step`
  (§22.3) — um `Test` cujo `Name` resolve a um `*ast.SagaDecl` do módulo
  (checado depois de Aggregate/UseCase, mesmo mapa nome->decl) vira `func
  TestX(t *testing.T)` que chama `<base>RunSteps` DIRETO em vez da entrada
  pública gerada (`PurchaseTickets(ctx, cmd)`, `decl_saga.go`): a entrada
  "await" só devolve `(*State, error)`, nenhuma forma pública expõe
  `runtime.SagaResult` (`Compensated`/`Unrecoverable`/`FinalState`), exatamente
  o que `saga compensated`/`compensated [...]` precisam observar - `RunSteps`
  é a MESMA função que a entrada pública chama por baixo (dentro de uma
  goroutine, para "await"), então chamá-la direto reproduz a orquestração
  real byte a byte, só pulando o wrapper de goroutine+timeout (fora de
  escopo aqui) e evitando de graça a janela de corrida que a entrada "await"
  documenta. `given state {...}` (a ÚNICA forma suportada - uma Saga não tem
  Event/EventStore) sobrescreve `state.<Campo>` direto, ANTES do seed
  automático de `when Cmd(...)` (que copia todo campo de `state` cujo nome
  bate com um campo do Command - mecanismo preexistente de `decl_saga.go`,
  não novo nesta fatia); o `cmd` vence em caso de colisão de nome (não há um
  2º `given` para desempatar como em §22.1). `mock Target returns X` resolve
  Target a um Adapter do módulo e reatribui a var de pacote que
  `Call<Nome>`/`Notify<Nome>` invocam por baixo (`adapterCallVarName`,
  `decl_io.go` - NOVO seam desta fatia: antes, `Call`/`Notify` chamavam o
  leaf HTTP/FFI direto, sem nenhum ponto de interceptação) para uma closure
  que registra a chamada (`then { called Target }`) e SEMPRE sucede - `X` é
  construído e type-checked como Go real, mas não pode desviar o fluxo de
  negócio hoje (`Call`/`Notify` só devolvem `error`; simular uma falha
  causada por um Adapter é papel de `fail step`, não de `mock`, documentado
  como lacuna para uma evolução futura). `fail step Name with Err` troca
  `<base>Steps[idx].Up` por uma função que devolve um erro SINTÉTICO via
  `errors.New` - NUNCA `runtime.BusinessError` - simulando estritamente uma
  falha de INFRAESTRUTURA (§19); `Err` é texto livre, nunca validado contra
  nenhum `Error` declarado (não há Error de infra no domínio), só embutido na
  mensagem para diagnóstico. Como `<base>Steps` é uma var de PACOTE
  compartilhada entre os cenários de uma mesma suíte (sem `t.Parallel()`, mas
  sem isolamento automático também), uma `<base>StepsOriginal` é emitida UMA
  VEZ por Saga (cópia pristina capturada por um inicializador, antes de
  qualquer `func` de teste rodar) e CADA cenário a restaura como primeira
  linha do seu corpo, antes de aplicar `mock`/`fail step` - garante que a
  mutação de um cenário nunca vaze para o próximo. Habilitar um passo de
  Saga a CHAMAR um Adapter (pré-requisito para `mock`/`called` terem algo de
  verdade para instalar/observar - nenhuma Saga anterior a esta task chamava
  um Adapter) exigiu passar `adapterByName` por `EmitSaga`/`EmitSagas`/
  `emitSagaDecl`/`emitSagaStepFuncs`/`emitSagaStepPhaseFunc` até
  `StmtLowerer.WithNotifyAdapters` (mesmo registry que `EmitPolicies`/
  `EmitUseCases` já recebem - `decl_saga.go`), e o novo seam de
  `decl_io.go` (`var send<Nome>Fn = send<Nome>` / `var
  call<Nome>ForeignFn = call<Nome>Foreign`, com `Call<Nome>`/`Notify<Nome>`
  agora invocando a VAR em vez do leaf direto) precisou de golden files
  atualizados (`adapter_deposit_notification.go.golden`,
  `adapter_payment_request.go.golden`) e das asserções `strings.Contains`
  correspondentes em `decl_io_test.go`, que ainda checavam a chamada direta
  pré-seam. Dois achados corrigidos nesta fatia, sobre o Go de fato gerado
  (não hipóteses): (1) um cenário cujo ÚNICO `then` é `called Adapter` (o
  único verbo que não lê `res`) gerava `res declared and not used` - erro de
  COMPILAÇÃO, não só de `go vet` -, corrigido com um `_ = res` logo após
  `res := <base>RunSteps(...)`, incondicional; (2) `then { compensated [] }`
  (a forma correta de provar "nenhum step foi compensado", êxito total) batia
  `reflect.DeepEqual` contra `res.Compensated`, que fica `nil` (nunca
  inicializado) no caminho 100% feliz - `nil` e `[]string{}` são
  estruturalmente diferentes para `DeepEqual`, então a asserção SEMPRE
  falhava; corrigido normalizando com `gotCompensated := append([]string{},
  res.Compensated...)` antes de comparar. Fixture sintética nova (módulo
  isolado `Booking`, não a `PurchaseTickets` de F3 - mexer no golden já
  congelado de F3 misturaria, no diff desta fatia, uma mudança de
  comportamento não relacionada): mesma estrutura de passos do exemplo
  canônico do spec (`ReserveTickets`/`ProcessPayment`/`ConfirmPurchase`,
  `mock PaymentRequest`, `fail step ConfirmPurchase`), com `ProcessPayment`
  chamando de fato o Adapter `PaymentRequest` (`mode async` - Notify nunca
  propaga erro, então o cenário SEM mock nem fail step continua no caminho
  feliz mesmo com a chamada HTTP real falhando rápido e local, `PAYMENT_URL`
  não setada - sem DNS, sem rede, só logada); 3 cenários (sem mock/fail,
  `then { compensated [] }`; `fail step ConfirmPurchase with InfraError`,
  `then { saga compensated, compensated [ProcessPayment, ReserveTickets] }`;
  `mock PaymentRequest returns true`, `then { called PaymentRequest }`) -
  golden + determinismo + smoke-compile do projeto inteiro +
  `TestEmitSagaTestsRunGreen` (`go test ./...` de verdade sobre o projeto
  gerado, os 3 `func TestPurchaseTickets_*` gerados SÃO os testes que rodam,
  mesmo espírito de `TestEmitTestsWalletRunsGreen`). Explicitamente NÃO
  coberto (erro de geração claro, nunca gerado silenciosamente errado):
  `Subject emitted Evento`/`Subject released` de dentro de um passo de Saga
  (ex. "Order emitted OrderCancelled" do exemplo do spec) - um passo de Saga
  não tem acesso a nenhum `runtime.Tx`/`Store`/`EventStore` (só `state`),
  persistir um Event a partir de um passo exigiria um mecanismo novo
  (análogo ao `ucSubjects` de §22.2, mas para dentro de um passo de Saga),
  maior que o resto desta fatia; nenhuma fixture real (wallet/shop) tem uma
  Saga que emite eventos hoje.
  **Progresso parcial (5ª fatia, `gentest_property.go`):** `property`
  (§22.5, REQ-31.3) — um `Test` cujo `Name` resolve a um `*ast.AggregateDecl`
  e declara `t.Properties` (checado depois dos `t.Scenarios` do mesmo Test,
  mesmo mapa `used` de `scenarioFuncName` para nunca colidir num nome de
  função Go) vira `func TestX(t *testing.T)`: `forall sequence of [Handle,
  ...]` (`ast.PropertyDecl.Forall`, um `ast.ListExpr` de `ast.Ident`,
  `resolveForallHandles`) nomeia HANDLES, não chamadas concretas — os
  argumentos de CADA passo, em CADA sequência, são sintetizados por um
  gerador ALEATÓRIO type-driven escrito à mão sobre `math/rand`
  (`propGen.genValue`, NFR-12: zero dependência externa no Go gerado, nunca
  um framework de QuickCheck): um primitivo vira uma expressão direta; um
  ValueObject (wrapper ou composto) usa um laço `for attempt` que resorteia
  os campos e chama `New<VO>` até `err == nil`, com um teto
  `propGenMaxAttempts` como defesa contra um `Valid` tão restritivo que o
  gerador nunca o satisfaça. `ast.PropertyDecl` NÃO tem `GivenClause`
  nenhuma (confirmado em `ast/test.go`/`parser/parse_testfile.go`) — começar
  de um Aggregate zero-value tornaria a property vazia na prática (ex.
  `state.active` ficaria `false`, e todo Handle que depende de
  `state.active == ActiveStatus(true)` falharia em TODA chamada, a
  invariante nunca seria exercitada por uma transição bem-sucedida); por
  isso cada campo de `agg.State` que o gerador suporta recebe um seed
  aleatório válido ANTES de cada sequência (mesma filosofia "seed direto" de
  `emitFieldSeed`), e um campo de tipo não suportado (ex.
  `AppendList<StatementEntry>`) fica no seu Go zero-value. Um problema de
  consistência entre instâncias apareceu com `Money`: seus `Operator`s
  exigem `currency == other.currency`, e como `Apply` é infalível-por-
  construção (`StmtContext{Panics: true}`), um `Money` aleatório cujo
  `currency` divergisse do já seedado em `state.balance` PANICARIA o teste
  gerado a cada iteração — a saída (`literalPool`, sem hardcoded nenhum nome
  de campo) colhe, do próprio `*.test.ds` (`pr.Forall`/`pr.Invariant`), todo
  literal STRING passado a um campo string de um ValueObject composto (ex.
  `"BRL"` do invariant do wallet real) e usa um "shared var" sorteado UMA
  VEZ por iteração (nunca por construção) para aquele (VOType, campo),
  reusado tanto no seed do state quanto em toda chamada de Handle da mesma
  iteração — um (VOType, campo) sem exemplo colhido cai de volta a texto
  aleatório puro (lacuna documentada: um domínio com mais de uma currency
  legítima arriscaria `CurrencyMismatch`, não exercitado pelo wallet real).
  A invariante é lowerizada UMA VEZ (`sl.ExprHoisted`, `state` vinculado a
  `<receiver>.state`, mesma convenção de `emitHandle`) e checada DEPOIS de
  aplicar os eventos de cada chamada bem-sucedida (`emitApplyDispatch`,
  reusa a MESMA correspondência Event→apply<Event> que `LoadX` EventSourced
  já usa — fidelidade semântica, NFR-15); uma chamada que devolve erro de
  negócio só faz `continue` (REQ-31.3: a invariante é sobre o state
  alcançado por transições BEM-SUCEDIDAS, não sobre quais chamadas
  sucedem). O contra-exemplo (REQ-31.3) é a sequência COMPLETA de passos
  executados até e incluindo o que violou a invariante (`[]dsPropStep`
  acumulado, `t.Fatalf` com `%+v` sobre o slice inteiro) — shrinking (o
  PREFIXO mínimo que ainda viola) não é implementado, documentado como
  evolução futura sem nenhum exemplo real do spec que precise dele.
  Determinismo (NFR-13): cada property roda com
  `rand.New(rand.NewSource(<literal>))`, onde `<literal>` é um `int64`
  calculado em TEMPO DE GERAÇÃO via FNV-1a sobre `Test.Name`/`Property.Name`
  (`propertySeed`) — o mesmo Test+property produz sempre o mesmo literal no
  Go gerado (regenerar não muda um byte) e o teste gerado reproduz sempre a
  mesma sequência de valores aleatórios a cada execução (a exploração é
  aleatória entre properties/execuções diferentes do GERADOR, nunca dentro
  da mesma property já gerada — uma falha é sempre reproduzível relendo o
  próprio `_test.go`). `propGenIterations`/`propGenMaxSteps` (100 × até 20
  passos) são fixos e conservadores (a gramática de §22.5 não declara
  nenhum parâmetro para isso). `wallet.test.ds` ganhou `property "saldo
  nunca fica negativo" { forall sequence of [Deposit, Withdraw] invariant
  state.balance >= Money(0, "BRL") }` (o exemplo do próprio spec, adaptado —
  o wallet real só tem `Deposit`/`Withdraw`, não há `Transfer`) — provado
  via golden + determinismo + smoke-compile + `TestEmitTestsWalletRunsGreen`
  (o `go test` real sobre o projeto gerado INTEIRO agora também roda
  `TestWallet_SaldoNuncaFicaNegativo`, a prova de que a invariante genuinamente
  se sustenta sobre o domínio real ao longo de milhares de transições
  exploradas). Explicitamente NÃO coberto: qualquer `Test` de property que
  resolva a um UseCase/Saga (erro de geração claro — REQ-31.3 exemplifica
  property só sobre Aggregate); tipos de parâmetro fora de
  integer/decimal/string/boolean e ValueObject wrapper/composto sobre esses
  (Enum, Generic/coleção, Shape — não exercitados por nenhum Handle do
  wallet real).
  **Progresso parcial (6ª fatia, Policy/Query — §22.4): as 3 camadas de
  infraestrutura faltante identificadas por uma investigação anterior desta
  task (registrada numa sessão prévia como "Policy/Query investigado e
  adiado" — resumo: nem wallet/shop tinham uma Policy com corpo de negócio
  de verdade; `list`/`count` não tinham seam de runtime nenhum; `where` não
  lowereizava por item) foram construídas e conectadas nesta sessão, em duas
  etapas.**

  *Camadas 1-2 (infraestrutura, sessão anterior, commit separado antes desta
  fatia):* `hoistQueryPredicate` (`codegen/lower/stmt.go`) redesenhou `where`
  de um bool solto avaliado uma única vez para um predicado POR ITEM de
  verdade — `func(item T) bool { return cond }` — vinculando o `Binding` da
  query (ou o nome sintético `"item"`) a um `TypeEnv`-filho só para a duração
  do `where`, reusando o `Lowerer.member` já existente (nenhuma resolução de
  membro nova). `runtime.Collection[T]` (`codegen/rtsrc/collection.go.txt`,
  novo) deu ao runtime vendorado um seam `Add`/`List`/`Count` mínimo,
  espelhando `Repository[T]`; `ListCall`/`CountCall` (`lower/builtins.go`)
  passaram a rotear por TIPO via `BuiltinLowerer.store(typeName)` — o MESMO
  mecanismo `WithPerAggregateStore` que o caminho 2PC de `decl_usecase.go`
  (G1) já provê, reusado sem nenhuma mudança de forma.

  *Camada 3 + geração de teste (esta fatia):* **Parte A**
  (`codegen/decl_policy.go`) conectou as duas pontas que faltavam para um
  corpo de Policy usar `list`/`count`/`emit` de verdade — (1)
  `policyCollectionTypeNames` varre CADA `PolicyDecl.Execute` do arquivo
  (`astutil.ForEachExprInBlock` + `*ast.QueryExpr` "list"/"count", mesmo
  padrão de `sema/rules_test_files.go:checkForeignSignatures`) e
  `emitPolicyCollectionVars` declara, uma vez por tipo distinto,
  `var <tipo>Collection = runtime.NewMemoryCollection[<Tipo>]()`
  (`policyCollectionVarName`: `lowerFirst(tipo) + "Collection"`, mesma
  convenção de `sourceVar` em `decl_worker.go:emitContinuous`) — cada
  `emitPolicyDecl` anexa `WithBuiltins(NewBuiltinLowerer(runtimeAlias, "ctx",
  "").WithPerAggregateStore(typeToVar))`, roteando list/count pro
  `Collection[T]` certo; (2) `policyBodyHasEmit` varre por `*ast.EmitStmt` e,
  se alguma Policy do arquivo usa `emit`, declara `var policyDispatcher
  runtime.Dispatcher` e anexa `StmtLowerer.WithEmitDispatch("policyDispatcher",
  "ctx")` (o seam que já existia desde as camadas 1-2, só nunca consumido) —
  `Wire` atribui `policyDispatcher = d` como 1ª linha do corpo, SEMPRE
  (nunca condicionada a canal/Delivery por Policy, ao contrário do resto de
  `Wire`). Ambos guardados: um arquivo cujas Policy não usam list/count/emit
  gera Go byte-idêntico ao de antes desta fatia (`TestEmitPoliciesGolden`/
  `TestGenerateShopPolicyRegistersSubscriberAndCompiles`, sobre a Policy real
  do shop, continuam verdes sem alteração).

  **Parte B** (`codegen/gentest.go`) implementou o cenário §22.4 em si:
  `EmitTests` ganhou um parâmetro `policies map[string]*ast.PolicyDecl`
  (`policiesByName`, construído em `codegen.go:generateModuleFiles` no MESMO
  padrão de `aggregates`/`usecasesByName`/`sagasByName`) e um 4º ramo na
  cadeia de resolução do alvo de um `Test` (depois de Aggregate/UseCase/Saga,
  antes do erro final; propriedades — §22.5 — continuam recusadas aqui, mesmo
  guard de UseCase/Saga). `given <binding> [...]` (`emitPolicyGiven`/
  `emitPolicyGivenEntity`) constrói cada item ("itemN := Tipo{}" + um
  "itemN.Campo = valor" por entrada do overlay `{...}`, SEM o prefixo
  ".state." de Aggregate — aqui o item É o receptor) e o adiciona ao MESMO
  var de pacote que a Policy sob teste lê (`policyCollectionVarName` reusada,
  nunca reimplementada); o(s) argumento(s) posicional(is) da própria
  construção (ex. o `"T1"` de `Ticket("T1")`) são ignorados — decisão
  documentada: o exemplo canônico do spec usa esse argumento só como rótulo
  legível, sem campo correspondente no item (diferente do `given` de
  Aggregate, cujo Event tem um `id` de verdade a espelhar). Achado NOVO,
  corrigido nesta fatia: `<tipo>Collection` é um var de PACOTE compartilhado
  por TODO `func TestX` do arquivo (Go roda os testes de um pacote
  sequencialmente, no mesmo processo) — sem reset, um item semeado por um
  cenário sobreviveria e contaminaria a contagem/filtragem do próximo;
  `emitPolicyGivenReset` reatribui cada `<tipo>Collection` referenciada pelo
  cenário a um `runtime.NewMemoryCollection[T]()` NOVO antes de semear (mesmo
  raciocínio de `<base>StepsOriginal` em `emitSagaScenarioBody`, adaptado:
  `Collection[T]` não tem um "original" para restaurar — cada cenário semeia
  do zero). `when event Evento(...)` chama a função gerada da Policy DIRETO
  (`RefundAllOnEventCancelled(ctx, &ev)`), como caixa-preta — o MESMO
  espírito de §22.1/22.2, nunca via `Dispatcher.Publish` (o Dispatcher aqui é
  o SEAM DE SAÍDA de uma Policy, não sua entrada).

  `then { emitted Evento(...), emitted count N }`: `policyEmittedEventNames`
  varre o CORPO da Policy (não o `then` do cenário — um cenário cujo `then`
  só tem `emitted count N` não revelaria tipo nenhum sozinho) por
  `*ast.EmitStmt` e devolve os nomes de Event distintos emitidos
  estaticamente; `emitPolicyDispatcherSetup` reatribui `policyDispatcher`
  (var de pacote de `decl_policy.go`) para um `runtime.NewDispatcher()`
  PRÓPRIO do cenário, com um `Subscribe` por nome apontando para o MESMO
  coletor (`published []runtime.Event`), instalado ANTES de invocar a Policy
  — só quando o cenário de fato usa `emitted` (guarda por SCENARIO, não só
  por arquivo). `emitted Evento(...)` busca, em `published`, ALGUM evento
  `reflect.DeepEqual` — decisão de design deliberada: checagem de MEMBRO
  (conjunto), ORDEM-INDEPENDENTE, não por índice estático como "Subject
  emitted" de §22.2 — uma Policy varre um `Collection[T]` cuja ordem é só a
  de inserção do `given`, sem garantia de negócio alguma sobre a ORDEM de
  emissão entre itens distintos, então comparar por conjunto é a leitura mais
  fiel do `then` declarativo do spec (limitação documentada, narrow: não é
  multiset — duas asserções `emitted X` idênticas no mesmo cenário poderiam
  casar contra o MESMO evento publicado; não exercitado pela fixture, cujos
  eventos são todos distintos). `emitted count N` compara `len(published)`.

  **Adaptação da fixture (mesmo precedente de "Fixtures de exemplo não são
  fonte de verdade", `.claude/specs/codegen/design.md` §6 — já usado pela
  fatia de Saga, que sintetizou o módulo `Booking`, e pela de property, que
  dropou o `Transfer` ilustrativo do spec):** nem wallet nem shop têm uma
  Policy com corpo de negócio de verdade (o único Policy do shop é `execute {
  return }`), então esta fatia sintetiza um módulo `Refunds` novo. O exemplo
  canônico do spec agrupa por `orderId` (3 tickets, 2 orders, 2
  `RefundRequested`) — exige `distinct`/agrupamento (§20), que este codegen
  NÃO implementa em lugar nenhum (confirmado: só citado como trabalho futuro
  em `codegen/lower/env.go`/`expr.go`). A fixture desta fatia dá a CADA
  ticket um `orderId` DISTINTO — "para cada Ticket casado pelo `where`, emite
  um `RefundRequested`" já produz, sem nenhuma lógica de dedup, um evento por
  ticket — e ajusta as contagens do `then` de acordo (4 tickets, 3 casam com
  o Event cancelado "E1", 1 não casa — 3 `RefundRequested`, não 2, e um
  scenario extra prova o predicado FILTRANDO de verdade: 1 ticket de outro
  evento, 0 reembolsos). Provado via golden (`testdata/
  tests_policy_refunds.go.golden`) + determinismo + smoke-compile do projeto
  inteiro + `TestEmitPolicyTestsRunGreen` (`go test ./...` de verdade sobre o
  projeto gerado — os 2 `func TestRefundAllOnEventCancelled_*` gerados a
  partir do `*.test.ds` SÃO os testes que rodam, mesmo espírito de
  `TestEmitSagaTestsRunGreen`/`TestEmitTestsWalletRunsGreen` — prova, sobre o
  Go de fato gerado, que o predicado por item filtra de verdade e que cada
  `RefundRequested` publicado é observado pelo coletor).

  **Adaptação REMOVIDA em `.claude/specs/read-side/tasks.md` (I6.2):**
  `distinct`/agrupamento fechou em I6.1 (§20, REQ-37.1) — a fixture voltou à
  forma EXATA do spec (`soldTickets.distinct(t => t.orderId)`, 3 tickets, 2
  orders, `emitted count 2`), removendo o "um `orderId` por ticket" acima.
  Único desvio remanescente: `reason` de `RefundRequested` usa um VO wrapper
  (`RefundReason(string)`) em vez do primitivo `string` cru do literal do
  spec (§22.4) — primitivo nu é proibido no Write Side (REQ-5.1).

  **H4 está completo** — as sete formas de cenário do §22 (cruzando as 4
  famílias de alvo: Aggregate/UseCase/Policy-Query/Saga, mais mock/fail-step/
  property/Fixture) estão todas cobertas. Só falta H5 (fechamento).
  **Commit:** `feat(codegen): geração de testes a partir de *.test.ds (cenário de Aggregate)`,
  `feat(codegen): geração de testes a partir de *.test.ds (cenário de UseCase)`,
  `feat(codegen): geração de testes a partir de *.test.ds (Fixture reusável)`,
  `feat(codegen): geração de testes a partir de *.test.ds (cenário de Saga)`,
  `feat(codegen): geração de testes a partir de *.test.ds (property-based)`,
  `feat(codegen): geração de testes a partir de *.test.ds (cenário de Policy/Query)`

- [x] **H5** Fechamento: auditoria de determinismo/idempotência (regen byte-idêntico,
  limpeza de órfãos), revisão contra o Definition of Done, atualizar `README.md`,
  `CLAUDE.md` (back-end deixa de ser "fora de escopo") e os specs. _(NFR-13, DoD §5)_

  **Auditoria de determinismo/idempotência — gap encontrado e fechado.** Antes
  desta task, `driver/generate_test.go` (`TestGenerateProjectIdempotentSameBytes`,
  `TestGenerateProjectRemovesOrphanFiles`, genéricos sobre projetos sintéticos) e
  `driver/generate_e2e_wallet_test.go` (`TestGenerateWalletE2ERegenTwoDirsByteIdentical`,
  `TestGenerateWalletE2ESmokeCompile`, `TestGenerateWalletE2EGoModHasNoExternalRequire`,
  `TestGenerateWalletE2EBehavior`) cobriam o Wallet — mas **nenhum teste chamava
  `GenerateProject` sobre `docs/examples/shop`**: `driver/shop_regression_test.go`
  só exercita o front-end (`CheckProject`/diagnósticos), e `codegen/channel_test.go`
  usa fixtures sintéticas só "inspiradas" na topologia do Shop. Isso era um gap
  genuíno contra a DoD §5.2, que cita `docs/examples/shop` explicitamente. Fechado
  com `driver/generate_e2e_shop_test.go`, espelhando a estrutura do arquivo do
  Wallet: `TestGenerateShopE2ELayout` (estrutura multi-service — dois
  `cmd/<service>/main.go`, um por Service da `topology.ds`, Sales e Delivery, não
  um `cmd/shop`; `contracts/events.go` para o `PublicEvent` compartilhado
  `OrderPlaced`), `TestGenerateShopE2ESmokeCompile` (`go build`/`go vet`/`go test`
  reais sobre a saída escrita em disco), `TestGenerateShopE2EGoModHasNoExternalRequire`
  e `TestGenerateShopE2ERegenTwoDirsByteIdentical`. Achado registrado no processo:
  `docs/examples/shop/orders/mod.ds` declara `Database MainDb { provider: "postgres" }`,
  mas `codegen/sql_wiring.go` (G1) só reconhece `"sqlite"` (case-insensitive) como
  adapter SQL real — `"postgres"` é decorativo (mesmo achado já documentado em
  `codegen/sql_adapter_test.go`) — e não há `Interface GRPC` nem `Telemetry` em
  nenhum `.ds` do Shop; logo o `go.mod` gerado do Shop, como o do Wallet, não tem
  nenhum `require` — provado empiricamente pelo teste, não só por leitura do
  código, então uma regressão futura em `sql_wiring.go` quebraria o teste, não
  passaria em silêncio. Julgamento sobre teste comportamental: o único Policy do
  Shop é `execute { return }` (sem lógica observável) e o exemplo não tem
  `*.test.ds`; sintetizar uma fixture de negócio à parte (mesmo esforço que a
  fatia de Policy/Query de H4 fez com o módulo `Refunds`) provaria uma fixture
  paralela, não o Shop real — decisão deliberada de **não** inflar essa superfície
  aqui; `go test ./...` sobre a saída real (incl. o runtime vendorado) já fecha o
  critério de fumaça honestamente. Os testes de auditoria pré-existentes do Wallet
  (`TestGenerateWalletE2E*`, `TestGenerateProjectIdempotentSameBytes`,
  `TestGenerateProjectRemovesOrphanFiles`) foram rodados de novo sobre o HEAD
  atual e continuam verdes — não apenas confirmados por existirem.

  **Revisão contra o Definition of Done (`requirements.md` §5):**

  1. **Atendido.** Todo construto do spec v6 modelado pelo front-end tem tarefa
     `[x]` em Marcos E–H (VO/Enum E3, Error/Event E4, Aggregate E6, Command/UseCase
     E7, View/Query/Projection E8, Policy/Worker F1/F2, Saga F3,
     Notification/Adapter/Foreign F4, gRPC H1, Telemetry/Metric H2/H3, testes H4).
     As exceções de §1.3 (exposição TCP/UDP, receptor `tenant` em corpos,
     `provision tenant(id)`, `events()` nativo) são exclusões documentadas porque
     o **front-end** também não as modela — não são gaps do gerador.
  2. **Atendido, e agora com paridade de evidência entre os dois exemplos.** Antes
     desta task o Wallet tinha cobertura E2E completa e o Shop só tinha cobertura
     de front-end; o gap foi fechado por `generate_e2e_shop_test.go` (acima).
  3. **Atendido.** `TestGenerateProjectIdempotentSameBytes` (mesmo `out`, duas
     rodadas, sem reescrita de arquivo inalterado — inclusive prova por `mtime`),
     `TestGenerateWalletE2ERegenTwoDirsByteIdentical` e
     `TestGenerateShopE2ERegenTwoDirsByteIdentical` (dois `out` distintos, bytes
     idênticos) — todos verdes.
  5. **Atendido.** Todo pacote de emissores (`codegen/decl_*.go`, `http.go`,
     `grpc.go`, `observ.go`, `gentest.go`, …) tem par de golden test (51 artefatos
     `.golden` sob `codegen/`); dependências externas (`sqlrt`, `grpcrt`, `otelrt`)
     isoladas em subpacotes próprios, referenciadas só quando o programa as exige
     (ver invariante de back-end registrada em `CLAUDE.md`).
  4. **Atendido.** `TestGenerateWalletE2EGoModHasNoExternalRequire` e
     `TestGenerateShopE2EGoModHasNoExternalRequire` prova que o `go.mod` de ambos
     os exemplos não tem `require`; o núcleo (`runtime/`) depende só da stdlib
     (REQ-16.2).
  6. **Atendido.** `cmd/dsc/main_test.go`: `TestRunGenRefusesInvalidProject` (exit
     1, nada escrito) e `TestRunGenValidProjectWritesFiles` (gera de fato), mais
     `TestRunGenMissingOutFlagExitsTwo` (uso incorreto → exit 2) — no nível de CLI,
     não só de `driver.GenerateProject`.
  7. **Atendido.** `go build ./...`, `go vet ./...`, `gofmt -l .` (vazio) e
     `go test ./...` rodados de fato nesta task sobre o HEAD após o fechamento —
     19 pacotes testáveis, todos `ok` (3 pacotes opt-in — `grpcrt`/`otelrt`/`sqlrt`
     — sem arquivo de teste próprio, o que é esperado: são runtime vendorado puro,
     exercitados pelos golden/smoke tests de `codegen` que os referenciam).

  Nenhum critério ficou incompleto — o único gap real encontrado (item 2, Shop
  sem cobertura E2E de geração) foi fechado nesta própria task, não adiado.

  `README.md` e `CLAUDE.md` (raiz) atualizados para descrever os dois estágios:
  pipeline com o estágio de geração, seção de back-end (`dsc gen`,
  `driver.GenerateProject`, o que é gerado, núcleo sem deps vs. deps opt-in),
  CLI com o subcomando `gen` (verificado contra `cmd/dsc/main.go` antes de
  documentar), Estado cobrindo os dois ciclos com os dois Definition of Done.
  `CLAUDE.md` ganhou uma seção "Back-end architecture invariants" (núcleo vs.
  deps opt-in, golden+smoke pareados, determinismo) e as entradas de pacote de
  `codegen/*` no layout — sem duplicar `design.md`/`tasks.md`.

  **Este é o fechamento de todo o ciclo `.claude/specs/codegen/` (Marcos E, F, G,
  H) — front-end e back-end do transpilador estão ambos completos.**
  **Commit:** `docs(repo): fecha o back-end e atualiza o estado`

---

## Mapa de Dependências

```
Marco E (núcleo runnable)
  E0 setup/prep ─▶ E1 emissor/nomes/tipos ─▶ E2 runtime ─┐
                                                          ├─▶ E3 VO/Enum ─┐
                                                          │                ├─▶ E5.0 TypeEnv ─▶ E5.1/2/3 lowering ─▶ E6 Aggregate ─▶ E7 UseCase ─▶ E8 Read ─▶ E9 HTTP ─▶ E10 CLI/smoke
                                                          └─▶ E4 Error/Event ┘
        │
        ▼
Marco F (reações) ─▶ Marco G (infra real) ─▶ Marco H (gRPC/OTel/testes/fechamento)
```

- **E0.3** (operadores de Money) é pré-requisito do smoke do Marco E (E10.2).
- **E5.0** (TypeEnv) precede todo lowering de corpo (E5.1–3, E6–E8): sem tipo de
  local, o dispatch de operador (§4.2) e o acesso a membro não têm forma Go.
- E5 depende de E1–E4; E6–E8 dependem de E5. F/G/H dependem do Marco E completo. G1
  (database/sql) não pode quebrar o caminho in-memory (NFR-12). H depende de F+G.

---

## Estratégia de Entrega Incremental

1. **Marco E — "gera e roda o núcleo transacional"**: primeiro projeto Go gerado que
   compila e roda com `go run`, zero deps externas. Valor demonstrável cedo.
2. **Marco F — "reações e coordenação"**: Policy/Worker/Saga/IO — reage a eventos e
   coordena transações distribuídas (shop cross-service).
3. **Marco G — "infraestrutura real"**: `database/sql`, ops de arquivo, idempotência,
   cache, rate limit, tenancy — pronto para banco real.
4. **Marco H — "exposição e observabilidade avançadas + testes"**: gRPC, OTel,
   métricas, testes gerados; fechamento e determinismo.

---

## Rastreabilidade REQ → Marco/Tasks

| Requisito | Tasks |
|---|---|
| REQ-14 | E0.1, E10.1 |
| REQ-15 | E1.1, E1.2, E1.3 |
| REQ-16 | E2.1 |
| REQ-17 | E0.3, E3.1, E3.2, E3.3 |
| REQ-18 | E4.1, E4.2, E4.3 |
| REQ-19 | E6.1, E6.2 |
| REQ-20 | E7.1, E7.2, G1 (2PC), G2 |
| REQ-21 | E8.1, E8.2, G3 |
| REQ-22 | E5.0, E5.1, E5.2, E5.3, G1a |
| REQ-23 | F1, F2 |
| REQ-24 | F3, F5 (canais) |
| REQ-25 | F4, F5, G1a |
| REQ-26 | E9.1, F5 (canais), G1, G1a |
| REQ-27 | G5 |
| REQ-28 | E9.2, G4, G6 |
| REQ-29 | H1 |
| REQ-30 | H2, H3 |
| REQ-31 | H4 |
| REQ-32 | E0.1, E10.1 |
| NFR-11..17 | transversais (golden + smoke em cada task; E0.2, E10.2, H5) |
