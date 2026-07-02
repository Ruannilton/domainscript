package codegen

import (
	"fmt"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
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

// EmitUseCase gera o Go de um único UseCaseDecl — a mesma forma de
// EmitUseCases, mantendo o contrato uniforme entre as duas funções (mesmo
// padrão de EmitCommand/EmitCommands, EmitEvent/EmitEvents).
func EmitUseCase(pkg string, decl *ast.UseCaseDecl, aggregates map[string]*ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	return EmitUseCases(pkg, []*ast.UseCaseDecl{decl}, aggregates, model, tab, module, reg)
}

// EmitUseCases gera o Go de vários UseCaseDecl num único arquivo,
// compartilhando a declaração de pacote "var uow runtime.UnitOfWork" (ver a
// doc do arquivo, decisão 3) — declarada uma única vez, mesmo quando decls
// tem mais de um UseCase (o wallet real declara 2: PerformDeposit,
// PerformWithdrawal).
func EmitUseCases(pkg string, decls []*ast.UseCaseDecl, aggregates map[string]*ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	e := emit.New(pkg)
	ctxAlias := e.Import("context")
	runtimeAlias := e.Import(RuntimeImportPath)

	e.Line("// uow é a unit of work do módulo (§design 3.8), compartilhada por todos os")
	e.Line("// UseCases deste pacote — só a declaração de pacote; a instância de verdade")
	e.Line("// vem do wiring (E9.1, Wire abaixo).")
	e.Line("var uow %s.UnitOfWork", runtimeAlias)
	emitUOWWireFunc(e, runtimeAlias)

	for _, decl := range decls {
		e.Line("")
		if err := emitUseCaseDecl(e, decl, aggregates, model, tab, module, reg, ctxAlias, runtimeAlias); err != nil {
			return nil, err
		}
	}

	return e.Bytes()
}

// emitUseCaseDecl emite a função de um único UseCaseDecl (ver a doc do
// arquivo para as decisões de timeout/caller/uow).
func emitUseCaseDecl(e *emit.Emitter, decl *ast.UseCaseDecl, aggregates map[string]*ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, ctxAlias, runtimeAlias string) error {
	env := lower.New(model, tab, module)
	env.SeedUseCaseExecute(decl.Handles)
	l := lower.NewLowerer(env, reg, runtimeAlias)
	l.BindGoName("caller", "caller")
	l.WithBuiltins(lower.NewBuiltinLowerer(runtimeAlias, "ctx", "tx"))

	var timeoutGo string
	if decl.Timeout != nil {
		e.Import("time")
		g, err := l.Expr(decl.Timeout)
		if err != nil {
			return fmt.Errorf("codegen: UseCase %s: timeout: %w", decl.Name, err)
		}
		timeoutGo = g
	}

	sig := fmt.Sprintf("func %s(ctx %s.Context, cmd %s) error", decl.Name, ctxAlias, decl.Handles)

	e.Line("// %s é o UseCase %s (§5.2): abre uma unit of work, executa", decl.Name, decl.Name)
	e.Line("// o corpo (load/ensure/dispatch de Handle) e propaga commit/rollback.")

	var bodyErr error
	e.Block(sig, func() {
		if timeoutGo != "" {
			e.Line("ctx, cancel := %s.WithTimeout(ctx, %s)", ctxAlias, timeoutGo)
			e.Line("defer cancel()")
		}
		e.Line("caller, _ := %s.CallerFrom(ctx)", runtimeAlias)

		stmtCtx := lower.StmtContext{ZeroValues: []string{}, SuccessReturn: "return nil"}
		sl := lower.NewStmtLowerer(l, e, stmtCtx).WithHandleDispatch(aggregates, "tx")

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
	return nil
}
