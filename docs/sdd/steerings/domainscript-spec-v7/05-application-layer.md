# 5. Camada de Aplicação

## 5.1. Commands

ValueObjects/Enums obrigatórios. `ref` para Type Safety. **Idempotency key implícita** em todo Command que muta estado (seção 15).

```ds
Command DepositCmd {
    walletId ref Wallet
    amount Money
    description TransactionDescription
}

Command TransferCmd {
    fromWalletId ref Wallet
    toWalletId ref Wallet
    amount Money
    description TransactionDescription
}
```

## 5.2. UseCases

Unit of Work implícito. Timeout com herança módulo → UseCase.

```ds
UseCase PerformDeposit handles DepositCmd {
    timeout 5s
    execute {
        wallet = load Wallet(cmd.walletId)
        ensure wallet exists else WalletNotFound
        wallet.Deposit(cmd.amount, cmd.description)
    }
}

UseCase PerformTransfer handles TransferCmd {
    execute {
        from = load Wallet(cmd.fromWalletId)
        ensure from exists else WalletNotFound
        to = load Wallet(cmd.toWalletId)
        ensure to exists else WalletNotFound

        from.SendTransfer(cmd.toWalletId, cmd.amount, cmd.description)
        to.ReceiveTransfer(cmd.fromWalletId, cmd.amount, cmd.description)
        // mesmo Database → commit atômico
    }
}
```

