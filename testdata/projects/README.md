# Projetos-fixture do transpilador

Projetos DomainScript usados como **entrada de teste**. Existem para pinar o
comportamento da implementação: são lidos do disco por testes de `driver/`,
`codegen/`, `codegen/lower/` e `codegen/goname/`, e o job `fixtures` do CI
roda `dsc check` + `dsc gen` + `go build`/`go vet` sobre cada um a cada push.

> **Isto não é material didático.** Estes projetos são escritos contra **o que
> o transpilador aceita hoje** — inclusive formas que a especificação da
> linguagem não descreve (o sentinela `ok` em `Valid`, `value` como receptor
> de ValueObject). Mudá-los para conformar à spec quebraria os goldens sem
> ganho: o papel deles é detectar regressão, não ensinar a linguagem.
>
> **Se você quer aprender DomainScript, vá para [`docs/examples/`](../../docs/examples/)** —
> lá os exemplos são escritos contra a especificação, que é a fonte de verdade.
> Eles ainda não compilam, e isso é intencional: são a meta de conformidade.
> `.claude/steerings/review-v7.md` mede a distância entre as duas coisas.

| Projeto | O que exercita |
|---------|----------------|
| [`wallet/`](./wallet) | Módulo completo: ValueObjects, Enum, Aggregate EventSourced, Commands, UseCases, Read Side (View/Query), interface HTTP, testes nativos. É a fixture mais lida — vários goldens saem dela. |
| [`shop/`](./shop) | Multi-módulo: dois módulos em services distintos ligados por canal. Cobre as regras cross-file — PublicEvent, Policy cross-service, canal obrigatório. |
| [`pizzeria/`](./pizzeria) | SaaS multi-tenant com dois módulos coreografados por eventos públicos: `tenant { from: subdomain }`, `tenancy: row_level`, `Idempotency`/`Cache` de módulo, `visibility` de View, Snapshot. **Valida mas ainda não gera** — ver `KNOWN_UNGENERATABLE` em `.github/workflows/ci.yml`. |

## Como rodar

Da raiz do repositório:

```sh
go build -o dsc ./cmd/dsc
./dsc check testdata/projects/wallet     # exit 0, sem diagnóstico
./dsc gen   testdata/projects/wallet -o /tmp/out
```

Saída esperada do `check`: nenhuma — exit code `0`. Introduza um erro (troque
um ValueObject por `integer` no `state` de um Aggregate) e a CLI passa a
reportar o diagnóstico com posição e mensagem acionável, com exit code `1`.

## Ao mexer aqui

Estas fixtures são entrada de golden test. Alterar um `.ds` muda o Go gerado e
**quebra o golden correspondente** — o que é o comportamento desejado quando a
mudança é intencional (regenere o golden), e um alarme útil quando não é.
