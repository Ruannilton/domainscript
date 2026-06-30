package astutil

import (
	"sort"
	"testing"

	"domainscript/ast"
	"domainscript/token"
)

// sp é um span zero — irrelevante para a travessia, que só olha a forma da árvore.
var sp ast.Span

func id(name string) *ast.Ident { return ast.NewIdent(name, sp) }

// exprNames coleta os nomes dos identificadores visitados por ForEachExpr, em
// ordem de visita. Um ident em cada folha funciona como marcador: aparecer na
// lista prova que a travessia desceu até aquele ramo.
func exprNames(e ast.Expr) []string {
	var out []string
	ForEachExpr(e, func(x ast.Expr) {
		if n, ok := x.(*ast.Ident); ok {
			out = append(out, n.Name)
		}
	})
	return out
}

// exprsNames extrai os nomes dos identificadores de uma lista de expressões.
func exprsNames(es []ast.Expr) []string {
	var out []string
	for _, e := range es {
		if n, ok := e.(*ast.Ident); ok {
			out = append(out, n.Name)
		}
	}
	return out
}

// equalSet compara dois slices como multiconjuntos (ordem irrelevante).
func equalSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
}

// equalSeq compara dois slices respeitando a ordem.
func equalSeq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalBlocks compara dois slices de blocos por identidade de ponteiro e ordem.
func equalBlocks(a, b []*ast.Block) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ForEachExpr deve visitar o próprio nó e descer em cada variante de expressão.
// Cada caso planta idents-marcadores nas folhas; vê-los todos prova a descida.
func TestForEachExprDesceEmCadaVariante(t *testing.T) {
	cases := []struct {
		nome string
		expr ast.Expr
		want []string
	}{
		{"ident: visita o próprio nó", id("x"), []string{"x"}},
		{"binary: desce em Left e Right",
			ast.NewBinaryExpr(token.PLUS, id("a"), id("b"), sp),
			[]string{"a", "b"}},
		{"unary: desce em X",
			ast.NewUnaryExpr(token.NOT, id("c"), sp),
			[]string{"c"}},
		{"member: desce no receptor X",
			ast.NewMemberExpr(id("d"), "m", token.Pos{}, sp),
			[]string{"d"}},
		{"call: desce em Fn e em cada Arg",
			ast.NewCallExpr(id("e"), []ast.Arg{{Value: id("f")}, {Name: "k", Value: id("g")}}, sp),
			[]string{"e", "f", "g"}},
		{"index: desce em X e Index",
			ast.NewIndexExpr(id("h"), id("i"), sp),
			[]string{"h", "i"}},
		{"range: desce em Low e High",
			ast.NewRangeExpr(id("j"), id("k"), sp),
			[]string{"j", "k"}},
		{"lambda: desce no Body",
			ast.NewLambdaExpr("p", id("l"), sp),
			[]string{"l"}},
		{"list: desce em cada elemento",
			ast.NewListExpr([]ast.Expr{id("m"), id("n")}, sp),
			[]string{"m", "n"}},
		{"query: desce em Target e em cada Clause",
			ast.NewQueryExpr("list", id("o"), "t",
				[]ast.QueryClause{{Kw: "where", Expr: id("p")}, {Kw: "take", Expr: id("q")}}, sp),
			[]string{"o", "p", "q"}},
		{"match: desce em Subject, Patterns, Guard e Body",
			ast.NewMatchExpr(id("r"), []ast.MatchExprArm{
				{Patterns: []ast.Expr{id("s"), id("t")}, Guard: id("u"), Body: id("v")},
			}, sp),
			[]string{"r", "s", "t", "u", "v"}},
		{"match: guard nil não quebra a travessia",
			ast.NewMatchExpr(id("w"), []ast.MatchExprArm{
				{Patterns: []ast.Expr{id("y")}, Guard: nil, Body: id("z")},
			}, sp),
			[]string{"w", "y", "z"}},
	}
	for _, c := range cases {
		got := exprNames(c.expr)
		if !equalSet(got, c.want) {
			t.Errorf("%s: visitou %v, queria %v", c.nome, got, c.want)
		}
	}
}

