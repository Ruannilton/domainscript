# Tasks — Correções de dívida técnica (ISSUE-6, ISSUE-7, ISSUE-8)

## Como ler este plano

Marco L, manutenção — mesmo molde do Marco K. Três fases independentes
(L1/L2/L3), uma por issue. Cada task referencia o REQ `(REQ-n)` e a seção de
design `(§design x)`. Regras do CLAUDE.md: **uma task por vez**, commit atômico
com a árvore verde no escopo da task, par de testes positivo/negativo (NFR-4),
uma PR por task.

Ordem: **L1 → L2 → L3** (valor/concretude decrescente). As fases são
independentes; dentro de cada uma há dependência linear leve.

Convenção de commit (CLAUDE.md): `feat(codegen): …`, `fix(codegen): …`,
`fix(sema): …`, `chore(repo): …`.

---

## Fase L1 — Wire unificado: UseCase + Policy no mesmo módulo (ISSUE-7, REQ-52)

- [x] **L1.1** Emissão do `Wire` combinado para o módulo misto. (REQ-52.1/52.2/52.3,
  §design 2.2)
  - `codegen/codegen.go`: remover o guarda `if hasUseCases && hasPolicies`
    (linha ~502); rotear o caso misto para um `emitCombinedWireFunc` (novo) que
    emite um único `func Wire(u UnitOfWork, d Dispatcher)` fazendo `uow = u` +
    `policyDispatcher = d` + os `o.Subscribe(...)` das Policies. Casos puros
    (só UseCase / só Policy) seguem pelos emissores atuais, **byte-idênticos**.
  - `codegen/decl_usecase.go`/`decl_policy.go`: extrair as partes reusáveis (a
    escrita de `uow` e a assinatura das Policies) para o emissor combinado, sem
    alterar a saída dos casos puros.
  - **Testes pareados (NFR-4):** fixture sintética misto (UseCase+Policy, sem
    canal de saída) → gera, `func Wire(` único, `gentest.SmokeCompile` builda;
    módulo só-UseCase e módulo só-Policy → Go byte-idêntico ao de hoje.
  - DoD: escopo verde; `go build`/`go vet`/`gofmt` limpos.

- [x] **L1.2** Call site em `main.go` para o módulo misto. (REQ-52.4, §design 2.2)
  - `codegen/codegen.go` (`generateCmdMainFile`, ~1288): para um `wireTarget`
    misto, montar os args de `Wire` com a `UnitOfWork` e o `Dispatcher` (o
    `dispatcher`/`uow` já em escopo quando `needsDispatcher`). Módulos puros
    inalterados.
  - Confirmar que o `Kitchen` do pizzeria não esbarra na guarda F5/G3
    (produtor-de-canal + Dispatcher no mesmo service) — é UseCase+Policy local,
    sem canal próprio; se esbarrar, registrar como issue nova (REQ-52.7) e parar.
  - **Testes pareados:** `main.go` do service com módulo misto chama `Wire(uow,
    dispatcher)`; service com módulos puros byte-idêntico.
  - DoD: escopo verde.

> **ISSUE-12 (achado de L1.2):** `pizzeria` está bloqueado por CINCO defeitos
> independentes além da colisão de Wire (já fechada por L1.1) — a guarda F5/G3
> (canal produtor + Dispatcher no mesmo service) e quatro gaps mais cedo no
> pipeline. As tasks L1.3a-L1.3e abaixo resolvem cada um, na ordem de menor
> para maior risco, ANTES da prova final (agora L1.3f). Nenhuma delas amplia
> REQ-52 — cada uma é um fix de codegen genuíno e independente, com seu próprio
> par de testes; ver ISSUE-12 (`.claude/issues.md`) para a análise de raiz
> completa de cada ponto.
>
> **PAUSADO em L1.3d (decisão do usuário):** L1.3a/L1.3b/L1.3c fecharam limpo.
> L1.3d tentou a rota (b) (dar a Kitchen um provider real) e a investigação
> mostrou que a premissa estava ERRADA — o gap (`list <Aggregate>` não
> suportado por `tryEmitListVO`, `codegen/decl_query.go`) é provider-agnóstico,
> um gap de codegen genuíno que provavelmente também afeta `sales/read.ds`
> (nunca exercitado). Nem rota (a) (estender o codegen) nem (b') (reescrever
> as Queries) foram tentadas — o usuário optou por PARAR aqui e seguir para as
> Fases L2/L3 (independentes de L1) em vez de perseguir o fechamento completo
> de `pizzeria` agora. L1.3d/L1.3e/L1.3f ficam bloqueadas, sem tentativa
> adicional neste ciclo — ver a nota "CORREÇÃO" em ISSUE-12 item 4
> (`.claude/issues.md`) e `.claude/state.md` para o registro completo.

