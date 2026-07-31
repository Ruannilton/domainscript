# 25. Regras de Compilação (Resumo)

Resumo tabular normativo. Cada regra é definida na seção citada; esta é a lista
autoritativa do que é **❌ erro**, do que é **⚠️ warning** e do que é rejeitado
na borda em runtime.

## 25.1. Sistema de Tipos

| Regra | Resultado |
|-------|-----------|
| Primitivo no Write Side — Aggregates, Commands, Events, ApplicationEvents ([§2.1](02-type-system.md)) | ❌ Erro |
| ValueObject que poderia ser Enum ([§2.3](02-type-system.md)) | ⚠️ Warning |

## 25.2. Referências e Identidade

| Regra | Resultado |
|-------|-----------|
| `ref` sobre tipo que não é Aggregate ([§2.7](02-type-system.md)) | ❌ Erro |
| Atribuição ou comparação entre `ref` de Aggregates distintos | ❌ Erro |
| `ref T` misturado com a representação subjacente (sem unwrap) | ❌ Erro |
| `load <Aggregate>(x)` com `x` que não é `ref <Aggregate>` | ❌ Erro |
| `<`/`>`/`orderBy` sobre `ref T` com `generation: client` | ❌ Erro |
| `identity { type: string, generation: system }` | ❌ Erro |
| `identity { type: integer, generation: system }` em Aggregate `EventSourced` | ❌ Erro |
| `identity { type: integer, generation: system }` sem `Database` em `storage.state` | ❌ Erro |
| Bloco `identity` repetido, ou em construto que não é Aggregate | ❌ Erro |
| `new_ref(T)` sobre identidade que não é `uuid` + `system` | ❌ Erro |
| `new_ref(T)` em `Apply`, `Valid`, `coerce` ou Query | ❌ Erro |
| Identificador chamado `ref` | ❌ Erro (sintaxe) |
| Literal em posição `ref T` fora de `*.test.ds` | ❌ Erro |
| Campo `id` declarado no `state` de um Aggregate ([§4.3.1](04-domain-core.md)) | ❌ Erro |
| `state.id` (leitura ou escrita), ou atribuição a `self.id` | ❌ Erro |
| `self.id` fora de `Handle`/`Apply`/`access`/`visibility`/instância carregada | ❌ Erro |
| `caller.id` fora de `access`/`visibility`, ou comparado a algo que não é `ref T` | ❌ Erro |
| Campo `identity` de Command: tipo ≠ `ref T`, mais de um, ausente com `generation: client`, ou presente com `generation: system` | ❌ Erro |
| Declarar ValueObject, Enum ou Aggregate chamado `EventId` ou `CallerId` | ❌ Erro |
| `ValueObject <T>Id` num programa que declara `Aggregate <T>` | ⚠️ Warning |
| Valor `ref` malformado na borda | ❌ 422 |

## 25.3. Métodos Embutidos e Coleções

| Regra | Resultado |
|-------|-----------|
| Chamada de método fora do catálogo ([§2.8](02-type-system.md)) | ❌ Erro |
| Método do catálogo com aridade errada ou no tipo errado | ❌ Erro |
| Forma sem parênteses em método com parâmetros | ❌ Erro |
| Forma sem parênteses em posição de statement | ❌ Erro |
| Chamada de método como alvo de atribuição | ❌ Erro |
| Mutador de coleção em posição de expressão | ❌ Erro |
| Mutador de coleção em `Valid`/`Operator`/`coerce`/Query/`visibility` | ❌ Erro |
| Indexação (`l[i]`, `s[i]`, `m[k]`) | ❌ Erro |
| `Map.get` com um argumento (sem `default`) | ❌ Erro |
| `for` direto sobre `Map<K,V>` | ❌ Erro |
| Chave de `Map` de tipo não admitido ([§2.8.8](02-type-system.md)) | ❌ Erro |
| `sum(f)` com projeção não numérica | ❌ Erro |
| Método de primitivo sobre ValueObject wrapper fora do corpo do VO | ❌ Erro |
| `Operator` de VO invocado em forma de método | ❌ Erro |
| Duração não-literal em `datetime.plus`/`minus` | ❌ Erro |
| Divisor literal `0` | ❌ Erro |
| Argumento literal negativo em `take`/`skip`/`round` | ❌ Erro |
| `remove()`/`clear()` em `AppendList<T>` ([§2.4](02-type-system.md)) | ❌ Erro |

