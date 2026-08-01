# `access { requires ... }` em UseCase não é parseado (§14)
- SPEC: [transpilador](../specs/transpilador/requirements.md)
- TASK: descoberto ao escrever os exemplos spec-first
  ([review-v7.md §A-9](../steerings/review-v7.md))
- DESCRIPTION: A [§14](../steerings/domainscript-spec-v7/14-multi-tenancy.md)
  (Multi-Tenancy) fecha com o exemplo canônico de acesso
  cross-tenant, e ele declara um bloco `access` **dentro do UseCase**:

  ```ds
  UseCase GenerateGlobalReport handles GlobalReportCmd {
      tenancy: cross_tenant
      access { requires caller.hasRole("super_admin") }
      execute {
          allWallets = list Wallet take 10000
      }
  }
  ```

  O parser só modela `access` em Aggregate (`ast.AggregateDecl.Access`);
  `ast.UseCaseDecl` não tem o campo, e `parseUseCase` rejeita o membro.
  Rodando o trecho acima verbatim com o `dsc` do HEAD:

  ```
  5:5:  error: membro de UseCase inesperado: IDENT
  5:14: error: membro de UseCase inesperado: IDENT
  5:29: error: membro de UseCase inesperado: .
  6:5:  error: esperava uma declaração de topo, encontrei IDENT
  ```

  Isolado: `tenancy: cross_tenant` sozinho passa limpo — é especificamente o
  bloco `access` que quebra.

  **Impacto.** A §14 exige que o opt-in cross-tenant venha acompanhado de role
  privilegiada ("Cross-tenant opt-in: `tenancy: cross_tenant` + role
  privilegiada + auditoria automática + warning"). A regra de compilação do
  opt-in existe
  ([`checkCrossTenantOptIn`](../../../sema/rules_crossfile.go#L106),
  [rules_crossfile.go](../../../sema/rules_crossfile.go)) e o warning de
  auditoria também (`checkCrossTenantAudit`), mas **a sintaxe que
  expressaria a exigência de role não existe** — hoje só dá para declarar o
  opt-in, não a proteção que a spec pede junto.

  **Escopo a confirmar ao implementar.** A spec mostra esta forma **uma única
  vez**, de passagem, no exemplo da §14; a
  [§5.2](../steerings/domainscript-spec-v7/05-application-layer.md)
  (UseCases) não menciona `access`. Então dá para implementar o que está
  escrito (`access { requires
  <condição> }` num UseCase, mesma gramática de condição do bloco `access` do
  Aggregate), mas vale checar se o alcance pretendido é maior — por exemplo, se
  Query também deveria aceitá-lo. Se a resposta não estiver no texto, é caso de
  issue de revisão de spec separada, não de palpite.

  **Não é lacuna do spec** para o que está escrito: a forma é inequívoca. É
  defeito de código.
- SOLVED: FALSE

# Solução proposta

## Veredito

A issue continua real e o repro é exato. Rodando o trecho da
[§14](../steerings/domainscript-spec-v7/14-multi-tenancy.md#L21) verbatim com o
`dsc` do HEAD (arquivo fora do repositório, `dsc check`):

```
3:5:  error: membro de UseCase inesperado: IDENT
3:14: error: membro de UseCase inesperado: IDENT
3:29: error: membro de UseCase inesperado: .
3:37: error: membro de UseCase inesperado: (
3:51: error: membro de UseCase inesperado: )
4:5:  error: esperava uma declaração de topo, encontrei IDENT
6:5:  error: esperava uma declaração de topo, encontrei }
```

O mesmo UseCase sem o bloco `access` fecha limpo (só o warning de auditoria
cross-tenant), confirmando o isolamento que a issue afirma. As demais
afirmações também se sustentam nos arquivos de hoje:
[`ast.UseCaseDecl`](../../../ast/decl.go#L199-L212) não tem campo `Access`
(tem `Timeout`/`Idempotency`/`Tenancy`/`Execute`);
[`parseUseCase`](../../../parser/parse_decl.go#L784-L834) reconhece só esses
quatro membros e cai no `default` de
[parse_decl.go:827](../../../parser/parse_decl.go#L827);
`Access []*ast.AccessRule` existe apenas em
[`ast.AggregateDecl`](../../../ast/decl.go#L167-L177) e, como `Visibility`, em
[`ast.ViewDecl`](../../../ast/decl.go#L229-L235). A regra de opt-in citada
segue viva e no lugar apontado —
[`checkCrossTenantOptIn`](../../../sema/rules_crossfile.go#L106) e
[`checkCrossTenantAudit`](../../../sema/rules_warnings.go#L110) —, e o
closed-by-default de
[`checkAggregateAccess`](../../../sema/rules_domain.go#L65) continua sendo
regra **só de Aggregate** (Handle sem entrada homônima).

**Achado novo, não registrado na issue:**
[`parseAccessBlock`](../../../parser/parse_decl.go#L906-L923) lê **nome
primeiro** e trata `requires` como opcional, então a forma nua da §14, escrita
dentro de um **Aggregate**, é aceita em silêncio como uma regra chamada
`"requires"` — verificado: `access { requires caller.hasRole("x") }` num
Aggregate não produz erro de sintaxe algum, e o `Handle Create` seguinte é
acusado de "não tem entrada no bloco access", provando que a condição foi
arquivada sob o nome errado. É a armadilha exata que uma correção preguiçosa
(reusar `parseAccessBlock` no UseCase) reproduziria com `Name == "requires"` em
vez de `Name == ""`.

## Causa raiz

O bloco `access` foi modelado como propriedade do Aggregate — nó, parser,
resolver, sema e codegen assumem "uma regra por Handle, indexada por nome" — e
a §14 usa uma segunda forma, **anônima** e presa ao procedimento inteiro, que
nenhuma dessas camadas prevê.

## Solução proposta

Atravessa as três fases; a fronteira entre o que dá para fazer e o que não dá
está toda na **semântica**, não na sintaxe (ver *Bloqueios*).

**1. `ast` + `parser` (sintaxe pura, NFR-6).** Acrescentar
`Access []*ast.AccessRule` a `ast.UseCaseDecl` (novo parâmetro de
`NewUseCaseDecl`; campo `nil` no caso comum, o que preserva byte-identidade de
tudo que existe hoje). No `parseUseCase`, um `case p.atIdentLit("access")` que
chama uma função **nova**, `parseRequiresBlock()`, e **não** `parseAccessBlock`:
a gramática do UseCase é `access { requires <expr> }` — sem nome de regra —,
então a função exige literalmente `requires` e produz `ast.NewAccessRule("",
cond, span)`. Reusar `parseAccessBlock` aqui é o erro a evitar (vira
`Name: "requires"`, ver o achado acima). Na mesma task, corrigir
`parseAccessBlock` para **exigir** o nome do Handle e recusar a forma nua
(`p.errorf` + recuperação), fechando a aceitação silenciosa no Aggregate: as
duas gramáticas passam a ser disjuntas e cada erro fica no lugar certo. O
parser continua sem saber regra semântica nenhuma: aceita N linhas `requires`,
aceita condição arbitrária, aceita `access` em UseCase sem `tenancy`.

**2. `resolver` + `sema` (nomes e tipos, sem regra de negócio nova).** Em
[`receivers.go`](../../../resolver/receivers.go#L32), um construto novo
`constructUseCaseAccess: {"caller"}` — **só** `caller`. Nada de `self` (não há
Aggregate em escopo) e nada de `cmd` (a spec não o coloca ali); a consequência
é que `self.id`/`cmd.x` numa condição de UseCase caem em `E100: nome não
declarado`, que é a recusa conservadora correta enquanto o texto não decidir.
Em [`resolve_body.go`](../../../resolver/resolve_body.go#L105), o `case
*ast.UseCaseDecl` passa a resolver também as condições, no molde do que o
`case *ast.AggregateDecl` já faz em
[resolve_body.go:98-104](../../../resolver/resolve_body.go#L98-L104)
(`NewScope` + `seedReceivers` + `resolveExpr`, porque condição é `Expr`, não
`Block`). Em [`rules_typecheck.go:82`](../../../sema/rules_typecheck.go#L82) e
[`rules_compat.go:85`](../../../sema/rules_compat.go#L85), estender os
respectivos `case *ast.UseCaseDecl` com um `checkMembersInExpr`/
`checkCompatInExpr` sobre cada condição, com escopo **vazio** (o análogo do que
o Aggregate faz com `self` semeado —
[rules_typecheck.go:57-61](../../../sema/rules_typecheck.go#L57-L61)); `caller`
não é semeado hoje em lugar nenhum e por isso não gera falso positivo.
`astutil.DeclBlocks` não muda: condição é expressão, e o Aggregate já
estabelece esse precedente.

**3. `codegen` (o ponto inteiro do bloco: negar).** Em
[`emitUseCaseDecl`](../../../codegen/decl_usecase.go#L317-L326), logo depois de
`caller, _ := runtime.CallerFrom(ctx)`
([decl_usecase.go:322](../../../codegen/decl_usecase.go#L322)) e **antes** de
`emitCrossTenantBypass` e do `uow.Run`
([decl_usecase.go:346](../../../codegen/decl_usecase.go#L346)), emitir o mesmo
guard que o Handle já emite em
[decl_aggregate.go:250-253](../../../codegen/decl_aggregate.go#L250-L253):
`if !(<cond>) { return runtime.ErrForbidden }`. A condição sai de
[`lowerAccessCondition`](../../../codegen/decl_aggregate.go#L386-L428) **sem
mover nada** — os dois arquivos são `package codegen` —, o que traz de graça o
`and`/`or` recursivo, a conversão VO de igualdade e
[`lowerCallerHasRole`](../../../codegen/decl_aggregate.go#L508-L528), que é
exatamente a única forma que a §14 escreve. A ordem importa: negar antes do
bypass, senão um caller recusado ainda assim dispara a suspensão do filtro de
tenant e a linha de auditoria. `runtime.ErrForbidden` já é `BusinessError`
reservado e já mapeia para 403 em HTTP
([http.go:1119](../../../codegen/http.go#L1119)) e `PermissionDenied` em gRPC
([grpcrt/status.go.txt:45](../../../codegen/grpcrt/status.go.txt#L45)); a borda
HTTP sempre injeta um `Caller` no ctx (`devCallerFromRequest`), então o guard
não desreferencia interface nula. Duas interações a documentar no código:
(a) com `idempotency`, o guard fica na função **interna** (`innerName`), e a
negativa passa a ser cacheada como qualquer outro resultado — comportamento já
observável em [idempotency_test.go:465](../../../codegen/idempotency_test.go#L465);
(b) `len(Access) > 1` e qualquer forma que `lowerAccessCondition` não saiba
traduzir devem virar **erro de geração explícito**, no idioma que
`parseInterfaceTenantPlan` já usa ("erro claro, nunca um extractor
silenciosamente vazio") — nunca guard omitido.

O que **não** entra: nenhuma regra de compilação nova (closed-by-default de
UseCase, exigência de role no `cross_tenant`, `caller.id` em UseCase). Ver
*Bloqueios*.

## Alternativas descartadas

- **Reusar `parseAccessBlock` no UseCase.** Parseia por acidente e arquiva a
  regra como `Name: "requires"`; qualquer código que casar nome (o padrão de
  [`findAccessRule`](../../../codegen/decl_aggregate.go#L191)) passa a depender
  de uma string mágica. Perde por esconder o bug que já existe no Aggregate.
- **Parsear e ignorar (só as fases 1 e 2).** É precisamente o defeito já
  registrado em
  [visibility-de-view-nao-implementado](visibility-de-view-nao-implementado.md)
  e em [review-v7.md §B-4](../steerings/review-v7.md): controle de segurança
  aceito e silenciosamente inerte. Se a fase 3 não puder acompanhar, o estado
  correto **não** é o silêncio: é erro de geração ao encontrar o bloco.
- **Tratar `access` de UseCase como açúcar para uma regra no Aggregate
  alvo.** Um UseCase toca N Aggregates (ou nenhum, como o `list Wallet` da
  §14, que nem carrega instância); não há Handle a que ancorar. Sem apoio no
  texto.
- **Estender já para Query/Policy/Worker/Saga.** A spec mostra a forma só em
  UseCase; a [§6.2](../steerings/domainscript-spec-v7/06-read-side.md) dá
  `visibility` à View e nada dá a Query. Implementar mais que o texto viola a
  regra da fonte da verdade nos dois sentidos.
- **Exigir `and` implícito entre múltiplas linhas `requires`.** Semântica
  inventada; vira erro de geração até a spec decidir.

## Raio de alcance

Pequeno e bem contido. **Nenhuma** fixture de `testdata/projects/` usa `access`
em UseCase — as cinco ocorrências (`wallet:91`, `shop/orders:34`,
`pizzeria/{kitchen:89,sales:167,sales:216}`) são todas de Aggregate,
verificado —, logo nenhum golden existente muda e a byte-identidade (NFR-13)
fica intacta: `Access == nil` percorre exatamente o caminho de hoje. O job
`fixtures` do CI não é afetado enquanto nenhuma fixture declarar o bloco; para
cobrir a fase 3, prefira **novo** golden em `codegen/` (ou fixture nova) a
editar `wallet`/`shop`, que arrastariam goldens sem necessidade.
[`docs/examples/08-tenancy-e-limites/tenancy.ds:37`](../../../docs/examples/08-tenancy-e-limites/tenancy.ds#L37)
deixa de ser erro de sintaxe (não é validado por CI — é alvo de conformidade).
Testes novos, pelo par positivo/negativo da NFR-4: parser (forma da §14 parseia
com `Name == ""`; forma nomeada em UseCase e forma nua em Aggregate viram erro
de sintaxe), sema (`caller.hasRole(...)` silencioso; `self.id` na condição →
`E100`), codegen (golden do guard + teste que compila e roda a função gerada
negando um caller sem o papel, no molde de
[decl_aggregate_test.go:318](../../../codegen/decl_aggregate_test.go#L318)).
A correção de `parseAccessBlock` é a única com risco de regressão sobre
programas existentes, e ele é nulo: as cinco fixtures usam a forma nomeada.

## Bloqueios

A **sintaxe** é implementável — a §14 imprime a forma e ela é inequívoca. A
**semântica** não está escrita, e três decisões precisam de revisão de spec
antes de virarem código:

1. **`caller.id` num `access` de UseCase — contradição direta na §4.3.1.** A
   tabela de `CallerId`
   ([04-domain-core.md:382-383](../steerings/domainscript-spec-v7/04-domain-core.md))
   diz que a comparação de vínculo vale "em `access` ou `visibility`" e, na
   linha seguinte, que `caller.id` em "UseCase" é erro de compilação. Um
   `access` de UseCase cai nas duas. Pior: a mesma seção descreve o `access` do
   Aggregate como "**única** posição, com `visibility`, em que `caller.id`
   compara" ([04-domain-core.md:339](../steerings/domainscript-spec-v7/04-domain-core.md)),
   e a §4.3.1 foi escrita em 2026-07-31 assumindo que `access` é bloco de
   Aggregate. E se for legal, contra o quê compara? Não há `self` num UseCase;
   o único operando `ref T` disponível seria `cmd.<campo>`, e **nenhuma seção
   diz que `cmd` está em escopo numa condição de `access`**. A spec precisa
   decidir: `caller.id` é legal ali, e com que operandos em escopo (`caller`
   só? `caller` + `cmd`?).
	**nota do desenvolvedor:** realmente esta declaração é contraditória, caller.id deve ser acessível     (apenas leitura) em todo o escopo de execução da request
2. **`access` de UseCase é closed-by-default? E a §14 exige a role?** A §14
   afirma que o opt-in cross-tenant é "`tenancy: cross_tenant` + role
   privilegiada + auditoria automática + warning", mas a
   [§25.11](../steerings/domainscript-spec-v7/25-compilation-rules.md) só
   normatiza duas linhas — acesso cross-tenant sem opt-in → ❌, UseCase
   cross-tenant declarado → ⚠️ — e **nenhuma** exige o bloco `access`. O
   closed-by-default que a §25.5 escreve é o do Aggregate ("Handle sem entrada
   no `access`"), e a [§5.2](../steerings/domainscript-spec-v7/05-application-layer.md)
   não menciona `access`. Falta decidir: UseCase sem `access` é aberto (o
   comportamento de hoje) ou negado? `tenancy: cross_tenant` sem `access` é
   erro, warning ou nada? Mais de uma linha `requires` é legal, e com que
   composição?
	 **nota do desenvolvedor:** UseCase sem access é open
3. **`caller` não tem definição normativa em lugar nenhum** — e este é o
   bloqueio mais amplo, porque alcança o Aggregate também. A §14 diz que é
   "ambient context", e `caller.authenticated`/`caller.hasRole("...")` aparecem
   **só em exemplos** (§2.5, §4.3, §6.2, §14). Nenhuma seção declara o tipo de
   `caller`, seus membros, a assinatura de `hasRole`, o significado de
   `authenticated` ou de onde vêm os papéis (a
   [§11](../steerings/domainscript-spec-v7/11-interface.md) não tem bloco de
   auth). Pior: a [§2.8](../steerings/domainscript-spec-v7/02-type-system.md) é
   declarada **autoridade fechada** sobre "o que se pode invocar em um valor" e
   não lista `caller`; ao pé da letra, `caller.hasRole("super_admin")` — a
   única condição que a §14 escreve — é chamada fora do catálogo, isto é, erro
   de compilação. Recomendo **abrir issue de revisão de spec própria** para
   isso (`§2.8` + `§4.3.1` + `§11`): a §4.3.1 tipou `caller.id` e deixou os
   outros dois membros sem tipo. Não bloqueia a rota proposta — a fase 3 só
   estende a um sítio novo o mecanismo (`Caller.HasRole`) que o Aggregate já
   emite hoje —, mas bloqueia qualquer type-check conformante da condição, e
   deve estar registrado antes de alguém tentar escrevê-lo.
	**nota do desenvolvedor:** o caller é uma estrutura montada automaticamente por um miniframework interno, seu papel é trazer informações a respeito de quem fez a requisição atual, como tokens, RBAC access, etc. Realmente é preciso desenvolver melhor a especificação desta feature.

## Fatiamento sugerido

Quatro tasks, em ordem de dependência. As três primeiras são implementáveis
contra o texto atual; a quarta nasce `blocked`.

1. **`feat(parser): bloco access anônimo em UseCase`** — campo
   `Access []*ast.AccessRule` em `ast.UseCaseDecl`, `parseRequiresBlock()`
   nova, `case p.atIdentLit("access")` em `parseUseCase`, e `parseAccessBlock`
   passando a exigir o nome do Handle (fecha a aceitação silenciosa no
   Aggregate). Testes de parser positivo/negativo, incluindo o span e
   `Name == ""`. `target_files`: `ast/decl.go`, `parser/parse_decl.go`,
   `parser/parse_decl_test.go`.
2. **`feat(resolver,sema): resolução e checagem da condição de access de
   UseCase`** — `constructUseCaseAccess: {"caller"}`, resolução da condição no
   `case *ast.UseCaseDecl`, e a extensão dos dois checkers. Testes: condição
   com `caller.hasRole` silenciosa; `self.id`/`cmd.x` → `E100`.
   `target_files`: `resolver/receivers.go`, `resolver/resolve_body.go`,
   `sema/rules_typecheck.go`, `sema/rules_compat.go`,
   `sema/rules_access_test.go`, `resolver/*_test.go`.
3. **`feat(codegen): guard de access em UseCase (fail-closed)`** — guard com
   `runtime.ErrForbidden` antes do bypass cross-tenant e do `uow.Run`, reusando
   `lowerAccessCondition`; erro de geração explícito para `len(Access) > 1` e
   para condição não traduzível. Golden novo + teste que compila e executa a
   negativa. `target_files`: `codegen/decl_usecase.go`,
   `codegen/decl_usecase_test.go`, `codegen/testdata/**` (golden novo).
4. **`blocked` — regras de compilação do `access` de UseCase** — closed-by-
   default, exigência de role no `tenancy: cross_tenant`, legalidade de
   `caller.id` e escopo de `cmd`, múltiplas linhas `requires`. Só depois de a
   spec responder aos itens 1 e 2 de *Bloqueios*; abrir em paralelo a issue de
   revisão do item 3 (definição normativa de `caller`).
