# Spec v7: metadata implícito de Event sem tipos nem isenção da Regra de Ouro
- SPEC: [domainscript-spec-v7](../steerings/domainscript-spec-v7/README.md)
  (revisão da especificação)
- TASK: [review-v7.md §A-3](../steerings/review-v7.md)
- DESCRIPTION: A [§4.2](../steerings/domainscript-spec-v7/04-domain-core.md)
  abre com "Metadata implícito readonly: `timestamp`, `sequence`,
  `aggregateId`, `eventType`" e não diz mais nada sobre eles. A
  [§4.5](../steerings/domainscript-spec-v7/04-domain-core.md) então **usa**
  um deles no exemplo canônico — `Apply DepositPerformed`
  preenche `date: event.timestamp` ao montar um `StatementEntry`. Nenhum dos
  quatro existe hoje: rodando a §4.5 verbatim o `dsc` do HEAD dá
  `error[E102]: membro inexistente: "timestamp" em DepositPerformed`.
  O que falta a spec dizer para que a regra seja implementável:
  1. **Tipos.** `timestamp` é `datetime` e `sequence` é `integer` por
     inferência óbvia, mas `aggregateId` não tem tipo dedutível (string opaca?
     o VO de id do Aggregate? — ver a issue da identidade implícita, com que
     este ponto se cruza) e `eventType` idem (string? um enum fechado gerado
     pelo compilador?).
  2. **Relação com a Regra de Ouro
     ([§2.1](../steerings/domainscript-spec-v7/02-type-system.md)).**
     Primitivos são proibidos no Write
     Side, e Event é Write Side. Se `timestamp datetime` e `sequence integer`
     são campos do evento, a spec precisa declarar explicitamente que metadata
     implícito é isento — hoje isso só está subentendido.
  3. **Onde é legível.** A §4.5 lê `event.timestamp` dentro de um `Apply`. Vale
     em `Handle`? Em Policy (`event.*` já é receptor lá)? Em Query sobre
     `events()`? O texto não delimita.
  4. **Interação com `redactable`
     ([§4.4](../steerings/domainscript-spec-v7/04-domain-core.md)) e com
     Upcast ([§4.3](../steerings/domainscript-spec-v7/04-domain-core.md)).**
     Metadata entra
     na redação? Sobrevive a um upcast de versão?
  Observação de escopo: `event.timestamp` é o único dos quatro que algum
  exemplo da spec exercita; os outros três aparecem só na frase de abertura da
  §4.2. Definir os quatro de uma vez evita uma segunda rodada.
- SOLVED: FALSE
