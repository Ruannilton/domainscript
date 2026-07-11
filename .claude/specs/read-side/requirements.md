# Requirements — Read Side Completo (Cláusulas de Query e Smart Partial Loading)

> Documento 1 de 3 de um **novo** ciclo spec-driven (`requirements` → `design` →
> `tasks`), continuação direta do ciclo `.claude/specs/codegen/` (REQ-14..32,
> Marcos E–H, completo). Nasce dos gaps **G-1**, **G-2** e **G-8** de
> `.claude/specs/codegen/gaps.md`: as cláusulas SQL-like de Query e o Smart
> Partial Loading (§20 do spec) — as únicas lacunas em que **exemplos do próprio
> spec v6 não geram código hoje**.
>
> Continuidade de numeração: este ciclo continua a série a partir de **REQ-33**
> e **NFR-18** (o ciclo de codegen fechou em REQ-32/NFR-17), para um namespace
> de rastreabilidade único. Marco: **I** (o back-end fechou no Marco H).

## 1. Introdução

### 1.1. Objetivo

Fechar o Read Side do transpilador: fazer as Queries, Policies e Workers do
spec v6 (§6.3, §7, §8, §20) gerarem Go real — cláusulas `where`/`orderBy`/
`skip`/`take`/`as`/`join`, o operador `in`, e os métodos de coleção
`distinct`/`sum`/`focus` — primeiro sobre o runtime in-memory (zero deps,
NFR-12 intacto), com a descida para SQL no adapter existente (G1, sqlite)
preservada pelo mesmo seam.

### 1.2. Baseline (o que JÁ existe — nada disso é trabalho deste ciclo)

- **Front-end completo para todas as formas.** O parser reconhece todas as
  cláusulas (`parser/parse_query.go`: `join`/`on`/`where`/`orderBy` com
  direção/`skip`/`take`/`as`, binding de alias) e o operador `in` no nível dos
  comparativos (`parser/parse_expr.go`). Métodos de coleção com lambda
  (`.distinct(t => t.orderId)`) passam pelo checker sem erro — o catálogo de
  membros só valida shapes (`sema/rules_typecheck.go`), coleções são
  permissivas por design. **Nenhuma mudança de front-end é esperada**; se uma
  se mostrar necessária, é desvio a registrar no design.
- **`runtime.Collection[T]`** (H4): seam in-process com `Add`/`List`/`Count` e
  predicado por item `func(T) bool` — deliberadamente estreito; este ciclo o
  evolui (REQ-36/38).
- **Predicado por item** no lowering (`lower/stmt.go:hoistQueryPredicate`):
  `where` já vira `func(item T) bool { ... }` com o binding tipado em
  escopo-filho — mas recusa condição que precise de hoisting (G-8) e só
  alimenta `List`/`Count`.
- **`decl_query.go` mínimo** (E8.1): só `load X(id) as V` e `list <VO>` sem
  cláusulas; qualquer cláusula em `list <VO>` é erro de geração.
- **Adapter SQL opt-in** (G1, sqlite) atrás de `runtime.Store`; o núcleo
  in-memory compila sem deps (NFR-12).
- **Lambda no lowering** (`Lowerer.Lambda(le, paramGoType)`): existe desde E5,
  esperando exatamente pelos métodos de §20.

### 1.3. Escopo

| Em escopo | Fora de escopo |
|---|---|
| Cláusulas `where`/`orderBy`/`skip`/`take`/`as` sobre `list`/`load`-coleção, em Query/Policy/Worker/UseCase | Mudanças de front-end (parser/resolver/sema) — tudo já é parseado/validado |
| `join` mesmo-banco (in-memory) + operador `in` | `join` cross-database (já barrado pelo front-end, REQ-5) |
| `distinct(lambda)`/`sum(lambda)`/`focus(id)` (§20) + paginação nativa de `AppendList<T>` com `skip`/`take` | `avg`/`min`/`max`/`group by` (spec §25 — planejado, ciclo futuro) |
| Predicado de `where` falível (levantar G-8) | Providers reais novos (G-4 — postgres/rabbitmq/redis ficam para outro ciclo) |
| Descida das cláusulas para SQL no adapter sqlite existente, pelo seam | `visibility` de View (G-5), OTel metrics (G-6) — outros ciclos |
| Des-adaptar as fixtures que contornaram `distinct` (§22.4 canônico) | Full-text/espacial/graph (spec §1.2 — fora do escopo da linguagem) |

### 1.4. Critério-âncora (a régua deste ciclo)

Os três exemplos do spec v6 que hoje **não geram código** passam a gerar e a
se comportar:

1. `Query GetStatement` (§6.3) — `load Wallet(id).entries orderBy date
   descending skip page*20 take 20 as StatementEntryVW`.
