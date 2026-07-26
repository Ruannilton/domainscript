# Open Issues

## Revisão da especificação (bloqueiam implementação)

A spec é a fonte de verdade (ver `CLAUDE.md`). Estas issues apontam pontos em
que ela não pode ser implementada como está — texto incompleto ou contraditório.
**Nenhuma linha de código antes de a spec ser revisada**; a auditoria completa
que as originou está em `.claude/steerings/review-v7.md`.

- [`ref` é keyword na §5.1 e identificador na §2.5 (contradição interna)](spec-v7-ref-keyword-vs-identificador.md)
- [Identidade implícita do Aggregate (`self.id`) sem declaração nem tipo](spec-v7-identidade-implicita-do-aggregate.md)
- [Metadata implícito de Event sem tipos nem isenção da Regra de Ouro](spec-v7-metadata-implicito-de-event.md)
- [Sem catálogo normativo de métodos embutidos por tipo](spec-v7-catalogo-de-metodos-embutidos.md)
- [`RetryWithBackoff(3)` usado na §19.2 e definido em lugar nenhum](spec-v7-retrywithbackoff-sem-definicao.md)

## Implementação

- [`Cache`/`RateLimit` `backend:` exige string literal, contra a forma da §13](cache-ratelimit-backend-exige-string-contra-spec.md)
- [`access { requires ... }` em UseCase não é parseado (§14)](usecase-access-block-nao-parseado.md)
- [Features do spec v6 nunca modeladas pelo front-end](features-spec-v6-nao-modeladas-pelo-frontend.md)
- [Providers reais de infraestrutura ausentes (G-4)](providers-reais-de-infraestrutura-ausentes.md)
- [Field-Level Security de View não implementado (`visibility` ignorado)](visibility-de-view-nao-implementado.md)
- [Observabilidade OTel parcial (métricas e logs não exportados)](observabilidade-otel-parcial.md)
- [Lacunas nos testes gerados a partir de `*.test.ds`](lacunas-nos-testes-gerados-test-ds.md)
- [UseCase e Policy no mesmo módulo não geram (colisão de Wire)](usecase-e-policy-no-mesmo-modulo-colisao-de-wire.md)
- [Divergências menores do spec (§25, "em evolução")](divergencias-menores-do-spec-em-evolucao.md)
- [Pizzeria bloqueado por múltiplos defeitos independentes de codegen](pizzeria-bloqueado-por-multiplos-defeitos-de-codegen.md)
- [UseCase com `idempotency { required: true }` é intestável via `*.test.ds`](usecase-idempotency-required-intestavel-test-ds.md)
- [M1.1: nenhuma rota leva o `aggregateType` até `EventStore.Append`](m1-1-aggregatetype-nao-chega-a-eventstore-append.md)
- [M1.4: produtor durável escreve no banco real, mas toda Query do service lê da `store` em memória](m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real.md)
