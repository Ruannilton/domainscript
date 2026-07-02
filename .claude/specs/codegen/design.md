# Design — Back-end do Transpilador DomainScript (Geração de Go)

> Documento 2 de 3. Define **como** atender `requirements.md` (REQ-14..32,
> NFR-11..17). Cada decisão referencia os REQ/NFR que satisfaz. Estende os
> `design.md` anteriores (front-end e type-checking); reusa seus invariantes e o
> modelo de dados já implementado (`ast`, `symbols`, `types`, `program`).

## 1. Visão Arquitetural

### 1.1. Onde o trabalho mora

O pipeline atual termina no CHECKER, com o veredito num `DiagnosticBag`. Este
trabalho **adiciona um estágio de topo — o GERADOR — depois da validação**, sem
tocar nas fases anteriores. O gerador só roda se a validação passou.

```
... RESOLVER ──▶ CHECKER ──▶ (Program válido, DiagnosticBag)
                                        │
                          HasErrors()?  ├── sim ──▶ recusa (não gera) — REQ-14.1
                                        └── não ──▶ GERADOR ──▶ projeto Go
                                                    REQ-14..32
```

Decisão de fronteira (NFR-14): a **única** entrada do gerador é o que as
fases anteriores já produziram — `program.Program` (ASTs + grafo + módulos +
services + canais), `symbols.SymbolTable` (via `prog.Symbols`) e um
`types.Model` construído sobre a tabela (`types.NewModel(prog.Symbols)`). O gerador
**nunca** re-lexa nem re-parseia. A resolução de nomes (REQ-9) já foi validada,
mas a AST **não** carrega o símbolo resolvido em cada nó (`ast.Ident` não tem campo
de símbolo): por isso a geração **reconsulta** a `SymbolTable` e **reconstrói** o
tipo de cada nome/local via `types.Model` e um ambiente de tipos próprio (§3.6a).
Isso mantém a separação de fases dura (análoga a NFR-6) e torna a geração testável
isoladamente com ASTs sintéticas.

### 1.2. Invariantes preservados (dos ciclos anteriores)

- **Separação de fases:** o gerador consome `(AST resolvida, Model)`; não produz
  diagnósticos de análise. Erros de geração (que não deveriam existir sobre um
  programa válido) são bugs, tratados defensivamente (REQ-14.4).
- **Determinismo (NFR-3 → NFR-13):** toda iteração sobre mapas é ordenada; a ordem
  de declarações segue a ordem de origem (a AST preserva ordem de arquivo) e o
  conjunto de arquivos é ordenado por caminho (o `program` já ordena — `program.go`
  ordena `sources` e `Build` ordena `paths`).
- **Anti-cascata / tolerância a erro (REQ-4.5):** subárvores com nós de erro são
  puladas; como o gerador só roda sobre programa válido, isso é rede de segurança.
- **Reuso da travessia (NFR-8):** o lowering reusa os helpers **exportados** de
  `astutil` (`ForEachStmt`, `ForEachExpr`, `ForEachExprInBlock`, `StmtExprs`,
  `DeclBlocks`, `StateField`, `HeadName`, `IsIdent`) em vez de reescrever percursos.
  O lowering acrescenta, por cima deles, um percurso **tipado** (§3.6a): os helpers
  dão a estrutura; o `TypeEnv` dá o tipo de cada nó, que os helpers não conhecem.

### 1.3. Linguagem-alvo e mecanismo de emissão

Alvo: **Go** (o alvo canônico do spec). Mecanismo: **emissor de strings +
`go/format` (stdlib)**. O gerador escreve Go num buffer com helpers de indentação
e, ao fechar cada arquivo, passa o conteúdo por `format.Source` (stdlib), que:

- garante formatação `gofmt` (REQ-15.1);
- rejeita Go inválido em tempo de geração (uma falha de `format.Source` é um bug do
  gerador, apanhado nos golden/smoke tests — NFR-14);
- não adiciona dependência externa alguma (NFR-12).

**Alternativa considerada e rejeitada:** construir `go/ast` + `go/printer`. Garante
validade por construção, mas é muito mais verboso para cada nó e produz código com
formatação menos controlável; o custo/benefício não compensa para a legibilidade
buscada (NFR-11). `text/template` foi rejeitado por dar pouco controle sobre imports
e desambiguação de nomes. A decisão fica registrada em §6.

---

## 2. Estrutura de Pacotes (delta)

```
codegen/                     # NOVO PACOTE — orquestra a geração (REQ-14)
├── codegen.go               # Generate(prog, model, opts) → []File; pré-condição
├── project.go               # layout do projeto: go.mod, cmd/, pacote por módulo
├── decl_value.go            # VO e Enum (REQ-17)
├── decl_event.go            # Error, Event, registry, upcast, redactable (REQ-18)
├── decl_aggregate.go        # Aggregate: state/Handle/Apply/reconstrução (REQ-19)
├── decl_app.go              # Command, UseCase, unit of work (REQ-20)
├── decl_read.go             # View, Query, Projection (REQ-21)
├── decl_react.go            # Policy, Worker, Saga (REQ-23/24)
├── decl_io.go               # Notification, Adapter, Foreign (REQ-25)
├── infra.go                 # mod.ds → wiring + seam de persistência (REQ-26/27)
├── http.go                  # interface.ds HTTP → net/http (REQ-28)
├── grpc.go                  # interface.ds GRPC → .proto + stubs (REQ-29)
├── observ.go                # Telemetry, Metric (REQ-30)
├── gentest.go               # *.test.ds → testes Go (REQ-31)
├── names.go                 # mapeamento de identificadores idiomáticos (REQ-15.2)
│
├── emit/                    # NOVO — o emissor de Go formatado (REQ-15)
│   ├── file.go              # buffer, imports, format.Source no fechamento
│   └── emit_test.go
│
├── lower/                   # NOVO — lowering de Expr/Stmt/Block → Go (REQ-22)
│   ├── env.go               # TypeEnv: escopo tipado + inferência de locais (§3.6a)
│   ├── expr.go              # expressões (operadores de VO, membro, chamada, ...)
│   ├── stmt.go              # ensure, match, for, emit, return, log, assign
│   └── builtins.go          # now/uuid/load/list/count/... (§2.5/§2.6)
│
└── rtsrc/                   # NOVO — fonte do runtime vendorado (REQ-16)
    ├── embed.go             # //go:embed dos .txt; expõe o conjunto de arquivos
    ├── eventstore.go.txt    # (build-ignored) event store: interface + in-memory
    ├── repository.go.txt    # repositório de aggregate: interface + in-memory
    ├── dispatcher.go.txt    # dispatcher de eventos (Policies)
    ├── uow.go.txt           # unit of work + transação
    ├── idempotency.go.txt   # idempotency store
    ├── scheduler.go.txt     # agenda de Workers (every/cron/continuous)
    ├── cache.go.txt         # cache in-memory
    └── ...

driver/
└── driver.go                # + GenerateProject(dir, out) — API pública (REQ-32)

cmd/dsc/
└── main.go                  # + subcomando `gen` (REQ-32)
```

