package lower

import (
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/token"
	"domainscript/types"
)

// in_test.go prova a task I4.1 (§design read-side 3.6, REQ-35.3/35.4):
// BinaryExpr(token.IN) no dispatch de operadores de lower/expr.go.
//
// ANTES desta task, "x in [...]" quebrava em Lowerer.Expr assim que "binary"
// tentava lowerizar o RHS genericamente: *ast.ListExpr não tem forma de
// expressão Go isolada (nenhum case no switch de Lowerer.Expr) — o erro
// exato era "codegen: forma de expressão *ast.ListExpr não suportada em
// Lowerer.Expr (E5.1)", confirmado empiricamente antes de qualquer mudança
// nesta task. binaryIn (expr.go) intercepta token.IN ANTES desse caminho
// genérico.
//
// Duas camadas de teste, como o resto do pacote:
//
//  1. inComparableGoType testada DIRETAMENTE — a tabela de comparabilidade
//     por EXCLUSÃO (decimal/bytes não satisfazem `comparable` em Go).
//  2. binaryIn/hoistList de ponta a ponta sobre o wallet real — RHS lista
//     literal (primitivo e Enum, a forma exata do spec §6.3), RHS coleção
//     não-literal, LHS VO composto (erro), "in" dentro de "where" (inclusive
//     composto com "and", provando que o tipo do BinaryExpr(IN) em si
//     resolve para boolean — sem isso, "and" trataria o lado "in" como
//     ErrorType e o dispatch de operador de VO devolveria um erro falso) e
//     "in" fora de "where" (dentro de um "ensure").

// --- 1. inComparableGoType: a tabela de comparabilidade, por exclusão. ---

func TestInComparableGoType_PrimitiveIntegerOK(t *testing.T) {
	got, err := inComparableGoType(&types.Primitive{Name: "integer"})
	if err != nil {
		t.Fatalf("inComparableGoType(integer): erro inesperado: %v", err)
	}
	if got != "int64" {
		t.Fatalf("got %q, want %q", got, "int64")
	}
}

func TestInComparableGoType_PrimitiveStringOK(t *testing.T) {
	got, err := inComparableGoType(&types.Primitive{Name: "string"})
	if err != nil {
		t.Fatalf("inComparableGoType(string): erro inesperado: %v", err)
	}
	if got != "string" {
		t.Fatalf("got %q, want %q", got, "string")
	}
}

// TestInComparableGoType_DecimalRejectedExplicitly prova a exclusão
// "decimal": runtime.Decimal embute um big.Int, que por sua vez embute um
// slice (nat []Word) — NÃO satisfaz `comparable` (rtsrc/decimal.go.txt
// documenta isso literalmente). slices.Contains é genérico sobre E
// comparable: usar decimal aqui seria Go que não compila (NFR-20).
func TestInComparableGoType_DecimalRejectedExplicitly(t *testing.T) {
	if _, err := inComparableGoType(&types.Primitive{Name: "decimal"}); err == nil {
		t.Fatal("esperava erro: decimal (runtime.Decimal) não satisfaz comparable")
	}
}

// TestInComparableGoType_BytesRejectedExplicitly prova a outra exclusão:
// "bytes" vira []byte — slice, nunca comparable em Go, ponto.
func TestInComparableGoType_BytesRejectedExplicitly(t *testing.T) {
	if _, err := inComparableGoType(&types.Primitive{Name: "bytes"}); err == nil {
		t.Fatal("esperava erro: bytes ([]byte) é slice, nunca comparable")
	}
}

// TestInComparableGoType_VOWrapperOverDecimalRejectedExplicitly prova que a
// exclusão se propaga por um VO wrapper: "type Score runtime.Decimal" herda
// a mesma não-comparabilidade do tipo embrulhado.
func TestInComparableGoType_VOWrapperOverDecimalRejectedExplicitly(t *testing.T) {
	vo := &types.VOType{Name: "Score", Base: &types.Primitive{Name: "decimal"}}
	if _, err := inComparableGoType(vo); err == nil {
		t.Fatal("esperava erro: wrapper sobre decimal herda a não-comparabilidade")
	}
}

