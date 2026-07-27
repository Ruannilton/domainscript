# Review do transpilador contra a spec v7

> Auditoria da árvore em `main` (commit `b77b8d7`) contra
> `docs/sdd/steerings/domainscript-spec-v7/`, seção por seção. Lista o que está
> **faltando** e o que está **divergente** — não o que já funciona, exceto
> quando o contraste é necessário para localizar a divergência.
>
> **Método.** `dsc` compilado do HEAD e rodado sobre os exemplos **verbatim da
> própria spec**, um arquivo de sondagem por seção, mais leitura do código de
> cada fase. Cada item traz a evidência: a mensagem real do compilador e/ou o
> `arquivo:linha` que documenta o limite. Nada aqui é inferido só da leitura.
>
> **Baseline.** `dsc check` passa limpo nos três projetos-fixture
> (`testdata/projects/{wallet,shop,pizzeria}`); `dsc gen` passa em `wallet` e
> `shop` e falha em `pizzeria` (defeito já registrado). O que segue é a
> distância entre esse baseline e o que a v7 descreve. Os exemplos didáticos
> (`docs/examples/`) são outra coisa: escritos contra a spec, não passam hoje —
> ver a seção G.
>
> **Relação com o que já existe.** `docs/sdd/specs/codegen/gaps.md` mapeia o
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
grafia em que código e spec discordam.

> **A spec é a fonte de verdade.** Toda divergência abaixo se resolve
> **mudando o código**, nunca a spec. Onde a spec se mostrar ruim ou
> incompleta demais para ser implementada como está, o caminho é registrar
> uma issue pedindo a revisão dela — e implementar depois, contra o texto
> revisado. Ver `CLAUDE.md`, "A spec é a fonte de verdade".

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
por construto.

**Correção:** trocar `value` por `self` nos três construtos e migrar os
exemplos empacotados, que hoje usam a grafia da implementação
(`testdata/projects/wallet/domain.ds:8`: `Valid { value.length() > 0 }`). O
receptor `ok` que acompanha `value` em `Valid` é invenção da implementação —
ver F-1.

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

`testdata/projects/wallet/domain.ds:84` contorna declarando `id WalletId` no
`state` e semeando-o num `Apply`.

**Correção:** modelar a identidade como membro implícito do Aggregate. A spec
usa `self.id` de forma consistente (§4.5 e §2.5), mas **nunca diz qual é o
tipo dele** nem como ele se relaciona com o VO de id do domínio
(`WalletId`/`PersonId`) — bloqueio real de implementação, registrado em
`docs/sdd/issues/spec-v7-identidade-implicita-do-aggregate.md`.

### A-3. Metadata implícito de Event: `timestamp`/`sequence`/`aggregateId`/`eventType` (§4.2)

A §4.2 promete quatro campos readonly implícitos em todo Event. Nenhum existe
no modelo de tipos — e o `Apply DepositPerformed` da §4.5 usa `event.timestamp`:

```
60:25: error[E102]: membro inexistente: "timestamp" em DepositPerformed
```

Sem isso, o `StatementEntry` do exemplo canônico não tem como se datar sem um
campo explícito no evento (que é o que o wallet real faz — e por isso o
`StatementEntry` de `testdata/projects/wallet` não tem `date`).

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

`ref` é `token.REF` (hard keyword, para `personId ref Person` da §5.1).
Renomeando a variável, **todo o resto da §2.5 funciona**: `store`,
`load File(...)`, `signed_url(..., expires:)`, `delete file(...)`, roteamento
de campo `FileRef` para uma `FileStorage` do bloco `storage` — tudo
implementado (`codegen/lower/builtins.go`,
`codegen/decl_aggregate_storage.go`). A divergência é só a colisão do nome.

**Correção:** esta é uma **contradição interna da spec**, não uma escolha da
implementação — a §5.1 exige `ref` como keyword e a §2.5 o usa como
identificador; nenhuma gramática satisfaz as duas. Precisa de decisão na spec
antes de qualquer código: `docs/sdd/issues/spec-v7-ref-keyword-vs-identificador.md`.

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

Sem novidade em relação a G-5 / `docs/sdd/issues/visibility-de-view-nao-implementado.md`,
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

### A-8. `Cache`/`RateLimit` `backend:` só aceita string; a §13 escreve sem aspas

A §13 escreve o valor de `backend:` como identificador nu — `Cache { backend:
layered }`, `RateLimit { backend: redis }` —, coerente com o resto do bloco,
que só aspa valores opacos (`provider: "Postgres"`, `backoff: "exponential"`)
e deixa nus os enumerados (`storage: same`, `algorithm: token_bucket`,
`strategy: row_level`). O gerador exige a forma com aspas:

```
dsc: codegen: mod.ds Cache.backend: backend: esperava um literal string, veio *ast.Ident
```

Curiosamente os demais campos enumerados do mesmo bloco aceitam identificador
nu — o defeito é específico de `backend`, em
`codegen/decl_query_cache.go` (~l.295-352) e `codegen/ratelimit.go` (~l.264).

