# Spec v7: `sum()` sobre ValueObject e `focus()` sem semântica de ausência (§22 contra o novo §2.8)
- SPEC: [domainscript-spec-v7](../steerings/domainscript-spec-v7/README.md)
  (revisão da especificação)
- TASK: — (achado durante a revisão que fechou as cinco issues de spec v7;
  ver [§2.8](../steerings/domainscript-spec-v7/02-type-system.md))
- DESCRIPTION: A [§2.8](../steerings/domainscript-spec-v7/02-type-system.md)
  (catálogo normativo de métodos embutidos, escrito nesta revisão) fechou o
  contrato de `sum` e o conjunto de operações de coleção. Duas linhas da
  [§22](../steerings/domainscript-spec-v7/22-smart-partial-loading.md) — que
  é o exemplo canônico de Smart Partial Loading, e tem só seis linhas — ficaram
  contra esse contrato. Nenhuma das duas foi corrigida ao vento porque ambas
  exigem decisão de linguagem, não ajuste de exemplo.

  1. **`state.items.sum(i => i.price)` projeta um ValueObject.** A §2.8
     restringe a projeção de `sum(f)` a `integer`/`decimal` — o argumento
     escrito lá é que a coleção vazia precisa de um zero, e um VO arbitrário
     não tem um. Mas `price` é `Money` em todo o resto da spec, `Money` declara
     `Operator +` ([§2.2](../steerings/domainscript-spec-v7/02-type-system.md)),
     e somar dinheiro é exatamente o caso de uso que a §22 ilustra. As três
     saídas: (a) manter `sum` numérico e reescrever a §22 para projetar
     `i.price.amount` — barato, mas devolve `decimal` e joga a moeda fora,
     que é precisamente o que o VO `Money` existe para impedir; (b) estender
     `sum` a VOs que declarem `Operator +`, e então **decidir o valor de
     coleção vazia** (erro em runtime? um `Zero`/`identity` declarável no VO?
     proibir `sum` sobre coleção potencialmente vazia?); (c) introduzir uma
     forma que exija semente explícita (`sum(0, f)` / `fold`). A mesma linha
     aparece em
     [`docs/examples/03-aplicacao-e-leitura/read.ds:125`](../../examples/03-aplicacao-e-leitura/read.ds)
     (`sum(...) < Price(...)`), o que confirma que a intenção sempre foi somar
     VO monetário, não `decimal`.
     Nota secundária da mesma linha: `< 10000` compara o resultado com um
     literal numérico nu. Se (b) ou (c) vencer, o literal precisa virar
     `Money(...)`; se (a) vencer, ele já é legal.

  2. **`state.items.focus(itemId)` não tem semântica de ausência.**
     `focus` só aparece na §22, sem assinatura e sem contrato. A §2.8 aponta
     para lá e assume que a ausência é tratada por `ensure ... exists`, mas a
     §22 nunca diz isso — e a §2.8 fechou a porta pela qual isso normalmente
     se resolveria: não há indexação, não há `null`, não há `Option`, e
     `Map.get` foi definido como total (`get(k, default)`) justamente para
     evitar o assunto. `focus` é hoje a única operação da linguagem que
     seleciona um elemento por identidade dentro de uma coleção, e portanto a
     única que pode falhar em encontrá-lo. A spec precisa dizer: assinatura,
     tipo de retorno, o que acontece quando não há elemento (`ensure ... exists`
     como `load`? erro de negócio declarável? erro de infra?), e se `focus` vale
     em qualquer coleção ou só em `AppendList<T>` dentro de `state`.
- SOLVED: FALSE

# Solução proposta

## Recomendação — `sum`

**Saída (a): `sum` continua numérico.** A projeção é `integer`/`decimal`, e a
§22 passa a somar `i.price.amount` sob uma invariante de moeda explícita.

A justificativa não é economia: é que (b) destrói a única propriedade que a §22
existe para prometer. `sum` vira `SELECT SUM(...)` **sem materializar** a
coleção; `Operator +` é código DomainScript arbitrário — no caso de `Money`, um
`ensure self.currency == other.currency else CurrencyMismatchError` no meio do
fold. Nenhum banco executa isso, e um `+` de VO nem sequer é uma operação
associativa conhecida do planejador. Sob (b), portanto, a mesma expressão teria
duas semânticas: empurrada, soma cega de `amount` sem checar moeda; não
empurrada, `Error` de negócio na primeira divergência, na ordem de iteração.
Isso não é otimização — é o pushdown mudando *se o programa falha*, o que
inviabiliza o fallback da §22 ("carrega aggregate todo") como equivalente
observacional. Uma agregação também não tem onde pôr a falha: `Operator +` soma
**dois** valores num ponto do programa que tem dono, e a §23 admite duas classes
de erro — o erro nascido no meio de um fold não é declarável em nenhuma.

