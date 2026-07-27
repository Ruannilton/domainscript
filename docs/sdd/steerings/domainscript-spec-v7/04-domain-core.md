# 4. O Núcleo do Domínio (Write Side)

## 4.1. Errors

```ds
Error InsufficientBalance { message "Saldo insuficiente." }
Error InactiveWallet      { message "Carteira inativa." }
Error WalletNotFound      { message "Carteira não encontrada." }
```

## 4.2. Events

Metadata implícito readonly: `timestamp`, `sequence`, `aggregateId`, `eventType`.

**Event vs PublicEvent** — interno ao módulo vs. compartilhado em `contracts/*.ds`. Policy cross-module escutando `Event` privado → erro de compilação.

```ds
Event WalletCreated { id WalletId, holder HolderName, email Email }
Event WithdrawalPerformed { id WalletId, amount Money, description TransactionDescription }
PublicEvent DepositPerformed { id WalletId, amount Money, description TransactionDescription }
```

## 4.3. Versionamento de Eventos

Campos novos com `default`. Transformações complexas com `Upcast`. Compilador valida caminho resolvível de toda versão histórica; emite warning quando Upcast é substituível por default.

```ds
Event DepositPerformed {
    id WalletId
    amount Money
    channel Channel = Channel("unknown")   // adicionado em versão posterior
}

Upcast TransferSent v1 -> v2 {
    fee = Money(amount: 0, currency: event.amount.currency)
}
```

## 4.4. Redação de Eventos (GDPR)

Para direito ao esquecimento (GDPR Art. 17) sem corromper replay, campos podem ser `redactable`. A redação substitui o campo por placeholder tipado — replay continua funcionando, estrutura preservada, PII removida.

```ds
Event WalletCreated {
    id WalletId
    holder HolderName redactable
    email Email redactable
}
```

(Mecanismo de gatilho — feature em evolução.)

## 4.5. Aggregates

`StateStored` (padrão) ou `EventSourced`. Snapshot opt-in. Event stream nativo via `events()`. Bloco `access` closed-by-default. Bloco `storage` mapeia state e campos `FileRef`.

```ds
Aggregate Wallet {
    strategy EventSourced
    snapshot every 50 events

    state {
        balance Money
        active ActiveStatus
        holder HolderName
        email Email
        entries AppendList<StatementEntry>
    }

    access {
        Create          requires caller.authenticated
        Deposit         requires caller.authenticated
        Withdraw        requires caller.id == self.id or caller.hasRole("admin")
        SendTransfer    requires caller.id == self.id
        ReceiveTransfer requires caller.authenticated
    }

    Handle Create(holder HolderName, email Email) {
        emit WalletCreated(self.id, holder, email)
    }

    Apply WalletCreated {
        state.holder = event.holder
        state.email = event.email
        state.balance = Money(amount: 0, currency: "BRL")
        state.active = ActiveStatus(true)
    }

    Handle Deposit(amount Money, description TransactionDescription) {
        ensure state.active == ActiveStatus(true) else InactiveWallet
        emit DepositPerformed(self.id, amount, description)
    }

    Apply DepositPerformed {
        state.balance = state.balance + event.amount
        state.entries.add(StatementEntry(
            type: TransactionType.Deposit,
            amount: event.amount,
            description: event.description,
            date: event.timestamp
        ))
    }

    Handle Withdraw(amount Money, description TransactionDescription) {
        ensure state.active == ActiveStatus(true) else InactiveWallet
        ensure state.balance >= amount else InsufficientBalance
        emit WithdrawalPerformed(self.id, amount, description)
    }

    Apply WithdrawalPerformed {
        state.balance = state.balance - event.amount
        state.entries.add(StatementEntry(
            type: TransactionType.Withdrawal,
            amount: event.amount,
            description: event.description,
            date: event.timestamp
        ))
    }
}
```

**Event stream nativo:**

```ds
wallet.events()
wallet.events(from: 100, to: 200)
wallet.events(since: someDate, until: anotherDate)
```

`events()` é o log técnico; `AppendList` é a visão de negócio (ex: extrato). Coexistem com propósitos distintos.

