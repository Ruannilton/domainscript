# 1. Visão Geral e Filosofia

DomainScript não é uma linguagem de propósito geral. É uma DSL estritamente opinada para construir backends baseados em **Domain-Driven Design (DDD)** e **CQRS**.

## 1.1. O Paradigma

- **Regras Puras:** O desenvolvedor escreve apenas regras de negócio e contratos de dados.
- **Zero Infraestrutura:** Não há SQL, ORM, HTTP Client, injeção de dependência ou controle transacional no código de domínio.
- **Restrição Criativa (Fail-Fast):** A linguagem proíbe arquiteturas ruins em tempo de compilação. Se compila, a arquitetura está correta.
- **Transpilação:** DomainScript é transpilado para a linguagem alvo (ex: Go), aproveitando o ecossistema da plataforma destino.
- **Uma Forma Canônica:** Para cada operação existe uma única forma de expressá-la.
- **Exaustividade Obrigatória:** Toda ramificação de valor é exaustiva em tempo de compilação.
- **Observabilidade Nativa:** Instrumentação OpenTelemetry gerada automaticamente.
- **Deploy Derivado:** O compilador gera os artefatos de deploy a partir da topologia declarada.

## 1.2. Escopo

DomainScript foca **exclusivamente em sistemas transacionais empresariais** — backends com regras de negócio complexas, consistência forte, auditoria, integração entre módulos e times distribuídos.

**Não é uma solução universal, e isso é intencional:**

| Domínio fora de escopo | Razão |
|------------------------|-------|
| Streaming de alta frequência (IoT, market making) | Paradigma não-transacional |
| ML/AI workflows (training, feature stores) | Outro paradigma computacional |
| Algoritmos de grafo (recomendação, fraude relacional) | Query language não cobre traversal |
| Busca textual (full-text, fuzzy) | Requer engine especializada |
| Dados espaciais (geolocalização, polígonos) | Requer extensões espaciais |

Para estes, integre via Adapter ou FFI. A força da linguagem está em recusar a universalidade.

## 1.3. Estrutura de Arquivos

| Arquivo | Propósito |
|---------|-----------|
| `*.ds` | Código de domínio (ValueObjects, Aggregates, Commands, UseCases, Policies, Sagas, Workers, Metrics) |
| `*.test.ds` | Testes declarativos |
| `foreign/*.ds` | Declarações FFI (assinaturas tipadas de funções estrangeiras) |
| `mod.ds` | Infraestrutura do módulo (Database, FileStorage, Cache, RateLimit, Idempotency, Telemetry, Outbox) |
| `interface.ds` | Exposição via protocolos, tenant resolution, rate limit, versionamento |
| `topology.ds` | Topologia de deployment (services, canais) |
| `contracts/*.ds` | Eventos públicos compartilhados entre módulos |
| `versions/*.ds` | Transformações entre versões de API |
| `adapters/*`, `foreign/*` | Código na linguagem alvo (Adapters FFI e Foreign functions) |

