# State — Conclusão das dívidas remanescentes (ISSUE-6, ISSUE-8, ISSUE-12)

> Resumo agregado de quais tasks desta spec estão pendentes e quais estão
> bloqueadas, para retomar a execução após uma interrupção. O detalhe de cada
> task mora no próprio arquivo `tasks/<TASK-CODE>.md` (campo `status` no
> frontmatter: `pending` | `in_progress` | `completed`); este arquivo é só o
> índice agregado — atualize os dois juntos. Tasks `completed` saem das listas
> abaixo.
>
> Marco M, cinco fases **independentes entre si** (M1..M5, qualquer ordem ou
> paralelo); dentro de cada fase a ordem é linear. A lista abaixo está em ordem
> de valor decrescente — M1 fecha ISSUE-12 e desbloqueia o `pizzeria` no CI.
>
> **M1.4, M2.2 e M3.1 são tasks de design (sem código).** M2.2 e M3.1 podem
> escolher delimitar; nesse caso as tasks que dependem delas (M2.3/M2.4,
> M3.2/M3.3) são canceladas — remova-as daqui registrando o motivo, conforme o
> passo final da task de design.

## PENDING TASKS:

- M1.2 — `list <Aggregate>` sem cláusulas em EmitQuery. `depends_on:
  TASK-M1.1`, que voltou a `blocked` — não iniciar até M1.1 ser desbloqueada
  de novo (ver BLOCKED TASKS abaixo).
- M1.3 — `list <Aggregate>` com cláusulas (where/orderBy/skip/take/as) —
  mesma dependência transitiva de M1.1 via M1.2.
- M1.6 — Prova e2e do `pizzeria` e limpeza do CI
- M2.4 — Asserção `emitted` no `then` de um Test de Saga (depende de M2.3,
  agora `blocked` — não iniciar até M2.3 ser desbloqueada de novo)
- M4.2 — Staging na memory UoW: `rolledback` prova reversão real (§22.2)
- M5.1 — Cobertura §22.7 por ramo de `Error` em `sema`
- M5.2 — Delimitações e reclassificações em [gaps.md](../codegen/gaps.md) e nas issues
- M5.3 — Fechamento do Marco M: revisão de DoD

## BLOCKED TASKS:

- M1.1 — Seam de enumeração de streams no runtime (StreamLister). Bloqueada
  DE NOVO: a rota decidida em [design.md](design.md) §5.1/§7.2 (thread de
  `aggregateType` via `ctx`, carimbado uma vez antes de `uow.Run`) partia da
  premissa de que uma única `Tx.Run()` nunca grava eventos de mais de um
  `aggregateType` — verificado por leitura e **refutado**
  (`sema/rules_crossfile.go:checkTransactions` só restringe por `Database`,
  nunca por tipo de Aggregate; um módulo sem `Database` ou com dois
  Aggregates no mesmo `Database` pode despachar `Handle` de tipos diferentes
  no mesmo `Run`). Issue nova (a original já está `SOLVED`):
  [m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype](../../issues/m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype.md).
  [design.md](design.md) §5.1/§7.2 precisa decidir de novo antes de reabrir.
- M2.3 — Implementar o caminho de `emit` escolhido em M2.2 (rota (i)
  Dispatcher publish-only, [design.md](design.md) §4.4). Bloqueada: o mecanismo
  normativo nomeia `emitPolicyWireFunc`/`emitCombinedWireFunc`
  (`codegen/decl_policy.go`) como quem atribui `sagaDispatcher = d`, e não
  decide o caso de um módulo só-Saga (sem Policy/UseCase, sem `func Wire`
  nenhum para estender) — nenhum dos arquivos necessários
  (`codegen/decl_policy.go`, `codegen/codegen.go`) está em `target_files`.
  [m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files](../../issues/m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files.md).
  [design.md](design.md) §4.4 precisa decidir de novo (caso só-Saga) e `target_files`
  desta task precisa ganhar os arquivos necessários antes de reabrir.
- M4.1 — Shrinking determinístico do contra-exemplo de `property` (§22.5).
  Bloqueada: qualquer implementação fiel de REQ-58 muda o texto Go estático
  emitido para a `property` que `testdata/projects/wallet/wallet.test.ds` já
  declara (`Test Wallet`, "saldo nunca fica negativo") — struct `dsPropStep`
  estendida, clausura de replay, mensagem de `t.Fatalf` — quebrando
  `codegen/testdata/tests_wallet.go.golden` via a comparação byte a byte de
  `codegen/gentest_test.go:TestEmitTestsWalletGolden`. Nenhum dos dois
  arquivos está em `target_files` de M4.1 (só `codegen/gentest_property.go`
  e `codegen/gentest_property_test.go`), e o agente não pode nem tocá-los
  fora da lista nem rodar `go test`/`UPDATE_GOLDEN=1` para regenerar o
  golden. Issue:
  [m4-1-shrinking-de-property-muda-golden-fora-de-target-files](../../issues/m4-1-shrinking-de-property-muda-golden-fora-de-target-files.md).
  [M4.1.md](tasks/M4.1.md) precisa ganhar `codegen/testdata/tests_wallet.go.golden` e
  `codegen/gentest_test.go` em `target_files` (ou uma decisão de design que
  isole a mudança) antes de reabrir.

## CANCELLED TASKS:

- M3.2 (`Implementar 'result = call Adapter(...)' (§18.2)`) e M3.3 (`mock ...
  returns X injeta X como retorno do stub`) — canceladas por M3.1
  ([design.md](design.md) §4.5/§7.2, REQ-57.4). M3.1 verificou as três opções de
  contrato de resposta de `Adapter`/`Notification`: (a) "resposta tipada pela
  própria `Notification`" foi **refutada** por leitura (nenhuma declaração
  hoje carrega a forma de uma resposta); (b) `Adapter X returns <Tipo>`
  exigiria gramática nova em léxico→parser→resolver→sema, fora do que uma
  task de codegen decide sozinha; (c) delimitar foi a única opção
  implementável neste ciclo. Issue de revisão de spec registrada:
  [spec-v7-adapter-sem-contrato-de-resposta](../../issues/spec-v7-adapter-sem-contrato-de-resposta.md). M3.2/M3.3 só
  reabrem depois de a spec da linguagem decidir o contrato.
