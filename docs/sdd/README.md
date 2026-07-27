# Spec-Driven Development — DomainScript

Documentação do processo de desenvolvimento spec-driven do transpilador
DomainScript. Este diretório é um vault Obsidian — abra-o como vault para
navegação interlinked.

## Navegação

| Área | O que contém |
|------|-------------|
| [state.md](state.md) | Ponteiro de retomada — próxima spec-task e próxima issue |
| [specs/](specs/) | Ciclos de desenvolvimento (requirements, design, tasks) |
| [issues/](issues/open-issues.md) | Issues abertas — defeitos, lacunas, revisões de spec |
| [steerings/](steerings/domainscript-spec-v7/README.md) | Spec da linguagem (v7) e auditoria de conformidade |

## Specs (ciclos de desenvolvimento)

| Spec | Assunto | Marco |
|------|---------|-------|
| [transpilador](specs/transpilador/) | Front-end completo (lexer → sema) | A–D |
| [type-checking](specs/type-checking/) | Resolver + Checker (REQ-9..13) | — |
| [codegen](specs/codegen/) | Back-end completo (REQ-14..32) | E–H |
| [read-side](specs/read-side/) | Queries SQL-like + Smart Partial Loading | I |
| [infra-providers](specs/infra-providers/) | 5 providers reais (Postgres, RabbitMQ, Redis, S3, Outbox) | J |
| [correcoes-issues-9-10-11](specs/correcoes-issues-9-10-11/) | Correções ISSUE-9/10/11 | K |
| [correcoes-issues-6-8-12](specs/correcoes-issues-6-8-12/) | Correções ISSUE-6/8/12 (em andamento) | M |

## Referências rápidas

- [Spec da linguagem v7 — índice](steerings/domainscript-spec-v7/README.md)
- [Auditoria implementação vs. spec](steerings/review-v7.md)
- [Gaps do codegen](specs/codegen/gaps.md)
- [Issues abertas](issues/open-issues.md)
