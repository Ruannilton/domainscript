package lower

import (
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/token"
	"domainscript/types"
)

// stmt_test.go prova os critérios de conclusão da task E5.2 (§design codegen
// 3.6/4.3) sobre o wallet real (docs/examples/wallet), complementado por
// fixtures sintéticas só onde o wallet não exercita a forma (match, break
// all — ver comentário de cada teste), na mesma convenção de env_test.go/
// expr_test.go.

// lowerInFunc lowereiza stmts dentro de uma func Go sintética de assinatura
// sig (sem nenhum import pré-registrado além do que os statements
// registrarem sozinhos, ex. "log/slog" via LogStmt) e devolve o texto Go
// final FORMATADO — passar pelo emit.Bytes (que roda go/format.Source)
// prova que a saída é sintaticamente válida, não só um fragmento de string.
func lowerInFunc(t *testing.T, l *Lowerer, ctx StmtContext, sig string, stmts ...ast.Stmt) string {
	t.Helper()
	e := emit.New("testpkg")
	sl := NewStmtLowerer(l, e, ctx)
	var stmtErr error
	e.Block(sig, func() {
		for _, s := range stmts {
			if stmtErr != nil {
				return
			}
			stmtErr = sl.Stmt(s)
		}
	})
	if stmtErr != nil {
		t.Fatalf("lowering falhou: %v", stmtErr)
	}
	out, err := e.Bytes()
	if err != nil {
		t.Fatalf("emit.Bytes: %v", err)
	}
	return string(out)
}

// --- Critério de conclusão da task, literalmente (1a): ensure real do
// wallet, Handle Deposit -> "if !(...) { return nil, ErrInactiveWallet }". ---

func TestStmt_Ensure_CompletionCriterion_InactiveWallet(t *testing.T) {
	prog, l := newWalletLowerer(t)
	agg := findAggregate(t, prog, "Wallet")
	deposit := findHandle(t, agg, "Deposit")
	l.env.SeedHandle(agg.Name, deposit.Params)

	ensure, ok := deposit.Body.Stmts[0].(*ast.EnsureStmt)
	if !ok {
		t.Fatalf("esperava que o 1º statement de Handle Deposit fosse EnsureStmt, achei %T — o exemplo mudou?", deposit.Body.Stmts[0])
	}

	ctx := StmtContext{ZeroValues: []string{"nil"}}
	out := lowerInFunc(t, l, ctx, "func testDeposit() ([]int, error)", ensure)

	if !strings.Contains(out, "if !(state.Active == ActiveStatus(true)) {") {
		t.Fatalf("esperava \"if !(state.Active == ActiveStatus(true)) {\", got:\n%s", out)
	}
	if !strings.Contains(out, "return nil, ErrInactiveWallet") {
		t.Fatalf("esperava \"return nil, ErrInactiveWallet\", got:\n%s", out)
	}
}

// --- Critério de conclusão (1b): match sintético sobre Enum sem guard ->
// switch sem default (o wallet não usa match). ---

