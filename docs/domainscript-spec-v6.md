# 📘 DomainScript Specification (v6.0)

## Architecture-as-Code DSL — Framework de Arquitetura como Linguagem

---

## 1. Visão Geral e Filosofia

DomainScript não é uma linguagem de propósito geral. É uma DSL estritamente opinada para construir backends baseados em **Domain-Driven Design (DDD)** e **CQRS**.

### 1.1. O Paradigma

- **Regras Puras:** O desenvolvedor escreve apenas regras de negócio e contratos de dados.
- **Zero Infraestrutura:** Não há SQL, ORM, HTTP Client, injeção de dependência ou controle transacional no código de domínio. O compilador transpila o DomainScript para a linguagem/infraestrutura alvo.
- **Restrição Criativa (Fail-Fast):** A linguagem proíbe arquiteturas ruins em tempo de compilação. Se compila, a arquitetura está correta.
- **Transpilação:** DomainScript não gera binário diretamente — é transpilado para a linguagem alvo (ex: Go), aproveitando o ecossistema da plataforma destino.
- **Uma Forma Canônica:** Para cada operação existe uma única forma de expressá-la.
- **Exaustividade Obrigatória:** Toda ramificação de valor é exaustiva em tempo de compilação.
- **Observabilidade Nativa:** O compilador gera instrumentação OpenTelemetry automaticamente.

### 1.2. Escopo (O que DomainScript É e Não É)

DomainScript foca **exclusivamente em sistemas transacionais empresariais** — backends com regras de negócio complexas, consistência forte, auditoria, integração entre módulos e times distribuídos.

**Não é uma solução universal, e isso é intencional.** Para os domínios abaixo, a recomendação é usar linguagens de propósito geral (ou ferramentas especializadas) e integrar via Adapter:

| Domínio fora de escopo | Razão |
|------------------------|-------|
| Streaming de alta frequência (IoT, market making) | Paradigma não-transacional, milhares de eventos/segundo |
| ML/AI workflows (training, feature stores) | Outro paradigma computacional |
| Algoritmos de grafo (recomendação, fraude relacional) | Query language não cobre traversal |
| Busca textual (full-text, fuzzy) | Requer engine especializada (Elasticsearch) |
| Dados espaciais (geolocalização, polígonos) | Requer extensões espaciais (PostGIS) |

A força da linguagem está exatamente em recusar a universalidade e fazer uma coisa muito bem.

### 1.3. Estrutura de Arquivos

| Arquivo | Propósito |
|---------|-----------|
| `*.ds` | Código de domínio (ValueObjects, Aggregates, Commands, UseCases, Policies, Sagas, Workers, Metrics) |
| `*.test.ds` | Testes declarativos do domínio |
| `mod.ds` | Infraestrutura do módulo (Database, FileStorage, Cache, RateLimit, Idempotency, Telemetry, Outbox) |
| `interface.ds` | Exposição via protocolos (HTTP, gRPC, TCP, UDP), tenant resolution, rate limit, versioning |
| `topology.ds` | Topologia de deployment (services, canais) |
| `contracts/*.ds` | Eventos públicos compartilhados entre módulos |
| `versions/*.ds` | Transformações entre versões de API |
| `adapters/*` | Código FFI na linguagem alvo |

---

## 2. Sistema de Tipos

### 2.1. A Regra de Ouro

Primitivos (`integer`, `decimal`, `string`, `boolean`, `datetime`) são **proibidos no Write Side** (Aggregates, Commands, Events). Permitidos dentro de ValueObjects/Enums e no Read Side (Views, Queries, Projections).

### 2.2. ValueObjects

Blocos atômicos, imutáveis, auto-validáveis, com comportamento (operadores).

```ds
ValueObject Email(string) {
    Valid { self.contains("@") }
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
```

### 2.3. Enums

Conjunto fechado de valores nomeados sob namespace e tipo. Permitidos no Write Side.

```ds
Enum TransactionType : string {
    Deposit     = "DEPOSIT"
    Withdrawal  = "WITHDRAWAL"
    TransferIn  = "TRANSFER_IN"
    TransferOut = "TRANSFER_OUT"
}

Enum Priority : integer {
    Low = 1
    Medium = 2
    High = 3
}
```

**Coerção implícita** (padrão): conversão bidirecional automática na borda. Valor desconhecido → erro 422.

