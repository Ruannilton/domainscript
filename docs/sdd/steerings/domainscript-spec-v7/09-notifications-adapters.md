# 9. Notifications & Adapters

## 9.1. Notifications

Contrato de saída. Sem `Adapter` correspondente → erro de compilação.

```ds
Notification DepositNotification { to Email, amount Money }
Notification PaymentRequest { paymentId PaymentId, amount Money, method PaymentMethod }
```

## 9.2. `notify` (async) vs `call` (sync)

```ds
notify DepositNotification(to: wallet.state.email, amount: event.amount)
result = call PaymentRequest(paymentId: payment.id, amount: total, method: cmd.paymentMethod)
```

## 9.3. Adapters

**Nível 1 — HTTP Declarativo:**

```ds
Adapter DepositNotification {
    mode async
    http POST "https://api.sendgrid.com/v3/mail/send"
    headers { "Authorization" = "Bearer {env('SENDGRID_KEY')}" }
    body {
        to      = notification.to
        subject = "Depósito recebido"
        body    = "Você recebeu {notification.amount}."
    }
}
```

**Nível 2 — FFI vinculado a Notification:**

```ds
Adapter PaymentRequest {
    mode sync
    foreign "go" from "adapters/payment_gateway"
    function "ProcessPayment"
    map {
        paymentId = notification.paymentId
        amount    = notification.amount
    }
}
```

Assinatura incompatível → erro de compilação. Nível 1 migra automaticamente ao trocar linguagem alvo; Nível 2 exige reimplementação.

