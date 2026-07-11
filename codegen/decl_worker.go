package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// decl_worker.go emite o Go de um WorkerDecl (F2, REQ-23.2/23.3, §design
// 3.10): um job agendado conforme WorkerDecl.Schedule — "every" (ticker),
// "cron" (agenda mínima do runtime, ver a doc de codegen/rtsrc/worker.go.txt),
// "continuous" (loop consumindo um runtime.Source[T]) — honrando
// concurrency/batchSize/maxRate (Settings) e onError.retry (também Settings,
// ver a doc abaixo sobre por que onError mora ali). Segue o MESMO padrão de
// decl_policy.go (F1): um subscriber vira aqui um job; o corpo do execute usa
// a MESMA StmtLowerer/Lowerer de Handle/UseCase/Policy (REQ-23.5).
//
// --- Por que onError.retry está em WorkerDecl.Settings, não num campo dedicado ---
//
// O parser (parser/parse_decl.go, parseWorker) historicamente só CONSUMIA o
// bloco "onError { ... }" sem guardar nada (skipBraceBlock) — um bug latente
// desde 4B.9 (front-end): a sintaxe era aceita, mas o retry/backoff que REQ-
// 23.3 exige gerar nunca sobrevivia ao parse. Esta task muda parseWorker para
// guardar "onError" como mais um ConfigEntry de Settings (mesma forma "Key {
// Object }" que parse_config.go já reconhece para outros sub-blocos sem
// dois-pontos, ex. Telemetry.traces) em vez de inventar um campo novo em
// ast.WorkerDecl — WorkerDecl já trata "timeout"/"concurrency"/"batchSize"/
// "maxRate" como ConfigEntry soltos; onError é só mais um, aninhado.
// workerRetryPolicy (abaixo) faz a leitura.
//
// --- Settings: concurrency/batchSize/maxRate (REQ-23.2) ---
//
// Os 3 exemplos reais do spec (§8) usam sempre um literal INTEIRO nu
// ("concurrency: 1", "batchSize: 50", "maxRate: 200") — nenhum usa a forma
// RATE do lexer ("300/min", token.RATE), que também não tem lowering em
// lower/expr.go ainda (nenhum outro construto a exercita). workerConfigInt
// (abaixo) portanto só aceita INT — uma RATE aqui é erro de geração claro,
// não um palpite de unidade.
//
// --- Escopo do bloco `source` de um Worker `continuous` (REQ-23.2) ---
//
// Só a forma do exemplo canônico do spec (§8) é suportada: exatamente 1
// statement, "list T [binding] [where cond]" — nenhuma outra cláusula SQL-like
// (join/orderBy/skip/take/as). Isso NÃO usa o hoisting genérico de "list" de
// lower/stmt.go (hoistList -> BuiltinLowerer.ListCall -> "<store>.Select(ctx,
// runtime.Query[T]{...})", desde o ciclo Read Side — REQ-33/REQ-36/REQ-38):
// aquele caminho monta uma chamada a um método que NENHUMA implementação de
// runtime.EventStore declara hoje (documentado como "provisório" desde
// E5.3/builtins.go, nunca de fato conectado por E8.1 — ver decl_query.go, que
// resolve "list <VO>" com sua PRÓPRIA correlação em vez de hoistList) — usá-lo
// aqui geraria Go que não compila. Em vez disso, o
// binding e o where (se houver) só decidem o TIPO do item (via TypeEnv,
// astutil.HeadName + TypeOfName) e um PREDICADO Go puro (workerSourcePredicate,
// lowerizado com um TypeEnv filho que vincula o binding ao tipo do item) —
// aplicado item a item sobre o que quer que popule o runtime.Source[T] do
// Worker (ver a doc de emitContinuous sobre o wiring desse Source). Mais
// cláusulas entram quando um exemplo real precisar (mesmo espírito da nota de
// escopo de decl_query.go sobre where/orderBy/skip/take).
//
// --- Corpo via lowering (REQ-23.5) ---
//
// Worker não tem "caller"/"self" contextuais (resolver/receivers.go não lista
// nenhum receptor para constructWorkerSource/constructWorkerExecute — ao
// contrário de Policy, que tem "event"/"caller"). O único nome de corpo é o
// ExecuteParam (schedule continuous), semeado via TypeEnv.SeedWorkerExecute
// (lower/env.go) com o tipo do item da fonte. Builtins (load/list/count/
// exists, E5.3) NÃO são conectados aqui — mesma decisão e mesmo raciocínio de
// decl_policy.go: nenhum corpo real de Worker os exercita ainda, e a forma
// certa de um load dentro de um Worker é decisão de design maior deixada para
// quando um exemplo real precisar dela.
//
// --- Wire vs. StartWorkers (a colisão do F1, resolvida para Worker) ---
//
// F1 documentou (decl_policy.go) que um módulo com UseCase E Policy colide:
// os dois emitem "func Wire(...)" com assinaturas diferentes no mesmo pacote
// Go. Um 3º "func Wire" para Worker repetiria exatamente o mesmo problema.
// Em vez disso, Worker ganha seu PRÓPRIO ponto de entrada — "func
// StartWorkers(ctx context.Context)" (emitWorkersStartFunc) — nome
// deliberadamente distinto de "Wire" que NUNCA colide com UseCase/Policy,
// mesmo quando os três coexistem no mesmo módulo (codegen.go, generate
// ModuleFiles, chama os três de forma independente). Isso não é a unificação
// completa que o design a longo prazo aponta (um único "Wire" por módulo
// registrando tudo que o módulo declara) — é o suficiente para Worker nunca
// piorar a colisão existente; UseCase+Policy no mesmo módulo continua
// recusado (ver a doc de generateModuleFiles), agora com uma nota apontando
// para esta função como o precedente de como resolver isso de vez (dar a cada
// categoria seu próprio nome de função, chamado individualmente por
// generateCmdMainFile, em vez de forçar todas num único "Wire").

