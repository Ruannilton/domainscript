# 📘 DomainScript Specification (v7.0) — Index

## Architecture-as-Code DSL — Framework de Arquitetura como Linguagem

> **Domínio de referência:** os exemplos desta spec usam dois domínios coesos — **Carteira Digital** (Wallet) para construtos fundamentais e **Plataforma de Ingressos** (Ticketing) para fluxos distribuídos (Sagas, Policies, Workers). Cada construto novo reutiliza os tipos já apresentados.

This spec was split into one file per section so agents can load only the
part relevant to their task instead of the full ~1500-line document. Pick
the file(s) below that match what you're working on; each is self-contained
for its topic.

| # | File | Covers |
|---|------|--------|
| 1 | [01-overview.md](01-overview.md) | Filosofia, paradigma, escopo, estrutura de arquivos `.ds` |
| 2 | [02-type-system.md](02-type-system.md) | ValueObjects, Enums, coleções, `File`/`FileStream`/`FileRef`, funções built-in |
| 3 | [03-control-flow.md](03-control-flow.md) | `ensure`, `match`, `for`, `log` |
| 4 | [04-domain-core.md](04-domain-core.md) | Write Side: Errors, Events, versionamento/redação de eventos, Aggregates |
| 5 | [05-application-layer.md](05-application-layer.md) | Commands, UseCases |
| 6 | [06-read-side.md](06-read-side.md) | Views, field-level security, Queries, Projections cross-database |
| 7 | [07-policies.md](07-policies.md) | Policies (reações a eventos) |
| 8 | [08-workers.md](08-workers.md) | Workers (background processing) |
| 9 | [09-notifications-adapters.md](09-notifications-adapters.md) | Notifications, `notify`/`call`, Adapters (HTTP declarativo / FFI) |
| 10 | [10-ffi.md](10-ffi.md) | `Foreign` (FFI geral): pure/impure, marshalling, onde pode ser chamado, testing |
| 11 | [11-interface.md](11-interface.md) | `interface.ds`: HTTP/gRPC, tenant resolution, versionamento, rate limit |
| 12 | [12-topology.md](12-topology.md) | `topology.ds`: services, canais entre módulos |
| 13 | [13-module-infra.md](13-module-infra.md) | `mod.ds`: Database, FileStorage, Idempotency, Cache, RateLimit, Outbox, Telemetry |
| 14 | [14-multi-tenancy.md](14-multi-tenancy.md) | Multi-tenancy: estratégias, resolução, cross-tenant opt-in |
| 15 | [15-idempotency.md](15-idempotency.md) | Idempotência de Commands |
| 16 | [16-cache.md](16-cache.md) | Cache: política na Query, backend no módulo |
| 17 | [17-rate-limiting.md](17-rate-limiting.md) | Rate Limiting: dimensões, algoritmos, tiers |
| 18 | [18-api-versioning.md](18-api-versioning.md) | Versionamento de API: upcast/downcast, deprecated/sunset |
| 19 | [19-transactions-sagas.md](19-transactions-sagas.md) | Inferência transacional, Sagas |
| 20 | [20-observability.md](20-observability.md) | OpenTelemetry: traces, métricas, logs, `Metric` |
| 21 | [21-deploy.md](21-deploy.md) | Geração de Dockerfile/docker-compose a partir da topologia |
| 22 | [22-smart-partial-loading.md](22-smart-partial-loading.md) | Smart Partial Loading (`focus`, `sum`, paginação nativa) |
| 23 | [23-error-classification.md](23-error-classification.md) | Erros: negócio vs. infraestrutura |
| 24 | [24-testing.md](24-testing.md) | Testing nativo (`*.test.ds`): Aggregate, UseCase, mocks, Saga, Policy/Query, property-based, fixtures |
| 25 | [25-compilation-rules.md](25-compilation-rules.md) | Regras de compilação (resumo tabular de erros e warnings) |
| 26 | [26-glossary.md](26-glossary.md) | Glossário de conceitos |
| 27 | [27-evolving-features.md](27-evolving-features.md) | Funcionalidades em evolução / planejadas |

## Guidance for agents

- Need the "big picture" before writing a spec? Read `01-overview.md` +
  `25-compilation-rules.md` + `26-glossary.md` first — cheap, high signal.
- Working on a specific construct (e.g. Sagas, Policies, Cache)? Load only
  that section's file plus `25-compilation-rules.md` if you need the
  authoritative list of what's an error vs. a warning.
- Don't load the whole spec unless doing something that spans most of it
  (e.g. auditing an implementation against the full v7 surface).
