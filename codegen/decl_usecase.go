package codegen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/program"
	"domainscript/symbols"
	"domainscript/types"
)

// decl_usecase.go emite o Go de um UseCaseDecl (E7.2, REQ-20.2/20.3, §design
// 3.8): a função que abre uma unit of work (runtime.UnitOfWork.Run), executa
// o corpo do execute (load/ensure/dispatch de Handle) e propaga commit/
// rollback via o retorno de erro da closure. Fecha o ciclo write-side do
// Marco E — a primeira task a juntar Command (E7.1), Aggregate/Handle/LoadX
// (E6.1/E6.2) e o UnitOfWork do runtime (E2.1).
//
// --- Dispatch de Handle: a peça que faltava (ver codegen/lower/stmt.go) ---
//
// "wallet.Deposit(cmd.amount, cmd.description)" é um *ast.ExprStmt cujo X é
// um *ast.CallExpr{Fn: *ast.MemberExpr{X: Ident("wallet"), Name: "Deposit"}}.
// Antes desta task, StmtLowerer.exprStmtCall (E5.2) só reconhecia essa forma
// como chamada de MÉTODO EMBUTIDO (goname.GoBuiltinCall) — "Deposit" não
// está nessa tabela, então falharia. lower/stmt.go ganhou
// handleDispatchCall: quando o receptor infere para o shape de um Aggregate
// CONHECIDO (um mapa nome->decl que ESTE arquivo constrói a partir do
// parâmetro "aggregates" e passa para o StmtLowerer via
// StmtLowerer.WithHandleDispatch) e o nome do membro bate com um Handle
// desse Aggregate, a chamada vira o padrão do §design 3.8:
//
//	events, err := wallet.Deposit(caller, cmd.Amount, cmd.Description)
//	if err != nil { return err }
//	if err := tx.Append(string(wallet.id), events); err != nil { return err }
//
// A EXISTÊNCIA do *ast.AggregateDecl de verdade é necessária porque
// types.ShapeType só carrega Fields (os campos de state), não os nomes dos
// Handlers — daí "aggregates map[string]*ast.AggregateDecl" ser um parâmetro
// desta função, construído pelo CHAMADOR (nos testes desta task, a partir do
// Aggregate Wallet real parseado; quando E9.1 fizer o wiring completo do
// projeto, a partir de todos os Aggregates do módulo).
//
// --- Timeout, caller, uow (decisões documentadas aqui, únicas vezes) ---
//
//  1. "timeout" (UseCaseDecl.Timeout, uma Expr DURATION tipo "5s") é
//     lowerizado pelo MESMO Lowerer.Expr que já sabe traduzir um literal
//     DURATION (lower/expr.go, lowerDurationLiteral — reuso, não duplicação):
//     "5s" vira "time.Duration(5000000000)". Emitido como
//     "ctx, cancel := context.WithTimeout(ctx, <duração>)" + "defer cancel()".
//     UseCaseDecl.Timeout == nil pula essa parte inteira (sem context.
//     WithTimeout) — nenhum caso do wallet exercita essa ausência
//     (PerformDeposit/PerformWithdrawal declaram "timeout 5s"), mas a
//     fixture sintética desta task cobre o caminho sem timeout.
//  2. "caller" é extraído do ctx via runtime.CallerFrom(ctx) na PRIMEIRA
//     linha do corpo da função (fora da closure de uow.Run, mas ainda
//     visível dentro dela por captura de closure Go normal) — vinculado no
//     Lowerer como o local "caller" (Lowerer.BindGoName("caller", "caller")),
//     a mesma convenção de decl_aggregate.go.
//  3. "uow" é uma variável de PACOTE ainda não inicializada ("var uow
//     runtime.UnitOfWork" — só a declaração; a instância real vem do wiring,
//     E9.1). EmitUseCases (plural) declara essa var UMA VEZ por
//     arquivo/pacote, mesmo quando gera vários UseCases — a mesma convenção
//     de var compartilhada de emissores em lote.
//
// --- Wire: como cmd/<service>/main.go injeta a uow de verdade (E9.1) ---
//
// "var uow runtime.UnitOfWork" acima é NÃO-EXPORTADA (minúscula) — de
// propósito, imutabilidade de pacote é preferível a expor um ponteiro/valor
// mutável de estado global (§design 3.11). Mas cmd/<service>/main.go vive
// num pacote DIFERENTE ("main") do pacote de domínio (ex. "wallet") e por
// isso NÃO CONSEGUE atribuir a "wallet.uow" diretamente — Go não permite
// atribuir a uma variável não-exportada de outro pacote. A opção adotada
// (documentada em codegen/codegen.go, orquestrador E9.1, decisão (a) do
// prompt da task, preferida a exportar a var): uma função exportada Wire
// no MESMO arquivo/pacote que declara "uow", que faz "uow = u" — o único
// lugar que escreve na var. cmd/main.go chama "<pkg>.Wire(uow)" na
// inicialização (ver codegen.go, generateCmdMain). Só existe quando o
// módulo de fato declara UseCases (só então "uow" existe para injetar).
func emitUOWWireFunc(e *emit.Emitter, runtimeAlias string) {
	e.Line("")
	e.Line("// Wire injeta a unit of work usada pelos UseCases deste pacote — chamada por")
	e.Line("// cmd/<service>/main.go na inicialização (wiring in-memory, §design 3.11).")
	e.Block(fmt.Sprintf("func Wire(u %s.UnitOfWork)", runtimeAlias), func() {
		e.Line("uow = u")
	})
}

