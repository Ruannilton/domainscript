package sema

import (
	"domainscript/ast"
	"domainscript/token"
)

// checkValueObjectAsEnum implementa REQ-5.19 (⚠️, §2.3): um ValueObject wrapper
// sobre string cuja validação é apenas uma disjunção de igualdades contra
// literais ("A" or "B" or ...) descreve um conjunto fechado — modelaria melhor
// como Enum. Avisa quando reconhece essa forma com pelo menos dois literais.
func (c *Checker) checkValueObjectAsEnum(vo *ast.ValueObjectDecl) {
	if vo.Base == nil || vo.Base.Name != "string" || len(vo.Fields) > 0 {
		return // só a forma wrapper sobre string pode virar Enum
	}
	if n := closedStringSetSize(vo.Valid); n >= 2 {
		c.bag.Warningf(vo.Pos(),
			"ValueObject %q valida contra um conjunto fechado de %d literais string; um Enum modelaria melhor (§2.3)",
			vo.Name, n)
	}
}

// closedStringSetSize devolve o número de comparações `x == "lit"` quando o bloco
// Valid é uma única expressão que é uma disjunção pura dessas comparações; 0 caso
// contrário (ex.: `ok`, chamadas de método, checagens de range).
func closedStringSetSize(valid *ast.Block) int {
	if valid == nil || len(valid.Stmts) != 1 {
		return 0
	}
	es, ok := valid.Stmts[0].(*ast.ExprStmt)
	if !ok {
		return 0
	}
	return countEqStringLeaves(es.X)
}

func countEqStringLeaves(e ast.Expr) int {
	b, ok := e.(*ast.BinaryExpr)
	if !ok {
		return 0
	}
	switch b.Op {
	case token.OR:
		l, r := countEqStringLeaves(b.Left), countEqStringLeaves(b.Right)
		if l == 0 || r == 0 {
			return 0 // qualquer ramo fora da forma desqualifica o todo
		}
		return l + r
	case token.EQ:
		if isStringLit(b.Left) || isStringLit(b.Right) {
			return 1
		}
	}
	return 0
}

func isStringLit(e ast.Expr) bool {
	lit, ok := e.(*ast.Literal)
	return ok && lit.Kind == token.STRING
}

// checkChannelOrderBy implementa REQ-5.16 (⚠️, §11): um canal de entrega por
// `queue` ou `stream` sem `orderBy` não garante ordem de mensagens. Avisa por
// canal sem a chave.
func (c *Checker) checkChannelOrderBy(topo *ast.TopologyDecl) {
	for _, ch := range topo.Channels {
		if ch == nil {
			continue
		}
		via := configIdent(ch.Entries, "via")
		if via != "queue" && via != "stream" {
			continue
		}
		if !hasConfigKey(ch.Entries, "orderBy") {
			c.bag.Warningf(ch.Pos(),
				"canal %s -> %s via %q não declara orderBy: a ordem das mensagens não é garantida (§11)",
				ch.From, ch.To, via)
		}
	}
}

// configIdent devolve o nome do identificador valor da chave key (ex.: via:
// queue → "queue"); "" se ausente ou não-identificador.
func configIdent(entries []ast.ConfigEntry, key string) string {
	for _, e := range entries {
		if e.Key == key {
			if id, ok := e.Value.(*ast.Ident); ok {
				return id.Name
			}
		}
	}
	return ""
}

func hasConfigKey(entries []ast.ConfigEntry, key string) bool {
	for _, e := range entries {
		if e.Key == key {
			return true
		}
	}
	return false
}