// --- 1. Leitura de Settings (concurrency/batchSize/maxRate/timeout/onError). ---

// workerConfigInt busca settings[key] e exige um literal INT — concurrency/
// batchSize/maxRate são sempre um inteiro nu nos exemplos reais do spec (ver
// a doc do arquivo). ok=false sem erro quando a chave está ausente.
func workerConfigInt(settings []ast.ConfigEntry, key string) (n int64, ok bool, err error) {
	for _, entry := range settings {
		if entry.Key != key {
			continue
		}
		lit, isLit := entry.Value.(*ast.Literal)
		if !isLit || lit.Kind != token.INT {
			return 0, false, fmt.Errorf("%s: esperava um literal inteiro, veio %T", key, entry.Value)
		}
		n, err = strconv.ParseInt(lit.Value, 10, 64)
		if err != nil {
			return 0, false, fmt.Errorf("%s: %w", key, err)
		}
		return n, true, nil
	}
	return 0, false, nil
}

// workerConfigObject busca settings[key] e exige um *ast.ObjectExpr (a forma
// "Key { ... }" sem dois-pontos que parse_config.go já reconhece para
// sub-blocos, ex. onError). ok=false sem erro quando a chave está ausente.
func workerConfigObject(settings []ast.ConfigEntry, key string) (obj *ast.ObjectExpr, ok bool, err error) {
	for _, entry := range settings {
		if entry.Key != key {
			continue
		}
		obj, isObj := entry.Value.(*ast.ObjectExpr)
		if !isObj {
			return nil, false, fmt.Errorf("%s: esperava um objeto de configuração, veio %T", key, entry.Value)
		}
		return obj, true, nil
	}
	return nil, false, nil
}

// workerDurationSetting busca settings[key] e loweriza seu valor (um literal
// DURATION) via l — usado para "timeout" (WorkerDecl não tem um campo Timeout
// dedicado como UseCaseDecl; mora em Settings, ver parser.parseWorker).
func workerDurationSetting(settings []ast.ConfigEntry, key string, l *lower.Lowerer) (goExpr string, ok bool, err error) {
	for _, entry := range settings {
		if entry.Key != key {
			continue
		}
		goExpr, err = l.Expr(entry.Value)
		if err != nil {
			return "", false, fmt.Errorf("%s: %w", key, err)
		}
		return goExpr, true, nil
	}
	return "", false, nil
}

