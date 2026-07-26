# 07 — Infraestrutura, interface e topologia

Cobre **§11 (Interface)**, **§12 (Topologia)**, **§13 (Infraestrutura de
Módulo)** e **§21 (Geração de Artefatos de Deploy)**.

| Arquivo | Mostra |
|---|---|
| `mod.ds` | Database, FileStorage, Idempotency, Cache, RateLimit, Outbox, Telemetry |
| `interface.ds` | HTTP e gRPC, versionamento, tenant resolution, rate limit por rota |
| `topology.ds` | Services, canais `queue` e `grpc` |

## As ideias que valem a leitura

**Mudar a topologia não altera o domínio.** O mesmo código de negócio roda
como monólito (um service) ou distribuído (vários) — o que muda é
`topology.ds`. E o compilador **revalida** a cada mudança: dividir dois
módulos em services diferentes transforma uma transação local num erro de
compilação que exige Saga. A arquitetura distribuída não vira problema de
runtime porque vira problema de compilação primeiro.

**O domínio não sabe qual protocolo o alcança.** Expor o mesmo UseCase por
HTTP e por gRPC é escrever duas entradas em `interface.ds`. Path params, query
string, body, status codes e o `.proto` saem do mapeamento automático.

**`mod.ds` não é configuração de framework.** Cada bloco alimenta uma feature
da linguagem: `supportsXA` decide se o compilador aceita uma transação
cross-database ou exige Saga; `Idempotency { storage: same }` grava a chave na
mesma transação do negócio; `tenancy` liga o filtro automático de todas as
queries.

**Deploy é derivado, não declarado (§21).** O compilador conhece services,
bancos, filas, cache e telemetria — então gera Dockerfile por service,
`docker-compose` com healthchecks e `depends_on`, migrations SQL, config do
OTEL Collector e um `.env.example` com todos os `env(...)` do código. Infra
declarada duas vezes (uma no `mod.ds`, outra num YAML à mão) é infra que
diverge.

**Fail-closed por padrão.** Tenant ausente vira 400; rota que não quer tenant
declara `tenancy: none`. O default protege; a exceção é explícita.

**Canal sem `orderBy` é warning.** Uma fila sem chave de ordenação entrega
eventos do mesmo agregado fora de sequência — funciona em teste, quebra em
carga.

## Regras da §25 exercitadas

- Módulos em services diferentes sem canal → ❌
- UseCase cross-database sem XA / cross-service sem Saga → ❌
- Canal `queue`/`stream` sem `orderBy` → ⚠️
- UseCase/Query não exposto em interface → ⚠️
- Provider cloud sem equivalente local (profile dev) → ⚠️
