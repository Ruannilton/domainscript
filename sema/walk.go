package sema

import "domainscript/ast"

// walk.go reúne percursos genéricos da AST usados por várias regras semânticas.
// São deliberadamente totais sobre os nós existentes: adicionar um construto novo
// exige estendê-los aqui, num só lugar (NFR-5).

// forEachStmt visita s e todos os statements aninhados (profundidade primeiro),
// descendo em blocos, corpos de for, ações de ensure e braços de match-statement.
func forEachStmt(s ast.Stmt, fn func(ast.Stmt)) {
	if s == nil {
		return
	}
	fn(s)
	switch n := s.(type) {
	case *ast.Block:
		for _, st := range n.Stmts {
			forEachStmt(st, fn)
		}
	case *ast.ForStmt:
		forEachStmt(n.Body, fn)
	case *ast.EnsureStmt:
		forEachStmt(n.Else, fn)
	case *ast.MatchStmt:
		for _, arm := range n.Arms {
			forEachStmt(arm.Body, fn)
		}
	}
}

// forEachExpr visita e e todas as suas subexpressões (profundidade primeiro).
func forEachExpr(e ast.Expr, fn func(ast.Expr)) {
	if e == nil {
		return
	}
	fn(e)
	switch n := e.(type) {
	case *ast.BinaryExpr:
		forEachExpr(n.Left, fn)
		forEachExpr(n.Right, fn)
	case *ast.UnaryExpr:
		forEachExpr(n.X, fn)
	case *ast.MemberExpr:
		forEachExpr(n.X, fn)
	case *ast.CallExpr:
		forEachExpr(n.Fn, fn)
		for _, a := range n.Args {
			forEachExpr(a.Value, fn)
		}
	case *ast.IndexExpr:
		forEachExpr(n.X, fn)
		forEachExpr(n.Index, fn)
	case *ast.RangeExpr:
		forEachExpr(n.Low, fn)
		forEachExpr(n.High, fn)
	case *ast.LambdaExpr:
		forEachExpr(n.Body, fn)
	case *ast.ListExpr:
		for _, el := range n.Elems {
			forEachExpr(el, fn)
		}
	case *ast.QueryExpr:
		forEachExpr(n.Target, fn)
		for _, cl := range n.Clauses {
			forEachExpr(cl.Expr, fn)
		}
	case *ast.MatchExpr:
		forEachExpr(n.Subject, fn)
		for _, arm := range n.Arms {
			for _, p := range arm.Patterns {
				forEachExpr(p, fn)
			}
			forEachExpr(arm.Guard, fn)
			forEachExpr(arm.Body, fn)
		}
	}
}

// stmtExprs devolve as expressões diretamente contidas por um único statement (não
// desce em statements aninhados — quem faz isso é forEachStmt).
func stmtExprs(s ast.Stmt) []ast.Expr {
	switch n := s.(type) {
	case *ast.ExprStmt:
		return []ast.Expr{n.X}
	case *ast.AssignStmt:
		return []ast.Expr{n.Target, n.Value}
	case *ast.EnsureStmt:
		return []ast.Expr{n.Cond}
	case *ast.ReturnStmt:
		if n.Value != nil {
			return []ast.Expr{n.Value}
		}
	case *ast.ForStmt:
		return []ast.Expr{n.Iter}
	case *ast.EmitStmt:
		return []ast.Expr{n.Call}
	case *ast.MatchStmt:
		out := []ast.Expr{n.Subject}
		for _, arm := range n.Arms {
			out = append(out, arm.Patterns...)
			if arm.Guard != nil {
				out = append(out, arm.Guard)
			}
		}
		return out
	case *ast.LogStmt:
		var out []ast.Expr
		if n.Message != nil {
			out = append(out, n.Message)
		}
		for _, f := range n.Fields {
			out = append(out, f.Value)
		}
		return out
	}
	return nil
}

// forEachExprInBlock visita toda expressão que aparece em qualquer ponto de b,
// incluindo as de statements aninhados e suas subexpressões.
func forEachExprInBlock(b *ast.Block, fn func(ast.Expr)) {
	if b == nil {
		return
	}
	forEachStmt(b, func(s ast.Stmt) {
		for _, e := range stmtExprs(s) {
			forEachExpr(e, fn)
		}
	})
}

// isIdent reporta se e é um identificador de nome name.
func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// stateField devolve o nome do campo se e é o acesso "state.<campo>", senão "".
func stateField(e ast.Expr) string {
	m, ok := e.(*ast.MemberExpr)
	if !ok {
		return ""
	}
	if isIdent(m.X, "state") {
		return m.Name
	}
	return ""
}
