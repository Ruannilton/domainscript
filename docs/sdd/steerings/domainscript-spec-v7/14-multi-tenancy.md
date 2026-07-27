# 14. Multi-Tenancy

Tenant é **ambient context** (como `caller`). Nunca parâmetro explícito.

| Estratégia | Isolamento | Quando usar |
|------------|-----------|-------------|
| `row_level` | Coluna `tenant_id` | Alta escala, muitos tenants pequenos |
| `schema_per_tenant` | Schema dedicado | Escala média |
| `database_per_tenant` | Banco separado | Regulados, tenants grandes |

- Resolução na borda (`interface.ds`): `subdomain`, `header`, `jwt_claim`, `path`.
- Contexto: `tenant.id`, `tenant.tier`, `tenant.exists`.
- **Filtro automático** em queries e loads. Aggregate de outro tenant → 404.
- **Cross-tenant opt-in**: `tenancy: cross_tenant` + role privilegiada + auditoria automática + warning.
- `Aggregate Tenant` em módulo Platform (sem tenancy). Provisionamento via `provision tenant(id)` numa Policy.
- Channels propagam tenant automaticamente. Workers: `scope: per_tenant`/`global`. Tenant ausente → 400 (fail-closed). Rotas sem tenant: `{ tenancy: none }`.

```ds
UseCase GenerateGlobalReport handles GlobalReportCmd {
    tenancy: cross_tenant
    access { requires caller.hasRole("super_admin") }
    execute {
        allWallets = list Wallet take 10000
    }
}
```

