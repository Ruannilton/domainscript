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
	case p.at(token.ENUM):
		return p.parseEnum()
	case p.at(token.ERROR):
		return p.parseErrorType()
	case p.at(token.EVENT), p.at(token.PUBLICEVENT):
		return p.parseEvent()
	case p.at(token.AGGREGATE):
		return p.parseAggregate()
	case p.at(token.COMMAND):
		return p.parseCommand()
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

// parseFieldBlock parseia "{ Field (,)? ... }" — campos separados por vírgula
// ou apenas por quebra de linha. Reusado por Event, Command, View, etc.
func (p *parser) parseFieldBlock() []*ast.Field {
	p.expect(token.LBRACE)
	var fields []*ast.Field
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		fields = append(fields, p.parseField())
		p.accept(token.COMMA)
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return fields
}

// parseErrorType parseia "Error Name { message \"...\" }" (§4.1).
func (p *parser) parseErrorType() ast.Decl {
	start := p.cur().Pos
	p.expect(token.ERROR)
	name := p.parseIdentName()
	var msg ast.Expr
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		if p.atIdentLit("message") {
			p.advance()
			msg = p.parseExpr()
		} else {
			p.advance() // ignora conteúdo desconhecido; recovery garante progresso
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewErrorTypeDecl(name, msg, p.spanFrom(start))
}

// parseEvent parseia "Event|PublicEvent Name { Fields }" (§4.2).
func (p *parser) parseEvent() ast.Decl {
	start := p.cur().Pos
	public := p.at(token.PUBLICEVENT)
	p.advance() // Event ou PublicEvent
	name := p.parseIdentName()
	fields := p.parseFieldBlock()
	return ast.NewEventDecl(name, public, fields, p.spanFrom(start))
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

// parseEnum parseia "Enum Name [: Base] { membros [coerce] }" (§2.3).
func (p *parser) parseEnum() ast.Decl {
	start := p.cur().Pos
	p.expect(token.ENUM)
	name := p.parseIdentName()
	var base *ast.TypeRef
	if p.accept(token.COLON) {
		base = p.parseTypeRef()
	}
	var (
		members []*ast.EnumMember
		coerce  *ast.CoerceBlock
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		if p.atIdentLit("coerce") {
			coerce = p.parseCoerce()
		} else {
			members = append(members, p.parseEnumMember())
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewEnumDecl(name, base, members, coerce, p.spanFrom(start))
}

func (p *parser) parseEnumMember() *ast.EnumMember {
	start := p.cur().Pos
	name := p.parseIdentName()
	var value ast.Expr
	if p.expect(token.ASSIGN) {
		value = p.parseExpr()
	}
	return ast.NewEnumMember(name, value, p.spanFrom(start))
}

// parseCoerce parseia "coerce from Type { Body }".
func (p *parser) parseCoerce() *ast.CoerceBlock {
	start := p.cur().Pos
	p.advance() // "coerce"
	if p.atIdentLit("from") {
		p.advance()
	}
	from := p.parseTypeRef()
	body := p.parseBlock()
	return ast.NewCoerceBlock(from, body, p.spanFrom(start))
}

// parseCommand parseia "Command Name { Fields }" (§5.1).
func (p *parser) parseCommand() ast.Decl {
	start := p.cur().Pos
	p.expect(token.COMMAND)
	name := p.parseIdentName()
	fields := p.parseFieldBlock()
	return ast.NewCommandDecl(name, fields, p.spanFrom(start))
}

// parseAggregate parseia "Aggregate Name { membros }" (§4.5).
func (p *parser) parseAggregate() ast.Decl {
	start := p.cur().Pos
	p.expect(token.AGGREGATE)
	name := p.parseIdentName()
	var (
		strategy string
		snapshot ast.Expr
		storage  []ast.StorageEntry
		state    []*ast.Field
		access   []*ast.AccessRule
		handlers []*ast.HandleDecl
		appliers []*ast.ApplyDecl
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("strategy"):
			p.advance()
			strategy = p.parseIdentName()
		case p.atIdentLit("snapshot"):
			p.advance()
			if p.atIdentLit("every") {
				p.advance()
			}
			snapshot = p.parseExpr()
			if p.atIdentLit("events") {
				p.advance()
			}
		case p.atIdentLit("storage"):
			p.advance()
			storage = p.parseStorageBlock()
		case p.atIdentLit("state"):
			p.advance()
			state = p.parseFieldBlock()
		case p.atIdentLit("access"):
			p.advance()
			access = p.parseAccessBlock()
		case p.atIdentLit("Handle"):
			handlers = append(handlers, p.parseHandle())
		case p.atIdentLit("Apply"):
			appliers = append(appliers, p.parseApply())
		default:
			p.errorf(p.cur().Pos, "membro de Aggregate inesperado: %s", p.cur().Kind)
			p.advance() // progresso garantido
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewAggregateDecl(name, strategy, snapshot, storage, state, access, handlers, appliers, p.spanFrom(start))
}

// parseStorageBlock parseia "{ Key: Value (,)? ... }".
func (p *parser) parseStorageBlock() []ast.StorageEntry {
	p.expect(token.LBRACE)
	var entries []ast.StorageEntry
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		key := p.parseIdentName()
		p.expect(token.COLON)
		val := p.parseIdentName()
		entries = append(entries, ast.StorageEntry{Key: key, Value: val})
		p.accept(token.COMMA)
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return entries
}

// parseAccessBlock parseia "{ Handle requires Condition ... }".
func (p *parser) parseAccessBlock() []*ast.AccessRule {
	p.expect(token.LBRACE)
	var rules []*ast.AccessRule
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		start := p.cur().Pos
		name := p.parseIdentName()
		if p.atIdentLit("requires") {
			p.advance()
		}
		cond := p.parseExpr()
		rules = append(rules, ast.NewAccessRule(name, cond, p.spanFrom(start)))
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return rules
}

// parseHandle parseia "Handle Name(Params) { Body }".
func (p *parser) parseHandle() *ast.HandleDecl {
	start := p.cur().Pos
	p.advance() // "Handle"
	name := p.parseIdentName()
	params := p.parseParamList()
	body := p.parseBlock()
	return ast.NewHandleDecl(name, params, body, p.spanFrom(start))
}

// parseApply parseia "Apply Event { Body }".
func (p *parser) parseApply() *ast.ApplyDecl {
	start := p.cur().Pos
	p.advance() // "Apply"
	event := p.parseIdentName()
	body := p.parseBlock()
	return ast.NewApplyDecl(event, body, p.spanFrom(start))
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
