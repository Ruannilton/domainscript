# Field-Level Security de View não implementado (`visibility` ignorado) (ex-ISSUE-4)
- SPEC: [codegen](../specs/codegen/requirements.md)
- TASK: [gaps.md §G-5](../specs/codegen/gaps.md) (Field-Level Security de
  View)
- DESCRIPTION: O bloco `visibility` de View (spec
  [§6.2](../steerings/domainscript-spec-v7/06-read-side.md)) é **parseado**
  (`ast.ViewDecl.Visibility`, [parse_decl.go](../../../parser/parse_decl.go))
  mas **nenhum arquivo do codegen consome `Visibility`** — a omissão
  condicional de campos na serialização não acontece. É a lacuna "silenciosa"
  mais arriscada do inventário (cheiro de segurança que falha em silêncio):
  o programa compila, o bloco é aceito e ignorado. O exemplo
  [`testdata/projects/pizzeria`](../../../testdata/projects/pizzeria)
  (`sales/read.ds`, `OrderVW`) exercita e documenta essa limitação.
  Atenuantes: o spec marca a feature como "em evolução" (§25 em numeração v6
  — hoje
  [`27-evolving-features.md`](../steerings/domainscript-spec-v7/27-evolving-features.md),
  ver nota abaixo) e wallet/shop não a usam. Fechar exige decidir a semântica
  de serialização condicional por caller na borda HTTP/gRPC (o
  `runtime.Caller` já circula até lá) e emitir a filtragem no encode das
  Views.
  **Revisão (spec como fonte de verdade):** o paliativo antes registrado aqui —
  um *warning de geração* "visibility declarado e ignorado", para tirar o
  silêncio — **não é conforme** e fica retirado: seria um diagnóstico que a
  [`27-evolving-features.md`](../steerings/domainscript-spec-v7/27-evolving-features.md)
  não prevê, ou seja, comportamento implementado fora da especificação
  (a issue original citava esta seção ora como "§25", ora como "§27" — mesma
  seção v6, o arquivo v7 correto é `27-evolving-features.md`). O único
  caminho é implementar a §6.2 de fato. Se a §6.2 se mostrar incompleta
  demais para isso (ela é marcada como "em evolução" ali), o passo é abrir
  uma issue de revisão da spec, não emitir um diagnóstico próprio.
- SOLVED: FALSE
