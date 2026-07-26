# State

Ponteiro mínimo de retomada — só o que vem a seguir. **Mantenha este arquivo
enxuto**: duas linhas, cada uma sobrescrita ao mudar, nunca anexada. Nada de
tabela por spec, nada de histórico — isso já mora em cada
`.claude/specs/<spec>/state.md` (ou `tasks.md` no modelo legado) e em
`.claude/issues/open-issues.md`. Ver `CLAUDE.md`.

## Próxima spec-task

`.claude/specs/correcoes-issues-6-8-12/tasks/M2.1.md` → **M2.1** — sem
dependências, `emit` em passo de Saga vira erro de geração claro
(REQ-56.1/56.5). M1.5 concluída (fan-out via Dispatcher em
`generateCmdMainFile`, REQ-55.7/55.8); a combinação residual "módulo AO MESMO
TEMPO produtor durável E dono de Dispatcher local" segue com erro de geração
claro (não implementada — exigiria estender `NewOutboxUnitOfWork`,
`codegen/sqlrt/uow.go.txt`, fora de `target_files` de M1.5), documentada em
`tasks/M1.5.md`. **M1.1 está `blocked`**: nenhuma rota dentro do seu escopo
leva `aggregateType` até `EventStore.Append` — ver
`.claude/issues/m1-1-aggregatetype-nao-chega-a-eventstore-append.md`. M1.2,
M1.3 e M1.6 dependem, direta ou transitivamente, de M1.1 e ficam travadas até
a decisão de design; M1.6 também precisa considerar
`.claude/issues/m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real.md`,
achado independente de REQ-55.7/55.8 durante M1.4.

## Próxima issue

`.claude/issues/visibility-de-view-nao-implementado.md` — sem spec dedicada
ainda; a própria issue a marca como a lacuna silenciosa de maior risco do
inventário (falha de segurança sem diagnóstico). Escolha manual — reavalie
ao concluir ou ao mudar de prioridade.
