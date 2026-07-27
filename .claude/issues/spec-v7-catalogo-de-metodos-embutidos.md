# Spec v7: sem catálogo normativo de métodos embutidos por tipo
- SPEC: [domainscript-spec-v7](../steerings/domainscript-spec-v7/README.md)
  (revisão da especificação)
- TASK: [review-v7.md §A-7](../steerings/review-v7.md)
- DESCRIPTION: A spec não tem, em lugar nenhum, a lista autoritativa dos
  métodos disponíveis por tipo. A
  [§2.6](../steerings/domainscript-spec-v7/02-type-system.md) cataloga só
  **funções livres** (`now()`, `uuid()`, `random()`, `random_str()`,
  `signed_url()`); a [§2.4](../steerings/domainscript-spec-v7/02-type-system.md)
  lista operações de coleção numa coluna de tabela ("`add()`, `remove()`,
  `clear()`, indexação"), **sem assinaturas, sem tipos de retorno e sem
  semântica**; e os métodos de `string` aparecem apenas espalhados pelos
  exemplos — `self.length` e `self.contains("@")`
  ([§2.2](../steerings/domainscript-spec-v7/02-type-system.md)),
  `self.uppercase()` ([§2.3](../steerings/domainscript-spec-v7/02-type-system.md)).
  Sem catálogo, não há como o front-end decidir se uma chamada é válida.
  Três problemas concretos, todos verificados com o `dsc` do HEAD:
  1. **`length` é property ou method?** A §2.2 escreve `self.length >= 2` (sem
     parênteses) e `self.length <= 120`, enquanto todo o resto da spec usa a
     forma de chamada (`self.contains("@")`, `self.uppercase()`,
     `availableTickets.count()` na
     [§19.2](../steerings/domainscript-spec-v7/19-transactions-sagas.md),
     `state.items.sum(...)` na
     [§22](../steerings/domainscript-spec-v7/22-smart-partial-loading.md)).
     As duas grafias não podem coexistir sem uma regra que diga quando cada
     uma vale.
  2. **A tabela da §2.4 não é implementável como está.** Falta o contrato de
     cada operação: `List.remove()` remove por índice ou por valor? `Map.get()`
     devolve o quê quando a chave não existe (a linguagem não tem `null` nem
     `Option` declarados)? `Set.add()` devolve `boolean` de "inseriu" ou nada?
     "indexação" de `List<T>` é `l[i]` — e o que acontece fora do range, erro
     de negócio ou de infra
     ([§23](../steerings/domainscript-spec-v7/23-error-classification.md))?
  3. **O que a linguagem promete sobre `string`.** `length`, `contains`,
     `uppercase` aparecem em exemplos; `lowercase`, `trim`, `startsWith`,
     `split`, `replace` não aparecem — mas nada diz que não existem. A
     [§27](../steerings/domainscript-spec-v7/27-evolving-features.md) lista
     "Funções utilitárias adicionais | Planejado", o que sugere que a lista
     atual **é** fechada; se for, precisa estar escrita.
  Estado da implementação, para contexto: o catálogo é de três entradas —
  `string.length()`, `string.uppercase()`, `AppendList.add()`
  ([`codegen/goname/types.go:85-92`](../../codegen/goname/types.go#L85-L92)).
  Toda a tabela da §2.4 é inemitível fora de `AppendList.add`, e `Valid {
  self.contains("@") }` da §2.2 falha. Pior: o front-end **não valida
  chamada de método nenhuma** — `value.frobnicate()` passa no `dsc check`
  com exit 0 e só morre no `dsc gen` ("método embutido desconhecido em
  corpo de VO"), o que viola o contrato "se compila, a arquitetura está
  correta"
  ([§1.1](../steerings/domainscript-spec-v7/01-overview.md)). Mesmo padrão
  vale para `events()`
  ([§4.5](../steerings/domainscript-spec-v7/04-domain-core.md)).
  Fechar o lado do código exige o catálogo em dois lugares — `types.Model` (para
  o `check` recusar) e `goname.builtinArity`/`GoBuiltinCall` (para o `gen`
  emitir) — mas nenhum dos dois pode ser escrito antes de a spec dizer o que
  existe.
- SOLVED: FALSE
