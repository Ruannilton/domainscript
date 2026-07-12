package lower

import (
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/types"
)

// smartpartial_test.go prova os critérios de conclusão da task I6.1 (§design
// read-side 3.8/3.10, REQ-37): distinct/sum/focus como métodos embutidos de
// coleção com lambda tipada. Mesma convenção de orderby_test.go: tipos
// fabricados à mão (fieldMap/VOType/ShapeType/EnumType construídos direto,
// registrados no VOOperatorRegistry quando precisam de Operator) sobre o
// Lowerer do wallet real (newWalletLowererWithBuiltins) — o wallet não
// declara nenhum construto de coleção com item Shape/VO composto rico o
// bastante para exercitar as três formas, então as fixtures são sintéticas,
// como orderBy também precisou.

// lambdaExpr constrói uma *ast.LambdaExpr "param => body" para os testes
// abaixo (o parser produz esta forma para "x => x.k" — parse_range_lambda_
// test.go).
func lambdaExpr(param string, body ast.Expr) *ast.LambdaExpr {
	return ast.NewLambdaExpr(param, body, ast.Span{})
}

// ticketShapeType é o item de coleção fabricado usado por toda esta bateria:
// um Shape com um campo "id" (string, para focus) e "orderId" (string, para
// distinct) e "amount" (o Money REAL do wallet — decl_value.go/emitOperators
// já registrou "+"/"-"/">=" nele via newWalletLowererWithBuiltins, então sum
// exercita o dispatch de Operator sem precisar fabricar um VO à parte).
func ticketShapeType(moneyVO *types.VOType) *types.ShapeType {
	return &types.ShapeType{Name: "Ticket", Fields: []types.Field{
		{Name: "id", Type: &types.Primitive{Name: "string"}},
		{Name: "orderId", Type: &types.Primitive{Name: "string"}},
		{Name: "amount", Type: moneyVO},
	}}
}

// moneyVOType espelha o Money real do wallet (amount decimal, currency
// string) — usado tanto como campo de ticketShapeType quanto como o próprio
// tipo somado em alguns testes de sum.
func moneyVOType() *types.VOType {
	return &types.VOType{Name: "Money", Fields: []types.Field{
		{Name: "amount", Type: &types.Primitive{Name: "decimal"}},
		{Name: "currency", Type: &types.Primitive{Name: "string"}},
	}}
}

func ticketsGeneric(item types.Type) *types.Generic {
	return &types.Generic{Ctor: "AppendList", Args: []types.Type{item}}
}

// --- 1. distinct: tabela de comparabilidade de chave (§design read-side 3.8). ---

// TestHoistDistinct_PrimitiveStringKey_ListResultReceptor prova a forma
// EXATA da Policy §7 do spec: "soldTickets.distinct(t => t.orderId)" — o
// receptor é a variável materializada por um "list" anterior (aqui simulada
// via Bind direto, o mesmo efeito observável de assignBareIdent tipando o
// resultado de um "list", §design read-side 3.8 último bullet).
func TestHoistDistinct_PrimitiveStringKey_ListResultReceptor(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := ticketShapeType(moneyVOType())
	l.env.Bind("soldTickets", &types.Generic{Ctor: "List", Args: []types.Type{ticket}})

	call := callExpr(member(ident("soldTickets"), "distinct"), arg(lambdaExpr("t", member(ident("t"), "orderId"))))
	assign := ast.NewAssignStmt(ident("orderIds"), call, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testDistinct()", assign)

	for _, want := range []string{
		"tmp1 := map[string]struct{}{}",
		"tmp2 := make([]string, 0, len(soldTickets))",
		"for _, t := range soldTickets {",
		"if _, ok := tmp1[t.OrderId]; !ok {",
		"tmp1[t.OrderId] = struct{}{}",
		"tmp2 = append(tmp2, t.OrderId)",
		"orderIds := tmp2",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("esperava %q no Go gerado, não achei:\n%s", want, out)
		}
	}
}

