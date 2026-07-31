# 6. Leituras e Queries (Read Side)

Primitivos permitidos — o dado já foi validado por VO na borda de entrada.

## 6.1. Views

```ds
View WalletSummaryVW From Wallet    // auto-mapping com flattening

View StatementEntryVW {
    type string
    amount_value decimal
    amount_currency string
    description string
    date datetime
}
```

## 6.2. Field-Level Security

`access` do Aggregate controla **quem invoca**; `visibility` da View controla **quem vê quais campos**. Campos não autorizados são omitidos da serialização (não retornam `null`).

```ds
View WalletSummaryVW From Wallet {
    visibility {
        balance requires caller.id == self.id or caller.hasRole("admin")
        email   requires caller.id == self.id or caller.hasRole("support")
        // campos não listados: visíveis a qualquer caller autorizado pela Query
    }
}
```

(Casos avançados — em evolução.)

## 6.3. Queries

`load`, `list`, `count`, `join` (mesmo banco), `in`, `distinct`. `join` cross-database → erro, exige `Projection`. Bloco `cache` opcional (seção 16).

```ds
Query GetStatement(walletId ref Wallet, page integer) -> List<StatementEntryVW> {
    return load Wallet(walletId)
           .entries
           orderBy date descending
           skip page * 20
           take 20
           as StatementEntryVW
}

Query GetMyTickets(userId UserId) -> List<TicketVW> {
    return list Ticket t
           join Order o on t.orderId == o.id
           where o.userId == userId
             and t.status in [TicketStatus.Sold, TicketStatus.Used]
           as TicketVW
}
```

## 6.4. Projections (Cross-Database)

```ds
Projection InvoiceWithHolderVW {
    source Invoice, Wallet
    map {
        invoiceId = Invoice.id
        amount    = Invoice.amount
        holder    = Wallet.holder
    }
    refreshOn [InvoiceCreated, WalletUpdated]
}
```

