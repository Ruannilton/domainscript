# 2. Sistema de Tipos

## 2.1. A Regra de Ouro

Primitivos (`integer`, `decimal`, `string`, `boolean`, `datetime`, `bytes`) são **proibidos no Write Side** (Aggregates, Commands, Events). Permitidos dentro de ValueObjects/Enums e no Read Side (Views, Queries, Projections).

## 2.2. ValueObjects

Blocos atômicos, imutáveis, auto-validáveis, com comportamento (operadores). Base de todos os exemplos seguintes:

```ds
ValueObject WalletId(string) {
    Valid { self.length > 0 }
}

ValueObject Email(string) {
    Valid { self.contains("@") }
}

ValueObject HolderName(string) {
    Valid { self.length >= 2 and self.length <= 120 }
}

ValueObject TransactionDescription(string) {
    Valid { self.length <= 256 }
}

ValueObject ActiveStatus(boolean) {
    Valid { true }
}

ValueObject Money {
    amount decimal
    currency string

    Valid { amount >= 0 }

    Operator +(other Money) -> Money {
        ensure self.currency == other.currency else CurrencyMismatchError
        return Money(amount: self.amount + other.amount, currency: self.currency)
    }

    Operator -(other Money) -> Money {
        ensure self.currency == other.currency else CurrencyMismatchError
        ensure self.amount >= other.amount else NegativeResultError
        return Money(amount: self.amount - other.amount, currency: self.currency)
    }

    Operator >=(other Money) -> boolean {
        ensure self.currency == other.currency else CurrencyMismatchError
        return self.amount >= other.amount
    }
}

ValueObject StatementEntry {
    type TransactionType
    amount Money
    description TransactionDescription
    date datetime
}
```

## 2.3. Enums

Conjunto fechado de valores nomeados sob namespace e tipo. Permitidos no Write Side. Syntactic sugar para VO com validação de pertencimento — o compilador emite warning quando um VO poderia ser Enum.

```ds
Enum TransactionType : string {
    Deposit     = "DEPOSIT"
    Withdrawal  = "WITHDRAWAL"
    TransferIn  = "TRANSFER_IN"
    TransferOut = "TRANSFER_OUT"
}

Enum TicketStatus : string {
    Available = "AVAILABLE"
    Reserved  = "RESERVED"
    Sold      = "SOLD"
    Cancelled = "CANCELLED"
    Used      = "USED"
}
```

**Coerção implícita (padrão):** conversão bidirecional automática na borda. Valor desconhecido → erro 422.

**Coerção explícita (`coerce`):** para aliases, legados, case-insensitive.

```ds
Enum PaymentMethod : string {
    CreditCard = "CREDIT_CARD"
    DebitCard  = "DEBIT_CARD"
    Pix        = "PIX"

    coerce from string {
        match self.uppercase() {
            "CREDIT_CARD", "CC" => CreditCard
            "DEBIT_CARD", "DC"  => DebitCard
            "PIX"               => Pix
            _ => InvalidPaymentMethodError
        }
    }
}
```

## 2.4. Tipos de Coleção

| Tipo | Descrição | Operações |
|------|-----------|-----------|
| `List<T>` | Lista mutável ordenada | `add()`, `remove()`, `clear()`, indexação |
| `AppendList<T>` | Append-only. Compilador otimiza storage e paginação | `add()` apenas |
| `Set<T>` | Sem duplicatas | `add()`, `remove()`, `contains()`, `clear()` |
| `Map<K, V>` | Chave-valor | `put()`, `get()`, `remove()`, `containsKey()`, `keys()`, `values()` |

`AppendList<T>`: `remove()`/`clear()`/reatribuição → **erro de compilação**.

## 2.5. Tipo `File`

Arquivos são conceitualmente ValueObjects, mas operacionalmente especiais: bytes caros de carregar, storage dedicado, ciclo de vida próprio.

| Tipo | Função |
|------|--------|
| `File` | VO com bytes em memória. Commands e operações transitórias. |
| `FileStream` | Streaming chunk-a-chunk para arquivos grandes. |
| `FileRef` | Referência leve com metadata. Vive no Aggregate state. Nunca carrega bytes implicitamente. |

**Operações:**

| Operação | Comportamento |
|----------|---------------|
| `store cmd.file` | Upload ao storage, retorna `FileRef` |
| `load File(ref)` | Carrega bytes explicitamente |
| `signed_url(ref, expires:)` | URL temporária do storage |
| `delete file(ref)` | Remoção explícita — ciclo de vida é responsabilidade do dev |

```ds
Aggregate Person {
    storage {
        state: PersonDb
        document: DocumentStorage    // campo FileRef → FileStorage específico
    }

    state {
        name HolderName
        document FileRef
    }

    access {
        AttachDocument requires caller.id == self.id
    }

    Handle AttachDocument(ref FileRef) {
        emit DocumentAttached(self.id, ref)
    }

    Apply DocumentAttached {
        state.document = event.document
    }
}

Command UploadDocumentCmd {
    personId ref Person
    document File
}

UseCase UploadDocument handles UploadDocumentCmd {
    execute {
        person = load Person(cmd.personId)
        ensure person exists else PersonNotFound
        ref = store cmd.document
        person.AttachDocument(ref)
    }
}

Query GetDocumentUrl(personId PersonId) -> string {
    person = load Person(personId)
    ensure person exists else PersonNotFound
    return signed_url(person.state.document, expires: 15min)
}
```

Em domínios EventSourced/auditados, arquivos podem viver além do aggregate — por isso a remoção nunca é automática.

## 2.6. Funções Utilitárias (Built-in)

| Função | Retorno | Descrição |
|--------|---------|-----------|
| `now()` | `datetime` | Timestamp atual |
| `uuid()` | `string` | UUID v4 |
| `random(min, max)` | `integer` | Inteiro aleatório |
| `random_str(length)` | `string` | String aleatória |
| `signed_url(ref, expires:)` | `string` | URL temporária de FileStorage |

