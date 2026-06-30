package sema

import (
	"strings"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/symbols"
	"domainscript/token"
)

// armInfo resume um braço de match para a checagem de exaustividade: os padrões e
// se o braço tem guard (when).
type armInfo struct {
	patterns []ast.Expr
	hasGuard bool
}

// checkMatchExhaustiveness implementa REQ-5.5 (§3.2): match sobre conjunto
// fechado (enum) deve ser exaustivo, com `_` proibido; match com guards (when)
// exige `_`. Percorre todos os match-statement e match-expressão dos corpos da
// declaração.
func (c *Checker) checkMatchExhaustiveness(module string, d ast.Decl) {
	for _, b := range astutil.DeclBlocks(d) {
		astutil.ForEachStmt(b, func(s ast.Stmt) {
			if m, ok := s.(*ast.MatchStmt); ok {
				c.checkMatch(module, m.Pos(), stmtArms(m.Arms))
			}
		})
		astutil.ForEachExprInBlock(b, func(e ast.Expr) {
			if m, ok := e.(*ast.MatchExpr); ok {
				c.checkMatch(module, m.Pos(), exprArms(m.Arms))
			}
		})
	}
}

func stmtArms(arms []ast.MatchStmtArm) []armInfo {
	out := make([]armInfo, len(arms))
	for i, a := range arms {
		out[i] = armInfo{patterns: a.Patterns, hasGuard: a.Guard != nil}
	}
	return out
}

func exprArms(arms []ast.MatchExprArm) []armInfo {
	out := make([]armInfo, len(arms))
	for i, a := range arms {
		out[i] = armInfo{patterns: a.Patterns, hasGuard: a.Guard != nil}
	}
	return out
}

// checkMatch aplica as regras de exaustividade/wildcard a um match já reduzido a
// armInfo. Quando há guards, o match opera por condição: exige `_` e dispensa a
// análise de enum. Sem guards, tenta identificar o enum pelos padrões
// qualificados (Enum.Membro); se identificado, `_` é proibido e a cobertura deve
// ser total.
func (c *Checker) checkMatch(module string, pos token.Pos, arms []armInfo) {
	hasGuard := false
	hasWild := false
	covered := map[string]bool{}
	enumName := ""
	for _, a := range arms {
		if a.hasGuard {
			hasGuard = true
		}
		for _, p := range a.patterns {
			if astutil.IsIdent(p, "_") {
				hasWild = true
				continue
			}
			if en, mem, ok := enumMemberRef(p); ok {
				enumName = en
				covered[mem] = true
			}
		}
	}

	if hasGuard {
		if !hasWild {
			c.bag.Errorf(pos, "match com guards (when) exige um braço wildcard `_`")
		}
		return
	}

	sym, ok := c.tab.Lookup(module, enumName)
	if !ok || sym.Kind != symbols.KindEnum {
		return // não é um match sobre enum identificável; exaustividade não verificável aqui
	}
	if hasWild {
		c.bag.Errorf(pos, "wildcard `_` é proibido em match sobre o enum %q (deve ser exaustivo)", enumName)
		return
	}
	ed, ok := sym.Decl.(*ast.EnumDecl)
	if !ok {
		return
	}
	var missing []string
	for _, m := range ed.Members {
		if !covered[m.Name] {
			missing = append(missing, m.Name)
		}
	}
	if len(missing) > 0 {
		c.bag.Errorf(pos, "match não-exaustivo sobre o enum %q: faltam os casos %s",
			enumName, strings.Join(missing, ", "))
	}
}

// checkNop implementa REQ-5.6 (§3.1): `Nop` (ação sem efeito) é proibido em
// Handle e UseCase — só é aceito em Policy/Worker e dentro de for. Sinaliza
// qualquer uso de Nop no corpo, em qualquer posição de ação.
func (c *Checker) checkNop(b *ast.Block, ctx string) {
	astutil.ForEachExprInBlock(b, func(e ast.Expr) {
		if astutil.IsIdent(e, "Nop") {
			c.bag.Errorf(e.Pos(), "`Nop` é proibido em %s: toda ação deve ter efeito (§3.1)", ctx)
		}
	})
}

// checkLoopControlDecl implementa REQ-5.7 (§3.3): break, break all e continue só
// valem dentro de um for. Percorre cada corpo da declaração rastreando a
// profundidade de laço.
func (c *Checker) checkLoopControlDecl(d ast.Decl) {
	for _, b := range astutil.DeclBlocks(d) {
		c.checkLoopControl(b, false)
	}
}

// checkLoopControl percorre s propagando se o cursor está dentro de um for. Um
// controle de laço encontrado com inFor falso é reportado.
func (c *Checker) checkLoopControl(s ast.Stmt, inFor bool) {
	switch n := s.(type) {
	case *ast.Block:
		for _, st := range n.Stmts {
			c.checkLoopControl(st, inFor)
		}
	case *ast.ForStmt:
		c.checkLoopControl(n.Body, true)
	case *ast.EnsureStmt:
		c.checkLoopControl(n.Else, inFor)
	case *ast.MatchStmt:
		for _, arm := range n.Arms {
			c.checkLoopControl(arm.Body, inFor)
		}
	case *ast.BreakStmt:
		if !inFor {
			what := "break"
			if n.All {
				what = "break all"
			}
			c.bag.Errorf(n.Pos(), "`%s` fora de um for não é permitido (§3.3)", what)
		}
	case *ast.ContinueStmt:
		if !inFor {
			c.bag.Errorf(n.Pos(), "`continue` fora de um for não é permitido (§3.3)")
		}
	}
}

// enumMemberRef reconhece um padrão "Enum.Membro" e devolve (Enum, Membro, true).
func enumMemberRef(p ast.Expr) (enum, member string, ok bool) {
	m, isMember := p.(*ast.MemberExpr)
	if !isMember {
		return "", "", false
	}
	id, isIdent := m.X.(*ast.Ident)
	if !isIdent {
		return "", "", false
	}
	return id.Name, m.Name, true
}
