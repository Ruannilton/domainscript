package types

import (
	"testing"

	"domainscript/ast"
	"domainscript/token"
)

func span() ast.Span { return ast.Span{} }

func lit(k token.Kind, v string) *ast.Literal { return ast.NewLiteral(k, v, span()) }
func id(name string) *ast.Ident               { return ast.NewIdent(name, span()) }

// REQ-11.2: literais inferem seus primitivos.
func TestInferLiterals(t *testing.T) {
	m := NewModel(nil)
	cases := []struct {
		e    ast.Expr
		want string
	}{
		{lit(token.INT, "42"), "integer"},
		{lit(token.FLOAT, "3.14"), "decimal"},
		{lit(token.STRING, "x"), "string"},
		{lit(token.TRUE, "true"), "boolean"},
	}
	for _, c := range cases {
		if got := m.Infer("m", c.e, nil); got.String() != c.want {
			t.Errorf("Infer(%v) = %v, quer %v", c.e, got, c.want)
		}
	}
}

// REQ-11.2: um Ident infere o tipo do seu binding no escopo.
func TestInferIdentFromScope(t *testing.T) {
	m := NewModel(nil)
	sc := MapScope{"amount": &Primitive{Name: "decimal"}}
	got := m.Infer("m", id("amount"), sc)
	if got.String() != "decimal" {
		t.Errorf("Infer(amount) = %v, quer decimal", got)
	}
	// Nome ausente em todo escopo → ErrorType (sem cascata).
	if !IsError(m.Infer("m", id("inexistente"), sc)) {
		t.Error("ident inexistente deveria inferir ErrorType")
	}
}

// REQ-11.2: construção de VO infere o próprio VOType; acesso a membro infere o
// tipo do campo.
func TestInferConstructionAndMember(t *testing.T) {
	m, tab := modelFrom(t, `
		ValueObject Money { amount decimal currency string Valid { ok } }
	`)
	moneyT := m.TypeOf(lookup(t, tab, "Money"))

	// Money(amount: 10, currency: "BRL") → Money
	call := ast.NewCallExpr(id("Money"), []ast.Arg{
		{Name: "amount", Value: lit(token.INT, "10")},
		{Name: "currency", Value: lit(token.STRING, "BRL")},
	}, span())
	if got := m.Infer("m", call, nil); !Identical(got, moneyT) {
		t.Errorf("Infer(Money(...)) = %v, quer Money", got)
	}

	// m.amount → decimal, com m: Money no escopo.
	sc := MapScope{"m": moneyT}
	access := ast.NewMemberExpr(id("m"), "amount", token.Pos{}, span())
	if got := m.Infer("m", access, sc); got.String() != "decimal" {
		t.Errorf("Infer(m.amount) = %v, quer decimal", got)
	}
}

// REQ-11.2: operadores — comparação dá boolean, aritmético preserva o operando.
func TestInferOperators(t *testing.T) {
	m := NewModel(nil)
	cmp := ast.NewBinaryExpr(token.LT, lit(token.INT, "1"), lit(token.INT, "2"), span())
	if got := m.Infer("m", cmp, nil); got.String() != "boolean" {
		t.Errorf("Infer(1 < 2) = %v, quer boolean", got)
	}
	sum := ast.NewBinaryExpr(token.PLUS, lit(token.FLOAT, "1.0"), lit(token.FLOAT, "2.0"), span())
	if got := m.Infer("m", sum, nil); got.String() != "decimal" {
		t.Errorf("Infer(1.0 + 2.0) = %v, quer decimal", got)
	}
}

// REQ-11.3 (anti-cascata): uma subexpressão de erro produz ErrorType e a
// inferência não emite diagnóstico (Infer não tem acesso a um DiagnosticBag).
func TestInferErrorSubexpressionPropagates(t *testing.T) {
	m := NewModel(nil)
	// (<erro> + 1) → ErrorType, sem segundo erro.
	bad := ast.NewBinaryExpr(token.PLUS, ast.NewErrorExpr(span()), lit(token.INT, "1"), span())
	if !IsError(m.Infer("m", bad, nil)) {
		t.Error("operação com subexpressão de erro deveria inferir ErrorType")
	}
	// Acesso a membro sobre um erro não vira erro novo: continua ErrorType.
	access := ast.NewMemberExpr(ast.NewErrorExpr(span()), "campo", token.Pos{}, span())
	if !IsError(m.Infer("m", access, nil)) {
		t.Error("acesso a membro sobre erro deveria inferir ErrorType")
	}
}

// REQ-11.2: indexação de coleção infere o tipo do elemento.
func TestInferIndex(t *testing.T) {
	m := NewModel(nil)
	listInt := &Generic{Ctor: "List", Args: []Type{&Primitive{Name: "integer"}}}
	sc := MapScope{"xs": listInt}
	idx := ast.NewIndexExpr(id("xs"), lit(token.INT, "0"), span())
	if got := m.Infer("m", idx, sc); got.String() != "integer" {
		t.Errorf("Infer(xs[0]) = %v, quer integer", got)
	}
}