Direção de dependências (sempre "para baixo", como o resto do projeto):
`driver → codegen → {lower, emit, program, types, symbols, ast, token}`;
`lower → {emit, types, symbols, astutil, ast, token}`; `emit → stdlib` só. O
`rtsrc/` não importa nada do projeto: são arquivos-fonte opacos embutidos.

**Runtime como fonte embutida:** os arquivos do runtime são Go real, mas guardados
com sufixo `.go.txt` para **não** compilarem junto do compilador (evita que o
`codegen` dependa do runtime). São embutidos via `//go:embed` (stdlib) e escritos
no projeto gerado com extensão `.go`. Um teste do compilador copia `rtsrc/` para um
diretório temporário, renomeia e roda `go build` para garantir que o runtime em si
compila (NFR-14). Decisão registrada em §6.

---

## 3. Componentes

### 3.1. Orquestrador (`codegen/codegen.go`) — REQ-14

```go
type Options struct {
    ModulePath string // caminho do módulo Go gerado (default: derivado do nome do dir de saída)
    GoVersion  string // default "1.22" (mínimo p/ ServeMux METHOD /path/{param} — REQ-28)
}

type File struct { Path string; Content []byte } // caminho relativo à raiz de saída

// Generate produz os arquivos do projeto Go a partir de um programa VÁLIDO.
// Retorna erro se o bag tem erros (REQ-14.1) — o gerador não valida, só verifica.
func Generate(prog *program.Program, model *types.Model, bag *diag.DiagnosticBag, opts Options) ([]File, error)
```

Fluxo:

1. **Pré-condição (REQ-14.1):** se `bag.HasErrors()` → retorna `ErrHasDiagnostics`,
   sem gerar. O `driver` decide imprimir os diagnósticos.
2. **Runtime (REQ-16):** emite `runtime/*.go` a partir de `rtsrc/` (verbatim) e
   `go.mod` (REQ-14.5, `project.go`).
3. **Por módulo (REQ-14.5):** o `program` não expõe um helper "arquivos do módulo
   X"; o gerador agrupa `prog.Files` (um `map[path]*ast.File`) por
   `prog.ModuleOf(path)`, **iterando os caminhos em ordem alfabética** (o mapa não
   tem ordem — determinismo, NFR-13). Para cada módulo cria um pacote Go e percorre
   as `Decl` dos seus arquivos, roteando por tipo para o emissor correspondente
   (`decl_*.go`), na ordem de origem. Arquivos com `ModuleOf == ""` (fora de módulo:
   `topology.ds`, `versions/`) são tratados à parte pelas bordas/wiring.
4. **Bordas e wiring:** gera HTTP (`http.go`), gRPC (`grpc.go`), observabilidade e o
   `cmd/<service>/main.go` por service (a partir de `prog.Services`).
5. Ordena os `File` por `Path` antes de retornar (NFR-13).

O roteamento por tipo de `Decl` é um `switch` (espelha o `parseFile` do parser),
ponto único de extensão por construto novo (NFR-16).

### 3.1a. Contexto ambiente (`context.Context`) — REQ-20/27/30

`caller` (§access), `tenant` (§13), a chave de idempotência (§14) e o trace (§21) são
**contexto ambiente** — nunca parâmetros de domínio. O veículo é o `context.Context`,
**threadado em toda fronteira do runtime desde o Marco E** (E2.1): `Store.Load(ctx,
id)`, `uow.Run(ctx, fn)`, `repo.Count(ctx, …)`; um `Handle` recebe `caller` explícito
(`runtime.Caller`) e o `ctx` quando seu corpo precisa (`now()`/`load`/log).
`caller`/`tenant`/idempotency-key/trace entram no `ctx` via **chaves tipadas do
pacote `runtime`** (ex.: `runtime.CallerFrom(ctx)`), **no-op** até os marcos que os
ativam (tenancy G5, idempotência G2, trace H2). Fixar `ctx` cedo evita reescrever
todas as assinaturas do runtime quando esses marcos chegarem. *(Decisão
pré-desenvolvimento; §6.)*

### 3.2. Emissor (`codegen/emit`) — REQ-15

```go
type Emitter struct {
    // buffer, nível de indentação, conjunto de imports (path → alias)
}
func New(pkg string) *Emitter
func (e *Emitter) Import(path string) string        // registra e devolve o nome usável
func (e *Emitter) Line(format string, args ...any)  // linha indentada
func (e *Emitter) Block(head string, body func())   // "head {" … "}" com indent
func (e *Emitter) Bytes() ([]byte, error)            // monta header+imports+corpo, roda format.Source
```

Decisões:

- **Imports geridos, nunca à mão (REQ-15.3):** o emissor coleta os imports usados e
  os escreve ordenados no header; `format.Source` valida que não sobra import não
  usado (Go rejeita → apanhado como bug de geração).
- **`Bytes()` roda `format.Source` (REQ-15.1):** ponto único onde a formatação é
  garantida; se falhar, devolve o erro com o Go bruto para depuração.
- **Determinismo (NFR-13):** imports ordenados; nenhum `map` é iterado sem
  ordenação; o buffer é append-only.

### 3.3. Estratégia de nomes (`codegen/names.go`) — REQ-15.2

Mapeamento DomainScript → identificador Go, determinístico e idiomático:

