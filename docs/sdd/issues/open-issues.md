# Open Issues

## Revisão da especificação (bloqueiam implementação)

A spec é a fonte de verdade (ver [CLAUDE.md](../../../CLAUDE.md)). Estas issues apontam pontos em
que ela não pode ser implementada como está — texto incompleto ou contraditório.
**Nenhuma linha de código antes de a spec ser revisada**; a auditoria completa
que as originou está em [review-v7.md](../steerings/review-v7.md).

- [`sum()` sobre ValueObject e `focus()` sem semântica de ausência (§22 contra o novo §2.8)](spec-v7-sum-e-focus-da-secao-22-contra-catalogo-de-metodos.md)

### Resolvidas na revisão de 2026-07-31

Cinco issues de revisão de spec foram fechadas escrevendo as seções que
faltavam. A spec v7 agora define `ref T`, identidade de Aggregate, envelope de
evento, `ApplicationEvent`, catálogo de métodos, contrato de resposta de
Adapter e o vocabulário de retry/compensação de Saga — **nada disso está
implementado**, e o delta correspondente é trabalho de conformidade a planejar.

| Issue | Onde a spec passou a responder |
|-------|-------------------------------|
| [Identidade implícita do Aggregate (`self.id`)](spec-v7-identidade-implicita-do-aggregate.md) | [§2.7](../steerings/domainscript-spec-v7/02-type-system.md) + [§4.3.1](../steerings/domainscript-spec-v7/04-domain-core.md) |
| [Metadata implícito de Event](spec-v7-metadata-implicito-de-event.md) | [§4.2.3](../steerings/domainscript-spec-v7/04-domain-core.md) + [§5.3](../steerings/domainscript-spec-v7/05-application-layer.md) |
| [Catálogo normativo de métodos embutidos](spec-v7-catalogo-de-metodos-embutidos.md) | [§2.8](../steerings/domainscript-spec-v7/02-type-system.md) |
| [`RetryWithBackoff(3)` sem definição](spec-v7-retrywithbackoff-sem-definicao.md) | [§19.3](../steerings/domainscript-spec-v7/19-transactions-sagas.md) |
| [Contrato de resposta de `Adapter`/`Notification`](spec-v7-adapter-sem-contrato-de-resposta.md) | [§9.4](../steerings/domainscript-spec-v7/09-notifications-adapters.md) |

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
- [M1.4: produtor durável escreve no banco real, mas toda Query do service lê da `store` em memória](m1-4-produtor-duravel-query-le-store-em-memoria-nao-o-banco-real.md)
- [M1.1: uma `Tx.Run()` pode gravar eventos de mais de um `aggregateType` — a rota "thread via `ctx`" não serve](m1-1-tx-run-pode-gravar-mais-de-um-aggregatetype.md)
- [M1.1: nenhuma rota leva o `aggregateType` até `EventStore.Append` dentro do escopo da task](m1-1-aggregatetype-nao-chega-a-eventstore-append.md)
- [M2.3: mecanismo normativo de `emit` em passo de Saga (design.md §4.4, rota i) exige [decl_policy.go](../../../codegen/decl_policy.go)/[codegen.go](../../../codegen/codegen.go), fora de `target_files`](m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files.md)
- [M4.1: shrinking do contra-exemplo de `property` muda `tests_wallet.go.golden`/`gentest_test.go`, ambos fora de `target_files`](m4-1-shrinking-de-property-muda-golden-fora-de-target-files.md)
