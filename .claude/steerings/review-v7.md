# Review do transpilador contra a spec v7

> Auditoria da árvore em `main` (commit `b77b8d7`) contra
> `.claude/steerings/domainscript-spec-v7/`, seção por seção. Lista o que está
> **faltando** e o que está **divergente** — não o que já funciona, exceto
> quando o contraste é necessário para localizar a divergência.
>
> **Método.** `dsc` compilado do HEAD e rodado sobre os exemplos **verbatim da
> própria spec**, um arquivo de sondagem por seção, mais leitura do código de
> cada fase. Cada item traz a evidência: a mensagem real do compilador e/ou o
> `arquivo:linha` que documenta o limite. Nada aqui é inferido só da leitura.
>
> **Baseline.** `dsc check` passa limpo nos três exemplos empacotados
> (`wallet`, `shop`, `pizzeria`); `dsc gen` passa em `wallet` e `shop` e falha
> em `pizzeria` (defeito já registrado). O que segue é a distância entre esse
> baseline e o que a v7 descreve.
>
> **Relação com o que já existe.** `.claude/specs/codegen/gaps.md` mapeia o
> transpilador contra a **v6** e continua válido no que cobre; os itens
> repetidos aqui aparecem com a referência cruzada (`G-n`) para não duplicar
> registro. O que este documento acrescenta é o **delta v7**: a §10 (FFI
> geral), a §21 (deploy) e um conjunto de divergências de *forma* — casos em
> que o exemplo canônico da spec não compila porque a implementação escolheu
> outra grafia.

---

## 1. Resumo por seção

| § | Seção | Estado |
|---|-------|--------|
| 1 | Visão geral | — (sem superfície verificável) |
| 2 | Sistema de tipos | 🔴 **divergente** — `self` em VO, catálogo de métodos, coleções |
| 3 | Controle de fluxo | 🟢 completo (`ensure`/`match`/`for`/`log`, guards, `break all`) |
| 4 | Núcleo do domínio | 🔴 **divergente** — `self.id` e metadata implícito de Event |
| 5 | Camada de aplicação | 🟢 completo |
| 6 | Read Side | 🟠 `visibility` parseado e ignorado (G-5) |
| 7 | Policies | 🟢 completo |
| 8 | Workers | 🟢 completo (`every`/`cron`/`continuous`) |
| 9 | Notifications & Adapters | 🔴 **divergente** — a keyword `notify` não existe |
| 10 | **FFI geral (`Foreign`)** | 🔴 **ausente de ponta a ponta** |
| 11 | Interface | 🟠 só HTTP/gRPC — TCP/UDP ausentes |
| 12 | Topologia | 🟠 só `direct`/`queue` — `grpc`/`http`/`stream` são erro de geração |
| 13 | Infra de módulo | 🟠 parseia tudo; providers reais são um recorte (G-4) |
| 14 | Multi-tenancy | 🔴 `tenant.*` e `provision tenant()` inexistentes; só `row_level` |
| 15 | Idempotência | 🟠 só `storage: same`, in-memory |
| 16 | Cache | 🟠 `memory`/`redis` — `layered`/`distributed` ausentes |
| 17 | Rate limiting | 🟢 completo (dimensões, algoritmos, tiers) |
| 18 | Versionamento de API | 🟢 completo (`upcast`/`downcast`/`deprecated`/`sunset`/`route`) |
| 19 | Transações e Sagas | 🔴 **divergente** — `cmd` invisível nos steps; `RetryWithBackoff` inexistente |
| 20 | Observabilidade | 🟠 traces reais; métricas e logs não exportados (G-6) |
| 21 | **Deploy** | 🔴 **ausente por inteiro** |
| 22 | Smart partial loading | 🟢 completo (`focus`/`sum`/`distinct`, paginação) |
| 23 | Classificação de erros | 🟢 completo |
| 24 | Testing nativo | 🟠 gramática completa; geração com lacunas (G-7) |
| 25 | Regras de compilação | 🟠 24 de 30 regras — as 5 de FFI e a de deploy faltam |
| 26 | Glossário | — |
| 27 | Em evolução | — (a própria spec marca como pendente) |