// TestHoistDistinct_StateFieldReceptor prova o receptor (a): campo de coleção
// do state ("state.tickets.distinct(...)") — MemberExpr, não um Ident nu.
func TestHoistDistinct_StateFieldReceptor(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := ticketShapeType(moneyVOType())
	stateShape := &types.ShapeType{Name: "CartState", Fields: []types.Field{
		{Name: "tickets", Type: ticketsGeneric(ticket)},
	}}
	l.env.Bind("state", stateShape)

	call := callExpr(member(member(ident("state"), "tickets"), "distinct"), arg(lambdaExpr("t", member(ident("t"), "orderId"))))
	assign := ast.NewAssignStmt(ident("orderIds"), call, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testDistinct()", assign)
	if !strings.Contains(out, "for _, t := range state.Tickets {") {
		t.Fatalf("esperava iterar sobre state.Tickets, got:\n%s", out)
	}
}

// TestHoistDistinct_ParamReceptor prova o receptor (c): um parâmetro nu de
// tipo coleção.
func TestHoistDistinct_ParamReceptor(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := ticketShapeType(moneyVOType())
	l.env.Bind("items", ticketsGeneric(ticket))

	call := callExpr(member(ident("items"), "distinct"), arg(lambdaExpr("t", member(ident("t"), "orderId"))))
	assign := ast.NewAssignStmt(ident("orderIds"), call, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testDistinct()", assign)
	if !strings.Contains(out, "for _, t := range items {") {
		t.Fatalf("esperava iterar sobre items, got:\n%s", out)
	}
}

// TestHoistDistinct_VOWrapperKey prova que um VO wrapper (sobre um primitivo
// comparável) é uma chave válida — "type X Base" é comparável exatamente
// quando Base é.
func TestHoistDistinct_VOWrapperKey(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	itemIDVO := &types.VOType{Name: "ItemId", Base: &types.Primitive{Name: "string"}}
	ticket := &types.ShapeType{Name: "Ticket", Fields: []types.Field{
		{Name: "id", Type: itemIDVO},
	}}
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "distinct"), arg(lambdaExpr("t", member(ident("t"), "id"))))
	assign := ast.NewAssignStmt(ident("ids"), call, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testDistinct()", assign)
	if !strings.Contains(out, "map[ItemId]struct{}{}") {
		t.Fatalf("esperava chave ItemId (VO wrapper), got:\n%s", out)
	}
}

// TestHoistDistinct_EnumKey prova que um Enum é uma chave válida.
func TestHoistDistinct_EnumKey(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	statusEnum := &types.EnumType{Name: "Status", Base: &types.Primitive{Name: "string"}, Members: []string{"Open", "Closed"}}
	ticket := &types.ShapeType{Name: "Ticket", Fields: []types.Field{
		{Name: "status", Type: statusEnum},
	}}
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "distinct"), arg(lambdaExpr("t", member(ident("t"), "status"))))
	assign := ast.NewAssignStmt(ident("statuses"), call, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testDistinct()", assign)
	if !strings.Contains(out, "map[Status]struct{}{}") {
		t.Fatalf("esperava chave Status (Enum), got:\n%s", out)
	}
}

func expectSmartPartialError(t *testing.T, l *Lowerer, assign *ast.AssignStmt, wantSubstr string) {
	t.Helper()
	e := emit.New("testpkg")
	sl := NewStmtLowerer(l, e, StmtContext{})
	err := sl.Stmt(assign)
	if err == nil {
		t.Fatalf("esperava erro (%s), obtive sucesso", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("erro não contém %q: %v", wantSubstr, err)
	}
}

// TestHoistDistinct_DecimalKeyFailsExplicitly prova NFR-20: decimal
// (runtime.Decimal, backed por big.Int) NÃO é chave de map válida em Go —
// erro de geração, nunca Go que não compila.
func TestHoistDistinct_DecimalKeyFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := ticketShapeType(moneyVOType())
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "distinct"), arg(lambdaExpr("t", member(member(ident("t"), "amount"), "amount"))))
	assign := ast.NewAssignStmt(ident("x"), call, ast.Span{})
	expectSmartPartialError(t, l, assign, "não é comparável")
}

