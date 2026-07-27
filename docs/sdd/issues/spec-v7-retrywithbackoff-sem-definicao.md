# Spec v7: `RetryWithBackoff(3)` usado na §19.2 e definido em lugar nenhum
- SPEC: domainscript-spec-v7 (revisão da especificação)
- TASK: review-v7.md §A-4 (achado ao rodar a Saga canônica da §19.2)
- DESCRIPTION: A `Saga PurchaseTickets` da §19.2 usa `onInfraError {
  RetryWithBackoff(3) }` em dois dos três steps. `RetryWithBackoff` não aparece
  em nenhuma outra seção: não está na §2.6 (funções built-in), não é construto
  de topo, não é método de nenhum tipo, e a §19 não descreve a gramática do
  bloco `onInfraError` — só o nomeia na frase "Steps com `up`/`down`/
  `onInfraError`". Rodando o exemplo verbatim, o `dsc` do HEAD dá
  `error[E100]: nome não declarado: "RetryWithBackoff"`.
  **A spec precisa definir** o vocabulário de `onInfraError`: quais ações
  existem (só retry com backoff? `Compensate`? `Abort`? `Ignore`?), a
  assinatura de cada uma (`RetryWithBackoff(3)` — 3 é número de tentativas, e o
  backoff é qual? a §13 usa a forma declarativa `retry: { attempts: 3, backoff:
  "exponential" }` para Database, que é vocabulário diferente para a mesma
  ideia), e se o bloco aceita statements arbitrários ou é uma lista fechada de
  ações declarativas. Vale considerar alinhar com a forma já usada na §13, em
  nome da "Uma Forma Canônica" da §1.1 — hoje a linguagem tem duas grafias para
  configurar retry.
  Nota de escopo: o `unrecoverable` de `down { unrecoverable }` (mesma §19.2)
  **está** implementado (`codegen/decl_saga.go`), então a lacuna é só do
  `onInfraError`.
- SOLVED: FALSE
