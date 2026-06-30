package sema

import (
	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// rules_compat.go é a checagem de compatibilidade de tipos (REQ-13, §design
// type-checking 3.6). Em cada uso onde um tipo é esperado — atribuição (a = b),
// argumento de construção/chamada, operandos de operador e valor de return —
// compara o tipo esperado com o encontrado pela relação types.Assignable e reporta
// o uso incompatível com a mensagem acionável esperado-vs-encontrado (REQ-13.2).
//
// A regra é conservadora por design, como a de acesso a membro (rules_typecheck):
// só dispara entre dois tipos de domínio *distintos* (VO/Enum/Shape/coleção).
//
// Coordenação com a Regra de Ouro do Write Side (REQ-5.1 / REQ-13.3): quando um dos
// lados é um primitivo, a regra silencia. Um primitivo onde um tipo de domínio é
// estruturalmente esperado é exatamente o que a REQ-5.1 já reporta (campo primitivo
// no Write Side); reportá-lo de novo aqui duplicaria o diagnóstico da mesma causa.
// Fora do Write Side, um primitivo é literal/coerção legítima ou está além do
// alcance estático deste estágio. Assim a REQ-5.1 continua dona dos primitivos e
// esta regra apanha a classe que nomes sozinhos não pegam: trocar um tipo de
// domínio por outro.

// checkTypeCompat roda a checagem de compatibilidade sobre todas as unidades,
// usando o Model compartilhado.
func (c *Checker) checkTypeCompat(m *types.Model) {
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			c.checkDeclCompat(u.Module, m, d)
		}
	}
}

// checkDeclCompat monta o escopo de tipos de cada corpo do construto (os mesmos
// receptores de §3.2 da Fase D) e checa a compatibilidade dos usos dentro dele,
// passando o tipo de retorno esperado quando o construto tem um.
func (c *Checker) checkDeclCompat(module string, m *types.Model, d ast.Decl) {
	switch n := d.(type) {
	case *ast.AggregateDecl:
		selfT := c.typeOfName(module, m, n.Name)
		for _, h := range n.Handlers {
			sc := types.MapScope{}
			seed(sc, "self", selfT)
			seed(sc, "state", selfT)
			seedParams(sc, m, module, h.Params)
			c.checkCompatInBlock(module, m, sc, h.Body, nil)
		}
		for _, a := range n.Appliers {
			sc := types.MapScope{}
			seed(sc, "state", selfT)
			seed(sc, "event", c.typeOfName(module, m, a.Event))
			c.checkCompatInBlock(module, m, sc, a.Body, nil)
		}
		for _, rule := range n.Access {
			sc := types.MapScope{}
			seed(sc, "self", selfT)
			c.checkCompatInExpr(module, m, sc, rule.Condition)
		}

	case *ast.ValueObjectDecl:
		fields := m.Members(c.typeOfName(module, m, n.Name))
		valid := types.MapScope{}
		seedAll(valid, fields)
		c.checkCompatInBlock(module, m, valid, n.Valid, nil)
		for _, op := range n.Operators {
			sc := types.MapScope{}
			seedAll(sc, fields)
			seedParams(sc, m, module, op.Params)
			ret := m.TypeOfRef(module, op.Return)
			c.checkCompatInBlock(module, m, sc, op.Body, ret)
		}

	case *ast.QueryDecl:
		sc := types.MapScope{}
		seedParams(sc, m, module, n.Params)
		ret := m.TypeOfRef(module, n.Return)
		c.checkCompatInBlock(module, m, sc, n.Body, ret)

	case *ast.UseCaseDecl:
		sc := types.MapScope{}
		seed(sc, "cmd", c.typeOfName(module, m, n.Handles))
		c.checkCompatInBlock(module, m, sc, n.Execute, nil)

	case *ast.PolicyDecl:
		sc := types.MapScope{}
		seed(sc, "event", c.typeOfName(module, m, n.On))
		c.checkCompatInBlock(module, m, sc, n.Execute, nil)
	}
}

// checkCompatInBlock percorre o bloco uma vez: trata atribuição e return no nível
// do statement (que precisam emparelhar alvo e valor), e operadores e argumentos de
// construção no nível da expressão. ret é o tipo de retorno esperado do construto,
// ou nil quando ele não tem um.
func (c *Checker) checkCompatInBlock(module string, m *types.Model, sc types.Scope, b *ast.Block, ret types.Type) {
	if b == nil {
		return
	}
	astutil.ForEachStmt(b, func(s ast.Stmt) {
		switch n := s.(type) {
		case *ast.AssignStmt:
			c.compatAssign(module, m, sc, n)
		case *ast.ReturnStmt:
			if ret != nil && n.Value != nil {
				c.reportIncompat(ret, m.Infer(module, n.Value, sc), n.Value.Pos())
			}
		}
		for _, e := range astutil.StmtExprs(s) {
			astutil.ForEachExpr(e, func(sub ast.Expr) {
				c.compatExpr(module, m, sc, sub)
			})
		}
	})
}