// workerRetryPolicy extrai onError.retry{attempts, backoff} de settings
// (REQ-23.3). attempts=1 (== sem retry) e backoff=runtime.BackoffFixed são o
// default quando onError está ausente ou não declara "retry" — o mesmo
// default que runtime.RetryPolicy{} (zero value) já assume (ver a doc de
// codegen/rtsrc/worker.go.txt). Um "backoff" desconhecido (nem "exponential"
// nem "fixed") cai em BackoffFixed — fallback conservador, mesmo espírito do
// fallback de PolicyDecl.Delivery em decl_policy.go: sema não valida esse
// literal, um valor que o front-end já aceitou não deveria virar erro de
// geração.
func workerRetryPolicy(settings []ast.ConfigEntry, runtimeAlias string) (attempts int64, backoffGo string, err error) {
	backoffGo = runtimeAlias + ".BackoffFixed"

	onError, hasOnError, err := workerConfigObject(settings, "onError")
	if err != nil {
		return 0, "", err
	}
	if !hasOnError {
		return 1, backoffGo, nil
	}

	var retryObj *ast.ObjectExpr
	for _, entry := range onError.Entries {
		if entry.Key != "retry" {
			continue
		}
		obj, ok := entry.Value.(*ast.ObjectExpr)
		if !ok {
			return 0, "", fmt.Errorf("onError.retry: esperava um objeto, veio %T", entry.Value)
		}
		retryObj = obj
	}
	if retryObj == nil {
		return 1, backoffGo, nil
	}

	attempts = 1
	for _, entry := range retryObj.Entries {
		switch entry.Key {
		case "attempts":
			lit, ok := entry.Value.(*ast.Literal)
			if !ok || lit.Kind != token.INT {
				return 0, "", fmt.Errorf("onError.retry.attempts: esperava um literal inteiro, veio %T", entry.Value)
			}
			n, err := strconv.ParseInt(lit.Value, 10, 64)
			if err != nil {
				return 0, "", fmt.Errorf("onError.retry.attempts: %w", err)
			}
			attempts = n
		case "backoff":
			lit, ok := entry.Value.(*ast.Literal)
			if !ok || lit.Kind != token.STRING {
				return 0, "", fmt.Errorf("onError.retry.backoff: esperava um literal string, veio %T", entry.Value)
			}
			if lit.Value == "exponential" {
				backoffGo = runtimeAlias + ".BackoffExponential"
			}
		}
	}
	return attempts, backoffGo, nil
}

// --- 2. O bloco `source` de um Worker continuous (ver a doc do arquivo). ---

// workerSourceShape é a forma reconhecida do bloco Source (ver a doc do
// arquivo sobre o escopo).
type workerSourceShape struct {
	itemTypeName string // nome Go do tipo do item (identidade — mesmo pacote, ver goname.GoFieldType)
	itemType     types.Type
	binding      string   // nome do binding do "list T binding" ("" se ausente)
	where        ast.Expr // condição de "where", nil se ausente
}

