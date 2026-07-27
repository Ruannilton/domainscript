# Exemplos de DomainScript

Estes exemplos existem para **mostrar o que dá para fazer com a linguagem**.
São escritos contra a especificação — [domainscript-spec-v7](../sdd/steerings/domainscript-spec-v7/README.md)
—, que é a fonte de verdade do que a DomainScript é.

> ⚠️ **Estes exemplos não compilam com o transpilador de hoje, e isso é
> intencional.** Eles usam formas que a spec descreve e a implementação ainda
> não aceita (`self` em corpos de ValueObject, `notify`, `Foreign` com
> `pure`/`impure`, identidade implícita do Aggregate, metadata implícito de
> Event). O inventário completo dessa distância está em
> [review-v7.md](../sdd/steerings/review-v7.md). Aqui a spec manda; quando código e spec
> divergem, o código é que está errado.
>
> Se você procura projetos que **passam** no `dsc check`/`dsc gen` de hoje,
> eles vivem em `testdata/projects/` — são fixtures de teste, escritas contra
> o que o transpilador aceita, não material didático.

## Mapa

Um exemplo por área da spec. Cada um é autocontido e tem seu próprio README
dizendo exatamente quais seções demonstra.

| Exemplo | Seções | Mostra |
|---|---|---|
| [`01-tipos-e-fluxo/`](01-tipos-e-fluxo/) | §2, §3 | ValueObjects (wrapper, composto, operadores), Enums e `coerce`, coleções, `File`/`FileRef`, `ensure`/`match`/`for`/`log` |
| [`02-write-side/`](02-write-side/) | §4 | Errors, Events e PublicEvents, versionamento (`default` e `Upcast`), campos `redactable`, Aggregate com `access`/`storage`/snapshot |
| [`03-aplicacao-e-leitura/`](03-aplicacao-e-leitura/) | §5, §6, §16, §22 | Commands, UseCases, Views com `visibility`, Queries com `join`/`in`/cache, Projection cross-database, `focus`/`sum` |
| [`04-reacoes-e-workers/`](04-reacoes-e-workers/) | §7, §8, §9 | Policies, os três modos de Worker, Notifications e Adapters (HTTP declarativo e FFI), `notify` vs `call` |
| [`05-ffi/`](05-ffi/) | §10 | `Foreign` `pure`/`impure`, `throws`, marshalling, onde cada natureza pode ser chamada |
| [`06-sagas/`](06-sagas/) | §19, §23 | Inferência transacional, Saga com `up`/`down`/`onInfraError`, `unrecoverable` |
| [`07-infra-e-topologia/`](07-infra-e-topologia/) | §11, §12, §13, §21 | `mod.ds`, `interface.ds` (HTTP e gRPC), `topology.ds`, e o deploy derivado daí |
| [`08-tenancy-e-limites/`](08-tenancy-e-limites/) | §14, §15, §17, §18, §20 | Multi-tenancy, idempotência, rate limiting por tier, versionamento de API, `Metric` |
| [`09-testes/`](09-testes/) | §24 | `*.test.ds`: Aggregate, UseCase transacional, mocks, Saga, Policy, property-based, fixtures |

## Domínios usados

Os mesmos dois da spec, para que os tipos se reaproveitem entre exemplos em
vez de cada um inventar um mundo novo:

- **Carteira Digital (Wallet)** — construtos fundamentais.
- **Plataforma de Ingressos (Ticketing)** — fluxos distribuídos: Sagas,
  Policies, Workers.

## Como ler

Comece pelo `01`, que estabelece os tipos que os outros reusam. Cada README
lista as regras da §25 (regras de compilação) que o exemplo exercita — inclusive
as que ele **viola de propósito**, em blocos marcados, para mostrar o que o
compilador recusa.