// TestHoistDistinct_CompositeVOKeyFailsExplicitly prova que um VO composto
// (Money inteiro, não um campo dele) não é chave válida — sem forma nativa
// de igualdade/hash estrutural neste escopo.
func TestHoistDistinct_CompositeVOKeyFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := ticketShapeType(moneyVOType())
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "distinct"), arg(lambdaExpr("t", member(ident("t"), "amount"))))
	assign := ast.NewAssignStmt(ident("x"), call, ast.Span{})
	expectSmartPartialError(t, l, assign, "não é comparável")
}

// TestHoistDistinct_WrongArity/NonLambdaArg cobrem a validação de forma do
// argumento (NFR-20: erro claro, não panic/Go quebrado).
func TestHoistDistinct_WrongArityFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("tickets", ticketsGeneric(ticketShapeType(moneyVOType())))
	call := callExpr(member(ident("tickets"), "distinct"))
	assign := ast.NewAssignStmt(ident("x"), call, ast.Span{})
	expectSmartPartialError(t, l, assign, "espera exatamente 1 argumento")
}

func TestHoistDistinct_NonLambdaArgFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("tickets", ticketsGeneric(ticketShapeType(moneyVOType())))
	call := callExpr(member(ident("tickets"), "distinct"), arg(ident("notALambda")))
	assign := ast.NewAssignStmt(ident("x"), call, ast.Span{})
	expectSmartPartialError(t, l, assign, "precisa ser uma lambda")
}

// TestHoistDistinct_NonCollectionReceptorFailsExplicitly prova que um
// receptor que não é List/AppendList/Set falha claro, não silenciosamente.
func TestHoistDistinct_NonCollectionReceptorFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("notACollection", &types.Primitive{Name: "string"})
	call := callExpr(member(ident("notACollection"), "distinct"), arg(lambdaExpr("t", ident("t"))))
	assign := ast.NewAssignStmt(ident("x"), call, ast.Span{})
	expectSmartPartialError(t, l, assign, "não é uma coleção conhecida")
}

// --- 2. sum: fold a partir do primeiro item (§design read-side 3.8). ---

// TestHoistSum_Integer prova a soma nativa de integer.
func TestHoistSum_Integer(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := &types.ShapeType{Name: "Ticket", Fields: []types.Field{{Name: "qty", Type: &types.Primitive{Name: "integer"}}}}
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "sum"), arg(lambdaExpr("t", member(ident("t"), "qty"))))
	assign := ast.NewAssignStmt(ident("total"), call, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testSum()", assign)
	for _, want := range []string{
		"var tmp1 int64",
		"if len(tickets) > 0 {",
		"tmp2 := tickets[0]",
		"tmp1 = tmp2.Qty",
		"for _, t := range tickets[1:] {",
		"tmp1 = tmp1 + t.Qty",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("esperava %q no Go gerado, não achei:\n%s", want, out)
		}
	}
}

// TestHoistSum_Decimal prova que decimal soma via .Add (runtime.Decimal não
// é numérico nativo do Go — "+" não compilaria).
func TestHoistSum_Decimal(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := &types.ShapeType{Name: "Ticket", Fields: []types.Field{{Name: "price", Type: &types.Primitive{Name: "decimal"}}}}
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "sum"), arg(lambdaExpr("t", member(ident("t"), "price"))))
	assign := ast.NewAssignStmt(ident("total"), call, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testSum()", assign)
	if !strings.Contains(out, "tmp1 = tmp1.Add(t.Price)") {
		t.Fatalf("esperava soma via .Add, got:\n%s", out)
	}
}