// emitUOW2PCWireFunc espelha emitUOWWireFunc para o caminho 2PC (G1, REQ-20.5,
// §design 3.8): só emitido quando ao menos um UseCase do módulo precisa dele
// (ver usecase2PCPlan) — um módulo sem UseCase cross-database (o caso de
// wallet/shop hoje) nunca ganha "uow2pc"/"Wire2PC", byte-a-byte igual a antes
// de G1.
func emitUOW2PCWireFunc(e *emit.Emitter, runtimeAlias string) {
	e.Line("")
	e.Line("// Wire2PC injeta a unit of work de duas fases usada pelos UseCases deste")
	e.Line("// pacote que tocam Aggregates de bancos XA distintos (G1, §design 3.8) —")
	e.Line("// chamada por cmd/<service>/main.go na inicialização, ao lado de Wire.")
	e.Block(fmt.Sprintf("func Wire2PC(u %s.UnitOfWork2PC)", runtimeAlias), func() {
		e.Line("uow2pc = u")
	})
}

// EmitUseCase gera o Go de um único UseCaseDecl — a mesma forma de
// EmitUseCases, mantendo o contrato uniforme entre as duas funções (mesmo
// padrão de EmitCommand/EmitCommands, EmitEvent/EmitEvents). adapters (F4,
// REQ-25.3) é o registry de Notification/Adapter do módulo — nil preserva o
// comportamento anterior a F4 (nenhum notify/call reconhecido no corpo). prog
// (G1, REQ-20.5) é o Program agregado — usado só para decidir, por UseCase,
// se ele toca Aggregates de Databases XA distintos e portanto precisa do
// caminho 2PC (ver usecase2PCPlan); nil preserva o comportamento anterior a
// G1 (sempre o caminho de uma unit of work só).
func EmitUseCase(pkg string, decl *ast.UseCaseDecl, aggregates map[string]*ast.AggregateDecl, prog *program.Program, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl) ([]byte, error) {
	return EmitUseCases(pkg, []*ast.UseCaseDecl{decl}, aggregates, prog, model, tab, module, reg, adapters)
}

