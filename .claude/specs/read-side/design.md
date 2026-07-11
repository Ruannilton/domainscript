# Design — Read Side Completo (Cláusulas de Query e Smart Partial Loading)

> Documento 2 de 3. Define **como** atender `requirements.md` (REQ-33..39,
> NFR-18..20). Estende o `design.md` de codegen (§3.6/3.9/4.4) sem contradizê-lo:
> os seams e invariantes de lá continuam valendo; este ciclo preenche o que
> aquele documento adiou explicitamente como "Read Side de verdade (E8)".

## 1. Visão Arquitetural

### 1.1. Onde o trabalho mora

**Nenhuma fase do front-end muda.** O parser já produz `ast.QueryExpr{Op,
Target, Binding, Clauses}` com toda cláusula (`parser/parse_query.go`) e o
operador `in` já é um `BinaryExpr(token.IN)` comum. O trabalho é inteiro em:

```
codegen/rtsrc/collection.go.txt   Query[T] declarativa + Select/SelectSlice (§3.1, §3.2)
codegen/lower/stmt.go             hoistList/hoistCount evoluem; predicado falível (§3.3)
codegen/lower/expr.go             operador in (§3.6); métodos distinct/sum/focus (§3.7)
codegen/lower/env.go              inferência dos encadeamentos e dos métodos (§3.8)
codegen/decl_query.go             remove o "mínimo de E8.1"; formas completas (§3.5)
codegen/sqlrt/                    tradução da parte declarativa para SQL (§3.9)
```

### 1.2. Princípio central: um espec de query, dois intérpretes

A tensão de REQ-38 é: o caminho in-memory quer **closures Go idiomáticas**
(NFR-11); o adapter SQL quer **estrutura declarativa** para traduzir. A
resposta é um valor híbrido — `runtime.Query[T]` — em que **cada cláusula
carrega as duas formas quando as duas existem**: a closure compilada (sempre)
e o descritor declarativo (quando a cláusula é simples o bastante para SQL).
O intérprete in-memory ignora os descritores e roda as closures; o adapter
SQL consome os descritores que reconhece e roda as closures restantes como
pós-filtro (REQ-38.2). A semântica observável é uma só (NFR-18) porque os
dois intérpretes compartilham a mesma ordem de aplicação: **where → orderBy →
skip → take**, fixada em um único lugar (`SelectSlice`, §3.2).

## 2. O seam: `runtime.Query[T]` (REQ-33, REQ-36, REQ-38)

### 2.1. Forma

```go
// rtsrc/collection.go.txt (evolução do tipo de H4)
type Query[T any] struct {
    // Where filtra por item; nil = todos. O error propaga e aborta a
    // operação inteira (REQ-36.1 — item inválido nunca é pulado em silêncio).
    Where func(T) (bool, error)
    // WhereEq é o subconjunto declarativo de Where: igualdades em AND sobre
    // campos nomeados (caminho de coluna). Preenchido pelo gerador SOMENTE
    // quando Where é exatamente essa conjunção; o adapter SQL o usa no lugar
    // de Where. Vazio ≠ "sem filtro": Where continua sendo a verdade.
    WhereEq []FieldEq // {Field string; Value any}
    // Less ordena (sort estável); nil = ordem de inserção. Error idem Where.
    Less func(a, b T) (bool, error)
    // OrderField/OrderDesc: descritor declarativo de Less quando a chave é
    // um acesso de membro simples. Mesma regra do WhereEq.
    OrderField string
    OrderDesc  bool
    // Skip/Take: -1 = ausente. Aplicados após a ordenação.
    Skip, Take int
}

func NewQuery[T any]() Query[T] // Skip/Take já em -1

type Collection[T any] interface {
    Add(ctx context.Context, item T) error
    Select(ctx context.Context, q Query[T]) ([]T, error)
    Count(ctx context.Context, q Query[T]) (int64, error)
}

// SelectSlice aplica q sobre um slice já materializado — o MESMO código que
// memoryCollection.Select usa por baixo. É o intérprete único da semântica
// (NFR-18) e o caminho de "load X(id).entries orderBy ..." (§3.4), que opera
// sobre a coleção de um aggregate carregado, sem Collection nenhuma.
func SelectSlice[T any](items []T, q Query[T]) ([]T, error)
```

