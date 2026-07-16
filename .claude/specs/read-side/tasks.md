# Tasks — Read Side Completo (Cláusulas de Query e Smart Partial Loading)

> Documento 3 de 3. Plano executável para `requirements.md` (REQ-33..39) via
> `design.md`. Mesmas convenções dos `tasks.md` anteriores: ordem respeita
> dependências, fatiar **verticalmente** (uma capacidade: runtime → lowering →
> golden → smoke → comportamental, antes de alargar), cada task tem **critério
> de conclusão** verificável e fecha em **commit** atômico (Conventional
> Commits, português imperativo).

## Como ler este plano

- Todas as tarefas começam `[ ]`. A baseline é o repositório com os ciclos
  E–H **completos e verdes** (`gaps.md` registra o ponto de partida).
- `(REQ-n)` = requisito atendido; `(§design x)` = seção do design deste ciclo.
- Cada task lista **Toca**, **Conclusão** e **Commit**. **Depende** aparece
  quando a ordem não é óbvia pela numeração.
- **Regra de verde dupla** (inalterada): só commitar com a árvore do
  compilador verde E o Go gerado dos exemplos compilando/passando.
- **Goldens que mudam por regeneração legítima** (ex. assinatura do predicado,
  §design 2.2) são revisados e justificados no corpo do commit — nunca
  regravados às cegas (NFR-19).
- Marco único: **I** ("Read Side de verdade"). As fases I0–I2 são o alicerce
  (runtime + lowering base); I3–I6 são as capacidades visíveis, cada uma
  ancorada num exemplo do spec; I7 desce para SQL; I8 fecha.

---

## Marco I — Read Side de Verdade

> Ao fim do Marco I, os três exemplos-âncora do spec (requirements §1.4) geram
> Go que compila e se comporta como o spec descreve, e a fixture canônica de
> §22.4 volta à forma do spec (3 tickets, 2 orders, `emitted count 2`).

### Fase I0 — Seam novo no runtime

- [x] **I0.1** `runtime.Query[T]` + `Select`/`SelectSlice`/`Count` no lugar de
  `List`/`Count` com predicado nu: a struct híbrida (closure + descritor
  declarativo), a ordem semântica fixa where→orderBy→skip→take num ÚNICO
  intérprete (`SelectSlice`, que `memoryCollection.Select` delega), sort
  **estável**, `Skip`/`Take` com sentinela -1, predicado e `Less` falíveis
  (`func(T) (bool, error)` / `func(a, b T) (bool, error)`). Migrar TODOS os
  chamadores gerados de H4 (`decl_policy.go`, `gentest.go` §22.4) para a
  forma nova — os goldens afetados regeneram uma vez, revisados. _(REQ-33.1,
  REQ-36.2, §design 2)_
  **Toca:** `codegen/rtsrc/collection.go.txt`, `collection_test.go.txt`,
  `codegen/decl_policy.go`, `codegen/gentest.go`, goldens de H4.
  **Conclusão:** testes do runtime cobrem ordem semântica, estabilidade,
  bordas de skip/take, erro de predicado abortando, e que o in-memory IGNORA
  `WhereEq`/`OrderField` (closures são a verdade); goldens/smoke de H4 verdes
  na forma nova; regen byte-idêntico.
  **Commit:** `feat(runtime): Query[T] declarativa com Select/SelectSlice`

### Fase I1 — Predicado falível (fecha G-8)

- [x] **I1.1** `hoistQueryPredicate` emite corpo em bloco: linhas hoisted
  antes do `return`, `if err != nil { return false, err }` entre elas — a
  condição com construção de VO composto/operador falível passa a ser aceita;
  a doc-nota da limitação G-8 em `lower/stmt.go` é substituída. Condição sem
  hoisting continua uma linha (`return cond, nil`). _(REQ-36, §design 3.3)_
  **Toca:** `codegen/lower/stmt.go`, `codegen/lower/builtins_test.go` (o
  teste `NeedsHoistingFailsExplicitly` INVERTE: passa a provar a forma
  gerada).
  **Conclusão:** teste sintético com `where e.amount == Money(amount: 10,
  currency: "BRL")` gera predicado válido que compila; teste comportamental
  prova que um item com operador falhando aborta a query com o erro.
  **Commit:** `feat(codegen): predicado de where falível (hoisting no corpo)`

