package codegen

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/types"
)

// decl_saga.go emite o Go de um SagaDecl (F3, REQ-24, §design 3.10): uma
// state machine com um struct de state (SagaDecl.State) e uma lista ORDENADA
// de runtime.Step (um por SagaDecl.Steps) — Up/Down/OnInfraError lowerizados
// pela MESMA StmtLowerer/Lowerer de Handle/UseCase/Policy/Worker (REQ-23.5,
// reaplicado aqui). A orquestração (rodar Up em ordem; numa falha, compensar
// os passos já completados em ordem REVERSA, respeitando "down {
// unrecoverable }") mora em runtime.RunSaga (codegen/rtsrc/saga.go.txt) — este
// arquivo só CONSTRÓI o []runtime.Step[<Nome>State] a partir dos corpos
// lowerizados e decide a forma de entrada por modo (`async`/`await`).
//
// --- Corpo via lowering: só "state" (constructSagaStep, REQ-9.3/9.5.6) ---
//
// resolver/receivers.go só lista "state" para constructSagaStep — SEM "cmd"
// (diferente de UseCase.execute) e sem "caller" (diferente de Handle/Policy).
// Isso significa que um passo de Saga NÃO enxerga o Command recebido: a única
// forma de um Up/Down/OnInfraError usar um dado do Command é ele já ter sido
// copiado para "state" ANTES do 1º passo — exatamente o que "Steps semeiam
// state" (tasks.md, F3) descreve por outro ângulo (cada Up ESCREVE em state
// conforme avança). A ponte entre os dois (semear os campos de mesmo nome do
// Command declarado em `handles` dentro de state, antes do 1º passo) é uma
// convenção deste emissor (sagaSeedFromCommandLines, abaixo), não um
// mecanismo do front-end: não há receptor "cmd" nos passos, então não há
// outro jeito de um campo do Command chegar ao state.
//
// TypeEnv.SeedSagaStep (lower/env.go) dá a "state" um *types.ShapeType com os
// Fields de verdade de SagaDecl.State (ao contrário de types.Model.TypeOf
// sobre o símbolo da Saga, que devolve um ShapeType SEM Fields — REQ-12 não
// cobre Saga, ver a doc de SeedSagaStep) — assim "state.<Campo>" lowereiza
// para "state.<Campo Exportado>" normalmente (Lowerer.member, expr.go).
//
// --- down { unrecoverable } (REQ-24.2, §18.2) ---
//
// O parser não distingue "unrecoverable" sintaticamente: "down {
// unrecoverable }" parseia como um Block de 1 statement, um *ast.ExprStmt cujo
// X é um *ast.Ident{Name: "unrecoverable"} (parser/parse_saga_test.go confirma
// essa forma) — indistinguível, na AST, de um corpo real que só chama uma
// função nula "unrecoverable()" sem parênteses... exceto que não há CallExpr
// nenhum: é um Ident NU em posição de statement, uma forma que StmtLowerer
// nunca aceitaria como corpo de verdade (exprStmt exige um CallExpr — ver
// lower/stmt.go). isUnrecoverableDown (abaixo) reconhece esse padrão exato
// ANTES de tentar lowerizar o bloco: um step assim NÃO ganha uma função Down
// (não há corpo de verdade para gerar) e Step.Unrecoverable vira true no
// literal do runtime.Step correspondente — runtime.RunSaga sabe pular a
// compensação desse step sem chamar nada (ver a doc de saga.go.txt).
//
// --- onInfraError (REQ-24.1, §18.2) ---
//
// Não é um retry automático: ao contrário do onError.retry de Worker (um
// objeto de config estruturado {attempts, backoff} — decl_worker.go), o
// onInfraError de um passo de Saga é um Block de statements DomainScript
// livre, sem contagem de tentativas alguma. Este emissor só lowereiza o bloco
// para uma função-hook chamada UMA VEZ por runtime.RunSaga quando Up falha com
// um erro que NÃO é runtime.BusinessError (distinção negócio/infra do §19,
// já usada por codegen/http.go/writeBusinessError) — o step continua
// considerado falho depois (compensação roda do mesmo jeito). Ver a doc de
// Step em saga.go.txt para o raciocínio completo.
//
// --- mode async/await (REQ-24.3) ---
//
// "await" bloqueia o chamador: os passos rodam numa goroutine própria
// (destacada do ctx recebido — ver a doc de emitSagaAwaitEntry sobre por que
// isso evita uma corrida de dados sobre *state) enquanto a função de entrada
// espera (select) o resultado OU ctx/timeout vencer primeiro. "async" (e
// qualquer modo não reconhecido — mesma postura conservadora de
// PolicyDecl.Delivery em decl_policy.go: sema não valida esse literal) inicia
// os passos numa goroutine e devolve um sagaId de imediato; o andamento fica
// consultável via "<Nome>Status(sagaId)" sobre um runtime.SagaStore em memória
// PRÓPRIO do pacote (sem Wire — ver a nota abaixo).
//
// --- Sem "func Wire" (a colisão do F1/F2, resolvida para Saga: nunca ocorre) ---
//
// F1 (decl_policy.go) e F2 (decl_worker.go) documentam que somar um 2º/3º
// "func Wire" ao mesmo pacote Go colidiria; F2 deu a Worker seu próprio nome
// ("StartWorkers") para nunca colidir. Saga vai além: não precisa de NENHUM
// ponto de entrada compartilhado injetado por cmd/<service>/main.go — o
// SagaStore de "async" é uma var de pacote inicializada DIRETO
// ("runtime.NewMemorySagaStore()", sem wiring), o mesmo padrão que
// emitContinuous (decl_worker.go) já usa para o runtime.Source[T] default de
// um Worker "continuous". A função de entrada da Saga (o nome da própria
// declaração, IGUAL a UseCase — nunca "Wire") é diretamente chamável por quem
// precisar (borda HTTP/outro código gerado); nenhuma mudança em
// generateCmdMainFile é necessária.

