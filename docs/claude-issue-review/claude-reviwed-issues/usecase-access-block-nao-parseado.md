CODIGO: usecase-access-block-nao-parseado
CATEGORIA: Dependente de decisão do desenvolvedor

## Resumo da issue

O exemplo canônico de acesso cross-tenant no fim da
[[docs/sdd/steerings/domainscript-spec-v7/14-multi-tenancy|§14 (Multi-Tenancy)]]
declara um bloco `access { requires ... }` dentro de um `UseCase`. O parser só
modela `access` em `Aggregate`; `ast.UseCaseDecl` não tem esse campo e
`parseUseCase` rejeita o membro com erro de sintaxe. A sintaxe é inequívoca e
implementável, mas a semântica por trás dela (o que `access` de UseCase
proíbe, quem é `caller`) nunca foi normatizada, e isso bloqueia as fases de
resolver/sema/codegen.

## Evidencias

- Rodando o trecho da
  [[docs/sdd/steerings/domainscript-spec-v7/14-multi-tenancy|§14]] verbatim: `3:5: error: membro de UseCase inesperado:
  IDENT` e mais 6 erros em cascata até o `}` de fechamento — confirmado contra
  o `dsc` de HEAD. Sem o bloco `access`, o mesmo UseCase fecha limpo.
- `ast.UseCaseDecl` ([[docs/sdd/issues/usecase-access-block-nao-parseado]] cita
  `ast/decl.go#L199-L212`) só tem `Timeout`/`Idempotency`/`Tenancy`/`Execute`;
  `parseUseCase` (`parser/parse_decl.go#L784-L834`) cai no `default` em
  `parse_decl.go#L827`. `Access []*ast.AccessRule` só existe em `AggregateDecl`
  e `ViewDecl`.
- Achado novo da análise: `parseAccessBlock` (`parser/parse_decl.go#L906-L923`)
  lê nome primeiro e trata `requires` como opcional — a forma nua da §14,
  escrita dentro de um **Aggregate**, é aceita em silêncio como uma regra
  chamada `"requires"`, arquivando a condição sob o nome errado sem erro de
  sintaxe algum.
- Três blocos de "nota do desenvolvedor" já embutidos na própria issue
  respondem parte dos pontos em aberto:
  - Sobre `caller.id` em `access` de UseCase (contradição
    [[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|§4.3.1]]): "realmente
    esta declaração é contraditória, caller.id deve ser acessível (apenas
    leitura) em todo o escopo de execução da request".
  - Sobre closed-by-default de UseCase: "UseCase sem access é open".
  - Sobre a definição normativa de `caller`: "o caller é uma estrutura montada
    automaticamente por um miniframework interno, seu papel é trazer
    informações a respeito de quem fez a requisição atual, como tokens, RBAC
    access, etc. Realmente é preciso desenvolver melhor a especificação desta
    feature."

## Impacto no projeto

Enquanto a sintaxe não existe, não há forma textual de expressar a exigência
de role que a própria
[[docs/sdd/steerings/domainscript-spec-v7/14-multi-tenancy|§14]] promete junto
do opt-in cross-tenant ("`tenancy:
cross_tenant` + role privilegiada + auditoria automática + warning") — hoje só
dá para declarar o opt-in, nunca a proteção. A regra de auditoria
(`checkCrossTenantAudit`) já dispara sozinha, sem nenhum guard de acesso
correspondente possível de escrever.

## Soluçoes possíveis
### Solucão 1
Implementar as três fases (parser, resolver/sema, codegen) exatamente como a
análise propõe: `Access []*ast.AccessRule` novo em `UseCaseDecl` com uma
`parseRequiresBlock()` dedicada (não reaproveitar `parseAccessBlock`, que
herdaria o bug do nome "requires"); ao mesmo tempo, corrigir `parseAccessBlock`
para exigir nome de Handle no Aggregate, fechando a aceitação silenciosa
encontrada. Resolver/sema resolvem a condição só com `caller` em escopo (nem
`self`, nem `cmd`, até a spec decidir o contrário). Codegen emite o guard
fail-closed com `runtime.ErrForbidden`, reusando `lowerAccessCondition` do
Aggregate — reaproveita `and`/`or`, comparação de VO e `lowerCallerHasRole`
já existentes. Como nenhuma fixture de `testdata/projects/` usa `access` em
UseCase, nenhum golden existente muda.
### Solução 2
Parsear e resolver a sintaxe, mas não gerar o guard (fases 1-2 apenas). A
própria análise descarta essa rota citando o precedente já registrado em
[[docs/sdd/issues/visibility-de-view-nao-implementado]] e em
[[docs/sdd/steerings/review-v7|review-v7.md §B-4]]: controle de segurança
aceito e silenciosamente inerte é pior que recusar. Se a fase 3 não
puder acompanhar por algum motivo, o correto é erro de geração explícito ao
encontrar o bloco, não silêncio.

## O que precisa ser resolvido antes

- `tenancy: cross_tenant` sem `access` é erro de compilação, warning, ou nada?
  (a nota do desenvolvedor resolveu "UseCase sem access, sozinho, é open" —
  mas não resolveu o caso combinado com `cross_tenant`, que é justamente o que
  a [[docs/sdd/steerings/domainscript-spec-v7/14-multi-tenancy|§14]] promete
  como par obrigatório.)
- Mais de uma linha `requires` no mesmo bloco `access` de UseCase é legal, e
  com que composição (`and` implícito, ou outra)?
- Definição normativa completa de `caller`: tipo, membros, assinatura de
  `hasRole`, semântica de `authenticated`, e sua posição (hoje ausente) no
  catálogo fechado da
  [[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.8]] — a nota do
  desenvolvedor já descreve o papel
  conceitual (estrutura montada por um miniframework interno, carregando
  tokens/RBAC), mas confirma explicitamente que falta escrever a
  especificação. Sem isso, não há como fazer o type-check da condição de
  `access`/`visibility` de forma conformante.
- Recomendação da própria issue: abrir revisão de spec própria para o ponto de
  `caller`
  ([[docs/sdd/steerings/domainscript-spec-v7/02-type-system|§2.8]] +
  [[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|§4.3.1]] +
  [[docs/sdd/steerings/domainscript-spec-v7/11-interface|§11]]), separada da
  tarefa de sintaxe/parser desta
  issue, que já pode avançar de forma independente das duas primeiras
  perguntas.
