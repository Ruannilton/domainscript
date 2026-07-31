# 23. Erros: Negócio vs. Infraestrutura

| Tipo | Declaração | Tratamento |
|------|-----------|------------|
| Negócio | `Error` no domínio (ou `throws` em Foreign) | HTTP 4xx |
| Infraestrutura | Nunca no domínio | `mod.ds` (retry, circuit breaker) + bloco `retry:` do step de Saga ([§19.3](19-transactions-sagas.md)) |

