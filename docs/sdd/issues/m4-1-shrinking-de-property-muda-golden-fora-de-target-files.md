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

# Solução proposta

> Análise de 2026-07-31 sobre `main` @ a6b239b. Todas as linhas citadas foram
> reverificadas hoje; `go build ./...` está verde.

## Veredito

**A premissa principal morreu; o bloqueio sobrevive por outro motivo, mais
estreito.**

O argumento central do registro — "o agente `task-implementer` não pode nem
editar arquivo fora de `target_files`" — **não vale mais**. A regra citada
("Precisar de um arquivo fora dessa lista é empecilho, não licença para
ampliar") foi substituída em `1163143` (*"chore(agents): target_files é
referência, não cerca"*, 2026-07-27 05:47 UTC), **depois** de esta issue ser
registrada (`179a218`, 2026-07-26 18:19 UTC). O texto vigente
([task-implementer.md:95](../../../.claude/agents/task-implementer.md)) diz o
oposto: "`target_files` é uma lista de **referência**, não uma cerca … tocar um
arquivo fora dela não é, por si só, empecilho — não gere issue só por isso".
Tocar o golden está liberado hoje.

Os fatos técnicos, por outro lado, conferem todos:

- [tests_wallet.go.golden](../../../codegen/testdata/tests_wallet.go.golden)
  contém `type dsPropStep struct` (linha 17),
  `func TestWallet_SaldoNuncaFicaNegativo` (144), `var trail []dsPropStep`
  (202) e o `t.Fatalf` com a sequência completa (287).
- [gentest_test.go:122](../../../codegen/gentest_test.go) compara byte a byte
  via `gentest.Golden`.
- É o **único** golden afetado: `grep -rl dsPropStep codegen/testdata/` casa 1
  dos 56 `*.golden`; `shop` e `pizzeria` não declaram `property` nenhuma (só
  [wallet.test.ds:52](../../../testdata/projects/wallet/wallet.test.ds) e o
  exemplo `docs/examples/09-testes/wallet.test.ds`).

**O que continua bloqueando de verdade:** o guard
([task-implementer-guard.sh:57](../../../.claude/hooks/task-implementer-guard.sh))
recusa **qualquer** `go test`, e `UPDATE_GOLDEN=1 go test -run …`
([golden.go:15-23](../../../codegen/gentest/golden.go)) é o **único** produtor
dos bytes do golden. O executor tem permissão de escrever o arquivo e não tem
como produzir seu conteúdo — 360 linhas de Go já passadas por `go/format`.

**Duas correções ao enunciado**, ambas reduzem o escopo:

1. **[gentest_test.go](../../../codegen/gentest_test.go) não precisa ser
   editado.** Nenhuma das 29 asserções `strings.Contains` (linhas 89-117)
   menciona a emissão de `property` — nem `dsPropStep`, nem
   `TestWallet_SaldoNuncaFicaNegativo`, nem o texto do `t.Fatalf`. O arquivo
   *falha* até o golden ser regravado, mas não muda. O único arquivo fora de
   `target_files` que **precisa** mudar é o golden.
2. **`dsPropStep` não precisa de campo novo.** A clausura de replay é tipada
   pelo Aggregate (`func(*Wallet) ([]runtime.Event, error)`) e `dsPropStep` é
   um tipo *package-level* compartilhado por todos os Test do arquivo — pôr a
   clausura ali obrigaria a `any` + type assertion. A rota certa é uma **slice
   paralela**, declarada dentro da própria função da property, ao lado de
   `trail`. Com isso os três helpers compartilhados
   ([golden:12-40](../../../codegen/testdata/tests_wallet.go.golden)) ficam
   byte-idênticos, exceto pelo shrinker novo (aditivo), e todo o resto do diff
   fica dentro de `TestWallet_SaldoNuncaFicaNegativo`.

## Causa raiz

O golden é **artefato derivado**, não fonte: seu único gerador é o binário de
teste. A task foi fatiada listando os arquivos que alguém *digita*, e o agente
que a executa está estruturalmente proibido de rodar o gerador do arquivo que a
mudança inevitavelmente invalida — a task manda mexer no emissor e nega acesso
à saída do emissor.

## Desbloqueio proposto

**Regenerar golden deixa de ser "rodar teste" e passa a ser um alvo de build.**
Um alvo `golden` no [Makefile](../../../Makefile)
(`UPDATE_GOLDEN=1 go test ./codegen/... -run Golden`), mais uma frase no
[agente](../../../.claude/agents/task-implementer.md) e a exceção
correspondente no [guard](../../../.claude/hooks/task-implementer-guard.sh):
`UPDATE_GOLDEN=1` **com** `-run` é liberado; todo o resto continua negado.

**Por que não é workaround.** O que a proibição protege é o agente não ter
*feedback de asserção* local e não iterar em cima de um CI vermelho. Com
`UPDATE_GOLDEN=1`, `Golden` **retorna antes de ler o arquivo de referência**
([golden.go:16-24](../../../codegen/gentest/golden.go)): não compara, não
afirma, só escreve — é a mesma categoria de `gofmt -w`, que já é permitido pelo
mesmo guard e pelo mesmo motivo. E a rede de segurança fica intacta: um golden
regravado errado **não** é silenciado por essa regravação, porque
`TestEmitTestsWalletRunsGreen`
([gentest_test.go:138](../../../codegen/gentest_test.go)) compila e **roda** os
testes gerados de verdade, e o job `fixtures`
([ci.yml:47-95](../../../.github/workflows/ci.yml)) faz `dsc gen` +
`go build`/`go vet` sobre os bytes em disco. Golden mentiroso continua caindo
no CI.

**Alternativa de custo zero em regra, que prova que a proibição não protege
nada aqui:** deixar o CI produzir o golden. `Golden` falha imprimindo o `got`
inteiro ([golden.go:29-31](../../../codegen/gentest/golden.go)); o agente lê
360 linhas do log e as escreve no arquivo. Funciona **hoje**, é literalmente "o
CI é meu único feedback" — e é péssimo: dois ciclos de CI, fidelidade de bytes
dependendo de copiar log, e nenhuma garantia a mais do que o alvo de Makefile
daria.

**Descartadas:** (a) fatiar para que outro agente regrave o golden — nenhum
agente edita código *e* roda teste, por desenho (`issue-registrar` roda testes e
nunca toca a árvore versionada); (b) escrever um regerador fora de `go test` —
duplicaria o harness que monta as entradas de `EmitTests`
([gentest_test.go:69-85](../../../codegen/gentest_test.go)), que é justamente
o que o golden fixa.

Independentemente da rota, [M4.1.md](../specs/correcoes-issues-6-8-12/tasks/M4.1.md)
deve ganhar `codegen/testdata/tests_wallet.go.golden` em `target_files` (como
documentação do alcance, não como permissão) e **perder** a exigência de
[gentest_test.go](../../../codegen/gentest_test.go), que não muda.

## Implementação proposta

Tudo dentro da função emitida da property; `dsPropStep` intocado.

**(a) Seed reconstruível.** Hoje `w := &Wallet{}`
([gentest_property.go:299](../../../codegen/gentest_property.go)) é emitido
*antes* dos laços `for attempt` de `genValue`, intercalando sorteio e
atribuição. Emitir primeiro **todos** os sorteios (mesma ordem, mesmo consumo
de `r` — Step 5 ao pé da letra), depois
`dsSeed := func() *Wallet { w := &Wallet{}; w.state.Id = v1; …; return w }` e
`w := dsSeed()`. Os sorteios ficam **fora** da clausura: replay nunca pode
tocar em `r`. O `caller` ([gentest_property.go:319](../../../codegen/gentest_property.go))
é reusado — o `state.Id` é o mesmo em toda reconstrução.

**(b) Trail de replay tipado.** Ao lado de `trail` (tipo inalterado),
`var replay []func(*Wallet) ([]runtime.Event, error)`; cada `case` do switch
([gentest_property.go:356-373](../../../codegen/gentest_property.go)) acrescenta
uma clausura sobre os argumentos **já sorteados**
(`func(w *Wallet) (…) { return w.Deposit(caller, v13, v16) }`). Valores
concretos e tipados, sem `any`, sem re-sorteio.

**(c) Invariante como clausura.** A lowerização única
([gentest_property.go:248](../../../codegen/gentest_property.go)) passa a ser
emitida em `dsInv := func(w *Wallet) bool { <hoisted>; return <expr> }`, com o
parâmetro nomeado exatamente `receiver` — o texto lowerizado é reusado
**verbatim**, sem um segundo `ExprHoisted` e sem re-bind. A checagem no laço
vira `if !dsInv(w)`. O mesmo vale para o dispatch de Apply
([gentest_property.go:384](../../../codegen/gentest_property.go)), extraído
para `dsApply := func(w *Wallet, evs []runtime.Event)` e usado pelos dois
sítios, em vez de emitido duas vezes.

**(d) Predicado.**
`stillFails := func(idx []int) bool { w := dsSeed(); for _, i := range idx { evs, err := replay[i](w); if err != nil { continue }; dsApply(w, evs); if !dsInv(w) { return true } }; return false }`
— o `continue` no erro de negócio reproduz exatamente a semântica de hoje
(invariante só é checada depois de transição bem-sucedida).

**(e) Shrinker**, helper package-level novo em `emitPropertyHelpers`
([gentest_property.go:750](../../../codegen/gentest_property.go)), puramente
sobre índices — sem o tipo do Aggregate, portanto sem generics:
`func dsPropShrink(n, budget int, stillFails func([]int) bool) []int`.
Algoritmo *ddmin* enxuto: parte de `[0..n-1]`; para tamanhos de bloco
`n/2, n/4, …, 1`, tenta remover cada bloco contíguo da esquerda para a direita,
aceitando a remoção sempre que `stillFails` continuar verdadeiro; termina quando
uma passada inteira com bloco 1 não aceita nada, ou quando o orçamento de
re-execuções se esgota. **Determinístico** (ordem de candidatas fixa, zero
consumo de `r` — REQ-58.2 por construção, não por disciplina), **monotônico**
(toda candidata aceita é estritamente menor ⇒ no máximo `n` remoções aceitas) e
**limitado** por um `propShrinkMaxCandidates` explícito no espírito de
`propGenMaxAttempts` (Step 3). Com `propGenMaxSteps = 20`
([gentest_property.go:159-163](../../../codegen/gentest_property.go)) a
1-minimização completa custa ≈ n²/2 ≈ 200 replays: o teto nunca morde na
prática, é defesa.

**(f) Relato.** Convergido o índice, **um** replay final gravando reconstrói o
`[]dsPropStep` com os erros observados *na sequência mínima* — um `Withdraw`
que sucedeu na posição 5 pode legitimamente falhar na posição 0, e reportar o
`Err` original seria mentir sobre uma sequência que nunca rodou. O `t.Fatalf`
mantém o formato legível de hoje e acrescenta a contagem:
`"propriedade %q violada (iteração %d): sequência mínima de %d passos (de %d gerados) %+v"`.
Texto de mensagem de teste gerado **não é matéria da spec da linguagem** (§24
nunca especifica texto de Go emitido) — é decisão de design, não invenção sobre
a spec.

**(g) Caminho verde.** Zero replays quando a invariante nunca falha (REQ-58.3).
O custo residual é uma clausura alocada por passo (2 000 por execução da
property), irrelevante ao lado dos `for attempt` já emitidos — mas REQ-58.3 lido
ao pé da letra ("sem custo no caminho verde") proíbe até isso; o
[design.md](../specs/correcoes-issues-6-8-12/design.md) §4.5 deveria dizer
**"nenhuma re-execução"**, que é a garantia entregável.

**Alternativa descartada:** re-semear a RNG por iteração
(`rand.NewSource(seed ^ int64(iter))`) e reproduzir a sequência re-sorteando,
em vez de guardar clausuras. Custo verde zero — mas muda o fluxo de sorteios e,
com ele, **quais** sequências são exploradas, contrariando o Step 5 explícito
("a sequência original gerada deve continuar sendo a mesma de hoje").

**Testes** (`codegen/gentest_property_test.go`, novo) seguindo o padrão já
existente de [gentest_thenstate_test.go](../../../codegen/gentest_thenstate_test.go)
(`thenStateFixtureSrc:57`, `generateThenStateProject:116`,
`runGeneratedTestsExpectingFailure:219`): um Aggregate sintético com um Handle
`Boom` cujo evento leva um campo `integer` além de um limiar que o seed
aleatório não alcança (`genPrimitive` sorteia `r.Int63n(1_000_000)`,
[gentest_property.go:499](../../../codegen/gentest_property.go)) — a sequência
mínima é **exatamente** o passo `Boom`, quantos passos inertes o precedam
(TEST-1); um Aggregate cuja invariante quebra em todo Handle termina dentro do
orçamento (TEST-3); `wallet` continua verde por `runGeneratedTests` (TEST-2).

## O padrão

**Confirmado, e é a mesma causa nas três.** `m1-1-*`, `m2-3-*` e esta descrevem
o mesmo defeito de fatiamento por três recortes diferentes do que ficou de fora
de `target_files`: em `m1-1`, o *lugar real dos testes*
(`rtsrc/runtime_test.go.txt`, não `rtsrc_test.go`) e o emissor vizinho na única
rota (`lower/stmt.go`); em `m2-3`, o *mecanismo que o próprio design.md nomeia*
(`decl_policy.go`) e sua fiação (`codegen.go`); aqui, o *artefato derivado* que
a mudança do emissor invalida por construção.

A causa comum: **`target_files` foi escrito como palpite sobre onde se digita, e
não como o fecho da mudança**. Nenhuma das 18 tasks desta spec lista um único
`codegen/testdata/*.golden`, embora M1.2, M1.3, M2.3, M2.4, M3.3, M4.1 e M4.2
mudem emissores que têm golden. E a
[requirements.md:309-315](../specs/correcoes-issues-6-8-12/requirements.md)
agrava: **NFR-31 promete byte-identidade global** de `wallet`/`shop` "assim como
toda forma de … `*.test.ds` que já gerava" — ou seja, o mesmo documento que
manda mudar a emissão da property proíbe, num NFR, a consequência de mudá-la.
`m1-1` chegou à mesma conclusão de forma independente ("NFR-31 precisa ser
emendado").

O que muda no fatiamento, para parar de produzir isto:

1. **`target_files` é fecho, não palpite.** Numa task de codegen ele inclui
   sempre: o emissor, o teste do emissor, **todo `codegen/testdata/*.golden`
   que aquele emissor produz** (descobrível mecanicamente — `grep -rl` pelo
   símbolo emitido, como o `dsPropStep` que aponta 1 golden entre 56) e
   qualquer fixture de `testdata/projects/` cuja saída se mova.
2. **Duas categorias de entrada**: *fonte* (editada à mão) e *derivada*
   (regerada). Entrada derivada não precisa de justificativa e **carrega o
   comando que a regenera** — o que torna impossível fatiar uma task cujo
   artefato o executor não sabe produzir, que é exatamente o resíduo desta
   issue.
3. **Byte-identidade se escreve como delta, nunca como promessa global.** Cada
   task declara quais goldens *pode* mover; golden que se move fora dessa lista
   é o alarme. Isso preserva o propósito real de NFR-31 e o torna satisfazível.
4. **Uma linha de "raio de alcance" por task, no momento de escrever a spec** —
   goldens, fixtures, jobs de CI. É precisamente o que as três issues tiveram
   de reconstruir *depois*, cada uma ao custo de um ciclo de execução perdido.
5. **Re-triagem, não novo registro.** Com `target_files` sendo referência desde
   `1163143`, "o arquivo está fora da lista" deixou de ser motivo de bloqueio.
   Sobram duas classes legítimas: *ninguém decidiu a rota* (m2-3, travada na
   §5.3.2 da spec) e *o executor não consegue produzir o artefato* (esta). Toda
   issue cujo único argumento seja o primeiro deve ser retriada, não repetida.

## Raio de alcance

- **Goldens: 1 de 56.**
  [tests_wallet.go.golden](../../../codegen/testdata/tests_wallet.go.golden), e
  dentro dele o diff fica confinado a `TestWallet_SaldoNuncaFicaNegativo`
  (144-289) mais o bloco de helpers (12-40), onde a única mudança é **aditiva**
  (`dsPropShrink`); `dsPropStep` (17-21) permanece igual pela rota da slice
  paralela.
- **[gentest_test.go](../../../codegen/gentest_test.go): nenhuma edição.**
  `TestEmitTestsWalletGolden` falha até a regravação;
  `TestEmitTestsWalletRunsGreen` e `TestEmitFixturesWalletBehavior` **executam**
  a property emitida (100 × até 20 passos) — é a guarda comportamental que
  golden nenhum dá (NFR-17).
- **[testdata/projects/](../../../testdata/projects):** nenhum `.ds` muda; o job
  `fixtures` refaz `dsc gen` + `go build`/`go vet` sobre `wallet`, então a
  emissão nova precisa compilar. `shop`/`pizzeria`: zero bytes (não declaram
  `property`).
- **NFR-13:** preservado por construção — o shrinking não consome `r`, a ordem
  de candidatas é aritmética de índices, e o texto emitido segue sendo função
  pura do AST.
- **NFR-31:** precisa da emenda descrita em "O padrão"; sem ela, a task viola um
  NFR só por cumprir seu REQ.

## Bloqueios

1. **Spec da linguagem: nenhum.** §24.5
   ([24-testing.md:105-116](../steerings/domainscript-spec-v7/24-testing.md))
   diz "reporta o **contra-exemplo mínimo** em falha" — é licença explícita, e a
   ausência de algoritmo não é lacuna: a spec nunca descreve o Go emitido. A
   revisão de 2026-07-31 (§2.7, §2.8, §4.2.3, §4.3.1, §5.3, §9.4, §19.3) não
   toca §24.5 nem o caminho de `property`. **É o contraste limpo com m2-3**, que
   está travada na spec de verdade.
2. **[design.md](../specs/correcoes-issues-6-8-12/design.md) §4.5 precisa fixar
   dois pontos** antes da reabertura — decisão do dono do ciclo, não de quem
   implementa: (a) "mínimo" = **1-minimal por remoção**, não mínimo global, para
   ninguém ler §24.5 como promessa mais forte do que qualquer shrinker entrega;
   (b) os erros relatados vêm do replay mínimo, não da execução original.
3. **Processo:** a rota de regeneração do golden (seção "Desbloqueio") tem de
   ser escolhida. É o único resíduo real do bloqueio original — e vale para
   M1.2, M1.3, M2.4, M3.3 e M4.2 também, não só para M4.1.

## Fatiamento sugerido

1. **M4.0 — regenerar golden deixa de ser "rodar teste".** Alvo `golden` no
   Makefile, exceção `UPDATE_GOLDEN=1 … -run` no guard, frase correspondente no
   agente e na seção *Commands* do CLAUDE.md. Independe de M4.1 e desbloqueia
   toda task futura que toque emissor com golden.
   `target_files`: `Makefile`, `.claude/hooks/task-implementer-guard.sh`,
   `.claude/agents/task-implementer.md`, `CLAUDE.md`.
2. **M4.1a — texto do ciclo** *(spec-writer, sem código)*: design.md §4.5 com
   1-minimal + formato do relato + leitura de REQ-58.3; NFR-31 reescrita como
   delta; M4.1 refatiada em b/c com `target_files` real.
   `target_files`: `docs/sdd/specs/correcoes-issues-6-8-12/design.md`,
   `.../requirements.md`, `.../tasks/M4.1*.md`, `.../state.md`.
3. **M4.1b — reestruturação sem mudança semântica:** `dsSeed`/`dsInv`/`dsApply`
   como clausuras, `replay` acumulada e ainda não usada, nenhum shrinking.
   Comportamento idêntico, um único golden regravado, diff inteiramente
   mecânico — é o que torna o passo seguinte auditável.
   `target_files`: `codegen/gentest_property.go`,
   `codegen/testdata/tests_wallet.go.golden`.
4. **M4.1c — o shrinker:** `dsPropShrink` + caminho de falha + relato mínimo com
   contagem; `codegen/gentest_property_test.go` com TEST-1/TEST-2/TEST-3.
   Segundo golden regravado, com diff restrito ao ramo de falha e ao bloco de
   helpers.
   `target_files`: `codegen/gentest_property.go`,
   `codegen/gentest_property_test.go`,
   `codegen/testdata/tests_wallet.go.golden`.
5. **M4.1d (opcional) — guarda de determinismo do contra-exemplo (REQ-58.2):**
   rodar o projeto sintético que falha **duas** vezes via
   `runGeneratedTestsExpectingFailure` e exigir a mesma sequência mínima nas
   duas saídas.
   `target_files`: `codegen/gentest_property_test.go`.