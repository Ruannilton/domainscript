# Issues

Registro de erros encontrados pelos agentes durante a execução das specs que
**não pertencem ao escopo da task/spec em andamento** (erros do escopo atual
são corrigidos na hora, sem entrar aqui — ver `CLAUDE.md`).

## Como registrar

Cada issue é um bloco novo, nesta forma:

```
## ISSUE-<numero>
- SPEC: <nome-da-spec>
- TASK: <numero-da-task>
- DESCRIPTION: <descrição do erro encontrado, contexto e impacto>
```

- `<numero>` é sequencial, nunca reaproveitado.
- `SPEC`/`TASK` identificam onde o erro foi **encontrado** (não necessariamente
  onde ele deveria ser corrigido).
- Issues aqui ficam pendentes até serem resolvidas em uma task futura; ao
  resolver, marque com `RESOLVED (commit <hash>)` ao final do bloco em vez de
  apagar o registro.

---

## ISSUE-1
- SPEC: read-side
- TASK: I5.1
- DESCRIPTION: `emitQueryJoinCollectionVars` (`codegen/decl_query.go`) gera
  variáveis de pacote como `var ticketCollection = runtime.NewMemoryCollection[Ticket]()`
  no arquivo `queries.go`. Se o MESMO tipo (ex. `Ticket`) também for
  referenciado num `list`/`count` dentro de uma Policy do MESMO módulo,
  `emitPolicyCollectionVars` (`codegen/decl_policy.go`) gera a MESMA variável,
  com o MESMO nome (`policyCollectionVarName`/convenção `<tipo>Collection`),
  em `policies.go` — os dois arquivos compartilham o MESMO pacote Go, então o
  compilador falha com "redeclared in this block". Nenhum exemplo real hoje
  exercita essa combinação (nenhum módulo tem Query com join E Policy com
  list/count sobre o mesmo tipo), por isso não foi pego pelos testes
  existentes. Correção sugerida: centralizar a declaração dessas
  Collection[T] var num único arquivo por módulo (ex. `collections.go`),
  compartilhado entre `EmitQueries`/`EmitPolicies`, em vez de cada emissor
  declarar as suas independentemente.
- RESOLVED (commit `3a22df3`): `codegen/decl_collections.go` (novo) calcula
  a interseção entre os tipos usados por join de Query e por list/count de
  Policy do módulo; quando não vazia, `generateModuleFiles` emite um único
  `collections.go` com esses vars e repassa o mapa tipo->var a
  `EmitQueries`/`EmitPolicies`, que passam a reusar o var compartilhado em
  vez de declarar o seu. Vazio no caso comum (Go byte-idêntico a antes).
  Fixture de regressão em `codegen/decl_collections_test.go`.

---

## ISSUE-2
- SPEC: codegen
- TASK: gaps.md §G-3 (exclusões de `requirements.md` §1.3)
- DESCRIPTION: Features do spec v6 que o **front-end nunca modelou** — não são
  gaps só do codegen: parser/resolver/checker não as reconhecem, então fechar
  qualquer uma começa por um ciclo novo de front-end (parser → resolver → sema
  → types) e só depois codegen. São quatro:
  (a) **Exposição TCP/UDP** (spec §10/§14) — `interface.ds` só modela HTTP e
  GRPC.
  (b) **Receptor `tenant.*` em corpos** (`tenant.id`/`tenant.tier`/
  `tenant.exists`, spec §13.2) — a tenancy row_level funciona no runtime
  (filtro, cross_tenant, fail-closed 400), mas o domínio não consegue LER o
  tenant corrente de dentro de um Handle/UseCase.
  (c) **Built-in `provision tenant(id)`** (spec §13.4) — sem ela o fluxo de
  provisionamento de tenant do spec não é expressável.
  (d) **Acesso nativo `events()` em Aggregates** (spec §4.5).
  Impacto: cada uma é um ciclo de spec próprio (as mais caras do inventário,
  atravessam o pipeline inteiro); abrir só quando houver demanda real.

## ISSUE-3
- SPEC: codegen
- TASK: gaps.md §G-4 (Marcos F/G — providers reais de infraestrutura)
- DESCRIPTION: Tudo está atrás de seams limpos (NFR-12 respeitado), mas a única
  dependência externa real por categoria é sqlite — o sistema gerado hoje
  **não é implantável contra infraestrutura real** além disso. Categorias em
  aberto: Database (spec pede Postgres §12; só `"sqlite"` é adapter real,
  `"postgres"`/`"mongodb"` são rótulos decorativos — `codegen/sql_wiring.go`);
  Canais (`grpc`/`http`/`stream` §11 → erro de geração; provider `rabbitmq`
  não existe, só `direct`/`queue` in-memory — `codegen/channel_test.go`);
  Cache backend (`redis`/`layered` §15 → só in-memory); RateLimit backend
  (`redis` §16 → só in-memory); FileStorage (`"s3"` §12 → seam in-memory);
  Idempotency storage (`external` Redis/Dynamo §14 → só `same` in-memory,
  `codegen/rtsrc/idempotency.go.txt`); Outbox (durabilidade real §12 → in-
  memory). Fechar exige um provider real por vez, opt-in e isolado (padrão já
  existe: `codegen/sqlrt/`, `grpcrt/`, `otelrt/`). Postgres ou rabbitmq
  primeiro (validam os seams mais centrais). Nota: o seam `Dialect` (REQ-40,
  read-side/I7.0, `codegen/sqlrt/dialect.go.txt`) já reduz o custo da parte
  SQL — adicionar banco vira "implementar `Dialect` + entrada no registro"; o
  restante (driver real, migrations, type mapping) segue aberto.
