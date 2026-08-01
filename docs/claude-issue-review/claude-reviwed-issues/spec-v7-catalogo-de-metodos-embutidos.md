CODIGO: spec-v7-catalogo-de-metodos-embutidos
CATEGORIA: Correção de código
Issue original: [[docs/sdd/issues/spec-v7-catalogo-de-metodos-embutidos]]

## Resumo da issue

A spec não tinha, em nenhum lugar, a lista autoritativa de métodos disponíveis por tipo. Ficava sem resposta se `self.length` era propriedade ou método, o que `List.remove()`/`Map.get()` faziam exatamente, e quais métodos de `string` a linguagem prometia — sem catálogo, o `check` não tinha como recusar uma chamada inválida antes da geração, contrariando o próprio princípio "se compila, a arquitetura está correta". O código de hoje só reconhece três entradas (`string.length()`, `string.uppercase()`, `AppendList.add()`), e o front-end não valida chamada de método nenhuma — só falha tarde, no `dsc gen`.

## Evidencias

- `codegen/goname/types.go:85-92` — o catálogo implementado hoje é de três entradas.
- `Valid { self.contains("@") }` da antiga §2.2 (hoje `[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|02-type-system.md]]` §2.2) falhava porque `contains` não estava no catálogo emitido.
- `value.frobnicate()` passava no `dsc check` com exit 0 e só morria no `dsc gen` com "método embutido desconhecido em corpo de VO" — violação do contrato do `[[docs/sdd/steerings/domainscript-spec-v7/01-overview|01-overview.md]]` §1.1 citado na issue.
- A nota do desenvolvedor em `[[docs/notes/Issues/spec-v7-catalogo-de-metodos-embutidos|docs/notes/Issues/spec-v7-catalogo-de-metodos-embutidos]]` decide: primitivos ganham suporte a todos os métodos/funções da contraparte em Go, e método de aridade 0 pode ser chamado sem parênteses (`self.length` ≡ `self.length()`).
- `[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|02-type-system.md]]` §2.8 já escreve esse catálogo por completo: §2.8.1 (método/campo/operador não se sobrepõem), §2.8.2 (parênteses opcionais, exceção deliberada e explícita a "Uma Forma Canônica"), §2.8.3 (invariantes: totalidade, pureza, imutabilidade), §2.8.4-§2.8.9 (assinaturas completas de `string`/`integer`/`decimal`/`boolean`/`datetime`/`bytes`/coleções, inclusive por que indexação não existe), §2.8.10 (onde cada chamada é legal) e §2.8.11 (lista explícita do que não existe, com a forma canônica equivalente).

## Impacto no projeto

Sem o catálogo implementado, todo o corpo de exemplos da spec que usa `self.contains`, `self.length`, `.sum(...)`, `.count()` etc. continua não compilando contra o `dsc` real, e o front-end segue violando sua própria garantia central (erro de método deveria morrer no `check`, não no `gen`).

## Soluçoes possíveis

### Solucão 1

Implementar o catálogo de `[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|02-type-system.md]]` §2.8 nos dois lugares que a issue já identifica: `types.Model` (para o `check` recusar chamada inválida antes da geração) e `goname.builtinArity`/`GoBuiltinCall` (para o `gen` emitir a chamada Go correspondente). Inclui o açúcar de parênteses opcionais (`[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|02-type-system.md]]` §2.8.2) como resolução léxica antes de qualquer análise semântica, como a spec exige.

### Solução 2

Não há uma segunda rota razoável: a issue original já descartava tacitamente "manter a lista atual e documentar as exceções" (deixaria `Valid { self.contains("@") }`, exemplo canônico da própria spec, sem compilar) e "inferir o catálogo a partir do Go" sem normatizá-lo na spec (repetiria o problema de hoje — o front-end tarda a recusar). A spec revisada fechou a lista explicitamente (`[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|02-type-system.md]]` §2.8.3: "o catálogo é fechado... a seção não introduz nenhum warning"), então o único caminho é implementar exatamente o que está escrito.

## O que precisa ser resolvido antes

Nenhuma — a spec já é clara: `[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|02-type-system.md]]` §2.8 é hoje a autoridade completa, com assinaturas, tipos de retorno, semântica e contextos permitidos para cada método, e responde às três perguntas concretas da issue original (parênteses opcionais, contrato de `List`/`Map`/`Set`, superfície fechada de `string`). O trabalho restante é implementação pura em `types.Model` e `codegen/goname`.