// checkCompatInExpr checa os usos com tipo esperado numa expressão solta (ex.: a
// condição de uma regra de access, que é expressão e não bloco).
func (c *Checker) checkCompatInExpr(module string, m *types.Model, sc types.Scope, e ast.Expr) {
	astutil.ForEachExpr(e, func(sub ast.Expr) {
		c.compatExpr(module, m, sc, sub)
	})
}

// compatAssign checa que o valor atribuído é compatível com o tipo do alvo.
func (c *Checker) compatAssign(module string, m *types.Model, sc types.Scope, n *ast.AssignStmt) {
	dst := m.Infer(module, n.Target, sc)
	src := m.Infer(module, n.Value, sc)
	c.reportIncompat(dst, src, n.Value.Pos())
}

// compatExpr checa os usos com tipo esperado dentro de uma expressão: operandos de
// operador binário e argumentos de uma construção/chamada.
func (c *Checker) compatExpr(module string, m *types.Model, sc types.Scope, e ast.Expr) {
	switch n := e.(type) {
	case *ast.BinaryExpr:
		if !isTypedBinaryOp(n.Op) {
			return
		}
		l := m.Infer(module, n.Left, sc)
		r := m.Infer(module, n.Right, sc)
		// Operandos devem ser mutuamente compatíveis; reporta no operando direito.
		if reportable(l, r) && reportable(r, l) {
			c.bag.Errorf(n.Right.Pos(),
				"operandos incompatíveis: %s e %s", l.String(), r.String())
		}
	case *ast.CallExpr:
		c.compatArgs(module, m, sc, n)
	}
}

// compatArgs checa cada argumento de uma construção/chamada contra o tipo do
// parâmetro correspondente. Só se aplica quando Fn nomeia um símbolo construível
// (não um método X.f(...), cuja assinatura este estágio não modela).
func (c *Checker) compatArgs(module string, m *types.Model, sc types.Scope, call *ast.CallExpr) {
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return
	}
	// Um local que sombreia o nome não é uma construção (espelha inferCall).
	if sc != nil {
		if _, shadowed := sc.LookupType(id.Name); shadowed {
			return
		}
	}
	sym := c.symbolOfName(module, id.Name)
	if sym == nil {
		return
	}
	params := m.CtorParams(sym)
	if params == nil {
		return
	}
	for i, arg := range call.Args {
		expected := paramType(params, arg, i)
		if expected == nil {
			continue
		}
		c.reportIncompat(expected, m.Infer(module, arg.Value, sc), arg.Value.Pos())
	}
}

// paramType escolhe o parâmetro que corresponde a um argumento: por nome quando o
// argumento é nomeado, por posição quando é posicional. nil se não há correspondente.
func paramType(params []types.Field, arg ast.Arg, i int) types.Type {
	if arg.Name != "" {
		for _, p := range params {
			if p.Name == arg.Name {
				return p.Type
			}
		}
		return nil
	}
	if i < len(params) {
		return params[i].Type
	}
	return nil
}

// reportIncompat emite o erro esperado-vs-encontrado se src não é atribuível a dst
// e o par é reportável (ver reportable).
func (c *Checker) reportIncompat(dst, src types.Type, pos token.Pos) {
	if reportable(dst, src) {
		c.bag.Errorf(pos,
			"tipo incompatível: esperado %s, encontrado %s", dst.String(), src.String())
	}
}

// reportable decide se uma incompatibilidade dst←src deve ser reportada. Silencia
// quando qualquer lado é tipo de erro (anti-cascata, REQ-13/NFR-9) ou primitivo
// (coordenação com a REQ-5.1, ver doc do arquivo). Caso contrário, reporta sse src
// não é atribuível a dst.
func reportable(dst, src types.Type) bool {
	if types.IsError(dst) || types.IsError(src) {
		return false
	}
	if types.IsPrimitive(dst) || types.IsPrimitive(src) {
		return false
	}
	return !types.Assignable(dst, src)
}

// symbolOfName procura o símbolo de nome name no módulo e, em fallback, globalmente
// (tipo público de outro módulo, REQ-7.3). nil se ausente/não resolvido.
func (c *Checker) symbolOfName(module, name string) *symbols.Symbol {
	if name == "" {
		return nil
	}
	if sym, ok := c.tab.Lookup(module, name); ok {
		return sym
	}
	if sym, ok := c.tab.Find(name); ok {
		return sym
	}
	return nil
}

// isTypedBinaryOp reporta se o operador exige operandos de tipos compatíveis:
// aritméticos e relacionais/igualdade. Operadores lógicos (and/or) operam sobre
// booleanos e não entram nesta checagem nominal de domínio.
func isTypedBinaryOp(op token.Kind) bool {
	switch op {
	case token.PLUS, token.MINUS, token.STAR, token.SLASH,
		token.EQ, token.NEQ, token.LT, token.GT, token.LE, token.GE:
		return true
	}
	return false
}