// TestHoistSum_VOCompositeWithOperatorPlus prova o dispatch fallível de
// Operator + (Money real do wallet, que declara "+") — o erro do operador
// propaga pelo ctx.ExitOnError do statement ao redor, não por uma closure.
func TestHoistSum_VOCompositeWithOperatorPlus(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	money := moneyVOType()
	ticket := ticketShapeType(money)
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "sum"), arg(lambdaExpr("t", member(ident("t"), "amount"))))
	assign := ast.NewAssignStmt(ident("total"), call, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testSum() error", assign, ast.NewReturnStmt(nil, ast.Span{}))
	for _, want := range []string{
		"var tmp1 Money",
		"tmp1 = tmp2.Amount",
		"var err error",
		"tmp1, err = tmp1.Add(t.Amount)",
		"if err != nil {",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("esperava %q no Go gerado, não achei:\n%s", want, out)
		}
	}
}

// TestHoistSum_VOCompositeWithoutOperatorPlusFailsExplicitly prova NFR-20
// para um VO composto sem Operator + declarado.
func TestHoistSum_VOCompositeWithoutOperatorPlusFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	rankVO := &types.VOType{Name: "Rank", Fields: []types.Field{{Name: "value", Type: &types.Primitive{Name: "integer"}}}}
	ticket := &types.ShapeType{Name: "Ticket", Fields: []types.Field{{Name: "rank", Type: rankVO}}}
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "sum"), arg(lambdaExpr("t", member(ident("t"), "rank"))))
	assign := ast.NewAssignStmt(ident("total"), call, ast.Span{})
	expectSmartPartialError(t, l, assign, "não declara Operator +")
}

// TestHoistSum_VOWrapperFailsExplicitly prova que um wrapper (nunca declara
// Operator) não soma.
func TestHoistSum_VOWrapperFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	qtyVO := &types.VOType{Name: "Qty", Base: &types.Primitive{Name: "integer"}}
	ticket := &types.ShapeType{Name: "Ticket", Fields: []types.Field{{Name: "qty", Type: qtyVO}}}
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "sum"), arg(lambdaExpr("t", member(ident("t"), "qty"))))
	assign := ast.NewAssignStmt(ident("total"), call, ast.Span{})
	expectSmartPartialError(t, l, assign, "não declara Operator +")
}

// TestHoistSum_NonNumericPrimitiveFailsExplicitly prova que um primitivo sem
// soma com sentido (string) falha claro.
func TestHoistSum_NonNumericPrimitiveFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := &types.ShapeType{Name: "Ticket", Fields: []types.Field{{Name: "label", Type: &types.Primitive{Name: "string"}}}}
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "sum"), arg(lambdaExpr("t", member(ident("t"), "label"))))
	assign := ast.NewAssignStmt(ident("total"), call, ast.Span{})
	expectSmartPartialError(t, l, assign, "não suporta soma")
}

// --- 3. focus: busca linear pelo campo "id" (§design read-side 3.8). ---

// TestHoistFocus_HappyPath prova a forma Go exata: ponteiro, nil se ausente.
func TestHoistFocus_HappyPath(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := ticketShapeType(moneyVOType())
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "focus"), arg(ident("ticketId")))
	l.env.Bind("ticketId", &types.Primitive{Name: "string"})
	assign := ast.NewAssignStmt(ident("ticket"), call, ast.Span{})

	out := lowerInFunc(t, l, StmtContext{}, "func testFocus()", assign)
	for _, want := range []string{
		"var tmp1 *Ticket",
		"for i := range tickets {",
		"if tickets[i].Id == ticketId {",
		"tmp1 = &tickets[i]",
		"break",
		"ticket := tmp1",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("esperava %q no Go gerado, não achei:\n%s", want, out)
		}
	}
}