// --- 1. State: campos de SagaDecl.State -> struct Go. ---

// sagaStateTypeName é o nome Go do struct de state de decl: "<Nome>State".
func sagaStateTypeName(decl *ast.SagaDecl) string {
	return decl.Name + "State"
}

// sagaStateField é a forma Go já resolvida de um campo de SagaDecl.State —
// mesmo padrão de commandFieldInfo (decl_command.go).
type sagaStateField struct {
	field      *ast.Field
	goType     string
	exportName string
}

// sagaStateFields resolve o tipo Go de cada campo de decl.State, na ordem
// declarada.
func sagaStateFields(decl *ast.SagaDecl) ([]sagaStateField, error) {
	infos := make([]sagaStateField, 0, len(decl.State))
	for _, f := range decl.State {
		if f == nil || f.Name == "" {
			continue
		}
		goType, err := goname.GoFieldType(f.Type)
		if err != nil {
			return nil, fmt.Errorf("state: campo %s: %w", f.Name, err)
		}
		infos = append(infos, sagaStateField{field: f, goType: goType, exportName: goname.ExportField(f.Name)})
	}
	return infos, nil
}

// emitSagaStateStruct emite "type <Nome>State struct { ... }" com os campos
// de decl.State, na ordem declarada — mesma forma de emitCommandDecl
// (decl_command.go), sem tag de idempotência (não se aplica aqui).
func emitSagaStateStruct(e *emit.Emitter, decl *ast.SagaDecl, fields []sagaStateField) {
	typeName := sagaStateTypeName(decl)
	e.Line("// %s é o state da Saga %s (§18.2) — os campos que os passos", typeName, decl.Name)
	e.Line("// semeiam conforme executam (Up escreve em state.<Campo>; ver a doc do")
	e.Line("// arquivo sobre por que o Command não é visível diretamente dentro de um")
	e.Line("// passo).")
	e.Block(fmt.Sprintf("type %s struct", typeName), func() {
		for _, fi := range fields {
			e.Line("%s %s %s", fi.exportName, fi.goType, goname.JSONTag(fi.field.Name))
		}
	})
}

// resolveSagaCommand resolve o Command tratado pela Saga (SagaDecl.Handles) —
// mesmo padrão de resolvePolicyEvent (decl_policy.go), sem a distinção
// Public/contracts (Command não tem variante pública).
func resolveSagaCommand(tab *symbols.SymbolTable, module, name string) (*ast.CommandDecl, error) {
	sym, ok := tab.Lookup(module, name)
	if !ok {
		sym, ok = tab.Find(name)
	}
	if !ok {
		return nil, fmt.Errorf("command %q não resolvido (bug de geração — REQ-9 já deveria ter barrado isso)", name)
	}
	cd, ok := sym.Decl.(*ast.CommandDecl)
	if !ok {
		return nil, fmt.Errorf("%q não resolve a um Command (got %T)", name, sym.Decl)
	}
	return cd, nil
}

// sagaSeedFromCommandLines devolve as linhas Go "state.<Campo> = cmd.<Campo>"
// para todo campo de state cujo NOME bate com um campo do Command (ver a doc
// do arquivo sobre por que essa cópia precisa acontecer aqui, fora do corpo
// lowerizado dos passos).
func sagaSeedFromCommandLines(fields []sagaStateField, cmdFields []*ast.Field) []string {
	cmdNames := make(map[string]bool, len(cmdFields))
	for _, f := range cmdFields {
		if f != nil {
			cmdNames[f.Name] = true
		}
	}
	var lines []string
	for _, fi := range fields {
		if cmdNames[fi.field.Name] {
			lines = append(lines, fmt.Sprintf("state.%s = cmd.%s", fi.exportName, fi.exportName))
		}
	}
	return lines
}

