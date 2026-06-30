# Exemplo: módulo `Wallet`

Um bounded context mínimo, porém completo, de uma carteira digital. Mostra como as
peças de DomainScript se encaixam num módulo único e válido.

## Estrutura

| Arquivo | Camada | Conteúdo |
|---------|--------|----------|
| `mod.ds` | Infraestrutura (§12) | `Module Wallet` e o `Database` que persiste o Aggregate. |
| `domain.ds` | Write Side (§2, §4) | ValueObjects, `Enum TransactionType`, `Error`s, `Event`s e o `Aggregate Wallet` (EventSourced, com `access`, `Handle` e `Apply`). |
| `application.ds` | Aplicação (§5) | `Command`s e `UseCase`s que carregam o Aggregate e despacham os Handles. |
| `read.ds` | Read Side (§6) | `View WalletView` e as `Query`s de leitura. |
| `interface.ds` | Borda (§10) | `Interface HTTP` mapeando rotas para UseCases e Queries. |
| `wallet.test.ds` | Testes (§22) | Cenários `given/when/then` para os Handles, incl. os caminhos de erro. |

## O que o exemplo respeita

Por ser um projeto válido, ele honra as regras da §23 que o compilador verifica —
entre elas:

- **Regra de Ouro (REQ-5.1):** o Write Side (`Aggregate`, `Command`, `Event`) só
  usa ValueObjects e Enums, nunca primitivos soltos.
- **`access` fechado (REQ-5.2):** todo `Handle` tem entrada correspondente no bloco
  `access`.
- **Exposição (REQ-5.23):** todo `UseCase`/`Query` está exposto numa rota da
  interface — por isso não há avisos de "operação inalcançável".
- **Cobertura de erro (REQ-5.22):** cada `Handle` com caminho de erro de negócio
  tem um cenário `then error` no arquivo de teste.

## Validar

A partir da raiz do repositório:

```sh
go build -o dsc ./cmd/dsc
./dsc docs/examples/wallet      # sem saída e exit 0 = válido
```

## Convenção `Command` ↔ `Handle`

Cada `Command` tem o mesmo nome do `Handle` que aciona (`Deposit`, `Withdraw`).
Isso torna o vínculo comando→handler explícito de ponta a ponta e é o que permite
os cenários de teste escreverem `when Deposit(...)` referenciando a operação
diretamente.
