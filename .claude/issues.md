# Issues

Registro de erros encontrados pelos agentes durante a execução das specs que
**não pertencem ao escopo da task/spec em andamento** (erros do escopo atual
são corrigidos na hora, sem entrar aqui — ver `CLAUDE.md`).

## Como registrar

Cada issue é um bloco novo, nesta forma:

```
## ISSUE-<numero>
- SPEC: <nome-da-spec>
- TASK: <numero-da-task>
- DESCRIPTION: <descrição do erro encontrado, contexto e impacto>
```

- `<numero>` é sequencial, nunca reaproveitado.
- `SPEC`/`TASK` identificam onde o erro foi **encontrado** (não necessariamente
  onde ele deveria ser corrigido).
- Issues aqui ficam pendentes até serem resolvidas em uma task futura; ao
  resolver, marque com `RESOLVED (commit <hash>)` ao final do bloco em vez de
  apagar o registro.

---

## ISSUE-1
- SPEC: read-side
- TASK: I5.1
- DESCRIPTION: `emitQueryJoinCollectionVars` (`codegen/decl_query.go`) gera
  variáveis de pacote como `var ticketCollection = runtime.NewMemoryCollection[Ticket]()`
  no arquivo `queries.go`. Se o MESMO tipo (ex. `Ticket`) também for
  referenciado num `list`/`count` dentro de uma Policy do MESMO módulo,
  `emitPolicyCollectionVars` (`codegen/decl_policy.go`) gera a MESMA variável,
  com o MESMO nome (`policyCollectionVarName`/convenção `<tipo>Collection`),
  em `policies.go` — os dois arquivos compartilham o MESMO pacote Go, então o
  compilador falha com "redeclared in this block". Nenhum exemplo real hoje
  exercita essa combinação (nenhum módulo tem Query com join E Policy com
  list/count sobre o mesmo tipo), por isso não foi pego pelos testes
  existentes. Correção sugerida: centralizar a declaração dessas
  Collection[T] var num único arquivo por módulo (ex. `collections.go`),
  compartilhado entre `EmitQueries`/`EmitPolicies`, em vez de cada emissor
  declarar as suas independentemente.