// --- 2. Passos: up/down/onInfraError -> funções Go + []runtime.Step. ---

// isUnrecoverableDown reconhece "down { unrecoverable }" pela forma exata que
// o parser produz (ver a doc do arquivo): um Block de 1 statement, um
// *ast.ExprStmt cujo X é o Ident nu "unrecoverable".
func isUnrecoverableDown(down *ast.Block) bool {
	if down == nil || len(down.Stmts) != 1 {
		return false
	}
	es, ok := down.Stmts[0].(*ast.ExprStmt)
	return ok && astutil.IsIdent(es.X, "unrecoverable")
}

// sagaBase devolve o nome-base (1ª letra minúscula) usado para prefixar as
// funções privadas de uma Saga (ex. "purchaseTickets") — mesma convenção de
// workerEmitter.tickFuncName (decl_worker.go), fatorada aqui porque uma Saga
// usa esse prefixo em bem mais lugares (Up/Down/OnInfraError por passo, a
// lista de Steps, RunSteps, SagaStore).
func sagaBase(name string) string {
	if name == "" {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:]
}

// emitSagaStepPhaseFunc emite "func <funcName>(ctx context.Context, state
// *<stateType>) error" lowerizando block com l — mesmo padrão de
// emitBodyFunc (decl_worker.go)/emitPolicyDecl, reusando a MESMA
// StmtLowerer/Lowerer (REQ-23.5). Chamada só quando block tem um corpo de
// verdade a gerar (ver a doc do arquivo: um down "unrecoverable" NUNCA chega
// aqui).
//
// adapterByName (F4/H4, REQ-25.3/31.3) habilita StmtLowerer.WithNotifyAdapters
// sobre este corpo: SEM isso, um passo que chamasse "PaymentRequest(...)"
// como statement (a MESMA forma que Policy/UseCase já reconhecem, ver
// decl_io.go) falharia a geração ("ExprStmt de CallExpr... não suportado",
// lower/stmt.go:exprStmtCall — Fn é um Ident nu, não um MemberExpr) —
// nenhuma fixture de Saga anterior a esta task chamava um Adapter, então essa
// lacuna nunca tinha sido exercitada. nil preserva o comportamento anterior
// (nenhuma mudança para Sagas que não chamam Adapter, o caso comum).
func emitSagaStepPhaseFunc(e *emit.Emitter, funcName string, block *ast.Block, l *lower.Lowerer, ctxAlias, stateType string, adapterByName map[string]*ast.AdapterDecl) error {
	e.Line("")
	sig := fmt.Sprintf("func %s(ctx %s.Context, state *%s) error", funcName, ctxAlias, stateType)

	lastIsReturn := false
	if block != nil && len(block.Stmts) > 0 {
		_, lastIsReturn = block.Stmts[len(block.Stmts)-1].(*ast.ReturnStmt)
	}

	var bodyErr error
	e.Block(sig, func() {
		stmtCtx := lower.StmtContext{ZeroValues: []string{}, SuccessReturn: "return nil", CtxVar: "ctx"}
		sl := lower.NewStmtLowerer(l, e, stmtCtx).WithNotifyAdapters(adapterByName, "ctx")
		if bodyErr = sl.Block(block); bodyErr != nil {
			return
		}
		if !lastIsReturn {
			e.Line("return nil")
		}
	})
	return bodyErr
}

// sagaStepEmit é o resultado de emitir os 3 corpos (up/down/onInfraError) de
// um único SagaStep: os nomes Go das funções emitidas ("" quando ausente) e
// se o passo é Unrecoverable.
type sagaStepEmit struct {
	name                       string
	upFn, downFn, onInfraErrFn string
	unrecoverable              bool
}

