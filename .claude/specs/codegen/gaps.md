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

Tudo está atrás de seams limpos (NFR-12 respeitado), mas a única dependência
externa real por categoria é a listada abaixo. O sistema gerado hoje **não é
implantável contra infraestrutura real** além de sqlite.

| Categoria | Spec pede | Implementado | Onde está documentado |
|---|---|---|---|
| Database | `"Postgres"` (§12) | só `"sqlite"` é adapter real; `"postgres"` é rótulo decorativo | `codegen/sql_wiring.go`; provado empiricamente em `driver/generate_e2e_shop_test.go` |
| Canais | `direct`/`queue`/`grpc`/`http`/`stream` (§11) | só `direct` e `queue` in-memory; os outros 3 → **erro de geração**; provider `rabbitmq` não existe | `codegen/channel_test.go` (unsupported kind); F5 em `tasks.md` |
| Cache backend | `memory`/`redis`/`layered` (§15) | in-memory apenas | G3 em `tasks.md` |
| RateLimit backend | `redis` (§16) | in-memory apenas | G4 em `tasks.md` |
| FileStorage | `"s3"` (§12) | seam in-memory | G1a em `tasks.md` |
| Idempotency storage | `same`/`external` Redis-Dynamo (§14) | só `same` in-memory | `codegen/rtsrc/idempotency.go.txt` |
| Outbox | durabilidade real (§12) | in-memory | F5 em `tasks.md` |

**Fechar exige:** um provider real por vez, cada um opt-in e isolado (o
padrão já existe: `codegen/sqlrt/`, `codegen/grpcrt/`, `codegen/otelrt/`).
Postgres ou rabbitmq primeiro — são os que validam os seams mais centrais
(persistência e canal cross-service). **Nota (ciclo read-side):** REQ-40 do
ciclo `.claude/specs/read-side/` cria o seam `Dialect` + registro único de
provider — depois dele, adicionar um banco vira "implementar uma interface +
uma entrada de registro" (modelo de ORM), reduzindo o custo da parte SQL
deste gap.

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
| Exemplo canônico de §22.4 (agrupamento por `orderId`) | §22.4 | depende de `distinct` (G-2); fixture adaptada |

### G-8. Predicado de `where` com limitação de hoisting

Condição que exija hoisting (construção de VO composto ou operador de VO
falível dentro do `where`) → erro de geração claro — a assinatura
`func(T) bool` de `Collection[T]` não acomoda `error`
(`lower/stmt.go:hoistQueryPredicate`). Os casos reais de §22.4 (igualdade
sobre campos wrapper/primitivos) não precisam. Reavaliar junto com G-1.

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

1. **"Marco E8 de verdade"** — Read Side completo: G-1 + G-2 (+ reavaliar
   G-8). Destrava três seções do spec de uma vez (§6.3, §7, §20 — e o §22.4
   canônico de brinde) e é a dívida mais citada internamente.
2. **Primeiro provider real** — G-4, começando por postgres (valida o seam
   de persistência sob produção) ou rabbitmq (valida o canal cross-service).
3. **`visibility` de View** — G-5, por ser a única lacuna com cheiro de
   segurança que falha em silêncio (ou, como paliativo imediato, o warning
   de geração).
4. **G-3 (front-end)** — cada feature vale um ciclo de spec próprio quando
   houver demanda real; são as mais caras porque atravessam o pipeline
   inteiro.
5. **G-6/G-7** — oportunistas: fechar quando o item vizinho for tocado (ex.
   métricas OTel quando mexer em telemetria; shrinking quando property
   ganhar um caso real que o exija).
