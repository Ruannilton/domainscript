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

# Solução proposta

## Veredito

**Real, e mais ampla do que a issue descreve.** Verificado hoje contra o
código:

- O bloco é parseado e guardado: [parse_decl.go](../../../parser/parse_decl.go):683-713 (`parseView`,
  linha 701 reconhece `visibility` e delega a `parseAccessBlock`, :907) →
  [ast/decl.go](../../../ast/decl.go):229-235 (`ViewDecl.Visibility []*AccessRule`).
- **Nenhum consumidor, em fase nenhuma.** `Visibility` só aparece em
  `ast/decl.go`, no parser e em testes de parser — nenhum arquivo de
  `codegen/`, `sema/`, `resolver/` ou `types/` lê o campo.
  [decl_view.go](../../../codegen/decl_view.go):34-38 admite isso por escrito, e
  `emitViewDecl` (:61-88) constrói o struct sem sequer olhar `decl.Visibility`.
- O silêncio começa **antes** do back-end: [resolver.go](../../../resolver/resolver.go):193-194
  resolve só `n.Fields` de um `ViewDecl`, e
  [resolve_body.go](../../../resolver/resolve_body.go):77-127 (`resolveDeclBodies`) **não tem caso
  `*ast.ViewDecl`** — as condições de `access` do Aggregate são resolvidas ali
  (:98-104), as de `visibility` não são resolvidas por ninguém. O mesmo em
  [rules_typecheck.go](../../../sema/rules_typecheck.go):40-91 (`checkDeclMembers` cobre `access` em
  :57-61, não cobre View) e em [rules_compat.go](../../../sema/rules_compat.go):60 (só `n.Access`).

Prova empírica (cópia do fixture `wallet` **fora** do repositório, com um
bloco `visibility` deliberadamente absurdo — `caller.id ==
self.campoQueNaoExiste`, `campoInexistenteNaView requires
nomeDesconhecido.foo() == 42`): `dsc check` sai **0, sem um único
diagnóstico**, e `dsc gen` sai 0 emitindo

```go
type WalletView struct {
	Id      WalletId   `json:"id"`
	Balance Money      `json:"balance"`
	Holder  HolderName `json:"holder"`
}
```

— `balance` serializado para qualquer caller. Não é só "a regra não é
aplicada": nome inexistente, campo inexistente e receptor inexistente dentro
do bloco também passam. É uma zona cega completa do compilador.

## Causa raiz

`visibility` foi parseado como *dado sintático* e nunca ligado a nenhuma fase
seguinte: o `ViewDecl` entra na resolução de tipos só pelos campos e não entra
na resolução de corpos nem na checagem de membro, e o emissor de View foi
escrito (E8.1) para um recorte que explicitamente excluía o bloco. Como
`visibility` não tem efeito observável nenhum, nada quebrou — que é
exatamente a definição de falha silenciosa de segurança.

## Mitigação imediata

Duas medidas, **nenhuma delas inventa diagnóstico de linguagem** — o ponto que
a revisão desta issue corretamente barrou:

**(1) Recusar a geração, em vez de gerar código inseguro.** `codegen` passa a
falhar com erro de **geração** (não `diag.Diagnostic`) quando um `ViewDecl`
tem `Visibility` não vazio: *"View X declara `visibility` (§6.2) e este
gerador ainda não emite a filtragem de campo — gerar o struct sem ela
produziria código que serializa campo restrito para qualquer caller"*. Isso é
superfície do **transpilador**, não da linguagem: o veredito do front-end
(`dsc check`) não muda, o programa continua válido, e a §27 continua sem
ganhar diagnóstico nenhum. O padrão já é doutrina neste repositório e está no
mesmo arquivo que a solução vai tocar — [http.go](../../../codegen/http.go):180 e :197 recusam
`tenant { from: path }` e `jwt_claim(...)` com a justificativa literal *"falha
explícita em vez de gerar um extractor silenciosamente vazio"*, e
[http.go](../../../codegen/http.go):890 recusa `VersionRoute` sobre Query. `visibility` é o mesmo
caso, com consequência pior. Custo em CI: **zero** — nenhum fixture gerável
usa `visibility` (só [pizzeria/sales/read.ds](../../../testdata/projects/pizzeria/sales/read.ds):35-49, já em
`KNOWN_UNGENERATABLE` por outro motivo, [ci.yml](../../../.github/workflows/ci.yml):69).