func TestStmt_Match_CompletionCriterion_EnumNoDefault(t *testing.T) {
	_, l := newWalletLowerer(t)
	enumType := l.env.TypeOfName("TransactionType")
	l.env.Bind("tx", enumType)

	arms := []ast.MatchStmtArm{
		{Patterns: []ast.Expr{member(ident("TransactionType"), "Deposit")}, Body: ast.NewReturnStmt(nil, ast.Span{})},
		{Patterns: []ast.Expr{member(ident("TransactionType"), "Withdrawal")}, Body: ast.NewReturnStmt(nil, ast.Span{})},
	}
	match := ast.NewMatchStmt(ident("tx"), arms, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testMatch()", match)

	if !strings.Contains(out, "switch tx {") {
		t.Fatalf("esperava \"switch tx {\", got:\n%s", out)
	}
	if !strings.Contains(out, "case TransactionTypeDeposit:") || !strings.Contains(out, "case TransactionTypeWithdrawal:") {
		t.Fatalf("esperava os 2 cases de Enum, got:\n%s", out)
	}
	if strings.Contains(out, "default:") {
		t.Fatalf("esperava NENHUM default (exaustividade já garantida pelo front-end), got:\n%s", out)
	}
}

// --- Critério de conclusão (1c): for sintético com break all aninhado em 2
// níveis -> label no for MAIS EXTERNO, "break <label>" no break all interno
// (o wallet não usa for/break). ---

func TestStmt_For_CompletionCriterion_BreakAllNestedLabel(t *testing.T) {
	_, l := newWalletLowerer(t)
	listInt := &types.Generic{Ctor: "List", Args: []types.Type{&types.Primitive{Name: "integer"}}}
	l.env.Bind("xs", listInt)
	l.env.Bind("ys", listInt)

	cond := ast.NewBinaryExpr(token.LE, ident("b"), lit(token.INT, "10"), ast.Span{})
	innerEnsure := ast.NewEnsureStmt(cond, ast.NewBreakStmt(true, ast.Span{}), ast.Span{})
	innerFor := ast.NewForStmt("b", ident("ys"), ast.NewBlock([]ast.Stmt{innerEnsure}, ast.Span{}), ast.Span{})
	outerFor := ast.NewForStmt("a", ident("xs"), ast.NewBlock([]ast.Stmt{innerFor}, ast.Span{}), ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testFor()", outerFor)

	if strings.Count(out, "outer1:") != 1 {
		t.Fatalf("esperava exatamente 1 label \"outer1:\" (no for MAIS EXTERNO), got:\n%s", out)
	}
	if !strings.Contains(out, "break outer1") {
		t.Fatalf("esperava \"break outer1\" no break all interno, got:\n%s", out)
	}

	labelIdx := strings.Index(out, "outer1:")
	outerForIdx := strings.Index(out, "for _, a := range xs")
	innerForIdx := strings.Index(out, "for _, b := range ys")
	if labelIdx == -1 || outerForIdx == -1 || innerForIdx == -1 {
		t.Fatalf("esperava label + for externo + for interno todos presentes, got:\n%s", out)
	}
	if !(labelIdx < outerForIdx && outerForIdx < innerForIdx) {
		t.Fatalf("esperava a ordem label -> for externo -> for interno, got:\n%s", out)
	}
}

// --- 2. Apply DepositPerformed completo do wallet real: assignment
// composto SEM hoisting (state.active = ...) NÃO existe aqui, mas
// state.balance = state.balance + event.amount PRECISA de hoisting (.Add
// devolve (Money, error), Money declara Operator +), e o ExprStmt
// state.entries.add(StatementEntry(...)) precisa de hoisting da construção
// composta. StmtContext{Panics: true} (Apply nunca devolve erro). ---

func TestStmt_Apply_DepositPerformed_RealWallet(t *testing.T) {
	prog, l := newWalletLowerer(t)
	agg := findAggregate(t, prog, "Wallet")
	var apply *ast.ApplyDecl
	for _, a := range agg.Appliers {
		if a.Event == "DepositPerformed" {
			apply = a
			break
		}
	}
	if apply == nil {
		t.Fatalf("esperava Apply DepositPerformed no Aggregate Wallet — o exemplo mudou?")
	}
	l.env.SeedApply(agg.Name, apply.Event)
	l.BindGoName("event", "ev")

	ctx := StmtContext{Panics: true}
	out := lowerInFunc(t, l, ctx, "func applyDepositPerformed(state Wallet, ev DepositPerformed)", apply.Body.Stmts...)

	if !strings.Contains(out, "tmp1, err := state.Balance.Add(ev.Amount)") {
		t.Fatalf("esperava hoisting de state.Balance.Add(ev.Amount) em tmp1, got:\n%s", out)
	}
	if !strings.Contains(out, "panic(err)") {
		t.Fatalf("esperava panic(err) no caminho de erro (ctx.Panics=true), got:\n%s", out)
	}
	if !strings.Contains(out, "state.Balance = tmp1") {
		t.Fatalf("esperava a atribuição final referenciando a temporária, got:\n%s", out)
	}
	if !strings.Contains(out, "tmp2, err := NewStatementEntry(TransactionTypeDeposit, ev.Amount, ev.Description)") {
		t.Fatalf("esperava hoisting da construção de StatementEntry em tmp2, got:\n%s", out)
	}
	if !strings.Contains(out, "state.Entries.Add(tmp2)") {
		t.Fatalf("esperava state.Entries.Add(tmp2), got:\n%s", out)
	}
}

// --- 3. AssignStmt de alvo nu: "wallet = load Wallet(cmd.walletId)" (do
// UseCase.execute real, PerformDeposit) — prova só o mecanismo ":=" (1ª
// atribuição) vs "=" (reatribuição) do AssignStmt (REQ-22.8). A lowering do
// PRÓPRIO "load" (repo.Load, cláusulas) é E5.3 (builtins.go) — fora do
// escopo desta task, e Lowerer.Expr (E5.1) não tem case para QueryExpr — daí
// o RHS de teste usar "cmd.walletId" (já suportado, mesmo campo referenciado
// pelo load real) no lugar da QueryExpr; o alvo ("wallet", nu, 1ª ocorrência
// no escopo) é o que este teste de fato exercita. Mesma ambiguidade do
// parser documentada em env_test.go (TestInferAssignRHS_LoadFromRealUseCase)
// para esta mesma linha do wallet.
func TestStmt_Assign_BareIdent_FirstOccurrenceUsesShortDecl(t *testing.T) {
	prog, l := newWalletLowerer(t)
	uc := findUseCase(t, prog, "PerformDeposit")
	l.env.SeedUseCaseExecute(uc.Handles)

	assign := ast.NewAssignStmt(ident("wallet"), member(ident("cmd"), "walletId"), ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testExecute(cmd Deposit)", assign)
	if !strings.Contains(out, "wallet := cmd.WalletId") {
		t.Fatalf("esperava \":=\" na 1ª atribuição de \"wallet\", got:\n%s", out)
	}
}

func TestStmt_Assign_BareIdent_SecondOccurrenceUsesReassign(t *testing.T) {
	prog, l := newWalletLowerer(t)
	uc := findUseCase(t, prog, "PerformDeposit")
	l.env.SeedUseCaseExecute(uc.Handles)

	assign1 := ast.NewAssignStmt(ident("wallet"), member(ident("cmd"), "walletId"), ast.Span{})
	assign2 := ast.NewAssignStmt(ident("wallet"), member(ident("cmd"), "walletId"), ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testExecute(cmd Deposit)", assign1, assign2)
	if strings.Count(out, "wallet := cmd.WalletId") != 1 {
		t.Fatalf("esperava exatamente 1 \":=\" (só na 1ª atribuição), got:\n%s", out)
	}
	if !strings.Contains(out, "wallet = cmd.WalletId") {
		t.Fatalf("esperava \"=\" na 2ª atribuição (mesmo nome, mesmo escopo Go), got:\n%s", out)
	}
}

// AssignStmt de alvo composto: sempre "=" (mutação de campo), coberto acima
// (state.balance = ...) no teste de Apply.

// --- 4. LogStmt: um caso sintético simples (REQ-22.8). ---

func TestStmt_Log_Synthetic(t *testing.T) {
	_, l := newWalletLowerer(t)

	logStmt := ast.NewLogStmt("info", lit(token.STRING, "deposit realizado"), []ast.LogField{
		{Name: "amount", Value: lit(token.INT, "100")},
	}, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testLog()", logStmt)

	want := `slog.Info("deposit realizado", "amount", 100)`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
	if !strings.Contains(out, `"log/slog"`) {
		t.Fatalf("esperava import de \"log/slog\", got:\n%s", out)
	}
}

// --- EmitStmt (REQ-22.4): emit real do wallet, "emit DepositPerformed(
// self.id, amount, description)" -> events = append(events, &Event{...}). ---

func TestStmt_Emit_RealWalletDeposit(t *testing.T) {
	prog, l := newWalletLowerer(t)
	agg := findAggregate(t, prog, "Wallet")
	deposit := findHandle(t, agg, "Deposit")
	l.env.SeedHandle(agg.Name, deposit.Params)

	emitStmt, ok := deposit.Body.Stmts[1].(*ast.EmitStmt)
	if !ok {
		t.Fatalf("esperava que o 2º statement de Handle Deposit fosse EmitStmt, achei %T — o exemplo mudou?", deposit.Body.Stmts[1])
	}

	ctx := StmtContext{ZeroValues: []string{"nil"}}
	out := lowerInFunc(t, l, ctx, "func testDeposit(self Wallet, amount Money, description TransactionDescription) ([]int, error)", emitStmt)

	want := "events = append(events, &DepositPerformed{Id: self.Id, Amount: amount, Description: description})"
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// --- break/continue simples (REQ-22.3), fora de "break all". ---

func TestStmt_BreakContinue_Simple(t *testing.T) {
	_, l := newWalletLowerer(t)
	listInt := &types.Generic{Ctor: "List", Args: []types.Type{&types.Primitive{Name: "integer"}}}
	l.env.Bind("xs", listInt)

	body := ast.NewBlock([]ast.Stmt{
		ast.NewContinueStmt(ast.Span{}),
		ast.NewBreakStmt(false, ast.Span{}),
	}, ast.Span{})
	forStmt := ast.NewForStmt("a", ident("xs"), body, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testFor()", forStmt)
	if !strings.Contains(out, "continue") {
		t.Fatalf("esperava \"continue\", got:\n%s", out)
	}
	if !strings.Contains(out, "break\n") && !strings.Contains(out, "break}") && !strings.Contains(out, "break }") {
		t.Fatalf("esperava um \"break\" simples (sem label), got:\n%s", out)
	}
	if strings.Contains(out, "outer") {
		t.Fatalf("esperava NENHUM label (sem break all na sub-árvore), got:\n%s", out)
	}
}

// --- ensure ... else Nop, dentro de um for (REQ-22.1). ---

func TestStmt_Ensure_ElseNop_InsideFor(t *testing.T) {
	_, l := newWalletLowerer(t)
	listInt := &types.Generic{Ctor: "List", Args: []types.Type{&types.Primitive{Name: "integer"}}}
	l.env.Bind("xs", listInt)

	cond := ast.NewBinaryExpr(token.GT, ident("a"), lit(token.INT, "0"), ast.Span{})
	ensure := ast.NewEnsureStmt(cond, ast.NewExprStmt(ident("Nop"), ast.Span{}), ast.Span{})
	forStmt := ast.NewForStmt("a", ident("xs"), ast.NewBlock([]ast.Stmt{ensure}, ast.Span{}), ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testFor()", forStmt)
	if !strings.Contains(out, "if !(a > 0) {") {
		t.Fatalf("esperava \"if !(a > 0) {\", got:\n%s", out)
	}
	if !strings.Contains(out, "continue") {
		t.Fatalf("esperava \"continue\" (Nop dentro de for), got:\n%s", out)
	}
}

// TestStmt_Ensure_ElseNop_OutsideFor_FailsExplicitly prova a checagem
// defensiva: Nop fora de qualquer for sendo lowerizado é um bug de geração
// (a semântica do front-end já garante Nop só em laço, REQ-5) — erro claro,
// não um comportamento inventado.
func TestStmt_Ensure_ElseNop_OutsideFor_FailsExplicitly(t *testing.T) {
	_, l := newWalletLowerer(t)
	cond := lit(token.TRUE, "")
	ensure := ast.NewEnsureStmt(cond, ast.NewExprStmt(ident("Nop"), ast.Span{}), ast.Span{})

	e := emit.New("testpkg")
	sl := NewStmtLowerer(l, e, StmtContext{})
	err := sl.Stmt(ensure)
	if err == nil {
		t.Fatal("esperava erro: Nop fora de um for sendo lowerizado")
	}
}
