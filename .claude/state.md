# State

Ponteiro mínimo de retomada — só o que vem a seguir. **Mantenha este arquivo
enxuto**: duas linhas, cada uma sobrescrita ao mudar, nunca anexada. Nada de
tabela por spec, nada de histórico — isso já mora em cada
`.claude/specs/<spec>/state.md` (ou `tasks.md` no modelo legado) e em
`.claude/issues/open-issues.md`. Ver `CLAUDE.md`.

## Próxima spec-task

`.claude/specs/correcoes-issues-6-8-12/tasks/M1.1.md` → **M1.1** — seam de
enumeração de streams no runtime (`StreamLister`), REQ-55.1/55.2.
**Desbloqueada**: o usuário decidiu (opção 1) como `aggregateType` chega a
`Append` — thread via `ctx`, mesmo padrão de `tenantID` — registrado em
`design.md` §5.1/§7.2 e em `tasks/M1.1.md` (`target_files` ampliado com
`contextkeys.go.txt` e `decl_usecase.go`, o call site real de `uow.Run`).
M1.1 ainda precisa confirmar por leitura que uma `Tx.Run()` nunca grava
eventos de mais de um `aggregateType` antes de carimbar — ver a nota na
própria task. Retomada de maior valor: fecha ISSUE-12 e desbloqueia o
`pizzeria` no CI (M1.2/M1.3/M1.6 dependem, direta ou transitivamente, de
M1.1). M1.4/M1.5 concluídas (fan-out via Dispatcher em `generateCmdMainFile`,
REQ-55.7/55.8); M2.1 (sem dependências, `emit` em passo de Saga vira erro de
geração claro) é a próxima opção se M1.1 bloquear de novo. M1.6 também
precisa considerar
`.claude/issues/m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real.md`,
achado independente de REQ-55.7/55.8 durante M1.4.

## Próxima issue

`.claude/issues/visibility-de-view-nao-implementado.md` — sem spec dedicada
ainda; a própria issue a marca como a lacuna silenciosa de maior risco do
inventário (falha de segurança sem diagnóstico). Escolha manual — reavalie
ao concluir ou ao mudar de prioridade.