## 25.4. Controle de Fluxo

| Regra | Resultado |
|-------|-----------|
| `match` não-exaustivo / guards sem `_` ([§3](03-control-flow.md)) | ❌ Erro |
| `Nop` em Handle/UseCase | ❌ Erro |
| `break`/`continue` fora de `for` | ❌ Erro |

## 25.5. Write Side: Aggregates e Events

| Regra | Resultado |
|-------|-----------|
| Handle sem entrada no `access` ([§4.3](04-domain-core.md)) | ❌ Erro |
| Campo de `Event`/`PublicEvent` com nome de envelope (`eventId`, `eventType`, `timestamp`, `sequence`, `aggregateId`) ([§4.2.3](04-domain-core.md)) | ❌ Erro |
| Escrita em campo do envelope, inclusive dentro de `Upcast` | ❌ Erro |
| Campo do envelope passado como argumento de `emit` | ❌ Erro |
| `Event` emitido por mais de um Aggregate, ou dentro e fora de Aggregate | ❌ Erro |
| `event.aggregateId`/`event.sequence` em evento sem Aggregate emissor | ❌ Erro |
| `event.*` em `Handle`, UseCase, Saga, Worker, `visibility`/Projection ou ValueObject | ❌ Erro |
| `ref <Event>`; `EventId` como tipo de campo declarado; `EventId` ordenado ou com método | ❌ Erro |
| `eventType` comparado a literal não declarado ou sem qualificação de módulo | ❌ Erro |
| `Event` declarado e nunca emitido nem aplicado | ⚠️ Warning |
| Comparação de `eventType` em contexto de tipo estaticamente único (`Apply E`, `Policy on E`, `Upcast E`, `Metric on E`) | ⚠️ Warning |
| Upcast substituível por `default` ([§4.2.1](04-domain-core.md)) | ⚠️ Warning |

## 25.6. Camada de Aplicação

| Regra | Resultado |
|-------|-----------|
| `emit` de `Event`/`PublicEvent` fora de Aggregate ([§5.3](05-application-layer.md)) | ❌ Erro |
| `emit` de `ApplicationEvent` em Handle, Apply, Query, View/Projection, Adapter ou ValueObject | ❌ Erro |
| `Apply` sobre `ApplicationEvent` | ❌ Erro |
| `Upcast` de `ApplicationEvent` | ❌ Erro |
| Campo `redactable` em `ApplicationEvent` | ❌ Erro |
| `event.aggregateId`/`event.sequence` em `ApplicationEvent` | ❌ Erro |
| Campo de `ApplicationEvent` colidindo com o envelope (`eventId`, `eventType`, `timestamp`, `procedureName`) | ❌ Erro |
| Literal de `procedureName` que não nomeia procedimento declarado | ❌ Erro |
| `ApplicationEvent` declarado e nunca emitido | ⚠️ Warning |
| `ApplicationEvent` emitido sem consumidor algum | ⚠️ Warning |
| Comparação de `procedureName` com sítio de `emit` único | ⚠️ Warning |
| Conflito de idempotency (mesma chave, command diferente) ([§15](15-idempotency.md)) | ❌ 422 |

## 25.7. Read Side

| Regra | Resultado |
|-------|-----------|
| JOIN cross-database ([§6.3](06-read-side.md)) | ❌ Erro |
| Cache em listagem de alta cardinalidade ([§16](16-cache.md)) | ⚠️ Warning |

## 25.8. Policies e Workers

| Regra | Resultado |
|-------|-----------|
| Policy cross-module escutando `Event` não público ([§7](07-policies.md)) | ❌ Erro |
| Policy cross-module escutando `ApplicationEvent` não público ([§5.3](05-application-layer.md)) | ❌ Erro |

## 25.9. Notifications, Adapters e FFI

