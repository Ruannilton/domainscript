# Spec v7: `RetryWithBackoff(3)` usado na §19.2 e definido em lugar nenhum
- SPEC: [domainscript-spec-v7](../steerings/domainscript-spec-v7/README.md)
  (revisão da especificação)
- TASK: [review-v7.md](../steerings/review-v7.md) — citada como "§A-4" ao
  registrar esta issue, mas `### A-4` em `review-v7.md` é sobre um achado
  diferente (`cmd` invisível em steps de Saga); `RetryWithBackoff` só
  aparece ali na tabela-resumo (linha da §19) e na tabela-índice ao final do
  documento, sem subseção `A-N` própria — referência corrigida aqui, sem
  editar `review-v7.md`.
- DESCRIPTION: A `Saga PurchaseTickets` da
  [§19.2](../steerings/domainscript-spec-v7/19-transactions-sagas.md) usa
  `onInfraError { RetryWithBackoff(3) }` em dois dos três steps.
  `RetryWithBackoff` não aparece em nenhuma outra seção: não está na
  [§2.6](../steerings/domainscript-spec-v7/02-type-system.md) (funções
  built-in), não é construto de topo, não é método de nenhum tipo, e a §19
  não descreve a gramática do bloco `onInfraError` — só o nomeia na frase
  "Steps com `up`/`down`/`onInfraError`". Rodando o exemplo verbatim, o `dsc`
  do HEAD dá `error[E100]: nome não declarado: "RetryWithBackoff"`.
  **A spec precisa definir** o vocabulário de `onInfraError`: quais ações
  existem (só retry com backoff? `Compensate`? `Abort`? `Ignore`?), a
  assinatura de cada uma (`RetryWithBackoff(3)` — 3 é número de tentativas, e o
  backoff é qual? a
  [§13](../steerings/domainscript-spec-v7/13-module-infra.md) usa a forma
  declarativa `retry: { attempts: 3, backoff: "exponential" }` para
  Database, que é vocabulário diferente para a mesma ideia), e se o bloco
  aceita statements arbitrários ou é uma lista fechada de ações declarativas.
  Vale considerar alinhar com a forma já usada na §13, em nome da "Uma Forma
  Canônica" da [§1.1](../steerings/domainscript-spec-v7/01-overview.md) —
  hoje a linguagem tem duas grafias para configurar retry.
  Nota de escopo: o `unrecoverable` de `down { unrecoverable }` (mesma §19.2)
  **está** implementado
  ([`codegen/decl_saga.go`](../../codegen/decl_saga.go)), então a lacuna é
  só do `onInfraError`.
- SOLVED: FALSE
