# Gaps — Divergências entre o Implementado e o Spec v6

> Documento complementar ao ciclo `requirements.md` → `design.md` → `tasks.md`
> (REQ-14..32, Marcos E–H, **completo**). Registra, ordenado por criticidade, o
> que o spec da linguagem (`.claude/steerings/domainscript-spec-v6.md`) promete e o
> transpilador ainda não entrega — auditoria feita após o fechamento de H5,
> cruzando o spec com o código real e as notas de escopo de `tasks.md`.
>
> **Como ler:** cada item aponta a seção do spec, o estado real (com o arquivo
> que o documenta ou onde a lacuna mora) e o que fechá-lo exigiria. Nada aqui é
> "esquecimento": todo item já está registrado como decisão de escopo em algum
> lugar do repositório — este documento só centraliza e prioriza. Um ciclo novo
> de trabalho deve nascer como um novo `requirements.md` (continuando a série a
> partir de REQ-33/NFR-18), não como edição deste arquivo.

---

## 🔴 Crítico — exemplos do próprio spec não gerariam código

### G-1. Read Side incompleto: cláusulas SQL-like em Query (spec §6.3)

**✅ Fechado pelo ciclo `.claude/specs/read-side/` (REQ-33..35/38/40, Marco I).**
`where`/`orderBy`/`skip`/`take`/`as` sobre `list`/`load`-coleção, `join`
mesmo-banco e o operador `in` geram Go real sobre `runtime.Query[T]`
(in-memory) com descida para SQL parametrizado no adapter sqlite pelo seam
`Dialect` (REQ-40). As Queries `GetStatement` e `GetMyTickets` do spec §6.3
agora geram, compilam e passam teste comportamental (`.claude/specs/
read-side/requirements.md` §1.4, itens 1–2). Desvio remanescente:
`orderBy`/`skip`/`take` pós-join ficaram de fora (erro de geração claro em
vez de adivinhar a semântica) — ver `read-side/tasks.md` I5.1. `join`
traduzido para SQL real (hoje materializa in-memory mesmo no backend SQL) e
`in` com subquery seguem fora de escopo (`read-side/design.md` §5).

**Promessa:** `load`/`list` com `where`/`orderBy`/`skip`/`take`/`join`/`in`,
`join` cross-database barrado (exige `Projection`).

