# 15. Idempotência de Commands

Chave fornecida pelo cliente (sem fallback). Metadata implícito em todo Command que muta estado.

| Protocolo | Como enviar |
|-----------|-------------|
| HTTP | Header `Idempotency-Key` |
| gRPC | Metadata `idempotency-key` |
| TCP/UDP | Campo no header da mensagem |

- **Storage:** `same` (atômico com a transação) ou `external` (Redis/Dynamo).
- **Cache de resultado:** sucesso ✅, erro de negócio ✅, erro de infra ❌ (permite retry).
- **Race** (mesma chave em paralelo): `wait` ou `reject`. **Conflito** (mesma chave, command diferente): 422 `IdempotencyKeyConflict`.
- Limpeza via Worker automático. Para Sagas: chave (entrada) → `sagaId` (saída), mapeamento estável. Retry idempotente não consome rate limit.

```ds
UseCase PerformDeposit handles DepositCmd {
    idempotency { required: true, window: 48h }
    execute { ... }
}
```

