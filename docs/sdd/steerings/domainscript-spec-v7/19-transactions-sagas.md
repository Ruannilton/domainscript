# 19. Transações e Sagas

## 19.1. Inferência Transacional

| Cenário | Comportamento |
|---------|---------------|
| Mesmo `Database` | Commit local |
| Diferentes, ambos XA | 2PC automático |
| Diferentes, sem XA | ❌ Erro — exige Saga |
| Cross-service sem Saga | ❌ Erro |

## 19.2. Sagas

`async` (retorna `sagaId`, compilador gera `SagaStatus`) ou `await timeout Ns`. Steps com `up`/`down` — exatamente esses dois, mais o `retry:` declarativo da §19.3. `down { unrecoverable }` para compensação impossível (gera alerta em runtime). Falha, tentativas e compensação: §19.3.

```ds
Saga PurchaseTickets handles PurchaseTicketsCmd {
    mode await timeout 60s
    state { orderId ref Order, ticketIds List<ref Ticket>, paymentId PaymentId }

    step ReserveTickets {
        retry: { attempts: 3, backoff: "exponential" }
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
    }

    step ProcessPayment {
        retry: { attempts: 3, backoff: "exponential" }
        up {
            result = call PaymentRequest(paymentId: state.paymentId, amount: total, method: cmd.paymentMethod)
            ensure result.status == PaymentStatus.Approved else PaymentDeclined
        }
        down { call RefundRequest(paymentId: state.paymentId, amount: total) }
    }

    step ConfirmPurchase {
        up { ... }
        down { ... }
    }
}
```

## 19.3. Falhas, Tentativas e Compensação

Um `up` falha por **erro de negócio** ou por **erro de infraestrutura** — a distinção da [§23](23-error-classification.md), e ela governa todo o resto:

| Falha no `up` | Comportamento |
|---------------|---------------|
| **Negócio** — `Error` do domínio, `ensure ... else Error`, operador de ValueObject, `throws` de Foreign/Adapter, `compensate` | Compensa **imediatamente**. Nunca retenta: a regra de negócio não muda por tentar de novo. |
| **Infraestrutura** — timeout, indisponibilidade, panic de FFI, qualquer falha não mapeada como `Error` | Retenta até o limite do `retry` efetivo do step. Só depois de esgotado, compensa. |

Não existe bloco `onInfraError`. Um `step` tem exatamente `up` e `down`, mais o `retry:` declarativo abaixo. `onInfraError` e `RetryWithBackoff(...)` → ❌ **erro de compilação** (construtos inexistentes).

### 19.3.1. `retry:` — política de tentativas

Forma canônica única, a mesma de [§13](13-module-infra.md) (`Database`) e [§8](08-workers.md) (`Worker.onError`):

```ds
retry: { attempts: 3, backoff: "exponential", delay: 1s }
```

| Chave | Tipo | Default | Regra |
|-------|------|---------|-------|
| `attempts` | literal inteiro | `1` | Obrigatória quando `retry:` é declarado. `attempts >= 1`; `< 1` ou não-literal → ❌ erro. Conta a execução inicial: `attempts: 3` = 1 execução + 2 retentativas. |
| `backoff` | `"none"` \| `"fixed"` \| `"exponential"` | `"exponential"` | Fora do conjunto fechado → ❌ erro. |
| `delay` | literal de duração | `1s` | Espera-base entre tentativas. |

| `backoff` | Espera antes da k-ésima retentativa (k ≥ 1) |
|-----------|---------------------------------------------|
| `"none"` | nenhuma |
| `"fixed"` | `delay` |
| `"exponential"` | `delay * 2^(k-1)` |

As esperas são **determinísticas** — sem jitter — para que o compilador consiga computar o pior caso do orçamento (§19.3.4).

**Onde se declara e como herda:**

| Local | Efeito |
|-------|--------|
| Corpo da `Saga` | Política padrão de **todos** os steps. |
| Corpo do `step` | Sobrepõe a da Saga **chave a chave**: chave declarada no step vence; chave omitida herda a da Saga; chave omitida em ambos usa o default da tabela acima. |
| Nenhum dos dois | `attempts: 1` — sem retentativa. Retry é opt-in: retentar muda a semântica dos efeitos (§19.3.5). |