// TestInComparableGoType_VOCompositeRejectedExplicitly prova a regra
// central do design (§design read-side 3.6): VO composto é SEMPRE recusado
// à esquerda de "in", mesmo quando "==" nativo funcionaria via
// goname.LowerVOBinaryDispatch (structs Go comparáveis por padrão) —
// "in" não tem forma de igualdade estrutural nativa por design.
func TestInComparableGoType_VOCompositeRejectedExplicitly(t *testing.T) {
	vo := &types.VOType{Name: "Money", Fields: []types.Field{
		{Name: "amount", Type: &types.Primitive{Name: "decimal"}},
		{Name: "currency", Type: &types.Primitive{Name: "string"}},
	}}
	if _, err := inComparableGoType(vo); err == nil {
		t.Fatal("esperava erro: ValueObject composto não tem igualdade estrutural nativa para \"in\"")
	}
}

func TestInComparableGoType_EnumOK(t *testing.T) {
	en := &types.EnumType{Name: "TransactionType", Base: &types.Primitive{Name: "string"}, Members: []string{"Deposit", "Withdrawal"}}
	got, err := inComparableGoType(en)
	if err != nil {
		t.Fatalf("inComparableGoType(enum): erro inesperado: %v", err)
	}
	if got != "TransactionType" {
		t.Fatalf("got %q, want %q", got, "TransactionType")
	}
}

// --- 2. binaryIn/hoistList de ponta a ponta, sobre o wallet real. ---