- [x] **L1.3a** Corrigir o typo do fixture: `items List<TicketItem>` →
  `items AppendList<TicketItem>`. (ISSUE-12 item 3)
  - `docs/examples/pizzeria/kitchen/domain.ds`: `.add(...)` só é mapeado
    (`codegen/goname/types.go:111`) para um campo `AppendList<T>`; `List<T>`
    comum não suporta `.add`. O padrão correto já existe em
    `wallet/domain.ds:88` (`entries AppendList<StatementEntry>`).
  - Confirmar que a mudança não quebra nenhum outro uso de `items` no
    `pizzeria` (read.ds/policy.ds) — `AppendList<T>` deve ser um superset
    compatível de leitura.
  - DoD: escopo verde (front-end continua validando pizzeria limpo); não é
    um fix de codegen, só do fixture — sem par de teste de codegen novo.

- [x] **L1.3b** `lowerAccessCondition`: suportar uma condição de acesso que é
  SÓ `caller.hasRole(...)` (sem `&&`/`||`/`==` compondo). (ISSUE-12 item 1)
  - `codegen/decl_aggregate.go:341` (`lowerAccessCondition`): hoje só trata
    `BinaryExpr` (composto) ou cai no fallback genérico `l.Expr(cond)`, que
    rejeita `CallExpr` puro. Adicionar o caso "condição é diretamente uma
    chamada de `caller.hasRole(...)`/outro predicado de `caller` reconhecido"
    como uma forma de primeira classe, gerando o `if` equivalente sem passar
    pelo `Lowerer.Expr` genérico.
  - **Testes pareados (NFR-4):** `access { X requires caller.hasRole("r") }`
    sozinho (sem composição) gera e compila; a forma composta já suportada
    (`caller.authenticated && caller.hasRole(...)`) continua byte-idêntica.
  - DoD: escopo verde; `go build`/`go vet`/`gofmt` limpos.

- [x] **L1.3c** `emitApply`: anexar `BuiltinLowerer` (builtins de função
  utilizáveis dentro de `Apply`). (ISSUE-12 item 2)
  - `codegen/decl_aggregate.go:274` (`emitApply`): constrói o `Lowerer` sem
    `.WithBuiltins(...)`, ao contrário de `emitUseCasesBytes`/
    `emitPolicyExecute`/Saga/Query, que sempre anexam um `BuiltinLowerer`.
    Qualquer builtin (`now()`/`uuid()`/`random(...)`) usado dentro de um
    `Apply` falha hoje. Corrigir para anexar o mesmo `BuiltinLowerer` que os
    outros emissores já usam.
  - **Testes pareados:** um `Apply` que usa `now()`/`uuid()` gera e compila;
    um `Apply` sem builtin nenhum (o caso comum, wallet/shop) byte-idêntico.
  - DoD: escopo verde; `go build`/`go vet`/`gofmt` limpos; wallet/shop sem
    regressão.

