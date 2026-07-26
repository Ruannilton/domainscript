# 03 — Aplicação e leitura

Cobre **§5 (Camada de Aplicação)**, **§6 (Read Side)**, **§16 (Cache)** e
**§22 (Smart Partial Loading)**.

| Arquivo | Mostra |
|---|---|
| `application.ds` | Commands com `ref`, UseCases com Unit of Work implícito |
| `read.ds` | Views, `visibility`, Queries com `join`/`in`/`orderBy`, cache, Projection, `focus`/`sum` |

## As ideias que valem a leitura

**Unit of Work implícito.** Um `execute` commita tudo ou nada. Não há
`begin`/`commit`/`rollback` para escrever — nem para esquecer. O
`PerformTransfer` move dinheiro entre duas carteiras e a atomicidade é
inferida do fato de estarem no mesmo `Database` (§19.1); em bancos diferentes
sem XA, o mesmo código seria erro de compilação e exigiria Saga.

**`ref` é type-safety, não documentação.** `walletId ref Wallet` significa que
o campo carrega o id *de uma Wallet* — passar o id de outro Aggregate ali não
compila.

**O Read Side pode usar primitivos, e deve.** O dado já foi validado por VO na
entrada; reembrulhar na saída seria cerimônia. Repare no `StatementEntryVW`: o
`Money` do domínio chega achatado como `amount_value` + `amount_currency`.

**`access` e `visibility` são eixos diferentes.** Um controla quem *invoca* o
comando; o outro, quem *vê* cada campo do resultado. Campo não autorizado é
omitido da serialização, não devolvido como `null` — porque `null` já vaza que
o campo existe.

**Cache: política na Query, backend no módulo.** A invalidação é inferida dos
Aggregates que a Query toca — declarar `invalidateOn` é override, não
obrigação. Cache stampede sai por construção (request coalescing) e a chave já
inclui o tenant.

**`join` é só dentro do mesmo banco.** Cross-database é erro de compilação, e
a saída é a `Projection`: uma view materializada alimentada por eventos. A
linguagem prefere te barrar a deixar você escrever um join distribuído que
funciona em dev e derrete em produção.

**Smart Partial Loading é sobre não carregar o que não precisa.** `focus(id)`
vira `SELECT ... WHERE id = ?`; `sum(lambda)` vira `SELECT SUM(...)`. Sem
isso, olhar um item de um carrinho de mil linhas carregaria os mil.

## Regras da §25 exercitadas

- JOIN cross-database → ❌
- UseCase cross-database sem XA / cross-service sem Saga → ❌
- Cache em listagem de alta cardinalidade → ⚠️
- UseCase/Query não exposto em interface → ⚠️