// workerParseSource reconhece "list T [binding] [where cond]" como o único
// statement de source (ver a doc do arquivo). Qualquer outra forma é erro de
// geração claro.
func workerParseSource(env *lower.TypeEnv, source *ast.Block) (workerSourceShape, error) {
	if source == nil || len(source.Stmts) != 1 {
		n := 0
		if source != nil {
			n = len(source.Stmts)
		}
		return workerSourceShape{}, fmt.Errorf("esperava exatamente 1 statement (\"list T [binding] [where cond]\"), achei %d", n)
	}
	exprStmt, ok := source.Stmts[0].(*ast.ExprStmt)
	if !ok {
		return workerSourceShape{}, fmt.Errorf("esperava um ExprStmt, veio %T", source.Stmts[0])
	}
	qe, ok := exprStmt.X.(*ast.QueryExpr)
	if !ok || qe.Op != "list" {
		return workerSourceShape{}, fmt.Errorf("esperava \"list T ...\", veio %T", exprStmt.X)
	}

	var where ast.Expr
	for _, cl := range qe.Clauses {
		if cl.Kw != "where" {
			return workerSourceShape{}, fmt.Errorf("cláusula %q não suportada sobre \"list\" (só \"where\" — join/orderBy/skip/take/as ficam para quando um exemplo real precisar, mesmo escopo de decl_query.go)", cl.Kw)
		}
		where = cl.Expr
	}

	itemTypeName := astutil.HeadName(qe.Target)
	itemType := env.TypeOfName(itemTypeName)
	if types.IsError(itemType) {
		return workerSourceShape{}, fmt.Errorf("não consegui resolver o tipo de %q (bug de geração — REQ-9 já deveria ter barrado isso)", itemTypeName)
	}
	if where != nil && qe.Binding == "" {
		return workerSourceShape{}, fmt.Errorf("cláusula \"where\" exige um binding (\"list T binding where ...\")")
	}
	return workerSourceShape{itemTypeName: itemTypeName, itemType: itemType, binding: qe.Binding, where: where}, nil
}

// workerSourcePredicate loweriza o "where" de shape (se houver) para um texto
// Go booleano puro, sobre um TypeEnv FILHO que vincula shape.binding ao tipo
// do item — o mesmo padrão de Lowerer.Lambda (lower/expr.go), reproduzido
// aqui porque o Lowerer é construído fora do pacote lower (seus campos não
// exportados impedem "&lower.Lowerer{...}" a partir de codegen — daí
// lower.NewLowerer sobre o env filho em vez disso). "" (sem erro) quando não
// há "where".
func workerSourcePredicate(shape workerSourceShape, env *lower.TypeEnv, reg *goname.VOOperatorRegistry, runtimeAlias string) (string, error) {
	if shape.where == nil {
		return "", nil
	}
	child := env.Child()
	child.Bind(shape.binding, shape.itemType)
	predLowerer := lower.NewLowerer(child, reg, runtimeAlias)
	predGo, err := predLowerer.Expr(shape.where)
	if err != nil {
		return "", fmt.Errorf("where: %w", err)
	}
	return predGo, nil
}

// --- 3. Emissão por Worker. ---

// workerEmitter carrega o estado compartilhado pelas funções de emissão de um
// único WorkerDecl (ver a doc do arquivo para o desenho geral).
type workerEmitter struct {
	e    *emit.Emitter
	decl *ast.WorkerDecl
	env  *lower.TypeEnv
	l    *lower.Lowerer
	reg  *goname.VOOperatorRegistry

	ctxAlias, runtimeAlias, timeAlias, slogAlias, syncAlias string

	concurrency, maxRate, batchSize int64
	attempts                        int64
	backoffGo                       string
	timeoutGo                       string
	hasTimeout                      bool
}

// tickFuncName devolve o nome Go (privado, não-exportado) da função que
// executa o corpo do execute uma vez — "...Tick" para every/cron (sem
// parâmetro), "...Execute" para continuous (recebe o item).
func (w *workerEmitter) tickFuncName() string {
	base := strings.ToLower(w.decl.Name[:1]) + w.decl.Name[1:]
	if w.decl.Schedule == "continuous" {
		return base + "Execute"
	}
	return base + "Tick"
}

// emitCommonVars emite as variáveis de every/cron: um semáforo que bound a
// quantidade de ticks concorrentes em voo (um "go func()" novo por tick,
// emitRetryDispatch abaixo), o rate limiter de maxRate e a política de retry
// de onError.retry (REQ-23.2/23.3).
func (w *workerEmitter) emitCommonVars() {
	e := w.e
	e.Line("sem := %s.NewSemaphore(%d)", w.runtimeAlias, w.concurrency)
	w.emitLimiterAndRetry()
}