O zero é o segundo prego, não o primeiro: `Money` não tem zero único (a moeda é
parte do valor), e as três saídas para isso são todas piores que (a) — erro em
runtime abre um modo de falha que a invariante de totalidade da
[§2.8.3](../steerings/domainscript-spec-v7/02-type-system.md) proíbe e que a §23
não sabe classificar; um `Zero` declarável no VO cria uma **segunda** extensão de
tipo além de `Operator`, contra a
[§2.8.1](../steerings/domainscript-spec-v7/02-type-system.md); proibir `sum`
sobre coleção potencialmente vazia exige uma análise de não-vacuidade que a
linguagem não tem como expressar.

E a objeção de que (a) "joga a moeda fora" se responde invertendo-a: a moeda não
deve morar dentro de um operador que falha silenciosamente no meio de uma soma —
deve ser uma guarda visível, na forma canônica da linguagem
([§3.1](../steerings/domainscript-spec-v7/03-control-flow.md)), e que também é
empurrável (`NOT EXISTS`).

**Texto normativo — [§2.8.8](../steerings/domainscript-spec-v7/02-type-system.md),
linha de `sum` da tabela de consultas:**

> | `sum(f)` | `integer` / `decimal` | Soma da projeção, no tipo da projeção; coleção vazia → `0` |

**Texto normativo — §2.8.8, substituindo o parágrafo que hoje começa em "`f` é a
forma `x => <expressão>`":**

> `f` é a forma `x => <expressão>` da [§22](22-smart-partial-loading.md): expressão
> pura sobre o item, sem mutador e sem `focus`. A projeção de `sum` deve ter tipo
> `integer` ou `decimal` — nunca ValueObject, nem wrapper sobre numérico (a base de
> um wrapper é opaca fora do corpo do VO, [§2.8.1](02-type-system.md)). Projeção de
> outro tipo → **erro de compilação**. Não existe forma com semente (`sum(0, f)`)
> nem `fold`/`reduce` ([§2.8.11](02-type-system.md)): `sum` é total porque o zero de
> `integer`/`decimal` é único, e é empurrável para `SELECT SUM(...)`
> ([§22](22-smart-partial-loading.md)) porque `+` numérico é o `+` do banco.
>
> **Somar valor monetário.** Projete o campo numérico e escreva a unidade como
> invariante explícita, nunca como efeito de um `Operator`:
>
> ```ds
> ensure state.items.all(i => i.price.currency == limit.currency) else CurrencyMismatchError
> ensure state.items.sum(i => i.price.amount) < limit.amount else CartLimitExceeded
> ```
>
> `sum` **nunca** invoca `Operator +` de ValueObject ([§2.2](02-type-system.md)):
> um operador soma dois valores num ponto que tem dono para a falha; uma agregação
> não tem esse ponto, e a falha não sobreviveria ao pushdown. Quem precisa do VO no
> resultado o reconstrói (`Money(amount: ..., currency: limit.currency)`) depois de
> garantida a invariante. Tipo que se pretenda somável é ValueObject **composto**
> com campo numérico, como `Money.amount`; um wrapper (`ValueObject
> Quantity(integer)`) não é projetável e portanto não é somável — por construção.

## Por que as outras perdem

| Saída | Por que perde |
|-------|---------------|
| (b) `sum` sobre VO com `Operator +` | **Intraduzível para SQL** — é o argumento decisivo. `Operator +` é código de domínio com `ensure`/`Error` dentro; `SELECT SUM` não o executa. A expressão passaria a ter duas semânticas conforme o pushdown ocorra ou não, e o fallback da §22 deixaria de ser equivalente observacional. Some-se a isso o zero inexistente de `Money` e a ordem de iteração virando parte da semântica de erro |
| (c) semente explícita (`sum(0, f)` / `fold`) | Resolve o zero e **nada mais**: um fold semeado sobre operador falível continua sem tradução para SQL. Além disso cria duas construções para a mesma operação, contra "Uma Forma Canônica" ([§1.1](../steerings/domainscript-spec-v7/01-overview.md)), e `fold` é `reduce` com outro nome — que a [§2.8.11](../steerings/domainscript-spec-v7/02-type-system.md) fecha explicitamente |

Nota de implementação que corrobora: a linha da §22 hoje **não gera**, mesmo com
(b) implementado — `codegen/lower/smartpartial.go` exige `Operator +` declarado
no VO projetado, e o `ValueObject Price` de
[`docs/examples/03-aplicacao-e-leitura/read.ds:102`](../../examples/03-aplicacao-e-leitura/read.ds)
não declara nenhum. O caminho VO de `sum` no back-end nunca teve exemplo
válido na spec; tem só um erro de geração.

