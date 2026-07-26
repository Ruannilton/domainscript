# 08 — Multi-tenancy, idempotência, limites e versionamento

Cobre **§14 (Multi-Tenancy)**, **§15 (Idempotência)**, **§17 (Rate Limiting)**,
**§18 (Versionamento de API)** e **§20 (Observabilidade)**.

| Arquivo | Mostra |
|---|---|
| `tenancy.ds` | Filtro automático, `tenant.*`, cross-tenant opt-in, `provision tenant`, idempotência, `RateLimitTier`, `Metric` |
| `versions/v1.ds` | `upcast`/`downcast`, `deprecated`/`sunset`, rota redirecionada |

## As ideias que valem a leitura

**Tenant é ambient context, e é por isso que funciona.** Se tenant fosse
parâmetro, esquecer de passá-lo seria um vazamento de dados entre clientes —
o bug mais caro que um SaaS pode ter. Como é ambiente, o filtro entra em toda
query e todo load automaticamente, e não há como esquecer.

**Aggregate de outro tenant devolve 404, não 403.** 403 confirmaria que o
recurso existe. A escolha do status é parte do isolamento.

**Cross-tenant é opt-in, exige role privilegiada, gera auditoria — e ainda
avisa.** O warning não é ruído: é para a decisão aparecer na revisão de
código, não só no momento em que alguém a escreveu.

**A chave de idempotência vem do cliente, sem fallback.** Uma chave gerada
pelo servidor não protegeria contra o retry do cliente — que é exatamente o
caso real (app móvel em rede instável, callback de gateway repetido).

**Erro de infra não entra no cache de idempotência.** Sucesso e erro de
negócio ficam cacheados; falha de infraestrutura não, senão uma falha
transitória viraria permanente para aquela chave.

**Todas as dimensões de rate limit precisam passar.** `perIp`, `perUser`,
`perTenant`, `perApiKey`, `global` — não é "o mais restritivo vence", é
conjunção. E `byTier` resolve de `tenant.tier`, o que liga o limite ao plano
sem espalhar `if plano == "pro"` pelo código.

**Versionamento de API é simétrico ao Upcast de eventos.** Lá se traduz o
passado que ficou gravado; aqui, o passado que ainda chama. Nos dois casos o
domínio conhece só o presente. E a validação é sempre a atual: traduzir a
forma não afrouxa a regra.

**Mudança semântica não se resolve traduzindo forma.** Se a regra de negócio
mudou, a rota antiga aponta para outro UseCase — `route ... -> PerformLegacyTransfer`.

## Regras da §25 exercitadas

- Acesso cross-tenant sem opt-in → ❌
- Upcast de API com campo obrigatório sem default → ❌
- Idempotency conflito (mesma chave, command diferente) → ❌ 422
- UseCase cross-tenant declarado → ⚠️ (auditoria)
