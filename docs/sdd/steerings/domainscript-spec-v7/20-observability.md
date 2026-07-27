# 20. Observabilidade (OpenTelemetry)

- **Traces automáticos** para todo construto; propagação cross-service (W3C headers em grpc/queue/stream/http).
- **Métricas automáticas** (duration, counters, gauges) por UseCase, Aggregate, Saga, Policy, Worker, Channel, Adapter.
- **Logs automáticos** + `log` explícito.
- **Métricas de negócio declarativas:**

```ds
Metric DepositVolume {
    type counter
    value event.amount.amount
    on DepositPerformed
    labels { currency = event.amount.currency }
}

Metric PurchaseLatency {
    type histogram
    buckets [100ms, 250ms, 500ms, 1s, 2s, 5s]
    on PurchaseTickets.completed
}
```