---

## 🔴 A. Divergências que quebram exemplos da própria spec

Estas são as mais caras de ignorar: quem lê a spec e escreve o exemplo
literalmente recebe um erro. Não são "features faltando" — são decisões de
grafia em que código e spec discordam, e **uma das duas precisa ceder**.

### A-1. `self` não existe em corpos de ValueObject (§2.2, §2.3)

A spec usa `self` em `Valid`, em `Operator` e no bloco `coerce`. A
implementação semeia `value` (e `ok` em `Valid`) — `self` só existe em
`Handle` e em `access`.

```
$ dsc check probes/vo.ds        # §2.2 copiado verbatim
5:13:  error[E100]: nome não declarado: "self"     # Valid { self.contains("@") }
9:13:  error[E100]: nome não declarado: "self"     # Valid { self.length >= 2 ... }
19:16: error[E100]: nome não declarado: "self"     # Operator +: self.currency
20:30: error[E100]: nome não declarado: "self"
```

**Onde mora:** `resolver/receivers.go:32` (`contextualReceiverNames`) —
`constructValid: {"value","ok"}`, `constructOperator: {"value"}`,
`constructCoerce: {"value"}`. O arquivo se declara "o único ponto a editar
quando um construto novo ganha um receptor", então a correção é de uma linha
por construto (aceitar `self` como sinônimo, ou trocar).

Os exemplos empacotados usam a grafia da implementação
(`docs/examples/wallet/domain.ds:8`: `Valid { value.length() > 0 }`), então a
divergência é puramente spec↔código.

### A-2. Identidade implícita do Aggregate: `self.id` (§4.5)

O `Aggregate Wallet` da §4.5 **não declara `id` no `state`** e mesmo assim usa
`self.id` nos dois `Handle`. A implementação exige que `id` seja um campo de
`state` como outro qualquer — `Model.Members` de um Aggregate são exatamente
os campos do `state` (`types/model.go:166`).

```
$ dsc check p3/          # Aggregate Wallet da §4.5, verbatim
39:33: error[E102]: membro inexistente: "id" em Wallet
51:36: error[E102]: membro inexistente: "id" em Wallet
```

`docs/examples/wallet/domain.ds:84` contorna declarando `id WalletId` no
`state` e semeando-o num `Apply`. Fechar exige modelar a identidade do
Aggregate como membro implícito (e decidir seu tipo: hoje seria o VO do campo
declarado).

### A-3. Metadata implícito de Event: `timestamp`/`sequence`/`aggregateId`/`eventType` (§4.2)

A §4.2 promete quatro campos readonly implícitos em todo Event. Nenhum existe
no modelo de tipos — e o `Apply DepositPerformed` da §4.5 usa `event.timestamp`:

```
60:25: error[E102]: membro inexistente: "timestamp" em DepositPerformed
```

Sem isso, o `StatementEntry` do exemplo canônico não tem como se datar sem um
campo explícito no evento (que é o que o wallet real faz — e por isso o
`StatementEntry` de `docs/examples/wallet` não tem `date`).

### A-4. `cmd` é invisível dentro dos steps de Saga (§19.2)

A `Saga PurchaseTickets` da §19.2 usa `cmd.eventId`, `cmd.quantity`,
`cmd.userId` e `cmd.paymentMethod` dentro de `step ReserveTickets { up { ... } }`.
`constructSagaStep` semeia **só** `state`:

```
16:46: error[E100]: nome não declarado: "cmd"
```

O codegen já contorna com uma convenção própria: `sagaSeedFromCommandLines`
(`codegen/decl_saga.go:165`) copia campos **de mesmo nome** do Command para o
`state` antes do 1º passo. O próprio cabeçalho do arquivo
(`codegen/decl_saga.go:26-40`) documenta que essa ponte existe porque não há
receptor `cmd`. Ou seja: a spec assume `cmd`, o front-end não o oferece, e o
back-end inventou um substituto por casamento de nomes — três semânticas
diferentes para a mesma coisa.