Declarado duas vezes no mesmo bloco → ❌ erro. Ordem dentro do bloco é livre.

**O que conta como uma tentativa:** uma execução completa do `up` **daquele step**. A retentativa nunca re-executa steps anteriores nem reabre a Saga — o avanço continua exatamente do step corrente. O mesmo `retry:` efetivo vale para o `down` do step (§19.3.3).

**Composição com o `retry:` de infraestrutura ([§13](13-module-infra.md)):** são escopos diferentes da mesma grafia. O do `mod.ds` cobre a chamada individual ao cliente de infra (conexão, driver, circuit breaker); o do step cobre a re-execução do `up` inteiro. O step só enxerga a falha depois de esgotado o retry do driver.

⚠️ **Warning:** `retry:` inerte — bloco que não altera nada em relação ao que já valeria (`attempts: 1` sem política na Saga, ou repetição exata do bloco da Saga).

### 19.3.2. `compensate` — disparar a compensação

```ds
compensate <Error>
```

Encerra o `up` no ponto em que aparece e inicia a cadeia de compensação (§19.3.3). O `Error` é o desfecho da Saga: em `mode await` é o que o chamador recebe (4xx, [§23](23-error-classification.md)); em `mode async` fica registrado no `SagaStatus`.

| Aspecto | Regra |
|---------|-------|
| Onde é válido | Dentro de `up`, inclusive aninhado em `for` e em braço de `match`. |
| Onde é ❌ erro de compilação | Em `down`; no corpo da Saga fora de um `step`; em Handle, UseCase, Policy, Worker, Query, Apply ou ValueObject. |
| `Error` obrigatório | `compensate` sem `Error` → ❌ erro. Forma única, sem variante nua. |
| Classificação | É **erro de negócio**. Nunca retenta, mesmo com `retry:` declarado. |

Quando existe uma condição a guardar, a forma é `ensure cond else Error` — que dispara a mesma compensação. `compensate` é a forma para os pontos onde não há guard: braço de `match`, decisão dentro de `for`, saída incondicional.

### 19.3.3. Cadeia de compensação

Dispara quando um `up` falha com erro de negócio, quando executa `compensate`, ou quando as tentativas de infra se esgotam (inclusive por prazo, §19.3.4).

A cadeia começa **no step que falhou** e caminha em **ordem reversa** até o primeiro. O `down` do step que falhou roda também — o `up` pode ter deixado efeito parcial. Steps ainda não executados não compensam. É o que [§24.3](24-testing.md) já assere: falha em `ConfirmPurchase` → `compensated [ConfirmPurchase, ProcessPayment, ReserveTickets]`.

| Situação no `down` | Comportamento em runtime |
|--------------------|--------------------------|
| Sucesso | Step entra em `compensated`; a cadeia segue. |
| Erro de infra dentro do limite | Retenta com o `retry:` efetivo do step. |
| Erro de infra com limite esgotado | Step fica **sem compensação**: alerta em runtime para intervenção humana; a cadeia **segue** nos steps anteriores; Saga termina `Failed`. |
| Erro de negócio | Não retenta. Idem à linha acima. |
| `down { unrecoverable }` | Nada executa, nada retenta: alerta em runtime; a cadeia segue; Saga termina `Failed`. |

Um step sem compensação nunca interrompe a cadeia — os demais compensam assim mesmo. `unrecoverable` deve ser o único statement do bloco `down`; acompanhado de qualquer outro → ❌ erro de compilação. Um `retry:` declarado num step com `down { unrecoverable }` continua valendo para o `up`.

**Estados terminais da Saga:**

| Estado | Significado |
|--------|-------------|
| `Completed` | Todos os `up` concluíram. |
| `Compensated` | Falhou e **todo** step da cadeia compensou. |
| `Failed` | Falhou e ao menos um step ficou sem compensação (`unrecoverable` ou `down` esgotado). Exige intervenção humana. |

Em `mode await` o chamador recebe o erro que parou a Saga — de negócio → 4xx, de infra → 5xx ([§23](23-error-classification.md)). Em `mode async` recebe o `sagaId` e consulta o `SagaStatus`.

### 19.3.4. Prazo e `mode`