// ForEachExpr com nil retorna sem invocar a função.
func TestForEachExprNilNaoVisita(t *testing.T) {
	ForEachExpr(nil, func(ast.Expr) {
		t.Fatalf("não deveria visitar nada para uma expr nil")
	})
}

// ForEachStmt desce em blocos, corpos de for, ação else de ensure e braços de
// match — em profundidade primeiro. Cada ExprStmt aninhado carrega um marcador;
// a ordem [a,b,c,d] confirma a ordem e a descida em cada construto.
func TestForEachStmtDesceEmStmtsAninhados(t *testing.T) {
	es := func(name string) *ast.ExprStmt { return ast.NewExprStmt(id(name), sp) }
	root := ast.NewBlock([]ast.Stmt{
		es("a"),
		ast.NewForStmt("i", id("it"), ast.NewBlock([]ast.Stmt{es("b")}, sp), sp),
		ast.NewEnsureStmt(id("cond"), es("c"), sp),
		ast.NewMatchStmt(id("subj"), []ast.MatchStmtArm{
			{Patterns: []ast.Expr{id("p")}, Body: es("d")},
		}, sp),
	}, sp)

	var seen []string
	ForEachStmt(root, func(s ast.Stmt) {
		if e, ok := s.(*ast.ExprStmt); ok {
			seen = append(seen, e.X.(*ast.Ident).Name)
		}
	})
	want := []string{"a", "b", "c", "d"}
	if !equalSeq(seen, want) {
		t.Errorf("ExprStmts visitados em ordem %v, queria %v", seen, want)
	}
}

// ForEachStmt visita o próprio statement, mesmo quando ele não tem filhos.
func TestForEachStmtVisitaOProprioStmt(t *testing.T) {
	count := 0
	ForEachStmt(ast.NewBreakStmt(false, sp), func(ast.Stmt) { count++ })
	if count != 1 {
		t.Errorf("esperava visitar exatamente o próprio stmt, visitou %d", count)
	}
}

// ForEachStmt com nil retorna sem invocar a função.
func TestForEachStmtNilNaoVisita(t *testing.T) {
	ForEachStmt(nil, func(ast.Stmt) {
		t.Fatalf("não deveria visitar nada para um stmt nil")
	})
}

// StmtExprs devolve as expressões diretamente contidas por um statement, sem
// descer em statements aninhados (o corpo do for, a ação else do ensure, etc.).
func TestStmtExprs(t *testing.T) {
	es := func(name string) *ast.ExprStmt { return ast.NewExprStmt(id(name), sp) }
	cases := []struct {
		nome string
		stmt ast.Stmt
		want []string
	}{
		{"exprstmt: a expressão",
			ast.NewExprStmt(id("x"), sp), []string{"x"}},
		{"assign: target e value",
			ast.NewAssignStmt(id("t"), id("v"), sp), []string{"t", "v"}},
		{"ensure: só a condição, não o else",
			ast.NewEnsureStmt(id("c"), es("else"), sp), []string{"c"}},
		{"return com valor",
			ast.NewReturnStmt(id("r"), sp), []string{"r"}},
		{"return sem valor: nenhuma expressão",
			ast.NewReturnStmt(nil, sp), nil},
		{"for: o iterável, não o corpo",
			ast.NewForStmt("i", id("it"), ast.NewBlock([]ast.Stmt{es("body")}, sp), sp), []string{"it"}},
		{"emit: a construção do evento",
			ast.NewEmitStmt(id("e"), sp), []string{"e"}},
		{"match: subject, padrões e guard",
			ast.NewMatchStmt(id("s"), []ast.MatchStmtArm{
				{Patterns: []ast.Expr{id("p1"), id("p2")}, Guard: id("g"), Body: es("body")},
			}, sp), []string{"s", "p1", "p2", "g"}},
		{"match: sem guard, omite o guard",
			ast.NewMatchStmt(id("s"), []ast.MatchStmtArm{
				{Patterns: []ast.Expr{id("p")}, Guard: nil, Body: es("body")},
			}, sp), []string{"s", "p"}},
		{"log: mensagem e valores dos campos",
			ast.NewLogStmt("info", id("m"), []ast.LogField{{Name: "f", Value: id("fv")}}, sp),
			[]string{"m", "fv"}},
		{"log: sem mensagem, só os campos",
			ast.NewLogStmt("info", nil, []ast.LogField{{Name: "f", Value: id("fv")}}, sp),
			[]string{"fv"}},
		{"stmt sem expressões: nil",
			ast.NewBreakStmt(false, sp), nil},
	}
	for _, c := range cases {
		got := exprsNames(StmtExprs(c.stmt))
		if !equalSeq(got, c.want) {
			t.Errorf("%s: StmtExprs deu %v, queria %v", c.nome, got, c.want)
		}
	}
}

