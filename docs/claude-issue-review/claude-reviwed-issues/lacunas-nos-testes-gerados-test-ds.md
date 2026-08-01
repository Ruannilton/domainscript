CODIGO: lacunas-nos-testes-gerados-test-ds
CATEGORIA: Dependente de decisão do desenvolvedor

Issue original: [[docs/sdd/issues/lacunas-nos-testes-gerados-test-ds]]

## Resumo da issue

Os testes gerados a partir de `*.test.ds` (spec [[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24]]) cobrem só o caminho feliz; várias formas da seção têm semântica reduzida ou ausente: `then state`, cenário de acesso NEGADO, `mock ... returns X`, `emitted`/`released` dentro de passo de Saga, contra-exemplo mínimo de `property` e `rolledback` com reversão real. A revisão de 2026-07-31 fechou um item (`then state`), destravou normativamente três outros (contrato de resposta de Adapter, `emit`/`emitted` de ApplicationEvent, `compensate`) e trouxe três obrigações novas do [[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24.7]] que ninguém implementa hoje — mas dois pontos continuam genuinamente sem definição na spec.

## Evidencias

- `then state` — ✅ fechado: `emitAggregateThenState` (`codegen/gentest.go:438`) + golden `codegen/testdata/tests_thenstate_counter.go.golden`.
- Acesso NEGADO — aberto: `codegen/gentest.go:405`/`:915` e `codegen/gentest_property.go:319` sempre fixam um caller autenticado; a tabela [[docs/sdd/steerings/domainscript-spec-v7/24-testing|24-testing.md]] §24.7 não tem nenhuma linha sobre acesso.
- `mock ... returns X` — desbloqueado normativamente: [[docs/sdd/steerings/domainscript-spec-v7/09-notifications-adapters|09-notifications-adapters.md]] §9.4 agora define `Notification X { … } -> T`, bloco `response {}` e `function "F" -> T throws E`; falta implementar (`codegen/gentest.go:1344-1354`, `codegen/lower/builtins.go:340-341` ainda erro explícito).
- `emitted`/`released` em Saga — cindido: miscompilação silenciosa já fechada (`codegen/decl_saga.go:210-233`); `released` continua indefinido — uma única ocorrência em toda a v7 (`24-testing.md:68`, dentro de um exemplo) e zero em código.
- Shrinking de `property` ([[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24.5]]) — aberto, bloqueio puramente processual: `codegen/gentest_property.go:132-138` documenta a ausência; falta só corrigir `target_files` da task M4.1.
- `rolledback` real ([[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24.2]]) — aberto: `codegen/gentest.go:1011-1015` só checa `err == nil`; `memoryUnitOfWork.Run` (`codegen/rtsrc/uow.go.txt:109-122`) grava direto, sem staging — é a task M4.2, sem dependências.
- Itens novos do [[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24.7]]: metadata de evento em `given/when/then` hoje passa em silêncio com falso positivo (`gentest.go:1057` zera `EventMeta` antes do `reflect.DeepEqual`); `emitted <ApplicationEvent>` não implementável (`ApplicationEvent` tem zero ocorrências em código Go); `compensate` de `Error` inexistente não implementável (`compensate` não existe no front-end, só o verbo `compensated`, `parser/parse_testfile.go:240`).

## Impacto no projeto

Cenários que a spec descreve como testáveis ([[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24]]) não podem hoje ser expressos ou, pior, passam silenciosamente mesmo quando deveriam falhar (o caso da metadata de evento zerada antes da comparação é um falso positivo real, não apenas uma lacuna). Bloqueia o fechamento do Marco L/M (`correcoes-issues-6-8-12`) e mantém pendente a issue irmã [[docs/sdd/issues/usecase-idempotency-required-intestavel-test-ds|usecase-idempotency-required-intestavel-test-ds]], que compartilha a mesma causa raiz (o cenário de §24 não sabe descrever contexto de chamada).

## Soluçoes possíveis

### Solucão 1

Tratar a issue como três frentes disjuntas, como a "Solução proposta" já propõe: Frente A (`sema` fecha o que [[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24.7]] tornou estático — validação de envelope, sem gramática nova), Frente B (`codegen`/runtime fecha o que já está especificado — staging real para `rolledback`, shrinking determinístico), Frente C (ciclo de front-end com texto normativo já pronto — contrato de resposta de Adapter, `ApplicationEvent`, `compensate`). As três avançam sem esperar as duas questões ainda abertas.

### Solução 2

Manter o item como "oportunista, fechar quando o vizinho for tocado" (o critério anterior). Descartada pela própria análise: foi o que produziu dois Marcos (L e M) com metade das tasks bloqueadas — os seis itens têm naturezas incompatíveis (sema puro, runtime, gramática nova) e nenhum critério de pronto comum.

## O que precisa ser resolvido antes

1. O que o verbo `released` assere em `*.test.ds` ([[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24.3]]): liberação de recurso reservado por um passo compensado? asserção sobre `state` da Saga? sobre eventos de um Aggregate tocado pelo `down`? E o que a ausência de compensação implica?
2. Como o cenário de [[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24]] deve descrever o *contexto de chamada* — caller autenticado/negado, chave de idempotência — já que hoje só descreve estado e ação. (Detalhado na issue irmã [[docs/sdd/issues/usecase-idempotency-required-intestavel-test-ds|usecase-idempotency-required-intestavel-test-ds]].)

As demais frentes (A, B, e a parte de C que depende do contrato de resposta de Adapter/`ApplicationEvent`/`compensate`) já têm decisão normativa tomada na revisão de spec de 2026-07-31 e podem avançar sem esperar as duas perguntas acima.
