# Exemplo: módulo `Wallet`

Um bounded context mínimo, porém completo, de uma carteira digital. Mostra como as
peças de DomainScript se encaixam num módulo único e válido — e, desde a
atualização de infraestrutura descrita abaixo, como um deploy **real** desse
módulo se pareceria: Postgres como fonte da verdade e Redis para o caminho
quente de leitura.

## Infraestrutura simulada (providers reais)

O exemplo declara providers que o gerador reconhece de verdade (Marco J):

| Bloco | Provider | O que o gerador faz |
|-------|----------|---------------------|
| `Database MainDb` | `postgres` | vendora `sqlruntime/*`, puxa `github.com/jackc/pgx/v5`, abre a conexão em `cmd/wallet/main.go` |
| `Cache` | `redis` | cache da Query `GetWallet` compartilhado entre réplicas; puxa `github.com/redis/go-redis/v9` |
| `RateLimit` | `redis` | cota **global ao cluster**, não por processo |
| `Idempotency` | `same` | chave gravada na MESMA transação dos eventos |

Nada disso é decorativo: sem declarar o provider, o `go.mod` gerado não ganha
dependência externa nenhuma (NFR-21). Todo endpoint/credencial vem do ambiente:

```sh
export WALLET_DATABASE_URL="postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"
export WALLET_REDIS_URL="redis://localhost:6379/0"
export WALLET_HTTP_PORT=8080
```

**Redis fora do ar não derruba o serviço.** O cache degrada para in-process
(fail-open, §15) e o rate limit degrada para contagem por-réplica (REQ-44.5) —
os dois logam o motivo e seguem servindo. O Postgres, esse sim, é fail-fast:
sem banco não há fonte da verdade.

## Estrutura

| Arquivo | Camada | Conteúdo |
|---------|--------|----------|
| `mod.ds` | Infraestrutura (§12) | `Module Wallet`, o `Database` Postgres que persiste o Aggregate, e os blocos `Cache`/`RateLimit`/`Idempotency`. |
| `domain.ds` | Write Side (§2, §4) | ValueObjects, `Enum TransactionType`, `Error`s, `Event`s e o `Aggregate Wallet` (EventSourced, com `access`, `Handle` e `Apply`). |
| `application.ds` | Aplicação (§5) | `Command`s e `UseCase`s que carregam o Aggregate e despacham os Handles. |
| `read.ds` | Read Side (§6) | `View WalletView` e as `Query`s de leitura — `GetWallet` com `cache` (Redis), `ListEntries` deliberadamente sem. |
| `interface.ds` | Borda (§10) | `Interface HTTP` mapeando rotas para UseCases e Queries, com `rateLimit` por rota. |
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

## Gerar o back-end

```sh
./dsc gen docs/examples/wallet -o /tmp/wallet-gen
cd /tmp/wallet-gen && go mod tidy && go build ./... && go vet ./...
```

O projeto gerado compila sem nenhum serviço de pé — as conexões só são abertas
em runtime, e as falhas de Redis degradam como descrito acima.

## Convenção `Command` ↔ `Handle`

Cada `Command` tem o mesmo nome do `Handle` que aciona (`Deposit`, `Withdraw`).
Isso torna o vínculo comando→handler explícito de ponta a ponta e é o que permite
os cenários de teste escreverem `when Deposit(...)` referenciando a operação
diretamente.