**Coerção explícita** (`coerce`): para aliases, legados, case-insensitive.

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

| Tipo | Descrição |
|------|-----------|
| `List<T>` | Lista mutável ordenada |
| `AppendList<T>` | Append-only. Só `add()`. Compilador otimiza storage e paginação. |
| `Set<T>` | Coleção sem duplicatas |
| `Map<K, V>` | Dicionário chave-valor |

### 2.5. Tipo `File`

Arquivos são conceitualmente ValueObjects, mas operacionalmente especiais: bytes são caros de carregar, exigem storage dedicado e têm ciclo de vida próprio. Por isso há três tipos relacionados.

| Tipo | Função |
|------|--------|
| `File` | VO com bytes. Usado em Commands e operações transitórias. Carregado em memória. |
| `FileStream` | Variante para arquivos grandes. Upload/download chunk-a-chunk. |
| `FileRef` | Referência leve com metadata. Vive no Aggregate state. Nunca carrega bytes implicitamente. |

```ds
// FileRef — o que vive no state
FileRef {
    id FileId
    name string
    contentType string
    size integer
    metadata Map<string, string>
    storedAt datetime
}

// File — conteúdo transitório (Command, processamento)
File {
    name string
    contentType string
    size integer
    metadata Map<string, string>
    buffer bytes
}
```

**Operações:**

| Operação | Comportamento |
|----------|---------------|
| `store cmd.file` | Faz upload ao storage e retorna `FileRef` automaticamente |
| `load File(ref)` | Carrega os bytes explicitamente |
| `signed_url(ref, expires:)` | Gera URL temporária do storage |
| `delete file(ref)` | Remove do storage (ciclo de vida explícito, responsabilidade do dev) |

```ds
// Aggregate guarda só a referência, configurando storage por campo
Aggregate Person {
    storage {
        state: PersonDb
        document: DocumentStorage    // FileRef de document → este storage
        avatar: AvatarStorage
    }

    state {
        name HolderName
        document FileRef
        avatar FileRef
    }

    Handle AttachDocument(ref FileRef) {
        emit DocumentAttached(self.id, ref)
    }

    Apply DocumentAttached {
        state.document = event.document
    }
}

// UseCase — store faz upload e devolve FileRef
UseCase UploadDocument handles UploadDocumentCmd {
    execute {
        person = load Person(cmd.personId)
        ensure person exists else PersonNotFound
        ref = store cmd.document
        person.AttachDocument(ref)
    }
}

// Download via URL assinada
Query GetDocumentUrl(personId PersonId) -> string {
    person = load Person(personId)
    ensure person exists else PersonNotFound
    return signed_url(person.state.document, expires: 15min)
}
```

O ciclo de vida do arquivo é **explícito** — só removido via `delete file(ref)`. Em domínios com auditoria ou EventSourced, arquivos podem viver além do aggregate.

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

### 3.1. `ensure` (Guard Clause)

```ds
ensure [condição] else [ação]
```

| Contexto | `else` aceita |
|----------|---------------|
| Handle / UseCase | `Error` |
| Policy / Worker | `Error`, `Nop` |
| Dentro de `for` | `Error`, `Nop`, `break`, `break all`, `continue` |

`Nop` em Handle/UseCase → erro de compilação.

### 3.2. `match` (Pattern Matching)

Sempre exaustivo. Sobre enum/conjunto fechado: wildcard `_` proibido. Com guards (`when`): wildcard obrigatório.

```ds
match ticket.state.status {
    TicketStatus.Available => ticket.Reserve(orderId, userId)
    TicketStatus.Reserved  => ticket.ConfirmSale(orderId, holder)
    TicketStatus.Sold      => Nop
    TicketStatus.Cancelled => Nop
    TicketStatus.Used      => Nop
}

// Como expressão
label = match entry.type {
    TransactionType.Deposit     => "Depósito recebido"
    TransactionType.Withdrawal  => "Saque realizado"
    TransactionType.TransferIn  => "Transferência recebida"
    TransactionType.TransferOut => "Transferência enviada"
}

// Com guards
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

`continue`, `break`, `break all` (aninhado). Fora de `for` → erro de compilação.

### 3.4. `log`

Disponível em qualquer contexto. Compilador anexa trace context, timestamp e metadata. Níveis: `debug`, `info`, `warn`, `error`.

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
```