| Origem DomainScript | Go | Exemplo |
|---|---|---|
| Nome de declaração (tipo) | PascalCase exportado (já é) | `Wallet` → `Wallet` |
| Campo de struct | Exportado (capitaliza) + tag json com nome original | `balance` → `Balance` `` `json:"balance"` `` |
| Operador de VO | Método nomeado | `+`→`Add`, `-`→`Sub`, `*`→`Mul`, `/`→`Div`, `>=`→`Gte`, `<=`→`Lte`, `>`→`Gt`, `<`→`Lt`, `==`→`Eq`, `!=`→`Neq` |
| Membro de Enum | `Tipo` + `Membro` const | `TransactionType.Deposit` → `TransactionTypeDeposit` |
| Parâmetro / var local | camelCase (mantém) | `amount` → `amount` |
| Receptor de Aggregate | idiomático | `self`→receiver (`w`); `state`→`w.state`; `event`→`ev`; `caller`→`caller` (`runtime.Caller`) |
| Receptor de VO | **não é `self`** | `value`→o valor embrulhado (wrapper) ou o próprio receptor (composto); campos do VO composto por **nome nu** (`amount`→`m.Amount`); `ok`→sentinela "validação passa" (ver abaixo) |
| Colisão com keyword Go | sufixo `_` determinístico | `type`→`type_`, `range`→`range_`, `func`→`func_` |

**Receptores de VO (alinhados ao front-end, `resolver/receivers.go`).** O front-end
**não** semeia `self` em corpos de VO: `Valid` vê `value` e `ok`; `Operator` e
`coerce` veem `value`; num VO composto os campos são nomes nus. A tabela §3.3 do
front-end é o contrato — o lowering **espelha exatamente** esses nomes, senão o Go
gerado referencia identificadores que não existem. (Observação: os exemplos do spec
v6 §2.2 escrevem `self.amount`/`self.currency` em operadores de `Money`; isso
**diverge** do front-end atual, que usa `value` + campos nus. O gerador segue o
front-end, não a prosa do spec.) O sentinela **`ok`** (só em `Valid`, ex.:
`ValueObject ActiveStatus(boolean) { Valid { ok } }`) significa "a validação passa":
lowering → `NewActiveStatus` não retorna erro. *(Decisão confirmada pelo dono da
linguagem; §6.)*

**Colisão módulo↔tipo.** É legal (e ocorre no exemplo wallet) um `Module Wallet`
conter um `Aggregate Wallet`: o pacote Go é `wallet` (minúsculo) e o tipo é `Wallet`
(exportado) — não colidem. Mas um `Command Deposit` e um `Handle Deposit` coexistem:
viram, respectivamente, o tipo `Deposit` e o método `(*Wallet).Deposit`, também sem
colisão. `names.go` só desambigua contra **palavras-reservadas Go** e contra dois
símbolos que mapeariam ao mesmo identificador exportado no mesmo pacote. **Colisão
intra-pacote:** aplica-se um sufixo **numérico determinístico** na ordem estável de
declaração (o 2º símbolo vira `Nome2`, etc.). **Refs cross-pacote:** `names.go`
integra com `emit.Import` para produzir referências **qualificadas por pacote**
(`contracts.OrderPlaced`, `orders.Wallet`) — necessário para os `PublicEvent` do
pacote `contracts/` (§3.4) e para dependências entre módulos. *(§6.)*

**Mapeamento de tipos primitivos → Go** (único e documentado — REQ-15.6). Os tipos
de `File` (§2.5) são opacos no `types.Model` (tratados como primitivos, sem campos):
suas structs vêm de um **template fixo do runtime** (§3.6), não do modelo.

| Primitivo DomainScript | Go | Nota |
|---|---|---|
| `integer` | `int64` | — |
| `decimal` | `runtime.Decimal` | decimal de **escala fixa** (sobre `math/big.Int`, sem dep externa), arredondamento **half-even**, escala default **4** — nunca `float64` (§3.5/§6) |
| `string` | `string` | — |
| `boolean` | `bool` | — |
| `datetime` | `time.Time` | stdlib |
| `bytes` | `[]byte` | — |
| `duration` (literal) | `time.Duration` | stdlib |
| `size` (literal) | `int64` | bytes |
| `rate` (literal) | `runtime.Rate` | ex.: `1000/min` (rate limit/worker) |
| `File`/`FileStream`/`FileRef` | `runtime.File`/`runtime.FileStream`/`runtime.FileRef` | structs built-in do runtime |
| `List<T>`/`Set<T>` | `[]T`/`map[T]struct{}` | §2.4 |
| `AppendList<T>` | `runtime.AppendList[T]` | só `Add` exposto (REQ §23) |
| `Map<K,V>` | `map[K]V` | — |

Nomes de arquivo gerados seguem `snake_case` do construto (`aggregate_wallet.go`,
`events.go`, `usecases.go`).

### 3.4. Layout do projeto gerado (`codegen/project.go`) — REQ-14.5

```
<out>/
├── go.mod                      # module <path>; go <version>; sem require (núcleo)
├── runtime/                    # vendorado, hand-written (REQ-16)
│   ├── eventstore.go
│   ├── repository.go
│   └── ...
├── contracts/                  # PublicEvents (pacote compartilhado — quebra ciclos)
│   └── events.go               # espelha contracts/*.ds; importado por vários módulos
├── <modulo>/                   # um pacote Go por módulo de domínio
│   ├── valueobjects.go         # VOs + Enums do módulo
│   ├── errors.go               # Errors de negócio
│   ├── events.go               # Events **privados** do módulo + registry
│   ├── aggregate_<nome>.go     # um por Aggregate
│   ├── commands.go             # Commands
│   ├── usecases.go             # UseCases
│   ├── queries.go              # Views + Queries + Projections
│   └── ...                     # policies.go, workers.go, sagas.go (Marco F)
├── http/                       # borda HTTP (REQ-28) — por service
│   └── router.go
└── cmd/<service>/main.go       # wiring + servidor, um por service da topologia
```

**`PublicEvent` num pacote `contracts/` compartilhado (§6).** Um `PublicEvent` é
referenciado por módulos (e services) diferentes — ex.: `Policy NotifyShipping on
OrderPlaced` (módulo Shipping) reage a um `PublicEvent OrderPlaced` de Orders. Gerar
todo `PublicEvent` num pacote `contracts/` (espelhando `contracts/*.ds`) **quebra por
construção** os ciclos de import módulo↔módulo; o `Event` privado fica no pacote do
seu módulo. O emissor produz refs qualificadas (`contracts.OrderPlaced`) desde E1.1.