// emitSagaStepFuncs emite as funções Go de um único SagaStep (up sempre;
// down só quando presente e não "unrecoverable"; onInfraError só quando
// presente) e devolve os nomes para montar o []runtime.Step depois.
// adapterByName é repassado a emitSagaStepPhaseFunc (ver a doc lá).
func emitSagaStepFuncs(e *emit.Emitter, base string, step *ast.SagaStep, l *lower.Lowerer, ctxAlias, stateType string, adapterByName map[string]*ast.AdapterDecl) (sagaStepEmit, error) {
	if step.Up == nil {
		return sagaStepEmit{}, fmt.Errorf("passo %s: sem bloco \"up\" (bug de geração sobre um programa validado — REQ-24.1 exige up em todo passo)", step.Name)
	}

	se := sagaStepEmit{name: step.Name}

	se.upFn = base + step.Name + "Up"
	if err := emitSagaStepPhaseFunc(e, se.upFn, step.Up, l, ctxAlias, stateType, adapterByName); err != nil {
		return sagaStepEmit{}, fmt.Errorf("passo %s: up: %w", step.Name, err)
	}

	se.unrecoverable = isUnrecoverableDown(step.Down)
	if step.Down != nil && !se.unrecoverable {
		se.downFn = base + step.Name + "Down"
		if err := emitSagaStepPhaseFunc(e, se.downFn, step.Down, l, ctxAlias, stateType, adapterByName); err != nil {
			return sagaStepEmit{}, fmt.Errorf("passo %s: down: %w", step.Name, err)
		}
	}

	if step.OnInfraError != nil {
		se.onInfraErrFn = base + step.Name + "OnInfraError"
		if err := emitSagaStepPhaseFunc(e, se.onInfraErrFn, step.OnInfraError, l, ctxAlias, stateType, adapterByName); err != nil {
			return sagaStepEmit{}, fmt.Errorf("passo %s: onInfraError: %w", step.Name, err)
		}
	}

	return se, nil
}

// emitSagaStepsTable emite "var <base>Steps = []runtime.Step[<stateType>]{
// ... }" — um literal por passo, na ordem declarada (a ordem que
// runtime.RunSaga percorre em Up e reverte em Down).
func emitSagaStepsTable(e *emit.Emitter, base, runtimeAlias, stateType string, steps []sagaStepEmit) {
	e.Line("")
	e.Line("// %sSteps é a lista de passos da Saga, na ordem declarada (§18.2) —", base)
	e.Line("// runtime.RunSaga roda Up em ordem e, numa falha, compensa (Down) os passos")
	e.Line("// já completados em ordem REVERSA, respeitando Unrecoverable (REQ-24.2).")
	e.Block(fmt.Sprintf("var %sSteps = []%s.Step[%s]", base, runtimeAlias, stateType), func() {
		for _, s := range steps {
			downExpr, onInfraExpr := "nil", "nil"
			if s.downFn != "" {
				downExpr = s.downFn
			}
			if s.onInfraErrFn != "" {
				onInfraExpr = s.onInfraErrFn
			}
			e.Line("{Name: %q, Up: %s, Down: %s, OnInfraError: %s, Unrecoverable: %t},",
				s.name, s.upFn, downExpr, onInfraExpr, s.unrecoverable)
		}
	})
}

// emitSagaRunStepsFunc emite "func <base>RunSteps(ctx, state) runtime.SagaResult"
// — a orquestração de 1 execução dos passos (runtime.RunSaga), compartilhada
// pelas entradas await/async (ver a doc do arquivo sobre os dois modos).
func emitSagaRunStepsFunc(e *emit.Emitter, base, runStepsFn, ctxAlias, runtimeAlias, stateType string) {
	e.Line("")
	e.Line("// %s executa os passos da Saga uma vez sobre state (§18.2): Up em", runStepsFn)
	e.Line("// ordem; numa falha, Down dos passos já completados em ordem reversa")
	e.Line("// (runtime.RunSaga, REQ-24.2).")
	e.Block(fmt.Sprintf("func %s(ctx %s.Context, state *%s) %s.SagaResult", runStepsFn, ctxAlias, stateType, runtimeAlias), func() {
		e.Line("return %s.RunSaga(ctx, state, %sSteps)", runtimeAlias, base)
	})
}

// emitSagaSeed emite cada linha de seedLines cru (já prontas, ver
// sagaSeedFromCommandLines).
func emitSagaSeed(e *emit.Emitter, seedLines []string) {
	for _, l := range seedLines {
		e.Line("%s", l)
	}
}

// --- 3. Entrada por modo: await (bloqueante+timeout) / async (sagaId+status). ---

