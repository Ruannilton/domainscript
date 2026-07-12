package lower

import (
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/token"
	"domainscript/types"
)

// loadcollection_test.go prova os critérios de conclusão da task I3.1
// (§design read-side 3.4, REQ-33.2): "load T(id).<campo> [where] [orderBy]
// [skip] [take]" — quando o Target de um "load" é um MemberExpr sobre a
// construção do aggregate — carrega o aggregate via a MESMA LoadCall de
// hoistLoad (E6.2, intocada) e aplica runtime.SelectSlice sobre o campo de
// coleção do state, SEM Collection[T] nenhum. Todos os testes usam o wallet
// real (docs/examples/wallet): Wallet.state.entries é AppendList<StatementEntry>
// — a coleção real que o exemplo-âncora GetStatement (spec §6.3) varre.
//
// A composição com where/orderBy/skip/take (Passo 2 de hoistLoadCollection)
// é REUSADA de hoistQueryPredicate/hoistOrderBy/hoistSkipTakeExpr por
// inteiro (I1.1/I2.1) — orderby_test.go/builtins_test.go já cobrem a tabela
// de comparabilidade e os erros de cláusula duplicada/count exaustivamente;
// aqui só provamos que a MESMA composição funciona também nesta forma nova
// (não reimplementamos os testes da tabela).

func walletAggConstruction(idArg ast.Expr) *ast.CallExpr {
	return callExpr(ident("Wallet"), arg(idArg))
}

func loadCollectionTarget(field string, idArg ast.Expr) *ast.MemberExpr {
	return member(walletAggConstruction(idArg), field)
}

// TestHoistLoadCollection_Basic prova a forma mínima: "load Wallet(walletId).
// entries" sem cláusula nenhuma — carrega o aggregate e devolve
// runtime.SelectSlice sobre ".Items()" (AppendList<StatementEntry>) com uma
// Query[StatementEntry]{} vazia.
func TestHoistLoadCollection_Basic(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("walletId", l.env.TypeOfName("WalletId"))

	qe := ast.NewQueryExpr("load", loadCollectionTarget("entries", ident("walletId")), "", nil, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testGetStatement()", assign)

	if !strings.Contains(out, "tmp1, err := LoadWallet(tx, walletId)") {
		t.Fatalf("esperava a chamada LoadWallet (E6.2, INTOCADA) sobre tx, got:\n%s", out)
	}
	if !strings.Contains(out, "tmp2, err := runtime.SelectSlice(tmp1.state.Entries.Items(), runtime.Query[StatementEntry]{})") {
		t.Fatalf("esperava SelectSlice sobre tmp1.state.Entries.Items() com Query[StatementEntry] vazia, got:\n%s", out)
	}
	if !strings.Contains(out, "entries := tmp2") {
		t.Fatalf("esperava \"entries := tmp2\" (o resultado materializado atribuído ao alvo do assign), got:\n%s", out)
	}
}

// TestHoistLoadCollection_WhereOrderBySkipTake é o critério de conclusão
// literal de I3.1: where + orderBy + skip + take juntos sobre a coleção de
// um aggregate carregado — a forma exata de GetStatement (spec §6.3), com
// "date" trocado por "description" (StatementEntry real não tem campo date
// — description é um VO wrapper sobre string, a mesma linha "wrapper sobre
// primitivo ordenável" da tabela de comparabilidade, já provada
// exaustivamente por orderby_test.go).
func TestHoistLoadCollection_WhereOrderBySkipTake(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("walletId", l.env.TypeOfName("WalletId"))
	l.env.Bind("page", &types.Primitive{Name: "integer"})

	whereCond := ast.NewBinaryExpr(token.NEQ,
		member(ident("e"), "description"),
		callExpr(ident("TransactionDescription"), arg(lit(token.STRING, ""))),
		ast.Span{})
	skipExpr := ast.NewBinaryExpr(token.STAR, ident("page"), lit(token.INT, "20"), ast.Span{})
	qe := ast.NewQueryExpr("load", loadCollectionTarget("entries", ident("walletId")), "e", []ast.QueryClause{
		whereClause(whereCond),
		orderByClause(member(ident("e"), "description"), "descending"),
		skipClause(skipExpr),
		takeClause(lit(token.INT, "20")),
	}, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testGetStatement()", assign)

	if !strings.Contains(out, "tmp1, err := LoadWallet(tx, walletId)") {
		t.Fatalf("esperava LoadWallet, got:\n%s", out)
	}
	want := `runtime.SelectSlice(tmp1.state.Entries.Items(), runtime.Query[StatementEntry]{` +
		`Where: func(e StatementEntry) (bool, error) { return e.Description != TransactionDescription(""), nil }, ` +
		`Less: func(a, b StatementEntry) (bool, error) { return b.Description < a.Description, nil }, ` +
		`OrderField: "description", OrderDesc: true, Skip: int(page * 20), Take: 20})`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava a Query[T] COMPLETA (where+orderBy+skip+take, ordem semântica fixa):\n%s\ngot:\n%s", want, out)
	}
}

// TestHoistLoadCollection_ClauseOrderInSourceDoesNotMatter prova, sobre esta
// forma nova, a MESMA garantia que TestHoistList_ClauseOrderInSourceDoesNotMatter
// (orderby_test.go) já prova para "list": a ordem TEXTUAL das cláusulas não
// muda o Go gerado — a ordem semântica é fixa (§design read-side 3.1).
func TestHoistLoadCollection_ClauseOrderInSourceDoesNotMatter(t *testing.T) {
	_, l1 := newWalletLowererWithBuiltins(t)
	l1.env.Bind("walletId", l1.env.TypeOfName("WalletId"))
	qe1 := ast.NewQueryExpr("load", loadCollectionTarget("entries", ident("walletId")), "e", []ast.QueryClause{
		takeClause(lit(token.INT, "2")),
		skipClause(lit(token.INT, "1")),
		orderByClause(member(ident("e"), "description"), ""),
	}, ast.Span{})
	out1 := lowerInFunc(t, l1, StmtContext{}, "func testGetStatement()", ast.NewAssignStmt(ident("entries"), qe1, ast.Span{}))

	_, l2 := newWalletLowererWithBuiltins(t)
	l2.env.Bind("walletId", l2.env.TypeOfName("WalletId"))
	qe2 := ast.NewQueryExpr("load", loadCollectionTarget("entries", ident("walletId")), "e", []ast.QueryClause{
		orderByClause(member(ident("e"), "description"), ""),
		skipClause(lit(token.INT, "1")),
		takeClause(lit(token.INT, "2")),
	}, ast.Span{})
	out2 := lowerInFunc(t, l2, StmtContext{}, "func testGetStatement()", ast.NewAssignStmt(ident("entries"), qe2, ast.Span{}))

	if out1 != out2 {
		t.Fatalf("esperava o MESMO Go independente da ordem textual:\nA:\n%s\nB:\n%s", out1, out2)
	}
	if !strings.Contains(out1, "Skip: 1, Take: 2") {
		t.Fatalf("esperava \"Skip: 1, Take: 2\", got:\n%s", out1)
	}
}

// TestHoistLoadCollection_DuplicateClauseFailsExplicitly prova que
// ensureListClausesWellFormed (I2.1) também guarda esta forma nova — cláusula
// duplicada é erro de geração claro, não "usa só a última" silencioso.
func TestHoistLoadCollection_DuplicateClauseFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("walletId", l.env.TypeOfName("WalletId"))
	qe := ast.NewQueryExpr("load", loadCollectionTarget("entries", ident("walletId")), "", []ast.QueryClause{
		skipClause(lit(token.INT, "1")),
		skipClause(lit(token.INT, "2")),
	}, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	sl := NewStmtLowerer(l, emit.New("testpkg"), StmtContext{})
	if err := sl.Stmt(assign); err == nil {
		t.Fatal("esperava erro: \"skip\" duplicado")
	}
}

// TestHoistLoadCollection_FieldNotACollectionFailsExplicitly prova que
// "load Wallet(id).balance" (balance é Money, não List/AppendList) falha
// explicitamente — NFR-20, nunca Go que não compila.
func TestHoistLoadCollection_FieldNotACollectionFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("walletId", l.env.TypeOfName("WalletId"))
	qe := ast.NewQueryExpr("load", loadCollectionTarget("balance", ident("walletId")), "", nil, ast.Span{})
	assign := ast.NewAssignStmt(ident("x"), qe, ast.Span{})

	sl := NewStmtLowerer(l, emit.New("testpkg"), StmtContext{})
	if err := sl.Stmt(assign); err == nil {
		t.Fatal("esperava erro: \"balance\" (Money) não é uma coleção")
	}
}

