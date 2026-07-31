# 9. Notifications & Adapters

## 9.1. Notifications

Contrato de saída. Sem `Adapter` correspondente → erro de compilação.

```ds
Notification DepositNotification { to Email, amount Money }
Notification PaymentRequest { paymentId PaymentId, amount Money, method PaymentMethod } -> PaymentResult
```

`-> Tipo` declara o contrato de resposta (seção 9.4). Sem `->`, a Notification não tem resposta.

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
    function "ProcessPayment" -> PaymentResult
    map {
        paymentId = notification.paymentId
        amount    = notification.amount
    }
}
```

Assinatura incompatível → erro de compilação. Nível 1 migra automaticamente ao trocar linguagem alvo; Nível 2 exige reimplementação.

## 9.4. Contrato de Resposta

A `Notification` declara o que **sai**; o contrato de resposta declara o que **volta**. A resposta pertence à assinatura da Notification, nunca ao Adapter: o domínio depende do contrato, e trocar o transporte que o cumpre não toca uma linha de domínio.

### 9.4.1. Declaração (`-> Tipo`)

```ds
Notification DepositNotification { to Email, amount Money }                             // sem resposta
Notification PaymentRequest { paymentId PaymentId, amount Money, method PaymentMethod } -> PaymentResult
```

`-> T` é opcional. Sem `->`, a Notification **não tem resposta**: nenhum valor atravessa a fronteira de volta, e não há como capturá-lo.

| Regra | Resultado |
|-------|-----------|
| `T` não resolvido no escopo do módulo | ❌ Erro |
| `T` não é ValueObject nem Enum (Aggregate, Event, Command, primitivo, coleção) | ❌ Erro |
| `-> T` em Notification cujo Adapter é `mode async` | ❌ Erro |

Resposta composta (lista, paginação, metadados do provedor) → envolva num ValueObject. `-> List<T>` é erro: o contrato é sempre **um** tipo nomeado, e é ele o ponto de extensão.

### 9.4.2. `notify`, `call` e o `mode` do Adapter

O verbo não é escolha de estilo do call site: é determinado pelo `mode` do Adapter da Notification.

| Adapter | Verbo | Semântica | `-> T` |
|---------|-------|-----------|--------|
| `mode async` | `notify` | dispara e segue; entrega pelo Outbox ([§13](13-module-infra.md)) | ❌ proibido |
| `mode sync` | `call` | bloqueia até resposta ou timeout | opcional |

`mode sync` **não** exige `-> T`. Um `call` sem resposta é confirmação síncrona de entrega — exatamente o que `down { call RefundRequest(...) }` ([§19.2](19-transactions-sagas.md)) faz.

| Forma | Resultado |
|-------|-----------|
| `notify X(...)` com Adapter `mode sync` | ❌ Erro |
| `call X(...)` com Adapter `mode async` | ❌ Erro |
| `r = notify X(...)` | ❌ Erro — `notify` não produz valor |
| `r = call X(...)` onde `X` não declara `-> T` | ❌ Erro |
| `r = call X(...)` onde `X` declara `-> T` | `r` tem tipo `T` |
| `call X(...)` como statement, `X` declara `-> T` | ✅ resposta descartada, sem warning |
| `call X(...)` dentro de UseCase transacional | ⚠️ Warning — efeito não é revertido em rollback; considere Saga |

Onde cada verbo pode aparecer:

| Contexto | `notify` | `call` |
|----------|----------|--------|
| UseCase / Saga / Policy / Worker | ✅ | ✅ |
| Query | ❌ | ✅ só Nível 1 `mode sync` + GET — resposta cacheável ([§16](16-cache.md)) |
| Handle | ❌ | ❌ |
| **Apply** | ❌ | ❌ |
| ValueObject (Valid/Operator) | ❌ | ❌ |

O Aggregate emite Evento; quem notifica é Policy, UseCase, Saga ou Worker. **Apply é hermético** ([§10.4](10-ffi.md)): replay não repete efeito externo.

### 9.4.3. Adapter Nível 1 — bloco `response { }`

`body { }` mapeia domínio → requisição. `response { }` mapeia resposta → domínio. São simétricos e pareados com o contrato.

```ds
ValueObject DeliveryReceipt {
    providerId string
    acceptedAt datetime
}

