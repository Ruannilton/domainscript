CODIGO: spec-v7-sum-e-focus-da-secao-22-contra-catalogo-de-metodos
CATEGORIA: Ajuste de especificação

Issue original: [[docs/sdd/issues/spec-v7-sum-e-focus-da-secao-22-contra-catalogo-de-metodos]]

## Resumo da issue

A [[docs/sdd/steerings/domainscript-spec-v7/22-smart-partial-loading|§22 (Smart Partial Loading)]],
escrita antes do catálogo normativo de métodos da
[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.8]], contradiz
esse catálogo em dois pontos: `sum(i => i.price)` projeta `Money` (um
ValueObject), mas a §2.8 só permite `sum` sobre `integer`/`decimal`; e
`state.items.focus(itemId)` não tem assinatura nem semântica de ausência
definida em lugar nenhum, embora seja a única operação da linguagem que pode
"não encontrar" um elemento. A mesma forma de `sum` sobre VO aparece em
`docs/examples/03-aplicacao-e-leitura/read.ds:125`, confirmando que não é
descuido pontual da §22.

## Evidencias

- [[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.8]] restringe a
  projeção de `sum(f)` a `integer`/`decimal` (a análise cita o motivo:
  coleção vazia precisa de um zero, e um VO arbitrário não tem um).
- `state.items.sum(i => i.price)`
  ([[docs/sdd/steerings/domainscript-spec-v7/22-smart-partial-loading|§22]])
  projeta `price`, que é `Money` — um ValueObject composto — em todo o resto
  da spec.
- `codegen/lower/smartpartial.go` hoje exige `Operator +` declarado no VO
  projetado para gerar a soma; `ValueObject Price` em
  `docs/examples/03-aplicacao-e-leitura/read.ds:102` não declara nenhum — ou
  seja, o caminho de VO em `sum` nunca teve um exemplo válido, só um erro de
  geração.
- `focus` só aparece na §22, sem assinatura, tipo de retorno ou contrato de
  ausência. A
  [[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.8.3]] já assume
  "`ensure x exists else <Error>` no caso de `load`/`focus`" mas nunca define
  isso para `focus` especificamente.
- `hoistFocus` (`codegen/lower/smartpartial.go:409-449`) devolve `*T` nil
  quando não encontra elemento — um `item.id` sem guard vira desreferência de
  ponteiro nil no Go gerado hoje, o que confirma que a lacuna não é só de
  texto, é um bug de runtime latente.

## Impacto no projeto

Enquanto a contradição não é resolvida, `sum` sobre VO monetário — o caso de
uso central que a §22 existe para ilustrar — não tem semântica válida:
qualquer implementação hoje falha na geração. `focus` sem contrato de ausência
deixa uma desreferência de ponteiro nil alcançável no Go gerado sempre que o
elemento buscado não existir, porque não há regra de compilação que exija o
guard antes do uso.

