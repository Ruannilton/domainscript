CODIGO: spec-v7-adapter-sem-contrato-de-resposta
CATEGORIA: Correção de código
Issue original: [[docs/sdd/issues/spec-v7-adapter-sem-contrato-de-resposta]]

## Resumo da issue

A spec v6 usava `result = call PaymentRequest(...)` e `mock PaymentRequest returns PaymentResult(...)` em vários lugares, mas nunca dizia de onde vinha o **tipo** da resposta: `Notification` só declarava campos de entrada, o `Adapter` Nível 1 (HTTP) só tinha `body { }` (saída), o Nível 2 (FFI) referenciava a função como string solta sem assinatura de retorno, e `PaymentResult` nunca era declarado em lugar nenhum. Era uma lacuna de gramática — implementar `call`/`mock` com retorno exigiria inventar sintaxe sem a spec ter decidido.

## Evidencias

- `codegen/lower/builtins.go:340` recusa `result = call Adapter(...)` explicitamente: `"chamada síncrona via Adapter/Notification não é suportada — fora do escopo de G1a"`.
- `codegen/gentest.go:1330` (`emitSagaMock`) descarta o valor de `mock ... returns X` (`_ = <expr>`) — nunca influencia o passo seguinte.
- A issue foi registrada com `SOLVED: TRUE` e cancelou explicitamente as tasks M3.2/M3.3 do ciclo `correcoes-issues-6-8-12` (REQ-57.2/57.3) até a spec decidir.
- A nota do desenvolvedor em `[[docs/notes/Issues/spec-v7-adapter-sem-contrato-de-resposta|docs/notes/Issues/spec-v7-adapter-sem-contrato-de-resposta]]` propõe exatamente a sintaxe `-> Tipo` na declaração da `Notification` (`Notification PaymentRequest { ... } -> PaymentRequestResponse`), com o tipo de resposta sendo um `ValueObject`.
- `[[docs/sdd/steerings/domainscript-spec-v7/09-notifications-adapters|09-notifications-adapters.md]]` §9.4 já escreve essa decisão como texto normativo completo: §9.4.1 (`-> T` na Notification, opcional), §9.4.2 (`notify`/`call` determinados pelo `mode` do Adapter), §9.4.3 (bloco `response { }` simétrico a `body { }` no Nível 1), §9.4.4 (assinatura `function "Nome" -> T throws E` derivada no Nível 2), §9.4.5 (declaração canônica de `PaymentStatus`/`PaymentResult`), §9.4.6 (semântica de `mock X returns V` como shape parcial).

## Impacto no projeto

Enquanto o código não acompanhar a spec revisada, `call` síncrono com captura de valor continua indisponível — bloqueando qualquer Saga ou Policy que precise decidir o próximo passo a partir da resposta de um Adapter (o próprio exemplo canônico de Saga da spec, `ProcessPayment`, depende disso) e qualquer teste que use `mock ... returns`.

## Soluçoes possíveis

### Solucão 1

Implementar a spec já escrita em `[[docs/sdd/steerings/domainscript-spec-v7/09-notifications-adapters|09-notifications-adapters.md]]` §9.4, ponta a ponta: parser/checker para `-> T` na declaração de `Notification` e `function "Nome" -> T throws E` no Adapter Nível 2, bloco `response { }` no Nível 1, tipagem de `r = call X(...)`, e o codegen correspondente (`Call<Nome>` passa a devolver `(T, error)`, `emitSagaMock` passa a usar o valor mockado em vez de descartá-lo). É trabalho de implementação, não de decisão — a gramática, os erros de compilação e os exemplos já estão todos normatizados.

### Solução 2

Não há uma segunda rota razoável aqui: a própria issue já cancelou as duas alternativas de "adivinhar a forma sem a spec decidir" (usar `Adapter X returns T`, ou inferir do bloco `body`) justamente porque exigiam inventar gramática. A spec revisada escolheu a rota "resposta pertence à assinatura da `Notification`, nunca ao Adapter" (`[[docs/sdd/steerings/domainscript-spec-v7/09-notifications-adapters|09-notifications-adapters.md]]` §9.4, motivo explícito: "o domínio depende do contrato, e trocar o transporte que o cumpre não toca uma linha de domínio") — não há indício de um caminho concorrente ainda em aberto.

## O que precisa ser resolvido antes

Nenhuma — a spec já é clara: `[[docs/sdd/steerings/domainscript-spec-v7/09-notifications-adapters|09-notifications-adapters.md]]` §9.4 cobre declaração, verbo, os dois níveis de Adapter, versionamento e mocks em teste, com tabelas de erro completas e um exemplo end-to-end. O trabalho restante é puramente de implementação (front-end + codegen) das tasks M3.2/M3.3, hoje canceladas no ciclo `correcoes-issues-6-8-12` e que precisam ser reabertas contra o texto revisado.
