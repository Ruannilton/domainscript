CODIGO: visibility-de-view-nao-implementado
CATEGORIA: Dependente de decisão do desenvolvedor

Issue original: [[docs/sdd/issues/visibility-de-view-nao-implementado]]

## Resumo da issue

O bloco `visibility` de uma View (spec [[docs/sdd/steerings/domainscript-spec-v7/06-read-side|§6.2]], campos visíveis condicionalmente por caller) é parseado e guardado no AST, mas nenhuma fase seguinte — resolução, checagem, codegen — o consome. O programa compila normalmente, o bloco é aceito e silenciosamente ignorado: hoje o transpilador emite o struct completo de qualquer View, serializando campos que deveriam ser restritos para qualquer caller. É a lacuna descrita como mais arriscada do inventário do projeto por ser uma falha de segurança silenciosa.

## Evidencias

- `ast/decl.go:229-235` (`ViewDecl.Visibility []*AccessRule`) preenchido por `parser/parse_decl.go:683-713`; nenhum arquivo de `codegen/`, `sema/`, `resolver/` ou `types/` lê o campo.
- `codegen/decl_view.go:34-38` admite isso por escrito; `emitViewDecl` (`:61-88`) constrói o struct sem olhar `decl.Visibility`.
- `resolver/resolve_body.go:77-127` (`resolveDeclBodies`) não tem caso `*ast.ViewDecl`; `sema/rules_typecheck.go:40-91` cobre `access` de Aggregate mas não View.
- Prova empírica citada na issue: cópia externa do fixture `wallet` com um bloco `visibility` deliberadamente absurdo (`caller.id == self.campoQueNaoExiste`, receptor inexistente) — `dsc check` sai 0, sem nenhum diagnóstico, e `dsc gen` emite o struct completo, `balance` incluso, para qualquer caller.
- [[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]] §4.2.3/§4.3.1 pressupõem que o compilador enxerga dentro do bloco `visibility` (listam-no entre os contextos onde ler o envelope do evento é erro, e fixam `caller.id` como erro fora de `access`/`visibility`) — hoje ele não enxerga.

## Impacto no projeto

Qualquer programa que declare `visibility` para restringir um campo sensível (ex.: saldo, dado pessoal) tem essa restrição completamente ignorada na saída gerada — o campo vaza para todo caller, sem aviso do compilador. Único exemplo que exercita a feature hoje, `testdata/projects/pizzeria/sales/read.ds` (`OrderVW`), já está bloqueado por outros defeitos de codegen (ver [[docs/sdd/issues/pizzeria-bloqueado-por-multiplos-defeitos-de-codegen|pizzeria-bloqueado-por-multiplos-defeitos-de-codegen]]), então o gap nunca foi provado ponta a ponta.

## Soluçoes possíveis

### Solucão 1

Mitigação imediata sem inventar diagnóstico de linguagem: (1) `codegen` passa a falhar com erro de geração — não `diag.Diagnostic` — quando um `ViewDecl` tem `Visibility` não vazio, seguindo o mesmo padrão já usado em `codegen/http.go:180/197/890` para outras lacunas ("falha explícita em vez de gerar um extractor/código silenciosamente vazio"); (2) resolver nomes e checar acesso a membro dentro das condições de `visibility`, exatamente como já se faz para `access` — não é regra nova, é aplicar regras existentes a um bloco hoje invisível ao compilador. Fecha o risco de segurança sem esperar decisão de semântica de serialização.

### Solução 2

Implementação completa (`VisibleFor(caller) runtime.FieldSet` por View, filtrando na borda HTTP depois do cache — nunca dentro da Query, para não vazar dado filtrado-por-caller no cache compartilhado). Só é executável depois que os cinco bloqueios abaixo forem decididos; a mitigação da Solução 1 é o passo que não depende deles.

## O que precisa ser resolvido antes

1. `visibility` exige `View ... From T`? A [[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|§4.3.1]] fala em "`self` é a instância projetada" pressupondo um `T`; a [[docs/sdd/steerings/domainscript-spec-v7/06-read-side|§6.2]] só exemplifica com `From`. Uma View sem `From` (o caso do pizzeria, único exercitado pelos fixtures) não tem `T` — a spec precisa dizer se isso é erro ou se `self` passa a denotar a própria View.
2. `caller.id` comparável contra o quê, hoje? A [[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|§4.3.1]] fixa `caller.id : CallerId` comparável só contra `ref T`; nem `CallerId`, nem `ref T`, nem identidade implícita existem na implementação atual.
3. O resultado cacheado de uma Query ([[docs/sdd/steerings/domainscript-spec-v7/16-cache|§15]]) é por caller, ou o cache guarda sempre o valor completo e a filtragem é só de serialização? A spec precisa dizer isso explicitamente, ou a próxima implementação pode escolher o lado errado e vazar dado.
4. Como "campo omitido da serialização" se traduz na borda gRPC/proto3, que não tem noção de "campo ausente" para escalar?
5. O que `visibility { xpto requires ... }` com `xpto` inexistente na View deve ser: erro de geração (superfície do transpilador) ou diagnóstico novo do front-end (regra de linguagem)?

## Nota do desenvolvedor
1. o self na view refere-se a instancia do agregado que gerou a view, logo usar a key "self" em uma view que não foi declarada como From T é um erro
2. 
3. A filtragem é apenas na serialização
4. podemos definir os campos que possuem regras de visualização como opcionais
5. erro