| Regra | Resultado |
|-------|-----------|
| `Notification` sem `Adapter` ([§9.1](09-notifications-adapters.md)) | ❌ Erro |
| Tipo de resposta de `Notification` inexistente ou não-VO/Enum ([§9.4](09-notifications-adapters.md)) | ❌ Erro |
| `Notification` com `-> Tipo` e `Adapter mode async` | ❌ Erro |
| `notify` sobre Adapter `mode sync` / `call` sobre Adapter `mode async` | ❌ Erro |
| Captura de resultado de `notify`, ou `call` de Notification sem `-> Tipo` | ❌ Erro |
| `notify`/`call` em Handle, Apply ou ValueObject | ❌ Erro |
| Adapter Nível 1 sem `response { }` para Notification com `-> Tipo` (ou com ele sem `->`) | ❌ Erro |
| `response { }` com campo do VO faltando, inexistente ou duplicado | ❌ Erro |
| `function` do Adapter Nível 2 com retorno divergente do `-> Tipo` da Notification | ❌ Erro |
| FFI/Adapter com assinatura incompatível ([§10](10-ffi.md)) | ❌ Erro |
| Aggregate cruzando fronteira FFI | ❌ Erro |
| FFI em `Apply` (pure ou impure) | ❌ Erro |
| FFI impura em Handle sem captura em evento | ❌ Erro |
| FFI impura em Query/ValueObject | ❌ Erro |
| FFI impura dentro de transação | ⚠️ Warning (efeito não revertido) |
| `call` dentro de UseCase transacional | ⚠️ Warning (efeito não revertido) |

## 25.10. Transações e Sagas

| Regra | Resultado |
|-------|-----------|
| UseCase cross-database sem XA / cross-service sem Saga ([§19.1](19-transactions-sagas.md)) | ❌ Erro |
| `onInfraError` / `RetryWithBackoff` em step de Saga ([§19.3](19-transactions-sagas.md)) | ❌ Erro |
| `retry.attempts` ausente, não-literal ou < 1 | ❌ Erro |
| `retry.backoff` fora de `"none"`/`"fixed"`/`"exponential"` | ❌ Erro |
| `retry:` duplicado no mesmo bloco | ❌ Erro |
| `compensate` fora de `up` de Saga, ou sem `Error` | ❌ Erro |
| `unrecoverable` acompanhado de outro statement no `down` | ❌ Erro |
| `retry:` inerte (não altera a política herdada) | ⚠️ Warning |
| Orçamento de retry maior que o `timeout` do `mode await` | ⚠️ Warning |
| `up` retentável com função não determinística (`uuid()`, `now()`, `random*`) | ⚠️ Warning |
| Saga `await` sobre canal `queue` ([§12](12-topology.md)) | ⚠️ Warning |

## 25.11. Topologia, Tenancy e Infraestrutura

| Regra | Resultado |
|-------|-----------|
| Módulos em services diferentes sem canal ([§12](12-topology.md)) | ❌ Erro |
| Acesso cross-tenant sem opt-in ([§14](14-multi-tenancy.md)) | ❌ Erro |
| Canal `queue`/`stream` sem `orderBy` | ⚠️ Warning |
| Canal com `orderBy` carregando só `PublicApplicationEvent` | ⚠️ Warning (inerte) |
| UseCase cross-tenant declarado | ⚠️ Warning (auditoria) |
| Provider cloud sem equivalente local (profile dev) ([§21](21-deploy.md)) | ⚠️ Warning |

## 25.12. Interface e Versionamento de API

| Regra | Resultado |
|-------|-----------|
| Upcast de API com campo obrigatório sem default ([§18](18-api-versioning.md)) | ❌ Erro |
| UseCase/Query não exposto em interface ([§11](11-interface.md)) | ⚠️ Warning |

## 25.13. Testes

| Regra | Resultado |
|-------|-----------|
| Teste referenciando evento/comando inexistente ([§24](24-testing.md)) | ❌ Erro |
| Metadata de evento fornecido em `given`/`when`, ou asserido em `then` | ❌ Erro |
| Mock de Notification sem `-> Tipo`, ou sem `returns` quando há `-> Tipo` | ❌ Erro |
| `T("alias") emitted <ApplicationEvent>`, ou `ApplicationEvent` em `given` | ❌ Erro |
| Handle sem cenário de erro testado | ⚠️ Warning |
