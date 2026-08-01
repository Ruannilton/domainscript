CODIGO: spec-v7-identidade-implicita-do-aggregate
CATEGORIA: Correção de código
Issue original: [[docs/sdd/issues/spec-v7-identidade-implicita-do-aggregate]]

## Resumo da issue

Os exemplos canônicos da spec (`Aggregate Wallet`, `Aggregate Person`) usavam `self.id` em `Handle` e em `access` sem que `id` jamais aparecesse no bloco `state` — a spec pressupunha uma identidade implícita de Aggregate, mas nunca definia seu tipo, se ela se relacionava com um VO de id do domínio, nem como era atribuída. Rodar o exemplo da antiga §4.5 (hoje `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]`) verbatim contra o `dsc` do HEAD dava `error[E102]: membro inexistente: "id" em Wallet`, e a única forma que funcionava (`testdata/projects/wallet`) contornava declarando `id WalletId` manualmente — o oposto do que a spec descrevia.

## Evidencias

- `error[E102]: membro inexistente: "id" em Wallet` — verificado rodando a antiga §4.5 (hoje `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]`) verbatim.
- `types/model.go:164-168` — hoje `Model.Members` de um Aggregate são exatamente os campos declarados em `state`, sem membro implícito nenhum.
- `testdata/projects/wallet/domain.ds:83` contorna o problema declarando `id WalletId` no `state` e semeando-o num `Apply` — forma que a spec não usa e que a issue já sinalizava como incompatível.
- A nota do desenvolvedor em `[[docs/notes/Issues/spec-v7-identidade-implicita-do-aggregate|docs/notes/Issues/spec-v7-identidade-implicita-do-aggregate]]` decide: "Todos os agregados possuem implicitamente um campo `id` cujo tipo é uma Ref Agregado ([[docs/notes/Features/Tipos Referencia|Tipos Referencia]]). Os Eventos terão sempre um `id` do tipo UUIDv7" — e o rascunho de `Tipos Referencia` já mostra exatamente o exemplo `Aggregate Person` com `self.id` implícito, sem `id` em `state`.
- `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.3.1 escreve essa decisão como texto normativo completo: `id` é membro implícito de tipo `ref T`, nunca declarável em `state`, readonly, com tabela de onde é legível (`Handle`, `Apply`, `access`, `visibility`, instância carregada fora do Aggregate) e como é atribuído (`generation: system` vs. `client`, incluindo o marcador `identity` em campo de Command).

## Impacto no projeto

Sem essa definição, o exemplo canônico da própria spec não compilava, e não havia base normativa para decidir se `caller.id == self.id` (usado em `access`) fazia sentido de tipos — a comparação de identidade é central ao modelo de autorização da linguagem.

## Soluçoes possíveis

### Solucão 1

Implementar `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.3.1 como está escrito: todo `Aggregate T` ganha um membro implícito `self.id : ref T`, nunca declarável em `state` (erro de compilação em colisão de nome), readonly, atribuído antes do primeiro evento (`generation: system`, runtime aloca) ou no mesmo instante que a instância é endereçada (`generation: client`, valor vem do campo marcado `identity` no Command de criação). Isso conecta diretamente com `[[docs/sdd/steerings/domainscript-spec-v7/02-type-system|02-type-system.md]]` §2.7 (`ref T`), que já é a definição normativa do tipo em si.

### Solução 2

Não há uma segunda rota razoável: a issue original já apresentava as duas alternativas possíveis — (a) `id` implícito com tipo dedutível, ou (b) identidade sempre um campo explícito de `state`, corrigindo os exemplos da spec. A nota do desenvolvedor escolheu (a) de forma inequívoca, e a spec revisada já reflete essa escolha; manter (b) exigiria reescrever os exemplos canônicos da spec para se adequar ao código, o que contraria a regra do projeto de que a spec nunca retrocede para a forma que a implementação já aceita.

## O que precisa ser resolvido antes

Nenhuma — a spec já é clara: `[[docs/sdd/steerings/domainscript-spec-v7/04-domain-core|04-domain-core.md]]` §4.3.1 define tipo, atribuição, contextos de leitura, interação com `CallerId`/`caller.id` e com ValueObjects de id legados, com uma tabela completa de erros que o exemplo final recusaria. O trabalho restante é implementação em `types.Model` (para expor `self.id` como membro implícito de todo Aggregate) e no resolver/checker (para as regras de colisão, readonly e contexto de leitura).
