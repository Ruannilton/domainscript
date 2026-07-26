# UseCase com `idempotency { required: true }` é intestável via `*.test.ds` (ex-ISSUE-13)
- SPEC: (nenhuma — achado ao atualizar `testdata/projects/wallet` para infra real)
- TASK: atualização do exemplo wallet (Postgres + Redis)
- DESCRIPTION: um `UseCase` com `idempotency { required: true, ... }` (§14)
  fica **intestável pelo formato nativo `*.test.ds`**: a gramática de cenário
  não tem como fornecer uma `Idempotency-Key`, então todo cenário que exercita
  esse UseCase falha com "an Idempotency-Key is required for this operation"
  em vez do comportamento de negócio que queria provar. Reproduzido ao pôr
  `required: true` nos dois UseCases do wallet: 3 cenários gerados quebraram
  (`TestPerformDeposit_DepositoBemSucedidoCommita`,
  `TestPerformDeposit_CarteiraNuncaCriadaFalhaENaoCommita`,
  `TestE2ESeedGivenEventThenPerformDepositWiresThroughRealStore`).
  O RUNTIME já sabe receber a chave — `runtime.WithIdempotencyKey(ctx, ...)`
  existe e é testada (`rtsrc/runtime_test.go.txt:TestWithIdempotencyKeyFrom`);
  quem não sabe EMITI-LA é o gerador de testes (`codegen/gentest.go`), que
  monta o ctx do cenário sem nenhuma chave. É a MESMA natureza do gap de
  "cenário de acesso NEGADO" (L2.6/REQ-53.7) e do `given caller` inexistente:
  o cenário não consegue descrever o *contexto de chamada*, só o estado e a
  ação. Duas rotas para fechar: (a) gramática nova no `.test.ds` (ex. `given
  idempotencyKey "k1"`), que é ciclo de front-end (mesma natureza da issue
  sobre features nunca modeladas pelo front-end); ou (b) o gerador injetar
  uma chave sintética determinística por cenário quando o UseCase exigir
  uma — mais barato, e suficiente para o cenário provar o comportamento de
  negócio (a idempotência em si continuaria coberta pelos testes de
  runtime). Contornado no wallet declarando `idempotency { window: 48h }`
  sem `required` — que também é o que APIs de pagamento reais fazem (o
  Stripe recomenda o header, não o exige), então o exemplo não perde
  realismo. O gap em si (o gerador de testes não consegue emitir uma
  Idempotency-Key sintética) segue aberto.
- SOLVED: FALSE