// emitLimiterAndRetry emite só o rate limiter e a política de retry —
// compartilhado por every/cron (via emitCommonVars, que soma o semáforo) e
// continuous (que NÃO usa semáforo: concurrency ali já é o TAMANHO do pool
// de goroutines consumidoras, ver emitContinuous — declarar um semáforo não
// utilizado quebraria a compilação, "declared and not used").
func (w *workerEmitter) emitLimiterAndRetry() {
	e := w.e
	e.Line("limiter := %s.NewRateLimiter(%d)", w.runtimeAlias, w.maxRate)
	e.Line("retry := %s.RetryPolicy{Attempts: %d, Backoff: %s}", w.runtimeAlias, w.attempts, w.backoffGo)
}

// emitRetryDispatch emite o disparo de UMA execução do corpo (tick ou item)
// numa goroutine própria, respeitando concurrency (semáforo)/maxRate (rate
// limiter) e onError.retry (retry.Run) — compartilhado por every/cron
// (callArgs="ctx") e continuous (callArgs="ctx, <item>").
func (w *workerEmitter) emitRetryDispatch(fnName, callArgs string) {
	e := w.e
	e.Block("if err := limiter.Wait(ctx); err != nil", func() { e.Line("return") })
	e.Block("if err := sem.Acquire(ctx); err != nil", func() { e.Line("return") })
	e.BlockSuffix("go func()", "()", func() {
		e.Line("defer sem.Release()")
		execCtx := "ctx"
		if w.hasTimeout {
			e.Line("execCtx, cancel := %s.WithTimeout(ctx, %s)", w.ctxAlias, w.timeoutGo)
			e.Line("defer cancel()")
			execCtx = "execCtx"
			callArgs = strings.Replace(callArgs, "ctx", execCtx, 1)
		}
		e.BlockSuffix(fmt.Sprintf("err := retry.Run(%s, func() error", execCtx), ")", func() {
			e.Line("return %s(%s)", fnName, callArgs)
		})
		e.Block("if err != nil", func() {
			e.Line("%s.Error(%q, %q, %q, %q, err)", w.slogAlias, "worker: tentativas esgotadas", "worker", w.decl.Name, "error")
		})
	})
}

// emitBodyFunc emite a função que executa o corpo do execute uma vez —
// "func <fnName>(ctx context.Context<extraParam>) error" — reusando a MESMA
// StmtLowerer/Lowerer de Handle/UseCase/Policy (REQ-23.5). extraParam já vem
// pronto (", item T") ou vazio (every/cron, sem ExecuteParam).
func (w *workerEmitter) emitBodyFunc(fnName, extraParam string) error {
	e := w.e
	e.Line("")
	e.Line("// %s executa o corpo do Worker %s (§8) uma vez.", fnName, w.decl.Name)

	lastIsReturn := false
	if w.decl.Execute != nil && len(w.decl.Execute.Stmts) > 0 {
		_, lastIsReturn = w.decl.Execute.Stmts[len(w.decl.Execute.Stmts)-1].(*ast.ReturnStmt)
	}

	var bodyErr error
	sig := fmt.Sprintf("func %s(ctx %s.Context%s) error", fnName, w.ctxAlias, extraParam)
	e.Block(sig, func() {
		stmtCtx := lower.StmtContext{ZeroValues: []string{}, SuccessReturn: "return nil", CtxVar: "ctx"}
		sl := lower.NewStmtLowerer(w.l, e, stmtCtx)
		if bodyErr = sl.Block(w.decl.Execute); bodyErr != nil {
			return
		}
		if !lastIsReturn {
			e.Line("return nil")
		}
	})
	if bodyErr != nil {
		return fmt.Errorf("execute: %w", bodyErr)
	}
	return nil
}

// emitEvery emite o schedule "every <duração>" (REQ-23.2): um ticker que
// dispara emitRetryDispatch a cada intervalo, até ctx ser cancelado.
func (w *workerEmitter) emitEvery() error {
	e := w.e
	tickFn := w.tickFuncName()

	intervalGo, err := w.l.Expr(w.decl.ScheduleArg)
	if err != nil {
		return fmt.Errorf("schedule every: %w", err)
	}

	e.Line("// %s é o Worker %s (§8): roda a cada intervalo (schedule every).", w.decl.Name, w.decl.Name)
	e.Block(fmt.Sprintf("func %s(ctx %s.Context)", w.decl.Name, w.ctxAlias), func() {
		w.emitCommonVars()
		e.Line("ticker := %s.NewTicker(%s)", w.timeAlias, intervalGo)
		e.Line("defer ticker.Stop()")
		e.Block("for", func() {
			e.Block("select", func() {
				e.Line("case <-ctx.Done():")
				e.Line("return")
				e.Line("case <-ticker.C:")
			})
			w.emitRetryDispatch(tickFn, "ctx")
		})
	})

	return w.emitBodyFunc(tickFn, "")
}

