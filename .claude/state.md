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
| read-side (REQ-33..40) | `.claude/specs/read-side/` | done | — |
| infra-providers (REQ-41..48) | `.claude/specs/infra-providers/` | done (recorte de 5 fechado; residual REQ-42.6 registrado) | — |
| correcoes-issues-9-10-11 (REQ-49..51) | `.claude/specs/correcoes-issues-9-10-11/` | done | — |
| correcoes-issues-6-7-8 (REQ-52..54) | `.claude/specs/correcoes-issues-6-7-8/` | in-progress (L1.1/L1.2/L1.3a/L1.3b/L1.3c/L2.1 done; L1.3d PAUSADA por decisão do usuário; L1.3e/L1.3f bloqueadas em cascata; L2.2/L2.3 re-escritas em cadeias L2.2a-d / L2.3a-c após validação de coerência) | L2.5 |



**Próxima task: L2.5** — `rolledback` com reversão real (staging na
`memoryUnitOfWork`, §22.2/REQ-53.5). Escolhida por ser a coerente de maior
valor e escopo contido (`rtsrc/uow.go.txt`); L2.4/L3.1 vêm depois, e as cadeias
L2.2*/L2.3* começam por L2.2a (imediata) e L2.3a (design) quando priorizadas.

## Issues em aberto

Ver `.claude/issues.md`. ISSUE-1 (read-side/I5.1) **RESOLVIDA** (commit
`3a22df3`): `codegen/decl_collections.go` centraliza a declaração de
`Collection[T]` var disputado entre `EmitQueries`/`EmitPolicies` num único
`collections.go` por módulo. ISSUE-9/10/11 têm **spec de correção criada**
(`.claude/specs/correcoes-issues-9-10-11/`, Marco K) e ISSUE-6/7/8 também
(`.claude/specs/correcoes-issues-6-7-8/`, Marco L) — todas ainda abertas até a
execução dos respectivos Marcos fechá-las. ISSUE-2/3/4/5 seguem abertas sem spec
dedicada (itens maiores / front-end / spec-da-linguagem).
