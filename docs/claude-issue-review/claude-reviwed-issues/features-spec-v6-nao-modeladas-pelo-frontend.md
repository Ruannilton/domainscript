CODIGO: features-spec-v6-nao-modeladas-pelo-frontend
CATEGORIA: Ajuste de especificação
Issue original: [[docs/sdd/issues/features-spec-v6-nao-modeladas-pelo-frontend]]

## Resumo da issue

Quatro features do spec v6 nunca foram modeladas pelo front-end — não são gaps só do codegen, o parser/resolver/checker sequer as reconhecem, então fechar qualquer uma exige um ciclo de front-end inteiro antes de chegar a codegen: (a) exposição TCP/UDP direta em `interface.ds`, (b) receptor `tenant.*` legível de dentro de um corpo de domínio (`tenant.id`/`tenant.tier`/`tenant.exists`), (c) o built-in `provision tenant(id)`, e (d) acesso nativo a `events()` em Aggregates. A issue já classificava as quatro como "abrir só quando houver demanda real" — não são bugs, são features nunca priorizadas.

## Evidencias

- (a) `interface.ds` hoje só modela HTTP e gRPC — TCP/UDP não tem forma nenhuma na gramática.
- (b) A tenancy `row_level` já funciona no runtime (filtro, `cross_tenant`, fail-closed 400), mas o domínio não tem como *ler* o tenant corrente de dentro de um `Handle`/`UseCase` — a spec (`[[docs/sdd/steerings/domainscript-spec-v7/14-multi-tenancy|14-multi-tenancy.md]]`, antiga §13.2) descreve o receptor mas o front-end não o reconhece.
- (c) `provision tenant(id)` (antiga §13.4, hoje `[[docs/sdd/steerings/domainscript-spec-v7/14-multi-tenancy|14-multi-tenancy.md]]`) não é expressável — sem ela o fluxo de provisionamento de tenant da própria spec não compila.
- (d) `events()` nativo em Aggregates (`[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.3, "Event stream nativo") já aparece nos exemplos da spec (`wallet.events(from: 100, to: 200)`), mas o front-end não o implementa.
- Duas notas do desenvolvedor estão embutidas diretamente na issue original: "não daremos suporte a exposição TCP/UDP direta no momento" e "tenancy deve ser definido por agregado, considerando que o agregado define toda a borda de persistência."

## Impacto no projeto

Cada uma dessas quatro é, segundo a própria issue, "das mais caras do inventário" — atravessa o pipeline inteiro (parser → resolver → sema → types → codegen). Sem uma decisão registrada, cada uma continua sendo um item de backlog indefinido em vez de um ciclo de spec planejável; e a nota sobre tenancy, em particular, sinaliza uma correção de rumo (identidade por agregado, não por sessão/request genérica) que ainda não está escrita em `[[docs/sdd/steerings/domainscript-spec-v7/14-multi-tenancy|14-multi-tenancy.md]]`.

## Soluçoes possíveis

### Solucão 1

Registrar as decisões já tomadas como texto normativo, item a item: (a) declarar TCP/UDP explicitamente fora de escopo em `[[docs/sdd/steerings/domainscript-spec-v7/11-interface|11-interface.md]]` (ou na lista de `[[docs/sdd/steerings/domainscript-spec-v7/27-evolving-features|27-evolving-features.md]]` §27, "Planejado" ou removido da lista de features futuras, conforme o dono do ciclo preferir) — encerra o item sem exigir código. (b)/(c) redesenhar o receptor `tenant.*` e o `provision tenant(id)` em `[[docs/sdd/steerings/domainscript-spec-v7/14-multi-tenancy|14-multi-tenancy.md]]` around o princípio "tenancy definida por agregado" que a nota estabelece — isso é mais que uma correção de redação: muda a forma como o tenant é resolvido (por agregado, não por sessão), então precisa de texto normativo novo, não só uma frase. (d) `events()` nativo permanece sem decisão de priorização — a spec já o exemplifica (`[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.3) mas a issue não traz nota alguma sobre quando abrir esse ciclo.

### Solução 2

Tratar as quatro como um único ciclo de front-end assim que houver demanda real, sem editar a spec agora — é a leitura literal do texto original da issue ("abrir só quando houver demanda real"). Descartada como resposta única porque duas das notas do desenvolvedor já são decisões de produto tomadas *agora* (não "quando houver demanda"): manter (a) sem registrar a exclusão na spec deixa a ambiguidade voltar a aparecer na próxima revisão, e (b)/(c) sem o texto novo de "tenancy por agregado" deixam abertas duas leituras concorrentes (tenant como contexto de sessão vs. como propriedade do agregado) que a nota já resolveu.

## O que precisa ser resolvido antes

Para (a): nenhuma pendência de decisão — a nota do desenvolvedor já fecha o item ("não daremos suporte... no momento"); falta só registrar isso em `[[docs/sdd/steerings/domainscript-spec-v7/11-interface|11-interface.md]]`.

Para (b)/(c): a direção já foi decidida ("tenancy deve ser definido por agregado, considerando que o agregado define toda a borda de persistência"), mas falta a decisão de detalhe — como exatamente o compilador liga um Aggregate ao seu tenant (convenção de nome? bloco de configuração, como `identity`? campo implícito, como `self.id`?) — antes de virar texto normativo em `[[docs/sdd/steerings/domainscript-spec-v7/14-multi-tenancy|14-multi-tenancy.md]]`.

Para (d): nenhuma nota do desenvolvedor cobre este item. Continua sem decisão de priorização — abrir ou não um ciclo de spec para `events()` nativo é uma chamada de produto ainda pendente, não uma ambiguidade técnica (a forma de uso já está exemplificada em `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.3).