// TestHoistFocus_ExistsComposition prova a composição com "ensure ...
// exists" (§design read-side 3.8, último bullet): "exists" já traduz "<X> !=
// nil" sobre QUALQUER X hoisted — funciona sobre focus sem nenhum código
// novo em existsExpr, porque hoistQueryExpr hoisteia o Target de "exists"
// através do MESMO hoistSubtree que despacha para hoistFocus.
func TestHoistFocus_ExistsComposition(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := ticketShapeType(moneyVOType())
	l.env.Bind("tickets", ticketsGeneric(ticket))
	l.env.Bind("ticketId", &types.Primitive{Name: "string"})

	focusCall := callExpr(member(ident("tickets"), "focus"), arg(ident("ticketId")))
	existsExpr := ast.NewQueryExpr("exists", focusCall, "", nil, ast.Span{})
	ensure := ast.NewEnsureStmt(existsExpr, ast.NewExprStmt(ident("TicketNotFound"), ast.Span{}), ast.Span{})

	out := lowerInFunc(t, l, StmtContext{ZeroValues: []string{"nil"}}, "func testFocus() ([]int, error)", ensure)
	for _, want := range []string{
		"var tmp1 *Ticket",
		"if !(tmp1 != nil) {",
		"return nil, ErrTicketNotFound",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("esperava %q no Go gerado, não achei:\n%s", want, out)
		}
	}
}

// TestHoistFocus_ItemWithoutIDFieldFailsExplicitly prova NFR-20: item sem
// campo "id" — a convenção fixa do §20 não tem forma alternativa a inventar.
func TestHoistFocus_ItemWithoutIDFieldFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	noIDShape := &types.ShapeType{Name: "Anonymous", Fields: []types.Field{{Name: "value", Type: &types.Primitive{Name: "string"}}}}
	l.env.Bind("items", ticketsGeneric(noIDShape))
	l.env.Bind("x", &types.Primitive{Name: "string"})

	call := callExpr(member(ident("items"), "focus"), arg(ident("x")))
	assign := ast.NewAssignStmt(ident("found"), call, ast.Span{})
	expectSmartPartialError(t, l, assign, `não declara um campo "id"`)
}

// TestHoistFocus_WrongArityFailsExplicitly prova a validação de aridade.
func TestHoistFocus_WrongArityFailsExplicitly(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	l.env.Bind("tickets", ticketsGeneric(ticketShapeType(moneyVOType())))
	call := callExpr(member(ident("tickets"), "focus"))
	assign := ast.NewAssignStmt(ident("x"), call, ast.Span{})
	expectSmartPartialError(t, l, assign, "espera exatamente 1 argumento")
}

// --- 4. Inferência de tipos (TypeEnv.InferAssignRHS, §design read-side 3.10). ---

func TestInferSmartPartialCall_DistinctIsListOfKeyType(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := ticketShapeType(moneyVOType())
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "distinct"), arg(lambdaExpr("t", member(ident("t"), "orderId"))))
	got, err := l.env.InferAssignRHS(call)
	if err != nil {
		t.Fatalf("InferAssignRHS(distinct): erro inesperado: %v", err)
	}
	gen, ok := got.(*types.Generic)
	if !ok || gen.Ctor != "List" || gen.Args[0].String() != "string" {
		t.Fatalf("esperava List<string>, got %s (%T)", got.String(), got)
	}
}

func TestInferSmartPartialCall_SumIsValueType(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	money := moneyVOType()
	ticket := ticketShapeType(money)
	l.env.Bind("tickets", ticketsGeneric(ticket))

	call := callExpr(member(ident("tickets"), "sum"), arg(lambdaExpr("t", member(ident("t"), "amount"))))
	got, err := l.env.InferAssignRHS(call)
	if err != nil {
		t.Fatalf("InferAssignRHS(sum): erro inesperado: %v", err)
	}
	if got.String() != "Money" {
		t.Fatalf("esperava Money, got %s (%T)", got.String(), got)
	}
}

func TestInferSmartPartialCall_FocusIsItemType(t *testing.T) {
	_, l := newWalletLowererWithBuiltins(t)
	ticket := ticketShapeType(moneyVOType())
	l.env.Bind("tickets", ticketsGeneric(ticket))
	l.env.Bind("ticketId", &types.Primitive{Name: "string"})

	call := callExpr(member(ident("tickets"), "focus"), arg(ident("ticketId")))
	got, err := l.env.InferAssignRHS(call)
	if err != nil {
		t.Fatalf("InferAssignRHS(focus): erro inesperado: %v", err)
	}
	if got.String() != "Ticket" {
		t.Fatalf("esperava Ticket, got %s (%T)", got.String(), got)
	}
}