// emitCron emite o schedule "cron <spec>" (REQ-23.2): usa o parser de cron
// mínimo do runtime (runtime.ParseCron/CronSchedule.Next, ver a doc de
// codegen/rtsrc/worker.go.txt sobre o escopo deliberadamente limitado) para
// calcular a próxima ocorrência a cada iteração, disparando emitRetryDispatch
// quando ela chega.
func (w *workerEmitter) emitCron() error {
	e := w.e
	tickFn := w.tickFuncName()

	specGo, err := w.l.Expr(w.decl.ScheduleArg)
	if err != nil {
		return fmt.Errorf("schedule cron: %w", err)
	}

	e.Line("// %s é o Worker %s (§8): roda conforme a agenda cron (schedule cron).", w.decl.Name, w.decl.Name)
	e.Block(fmt.Sprintf("func %s(ctx %s.Context)", w.decl.Name, w.ctxAlias), func() {
		w.emitCommonVars()
		e.Line("sched, err := %s.ParseCron(%s)", w.runtimeAlias, specGo)
		e.Block("if err != nil", func() {
			e.Line("%s.Error(%q, %q, %q, %q, err)", w.slogAlias, "worker: cron inválido", "worker", w.decl.Name, "error")
			e.Line("return")
		})
		e.Block("for", func() {
			e.Line("next := sched.Next(%s.Now())", w.timeAlias)
			e.Line("timer := %s.NewTimer(%s.Until(next))", w.timeAlias, w.timeAlias)
			e.Block("select", func() {
				e.Line("case <-ctx.Done():")
				e.Line("timer.Stop()")
				e.Line("return")
				e.Line("case <-timer.C:")
			})
			w.emitRetryDispatch(tickFn, "ctx")
		})
	})

	return w.emitBodyFunc(tickFn, "")
}

