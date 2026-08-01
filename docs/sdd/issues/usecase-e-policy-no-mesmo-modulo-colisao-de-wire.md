# UseCase e Policy no mesmo módulo não geram (colisão de Wire) (ex-ISSUE-7)
- SPEC: [codegen](../specs/codegen/requirements.md)
- TASK: descoberto em [`testdata/projects/pizzeria`](../../../testdata/projects/pizzeria) (não estava no [gaps.md](../specs/codegen/gaps.md))
- DESCRIPTION: Um módulo que combina **`UseCase` E `Policy` no mesmo módulo**
  ainda não gera — `dsc gen` falha com "UseCase e Policy no mesmo módulo ainda
  não têm wiring combinado suportado (cada um gera seu próprio Wire —
  colidiriam); ver a doc de decl_policy.go". `generateModuleFiles`
  ([codegen.go](../../../codegen/codegen.go)) emitiria dois `func Wire(...)` no mesmo pacote Go
  (um de `emitUOWWireFunc`, outro de `emitPolicyWireFunc`), que colidem. Nem
  wallet nem shop exercitavam essa combinação; o módulo `Kitchen` do exemplo
  pizzeria (Claim/Finish via HTTP **e** criação reativa via Policy sobre
  `OrderPaid`) é o primeiro caso real — o próprio comentário no código já
  previa "fica para quando um exemplo real precisar disso". Bloqueia a geração
  do back-end do exemplo pizzeria (o front-end valida limpo). Fechar exige
  unificar o wiring: um único `Wire(...)` por módulo que registre tanto os
  UseCases (dispatcher/UoW) quanto as Policies (assinaturas de evento).

  EM ANDAMENTO (spec encerrada e sucedida): a spec `correcoes-issues-6-7-8`
  (Marco L, REQ-52 / §design 2 — arquivos removidos ao fechar, ver
  [`correcoes-issues-6-8-12/requirements.md`](../specs/correcoes-issues-6-8-12/requirements.md)
  para o ciclo sucessor) resolveu isto. Achado da análise de raiz: **o próprio
  código já resolve esta colisão em outros lugares** — `StartWorkers`,
  `WireQueryCache`, `WireOutboxStore`/`StartOutboxRelay` usam nome próprio
  em vez de um 2º `Wire`. Fix recomendado: um `Wire` unificado por módulo
  (`func Wire(u UnitOfWork, d Dispatcher)` no caso misto; casos puros
  byte-idênticos). O `Kitchen` do pizzeria é a fixture-âncora; ao fechar,
  `pizzeria` sai da lista `KNOWN_UNGENERATABLE` do CI
  (`.github/workflows/ci.yml`) e passa a gerar+compilar como wallet/shop.

  A task L1.1 já fechou a colisão de `Wire` em si (o escopo direto desta
  issue). **Porém não marcar esta issue como totalmente resolvida** enquanto
  [a issue sobre o pizzeria bloqueado por múltiplos defeitos de
  codegen](pizzeria-bloqueado-por-multiplos-defeitos-de-codegen.md) (o
  bloqueio real e maior de gerar o pizzeria de ponta a ponta, achado em
  L1.2) permanecer aberta.
- SOLVED: FALSE

# Solução proposta

> Análise de 2026-07-31 sobre `main` @ a6b239b. Desenho compartilhado com
> [a issue do `emit` em passo de Saga (M2.3)](m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files.md),
> que é a mesma pergunta vista do outro lado.

## Veredito

**A colisão de símbolo está fechada. Confirmado no código, não só no texto da
task.** O ramo misto existe e é o caminho normal:
[codegen.go:511](../../../codegen/codegen.go) (`mixed := hasUseCases &&
hasPolicies`), [codegen.go:626](../../../codegen/codegen.go)
(`emitUseCasesBytes(..., !mixed)` — usecases.go sai **sem** `Wire`),
[codegen.go:717-721](../../../codegen/codegen.go) (`emitPoliciesCombinedBytes`
no lugar de `EmitPolicies`), e
[decl_policy.go:648-673](../../../codegen/decl_policy.go)
(`emitCombinedWireFunc` emitindo `func Wire(u runtime.UnitOfWork, d
runtime.Dispatcher)` com `uow = u` como primeiro statement). O call site
correspondente monta a lista de argumentos por marca em
[codegen.go:1467-1476](../../../codegen/codegen.go). `go build ./...` passa e
`go test ./codegen/ -run TestGenerateMixedModuleWiresCombinedWireAndCompiles`
passa (única execução alvo desta análise) — o caso misto gera, compila e é
coberto por dois testes dedicados
([mixed_wire_test.go](../../../codegen/mixed_wire_test.go),
[mixed_wire_maincall_test.go](../../../codegen/mixed_wire_maincall_test.go)).

