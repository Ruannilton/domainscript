# State

Ponteiro mínimo de retomada — só o que vem a seguir. **Mantenha este arquivo
enxuto**: duas linhas, cada uma sobrescrita ao mudar, nunca anexada. Nada de
tabela por spec, nada de histórico — isso já mora em cada
`.claude/specs/<spec>/state.md` (ou `tasks.md` no modelo legado) e em
`.claude/issues/open-issues.md`. Ver `CLAUDE.md`.

## Próxima spec-task

`.claude/specs/correcoes-issues-6-8-12/tasks/M2.3.md` → **M2.3** — Implementar
o caminho de `emit` de passo de Saga escolhido em M2.2 (rota (i) Dispatcher
publish-only: var de pacote `sagaDispatcher` reatribuível pelo `Wire`, mesmo
mecanismo de `policyDispatcher`; `Step[S]`/`RunSaga` não mudam; `design.md`
§4.4/§7.2). M1.1 segue `blocked` DE NOVO: a rota decidida (thread de
`aggregateType` via `ctx`, `design.md` §5.1/§7.2) partia da premissa de que
uma única `Tx.Run()` nunca grava eventos de mais de um `aggregateType` —
verificado por leitura e refutado (`sema/rules_crossfile.go:checkTransactions`
só restringe por `Database`, nunca por tipo de Aggregate). Issue nova:
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
