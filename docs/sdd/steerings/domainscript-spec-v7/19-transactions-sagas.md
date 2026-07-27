# 19. Transações e Sagas

## 19.1. Inferência Transacional

| Cenário | Comportamento |
|---------|---------------|
| Mesmo `Database` | Commit local |
| Diferentes, ambos XA | 2PC automático |
| Diferentes, sem XA | ❌ Erro — exige Saga |
| Cross-service sem Saga | ❌ Erro |

## 19.2. Sagas

`async` (retorna `sagaId`, compilador gera `SagaStatus`) ou `await timeout Ns`. Steps com `up`/`down`/`onInfraError`. `down { unrecoverable }` para compensação impossível (gera alerta em runtime).

```ds
Saga PurchaseTickets handles PurchaseTicketsCmd {
    mode await timeout 60s
    state { orderId OrderId, ticketIds List<TicketId>, paymentId PaymentId }

    step ReserveTickets {
        up {
            availableTickets = list Ticket t
                where t.eventId == cmd.eventId
                  and t.status == TicketStatus.Available
                take cmd.quantity
            ensure availableTickets.count() == cmd.quantity else InsufficientTickets
            for ticket in availableTickets {
                ticket.Reserve(state.orderId, cmd.userId)
                state.ticketIds.add(ticket.id)
            }
        }
        down {
            for ticketId in state.ticketIds {
                ticket = load Ticket(ticketId)
                ticket.Release("Compensação")
            }
        }
        onInfraError { RetryWithBackoff(3) }
    }

    step ProcessPayment {
        up { result = call PaymentRequest(paymentId: state.paymentId, amount: total, method: cmd.paymentMethod) }
        down { call RefundRequest(paymentId: state.paymentId, amount: total) }
        onInfraError { RetryWithBackoff(3) }
    }

    step ConfirmPurchase {
        up { ... }
        down { ... }
    }
}
```

