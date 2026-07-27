# 3. Controle de Fluxo

Duas formas canônicas: `ensure` (validação) e `match` (ramificação). Sem `if/else`, `while`, `do-while`, `switch`.

## 3.1. `ensure` (Guard Clause)

```ds
ensure [condição] else [ação]
```

| Contexto | `else` aceita |
|----------|---------------|
| Handle / UseCase | `Error` |
| Policy / Worker | `Error`, `Nop` |
| Dentro de `for` | `Error`, `Nop`, `break`, `break all`, `continue` |

`Nop` (retorno vazio silencioso) em Handle/UseCase → erro de compilação.

## 3.2. `match` (Pattern Matching)

Sempre exaustivo. Sobre enum/conjunto fechado: wildcard `_` **proibido**. Com guards (`when`): wildcard **obrigatório**.

```ds
// Statement — exaustivo sobre o enum TicketStatus
match ticket.state.status {
    TicketStatus.Available => ticket.Reserve(orderId, userId)
    TicketStatus.Reserved  => ticket.ConfirmSale(orderId, holder)
    TicketStatus.Sold      => Nop
    TicketStatus.Cancelled => Nop
    TicketStatus.Used      => Nop
}

// Expressão
label = match entry.type {
    TransactionType.Deposit     => "Depósito recebido"
    TransactionType.Withdrawal  => "Saque realizado"
    TransactionType.TransferIn  => "Transferência recebida"
    TransactionType.TransferOut => "Transferência enviada"
}

// Guards — wildcard obrigatório
match order.state.totalAmount {
    amount when amount >= Money(amount: 1000, currency: "BRL") => applyDiscount(order)
    amount when amount >= Money(amount: 500, currency: "BRL")  => applyFreeShipping(order)
    _ => Nop
}
```

## 3.3. `for` (Loop)

Único construto de iteração: coleção ou range. Loop condicional proibido no domínio.

```ds
for ticket in availableTickets { ... }
for i in 1..batch.quantity { ... }
```

`continue`, `break`, `break all` (loops aninhados). Fora de `for` → erro de compilação.

## 3.4. `log`

Disponível em qualquer contexto. Níveis: `debug`, `info`, `warn`, `error`. Compilador anexa trace context, timestamp e metadata.

```ds
log info "Saque realizado" {
    walletId = self.id
    amount = amount
}
```