// emitContinuous emite o schedule "continuous" (REQ-23.2): um pool de
// "concurrency" goroutines consumidoras lendo de um canal ("batchSize" de
// capacidade) alimentado por uma goroutine "puller" que lê runtime.Source[T]
// item a item, aplica o predicado de "where" (se houver) e respeita maxRate
// antes de enfileirar — cada item processado passa por onError.retry como
// every/cron (emitRetryDispatch).
//
// O runtime.Source[T] wired por padrão é um runtime.SliceSource[T] vazio
// (nunca panica, só fica ocioso) — nenhum exemplo real (wallet/shop) declara
// Worker ainda, então não há uma fonte de verdade para injetar; substituir
// isso por uma fila de verdade (Marco F5, canais da topologia) é trabalho
// futuro documentado aqui, não um TODO solto.
func (w *workerEmitter) emitContinuous() error {
	e := w.e
	execFn := w.tickFuncName()

	shape, err := workerParseSource(w.env, w.decl.Source)
	if err != nil {
		return fmt.Errorf("source: %w", err)
	}
	predGo, err := workerSourcePredicate(shape, w.env, w.reg, w.runtimeAlias)
	if err != nil {
		return err
	}
	w.env.SeedWorkerExecute(w.decl.ExecuteParam, shape.itemType)

	if w.decl.ExecuteParam == "" {
		return fmt.Errorf("execute: schedule continuous exige um ExecuteParam (\"execute(item) { ... }\")")
	}
	itemVar := goname.Ident(shape.binding)
	if shape.binding == "" {
		itemVar = goname.Ident(w.decl.ExecuteParam)
	}

	sourceVar := strings.ToLower(w.decl.Name[:1]) + w.decl.Name[1:] + "Source"

	e.Line("// %s é a fonte de itens do Worker %s (§8, schedule continuous) — por", sourceVar, w.decl.Name)
	e.Line("// padrão vazia (nunca panica, só ociosa) até algo real substituir (uma fila")
	e.Line("// de verdade, Marco F5); ver a doc de emitContinuous (decl_worker.go).")
	e.Line("var %s %s.Source[%s] = %s.NewSliceSource[%s](nil)", sourceVar, w.runtimeAlias, shape.itemTypeName, w.runtimeAlias, shape.itemTypeName)
	e.Line("")

	e.Line("// %s é o Worker %s (§8): consome %s continuamente.", w.decl.Name, w.decl.Name, sourceVar)
	e.Block(fmt.Sprintf("func %s(ctx %s.Context)", w.decl.Name, w.ctxAlias), func() {
		w.emitLimiterAndRetry()
		e.Line("items := make(chan %s, %d)", shape.itemTypeName, w.batchSize)
		e.Line("var wg %s.WaitGroup", w.syncAlias)

		e.Block(fmt.Sprintf("for i := 0; i < %d; i++", w.concurrency), func() {
			e.Line("wg.Add(1)")
			e.BlockSuffix("go func()", "()", func() {
				e.Line("defer wg.Done()")
				e.Block(fmt.Sprintf("for %s := range items", itemVar), func() {
					execCtx := "ctx"
					if w.hasTimeout {
						e.Line("execCtx, cancel := %s.WithTimeout(ctx, %s)", w.ctxAlias, w.timeoutGo)
						e.Line("defer cancel()")
						execCtx = "execCtx"
					}
					e.BlockSuffix(fmt.Sprintf("err := retry.Run(%s, func() error", execCtx), ")", func() {
						e.Line("return %s(%s, %s)", execFn, execCtx, itemVar)
					})
					e.Block("if err != nil", func() {
						e.Line("%s.Error(%q, %q, %q, %q, err)", w.slogAlias, "worker: tentativas esgotadas", "worker", w.decl.Name, "error")
					})
				})
			})
		})

		e.BlockSuffix("func()", "()", func() {
			e.Line("defer close(items)")
			e.Block("for", func() {
				e.Block("select", func() {
					e.Line("case <-ctx.Done():")
					e.Line("return")
					e.Line("default:")
				})
				e.Line("%s, ok, err := %s.Next(ctx)", itemVar, sourceVar)
				e.Block("if err != nil", func() {
					e.Line("%s.Error(%q, %q, %q, %q, err)", w.slogAlias, "worker: erro ao ler da fonte", "worker", w.decl.Name, "error")
					e.Line("continue")
				})
				e.Block("if !ok", func() {
					e.Block("select", func() {
						e.Line("case <-ctx.Done():")
						e.Line("return")
						e.Line("case <-%s.After(100 * %s.Millisecond):", w.timeAlias, w.timeAlias)
					})
					e.Line("continue")
				})
				if predGo != "" {
					e.Block(fmt.Sprintf("if !(%s)", predGo), func() {
						e.Line("continue")
					})
				}
				e.Block("if err := limiter.Wait(ctx); err != nil", func() {
					e.Line("return")
				})
				e.Block("select", func() {
					e.Line("case items <- %s:", itemVar)
					e.Line("case <-ctx.Done():")
					e.Line("return")
				})
			})
		})
		e.Line("wg.Wait()")
	})
	e.Line("")

	return w.emitBodyFunc(execFn, fmt.Sprintf(", %s %s", goname.Ident(w.decl.ExecuteParam), shape.itemTypeName))
}

