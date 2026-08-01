CODIGO: spec-v7-retrywithbackoff-sem-definicao
CATEGORIA: Ajuste de especificação

Issue original: [[docs/sdd/issues/spec-v7-retrywithbackoff-sem-definicao]]

## Resumo da issue

O exemplo da [[docs/sdd/steerings/domainscript-spec-v7/19-transactions-sagas|§19.2]] (Sagas) usa `onInfraError { RetryWithBackoff(3) }` em dois dos três steps, mas `RetryWithBackoff` não é definido em nenhum lugar da spec v7 — não é built-in, não é construto de topo, não é método de tipo nenhum, e a [[docs/sdd/steerings/domainscript-spec-v7/19-transactions-sagas|§19]] nem descreve a gramática do bloco `onInfraError`. Rodar o exemplo verbatim falha com `error[E100]: nome não declarado: "RetryWithBackoff"`. Já existe nota do desenvolvedor com a decisão de substituição.

## Evidencias

- [[docs/sdd/steerings/domainscript-spec-v7/19-transactions-sagas|19-transactions-sagas.md]] — `Saga PurchaseTickets` (§19.2) usa `onInfraError { RetryWithBackoff(3) }`.
- `error[E100]: nome não declarado: "RetryWithBackoff"` ao compilar o exemplo verbatim.
- [[docs/sdd/steerings/domainscript-spec-v7/13-module-infra|13-module-infra.md]] já usa uma forma declarativa diferente para a mesma ideia — `retry: { attempts: 3, backoff: "exponential" }` — em `Database`, o que dá à linguagem duas grafias para o mesmo conceito, contrariando a "Uma Forma Canônica" da [[docs/sdd/steerings/domainscript-spec-v7/01-overview|01-overview.md]] §1.1.
- Escopo confirmado: `unrecoverable` de `down { unrecoverable }` (mesma [[docs/sdd/steerings/domainscript-spec-v7/19-transactions-sagas|§19.2]]) **está** implementado (`codegen/decl_saga.go`) — a lacuna é só do `onInfraError`.
- Nota do desenvolvedor em [[docs/notes/Issues/spec-v7-retrywithbackoff-sem-definicao|docs/notes/Issues/spec-v7-retrywithbackoff-sem-definicao]]: "O RetryWithBackoff muito provavelmente era apenas uma indicação para tentar 3 vez após um erro de infra, pensando bem é melhor ter um atributo na saga (ex maxAttempts) para indicar quantas vezes a saga deve executar antes de iniciar o processo de down, e um comando para triggar o down automaticamente."

## Impacto no projeto

Um exemplo normativo da própria spec ([[docs/sdd/steerings/domainscript-spec-v7/19-transactions-sagas|§19.2]]) não compila como escrito — qualquer leitor que tente reproduzi-lo verbatim encontra um erro de nome não declarado. Enquanto o vocabulário de `onInfraError` não for definido, a linguagem não tem como expressar retry de infraestrutura em Saga, e a implementação (`codegen/decl_saga.go`) não tem o que gerar para esse bloco.

## Soluçoes possíveis

### Solucão 1

Seguir a decisão já registrada pelo desenvolvedor: abandonar `RetryWithBackoff(n)` como chamada e substituir por um atributo declarativo na própria Saga (ex.: `maxAttempts`) controlando quantas vezes a Saga executa antes de iniciar o processo de `down`, mais um comando para disparar o `down` automaticamente ao esgotar as tentativas. Isso também resolveria a duplicidade de grafia com a forma declarativa de retry já usada em `Database` ([[docs/sdd/steerings/domainscript-spec-v7/13-module-infra|§13]]), alinhando com a "Uma Forma Canônica" da [[docs/sdd/steerings/domainscript-spec-v7/01-overview|§1.1]].

### Solução 2

Não há segunda alternativa concorrente registrada — a análise da issue já apontava a forma declarativa de `Database` ([[docs/sdd/steerings/domainscript-spec-v7/13-module-infra|§13]]) como candidata a alinhamento, e a nota do desenvolvedor converge exatamente para essa direção (declarativa, não uma chamada de função). Manter `RetryWithBackoff(n)` como chamada e apenas defini-la (built-in de Saga) foi implicitamente descartada pela nota, que prefere um atributo na Saga.

## O que precisa ser resolvido antes

Decisão já tomada pelo desenvolvedor (ver nota em [[docs/notes/Issues/spec-v7-retrywithbackoff-sem-definicao|docs/notes/Issues/spec-v7-retrywithbackoff-sem-definicao]]): substituir `onInfraError { RetryWithBackoff(3) }` por um atributo declarativo (ex.: `maxAttempts`) na Saga, mais um comando/mecanismo para disparar o `down` automaticamente ao esgotar as tentativas. Falta o trabalho editorial: escrever o texto normativo em [[docs/sdd/steerings/domainscript-spec-v7/19-transactions-sagas|19-transactions-sagas.md]] §19.2 (gramática do atributo, sua interação com `up`/`down`, e o exemplo `PurchaseTickets` reescrito para não usar mais `RetryWithBackoff`), e então a implementação subsequente em `codegen/decl_saga.go`.
