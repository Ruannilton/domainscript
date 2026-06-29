package parser

import (
	"testing"

	"domainscript/ast"
)

// sstmt renderiza um statement como s-expression para asserções de estrutura.
func sstmt(s ast.Stmt) string {
	switch n := s.(type) {
	case *ast.Block:
		out := "(block"
		for _, st := range n.Stmts {
			out += " " + sstmt(st)
		}
		return out + ")"
	case *ast.ExprStmt:
		return sexpr(n.X)
	case *ast.AssignStmt:
		return "(= " + sexpr(n.Target) + " " + sexpr(n.Value) + ")"
	case *ast.MatchStmt:
		out := "(matchS " + sexpr(n.Subject)
		for _, a := range n.Arms {
			out += " (arm " + matchArmHead(a.Patterns, a.Guard) + " " + sstmt(a.Body) + ")"
		}
		return out + ")"
	default:
		return "?stmt"
	}
}

func parseStmtOK(t *testing.T, src string) ast.Stmt {
	t.Helper()
	p, bag := mk(src)
	s := p.parseStmt()
	if bag.Len() != 0 {
		t.Fatalf("parseStmt(%q) gerou diagnósticos: %s", src, bag.Render())
	}
	if !p.atEnd() {
		t.Fatalf("parseStmt(%q) não consumiu tudo; parou em %v", src, p.cur().Kind)
	}
	return s
}

func TestMatchExpression(t *testing.T) {
	got := sexpr(parseExprOK(t, `match t.status { A => 1 B => 2 }`))
	want := "(matchE (. t status) (arm [A] 1) (arm [B] 2))"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestMatchMultiPatternAndWildcard(t *testing.T) {
	got := sexpr(parseExprOK(t, `match m { "CC", "CREDIT" => x _ => y }`))
	want := `(matchE m (arm ["CC","CREDIT"] x) (arm [_] y))`
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestMatchGuard(t *testing.T) {
	got := sexpr(parseExprOK(t, `match a { amount when amount >= b => c _ => d }`))
	want := "(matchE a (arm [amount]when(>= amount b) c) (arm [_] d))"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestMatchStatement(t *testing.T) {
	got := sstmt(parseStmtOK(t, `match s { A => foo(x) _ => Nop }`))
	want := "(matchS s (arm [A] (call foo x)) (arm [_] Nop))"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestMatchStatementBlockBody(t *testing.T) {
	got := sstmt(parseStmtOK(t, `match s { A => { x = 1 } }`))
	want := "(matchS s (arm [A] (block (= x 1))))"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

// match como expressão no lado direito de uma atribuição.
func TestMatchAsAssignValue(t *testing.T) {
	got := sstmt(parseStmtOK(t, `label = match e.type { A => "x" B => "y" }`))
	want := `(= label (matchE (. e type) (arm [A] "x") (arm [B] "y")))`
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

// Recovery: braço sem '=>' reporta erro e o parser segue (não trava).
func TestMatchArmMissingArrowRecovers(t *testing.T) {
	p, bag := mk(`match s { A 1 }`)
	_ = p.parseStmt()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico para '=>' ausente")
	}
	if !p.atEnd() {
		t.Errorf("parser não consumiu tudo após recovery; parou em %v", p.cur().Kind)
	}
}