### 4.2. Events

Metadata implícito readonly: `timestamp`, `sequence`, `aggregateId`, `eventType`.

**Event vs PublicEvent** — interno ao módulo vs. compartilhado em `contracts/*.ds`. Policy cross-module escutando `Event` privado → erro de compilação.

```ds
Event WalletCreated { id WalletId, holder HolderName, email Email }
PublicEvent DepositPerformed { id WalletId, amount Money, description TransactionDescription }
```

### 4.3. Versionamento de Eventos

Campos novos com `default`. Transformações complexas com `Upcast`.

```ds
Event DepositPerformed {
    id WalletId
    amount Money
    channel Channel = Channel("unknown")
}

Upcast TransferSent v1 -> v2 {
    fee = Money(amount: 0, currency: event.amount.currency)
}
```

### 4.4. Redação de Eventos (GDPR)

Para conformidade com direito ao esquecimento (GDPR Art. 17) sem corromper o replay, campos podem ser marcados como `redactable`. Quando redigidos, são substituídos por um valor neutro mantendo a estrutura do evento válida.

```ds
Event WalletCreated {
    id WalletId
    holder HolderName redactable
    email Email redactable
}
```

A operação de redação substitui o campo por um placeholder tipado. O replay continua funcionando; auditoria preserva a estrutura; PII é removida. (Mecanismo detalhado e gatilho de redação — feature em evolução.)

### 4.5. Aggregates

`StateStored` (padrão) ou `EventSourced`. Snapshot opt-in. Acesso nativo via `events()`. Bloco `access` closed-by-default.

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

---

## 5. Camada de Aplicação

### 5.1. Commands

ValueObjects/Enums obrigatórios. `ref` para Type Safety. **Idempotency key implícita** (ver seção 14).

```ds
Command DepositCmd {
    walletId ref Wallet
    amount Money
    description TransactionDescription
}
```

### 5.2. UseCases

Unit of Work implícito. Timeout configurável.

```ds
UseCase PerformDeposit handles DepositCmd {
    timeout 5s
    execute {
        wallet = load Wallet(cmd.walletId)
        ensure wallet exists else WalletNotFound
        wallet.Deposit(cmd.amount, cmd.description)
    }
}
```

---

## 6. Leituras e Queries (Read Side)

### 6.1. Views

```ds
View WalletSummaryVW From Wallet

View StatementEntryVW {
    type string
    amount_value decimal
    amount_currency string
    description string
    date datetime
}
```

### 6.2. Field-Level Security

O bloco `access` do Aggregate controla **quem invoca** Handles. Para controlar **quem vê quais campos** numa View, usa-se o bloco `visibility`.

```ds
View WalletSummaryVW From Wallet {
    visibility {
        balance      requires caller.id == self.id or caller.hasRole("admin")
        holder       requires caller.authenticated
        email        requires caller.id == self.id or caller.hasRole("support")
        // campos não listados: visíveis a qualquer caller autorizado pela Query
    }
}
```

Campos não autorizados são omitidos da serialização (não retornam `null` — simplesmente não aparecem), evitando vazamento de existência. (Mecanismo em evolução.)

### 6.3. Queries

`load`, `list`, `count`, `join` (mesmo banco), `in`, `distinct`. `join` cross-database → erro, exige `Projection`. Primitivos permitidos nos parâmetros.

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

### 6.4. Cross-Aggregate (Projection)

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

`BestEffort` ou `AtLeastOnce`. Usam `emit` para fluxos internos.

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

Três modos: `every`, `cron`, `continuous`.

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

`scope: per_tenant` ou `scope: global` (ver Multi-tenancy).

---

## 9. Notifications & Adapters

### 9.1. Notifications

Contrato de saída. Sem `Adapter` correspondente → erro de compilação.

```ds
Notification DepositNotification { to Email, amount Money }
```

### 9.2. `notify` (async) vs `call` (sync)

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

**Nível 2 — FFI (vinculado a Notification):**

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

### 9.4. FFI Geral

Para usar bibliotecas da linguagem alvo em algoritmos custom, desacoplado de Notifications (ex: criptografia, parsing proprietário).

```ds
Foreign "go" from "internal/crypto" {
    function ComputeMerkleRoot(items List<bytes>) -> bytes
    function VerifySignature(message bytes, signature bytes, key string) -> boolean
}

// Uso em qualquer contexto do domínio
hash = ComputeMerkleRoot(items)
```

