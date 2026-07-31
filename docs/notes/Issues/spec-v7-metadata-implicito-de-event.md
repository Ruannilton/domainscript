ISSUE: [[docs/sdd/issues/spec-v7-metadata-implicito-de-event|spec-v7-metadata-implicito-de-event]]
Solução:
Todo evento possui campos default que são gerados automáticamente pelo compilador, eles são somente leitura para o desenvolvedor, sendo os campos:
- id: Ref Event (uuid v7)
- aggregateId: Ref do agregado que emitiu o evento
- eventType: nome do evento serializado para string (permitindo a desserialização posteriormente)

PROBLEMA: no design original do evento colocamos um aggregateId fazendo com que todo evento seja atrelado a um agregado, entretanto alguns eventos podem ocorrer fora do escopo de um único agregado, por exemplo uma use case. Precisamos pensar melhor na especificação de um [[Application Event]]