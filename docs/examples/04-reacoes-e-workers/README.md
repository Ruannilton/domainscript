# 04 — Reações, workers e saída

Cobre **§7 (Policies)**, **§8 (Workers)** e **§9 (Notifications & Adapters)**.

| Arquivo | Mostra |
|---|---|
| `policies.ds` | Policies com `delivery`, `ensure ... else Nop`, `distinct` |
| `workers.ds` | Os três modos: `every`, `cron`, `continuous` com `source` |
| `adapters.ds` | Notifications, Adapter HTTP declarativo e Adapter FFI, `notify` vs `call` |

## As ideias que valem a leitura

**`delivery` é uma declaração honesta.** `BestEffort` ou `AtLeastOnce` — não
existe exactly-once, e a linguagem não finge que existe. Declarar
`AtLeastOnce` é assumir que a reação vai repetir, e escrevê-la de modo a
tolerar isso.

**`Nop` é permitido em Policy e proibido em Handle.** Uma reação assíncrona
que decide não agir (o pedido já saiu de `Pending`) é normal. Um comando que
não faz nada e não diz por quê é bug — por isso `Nop` em Handle/UseCase é erro
de compilação.

**Worker é um conceito, não três.** `every`, `cron` e `continuous` são modos
do mesmo construto. `concurrency`, `batchSize` e `maxRate` controlam vazão sem
ninguém escrever pool de workers à mão; `scope` decide entre uma execução por
tenant e uma global.

**Notification é contrato; Adapter é transporte.** O domínio conhece só o
contrato — trocar SendGrid por SES não toca uma linha de domínio. E
`Notification` sem `Adapter` é erro de compilação: contrato que ninguém
entrega é código morto que parece vivo.

**Os dois níveis de Adapter têm custos diferentes, e a spec diz isso.** O HTTP
declarativo migra sozinho ao trocar a linguagem alvo. O FFI vinculado exige
reimplementação. Saber disso na hora de escolher é o ponto.

**`notify` vs `call` é decisão de fluxo.** `notify` dispara e segue. `call`
bloqueia e devolve resultado, então o fluxo pode ramificar sobre ele — é o que
o `ProcessPayment` faz para recusar um pagamento negado.

## Regras da §25 exercitadas

- `Notification` sem `Adapter` → ❌
- `Nop` em Handle/UseCase → ❌ (e permitido em Policy/Worker)
- FFI/Adapter com assinatura incompatível → ❌
- Policy cross-module escutando `Event` não público → ❌
