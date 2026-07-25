# 📘 DomainScript Specification (v7.0)

## Architecture-as-Code DSL — Framework de Arquitetura como Linguagem

> **Domínio de referência:** os exemplos desta spec usam dois domínios coesos — **Carteira Digital** (Wallet) para construtos fundamentais e **Plataforma de Ingressos** (Ticketing) para fluxos distribuídos (Sagas, Policies, Workers). Cada construto novo reutiliza os tipos já apresentados.

---

## 1. Visão Geral e Filosofia

DomainScript não é uma linguagem de propósito geral. É uma DSL estritamente opinada para construir backends baseados em **Domain-Driven Design (DDD)** e **CQRS**.

### 1.1. O Paradigma

- **Regras Puras:** O desenvolvedor escreve apenas regras de negócio e contratos de dados.
- **Zero Infraestrutura:** Não há SQL, ORM, HTTP Client, injeção de dependência ou controle transacional no código de domínio.
- **Restrição Criativa (Fail-Fast):** A linguagem proíbe arquiteturas ruins em tempo de compilação. Se compila, a arquitetura está correta.
- **Transpilação:** DomainScript é transpilado para a linguagem alvo (ex: Go), aproveitando o ecossistema da plataforma destino.
- **Uma Forma Canônica:** Para cada operação existe uma única forma de expressá-la.
- **Exaustividade Obrigatória:** Toda ramificação de valor é exaustiva em tempo de compilação.
- **Observabilidade Nativa:** Instrumentação OpenTelemetry gerada automaticamente.
- **Deploy Derivado:** O compilador gera os artefatos de deploy a partir da topologia declarada.

### 1.2. Escopo

DomainScript foca **exclusivamente em sistemas transacionais empresariais** — backends com regras de negócio complexas, consistência forte, auditoria, integração entre módulos e times distribuídos.

**Não é uma solução universal, e isso é intencional:**

| Domínio fora de escopo | Razão |
|------------------------|-------|
| Streaming de alta frequência (IoT, market making) | Paradigma não-transacional |
| ML/AI workflows (training, feature stores) | Outro paradigma computacional |
| Algoritmos de grafo (recomendação, fraude relacional) | Query language não cobre traversal |
| Busca textual (full-text, fuzzy) | Requer engine especializada |
| Dados espaciais (geolocalização, polígonos) | Requer extensões espaciais |

Para estes, integre via Adapter ou FFI. A força da linguagem está em recusar a universalidade.

### 1.3. Estrutura de Arquivos

| Arquivo | Propósito |
|---------|-----------|
| `*.ds` | Código de domínio (ValueObjects, Aggregates, Commands, UseCases, Policies, Sagas, Workers, Metrics) |
| `*.test.ds` | Testes declarativos |
| `foreign/*.ds` | Declarações FFI (assinaturas tipadas de funções estrangeiras) |
| `mod.ds` | Infraestrutura do módulo (Database, FileStorage, Cache, RateLimit, Idempotency, Telemetry, Outbox) |
| `interface.ds` | Exposição via protocolos, tenant resolution, rate limit, versionamento |
| `topology.ds` | Topologia de deployment (services, canais) |
| `contracts/*.ds` | Eventos públicos compartilhados entre módulos |
| `versions/*.ds` | Transformações entre versões de API |
| `adapters/*`, `foreign/*` | Código na linguagem alvo (Adapters FFI e Foreign functions) |

---

## 2. Sistema de Tipos

### 2.1. A Regra de Ouro

Primitivos (`integer`, `decimal`, `string`, `boolean`, `datetime`, `bytes`) são **proibidos no Write Side** (Aggregates, Commands, Events). Permitidos dentro de ValueObjects/Enums e no Read Side (Views, Queries, Projections).

### 2.2. ValueObjects

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

### 2.3. Enums

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

### 2.4. Tipos de Coleção

| Tipo | Descrição | Operações |
|------|-----------|-----------|
| `List<T>` | Lista mutável ordenada | `add()`, `remove()`, `clear()`, indexação |
| `AppendList<T>` | Append-only. Compilador otimiza storage e paginação | `add()` apenas |
| `Set<T>` | Sem duplicatas | `add()`, `remove()`, `contains()`, `clear()` |
| `Map<K, V>` | Chave-valor | `put()`, `get()`, `remove()`, `containsKey()`, `keys()`, `values()` |

`AppendList<T>`: `remove()`/`clear()`/reatribuição → **erro de compilação**.

### 2.5. Tipo `File`

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

### 2.6. Funções Utilitárias (Built-in)

