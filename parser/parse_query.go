package parser

import (
	"domainscript/ast"
	"domainscript/token"
)

// Operações de domínio prefixas reconhecidas no início de uma primária.
var queryOps = map[string]bool{
	"load": true, "list": true, "count": true,
	"store": true, "call": true, "delete": true,
}

// Cláusulas estilo SQL que seguem uma operação de domínio.
var clauseKeywords = map[string]bool{
	"join": true, "where": true, "orderBy": true,
	"skip": true, "take": true, "as": true,
}

var directions = map[string]bool{
	"ascending": true, "descending": true, "asc": true, "desc": true,
}

func isQueryOp(lit string) bool   { return queryOps[lit] }
func isClauseKw(lit string) bool  { return clauseKeywords[lit] }
func isDirection(lit string) bool { return directions[lit] }

// parseQueryOp parseia uma operação de domínio prefixa: o operador, a
// expressão-alvo (entidade/fonte), um binding opcional (list Ticket t) e as
// cláusulas SQL-like (REQ-2.4, §6.3 do spec).
func (p *parser) parseQueryOp() ast.Expr {
	start := p.cur().Pos
	op := p.advance().Lit
	target := p.parsePostfix()
	binding := ""
	// O binding opcional (list Ticket t) só vale na MESMA linha que o alvo. Sem a
	// guarda de linha, após `order = load Bar(id)` o parser engoliria o `x` da
	// linha seguinte (de `x = id`) como binding, deixando o `=` órfão — ISSUE-11:
	// o binding não pode cruzar a fronteira de linha e roubar o identificador do
	// statement seguinte.
	if p.at(token.IDENT) && !isClauseKw(p.cur().Lit) && p.sameLineAsPrev() {
		binding = p.advance().Lit
	}
	clauses := p.parseQueryClauses()
	return ast.NewQueryExpr(op, target, binding, clauses, p.spanFrom(start))
}

// parseQueryClauses acumula cláusulas enquanto o token corrente iniciar uma.
func (p *parser) parseQueryClauses() []ast.QueryClause {
	var clauses []ast.QueryClause
	for {
		before := p.pos
		switch {
		case p.at(token.ON):
			p.advance()
			clauses = append(clauses, ast.QueryClause{Kw: "on", Expr: p.parseExpr()})
		case p.at(token.IDENT) && isClauseKw(p.cur().Lit):
			clauses = append(clauses, p.parseOneClause(p.advance().Lit))
		default:
			return clauses
		}
		p.ensureProgress(before)
	}
}

func (p *parser) parseOneClause(kw string) ast.QueryClause {
	switch kw {
	case "join":
		src := p.parsePostfix()
		alias := ""
		// Mesma guarda de linha do binding em parseQueryOp (ISSUE-11): o alias
		// opcional do join só vale na MESMA linha que a fonte, senão o parser
		// engoliria o identificador do statement seguinte como alias.
		if p.at(token.IDENT) && !isClauseKw(p.cur().Lit) && p.sameLineAsPrev() {
			alias = p.advance().Lit
		}
		return ast.QueryClause{Kw: "join", Expr: src, Extra: alias}
	case "where":
		return ast.QueryClause{Kw: "where", Expr: p.parseExpr()}
	case "orderBy":
		e := p.parseExpr()
		dir := ""
		if p.at(token.IDENT) && isDirection(p.cur().Lit) {
			dir = p.advance().Lit
		}
		return ast.QueryClause{Kw: "orderBy", Expr: e, Extra: dir}
	case "skip":
		return ast.QueryClause{Kw: "skip", Expr: p.parseExpr()}
	case "take":
		return ast.QueryClause{Kw: "take", Expr: p.parseExpr()}
	case "as":
		return ast.QueryClause{Kw: "as", Extra: p.parseIdentName()}
	default:
		return ast.QueryClause{Kw: kw}
	}
}

// parseListLiteral parseia "[ e, e, ... ]".
func (p *parser) parseListLiteral() ast.Expr {
	start := p.cur().Pos
	p.expect(token.LBRACK)
	var elems []ast.Expr
	for !p.at(token.RBRACK) && !p.atEnd() {
		before := p.pos
		elems = append(elems, p.parseExpr())
		if !p.accept(token.COMMA) {
			break
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACK)
	return ast.NewListExpr(elems, p.spanFrom(start))
}
