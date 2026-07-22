# Exemplos de DomainScript

Projetos de exemplo que exercitam o front-end do transpilador de ponta a ponta.
Cada exemplo é um projeto válido — `dsc` o valida sem erros nem avisos.

| Exemplo | O que demonstra |
|---------|-----------------|
| [`wallet/`](./wallet) | Um módulo de domínio completo: ValueObjects, Enum, Aggregate EventSourced, Commands, UseCases, Read Side (View/Query), interface HTTP e testes nativos. |
| [`shop/`](./shop) | Um projeto multi-módulo: dois módulos em services distintos, ligados por um canal na topologia. Foca nas regras arquiteturais *cross-file* — PublicEvent, Policy cross-service e canal obrigatório. |
| [`pizzeria/`](./pizzeria) | Um SaaS multi-tenant com dois módulos (Sales, Kitchen) coreografados por eventos públicos. Foca nas diretivas v6.0 que os outros exemplos não cobrem — `tenant { from: subdomain }`, `tenancy: row_level`, `Idempotency`/`Cache` de módulo, `cache`/`idempotency` por construto, `visibility` de View — e no padrão Snapshot (dependência temporal). |

## Como validar
Compile a CLI a partir da raiz do repositório e aponte para a pasta do exemplo:

```sh
go build -o dsc ./cmd/dsc
./dsc docs/examples/wallet
```

Saída esperada: nenhuma — exit code `0`, sinalizando um projeto válido. Introduza
um erro (ex.: troque um ValueObject por `integer` no `state` de um Aggregate) e a
CLI passa a reportar o diagnóstico com posição e mensagem acionável, retornando
exit code `1`.
