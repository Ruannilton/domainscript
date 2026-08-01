# Features do spec v6 nunca modeladas pelo front-end (ex-ISSUE-2)
- SPEC: [codegen](../specs/codegen/requirements.md)
- TASK: [gaps.md §G-3](../specs/codegen/gaps.md) (exclusões de
  [`requirements.md` §1.3](../specs/codegen/requirements.md))
- DESCRIPTION: Features do spec v6 que o **front-end nunca modelou** — não são
  gaps só do codegen: parser/resolver/checker não as reconhecem, então fechar
  qualquer uma começa por um ciclo novo de front-end (parser → resolver → sema
  → types) e só depois codegen. São quatro (numeração §N em v6; ver
  [`domainscript-spec-v7/README.md`](../steerings/domainscript-spec-v7/README.md)
  sobre o desvio de numeração v6→v7):
  (a) **Exposição TCP/UDP** (spec §10/§14, hoje
  [`11-interface.md`](../steerings/domainscript-spec-v7/11-interface.md)) —
  `interface.ds` só modela HTTP e GRPC.
  (b) **Receptor `tenant.*` em corpos** (`tenant.id`/`tenant.tier`/
  `tenant.exists`, spec §13.2, hoje
  [`14-multi-tenancy.md`](../steerings/domainscript-spec-v7/14-multi-tenancy.md)) —
  a tenancy row_level funciona no runtime (filtro, cross_tenant, fail-closed
  400), mas o domínio não consegue LER o tenant corrente de dentro de um
  Handle/UseCase.
  (c) **Built-in `provision tenant(id)`** (spec §13.4, hoje
  [`14-multi-tenancy.md`](../steerings/domainscript-spec-v7/14-multi-tenancy.md)) —
  sem ela o fluxo de provisionamento de tenant do spec não é expressável.
  (d) **Acesso nativo `events()` em Aggregates** (spec §4.5,
  [`04-domain-core.md`](../steerings/domainscript-spec-v7/04-domain-core.md)).
  Impacto: cada uma é um ciclo de spec próprio (as mais caras do inventário,
  atravessam o pipeline inteiro); abrir só quando houver demanda real.
- SOLVED: FALSE
**nota do desenvolvedor:** não daremos suporte a  exposição TCP/UPD direta no momento
**nota do desenvolvedor:** tenancy deve ser definido por agregado, considerando que o agregado define toda a borda de persistencia