### A-5. A keyword `notify` não existe na gramática (§9.2)

```
20:9: error[E100]: nome não declarado: "notify"
```

`call` é uma operação de domínio de verdade (`parser/parse_query.go:11`);
`notify` não. A implementação reconhece **a construção nua** de uma
Notification como statement (`DepositNotification(to: ..., amount: ...)`) e
deriva "async" daí — decisão registrada em `codegen/decl_io_test.go:35-43`
("nem precisa existir um par de keywords `notify`/`call` na gramática").

O efeito prático é o pior dos dois mundos: escrever a forma da spec não dá
erro de sintaxe, dá um `nome não declarado: "notify"` seguido de um statement
que *funciona* — a mensagem não aponta para a causa.

### A-6. `ref` é keyword reservada e colide com o exemplo da §2.5

O exemplo de `File`/`FileRef` da §2.5 usa `ref` como nome de parâmetro e de
variável local:

```ds
Handle AttachDocument(ref FileRef) { emit DocumentAttached(self.id, ref) }
...
ref = store cmd.document
```

```
14:27: error: esperava um identificador, encontrei ref
15:40: error: esperava uma expressão, encontrei ref
30:9:  error: esperava uma expressão, encontrei ref
```

`ref` é `token.REF` (hard keyword, para `personId ref Person`). Renomeando a
variável, **todo o resto da §2.5 funciona**: `store`, `load File(...)`,
`signed_url(..., expires:)`, `delete file(...)`, roteamento de campo `FileRef`
para uma `FileStorage` do bloco `storage` — tudo implementado
(`codegen/lower/builtins.go`, `codegen/decl_aggregate_storage.go`). A
divergência é só a colisão do nome.

### A-7. Catálogo de métodos embutidos: 3 entradas (§2.2, §2.4)

`codegen/goname/types.go:89` é o catálogo inteiro:

```go
{Receiver: "string", Method: "length"}:    0,
{Receiver: "AppendList", Method: "add"}:   1,
{Receiver: "string", Method: "uppercase"}: 0,
```

Consequências diretas:

- `Valid { self.contains("@") }` da §2.2 — `contains` não existe.
- A tabela inteira da §2.4 (`List.add/remove/clear`/indexação,
  `Set.add/remove/contains/clear`, `Map.put/get/remove/containsKey/keys/values`)
  não é emitível. Só `AppendList.add` existe.

**E o front-end não valida chamada de método nenhuma.** `value.frobnicate()`
passa no `check` sem uma palavra:

```
$ dsc check p7/          # ValueObject A(string) { Valid { value.frobnicate() > 0 } }
$ echo $?
0
$ dsc gen p8 -o out
dsc: codegen: ValueObject A: método embutido desconhecido em corpo de VO: string.contains
```

O erro é claro, mas chega só na geração — viola o contrato "se compila, a
arquitetura está correta" (§1.1) e o anti-cascata do front-end. Mesmo padrão
vale para `events()` (§4.5): `w.events(from: 100, to: 200)` passa no `check` e
morre no `gen` com `CallExpr com Fn *ast.MemberExpr não suportado`.

---

## 🔴 B. Seções ausentes por inteiro

### B-1. §10 — FFI geral (`Foreign`): nada existe

É a maior lacuna do delta v7. A seção inteira — 7 subseções, uma tabela de
marshalling, uma tabela de contexto e **5 das 31 regras da §25** — não tem
nenhuma implementação. Detalhe por camada:

| Camada | Estado |
|---|---|
| Léxico/sintaxe | `pure`/`impure`/`throws` são erro de sintaxe |
| AST | `ast.ForeignFunc` (`ast/decl.go:342`) não tem natureza nem `throws` |
| Resolver | `ForeignDecl` **não registra símbolo** (`resolver/resolver.go:132`) |
| Sema | só aridade (`sema/rules_test_files.go:104`), zero regras de contexto |
| Codegen | `EmitForeign` existe (`codegen/decl_io.go:471`) mas é inalcançável a partir do domínio |

