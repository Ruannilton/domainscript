package parser

import (
	"domainscript/ast"
	"domainscript/token"
)

// parseDecl roteia para o parser da declaração de topo conforme a keyword. O
// switch cresce a cada construto adicionado nas tarefas 4B; o roteamento
// completo com recovery de arquivo é a tarefa 4B.13.
func (p *parser) parseDecl() ast.Decl {
	switch {
	case p.at(token.VALUEOBJECT):
		return p.parseValueObject()
	default:
		start := p.cur().Pos
		p.errorf(start, "esperava uma declaração de topo, encontrei %s", p.cur().Kind)
		p.synchronize(topLevelStop)
		return ast.NewErrorDecl(p.spanFrom(start))
	}
}

// atIdentLit reporta se o token corrente é um IDENT com o lexema dado (usado
// para keywords contextuais de membros, ex.: Valid, Operator, state, access).
func (p *parser) atIdentLit(lit string) bool {
	return p.at(token.IDENT) && p.cur().Lit == lit
}

// parseTypeRef parseia uma referência de tipo, com argumentos genéricos opcionais
// em "<...>" (os tokens LT/GT; ">>" aninhado são dois GT consecutivos).
func (p *parser) parseTypeRef() *ast.TypeRef {
	start := p.cur().Pos
	name := p.parseIdentName()
	var args []*ast.TypeRef
	if p.accept(token.LT) {
		for !p.at(token.GT) && !p.atEnd() {
			before := p.pos
			args = append(args, p.parseTypeRef())
			if !p.accept(token.COMMA) {
				break
			}
			p.ensureProgress(before)
		}
		p.expect(token.GT)
	}
	return ast.NewTypeRef(name, args, p.spanFrom(start))
}

// parseField parseia "Name [ref] Type [redactable] [= Default]".
func (p *parser) parseField() *ast.Field {
	start := p.cur().Pos
	name := p.parseIdentName()
	ref := p.accept(token.REF)
	typ := p.parseTypeRef()
	redactable := false
	if p.atIdentLit("redactable") {
		p.advance()
		redactable = true
	}
	var def ast.Expr
	if p.accept(token.ASSIGN) {
		def = p.parseExpr()
	}
	return ast.NewField(name, typ, ref, redactable, def, p.spanFrom(start))
}

// parseParam parseia um parâmetro "Name [ref] Type".
func (p *parser) parseParam() *ast.Field {
	start := p.cur().Pos
	name := p.parseIdentName()
	ref := p.accept(token.REF)
	typ := p.parseTypeRef()
	return ast.NewField(name, typ, ref, false, nil, p.spanFrom(start))
}

// parseParamList parseia "( Param, Param, ... )".
func (p *parser) parseParamList() []*ast.Field {
	p.expect(token.LPAREN)
	var params []*ast.Field
	for !p.at(token.RPAREN) && !p.atEnd() {
		before := p.pos
		params = append(params, p.parseParam())
		if !p.accept(token.COMMA) {
			break
		}
		p.ensureProgress(before)
	}
	p.expect(token.RPAREN)
	return params
}

// parseValueObject parseia "ValueObject Name [(Base)] { membros }" (§2.2). Os
// membros são o bloco Valid, Operators e campos.
func (p *parser) parseValueObject() ast.Decl {
	start := p.cur().Pos
	p.expect(token.VALUEOBJECT)
	name := p.parseIdentName()
	var base *ast.TypeRef
	if p.accept(token.LPAREN) {
		base = p.parseTypeRef()
		p.expect(token.RPAREN)
	}
	var (
		fields []*ast.Field
		ops    []*ast.OperatorDecl
		valid  *ast.Block
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("Valid"):
			p.advance()
			valid = p.parseBlock()
		case p.atIdentLit("Operator"):
			ops = append(ops, p.parseOperator())
		default:
			fields = append(fields, p.parseField())
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewValueObjectDecl(name, base, fields, valid, ops, p.spanFrom(start))
}

// parseOperator parseia "Operator Op(Params) -> Return { Body }".
func (p *parser) parseOperator() *ast.OperatorDecl {
	start := p.cur().Pos
	p.advance() // "Operator"
	op := p.advance().Kind.String()
	params := p.parseParamList()
	p.expect(token.ARROW)
	ret := p.parseTypeRef()
	body := p.parseBlock()
	return ast.NewOperatorDecl(op, params, ret, body, p.spanFrom(start))
}
