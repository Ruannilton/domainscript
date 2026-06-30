package astutil

import "domainscript/ast"

// ForEachStmt visita s e todos os statements aninhados (profundidade primeiro),
// descendo em blocos, corpos de for, ações de ensure e braços de match-statement.
func ForEachStmt(s ast.Stmt, fn func(ast.Stmt)) {
	if s == nil {
		return
	}
	fn(s)
	switch n := s.(type) {
	case *ast.Block:
		for _, st := range n.Stmts {
			ForEachStmt(st, fn)
		}
	case *ast.ForStmt:
		ForEachStmt(n.Body, fn)
	case *ast.EnsureStmt:
		ForEachStmt(n.Else, fn)
	case *ast.MatchStmt:
		for _, arm := range n.Arms {
			ForEachStmt(arm.Body, fn)
		}
	}
}

// ForEachExpr visita e e todas as suas subexpressões (profundidade primeiro).
func ForEachExpr(e ast.Expr, fn func(ast.Expr)) {
	if e == nil {
		return
	}
	fn(e)
	switch n := e.(type) {
	case *ast.BinaryExpr:
		ForEachExpr(n.Left, fn)
		ForEachExpr(n.Right, fn)
	case *ast.UnaryExpr:
		ForEachExpr(n.X, fn)
	case *ast.MemberExpr:
		ForEachExpr(n.X, fn)
	case *ast.CallExpr:
		ForEachExpr(n.Fn, fn)
		for _, a := range n.Args {
			ForEachExpr(a.Value, fn)
		}
	case *ast.IndexExpr:
		ForEachExpr(n.X, fn)
		ForEachExpr(n.Index, fn)
	case *ast.RangeExpr:
		ForEachExpr(n.Low, fn)
		ForEachExpr(n.High, fn)
	case *ast.LambdaExpr:
		ForEachExpr(n.Body, fn)
	case *ast.ListExpr:
		for _, el := range n.Elems {
			ForEachExpr(el, fn)
		}
	case *ast.QueryExpr:
		ForEachExpr(n.Target, fn)
		for _, cl := range n.Clauses {
			ForEachExpr(cl.Expr, fn)
		}
	case *ast.MatchExpr:
		ForEachExpr(n.Subject, fn)
		for _, arm := range n.Arms {
			for _, p := range arm.Patterns {
				ForEachExpr(p, fn)
			}
			ForEachExpr(arm.Guard, fn)
			ForEachExpr(arm.Body, fn)
		}
	}
}

// StmtExprs devolve as expressões diretamente contidas por um único statement (não
// desce em statements aninhados — quem faz isso é ForEachStmt).
func StmtExprs(s ast.Stmt) []ast.Expr {
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

// ForEachExprInBlock visita toda expressão que aparece em qualquer ponto de b,
// incluindo as de statements aninhados e suas subexpressões.
func ForEachExprInBlock(b *ast.Block, fn func(ast.Expr)) {
	if b == nil {
		return
	}
	ForEachStmt(b, func(s ast.Stmt) {
		for _, e := range StmtExprs(s) {
			ForEachExpr(e, fn)
		}
	})
}

// DeclBlocks devolve todos os blocos de execução de uma declaração (corpos de
// Handle/Apply, execute, Valid, coerce, steps de Saga, ...). É a base das regras
// de fluxo e da resolução de corpos, que percorrem o corpo de cada construto.
func DeclBlocks(d ast.Decl) []*ast.Block {
	var out []*ast.Block
	add := func(b *ast.Block) {
		if b != nil {
			out = append(out, b)
		}
	}
	switch n := d.(type) {
	case *ast.ValueObjectDecl:
		add(n.Valid)
		for _, op := range n.Operators {
			add(op.Body)
		}
	case *ast.EnumDecl:
		if n.Coerce != nil {
			add(n.Coerce.Body)
		}
	case *ast.AggregateDecl:
		for _, h := range n.Handlers {
			add(h.Body)
		}
		for _, a := range n.Appliers {
			add(a.Body)
		}
	case *ast.UseCaseDecl:
		add(n.Execute)
	case *ast.QueryDecl:
		add(n.Body)
	case *ast.PolicyDecl:
		add(n.Execute)
	case *ast.WorkerDecl:
		add(n.Source)
		add(n.Execute)
	case *ast.SagaDecl:
		for _, s := range n.Steps {
			add(s.Up)
			add(s.Down)
			add(s.OnInfraError)
		}
	}
	return out
}

// IsIdent reporta se e é um identificador de nome name.
func IsIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// HeadName devolve o nome da "cabeça" de uma referência de domínio: o callee de
// uma construção (Withdraw(...), DepositPerformed(...)) ou um identificador nu
// (alvo de mock). Devolve "" para qualquer outra forma (acesso a membro, literal,
// método). Usado pelas regras que ligam expressões a símbolos.
func HeadName(e ast.Expr) string {
	switch n := e.(type) {
	case *ast.CallExpr:
		if id, ok := n.Fn.(*ast.Ident); ok {
			return id.Name
		}
	case *ast.Ident:
		return n.Name
	}
	return ""
}

// StateField devolve o nome do campo se e é o acesso "state.<campo>", senão "".
func StateField(e ast.Expr) string {
	m, ok := e.(*ast.MemberExpr)
	if !ok {
		return ""
	}
	if IsIdent(m.X, "state") {
		return m.Name
	}
	return ""
}