2. `Query GetMyTickets` (§6.3) — `list Ticket t join Order o on t.orderId ==
   o.id where o.userId == userId and t.status in [...] as TicketVW`.
3. `Policy RefundAllOnEventCancelled` (§7) — `list Ticket t where ...` +
   `.distinct(t => t.orderId)` + `for`/`emit`; e o teste canônico de §22.4
   (3 tickets, 2 orders, `emitted count 2`) **sem a adaptação** que H4
   precisou fazer.

---

## 2. Requisitos Funcionais

### REQ-33 — Cláusulas de Query in-memory (where/orderBy/skip/take)

**User story:** Como desenvolvedor de domínio, quero escrever Queries com
filtro, ordenação e paginação declarativas, para que o corpo da Query espelhe
o spec §6.3 sem código imperativo.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar, para `list T [b] [where C] [orderBy K [dir]]
   [skip N] [take M]`, código que filtra por item (predicado tipado no
   binding), ordena pela chave `K` na direção declarada (`ascending` default),
   e pagina com `skip`/`take` — nessa ordem semântica (filtro → ordenação →
   paginação), independente da ordem textual das cláusulas.
2. THE SYSTEM SHALL suportar as mesmas cláusulas sobre uma **coleção de um
   aggregate carregado** (`load Wallet(id).entries orderBy ... skip ... take
   ...`), o caso do `GetStatement` do spec.
3. WHEN a chave de `orderBy` é um campo de VO wrapper ou primitivo comparável,
   THE SYSTEM SHALL ordenar pelo valor embrulhado; WHEN a chave não é
   comparável nativamente (VO composto sem operador de ordem), THE SYSTEM
   SHALL falhar com **erro de geração claro** — nunca Go que não compila.
4. THE SYSTEM SHALL aceitar essas cláusulas em qualquer corpo executável que
   já aceita `list`/`count` (Query, UseCase, Policy, Worker), com o mesmo
   comportamento.
5. `count [where C]` SHALL aceitar `where` com a mesma semântica de predicado
   (já existente) — `orderBy`/`skip`/`take` em `count` são erro de geração
   (não têm efeito observável; aceitar silenciosamente esconderia um engano).

### REQ-34 — Projeção `as V` sobre resultados de query

**User story:** Como desenvolvedor, quero projetar o resultado de um
`list`/`load`-coleção para uma View com `as V`, para devolver o shape de
leitura declarado.

**Critérios de aceitação:**

1. THE SYSTEM SHALL mapear cada item do resultado para o struct da View `V`
   por casamento de nome de campo (a MESMA convenção de projeção que
   `load X(id) as V` já usa em E8.1), inclusive achatamento de VO composto
   quando a View declara campos primitivos (ex. `amount_value`/
   `amount_currency` de `StatementEntryVW`, spec §6.1).
2. WHEN um campo da View não tem correspondente no item de origem, THE SYSTEM
   SHALL falhar com erro de geração claro, apontando o campo.
3. O tipo devolvido SHALL ser `List<V>` (inferência em `lower.TypeEnv` já
   prevista desde E5.0 — estender ao encadeamento com as demais cláusulas).

### REQ-35 — `join` mesmo-banco e o operador `in`

**User story:** Como desenvolvedor, quero correlacionar duas fontes do mesmo
banco e testar pertinência a um conjunto, para expressar o `GetMyTickets` do
spec §6.3.

**Critérios de aceitação:**

1. THE SYSTEM SHALL gerar, para `list A a join B b on <cond-igualdade> [where
   C] [as V]`, a correlação in-memory das duas fontes com ambos os aliases
   tipados em escopo no `on`/`where`/projeção. O front-end (REQ-5.11) já
   barra join cross-database; o gerador assume mesmo-banco.
2. A condição de `on` SHALL ser uma igualdade entre membros dos dois aliases
   (a forma do spec); outra forma é erro de geração claro.
3. THE SYSTEM SHALL gerar, para `x in [e1, e2, ...]` (BinaryExpr `token.IN`
   com lista literal), um teste de pertinência tipado; `in` sobre uma
   expressão de coleção não-literal também SHALL funcionar quando o tipo do
   elemento é comparável.
4. `in` SHALL funcionar tanto dentro de `where` (o caso do spec) quanto em
   qualquer expressão booleana de corpo executável.

### REQ-36 — Predicado de `where` falível (fecha G-8)

**User story:** Como desenvolvedor, quero usar no `where` qualquer condição
válida do domínio — inclusive construção de VO composto ou operador falível —
sem esbarrar num erro de geração arbitrário.

**Critérios de aceitação:**

