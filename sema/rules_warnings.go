package sema

import (
	"domainscript/ast"
	"domainscript/symbols"
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

// checkCacheHighCardinality implementa REQ-5.20 (⚠️, §15): cache sobre uma Query
// que retorna uma listagem (List) é cache de alta cardinalidade, frequentemente
// ineficaz. Avisa quando há bloco cache e o retorno é List.
func (c *Checker) checkCacheHighCardinality(q *ast.QueryDecl) {
	if len(q.Cache) == 0 || q.Return == nil || q.Return.Name != "List" {
		return
	}
	c.bag.Warningf(q.Pos(),
		"Query %q tem cache sobre uma listagem (List): alta cardinalidade pode tornar o cache ineficaz (§15)",
		q.Name)
}

// checkHandleErrorCoverage implementa REQ-5.22 (⚠️, §22.7): para um Aggregate sob
// teste, cada Handle com caminho de erro de negócio (um `ensure ... else <Error>`)
// deveria ter um cenário que exercita esse erro (`then error ...`). Avisa por
// Handle cujo erro não é testado. Aggregates sem nenhum Test não são considerados
// aqui — isso é ausência total de teste, não falta de cobertura de ramo.
func (c *Checker) checkHandleErrorCoverage() {
	tested := c.testedErrorHandles()
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			agg, ok := d.(*ast.AggregateDecl)
			if !ok {
				continue
			}
			key := u.Module + "\x00" + agg.Name
			covered, underTest := tested[key]
			if !underTest {
				continue
			}
			for _, h := range agg.Handlers {
				if h == nil || h.Name == "" {
					continue
				}
				if c.handleRaisesError(u.Module, h) && !covered[h.Name] {
					c.bag.Warningf(h.Pos(),
						"Handle %q do Aggregate %q tem caminho de erro de negócio sem cenário de teste de erro (cobertura, §22.7)",
						h.Name, agg.Name)
				}
			}
		}
	}
}

// testedErrorHandles mapeia, por Aggregate (module\x00nome), o conjunto de Handles
// com um cenário `then error`. A presença da chave indica que o Aggregate tem ao
// menos um Test; o Handle vem da cabeça do `when` do cenário.
func (c *Checker) testedErrorHandles() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			t, ok := d.(*ast.TestDecl)
			if !ok {
				continue
			}
			key := u.Module + "\x00" + t.Name
			if out[key] == nil {
				out[key] = map[string]bool{}
			}
			for _, sc := range t.Scenarios {
				if sc == nil || sc.When == nil || sc.Then == nil || sc.Then.Error == "" {
					continue
				}
				if h := headName(sc.When.Action); h != "" {
					out[key][h] = true
				}
			}
		}
	}
	return out
}

// handleRaisesError reporta se o corpo de um Handle pode levantar um Error de
// negócio, i.e. tem um `ensure ... else <Error>` cuja ação é um Error declarado.
func (c *Checker) handleRaisesError(module string, h *ast.HandleDecl) bool {
	raises := false
	forEachStmt(h.Body, func(s ast.Stmt) {
		ens, ok := s.(*ast.EnsureStmt)
		if !ok {
			return
		}
		es, ok := ens.Else.(*ast.ExprStmt)
		if !ok {
			return
		}
		id, ok := es.X.(*ast.Ident)
		if !ok {
			return
		}
		if sym, ok := c.tab.Lookup(module, id.Name); ok && sym.Kind == symbols.KindError {
			raises = true
		}
	})
	return raises
}