**O que sobrou**, em ordem de importância:

1. **A regra nunca foi escrita.** L1.1 consertou a *instância*
   (UseCase+Policy) sem fixar o *invariante* que a produziu. Resultado
   verificável: cinco meses de construtos depois, M2.3 bateu na mesma parede
   por outro ângulo — a Saga não tem `Wire` e o
   [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 mandou pendurar
   a fiação dela na `Wire` de Policy. Enquanto o invariante não estiver
   escrito, cada construto novo re-litiga a decisão. É o que a seção
   "Solução proposta" abaixo entrega.
2. **Documentação divergente do código.** A doc de
   [decl_policy.go:104-115](../../../codegen/decl_policy.go) ainda afirma que
   "codegen.go (generateModuleFiles) **recusa esse caso HOJE** com um erro de
   geração claro" e que combinar as duas infra "fica para quando um exemplo
   real precisar disso" — texto pré-L1.1, hoje falso. A doc de `moduleMarks`
   ([codegen.go:408-414](../../../codegen/codegen.go)) e a de
   `generateCmdMainFile` ([codegen.go:990-998](../../../codegen/codegen.go))
   também descrevem só UseCase/Policy/Worker.
3. **O vínculo com `pizzeria`**, que era a razão de não marcar esta issue como
   resolvida. Aqui há novidade **boa**, verificada hoje rodando `dsc gen
   testdata/projects/pizzeria`: dos cinco bloqueios listados em
   [a issue do pizzeria](pizzeria-bloqueado-por-multiplos-defeitos-de-codegen.md),
   a guarda F5/G3 sumiu (substituída pelo fan-out de M1.4/M1.5, ver a doc em
   [codegen.go:1018-1038](../../../codegen/codegen.go); o que restou é a guarda
   estreita de produtor **durável** + Dispatcher,
   [codegen.go:1217-1219](../../../codegen/codegen.go), e o `MainDb` de Kitchen
   é `"mongodb"`, decorativo — não é produtor durável), e os itens 1-3
   (`caller.hasRole` em `access`, builtin em `Apply`, `.add` sobre `List`) já
   não aparecem: a geração agora avança até `Kitchen/queries.go` e falha em
   **um único** ponto — `list <Aggregate>` em posição de expressão pura
   (`Query GetBoardTickets`). Ou seja: `pizzeria` segue em
   `KNOWN_UNGENERATABLE` ([ci.yml:69](../../../.github/workflows/ci.yml)) por
   um defeito que **nada tem a ver com wiring**, e nenhuma proposta desta issue
   o tira de lá. *(Registrado aqui de propósito: não editei aquela issue.)*

## Causa raiz

`Wire` é a única entrada da superfície cuja **assinatura depende do conteúdo
declarado** — `Wire(u UnitOfWork)`
([decl_usecase.go:89](../../../codegen/decl_usecase.go)), `Wire(d Dispatcher)`
([decl_policy.go:623](../../../codegen/decl_policy.go)), `Wire(u, d)`
([decl_policy.go:661](../../../codegen/decl_policy.go)) — e `main.go` monta a
chamada posicionalmente. Com n construtos que precisem de dependência isso é um
espaço de 2^n assinaturas; toda combinação nova é uma colisão ou uma reescrita
de goldens. Os cinco construtos que escolheram nome próprio
(`StartWorkers`, `WireQueryCache`, `WireMetrics`, `WireOutboxStore`,
`WireFileStorage`) nunca colidiram com nada.

## Solução proposta

**Nomes próprios por concern, com contrato uniforme** — a uniformidade está na
*forma* (uma função exportada por concern, uma dependência por chamada, ordem
de chamada fixa em `main.go`), não numa função única. Uma `Wire` única e
uniforme é precisamente o que já falhou.

### 1. O invariante a registrar

| Forma | Papel | Precedentes no código |
|---|---|---|
| `Wire(...)` | **congelada** nas três assinaturas de L1.1; **nunca ganha parâmetro novo** | [decl_usecase.go:89](../../../codegen/decl_usecase.go), [decl_policy.go:623](../../../codegen/decl_policy.go), [decl_policy.go:661](../../../codegen/decl_policy.go) |
| `Wire<Dependência>(dep)` | injeção pura de um seam do serviço | `WireOutboxStore` ([decl_policy.go:732](../../../codegen/decl_policy.go)), `WireFileStorage` ([decl_filestorage.go:46](../../../codegen/decl_filestorage.go)) |
| `Wire<Concern>(dep)` | injeção **e** registro (Subscribe) | `WireQueryCache`, `WireMetrics` ([decl_metric.go:444](../../../codegen/decl_metric.go)) |
| `Start<Concern>(ctx)` | goroutine de fundo | `StartWorkers`, `StartIdempotencyCleanup`, `StartOutboxRelay/Cleanup` |

Ordem de chamada em `cmd/<service>/main.go`: `WireOutboxStore` → `Wire` →
`WireQueryCache` → `WireMetrics` → `WireDispatcher` → `Wire2PC` →
`WireFileStorage` → `Start*`. Só a primeira seta é dependência real (`Wire` lê
`outboxStore`); o resto é ordem fixa por determinismo — e por isso **inserir um
construto novo nunca reordena os existentes** (raio de alcance O(1), NFR-13
preservado por construção). O custo de cada construto futuro passa a ser
"um campo em `moduleMarks` + um `if wt.hasX { e.Line(...) }`", em vez de "mais
um parâmetro em `Wire` + todo golden de `Wire`".

### 2. O seam que falta hoje: `WireDispatcher(d runtime.Dispatcher)`

O único item ausente da tabela é a injeção pura do `runtime.Dispatcher` do
serviço no módulo. Hoje ela é um efeito colateral escondido de `Wire`
(`policyDispatcher = d`, [decl_policy.go:754-761](../../../codegen/decl_policy.go)),
e é por isso que a Saga não a alcança. Proposta: um `codegen/wiring.go` que
emite `<pkg>/wiring.go` com `var moduleDispatcher runtime.Dispatcher` + `func
WireDispatcher(d runtime.Dispatcher)`, gated por uma marca nova
(`moduleMarks.needsDispatcherSeam`) — arquivo próprio pelo mesmo motivo e com o
mesmo precedente de `collections.go`
([codegen.go:682-690](../../../codegen/codegen.go)): um var de pacote que dois
emissores diferentes querem declarar. `policyDispatcher` e o futuro dispatcher
de Saga passam a ser esse único var.

A razão de nomear pela **dependência** e não pelo construto está na revisão da
spec de 2026-07-31:
[§5.3.2](../steerings/domainscript-spec-v7/05-application-layer.md) torna
`emit <ApplicationEvent>` legal em UseCase `execute`, Policy `execute`, Worker
`execute` **e** step de Saga — quatro emissores, quatro arquivos gerados, **uma
única dependência**. `WireSagas`/`WireUseCaseEvents`/`WireWorkerEvents` seriam
quatro setters exportados para injetar o mesmo objeto.

### 3. Módulo só-Saga

Detalhado na
[issue-irmã](m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files.md):
`needsDispatcherSeam` entra no predicado de salto de
[codegen.go:1109](../../../codegen/codegen.go), o que faz um módulo sem
UseCase/Policy/Worker virar `wireTarget` pela primeira vez, força
`needsDispatcher` e emite `<pkg>.WireDispatcher(dispatcher)`. Sem isso o var
fica nil — e `runtime.Dispatcher` é **interface**
([rtsrc/dispatcher.go.txt:10](../../../codegen/rtsrc/dispatcher.go.txt)), então
o primeiro `emit` panica em produção enquanto os testes gerados (que reatribuem
o var) passam.

### 4. Higiene junto

Corrigir a doc estale de [decl_policy.go:104-115](../../../codegen/decl_policy.go)
(descreve a recusa que L1.1 removeu) e trocar os **seis** `map[string]bool`
paralelos de `Generate`/`generateCmdMainFile`
([codegen.go:165-194](../../../codegen/codegen.go),
[codegen.go:1075](../../../codegen/codegen.go)) por um único
`map[string]moduleMarks` — byte-idêntico, e é o que mantém o custo do próximo
construto em um campo.

## Alternativas descartadas

- **Uma `Wire` uniforme com todas as dependências** (`Wire(u, d, ...)`, ou
  `Wire(deps ModuleDeps)` com struct de dependências): a variante posicional é
  a explosão 2^n descrita acima; a variante com struct unifica de verdade, mas
  reescreve **todos** os `func Wire` e **todos** os call sites de `main.go` de
  uma vez (11 goldens de `Wire` + 6 goldens de `main.go`), para depois ainda
  precisar de um campo por construto no struct — mesmo custo marginal do nome
  próprio, com um custo inicial enorme e uma quebra de byte-identidade
  gratuita.
- **Manter `Wire` como está e não escrever regra nenhuma** (status quo): é o
  que produziu M2.3 travada. O próximo construto (e
  [§5.3](../steerings/domainscript-spec-v7/05-application-layer.md) traz um)
  repete a discussão.
- **Nome próprio por construto para o Dispatcher** (`WireSagas`,
  `WireUseCaseEvents`, …): funciona, é o menor delta, e é o fallback aceitável
  — mas multiplica setters para uma única dependência assim que §5.3 chegar.

## Raio de alcance

- **Fechar só o que sobrou desta issue (regra + docs):** 0 goldens, 0 fixtures,
  0 bytes de saída. Só [decl_policy.go](../../../codegen/decl_policy.go)
  (comentários), [codegen.go](../../../codegen/codegen.go) (comentários) e
  [design.md](../specs/correcoes-issues-6-8-12/design.md).
- **Refactor de plumbing (`map[string]moduleMarks`):** 2 arquivos, saída
  byte-idêntica.
- **`WireDispatcher` + migração de `policyDispatcher`:** **4** dos 56 goldens
  de [codegen/testdata/](../../../codegen/testdata) (`policy_pricecheck`,
  `policy_ranking_orderby`, `policy_ticketsales_smartpartial`,
  `tests_policy_refunds`), mais [gentest.go](../../../codegen/gentest.go) e 4
  testes que reatribuem o var pelo nome. **0** dos 6 goldens de
  `cmd/<service>/main.go`. **0** projetos de
  [testdata/projects/](../../../testdata/projects) — nenhum declara Saga e a
  única Policy real (`shop/shipping/policy.ds`) é `execute { return }`, sem
  `emit`. Portanto **wallet e shop continuam byte-idênticos** e os módulos que
  só têm UseCase ou só têm Policy também: toda emissão nova é gated por uma
  marca falsa neles.
- **`KNOWN_UNGENERATABLE` ([ci.yml:69](../../../.github/workflows/ci.yml)):**
  inalterado — `pizzeria` não sai de lá com nada disto (ver o item 3 do
  Veredito).

## Bloqueios

- **Nenhum na spec da linguagem para *esta* issue.** A superfície de wiring é
  estrutura do Go gerado; a spec v7 não descreve nomes de função gerados, então
  a decisão é de [design.md](../specs/correcoes-issues-6-8-12/design.md), não da
  spec — não há aqui o caso "task diverge da spec ⇒ bloqueada".
nota do desensolvedor: descreva na spec regras para nomes de funções e campos gerados pelo compilador durante o build. Utilize a solução proposta: **Nomes próprios por concern, com contrato uniforme** — a uniformidade está na
*forma* (uma função exportada por concern, uma dependência por chamada, ordem
de chamada fixa em `main.go`), não numa função única.


- **A issue-irmã tem um bloqueio grave e ele respinga no fatiamento:**
  [§5.3.2](../steerings/domainscript-spec-v7/05-application-layer.md) tornou
  `emit <Event>` fora de Aggregate erro de compilação e `ApplicationEvent` (a
  forma legal) não existe na implementação (`grep -rn ApplicationEvent
  --include=*.go .` = zero). O **seam** pode ser entregue já; o **cliente
  Saga** dele não.
- **Não consegui verificar** se `pizzeria` geraria de ponta a ponta depois de
  resolvido o `list <Aggregate>`: a geração para no primeiro erro, então os
  módulos seguintes (`Sales`) nunca foram exercitados — a própria issue do
  pizzeria já registra essa suspeita.

## Fatiamento sugerido

Fatiamento completo (5 tasks, com `target_files`) na
[issue-irmã](m2-3-mecanismo-de-emit-em-passo-de-saga-exige-arquivos-fora-de-target-files.md#fatiamento-sugerido);
o recorte que fecha **esta** issue é:

1. **W1 — registrar o invariante:** a tabela da seção 1 acima em
   [design.md](../specs/correcoes-issues-6-8-12/design.md) §4.4 (e o resumo em
   [CLAUDE.md](../../../CLAUDE.md), seção "Back-end", se o dono do ciclo
   quiser), mais a correção da doc estale de
   [decl_policy.go:104-115](../../../codegen/decl_policy.go).
   `target_files`: `docs/sdd/specs/correcoes-issues-6-8-12/design.md`,
   `codegen/decl_policy.go`, `codegen/codegen.go` (só comentários).
2. **W2 — plumbing:** os seis mapas paralelos viram
   `map[string]moduleMarks`; saída byte-idêntica é o critério de conclusão.
   `target_files`: `codegen/codegen.go`, `codegen/codegen_test.go`.
3. **W3 — `WireDispatcher`:** novo `codegen/wiring.go`, marca
   `needsDispatcherSeam`, entrada em `wireTargets`/`needsDispatcher`/call site,
   migração de `policyDispatcher`. Não depende de §5.3.
   `target_files`: `codegen/wiring.go` (novo), `codegen/codegen.go`,
   `codegen/decl_policy.go`, `codegen/gentest.go`,
   `codegen/testdata/policy_*.go.golden`,
   `codegen/testdata/tests_policy_refunds.go.golden`,
   `codegen/gentest_policy*_test.go`.

Depois de W1-W3 esta issue está tecnicamente encerrada; o vínculo com
`pizzeria` deixa de ser argumento para mantê-la aberta, porque o bloqueio
remanescente daquele exemplo (`list <Aggregate>`) é ortogonal a wiring e já tem
issue própria.