// emitSagaAwaitEntry emite "func <Nome>(ctx, cmd) (*<Nome>State, error)"
// (mode await, REQ-24.3): os passos rodam numa goroutine DESTACADA
// (context.Background(), não o ctx recebido) enquanto esta função espera
// (select) o resultado ou ctx.Done() (deadline de decl.Timeout, se houver, ou
// o cancelamento do chamador). Destacar a goroutine do ctx recebido é
// deliberado: se o caminho de timeout vencer primeiro, esta função devolve
// SEM tocar mais em *state (nil, ctx.Err()) — a goroutine em segundo plano
// continua e ainda é a ÚNICA a escrever em *state depois disso, evitando uma
// corrida de dados entre ela e um chamador que tivesse recebido o mesmo
// ponteiro no caminho de timeout.
//
// hooks (H3, REQ-30.3) são as Metric "on <ESTA Saga>.completed" já
// resolvidas (resolveSagaCompletionHooks) — atualizadas logo que "case res
// := <-done" reporta sucesso (res.Err == nil), ANTES do "return state,
// res.Err": a duração observada por um histogram implícito é "time.
// Since(start).Seconds()", start marcado ANTES de disparar a goroutine dos
// passos (mede o tempo de ponta a ponta da Saga, não só de RunSteps).
func emitSagaAwaitEntry(e *emit.Emitter, decl *ast.SagaDecl, base, stateType, runStepsFn string, seedLines []string, l *lower.Lowerer, ctxAlias, runtimeAlias string, hooks []sagaCompletionHook) error {
	var timeoutGo string
	if decl.Timeout != nil {
		e.Import("time") // decl.Timeout lowereiza para time.Duration(...) (lower/expr.go, lowerDurationLiteral)
		g, err := l.Expr(decl.Timeout)
		if err != nil {
			return fmt.Errorf("timeout: %w", err)
		}
		timeoutGo = g
	}
	if len(hooks) > 0 {
		e.Import("time")
	}

	e.Line("")
	e.Line("// %s é a Saga %s (§18.2, mode await): roda os passos numa goroutine à", decl.Name, decl.Name)
	e.Line("// parte e bloqueia até terminar ou o timeout expirar (REQ-24.3);")
	e.Line("// state.<Campo> é semeado a partir dos campos de mesmo nome de %s antes", decl.Handles)
	e.Line("// do 1º passo (ver a doc do arquivo sobre o receptor \"state\").")
	sig := fmt.Sprintf("func %s(ctx %s.Context, cmd %s) (*%s, error)", decl.Name, ctxAlias, decl.Handles, stateType)

	e.Block(sig, func() {
		e.Line("state := &%s{}", stateType)
		emitSagaSeed(e, seedLines)
		if len(hooks) > 0 {
			e.Line("start := time.Now()")
		}
		e.Line("done := make(chan %s.SagaResult, 1)", runtimeAlias)
		e.BlockSuffix("go func()", "()", func() {
			e.Line("done <- %s(%s.Background(), state)", runStepsFn, ctxAlias)
		})
		if timeoutGo != "" {
			e.Line("var cancel %s.CancelFunc", ctxAlias)
			e.Line("ctx, cancel = %s.WithTimeout(ctx, %s)", ctxAlias, timeoutGo)
			e.Line("defer cancel()")
		}
		e.Block("select", func() {
			e.Line("case res := <-done:")
			if len(hooks) > 0 {
				e.Block("if res.Err == nil", func() {
					emitSagaCompletionHookCalls(e, hooks, "time.Since(start).Seconds()")
				})
			}
			e.Line("return state, res.Err")
			e.Line("case <-ctx.Done():")
			e.Line("return nil, ctx.Err()")
		})
	})
	return nil
}

