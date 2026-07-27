# State

Ponteiro mínimo de retomada — só o que vem a seguir. **Mantenha este arquivo
enxuto**: duas linhas, cada uma sobrescrita ao mudar, nunca anexada. Nada de
tabela por spec, nada de histórico — isso já mora em cada
`specs/<spec>/state.md` (ou `tasks.md` no modelo legado) e em
[open-issues.md](issues/open-issues.md). Ver [CLAUDE.md](../../CLAUDE.md).

## Próxima spec-task

[specs/correcoes-issues-6-8-12/tasks/M4.2.md](specs/correcoes-issues-6-8-12/tasks/M4.2.md) → **M4.2** — Staging na
memory UoW: `rolledback` prova reversão real (§22.2, REQ-59, sem
dependências). M4.1 (shrinking de `property`, REQ-58) ficou `blocked`:
qualquer implementação fiel muda o texto Go estático da `property` que
`wallet.test.ds` já declara, quebrando `codegen/testdata/tests_wallet.go.golden`
via `codegen/gentest_test.go:TestEmitTestsWalletGolden` — nenhum dos dois
arquivos está em `target_files` de M4.1. Issue:
[m4-1-shrinking-de-property-muda-golden-fora-de-target-files](issues/m4-1-shrinking-de-property-muda-golden-fora-de-target-files.md).
M3.1 concluída: decidiu delimitar o contrato de resposta de
`Adapter`/`Notification` ([design.md](specs/correcoes-issues-6-8-12/design.md) §4.5/§7.2) — nenhuma declaração hoje
carrega a forma de uma resposta de `call`, e a única rota que resolveria
(`Adapter X returns <Tipo>`) exige gramática nova fora do escopo deste ciclo.
M3.2/M3.3 CANCELADAS em consequência (REQ-57.4); issue de revisão de spec:
[spec-v7-adapter-sem-contrato-de-resposta](issues/spec-v7-adapter-sem-contrato-de-resposta.md). M2.3 (`emit` em
passo de Saga) segue `blocked`: o mecanismo normativo de [design.md](specs/correcoes-issues-6-8-12/design.md) §4.4
(rota i) nomeia `emitPolicyWireFunc`/`emitCombinedWireFunc`
(`codegen/decl_policy.go`) como quem atribui `sagaDispatcher = d`, e não
decide o caso de um módulo só-Saga (sem Policy/UseCase, sem `func Wire` para
estender) — nenhum dos arquivos necessários está em `target_files` de M2.3.
Issue: [m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files](issues/m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files.md);
[design.md](specs/correcoes-issues-6-8-12/design.md) §4.4 precisa decidir de novo. M2.4 bloqueada transitivamente
(depende de M2.3). M1.1 segue `blocked` DE NOVO: a rota decidida (thread de
`aggregateType` via `ctx`, [design.md](specs/correcoes-issues-6-8-12/design.md) §5.1/§7.2) partia da premissa de que
uma única `Tx.Run()` nunca grava eventos de mais de um `aggregateType` —
verificado por leitura e refutado (`sema/rules_crossfile.go:checkTransactions`
só restringe por `Database`, nunca por tipo de Aggregate). Issue:
[m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype](issues/m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype.md);
[design.md](specs/correcoes-issues-6-8-12/design.md) precisa decidir de novo. M1.2/M1.3/M1.6 continuam bloqueadas
transitivamente (dependem de M1.1). M1.6 também precisa considerar
[m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real](issues/m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real.md),
achado independente de REQ-55.7/55.8 durante M1.4.

## Próxima issue

[visibility-de-view-nao-implementado](issues/visibility-de-view-nao-implementado.md) — sem spec dedicada
ainda; a própria issue a marca como a lacuna silenciosa de maior risco do
inventário (falha de segurança sem diagnóstico). Escolha manual — reavalie
ao concluir ou ao mudar de prioridade.
