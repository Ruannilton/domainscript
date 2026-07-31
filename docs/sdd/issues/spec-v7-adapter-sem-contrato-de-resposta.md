# Spec v7: nenhuma seção define o contrato de resposta de `Adapter`/`Notification` — pedido de revisão da especificação
- SPEC: [domainscript-spec-v7](../steerings/domainscript-spec-v7/README.md)
  (revisão da especificação)
- TASK: [correcoes-issues-6-8-12/M3.1](../specs/correcoes-issues-6-8-12/tasks/M3.1.md)
  (REQ-57.1/57.4)
- DESCRIPTION: `result = call PaymentRequest(...)` aparece em dois lugares da
  spec — [19-transactions-sagas](../steerings/domainscript-spec-v7/19-transactions-sagas.md) §19.2 (o exemplo canônico de Saga) e
  [09-notifications-adapters](../steerings/domainscript-spec-v7/09-notifications-adapters.md) §9.2 — e `mock PaymentRequest returns
  PaymentResult(status: PaymentStatus.Declined)` aparece em [24-testing.md](../steerings/domainscript-spec-v7/24-testing.md)
  §24.3. Em nenhum dos dois casos a spec declara o **tipo** do valor
  atribuído/mockado:
  - `Notification` (§9.1) só declara campos de ENTRADA (ex.: `Notification
    PaymentRequest { paymentId PaymentId, amount Money, method
    PaymentMethod }`) — nada nela descreve a forma de uma resposta.
  - `Adapter` Nível 1 — HTTP declarativo (§9.3) só tem `body { }`, o
    mapeamento de SAÍDA (notificação → requisição HTTP); não existe bloco
    simétrico para mapear a resposta HTTP de volta a um tipo DomainScript.
  - `Adapter` Nível 2 — FFI vinculado (§9.3) referencia `function
    "ProcessPayment"` como STRING solta, sem a assinatura `-> Tipo` que o
    `Foreign` genérico ([10-ffi.md](../steerings/domainscript-spec-v7/10-ffi.md) §10.2) tem (`pure function
    ComputeMerkleRoot(items List<bytes>) -> bytes`).
  - `PaymentResult`, o tipo usado no `mock ... returns PaymentResult(...)` do
    exemplo canônico de Test de Saga, não é declarado em NENHUM lugar da
    spec — `grep -rn "PaymentResult" ../steerings/domainscript-spec-v7/`
    só encontra as duas linhas do próprio exemplo (§19.2 e §24.3).
  - [24-testing.md](../steerings/domainscript-spec-v7/24-testing.md) §24.7 lista "Mock com retorno de tipo errado → Erro" como
    garantia semântica, o que pressupõe um tipo contra o qual checar — mas
    esse tipo nunca é declarado em lugar nenhum da gramática.

  **Evidência de código** (a implementação reflete fielmente essa lacuna, não
  a criou): `Call<Nome>` é emitido hoje só como `func Call<Nome>(ctx, n
  <Notif>) error` ([decl_io.go](../../../codegen/decl_io.go)) — sem canal de valor de retorno.
  [builtins.go:340](../../../codegen/lower/builtins.go#L340) recusa `result = call Adapter(...)`
  explicitamente:
  ```go
  case "call":
      return "", fmt.Errorf("codegen: QueryExpr.Op %q (chamada síncrona via Adapter/Notification) não é suportado — fora do escopo de G1a", n.Op)
  ```
  E [gentest.go:1330](../../../codegen/gentest.go#L1330) (`emitSagaMock`) hoje descarta o valor de `mock ...
  returns X` (`_ = <expr>`) — o mock nunca influencia o fluxo do passo
  seguinte.

  **O que a spec precisa decidir** (decisão de linguagem, não de
  implementação — não dá para adivinhar sem inventar gramática):
  1. De onde vem o tipo de resposta de um `call` — uma nova declaração
     `Adapter X returns <Tipo>`? Um bloco de mapeamento de resposta simétrico
     a `body { }` no Adapter Nível 1 (como o corpo HTTP vira o `Tipo`)? Uma
     assinatura `-> Tipo` na `function "Nome"` do Adapter Nível 2, igual ao
     `Foreign` genérico?
  2. A resposta é obrigatória para todo `Adapter mode sync`, ou opcional
     (alguns `call` não capturam valor, como `down { call RefundRequest(...)
     }` no próprio exemplo de §19.2)?
  3. Declarar formalmente o tipo `PaymentResult` do exemplo canônico (§19.2/
     §24.3), ou substituí-lo por um exemplo que use um tipo já declarado em
     outra parte da spec.

  Sem essa definição,
  [`M3.2`](../specs/correcoes-issues-6-8-12/tasks/M3.2.md) ("Implementar
  `result = call Adapter(...)`") e
  [`M3.3`](../specs/correcoes-issues-6-8-12/tasks/M3.3.md) ("`mock ...
  returns X` injeta X como retorno do stub") da spec `correcoes-issues-6-8-12`
  (REQ-57.2/57.3) ficam canceladas nesta revisão — ver
  [design.md](../specs/correcoes-issues-6-8-12/design.md)
  §4.5/§7.2 para o registro completo da decisão de delimitar em vez de
  adivinhar o contrato.
- SOLVED: TRUE
