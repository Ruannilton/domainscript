# Open Issues

## Revisão da especificação (bloqueiam implementação)

A spec é a fonte de verdade (ver `CLAUDE.md`). Estas issues apontam pontos em
que ela não pode ser implementada como está — texto incompleto ou contraditório.
**Nenhuma linha de código antes de a spec ser revisada**; a auditoria completa
que as originou está em [review-v7.md](../steerings/review-v7.md).

- [`ref` é keyword na §5.1 e identificador na §2.5 (contradição interna)](spec-v7-ref-keyword-vs-identificador.md)
- [Identidade implícita do Aggregate (`self.id`) sem declaração nem tipo](spec-v7-identidade-implicita-do-aggregate.md)
- [Metadata implícito de Event sem tipos nem isenção da Regra de Ouro](spec-v7-metadata-implicito-de-event.md)
- [Sem catálogo normativo de métodos embutidos por tipo](spec-v7-catalogo-de-metodos-embutidos.md)
- [`RetryWithBackoff(3)` usado na §19.2 e definido em lugar nenhum](spec-v7-retrywithbackoff-sem-definicao.md)
- [Nenhuma seção define o contrato de resposta de `Adapter`/`Notification` (`result = call ...`, `mock ... returns X`)](spec-v7-adapter-sem-contrato-de-resposta.md)

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
- [M2.3: mecanismo normativo de `emit` em passo de Saga (design.md §4.4, rota i) exige `decl_policy.go`/`codegen.go`, fora de `target_files`](m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files.md)
- [M4.1: shrinking do contra-exemplo de `property` muda `tests_wallet.go.golden`/`gentest_test.go`, ambos fora de `target_files`](m4-1-shrinking-de-property-muda-golden-fora-de-target-files.md)