Assinatura incompatível → erro de compilação. (Mecanismo em evolução.)

---

## 10. Interface (`interface.ds`)

Exposição via HTTP, gRPC, TCP, UDP. Domínio não sabe qual protocolo é usado.

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

    // Rotas sem tenant
    POST "/login"    -> Login         { tenancy: none, rateLimit: { perIp: 10/min, onBackendFailure: closed } }
    POST "/register" -> RegisterUser  { tenancy: none }
    GET  "/health"   -> HealthCheck   { tenancy: none }
}

Interface GRPC {
    port: env("GRPC_PORT")
    service WalletService {
        rpc Deposit -> PerformDeposit
        rpc GetWallet -> GetWalletSummary
    }
}
```

Mapeamento automático: path params → `ref`/parâmetro de Query, query params → query string, request body → JSON, status codes (`200`, `201`, `422`, `404`, `429`, `503`). Compilador gera `.proto` para gRPC.

---

## 11. Topologia (`topology.ds`)

Services agrupam módulos. Um service = monólito (implícito). Múltiplos = microsserviços.

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

Canais: `direct` (default in-process), `queue`, `grpc`, `http`, `stream`. Podem ser declarados no mesmo service (monólito modular). Mudar topologia não altera domínio; compilador revalida (cross-service sem Saga → erro).

---

## 12. Infraestrutura de Módulo (`mod.ds`)

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
        concurrentTimeout: 30s
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

## 13. Multi-tenancy

Tenant é **ambient context**, injetado pelo compilador (como `caller`). Nunca parâmetro explícito.

### 13.1. Estratégias

| Estratégia | Isolamento | Quando usar |
|------------|-----------|-------------|
| `row_level` | Filtro por coluna `tenant_id` | Alta escala, muitos tenants pequenos |
| `schema_per_tenant` | Schema dedicado | Escala média |
| `database_per_tenant` | Banco separado | Regulados, tenants grandes |

Configurado no `Database` do `mod.ds`. Resolução na borda (`interface.ds`): `subdomain`, `header`, `jwt_claim`, `path`.

### 13.2. Filtro Automático

O compilador injeta filtro de tenant em todas as queries e carregamentos. Acesso a aggregate de outro tenant → 404 (evita enumeração).

```ds
// Contexto disponível no domínio
tenant.id
tenant.tier      // para rate limit por plano
tenant.exists
```

### 13.3. Cross-Tenant (opt-in)

```ds
UseCase GenerateGlobalReport handles GlobalReportCmd {
    tenancy: cross_tenant
    access { requires caller.hasRole("super_admin") }
    execute {
        allWallets = list Wallet take 10000   // sem filtro de tenant
    }
}
```

Exige role privilegiada, gera auditoria automática, emite warning na compilação.

### 13.4. Aggregate Tenant e Provisionamento

`Aggregate Tenant` vive num módulo Platform separado (sem tenancy). Provisionamento via `provision tenant(id)` (built-in) numa Policy.

```ds
Policy ProvisionTenantInfrastructure on TenantCreated {
    delivery AtLeastOnce
    execute {
        provision tenant(event.id)
        // schema_per_tenant: CREATE SCHEMA
        // database_per_tenant: CREATE DATABASE + migrations
        // row_level: no-op
    }
}
```

Tenant context propagado automaticamente em channels. Workers: `scope: per_tenant` ou `global`. Tenant ausente → fail-closed (400).

---

## 14. Idempotência de Commands

Chave fornecida pelo cliente (sem fallback). Metadata implícito em todo Command que muta estado. Nunca declarado pelo dev.

| Protocolo | Como enviar |
|-----------|-------------|
| HTTP | Header `Idempotency-Key` |
| gRPC | Metadata `idempotency-key` |
| TCP/UDP | Campo no header da mensagem |

**Storage:** `same` (mesmo banco, atômico com a transação) ou `external` (Redis/Dynamo). Configurado no `mod.ds`.

**Cache de resultado:** sucesso ✅, erro de negócio ✅, erro de infraestrutura ❌ (permite retry).

**Race condition** (mesma chave, requests paralelos): `wait` ou `reject`.
**Conflito** (mesma chave, command diferente): erro 422 `IdempotencyKeyConflict`.

```ds
UseCase PerformDeposit handles DepositCmd {
    idempotency { required: true, window: 48h }
    execute { ... }
}

