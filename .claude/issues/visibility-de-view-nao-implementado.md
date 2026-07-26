# Field-Level Security de View não implementado (`visibility` ignorado) (ex-ISSUE-4)
- SPEC: codegen
- TASK: gaps.md §G-5 (Field-Level Security de View)
- DESCRIPTION: O bloco `visibility` de View (spec §6.2) é **parseado**
  (`ast.ViewDecl.Visibility`, `parser/parse_decl.go`) mas **nenhum arquivo do
  codegen consome `Visibility`** — a omissão condicional de campos na
  serialização não acontece. É a lacuna "silenciosa" mais arriscada do
  inventário (cheiro de segurança que falha em silêncio): o programa compila,
  o bloco é aceito e ignorado. O exemplo `testdata/projects/pizzeria`
  (`sales/read.ds`, `OrderVW`) exercita e documenta essa limitação. Atenuantes:
  o spec marca a feature como "em evolução" (§25) e wallet/shop não a usam.
  Fechar exige decidir a semântica de serialização condicional por caller na
  borda HTTP/gRPC (o `runtime.Caller` já circula até lá) e emitir a filtragem
  no encode das Views.
  **Revisão (spec como fonte de verdade):** o paliativo antes registrado aqui —
  um *warning de geração* "visibility declarado e ignorado", para tirar o
  silêncio — **não é conforme** e fica retirado: seria um diagnóstico que a
  §25 não prevê, ou seja, comportamento implementado fora da especificação. O
  único caminho é implementar a §6.2 de fato. Se a §6.2 se mostrar incompleta
  demais para isso (ela é marcada como "em evolução" na §27), o passo é abrir
  uma issue de revisão da spec, não emitir um diagnóstico próprio.
- SOLVED: FALSE
