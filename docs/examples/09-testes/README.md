# 09 — Testing nativo (`*.test.ds`)

Cobre **§24 (Testing Nativo)**.

| Arquivo | Mostra |
|---|---|
| `wallet.test.ds` | Aggregate EventSourced, asserção transacional de UseCase, property-based, Fixture |
| `ticketing.test.ds` | Mocks de Adapter/FFI, `fail step`, compensação de Saga, Policy, StateStored |

## As ideias que valem a leitura

**O cenário é o histórico de eventos.** Não há mock de repositório para
configurar nem container para subir: `given [eventos]` reconstrói o estado
pelo mesmo caminho que a produção usa. O teste exercita o replay de verdade.

**O teste é validado em tempo de compilação.** Referenciar evento ou comando
inexistente é **erro**, não um teste que passa em silêncio. Shape de evento
errada, mock com tipo de retorno errado, `fail step` apontando para um passo
que não existe: tudo isso quebra a build. É a diferença entre um teste que
protege e um teste que só parece existir.

**Asserção transacional é sobre a unidade de trabalho, não sobre o
Aggregate.** `then { Wallet("W1") emitted ..., committed }` verifica quem
emitiu o quê **e** se o conjunto commitou. `rolledback` é o outro lado — e é
onde a maioria dos bugs de transação mora.

**`fail step X with InfraError` testa a compensação sem esperar o mundo
falhar.** Provar que a Saga compensa na ordem certa exige poder injetar a
falha exatamente onde se quer — e a asserção `compensated [C, B, A]` fixa a
ordem reversa.

**FFI `pure` roda de verdade no teste; `impure` é mockada.** Mockar uma função
determinística seria testar o mock (§10.6).

**Property-based reporta o contra-exemplo MÍNIMO.** Ao falhar, o valor está na
menor sequência que ainda quebra a invariante — não na sequência de 200 passos
que por acaso a produziu.

**A cobertura é por Handle e por ramo, não por linha.** O compilador reporta
quais regras de negócio e caminhos de erro não têm cenário — e emite warning
para Handle sem cenário de erro testado. Cobertura de linha diria que 100% do
`Withdraw` está coberto sem nunca ter testado saldo insuficiente.

## Regras da §25 exercitadas

- Teste referenciando evento/comando inexistente → ❌
- Shape de evento esperada errada → ❌
- Mock com retorno de tipo errado → ❌
- `fail step X` inexistente → ❌
- Handle sem cenário de erro testado → ⚠️
