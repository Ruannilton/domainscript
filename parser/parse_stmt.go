package parser

import (
	"domainscript/ast"
	"domainscript/token"
)

// parseStmt parseia um statement. Nesta fase reconhece match, blocos, atribuições
// e expressões-statement; os construtos de controle de fluxo (ensure, for, log,
// emit, return, break/continue) são adicionados na tarefa 4A.5.
func (p *parser) parseStmt() ast.Stmt {
	switch {
	case p.at(token.MATCH):
		return p.parseMatchStmt()
	case p.at(token.ENSURE):
		return p.parseEnsure()
	case p.at(token.RETURN):
		return p.parseReturn()
	case p.at(token.FOR):
		return p.parseFor()
	case p.at(token.LOG):
		return p.parseLog()
	case p.at(token.EMIT):
		return p.parseEmit()
	case p.at(token.BREAK), p.at(token.CONTINUE):
		return p.parseLoopControl()
	case p.at(token.LBRACE):
		return p.parseBlock()
	default:
		return p.parseSimpleStmt()
	}
}

// parseEnsure parseia "ensure Cond else Action" (§3.1).
func (p *parser) parseEnsure() ast.Stmt {
	start := p.cur().Pos
	p.expect(token.ENSURE)
	cond := p.parseExpr()
	var els ast.Stmt
	if p.expect(token.ELSE) {
		els = p.parseEnsureAction()
	}
	return ast.NewEnsureStmt(cond, els, p.spanFrom(start))
}

// parseEnsureAction parseia a ação de um else: um controle de laço, ou uma
// expressão (nome de Error ou Nop).
func (p *parser) parseEnsureAction() ast.Stmt {
	if p.at(token.BREAK) || p.at(token.CONTINUE) {
		return p.parseLoopControl()
	}
	start := p.cur().Pos
	return ast.NewExprStmt(p.parseExpr(), p.spanFrom(start))
}

// parseLoopControl parseia "break", "break all" ou "continue".
func (p *parser) parseLoopControl() ast.Stmt {
	start := p.cur().Pos
	if p.at(token.CONTINUE) {
		p.advance()
		return ast.NewContinueStmt(p.spanFrom(start))
	}
	p.expect(token.BREAK)
	all := false
	if p.at(token.IDENT) && p.cur().Lit == "all" {
		p.advance()
		all = true
	}
	return ast.NewBreakStmt(all, p.spanFrom(start))
}

// parseReturn parseia "return [Expr]". Sem valor quando seguido de '}' ou EOF.
func (p *parser) parseReturn() ast.Stmt {
	start := p.cur().Pos
	p.expect(token.RETURN)
	var val ast.Expr
	if !p.at(token.RBRACE) && !p.atEnd() {
		val = p.parseExpr()
	}
	return ast.NewReturnStmt(val, p.spanFrom(start))
}

// parseFor parseia "for Var in Iter Body" (§3.3).
func (p *parser) parseFor() ast.Stmt {
	start := p.cur().Pos
	p.expect(token.FOR)
	v := p.parseIdentName()
	p.expect(token.IN)
	iter := p.parseExpr()
	body := p.parseBlock()
	return ast.NewForStmt(v, iter, body, p.spanFrom(start))
}

// parseLog parseia "log Level Message { Fields }" (§3.4); nível e bloco de
// campos são opcionais.
func (p *parser) parseLog() ast.Stmt {
	start := p.cur().Pos
	p.expect(token.LOG)
	level := ""
	if p.at(token.IDENT) {
		level = p.advance().Lit
	}
	var msg ast.Expr
	if p.at(token.STRING) {
		msg = p.parseExpr()
	}
	var fields []ast.LogField
	if p.at(token.LBRACE) {
		fields = p.parseLogFields()
	}
	return ast.NewLogStmt(level, msg, fields, p.spanFrom(start))
}

func (p *parser) parseLogFields() []ast.LogField {
	p.expect(token.LBRACE)
	var fields []ast.LogField
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		name := p.parseIdentName()
		p.expect(token.ASSIGN)
		val := p.parseExpr()
		fields = append(fields, ast.LogField{Name: name, Value: val})
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return fields
}

// parseEmit parseia "emit Call" — a publicação de um evento.
func (p *parser) parseEmit() ast.Stmt {
	start := p.cur().Pos
	p.expect(token.EMIT)
	return ast.NewEmitStmt(p.parseExpr(), p.spanFrom(start))
}

// parseSimpleStmt parseia uma expressão como statement; se vier seguida de '='
// (e não de '=>'), é uma atribuição.
func (p *parser) parseSimpleStmt() ast.Stmt {
	start := p.cur().Pos
	x := p.parseExpr()
	if p.at(token.ASSIGN) && !p.atFatArrow() {
		p.advance() // '='
		val := p.parseExpr()
		return ast.NewAssignStmt(x, val, p.spanFrom(start))
	}
	return ast.NewExprStmt(x, p.spanFrom(start))
}

// parseBlock parseia "{ STMT... }". O laço para em '}' ou EOF e garante progresso
// para nunca travar (REQ-3.6, NFR-2).
func (p *parser) parseBlock() *ast.Block {
	start := p.cur().Pos
	p.expect(token.LBRACE)
	var stmts []ast.Stmt
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		stmts = append(stmts, p.parseStmt())
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewBlock(stmts, p.spanFrom(start))
}

// parseMatchStmt parseia "match SUBJECT { ARM... }" como statement; cada braço
// executa uma ação (statement ou bloco) (REQ-2.4).
func (p *parser) parseMatchStmt() ast.Stmt {
	start := p.cur().Pos
	p.expect(token.MATCH)
	subject := p.parseExpr()
	p.expect(token.LBRACE)
	var arms []ast.MatchStmtArm
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		pats, guard := p.parseMatchArmHead()
		body := p.parseStmt()
		arms = append(arms, ast.MatchStmtArm{Patterns: pats, Guard: guard, Body: body})
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewMatchStmt(subject, arms, p.spanFrom(start))
}
