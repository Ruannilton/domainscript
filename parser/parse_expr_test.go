package parser

import (
	"strconv"
	"testing"

	"domainscript/ast"
	"domainscript/token"
)

// sexpr renderiza uma expressão como s-expression para asserções compactas de
// estrutura/precedência.
func sexpr(e ast.Expr) string {
	switch n := e.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.Literal:
		if n.Kind == token.STRING {
			return strconv.Quote(n.Value)
		}
		if n.Value != "" {
			return n.Value
		}
		return n.Kind.String()
	case *ast.BinaryExpr:
		return "(" + n.Op.String() + " " + sexpr(n.Left) + " " + sexpr(n.Right) + ")"
	case *ast.UnaryExpr:
		return "(" + n.Op.String() + " " + sexpr(n.X) + ")"
	case *ast.MemberExpr:
		return "(. " + sexpr(n.X) + " " + n.Name + ")"
	case *ast.CallExpr:
		s := "(call " + sexpr(n.Fn)
		for _, a := range n.Args {
			s += " "
			if a.Name != "" {
				s += a.Name + ":"
			}
			s += sexpr(a.Value)
		}
		return s + ")"
	case *ast.IndexExpr:
		return "(idx " + sexpr(n.X) + " " + sexpr(n.Index) + ")"
	case *ast.MatchExpr:
		s := "(matchE " + sexpr(n.Subject)
		for _, a := range n.Arms {
			s += " (arm " + matchArmHead(a.Patterns, a.Guard) + " " + sexpr(a.Body) + ")"
		}
		return s + ")"
	case *ast.ListExpr:
		s := "["
		for i, e := range n.Elems {
			if i > 0 {
				s += " "
			}
			s += sexpr(e)
		}
		return s + "]"
	case *ast.QueryExpr:
		s := "(" + n.Op + " " + sexpr(n.Target)
		if n.Binding != "" {
			s += " :" + n.Binding
		}
		for _, c := range n.Clauses {
			s += " {" + c.Kw
			if c.Expr != nil {
				s += " " + sexpr(c.Expr)
			}
			if c.Extra != "" {
				s += " " + c.Extra
			}
			s += "}"
		}
		return s + ")"
	case *ast.ObjectExpr:
		s := "{"
		for i, e := range n.Entries {
			if i > 0 {
				s += " "
			}
			s += e.Key + ":" + sexpr(e.Value)
		}
		return s + "}"
	case *ast.RangeExpr:
		return "(.. " + sexpr(n.Low) + " " + sexpr(n.High) + ")"
	case *ast.LambdaExpr:
		return "(lambda " + n.Param + " " + sexpr(n.Body) + ")"
	case *ast.ErrorExpr:
		return "<err>"
	default:
		return "?"
	}
}

// matchArmHead renderiza os padrões e o guard de um braço de match.
func matchArmHead(pats []ast.Expr, guard ast.Expr) string {
	s := "["
	for i, pt := range pats {
		if i > 0 {
			s += ","
		}
		s += sexpr(pt)
	}
	s += "]"
	if guard != nil {
		s += "when" + sexpr(guard)
	}
	return s
}

// parseExprOK lexa src, parseia uma expressão e exige zero diagnósticos e cursor
// no EOF (expressão consumida por completo).
func parseExprOK(t *testing.T, src string) ast.Expr {
	t.Helper()
	p, bag := mk(src)
	e := p.parseExpr()
	if bag.Len() != 0 {
		t.Fatalf("parseExpr(%q) gerou diagnósticos: %s", src, bag.Render())
	}
	if !p.atEnd() {
		t.Fatalf("parseExpr(%q) não consumiu tudo; parou em %v", src, p.cur().Kind)
	}
	return e
}

func TestExprPrecedence(t *testing.T) {
	cases := map[string]string{
		"a + b * c >= d":  "(>= (+ a (* b c)) d)",
		"a or b and c":    "(or a (and b c))",
		"a == b or c":     "(or (== a b) c)",
		"a - b - c":       "(- (- a b) c)", // esquerdo-associativo
		"-a * b":          "(* (- a) b)",
		"not a == b":      "(== (not a) b)",
		"(a + b) * c":     "(* (+ a b) c)",
		"a < b == c <= d": "(== (< a b) (<= c d))",
	}
	for src, want := range cases {
		if got := sexpr(parseExprOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

func TestExprPostfixChains(t *testing.T) {
	cases := map[string]string{
		"a.b.c":                      "(. (. a b) c)",
		"self.Deposit(amount, desc)": "(call (. self Deposit) amount desc)",
		"items[i]":                   "(idx items i)",
		"a.b(c)[d]":                  "(idx (call (. a b) c) d)",
		"x.y.z(w)":                   "(call (. (. x y) z) w)",
		"caller.hasRole(\"admin\")":  "(call (. caller hasRole) \"admin\")",
	}
	for src, want := range cases {
		if got := sexpr(parseExprOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

func TestExprNamedArgsConstruction(t *testing.T) {
	got := sexpr(parseExprOK(t, "Money(amount: a, currency: b)"))
	want := "(call Money amount:a currency:b)"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestExprErrorRecovers(t *testing.T) {
	// Operando faltando: produz diagnóstico e um nó de erro, sem travar.
	p, bag := mk("a +")
	e := p.parseExpr()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico para operando ausente")
	}
	bin, ok := e.(*ast.BinaryExpr)
	if !ok {
		t.Fatalf("esperava BinaryExpr, veio %T", e)
	}
	if _, ok := bin.Right.(*ast.ErrorExpr); !ok {
		t.Errorf("lado direito = %T, quero *ast.ErrorExpr", bin.Right)
	}
}
