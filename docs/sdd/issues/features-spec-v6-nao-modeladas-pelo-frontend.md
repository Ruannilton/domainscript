# Features do spec v6 nunca modeladas pelo front-end (ex-ISSUE-2)
- SPEC: codegen
- TASK: gaps.md §G-3 (exclusões de [requirements.md](../specs/codegen/requirements.md) §1.3)
- DESCRIPTION: Features do spec v6 que o **front-end nunca modelou** — não são
  gaps só do codegen: parser/resolver/checker não as reconhecem, então fechar
  qualquer uma começa por um ciclo novo de front-end (parser → resolver → sema
  → types) e só depois codegen. São quatro:
  (a) **Exposição TCP/UDP** (spec §10/§14) — `interface.ds` só modela HTTP e
  GRPC.
  (b) **Receptor `tenant.*` em corpos** (`tenant.id`/`tenant.tier`/
  `tenant.exists`, spec §13.2) — a tenancy row_level funciona no runtime
  (filtro, cross_tenant, fail-closed 400), mas o domínio não consegue LER o
  tenant corrente de dentro de um Handle/UseCase.
  (c) **Built-in `provision tenant(id)`** (spec §13.4) — sem ela o fluxo de
  provisionamento de tenant do spec não é expressável.
  (d) **Acesso nativo `events()` em Aggregates** (spec §4.5).
  Impacto: cada uma é um ciclo de spec próprio (as mais caras do inventário,
  atravessam o pipeline inteiro); abrir só quando houver demanda real.
- SOLVED: FALSE