```
$ dsc check probes/ffi.ds        # §10.2 verbatim
2:5: error: esperava 'function' em Foreign, encontrei IDENT     # pure function ...
4:58: error: esperava 'function' em Foreign, encontrei IDENT    # ... throws InvalidTaxIdError
8:5: error: esperava 'function' em Foreign, encontrei IDENT     # impure function ...
```

E mesmo o subconjunto **sem** `pure`/`impure` (a forma que o parser aceita) é
inutilizável: como o resolver não registra a função, chamá-la de qualquer
corpo dá `nome não declarado`.

```
$ dsc check p9/          # Foreign { function ValidateTaxId(taxId string) -> boolean }
1:37: error[E100]: nome não declarado: "ValidateTaxId"     # dentro de um Valid
8:26: error[E100]: nome não declarado: "ValidateTaxId"     # dentro de um Handle
```

Isso torna `checkForeignSignatures` (a única regra de FFI existente)
inalcançável na prática: nenhum programa que chame uma função foreign chega a
validar limpo. E a checagem é **só de aridade** — a §25 pede "assinatura
incompatível", que inclui tipos.

Faltam as 5 regras da §25 ligadas a FFI:

- Aggregate cruzando a fronteira FFI → erro (§10.3)
- FFI em `Apply`, pura ou impura → erro (§10.4, "Apply é hermético")
- FFI impura em `Handle` sem captura em evento → erro (§10.4)
- FFI impura em `Query`/`ValueObject` → erro (§10.4)
- FFI impura dentro de transação → warning (§10.7)

E a §10.6 (testing de FFI: `mock sign_via_hsm returns ...`): a cláusula `mock`
existe na gramática de teste (`ast.MockClause`), mas não há FFI para mockar.

**Nota:** o FFI **vinculado a Notification** (Adapter Nível 2, §9.3) funciona —
`foreign`/`from`/`function`/`map` no `AdapterDecl`, com marshalling em
`codegen/decl_io.go:395`. A §10 é justamente o mecanismo *desacoplado* de
Notifications, e é ele que não existe. Parte disso já estava registrado como
"features nunca modeladas pelo front-end" (G-3), mas a §10 v7 é bem maior do
que o item de uma linha que existia lá.

### B-2. §21 — Geração de artefatos de deploy: nada existe

Nenhum Dockerfile, nenhum `docker-compose.yml`, nenhum `.env.example`, nenhuma
migration SQL, nenhum config de OTEL Collector. Busca por qualquer um desses
termos em `codegen/`, `driver/` e `cmd/` não retorna nada fora de comentários.

O CLI tem `check` e `gen` (`cmd/dsc/main.go:27`); a §21.5 especifica
`ds build --target=docker-compose --profile=dev`. Não há flag de `--target`
nem de `--profile`, e `codegen.Options` (`codegen/codegen.go:27`) só tem
`ModulePath` e `GoVersion`.

Nada da §21.1 (inferência de containers a partir de topologia + `mod.ds`),
§21.3 (compose com healthchecks e `depends_on`) ou §21.4 (dedup por connection
string, perfis dev/prod, `.env.example` derivado dos `env(...)`) tem
contrapartida. A estrutura de saída da §21.5 hoje sai só como `cmd/<service>/main.go`
+ `go.mod` — o resto da árvore não é gerado.

Daí sai também a regra faltante da §25: **"Provider cloud sem equivalente
local (profile dev) → warning"**, que não tem onde existir sem perfis.

### B-3. §14 — Tenant como ambient context

Duas peças centrais da seção não existem no front-end:

```
4:54: error[E100]: nome não declarado: "tenant"      # log info "t" { t = tenant.id }
3:13: error[E100]: nome não declarado: "provision"   # provision tenant(event.id)
3:23: error[E100]: nome não declarado: "tenant"
```

- `tenant.id` / `tenant.tier` / `tenant.exists` — não há receptor `tenant` em
  `resolver/receivers.go:32`. O domínio **não consegue ler o tenant**, embora
  o filtro automático e o 404 cross-tenant funcionem por baixo.