**(2) Checar o *conteúdo* do bloco com as regras que já existem.** Resolver
nomes (REQ-4/REQ-9) e checar acesso a membro (REQ-12) dentro das condições de
`visibility`, exatamente como já se faz para `access`. Isso **não é regra
nova**: a [§4.2.3](../steerings/domainscript-spec-v7/04-domain-core.md) lista
`visibility` de View entre os contextos onde ler o envelope do evento é *erro
de compilação*, e a [§4.3.1](../steerings/domainscript-spec-v7/04-domain-core.md)
diz que `caller.id` fora de `access`/`visibility` é *erro de compilação* — as
duas pressupõem que o compilador enxerga dentro do bloco. Hoje ele não
enxerga; aplicar ali os diagnósticos que já existem é correção de omissão, não
comportamento fora da spec. Efeito prático imediato: um `visibility` com typo
deixa de ser aceito em silêncio.

Com (1)+(2) a lacuna deixa de ser silenciosa **sem** antecipar decisão nenhuma
de semântica de serialização.

## Solução proposta

Três peças, e a decisão central é **onde a filtragem acontece**.

**Onde filtrar: na borda, sobre o valor da View, nunca dentro da Query.**
Argumento decisivo é o cache. Uma Query pode declarar `cache { ttl }` (§15 —
`GetWallet` do wallet declara, `GetAvailableMenu` do pizzeria declara) e o
wrapper de cache embrulha a função da Query
([decl_query.go](../../../codegen/decl_query.go):276-281). Se a filtragem fosse aplicada dentro da
Query, o valor **já filtrado para o caller A** entraria no cache e seria
servido ao caller B — a implementação da feature de segurança viraria um vazamento
pior que o gap atual. Filtrando depois do cache, o valor cacheado é sempre o
completo (dado de servidor) e a decisão é por requisição. Além disso a §6.2
exige **omissão**, não `null`: zerar o campo na projeção
([`projectFieldAssignments`](../../../codegen/decl_query.go):709) produziria `"balance":
{...zero...}` no JSON, que é literalmente o que a spec proíbe.

**Peça 1 — runtime.** Um tipo novo em [rtsrc](../../../codegen/rtsrc): `FieldSet`, lista ordenada de
pares `(chave, valor)` com `MarshalJSON` que preserva a ordem de inserção.
Necessário porque `map[string]any` reordenaria os campos alfabeticamente
(determinístico, mas muda a forma do JSON) e `omitempty` conflaria "zero" com
"ausente". Depende só da stdlib (NFR-12 intacta).

**Peça 2 — a View.** Para cada `ViewDecl` com `Visibility`, `emitViewDecl`
([decl_view.go](../../../codegen/decl_view.go):61) emite, além do struct de hoje (inalterado):

```go
// VisibleFor devolve a forma serializável de OrderVW para caller (§6.2):
// campos cuja regra não é satisfeita são OMITIDOS.
func (v OrderVW) VisibleFor(caller runtime.Caller) runtime.FieldSet
```

Corpo: um `Add` por campo na ordem declarada; campo **não listado** entra
sempre ("visíveis a qualquer caller autorizado pela Query", §6.2); campo
listado entra sob `if <condição lowerizada>`. A condição reusa
`lowerAccessCondition`/`lowerCallerVOEquality`/`lowerCallerHasRole`
([decl_aggregate.go](../../../codegen/decl_aggregate.go):375-528, mesmo pacote, já testadas por
`access_hasrole_test.go`) — `visibility` e `access` têm a **mesma** linguagem
de predicado, e reusar o lowering garante que as duas convirjam juntas quando
o ciclo de `CallerId`/`ref T` chegar, em vez de divergirem. `caller` nulo
(gRPC hoje não injeta caller: [grpc.go](../../../codegen/grpc.go):415) e caller anônimo →
`false`, **fail-closed**, exatamente a semântica que a §4.3.1 fixa para
`caller.id`.

Restrição que torna a peça implementável e verificável: **todo `self.X` de um
predicado precisa existir como campo da View emitida** (após o achatamento de
VO composto de REQ-34). É o que faz `VisibleFor` ser função pura de `(valor da
View, Caller)` — sem `ctx`, sem store, sem recarregar o Aggregate. Um `self.X`
que não materializa na View é erro de geração claro. Para `View X From T` a
projeção é 1:1 com o `state` ([decl_view.go](../../../codegen/decl_view.go):120-140), então o caso
canônico da §6.2 fecha; o caso `self.id` sob identidade implícita depende do
ciclo de §4.3.1 (ver Bloqueios).

