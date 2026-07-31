# 26. Glossário

| Conceito | Descrição |
|----------|-----------|
| **ValueObject** | Tipo imutável com validação e comportamento. |
| **Enum** | Conjunto fechado de valores nomeados. Coerção na borda. |
| **File / FileStream / FileRef** | Bytes em memória / streaming / referência leve no state. |
| **ref T** | Identidade tipada de Aggregate. `ref T` ≠ `ref U`, sem conversão. Representação em `identity` (UUID v7 padrão). |
| **Aggregate** | Fronteira transacional. Identidade implícita `id : ref T`. State, handles, access, storage. Nunca cruza FFI. |
| **CallerId** | Identidade opaca do caller autenticado. Único operador: comparação de vínculo contra `ref T`, em `access`/`visibility`. |
| **Command** | DTO de entrada. Idempotency key implícita. |
| **UseCase** | Unit of Work implícito. |
| **Event / PublicEvent** | Fato imutável interno / compartilhado, emitido por Aggregate. Envelope `eventId`/`eventType`/`timestamp`/`sequence`/`aggregateId`. Campos `redactable` (GDPR). |
| **ApplicationEvent / PublicApplicationEvent** | Fato de escopo de requisição, emitido fora de Aggregate. Envelope troca `aggregateId`/`sequence` por `procedureName`. Não persistido, não replayável. |
| **EventId** | Identidade opaca de evento. UUID v7, única no programa. Só `==`/`!=`; nunca declarável como tipo de campo. |
| **Policy** | Reação a eventos. `BestEffort`/`AtLeastOnce`. |
| **Worker** | Background. `every`/`cron`/`continuous`. `scope` per_tenant/global. |
| **Notification / Adapter** | Contrato de saída / fronteira de infra (HTTP, FFI). |
| **Foreign** | FFI geral. `pure`/`impure`. Apply hermético. Captura em evento. |
| **Saga** | State-machine distribuída. `async`/`await`. `retry:`/`compensate`/`unrecoverable`. |
| **View** | Read-only. `visibility` para field-level security. |
| **Projection** | View materializada cross-aggregate. |
| **Query** | Consulta declarativa. Bloco `cache`. |
| **Tenant** | Ambient context. `row_level`/`schema`/`database` per tenant. |
| **Idempotency** | Chave do cliente. Storage `same`/`external`. |
| **Cache** | `memory`/`distributed`/`layered`. Invalidação por evento. |
| **RateLimit** | Por dimensão ou tier. Token bucket padrão. |
| **Version** | `upcast`/`downcast` de API. `deprecated`/`sunset`. |
| **Test** | Given-When-Then. Property-based. Cobertura semântica. |
| **Telemetry** | OpenTelemetry nativo. |
| **Deploy** | Dockerfile + docker-compose gerados da topologia. Perfis dev/prod. |