UseCase MarkAsRead handles MarkAsReadCmd {
    idempotency { required: false }   // operação naturalmente idempotente
    execute { ... }
}
```

Limpeza de chaves expiradas: Worker gerado automaticamente. Para Sagas: `Idempotency-Key` (entrada) mapeia para `sagaId` (saída) de forma estável. Retry idempotente não consome cota de rate limit.

---

## 15. Cache

Política declarada na Query. Backend no `mod.ds`.

```ds
Query GetWalletSummary(walletId WalletId) -> WalletSummaryVW {
    cache {
        ttl: 5min
        // invalidação inferida pelo compilador a partir dos aggregates tocados
        // override explícito:
        invalidateOn: [DepositPerformed, WithdrawalPerformed, TransferSent, TransferReceived]
        negativeCacheTtl: 10s
    }
    return load Wallet(walletId) as WalletSummaryVW
}
```

| Aspecto | Comportamento |
|---------|---------------|
| Backends | `memory`, `distributed`, `layered` |
| Invalidação | Por evento, inferida (override `invalidateOn`) |
| Granularidade | Cirúrgica (queries por ID), grossa (listagens — warning se alta cardinalidade) |
| Cache stampede | Request coalescing por construção |
| Adapters | Só `mode sync` + GET |
| Bypass | Header HTTP `Cache-Control: no-cache` |
| Multi-tenancy | Tenant na chave automaticamente |
| Falha do backend | Fail-open |
| EventBus assíncrono | Invalidação in-process imediata após `emit`, antes da fila externa |

---

## 16. Rate Limiting

Política no `interface.ds`, backend no `mod.ds`.

**Dimensões:** `perIp`, `perUser`, `perTenant`, `perApiKey`, `global`. Múltiplas dimensões → todas precisam passar.

**Algoritmos:** `token_bucket` (padrão, suporta `burst`), `sliding_window`, `fixed_window`.

**Por plano (tier-based):**

```ds
RateLimitTier Free {
    perUser: 100/min
    perTenant: 1000/min
}
RateLimitTier Pro {
    perUser: 1000/min
    perTenant: 20000/min
}