**Peça 3 — as bordas.** [http.go](../../../codegen/http.go):997 (`emitPlainViewEncode`) passa a
encodar `result.VisibleFor(caller)` quando o retorno da Query é uma View com
`visibility`, e `[]runtime.FieldSet` mapeado item a item quando é
`List<V>`/`AppendList<V>`. Nada mais muda: `caller` já está no escopo do
handler ([http.go](../../../codegen/http.go):584) e o `runtime` já está importado. Para gRPC
([grpc.go](../../../codegen/grpc.go):421), onde o handler de Query nem injeta caller e a
resposta vai por codec proto (que não tem "campo ausente" para escalar),
**erro de geração explícito** enquanto a semântica de omissão em proto não for
decidida — mesma disciplina da mitigação (1), aplicada a um caminho que
continua sem suporte.

## Alternativas descartadas

- **Filtrar na Query, na projeção.** Perde por três motivos independentes:
  envenena o cache de Query (acima), produz `null`/zero em vez de omissão, e
  espalha a decisão de segurança por todos os caminhos de projeção
  (`load … as V`, `list … as V` hoisteado, Projection) em vez de um ponto só.
- **Filtrar via `MarshalJSON` na própria View, lendo o caller de um campo
  oculto preenchido na projeção.** Resolve a omissão, mas o valor passa a
  carregar a decisão de *um* caller — mesma armadilha do cache, agravada por
  ser invisível no tipo.
- **`omitempty` nos campos protegidos.** Não distingue "oculto" de "zero
  legítimo" (saldo 0, string vazia) e vaza informação pelo próprio padrão de
  ausência.
- **Warning de geração "visibility declarado e ignorado".** É o que a revisão
  desta issue já rejeitou, e com razão: diagnóstico que a §27 não prevê. A
  recusa de geração da mitigação (1) obtém o mesmo efeito de "tirar o
  silêncio" sem tocar na superfície de diagnósticos da linguagem.
- **Implementar "negar sempre" (todo campo listado sempre omitido) como
  aproximação fail-closed.** Não é a §6.2, quebra funcionalidade sem avisar e
  fixa uma semântica errada em goldens.

## Raio de alcance

- **Goldens: nenhum muda.** Nenhuma View de `wallet`/`shop`/`notes`/demais
  fixtures geráveis declara `visibility`, e toda a emissão nova é condicionada
  a `len(decl.Visibility) > 0` — byte-identidade (NFR-13) preservada para todo
  programa existente. Golden novo só para o fixture novo (abaixo).
- **Runtime vendorado:** `runtime/fieldset.go` passa a ser copiado para todo
  projeto gerado. Nenhum teste fixa a lista de arquivos de `rtsrc.Sources()`
  (os testes iteram; [rtsrc_test.go](../../../codegen/rtsrc/rtsrc_test.go):37-44 só exige presença de um
  subconjunto), e a deleção de órfãos/idempotência de `GenerateProject` já
  lida com arquivo novo.
- **CI:** o job `fixtures` roda `dsc check` em **todos** os projetos e `dsc
  gen` + `go build` nos que não estão em `KNOWN_UNGENERATABLE`
  ([ci.yml](../../../.github/workflows/ci.yml):57-97). A mitigação (2) é o único risco real: se a
  resolução/checagem dentro de `visibility` produzir qualquer diagnóstico
  sobre [pizzeria/sales/read.ds](../../../testdata/projects/pizzeria/sales/read.ds):44-48, a **fase 1** quebra para o
  pizzeria. Por leitura, os três predicados de lá resolvem (`customer`,
  `phone`, `total`, `customerId` existem em `OrderVW`; `caller.*` é receptor
  não tipado, pulado pela anti-cascata de REQ-12.4) — mas isso precisa ser o
  canário da task, não uma suposição.
- **`docs/examples/03-aplicacao-e-leitura/read.ds`:28-35** é alvo de
  conformidade e **não** se toca: ele já está na forma da spec
  (`View WalletDetailVW From Wallet`, `caller.id == self.id`).
- **Testes gerados de `*.test.ds`** chamam a função da Query diretamente e
  continuam vendo a View completa — coerente com "visibility é regra de
  borda"; nenhum golden de `gentest` muda.
- **Fixture:** o pizzeria **não serve** como prova de ponta a ponta (não gera,
  por ISSUE-7) e, pior, seu predicado `caller.id == self.customerId` compara
  `CallerId` com um **ValueObject**, o que a §4.3.1 revisada torna **erro de
  compilação**. Ele é artefato v6 e não é o alvo semântico — precisa de um
  fixture novo, pequeno e gerável.

## Bloqueios

Nenhum impede a mitigação imediata; todos impedem *fechar* a issue.

1. **`visibility` exige `From T`?** A §4.3.1 diz que em `visibility` "`self` é
   a instância projetada" e fala em "View sobre `T`"; a §6.2 exemplifica só
   com `View WalletSummaryVW From Wallet`. Uma View **sem** `From` (o caso do
   pizzeria, e o único que os fixtures exercitam) não tem `T`: a fonte é
   escolhida por cada Query, na cláusula `as V`, e duas Queries podem projetar
   a mesma View de Aggregates diferentes. A spec precisa decidir: `visibility`
   sem `From` é erro, ou `self` denota a própria View? **Esta é a decisão que
   trava a Peça 2.**