Notification TicketDelivery { orderId ref Order, to Email } -> DeliveryReceipt

Adapter TicketDelivery {
    mode sync
    http POST "https://api.delivery.example/v1/send"
    headers { "Authorization" = "Bearer {env('DELIVERY_KEY')}" }
    body {
        order = notification.orderId
        email = notification.to
    }
    expect status [200, 201]
    response {
        providerId = response.body.delivery.id
        acceptedAt = response.body.delivery.accepted_at
    }
}
```

Cada linha é `<campo de T> = <origem>`. `response` é a raiz reservada da resposta recebida, como `notification` é a raiz da Notification em `body { }`:

| Origem | Tipo | Descrição |
|--------|------|-----------|
| `response.body.<caminho>` | conforme o JSON | caminho pontuado no corpo; `[n]` indexa arrays |
| `response.status` | `integer` | status HTTP |
| `response.headers["Nome"]` | `string` | header, nome case-insensitive |

`expect status [ ... ]` é opcional e lista os status tratados como sucesso. Default: `200..299`.

| Regra | Resultado |
|-------|-----------|
| Notification declara `-> T` e o Adapter Nível 1 não tem `response { }` | ❌ Erro |
| `response { }` em Adapter de Notification sem `-> T` | ❌ Erro |
| Campo de `T` sem linha em `response { }` | ❌ Erro |
| Linha citando campo inexistente em `T` | ❌ Erro |
| Campo de `T` mapeado mais de uma vez | ❌ Erro |
| `response.*` fora de `response { }`, ou `notification.*` dentro dele | ❌ Erro |

Em runtime, tudo abaixo é **InfraError** ([§23](23-error-classification.md)) — sujeito a retry/circuit breaker do `mod.ds` e, em passo de Saga, ao bloco `retry:` ([§19.3](19-transactions-sagas.md)):

| Situação | Resultado |
|----------|-----------|
| Status fora de `expect status` (default: não-2xx) | InfraError |
| Timeout ou falha de transporte | InfraError |
| Corpo não parseável, caminho ausente, tipo incompatível | InfraError |
| Valor viola o `Valid { }` do ValueObject ou o conjunto do Enum | InfraError |

**Resultado de negócio nunca chega como status HTTP.** Pagamento recusado é `PaymentResult(status: PaymentStatus.Declined)` dentro de um 200 — não um 402 traduzido em `Error`. Se o provedor sinaliza o desfecho pelo status, inclua-o em `expect status` e mapeie `response.status` para um campo de `T`.

### 9.4.4. Adapter Nível 2 — assinatura da `function`

A linha `function` carrega a assinatura completa, na mesma forma do `Foreign` genérico ([§10.2](10-ffi.md)). O nome continua string (é o símbolo na linguagem alvo); `-> T` e `throws` são DomainScript:

```ds
function "ProcessPayment" -> PaymentResult throws PaymentGatewayRejected
```

A assinatura esperada é **derivada**, nunca escrita duas vezes:

| Parte | Origem |
|-------|--------|
| Parâmetros, em ordem | as linhas de `map { }`, com o tipo do lado direito |
| Retorno | o `-> T` da linha `function` |
| Erros de negócio | `throws E1, E2` — cada `E` deve ser um `Error` declarado |
| Marshalling | [§10.3](10-ffi.md) — Aggregate atravessando a fronteira → ❌ Erro |

O compilador emite essa assinatura derivada como o contrato do símbolo importado e confronta a implementação na linguagem alvo com ela. É contra esse contrato que "assinatura incompatível → erro de compilação" (§9.3, [§25](25-compilation-rules.md)) se resolve:

| Regra | Resultado |
|-------|-----------|
| `-> T` na `function` e a Notification não declara `->` | ❌ Erro |
| Notification declara `-> T` e a `function` não declara `->` | ❌ Erro |
| `-> U` na `function` com `U` ≠ `T` da Notification | ❌ Erro |
| Símbolo ausente, ou aridade/tipos/retorno divergentes na linguagem alvo | ❌ Erro |
| `throws` citando erro não declarado como `Error` | ❌ Erro |

Falha fora do `throws` (panic, timeout, transporte) é **InfraError** ([§10.5](10-ffi.md), [§23](23-error-classification.md)).

### 9.4.5. Tipos de resposta canônicos

Os exemplos de Saga ([§19.2](19-transactions-sagas.md)) e de teste ([§24.3](24-testing.md)) usam `PaymentResult`. Declaração normativa:

```ds
Enum PaymentStatus : string {
    Approved = "APPROVED"
    Declined = "DECLINED"
}