// TestBinaryIn_LiteralList_Primitive prova o ramo "RHS ListExpr literal"
// sobre um primitivo direto (Money.currency: string, sem VO wrapper) — FORA
// de "where" (um AssignStmt simples), provando REQ-35.4 ("in" funciona em
// qualquer expressão booleana, não só dentro de "where").
func TestBinaryIn_LiteralList_Primitive(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("m", l.env.TypeOfName("Money"))

	inExpr := ast.NewBinaryExpr(token.IN,
		member(ident("m"), "currency"),
		ast.NewListExpr([]ast.Expr{lit(token.STRING, "BRL"), lit(token.STRING, "USD")}, ast.Span{}),
		ast.Span{})
	assign := ast.NewAssignStmt(ident("ok"), inExpr, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testIn()", assign)

	want := `ok := slices.Contains([]string{"BRL", "USD"}, m.Currency)`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
	if !strings.Contains(out, `"slices"`) {
		t.Fatalf("esperava import \"slices\" registrado, got:\n%s", out)
	}
}

// TestHoistList_Where_In_Enum prova o critério-âncora do design (§design
// read-side 3.6, exemplo literal): "t.status in [TicketStatus.Sold,
// TicketStatus.Used]" — reproduzido sobre StatementEntry.type/TransactionType
// (o Enum real do wallet), DENTRO de um "where" de "list" (o caso do spec,
// GetMyTickets §6.3).
func TestHoistList_Where_In_Enum(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)

	inExpr := ast.NewBinaryExpr(token.IN,
		member(ident("e"), "type"),
		ast.NewListExpr([]ast.Expr{
			member(ident("TransactionType"), "Deposit"),
			member(ident("TransactionType"), "Withdrawal"),
		}, ast.Span{}),
		ast.Span{})
	qe := ast.NewQueryExpr("list", ident("StatementEntry"), "e",
		[]ast.QueryClause{whereClause(inExpr)}, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	// gofmt quebra o corpo da closure em linhas (o texto é longo demais para
	// uma única linha) — checa a assinatura e o corpo separadamente, em vez
	// de um Contains de string única (que exigiria reproduzir a quebra de
	// linha exata do gofmt).
	if !strings.Contains(out, "Where: func(e StatementEntry) (bool, error) {") {
		t.Fatalf("esperava a assinatura de Where, got:\n%s", out)
	}
	want := `return slices.Contains([]TransactionType{TransactionTypeDeposit, TransactionTypeWithdrawal}, e.Type), nil`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
	if !strings.Contains(out, `"slices"`) {
		t.Fatalf("esperava import \"slices\" registrado, got:\n%s", out)
	}
}

// TestHoistList_Where_In_ComposedWithAnd prova o achado crítico desta task:
// sem ensinar Lowerer.inferType/TypeEnv.InferAssignRHS que um
// BinaryExpr(IN) produz boolean, "e.description == ... and e.type in [...]"
// veria o braço "in" como types.ErrorType quando o "and" externo (binary(),
// caminho genérico) chama inferType nos dois operandos para alimentar
// goname.LowerVOBinaryDispatch — que confundiria ErrorType.String() com um
// nome de ValueObject desconhecido e devolveria um erro de geração FALSO
// ("ValueObject <error> não declara Operator \"&&\""). Este teste prova que
// a composição funciona.
func TestHoistList_Where_In_ComposedWithAnd(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)

	eq := ast.NewBinaryExpr(token.EQ, member(ident("e"), "description"),
		callExpr(ident("TransactionDescription"), arg(lit(token.STRING, "Salário"))), ast.Span{})
	inExpr := ast.NewBinaryExpr(token.IN, member(ident("e"), "type"),
		ast.NewListExpr([]ast.Expr{member(ident("TransactionType"), "Deposit")}, ast.Span{}), ast.Span{})
	cond := ast.NewBinaryExpr(token.AND, eq, inExpr, ast.Span{})

	qe := ast.NewQueryExpr("list", ident("StatementEntry"), "e",
		[]ast.QueryClause{whereClause(cond)}, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	want := `return e.Description == TransactionDescription("Salário") && slices.Contains([]TransactionType{TransactionTypeDeposit}, e.Type), nil`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// TestBinaryIn_CollectionVariable_NotLiteral prova o ramo "RHS não é
// ListExpr": uma variável de coleção (List<string>) — "slices.Contains(rhs,
// lhs)" direto, sem literal ao redor (REQ-35.3, segunda metade). Estilo de
// teste "Expr puro" (sem StmtLowerer/hoisting ao redor — mesmo espírito de
// TestExpr_Exists_Pure, builtins_test.go): Lowerer.WithEmitter precisa ser
// encadeado manualmente aqui, já que não há NewStmtLowerer para anexá-lo.
func TestBinaryIn_CollectionVariable_NotLiteral(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("m", l.env.TypeOfName("Money"))
	l.env.Bind("allowedCurrencies", &types.Generic{Ctor: "List", Args: []types.Type{&types.Primitive{Name: "string"}}})
	l.WithEmitter(emit.New("testpkg"))

	inExpr := ast.NewBinaryExpr(token.IN, member(ident("m"), "currency"), ident("allowedCurrencies"), ast.Span{})

	got, err := l.Expr(inExpr)
	if err != nil {
		t.Fatalf("Expr(in sobre coleção não-literal): erro inesperado: %v", err)
	}
	want := "slices.Contains(allowedCurrencies, m.Currency)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBinaryIn_CompositeVOLeftFailsExplicitly prova a régua central do
// design: um VO composto (Money, real do wallet) à esquerda de "in" é erro
// de geração claro — nunca Go que não compila (NFR-20).
func TestBinaryIn_CompositeVOLeftFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("m", l.env.TypeOfName("Money"))
	l.WithEmitter(emit.New("testpkg"))

	inExpr := ast.NewBinaryExpr(token.IN, ident("m"),
		ast.NewListExpr([]ast.Expr{ident("m")}, ast.Span{}), ast.Span{})

	if _, err := l.Expr(inExpr); err == nil {
		t.Fatal("esperava erro: Money (VO composto) não tem igualdade estrutural nativa para \"in\"")
	}
}

// TestEnsure_In_OutsideWhere prova REQ-35.4 de novo, desta vez dentro de um
// "ensure" (a outra forma explicitamente citada pela task, além do
// AssignStmt simples de TestBinaryIn_LiteralList_Primitive) — "in" como
// corpo de uma condição de Handle, sem NENHUM "where"/"list" ao redor.
func TestEnsure_In_OutsideWhere(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("m", l.env.TypeOfName("Money"))

	cond := ast.NewBinaryExpr(token.IN, member(ident("m"), "currency"),
		ast.NewListExpr([]ast.Expr{lit(token.STRING, "BRL"), lit(token.STRING, "USD")}, ast.Span{}), ast.Span{})
	ensure := ast.NewEnsureStmt(cond, ast.NewExprStmt(ident("CurrencyMismatch"), ast.Span{}), ast.Span{})

	ctx := StmtContext{ZeroValues: []string{"nil"}}
	out := lowerInFunc(t, l, ctx, "func testEnsure() ([]int, error)", ensure)

	want := `if !(slices.Contains([]string{"BRL", "USD"}, m.Currency)) {`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
	if !strings.Contains(out, "return nil, ErrCurrencyMismatch") {
		t.Fatalf("esperava \"return nil, ErrCurrencyMismatch\", got:\n%s", out)
	}
}