2. **`caller.id` contra o quê, hoje?** A §4.3.1 fixa `caller.id : CallerId`
   comparável **só** contra `ref T`. Nem `CallerId` nem `ref T` nem a
   identidade implícita existem na implementação — `access` compara hoje
   `WalletId(caller.ID()) == w.state.Id` sobre um campo `id` **declarado no
   `state`** ([decl_query.go](../../../codegen/decl_query.go):908-919). Implementar `visibility`
   reusando esse lowering herda a divergência de `access` (não cria uma nova) e
   converge de graça quando o ciclo de identidade chegar — mas é preciso dizer
   isso explicitamente na task, ou o próximo leitor vê `visibility` "aprovando"
   uma comparação que a spec revisada proíbe.
3. **Campo oculto e cache (§15).** A spec não diz se o resultado cacheado de
   uma Query é por caller. A rota proposta (filtrar depois do cache) responde
   pela via da implementação, mas convém a §6.2 dizer que `visibility` é regra
   de **serialização**, não de resultado — senão a próxima implementação
   escolhe o outro lado e vaza.
4. **Borda gRPC / proto.** "Omitido da serialização" não tem tradução direta
   em proto3 para escalar. Sem decisão da spec, o caminho é recusar a geração
   (acima).
5. **Nome listado que não é campo da View.** A §6.2 não diz o que é
   `visibility { xpto requires … }` com `xpto` inexistente. Como erro de
   *geração* isso é seguro e não inventa linguagem; como diagnóstico do
   front-end seria regra nova — decisão da spec.

## Fatiamento sugerido

| # | Task | `target_files` |
|---|------|----------------|
| **V1** | Recusa de geração fail-closed: `emitViewDecl` falha quando `len(decl.Visibility) > 0`, com mensagem citando §6.2 e esta issue. Teste negativo (View com `visibility` → erro) + positivo (View sem → Go idêntico ao golden atual). Nota em `ci.yml` acrescentando o segundo motivo do pizzeria. | [codegen/decl_view.go](../../../codegen/decl_view.go), [codegen/decl_view_test.go](../../../codegen/decl_view_test.go), [.github/workflows/ci.yml](../../../.github/workflows/ci.yml) |
| **V2** | Front-end deixa de ser cego: `resolveDeclBodies` ganha caso `*ast.ViewDecl` (novo `construct` semeando `self`/`caller`, espelhando `constructAccess`) e `checkDeclMembers` ganha o caso equivalente, com `self` tipado pelo `From` quando houver. Par positivo/negativo por regra (NFR-4). **Canário: `dsc check` do pizzeria tem de seguir limpo.** | [resolver/resolve_body.go](../../../resolver/resolve_body.go), [resolver/receivers.go](../../../resolver/receivers.go), [sema/rules_typecheck.go](../../../sema/rules_typecheck.go), testes dos dois pacotes |
| **V3** | `runtime.FieldSet` (ordenado, `MarshalJSON` estável) no runtime vendorado, com teste próprio em `rtsrc`. Sem nenhum consumidor ainda. | [codegen/rtsrc/fieldset.go.txt](../../../codegen/rtsrc), [codegen/rtsrc/runtime_test.go.txt](../../../codegen/rtsrc/runtime_test.go.txt) |
| **V4** | `VisibleFor` na View: emissão condicional a `Visibility`, reusando `lowerAccessCondition`; regra "todo `self.X` precisa ser campo da View" como erro de geração; remove a recusa de V1 para as formas suportadas. Golden novo. | [codegen/decl_view.go](../../../codegen/decl_view.go), [codegen/decl_view_test.go](../../../codegen/decl_view_test.go), `codegen/testdata/*.golden` |
| **V5** | Borda: `emitPlainViewEncode` encoda `VisibleFor(caller)` (escalar e lista) **depois** do cache; gRPC recusa explicitamente. Fixture novo, pequeno e **gerável**, em `testdata/projects/`, exercitando um campo oculto e um visível ponta a ponta (o job `fixtures` o pega sozinho, pelo glob). | [codegen/http.go](../../../codegen/http.go), [codegen/grpc.go](../../../codegen/grpc.go), [codegen/http_test.go](../../../codegen/http_test.go), `testdata/projects/<novo>/` |

V1 e V2 são independentes entre si e podem ir em qualquer ordem; V4 depende de
V3 e da decisão do Bloqueio 1; V5 depende de V4.
