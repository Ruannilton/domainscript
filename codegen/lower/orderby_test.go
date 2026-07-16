package lower

import (
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/token"
	"domainscript/types"
)

// orderby_test.go prova os critérios de conclusão da task I2.1 (§design
// read-side 3.1/3.2, REQ-33.1/33.3/33.5): hoistList passa a montar a
// Query[T] completa (Less/OrderField/OrderDesc/Skip/Take), decidindo o
// corpo de "Less" pela tabela de comparabilidade de §design read-side 3.2.
//
// Duas camadas de teste, como o resto do pacote:
//
//  1. buildLess/primitiveLess/baseGoCompare testados DIRETAMENTE (sem passar
//     pelo pipeline de hoistList inteiro) para cobrir CADA linha da tabela —
//     o wallet real não declara campo integer/duration/size nem nenhum VO
//     wrapper sobre decimal/datetime, então essas linhas usam tipos
//     types.Type construídos à mão (mesma convenção de
//     goname/vobinary_test.go: um ValueObjectDecl fabricado só para registrar
//     um Operator no VOOperatorRegistry).
//  2. hoistList de ponta a ponta (via lowerInFunc, mesmo padrão de
//     builtins_test.go) sobre o wallet real — Money (amount decimal, currency
//     string) e StatementEntry (type Enum, description VO wrapper sobre
//     string, amount Money SEM Operator </>) cobrem as linhas "primitivo",
//     "wrapper sobre primitivo" e "Enum" da tabela com dados reais, mais
//     duplicidade de cláusula, orderBy/skip/take em count, independência da
//     ordem textual e a forma completa "where ... orderBy ... skip ... take".

// --- 1. buildLess/primitiveLess/baseGoCompare: a tabela de comparabilidade
// inteira, linha por linha (§design read-side 3.2). ---

// newOrderByStmtLowerer monta um StmtLowerer sobre o wallet real, pronto
// para chamar buildLess diretamente (sem outra cláusula ao redor) — usado
// pelos testes que exercitam a tabela de comparabilidade com tipos
// fabricados à mão.
func newOrderByStmtLowerer(t *testing.T) *StmtLowerer {
	t.Helper()
	_, l := newWalletLowererWithBuiltins(t)
	e := emit.New("testpkg")
	return NewStmtLowerer(l, e, StmtContext{})
}

