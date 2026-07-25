# Spec v7: `ref` é keyword na §5.1 e identificador na §2.5 (contradição interna)
- SPEC: domainscript-spec-v7 (revisão da especificação)
- TASK: review-v7.md §A-6
- DESCRIPTION: A §5.1 (Commands) exige `ref` como **keyword** na declaração de
  campo — `walletId ref Wallet` — e a implementação a leva como hard keyword
  (`token.REF`). A §2.5 (tipo `File`), no mesmo documento, usa `ref` como
  **identificador comum** em três posições do seu exemplo canônico:
  `Handle AttachDocument(ref FileRef)` (nome de parâmetro),
  `emit DocumentAttached(self.id, ref)` (referência a ele) e
  `ref = store cmd.document` (nome de variável local). Nenhuma gramática
  satisfaz as duas leituras: com `ref` reservado o exemplo da §2.5 é erro de
  sintaxe (`esperava um identificador, encontrei ref` / `esperava uma
  expressão, encontrei ref`, verificado com o `dsc` do HEAD); sem reservá-lo,
  a forma `campo ref Tipo` da §5.1 fica ambígua com `campo Tipo`.
  **A spec precisa decidir**: (a) manter `ref` reservado e reescrever o
  exemplo da §2.5 com outro nome (ex. `fileRef`/`docRef`); (b) trocar a
  marcação da §5.1 por outra grafia (ex. `walletId: ref Wallet`, ou um sufixo
  de tipo) e liberar `ref` como identificador; ou (c) tornar `ref` uma soft
  keyword, reconhecida só na posição de tipo de campo — a implementação já tem
  precedente disso (`nameableKeywords`, `parser/parse_decl.go:104`), o que
  torna (c) a saída mais barata sem perder nenhuma das duas formas.
  Fora isso o resto da §2.5 funciona ponta a ponta (`store`, `load File(...)`,
  `signed_url(..., expires:)`, `delete file(...)`, roteamento de campo
  `FileRef` para `FileStorage`) — o único bloqueio é a colisão do nome.
  **Nenhuma linha de código antes da decisão**: implementar qualquer das três
  saídas hoje é escolher pela spec.
- SOLVED: FALSE
