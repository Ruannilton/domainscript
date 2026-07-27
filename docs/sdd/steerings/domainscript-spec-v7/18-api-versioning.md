# 18. Versionamento de API

Domínio conhece só a versão atual. Tradução na borda (simétrico ao Upcast de eventos).

```ds
// versions/v1.ds
Version v1 {
    deprecated: "2026-01-01"
    sunset: "2026-06-01"

    upcast DepositCmd {
        from { value decimal, currency string, description string }
        to {
            amount = Money(amount: value, currency: currency)
            description = TransactionDescription(description)
            channel = Channel("legacy")
        }
    }

    downcast WalletSummaryVW {
        to {
            balance = self.balance_amount
            currency = self.balance_currency
            owner = self.holder
        }
    }

    // Mudança semântica → UseCase diferente
    route "/wallets/{walletId}/transfer" -> PerformLegacyTransfer
}
```

| Fase | Comportamento automático |
|------|--------------------------|
| Após `deprecated` | Headers `Deprecation`/`Sunset` + warning na compilação |
| Após `sunset` | 410 Gone |

Endpoints inalterados passam direto. Campo obrigatório sem default no upcast → erro. Validação é sempre a atual.

