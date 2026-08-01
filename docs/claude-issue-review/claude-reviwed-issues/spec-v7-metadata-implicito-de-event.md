CODIGO: spec-v7-metadata-implicito-de-event
CATEGORIA: Correção de código
Issue original: [[docs/sdd/issues/spec-v7-metadata-implicito-de-event]]

## Resumo da issue

A spec dizia que todo evento carrega metadata implícito readonly (`timestamp`, `sequence`, `aggregateId`, `eventType`) e depois **usava** esse metadata no exemplo canônico (`Apply DepositPerformed` lendo `event.timestamp`), mas nunca definia tipos, nunca dizia se esses campos eram isentos da Regra de Ouro (primitivos são proibidos no Write Side, e Event é Write Side), nem onde exatamente eram legíveis. Rodar o exemplo verbatim contra o `dsc` do HEAD dava `error[E102]: membro inexistente: "timestamp" em DepositPerformed`.

## Evidencias

- `error[E102]: membro inexistente: "timestamp" em DepositPerformed` — verificado rodando a antiga §4.5 (hoje `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]`) verbatim contra o `dsc` do HEAD.
- Dos quatro campos citados na abertura da antiga §4.2 (hoje `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.2.3), só `event.timestamp` era efetivamente usado em algum exemplo da spec; os outros três apareciam só na frase de abertura, sem definição.
- A nota do desenvolvedor em `[[docs/notes/Issues/spec-v7-metadata-implicito-de-event|docs/notes/Issues/spec-v7-metadata-implicito-de-event]]` decide os campos (`id: Ref Event` uuid v7, `aggregateId: Ref do agregado`, `eventType: nome serializado`), mas também registra um problema residual: "no design original... colocamos um `aggregateId` fazendo com que todo evento seja atrelado a um agregado, entretanto alguns eventos podem ocorrer fora do escopo de um único agregado... Precisamos pensar melhor na especificação de um `[[docs/notes/Features/Application Event|Application Event]]`."
- `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.2.3 resolve integralmente a parte do metadata de evento de Aggregate: tabela dos cinco campos com tipo/semântica/quem preenche, isenção explícita da Regra de Ouro (`"a isenção vale só para estes cinco campos"`), onde é legível por contexto, interação com `redactable`/`Upcast`, comportamento em `*.test.ds`, e um exemplo completo com a lista de erros que ele recusaria.
- `[[docs/sdd/steerings/domainscript-spec-v7/05-application-layer|05-application-layer.md]]` §5.3 resolve o problema residual que a nota do desenvolvedor apontou: o `ApplicationEvent` (evento de escopo de requisição, sem `aggregateId`/`sequence`, com `procedureName` no lugar) cobre exatamente o caso de fato que não pertence a um único Aggregate.

## Impacto no projeto

Sem essa definição, nenhum evento no domínio conseguia ler seu próprio metadata (nem o exemplo canônico da spec compilava), e não havia base normativa para o front-end recusar colisão de nome, atribuição a campo readonly, ou uso de metadata fora dos contextos permitidos.

## Soluçoes possíveis

### Solucão 1

Implementar o envelope conforme `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.2.3/`[[docs/sdd/steerings/domainscript-spec-v7/05-application-layer|05-application-layer.md]]` §5.3 já normatizam: `eventId`/`eventType`/`timestamp`/`sequence`/`aggregateId` como membros implícitos e readonly de `Event`, gerados pelo compilador (constantes por declaração ou preenchidos pelo runtime na emissão), com a checagem de colisão de nome, a isenção pontual da Regra de Ouro, e as regras de onde `event.*` está em escopo (`Apply`, `Upcast`, Policy, `Metric ... on E`, elemento de `events()`) — tudo já descrito com tabelas de erro prontas para virar teste.

### Solução 2

Implementar em paralelo o `ApplicationEvent` de `[[docs/sdd/steerings/domainscript-spec-v7/05-application-layer|05-application-layer.md]]` §5.3 (o par `eventId`/`eventType`/`timestamp`/`procedureName`, sem `aggregateId`/`sequence`) para o caso de evento emitido fora de Aggregate. Não é alternativa à Solução 1 — é o complemento que a própria nota do desenvolvedor pediu, e ambas precisam entrar juntas: `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.2.3 já assume a existência do `ApplicationEvent` na própria tabela ("E emitido só fora de Aggregate → evento de escopo de requisição, ver o gancho no fim desta seção").

## O que precisa ser resolvido antes

Nenhuma — a spec já é clara e cobre inclusive o problema residual que a nota do desenvolvedor tinha sinalizado como pendente: `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.2.3 (envelope de evento de domínio) e `[[docs/sdd/steerings/domainscript-spec-v7/05-application-layer|05-application-layer.md]]` §5.3 (envelope de `ApplicationEvent`) formam juntas a definição completa. O trabalho restante é implementação em parser/resolver/checker/`types.Model` (para reconhecer os membros implícitos e aplicar as regras de acesso) e no codegen (para o runtime preencher os campos na emissão). Depende, para a parte de `ApplicationEvent`, do mesmo esforço de front-end citado em `[[docs/sdd/issues/m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files|m2-3]]`.
