CODIGO: m4-1-shrinking-de-property-muda-golden-fora-de-target-files
CATEGORIA: Dependente de decisão do desenvolvedor

## Resumo da issue

A task M4.1 pede shrinking determinístico do contra-exemplo de `property`
(REQ-58, [[docs/sdd/steerings/domainscript-spec-v7/24-testing|§22.5]] da spec
de testes — numeração v6, o arquivo v7 correspondente é `24-testing.md`).
Qualquer implementação fiel muda o texto Go
estático emitido para a `property` já existente em
`testdata/projects/wallet/wallet.test.ds`, o que por sua vez invalida
`codegen/testdata/tests_wallet.go.golden` — um artefato **derivado**, cujo
único gerador é rodar `go test` com `UPDATE_GOLDEN=1`. O agente que executa a
task está proibido de rodar testes, então fica sem como produzir o próprio
arquivo que a mudança exige.

## Evidencias

- [[docs/sdd/issues/m4-1-shrinking-de-property-muda-golden-fora-de-target-files]]
  cita `type dsPropStep struct` (golden linha 17),
  `TestWallet_SaldoNuncaFicaNegativo` (144), `trail` (202) e o `t.Fatalf` (287)
  como o trecho que qualquer shrinker altera.
- `gentest_test.go:122` compara o golden **byte a byte** via `gentest.Golden`.
- `task-implementer-guard.sh:57` recusa qualquer `go test`; `golden.go:15-23`
  é o único produtor dos bytes do golden, via `UPDATE_GOLDEN=1`.
- É o único golden afetado hoje: `grep -rl dsPropStep codegen/testdata/` casa 1
  golden em 56 (`shop`/`pizzeria` não declaram `property`).
- A revisão de 2026-07-31 já derrubou metade do bloqueio original: a regra
  citada ("arquivo fora de `target_files` é empecilho") foi substituída em
  `1163143` — `target_files` é referência, não cerca, desde 2026-07-27. Tocar o
  golden já está liberado; o que falta é **como produzir** seu conteúdo.

## Impacto no projeto

Sem um jeito aprovado de regenerar o golden, M4.1 (e qualquer task futura de
codegen que toque um emissor com golden — a análise lista M1.2, M1.3, M2.4,
M3.3, M4.2 no mesmo caso) fica travada: o executor sabe o que escrever no
emissor, mas não consegue produzir a saída de 360 linhas já passada por
`go/format` que o teste exige, e não tem permissão de rodar o teste que a
geraria. REQ-58 (shrinking de contra-exemplo de `property`) fica sem
implementação enquanto isso não se resolve.

## Soluçoes possíveis
### Solucão 1
Alvo `golden` no `Makefile` (`UPDATE_GOLDEN=1 go test ./codegen/... -run
Golden`), com exceção equivalente no guard do `task-implementer` liberando
especificamente `UPDATE_GOLDEN=1` **com** `-run`. Justificativa técnica da
análise: com `UPDATE_GOLDEN=1`, `Golden()` retorna antes de ler o arquivo de
referência — não compara, não afirma, só escreve — mesma categoria de `gofmt
-w`, já permitido pelo mesmo guard. A rede de segurança continua intacta
porque `TestEmitTestsWalletRunsGreen` roda os testes gerados de verdade e o
job `fixtures` do CI compila/`go vet` os bytes em disco — um golden regravado
errado ainda cai no CI.
### Solução 2
Deixar o CI produzir o golden: `Golden` falha imprimindo o `got` inteiro, e o
agente copia as 360 linhas do log de volta para o arquivo. Funciona hoje sem
mudar Makefile/guard algum, mas a própria análise qualifica como "péssimo":
dois ciclos de CI por task, fidelidade de bytes dependendo de copiar um log, e
nenhuma garantia adicional sobre a Solução 1. Listada só para mostrar que o
processo atual não protege nada aqui — não é uma alternativa que a análise
recomenda adotar.

## O que precisa ser resolvido antes

- Qual rota de regeneração de golden vira processo padrão: alvo de Makefile +
  exceção no guard (Solução 1, recomendada pela análise), ou copiar do log do
  CI (Solução 2)? Vale para M4.1 e para toda task futura na mesma situação
  (M1.2, M1.3, M2.4, M3.3, M4.2).
- [[docs/sdd/specs/correcoes-issues-6-8-12/design|design.md]] §4.5 da spec
  `correcoes-issues-6-8-12` precisa fixar, por
  decisão do dono do ciclo (não de quem implementa): (a) "mínimo" no relato de
  REQ-58 significa 1-minimal por remoção, ou mínimo global? (b) os erros
  relatados no `t.Fatalf` vêm do replay da sequência mínima, ou da execução
  original? A análise já argumenta por (a) 1-minimal e (b) replay mínimo, mas
  registra explicitamente que a escolha é do dono do ciclo, não do executor.
- Depois de resolvidos os dois pontos acima,
  [[docs/sdd/specs/correcoes-issues-6-8-12/tasks/M4.1|M4.1.md]] deve ganhar
  `codegen/testdata/tests_wallet.go.golden` em `target_files` (como
  documentação de alcance) e perder a exigência de `gentest_test.go`, que a
  análise mostra não precisar mudar (nenhuma das asserções `strings.Contains`
  menciona a emissão de `property`).