## Recomendação — `focus`

`focus` é `load` para dentro de uma coleção, e deve ser especificado como tal —
mesma forma, mesmo guard, mesma classe de erro. É o que a
[§2.8.3](../steerings/domainscript-spec-v7/02-type-system.md) já assume
("`ensure x exists else <Error>` no caso de `load`/`focus`") e nunca definiu.

| Aspecto | Decisão |
|---------|---------|
| Assinatura | `focus(k)` — exatamente um argumento; nunca lambda |
| Receptor | `List<T>`, `AppendList<T>`, `Set<T>` — em qualquer contexto, não só em `state`. A semântica não pode depender de onde a coleção mora; o pushdown é que depende (§22) |
| `T` admissível | ValueObject **composto** que declare um campo `id`; qualquer outro `T` → ❌ erro |
| Tipo de `k` | Exatamente o tipo declarado do campo `id` de `T`, sem coerção — mesma regra de `load` ([§2.7](../steerings/domainscript-spec-v7/02-type-system.md)) |
| Retorno | `T` — **carregamento**, não valor: só pode aparecer como lado direito de uma atribuição |
| Correspondência | Primeiro elemento com `id == k` na ordem de iteração da [§2.8.8](../steerings/domainscript-spec-v7/02-type-system.md) |
| Ausência | `ensure <nome> exists else <Error>` obrigatório, dominando todo uso do nome — erro de **negócio**, 4xx ([§23](../steerings/domainscript-spec-v7/23-error-classification.md)); nunca infra, nunca falha de runtime |

Restringi-lo a `AppendList<T>` em `state` seria amarrar semântica a storage: a
mesma expressão passaria a existir ou não conforme o campo fosse persistido. O
que é privilégio de `state` é a tradução, não o significado — exatamente a
divisão que a §22 já faz com o fallback.

**Texto normativo — [§22](../steerings/domainscript-spec-v7/22-smart-partial-loading.md),
substituindo a seção inteira:**

> # 22. Smart Partial Loading
>
> O `state` de um Aggregate persistido em `Database` ([§13](13-module-infra.md)) não
> é materializado inteiro quando a operação toca só uma parte dele. A tradução é
> **otimização**: a semântica observável é a da [§2.8](02-type-system.md), e o
> fallback — carregar a coleção e executar em memória — é sempre legal e sempre
> equivalente.
>
> ```ds
> Handle Inspect(itemId ItemId, limit Money) {
>     item = state.items.focus(itemId)          // SELECT * WHERE parent_id = <self.id> AND id = <itemId>
>     ensure item exists else CartItemNotFound
>
>     ensure state.items.all(i => i.price.currency == limit.currency) else CurrencyMismatchError
>     ensure state.items.sum(i => i.price.amount) < limit.amount else CartLimitExceeded   // SELECT SUM(amount) ...
>
>     emit ItemInspected(self.id, item.id)
> }
> ```
>
> **`focus(k)`** — seleção pontual por identidade em `List<T>`/`AppendList<T>`/`Set<T>`.
> Vale sempre que `T` é ValueObject composto com um campo chamado `id`; `id` é a
> chave, e é convenção fixa — não há sintaxe para eleger outra. `k` tem o tipo
> declarado desse campo, sem coerção. O resultado é um **carregamento**, não um
> valor: só pode ser ligado a um nome (`item = state.items.focus(itemId)`); em
> qualquer outra posição — argumento, operando, corpo de lambda — → ❌ **erro de
> compilação**. O nome ligado só é utilizável depois de `ensure <nome> exists else
> <Error>` que domine todo uso; uso sem esse guard → ❌ **erro de compilação**. Não
> encontrar é erro de **negócio** ([§23](23-error-classification.md)), declarado
> pelo domínio — `focus` não tem modo de falha próprio, e por isso não fere a
> totalidade da [§2.8.3](02-type-system.md). `exists` só se aplica a nome ligado por
> `load` ou `focus`. Coleção sem elemento correspondente é o caso normal, não
> exceção; elemento repetido não existe pela chave, e havendo, vence o primeiro da
> ordem de iteração ([§2.8.8](02-type-system.md)).
>
> **Empurrável para o banco:** `focus`, `count`, `isEmpty`, `contains`, `any`, `all`,
> `sum`, e o par `skip`/`take` sobre `AppendList<T>` (paginação nativa). A condição é
> que o receptor seja campo de `state` de Aggregate com `storage.state` em `Database`,
> e que a lambda seja comparação entre acessos a campo do item, literais e valores já
> ligados. Fora disso, e em qualquer provider que não preserve o envelope aritmético
> da [§2.8.9](02-type-system.md), vale o fallback. Nenhum método fora dessa lista é
> empurrado; agregações adicionais estão em [§27](27-evolving-features.md).

