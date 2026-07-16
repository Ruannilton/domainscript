package lower

import (
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/token"
)

// builtins_test.go prova os critérios de conclusão da task E5.3 (§design
// codegen 3.6, REQ-22.7(a)) sobre o wallet real (docs/examples/wallet) onde
// possível, e fixtures sintéticas onde o wallet não exercita a forma (random/
// random_str, list com "where", count — nenhum construto do wallet os usa).
//
// Convenção de nomes desta task, usada em todo teste abaixo: runtimeAlias
// "runtime" (mesmo alias que newWalletLowerer já passa a NewLowerer),
// ctxGoName "ctx", storeGoName "tx" (o mesmo nome que §design 3.8 usa no
// exemplo de UseCase/unit of work para o parâmetro de acesso à persistência).

// callExpr monta um *ast.CallExpr(fn, args...) sem posição real — helper
// só para estes testes (paralelo a ident/member/lit de expr_test.go).
func callExpr(fn ast.Expr, args ...ast.Arg) *ast.CallExpr {
	return ast.NewCallExpr(fn, args, ast.Span{})
}

// arg é um argumento posicional (Name == "") — atalho para ast.Arg{Value: v}.
func arg(v ast.Expr) ast.Arg { return ast.Arg{Value: v} }

// newWalletLowererWithBuiltins é newWalletLowerer + um BuiltinLowerer padrão
// anexado (runtimeAlias "runtime", ctxGoName "ctx", storeGoName "tx").
func newWalletLowererWithBuiltins(t *testing.T) (*ast.AggregateDecl, *Lowerer) {
	t.Helper()
	prog, l := newWalletLowerer(t)
	l.WithBuiltins(NewBuiltinLowerer("runtime", "ctx", "tx"))
	return findAggregate(t, prog, "Wallet"), l
}

// --- 1. now()/uuid() — forma Go exata. ---

func TestExpr_Now(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)

	got, err := l.Expr(callExpr(ident("now")))
	if err != nil {
		t.Fatalf("Expr(now()): erro inesperado: %v", err)
	}
	want := "runtime.Now(ctx)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpr_UUID(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)

	got, err := l.Expr(callExpr(ident("uuid")))
	if err != nil {
		t.Fatalf("Expr(uuid()): erro inesperado: %v", err)
	}
	want := "runtime.UUID()"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// --- 2. random(min, max)/random_str(length) — forma Go exata (o teste
// comportamental do runtime estendido mora em codegen/rtsrc/runtime_test.go.txt,
// TestRandomWithinInclusiveRange/TestRandomStrLengthAndAlphanumericCharset). ---

func TestExpr_Random(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)

	got, err := l.Expr(callExpr(ident("random"), arg(lit(token.INT, "1")), arg(lit(token.INT, "100"))))
	if err != nil {
		t.Fatalf("Expr(random(1, 100)): erro inesperado: %v", err)
	}
	want := "runtime.Random(1, 100)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpr_RandomStr(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)

	got, err := l.Expr(callExpr(ident("random_str"), arg(lit(token.INT, "10"))))
	if err != nil {
		t.Fatalf("Expr(random_str(10)): erro inesperado: %v", err)
	}
	want := "runtime.RandomStr(10)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpr_BuiltinFunc_WrongArityFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)

	_, err := l.Expr(callExpr(ident("random"), arg(lit(token.INT, "1"))))
	if err == nil {
		t.Fatal("esperava erro: random(...) chamado com 1 argumento, esperava 2")
	}
}

func TestExpr_QueryExpr_WithoutBuiltinsFailsExplicitly(t *testing.T) {
	_, l := newWalletLowerer(t) // SEM WithBuiltins
	qe := ast.NewQueryExpr("exists", ident("wallet"), "", nil, ast.Span{})

	_, err := l.Expr(qe)
	if err == nil {
		t.Fatal("esperava erro: QueryExpr sem BuiltinLowerer configurado")
	}
}