- EM ANDAMENTO (spec criada): `.claude/specs/infra-providers/` (Marco J,
  REQ-41..48 / NFR-21..24) trata esta issue com **recorte de 5 providers** —
  Postgres, RabbitMQ, Redis (Cache+RateLimit), S3 e Outbox durável. As demais
  categorias de G-4 (outros bancos, gRPC-canal, Dynamo para idempotency
  `external`, backend `layered` de cache, GCS/Azure) ficam explicitamente fora
  do recorte, para ciclos futuros. Fecha PARCIALMENTE quando o Marco J fechar.

## ISSUE-4
- SPEC: codegen
- TASK: gaps.md §G-5 (Field-Level Security de View)
- DESCRIPTION: O bloco `visibility` de View (spec §6.2) é **parseado**
  (`ast.ViewDecl.Visibility`, `parser/parse_decl.go`) mas **nenhum arquivo do
  codegen consome `Visibility`** — a omissão condicional de campos na
  serialização não acontece. É a lacuna "silenciosa" mais arriscada do
  inventário (cheiro de segurança que falha em silêncio): o programa compila,
  o bloco é aceito e ignorado. O exemplo `docs/examples/pizzeria`
  (`sales/read.ds`, `OrderVW`) exercita e documenta essa limitação. Atenuantes:
  o spec marca a feature como "em evolução" (§25) e wallet/shop não a usam.
  Fechar exige decidir a semântica de serialização condicional por caller na
  borda HTTP/gRPC (o `runtime.Caller` já circula até lá) e emitir a filtragem
  no encode das Views. Paliativo imediato defensável: **warning de geração**
  ("visibility declarado e ignorado") para tirar o silêncio.

## ISSUE-5
- SPEC: codegen
- TASK: gaps.md §G-6 (Observabilidade OTel parcial, Marcos H2/H3)
- DESCRIPTION: Traces OTel reais e opt-in via `Telemetry` (H2) funcionam, mas o
  adapter **não exporta métricas nem logs OTel**: `Metric` vive num registry
  in-memory próprio (`rtsrc/metrics.go.txt`, H3) e logs são `slog` com trace
  id, não OTLP. Documentado no cabeçalho de `codegen/decl_telemetry.go`. O spec
  (§21/§1.1) promete "instrumentação OpenTelemetry automática" para os três
  sinais. Oportunista: fechar quando telemetria for tocada de novo.

## ISSUE-6
- SPEC: codegen
- TASK: gaps.md §G-7 (lacunas dos testes gerados, Marco H4)
- DESCRIPTION: `*.test.ds` → Go tests cobre o caminho feliz, mas várias formas
  do spec §22 têm semântica reduzida (cada uma registrada nas fatias de H4
  em `tasks.md`/`codegen/gentest.go`): `then state { ... }` (asserção de estado
  StateStored, §22.1) → erro de geração claro; cenário de acesso NEGADO (§22)
  → não expressável (a gramática não tem "como o caller X"); `mock ... returns
  X` desviando fluxo (§22.3) → mock sempre sucede, `X` é construído mas não
  influencia; `Subject emitted`/`released` de dentro de passo de Saga (§22.3)
  → erro de geração claro; contra-exemplo **mínimo**/shrinking em property
  (§22.5) → reporta a sequência completa sem encolher; `rolledback` com
  reversão real (§22.2) → é só `err != nil`, a UnitOfWork in-memory não tem
  staging. (O item §22.4 — agrupamento por `orderId` — JÁ foi fechado pelo
  ciclo read-side, REQ-39.1/I6.2, e não entra aqui.) Oportunista: fechar cada
  um quando o vizinho for tocado.

## ISSUE-7
- SPEC: codegen
- TASK: descoberto em `docs/examples/pizzeria` (não estava no `gaps.md`)
- DESCRIPTION: Um módulo que combina **`UseCase` E `Policy` no mesmo módulo**
  ainda não gera — `dsc gen` falha com "UseCase e Policy no mesmo módulo ainda
  não têm wiring combinado suportado (cada um gera seu próprio Wire —
  colidiriam); ver a doc de decl_policy.go". `generateModuleFiles`
  (`codegen/codegen.go`) emitiria dois `func Wire(...)` no mesmo pacote Go
  (um de `emitUOWWireFunc`, outro de `emitPolicyWireFunc`), que colidem. Nem
  wallet nem shop exercitavam essa combinação; o módulo `Kitchen` do exemplo
  pizzeria (Claim/Finish via HTTP **e** criação reativa via Policy sobre
  `OrderPaid`) é o primeiro caso real — o próprio comentário no código já
  previa "fica para quando um exemplo real precisar disso". Bloqueia a geração
  do back-end do exemplo pizzeria (o front-end valida limpo). Fechar exige
  unificar o wiring: um único `Wire(...)` por módulo que registre tanto os
  UseCases (dispatcher/UoW) quanto as Policies (assinaturas de evento).

## ISSUE-8
- SPEC: codegen
- TASK: gaps.md §G-baixo (§25 — em evolução no próprio spec)
- DESCRIPTION: Divergências menores, a maioria marcada como planejada/"em
  evolução" pelo próprio spec (§25) — registradas para rastreio, sem ação
  urgente: (a) **Redação GDPR** (§4.4) — placeholder tipado implementado
  (E4.3), mas o *gatilho* de redação não (spec o marca como em evolução);
  (b) **Cobertura semântica** (§22.7) — o warning "Handle sem cenário de erro
  testado" existe (`sema/rules_warnings.go:checkHandleErrorCoverage`,
  REQ-5.22), mas o relatório fino "por ramo de regra de negócio" fica na
  granularidade por Handle; (c) **itens §25** (avg/min/max/group by, aritmética
  estendida, marshalling FFI detalhado) — declarados planejado/a definir pelo
  spec, sem ação pendente deste lado.