- `provision tenant(id)` (§14) — não é operação de domínio nem built-in.

Das três estratégias de isolamento, só `row_level` gera:

```
codegen/codegen.go:251: tenancy.strategy %q não é suportada por este gerador
  (G5 implementa só "row_level" — "schema_per_tenant"/"database_per_tenant"
   exigem provisionamento por tenant, "provision tenant(id)" §13.4 ...)
```

O que **funciona**: resolução na borda (`subdomain`/`header`/`jwt_claim`/`path`),
filtro automático, 404 cross-tenant, fail-closed 400, `tenancy: none` por rota,
`tenancy: cross_tenant` com opt-in + warning de auditoria, propagação em canais.
(Já registrado parcialmente em G-3.)

### B-4. §6.2 — `visibility` de View: aceito e ignorado

Sem novidade em relação a G-5 / `.claude/issues/visibility-de-view-nao-implementado.md`,
mas vale repetir porque é a única lacuna com consequência de **segurança que
falha em silêncio**: o bloco é parseado (`ast.ViewDecl.Visibility`) e nenhum
arquivo de `codegen/` lê o campo. Campos declarados como restritos são
serializados para qualquer caller. Nem erro, nem warning.

---

## 🟠 C. Implementado com semântica reduzida

Itens já cobertos por `gaps.md`/issues; listados aqui só para completar o mapa
da v7. Nenhum deles é silencioso — todos dão erro de geração claro.

| § | Promessa | Estado |
|---|---|---|
| 11 | HTTP, gRPC, **TCP, UDP** | só HTTP e gRPC (`.proto` gerado) |
| 12 | `direct`/`queue`/`grpc`/`http`/`stream` | `direct` (in-memory) e `queue` (`rabbitmq` real); os outros três → `unsupportedChannelKindError` (`codegen/channel.go:70`) |
| 13/15 | Idempotency `storage: same` \| `external` | só `same`, in-memory (`codegen/rtsrc/idempotency.go.txt`) |
| 13/16 | Cache `memory`/`distributed`/`layered` | in-memory + `redis` real; `layered`/`distributed` não |
| 13 | Database `"Postgres"`, Mongo, … | `sqlite` e `postgres` reais; outros rótulos são decorativos |
| 13 | FileStorage `s3` | `s3` real; GCS/Azure não |
| 20 | OTel para traces, métricas **e logs** | só traces exportados; `Metric` num registry in-memory, logs em `slog` (G-6) |
| 6.3 | `join` com `orderBy`/`skip`/`take` | cláusula pós-join → erro de geração (`codegen/lower/join.go:214`) |
| 24 | Testing (7 formas) | gramática completa; geração com 6 lacunas conhecidas (G-7) |

---

## 🟡 D. §25 — as regras de compilação, uma a uma

A tabela tem 30 linhas (20 erros + 10 warnings); **24 estão implementadas**. O
mapeamento (via `sema/checker.go:48-80`):

**Erros — implementados (16):** primitivo no Write Side
(`checkWriteSidePrimitives`), Handle sem `access` (`checkAggregateAccess`),
Notification sem Adapter (`checkNotificationAdapters`), mutação de
`AppendList` (`checkAppendListMutation`), cross-database sem XA / cross-service
sem Saga (`checkTransactions`), JOIN cross-database (`checkCrossDatabaseJoin`),
`match` não-exaustivo e guard sem `_` (`checkMatchExhaustiveness`), `Nop` em
Handle/UseCase (`checkNop`), `break`/`continue` fora de `for`
(`checkLoopControlDecl`), Policy cross-module sobre Event privado
(`checkPolicyPublicEvent`), módulos em services distintos sem canal
(`checkServiceChannels`), cross-tenant sem opt-in (`checkCrossTenantOptIn`),
upcast de API sem default (`checkVersionUpcastDefaults`), teste referenciando
evento/comando inexistente (`checkTestFile`), FFI/Adapter com assinatura
incompatível (`checkForeignSignatures`, **só aridade**), conflito de
idempotência (runtime, 422).

