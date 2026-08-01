CODIGO: divergencias-menores-do-spec-em-evolucao
CATEGORIA: Dependente de decisão do desenvolvedor

Issue original: [[docs/sdd/issues/divergencias-menores-do-spec-em-evolucao]]

## Resumo da issue

Um item guarda-chuva com três divergências menores, a maioria já marcada pela própria spec como "em evolução" ([[docs/sdd/steerings/domainscript-spec-v7/27-evolving-features|§25]]/[[docs/sdd/steerings/domainscript-spec-v7/27-evolving-features|§27]]): (a) redação GDPR — o placeholder tipado existe (E4.3), mas o gatilho que decide quando redigir um campo não é definido; (b) cobertura semântica de erro ([[docs/sdd/steerings/domainscript-spec-v7/24-testing|§22.7]]) — o warning existe mas na granularidade "por Handle", não "por ramo de regra de negócio"; (c) itens declarados como planejados/a definir pelo próprio spec (avg/min/max/group by, aritmética estendida, marshalling FFI detalhado).

## Evidencias

- (a) GDPR: placeholder tipado implementado (E4.3); [[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]] §4.4 marca o gatilho de redação como "em evolução" — nenhuma sintaxe definida hoje.
- (b) `sema/rules_warnings.go` (`checkHandleErrorCoverage`, REQ-5.22) emite o warning "Handle sem cenário de erro testado" na granularidade por Handle, não por ramo (`ensure ... else Error`).
- (c) §25 (numeração v6, hoje espalhada em [[docs/sdd/steerings/domainscript-spec-v7/27-evolving-features|27-evolving-features.md]] e afins) marca avg/min/max/group by, aritmética estendida e marshalling FFI detalhado como planejado/a definir — sem sintaxe normativa hoje.
- Reclassificação registrada no próprio arquivo: itens (a) e (c) passaram de "dívida de codegen" para "aguardando definição no spec da linguagem (exigem sintaxe nova não definida; não há ação de codegen pendente)"; item (b) permanece uma análise de raiz em aberto (task L3.1: se `checkHandleErrorCoverage` consegue cruzar ramos com cenários testados, refina o warning; senão, mantém a granularidade atual e vira ciclo de sema dedicado).

## Impacto no projeto

Nenhum destes três itens bloqueia um fluxo hoje funcional — todos são melhorias sinalizadas pelo próprio spec como incrementais. O risco é de baixa prioridade: (a) sem gatilho de redação definido, dados que deveriam ser redigidos por LGPD/GDPR continuam sendo emitidos sem redação alguma (silencioso, mas o spec já avisa que o gatilho não existe); (b) o warning de cobertura de erro é menos útil do que poderia ser, mas não é falso; (c) funcionalidades de agregação/aritmética/FFI simplesmente não existem ainda.

## Soluçoes possíveis

### Solucão 1

Tratar (b) como trabalho de análise já encaminhado (task L3.1 do Marco M) — sem decisão de linguagem pendente, resolve-se lendo `checkHandleErrorCoverage` e decidindo, por engenharia, se dá para refinar por ramo ou não. Tratar (a) e (c) como itens que aguardam o spec definir sintaxe nova antes de qualquer código — não são candidatos a implementação enquanto isso não acontecer.

### Solução 2

Não há segunda rota concorrente registrada para (a)/(c): a própria issue já reclassificou esses dois itens como "aguardando definição no spec da linguagem", sem propor uma sintaxe candidata. Inventar uma sintaxe agora (por exemplo, decidir sozinho o gatilho de redação GDPR ou a forma de `group by`) contrariaria a regra do projeto de nunca implementar o que a spec não descreve.

## O que precisa ser resolvido antes

1. Qual é o gatilho de redação GDPR ([[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|§4.4]])? Um atributo no campo (`redacted`? algo ligado a `access`/`visibility`?), uma anotação separada, ou uma decisão condicionada ao caller como em `visibility`? A spec ainda marca isso como "em evolução" sem propor sintaxe.
2. Qual sintaxe normativa para agregações (avg/min/max/group by), aritmética estendida e marshalling FFI detalhado ([[docs/sdd/steerings/domainscript-spec-v7/27-evolving-features|§25]]/[[docs/sdd/steerings/domainscript-spec-v7/27-evolving-features|§27]], "em evolução")? Sem essas definições não há o que implementar.

O item (b) (cobertura semântica por ramo, [[docs/sdd/steerings/domainscript-spec-v7/24-testing|§22.7]]) não depende de decisão de spec — é uma análise de engenharia já encaminhada pela task L3.1 do Marco M e pode prosseguir independentemente das duas perguntas acima.