// TestHoistLoadCollection_UnknownFieldFailsExplicitly prova que um campo
// inexistente no state falha explicitamente.
func TestHoistLoadCollection_UnknownFieldFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("walletId", l.env.TypeOfName("WalletId"))
	qe := ast.NewQueryExpr("load", loadCollectionTarget("nonexistent", ident("walletId")), "", nil, ast.Span{})
	assign := ast.NewAssignStmt(ident("x"), qe, ast.Span{})

	sl := NewStmtLowerer(l, emit.New("testpkg"), StmtContext{})
	if err := sl.Stmt(assign); err == nil {
		t.Fatal("esperava erro: Wallet.state não declara \"nonexistent\"")
	}
}

// TestHoistLoadCollection_OrderByOnComputedKey_OrderFieldEmpty prova que a
// mesma regra de OrderField (§design read-side 3.2) vale aqui: só uma chave
// "<binding>.<campo>" NUA popula o descritor declarativo.
func TestHoistLoadCollection_OrderByOnComputedKey_OrderFieldEmpty(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("walletId", l.env.TypeOfName("WalletId"))
	computedKey := ast.NewBinaryExpr(token.PLUS, member(member(ident("e"), "amount"), "currency"), lit(token.STRING, "!"), ast.Span{})
	qe := ast.NewQueryExpr("load", loadCollectionTarget("entries", ident("walletId")), "e",
		[]ast.QueryClause{orderByClause(computedKey, "")}, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testGetStatement()", assign)
	if strings.Contains(out, "OrderField") {
		t.Fatalf("esperava OrderField AUSENTE (chave computada), got:\n%s", out)
	}
}