Para um monólito (um único service) há um só `cmd/`; para microsserviços, um `cmd/`
por service, cada um wirando só os módulos do seu service (via `prog.Services` e
`prog.ServiceOfModule`).

### 3.5. Catálogo de Mapeamento (o coração — REQ-17..25)

| Construto DomainScript | Artefato Go gerado | Notas |
|---|---|---|
| `ValueObject Email(string)` | `type Email string` + `func NewEmail(v string) (Email, error)` | `NewEmail` roda `Valid` |
| `ValueObject Money { … }` | `type Money struct{ Amount runtime.Decimal; Currency string }` + `NewMoney(...) (Money, error)` | imutável (sem setters); `decimal` → `runtime.Decimal` de escala fixa (§3.3/§6) |
| `Operator +(o Money) -> Money` | `func (m Money) Add(o Money) (Money, error)` | corpo via lowering; propaga `error` do `ensure` |
| `Enum TransactionType : string` | `type TransactionType string` + `const (…)` + `func ParseTransactionType(s string) (TransactionType, error)` | `coerce` → corpo do `Parse` |
| `Error InsufficientBalance` | `var ErrInsufficientBalance = runtime.BusinessError{Code:"…",Msg:"…"}` | comparável, `errors.Is` |
| `Event DepositPerformed { … }` | `type DepositPerformed struct{…}` + `func (DepositPerformed) EventType() string` + registro no registry | metadata via embed `runtime.EventMeta` |
| `Aggregate Wallet` | `type Wallet struct{ id …; state walletState }` + métodos | ver §3.7 |
| `Handle Deposit(...)` | `func (w *Wallet) Deposit(caller runtime.Caller, amount Money, …) ([]runtime.Event, error)` | checa `access`, corpo, retorna eventos |
| `Apply DepositPerformed` | `func (w *Wallet) applyDepositPerformed(ev DepositPerformed)` | muta `w.state` |
| `Command DepositCmd` | `type DepositCmd struct{…}` | DTO; idempotência no runtime |
| `UseCase PerformDeposit` | `func PerformDeposit(ctx, cmd DepositCmd) error` | unit of work — §3.8 |
| `Query GetStatement(...)` | `func GetStatement(ctx, walletId WalletId, page int) ([]StatementEntryVW, error)` | §3.9 |
| `View WalletSummaryVW` | `type WalletSummaryVW struct{…}` | `visibility` → serialização condicional |
| `Policy … on Event` | `func(ev Event) error` registrada no dispatcher | §3.10 |
| `Worker …` | goroutine agendada | §3.10 |
| `Saga …` | state machine com steps up/down | §3.10 |
| `Notification` / `Adapter` | contrato + chamada `net/http`/FFI | §3.13/decl_io |
| `Foreign "go" from "…"` | chamada direta à função em `adapters/` | assinatura verificada no front-end |
| `Metric` | atualização de counter/histogram no gatilho | §3.14 |

**Taxonomia de erros (§6).** `Error`s de negócio viram `runtime.BusinessError{Code,
Msg}` com `Code` = **nome do `Error`** (comparável, `errors.Is`). **Tudo que não for
`BusinessError` é infraestrutura** → 503 + retry na borda; a borda distingue por
`errors.As(&BusinessError)`. `runtime.ErrForbidden`/`runtime.ErrNotFound` são
`BusinessError`s **reservados** (403/404).

### 3.6. Lowering de Expressões e Statements (`codegen/lower`) — REQ-22

O lowering desce recursivamente sobre `ast.Expr`/`ast.Stmt`, consultando o
`types.Model` **através do `TypeEnv` (§3.6a)** para decidir a forma Go. Regras
principais:

**Expressões (`expr.go`):**

- `*ast.Literal` → literal Go (INT/FLOAT/STRING; DURATION→`time.Duration`;
  SIZE→bytes; booleanos).
- `*ast.Ident` → o nome mapeado (var local, receptor contextual, membro de Enum
  → const).
- `*ast.BinaryExpr` (dispatch completo em §4.2): (a) operandos **VO** com `Operator`
  correspondente → `left.Op(right)` (propaga `error` se o operador faz `ensure` —
  REQ-22.5/9); (b) **primitivos** → operador Go nativo; (c) VO com `==`/`!=` **sem**
  operador declarado → comparação Go nativa (os structs/wrappers de VO são
  comparáveis); (d) VO com aritmético/relacional (`+`,`-`,`>=`,…) **sem** operador
  declarado → **erro de geração** (não há método a chamar; ver o caveat do `Money` do
  wallet em §7).
- `*ast.MemberExpr` `X.name` → `lowered(X).Name` (campo exportado) ou chamada de
  método; usa `Model.Members` para saber se é campo ou método.
- `*ast.CallExpr`: **construção de VO/Event/Command** (`Fn` é um tipo) → `NewT(...)`
  ou literal de struct com campos nomeados; **chamada de método** → método Go;
  **built-in** → §builtins.
- `*ast.IndexExpr`/`*ast.RangeExpr`/`*ast.LambdaExpr` → indexação, range de `for`, e
  closure Go (`func(t T) … { return … }`) usada em `distinct`/`sum`/`focus`.
- `*ast.MatchExpr` → função-expressão `switch` (ou variável temporária atribuída num
  `switch`).
- `*ast.QueryExpr` (`load`/`list`/`count`/`store`/`exists`/`delete`) → §3.9.

**Statements (`stmt.go`):**

- `*ast.EnsureStmt` (REQ-22.1): `if !cond { <ação> }`. Ação por contexto:
  `Error` → `return zero, ErrX`; `Nop` → `continue`/no-op; `break`/`break all`/
  `continue` → controle de laço (labels para `break all`).
- `*ast.MatchStmt` (REQ-22.2): `switch subject { case …: … }`, exaustivo. Enum
  coberto → `case TipoMembro:` sem `default`; guards → `switch { case cond: }` com
  `default` do wildcard. A exaustividade já foi garantida no front-end (§design 4.2);
  o gerador só reflete.
- `*ast.ForStmt` (REQ-22.3): `for _, v := range iter { … }` (coleção) ou
  `for i := low; i <= high; i++` (range).
- `*ast.EmitStmt` (REQ-22.4): constrói o evento e faz `events = append(events, ev)`;
  o Handle retorna `events`.