1. THE SYSTEM SHALL aceitar, no predicado por item, condições que exigem
   hoisting (construção de VO composto, operador de VO que devolve `error`),
   propagando o erro do item para o chamador da query (a operação inteira
   falha — um item inválido não é silenciosamente pulado).
2. A assinatura do seam de predicado SHALL comportar erro (design decide a
   forma; `func(T) bool` de hoje não comporta).
3. Condição SEM necessidade de hoisting SHALL continuar gerando o predicado
   enxuto de hoje (sem custo novo para o caso comum).

### REQ-37 — Smart Partial Loading: `distinct`/`sum`/`focus` (§20)

**User story:** Como desenvolvedor, quero derivar valores de coleções
(`distinct`, `sum`) e focar um item (`focus`) sem materializar/iterar
manualmente, para escrever a Policy do spec §7 e os exemplos do §20.

**Critérios de aceitação:**

1. `col.distinct(x => x.k)` SHALL devolver `List<K>` com os valores únicos de
   `k`, preservando a ordem de primeira aparição (determinismo, NFR-13). `K`
   precisa ser comparável (primitivo, VO wrapper, Enum); senão, erro de
   geração claro.
2. `col.sum(x => x.v)` SHALL devolver o tipo de `v`: para numérico primitivo,
   soma nativa; para VO com `Operator +`, soma via o operador — **propagando
   o `error`** do operador falível (mesma disciplina de REQ-36); para tipo
   sem `+`, erro de geração claro.
3. `state.col.focus(id)` SHALL devolver o item de `col` cujo campo
   identificador casa com `id` (a semântica de "SELECT ... WHERE id=" do
   §20); item ausente segue a semântica de `ensure ... exists` já existente.
4. `AppendList<T>` com `skip`/`take` SHALL paginar sem cópia integral
   desnecessária no caminho in-memory (e SHALL ser descível a SQL por
   REQ-38).
5. Os três métodos SHALL funcionar nos mesmos corpos executáveis de REQ-33.4,
   e sobre o resultado de um `list` (ex. `soldTickets.distinct(...)` — a
   forma exata da Policy §7).

### REQ-38 — Descida para SQL pelo seam existente (sqlite)

**User story:** Como operador, quero que as mesmas Queries declarativas
executem como SQL parametrizado quando o módulo usa um Database real, sem
mudar uma linha de domínio.

**Critérios de aceitação:**

1. THE SYSTEM SHALL representar as cláusulas de forma **declarativa** no seam
   (não como closures opacas apenas), de modo que o adapter SQL (G1, sqlite)
   traduza `where`-igualdade, `orderBy`, `skip`/`take` e `count` para SQL
   parametrizado.
2. WHEN uma cláusula não é expressível em SQL pelo adapter (predicado
   arbitrário, chave de ordenação computada), THE SYSTEM SHALL degradar para
   avaliação in-memory do restante **após** aplicar em SQL o que for
   expressível — nunca resultado incorreto, nunca erro em runtime por forma
   não suportada.
3. O caminho in-memory SHALL continuar compilando **sem nenhuma dependência
   externa** (NFR-12) — a descida SQL é responsabilidade exclusiva do adapter
   opt-in.
4. `sum` sobre campo numérico simples SHOULD descer como agregação SQL
   (`SELECT SUM`) no adapter; é aceitável entregar in-memory primeiro e
   descer depois, desde que o seam já o permita sem redesenho.

### REQ-39 — Fixtures e testes canônicos re-alinhados

**User story:** Como mantenedor, quero que os exemplos e testes que foram
adaptados por falta do Read Side voltem à forma canônica do spec, para que o
repositório demonstre o spec de verdade.

**Critérios de aceitação:**

1. A fixture de §22.4 (H4, módulo `Refunds`) SHALL voltar à forma canônica do
   spec: 3 tickets, 2 orders, `.distinct(t => t.orderId)` na Policy, `emitted
   count 2` — removendo a adaptação "um orderId por ticket".