// EmitUseCases gera o Go de vários UseCaseDecl num único arquivo,
// compartilhando a declaração de pacote "var uow runtime.UnitOfWork" (ver a
// doc do arquivo, decisão 3) — declarada uma única vez, mesmo quando decls
// tem mais de um UseCase (o wallet real declara 2: PerformDeposit,
// PerformWithdrawal). adapters (F4) é repassado a cada corpo via
// StmtLowerer.WithNotifyAdapters (ver emitUseCaseDecl) — habilita
// "PaymentRequest(...)" dentro de execute a reconhecer notify (Mode
// async)/call (Mode sync) do Adapter parceiro (§9.1/9.3, REQ-25.3). prog (G1)
// habilita o caminho 2PC — ver a doc de EmitUseCase; "var uow2pc
// runtime.UnitOfWork2PC"/Wire2PC só são emitidos quando ALGUM decl de fato
// precisa (usecase2PCPlan), preservando byte a byte a saída de módulos sem
// UseCase cross-database (wallet, shop).
func EmitUseCases(pkg string, decls []*ast.UseCaseDecl, aggregates map[string]*ast.AggregateDecl, prog *program.Program, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl) ([]byte, error) {
	e := emit.New(pkg)
	ctxAlias := e.Import("context")
	runtimeAlias := e.Import(RuntimeImportPath)

	e.Line("// uow é a unit of work do módulo (§design 3.8), compartilhada por todos os")
	e.Line("// UseCases deste pacote — só a declaração de pacote; a instância de verdade")
	e.Line("// vem do wiring (E9.1, Wire abaixo).")
	e.Line("var uow %s.UnitOfWork", runtimeAlias)
	emitUOWWireFunc(e, runtimeAlias)

	anyNeeds2PC := false
	for _, decl := range decls {
		if _, ok := usecase2PCPlan(decl, aggregates, prog); ok {
			anyNeeds2PC = true
			break
		}
	}
	if anyNeeds2PC {
		e.Line("")
		e.Line("// uow2pc é a unit of work de duas fases (G1, REQ-20.5, §design 3.8) usada")
		e.Line("// pelos UseCases deste pacote que tocam Aggregates de bancos XA distintos —")
		e.Line("// só a declaração; a instância real vem do wiring (Wire2PC abaixo).")
		e.Line("var uow2pc %s.UnitOfWork2PC", runtimeAlias)
		emitUOW2PCWireFunc(e, runtimeAlias)
	}

	// Roteamento de FileStorage (G1a, §2.5): calculado UMA VEZ para o módulo
	// (independe de qual UseCase está sendo emitido) e anexado ao
	// BuiltinLowerer de cada decl (ver emitUseCaseDecl) — só produz efeito
	// quando o corpo de fato usa store/signed_url/delete file/load File(ref);
	// um módulo sem storage{}/FileStorage nenhum (todo módulo antes de G1a)
	// devolve (nil, "", nil) aqui, preservando o comportamento anterior.
	mod := programModule(prog, module)
	fsByField, err := moduleFileStorageRouting(aggregates, mod)
	if err != nil {
		return nil, fmt.Errorf("codegen: módulo %s: %w", module, err)
	}
	fsDefault := moduleFileStorageDefault(mod)

	// Idempotência (G2, REQ-20.4, spec §14): "idem" e o worker de limpeza só
	// existem quando ALGUM UseCase deste módulo declara "idempotency { ... }"
	// (o caso comum — nenhum do wallet/shop declara — preserva byte a byte a
	// saída de antes desta task). idemBlock é o bloco de módulo (mod.ds
	// Idempotency{}, pode ser nil) que planUseCaseIdempotency usa como
	// default quando o próprio UseCase não sobrescreve required/window.
	idemBlock := moduleIdempotencyBlock(mod)
	anyIdempotency := false
	for _, decl := range decls {
		if decl.Idempotency != nil {
			anyIdempotency = true
			break
		}
	}
	if anyIdempotency {
		emitIdempotencyStoreVar(e, runtimeAlias)
	}

	for _, decl := range decls {
		e.Line("")
		if err := emitUseCaseDecl(e, decl, aggregates, prog, model, tab, module, reg, adapters, ctxAlias, runtimeAlias, fsByField, fsDefault, idemBlock); err != nil {
			return nil, err
		}
	}

	if anyIdempotency {
		emitIdempotencyCleanupStarter(e, ctxAlias, runtimeAlias)
	}

	return e.Bytes()
}

