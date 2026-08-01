CODIGO: usecase-idempotency-required-intestavel-test-ds
CATEGORIA: Dependente de decisão do desenvolvedor

Issue original: [[docs/sdd/issues/usecase-idempotency-required-intestavel-test-ds]]

## Resumo da issue

Um `UseCase` com `idempotency { required: true }` não pode ser testado pelo
formato nativo `*.test.ds`: a gramática de cenário não tem como fornecer uma
`Idempotency-Key`, então todo cenário que exercita esse `UseCase` falha com "an
Idempotency-Key is required for this operation" em vez de provar o
comportamento de negócio. O runtime já sabe RECEBER a chave; quem não sabe
EMITI-LA é o gerador de testes, porque o cenário não tem onde descrevê-la.

## Evidencias

- `codegen/gentest.go:915` monta o `ctx` de todo cenário numa linha só, com
  `caller` e nada mais (`runtime.WithCaller(context.Background(),
  runtime.NewTestCaller("test-caller"))`); nenhuma chamada a
  `WithIdempotencyKey` em `codegen/gentest*.go`.
- A guarda que quebra os cenários: `codegen/usecase_idempotency.go:309-316`
  (`key, hasKey := runtime.IdempotencyKeyFrom(ctx)`; `if !hasKey { return
  runtime.ErrIdempotencyKeyRequired }`); mensagem em
  `codegen/rtsrc/errors.go.txt:60`.
- Reproduzido com `required: true` nos dois `UseCase` do wallet: 3 cenários
  quebraram — `TestPerformDeposit_DepositoBemSucedidoCommita`,
  `TestPerformDeposit_CarteiraNuncaCriadaFalhaENaoCommita`,
  `TestE2ESeedGivenEventThenPerformDepositWiresThroughRealStore`
  (este último em `driver/generate_e2e_wallet_test.go:253`, escrito à mão).
- Contorno hoje em produção: `testdata/projects/wallet/application.ds:41,50`
  declara `idempotency { window: 48h }` **sem** `required`, evitando o
  problema em vez de fechá-lo.
- Releitura completa de
  [[docs/sdd/steerings/domainscript-spec-v7/24-testing|24-testing.md]] (24.1–24.7)
  e [[docs/sdd/steerings/domainscript-spec-v7/15-idempotency|15-idempotency.md]]:
  a primeira não menciona `Idempotency-Key`/chave/contexto de chamada; a
  segunda não menciona `*.test.ds`. A revisão de
  [[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24]] de 2026-07-31 não
  tocou nisto.

## Impacto no projeto

O exemplo canônico da própria
[[docs/sdd/steerings/domainscript-spec-v7/15-idempotency|§15]]
(`UseCase PerformDeposit { idempotency { required: true, window: 48h } }`)
fica intestável pelo formato de teste nativo da linguagem — uma lacuna na
própria feature de testes
([[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24]]) que se manifesta
assim que alguém tenta usar
`required: true`, que é justamente a configuração recomendada para operações
sensíveis. Hoje o desfecho é o pior possível: o Go gerado compila e falha
vermelho com uma mensagem que se lê como regressão de domínio, em vez de um
erro de geração que aponte a causa real.

## Soluçoes possíveis

### Solucão 1

Duas etapas independentes. Etapa 1 (não depende de decisão de spec): o gerador
de testes recusa, com erro de geração claro, um `Test` sobre `UseCase` com
`idempotency { required: true }` — troca um "vermelho enganoso em runtime" por
um "erro de geração nomeado", seguindo o mesmo padrão que `gentest.go` já
aplica a outras formas de cenário não cobertas. Etapa 2 (depende da decisão de
spec, ver abaixo): fornecer a chave — via (a) o runner atribuindo uma chave
sintética determinística por cenário (uma linha em `emitUseCaseScenarioBody`,
no mesmo padrão de `propertySeed`), ou (b) gramática nova no `.test.ds` (`given
idempotencyKey "k1"` ou um bloco de contexto de chamada).

### Solução 2

Pular a Etapa 1 e ir direto para a rota (b) da Etapa 2 — gramática nova que
também resolve, no mesmo slot, o cenário de acesso NEGADO (mesma lacuna:
o cenário não descreve o contexto de chamada). É "estritamente melhor" segundo
a própria análise, porque é a única rota que permite testar a idempotência
**em si** (replay da mesma chave, conflito, corrida) — mas é ciclo de
front-end inteiro (lexer→parser→ast→sema→gentest), mais caro, e só faz sentido
depois que a spec decidir que o caminho é gramática nova em vez de chave
sintética do runner.

## O que precisa ser resolvido antes

A [[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24]] precisa decidir,
item a item (idealmente junto com o cenário de acesso NEGADO, mesma lacuna de
"contexto de chamada em cenário"):

1. O runner é "cliente" para efeito da
   [[docs/sdd/steerings/domainscript-spec-v7/15-idempotency|§15]]? Se sim, a
   chave existe sempre e `required: true` nunca falha em cenário; se não, o
   cenário precisa de uma forma explícita de fornecê-la
   ([[docs/sdd/steerings/domainscript-spec-v7/15-idempotency|§15]] hoje diz
   "sem fallback", o que aponta para "não").
2. Se a chave for atribuída pelo runner, qual o escopo dela — uma por cenário,
   ou uma por `when`? (Hoje `ScenarioDecl` só tem um `when`.)
3. Como se testa a idempotência em si — replay, conflito
   (`IdempotencyKeyConflict`), corrida (`wait`/`reject`)? Nenhum é expressável
   hoje porque não existe cenário com dois `when`.
4. Que linha nova entra em
   [[docs/sdd/steerings/domainscript-spec-v7/24-testing|§24.7]] para "cenário
   sobre `UseCase` com `idempotency { required: true }` sem chave" — erro,
   warning, ou comportamento definido (chave do runner)?

Nenhuma nota de desenvolvedor foi encontrada resolvendo essas perguntas (nem
embutida na issue, nem em `docs/notes/Issues/`). Issue irmã de mesma causa:
[[docs/sdd/issues/lacunas-nos-testes-gerados-test-ds]].
