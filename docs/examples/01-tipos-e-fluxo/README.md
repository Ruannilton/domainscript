# 01 — Sistema de tipos e controle de fluxo

Cobre **§2 (Sistema de Tipos)** e **§3 (Controle de Fluxo)**. É a base que os
outros exemplos reusam.

| Arquivo | Mostra |
|---|---|
| `domain.ds` | ValueObject wrapper e composto, operadores, Enums, `coerce` |
| `colecoes.ds` | `List`/`AppendList`/`Set`/`Map`, `File`/`FileStream`/`FileRef` |
| `fluxo.ds` | `ensure`, `match` (statement e expressão, com e sem guards), `for`, `log` |

## As ideias que valem a leitura

**A Regra de Ouro (§2.1).** Primitivos são proibidos no Write Side. `string`
não é um nome de cliente; `HolderName` é. O tipo carrega a validação, então
ela acontece uma vez, na borda, e nunca mais precisa ser repetida.

**`self` é o valor sendo validado.** Num VO wrapper, `self` É o primitivo
embrulhado (`self.length`, `self.contains("@")`). Num VO composto, os campos
são acessíveis pelo nome nu dentro de `Valid`, e por `self.campo` dentro de um
`Operator` — onde há um `other` para distinguir.

**Operadores dão comportamento ao tipo.** `Money + Money` é uma operação do
domínio e pode falhar (moedas diferentes). O erro é de negócio, declarado, e
sobe como 4xx (§23) — não é uma exceção de infraestrutura.

**Exaustividade não é opcional.** `match` sobre enum proíbe o wildcard `_`
justamente para que adicionar um valor novo ao enum quebre a compilação em
todos os pontos que precisam decidir sobre ele. Com guards (`when`), o
wildcard passa a ser obrigatório, porque exaustividade sobre condição
arbitrária não é decidível.

**`AppendList<T>` é uma decisão de storage, não de estilo.** Declarar que uma
coleção só cresce deixa o compilador otimizar persistência e paginação —
`remove()`/`clear()` nela são erro de compilação (§25).

**Arquivo nunca some sozinho.** `store` devolve uma `FileRef`; os bytes só
entram na memória com um `load File(ref)` explícito. Em domínio auditado o
arquivo costuma viver além do Aggregate, então `delete file(ref)` é sempre
uma decisão do desenvolvedor.

## Regras da §25 exercitadas

- Primitivo no Write Side → ❌ (evitado por construção em todo o exemplo)
- `remove()`/`clear()` em `AppendList<T>` → ❌
- `match` não-exaustivo / guards sem `_` → ❌
- `Nop` em Handle/UseCase → ❌
- `break`/`continue` fora de `for` → ❌
- ValueObject que poderia ser Enum → ⚠️