// emitSagaAsyncEntry emite "func <Nome>(ctx, cmd) string" + "func
// <Nome>Status(sagaId) (runtime.SagaStatus, bool)" (mode async e qualquer
// modo não reconhecido, REQ-24.3 — mesma postura conservadora de
// PolicyDecl.Delivery em decl_policy.go): os passos rodam numa goroutine
// destacada (context.Background()) contra um runtime.SagaStore em memória
// PRÓPRIO do pacote (var de pacote, sem Wire — ver a doc do arquivo), e a
// função de entrada devolve o sagaId de imediato.
//
// hooks (H3, REQ-30.3) são as Metric "on <ESTA Saga>.completed" já
// resolvidas — atualizadas dentro da MESMA goroutine que roda os passos,
// logo após "res := ...RunSteps(...)" reportar sucesso (res.Err == nil),
// ANTES de gravar o SagaStatus final — mesmo raciocínio de duração
// (start marcado antes de disparar a goroutine) do modo await.
func emitSagaAsyncEntry(e *emit.Emitter, decl *ast.SagaDecl, base, stateType, runStepsFn string, seedLines []string, ctxAlias, runtimeAlias string, hooks []sagaCompletionHook) error {
	if len(hooks) > 0 {
		e.Import("time")
	}
	storeVar := base + "SagaStore"

	e.Line("")
	e.Line("// %s é o SagaStore em memória da Saga %s (mode async, REQ-24.3) —", storeVar, decl.Name)
	e.Line("// rastreia sagaId -> SagaStatus; sem Wire (ver a doc do arquivo) — um")
	e.Line("// backend real de persistência fica para quando existir um seam dedicado a")
	e.Line("// Saga (Marco G).")
	e.Line("var %s = %s.NewMemorySagaStore()", storeVar, runtimeAlias)

	e.Line("")
	e.Line("// %s é a Saga %s (§18.2, mode async): inicia os passos numa goroutine à", decl.Name, decl.Name)
	e.Line("// parte e devolve o sagaId de imediato; consulte %sStatus(sagaId) para o", decl.Name)
	e.Line("// andamento (REQ-24.3). state.<Campo> é semeado a partir dos campos de")
	e.Line("// mesmo nome de %s antes do 1º passo.", decl.Handles)
	sig := fmt.Sprintf("func %s(ctx %s.Context, cmd %s) string", decl.Name, ctxAlias, decl.Handles)
	e.Block(sig, func() {
		e.Line("state := &%s{}", stateType)
		emitSagaSeed(e, seedLines)
		if len(hooks) > 0 {
			e.Line("start := time.Now()")
		}
		e.Line("sagaID := %s.UUID()", runtimeAlias)
		// Idempotency-Key -> sagaId estável (spec §14, G2 — REQ-20.4/REQ-24.3):
		// quando o chamador trouxe uma Idempotency-Key (repassada ao ctx desde
		// E9.2, runtime.WithIdempotencyKey), o sagaId vira uma derivação
		// DETERMINÍSTICA dela (SagaIDFromIdempotencyKey) em vez do runtime.UUID()
		// aleatório de cima — a MESMA chave sempre produz o MESMO sagaId. Se já
		// existe um status registrado sob esse sagaId (a chave já foi usada
		// antes, run em andamento ou já terminado), esta chamada é um retry
		// idempotente: devolve o sagaId de imediato SEM iniciar os passos de
		// novo — o cliente consulta %sStatus(sagaId) para o andamento real,
		// exatamente como faria com o 1º sagaId. Sem Idempotency-Key nenhuma
		// (a maioria dos chamadores hoje, já que nenhuma Saga async real do
		// wallet/shop declara idempotência — mudança inerte nesse caso), o
		// comportamento é idêntico ao de antes desta task.
		e.Block(fmt.Sprintf("if key, ok := %s.IdempotencyKeyFrom(ctx); ok", runtimeAlias), func() {
			e.Line("sagaID = %s.SagaIDFromIdempotencyKey(key)", runtimeAlias)
			e.Block(fmt.Sprintf("if _, found, _ := %s.Get(ctx, sagaID); found", storeVar), func() {
				e.Line("return sagaID")
			})
		})
		e.Line("_ = %s.Put(ctx, %s.SagaStatus{ID: sagaID, State: %s.SagaRunning})", storeVar, runtimeAlias, runtimeAlias)
		e.BlockSuffix("go func()", "()", func() {
			e.Line("bgCtx := %s.Background()", ctxAlias)
			e.Line("res := %s(bgCtx, state)", runStepsFn)
			if len(hooks) > 0 {
				e.Block("if res.Err == nil", func() {
					emitSagaCompletionHookCalls(e, hooks, "time.Since(start).Seconds()")
				})
			}
			e.Line("status := %s.SagaStatus{ID: sagaID, State: res.FinalState()}", runtimeAlias)
			e.Block("if res.Err != nil", func() {
				e.Line("status.Err = res.Err.Error()")
			})
			e.Line("_ = %s.Put(bgCtx, status)", storeVar)
		})
		e.Line("return sagaID")
	})

	e.Line("")
	e.Line("// %sStatus consulta o andamento da Saga %s iniciada em modo async pelo", decl.Name, decl.Name)
	e.Line("// sagaId devolvido por %s (REQ-24.3).", decl.Name)
	e.Block(fmt.Sprintf("func %sStatus(sagaID string) (%s.SagaStatus, bool)", decl.Name, runtimeAlias), func() {
		e.Line("st, ok, err := %s.Get(%s.Background(), sagaID)", storeVar, ctxAlias)
		e.Block("if err != nil", func() {
			e.Line("return %s.SagaStatus{}, false", runtimeAlias)
		})
		e.Line("return st, ok")
	})
	return nil
}

// --- 4. API pública. ---