// --- 3. O caso real do wallet: "wallet = load Wallet(cmd.walletId)" (do
// UseCase PerformDeposit real) — a limitação documentada em stmt_test.go
// (TestStmt_Assign_BareIdent_FirstOccurrenceUsesShortDecl, E5.2) fechada de
// verdade: load T(id) agora tem forma Go (builtins.go, BuiltinLowerer.LoadCall)
// e hoisting (hoistLoad, stmt.go) — mesmo tratamento que uma construção de VO
// falível já recebia.
//
// Mesma ressalva de sempre sobre a ambiguidade do parser (documentada em
// env_test.go/TestInferAssignRHS_LoadFromRealUseCase e stmt_test.go): a
// gramática não fecha statement por token/quebra de linha, então re-parsear
// literalmente "wallet = load Wallet(cmd.walletId)\nwallet.Deposit(...)"
// consumiria o "wallet" da PRÓXIMA linha como binding de "load" e encadearia
// ".Deposit(...)" sobre a própria QueryExpr — uma ambiguidade pré-existente
// do parser, fora do escopo desta task. A QueryExpr é construída à mão sobre
// o Model/SymbolTable REAIS do wallet, o mesmo padrão já autorizado pelos
// testes de E5.0/E5.2 para esta MESMA linha do wallet.
//
// O hoisting reusa o mecanismo geral (mesmo de VO composto/operador falível):
// "load Wallet(id)" hoisteia para uma temporária ("tmp1, err := LoadWallet(
// tx, cmd.WalletId); if err != nil { return err }"), e só então "wallet" é
// atribuído a partir dela ("wallet := tmp1") — a mesma forma que
// TestStmt_Apply_DepositPerformed_RealWallet já prova para
// "state.Balance = state.Balance.Add(ev.Amount)" (hoisting sempre passa por
// uma temporária, nunca embute o err diretamente no target do Assign).
func TestStmt_Load_RealWalletUseCase_CompletionCriterion(t *testing.T) {
	prog, l := newWalletLowerer(t)
	l.WithBuiltins(NewBuiltinLowerer("runtime", "ctx", "tx"))
	uc := findUseCase(t, prog, "PerformDeposit")
	l.env.SeedUseCaseExecute(uc.Handles) // cmd = Deposit (tipo real do Command)

	walletIdArg := member(ident("cmd"), "walletId")
	target := callExpr(ident("Wallet"), arg(walletIdArg))
	qe := ast.NewQueryExpr("load", target, "", nil, ast.Span{})
	assign := ast.NewAssignStmt(ident("wallet"), qe, ast.Span{})

	// UseCase.execute (§design 3.8) devolve só error: ZeroValues vazio ⇒
	// ExitOnError("err") = "return err".
	out := lowerInFunc(t, l, StmtContext{}, "func testExecute(cmd Deposit) error", assign)

	if !strings.Contains(out, "tmp1, err := LoadWallet(tx, cmd.WalletId)") {
		t.Fatalf("esperava \"tmp1, err := LoadWallet(tx, cmd.WalletId)\", got:\n%s", out)
	}
	if !strings.Contains(out, "if err != nil {") {
		t.Fatalf("esperava \"if err != nil {\", got:\n%s", out)
	}
	if !strings.Contains(out, "return err") {
		t.Fatalf("esperava \"return err\" no caminho de erro, got:\n%s", out)
	}
	if !strings.Contains(out, "wallet := tmp1") {
		t.Fatalf("esperava \"wallet := tmp1\" (wallet vinculado a partir da temporária), got:\n%s", out)
	}
}

// TestStmt_Load_AsClause_FailsExplicitly prova que "load T(id) as V"
// (mapeamento para View — Read Side, REQ-21, E8.1) falha explicitamente em
// vez de gerar Go com o tipo errado (NFR-14): não é responsabilidade desta
// task decidir a forma de mapeamento para View (ex. o "GetWallet" real do
// wallet, "return load Wallet(id) as WalletView", read.ds).
func TestStmt_Load_AsClause_FailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	target := callExpr(ident("Wallet"), arg(ident("id")))
	qe := ast.NewQueryExpr("load", target, "", []ast.QueryClause{{Kw: "as", Extra: "WalletView"}}, ast.Span{})
	assign := ast.NewAssignStmt(ident("view"), qe, ast.Span{})

	e := emit.New("testpkg")
	sl := NewStmtLowerer(l, e, StmtContext{})
	if err := sl.Stmt(assign); err == nil {
		t.Fatal("esperava erro explícito: load ... as V é Read Side (E8.1), fora do escopo de E5.3")
	}
}