**Correção:** aceitar o identificador nu. É defeito de código puro, sem lacuna
de spec envolvida — issue
`docs/sdd/issues/cache-ratelimit-backend-exige-string-contra-spec.md`.

---

### A-9. `access { requires ... }` num UseCase é erro de sintaxe (§14)

A §14 fecha com o exemplo canônico de acesso cross-tenant, e ele declara
`access` **no UseCase**:

```ds
UseCase GenerateGlobalReport handles GlobalReportCmd {
    tenancy: cross_tenant
    access { requires caller.hasRole("super_admin") }
    execute { ... }
}
```

O parser não modela `access` em UseCase — só em Aggregate. Rodando o exemplo
verbatim:

```
5:5:  error: membro de UseCase inesperado: IDENT
5:29: error: membro de UseCase inesperado: .
6:5:  error: esperava uma declaração de topo, encontrei IDENT
```

`tenancy: cross_tenant` sozinho passa; é o bloco `access` que quebra. Sem ele,
a exigência de "role privilegiada" que a §14 impõe ao opt-in cross-tenant não é
expressável — a regra de compilação existe (`checkCrossTenantOptIn`) mas a
sintaxe que a satisfaria, não.

**Correção:** aceitar `access` em `UseCaseDecl`. Anotar que a spec mostra a
forma **uma única vez**, de passagem, e a §5.2 (UseCases) não a menciona —
vale confirmar o alcance ao implementar (só `requires`? mesma gramática de
condição do Aggregate?).

---

## 🟣 F. Implementado **fora** da spec

A regra "nada deve ser implementado fora da especificação" corta nos dois
sentidos: além do que falta, conta o que existe sem autorização do texto. O
que a auditoria encontrou:

### F-1. Sentinela `ok` em `Valid`

`resolver/receivers.go:36` semeia `ok` junto de `value` em todo bloco `Valid`,
e `codegen/decl_value.go:59` o reconhece como "validação que sempre passa". A
string `ok` **não aparece uma única vez em toda a v7**. A spec já tem a forma
canônica para isso — `Valid { true }` (§2.2, `ValueObject ActiveStatus`).

**Estado:** os 8 usos nos exemplos empacotados já foram migrados para
`Valid { true }` (auditoria dos exemplos, seção G) — nenhum `.ds` do
repositório depende mais do sentinela. Falta só remover `ok` do
`resolver/receivers.go` e do `codegen/decl_value.go`, o que agora é deleção,
não migração.

### F-2. Receptor `value` (é o outro lado de A-1)

Não é só que falta `self`: `value` é um nome que a spec nunca define. Vale
registrar separado porque a correção de A-1 tem de **remover** `value`, não
apenas adicionar `self` como sinônimo — sob esta regra, manter os dois seria
deixar metade da superfície fora da spec.

### F-3. `asc`/`desc` como direções de `orderBy`

`parser/parse_query.go:20` aceita `ascending`, `descending`, `asc` e `desc`. A
§6.3 usa só `descending`. As formas abreviadas são extensão da implementação.

### F-4. Convenção de seed de Saga por casamento de nomes

`sagaSeedFromCommandLines` (`codegen/decl_saga.go:165`) copia campos de mesmo
nome do Command para o `state` antes do 1º passo. É uma semântica inventada
pelo back-end para compensar a ausência do receptor `cmd` (A-4) — nenhuma
linha da §19 a descreve. Some junto com a correção de A-4.

### F-5. Notification nua como `notify` (é o outro lado de A-5)

Reconhecer `DepositNotification(to: ...)` solto como envio assíncrono é forma
não especificada. A §9.2 define `notify`. Como em F-2, a correção de A-5 deve
**substituir**, não acumular.

---

## 📗 G. Exemplos e fixtures — duas árvores, dois papéis

> **Esta seção descreve o estado pós-merge da PR de exemplos**
> (`claude/exemplos-spec-first-1gcypy`), que é onde a separação é feita de
> fato. Se `testdata/projects/` ainda não existe na sua árvore, é porque
> aquela PR não entrou — os caminhos citados aqui são o destino, não o
> presente.

A auditoria dos exemplos levou a uma separação estrutural, porque os dois usos
eram incompatíveis: material didático precisa seguir a spec, fixture de teste
precisa seguir a implementação, e uma árvore só não faz as duas coisas.

| Árvore | Escrita contra | Compila hoje? | Quem valida |
|---|---|---|---|
| `docs/examples/` | a **especificação** | **não, de propósito** | ninguém — é a meta de conformidade |
| `testdata/projects/` | o que o **transpilador aceita** | sim (menos `pizzeria`) | job `fixtures` do CI + 10 testes que as leem |

**`docs/examples/`** foi reescrito do zero: um exemplo por área da spec,
`01-tipos-e-fluxo/` a `09-testes/`, cada um com README apontando as seções que
demonstra e as regras da §25 que exercita. Usa livremente tudo o que a v7
descreve e a implementação ainda não tem — `self` em corpos de VO, `notify`,
`Foreign` com `pure`/`impure`, identidade implícita do Aggregate,
`event.timestamp`, `tenant.*`, `provision tenant()`, `RetryWithBackoff`. Cada
uma dessas formas é, ao mesmo tempo, uma demonstração e um item desta
auditoria.