## Consequências

- **[§2.8.8](../steerings/domainscript-spec-v7/02-type-system.md)**: linha de `sum`
  reescrita, parágrafo de projeção substituído, linha de `focus` da tabela de
  consultas mantida apontando para a §22 — que agora tem o contrato.
- **§22**: deixa de ser exemplo e passa a ser seção normativa (contrato de `focus`,
  lista fechada do que é empurrável, fallback como equivalência observacional).
- **[§25.3](../steerings/domainscript-spec-v7/25-compilation-rules.md)**: a linha
  "`sum(f)` com projeção não numérica" ganha "(inclusive ValueObject wrapper)", e
  entram cinco regras novas — `focus` sobre coleção cujo elemento não declara `id`;
  argumento de `focus` de tipo diverso do campo `id`; `focus` fora do lado direito
  de uma atribuição; uso de nome ligado por `load`/`focus` sem `ensure ... exists`
  dominante; `exists` sobre nome não ligado por `load`/`focus`. As duas últimas
  regulam **`load` também** — hoje a spec só as pratica nos exemplos, nunca as
  escreveu, e sem elas a §2.8.3 ("ausência não é valor") não é verificável.
- **[`docs/examples/03-aplicacao-e-leitura/read.ds`](../../examples/03-aplicacao-e-leitura/read.ds)**:
  o `Handle Inspect` passa a `ensure item exists else CartItemNotFound` após o
  `focus`, e a linha 125 vira o par `all(...)` + `sum(i => i.price.amount) <
  limit.amount`. `ValueObject Price` continua sem `Operator +` — e agora isso está
  certo, não faltando.
- **Implementação**, o que passa a dever: (1) `types`/`sema` ganham o catálogo de
  coleção — hoje o front-end não valida chamada de método alguma sobre coleção
  (`codegen/lower/env.go:344-350` documenta a permissividade como deliberada), então
  `sum`/`focus` são checados apenas na geração, contra o contrato "erro de
  compilação detectado no `check`, nunca na geração" da §2.8; (2) o ramo `*types.VOType`
  de `buildSumAccumulate` (`codegen/lower/smartpartial.go:257-269`) é removido — soma
  via `Operator +` deixa de existir; (3) a regra de dominância de `ensure ... exists`
  fecha um defeito real e não hipotético: `hoistFocus`
  (`codegen/lower/smartpartial.go:409-449`) devolve `*T` nil quando não encontra, e um
  `item.id` sem guard vira desreferência de ponteiro nil no Go gerado; (4) a checagem
  de "campo `id` no item" migra de erro de geração para diagnóstico do `check`; (5) o
  pushdown de `any`/`all` é trabalho novo no read-side — a equivalência com o fallback
  é o critério de aceite, não a performance.

## O que fica em aberto

1. **`ensure ... else <Error>` em Query.** A tabela da
   [§3.1](../steerings/domainscript-spec-v7/03-control-flow.md) lista Handle/UseCase,
   Policy/Worker e "dentro de `for`" — **não** lista Query nem `visibility`. Mas a
   [§2.5](../steerings/domainscript-spec-v7/02-type-system.md) escreve exatamente
   isso dentro de uma `Query`, e a
   [§2.8.10](../steerings/domainscript-spec-v7/02-type-system.md) libera `focus` em
   Query. Ou a §3.1 ganha a linha, ou `load`/`focus` ficam proibidos em Query — não
   posso decidir por você porque a escolha muda o que uma Query pode retornar (4xx de
   leitura), e isso é decisão de produto, não de coerência interna.
2. **Chave de `focus` por convenção de nome.** Recomendo o campo `id` fixo (é o que o
   back-end já faz e é a única forma sem sintaxe nova). Um marcador explícito no VO
   (`key id ItemId`) seria mais honesto e é uma adição de gramática — fora do escopo
   desta issue, e sua se você a quiser.
3. **`distinct`.** É o terceiro método implementado junto com `sum`/`focus`
   (`codegen/lower/smartpartial.go`), aparece na
   [§6.3](../steerings/domainscript-spec-v7/06-read-side.md) como operação de `Query`,
   e **não** está no catálogo fechado da §2.8 — logo, como método de coleção, hoje não
   existe. Ou a §2.8.8 o lista, ou o back-end o perde. Não toquei nisso: é uma terceira
   decisão, merece issue própria.