// emitCrossTenantBypass emite, logo após "caller, _ := runtime.CallerFrom(ctx)",
// as duas linhas que materializam "tenancy: cross_tenant" (G5, REQ-27.3,
// spec §13.3) para UM UseCase que declara esse opt-in: uma entrada de
// auditoria via log/slog (tenant ativo, caller, nome do UseCase — o mínimo
// estrutural que "trilha de auditoria" pede, mesmo estilo simples de
// slog já usado por uow.go.txt/twophase.go.txt para outros eventos
// relevantes de runtime) e "ctx = runtime.WithCrossTenantBypass(ctx)" —
// dali em diante, TODO Load/Append desta unit of work (via tx, que carrega
// este MESMO ctx internamente — ver uow.go.txt/rtsrc/eventstore.go.txt)
// enxerga aggregates de QUALQUER tenant, não só o do caller (§13.2: o
// filtro row_level é isso que fica suspenso). A exigência de "role
// privilegiada" do REQ-27.3 já é responsabilidade do bloco "access {
// requires caller.hasRole(...) }" que o UseCase declara (E7.2/§23,
// inalterado por esta task) — não duplicada aqui.
func emitCrossTenantBypass(e *emit.Emitter, decl *ast.UseCaseDecl, runtimeAlias string) {
	slogAlias := e.Import("log/slog")
	e.Line("tenant, _ := %s.TenantFrom(ctx)", runtimeAlias)
	e.Line(
		"%s.Warn(%q, \"usecase\", %q, \"tenant\", tenant.ID, \"caller\", caller.ID())",
		slogAlias, "cross-tenant access (spec §13.3)", decl.Name,
	)
	e.Line("ctx = %s.WithCrossTenantBypass(ctx)", runtimeAlias)
}