**`testdata/projects/`** são os antigos `wallet`/`shop`/`pizzeria`, movidos sem
alteração de conteúdo. Conformidade **não se aplica** a eles: existem para
pinar comportamento, então formas fora da spec (o sentinela `ok`, o receptor
`value`) são legítimas ali enquanto a implementação as tiver. Alterá-los muda
o Go gerado e quebra o golden correspondente — o que é o alarme desejado.

Consequência prática para esta auditoria: **os itens A-1 a A-8 e F-1 a F-5
deixaram de ter "exemplo bloqueado" como custo**. Antes, corrigir `value` →
`self` exigia migrar os exemplos junto; agora o exemplo já está na forma certa,
esperando a implementação alcançar. O que sobra é só o trabalho de código.

## E. O que fazer com isso

Sob a regra "a spec é a fonte de verdade", todo item acima é **trabalho de
conformidade**: o código se move até o texto. A ordenação abaixo é por
dependência e custo/benefício, não por severidade nominal.

**Primeiro, o que está bloqueado por lacuna da própria spec.** Cinco itens não
podem ser implementados como estão porque o texto não diz o suficiente ou se
contradiz. Cada um já tem issue registrada pedindo a revisão — nenhuma linha de
código antes de a spec ser atualizada:

| Bloqueio | Issue |
|---|---|
| `ref` é keyword na §5.1 e identificador na §2.5 (A-6) | `spec-v7-ref-keyword-vs-identificador.md` |
| `self.id` usado sem declaração nem tipo (A-2) | `spec-v7-identidade-implicita-do-aggregate.md` |
| Metadata implícito de Event sem tipos nem isenção da Regra de Ouro (A-3) | `spec-v7-metadata-implicito-de-event.md` |
| Sem catálogo normativo de métodos por tipo; `length` property vs. method (A-7) | `spec-v7-catalogo-de-metodos-embutidos.md` |
| `RetryWithBackoff(3)` usado na §19.2 e definido em lugar nenhum | `spec-v7-retrywithbackoff-sem-definicao.md` |

**Depois, o que já é implementável contra o texto atual**, em ordem:

1. **A-1 + F-1 + F-2 (`self` em corpos de VO).** A menor mudança de todas —
   uma tabela em `resolver/receivers.go` — mas ela **substitui** `value`/`ok`,
   então arrasta a migração dos três exemplos empacotados e dos goldens. Não
   dá para fazer antes do catálogo de métodos (o bloqueio acima), senão só
   troca `nome não declarado` por `método embutido desconhecido`.

2. **A-5 + F-5 (`notify`).** Uma keyword no léxico, um caso no parser, e a
   remoção do reconhecimento da forma nua no lowering. Autocontido.

3. **A-4 + F-4 (`cmd` nos steps de Saga).** Um receptor a mais e a remoção de
   `sagaSeedFromCommandLines`. Autocontido, e apaga uma semântica inventada.

4. **§10 (FFI geral) é um ciclo de spec próprio.** Atravessa todas as fases
   (token → parser → ast → resolver → sema → types → codegen) e traz junto 5 das
   6 regras faltantes da §25. O menor passo útil e independente: **registrar
   `ForeignDecl.Functions` como símbolos no resolver** — sem isso nem a forma
   que o parser já aceita é usável, e é pré-requisito de tudo o mais.

5. **§21 (deploy) é o outro ciclo próprio**, e o mais autocontido de todos: não
   toca o front-end, consome só `program.Program` (topologia + `mod.ds` +
   `interface.ds`), e o `dsc gen` já produz `cmd/<service>/main.go`. É de longe
   a maior superfície de spec fechável sem mexer em nenhuma fase existente.

6. **§14 `tenant.*` (B-3)** é pequeno e desbloqueia a §17 por tier lida do
   domínio: um receptor a mais em `resolver/receivers.go` e um shape com
   `id`/`tier`/`exists` no `types.Model`. `provision tenant()` e as estratégias
   `schema`/`database_per_tenant` são bem maiores e podem esperar.

7. **`visibility` (B-4) já é issue aberta** e continua sendo a única lacuna que
   falha em silêncio com consequência de segurança. O paliativo antes cogitado
   (warning de geração "visibility declarado e ignorado") **não é conforme** —
   é diagnóstico fora da §25. O caminho é implementar a §6.2 de fato.

8. **A-8 (`backend:` sem aspas)** é defeito de código isolado, sem lacuna de
   spec: dois leitores de config passando a aceitar `*ast.Ident`. Fecha a
   última divergência dos exemplos que não depende de outra coisa.

9. **F-3 (`asc`/`desc`)** é limpeza de uma linha, oportunista.

Os itens de C já têm registro próprio (`gaps.md` G-4/G-6/G-7, issues abertas) e
não precisam de entrada nova — a v7 não mudou nada neles.