// --- 4. list/count/exists: pelo menos 1 teste sintético cada, provando que
// a forma é gerada — desde H4 (§22.4), backed de verdade por
// runtime.Collection[T] (rtsrc/collection.go.txt, ver builtins.go/
// stmt.go:hoistQueryPredicate): "where" vira um PREDICADO POR ITEM, não mais
// um bool solto avaliado uma única vez. Desde o ciclo Read Side (REQ-33/
// REQ-36/REQ-38, §design read-side 2), o predicado entra num
// "runtime.Query[T]{Where: ...}" consumido por Collection[T].Select/Count —
// desde I1.1, hoistQueryPredicate já emite "func(item T) (bool, error) {
// ... }" DIRETAMENTE (a forma que o campo Where exige, REQ-36.2), sem
// nenhuma ponte de adaptação: a ponte "runtime.Infallible(...)" que I0.1
// introduziu (quando hoistQueryPredicate ainda produzia "func(T) bool", sem
// error) foi removida do runtime junto com esta mudança — ver a doc de
// BuiltinLowerer.ListCall/queryLiteral (builtins.go) e de hoistQueryPredicate
// (stmt.go) sobre o hoisting de verdade que fecha G-8.

// TestStmt_List_Synthetic_NoWhere usa "list StatementEntry" (a MESMA forma
// do "ListEntries" real do wallet, read.ds: "return list StatementEntry")
// — sem cláusula "where", predGo é o literal Go "nil" e o campo Where fica
// de fora do literal Query[T] (Go zero value == "todo item passa", mesmo
// contrato do antigo "List(ctx, nil)" — rtsrc/collection.go.txt).
func TestStmt_List_Synthetic_NoWhere(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("list", ident("StatementEntry"), "", nil, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	want := "tmp1, err := tx.Select(ctx, runtime.Query[StatementEntry]{})"
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
	if !strings.Contains(out, "entries := tmp1") {
		t.Fatalf("esperava \"entries := tmp1\", got:\n%s", out)
	}
}

// TestStmt_List_Synthetic_WithWhere_NoBinding prova a forma SEM binding
// explícito (ex. "list StatementEntry where true"): o predicado ainda vira
// uma lambda de verdade (nunca mais um bool solto — a forma ANTERIOR a H4,
// documentada como achado incorreto na doc de hoistQueryPredicate), usando o
// nome sintético "item" (default quando QueryExpr.Binding == ""), e desde
// I1.1 já na assinatura "(bool, error)" diretamente (sem "runtime.
// Infallible(...)" — removido, ver a doc do arquivo). O wallet não usa
// "where" em nenhum list/count, por isso sintética; a condição é um literal
// trivial só para não entrar em dispatch de operador de VO (fora do escopo
// deste teste).
func TestStmt_List_Synthetic_WithWhere_NoBinding(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	clauses := []ast.QueryClause{{Kw: "where", Expr: lit(token.TRUE, "")}}
	qe := ast.NewQueryExpr("list", ident("StatementEntry"), "", clauses, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	want := "tmp1, err := tx.Select(ctx, runtime.Query[StatementEntry]{Where: func(item StatementEntry) (bool, error) { return true, nil }})"
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// TestStmt_List_Synthetic_WithBinding_FiltersPerItem é o critério de
// conclusão central do redesenho de H4 (§22.4, Camada 1): um "where" de
// verdade, com um binding explícito ("list StatementEntry e where
// e.description == ..."), precisa acessar um CAMPO do item — algo que a
// forma ANTERIOR a esta task não suportava (o "where" era avaliado como bool
// solto no escopo ATUAL, sem nenhum item vinculado — ver TestStmt_List_
// Synthetic_WithWhere_NoBinding, que confirmava exatamente essa lacuna antes
// de ser corrigida). "e" é vinculado ao tipo StatementEntry (TypeEnv.
// ItemTypeOf) num escopo-filho só para a duração de "where", então
// "e.description" resolve via o Lowerer.member já existente (nenhum código
// novo de resolução de membro) para "e.Description".
func TestStmt_List_Synthetic_WithBinding_FiltersPerItem(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	cond := ast.NewBinaryExpr(token.EQ, member(ident("e"), "description"), callExpr(ident("TransactionDescription"), arg(lit(token.STRING, "Salário"))), ast.Span{})
	clauses := []ast.QueryClause{{Kw: "where", Expr: cond}}
	qe := ast.NewQueryExpr("list", ident("StatementEntry"), "e", clauses, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	want := `tmp1, err := tx.Select(ctx, runtime.Query[StatementEntry]{Where: func(e StatementEntry) (bool, error) { return e.Description == TransactionDescription("Salário"), nil }, WhereEq: []runtime.FieldEq{{Field: "description", Value: TransactionDescription("Salário")}}})`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// TestStmt_Count_Synthetic_WithBinding_FiltersPerItem é o análogo de count —
// mesmo predicado por item, agora produzindo (int64, error).
func TestStmt_Count_Synthetic_WithBinding_FiltersPerItem(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	cond := ast.NewBinaryExpr(token.EQ, member(ident("e"), "description"), callExpr(ident("TransactionDescription"), arg(lit(token.STRING, "Salário"))), ast.Span{})
	clauses := []ast.QueryClause{{Kw: "where", Expr: cond}}
	qe := ast.NewQueryExpr("count", ident("StatementEntry"), "e", clauses, ast.Span{})
	assign := ast.NewAssignStmt(ident("total"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testCount()", assign)

	want := `tmp1, err := tx.Count(ctx, runtime.Query[StatementEntry]{Where: func(e StatementEntry) (bool, error) { return e.Description == TransactionDescription("Salário"), nil }, WhereEq: []runtime.FieldEq{{Field: "description", Value: TransactionDescription("Salário")}}})`
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
}

// TestStmt_List_Where_NeedsHoistingFailsExplicitly INVERTE (I1.1, fecha
// G-8): a mesma condição que ANTES desta task falhava com um erro explícito
// — "e.amount == Money(amount: 10, currency: \"BRL\")", onde Money é VO
// COMPOSTO construído com args nomeados, o caso que needsHoistVOConstruct
// sempre marca como falível — agora GERA um predicado válido, provando a
// forma em BLOCO documentada em hoistQueryPredicate: a construção de Money
// hoisteia para uma temporária DENTRO do corpo do predicado, com seu próprio
// "if err != nil { return false, err }" (a assinatura "(bool, error)"
// acomoda isso desde I0.1; só a GERAÇÃO do bloco era a lacuna, fechada
// agora), e só então o "return" final compara a temporária: "e.Amount ==
// tmp1" — a igualdade em si não é um Operator declarado de Money (só
// +/-/>=), então o BinaryExpr de "==" continua nativo, não mais um segundo
// hoisting.
func TestStmt_List_Where_NeedsHoistingFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	moneyCall := callExpr(ident("Money"),
		ast.Arg{Name: "amount", Value: lit(token.INT, "10")},
		ast.Arg{Name: "currency", Value: lit(token.STRING, "BRL")},
	)
	cond := ast.NewBinaryExpr(token.EQ, member(ident("e"), "amount"), moneyCall, ast.Span{})
	clauses := []ast.QueryClause{{Kw: "where", Expr: cond}}
	qe := ast.NewQueryExpr("list", ident("StatementEntry"), "e", clauses, ast.Span{})
	assign := ast.NewAssignStmt(ident("entries"), qe, ast.Span{})

	// lowerInFunc só devolve com sucesso quando e.Bytes() (go/format.Source)
	// aceita o texto como Go sintaticamente válido — a mesma prova de
	// "compila" (sintaxe) que todo outro teste deste arquivo já usa; o
	// smoke-compile de VERDADE (go build sobre um projeto gerado real) é
	// TestEmitPolicyPredicateSmokeCompile/TestPolicyPredicateHandwrittenRunGreen,
	// em codegen/gentest_policy_predicate_test.go.
	out := lowerInFunc(t, l, StmtContext{}, "func testList()", assign)

	for _, want := range []string{
		"func(e StatementEntry) (bool, error) {",
		"tmp1, err := NewMoney(runtime.NewDecimalFromInt(10), \"BRL\")",
		"if err != nil {",
		"return false, err",
		"return e.Amount == tmp1, nil",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("esperava %q no predicado gerado (forma em bloco, I1.1), got:\n%s", want, out)
		}
	}
}

// TestStmt_Count_Synthetic prova a forma de "count" — mesma ressalva de API
// provisória do list.
func TestStmt_Count_Synthetic(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qe := ast.NewQueryExpr("count", ident("StatementEntry"), "", nil, ast.Span{})
	assign := ast.NewAssignStmt(ident("total"), qe, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testCount()", assign)

	want := "tmp1, err := tx.Count(ctx, runtime.Query[StatementEntry]{})"
	if !strings.Contains(out, want) {
		t.Fatalf("esperava %q, got:\n%s", want, out)
	}
	if !strings.Contains(out, "total := tmp1") {
		t.Fatalf("esperava \"total := tmp1\", got:\n%s", out)
	}
}

// TestStmt_Ensure_Exists_Synthetic prova "exists" (QueryExpr pós-fixo) na
// forma real de uso do spec: "ensure X exists else Error". O wallet não usa
// "exists" hoje — por isso sintética, sobre uma variável "wallet" vinculada
// ao tipo do Aggregate real (o caso que a decisão de design (§7 do prompt da
// task) tem em mente: um ponteiro de Aggregate já carregado).
func TestStmt_Ensure_Exists_Synthetic(t *testing.T) {
	agg, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("wallet", l.env.TypeOfName(agg.Name))

	cond := ast.NewQueryExpr("exists", ident("wallet"), "", nil, ast.Span{})
	ensure := ast.NewEnsureStmt(cond, ast.NewExprStmt(ident("WalletNotFound"), ast.Span{}), ast.Span{})

	ctx := StmtContext{ZeroValues: []string{"nil"}}
	out := lowerInFunc(t, l, ctx, "func testExists() ([]int, error)", ensure)

	if !strings.Contains(out, "if !(wallet != nil) {") {
		t.Fatalf("esperava \"if !(wallet != nil) {\", got:\n%s", out)
	}
	if !strings.Contains(out, "return nil, ErrWalletNotFound") {
		t.Fatalf("esperava \"return nil, ErrWalletNotFound\", got:\n%s", out)
	}
}

// TestExpr_Exists_Pure prova o caminho de expressão pura direto (sem
// ensure ao redor), confirmando a forma exata "<X> != nil" (§design 4.3 do
// prompt da task).
func TestExpr_Exists_Pure(t *testing.T) {
	agg, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("wallet", l.env.TypeOfName(agg.Name))

	got, err := l.Expr(ast.NewQueryExpr("exists", ident("wallet"), "", nil, ast.Span{}))
	if err != nil {
		t.Fatalf("Expr(wallet exists): erro inesperado: %v", err)
	}
	want := "wallet != nil"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestQueryExprPure_FileOpsFailExplicitly prova que store/call/delete (ops
// de arquivo, §2.5) falham com um erro claro apontando pra G1a — não são Go
// arbitrário nem panics.
func TestQueryExprPure_FileOpsFailExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	for _, op := range []string{"store", "call", "delete"} {
		qe := ast.NewQueryExpr(op, ident("f"), "", nil, ast.Span{})
		if _, err := l.Expr(qe); err == nil {
			t.Fatalf("QueryExpr.Op %q: esperava erro (ops de arquivo são G1a), obtive sucesso", op)
		}
	}
}
