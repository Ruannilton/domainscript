# 05 — FFI geral (`Foreign`)

Cobre **§10 (FFI Geral)**. É o único buraco controlado na pureza do domínio —
e a seção que mais depende de ser lida inteira antes de usada.

| Arquivo | Mostra |
|---|---|
| `foreign/crypto.ds` | Declarações `pure`/`impure`, `throws` |
| `domain.ds` | Onde cada natureza pode ser chamada, captura em evento, marshalling |

## As ideias que valem a leitura

**`pure` vs `impure` é declaração do desenvolvedor, e o compilador confia.**
`pure` significa determinística e sem efeito colateral. Declarar `pure` uma
função com estado interno é bug do dev que o compilador **não detecta** — a
spec diz isso explicitamente. É o preço de ter um escape hatch.

**`Apply` é hermético — nem FFI pura entra.** Esta é a regra mais forte da
seção, e a que mais parece exagero até você pensar em replay: um `Apply`
reproduzido daqui a cinco anos precisa produzir exatamente o mesmo estado,
mesmo que a biblioteca externa tenha mudado de algoritmo ou deixado de
existir. Depender só do evento e de built-ins é o que torna isso verdade.

**Impure no Handle exige captura em evento.** Mesmo princípio de
`now()`/`uuid()`: o Handle roda uma vez, o Apply roda toda vez. O que for
não-determinístico tem de ser congelado no evento na primeira passagem. Usar o
resultado no controle de fluxo do Handle sem capturar é erro de compilação.

**Query aceita `pure` e recusa `impure`.** Leitura precisa ser repetível e
cacheável; efeito colateral quebra as duas coisas.

**Aggregate nunca atravessa a fronteira.** Ele tem identidade, ciclo de vida e
fronteira transacional — deixá-lo sair entregaria essas garantias a código que
a linguagem não controla. Passe ValueObjects ou campos específicos.

**`throws` separa negócio de infraestrutura.** O que está declarado vira 4xx.
O que não está (panic, timeout) é `InfraError` e cai no retry/circuit breaker
do `mod.ds` (§23).

**Quando NÃO usar FFI.** A §10.7 é direta: função que precisa de config ou
credenciais deveria ser um **Adapter**, não FFI. Volume grande pede
`FileStream`, não passagem em memória. E FFI impura dentro de transação gera
warning porque o efeito não é revertido no rollback — se isso importa, o
desenho certo é Saga.

## Regras da §25 exercitadas

- Aggregate cruzando fronteira FFI → ❌
- FFI em `Apply` (pure ou impure) → ❌
- FFI impura em Handle sem captura em evento → ❌
- FFI impura em Query/ValueObject → ❌
- FFI/Adapter com assinatura incompatível → ❌
- FFI impura dentro de transação → ⚠️