// No endpoint, resolve do tenant.tier
POST "/search" -> SearchEvents {
    rateLimit: byTier
}
```

Resposta automática: HTTP 429 + `Retry-After` + headers `X-RateLimit-*`. gRPC: `RESOURCE_EXHAUSTED`.

**Falha do backend:** `onBackendFailure: open` (padrão) ou `closed`, override por endpoint. Backend distribuído quase obrigatório com réplicas. Endpoints sem tenant usam só `perIp`. Retry idempotente não consome cota.

---

## 17. Versionamento de API

Domínio conhece só a versão atual. Tradução na borda (simétrico ao Upcast de eventos).

```ds
// interface.ds
versioning {
    strategy: path        // ou: header "Api-Version"
    current: v2
    supported: [v1, v2]
}
```

```ds
// versions/v1.ds
Version v1 {
    deprecated: "2026-01-01"
    sunset: "2026-06-01"

    // Request: shape antigo → Command atual
    upcast DepositCmd {
        from { value decimal, currency string, description string }
        to {
            amount = Money(amount: value, currency: currency)
            description = TransactionDescription(description)
            channel = Channel("legacy")
        }
    }

    // Response: View atual → shape antigo
    downcast WalletSummaryVW {
        to {
            balance = self.balance_amount
            currency = self.balance_currency
            owner = self.holder
        }
    }

    // Mudança semântica (não de shape) → UseCase diferente
    route "/wallets/{walletId}/transfer" -> PerformLegacyTransfer
}
```

| Fase | Comportamento (automático) |
|------|---------------------------|
| Antes de `deprecated` | Normal |
| Após `deprecated` | Headers `Deprecation` + `Sunset`, warning na compilação |
| Após `sunset` | `410 Gone` |

Endpoints inalterados passam direto (versionamento esparso). Campo obrigatório sem default no upcast → erro de compilação. Mudança de **comportamento** não é versionável por shape → UseCase separado via `route`.

---

## 18. Transações e Sagas

### 18.1. Inferência Transacional

| Cenário | Comportamento |
|---------|---------------|
| Mesmo `Database` | Commit local |
| Diferentes, ambos XA | 2PC automático |
| Diferentes, sem XA | ❌ Erro — exige Saga |
| Cross-service sem Saga | ❌ Erro |

### 18.2. Sagas

`async` (retorna `sagaId`, compilador gera `SagaStatus`) ou `await` (bloqueante, timeout). Steps com `up`/`down`/`onInfraError`. `down { unrecoverable }` para compensação impossível.

```ds
Saga PurchaseTickets handles PurchaseTicketsCmd {
    mode await timeout 60s
    state { orderId OrderId, ticketIds List<TicketId>, paymentId PaymentId }

    step ReserveTickets {
        up { ... }
        down { for ticketId in state.ticketIds { ... } }
        onInfraError { RetryWithBackoff(3) }
    }
    step ProcessPayment {
        up { result = call PaymentRequest(...) }
        down { call RefundRequest(...) }
        onInfraError { RetryWithBackoff(3) }
    }
    step ConfirmPurchase {
        up { ... }
        down { ... }
    }
}
```

Timeout: herança módulo → UseCase/Saga.

---

## 19. Erros: Negócio vs. Infraestrutura

| Tipo | Declaração | Tratamento |
|------|-----------|------------|
| Negócio | `Error` no domínio | HTTP 4xx |
| Infraestrutura | Nunca no domínio | `mod.ds` (retry, circuit breaker) + `onInfraError` |

---

## 20. Smart Partial Loading

```ds
item = state.items.focus(itemId)              // SELECT * WHERE parent_id=X AND id=Y
ensure state.items.sum(i => i.price) < 10000  // SELECT SUM(...) sem trazer entidades
```

`AppendList<T>` com `skip/take` → paginação nativa. Fallback: carrega aggregate todo.

---

## 21. Observabilidade (OpenTelemetry)

**Traces automáticos** para todo construto, propagação cross-service (headers W3C). **Métricas automáticas** (duration, counters, gauges) + `Metric` de negócio. **Logs automáticos** + `log` explícito.

```ds
Metric DepositVolume {
    type counter
    value event.amount.amount
    on DepositPerformed
    labels { currency = event.amount.currency }
}

Metric PurchaseLatency {
    type histogram
    buckets [100ms, 250ms, 500ms, 1s, 2s, 5s, 10s]
    on PurchaseTickets.completed
}
```

---

## 22. Testing Nativo (`*.test.ds`)

Teste declarativo Given-When-Then. O compilador conhece a estrutura semântica e executa em memória, sem infraestrutura real. Testes são validados contra o domínio em tempo de compilação.

### 22.1. Teste de Aggregate

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

Para `StateStored`, `given` é o estado direto; `then state { ... }`.

### 22.2. Teste de UseCase (asserção transacional)

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

### 22.3. Mock de Adapters e Teste de Saga

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
        given Event("E1") from [ /* ... */ ]
        when PurchaseTicketsCmd( /* ... */ )
        then {
            saga compensated
            compensated [ConfirmPurchase, ProcessPayment, ReserveTickets]
            called RefundRequest
        }
    }
}
```

### 22.4. Teste de Policy e Query