- `*ast.ReturnStmt`, `*ast.AssignStmt`, `*ast.LogStmt` → `return`, `:=`/`=`, chamada
  de log do runtime (`slog` com trace context — REQ-22.8/REQ-30.1).

**Built-ins (`builtins.go`, REQ-22.7):** duas famílias, com marcos distintos.

- **Núcleo (Marco E), sem dep externa:** `now()`→`runtime.Now(ctx)`,
  `uuid()`→`runtime.UUID()`, `random`/`random_str`→helpers do runtime,
  `load T(id)`→`repo.Load`, `list T t where …`→query do runtime, `count`→`repo.Count`,
  `exists` (QueryExpr pós-fixo, `ensure x exists`) → checagem de existência do `load`.
- **Ops de arquivo (§2.5), dependem do seam `FileStorage` do `mod.ds`:**
  `store f`→`storage.Store`, `signed_url`→`storage.SignedURL`, `delete file(r)`→
  `storage.Delete`, `load File(ref)`→`storage.Load`. Como exigem um bloco
  `FileStorage` (que o wallet **não** tem), **não** entram no Marco E: aterrissam com
  a infra de módulo (REQ-26, Marco G). O núcleo transacional não referencia storage.

**Métodos embutidos sobre primitivos/coleções.** Corpos usam métodos que não são
built-ins de topo nem operadores de VO: `value.length()` (string→`len`),
`state.entries.add(x)` (`AppendList.Add`), `list.distinct(lambda)`, `list.sum(lambda)`,
`state.items.focus(id)` (§20). `builtins.go` mantém uma **tabela `(tipo-receptor,
método) → emissão Go`** para esses casos; um par ausente é erro de geração (apanhado
no smoke), não Go arbitrário.

**Propagação de erro (REQ-22.9):** operações que podem falhar (operador de VO com
`ensure`, coerção, `load`) geram o padrão `x, err := …; if err != nil { return … }`.
Nunca `panic` no caminho de negócio (NFR-15).

### 3.6a. Ambiente de tipos local do lowering (`codegen/lower/env.go`) — REQ-22.6

O lowering decide a forma Go de muitos nós a partir do **tipo estático** do
receptor/operandos (`a + b` é `a.Add(b)` ou `a + b` nativo — §4.2; `x.campo` exige a
shape de `x`; `emit E(args)` exige os campos de `E`). Fornecer esse tipo é de um
**ambiente de tipos local** (`TypeEnv`) que o gerador constrói e propaga por cada
corpo — porque **o front-end não o entrega pronto**:

- A AST **não** anota símbolo/tipo por nó; a resolução (REQ-9) só validou nomes. O
  gerador reconsulta `SymbolTable.Lookup(module, name)` e reconstrói tipos via
  `types.Model`.
- `types.Model.Infer(module, e, sc)` cobre literal, ident, membro, chamada,
  binário/unário, índice e lista, mas devolve `ErrorType` para `*ast.QueryExpr`
  (`load`/`list`/`count`/`store`/`exists`), `*ast.MatchExpr` e `*ast.LambdaExpr`
  (`types/infer.go`, ramo `default`). São exatamente as formas que introduzem locais
  em corpos de UseCase/Query/Policy.
- Nem o resolver nem o checker registram o **tipo** de um local de atribuição: o
  resolver guarda só o *nome* (`Binding`, `resolver/resolve_body.go`) e o checker de
  membro semeia apenas receptores + parâmetros e é deliberadamente conservador
  (`sema/rules_typecheck.go`) — suficiente para validar (anti-cascata), insuficiente
  para gerar (que precisa de uma forma Go concreta para **todo** nó).

Portanto o `TypeEnv` implementa `types.Scope` e:

1. semeia os receptores contextuais **com o mesmo mapeamento do front-end**
   (`resolver/receivers.go`): Handle→`self`/`state`/`caller`; Apply→`state`/`event`;
   Access→`self`/`caller`; UseCase.execute→`cmd`/`caller`; Policy.execute→`event`/
   `caller`; VO `Valid`→`value`/`ok`; VO `Operator`→`value` + campos do VO por nome
   nu; `coerce`→`value`; Saga step→`state`;
2. semeia os parâmetros declarados com o tipo (`Model.TypeOfRef`);
3. estende o escopo em cada `*ast.AssignStmt` de alvo nu (`x = e`) inferindo o tipo
   de `e`, **incluindo as formas que `Infer` não cobre**: `load T(id)`→o tipo do
   Aggregate `T`; `list T … as V`→`List<V>` (sem `as`: `List<T>`); `count …`→
   `integer`; `store f`→`FileRef`; lambda→tipo do corpo; `match`→tipo do 1º braço;
4. abre escopos-filho nos binders (`for`, braço de `match`, `lambda`, alias de
   `list`/`join`) com o tipo do elemento iterado.

Decisão (registrada em §6): **estender a inferência dentro do `codegen`**, não em
`types` — mantém o front-end intacto e concentra no back-end o conhecimento extra que
só a geração precisa. Onde o tipo for genuinamente desconhecido, o lowering **falha
explicitamente** (bug de geração, apanhado no smoke/golden — NFR-14), em vez de emitir
Go arbitrário.

### 3.7. Aggregates: StateStored vs EventSourced (`decl_aggregate.go`) — REQ-19

```go
type Wallet struct {
    id      WalletId
    version int
    state   walletState        // struct dos campos de state{}
}
type walletState struct { Balance Money; Active ActiveStatus; /* … */ }
```

- **Handle (REQ-19.2):** recebe `caller` + params; primeiro emite a checagem de
  `access` (`if !(<condição do access>) { return nil, runtime.ErrForbidden }`),
  depois o corpo; acumula `emit`s em `events` e devolve `([]runtime.Event, error)`.
- **Apply (REQ-19.3):** método privado que muta `w.state` a partir do evento.
- **EventSourced (REQ-19.4):** `func LoadWallet(store, id) (*Wallet, error)` lê o
  stream (a partir do snapshot mais recente, se `snapshot every N`) e aplica cada
  evento via `applyX`. O commit de um Handle **append-a** os novos eventos ao store.
  A **chave de stream** é o **id do aggregate emissor**, atribuída como metadata no
  `append` (junto de `sequence`/`timestamp`/`eventType`); o campo `id` do payload de
  um evento é dado de negócio, **não** a chave. `LoadWallet` itera o stream por essa
  chave (`aggregateId`). *(Decisão pré-desenvolvimento; §6.)*
