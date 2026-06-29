package ast

import (
	"testing"

	"domainscript/token"
)

// Garante (em tempo de compilação e como documentação) que cada nó de erro
// satisfaz a interface correta — e só ela.
func TestErrorNodesSatisfyInterfaces(t *testing.T) {
	var (
		_ Decl = (*ErrorDecl)(nil)
		_ Stmt = (*ErrorStmt)(nil)
		_ Expr = (*ErrorExpr)(nil)
	)
}

func TestSpanEPos(t *testing.T) {
	sp := Span{
		Start: token.Pos{Line: 1, Col: 1},
		End:   token.Pos{Line: 2, Col: 10},
	}
	nodes := []Node{
		NewErrorDecl(sp),
		NewErrorStmt(sp),
		NewErrorExpr(sp),
	}
	for _, n := range nodes {
		if n.Span() != sp {
			t.Errorf("%T.Span() = %+v, quero %+v", n, n.Span(), sp)
		}
		if n.Pos() != sp.Start {
			t.Errorf("%T.Pos() = %+v, quero %+v", n, n.Pos(), sp.Start)
		}
	}
}