### 2.2. Compatibilidade com H4

`List(ctx, pred)` e `Count(ctx, pred)` de hoje somem: os chamadores gerados
(H4 — Policy `list`/`count`, testes de §22.4) **regeneram** para
`Select`/`Count` com `Query[T]{Where: ...}`. A mudança de assinatura do
predicado (`func(T) bool` → `func(T) (bool, error)`) é o levantamento de G-8
(REQ-36.2): goldens afetados mudam uma vez, revisados (NFR-19). Um predicado
sem hoisting continua uma linha: `return cond, nil` (REQ-36.3).

## 3. Decisões de Design

### 3.1. Ordem semântica fixa das cláusulas (REQ-33.1)

`where → orderBy → skip → take`, independente da ordem textual — a ordem do
SQL, que é a expectativa de quem lê §6.3. `parseQueryClauses` preserva a
ordem textual; o **lowering** reagrupa. Cláusula duplicada (dois `orderBy`) é
erro de geração claro (NFR-20).

### 3.2. `orderBy`: comparabilidade decidida em tempo de geração (REQ-33.3)

A chave `K` (lowerizada num escopo-filho com o binding tipado, o MESMO
mecanismo de `hoistQueryPredicate`) decide o corpo de `Less` pelo tipo:

| Tipo de K | `Less` gerado |
|---|---|
| primitivo ordenável (`integer`/`decimal`/`string`/`datetime`/`duration`/`size`) | comparação nativa / `Decimal.Cmp` / `time.Before` |
| VO wrapper sobre primitivo ordenável | idem, sobre o valor embrulhado |
| Enum | comparação do valor base |
| VO composto com `Operator <` (ou `>`) declarado | dispatch do operador (falível → `Less` devolve o `error`) |
| qualquer outro | **erro de geração** nomeando o tipo e a alternativa (NFR-20) |

Descritor declarativo (`OrderField`) só quando K é `<binding>.<campo>` nu;
chave computada (ex. `x.a + x.b`) gera só a closure — o adapter SQL degrada
(§3.9).

### 3.3. Predicado falível (REQ-36) — mudança em `hoistQueryPredicate`

O corpo do predicado passa a ser um bloco: as linhas hoisted entram ANTES do
`return`, com `if err != nil { return false, err }` entre elas — exatamente o
que a limitação de G-8 dizia não caber em `func(T) bool`. A doc-nota de G-8
em `lower/stmt.go` é substituída pela nova forma.

### 3.4. `load X(id).entries` + cláusulas (REQ-33.2) — o caminho sem Collection

Quando o `Target` de um `load` é um **MemberExpr sobre a construção do
aggregate** (`Wallet(walletId).entries`), o lowering: (1) reusa `LoadCall`
para carregar o aggregate (código de E6.2, intocado); (2) monta a `Query[T]`
com `T` = tipo do elemento da coleção (via `types.Model`); (3) aplica
`runtime.SelectSlice(agg.state.Entries, q)`. Nenhuma `Collection[T]`
envolvida — a fonte é a coleção já materializada do state. `AppendList<T>`
é `runtime.AppendList[T]` (um slice por baixo): `SelectSlice` opera sobre o
slice interno; a "paginação sem cópia integral" de REQ-37.4 é `skip`/`take`
por reslicing pós-ordenação (sem ordenação, reslicing direto — custo zero).

### 3.5. `decl_query.go`: fim do "mínimo de E8.1" (REQ-33, REQ-34)

O fast-path de return-expr de `decl_query.go` deixa de recusar cláusulas:
`return <QueryExpr com cláusulas>` delega ao mesmo hoisting de corpo
(`StmtLowerer`) e retorna a variável materializada. A projeção `as V`
(REQ-34) reusa a rotina de mapeamento campo-a-campo de `load X(id) as V`
(E8.1) — inclusive o achatamento de VO composto (`amount_value` ←
`amount.value`), que já existe lá — agora aplicada por item, num loop de
projeção emitido após o `SelectSlice`/`Select`. O cache de Query (G3)
embrulha a função gerada por fora e não percebe a mudança.

### 3.6. Operador `in` (REQ-35.3/35.4)

`BinaryExpr(token.IN)` entra no dispatch de operadores de `lower/expr.go`
(§4.2 do design de codegen) como caso novo:

- RHS `ListExpr` literal → `slices.Contains([]T{...}, lhs)` (stdlib Go 1.21+,
  sem dep; `T` inferido do LHS — elementos são checados contra ele).
- RHS expressão de coleção → `slices.Contains(rhs, lhs)` quando o elemento é
  comparável nativamente (primitivo, wrapper, Enum); senão erro de geração.
- VO composto no LHS → erro de geração claro (igualdade estrutural em `in`
  não tem forma nativa; ninguém no spec usa).

### 3.7. `join` mesmo-banco (REQ-35.1/35.2)

Forma suportada (a do spec, e só ela nesta fase): `list A a join B b on
<membro-de-a> == <membro-de-b> [where C] [as V]`. Lowering:

1. Materializa as duas fontes (`Select` com query vazia — filtros vão no
   `where`, não no `on`).
2. Loop aninhado com as duas variáveis de alias tipadas (escopos-filho, o
   mesmo `TypeEnv.Child` de sempre); `on` vira o `if` de correlação; `where`
   vira um segundo `if` no corpo (com hoisting falível normal — aqui não há
   assinatura de closure para limitar).
3. A projeção `as V` resolve cada campo da View contra os aliases **na ordem
   de declaração** (`a` primeiro, depois `b`); campo presente em ambos é
   ambiguidade → erro de geração (NFR-20). Sem `as`, o resultado é a lista do
   PRIMEIRO alias (o "SELECT t.*" implícito do exemplo do spec).
4. `on` que não seja igualdade entre membros dos dois aliases → erro de
   geração claro. `orderBy`/`skip`/`take` pós-join aplicam `SelectSlice`
   sobre o resultado projetado.

O(n·m) in-memory é aceitável e documentado (o front-end já garante
mesmo-banco; o adapter SQL pode traduzir para JOIN real num ciclo futuro —
fora de escopo, registrado em §5).

### 3.8. Métodos de coleção (§20): `distinct`/`sum`/`focus` (REQ-37)

Entram na tabela de métodos embutidos (E1.3) como **métodos que exigem
lambda tipada** — o lowering usa `Lowerer.Lambda(le, paramGoType)` (existente
desde E5, à espera disto). Todos os três são **hoisted** (produzem linhas +
uma variável de resultado), nunca expressão pura — `sum` pode falhar.

- **`col.distinct(x => x.k)`** → loop com `map[K]struct{}` de vistos + slice
  de resultado na ordem de 1ª aparição (NFR-13). `K` precisa ser chave de map
  válida E comparável do jeito que o domínio espera: primitivo, VO wrapper,
  Enum. VO composto → erro de geração.
- **`col.sum(x => x.v)`** → fold: acumulador inicializado com o `v` do
  primeiro item, somado item a item. `v` numérico primitivo → `+` nativo;
  `v` VO com `Operator +` (consulta ao `goname.VOOperatorRegistry`, o mesmo
  do dispatch §4.2) → `acc, err = acc.Add(...)` com propagação; sem `+` →
  erro de geração. **Coleção vazia → zero value de `v`** (documentado: para
  `Money` isso é `Money{}`; o spec usa `sum` em comparações `ensure`, onde o
  zero value se comporta como esperado — quem precisar de identidade
  específica trata o `count == 0` antes).
- **`state.col.focus(id)`** → busca linear pelo item cujo campo **`id`** casa
  com o argumento (a convenção do §20; VO do item sem campo `id` → erro de
  geração nomeando a limitação). Devolve o item + a semântica de ausência
  de `ensure ... exists` (E5.3) quando combinado.
- Os três aceitam como receptor: campo de coleção do state, resultado de
  `list` (a variável materializada — ex. `soldTickets.distinct(...)`, a forma
  da Policy §7), ou parâmetro de coleção.

### 3.9. Descida SQL (REQ-38) — só a parte declarativa, com pós-filtro