func TestBuildLess_PrimitiveInteger(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	got, fallible, err := sl.buildLess(&types.Primitive{Name: "integer"}, "a.X", "b.X")
	if err != nil {
		t.Fatalf("buildLess(integer): erro inesperado: %v", err)
	}
	if fallible {
		t.Fatal("buildLess(integer): esperava fallible=false (comparação nativa nunca falha)")
	}
	if want := "a.X < b.X"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildLess_PrimitiveString(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	got, fallible, err := sl.buildLess(&types.Primitive{Name: "string"}, "a.X", "b.X")
	if err != nil {
		t.Fatalf("buildLess(string): erro inesperado: %v", err)
	}
	if fallible {
		t.Fatal("buildLess(string): esperava fallible=false")
	}
	if want := "a.X < b.X"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildLess_PrimitiveDuration(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	got, fallible, err := sl.buildLess(&types.Primitive{Name: "duration"}, "a.X", "b.X")
	if err != nil {
		t.Fatalf("buildLess(duration): erro inesperado: %v", err)
	}
	if fallible {
		t.Fatal("buildLess(duration): esperava fallible=false")
	}
	if want := "a.X < b.X"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildLess_PrimitiveSize(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	got, fallible, err := sl.buildLess(&types.Primitive{Name: "size"}, "a.X", "b.X")
	if err != nil {
		t.Fatalf("buildLess(size): erro inesperado: %v", err)
	}
	if fallible {
		t.Fatal("buildLess(size): esperava fallible=false")
	}
	if want := "a.X < b.X"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBuildLess_PrimitiveDecimal prova a linha "decimal" da tabela: Decimal
// é backed por big.Int (rtsrc/decimal.go.txt) — NÃO comparável com "<" nativo
// — a comparação passa por .Cmp.
func TestBuildLess_PrimitiveDecimal(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	got, fallible, err := sl.buildLess(&types.Primitive{Name: "decimal"}, "a.X", "b.X")
	if err != nil {
		t.Fatalf("buildLess(decimal): erro inesperado: %v", err)
	}
	if fallible {
		t.Fatal("buildLess(decimal): esperava fallible=false (Cmp nunca falha)")
	}
	if want := "a.X.Cmp(b.X) < 0"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBuildLess_PrimitiveDatetime prova a linha "datetime": time.Time não
// tem operador relacional em Go — a comparação passa por .Before.
func TestBuildLess_PrimitiveDatetime(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	got, fallible, err := sl.buildLess(&types.Primitive{Name: "datetime"}, "a.X", "b.X")
	if err != nil {
		t.Fatalf("buildLess(datetime): erro inesperado: %v", err)
	}
	if fallible {
		t.Fatal("buildLess(datetime): esperava fallible=false")
	}
	if want := "a.X.Before(b.X)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBuildLess_PrimitiveNonOrderableFailsExplicitly prova NFR-20: boolean
// não está na linha "primitivo ordenável" da tabela — erro de geração claro,
// nunca Go que não compila.
func TestBuildLess_PrimitiveNonOrderableFailsExplicitly(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	if _, _, err := sl.buildLess(&types.Primitive{Name: "boolean"}, "a.X", "b.X"); err == nil {
		t.Fatal("esperava erro: boolean não é ordenável (§design read-side 3.2)")
	}
}

// TestBuildLess_VOWrapperOverInteger prova "VO wrapper sobre primitivo
// ordenável": um named type Go sobre int64 aceita "<" DIRETAMENTE, sem
// conversão nenhuma (ao contrário de decimal/datetime, abaixo).
func TestBuildLess_VOWrapperOverInteger(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	vo := &types.VOType{Name: "Priority", Base: &types.Primitive{Name: "integer"}}
	got, fallible, err := sl.buildLess(vo, "a.Priority", "b.Priority")
	if err != nil {
		t.Fatalf("buildLess(wrapper/integer): erro inesperado: %v", err)
	}
	if fallible {
		t.Fatal("buildLess(wrapper/integer): esperava fallible=false")
	}
	if want := "a.Priority < b.Priority"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBuildLess_VOWrapperOverDecimal prova a conversão explícita exigida
// quando o wrapper embrulha decimal: "type Score runtime.Decimal" NÃO herda
// o method-set de Decimal (defined type, não alias) — Cmp só existe depois
// de converter de volta para runtime.Decimal.
func TestBuildLess_VOWrapperOverDecimal(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	vo := &types.VOType{Name: "Score", Base: &types.Primitive{Name: "decimal"}}
	got, fallible, err := sl.buildLess(vo, "a.Score", "b.Score")
	if err != nil {
		t.Fatalf("buildLess(wrapper/decimal): erro inesperado: %v", err)
	}
	if fallible {
		t.Fatal("buildLess(wrapper/decimal): esperava fallible=false")
	}
	want := "runtime.Decimal(a.Score).Cmp(runtime.Decimal(b.Score)) < 0"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBuildLess_VOWrapperOverDatetime prova a conversão explícita análoga
// para datetime — e que o import "time" é registrado no ponto em que o
// texto "time.Time(...)" é de fato emitido (mesma disciplina do resto do
// pacote, ex. logStmt/log/slog).
func TestBuildLess_VOWrapperOverDatetime(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	vo := &types.VOType{Name: "ScheduledAt", Base: &types.Primitive{Name: "datetime"}}
	got, fallible, err := sl.buildLess(vo, "a.ScheduledAt", "b.ScheduledAt")
	if err != nil {
		t.Fatalf("buildLess(wrapper/datetime): erro inesperado: %v", err)
	}
	if fallible {
		t.Fatal("buildLess(wrapper/datetime): esperava fallible=false")
	}
	want := "time.Time(a.ScheduledAt).Before(time.Time(b.ScheduledAt))"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// baseGoCompare precisa ter registrado o import "time" (o texto
	// devolvido referencia o literal "time.Time") — escreve got de verdade
	// no corpo antes de formatar, pois emit.Bytes() recusa um import
	// registrado e NUNCA usado no corpo (a mesma disciplina de
	// TestEmitterBytesFailsOnUnusedImport, codegen/emit/emit_test.go).
	sl.e.Line("func _usesTime() bool { return %s }", got)
	out, err := sl.e.Bytes()
	if err != nil {
		t.Fatalf("e.Bytes(): %v", err)
	}
	if !strings.Contains(string(out), `"time"`) {
		t.Fatalf(`esperava import "time" registrado, got:%s`, out)
	}
}

// TestBuildLess_VOWrapperOverNonPrimitiveFailsExplicitly prova que um
// wrapper cujo Base não é um primitivo (uma forma que a linguagem hoje não
// produz, mas buildLess não deve assumir isso) falha explicitamente em vez
// de gerar Go quebrado.
func TestBuildLess_VOWrapperOverNonPrimitiveFailsExplicitly(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	vo := &types.VOType{Name: "Weird", Base: &types.ShapeType{Name: "Foo"}}
	if _, _, err := sl.buildLess(vo, "a.Weird", "b.Weird"); err == nil {
		t.Fatal("esperava erro: Base não-primitivo não tem forma de ordenar")
	}
}

// TestBuildLess_Enum prova a linha "Enum": comparação sobre o valor base,
// mesma conversão que um wrapper receberia (Enum é "type X Base" também,
// goname/types.go).
func TestBuildLess_Enum(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	en := &types.EnumType{Name: "Status", Base: &types.Primitive{Name: "string"}, Members: []string{"A", "B"}}
	got, fallible, err := sl.buildLess(en, "a.Status", "b.Status")
	if err != nil {
		t.Fatalf("buildLess(enum): erro inesperado: %v", err)
	}
	if fallible {
		t.Fatal("buildLess(enum): esperava fallible=false")
	}
	if want := "a.Status < b.Status"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestBuildLess_EnumNonPrimitiveBaseFailsExplicitly(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	en := &types.EnumType{Name: "Weird", Base: &types.ShapeType{Name: "Foo"}}
	if _, _, err := sl.buildLess(en, "a.Weird", "b.Weird"); err == nil {
		t.Fatal("esperava erro: Enum com Base não-primitivo não tem forma de ordenar")
	}
}

// TestBuildLess_VOCompositeWithLtOperator prova o dispatch de Operator "<"
// declarado — SEMPRE falível (E3.2: todo Operator devolve (T, error)).
func TestBuildLess_VOCompositeWithLtOperator(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	sl.reg.Register(&ast.ValueObjectDecl{Name: "Rank", Operators: []*ast.OperatorDecl{{Op: "<"}}})
	vo := &types.VOType{Name: "Rank", Fields: []types.Field{{Name: "value", Type: &types.Primitive{Name: "integer"}}}}

	got, fallible, err := sl.buildLess(vo, "a.Score", "b.Score")
	if err != nil {
		t.Fatalf("buildLess(composite, Lt): erro inesperado: %v", err)
	}
	if !fallible {
		t.Fatal("buildLess(composite, Lt): esperava fallible=true (Operator sempre devolve error)")
	}
	if want := "a.Score.Lt(b.Score)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBuildLess_VOCompositeWithOnlyGtOperator prova que, quando só ">" está
// declarado (não "<"), buildLess deriva "a < b" como "b > a" (álgebra
// elementar: a<b ⇔ b>a) em vez de recusar — a tabela aceita "< (ou >)".
func TestBuildLess_VOCompositeWithOnlyGtOperator(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	sl.reg.Register(&ast.ValueObjectDecl{Name: "Weight", Operators: []*ast.OperatorDecl{{Op: ">"}}})
	vo := &types.VOType{Name: "Weight", Fields: []types.Field{{Name: "value", Type: &types.Primitive{Name: "integer"}}}}

	got, fallible, err := sl.buildLess(vo, "a.W", "b.W")
	if err != nil {
		t.Fatalf("buildLess(composite, só Gt): erro inesperado: %v", err)
	}
	if !fallible {
		t.Fatal("buildLess(composite, só Gt): esperava fallible=true")
	}
	if want := "b.W.Gt(a.W)"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestBuildLess_VOCompositeWithoutOrderOperatorFailsExplicitly é a linha
// final da tabela: "qualquer outro" — erro de geração claro (NFR-20). Usa o
// Money REAL do wallet (Operator +/-/>=, mas NEM < NEM >) — a mesma forma
// que hoistList encontraria de verdade num "list StatementEntry e orderBy
// e.amount" (provado de ponta a ponta em
// TestHoistList_OrderBy_CompositeWithoutOperatorFailsExplicitly, abaixo).
func TestBuildLess_VOCompositeWithoutOrderOperatorFailsExplicitly(t *testing.T) {
	prog, l := newWalletLowerer(t)
	l.WithBuiltins(NewBuiltinLowerer("runtime", "ctx", "tx"))
	money := findValueObject(t, prog, "Money")
	if hasOrderOperator(money) {
		t.Fatalf("pré-condição do teste: Money não deveria declarar < nem > (declara %v)", operatorSymbols(money))
	}
	e := emit.New("testpkg")
	sl := NewStmtLowerer(l, e, StmtContext{})

	vo := &types.VOType{Name: "Money", Fields: []types.Field{
		{Name: "amount", Type: &types.Primitive{Name: "decimal"}},
		{Name: "currency", Type: &types.Primitive{Name: "string"}},
	}}
	if _, _, err := sl.buildLess(vo, "a.Amount", "b.Amount"); err == nil {
		t.Fatal("esperava erro: Money não declara Operator < nem > (§design read-side 3.2, NFR-20)")
	}
}

// TestBuildLess_UnknownTypeFailsExplicitly cobre "qualquer outro" tipo fora
// de Primitive/VOType/EnumType (ex. um Generic/ShapeType em posição de
// chave) — erro de geração, não Go arbitrário.
func TestBuildLess_UnknownTypeFailsExplicitly(t *testing.T) {
	sl := newOrderByStmtLowerer(t)
	if _, _, err := sl.buildLess(&types.Generic{Ctor: "List", Args: []types.Type{&types.Primitive{Name: "integer"}}}, "a.X", "b.X"); err == nil {
		t.Fatal("esperava erro: List<integer> não é uma chave de ordenação válida")
	}
}

func hasOrderOperator(vo *ast.ValueObjectDecl) bool {
	for _, op := range vo.Operators {
		if op.Op == "<" || op.Op == ">" {
			return true
		}
	}
	return false
}

func operatorSymbols(vo *ast.ValueObjectDecl) []string {
	out := make([]string, len(vo.Operators))
	for i, op := range vo.Operators {
		out[i] = op.Op
	}
	return out
}

// --- 2. hoistList de ponta a ponta (§design read-side 3.1), sobre o wallet
// real, via lowerInFunc (mesmo padrão de builtins_test.go). ---

func orderByClause(key ast.Expr, dir string) ast.QueryClause {
	return ast.QueryClause{Kw: "orderBy", Expr: key, Extra: dir}
}
func whereClause(cond ast.Expr) ast.QueryClause { return ast.QueryClause{Kw: "where", Expr: cond} }
func skipClause(e ast.Expr) ast.QueryClause     { return ast.QueryClause{Kw: "skip", Expr: e} }
func takeClause(e ast.Expr) ast.QueryClause     { return ast.QueryClause{Kw: "take", Expr: e} }

// TestHoistList_OrderBy_PrimitiveDecimalAscending prova a linha "primitivo
// ordenável" com um campo decimal DIRETO (não embrulhado): Money.amount, um
// campo real do wallet.
func TestHoistList_OrderBy_PrimitiveDecimalAscending(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("list", ident("Money"), "m",
		[]ast.QueryClause{orderByClause(member(ident("m"), "amount"), "")}, ast.Span{})
	assign := ast.NewAssignStmt(ident("prices"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	want := `tmp1, err := tx.Select(ctx, runtime.Query[Money]{Less: func(a, b Money) (bool, error) { return a.Amount.Cmp(b.Amount) < 0, nil }, OrderField: "amount"})`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// TestHoistList_OrderBy_PrimitiveStringDescending prova a direção
// "descending": os textos de chave A/B entram TROCADOS na mesma comparação
// "menor que" (ver a doc de hoistOrderBy sobre a álgebra a>b ⇔ b<a).
func TestHoistList_OrderBy_PrimitiveStringDescending(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("list", ident("Money"), "m",
		[]ast.QueryClause{orderByClause(member(ident("m"), "currency"), "descending")}, ast.Span{})
	assign := ast.NewAssignStmt(ident("prices"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	want := `tmp1, err := tx.Select(ctx, runtime.Query[Money]{Less: func(a, b Money) (bool, error) { return b.Currency < a.Currency, nil }, OrderField: "currency", OrderDesc: true})`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// TestHoistList_OrderBy_VOWrapper_OrderFieldPopulated prova a linha "VO
// wrapper sobre primitivo ordenável" com um campo real (StatementEntry.
// description, TransactionDescription — wrapper sobre string) E que
// OrderField é populado com o nome de campo NU (description é
// "<binding>.<campo>" direto).
func TestHoistList_OrderBy_VOWrapper_OrderFieldPopulated(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("list", ident("StatementEntry"), "e",
		[]ast.QueryClause{orderByClause(member(ident("e"), "description"), "ascending")}, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	want := `tmp1, err := tx.Select(ctx, runtime.Query[StatementEntry]{Less: func(a, b StatementEntry) (bool, error) { return a.Description < b.Description, nil }, OrderField: "description"})`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// TestHoistList_OrderBy_Enum prova a linha "Enum" com um campo real
// (StatementEntry.type, TransactionType : string).
func TestHoistList_OrderBy_Enum(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("list", ident("StatementEntry"), "e",
		[]ast.QueryClause{orderByClause(member(ident("e"), "type"), "")}, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	want := `tmp1, err := tx.Select(ctx, runtime.Query[StatementEntry]{Less: func(a, b StatementEntry) (bool, error) { return a.Type < b.Type, nil }, OrderField: "type"})`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// TestHoistList_OrderBy_CompositeWithoutOperatorFailsExplicitly prova, de
// PONTA A PONTA (não só a unidade buildLess), que ordenar por um campo
// Money (StatementEntry.amount — VO composto sem Operator < nem >) falha
// explicitamente.
func TestHoistList_OrderBy_CompositeWithoutOperatorFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("list", ident("StatementEntry"), "e",
		[]ast.QueryClause{orderByClause(member(ident("e"), "amount"), "")}, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	e := emit.New("testpkg")
	sl := NewStmtLowerer(l, e, StmtContext{})
	if err := sl.Stmt(assign); err == nil {
		t.Fatal("esperava erro: StatementEntry.amount (Money) não declara Operator < nem >")
	}
}

// TestHoistList_OrderBy_ComputedKey_OrderFieldEmpty prova que uma chave
// COMPUTADA (não um acesso de membro nu) gera só a closure Less, deixando
// OrderField vazio (§design read-side 3.2).
func TestHoistList_OrderBy_ComputedKey_OrderFieldEmpty(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	computedKey := ast.NewBinaryExpr(token.PLUS, member(ident("m"), "currency"), lit(token.STRING, "!"), ast.Span{})
	qe := ast.NewQueryExpr("list", ident("Money"), "m",
		[]ast.QueryClause{orderByClause(computedKey, "")}, ast.Span{})
	assign := ast.NewAssignStmt(ident("prices"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	if strings.Contains(out, "OrderField") {
		t.Fatalf("esperava OrderField AUSENTE (chave computada), got:\n%s", out)
	}
	want := `Less: func(a, b Money) (bool, error) { return a.Currency+"!" < b.Currency+"!", nil }`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// TestHoistList_SkipTake_PlainExpressions prova skip/take como expressões
// inteiras comuns (REQ-33, ex. do spec "skip page * 20 take 20") — nunca
// hoisted.
func TestHoistList_SkipTake_PlainExpressions(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("page", &types.Primitive{Name: "integer"})

	skipExpr := ast.NewBinaryExpr(token.STAR, ident("page"), lit(token.INT, "20"), ast.Span{})
	qe := ast.NewQueryExpr("list", ident("Money"), "m",
		[]ast.QueryClause{skipClause(skipExpr), takeClause(lit(token.INT, "20"))}, ast.Span{})
	assign := ast.NewAssignStmt(ident("prices"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	want := `tmp1, err := tx.Select(ctx, runtime.Query[Money]{Skip: int(page * 20), Take: 20})`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// TestHoistList_CompleteForm é o critério de conclusão LITERAL da task
// (tasks.md I2.1): "list T t where C orderBy t.k descending skip N take M"
// gera um Select com a Query[T] COMPLETA — todos os campos populados
// corretamente, na ordem de declaração de Query[T] (Where, WhereEq, Less,
// OrderField, OrderDesc, Skip, Take — rtsrc/collection.go.txt). WhereEq
// (I7.1, REQ-38.1) também aparece aqui: o "where" é exatamente uma
// igualdade simples de campo, então hoistWhereEq (whereeq.go) o reconhece
// mesmo dentro desta forma completa.
func TestHoistList_CompleteForm(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("page", &types.Primitive{Name: "integer"})

	whereCond := ast.NewBinaryExpr(token.EQ, member(ident("e"), "description"),
		callExpr(ident("TransactionDescription"), arg(lit(token.STRING, "Salário"))), ast.Span{})
	skipExpr := ast.NewBinaryExpr(token.STAR, ident("page"), lit(token.INT, "20"), ast.Span{})
	qe := ast.NewQueryExpr("list", ident("StatementEntry"), "e", []ast.QueryClause{
		whereClause(whereCond),
		orderByClause(member(ident("e"), "description"), "descending"),
		skipClause(skipExpr),
		takeClause(lit(token.INT, "20")),
	}, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	want := `tmp1, err := tx.Select(ctx, runtime.Query[StatementEntry]{` +
		`Where: func(e StatementEntry) (bool, error) { return e.Description == TransactionDescription("Salário"), nil }, ` +
		`WhereEq: []runtime.FieldEq{{Field: "description", Value: TransactionDescription("Salário")}}, ` +
		`Less: func(a, b StatementEntry) (bool, error) { return b.Description < a.Description, nil }, ` +
		`OrderField: "description", OrderDesc: true, Skip: int(page * 20), Take: 20})`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava a Query[T] COMPLETA:\n%s\ngot:\n%s", want, out)
	}
}

// TestHoistList_ClauseOrderInSourceDoesNotMatter prova que a ordem TEXTUAL
// das cláusulas no slice n.Clauses não afeta o Go gerado — a ordem SEMÂNTICA
// é fixa (§design read-side 3.1), então "take M skip N orderBy K" e
// "orderBy K skip N take M" devem produzir o MESMO literal Query[T].
func TestHoistList_ClauseOrderInSourceDoesNotMatter(t *testing.T) {
	_, l1 := newWalletLowererWithBuiltins(t)
	orderKey := member(ident("m"), "currency")
	clausesA := []ast.QueryClause{
		takeClause(lit(token.INT, "2")),
		skipClause(lit(token.INT, "1")),
		orderByClause(orderKey, ""),
	}
	qe1 := ast.NewQueryExpr("list", ident("Money"), "m", clausesA, ast.Span{})
	out1 := lowerInFunc(t, l1, StmtContext{}, "func testList()", ast.NewAssignStmt(ident("prices"), qe1, ast.Span{}))

	_, l2 := newWalletLowererWithBuiltins(t)
	clausesB := []ast.QueryClause{
		orderByClause(member(ident("m"), "currency"), ""),
		skipClause(lit(token.INT, "1")),
		takeClause(lit(token.INT, "2")),
	}
	qe2 := ast.NewQueryExpr("list", ident("Money"), "m", clausesB, ast.Span{})
	out2 := lowerInFunc(t, l2, StmtContext{}, "func testList()", ast.NewAssignStmt(ident("prices"), qe2, ast.Span{}))

	if out1 != out2 {
		t.Fatalf("esperava o MESMO Go gerado independente da ordem textual das cláusulas:\nordem A:\n%s\nordem B:\n%s", out1, out2)
	}
	if !strings.Contains(out1, "Skip: 1, Take: 2") {
		t.Fatalf("esperava \"Skip: 1, Take: 2\" na Query[T] gerada, got:\n%s", out1)
	}
}

// --- 3. Cláusula duplicada / orderBy-skip-take em count (NFR-20, REQ-33.5). ---

func expectListError(t *testing.T, l *Lowerer, qe *ast.QueryExpr, wantSubstr string) {
	t.Helper()
	assign := ast.NewAssignStmt(ident("out"), qe, ast.Span{})
	e := emit.New("testpkg")
	sl := NewStmtLowerer(l, e, StmtContext{})
	err := sl.Stmt(assign)
	if err == nil {
		t.Fatalf("esperava erro (%s), obtive sucesso", wantSubstr)
	}
}

func TestHoistList_DuplicateWhereFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("list", ident("Money"), "m", []ast.QueryClause{
		whereClause(lit(token.TRUE, "")),
		whereClause(lit(token.TRUE, "")),
	}, ast.Span{})
	expectListError(t, l, qe, "where duplicado")
}

func TestHoistList_DuplicateOrderByFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("list", ident("Money"), "m", []ast.QueryClause{
		orderByClause(member(ident("m"), "currency"), ""),
		orderByClause(member(ident("m"), "amount"), ""),
	}, ast.Span{})
	expectListError(t, l, qe, "orderBy duplicado")
}

func TestHoistList_DuplicateSkipFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("list", ident("Money"), "m", []ast.QueryClause{
		skipClause(lit(token.INT, "1")),
		skipClause(lit(token.INT, "2")),
	}, ast.Span{})
	expectListError(t, l, qe, "skip duplicado")
}

func TestHoistList_DuplicateTakeFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("list", ident("Money"), "m", []ast.QueryClause{
		takeClause(lit(token.INT, "1")),
		takeClause(lit(token.INT, "2")),
	}, ast.Span{})
	expectListError(t, l, qe, "take duplicado")
}

// TestHoistCount_OrderByFailsExplicitly prova REQ-33.5: orderBy não tem
// efeito observável numa contagem — erro de geração, não Go que ignora a
// cláusula em silêncio.
func TestHoistCount_OrderByFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("count", ident("Money"), "m", []ast.QueryClause{
		orderByClause(member(ident("m"), "currency"), ""),
	}, ast.Span{})
	expectListError(t, l, qe, "orderBy em count")
}

func TestHoistCount_SkipFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("count", ident("Money"), "m", []ast.QueryClause{
		skipClause(lit(token.INT, "1")),
	}, ast.Span{})
	expectListError(t, l, qe, "skip em count")
}

func TestHoistCount_TakeFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("count", ident("Money"), "m", []ast.QueryClause{
		takeClause(lit(token.INT, "1")),
	}, ast.Span{})
	expectListError(t, l, qe, "take em count")
}

// TestHoistCount_WhereStillWorks prova que a validação nova não regrediu o
// caso comum: "count ... where ..." continua funcionando normalmente.
func TestHoistCount_WhereStillWorks(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("count", ident("Money"), "m", []ast.QueryClause{
		whereClause(ast.NewBinaryExpr(token.EQ, member(ident("m"), "currency"), lit(token.STRING, "BRL"), ast.Span{})),
	}, ast.Span{})
	assign := ast.NewAssignStmt(ident("total"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testCount()", assign)

	want := `tmp1, err := tx.Count(ctx, runtime.Query[Money]{Where: func(m Money) (bool, error) { return m.Currency == "BRL", nil }, WhereEq: []runtime.FieldEq{{Field: "currency", Value: "BRL"}}})`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}
