# Spec v7: `sum()` sobre ValueObject e `focus()` sem semântica de ausência (§22 contra o novo §2.8)
- SPEC: [domainscript-spec-v7](../steerings/domainscript-spec-v7/README.md)
  (revisão da especificação)
- TASK: — (achado durante a revisão que fechou as cinco issues de spec v7;
  ver [§2.8](../steerings/domainscript-spec-v7/02-type-system.md))
- DESCRIPTION: A [§2.8](../steerings/domainscript-spec-v7/02-type-system.md)
  (catálogo normativo de métodos embutidos, escrito nesta revisão) fechou o
  contrato de `sum` e o conjunto de operações de coleção. Duas linhas da
  [§22](../steerings/domainscript-spec-v7/22-smart-partial-loading.md) — que
  é o exemplo canônico de Smart Partial Loading, e tem só seis linhas — ficaram
  contra esse contrato. Nenhuma das duas foi corrigida ao vento porque ambas
  exigem decisão de linguagem, não ajuste de exemplo.

  1. **`state.items.sum(i => i.price)` projeta um ValueObject.** A §2.8
     restringe a projeção de `sum(f)` a `integer`/`decimal` — o argumento
     escrito lá é que a coleção vazia precisa de um zero, e um VO arbitrário
     não tem um. Mas `price` é `Money` em todo o resto da spec, `Money` declara
     `Operator +` ([§2.2](../steerings/domainscript-spec-v7/02-type-system.md)),
     e somar dinheiro é exatamente o caso de uso que a §22 ilustra. As três
     saídas: (a) manter `sum` numérico e reescrever a §22 para projetar
     `i.price.amount` — barato, mas devolve `decimal` e joga a moeda fora,
     que é precisamente o que o VO `Money` existe para impedir; (b) estender
     `sum` a VOs que declarem `Operator +`, e então **decidir o valor de
     coleção vazia** (erro em runtime? um `Zero`/`identity` declarável no VO?
     proibir `sum` sobre coleção potencialmente vazia?); (c) introduzir uma
     forma que exija semente explícita (`sum(0, f)` / `fold`). A mesma linha
     aparece em
     [`docs/examples/03-aplicacao-e-leitura/read.ds:125`](../../examples/03-aplicacao-e-leitura/read.ds)
     (`sum(...) < Price(...)`), o que confirma que a intenção sempre foi somar
     VO monetário, não `decimal`.
     Nota secundária da mesma linha: `< 10000` compara o resultado com um
     literal numérico nu. Se (b) ou (c) vencer, o literal precisa virar
     `Money(...)`; se (a) vencer, ele já é legal.

  2. **`state.items.focus(itemId)` não tem semântica de ausência.**
     `focus` só aparece na §22, sem assinatura e sem contrato. A §2.8 aponta
     para lá e assume que a ausência é tratada por `ensure ... exists`, mas a
     §22 nunca diz isso — e a §2.8 fechou a porta pela qual isso normalmente
     se resolveria: não há indexação, não há `null`, não há `Option`, e
     `Map.get` foi definido como total (`get(k, default)`) justamente para
     evitar o assunto. `focus` é hoje a única operação da linguagem que
     seleciona um elemento por identidade dentro de uma coleção, e portanto a
     única que pode falhar em encontrá-lo. A spec precisa dizer: assinatura,
     tipo de retorno, o que acontece quando não há elemento (`ensure ... exists`
     como `load`? erro de negócio declarável? erro de infra?), e se `focus` vale
     em qualquer coleção ou só em `AppendList<T>` dentro de `state`.
- SOLVED: FALSE