```ds
Test RefundAllOnEventCancelled {
    scenario "reembolso de todos os pedidos" {
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

### 22.5. Property-Based Testing

O compilador gera sequências de comandos válidos e verifica invariantes, reportando o contra-exemplo mínimo em falha.

```ds
Test Wallet {
    property "saldo nunca fica negativo" {
        forall sequence of [Deposit, Withdraw, Transfer]
        invariant state.balance >= Money(0, "BRL")
    }
}
```

### 22.6. Fixtures

```ds
Fixture activeWallet {
    Wallet("W1") from [
        WalletCreated(id: "W1", holder: "João", email: "joao@x.com"),
        DepositPerformed(id: "W1", amount: Money(100, "BRL"), description: "init")
    ]
}
```

### 22.7. Garantias do Compilador

| Situação | Resultado |
|----------|-----------|
| Evento/comando inexistente no teste | ❌ Erro |
| Shape de evento esperado errada | ❌ Erro |
| Mock com retorno de tipo errado | ❌ Erro |
| `fail step X` onde X não existe | ❌ Erro |
| Handle sem cenário de erro testado | ⚠️ Warning (cobertura) |

**Cobertura semântica** (por Handle e ramo, não por linha): o compilador reporta exatamente quais regras de negócio e caminhos de erro não têm teste.

---

## 23. Regras de Compilação (Resumo)

| Regra | Resultado |
|-------|-----------|
| Primitivo no Write Side | ❌ Erro |
| Handle sem entrada no `access` | ❌ Erro |
| `Notification` sem `Adapter` | ❌ Erro |
| `remove()`/`clear()` em `AppendList<T>` | ❌ Erro |
| UseCase cross-database sem XA | ❌ Erro |
| UseCase cross-service sem Saga | ❌ Erro |
| JOIN cross-database | ❌ Erro |
| `match` não-exaustivo / com guards sem `_` | ❌ Erro |
| `Nop` em Handle/UseCase | ❌ Erro |
| `break`/`continue` fora de `for` | ❌ Erro |
| Policy cross-module escutando `Event` não público | ❌ Erro |
| Módulos em services diferentes sem canal | ❌ Erro |
| Acesso cross-tenant sem opt-in | ❌ Erro |
| Versionamento: campo obrigatório sem default no upcast | ❌ Erro |
| Idempotency conflito (mesma chave, command diferente) | ❌ 422 |
| Teste referenciando evento/comando inexistente | ❌ Erro |
| FFI/Adapter com assinatura incompatível | ❌ Erro |
| Canal `queue`/`stream` sem `orderBy` | ⚠️ Warning |
| Saga `await` sobre canal `queue` | ⚠️ Warning |
| Upcast substituível por default | ⚠️ Warning |
| ValueObject que poderia ser Enum | ⚠️ Warning |
| Cache em listagem de alta cardinalidade | ⚠️ Warning |
| UseCase cross-tenant declarado | ⚠️ Warning (auditoria) |
| Handle sem cenário de erro testado | ⚠️ Warning |
| UseCase/Query não exposto em interface | ⚠️ Warning |

---

## 24. Glossário

| Conceito | Descrição |
|----------|-----------|
| **ValueObject** | Tipo imutável com validação e comportamento. |
| **Enum** | Conjunto fechado de valores nomeados. Coerção na borda. |
| **File / FileStream / FileRef** | Conteúdo em memória / streaming / referência leve no state. |
| **Aggregate** | Fronteira transacional. State, handles, access, storage. |
| **Command** | DTO de entrada. Idempotency key implícita. |
| **UseCase** | Unit of Work implícito. |
| **Event / PublicEvent** | Fato imutável interno / compartilhado. Campos `redactable` (GDPR). |
| **Policy** | Reação a eventos. `BestEffort` ou `AtLeastOnce`. |
| **Worker** | Background. `every`, `cron`, `continuous`. `scope` per_tenant/global. |
| **Notification / Adapter** | Contrato de saída / fronteira de infra (HTTP, FFI). |
| **Foreign** | FFI geral para bibliotecas da linguagem alvo. |
| **Saga** | State-machine distribuída. `async`/`await`. `unrecoverable`. |
| **View** | Read-only. Bloco `visibility` para field-level security. |
| **Projection** | View materializada cross-aggregate. |
| **Query** | Consulta declarativa. Bloco `cache`. |
| **Tenant** | Contexto ambiente. `row_level`/`schema`/`database` per tenant. |
| **Idempotency** | Chave do cliente. Storage `same`/`external`. |
| **Cache** | `memory`/`distributed`/`layered`. Invalidação por evento. |
| **RateLimit** | `perIp`/`perUser`/`perTenant`/`global`. Tier-based. |
| **Version** | `upcast`/`downcast` de API. `deprecated`/`sunset`. |
| **Test** | Given-When-Then. Property-based. Cobertura semântica. |
| **Telemetry** | OpenTelemetry nativo. |

---

## 25. Funcionalidades em Evolução

| Feature | Status |
|---------|--------|
| Redação de eventos (GDPR) — mecanismo de gatilho | Design inicial, em evolução |
| Field-level security — casos avançados | Design inicial, em evolução |
| FFI geral — detalhes de marshalling | Design inicial, em evolução |
| Operações SQL adicionais (avg, min, max, group by) | Planejado |
| Funções utilitárias adicionais | Planejado |
| Nível de suporte a operações aritméticas e booleanas | A definir |