2. Fixtures/goldens novos SHALL cobrir `GetStatement` e `GetMyTickets` (§6.3)
   nas formas exatas do spec (adaptadas apenas nos nomes de domínio quando o
   wallet/shop real não tiver o construto — mesma regra "fixtures não são
   fonte de verdade" de sempre, com a adaptação documentada).
3. `docs/examples/wallet` e `docs/examples/shop` SHALL continuar gerando,
   compilando e passando seus testes sem regressão (NFR-19).

### REQ-40 — Seam de dialeto SQL (novo banco = uma implementação de interface)

**User story:** Como mantenedor, quero que adicionar suporte a um banco novo
seja implementar uma interface de dialeto e registrar o provider — o modelo
dos ORMs — e não editar strings SQL espalhadas pelo adapter.

**Critérios de aceitação:**

1. O adapter `sqlrt` SHALL consumir uma interface `Dialect` que encapsula
   TUDO que varia por banco na superfície SQL do gerador: estilo de
   placeholder (`?` vs `$1`), DDL das tabelas (`events`, e a tabela de
   `Collection[T]` de REQ-38), e a forma de paginação — **nenhuma string SQL
   específica de banco fora das implementações de `Dialect`**.
2. O reconhecimento de provider SHALL viver num **registro único** (provider
   string → módulo do driver p/ `go.mod` + import + construtor do `Dialect`):
   adicionar um banco = implementar `Dialect` + uma entrada no registro —
   zero mudanças em lowering, `decl_*.go` ou no runtime núcleo. (Hoje o
   "sqlite" está hardcoded em `sql_wiring.go` E `project.go` — dois pontos.)
3. A prova de plugabilidade SHALL ser dupla, sem dep externa nova: (a) o
   sqlite reescrito sobre `Dialect` sem regressão; (b) um **segundo dialeto
   de teste** com estilo de placeholder posicional (`$1`) exercitado contra o
   MESMO driver sqlite (que aceita ambas as sintaxes) — provando que nada
   fora do `Dialect` depende do estilo de placeholder.
4. Features específicas de banco (tipos nativos, upsert, índices avançados)
   ficam explicitamente FORA — o seam cobre só a superfície SQL que o gerador
   de fato emite, mantendo a paridade de comportamento entre bancos por
   construção (NFR-18).

---

## 3. Requisitos Não-Funcionais (incrementais)

> NFR-1..17 dos ciclos anteriores continuam valendo — em particular NFR-11
> (Go idiomático), NFR-12 (deps mínimas), NFR-13 (determinismo byte-idêntico),
> NFR-14 (correção por construção), NFR-17 (golden + smoke pareados).

### NFR-18 — Semântica de query única entre backends
O resultado observável de uma query (itens, ordem, contagem) é o MESMO no
caminho in-memory e no adapter SQL, para toda forma expressível em ambos —
diferenças de backend nunca vazam para o domínio. Testes comportamentais
pareados (mesma query, dois backends) são a prova.

### NFR-19 — Sem regressão nos exemplos existentes
Todo golden/smoke/behavioral test existente continua verde. Onde um golden
mudar por causa de forma nova de geração, a mudança é revisada e justificada
no commit — nunca regravada às cegas.

### NFR-20 — Erros de geração acionáveis
Toda forma fora do suporte (orderBy não-comparável, `on` não-igualdade,
`sum` sem operador `+`, campo de View sem origem) falha em tempo de geração
com mensagem que nomeia o construto e a alternativa — nunca Go inválido,
nunca comportamento silenciosamente errado (mesma disciplina de todo o ciclo
anterior).

---

## 4. Rastreabilidade

| Requisito | Tema | Gap de origem |
|---|---|---|
| REQ-33 | where/orderBy/skip/take in-memory | G-1 |
| REQ-34 | Projeção `as V` | G-1 |
| REQ-35 | join mesmo-banco + `in` | G-1 |
| REQ-36 | Predicado falível | G-8 |
| REQ-37 | distinct/sum/focus + paginação AppendList | G-2 |
| REQ-38 | Descida SQL pelo seam | G-1 (§4.4 do design de codegen) |
| REQ-39 | Fixtures canônicas | G-1/G-2 (consequência) |
| REQ-40 | Dialeto SQL plugável | G-4 (reduz o custo de fechá-lo) |
| NFR-18..20 | transversais | — |

---

## 5. Critérios de Pronto (Definition of Done)

O ciclo está completo quando:

1. Os três exemplos-âncora (§1.4) geram Go que compila e se comporta como o
   spec descreve, provados por golden + smoke + teste comportamental.
2. A fixture canônica de §22.4 está des-adaptada (REQ-39.1) e o teste gerado
   correspondente roda verde.
3. O caminho in-memory segue sem nenhuma dep externa; o adapter sqlite traduz
   as formas de REQ-38.1 para SQL parametrizado, com testes pareados
   (NFR-18).
3a. O adapter roda inteiro sobre o seam `Dialect` (REQ-40): nenhuma string
   SQL específica de banco fora dos dialetos, provider num registro único, e
   o dialeto de teste `$1` passando a MESMA suíte do dialeto sqlite.
4. `go build ./...` / `go test ./...` do compilador verdes; wallet e shop sem
   regressão (NFR-19).
5. `gaps.md` do ciclo de codegen atualizado: G-1, G-2 e G-8 marcados como
   fechados por este ciclo (com ponteiro para cá); G-7 atualizado no item
   "§22.4 canônico".