O adapter sqlite (G1) ganha a contraparte de `Collection[T]` sobre tabela:
`Select` monta `SELECT ... [WHERE <WhereEq como AND de "col = ?">]
[ORDER BY <OrderField> [DESC]] [LIMIT/OFFSET]` **apenas** com os descritores
declarativos presentes; o que não desceu (closure `Where` além do `WhereEq`,
`Less` sem `OrderField`) roda como pós-processamento in-memory via o MESMO
`SelectSlice` — por construção, mesma semântica (NFR-18). Regra de correção:
quando `Less` não desce, `Skip`/`Take` **também não descem** (paginar antes
de ordenar estaria errado); o adapter puxa o conjunto filtrado e delega o
resto. `Count` com só `WhereEq` desce como `SELECT COUNT(*)`; `sum` fica
in-memory nesta fase com o seam pronto para descer depois (REQ-38.4 permite).

### 3.10. Inferência de tipos (`lower/env.go`)

`TypeEnv` aprende: (a) `load X(id).<campo-coleção>` → tipo da coleção do
state; (b) query com cláusulas preserva o tipo do item (where/orderBy/skip/
take) e `as V` troca para `List<V>` (já existia para a forma simples);
(c) `distinct` → `List<K>` com `K` = tipo do corpo da lambda; (d) `sum` →
tipo do corpo da lambda; (e) `focus` → tipo do elemento. Nó desconhecido
continua falha explícita (contrato de E5.0).

## 4. Alternativas Rejeitadas

| Decisão tomada | Alternativa rejeitada | Por quê |
|---|---|---|
| `Query[T]` híbrida (closure + descritor) | AST de expressão interpretada no runtime | Runtime viraria um mini-interpretador (contra "sem runtime de DomainScript interpretado", req codegen §1.3); closures mantêm o Go legível (NFR-11) |
| Ordem semântica fixa where→orderBy→skip→take | Respeitar ordem textual das cláusulas | A ordem textual variando semântica seria uma pegadinha; SQL é a expectativa de quem lê §6.3 |
| Erro propagado aborta a query (REQ-36.1) | Pular item com erro | Item pulado em silêncio é resultado incorreto indetectável — viola a filosofia fail-fast do spec §1.1 |
| `sum` vazio → zero value, documentado | Erro em runtime para coleção vazia | O uso canônico (§20) é comparação em `ensure`; falhar em runtime criaria um erro de infra onde o domínio esperava um booleano |
| Join in-memory O(n·m), erro para `on` não-igualdade | Hash-join genérico / condições arbitrárias | Escopo mínimo que cobre o spec; otimização sem exemplo real é especulação (mesma disciplina de todo o ciclo E–H) |
| `focus` pela convenção do campo `id` | Parâmetro de configuração de chave | §20 não tem sintaxe para declarar a chave; inventá-la seria mudar a linguagem — quando o spec evoluir, o erro de geração aponta o caminho |

## 5. Fora de Escopo Registrado (para o próximo gaps)

- JOIN traduzido para SQL real no adapter (hoje: materializa e correlaciona
  in-memory mesmo no backend SQL).
- `sum`/`distinct` como agregação SQL (`SELECT SUM/DISTINCT`) — o seam
  permite; entra quando houver medição que justifique.
- `avg`/`min`/`max`/`group by` (spec §25, planejado).
- `in` com subquery.

## 6. Estratégia de Testes (NFR-17..20)

- **Runtime:** `collection_test.go.txt` cresce com `Select`/`SelectSlice`
  (ordem semântica, estabilidade do sort, skip/take nas bordas, erro de
  predicado abortando, `WhereEq`/`OrderField` ignorados pelo in-memory).
- **Lowering:** testes sintéticos por cláusula/método em `codegen/lower/`
  (mesmo padrão de `builtins_test.go`), incluindo os erros de geração de
  NFR-20 (um teste por mensagem).
- **Golden + smoke pareados** (NFR-17) por fatia, como sempre.
- **Comportamentais-âncora (§1.4 dos requirements):** fixtures de
  `GetStatement`, `GetMyTickets` e da Policy §7 canônica geram, compilam e
  rodam `go test` verde sobre o projeto gerado inteiro.
- **Pareados in-memory × SQL (NFR-18):** a MESMA query executada nos dois
  backends com o MESMO seed de dados compara resultados — inclusive um caso
  que força a degradação (closure não-descível) para provar REQ-38.2.
- **Regressão (NFR-19):** wallet/shop/goldens existentes; onde o golden mudar
  (assinatura do predicado, §2.2), o diff é revisado no commit.
