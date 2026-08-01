# UseCase com `idempotency { required: true }` é intestável via `*.test.ds` (ex-ISSUE-13)
- SPEC: (nenhuma — achado ao atualizar
  [`testdata/projects/wallet`](../../../testdata/projects/wallet) para infra
  real)
- TASK: atualização do exemplo wallet (Postgres + Redis)
- DESCRIPTION: um `UseCase` com `idempotency { required: true, ... }` (§14 em
  numeração v6, hoje
  [`15-idempotency.md`](../steerings/domainscript-spec-v7/15-idempotency.md))
  fica **intestável pelo formato nativo `*.test.ds`**: a gramática de cenário
  não tem como fornecer uma `Idempotency-Key`, então todo cenário que exercita
  esse UseCase falha com "an Idempotency-Key is required for this operation"
  em vez do comportamento de negócio que queria provar. Reproduzido ao pôr
  `required: true` nos dois UseCases do wallet: 3 cenários gerados quebraram
  (`TestPerformDeposit_DepositoBemSucedidoCommita`,
  `TestPerformDeposit_CarteiraNuncaCriadaFalhaENaoCommita`,
  `TestE2ESeedGivenEventThenPerformDepositWiresThroughRealStore`).
  O RUNTIME já sabe receber a chave — `runtime.WithIdempotencyKey(ctx, ...)`
  existe e é testada
  ([`rtsrc/runtime_test.go.txt:TestWithIdempotencyKeyFrom`](../../../codegen/rtsrc/runtime_test.go.txt#L615));
  quem não sabe EMITI-LA é o gerador de testes ([gentest.go](../../../codegen/gentest.go)), que
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

# Solução proposta

> Análise de 2026-07-31, contra o código de hoje (`a6b239b`) e a spec v7 já
> revisada (`d394745`). Instância concreta de
> [Lacunas nos testes gerados](lacunas-nos-testes-gerados-test-ds.md) — mesma
> causa (o cenário não descreve o **contexto de chamada**), inventário e
> fatiamento lá.

## Veredito

**Real, e nenhuma linha do caminho mudou.** Verificado hoje, ponta a ponta:

- O ctx de todo cenário de UseCase é montado numa linha só, com caller e nada
  mais: `ctx := runtime.WithCaller(context.Background(),
  runtime.NewTestCaller("test-caller"))` —
  [gentest.go:915](../../../codegen/gentest.go). Nenhuma chamada a
  `WithIdempotencyKey` em `codegen/gentest*.go`.
- A guarda que quebra os cenários continua exatamente onde a issue diz:
  [usecase_idempotency.go:309-316](../../../codegen/usecase_idempotency.go) —
  `key, hasKey := runtime.IdempotencyKeyFrom(ctx)`, e `if !hasKey { return
  runtime.ErrIdempotencyKeyRequired }` quando `plan.required`. A mensagem é a
  citada: `codegen/rtsrc/errors.go.txt:60`.
- O runtime segue sabendo receber a chave: `WithIdempotencyKey`
  (`codegen/rtsrc/contextkeys.go.txt:85-89`), com teste em
  `codegen/rtsrc/runtime_test.go.txt:615` — a linha citada confere.
- O contorno continua em pé: `testdata/projects/wallet/application.ds:41` e
  `:50` declaram `idempotency { window: 48h }` **sem** `required`.
- A explosão descrita é maior que os cenários gerados: além dos dois
  `TestPerformDeposit_*` emitidos de `testdata/projects/wallet/wallet.test.ds:62-76`,
  quebra o teste **escrito à mão**
  `driver/generate_e2e_wallet_test.go:253` (`TestE2ESeed…`), que chama
  `PerformDeposit(ctx, cmd)` com um ctx que só carrega caller
  (`generate_e2e_wallet_test.go:280`) e espera `ErrInactiveWallet`.
- A revisão de §24 de 2026-07-31 **não tocou nisto**: as quatro linhas novas de
  §24.7 são sobre mock/contrato de resposta, metadata de evento,
  `emitted <ApplicationEvent>` e `compensate`. Nada sobre chave de idempotência,
  nada sobre caller.

## Causa raiz

Um cenário de §24 descreve **estado** (`given`) e **ação** (`when`); não existe
lugar na gramática para o **contexto de chamada** — a `Idempotency-Key` que §15
exige do cliente, ou o caller que um `access` restritivo avalia. O gerador, sem
onde ler isso, fixa um caller sintético e omite a chave; um UseCase que exige a
chave passa a falhar por infraestrutura de teste, com mensagem de negócio.

## Solução proposta

**Duas etapas, nesta ordem — a segunda depende de uma decisão de spec que ainda
não existe.**

**Etapa 1 (implementável já, sem inventar semântica): erro de geração claro.**
Hoje o desfecho é o pior possível — o Go gerado compila, roda e falha vermelho
com `"an Idempotency-Key is required for this operation"`, que se lê como
regressão de domínio. A regra que este repositório já aplica a toda forma de
cenário não coberta ("erro de geração claro agora, nunca gerado silenciosamente
errado", cabeçalho de `gentest.go:40-42`) vale aqui igual. Concretamente:
`emitUseCaseScenarioBody` (`gentest.go:889`) — ou `emitUseCaseTestDecl`
(`gentest.go:862`), uma vez por Test em vez de por cenário — recusa o Test com
uma mensagem que nomeia o UseCase, o `required: true` e a lacuna de §24. A
resolução do flag **não** pode ser reimplementada: `required` pode vir do
`Idempotency { }` do `mod.ds` por herança (`usecase_idempotency.go:178-183`),
então extrair de `planUseCaseIdempotency` (`usecase_idempotency.go:164`) um
helper estreito — `usecaseRequiresIdempotencyKey(decl *ast.UseCaseDecl,
moduleBlock *ast.ConfigBlock) (bool, error)`, sem `Lowerer` (não há duração a
lowerizar) — e chamá-lo dos dois lados. Isso exige passar o bloco de módulo até
`EmitTests` (`gentest.go:182`), a partir do call site `codegen.go:831`, que já
tem o módulo em mão (`moduleIdempotencyBlock`, `usecase_idempotency.go:122`).

**Etapa 2 (depois da decisão de §24): fornecer a chave.** Duas formas, e a
escolha é da spec, não do gerador:
 - *(a) runner atribui, deterministicamente* — uma linha a mais em
   `gentest.go:915`: `ctx = runtime.WithIdempotencyKey(ctx, "<literal>")`, com o
   literal derivado em tempo de **geração** de `(Test.Name, índice/nome do
   cenário)`, exatamente como `propertySeed` (`gentest_property.go:404`) já
   deriva a semente da property por FNV-1a — determinismo de texto (NFR-13)
   preservado, regeneração byte-idêntica.
 - *(b) o cenário declara o contexto de chamada* — gramática nova (`given
   idempotencyKey "k1"`, ou um bloco de contexto que também carregue o caller),
   atravessando lexer→parser→`ast.ScenarioDecl`→sema→gentest. Mais caro, e
   estritamente melhor: **o mesmo slot fecha o cenário de acesso NEGADO**, hoje
   impossível pela mesma razão (`gentest.go:405`/`:915` fixam
   `runtime.NewTestCaller`), e é a única rota que permite testar a idempotência
   **em si** (replay da mesma chave, conflito, corrida).

Se a spec escolher (b), a Etapa 1 continua sendo o comportamento correto no
intervalo, e vira o caso "cenário não fornece chave para UseCase que a exige".

## Alternativas descartadas

- **Injetar a chave sintética já, sem decisão de spec** (rota (b) da descrição
  original desta issue): §15 diz "chave fornecida pelo cliente (**sem
  fallback**)" — um runner que sintetiza a chave *é* um fallback. Sem §24 dizer
  que o runner é cliente para esse efeito, é adivinhar semântica de linguagem, o
  que o [CLAUDE.md](../../../CLAUDE.md) proíbe explicitamente. Custo colateral:
  mudaria o ctx de **todo** cenário de UseCase, reescrevendo
  `codegen/testdata/tests_wallet.go.golden` sem que nenhum cenário precise.
- **Aceitar o contorno como solução** (declarar só `window`, sem `required`):
  é escolha de exemplo, não fechamento — o exemplo canônico do próprio §15
  (`UseCase PerformDeposit { idempotency { required: true, window: 48h } }`,
  `15-idempotency.md:16-21`) continuaria intestável pelo formato nativo, e §24.2
  testa justamente UseCases.
- **Emitir `t.Skip` no cenário afetado:** silencia sem diagnóstico — o mesmo
  padrão "aceita e ignora" que a issue de `visibility` de View já registra como
  o pior desfecho possível.
- **Considerar a idempotência coberta pelos testes de runtime**
  (`rtsrc/runtime_test.go.txt`): esses provam o mecanismo, e não são o problema.
  O problema é o cenário de **negócio** ficar vermelho por causa dele.
- **Relaxar `required` no wrapper quando rodando sob teste** (algum modo "test"
  no runtime): faz o binário de produção e o de teste divergirem no ponto exato
  que a feature protege. Nunca.

## Raio de alcance

- **Etapa 1:** nenhum golden muda — nenhum `*.test.ds` de `testdata/` exercita
  um UseCase com `required: true` (o wallet usa só `window`). `docs/examples/`
  também fica intacto: `required: true` só aparece em
  `docs/examples/08-tenancy-e-limites/tenancy.ds:73`, e aquele diretório não tem
  `*.test.ds` (os únicos vivem em `docs/examples/09-testes/`) — o job `examples`
  do CI segue igual. Mudam `codegen/gentest.go`, a assinatura de `EmitTests` e o
  seu único call site (`codegen/codegen.go:831`).
- **Etapa 2, rota (a):** reescreve exatamente as duas linhas `ctx :=` de
  `codegen/testdata/tests_wallet.go.golden` (301 e 334) e a asserção de
  substring `codegen/gentest_test.go:109`. Os demais goldens
  (`tests_saga_purchasetickets`, `tests_policy_refunds`,
  `tests_thenstate_counter`) **não** têm cenário de UseCase e devem permanecer
  byte-idênticos — é a guarda NFR-13 da task.
- **Etapa 2, rota (b):** pipeline inteiro (`token`, `lexer`, `parser`,
  `ast/test.go`, `sema/rules_test_files.go`, `codegen/gentest.go`) + os mesmos
  goldens. Ciclo próprio.
- **Fora do gerador:** `driver/generate_e2e_wallet_test.go:253` só é afetado se
  o `wallet` passar a declarar `required: true` — o que **não** é parte desta
  proposta.

## Bloqueios

**Sim, e é de spec: §24 não descreve como se testa idempotência.** Leitura
completa de `24-testing.md` (24.1–24.7) e de `15-idempotency.md`: a primeira não
menciona `Idempotency-Key`, chave, nem contexto de chamada; a segunda não
menciona `*.test.ds`. Não há o que implementar por dedução — o mais próximo de
normativo são §4.2.3 ("Em `*.test.ds`… o cenário nunca fornece [o envelope], o
runner sempre atribui", com `eventId` "derivado deterministicamente do cenário e
da posição") e §4.3.1/§2.7 (alias de teste materializado pelo runner). Os dois
estabelecem o **padrão** "valor técnico ambiente é atribuído pelo runner, de
forma determinística" — mas nenhum dos dois alcança contexto de chamada, e §15
puxa no sentido contrário ("sem fallback"). O que §24 precisa decidir, item a
item:

1. **O runner é "cliente" para efeito de §15?** Se sim, a chave existe sempre
   (e `required: true` nunca falha em cenário); se não, o cenário precisa de
   forma para fornecê-la.
2. **Escopo da chave**, se atribuída pelo runner: uma por cenário, ou uma por
   `when`. Hoje `ScenarioDecl` tem um `when` só, então a distinção só passa a
   existir se (3) for endereçado.
3. **Como se testa a idempotência em si** — replay (mesma chave, mesmo command →
   segundo `when` devolve o resultado cacheado), conflito (mesma chave, command
   diferente → `IdempotencyKeyConflict`), corrida (`wait`/`reject`). Nenhum é
   expressável: não existe cenário com dois `when`.
4. **Linha nova em §24.7**: "cenário sobre UseCase com `idempotency { required:
   true }` sem chave" → ❌ Erro (de compilação/geração), ⚠️ Warning, ou
   comportamento definido (chave do runner).

Vale decidir junto com o **caller** (cenário de acesso NEGADO, registrado na
issue-mãe): são o mesmo buraco na gramática, e uma decisão só — "como um cenário
descreve o contexto de chamada" — fecha os dois.

## Fatiamento sugerido

1. **Issue de revisão de spec §24 — contexto de chamada em cenário**, com os
   quatro pontos acima e o de acesso NEGADO junto (skill `issue-generator`).
   `target_files`: `docs/sdd/issues/<nova-issue>.md`,
   `docs/sdd/issues/open-issues.md`. **Precede tudo**: sem ela a task 3 não
   existe.
2. **Erro de geração claro para UseCase com `required: true` sob teste.**
   `target_files`: `codegen/gentest.go`, `codegen/usecase_idempotency.go`
   (extrair `usecaseRequiresIdempotencyKey`), `codegen/codegen.go` (passar o
   bloco de módulo a `EmitTests`), `codegen/gentest_idempotency_test.go` (novo).
   Par NFR-4, com fontes inline no padrão de `codegen/gentest_thenstate_test.go`:
   um projeto cujo UseCase declara `required: true` + um `Test` → erro exato; o
   mesmo projeto com `idempotency { window: 48h }` → gera e o Go compila.
   Independente da task 1.
3. **(Bloqueada até 1)** Fornecer a chave conforme a decisão — rota (a): uma
   linha em `emitUseCaseScenarioBody` + derivação determinística no padrão de
   `propertySeed`. `target_files`: `codegen/gentest.go`,
   `codegen/gentest_test.go`, `codegen/testdata/tests_wallet.go.golden`,
   `codegen/gentest_idempotency_test.go`. Se a decisão for a rota (b), esta task
   é substituída por um ciclo de front-end e o resíduo volta para a issue-mãe.
4. **Fechamento documental**: esta issue e
   [gaps.md §G-7](../specs/codegen/gaps.md) passam a listar "contexto de chamada
   em cenário" como um item único (idempotência + acesso NEGADO), com o estado
   da decisão de §24. `target_files`:
   `docs/sdd/issues/usecase-idempotency-required-intestavel-test-ds.md`,
   `docs/sdd/issues/lacunas-nos-testes-gerados-test-ds.md`,
   `docs/sdd/specs/codegen/gaps.md`.