| Função | Retorno | Descrição |
|--------|---------|-----------|
| `now()` | `datetime` | Timestamp atual |
| `uuid()` | `string` | UUID v4 |
| `random(min, max)` | `integer` | Inteiro aleatório |
| `random_str(length)` | `string` | String aleatória |
| `signed_url(ref, expires:)` | `string` | URL temporária de FileStorage |

---

## 3. Controle de Fluxo

Duas formas canônicas: `ensure` (validação) e `match` (ramificação). Sem `if/else`, `while`, `do-while`, `switch`.

### 3.1. `ensure` (Guard Clause)

```ds
ensure [condição] else [ação]
```

| Contexto | `else` aceita |
|----------|---------------|
| Handle / UseCase | `Error` |
| Policy / Worker | `Error`, `Nop` |
| Dentro de `for` | `Error`, `Nop`, `break`, `break all`, `continue` |

`Nop` (retorno vazio silencioso) em Handle/UseCase → erro de compilação.

### 3.2. `match` (Pattern Matching)

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

### 3.3. `for` (Loop)

Único construto de iteração: coleção ou range. Loop condicional proibido no domínio.

```ds
for ticket in availableTickets { ... }
for i in 1..batch.quantity { ... }
```

`continue`, `break`, `break all` (loops aninhados). Fora de `for` → erro de compilação.

### 3.4. `log`

Disponível em qualquer contexto. Níveis: `debug`, `info`, `warn`, `error`. Compilador anexa trace context, timestamp e metadata.

```ds
log info "Saque realizado" {
    walletId = self.id
    amount = amount
}
```

---

## 4. O Núcleo do Domínio (Write Side)

### 4.1. Errors

```ds
Error InsufficientBalance { message "Saldo insuficiente." }
Error InactiveWallet      { message "Carteira inativa." }
Error WalletNotFound      { message "Carteira não encontrada." }
```

### 4.2. Events

Metadata implícito readonly: `timestamp`, `sequence`, `aggregateId`, `eventType`.

**Event vs PublicEvent** — interno ao módulo vs. compartilhado em `contracts/*.ds`. Policy cross-module escutando `Event` privado → erro de compilação.

```ds
Event WalletCreated { id WalletId, holder HolderName, email Email }
Event WithdrawalPerformed { id WalletId, amount Money, description TransactionDescription }
PublicEvent DepositPerformed { id WalletId, amount Money, description TransactionDescription }
```

### 4.3. Versionamento de Eventos

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

### 4.4. Redação de Eventos (GDPR)

Para direito ao esquecimento (GDPR Art. 17) sem corromper replay, campos podem ser `redactable`. A redação substitui o campo por placeholder tipado — replay continua funcionando, estrutura preservada, PII removida.

```ds
Event WalletCreated {
    id WalletId
    holder HolderName redactable
    email Email redactable
}
```

(Mecanismo de gatilho — feature em evolução.)

### 4.5. Aggregates

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

---

## 5. Camada de Aplicação

### 5.1. Commands

ValueObjects/Enums obrigatórios. `ref` para Type Safety. **Idempotency key implícita** em todo Command que muta estado (seção 15).

```ds
Command DepositCmd {
    walletId ref Wallet
    amount Money
    description TransactionDescription
}

Command TransferCmd {
    fromWalletId ref Wallet
    toWalletId ref Wallet
    amount Money
    description TransactionDescription
}
```

### 5.2. UseCases

Unit of Work implícito. Timeout com herança módulo → UseCase.

```ds
UseCase PerformDeposit handles DepositCmd {
    timeout 5s
    execute {
        wallet = load Wallet(cmd.walletId)
        ensure wallet exists else WalletNotFound
        wallet.Deposit(cmd.amount, cmd.description)
    }
}

UseCase PerformTransfer handles TransferCmd {
    execute {
        from = load Wallet(cmd.fromWalletId)
        ensure from exists else WalletNotFound
        to = load Wallet(cmd.toWalletId)
        ensure to exists else WalletNotFound

        from.SendTransfer(cmd.toWalletId, cmd.amount, cmd.description)
        to.ReceiveTransfer(cmd.fromWalletId, cmd.amount, cmd.description)
        // mesmo Database → commit atômico
    }
}
```

---

## 6. Leituras e Queries (Read Side)

Primitivos permitidos — o dado já foi validado por VO na borda de entrada.

### 6.1. Views

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

### 6.2. Field-Level Security

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

### 6.3. Queries

