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

(nenhuma issue registrada ainda)