## Soluçoes possíveis
### Solucão 1
`sum` permanece estritamente numérico (`integer`/`decimal`); a §22 passa a
projetar `i.price.amount` sob uma invariante de moeda explícita
(`ensure state.items.all(i => i.price.currency == limit.currency) else
CurrencyMismatchError` antes do `sum`). É a saída que a análise recomenda com
justificativa técnica decisiva, não só estilística: estender `sum` a VOs com
`Operator +` (a alternativa natural) é **intraduzível para SQL** — `Operator
+` é código de domínio com `ensure`/`Error` potencialmente dentro, e
`SELECT SUM(...)` não executa isso; o pushdown passaria a mudar *se* o
programa falha, quebrando a equivalência entre a rota otimizada e o fallback
que é a própria razão de existir da
[[docs/sdd/steerings/domainscript-spec-v7/22-smart-partial-loading|§22]].
`focus(k)` ganha contrato completo,
modelado como "`load` para dentro de uma coleção": um argumento, receptor
`List<T>`/`AppendList<T>`/`Set<T>`, exige `T` ValueObject composto com campo
`id`, retorno é carregamento (só pode ser ligado a um nome), e todo uso do
nome deve ser dominado por `ensure <nome> exists else <Error>` — a mesma
disciplina de `load`. Ambos os textos normativos já estão redigidos na análise
da issue, prontos para colar em
[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.8.8]] e
[[docs/sdd/steerings/domainscript-spec-v7/22-smart-partial-loading|§22]].
### Solução 2
Estender `sum` a VOs que declarem `Operator +`, decidindo separadamente o
valor de coleção vazia (erro em runtime? `Zero`/`identity` declarável no VO?
proibir `sum` sobre coleção potencialmente vazia?). A própria análise descarta
essa rota pelo argumento de intraduzibilidade para SQL acima, e porque
qualquer resposta ao problema do zero é pior que a Solução 1: erro em runtime
viola a invariante de totalidade da
[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.8.3]]; um `Zero`
declarável cria uma segunda extensão de tipo de VO além de `Operator`, contra
a [[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.8.1]]; proibir
`sum` sobre coleção potencialmente vazia exige análise de não-vacuidade que a
linguagem não tem como expressar. Uma terceira variante, `sum(0, f)`/`fold`
com semente explícita, resolve só o problema do zero e mantém a
intraduzibilidade, além de violar "Uma Forma Canônica"
([[docs/sdd/steerings/domainscript-spec-v7/01-overview|§1.1]]) ao introduzir
uma segunda construção para a mesma operação.

## O que precisa ser resolvido antes

A decisão já está tomada pela própria análise da issue — não por nota
explícita do desenvolvedor, mas por eliminação técnica decisiva das
alternativas (intraduzibilidade para SQL descarta a rota de VO em `sum`; o
precedente de `load` decide a forma de `focus`). O que falta é trabalho
editorial: escrever em
[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|02-type-system.md §2.8.8]]
a linha revisada de `sum` (projeção restrita a `integer`/`decimal`,
coleção vazia → `0`, nunca invoca `Operator +`) e substituir integralmente
[[docs/sdd/steerings/domainscript-spec-v7/22-smart-partial-loading|22-smart-partial-loading.md]]
pela versão normativa (contrato de `focus`, lista fechada do que é
empurrável ao banco, fallback como equivalência observacional); atualizar
[[docs/sdd/steerings/domainscript-spec-v7/25-compilation-rules|25-compilation-rules.md §25.3]]
com as cinco regras novas de erro de compilação listadas na análise
(inclusive as duas que também passam a valer para `load`); e ajustar
`docs/examples/03-aplicacao-e-leitura/read.ds` para o novo par
`all(...)` + `sum(i => i.price.amount)` com `ensure item exists` após
`focus`.

Três itens ficam fora do escopo desta issue e não bloqueiam a adoção do texto
acima, mas precisam de acompanhamento:
- `ensure ... else <Error>` dentro de `Query` não está na tabela da
  [[docs/sdd/steerings/domainscript-spec-v7/03-control-flow|§3.1]] (que lista
  só Handle/UseCase, Policy/Worker e `for`), mas a
  [[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.5]] já a usa
  dentro de uma Query e a
  [[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.8.10]] libera
  `focus` em Query — a própria análise diz explicitamente não poder decidir
  isso ("a escolha muda o que uma Query pode retornar, isso é decisão de
  produto").
- A chave de `focus` por convenção de nome fixo (`id`) é a recomendação da
  análise, mas um marcador explícito de chave no VO (`key id ItemId`) fica
  registrado como extensão de gramática fora do escopo.
- `distinct` (implementado em `codegen/lower/smartpartial.go` junto com
  `sum`/`focus`, citado na
  [[docs/sdd/steerings/domainscript-spec-v7/06-read-side|§6.3]], mas ausente
  do catálogo fechado da
  [[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.8]]) é uma
  terceira decisão de mesmo formato, recomendada como issue própria.