| Modo | Prazo |
|------|-------|
| `mode await timeout Ns` | `Ns` é o orçamento **total da fase de avanço**: soma de todos os `up`, de suas retentativas e das esperas de backoff. Não é por step. |
| `mode async` | Sem prazo global. O único limite é o `retry:` de cada step. |

Estourar o prazo **equivale a esgotar as tentativas**: a tentativa em voo é abandonada e a cadeia de compensação começa no step corrente.

A fase de compensação **não** está sujeita ao prazo — roda até concluir. Em `mode await`, o chamador é liberado no instante do estouro com erro de infraestrutura (5xx) e o desfecho da compensação fica no `SagaStatus` da instância; por isso o compilador gera `SagaStatus` para **toda** Saga, não só as `async`.

⚠️ **Warning:** pior caso do orçamento de retry (computável — todos os valores são literais) maior que o `timeout` declarado. O prazo vence antes, e o `retry:` declarado nunca se esgota.

### 19.3.5. Idempotência

**A linguagem garante:**

1. O `state` da Saga é persistido na fronteira de step. Uma retentativa do `up` recomeça com o `state` como estava no **início** daquele step; mutações de `state` feitas pela tentativa que falhou são descartadas.
2. Retentativa só acontece para erro de infraestrutura ([§23](23-error-classification.md)). Erro de negócio nunca é retentado.
3. Retentativa é sempre local ao step: steps já concluídos não re-executam.

**É responsabilidade do autor:**

1. Efeitos fora do `state` — `call`, `notify`, `emit`, escrita em Aggregates — **não** são revertidos entre tentativas. O `up` precisa ser idempotente para tolerar `attempts > 1`.
2. Identificadores de efeito externo vêm do `state` da Saga (ex. `state.paymentId`), fixados antes do step, nunca gerados dentro de um `up` retentável.
3. O `down` precisa tolerar um `up` parcial — ele roda também para o step que falhou.

⚠️ **Warning:** `up` de step com `attempts > 1` que chama função não determinística (`uuid()`, `now()`, `random()`, `random_str()` — [§2.6](02-type-system.md)): cada tentativa produz um valor diferente e o efeito deixa de ser idempotente.

### 19.3.6. Exemplo

```ds
Saga PurchaseTickets handles PurchaseTicketsCmd {
    mode await timeout 60s
    retry: { attempts: 3, backoff: "exponential", delay: 1s }   // padrão de todos os steps

    state { orderId ref Order, ticketIds List<ref Ticket>, paymentId PaymentId }

    step ReserveTickets {
        retry: { attempts: 1 }        // reserva não é idempotente: uma execução só
        up { ... }
        down { ... }
    }

    step ProcessPayment {             // herda attempts: 3, exponential, 1s
        up { result = call PaymentRequest(paymentId: state.paymentId, amount: total, method: cmd.paymentMethod) }
        down { call RefundRequest(paymentId: state.paymentId, amount: total) }
    }

    step ConfirmPurchase {
        retry: { backoff: "fixed" }   // herda attempts: 3 e delay: 1s da Saga
        up {
            for ticketId in state.ticketIds {
                ticket = load Ticket(ticketId)
                match ticket.state.status {
                    TicketStatus.Reserved  => ticket.ConfirmSale(state.orderId, cmd.userId)
                    TicketStatus.Sold      => compensate TicketAlreadySold
                    TicketStatus.Available => compensate TicketNotReserved
                    TicketStatus.Cancelled => compensate TicketNotReserved
                    TicketStatus.Used      => compensate TicketNotReserved
                }
            }
        }
        down { unrecoverable }
    }
}
```

`compensate TicketAlreadySold` é erro de negócio: não retenta, e a cadeia roda `ConfirmPurchase` (sem compensação, `unrecoverable` → alerta) → `ProcessPayment` (`RefundRequest`) → `ReserveTickets` (libera os ingressos). Saga termina `Failed`, e o chamador recebe `TicketAlreadySold` em 4xx. Um timeout do `PaymentRequest` é erro de infra: 3 execuções do `up` de `ProcessPayment` espaçadas 1s e 2s; se todas falharem, a cadeia roda `ProcessPayment` → `ReserveTickets` e a Saga termina `Compensated` com erro de infra em 5xx.

