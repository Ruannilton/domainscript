package lower

import (
	"testing"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/token"
)

// whereeq_test.go prova o critério de conclusão de I7.1 (REQ-38.1, §design
// read-side 3.9) do lado da LOWERING: hoistWhereEq reconhece a forma
// declarativa (positivo) e degrada com segurança para "" em toda forma que
// não se encaixa (negativo) — cada teste usa StatementEntry (wallet real,
// docs/examples/wallet/domain.ds): "type TransactionType" (Enum, comparável),
// "description TransactionDescription" (VO wrapper sobre string, comparável),
// "amount Money" (VO COMPOSTO, não-comparável — a mesma régua de "in").
func newStatementEntryQueryExpr(clauses ...ast.QueryClause) *ast.QueryExpr {
	return ast.NewQueryExpr("list", ident("StatementEntry"), "e", clauses, ast.Span{})
}

func hoistWhereEqFor(t *testing.T, l *Lowerer, qe *ast.QueryExpr) string {
	t.Helper()
	e := emit.New("testpkg")
	sl := NewStmtLowerer(l, e, StmtContext{ZeroValues: []string{"nil"}})
	return sl.hoistWhereEq(qe)
}

// TestHoistWhereEq_SingleEquality prova a forma mínima: um único
// "<binding>.<campo> == <expr independente>" vira WhereEq de 1 entrada.
func TestHoistWhereEq_SingleEquality(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	cond := ast.NewBinaryExpr(token.EQ, member(ident("e"), "description"),
		callExpr(ident("TransactionDescription"), arg(lit(token.STRING, "Salário"))), ast.Span{})
	qe := newStatementEntryQueryExpr(whereClause(cond))

	got := hoistWhereEqFor(t, l, qe)
	want := `[]runtime.FieldEq{{Field: "description", Value: TransactionDescription("Salário")}}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestHoistWhereEq_AndConjunction prova o caso central (a forma da Policy
// RefundAllOnEventCancelled, §7): um AND de duas igualdades vira WhereEq de
// 2 entradas, na ORDEM TEXTUAL — o campo Enum ("type") e o wrapper
// ("description") são ambos comparáveis.
func TestHoistWhereEq_AndConjunction(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	left := ast.NewBinaryExpr(token.EQ, member(ident("e"), "type"), member(ident("TransactionType"), "Deposit"), ast.Span{})
	right := ast.NewBinaryExpr(token.EQ, member(ident("e"), "description"),
		callExpr(ident("TransactionDescription"), arg(lit(token.STRING, "Salário"))), ast.Span{})
	cond := ast.NewBinaryExpr(token.AND, left, right, ast.Span{})
	qe := newStatementEntryQueryExpr(whereClause(cond))

	got := hoistWhereEqFor(t, l, qe)
	want := `[]runtime.FieldEq{{Field: "type", Value: TransactionTypeDeposit}, {Field: "description", Value: TransactionDescription("Salário")}}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestHoistWhereEq_NoWhereClause devolve "" quando não há "where" algum.
func TestHoistWhereEq_NoWhereClause(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := newStatementEntryQueryExpr()

	if got := hoistWhereEqFor(t, l, qe); got != "" {
		t.Fatalf(`got %q, want ""`, got)
	}
}

// TestHoistWhereEq_OrDegrades devolve "" para um OR — REQ-38.2, degradação
// segura (a closure Where continua correta e roda sempre; só a otimização
// SQL fica de fora).
func TestHoistWhereEq_OrDegrades(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	left := ast.NewBinaryExpr(token.EQ, member(ident("e"), "type"), member(ident("TransactionType"), "Deposit"), ast.Span{})
	right := ast.NewBinaryExpr(token.EQ, member(ident("e"), "description"),
		callExpr(ident("TransactionDescription"), arg(lit(token.STRING, "Salário"))), ast.Span{})
	cond := ast.NewBinaryExpr(token.OR, left, right, ast.Span{})
	qe := newStatementEntryQueryExpr(whereClause(cond))

	if got := hoistWhereEqFor(t, l, qe); got != "" {
		t.Fatalf(`got %q, want "" (OR não decompõe em WhereEq)`, got)
	}
}

// TestHoistWhereEq_NonEqualityDegrades devolve "" para uma comparação que
// não é "==" (ex. "!="), mesmo sobre um campo comparável.
func TestHoistWhereEq_NonEqualityDegrades(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	cond := ast.NewBinaryExpr(token.NEQ, member(ident("e"), "type"), member(ident("TransactionType"), "Deposit"), ast.Span{})
	qe := newStatementEntryQueryExpr(whereClause(cond))

	if got := hoistWhereEqFor(t, l, qe); got != "" {
		t.Fatalf(`got %q, want ""`, got)
	}
}

// TestHoistWhereEq_NonComparableFieldDegrades devolve "" quando o campo é um
// VO COMPOSTO ("amount Money") — igualdade estrutural não tem uma única
// coluna JSON para comparar (a mesma régua de "in", inComparableGoType).
func TestHoistWhereEq_NonComparableFieldDegrades(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	cond := ast.NewBinaryExpr(token.EQ, member(ident("e"), "amount"),
		callExpr(ident("Money"), arg(lit(token.INT, "10")), arg(lit(token.STRING, "BRL"))), ast.Span{})
	qe := newStatementEntryQueryExpr(whereClause(cond))

	if got := hoistWhereEqFor(t, l, qe); got != "" {
		t.Fatalf(`got %q, want "" (Money é VO composto, não-comparável)`, got)
	}
}

// TestHoistWhereEq_RHSReferencingItemDegrades devolve "" quando o RHS
// referencia o PRÓPRIO binding do item ("e.type == e.type" — contrived, mas
// prova a guarda: um valor que só existe DENTRO do predicado por item nunca
// vira um parâmetro de coluna avaliado uma única vez fora do loop).
func TestHoistWhereEq_RHSReferencingItemDegrades(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	cond := ast.NewBinaryExpr(token.EQ, member(ident("e"), "type"), member(ident("e"), "type"), ast.Span{})
	qe := newStatementEntryQueryExpr(whereClause(cond))

	if got := hoistWhereEqFor(t, l, qe); got != "" {
		t.Fatalf(`got %q, want ""`, got)
	}
}

// TestHoistWhereEq_ReversedOperandsRecognized prova que "<expr> ==
// <binding>.<campo>" (RHS/LHS trocados) é reconhecido igual a
// "<binding>.<campo> == <expr>".
func TestHoistWhereEq_ReversedOperandsRecognized(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	cond := ast.NewBinaryExpr(token.EQ,
		callExpr(ident("TransactionDescription"), arg(lit(token.STRING, "Salário"))), member(ident("e"), "description"), ast.Span{})
	qe := newStatementEntryQueryExpr(whereClause(cond))

	got := hoistWhereEqFor(t, l, qe)
	want := `[]runtime.FieldEq{{Field: "description", Value: TransactionDescription("Salário")}}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
