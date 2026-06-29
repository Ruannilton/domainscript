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
	case p.at(token.LBRACE):
		return p.parseBlock()
	default:
		return p.parseSimpleStmt()
	}
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