- **StateStored (REQ-19.5):** `LoadWallet` lê o `state` direto do repositório; o
  commit persiste o `state` mutado. Os mesmos `Handle`/`Apply` valem — a diferença é
  só a fonte/destino de persistência (seam do runtime, REQ-26).

### 3.8. UseCase e Unit of Work (`decl_app.go`) — REQ-20

```go
func PerformDeposit(ctx context.Context, cmd DepositCmd) error {
    return uow.Run(ctx, func(tx runtime.Tx) error {   // unit of work — REQ-20.2
        wallet, err := LoadWallet(tx, cmd.WalletId)
        if err != nil { return err }
        // ensure wallet exists else WalletNotFound
        events, err := wallet.Deposit(caller(ctx), cmd.Amount, cmd.Description)
        if err != nil { return err }
        return tx.Append(wallet.id, events)           // commit atômico no Run
    })
}
```

- `uow.Run` abre a transação, executa e faz commit/rollback (REQ-20.2). `timeout` →
  `context.WithTimeout` (REQ-20.3). `idempotency` → o `Run` consulta o idempotency
  store antes/depois (REQ-20.4).
- A **inferência transacional** (§18.1) usa `prog.DatabaseOfAggregate` /
  `ServiceOfAggregate`: mesmo Database → um `Tx`. Databases distintos **todos com
  `supportsXA`** são **válidos** para o front-end (`rules_crossfile` só barra "sem
  XA universal") → o `uow.Run` coordena um **2PC** sobre os stores envolvidos
  (prepare/commit em duas fases atrás do seam de persistência). No in-memory
  (Marco E) há um único store e o caso degenera em commit local; o 2PC real
  aterrissa com o adapter `database/sql` (G1). O caso proibido (sem XA universal /
  cross-service sem Saga) já foi barrado no front-end (REQ-5.9), então o gerador
  assume o caminho válido.
- **Tipo de um campo `ref Aggregate`.** O `types.Model` neutraliza campos `ref` para
  `ErrorType` (`CtorParams`/`ctorFields`): o id não é modelado ali. Para o DTO do
  Command o gerador precisa de um tipo Go concreto — regra: `ref T` → o tipo do campo
  `id` do `state` de `T` (ex.: `Command Deposit { walletId ref Wallet }` e
  `Aggregate Wallet { state { id WalletId … } }` ⇒ campo Go `WalletId WalletId`). Se
  `T` não declara `id`, cai para `string`. `load T(cmd.walletId)` usa esse mesmo tipo.

### 3.9. Read Side (`decl_read.go`) — REQ-21

O `*ast.QueryExpr` e as `QueryClause` (`where`/`join`/`orderBy`/`skip`/`take`/`as`)
lowerizam para operações do runtime de query. Um seam único:

- **In-memory (Marco E):** as cláusulas viram operações sobre slices (filter/sort/
  paginate) — legível e sem dep externa. Um `list <VO aninhado>` (ex.: `list
  StatementEntry`, VO que vive em `Wallet.entries`) varre os aggregates conhecidos e
  concatena o campo `AppendList` daquele tipo ("read não-otimizado do Marco E",
  substituído pelo backend `database/sql` em G — §6).
- **`database/sql` (Marco G):** as mesmas cláusulas viram SQL parametrizado atrás da
  mesma interface `runtime.QueryRunner`. O lowering não muda; só a implementação
  injetada muda (NFR-12). `join` cross-database já foi barrado no front-end
  (REQ-5.10).
- `as View` → `map` do resultado para o struct da View. `visibility` → campos
  omitidos condicionalmente na serialização (`json` + checagem de `caller`).
- **Smart Partial Loading (§20):** o in-memory usa o **fallback** que o próprio
  §20 permite (carrega o aggregate todo e opera em memória) para `focus`/`sum`/
  `skip`/`take`; o backend `database/sql` (G1) pode empurrar essas operações para
  SQL (`WHERE`/`SUM`/paginação) sem mudar o lowering.

### 3.10. Reações: Policy, Worker, Saga (`decl_react.go`) — REQ-23/24

- **Policy:** `func onEventCancelled(ctx, ev EventCancelled) error { … }` registrada
  no `runtime.Dispatcher` para o tipo do evento; `AtLeastOnce` → via outbox;
  `BestEffort` → in-process. Corpo via lowering (REQ-23.5).
- **Worker:** um `runtime.Job` com o schedule (`every`→ticker, `cron`→parser de cron
  simples do runtime, `continuous`→loop consumindo `source`), `concurrency`/
  `batchSize`/`maxRate` como parâmetros do runtime; `onError.retry` → política de
  backoff.
- **Saga:** um `runtime.Saga` com `state` e uma lista de `Step{Up, Down, OnInfraError}`.
  Falha após N steps → executa `Down` em ordem reversa (REQ-24.2); `unrecoverable`
  marca compensação impossível. `async` → devolve `sagaId` + `SagaStatus`; `await` →
  bloqueia com timeout (REQ-24.3).

### 3.11. Infra de módulo e seam de persistência (`infra.go`) — REQ-26/27

O `mod.ds` (`ast.ModuleDecl` + `program.Module`/`Database`) vira **wiring**: o
`cmd/<service>/main.go` instancia as implementações do runtime conforme os blocos.

- **Persistência (REQ-26.1/2):** interface `runtime.Store`. Marco E injeta
  `runtime.NewMemoryStore()`; Marco G gera um `sqlstore` sobre `database/sql`
  (stdlib) e injeta o driver (única dep externa, isolada — NFR-12). `supportsXA`/
  `retry`/`circuitBreaker`/`tenancy` configuram o adapter.
- **Cache/Idempotency/RateLimit/Outbox (REQ-26):** interfaces do runtime, in-memory
  no Marco E, backends reais depois.
- **Canais da topologia (REQ-26.5):** os canais do `program` viram wiring de
  entrega: `direct` → dispatcher in-process; `queue` → fila in-memory do runtime,
  respeitando `orderBy` e `workers{concurrency,maxRate,batchSize}`; `grpc`/`http`
  → cliente da borda com `timeout`/`circuitBreaker`. Provider real (`rabbitmq`) é
  dep externa opt-in atrás do mesmo seam (NFR-12), ausente quando não usado.
- **Tenancy (REQ-27):** `tenant` no `context.Context`, injetado na borda; o `Store`
  aplica o filtro conforme a estratégia; `cross_tenant` gera o caminho sem filtro +
  auditoria (REQ-27.3).

### 3.12. Bordas HTTP e gRPC (`http.go`, `grpc.go`) — REQ-28/29

- **HTTP (REQ-28):** `net/http` (stdlib). Cada `ast.Route` → registro num
  `http.ServeMux` (Go 1.22+ suporta padrões `METHOD /path/{param}`). O handler
  decodifica path/query/body, resolve tenant e idempotência, chama o UseCase/Query e
  mapeia o resultado a status **distinguindo o erro por `errors.As(&BusinessError)`**:
  negócio (`runtime.BusinessError`) → 422; `ErrForbidden` → 403; `ErrNotFound` → 404;
  tenant ausente → 400 (G5); versão após `sunset` → 410 (G6); rate-limit → 429;
  **qualquer outro `error` (infra) → 503**. O `caller` vem de um
  **caller de dev** populado a partir de um header simples (`X-Caller-Id`),
  placeholder até auth real; `caller.id == self.id` loweriza para `caller.ID() ==
  string(self.id)`. A porta HTTP vem do setting `port:` da `Interface` quando
  declarado (ex.: `port: env("HTTP_PORT")`, §10); fallback `8080` quando ausente
  (caso do wallet, cujo `interface.ds` só declara rotas). *(§6.)*
- **gRPC (REQ-29):** gera `.proto` e stubs; **isola** a dep `google.golang.org/grpc`
  num pacote de borda separado, presente só quando há `Interface GRPC`. Exceção
  documentada à NFR-12.

### 3.13. Observabilidade (`observ.go`) — REQ-30

- Padrão: `log/slog` (stdlib) para logs estruturados com trace context; sem dep
  externa (REQ-30.1).
- `Telemetry` (OTel) declarado → adapter atrás de `runtime.Observer`, dep externa
  isolada e opt-in (REQ-30.2).
- `Metric` → atualização de counter/histogram no gatilho `on Evento` (subscriber no
  dispatcher) com labels (REQ-30.3).

### 3.14. Testes gerados (`gentest.go`) — REQ-31

Cada `ast` de `*.test.ds` (`Test`, `scenario`, `given`/`when`/`then`, `mock`,
`fail step`, `property`, `Fixture`) vira um teste Go `func TestX(t *testing.T)`:
monta o `given` (eventos/estado), executa o `when` (Handle/Command/evento), e
verifica o `then` (eventos emitidos / erro / estado / commit-rollback). `mock …
returns …` → injeção de stub de Adapter; `fail step X` → injeção de falha no runtime
de teste; `property` → gerador de sequências + checagem de invariante.

### 3.15. CLI e API (`driver`, `cmd/dsc`) — REQ-32

```go
// driver — API pública
func GenerateProject(dir, out string, opts codegen.Options) (*diag.DiagnosticBag, error)
```

`GenerateProject` chama `CheckProject`; se `bag.HasErrors()`, devolve o bag sem
gerar (REQ-32.2). Senão, constrói o `types.Model`, chama `codegen.Generate` e
escreve os arquivos em `out` de forma idempotente (limpa artefatos órfãos de
declarações removidas — REQ-32.3). A CLI adiciona o subcomando `gen`.

---

## 4. Fluxos de Decisão Chave

### 4.1. Recusa sobre programa inválido (REQ-14.1)

```
GenerateProject(dir, out):
  prog, bag = CheckProject(dir)
  se bag.HasErrors(): imprime bag; return bag, nil      # NÃO gera (exit ≠ 0)
  model = types.NewModel(prog.Symbols)
  files, err = codegen.Generate(prog, model, bag, opts)
  escreve files em out (idempotente)
```

### 4.2. Dispatch de operador de VO (REQ-22.5)

```
lowerBinary(e):
  lt, rt = TypeEnv.Infer(e.Left), TypeEnv.Infer(e.Right)   # §3.6a, não só types.Infer
  se lt é VOType e existe Operator(e.Op) em lt:
    → "left.<MétodoDoOp>(right)"       # pode devolver (T, error) → propaga
  senão se lt/rt são primitivos:
    → "left <opGo> right"              # operador Go nativo
  senão se lt é VOType e e.Op ∈ {==, !=} e NÃO há operador:
    → "left == right" / "left != right"   # VO comparável por construção
  senão:                                # VO com +,-,>=,… sem operador declarado
    → ERRO DE GERAÇÃO                   # não há método; ver caveat Money/wallet (§7)
  # o front-end garantiu compatibilidade de tipos (REQ-13), mas NÃO exige que o
  # operador exista — daí o último ramo poder disparar sobre um programa "válido".
```

### 4.3. Lowering de `match` exaustivo (REQ-22.2, NFR-15)

```
lowerMatch(m):
  t = Infer(m.Subject)
  se t é EnumType e nenhum braço tem guard:
    switch subj { case EnumMembroA: …; case EnumMembroB: … }   # sem default
  se algum braço tem guard (when):
    switch { case guardA: …; default: <braço _> }              # wildcard obrigatório
  # ambos os shapes já foram validados no front-end; aqui é fiel reflexo
```

### 4.4. Seam de dependência externa (NFR-12)

```
recurso           núcleo (Marco E)          opt-in posterior (isolado)
persistência      runtime.MemoryStore       sqlstore(database/sql)+driver   (Marco G)
HTTP              net/http (stdlib)          —
gRPC              —                          grpc-go em pacote de borda       (Marco H)
observabilidade   log/slog (stdlib)          OTel adapter                     (Marco H)
```

O núcleo nunca importa uma dep externa; a wiring escolhe a implementação. Ausência
de `Interface GRPC`/`Telemetry` ⇒ nenhum código com dep externa é gerado.

---

## 5. Estratégia de Testes (NFR-17)

- **Golden tests por emissor:** para cada construto, um `.ds` de entrada e um `.go`
  de referência versionado; o teste gera e compara byte a byte (também guarda o
  determinismo — dois runs, saída idêntica, NFR-13).
- **Smoke compile end-to-end:** gera `docs/examples/wallet` e `docs/examples/shop`
  num diretório temporário e roda `go build ./...` (e `go vet`) sobre o gerado
  (NFR-14). Falha de compilação = falha de teste.
- **Teste de runtime:** `rtsrc/` é copiado, renomeado e compilado isoladamente para
  garantir que o runtime vendorado em si é válido.
- **Teste de comportamento (Marco E+):** os testes gerados de `*.test.ds` (REQ-31)
  rodam sobre o gerado — o próprio Given-When-Then do usuário vira o teste de
  fidelidade semântica (NFR-15).
- **Pareamento (NFR-17):** cada emissor novo entra com seu golden test na mesma task.

---

## 6. Decisões e Trade-offs Registrados

| Decisão | Alternativa | Por quê |
|---|---|---|
| Emissor de strings + `go/format` | `go/ast`+`go/printer`; `text/template` | Legibilidade e controle de imports com validação gofmt, sem verbosidade (NFR-11, REQ-15) |
| Runtime hand-written vendorado | Inline por módulo; framework externo | Gerado enxuto e legível, sem dep externa, sem `go get` (REQ-16, NFR-12) |
| Runtime como `.go.txt` + `//go:embed` | Gerar o runtime por template a cada vez | Runtime é estável e testável isoladamente; embed é stdlib e determinístico (REQ-16.4) |
| In-memory primeiro, `database/sql` depois | Postgres desde o início | Núcleo roda sem dep externa; persistência é seam plugável (NFR-12, escolha do usuário) |
| Um pacote Go por módulo de domínio | Um pacote único | Espelha a fronteira de módulo do domínio; imports claros (NFR-11) |
| Só gera programa válido | Gerar best-effort com erros | Correção por construção; o gerado sempre compila (NFR-14, REQ-14.1) |
| gRPC/OTel isolados e opt-in | Sempre presentes | Manter o núcleo sem dep externa; exceção documentada (NFR-12) |
| Lowering reusa `astutil` | Novo percurso | Extensibilidade e consistência com as fases (NFR-8/16) |
| `context.Context` em toda fronteira desde E2.1 | `ctx` só quando tenancy/idempotência chegarem | Evita reescrever todas as assinaturas do runtime em G/H; ambiente (caller/tenant/trace) nunca é param de domínio (§3.1a) |
| `runtime.Decimal` de escala fixa (`big.Int` + escala 4, half-even) | `*big.Rat`; `float64` | Dinheiro exato sem frações infinitas na divisão; sem dep externa (§3.3) |
| `BusinessError` (`Code`=nome) vs. resto = infra | Hierarquia de tipos de erro | Borda mapeia status por `errors.As`; contrato simples e comparável (§3.5) |
| `PublicEvent` em pacote `contracts/` compartilhado | `PublicEvent` no pacote do módulo declarante | Quebra ciclos de import módulo↔módulo por construção (§3.4) |
| Chave de stream = id do aggregate emissor (metadata no `append`) | 1º campo `id` do payload como chave | Payload é dado de negócio; metadata (seq/ts/tipo) é do store (§3.7) |
| Fixtures de exemplo não são fonte de verdade | Tratar o wallet como domínio canônico | O exemplo exercita a tokenização; o smoke semeia estado via `given` (evento isolado) ou construção completa (fluxo) — §5 |
| `value` = valor embrulhado / receptor; `ok` = "validação passa" | `ok` = booleano embrulhado | Única leitura consistente com construir/comparar `ActiveStatus(true)` e guardar `state.active` falso (§3.3) |
| Caller de dev via header na borda do Marco E | Extrator de auth plugável já no Marco E | Runnable cedo; auth real é marco posterior (§3.12) |
| 2PC quando todos os Databases têm XA; in-memory degenera em commit local | Sempre Saga para cross-db | O front-end **permite** cross-db all-XA (§18.1; `rules_crossfile` só barra "sem XA universal") — REQ-20.5 (§3.8) |
| Canais de topologia atrás de seam (queue in-memory; provider opt-in) | Provider real desde o Marco F | Núcleo sem dep externa; o shop usa `queue` sem provider (NFR-12, REQ-26.5, §3.11) |
| Cobertura = construtos modelados pelo front-end | Perseguir 100% da prosa do spec v6 | TCP/UDP, `tenant.*` em corpos, `provision`, `events()` não têm AST/resolução; entram quando o front-end os modelar (req. §1.3) |

---

## 7. Riscos e Mitigações

| Risco | Mitigação |
|---|---|
| Gerado não compila | Smoke compile obrigatório sobre os exemplos + `format.Source` no fechamento (NFR-14) |
| Saída não-determinística polui diffs | Ordenação estável de tudo; golden test que roda duas vezes (NFR-13) |
| `decimal` do spec sem tipo Go nativo | **Decidido:** `runtime.Decimal` de escala fixa (sobre `math/big.Int`, escala 4, half-even), **nunca** `float64` (§3.3/§6) |
| Inferência do front-end não cobre locais de `load`/`list`/`match`/`lambda` | `TypeEnv` do `codegen` estende a inferência (§3.6a); implementado e testado **antes** do lowering de corpos (task E5.0) |
| Exemplo wallet: `Money` usa `+`/`-`/`>=` **sem** declarar operadores → Go não compila | **Resolvido** em E0.3 (operadores declarados). Fixtures não são fonte de verdade (§6): o smoke semeia estado via `given` ou construção completa, não depende de o exemplo ser canônico |
| `ok`/`value` de VO sem semântica de geração fixada | **Decidido** (§3.3/§6): `value` = valor embrulhado (wrapper) / o receptor (composto); `ok` = sentinela "validação passa" → `NewX` sem erro |
| Colisão de nomes com keywords Go | Estratégia determinística de sufixo em `names.go` (REQ-15.2) |
| Escopo enorme trava a entrega | Marcos E→H; núcleo transacional runnable primeiro (§5 requirements) |
| Dep externa vaza pro núcleo | Seam por interface; ausência do recurso ⇒ nenhum import externo (NFR-12, §4.4) |
| Runtime vendorado diverge e não compila | Teste que compila `rtsrc/` isoladamente (NFR-17) |
| Semântica do domínio distorcida no lowering | Testes gerados de `*.test.ds` rodam sobre o gerado (NFR-15, REQ-31) |