// emitUseCaseDecl emite a função de um único UseCaseDecl (ver a doc do
// arquivo para as decisões de timeout/caller/uow). Quando usecase2PCPlan
// reconhece que decl toca Aggregates de 2+ Databases XA distintos (G1), emite
// o caminho 2PC (uow2pc.Run com "txs map[string]runtime.Tx", cada dispatch/
// load roteado ao Tx do Database certo — ver lower.WithHandleDispatchRouted/
// BuiltinLowerer.WithPerAggregateStore); senão, o caminho de sempre (uow.Run
// com um "tx" só, Marco E/F, byte a byte inalterado).
func emitUseCaseDecl(e *emit.Emitter, decl *ast.UseCaseDecl, aggregates map[string]*ast.AggregateDecl, prog *program.Program, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl, ctxAlias, runtimeAlias string, fsByField map[string]string, fsDefault string, idemBlock *ast.ConfigBlock) error {
	env := lower.New(model, tab, module)
	env.SeedUseCaseExecute(decl.Handles)
	l := lower.NewLowerer(env, reg, runtimeAlias).WithEmitter(e)
	l.BindGoName("caller", "caller")

	// Idempotência (G2, REQ-20.4, spec §14): decl.Idempotency == nil (o caso
	// comum) devolve plan == nil e NADA muda daqui pra baixo — innerName ==
	// decl.Name, a mesma função pública de sempre carrega o corpo direto,
	// byte a byte igual à saída de antes desta task. Só quando plan != nil o
	// corpo migra para um nome PRIVADO (innerName) e a função pública vira o
	// wrapper de idempotência (emitIdempotencyWrapper, ao final).
	idemPlan, err := planUseCaseIdempotency(decl, idemBlock, l)
	if err != nil {
		return fmt.Errorf("codegen: %w", err)
	}
	innerName := decl.Name
	if idemPlan != nil {
		innerName = unexportedRunName(decl.Name)
	}

	dbNames, needs2PC := usecase2PCPlan(decl, aggregates, prog)
	var txGoNameFor func(string) string
	if needs2PC {
		txGoNameFor = func(aggName string) string {
			db := prog.DatabaseOfAggregate(aggName)
			return fmt.Sprintf("txs[%s]", strconv.Quote(db.Name))
		}
		l.WithBuiltins(lower.NewBuiltinLowerer(runtimeAlias, "ctx", "txs").WithPerAggregateStore(txGoNameFor).WithFileStorage(fsByField, fsDefault))
	} else {
		l.WithBuiltins(lower.NewBuiltinLowerer(runtimeAlias, "ctx", "tx").WithFileStorage(fsByField, fsDefault))
	}

	var timeoutGo string
	if decl.Timeout != nil {
		e.Import("time")
		g, err := l.Expr(decl.Timeout)
		if err != nil {
			return fmt.Errorf("codegen: UseCase %s: timeout: %w", decl.Name, err)
		}
		timeoutGo = g
	}
	// signed_url(ref, expires: <duração>) (G1a, §2.5) pode aparecer dentro de
	// um execute, não só de um Query.Body — mesmo motivo/mesma checagem que
	// emitQueryDecl faz (decl_query.go, bodyUsesSignedURL): garante o import
	// de "time" ANTES de lowerizar, mesmo quando Timeout é nil.
	if bodyUsesSignedURL(decl.Execute) {
		e.Import("time")
	}

	sig := fmt.Sprintf("func %s(ctx %s.Context, cmd %s) error", innerName, ctxAlias, decl.Handles)

	if idemPlan != nil {
		e.Line("// %s carrega o corpo de verdade do UseCase %s (§5.2) — a idempotência", innerName, decl.Name)
		e.Line("// (spec §14, G2) mora no wrapper público %s, abaixo.", decl.Name)
	} else {
		e.Line("// %s é o UseCase %s (§5.2): abre uma unit of work, executa", decl.Name, decl.Name)
		e.Line("// o corpo (load/ensure/dispatch de Handle) e propaga commit/rollback.")
	}

	var bodyErr error
	e.Block(sig, func() {
		if timeoutGo != "" {
			e.Line("ctx, cancel := %s.WithTimeout(ctx, %s)", ctxAlias, timeoutGo)
			e.Line("defer cancel()")
		}
		e.Line("caller, _ := %s.CallerFrom(ctx)", runtimeAlias)
		if decl.Tenancy == "cross_tenant" {
			emitCrossTenantBypass(e, decl, runtimeAlias)
		}

		stmtCtx := lower.StmtContext{ZeroValues: []string{}, SuccessReturn: "return nil", CtxVar: "ctx"}

		if needs2PC {
			sl := lower.NewStmtLowerer(l, e, stmtCtx).WithHandleDispatchRouted(aggregates, txGoNameFor).WithNotifyAdapters(adapters, "ctx")
			dbNamesGo := make([]string, len(dbNames))
			for i, name := range dbNames {
				dbNamesGo[i] = strconv.Quote(name)
			}
			header := fmt.Sprintf("return uow2pc.Run(ctx, []string{%s}, func(txs map[string]%s.Tx) error", strings.Join(dbNamesGo, ", "), runtimeAlias)
			e.BlockSuffix(header, ")", func() {
				if bodyErr = sl.Block(decl.Execute); bodyErr != nil {
					return
				}
				e.Line("return nil")
			})
			return
		}

		sl := lower.NewStmtLowerer(l, e, stmtCtx).WithHandleDispatch(aggregates, "tx").WithNotifyAdapters(adapters, "ctx")
		e.BlockSuffix(fmt.Sprintf("return uow.Run(ctx, func(tx %s.Tx) error", runtimeAlias), ")", func() {
			if bodyErr = sl.Block(decl.Execute); bodyErr != nil {
				return
			}
			e.Line("return nil")
		})
	})
	if bodyErr != nil {
		return fmt.Errorf("codegen: UseCase %s: %w", decl.Name, bodyErr)
	}

	if idemPlan != nil {
		emitIdempotencyWrapper(e, decl, idemPlan, innerName, ctxAlias, runtimeAlias)
		emitIdempotencyExportedIsReplay(e, decl, ctxAlias, runtimeAlias)
	}
	return nil
}

// --- G1: detecção estática de UseCase cross-database (REQ-20.5, §design 3.8). ---