### Fase I2 — orderBy/skip/take no lowering

- [x] **I2.1** `hoistList` monta a `Query[T]` completa: `orderBy` lowerizado
  em escopo-filho (mesmo mecanismo do predicado), corpo de `Less` decidido
  pela tabela de comparabilidade (§design 3.2 — primitivos/wrapper/Enum
  nativos, VO composto via `Operator </>` do registry, resto = erro de
  geração), descritores `OrderField`/`WhereEq` preenchidos SÓ nas formas
  simples, `skip`/`take` como expressões inteiras. Cláusula duplicada → erro
  claro. `orderBy`/`skip`/`take` em `count` → erro claro (REQ-33.5).
  _(REQ-33.1/33.3, §design 3.1/3.2)_
  **Toca:** `codegen/lower/stmt.go`, `codegen/lower/env.go` (inferência
  preserva o tipo do item pelo encadeamento), testes sintéticos por cláusula
  e por linha da tabela de comparabilidade (incl. os erros, NFR-20).
  **Conclusão:** `list T t where C orderBy t.k descending skip N take M`
  gera `Select` com a Query completa; golden sintético; smoke compila.
  **Commit:** `feat(codegen): orderBy/skip/take na lowering de list`

### Fase I3 — `load X(id).entries` + projeção `as V` (GetStatement, §6.3)

- [x] **I3.1** Caminho sem Collection: `load` com Target `MemberExpr` sobre
  construção de aggregate carrega o aggregate (LoadCall intocado) e aplica
  `runtime.SelectSlice` sobre o campo de coleção do state; `AppendList<T>`
  reslica sem cópia integral quando não há ordenação. Inferência de
  `load X(id).<campo>` no `TypeEnv`. _(REQ-33.2, REQ-37.4 parcial, §design
  3.4/3.10)_
  **Commit:** `feat(codegen): cláusulas sobre coleção de aggregate carregado`

- [x] **I3.2** Projeção `as V` por item sobre resultado de query: reusa o
  mapeamento campo-a-campo (com achatamento de VO composto) de `load X(id)
  as V` (E8.1), num loop de projeção; campo da View sem origem → erro de
  geração nomeando o campo. `decl_query.go` remove o erro "cláusulas não
  suportadas (E8.1)" — o fast-path de return delega ao hoisting de corpo.
  Cache de Query (G3) inalterado por construção. _(REQ-34, §design 3.5)_
  **Depende:** I3.1.
  **Conclusão (âncora 1):** fixture `GetStatement` na forma exata do spec
  §6.3 (`load Wallet(id).entries orderBy date descending skip page*20 take 20
  as StatementEntryVW` — adaptada só nos nomes se o wallet real divergir,
  documentado) gera, compila e um teste comportamental prova ordenação
  descendente + paginação; golden + determinismo.
  **Commit:** `feat(codegen): projeção as V sobre resultados de query`

### Fase I4 — Operador `in`

- [x] **I4.1** `BinaryExpr(token.IN)` no dispatch de operadores: RHS lista
  literal → `slices.Contains([]T{...}, lhs)`; RHS coleção → idem quando o
  elemento é comparável nativamente; VO composto no LHS → erro claro.
  Funciona em `where` e em qualquer expressão booleana. _(REQ-35.3/35.4,
  §design 3.6)_
  **Toca:** `codegen/lower/expr.go`, `codegen/lower/env.go` (tipo boolean),
  testes sintéticos (dentro e fora de where; erro do VO composto).
  **Commit:** `feat(codegen): operador in`

### Fase I5 — `join` mesmo-banco (GetMyTickets, §6.3)

