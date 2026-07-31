# 24. Testing Nativo (`*.test.ds`)

Teste declarativo Given-When-Then, executado em memória. Validado contra o domínio em tempo de compilação.

## 24.1. Aggregate

```ds
Test Wallet {
    scenario "saque com saldo suficiente" {
        given [
            WalletCreated(id: "W1", holder: "João", email: "joao@x.com"),
            DepositPerformed(id: "W1", amount: Money(100, "BRL"), description: "init")
        ]
        when Withdraw(amount: Money(30, "BRL"), description: "Saque")
        then [ WithdrawalPerformed(id: "W1", amount: Money(30, "BRL"), description: "Saque") ]
    }

    scenario "saque com saldo insuficiente" {
        given [
            WalletCreated(id: "W1", holder: "João", email: "joao@x.com"),
            DepositPerformed(id: "W1", amount: Money(20, "BRL"), description: "init")
        ]
        when Withdraw(amount: Money(50, "BRL"), description: "Saque")
        then error InsufficientBalance
    }
}
```

`StateStored`: `given` estado direto; `then state { ... }`.

## 24.2. UseCase (asserção transacional)

```ds
Test PerformTransfer {
    scenario "transferência bem-sucedida" {
        given Wallet("W1") from [
            WalletCreated(id: "W1", holder: "João", email: "joao@x.com"),
            DepositPerformed(id: "W1", amount: Money(100, "BRL"), description: "init")
        ]
        given Wallet("W2") from [ WalletCreated(id: "W2", holder: "Maria", email: "maria@x.com") ]
        when TransferCmd(fromWalletId: "W1", toWalletId: "W2", amount: Money(30, "BRL"), description: "x")
        then {
            Wallet("W1") emitted TransferSent(amount: Money(30, "BRL"))
            Wallet("W2") emitted TransferReceived(amount: Money(30, "BRL"))
            committed
        }
    }

    scenario "carteira inexistente faz rollback" {
        given Wallet("W1") from [ WalletCreated(id: "W1", holder: "João", email: "joao@x.com") ]
        when TransferCmd(fromWalletId: "W1", toWalletId: "W2", amount: Money(30, "BRL"), description: "x")
        then { error WalletNotFound, rolledback }
    }
}
```

## 24.3. Mock de Adapters/FFI e Saga

```ds
Test PurchaseTickets {
    scenario "pagamento recusado dispara compensação" {
        mock PaymentRequest returns PaymentResult(status: PaymentStatus.Declined)
        given Event("E1") from [ /* ... */ ]
        when PurchaseTicketsCmd( /* ... */ )
        then {
            saga compensated
            Order emitted OrderCancelled
            tickets released
        }
    }

    scenario "falha de infra no step de confirmação" {
        mock PaymentRequest returns PaymentResult(status: PaymentStatus.Approved)
        fail step ConfirmPurchase with InfraError
        when PurchaseTicketsCmd( /* ... */ )
        then {
            saga compensated
            compensated [ConfirmPurchase, ProcessPayment, ReserveTickets]
            called RefundRequest
        }
    }
}
```

## 24.4. Policy e Query

```ds
Test RefundAllOnEventCancelled {
    scenario "reembolso agrupado por pedido" {
        given tickets [
            Ticket("T1") { eventId: "E1", status: TicketStatus.Sold, orderId: "O1" },
            Ticket("T2") { eventId: "E1", status: TicketStatus.Sold, orderId: "O1" },
            Ticket("T3") { eventId: "E1", status: TicketStatus.Sold, orderId: "O2" }
        ]
        when event EventCancelled(id: "E1", reason: "Chuva")
        then {
            emitted RefundRequested(orderId: "O1")
            emitted RefundRequested(orderId: "O2")
            emitted count 2
        }
    }
}
```

## 24.5. Property-Based

```ds
Test Wallet {
    property "saldo nunca fica negativo" {
        forall sequence of [Deposit, Withdraw, Transfer]
        invariant state.balance >= Money(0, "BRL")
    }
}
```

Compilador gera sequências válidas e reporta o contra-exemplo mínimo em falha.

## 24.6. Fixtures

```ds
Fixture activeWallet {
    Wallet("W1") from [
        WalletCreated(id: "W1", holder: "João", email: "joao@x.com"),
        DepositPerformed(id: "W1", amount: Money(100, "BRL"), description: "init")
    ]
}
```

## 24.7. Garantias e Cobertura Semântica

| Situação | Resultado |
|----------|-----------|
| Evento/comando inexistente no teste | ❌ Erro |
| Shape de evento esperado errada | ❌ Erro |
| Mock com retorno de tipo errado, ou em Notification sem contrato de resposta ([§9.4](09-notifications-adapters.md)) | ❌ Erro |
| Cenário atinge `call X` sem `mock X returns …` | ❌ Falha em execução |
| `fail step X` inexistente | ❌ Erro |
| Metadata de evento fornecido em `given`/`when`, ou asserido em `then` ([§4.2.3](04-domain-core.md)) | ❌ Erro |
| `emitted <ApplicationEvent>` qualificado por instância ([§5.3.7](05-application-layer.md)) | ❌ Erro |
| `compensate` de `Error` inexistente ([§19.3](19-transactions-sagas.md)) | ❌ Erro |
| Handle sem cenário de erro testado | ⚠️ Warning |

Cobertura por Handle e ramo (não por linha): o compilador reporta exatamente quais regras e caminhos de erro não têm teste.