// EmitSaga gera o Go de um único SagaDecl — a mesma forma de EmitSagas,
// mantendo o contrato uniforme entre as duas funções (mesmo padrão de
// EmitWorker/EmitWorkers). adapterByName (H4, REQ-31.3) é o registry de
// Adapter deste módulo (mesmo mapa que EmitPolicies/EmitUseCases já
// recebem) — habilita um passo a chamar um Adapter (ver a doc de
// emitSagaStepPhaseFunc); nil preserva o comportamento anterior a esta task
// (nenhum passo consegue chamar Adapter, o caso de toda Saga anterior a
// H4). sagaMetrics (H3, REQ-30.3) é o mapa nome-da-Saga -> Metric de
// negócio que dispara "on <ESTA Saga>.completed" (calculado pelo CHAMADOR
// via EmitMetrics — ver a doc de codegen/decl_metric.go); nil preserva o
// comportamento anterior a H3 (nenhum hook de Metric emitido).
func EmitSaga(pkg string, decl *ast.SagaDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, adapterByName map[string]*ast.AdapterDecl, sagaMetrics map[string][]*ast.MetricDecl) ([]byte, error) {
	return EmitSagas(pkg, []*ast.SagaDecl{decl}, model, tab, module, reg, adapterByName, sagaMetrics)
}

// EmitSagas gera o Go de várias SagaDecl num único arquivo (sagas.go), sem
// nenhum estado de pacote COMPARTILHADO entre elas (ao contrário de
// UseCase/Policy — "uow"/nada — cada Saga tem seu próprio SagaStore quando
// async; ver a doc do arquivo). adapterByName/sagaMetrics são repassados a
// emitSagaDecl (ver a doc de EmitSaga).
func EmitSagas(pkg string, decls []*ast.SagaDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, adapterByName map[string]*ast.AdapterDecl, sagaMetrics map[string][]*ast.MetricDecl) ([]byte, error) {
	e := emit.New(pkg)
	ctxAlias := e.Import("context")
	runtimeAlias := e.Import(RuntimeImportPath)

	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		if err := emitSagaDecl(e, decl, model, tab, module, reg, ctxAlias, runtimeAlias, adapterByName, sagaMetrics[decl.Name]); err != nil {
			return nil, fmt.Errorf("codegen: Saga %s: %w", decl.Name, err)
		}
	}

	return e.Bytes()
}

// emitSagaDecl emite o Go de uma única SagaDecl: o struct de state, as
// funções de cada passo, a tabela de runtime.Step, a função de execução
// (RunSteps) e a entrada por modo (ver a doc do arquivo). adapterByName (H4)
// é repassado a cada passo (emitSagaStepFuncs). metrics (H3) são as Metric
// de negócio que disparam "on <ESTA Saga>.completed" — resolvidas aqui
// (resolveSagaCompletionHooks) contra o MESMO env/l já seedado com "state"
// (SeedSagaStep, único receptor que existe no ponto de conclusão de uma
// Saga) e injetadas por emitSagaAwaitEntry/emitSagaAsyncEntry no ponto de
// sucesso.
func emitSagaDecl(e *emit.Emitter, decl *ast.SagaDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, ctxAlias, runtimeAlias string, adapterByName map[string]*ast.AdapterDecl, metrics []*ast.MetricDecl) error {
	stateFields, err := sagaStateFields(decl)
	if err != nil {
		return err
	}
	stateType := sagaStateTypeName(decl)
	emitSagaStateStruct(e, decl, stateFields)

	cmdDecl, err := resolveSagaCommand(tab, module, decl.Handles)
	if err != nil {
		return err
	}
	seedLines := sagaSeedFromCommandLines(stateFields, cmdDecl.Fields)

	env := lower.New(model, tab, module)
	env.SeedSagaStep(decl.Name, decl.State)
	l := lower.NewLowerer(env, reg, runtimeAlias).WithEmitter(e)
	// now()/uuid()/random(...)/random_str(...) (REQ-22.7(a)) são úteis dentro
	// de um passo de Saga (ex.: gerar um PaymentId novo em ProcessPayment.up) —
	// load/list/count NÃO se aplicam aqui (um passo não tem acesso a um
	// runtime.Tx/Store, ver a doc do arquivo sobre "state" ser o único
	// receptor), então storeGoName fica vazio: alcançável só se um corpo
	// tentasse load/list/count, o que produz o erro claro de E5.3 (BuiltinLowerer
	// configurado mas sem uso de store), nunca Go quebrado.
	l.WithBuiltins(lower.NewBuiltinLowerer(runtimeAlias, ctxAlias, ""))

	hooks, err := resolveSagaCompletionHooks(e, env, l, metrics, runtimeAlias)
	if err != nil {
		return fmt.Errorf("metric on %s.completed: %w", decl.Name, err)
	}

	base := sagaBase(decl.Name)

	steps := make([]sagaStepEmit, 0, len(decl.Steps))
	for _, step := range decl.Steps {
		se, err := emitSagaStepFuncs(e, base, step, l, ctxAlias, stateType, adapterByName)
		if err != nil {
			return err
		}
		steps = append(steps, se)
	}

	emitSagaStepsTable(e, base, runtimeAlias, stateType, steps)

	runStepsFn := base + "RunSteps"
	emitSagaRunStepsFunc(e, base, runStepsFn, ctxAlias, runtimeAlias, stateType)

	if decl.Mode == "async" {
		return emitSagaAsyncEntry(e, decl, base, stateType, runStepsFn, seedLines, ctxAlias, runtimeAlias, hooks)
	}
	return emitSagaAwaitEntry(e, decl, base, stateType, runStepsFn, seedLines, l, ctxAlias, runtimeAlias, hooks)
}

