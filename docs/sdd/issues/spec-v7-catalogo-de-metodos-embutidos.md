# Spec v7: sem catálogo normativo de métodos embutidos por tipo
- SPEC: domainscript-spec-v7 (revisão da especificação)
- TASK: review-v7.md §A-7
- DESCRIPTION: A spec não tem, em lugar nenhum, a lista autoritativa dos
  métodos disponíveis por tipo. A §2.6 cataloga só **funções livres**
  (`now()`, `uuid()`, `random()`, `random_str()`, `signed_url()`); a §2.4 lista
  operações de coleção numa coluna de tabela ("`add()`, `remove()`, `clear()`,
  indexação"), **sem assinaturas, sem tipos de retorno e sem semântica**; e os
  métodos de `string` aparecem apenas espalhados pelos exemplos — `self.length`
  e `self.contains("@")` (§2.2), `self.uppercase()` (§2.3). Sem catálogo, não
  há como o front-end decidir se uma chamada é válida.
  Três problemas concretos, todos verificados com o `dsc` do HEAD:
  1. **`length` é property ou method?** A §2.2 escreve `self.length >= 2` (sem
     parênteses) e `self.length <= 120`, enquanto todo o resto da spec usa a
     forma de chamada (`self.contains("@")`, `self.uppercase()`,
     `availableTickets.count()` na §19.2, `state.items.sum(...)` na §22). As
     duas grafias não podem coexistir sem uma regra que diga quando cada uma
     vale.
  2. **A tabela da §2.4 não é implementável como está.** Falta o contrato de
     cada operação: `List.remove()` remove por índice ou por valor? `Map.get()`
     devolve o quê quando a chave não existe (a linguagem não tem `null` nem
     `Option` declarados)? `Set.add()` devolve `boolean` de "inseriu" ou nada?
     "indexação" de `List<T>` é `l[i]` — e o que acontece fora do range, erro
     de negócio ou de infra (§23)?
  3. **O que a linguagem promete sobre `string`.** `length`, `contains`,
     `uppercase` aparecem em exemplos; `lowercase`, `trim`, `startsWith`,
     `split`, `replace` não aparecem — mas nada diz que não existem. A §27
     lista "Funções utilitárias adicionais | Planejado", o que sugere que a
     lista atual **é** fechada; se for, precisa estar escrita.
  Estado da implementação, para contexto: o catálogo é de três entradas —
  `string.length()`, `string.uppercase()`, `AppendList.add()`
  (`codegen/goname/types.go:89`). Toda a tabela da §2.4 é inemitível fora de
  `AppendList.add`, e `Valid { self.contains("@") }` da §2.2 falha. Pior: o
  front-end **não valida chamada de método nenhuma** — `value.frobnicate()`
  passa no `dsc check` com exit 0 e só morre no `dsc gen` ("método embutido
  desconhecido em corpo de VO"), o que viola o contrato "se compila, a
  arquitetura está correta" (§1.1). Mesmo padrão vale para `events()` (§4.5).
  Fechar o lado do código exige o catálogo em dois lugares — `types.Model` (para
  o `check` recusar) e `goname.builtinArity`/`GoBuiltinCall` (para o `gen`
  emitir) — mas nenhum dos dois pode ser escrito antes de a spec dizer o que
  existe.
- SOLVED: FALSE