// ForEachExprInBlock visita toda expressão em qualquer ponto do bloco, incluindo
// as de statements aninhados e suas subexpressões, em profundidade primeiro.
func TestForEachExprInBlockVisitaTudo(t *testing.T) {
	inner := ast.NewBlock([]ast.Stmt{
		ast.NewEnsureStmt(
			ast.NewBinaryExpr(token.EQ, id("a"), id("b"), sp),
			ast.NewExprStmt(id("nop"), sp), sp),
	}, sp)
	blk := ast.NewBlock([]ast.Stmt{
		ast.NewForStmt("i", id("xs"), inner, sp),
		ast.NewAssignStmt(id("t"), ast.NewCallExpr(id("f"), []ast.Arg{{Value: id("arg")}}, sp), sp),
	}, sp)

	var names []string
	ForEachExprInBlock(blk, func(e ast.Expr) {
		if n, ok := e.(*ast.Ident); ok {
			names = append(names, n.Name)
		}
	})
	want := []string{"xs", "a", "b", "nop", "t", "f", "arg"}
	if !equalSeq(names, want) {
		t.Errorf("expressões visitadas %v, queria %v", names, want)
	}
}

// ForEachExprInBlock com nil retorna sem invocar a função.
func TestForEachExprInBlockNilNaoVisita(t *testing.T) {
	ForEachExprInBlock(nil, func(ast.Expr) {
		t.Fatalf("não deveria visitar nada para um bloco nil")
	})
}