// --- 5. Metric "on Saga.completed" (H3, REQ-30.3). ---

// sagaCompletionHook é a forma já lowerizada de UMA Metric acionada por "on
// <ESTA Saga>.completed": o nome Go da var de registry (metrics.go,
// EmitMetrics — que emite a var mas NÃO o subscriber para este caso, ver a
// doc de codegen/decl_metric.go), se é counter (Add) ou histogram
// (Observe), e o texto Go já pronto do valor/labels ("" ⇒ implícito: 1 para
// counter, a duração da Saga para histogram — ver a doc do arquivo).
type sagaCompletionHook struct {
	varName   string
	isCounter bool
	valueGo   string
	labelsGo  string
}

// resolveSagaCompletionHooks lowereiza, para CADA Metric em metrics, o valor
// numérico (ou "" quando implícito) e os labels (ou "" quando ausentes)
// contra env/l JÁ seedados com "state" (SeedSagaStep, emitSagaDecl) — o
// único receptor que existe no ponto de conclusão de uma Saga (não há
// "event": uma Saga não publica nada ao concluir). Emite a var de registry
// de cada Metric (emitMetricRegistryVar, decl_metric.go) e devolve os hooks
// prontos para emitSagaAwaitEntry/emitSagaAsyncEntry injetarem no ponto de
// sucesso. metrics vazio/nil devolve (nil, nil) sem emitir nada.
func resolveSagaCompletionHooks(e *emit.Emitter, env *lower.TypeEnv, l *lower.Lowerer, metrics []*ast.MetricDecl, runtimeAlias string) ([]sagaCompletionHook, error) {
	if len(metrics) == 0 {
		return nil, nil
	}
	var fmtAlias string
	hooks := make([]sagaCompletionHook, 0, len(metrics))
	for _, m := range metrics {
		isCounter, err := metricIsCounter(m)
		if err != nil {
			return nil, err
		}
		varName, err := emitMetricRegistryVar(e, m, runtimeAlias)
		if err != nil {
			return nil, err
		}

		var valueGo string
		if m.Value != nil {
			valueGo, err = metricNumericGo(env, l, m.Value)
			if err != nil {
				return nil, fmt.Errorf("Metric %s: value: %w", m.Name, err)
			}
		}
		// valueGo == "" e histogram: implícito = duração da Saga (§21, exemplo
		// PurchaseLatency) — sempre válido aqui, já que só entramos nesta
		// função para Metric agrupadas por "on <ESTA Saga>.completed".

		var labelsGo string
		if len(m.Labels) > 0 {
			if fmtAlias == "" {
				fmtAlias = e.Import("fmt")
			}
			labelsGo, err = metricLabelsGo(env, l, fmtAlias, m.Labels)
			if err != nil {
				return nil, fmt.Errorf("Metric %s: labels: %w", m.Name, err)
			}
		}

		hooks = append(hooks, sagaCompletionHook{varName: varName, isCounter: isCounter, valueGo: valueGo, labelsGo: labelsGo})
	}
	return hooks, nil
}

// emitSagaCompletionHookCalls emite uma linha "<var>.Add(...)"/"<var>.
// Observe(...)" por hook — chamada de dentro do "if res.Err == nil" do
// ponto de sucesso de emitSagaAwaitEntry/emitSagaAsyncEntry. durationGo é o
// texto Go já pronto da duração observada (ex. "time.Since(start).
// Seconds()"), usado como valor quando um histogram não declara "value"
// (ver a doc de sagaCompletionHook).
func emitSagaCompletionHookCalls(e *emit.Emitter, hooks []sagaCompletionHook, durationGo string) {
	for _, h := range hooks {
		valueGo := h.valueGo
		if valueGo == "" {
			if h.isCounter {
				valueGo = "1"
			} else {
				valueGo = durationGo
			}
		}
		labelsGo := h.labelsGo
		if labelsGo == "" {
			labelsGo = "nil"
		}
		method := "Observe"
		if h.isCounter {
			method = "Add"
		}
		e.Line("%s.%s(%s, %s)", h.varName, method, valueGo, labelsGo)
	}
}