// usecase2PCPlan decide se decl precisa do caminho 2PC: toca (via
// touchedAggregates) 2+ Aggregates cujos Databases (prog.DatabaseOfAggregate)
// são TODOS supportsXA e providos por um adapter real que este gerador sabe
// coordenar 2PC de verdade sobre (hoje só "sqlite" — ver
// program.Database.Provider/codegen/sqlrt), e esses Databases são 2+
// DISTINTOS por nome. Devolve os nomes (ordenados, NFR-13) e ok=true só
// nesse caso — em QUALQUER outro (prog nil, menos de 2 Aggregates tocados,
// bancos não-XA, provider não reconhecido, ou um único Database mesmo que
// XA) o UseCase segue o caminho de sempre: uma unit of work só, que Marco E
// já documenta como "degenera em commit local" quando o backend é in-memory
// (§design 3.8) — o front-end (REQ-5.9) já barrou o único caso realmente
// perigoso (bancos distintos SEM XA universal), então esta função só decide
// QUAL implementação de unit of work usar, nunca se é seguro prosseguir.
func usecase2PCPlan(decl *ast.UseCaseDecl, aggregates map[string]*ast.AggregateDecl, prog *program.Program) (dbNames []string, ok bool) {
	if prog == nil {
		return nil, false
	}
	touched := touchedAggregates(decl.Execute, aggregates)
	if len(touched) < 2 {
		return nil, false
	}

	seen := make(map[string]bool, len(touched))
	for _, agg := range touched {
		db := prog.DatabaseOfAggregate(agg)
		if db == nil || !db.SupportsXA {
			return nil, false
		}
		if _, ok := recognizedSQLProvider(db.Provider); !ok {
			return nil, false
		}
		seen[db.Name] = true
	}
	if len(seen) < 2 {
		return nil, false
	}

	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, true
}

// touchedAggregates varre ESTATICAMENTE (sem lowering) os statements de
// nível superior de execute, reconhecendo o padrão "local = load Agg(...)"
// seguido, em qualquer statement depois, de um dispatch "local.Handle(...)"
// sobre um Handle conhecido de Agg — o MESMO padrão que
// repairLoadDispatchExecute já normaliza (usecase_repair.go) e que
// lower.StmtLowerer.handleDispatchCall reconhece na hora de emitir de
// verdade; esta função só faz uma leitura antecipada, ANTES de emitir, para
// decidir se decl precisa do caminho 2PC (usecase2PCPlan).
//
// Devolve os nomes de Aggregate tocados, ordenados e sem repetição
// (determinismo, NFR-13). É deliberadamente conservadora: um corpo que ela
// não reconheça (formas que repairLoadDispatchExecute não normaliza, ou
// dispatch sobre uma expressão que não é um Ident simples) simplesmente não
// conta como "tocando" aquele Aggregate — no pior caso SUBESTIMA o conjunto
// e mantém o UseCase no caminho de commit único (o comportamento seguro de
// sempre), nunca superestima a ponto de forçar 2PC indevidamente.
func touchedAggregates(execute *ast.Block, aggregates map[string]*ast.AggregateDecl) []string {
	if execute == nil || len(aggregates) == 0 {
		return nil
	}

	locals := make(map[string]string) // nome do local -> nome do Aggregate
	touched := make(map[string]bool)

	for _, s := range execute.Stmts {
		switch st := s.(type) {
		case *ast.AssignStmt:
			id, ok := st.Target.(*ast.Ident)
			if !ok {
				continue
			}
			qe, ok := st.Value.(*ast.QueryExpr)
			if !ok || qe.Op != "load" {
				continue
			}
			call, ok := qe.Target.(*ast.CallExpr)
			if !ok {
				continue
			}
			aggIdent, ok := call.Fn.(*ast.Ident)
			if !ok {
				continue
			}
			if _, known := aggregates[aggIdent.Name]; known {
				locals[id.Name] = aggIdent.Name
			}
		case *ast.ExprStmt:
			call, ok := st.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			mem, ok := call.Fn.(*ast.MemberExpr)
			if !ok {
				continue
			}
			recv, ok := mem.X.(*ast.Ident)
			if !ok {
				continue
			}
			aggName, ok := locals[recv.Name]
			if !ok {
				continue
			}
			if aggDeclHasHandle(aggregates[aggName], mem.Name) {
				touched[aggName] = true
			}
		}
	}

	names := make([]string, 0, len(touched))
	for name := range touched {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// aggDeclHasHandle devolve true se decl declara um Handle chamado name —
// espelha lower.findHandleDecl (não-exportado do pacote lower), reimplantado
// aqui pela mesma razão de queryClauseExtra em decl_query.go.
func aggDeclHasHandle(decl *ast.AggregateDecl, name string) bool {
	if decl == nil {
		return false
	}
	for _, h := range decl.Handlers {
		if h != nil && h.Name == name {
			return true
		}
	}
	return false
}