// DeclBlocks devolve todos os blocos de execução de uma declaração e pula os nil.
func TestDeclBlocks(t *testing.T) {
	vb, ob := ast.NewBlock(nil, sp), ast.NewBlock(nil, sp)
	cb := ast.NewBlock(nil, sp)
	hb, ab := ast.NewBlock(nil, sp), ast.NewBlock(nil, sp)
	ub := ast.NewBlock(nil, sp)
	qb := ast.NewBlock(nil, sp)
	pb := ast.NewBlock(nil, sp)
	wsrc, wex := ast.NewBlock(nil, sp), ast.NewBlock(nil, sp)
	up, down, oie := ast.NewBlock(nil, sp), ast.NewBlock(nil, sp), ast.NewBlock(nil, sp)
	up2 := ast.NewBlock(nil, sp)

	cases := []struct {
		nome string
		decl ast.Decl
		want []*ast.Block
	}{
		{"valueobject: Valid + corpos dos Operators",
			ast.NewValueObjectDecl("V", nil, nil, vb,
				[]*ast.OperatorDecl{ast.NewOperatorDecl("+", nil, nil, ob, sp)}, sp),
			[]*ast.Block{vb, ob}},
		{"valueobject sem corpos: nil",
			ast.NewValueObjectDecl("V", nil, nil, nil, nil, sp), nil},
		{"enum: corpo do coerce",
			ast.NewEnumDecl("E", nil, nil, ast.NewCoerceBlock(nil, cb, sp), sp),
			[]*ast.Block{cb}},
		{"enum sem coerce: nil",
			ast.NewEnumDecl("E", nil, nil, nil, sp), nil},
		{"aggregate: corpos de Handlers e Appliers",
			ast.NewAggregateDecl("A", "EventSourced", nil, nil, nil, nil,
				[]*ast.HandleDecl{ast.NewHandleDecl("H", nil, hb, sp)},
				[]*ast.ApplyDecl{ast.NewApplyDecl("Ev", ab, sp)}, sp),
			[]*ast.Block{hb, ab}},
		{"usecase: execute",
			ast.NewUseCaseDecl("U", "C", nil, nil, "", ub, sp),
			[]*ast.Block{ub}},
		{"query: body",
			ast.NewQueryDecl("Q", nil, nil, nil, qb, sp),
			[]*ast.Block{qb}},
		{"policy: execute",
			ast.NewPolicyDecl("P", "Ev", "", pb, sp),
			[]*ast.Block{pb}},
		{"worker: source + execute",
			ast.NewWorkerDecl("W", "every", nil, "", nil, wsrc, "", wex, sp),
			[]*ast.Block{wsrc, wex}},
		{"saga: up/down/onInfraError de cada step",
			ast.NewSagaDecl("S", "C", "async", nil, nil,
				[]*ast.SagaStep{ast.NewSagaStep("s1", up, down, oie, sp)}, sp),
			[]*ast.Block{up, down, oie}},
		{"saga: blocos nil de um step são pulados",
			ast.NewSagaDecl("S", "C", "async", nil, nil,
				[]*ast.SagaStep{ast.NewSagaStep("s1", up2, nil, nil, sp)}, sp),
			[]*ast.Block{up2}},
		{"decl sem blocos: nil",
			ast.NewCommandDecl("C", nil, sp), nil},
	}
	for _, c := range cases {
		got := DeclBlocks(c.decl)
		if !equalBlocks(got, c.want) {
			t.Errorf("%s: DeclBlocks devolveu %d blocos, queria %d", c.nome, len(got), len(c.want))
		}
	}
}

// IsIdent casa só com um identificador do nome exato.
func TestIsIdent(t *testing.T) {
	if !IsIdent(id("x"), "x") {
		t.Errorf("identificador de mesmo nome deveria casar")
	}
	if IsIdent(id("x"), "y") {
		t.Errorf("nome diferente não deveria casar")
	}
	if IsIdent(ast.NewLiteral(token.INT, "1", sp), "1") {
		t.Errorf("um literal não é identificador")
	}
}

// HeadName devolve o nome da cabeça de uma referência: callee de chamada ou
// ident nu; "" para qualquer outra forma.
func TestHeadName(t *testing.T) {
	if got := HeadName(ast.NewCallExpr(id("Deposit"), nil, sp)); got != "Deposit" {
		t.Errorf("HeadName de chamada = %q, queria Deposit", got)
	}
	if got := HeadName(id("Wallet")); got != "Wallet" {
		t.Errorf("HeadName de ident = %q, queria Wallet", got)
	}
	method := ast.NewMemberExpr(id("x"), "m", token.Pos{}, sp)
	if got := HeadName(ast.NewCallExpr(method, nil, sp)); got != "" {
		t.Errorf("HeadName de chamada de método = %q, queria vazio", got)
	}
	if got := HeadName(method); got != "" {
		t.Errorf("HeadName de acesso a membro = %q, queria vazio", got)
	}
}

// StateField devolve o campo de "state.<campo>" e "" para qualquer outra forma.
func TestStateField(t *testing.T) {
	if got := StateField(ast.NewMemberExpr(id("state"), "balance", token.Pos{}, sp)); got != "balance" {
		t.Errorf("StateField de state.balance = %q, queria balance", got)
	}
	if got := StateField(ast.NewMemberExpr(id("self"), "balance", token.Pos{}, sp)); got != "" {
		t.Errorf("StateField de self.balance = %q, queria vazio", got)
	}
	if got := StateField(id("state")); got != "" {
		t.Errorf("StateField de um ident = %q, queria vazio", got)
	}
}
