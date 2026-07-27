# Spec v7: identidade implícita do Aggregate (`self.id`) usada sem declaração nem tipo
- SPEC: [domainscript-spec-v7](../steerings/domainscript-spec-v7/README.md)
  (revisão da especificação)
- TASK: [review-v7.md §A-2](../steerings/review-v7.md)
- DESCRIPTION: A [§4.5](../steerings/domainscript-spec-v7/04-domain-core.md)
  (`Aggregate Wallet`) e a [§2.5](../steerings/domainscript-spec-v7/02-type-system.md)
  (`Aggregate Person`) usam `self.id` — em `Handle` (`emit
  WalletCreated(self.id, holder, email)`) e no bloco `access` (`Withdraw
  requires caller.id == self.id`) — sem que `id` apareça no bloco `state` de
  nenhum dos dois. A spec portanto **pressupõe uma identidade implícita**,
  mas em nenhum lugar a define: não diz qual é o tipo de `self.id`, se ele se
  relaciona com o VO de id do domínio (`WalletId`, `PersonId`) ou se é um id
  opaco da plataforma, nem como ele é atribuído (o `Apply WalletCreated` da
  §4.5 não escreve `state.id`). A
  [§26](../steerings/domainscript-spec-v7/26-glossary.md) (glossário)
  descreve Aggregate como "State, handles, access, storage" — sem mencionar
  identidade.
  Sem essa definição a regra não é implementável: hoje `Model.Members` de um
  Aggregate são exatamente os campos de `state`
  ([`types/model.go:164-168`](../../../types/model.go#L164-L168)), e o exemplo
  da §4.5 rodado verbatim dá `error[E102]: membro inexistente: "id" em
  Wallet` (verificado com o `dsc` do HEAD).
  [`testdata/projects/wallet/domain.ds:83`](../../../testdata/projects/wallet/domain.ds#L83)
  contorna declarando `id WalletId` no `state` e semeando-o num `Apply` — o
  que funciona, mas é a forma que a spec **não** usa.
  **A spec precisa definir**: (a) que `id` é membro implícito de todo Aggregate,
  e qual o seu tipo — provavelmente o VO de id declarado, o que exige dizer como
  o compilador o identifica (convenção de nome? um marcador no `state`? o tipo
  do campo `ref` do Command que o endereça?); ou (b) que a identidade é sempre
  um campo explícito de `state`, e então corrigir os exemplos da §4.5 e §2.5
  para declará-lo. Vale notar a interação com a Regra de Ouro
  ([§2.1](../steerings/domainscript-spec-v7/02-type-system.md)): se a
  identidade implícita for `string`, é um primitivo no Write Side.
- SOLVED: FALSE
