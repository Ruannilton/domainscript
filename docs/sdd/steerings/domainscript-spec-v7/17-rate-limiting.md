# 17. Rate Limiting

Política no `interface.ds`, backend no `mod.ds`.

- **Dimensões:** `perIp`, `perUser`, `perTenant`, `perApiKey`, `global` — todas precisam passar.
- **Algoritmos:** `token_bucket` (padrão, com `burst`), `sliding_window`, `fixed_window`.
- **Tier-based (feature de plano SaaS):**

```ds
RateLimitTier Free { perUser: 100/min, perTenant: 1000/min }
RateLimitTier Pro  { perUser: 1000/min, perTenant: 20000/min }

POST "/search" -> SearchEvents {
    rateLimit: byTier    // resolve de tenant.tier
}
```

- Resposta automática: 429 + `Retry-After` + `X-RateLimit-*`. gRPC: `RESOURCE_EXHAUSTED`.
- **Falha do backend:** `open` (padrão) ou `closed`, override por endpoint.
- Endpoints sem tenant: só `perIp`. Retry idempotente não consome cota.