**Estado real:** o front-end parseia todas as cláusulas (`ast.QueryClause`),
mas o codegen só gera as duas formas mínimas exercitadas pelo wallet:
`load X(id) as V` e `list <VO>` sem cláusula nenhuma. Documentado em
`codegen/decl_query.go` (cabeçalho: "Cláusulas SQL-like NÃO aparecem em
nenhuma das duas Queries reais — não implementadas por esta task"); `list
<VO>` com cláusula é **erro de geração** explícito. `join` e o operador `in`
não têm lowering algum (nenhuma referência em `codegen/lower/`). As Queries
`GetStatement` e `GetMyTickets` do próprio spec §6.3 não gerariam código.

**Exceção parcial:** `list`/`count` com `where` FUNCIONAM via
`runtime.Collection[T]` + predicado por item (fatia H4 de Policy/Query,
`lower/stmt.go:hoistQueryPredicate`) — mas só nesse par, sem ordenação/
paginação/join, e a limitação de hoisting no predicado (G-8) se aplica.

**Fechar exige:** desenhar a API de query real sobre o runtime (in-memory
primeiro, descendo a SQL no adapter G1 depois — `design.md` §3.9 e §4.4 já
apontam a direção), lowering de cada cláusula, e goldens/smoke sobre as
Queries do spec. É o "Marco E8 de verdade" — a dívida mais citada dentro do
próprio codebase (`lower/builtins.go`, `decl_query.go`, fatia H4 §22.4 em
`tasks.md`).

### G-2. Smart Partial Loading: `distinct`/`sum`/`focus` (spec §20)

**✅ Fechado pelo ciclo `.claude/specs/read-side/` (REQ-37, Marco I).**
`distinct(lambda)`/`sum(lambda)`/`focus(id)` geram Go sobre `Lowerer.Lambda`
com paginação nativa de `AppendList<T>`; a Policy `RefundAllOnEventCancelled`
do spec §7 gera na forma canônica (`.claude/specs/read-side/requirements.md`
§1.4, item 3; `read-side/tasks.md` I6.1/I6.2). Desvio remanescente: `sum`/
`distinct` como agregação SQL (`SELECT SUM`/`DISTINCT`) não descem — o seam
permite, mas entra só quando houver medição que justifique
(`read-side/design.md` §5); `avg`/`min`/`max`/`group by` seguem fora
(spec §25, planejado).

**Promessa:** `state.items.focus(id)`, `.sum(i => i.price)`,
`.distinct(t => t.orderId)`, paginação nativa de `AppendList<T>`.

**Estado real:** inexistentes. Só aparecem como trabalho futuro em
comentários (`codegen/lower/expr.go` — o lowering de `LambdaExpr` existe
justamente esperando por eles; `codegen/lower/env.go`). Quebra em cascata:
a Policy `RefundAllOnEventCancelled` do spec §7 usa `.distinct`, e o exemplo
canônico de teste §22.4 depende dela — a fatia H4 precisou **adaptar a
fixture** (um `orderId` distinto por Ticket) exatamente por isso (registrado
na 6ª fatia de H4, `tasks.md`).

**Fechar exige:** métodos de coleção com lambda no lowering (o mecanismo
`Lowerer.Lambda(le, paramGoType)` já existe), semântica de agregação no
runtime/`Collection[T]`, e a descida opcional para SQL (G1) sem mudar o
lowering (`design.md` §4.4 já prevê). Naturalmente na mesma fatia de G-1.

### G-3. Features que o front-end nunca modelou (exclusões de `requirements.md` §1.3)

Zero suporte de ponta a ponta — **não são gaps do codegen**: o parser/
resolver/checker não as reconhecem, então fechá-las começa por um ciclo novo
de front-end (parser → resolver → sema → types), e só depois codegen.

| Feature | Spec | Nota |
|---|---|---|
| Exposição TCP/UDP | §10, §14 | `interface.ds` só modela HTTP e GRPC |
| Receptor `tenant.*` em corpos (`tenant.id`/`tier`/`exists`) | §13.2 | tenancy G5 funciona (filtro, cross_tenant, fail-closed 400), mas o domínio não consegue LER o tenant |
| Built-in `provision tenant(id)` | §13.4 | sem ela, o fluxo de provisionamento do spec não é expressável |
| Acesso nativo `events()` em Aggregates | §4.5 | — |

---

## 🟠 Alto — funciona, mas só em memória (spec promete produção)

### G-4. Providers reais de infraestrutura

**Parcialmente fechado pelo ciclo `.claude/specs/infra-providers/` (Marco J,
REQ-41..48) — um recorte deliberado de 5 categorias.** Tudo continua atrás
de seams limpos (NFR-12 respeitado); a tabela abaixo reflete o estado
ATUAL, pós-Marco J:

| Categoria | Spec pede | Implementado | Onde está documentado |
|---|---|---|---|
| Database | `"Postgres"` (§12) | `"sqlite"` **e** `"postgres"` são adapters reais (REQ-41) — outros bancos (MySQL, SQL Server, Mongo, Cassandra) seguem rótulo decorativo | `codegen/sql_wiring.go`/`codegen/sqlrt/`; J1 em `infra-providers/tasks.md` |
| Outbox | durabilidade real (§12) | tabela SQL transacional real (REQ-42) — atômico com a tx de negócio, retry/backoff, cleanup de retenção; **produtor→canal cross-service fechado** (REQ-42.6/REQ-51, ciclo `correcoes-issues-9-10-11`/Marco K, K3.1-K3.4): um módulo com Database real + canal `provider:"rabbitmq"` enfileira o `PublicEvent` cross-service no outbox atomicamente com a tx de negócio, e o relay do `DurableOutbox` (com o canal como `publisher`) publica de verdade — nunca mais publish direto no commit | `codegen/sql_wiring.go`/`codegen/rtsrc/outbox.go.txt`/`codegen/sqlrt/uow.go.txt`; J2 em `infra-providers/tasks.md`; K3 em `correcoes-issues-9-10-11/tasks.md`; ISSUE-9 (`.claude/issues.md`, RESOLVED) |
| Canais | `direct`/`queue`/`grpc`/`http`/`stream` (§11) | `queue` ganhou o provider `"rabbitmq"` real (cross-process, ordenação por chave, reconexão, DLQ — REQ-43); `direct` continua in-memory (não precisa de provider); `grpc`/`http`/`stream` continuam erro de geração | `codegen/channel_rabbitmq.go`; J3 em `infra-providers/tasks.md` |
| Cache backend | `memory`/`redis`/`layered` (§15) | `redis` real (REQ-44) — `layered` segue fora de escopo | `codegen/redisrt/`; J4 em `infra-providers/tasks.md` |
| RateLimit backend | `redis` (§16) | real, com fallback local em falha do Redis (REQ-44.5) | `codegen/redisrt/`; J4 em `infra-providers/tasks.md` |
| FileStorage | `"s3"` (§12) | `"s3"` real (REQ-45) — GCS/Azure Blob seguem fora de escopo | `codegen/s3rt/`; J5 em `infra-providers/tasks.md` |
| Idempotency storage | `same`/`external` Redis-Dynamo (§14) | só `same` in-memory — `external` explicitamente fora do recorte de Marco J | `codegen/rtsrc/idempotency.go.txt` |

**Residual aberto (não fechado por Marco J, registrado para um ciclo
futuro):**
- ~~**Outbox → canal cross-service (REQ-42.6, ISSUE-9)**~~ — **fechado** pelo
  ciclo `.claude/specs/correcoes-issues-9-10-11/` (Marco K, K3.1-K3.4,
  REQ-51): `durableProducer` (K3.1) detecta a condição de ativação (Database
  real + canal `provider:"rabbitmq"`); o produtor abre uma UnitOfWork real
  sobre `database/sql` (K3.2, `emitSingleDatabaseWiring`); ela enfileira o
  `PublicEvent` cross-service no outbox ANTES do commit, na MESMA `*sql.Tx`
  do `Append`, via o construtor distinto `sqlruntime.NewOutboxUnitOfWork`
  (K3.3, REQ-51.1/51.4); `generateCmdMainFile` monta um
  `runtime.NewDurableOutbox(outboxStore, registry, <canal>)` com o canal
  como `publisher` e sobe o relay/cleanup (K3.3, REQ-51.2/51.3) — a UoW não
  publica mais nada pós-commit. Provado com fixtures dedicadas + smoke
  compile (K3.2/K3.3) e um teste comportamental fim-a-fim de crash simulado
  sobre o **caminho gerado** do produtor (K3.4, REQ-51.7): `Publish` falha
  na 1ª tentativa, a linha fica undelivered (`attempts++`), o `Tick`
  seguinte re-publica — nenhum evento perdido. `wallet`/`shop` permanecem
  byte-idênticos (nenhum dos dois satisfaz a condição de ativação). Ver
  ISSUE-9 (`.claude/issues.md`, RESOLVED).
- **Vendorização real / build offline (`R10`, §design infra-providers
  §2.2):** o critério "`go build -mod=vendor` contra um `vendor/` real,
  materializado a partir da árvore vendorizada do próprio repositório
  domainscript" nunca foi implementado — os smoke tests dos 5 providers
  usam `go mod tidy` (rede) em vez de `-mod=vendor` (offline). Registrado
  como desvio explícito em J6.1/`.claude/state.md`.
- **Outros bancos** (MySQL, SQL Server, Mongo, Cassandra), **gRPC/HTTP/
  stream** como `via` de canal, **Idempotency `external`** (Redis/Dynamo),
  **Cache `layered`**, **GCS/Azure Blob** para FileStorage — todos
  explicitamente fora do recorte de 5 categorias do Marco J (ver
  `.claude/specs/infra-providers/requirements.md`, "Fora de escopo").

**Fechar o restante exige:** para os bancos/canais/backends fora do recorte
de Marco J, o modelo "implementar `Dialect`/adapter + 1 entrada de
registro" que Marco J já generalizou (REQ-46, `codegen/provider_registry.go`)
reduz o custo significativamente — cada categoria nova é, estruturalmente, o
mesmo trabalho que Postgres/RabbitMQ/Redis/S3 já fizeram.

### G-5. Field-Level Security de View: bloco `visibility` (spec §6.2)

**Estado real:** parseado (`ast.ViewDecl.Visibility`, `parser/parse_decl.go`)
mas **nenhum arquivo do codegen consome `Visibility`** — a omissão de campos
na serialização não acontece. É a lacuna "silenciosa" mais arriscada do
inventário: o programa compila, o bloco é aceito e ignorado. Atenuantes: o
spec marca a feature como "em evolução" (§25), e nenhum exemplo real
(wallet/shop) a usa.

**Fechar exige:** decidir a semântica de serialização condicional por caller
na borda HTTP/gRPC (o `runtime.Caller` já circula até lá) e emitir a
filtragem no encode das Views. Enquanto não fechar, o mínimo defensável é um
**warning de geração** ("visibility declarado e ignorado") para tirar o
silêncio.

---

## 🟡 Médio — implementado com semântica reduzida (tudo documentado)

### G-6. Observabilidade OTel parcial (spec §21, §1.1)

Traces OTel reais e opt-in via `Telemetry` (H2). Mas o adapter **não exporta
métricas nem logs OTel** — `Metric` vive num registry in-memory próprio
(`rtsrc/metrics.go.txt`, H3) e logs são `slog` com trace id. Documentado em
`codegen/decl_telemetry.go` (cabeçalho). O spec promete "instrumentação
OpenTelemetry automática" para os três sinais.

### G-7. Lacunas dos testes gerados (spec §22)

Cada uma registrada nas fatias de H4 em `tasks.md` e/ou em
`codegen/gentest.go`:

| Lacuna | Spec | Estado |
|---|---|---|
| `then state { ... }` (asserção de estado, StateStored) | §22.1 | erro de geração claro |
| Cenário de acesso NEGADO | §22 | não expressável — a gramática não tem "como o caller X" |
| `mock ... returns X` desviando fluxo | §22.3 | mock sempre sucede; `X` é construído mas não influencia (Adapters devolvem só `error` — `PaymentResult(status: Declined)` do spec não muda comportamento; falha simulada é papel de `fail step`) |
| `Subject emitted`/`released` de dentro de um passo de Saga | §22.3 | erro de geração claro (passo de Saga não tem Tx/Store) |
| Contra-exemplo **mínimo** (shrinking) em property | §22.5 | reporta a sequência completa, sem shrinking |
| `rolledback` com reversão real | §22.2 | é só `err != nil` — a UnitOfWork in-memory não tem staging (`rtsrc/uow.go.txt`) |
| Exemplo canônico de §22.4 (agrupamento por `orderId`) | §22.4 | **✅ fechado** — fixture des-adaptada pelo ciclo `.claude/specs/read-side/` (REQ-39.1, task I6.2): 3 tickets, 2 orders, `.distinct(t => t.orderId)`, `emitted count 2`, sem a adaptação "um orderId por ticket". Desvio remanescente: `reason` de `RefundRequested` usa o VO wrapper `RefundReason(string)` em vez do primitivo `string` cru do literal do spec — primitivo nu é proibido no Write Side (REQ-5.1); ver `read-side/tasks.md` I6.2. |

### G-8. Predicado de `where` com limitação de hoisting

**✅ Fechado pelo ciclo `.claude/specs/read-side/` (REQ-36, Marco I, task I1.1).**
O seam de predicado evoluiu para `func(T) (bool, error)` (`runtime.Query[T]`,
task I0.1): condição que exige hoisting (construção de VO composto, operador
de VO falível) agora é aceita, propagando o erro do item para o chamador da
query em vez de esbarrar num erro de geração arbitrário. Condição sem
hoisting continua gerando o predicado enxuto de antes.

---

## 🟢 Baixo — divergências menores ou "em evolução" no próprio spec (§25)

- **Redação GDPR (§4.4):** placeholder tipado implementado (E4.3); o
  *gatilho* de redação não — o próprio spec o marca como em evolução.
- **Cobertura semântica (§22.7):** o warning "Handle sem cenário de erro
  testado" existe (`sema/rules_warnings.go:checkHandleErrorCoverage`,
  REQ-5.22), mas o relatório fino "por ramo de regra de negócio" fica nessa
  granularidade por Handle.
- **§25 do spec** (avg/min/max/group by, aritmética estendida, marshalling
  FFI detalhado): o próprio spec declara como planejado/a definir — sem ação
  pendente deste lado.

---

## Priorização recomendada

1. ~~**"Marco E8 de verdade"** — Read Side completo: G-1 + G-2 + G-8.~~
   **Fechado** pelo ciclo `.claude/specs/read-side/` (Marco I) — §6.3, §7 e
   §20 do spec geram Go de verdade, e o §22.4 canônico voltou à forma do
   spec.
2. **Primeiro provider real** — G-4, começando por postgres (valida o seam
   de persistência sob produção) ou rabbitmq (valida o canal cross-service);
   o seam `Dialect` do read-side (REQ-40) já reduz o custo da parte SQL.
3. **`visibility` de View** — G-5, por ser a única lacuna com cheiro de
   segurança que falha em silêncio (ou, como paliativo imediato, o warning
   de geração).
4. **G-3 (front-end)** — cada feature vale um ciclo de spec próprio quando
   houver demanda real; são as mais caras porque atravessam o pipeline
   inteiro.
5. **G-6/G-7** — oportunistas: fechar quando o item vizinho for tocado (ex.
   métricas OTel quando mexer em telemetria; shrinking quando property
   ganhar um caso real que o exija). O item de §22.4 em G-7 já fechou junto
   com G-2.
