# State

Ponteiro mínimo de retomada — só o que vem a seguir. **Mantenha este arquivo
enxuto**: duas linhas, cada uma sobrescrita ao mudar, nunca anexada. Nada de
tabela por spec, nada de histórico — isso já mora em cada
`.claude/specs/<spec>/state.md` (ou `tasks.md` no modelo legado) e em
`.claude/issues/open-issues.md`. Ver `CLAUDE.md`.

## Próxima spec-task

`.claude/specs/correcoes-issues-6-8-12/tasks/M3.1.md` → **M3.1** — (design,
sem código) Contrato de resposta de Adapter (`design.md` §4.5, REQ-57.1/57.4).
M2.3 (`emit` em passo de Saga) voltou a `blocked`: o mecanismo normativo de
`design.md` §4.4 (rota i) nomeia `emitPolicyWireFunc`/`emitCombinedWireFunc`
(`codegen/decl_policy.go`) como quem atribui `sagaDispatcher = d`, e não
decide o caso de um módulo só-Saga (sem Policy/UseCase, sem `func Wire` para
estender) — nenhum dos arquivos necessários está em `target_files` de M2.3.
Issue: `.claude/issues/m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files.md`;
`design.md` §4.4 precisa decidir de novo. M2.4 bloqueada transitivamente
(depende de M2.3). M1.1 segue `blocked` DE NOVO: a rota decidida (thread de
`aggregateType` via `ctx`, `design.md` §5.1/§7.2) partia da premissa de que
uma única `Tx.Run()` nunca grava eventos de mais de um `aggregateType` —
verificado por leitura e refutado (`sema/rules_crossfile.go:checkTransactions`
só restringe por `Database`, nunca por tipo de Aggregate). Issue:
`.claude/issues/m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype.md`;
`design.md` precisa decidir de novo. M1.2/M1.3/M1.6 continuam bloqueadas
transitivamente (dependem de M1.1). M1.6 também precisa considerar
`.claude/issues/m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real.md`,
achado independente de REQ-55.7/55.8 durante M1.4.

## Próxima issue

`.claude/issues/visibility-de-view-nao-implementado.md` — sem spec dedicada
ainda; a própria issue a marca como a lacuna silenciosa de maior risco do
inventário (falha de segurança sem diagnóstico). Escolha manual — reavalie
ao concluir ou ao mudar de prioridade.
