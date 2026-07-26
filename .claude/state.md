# State

Ponteiro mínimo de retomada — só o que vem a seguir. **Mantenha este arquivo
enxuto**: duas linhas, cada uma sobrescrita ao mudar, nunca anexada. Nada de
tabela por spec, nada de histórico — isso já mora em cada
`.claude/specs/<spec>/state.md` (ou `tasks.md` no modelo legado) e em
`.claude/issues/open-issues.md`. Ver `CLAUDE.md`.

## Próxima spec-task

`.claude/specs/correcoes-issues-6-8-12/tasks/M1.5.md` → **M1.5** — implementa
o wiring decidido por M1.4 (`design.md` §4.3/§5.2): remove as guardas F5 e
F5/G3 em `generateCmdMainFile` (REQ-55.7/55.8). **M1.1 está `blocked`**:
nenhuma rota dentro do seu escopo leva `aggregateType` até
`EventStore.Append` — ver
`.claude/issues/m1-1-aggregatetype-nao-chega-a-eventstore-append.md`. M1.2 e
M1.3 dependem de M1.1 e ficam travadas até a decisão de design. M1.5 também
precisa considerar `.claude/issues/m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real.md`,
achado independente de REQ-55.7/55.8 durante M1.4, registrado para M1.6.

## Próxima issue

`.claude/issues/visibility-de-view-nao-implementado.md` — sem spec dedicada
ainda; a própria issue a marca como a lacuna silenciosa de maior risco do
inventário (falha de segurança sem diagnóstico). Escolha manual — reavalie
ao concluir ou ao mudar de prioridade.
