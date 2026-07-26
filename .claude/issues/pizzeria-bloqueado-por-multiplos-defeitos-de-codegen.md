# Pizzeria bloqueado por múltiplos defeitos independentes de codegen (ex-ISSUE-12)
- SPEC: correcoes-issues-6-7-8
- TASK: L1.2 (achado ao provar `testdata/projects/pizzeria` fim-a-fim, REQ-52.4/52.7)
- DESCRIPTION: L1.2 pedia para confirmar que o `Kitchen` do `pizzeria` não
  esbarra na guarda F5/G3 pré-existente (`codegen/codegen.go:1143`:
  `"codegen: cmd/%s/main.go: módulo com Policy/Query cacheada E módulo
  produtor de canal de saída no mesmo service ainda não têm wiring combinado
  suportado (F5/G3)"`, disparada quando `producerChannel != nil &&
  needsDispatcher`). **A leitura confirma que ele ESBARRA, sim** — ao
  contrário do que a task text presumia ("é UseCase+Policy local, sem canal
  próprio"):
  - `testdata/projects/pizzeria/kitchen/domain.ds`: `Handle Finish` faz `emit
    TicketFinished(self.orderRef)`, e `TicketFinished` é um `PublicEvent`.
  - `testdata/projects/pizzeria/topology.ds`: o canal `Kitchen -> Sales` (`via:
    queue`, `provider: "rabbitmq"`, `orderBy: orderRef`) existe DENTRO do
    MESMO service `PizzeriaMonolith { modules: [Sales, Kitchen] }` que
    `Sales -> Kitchen`. Logo `producerChannelFor(prog, "Kitchen")` resolve
    esse canal: Kitchen **é** produtor de canal de saída.
  - `testdata/projects/pizzeria/kitchen/policy.ds`: `Policy
    CreateTicketOnOrderPaid on OrderPaid` é uma Policy LOCAL do módulo
    Kitchen, que força `needsDispatcher = true` para o grupo
    `PizzeriaMonolith` (`codegen.go:1089`).
  - `producerChannel != nil && needsDispatcher` é exatamente essa combinação
    → a guarda F5/G3 dispara para o `pizzeria`.
  - Confirmado por leitura estática (ver acima) e por reprodução empírica
    parcial (abaixo) — não foi possível chegar ao PONTO exato do erro F5/G3
    rodando o `pizzeria` real porque **outros bloqueios independentes, mais
    cedo no pipeline, impedem a geração de chegar a `generateCmdMainFile`**
    (onde a guarda mora). Rodando `dsc gen testdata/projects/pizzeria` hoje
    (pós-L1.1) o erro real é:
    ```
    dsc: codegen: módulo Kitchen: aggregate_kitchen_ticket.go: codegen:
    Aggregate KitchenTicket: Handle Create: access: codegen: CallExpr com Fn
    *ast.MemberExpr não suportado em Lowerer.Expr (só construção de tipo via
    identificador nu; chamada de método/built-in é E5.3/E6+)
    ```
    Investigando numa cópia de trabalho isolada (nunca commitada, só para
    diagnóstico — o fixture real do `pizzeria` NÃO foi alterado), contornando
    esse erro e os seguintes um a um, aparecem em sequência MAIS QUATRO
    bloqueios independentes, cada um ortogonal a REQ-52/F5-G3:
    1. `access { Create requires caller.hasRole("system_sales") }`
       (`kitchen/domain.ds:90-93`): `lowerAccessCondition`
       (`codegen/decl_aggregate.go:341`) só trata condições que são um
       `BinaryExpr` (`&&`/`||`/`==`/`!=` com "caller.X == VOWrapper") ou caem
       no fallback genérico `l.Expr(cond)` — uma condição que é SÓ
       `caller.hasRole(...)` (um `CallExpr` puro, sem `&&`/`||`/`==`) não é
       nenhuma das formas tratadas por `lowerCallerVOEquality` e cai no
       `Lowerer.call` genérico, que rejeita qualquer `CallExpr` cujo `Fn` não
       seja "construção de tipo via identificador nu" — daí o erro acima.
       `wallet`/`shop` nunca exercitam essa forma (só usam `caller.
       authenticated`), então nunca foi pega.
    2. `Apply TicketCreated { state.createdAt = CreatedAt(now()) }`
       (`kitchen/domain.ds:104`, também `:125`): `emitApply`
       (`codegen/decl_aggregate.go:274`) constrói o `Lowerer` com
       `lower.NewLowerer(env, reg, runtimeAlias)` e NUNCA chama
       `.WithBuiltins(...)` (ao contrário de `emitUseCasesBytes`/
       `emitPolicyExecute`/Saga/Query, que sempre anexam um
       `BuiltinLowerer`) — qualquer builtin de função (`now()`/`uuid()`/
       `random(...)`) usado dentro de um corpo de `Apply` falha com "CallExpr
       sobre \"now\" não é construção de VO/Event/Command conhecida".
       `wallet`/`shop` nunca chamam nenhum builtin de função dentro de um
       `Apply`, então nunca foi pego.
    3. `Apply TicketItemAdded { state.items.add(event.item) }`
       (`kitchen/domain.ds:112`): `.add(...)` só está mapeado
       (`codegen/goname/types.go:111`, `BuiltinMethod{Receiver:
       "AppendList", Method: "add"}`) para um campo `AppendList<T>`; Kitchen
       declara `items List<TicketItem>` (List comum), não `AppendList` — ao
       que tudo indica um TYPO/bug do PRÓPRIO fixture `pizzeria` (deveria
       ser `AppendList<TicketItem>`, o padrão que
       `wallet/domain.ds:88` — `entries AppendList<StatementEntry>` — já
       usa), não necessariamente um gap de codegen. Sinalizado aqui porque
       bloqueia a geração de qualquer forma, mas provavelmente se resolve
       ajustando o `.ds`, não o back-end.
    4. `Query GetBoardTickets() -> List<KitchenTicketVW> { return list
       KitchenTicket t where t.status in [...] orderBy t.createdAt ascending
       as KitchenTicketVW }` (`kitchen/read.ds:15-20`): mesmo reduzindo para
       a forma mínima "`return list KitchenTicket where ... as
       KitchenTicketVW`" (idêntica em espírito à
       `sales/read.ds:20` que FUNCIONA sobre o Aggregate `MenuItem`), a
       geração falha com "list ... em posição de expressão pura não é
       suportado por Lowerer.Expr". A diferença de fato entre os dois é que
       `Sales.MainDb` usa `provider: "postgres"` (REAL, Marco J) e
       `Kitchen.MainDb` usa `provider: "mongodb"` (DECORATIVO,
       `kitchen/mod.ds:9-13`) — sem um provider real, o Read Side de Kitchen
       cai no seam in-memory (`runtime.Query[T]`), cujo suporte de "list
       <VO/Aggregate>" (E8.1, `codegen/decl_query.go` cabeçalho) exige
       correlacionar o VO a um campo `AppendList<VO>` de um ÚNICO Aggregate
       conhecido — listar o PRÓPRIO Aggregate (`KitchenTicket`) diretamente,
       sem provider real por trás, não é uma forma coberta.
  - **CORREÇÃO (achado durante a tentativa de L1.3d — a premissa acima do
    item 4 estava ERRADA):** a task L1.3d tentou a rota (b) — dar a Kitchen
    um provider real (`"sqlite"`) — presumindo que o provider fosse a única
    diferença entre a Query de Kitchen (falha) e a de Sales (funciona).
    **Não é.** Trocar o provider de Kitchen NÃO mudou o erro (confirmado
    empiricamente, inclusive numa fixture mínima isolada com `"sqlite"` E
    `"postgres"`). A causa raiz de verdade, lida em `codegen/decl_query.go`
    (`tryEmitListVO`, ~linha 461): `if _, ok :=
    qc.env.TypeOfName(voName).(*types.VOType); !ok { return false, nil }` —
    a função SÓ reconhece `list <nome>` quando `<nome>` resolve a um
    `*types.VOType` correlacionado via campo `AppendList<VO>` de um
    Aggregate conhecido (o padrão de `wallet`, `list StatementEntry`). Um
    NOME DE AGGREGATE (`KitchenTicket`, `MenuItem`) resolve a
    `*types.ShapeType`, não `*types.VOType` — a checagem falha SEMPRE,
    **independente de provider**. É um gap de codegen genuíno e
    provider-agnóstico (a forma "`list <Aggregate> ... as View`" nunca foi
    implementada), não um problema do `pizzeria`. **Achado ainda mais
    importante:** `sales/read.ds`'s `GetAvailableMenu`/`GetActiveOrders` têm
    a MESMA forma (`list MenuItem`/`list Order`, o próprio Aggregate, sem
    correlação via `AppendList`) e NUNCA foram de fato exercitadas — a
    geração do `pizzeria` sempre falha em Kitchen primeiro (ordem
    alfabética de módulos), então ninguém tinha confirmado que a Query de
    Sales realmente funciona. É plausível que ela tropece no MESMO erro se
    a geração algum dia chegar lá. **Decisão do usuário (não perseguir agora):**
    em vez de (a) estender `tryEmitListVO`/`EmitQuery` para suportar `list
    <Aggregate>` de verdade (a rota antes rejeitada como grande demais, mas
    que parece inevitável), ou (b') reescrever as Queries de Kitchen/Sales
    para a forma já suportada, o usuário optou por **parar aqui, registrar
    e seguir para as Fases L2/L3** (independentes de L1) em vez de perseguir
    o fechamento completo de `pizzeria` agora. L1.3d/L1.3e/L1.3f ficam
    **BLOQUEADAS**, sem tentativa adicional neste ciclo — ver
    `.claude/state.md` para o registro completo da decisão.
  - **Conclusão:** o `pizzeria` está bloqueado por PELO MENOS CINCO defeitos
    independentes (F5/G3 + os quatro acima), nenhum deles dentro do escopo de
    REQ-52 (Wire unificado) — REQ-52.7 pede exatamente isto: registrar como
    issue nova em vez de ampliar o escopo da task. **L1.2 fecha normalmente**
    (seu próprio escopo — o call site de `main.go` + esta confirmação — está
    completo); **L1.3 fica BLOQUEADA** até esta issue (ou uma investigação
    dedicada) resolver os cinco pontos, e provavelmente precisa de um recorte
    NOVO (talvez até uma fixture-alvo diferente de `pizzeria`, ou correções
    em `pizzeria` + em pelo menos dois pacotes de codegen distintos:
    `decl_aggregate.go`/`lower/` para os itens 1/2, `decl_query.go`/E8.1 para
    o item 4, e a guarda F5/G3 em si). Este registro é INDEPENDENTE da issue
    sobre a colisão de Wire (que L1.1 já fechou); não considerar aquela issue
    totalmente resolvida enquanto esta (o bloqueio real e maior de
    `pizzeria`) permanecer aberta — ver `.claude/specs/correcoes-issues-6-7-8/`.
- SOLVED: FALSE