- [ ] **L1.3d** ⛔ **BLOQUEADA** — Read Side de `Kitchen`: decidir o destino de
  `list KitchenTicket` sem provider real por trás. (ISSUE-12 item 4)
  - Investigar: `Query GetBoardTickets` faz `list KitchenTicket where ...` —
    o seam in-memory (`runtime.Query[T]`, `codegen/decl_query.go`, E8.1) exige
    correlacionar o VO/Aggregate listado a um campo `AppendList<VO>` de um
    Aggregate conhecido; listar o PRÓPRIO Aggregate diretamente, sem um
    provider real (`Kitchen.MainDb` usa `"mongodb"`, decorativo) por trás,
    não é uma forma coberta hoje.
  - ~~Duas rotas possíveis (decidir na task, documentar a escolha): (a)
    estender o seam in-memory... ou (b) ajustar o `pizzeria`...~~ **Rota (b)
    tentada e REJEITADA por investigação empírica** (ver ISSUE-12 item 4,
    nota "CORREÇÃO"): trocar o provider de Kitchen não muda nada — a causa
    raiz (`tryEmitListVO`, `codegen/decl_query.go:461`, só reconhece `list
    <VO correlacionado via AppendList>`, nunca `list <Aggregate>` direto,
    **independente de provider**) é um gap de codegen genuíno, não um
    problema de fixture. `sales/read.ds` tem a MESMA forma e provavelmente o
    MESMO bug latente, nunca exercitado. Só rota (a) (estender
    `EmitQuery`/`tryEmitListVO`) ou (b') (reescrever as Queries para a forma
    já suportada) fecham isto de verdade — nenhuma tentada ainda.
  - **Decisão do usuário: parar aqui, não perseguir (a) nem (b') agora —
    seguir para as Fases L2/L3 (independentes de L1).** Task fica BLOQUEADA,
    sem tentativa adicional neste ciclo.
  - DoD: NÃO cumprido — ver acima.

- [ ] **L1.3e** ⛔ **BLOQUEADA** (depende de L1.3d) — Guarda F5/G3: suportar um módulo que é SIMULTANEAMENTE
  produtor de canal de saída E dono de uma Policy/Query cacheada local, no
  MESMO service. (ISSUE-12 item 5, o bloqueio arquitetural central)
  - `codegen/codegen.go:1143` (o `if producerChannel != nil && needsDispatcher
    { return erro }`): hoje `generateCmdMainFile` recusa combinar os dois.
    Investigar se o Wire combinado de L1.1 (`Wire(uow, dispatcher)`) já
    resolve o lado de EMISSÃO (o módulo consegue registrar Policies E
    UseCases no mesmo pacote) — o que falta é o lado de CONSTRUÇÃO em
    `main.go`: hoje o `uow` é construído com `NewUnitOfWork(store,
    dispatcher)` OU `NewUnitOfWork(store, <canal>)`, nunca os dois ao mesmo
    tempo (a UoW só aceita 1 publisher). Desenhar a solução (ex.: o
    `dispatcher` publica localmente E o canal assina o Dispatcher para o(s)
    tipo(s) de `PublicEvent` que atravessam, em vez de o canal ser o
    publisher direto da UoW) ANTES de implementar — é uma mudança de design,
    não um fix mecânico. Documentar a decisão em `design.md` antes de tocar
    código, se a mudança for não-trivial.
  - **Testes pareados:** fixture sintética com módulo produtor+Policy local no
    mesmo service → gera e compila; `wallet`/`shop`/fixtures existentes de
    canal (sem essa combinação) byte-idênticos.
  - DoD: escopo verde; `go build`/`go vet`/`gofmt` limpos; nenhuma regressão
    em nenhuma fixture de canal/Policy existente (Marco F/J/K).

- [ ] **L1.3f** ⛔ **BLOQUEADA** (depende de L1.3d/L1.3e) — Prova com o
  `pizzeria` + limpeza do CI. (REQ-52.5/52.6/52.7,
  §design 2.3 — só depois de L1.3a-L1.3e fecharem ISSUE-12 por completo)
  - Teste e2e (padrão `driver.TestGenerate*`): `GenerateProject` sobre
    `docs/examples/pizzeria` gera e o Go compila (`go build`/`go vet` sobre os
    bytes em disco, com `go mod tidy` condicional como wallet/shop).
  - `.github/workflows/ci.yml`: remover `pizzeria` de `KNOWN_UNGENERATABLE` — a
    partir daqui o job `examples` gera+compila o pizzeria como os demais.
  - Se surgir um bloqueio adicional na geração do pizzeria (fora do já
    mapeado por ISSUE-12), **registrar nova issue** e não ampliar REQ-52
    (REQ-52.7).
  - DoD: `dsc gen docs/examples/pizzeria` sai 0 e builda; e2e verde; wallet/shop
    sem regressão; fecha a Fase L1; ISSUE-12 marcada `RESOLVED`.

---

## Fase L2 — Semântica plena dos testes gerados (ISSUE-6, REQ-53)

> Cinco sub-itens independentes (L2.1–L2.5), cada um com par NFR-4 sobre o
> projeto gerado. L2.6 delimita o item que fica fora (acesso NEGADO).

- [ ] **L2.1** `then state { ... }` (§22.1, asserção de estado). (REQ-53.1, §design 3.2)
  - `codegen/gentest.go`: onde hoje erra, reconstruir o estado do Aggregate
    (replay via o `Apply` gerado) e emitir uma asserção por campo declarado.
  - **Testes pareados:** um `then state` que bate (passa) e um que diverge
    (falha com diff claro).

- [ ] **L2.2** `emitted`/`released` a partir de passo de Saga (§22.3). (REQ-53.2,
  §design 3.2)
  - `codegen/gentest.go` (helpers de Saga): reusar a coleta de eventos de
    §22.4/Policy para afirmar `emitted Evento(...)`/`released` de um passo.
  - **Testes pareados:** passo que emite o evento (passa) e um `then` que espera
    um evento não emitido (falha).

- [ ] **L2.3** `mock Target returns X` influencia o fluxo (§22.3). (REQ-53.3,
  §design 3.2)
  - Auditar `emitSagaMock`: fazer `X` ser o retorno efetivo do alvo mockado
    (hoje ignorado). A auditoria decide a forma (stub-função substituível).
  - **Testes pareados:** `X` que desvia o resultado do passo seguinte (observável)
    e o caso sem mock (inalterado).

- [ ] **L2.4** Shrinking do contra-exemplo de `property` (§22.5). (REQ-53.4,
  §design 3.2)
  - `codegen/gentest_property.go`: ao falhar, encolher a sequência (remoção/
    bissecção de passos, re-testando) até o mínimo que ainda falha —
    **determinístico** (semente fixa, NFR-13).
  - **Testes pareados:** property que falha → contra-exemplo mínimo estável; uma
    que passa → sem shrinking.

- [ ] **L2.5** `rolledback` com reversão real: staging na memory store (§22.2).
  (REQ-53.5, §design 3.2, NFR-30)
  - `codegen/rtsrc/uow.go.txt` (+ `eventstore.go.txt` se preciso): `memoryTx`
    acumula `Append` num buffer e só aplica na store no commit (fn → nil); no
    erro, descarta. Preservar read-your-writes dentro do mesmo `Run`. `rtsrc/`
    puro (stdlib), sem dep nova.
  - `codegen/gentest.go`: `rolledback` passa a afirmar que a store ficou
    intacta (não só `err != nil`).
  - **Testes pareados:** um `Run` com erro não deixa evento na store; um `Run` OK
    deixa. Guarda: os testes comportamentais existentes da memory UoW seguem
    verdes (commit continua durável).
  - DoD: escopo verde; `go build`/`go vet`/`gofmt` limpos; wallet/shop sem
    regressão.

- [ ] **L2.6** Delimitar o cenário de acesso NEGADO (§22). (REQ-53.7, §design 3.1)
  - Sem código: documentar em `.claude/specs/codegen/gaps.md` §G-7 e em ISSUE-6
    que o item exige nova gramática ("como o caller X"), fora do escopo de
    codegen — apontado para um ciclo de front-end (natureza de ISSUE-2). ISSUE-6
    fecha só a fatia tratável; este resíduo mantém a issue parcialmente aberta.
  - DoD: docs atualizados; nenhuma mudança de código.

---

## Fase L3 — Divergências menores: refino + reclassificação (ISSUE-8, REQ-54)

- [ ] **L3.1** §22.7: análise de raiz + refino OU reclassificação. (REQ-54.1/54.3,
  §design 4.1)
  - Ler `sema/rules_warnings.go:checkHandleErrorCoverage` e decidir: o checker
    tem os ramos `ensure ... else Error` por Handle E consegue cruzá-los com os
    cenários de erro testados?
    - **Se sim:** refinar o warning para apontar o ramo específico não coberto;
      par NFR-4 (um ramo não testado → warning; todos testados → silêncio).
    - **Se não** (exige análise `*.test.ds` × Handle que o checker não faz):
      manter o por-Handle e reclassificar o refino como ciclo de sema dedicado,
      registrando o motivo preciso em `gaps.md`/ISSUE-8.
  - DoD: escopo verde (se refinar) ou docs atualizados (se reclassificar).

- [ ] **L3.2** Reclassificar §4.4 (GDPR) e §25 (agregações/aritmética/FFI).
  (REQ-54.2, §design 4.1)
  - Sem código: `.claude/specs/codegen/gaps.md` §G-baixo e ISSUE-8 — mover (a) e
    (c) de "dívida de codegen" para **"aguardando definição no spec da
    linguagem"** (o placeholder de redação já existe; agregações/aritmética
    exigem sintaxe nova não definida). Deixa claro que não há ação de codegen
    pendente para (a)/(c).
  - DoD: docs atualizados; nenhuma mudança de código.

