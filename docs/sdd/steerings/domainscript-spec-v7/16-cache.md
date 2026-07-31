# 16. Cache

Política na Query, backend no `mod.ds`.

```ds
Query GetWalletSummary(walletId ref Wallet) -> WalletSummaryVW {
    cache {
        ttl: 5min
        invalidateOn: [DepositPerformed, WithdrawalPerformed]  // override; default é inferido
        negativeCacheTtl: 10s
    }
    return load Wallet(walletId) as WalletSummaryVW
}
```

| Aspecto | Comportamento |
|---------|---------------|
| Backends | `memory`, `distributed`, `layered` |
| Invalidação | Por evento, inferida dos aggregates tocados (override `invalidateOn`) |
| Granularidade | Cirúrgica (por ID), grossa (listagens — warning se alta cardinalidade) |
| Cache stampede | Request coalescing por construção |
| Adapters | Só `mode sync` + GET |
| Bypass | Header `Cache-Control: no-cache` |
| Multi-tenancy | Tenant na chave automaticamente |
| Falha do backend | Fail-open |
| EventBus assíncrono | Invalidação in-process imediata após `emit`, antes da fila externa |

