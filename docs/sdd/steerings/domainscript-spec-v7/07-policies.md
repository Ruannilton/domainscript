# 7. Policies (Reações a Eventos)

`BestEffort` ou `AtLeastOnce`. `ensure ... else Nop` para ignorar cenários silenciosamente.

Reagem tanto a `Event` de domínio ([§4.2](04-domain-core.md)) quanto a `ApplicationEvent` ([§5.3](05-application-layer.md)), com a mesma forma `on X`. Para fluxos internos usam `emit` de `ApplicationEvent`: Policy não é Aggregate, e `Event` de domínio só se emite dentro de um ([§5.3.2](05-application-layer.md)).

```ds
ApplicationEvent RefundRequested { orderId ref Order, reason CancellationReason }

Policy RefundAllOnEventCancelled on EventCancelled {
    delivery AtLeastOnce
    execute {
        soldTickets = list Ticket t
            where t.eventId == event.id and t.status == TicketStatus.Sold
        orderIds = soldTickets.distinct(t => t.orderId)
        for orderId in orderIds {
            emit RefundRequested(orderId: orderId, reason: CancellationReason("Evento cancelado"))
        }
    }
}

Policy ExpireReservations on ReservationExpired {
    delivery AtLeastOnce
    execute {
        order = load Order(event.orderId)
        ensure order.state.status == OrderStatus.Pending else Nop
        order.Cancel("Reserva expirada")
    }
}
```

