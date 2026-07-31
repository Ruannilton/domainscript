# 8. Workers (Background Processing)

Conceito único, três modos: `every`, `cron`, `continuous`. `scope: per_tenant` ou `global` (seção 14).

```ds
Worker ProcessExpiredReservations {
    schedule every 1min
    concurrency: 1
    timeout 5min
    onError { retry: { attempts: 3, backoff: "exponential" } }   // chaves e defaults: §19.3.1
    execute {
        expiredOrders = list Order o
            where o.status == OrderStatus.Pending and o.expiresAt < now()
            take 100
        for order in expiredOrders {
            order.Cancel("Reserva expirada")
        }
    }
}

Worker DailySettlement {
    schedule cron "0 2 * * *"
    timeout 10min
    execute { ... }
}

Worker ProcessOutboundNotifications {
    schedule continuous
    concurrency: 3
    batchSize: 50
    maxRate: 200
    source { list Notification n where n.status == NotificationStatus.Pending }
    execute(notification) { ... }
}
```

