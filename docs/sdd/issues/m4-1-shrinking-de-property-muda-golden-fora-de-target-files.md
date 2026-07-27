# M4.1: shrinking do contra-exemplo de `property` muda `tests_wallet.go.golden`/`gentest_test.go`, ambos fora de `target_files`
- SPEC: [correcoes-issues-6-8-12](../specs/correcoes-issues-6-8-12/requirements.md)
- TASK: [M4.1](../specs/correcoes-issues-6-8-12/tasks/M4.1.md)
- DESCRIPTION: [M4.1.md](../specs/correcoes-issues-6-8-12/tasks/M4.1.md) restringe `target_files` a
  [`codegen/gentest_property.go`](../../../codegen/gentest_property.go) e
  `codegen/gentest_property_test.go` (ainda não existe — arquivo a criar por
  esta task).
  Implementar REQ-58 (shrinking determinístico do contra-exemplo de
  `property`, §22.5) como a task descreve — Step 1 (encolher a sequência por
  remoção/bissecção, re-executando cada candidata), Step 2 (reusar o mesmo
  seed de state), Step 4 ("reportar a sequência mínima... indicando que
  houve shrinking — quantos passos foram eliminados") — muda
  necessariamente o texto Go **estático** que `emitAggregatePropertyDecls`/
  `emitPropertyBody`/`emitPropertyHelpers` emitem para **toda** `property`
  já existente, não só para as futuras: o campo novo em `dsPropStep` (para
  guardar os eventos de cada passo e permitir re-aplicá-los num replay), a
  clausura de replay, a chamada a um shrinker, e a mensagem de
  `t.Fatalf` (que passa a reportar quantos passos foram eliminados) fazem
  parte do código gerado **independentemente** de a property passar ou
  falhar em tempo de execução — o golden compara o texto fonte, não o
  comportamento em runtime.

  [`testdata/projects/wallet/wallet.test.ds`](../../../testdata/projects/wallet/wallet.test.ds)
  já declara uma `property` (linha 52, `"saldo nunca fica negativo"`,
  `Test Wallet`), e
  [`codegen/testdata/tests_wallet.go.golden`](../../../codegen/testdata/tests_wallet.go.golden)
  já contém a emissão completa dessa property: o `type dsPropStep struct`
  (linhas 12-19), `TestWallet_SaldoNuncaFicaNegativo` (linha 144 em diante),
  com o `trail` atual (linhas 202-261, ex. `trail = append(trail,
  dsPropStep{Handle: "Deposit", Args: []any{v13, v16}, Err: err})`). Qualquer
  implementação fiel de REQ-58 muda esse trecho.

  [`codegen/gentest_test.go`](../../../codegen/gentest_test.go):`TestEmitTestsWalletGolden`
  (linha 87 hoje; 122 à época do registro) chama `gentest.Golden(t,
  "testdata/tests_wallet.go.golden", got)` — comparação **byte a byte**
  contra esse arquivo. Nem [gentest_test.go](../../../codegen/gentest_test.go) nem
  `codegen/testdata/tests_wallet.go.golden` estão em `target_files` de
  M4.1 — e o agente `task-implementer` não pode nem editar arquivo fora de
  `target_files` ("Precisar de um arquivo fora dessa lista é empecilho, não
  licença para ampliar") nem rodar `go test`/`UPDATE_GOLDEN=1` para
  regenerar o golden ("Você NÃO executa testes — em hipótese alguma").

  Não achei nenhuma forma de implementar REQ-58 que deixe
  `tests_wallet.go.golden` intocado: mesmo um shrinker que nunca dispara em
  runtime ainda precisa existir como **texto gerado** (a mensagem de
  `t.Fatalf`, a struct `dsPropStep` estendida, a clausura de replay), então
  a mudança de bytes é estrutural, não um detalhe de implementação evitável.

  Isso também tensiona com NFR-31 ("`wallet` e `shop` permanecem
  byte-idênticos... assim como toda forma de... `*.test.ds` que já
  gerava") — a leitura mais estrita da frase proibiria qualquer mudança no
  Go gerado da property de `wallet`, o oposto do que REQ-58 pede; a leitura
  mais provável (design.md, "a guarda de byte-identidade da **forma
  vizinha** que ela não deve tocar") é sobre não vazar a mudança para OUTRAS
  formas do mesmo emissor (scenarios, fixtures) — mas mesmo essa leitura
  mais branda não resolve o problema de `target_files`: a property de
  `wallet` **é** a forma que M4.1 deve tocar, e seu golden não está na
  lista.

  Não implementei nada — nem parcialmente — para não deixar o `TestEmitTestsWalletGolden`
  quebrado sem chance de correção dentro do escopo desta task. [M4.1.md](../specs/correcoes-issues-6-8-12/tasks/M4.1.md)
  precisa ganhar `codegen/testdata/tests_wallet.go.golden` e
  [gentest_test.go](../../../codegen/gentest_test.go) em `target_files` (para reconciliar as
  asserções `strings.Contains` daquele teste com o novo formato de
  `t.Fatalf`, se ele mudar) antes de esta task poder ser reaberta — ou uma
  decisão explícita de design sobre como isolar a mudança da forma já
  golden-testada de `wallet`.
- SOLVED: []