// emitWorkerDecl monta o workerEmitter de decl (lendo Settings — concurrency/
// batchSize/maxRate/timeout/onError) e despacha por Schedule.
func emitWorkerDecl(e *emit.Emitter, decl *ast.WorkerDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, ctxAlias, runtimeAlias, timeAlias, slogAlias, syncAlias string) error {
	env := lower.New(model, tab, module)
	l := lower.NewLowerer(env, reg, runtimeAlias)

	concurrency, hasConcurrency, err := workerConfigInt(decl.Settings, "concurrency")
	if err != nil {
		return err
	}
	if !hasConcurrency || concurrency < 1 {
		concurrency = 1
	}
	maxRate, _, err := workerConfigInt(decl.Settings, "maxRate")
	if err != nil {
		return err
	}
	batchSize, hasBatchSize, err := workerConfigInt(decl.Settings, "batchSize")
	if err != nil {
		return err
	}
	if !hasBatchSize || batchSize < 1 {
		batchSize = 1
	}
	attempts, backoffGo, err := workerRetryPolicy(decl.Settings, runtimeAlias)
	if err != nil {
		return err
	}
	timeoutGo, hasTimeout, err := workerDurationSetting(decl.Settings, "timeout", l)
	if err != nil {
		return err
	}

	w := &workerEmitter{
		e: e, decl: decl, env: env, l: l, reg: reg,
		ctxAlias: ctxAlias, runtimeAlias: runtimeAlias, timeAlias: timeAlias, slogAlias: slogAlias, syncAlias: syncAlias,
		concurrency: concurrency, maxRate: maxRate, batchSize: batchSize,
		attempts: attempts, backoffGo: backoffGo,
		timeoutGo: timeoutGo, hasTimeout: hasTimeout,
	}

	switch decl.Schedule {
	case "every":
		return w.emitEvery()
	case "cron":
		return w.emitCron()
	case "continuous":
		return w.emitContinuous()
	default:
		return fmt.Errorf("schedule %q desconhecido (esperava every/cron/continuous — o front-end já deveria ter barrado qualquer outro valor)", decl.Schedule)
	}
}

// --- 4. API pública + StartWorkers (ver a doc do arquivo sobre Wire). ---

// EmitWorker gera o Go de um único WorkerDecl — a mesma forma de
// EmitWorkers, mantendo o contrato uniforme entre as duas funções (mesmo
// padrão de EmitPolicy/EmitPolicies).
func EmitWorker(pkg string, decl *ast.WorkerDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	return EmitWorkers(pkg, []*ast.WorkerDecl{decl}, model, tab, module, reg)
}

// EmitWorkers gera o Go de vários WorkerDecl num único arquivo (workers.go),
// com "func StartWorkers(ctx context.Context)" compartilhado (ver a doc do
// arquivo) — como um módulo real pode declarar mais de um Worker.
func EmitWorkers(pkg string, decls []*ast.WorkerDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	e := emit.New(pkg)
	ctxAlias := e.Import("context")
	runtimeAlias := e.Import(RuntimeImportPath)
	timeAlias := e.Import("time")
	slogAlias := e.Import("log/slog")

	needsSync := false
	for _, d := range decls {
		if d.Schedule == "continuous" {
			needsSync = true
			break
		}
	}
	var syncAlias string
	if needsSync {
		syncAlias = e.Import("sync")
	}

	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		if err := emitWorkerDecl(e, decl, model, tab, module, reg, ctxAlias, runtimeAlias, timeAlias, slogAlias, syncAlias); err != nil {
			return nil, fmt.Errorf("codegen: Worker %s: %w", decl.Name, err)
		}
	}

	e.Line("")
	emitWorkersStartFunc(e, ctxAlias, decls)

	return e.Bytes()
}

// emitWorkersStartFunc emite "func StartWorkers(ctx context.Context)": uma
// goroutine "go <Worker>(ctx)" por Worker deste pacote — o ponto de entrada
// que cmd/<service>/main.go chama na inicialização, deliberadamente NÃO
// chamado "Wire" (ver a doc do arquivo sobre a colisão de F1).
func emitWorkersStartFunc(e *emit.Emitter, ctxAlias string, decls []*ast.WorkerDecl) {
	e.Line("// StartWorkers inicia, cada um na sua goroutine, todos os Workers deste")
	e.Line("// pacote — chamada por cmd/<service>/main.go na inicialização (wiring")
	e.Line("// in-memory, §design 3.11). Roda até ctx ser cancelado.")
	e.Block(fmt.Sprintf("func StartWorkers(ctx %s.Context)", ctxAlias), func() {
		for _, decl := range decls {
			e.Line("go %s(ctx)", decl.Name)
		}
	})
}