`load`, `list`, `count`, `join` (mesmo banco), `in`, `distinct`. `join` cross-database → erro, exige `Projection`. Bloco `cache` opcional (seção 16).

```ds
Query GetStatement(walletId WalletId, page int) -> List<StatementEntryVW> {
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

### 6.4. Projections (Cross-Database)

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

---

## 7. Policies (Reações a Eventos)

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

---

## 8. Workers (Background Processing)

Conceito único, três modos: `every`, `cron`, `continuous`. `scope: per_tenant` ou `global` (seção 14).

```ds
Worker ProcessExpiredReservations {
    schedule every 1min
    concurrency: 1
    timeout 5min
    onError { retry: { attempts: 3, backoff: "exponential" } }
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

---

## 9. Notifications & Adapters

### 9.1. Notifications

Contrato de saída. Sem `Adapter` correspondente → erro de compilação.

```ds
Notification DepositNotification { to Email, amount Money }
Notification PaymentRequest { paymentId PaymentId, amount Money, method PaymentMethod }
```

### 9.2. `notify` (async) vs `call` (sync)

```ds
notify DepositNotification(to: wallet.state.email, amount: event.amount)
result = call PaymentRequest(paymentId: payment.id, amount: total, method: cmd.paymentMethod)
```

### 9.3. Adapters

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

---

## 10. FFI Geral (`Foreign`)

Mecanismo para usar bibliotecas da linguagem alvo em algoritmos custom, **desacoplado de Notifications**. É o único buraco controlado na pureza do domínio — por isso é explícito, tipado e restrito por contexto.

### 10.1. Pure vs Impure

O dev declara explicitamente a natureza de cada função:

| Natureza | Definição | Exemplos |
|----------|-----------|----------|
| `pure` | Determinística, sem efeitos colaterais. Mesmo input → mesmo output. | Hash, verificação de assinatura, parsing, compressão |
| `impure` | Efeito colateral ou não-determinismo. | Geração de PDF com temp files, leitura de hardware, estado global |

### 10.2. Declaração

Assinaturas em `foreign/*.ds`; implementação na linguagem alvo:

```ds
// foreign/crypto.ds
Foreign "go" from "foreign/crypto" {
    pure function ComputeMerkleRoot(items List<bytes>) -> bytes
    pure function VerifySignature(message bytes, signature bytes, publicKey string) -> boolean
    pure function ValidateTaxId(taxId string) -> boolean throws InvalidTaxIdError
}

Foreign "go" from "foreign/documents" {
    impure function GeneratePdf(template string, data Map<string, string>) -> bytes
        throws PdfGenerationError
}
```

```go
// foreign/crypto/crypto.go
package crypto

func ComputeMerkleRoot(items [][]byte) []byte { ... }
func VerifySignature(message, signature []byte, publicKey string) bool { ... }
```

### 10.3. Marshalling (o que cruza a fronteira)

O compilador gera todo o marshalling automaticamente:

| Tipo DomainScript | Cruza? | Mapeamento (Go) |
|-------------------|--------|-----------------|
| Primitivos | ✅ | Tipos nativos |
| `List<T>`, `Set<T>`, `Map<K,V>` | ✅ | slice, map |
| ValueObject | ✅ | struct |
| Enum | ✅ | Valor do tipo base |
| Event | ✅ | struct de dados |
| `FileRef` / `File` / `FileStream` | ✅ | struct / bytes / reader |
| **Aggregate** | ❌ | **Erro de compilação** |

**Aggregates nunca atravessam** — têm identidade, ciclo de vida e fronteira transacional. Passe ValueObjects ou campos específicos.

### 10.4. Onde cada tipo pode ser chamado

| Contexto | `pure` | `impure` |
|----------|--------|----------|
| ValueObject (Valid/Operator) | ✅ | ❌ |
| Handle | ✅ | ⚠️ só se resultado for capturado em evento |
| **Apply** | ❌ | ❌ |
| UseCase / Saga / Policy / Worker | ✅ | ✅ |
| Query | ✅ | ❌ |

**Apply é hermético** — nem FFI pura. Depende só do evento e de built-ins, garantindo que replay anos depois produza o mesmo estado mesmo se a biblioteca mudou.

**Impure no Handle exige captura em evento** (mesmo princípio de `now()`/`uuid()`):

```ds
Handle SignDocument(content bytes) {
    signature = sign_via_hsm(content)      // impure — DEVE ir para o evento
    emit DocumentSigned(self.id, signature)
}

Apply DocumentSigned {
    state.signature = event.signature      // lê do evento, nunca re-executa
}
```

Resultado de impure usado em controle de fluxo do Handle sem captura → erro de compilação.

### 10.5. Erros

`throws DomainError` declara erros de negócio (HTTP 4xx). Qualquer falha não-mapeada (panic, timeout) é `InfraError` — sujeita a retry/circuit breaker do `mod.ds`.

### 10.6. Testing de FFI

| Natureza | Comportamento no teste |
|----------|------------------------|
| `pure` | Executa de verdade (determinística) ou golden value via `mock` |
| `impure` | Mockada por padrão, como Adapters |

```ds
scenario "assinatura de documento" {
    mock sign_via_hsm returns "SIGNATURE_BYTES"
    when SignDocument(content: ...)
    then [ DocumentSigned(signature: "SIGNATURE_BYTES") ]
}
```

### 10.7. Diretrizes

- Função que precisa de config/credenciais → deveria ser **Adapter**, não FFI (compilador orienta).
- Volumes grandes → `FileStream`, não passagem em memória.
- Declarar `pure` para função com estado interno é bug do dev — não detectável pelo compilador.
- FFI impura dentro de transação → warning: efeito não é revertido em rollback; considere Saga.

---

## 11. Interface (`interface.ds`)

Exposição via HTTP, gRPC, TCP, UDP. Hospeda tenant resolution, versionamento e rate limit. Domínio não sabe qual protocolo é usado.

```ds
Interface HTTP {
    port: env("HTTP_PORT")
    basePath: "/api"

    versioning {
        strategy: path
        current: v2
        supported: [v1, v2]
    }

    tenant {
        from: subdomain     // ou: header "X-Tenant-Id", jwt_claim "tenant_id", path
    }

    rateLimit {
        perIp: 1000/min
        perUser: 300/min
    }

    POST "/wallets"                       -> CreateWallet
    POST "/wallets/{walletId}/deposit"    -> PerformDeposit {
        rateLimit { perUser: 60/min, burst: 10 }
    }
    GET  "/wallets/{walletId}"            -> GetWalletSummary
    GET  "/wallets/{walletId}/statement"  -> GetStatement

    POST "/login"  -> Login       { tenancy: none, rateLimit: { perIp: 10/min, onBackendFailure: closed } }
    GET  "/health" -> HealthCheck { tenancy: none }
}

Interface GRPC {
    port: env("GRPC_PORT")
    service WalletService {
        rpc Deposit   -> PerformDeposit
        rpc GetWallet -> GetWalletSummary
    }
}
```

Mapeamento automático: path params → `ref`/parâmetro, query params → query string, body → JSON, status codes (`200/201/422/404/429/503`). Compilador gera `.proto` para gRPC.

---

## 12. Topologia (`topology.ds`)

Services agrupam módulos. Um service = monólito (implícito). Múltiplos = microsserviços. Canais entre módulos do mesmo service = monólito modular.

```ds
Topology {
    services {
        CarteiraService { modules: [Carteira] }
        NotificacoesService { modules: [Notificacoes] }
        PagamentosService { modules: [Pagamentos] }
    }
    channels {
        Carteira -> Notificacoes {
            via: queue
            provider: "rabbitmq"
            connection: env("RABBITMQ_URL")
            orderBy: aggregateId
            workers { concurrency: 5, maxRate: 100, batchSize: 10 }
        }
        Carteira -> Pagamentos {
            via: grpc
            connection: env("PAGAMENTOS_GRPC_URL")
            timeout: 10s
            circuitBreaker: { threshold: 5, cooldown: 30s }
        }
    }
}
```

Canais: `direct` (default), `queue`, `grpc`, `http`, `stream`. Mudar topologia não altera domínio; compilador revalida (cross-service sem Saga → erro). Tenant e trace context propagados automaticamente.

---

## 13. Infraestrutura de Módulo (`mod.ds`)

Configuração completa de um módulo — cada bloco alimenta uma feature da linguagem e a geração de deploy (seção 21):

```ds
Module Carteira {
    timeout 30s

    Database WalletDb {
        provider: "Postgres"
        connection: env("DB_URL")
        supportsXA: true
        manages: [Wallet]
        retry: { attempts: 3, backoff: "exponential" }
        circuitBreaker: { threshold: 5, cooldown: 30s }
        tenancy: { strategy: row_level, column: "tenant_id" }
    }

    FileStorage DocumentStorage {
        provider: "s3"
        bucket: env("DOCUMENTS_BUCKET")
        region: env("AWS_REGION")
    }

    Idempotency {
        storage: same
        window: 24h
        required: true
        concurrentRetry: wait
    }

    Cache {
        backend: layered
        layers: [
            { type: memory, maxSize: 100MB, ttl: 30s },
            { type: redis, connection: env("REDIS_URL"), ttl: 5min }
        ]
        defaultTtl: 1min
        stampedeProtection: true
    }

    RateLimit {
        backend: redis
        connection: env("REDIS_URL")
        algorithm: token_bucket
        onBackendFailure: open
    }

    Outbox {
        pollInterval: 1s
        batchSize: 50
        concurrency: 3
    }

    Telemetry {
        exporter: "otlp"
        endpoint: env("OTEL_EXPORTER_ENDPOINT")
        traces { sampler: "parentbased_traceidratio", sampleRate: 0.1 }
        metrics { interval: 30s }
        logs { level: "info", format: "json" }
    }
}
```

---

## 14. Multi-Tenancy

Tenant é **ambient context** (como `caller`). Nunca parâmetro explícito.

| Estratégia | Isolamento | Quando usar |
|------------|-----------|-------------|
| `row_level` | Coluna `tenant_id` | Alta escala, muitos tenants pequenos |
| `schema_per_tenant` | Schema dedicado | Escala média |
| `database_per_tenant` | Banco separado | Regulados, tenants grandes |

- Resolução na borda (`interface.ds`): `subdomain`, `header`, `jwt_claim`, `path`.
- Contexto: `tenant.id`, `tenant.tier`, `tenant.exists`.
- **Filtro automático** em queries e loads. Aggregate de outro tenant → 404.
- **Cross-tenant opt-in**: `tenancy: cross_tenant` + role privilegiada + auditoria automática + warning.
- `Aggregate Tenant` em módulo Platform (sem tenancy). Provisionamento via `provision tenant(id)` numa Policy.
- Channels propagam tenant automaticamente. Workers: `scope: per_tenant`/`global`. Tenant ausente → 400 (fail-closed). Rotas sem tenant: `{ tenancy: none }`.

```ds
UseCase GenerateGlobalReport handles GlobalReportCmd {
    tenancy: cross_tenant
    access { requires caller.hasRole("super_admin") }
    execute {
        allWallets = list Wallet take 10000
    }
}
```

---

## 15. Idempotência de Commands

Chave fornecida pelo cliente (sem fallback). Metadata implícito em todo Command que muta estado.

| Protocolo | Como enviar |
|-----------|-------------|
| HTTP | Header `Idempotency-Key` |
| gRPC | Metadata `idempotency-key` |
| TCP/UDP | Campo no header da mensagem |

- **Storage:** `same` (atômico com a transação) ou `external` (Redis/Dynamo).
- **Cache de resultado:** sucesso ✅, erro de negócio ✅, erro de infra ❌ (permite retry).
- **Race** (mesma chave em paralelo): `wait` ou `reject`. **Conflito** (mesma chave, command diferente): 422 `IdempotencyKeyConflict`.
- Limpeza via Worker automático. Para Sagas: chave (entrada) → `sagaId` (saída), mapeamento estável. Retry idempotente não consome rate limit.

```ds
UseCase PerformDeposit handles DepositCmd {
    idempotency { required: true, window: 48h }
    execute { ... }
}
```

---

## 16. Cache

Política na Query, backend no `mod.ds`.

```ds
Query GetWalletSummary(walletId WalletId) -> WalletSummaryVW {
    cache {
        ttl: 5min
        invalidateOn: [DepositPerformed, WithdrawalPerformed]  // override; default é inferido
        negativeCacheTtl: 10s
    }
    return load Wallet(walletId) as WalletSummaryVW
}
```

| Aspecto | Comportamento |
|---------|---------------|
| Backends | `memory`, `distributed`, `layered` |
| Invalidação | Por evento, inferida dos aggregates tocados (override `invalidateOn`) |
| Granularidade | Cirúrgica (por ID), grossa (listagens — warning se alta cardinalidade) |
| Cache stampede | Request coalescing por construção |
| Adapters | Só `mode sync` + GET |
| Bypass | Header `Cache-Control: no-cache` |
| Multi-tenancy | Tenant na chave automaticamente |
| Falha do backend | Fail-open |
| EventBus assíncrono | Invalidação in-process imediata após `emit`, antes da fila externa |

---

## 17. Rate Limiting

Política no `interface.ds`, backend no `mod.ds`.

- **Dimensões:** `perIp`, `perUser`, `perTenant`, `perApiKey`, `global` — todas precisam passar.
- **Algoritmos:** `token_bucket` (padrão, com `burst`), `sliding_window`, `fixed_window`.
- **Tier-based (feature de plano SaaS):**

```ds
RateLimitTier Free { perUser: 100/min, perTenant: 1000/min }
RateLimitTier Pro  { perUser: 1000/min, perTenant: 20000/min }

POST "/search" -> SearchEvents {
    rateLimit: byTier    // resolve de tenant.tier
}
```

- Resposta automática: 429 + `Retry-After` + `X-RateLimit-*`. gRPC: `RESOURCE_EXHAUSTED`.
- **Falha do backend:** `open` (padrão) ou `closed`, override por endpoint.
- Endpoints sem tenant: só `perIp`. Retry idempotente não consome cota.

---

## 18. Versionamento de API

Domínio conhece só a versão atual. Tradução na borda (simétrico ao Upcast de eventos).

```ds
// versions/v1.ds
Version v1 {
    deprecated: "2026-01-01"
    sunset: "2026-06-01"

    upcast DepositCmd {
        from { value decimal, currency string, description string }
        to {
            amount = Money(amount: value, currency: currency)
            description = TransactionDescription(description)
            channel = Channel("legacy")
        }
    }

    downcast WalletSummaryVW {
        to {
            balance = self.balance_amount
            currency = self.balance_currency
            owner = self.holder
        }
    }

    // Mudança semântica → UseCase diferente
    route "/wallets/{walletId}/transfer" -> PerformLegacyTransfer
}
```

| Fase | Comportamento automático |
|------|--------------------------|
| Após `deprecated` | Headers `Deprecation`/`Sunset` + warning na compilação |
| Após `sunset` | 410 Gone |

Endpoints inalterados passam direto. Campo obrigatório sem default no upcast → erro. Validação é sempre a atual.

---

## 19. Transações e Sagas

### 19.1. Inferência Transacional

| Cenário | Comportamento |
|---------|---------------|
| Mesmo `Database` | Commit local |
| Diferentes, ambos XA | 2PC automático |
| Diferentes, sem XA | ❌ Erro — exige Saga |
| Cross-service sem Saga | ❌ Erro |

### 19.2. Sagas

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

---

## 20. Observabilidade (OpenTelemetry)

- **Traces automáticos** para todo construto; propagação cross-service (W3C headers em grpc/queue/stream/http).
- **Métricas automáticas** (duration, counters, gauges) por UseCase, Aggregate, Saga, Policy, Worker, Channel, Adapter.
- **Logs automáticos** + `log` explícito.
- **Métricas de negócio declarativas:**

```ds
Metric DepositVolume {
    type counter
    value event.amount.amount
    on DepositPerformed
    labels { currency = event.amount.currency }
}

Metric PurchaseLatency {
    type histogram
    buckets [100ms, 250ms, 500ms, 1s, 2s, 5s]
    on PurchaseTickets.completed
}
```

---

## 21. Geração de Artefatos de Deploy

O compilador conhece toda a topologia e infraestrutura — a geração de deploy é derivada, não declarada. Suporte inicial: **Dockerfile** e **docker-compose**.

### 21.1. Fontes de Inferência

| Fonte | Informação derivada |
|-------|---------------------|
| `topology.ds` — services | Containers de aplicação (um binário Go por service) |
| `topology.ds` — channels | Message brokers (RabbitMQ, Kafka) |
| `mod.ds` — Database | Bancos (Postgres, Mongo) com healthchecks |
| `mod.ds` — Cache / RateLimit / Idempotency external | Redis/Memcached |
| `mod.ds` — FileStorage | MinIO (dev) / S3 (prod) |
| `mod.ds` — Telemetry | OpenTelemetry Collector |
| `interface.ds` | Portas expostas |

### 21.2. Dockerfile (um por service)

Multi-stage build, usuário não-root. Service worker-only (sem `interface.ds`) não expõe porta. FFI com dependência C → compilador habilita CGO e dependências no estágio de build.

```dockerfile
# Gerado: docker/carteira-service.Dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/carteira-service ./cmd/carteira-service

FROM alpine:3.20
RUN adduser -D -u 10001 appuser
COPY --from=build /bin/carteira-service /bin/carteira-service
USER appuser
EXPOSE 8080
ENTRYPOINT ["/bin/carteira-service"]
```

### 21.3. docker-compose

Services da aplicação + infraestrutura inferida, com `depends_on` ordenado por healthcheck:

```yaml
services:
  carteira-service:
    build: { context: ., dockerfile: docker/carteira-service.Dockerfile }
    ports: ["8080:8080"]
    environment:
      DB_URL: postgres://user:pass@carteira-db:5432/carteira
      REDIS_URL: redis://cache:6379
      RABBITMQ_URL: amqp://rabbitmq:5672
      OTEL_EXPORTER_ENDPOINT: http://otel-collector:4317
    depends_on:
      carteira-db: { condition: service_healthy }
      rabbitmq: { condition: service_healthy }

  carteira-db:
    image: postgres:16-alpine
    volumes: [carteira-db-data:/var/lib/postgresql/data]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U user"]
      interval: 5s
      retries: 5

  cache:
    image: redis:7-alpine

  rabbitmq:
    image: rabbitmq:3-management-alpine
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "ping"]

  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    volumes: [./docker/otel-collector-config.yaml:/etc/otel-collector-config.yaml]

volumes:
  carteira-db-data:
```

### 21.4. Regras de Geração

| Regra | Comportamento |
|-------|---------------|
| Deduplicação de infra | Por connection string: mesma URL = container compartilhado |
| Perfis | `--profile=dev` (tudo local: MinIO, ElasticMQ, containers de banco) / `--profile=prod` (referências externas) |
| Provider cloud sem equivalente local (dev) | ⚠️ Warning |
| Variáveis de ambiente | `.env.example` gerado de todos os `env(...)` do código, com defaults dev apontando para os containers |
| Configs auxiliares | OTEL Collector config (do bloco `Telemetry`), migrations SQL (do schema dos aggregates: state, snapshot, event store, outbox, idempotency) |
| Worker-only service | Sem `ports`/`EXPOSE` |

### 21.5. Comando e Estrutura de Saída

```
ds build --target=docker-compose --profile=dev
```

```
build/
├── cmd/<service>/main.go
├── docker/<service>.Dockerfile
├── docker/otel-collector-config.yaml
├── migrations/<module>/001_init.sql
├── docker-compose.yml
├── .env.example
└── go.mod
```

Resultado: `docker compose up` sobe o sistema completo — aplicação, bancos, filas, cache e observabilidade — sem o dev escrever uma linha de YAML.

---

## 22. Smart Partial Loading

```ds
item = state.items.focus(itemId)              // SELECT * WHERE parent_id=X AND id=Y
ensure state.items.sum(i => i.price) < 10000  // SELECT SUM(...) sem materializar
```

`AppendList<T>` com `skip/take` → paginação nativa. Fallback: carrega aggregate todo.

---

## 23. Erros: Negócio vs. Infraestrutura

| Tipo | Declaração | Tratamento |
|------|-----------|------------|
| Negócio | `Error` no domínio (ou `throws` em Foreign) | HTTP 4xx |
| Infraestrutura | Nunca no domínio | `mod.ds` (retry, circuit breaker) + `onInfraError` |

---

## 24. Testing Nativo (`*.test.ds`)

Teste declarativo Given-When-Then, executado em memória. Validado contra o domínio em tempo de compilação.

### 24.1. Aggregate

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

### 24.2. UseCase (asserção transacional)

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

### 24.3. Mock de Adapters/FFI e Saga

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

### 24.4. Policy e Query

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

### 24.5. Property-Based

```ds
Test Wallet {
    property "saldo nunca fica negativo" {
        forall sequence of [Deposit, Withdraw, Transfer]
        invariant state.balance >= Money(0, "BRL")
    }
}
```

Compilador gera sequências válidas e reporta o contra-exemplo mínimo em falha.

### 24.6. Fixtures

```ds
Fixture activeWallet {
    Wallet("W1") from [
        WalletCreated(id: "W1", holder: "João", email: "joao@x.com"),
        DepositPerformed(id: "W1", amount: Money(100, "BRL"), description: "init")
    ]
}
```

### 24.7. Garantias e Cobertura Semântica

| Situação | Resultado |
|----------|-----------|
| Evento/comando inexistente no teste | ❌ Erro |
| Shape de evento esperado errada | ❌ Erro |
| Mock com retorno de tipo errado | ❌ Erro |
| `fail step X` inexistente | ❌ Erro |
| Handle sem cenário de erro testado | ⚠️ Warning |

Cobertura por Handle e ramo (não por linha): o compilador reporta exatamente quais regras e caminhos de erro não têm teste.

---

## 25. Regras de Compilação (Resumo)

| Regra | Resultado |
|-------|-----------|
| Primitivo no Write Side | ❌ Erro |
| Handle sem entrada no `access` | ❌ Erro |
| `Notification` sem `Adapter` | ❌ Erro |
| `remove()`/`clear()` em `AppendList<T>` | ❌ Erro |
| UseCase cross-database sem XA / cross-service sem Saga | ❌ Erro |
| JOIN cross-database | ❌ Erro |
| `match` não-exaustivo / guards sem `_` | ❌ Erro |
| `Nop` em Handle/UseCase | ❌ Erro |
| `break`/`continue` fora de `for` | ❌ Erro |
| Policy cross-module escutando `Event` não público | ❌ Erro |
| Módulos em services diferentes sem canal | ❌ Erro |
| Acesso cross-tenant sem opt-in | ❌ Erro |
| Upcast de API com campo obrigatório sem default | ❌ Erro |
| Teste referenciando evento/comando inexistente | ❌ Erro |
| FFI/Adapter com assinatura incompatível | ❌ Erro |
| Aggregate cruzando fronteira FFI | ❌ Erro |
| FFI em `Apply` (pure ou impure) | ❌ Erro |
| FFI impura em Handle sem captura em evento | ❌ Erro |
| FFI impura em Query/ValueObject | ❌ Erro |
| Idempotency conflito (mesma chave, command diferente) | ❌ 422 |
| Canal `queue`/`stream` sem `orderBy` | ⚠️ Warning |
| Saga `await` sobre canal `queue` | ⚠️ Warning |
| Upcast substituível por default | ⚠️ Warning |
| ValueObject que poderia ser Enum | ⚠️ Warning |
| Cache em listagem de alta cardinalidade | ⚠️ Warning |
| UseCase cross-tenant declarado | ⚠️ Warning (auditoria) |
| Handle sem cenário de erro testado | ⚠️ Warning |
| UseCase/Query não exposto em interface | ⚠️ Warning |
| FFI impura dentro de transação | ⚠️ Warning (efeito não revertido) |
| Provider cloud sem equivalente local (profile dev) | ⚠️ Warning |

---

## 26. Glossário

| Conceito | Descrição |
|----------|-----------|
| **ValueObject** | Tipo imutável com validação e comportamento. |
| **Enum** | Conjunto fechado de valores nomeados. Coerção na borda. |
| **File / FileStream / FileRef** | Bytes em memória / streaming / referência leve no state. |
| **Aggregate** | Fronteira transacional. State, handles, access, storage. Nunca cruza FFI. |
| **Command** | DTO de entrada. Idempotency key implícita. |
| **UseCase** | Unit of Work implícito. |
| **Event / PublicEvent** | Fato imutável interno / compartilhado. Campos `redactable` (GDPR). |
| **Policy** | Reação a eventos. `BestEffort`/`AtLeastOnce`. |
| **Worker** | Background. `every`/`cron`/`continuous`. `scope` per_tenant/global. |
| **Notification / Adapter** | Contrato de saída / fronteira de infra (HTTP, FFI). |
| **Foreign** | FFI geral. `pure`/`impure`. Apply hermético. Captura em evento. |
| **Saga** | State-machine distribuída. `async`/`await`. `unrecoverable`. |
| **View** | Read-only. `visibility` para field-level security. |
| **Projection** | View materializada cross-aggregate. |
| **Query** | Consulta declarativa. Bloco `cache`. |
| **Tenant** | Ambient context. `row_level`/`schema`/`database` per tenant. |
| **Idempotency** | Chave do cliente. Storage `same`/`external`. |
| **Cache** | `memory`/`distributed`/`layered`. Invalidação por evento. |
| **RateLimit** | Por dimensão ou tier. Token bucket padrão. |
| **Version** | `upcast`/`downcast` de API. `deprecated`/`sunset`. |
| **Test** | Given-When-Then. Property-based. Cobertura semântica. |
| **Telemetry** | OpenTelemetry nativo. |
| **Deploy** | Dockerfile + docker-compose gerados da topologia. Perfis dev/prod. |

---

## 27. Funcionalidades em Evolução

| Feature | Status |
|---------|--------|
| Redação de eventos (GDPR) — gatilho de redação | Design inicial, em evolução |
| Field-level security — casos avançados | Design inicial, em evolução |
| FFI geral — detalhes finos de marshalling | Especificado, refinamento contínuo |
| Deploy — targets adicionais (Kubernetes, Terraform) | Planejado |
| Operações SQL adicionais (avg, min, max, group by) | Planejado |
| Funções utilitárias adicionais | Planejado |
| Cost-based rate limiting | Segunda fase |
| Encadeamento de versões intermediárias (API) | Evolução futura |
| Nível de suporte a operações aritméticas e booleanas | A definir |
