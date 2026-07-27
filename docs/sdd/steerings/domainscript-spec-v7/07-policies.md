# 7. Policies (Reações a Eventos)

`BestEffort` ou `AtLeastOnce`. Usam `emit` para fluxos internos. `ensure ... else Nop` para ignorar cenários silenciosamente.

```ds
Policy RefundAllOnEventCancelled on EventCancelled {
    delivery AtLeastOnce
    execute {
        soldTickets = list Ticket t
            where t.eventId == event.id and t.status == TicketStatus.Sold
        orderIds = soldTickets.distinct(t => t.orderId)
        for orderId in orderIds {
            emit RefundRequested(orderId: orderId, reason: "Evento cancelado")
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

