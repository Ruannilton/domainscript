# State

Rastreio do estado de cada `spec::task`, para retomar o desenvolvimento caso
a execução seja interrompida. Atualizado ao final de **cada task concluída**
(ver `CLAUDE.md`) — nunca em lote no fim de uma spec inteira.

Convenção de status: `done` | `in-progress` | `pending` | `blocked`.

## Resumo por spec

| Spec | Diretório | Status | Próxima task |
|---|---|---|---|
| transpilador (front-end, REQ-1..8) | `.claude/specs/transpilador/` | done | — |
| type-checking (REQ-9..13) | `.claude/specs/type-checking/` | done | — |
| codegen (back-end, REQ-14..32) | `.claude/specs/codegen/` | done | — |
| read-side (REQ-33..40) | `.claude/specs/read-side/` | in-progress | I5.1 |

## transpilador — `.claude/specs/transpilador/tasks.md`

Fases 0–11 completas (setup, léxico, parser/AST, config/test, símbolos,
agregação de programa, regras locais e cross-file, driver/CLI, robustez e
determinismo). Nenhuma task pendente.

## type-checking — `.claude/specs/type-checking/tasks.md`

Fases A–F completas (resolução de nomes em corpos, refs de config, modelo de
tipos, acesso a membro, compatibilidade de tipos, códigos de diagnóstico).
Nenhuma task pendente.

## codegen — `.claude/specs/codegen/tasks.md`

Marcos E–H completos (núcleo transacional, reações/coordenação, infra real,
exposição/observabilidade avançadas + testes). Nenhuma task pendente.
`gaps.md` documenta divergências conhecidas entre o spec da linguagem e o
que o back-end entrega — não são tasks pendentes desta spec.

## read-side — `.claude/specs/read-side/tasks.md`

Marco I ("Read Side de Verdade"), REQ-33..40. Fases I0–I4 concluídas (seam no
runtime, predicado falível, `orderBy`/`skip`/`take`, `load X(id).entries` +
`as V`, operador `in`).

Pendente, na ordem do plano:

- [ ] **I5.1** — Lowering do `join` mesmo-banco (materializa as duas fontes,
      loop aninhado) — fatia `GetMyTickets`.
- [ ] **I6.2** — Âncora 3 + des-adaptação: fixture da Policy §7 para Smart
      Partial Loading (§20).
- [ ] **I7.0** — Seam `Dialect` + registro único de provider (REQ-40).
- [ ] **I7.1** — Contraparte de `Collection[T]` sobre tabela no adapter
      sqlite.
- [ ] **I8.1** — Revisão contra a DoD (`requirements.md` §5); atualizar
      documentação de fechamento.

## Issues em aberto

Ver `.claude/issues.md`. Nenhuma issue registrada até o momento.