- [x] **I5.1** Lowering do join: materializa as duas fontes, loop aninhado
  com aliases tipados em escopos-filho, `on` só igualdade membro-a-membro
  (senão erro claro), `where` com hoisting normal no corpo do loop, projeção
  `as V` resolvendo contra os aliases na ordem de declaração (ambiguidade →
  erro), sem `as` → lista do primeiro alias. _(REQ-35.1/35.2, §design 3.7)_
  **Depende:** I2–I4 (o exemplo canônico usa where + in + as).
  **Conclusão (âncora 2):** fixture `GetMyTickets` na forma do spec §6.3
  (`list Ticket t join Order o on t.orderId == o.id where o.userId == userId
  and t.status in [...] as TicketVW`) gera, compila e um comportamental prova
  a correlação; golden + determinismo + smoke.
  **Commit:** `feat(codegen): join mesmo-banco in-memory`
  **Desvio registrado:** `orderBy`/`skip`/`take` pós-join (§design 3.7 ponto
  4) ficou de fora — a âncora `GetMyTickets` não usa nenhuma das três, e a
  semântica de contra qual binding a chave de ordenação resolveria
  pós-projeção não está fechada no design; `ensureJoinClausesWellFormed`
  (`codegen/lower/join.go`) recusa as três com um erro de geração claro em
  vez de adivinhar. Fica para uma task futura quando um exemplo real precisar.
  Também novo nesta task, fora do texto original: um "list T ... join U ..."
  dentro de uma Query passou a rotear para `runtime.Collection[T]` (um var
  `<tipo>Collection` por fonte, mesmo padrão de `decl_policy.go` — nenhuma
  Query tinha essa forma de sourcing antes; só "load Agg(id)"/"list <VO>
  correlacionado" existiam).

### Fase I6 — Smart Partial Loading (§20) e fixtures canônicas

- [x] **I6.1** `distinct`/`sum`/`focus` como métodos embutidos com lambda
  tipada (via `Lowerer.Lambda`, existente): `distinct` com mapa de vistos +
  ordem de 1ª aparição; `sum` como fold do primeiro item (numérico nativo ou
  `Operator +` via registry, com propagação de erro; vazio → zero value,
  documentado); `focus` por convenção do campo `id` (ausente → erro claro).
  Receptores: campo do state, resultado de `list`, parâmetro. Inferência dos
  três no `TypeEnv`. _(REQ-37.1/37.2/37.3/37.5, §design 3.8/3.10)_
  **Toca:** `codegen/lower/expr.go`, `env.go`, testes sintéticos por método
  (incl. cada erro de geração: K não-comparável, sum sem `+`, focus sem
  `id`).
  **Commit:** `feat(codegen): distinct, sum e focus (§20)`

- [x] **I6.2** Âncora 3 + des-adaptação: a fixture da Policy §7
  (`RefundAllOnEventCancelled` com `list ... where` + `.distinct(t =>
  t.orderId)` + `for`/`emit`) na forma canônica; a fixture de §22.4 (H4,
  módulo Refunds) VOLTA ao spec — 3 tickets, 2 orders, `emitted count 2` —
  removendo a adaptação "um orderId por ticket" e atualizando a nota da 6ª
  fatia de H4 em `.claude/specs/codegen/tasks.md` (a adaptação deixa de
  existir; o registro histórico aponta para este ciclo). _(REQ-37.5,
  REQ-39.1/39.2)_
  **Depende:** I6.1.
  **Conclusão:** o teste gerado de §22.4 canônico roda verde sobre o projeto
  gerado inteiro; goldens/smoke/determinismo; wallet e shop sem regressão.
  **Commit:** `feat(codegen): Policy §7 canônica e fixtures de §22.4 des-adaptadas`
  **Desvio remanescente:** `reason` de `RefundRequested` usa o VO wrapper
  `RefundReason(string)` em vez do primitivo `string` cru do literal do spec
  (§22.4) — primitivo nu é proibido no Write Side (REQ-5.1); documentado em
  `codegen/gentest_policy_test.go` e `.claude/specs/codegen/tasks.md`.

### Fase I7 — Descida SQL (sqlite) sobre dialeto plugável

- [x] **I7.0** Seam `Dialect` + registro único de provider (REQ-40): extrair
  de `sqlrt` tudo que varia por banco (placeholders, DDL de `events`,
  paginação) para uma interface `Dialect` consumida por todo o adapter —
  nenhuma string SQL específica de banco fora dos dialetos; colapsar o
  reconhecimento de provider (hoje duplicado em `sql_wiring.go` e
  `project.go`) num registro único provider → {módulo do driver, import,
  dialeto}. Prova de plugabilidade sem dep nova: um dialeto de TESTE com
  placeholder posicional `$n` roda a suíte inteira do adapter contra o mesmo
  driver sqlite (que aceita as duas sintaxes). _(REQ-40, §design 3.9a)_
  **Toca:** `codegen/sqlrt/` (novo `dialect.go.txt`), `codegen/sql_wiring.go`,
  `codegen/project.go`, testes do adapter.
  **Conclusão:** suíte de G1 verde sobre o sqlite reescrito em `Dialect`;
  a MESMA suíte verde com o dialeto `$n`; go.mod/wiring gerados idênticos
  aos de antes (golden sem mudança para o usuário final).
  **Commit:** `refactor(codegen): seam Dialect e registro único de provider SQL`

- [ ] **I7.1** Contraparte de `Collection[T]` sobre tabela no adapter sqlite:
  `Select`/`Count` montam SQL parametrizado SÓ com os descritores presentes
  (`WhereEq` → AND de `col = ?`; `OrderField`/`OrderDesc` → ORDER BY;
  `Skip`/`Take` → LIMIT/OFFSET; `Count` só-WhereEq → `SELECT COUNT(*)`);
  o resto pós-processa via o MESMO `SelectSlice`. Regra de correção: `Less`
  sem descritor ⇒ `Skip`/`Take` também não descem. Todo SQL montado via o
  `Dialect` de I7.0 — nunca string direta. O caminho in-memory segue sem dep
  externa (NFR-12). _(REQ-38, §design 3.9/3.9a)_ **Depende:** I7.0.
  **Toca:** `codegen/sqlrt/`, wiring de G1.
  **Conclusão:** testes **pareados** (NFR-18): a mesma query, mesmo seed de
  dados, nos dois backends, resultados idênticos — incluindo um caso que
  força a degradação (closure não-descível) provando REQ-38.2; smoke do
  caminho sqlite via a fixture opt-in de G1.
  **Commit:** `feat(codegen): descida das cláusulas para SQL no adapter sqlite`

### Fase I8 — Fechamento do ciclo

- [ ] **I8.1** Revisão contra a DoD (requirements §5); atualizar
  `.claude/specs/codegen/gaps.md` (G-1/G-2/G-8 fechados com ponteiro para
  este ciclo; item §22.4 de G-7 atualizado; "Fora de Escopo Registrado" do
  design §5 vira os gaps remanescentes); atualizar `README.md`/`CLAUDE.md` se
  o estado descrito neles mudou. _(REQ-39.3, DoD §5)_
  **Commit:** `docs(repo): fecha o ciclo de Read Side e atualiza gaps`

---

## Mapa de Dependências

```
I0 runtime Query[T] ─▶ I1 predicado falível ─▶ I2 orderBy/skip/take ─┬─▶ I3 load.entries + as V (âncora 1)
                                                                      ├─▶ I4 operador in ─▶ I5 join (âncora 2)
                                                                      └─▶ I6 §20 + fixtures canônicas (âncora 3)
                                                                                 │
                                                              I7 SQL (sqlite) ◀──┘ (precisa das formas prontas)
                                                                                 │
                                                                          I8 fechamento
```

- **I0 antes de tudo:** toda fatia consome `Query[T]`/`SelectSlice`.
- **I5 depende de I2+I4** (o exemplo canônico usa where/in/as juntos).
- **I7 depende de I2–I6** (desce o que já existe; não desenha formas novas).
- I3, I4 e I6 são paralelizáveis entre si após I2.

---

## Rastreabilidade REQ → Tasks

| Requisito | Tasks |
|---|---|
| REQ-33 | I0.1, I2.1, I3.1 |
| REQ-34 | I3.2 |
| REQ-35 | I4.1, I5.1 |
| REQ-36 | I0.1 (assinatura), I1.1 |
| REQ-37 | I3.1 (paginação AppendList), I6.1, I6.2 |
| REQ-38 | I7.1 |
| REQ-39 | I6.2, I8.1 |
| REQ-40 | I7.0 |
| NFR-18 | I7.1 (testes pareados) |
| NFR-19 | transversal (regra de verde dupla + revisão de goldens) |
| NFR-20 | I2.1, I3.2, I4.1, I5.1, I6.1 (um teste por erro) |