ValueObject PaymentResult {
    status PaymentStatus
    authCode string
}
```

### 9.4.6. Mocks em teste

`mock X returns V` ([§24.3](24-testing.md)) liga-se ao `-> T` da Notification. `V` é uma **shape** de `T`, com a mesma semântica parcial de `then` ([§24.2](24-testing.md)): campos citados devem existir em `T` e casar em tipo; campos omitidos assumem o valor vazio do seu tipo. Por isso `mock PaymentRequest returns PaymentResult(status: PaymentStatus.Declined)` é completo mesmo sem `authCode`.

| Regra | Resultado |
|-------|-----------|
| `X` não é Notification nem função `Foreign` | ❌ Erro |
| `mock X returns V` onde `X` não declara `-> T` | ❌ Erro |
| `V` de tipo diferente de `T` | ❌ Erro — é esta a regra "Mock com retorno de tipo errado" ([§24.7](24-testing.md)) |
| `V` cita campo inexistente em `T`, ou com tipo incompatível | ❌ Erro |
| `mock X` sem `returns` onde `X` declara `-> T` | ❌ Erro |

Cenário que atinge `call X(...)` sem `mock X returns ...` correspondente → falha do teste em execução, apontando a Notification. Notification sem `-> T` não precisa de `mock`: `called X` continua sendo a asserção de invocação.

### 9.4.7. Exemplo

```ds
// Wallet — sem resposta: dispara e segue.
Notification DepositNotification { to Email, amount Money }

Policy NotifyOnDeposit on DepositPerformed {
    delivery AtLeastOnce
    execute {
        wallet = load Wallet(event.id)
        notify DepositNotification(to: wallet.state.email, amount: event.amount)
    }
}

// Ticketing — com resposta: o valor decide o passo seguinte.
Notification PaymentRequest { paymentId PaymentId, amount Money, method PaymentMethod } -> PaymentResult
Notification RefundRequest { paymentId PaymentId, amount Money }

Adapter PaymentRequest {
    mode sync
    foreign "go" from "adapters/payment_gateway"
    function "ProcessPayment" -> PaymentResult
    map {
        paymentId = notification.paymentId
        amount    = notification.amount
        method    = notification.method
    }
}

Adapter RefundRequest {
    mode sync
    foreign "go" from "adapters/payment_gateway"
    function "RefundPayment"
    map {
        paymentId = notification.paymentId
        amount    = notification.amount
    }
}

step ProcessPayment {
    retry: { attempts: 3, backoff: "exponential" }   // §19.3.1
    up {
        result = call PaymentRequest(paymentId: state.paymentId, amount: total, method: cmd.paymentMethod)
        ensure result.status == PaymentStatus.Approved else PaymentDeclined
    }
    down { call RefundRequest(paymentId: state.paymentId, amount: total) }   // sem resposta por contrato
}
```

