package parser

import (
	"domainscript/ast"
	"domainscript/token"
)

// parseExpr é o ponto de entrada da gramática de expressões. A cadeia de
// precedência (do menor para o maior) é: or < and < igualdade < relacional <
// aditivo < multiplicativo < unário < pós-fixo (REQ-2.5). Range e lambdas
// envolvem este ponto a partir da tarefa 4A.3.
func (p *parser) parseExpr() ast.Expr {
	return p.parseOr()
}

func (p *parser) parseOr() ast.Expr {
	return p.parseBinaryLevel(p.parseAnd, token.OR)
}

func (p *parser) parseAnd() ast.Expr {
	return p.parseBinaryLevel(p.parseEquality, token.AND)
}

func (p *parser) parseEquality() ast.Expr {
	return p.parseBinaryLevel(p.parseRelational, token.EQ, token.NEQ)
}

func (p *parser) parseRelational() ast.Expr {
	return p.parseBinaryLevel(p.parseAdditive, token.LT, token.GT, token.LE, token.GE)
}

func (p *parser) parseAdditive() ast.Expr {
	return p.parseBinaryLevel(p.parseMultiplicative, token.PLUS, token.MINUS)
}

func (p *parser) parseMultiplicative() ast.Expr {
	return p.parseBinaryLevel(p.parseUnary, token.STAR, token.SLASH)
}

// parseBinaryLevel parseia um nível de operadores binários esquerdo-associativos
// cujos operandos são produzidos por next.
func (p *parser) parseBinaryLevel(next func() ast.Expr, ops ...token.Kind) ast.Expr {
	left := next()
	for p.matchAny(ops...) {
		op := p.advance().Kind
		right := next()
		left = ast.NewBinaryExpr(op, left, right, p.spanFrom(left.Pos()))
	}
	return left
}

func (p *parser) parseUnary() ast.Expr {
	if p.at(token.MINUS) || p.at(token.NOT) {
		start := p.cur().Pos
		op := p.advance().Kind
		x := p.parseUnary()
		return ast.NewUnaryExpr(op, x, p.spanFrom(start))
	}
	return p.parsePostfix()
}

// parsePostfix encadeia acessos a membro, chamadas/construções e indexações
// sobre uma expressão primária (REQ-2.5).
func (p *parser) parsePostfix() ast.Expr {
	x := p.parsePrimary()
	for {
		start := x.Pos()
		switch {
		case p.at(token.DOT):
			p.advance()
			name := p.parseIdentName()
			x = ast.NewMemberExpr(x, name, p.spanFrom(start))
		case p.at(token.LPAREN):
			args := p.parseArgList()
			x = ast.NewCallExpr(x, args, p.spanFrom(start))
		case p.at(token.LBRACK):
			p.advance()
			idx := p.parseExpr()
			p.expect(token.RBRACK)
			x = ast.NewIndexExpr(x, idx, p.spanFrom(start))
		default:
			return x
		}
	}
}

func (p *parser) parsePrimary() ast.Expr {
	t := p.cur()
	switch {
	case t.Kind == token.IDENT:
		p.advance()
		return ast.NewIdent(t.Lit, p.spanFrom(t.Pos))
	case isLiteralKind(t.Kind):
		p.advance()
		return ast.NewLiteral(t.Kind, t.Lit, p.spanFrom(t.Pos))
	case t.Kind == token.LPAREN:
		p.advance()
		x := p.parseExpr()
		p.expect(token.RPAREN)
		return x
	case t.Kind == token.MATCH:
		return p.parseMatchExpr()
	default:
		// Não consome: deixa o token para o recovery do nível de cima (a
		// terminação dos laços é garantida por ensureProgress).
		p.errorf(t.Pos, "esperava uma expressão, encontrei %s", t.Kind)
		return ast.NewErrorExpr(p.spanFrom(t.Pos))
	}
}

// parseArgList parseia "( arg, arg, ... )" com argumentos posicionais ou
// nomeados (nome: valor).
func (p *parser) parseArgList() []ast.Arg {
	p.expect(token.LPAREN)
	var args []ast.Arg
	for !p.at(token.RPAREN) && !p.atEnd() {
		before := p.pos
		var name string
		if p.at(token.IDENT) && p.peek().Kind == token.COLON {
			name = p.advance().Lit
			p.advance() // ':'
		}
		val := p.parseExpr()
		args = append(args, ast.Arg{Name: name, Value: val})
		if !p.accept(token.COMMA) {
			break
		}
		p.ensureProgress(before)
	}
	p.expect(token.RPAREN)
	return args
}

// parseIdentName consome um identificador e devolve seu lexema, ou "" com
// diagnóstico se o token corrente não for um IDENT.
func (p *parser) parseIdentName() string {
	if p.at(token.IDENT) {
		return p.advance().Lit
	}
	p.errorf(p.cur().Pos, "esperava um identificador, encontrei %s", p.cur().Kind)
	return ""
}

// atFatArrow reporta se o cursor está em "=>" (os tokens ASSIGN GT adjacentes;
// o lexer não tem um token dedicado, REQ-1.3).
func (p *parser) atFatArrow() bool {
	return p.at(token.ASSIGN) && p.peek().Kind == token.GT
}

// expectFatArrow consome "=>" ou reporta erro sem consumir.
func (p *parser) expectFatArrow() bool {
	if p.atFatArrow() {
		p.advance() // '='
		p.advance() // '>'
		return true
	}
	p.errorf(p.cur().Pos, "esperava '=>', encontrei %s", p.cur().Kind)
	return false
}

// parseMatchExpr parseia "match SUBJECT { ARM... }" como expressão; cada braço
// produz um valor (REQ-2.4).
func (p *parser) parseMatchExpr() ast.Expr {
	start := p.cur().Pos
	p.expect(token.MATCH)
	subject := p.parseExpr()
	p.expect(token.LBRACE)
	var arms []ast.MatchExprArm
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		pats, guard := p.parseMatchArmHead()
		body := p.parseExpr()
		arms = append(arms, ast.MatchExprArm{Patterns: pats, Guard: guard, Body: body})
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewMatchExpr(subject, arms, p.spanFrom(start))
}

// parseMatchArmHead parseia os padrões (separados por vírgula), o guard opcional
// (when EXPR) e consome o "=>" que precede o corpo. Os padrões usam parseOr (sem
// range/lambda) para não consumir o "=>" do próprio braço.
func (p *parser) parseMatchArmHead() (patterns []ast.Expr, guard ast.Expr) {
	patterns = append(patterns, p.parseOr())
	for p.accept(token.COMMA) {
		patterns = append(patterns, p.parseOr())
	}
	if p.accept(token.WHEN) {
		guard = p.parseOr()
	}
	p.expectFatArrow()
	return patterns, guard
}

func (p *parser) matchAny(ops ...token.Kind) bool {
	for _, o := range ops {
		if p.at(o) {
			return true
		}
	}
	return false
}

// spanFrom devolve o span de start até o último token consumido.
func (p *parser) spanFrom(start token.Pos) ast.Span {
	return ast.Span{Start: start, End: p.lastPos}
}

func isLiteralKind(k token.Kind) bool {
	switch k {
	case token.INT, token.FLOAT, token.STRING, token.DURATION, token.RATE,
		token.SIZE, token.VERSIONID, token.TRUE, token.FALSE:
		return true
	}
	return false
}