**Warnings — implementados (8):** canal `queue`/`stream` sem `orderBy`,
Saga `await` sobre `queue`, Upcast substituível por default, VO que poderia ser
Enum, cache de alta cardinalidade, UseCase cross-tenant declarado, Handle sem
cenário de erro testado, UseCase/Query não exposto em interface.

**Faltando (6):**

| Regra | Bloqueada por |
|---|---|
| Aggregate cruzando fronteira FFI → ❌ | B-1 |
| FFI em `Apply` (pure ou impure) → ❌ | B-1 |
| FFI impura em Handle sem captura em evento → ❌ | B-1 |
| FFI impura em Query/ValueObject → ❌ | B-1 |
| FFI impura dentro de transação → ⚠️ | B-1 |
| Provider cloud sem equivalente local (profile dev) → ⚠️ | B-2 |

Ou seja: **as 6 regras faltantes são exatamente as duas seções ausentes.** As
demais 24 estão cobertas, cada uma com par de teste positivo/negativo
(`sema/rules_*_test.go`).

---

## E. O que fazer com isso

Ordenado por custo/benefício, não por severidade nominal.

1. **Decidir a grafia canônica das divergências A-1 a A-6 antes de qualquer
   implementação.** São baratas de corrigir (A-1 é uma tabela de uma linha por
   construto; A-6 é uma keyword) e caras de deixar: cada uma faz o exemplo
   literal da spec falhar, que é o pior sinal possível numa DSL cujo argumento
   de venda é "se compila, está correto". Para cada uma, a escolha é *emendar a
   spec* (adotar `value`, exigir `id` no `state`) ou *emendar o código*
   (aceitar `self`, modelar identidade e metadata implícitos). Recomendo emendar
   o **código** em A-1/A-3/A-4/A-5 (a spec está mais coerente consigo mesma) e a
   **spec** em A-2/A-6 (identidade explícita no `state` é defensável; `ref`
   keyword é irreversível sem custo alto).

2. **A-7 antes de A-1.** Adotar `self` sem catálogo de métodos só troca o erro
   de `nome não declarado` por `método embutido desconhecido`. Vale um ciclo
   próprio: catálogo de métodos de `string`/`List`/`Set`/`Map` no `types.Model`
   (para o front-end recusar `frobnicate` no `check`) **e** em
   `goname.builtinArity`/`GoBuiltinCall` (para o back-end emitir). Fecha §2.4
   inteira e tira o `check` verde seguido de `gen` vermelho.

3. **§10 (FFI geral) é um ciclo de spec próprio.** Atravessa todas as fases
   (token → parser → ast → resolver → sema → types → codegen) e traz junto 5 das
   6 regras faltantes da §25. O menor passo útil e independente: **registrar
   `ForeignDecl.Functions` como símbolos no resolver** — sem isso nem a forma
   que o parser já aceita é usável, e é pré-requisito de tudo o mais.

4. **§21 (deploy) é o outro ciclo próprio**, e o mais autocontido de todos: não
   toca o front-end, consome só `program.Program` (topologia + `mod.ds` +
   `interface.ds`), e o `dsc gen` já produz `cmd/<service>/main.go`. É de longe
   a maior superfície de spec fechável sem mexer em nenhuma fase existente.

5. **`visibility` (B-4) já é issue aberta** e continua sendo a única lacuna que
   falha em silêncio com consequência de segurança. Enquanto não fechar, o
   mínimo defensável segue sendo um warning de geração — como já anotado em
   G-5.

6. **§14 `tenant.*` (B-3)** é pequeno e desbloqueia a §17 por tier lida do
   domínio: um receptor a mais em `resolver/receivers.go` e um shape com
   `id`/`tier`/`exists` no `types.Model`. `provision tenant()` e as estratégias
   `schema`/`database_per_tenant` são bem maiores e podem esperar.

Os itens de C já têm registro próprio (`gaps.md` G-4/G-6/G-7, issues abertas) e
não precisam de entrada nova — a v7 não mudou nada neles.