---

## Fechamento do Marco L

- [ ] **L.fim** Revisão de DoD (requirements §5): ISSUE-7 resolvida (pizzeria
  gera+compila, fora do KNOWN_UNGENERATABLE); ISSUE-6 resolvida na fatia
  tratável com o resíduo "acesso NEGADO" apontado; ISSUE-8 fechada em (b) e
  reclassificada em (a)/(c); wallet/shop byte-idênticos; `.claude/issues.md` e
  `.claude/state.md` e `gaps.md` atualizados. (Sem `go test ./...` local no
  fechamento — CI roda a suíte nas PRs.)

---

## Rastreabilidade REQ → Tasks

| REQ | Tasks | Issue |
|---|---|---|
| REQ-52 (Wire unificado + pizzeria + CI) | L1.1, L1.2, L1.3a-L1.3f | ISSUE-7, ISSUE-12 |
| REQ-53.1 (then state) | L2.1 | ISSUE-6 |
| REQ-53.2 (saga emitted/released) | L2.2 | ISSUE-6 |
| REQ-53.3 (mock returns X) | L2.3 | ISSUE-6 |
| REQ-53.4 (shrinking) | L2.4 | ISSUE-6 |
| REQ-53.5 (rolledback/staging) | L2.5 | ISSUE-6 |
| REQ-53.7 (acesso NEGADO — delimitado) | L2.6 | ISSUE-6 |
| REQ-54.1 (§22.7 refino/reclassif.) | L3.1 | ISSUE-8 |
| REQ-54.2 (§4.4/§25 reclassificação) | L3.2 | ISSUE-8 |

## Mapa de dependências

```
L1.1 ──▶ L1.2 ──▶ L1.3a ──▶ L1.3b ──▶ L1.3c ──▶ L1.3d ──▶ L1.3e ──▶ L1.3f
        (Wire combinado → call site → ISSUE-12: typo, hasRole, builtins,
         read-side Kitchen, F5/G3 → prova final+CI)
L2.1  L2.2  L2.3  L2.4  L2.5      (independentes entre si; cada um par NFR-4)
L2.6  (doc, independente)
L3.1  L3.2                        (independentes; L3.2 é doc)
                        └─▶ L.fim

Entre fases: L1, L2, L3 são independentes (qualquer ordem/paralelo).
```
