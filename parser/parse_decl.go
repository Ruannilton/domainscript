package parser

import (
	"domainscript/ast"
	"domainscript/diag"
	"domainscript/token"
)

// Parse constrói a AST de um arquivo a partir dos tokens, acumulando erros de
// sintaxe no bag. Nunca retorna nil: declarações não parseáveis viram nós de
// erro (REQ-2.7). É a API que o driver usa após o lexer.
func Parse(toks []token.Token, bag *diag.DiagnosticBag) *ast.File {
	return newParser(toks, bag).parseFile()
}

// parseFile parseia a sequência de declarações de topo até o EOF. Cada iteração
// reconhece uma declaração independentemente; falhas reancoram na próxima
// declaração de topo via o recovery de parseDecl, e ensureProgress garante
// terminação (REQ-2.1, REQ-3.7).
func (p *parser) parseFile() *ast.File {
	start := p.cur().Pos
	var decls []ast.Decl
	for !p.atEnd() {
		before := p.pos
		decls = append(decls, p.parseDecl())
		p.ensureProgress(before)
	}
	return ast.NewFile(decls, p.spanFrom(start))
}

// parseDecl roteia para o parser da declaração de topo conforme a keyword.
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
	case p.at(token.USECASE):
		return p.parseUseCase()
	case p.at(token.VIEW):
		return p.parseView()
	case p.at(token.PROJECTION):
		return p.parseProjection()
	case p.at(token.QUERY):
		return p.parseQuery()
	case p.at(token.POLICY):
		return p.parsePolicy()
	case p.at(token.WORKER):
		return p.parseWorker()
	case p.at(token.NOTIFICATION):
		return p.parseNotification()
	case p.at(token.ADAPTER):
		return p.parseAdapter()
	case p.at(token.FOREIGN):
		return p.parseForeign()
	case p.at(token.SAGA):
		return p.parseSaga()
	case p.at(token.METRIC):
		return p.parseMetric()
	case p.at(token.UPCAST):
		return p.parseUpcast()
	case p.at(token.MODULE):
		return p.parseModule()
	case p.at(token.INTERFACE):
		return p.parseInterface()
	case p.at(token.TOPOLOGY):
		return p.parseTopology()
	case p.at(token.VERSION):
		return p.parseVersionDecl()
	case p.atIdentLit("RateLimitTier"):
		return p.parseRateLimitTier()
	case p.at(token.TEST):
		return p.parseTest()
	case p.at(token.FIXTURE):
		return p.parseFixture()
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

// nameableKeywords são as keywords de declaração que, fora da posição de
// declaração, podem ser usadas como nomes de tipo/entidade (soft keywords) — por
// exemplo "list Notification n" ou um campo "out Notification". O roteamento de
// topo (parseDecl) reconhece a keyword antes de qualquer expressão, então não há
// ambiguidade.
var nameableKeywords = func() map[token.Kind]bool {
	m := make(map[token.Kind]bool, len(topLevelKeywords))
	for _, k := range topLevelKeywords {
		m[k] = true
	}
	return m
}()

func isNameableKeyword(k token.Kind) bool { return nameableKeywords[k] }

// parseName consome um nome de tipo/entidade: um IDENT ou uma soft keyword.
func (p *parser) parseName() string {
	if p.at(token.IDENT) {
		return p.advance().Lit
	}
	if isNameableKeyword(p.cur().Kind) {
		return p.advance().Kind.String()
	}
	p.errorf(p.cur().Pos, "esperava um nome, encontrei %s", p.cur().Kind)
	return ""
}

// parseTypeRef parseia uma referência de tipo, com argumentos genéricos opcionais
// em "<...>" (os tokens LT/GT; ">>" aninhado são dois GT consecutivos).
func (p *parser) parseTypeRef() *ast.TypeRef {
	start := p.cur().Pos
	name := p.parseName()
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

// parseWorker parseia "Worker Name { schedule/settings/scope/source/execute }" (§8).
func (p *parser) parseWorker() ast.Decl {
	start := p.cur().Pos
	p.expect(token.WORKER)
	name := p.parseIdentName()
	var (
		schedule    string
		scheduleArg ast.Expr
		scope       string
		settings    []ast.ConfigEntry
		source      *ast.Block
		execParam   string
		execute     *ast.Block
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("schedule"):
			p.advance()
			schedule = p.parseIdentName()
			if schedule != "continuous" {
				scheduleArg = p.parseExpr()
			}
		case p.atIdentLit("scope"):
			p.advance()
			p.accept(token.COLON)
			scope = p.parseIdentName()
		case p.atIdentLit("timeout"):
			p.advance()
			settings = append(settings, ast.ConfigEntry{Key: "timeout", Value: p.parseExpr()})
		case p.atIdentLit("onError"):
			// "onError { retry: { attempts: 3, backoff: "exponential" } }" —
			// mesma forma "Key { Object }" que parseConfigEntry (parse_config.go)
			// já reconhece para sub-blocos sem dois-pontos (ex.: Telemetry.traces).
			// Guardado em Settings como um ConfigEntry aninhado em vez de
			// descartado: o gerador (codegen, Marco F2) precisa do retry/backoff
			// declarado ali (REQ-23.3).
			settings = append(settings, p.parseConfigEntry())
		case p.atIdentLit("source"):
			p.advance()
			source = p.parseBlock()
		case p.atIdentLit("execute"):
			p.advance()
			if p.accept(token.LPAREN) {
				execParam = p.parseIdentName()
				p.expect(token.RPAREN)
			}
			execute = p.parseBlock()
		case p.at(token.IDENT) && p.peek().Kind == token.COLON:
			key := p.advance().Lit
			p.advance() // ':'
			settings = append(settings, ast.ConfigEntry{Key: key, Value: p.parseExpr()})
		default:
			p.errorf(p.cur().Pos, "membro de Worker inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewWorkerDecl(name, schedule, scheduleArg, scope, settings, source, execParam, execute, p.spanFrom(start))
}

// parseMetric parseia "Metric Name { type/value/on/buckets/labels }" (§21).
func (p *parser) parseMetric() ast.Decl {
	start := p.cur().Pos
	p.expect(token.METRIC)
	d := &ast.MetricDecl{Name: p.parseIdentName()}
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("type"):
			p.advance()
			d.Type = p.parseIdentName()
		case p.atIdentLit("value"):
			p.advance()
			d.Value = p.parseExpr()
		case p.atIdentLit("buckets"):
			p.advance()
			d.Buckets = p.parseExpr()
		case p.atIdentLit("labels"):
			p.advance()
			d.Labels = p.parseMapBlock()
		case p.at(token.ON):
			p.advance()
			d.On = p.parseExpr()
		default:
			p.errorf(p.cur().Pos, "membro de Metric inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewMetricDecl(d, p.spanFrom(start))
}

// parseUpcast parseia "Upcast Event vFrom -> vTo { Body }" (§4.3).
func (p *parser) parseUpcast() ast.Decl {
	start := p.cur().Pos
	p.expect(token.UPCAST)
	event := p.parseName()
	fromVer := p.parseVersion()
	p.expect(token.ARROW)
	toVer := p.parseVersion()
	body := p.parseBlock()
	return ast.NewUpcastDecl(event, fromVer, toVer, body, p.spanFrom(start))
}

// parseVersion consome um version_id (ex.: v1) e devolve seu lexema.
func (p *parser) parseVersion() string {
	if p.at(token.VERSIONID) {
		return p.advance().Lit
	}
	p.errorf(p.cur().Pos, "esperava um version_id (ex.: v1), encontrei %s", p.cur().Kind)
	return ""
}

// parseSaga parseia "Saga Name handles Cmd { mode ...; state {...}; step ... }" (§18.2).
func (p *parser) parseSaga() ast.Decl {
	start := p.cur().Pos
	p.expect(token.SAGA)
	name := p.parseIdentName()
	handles := ""
	if p.accept(token.HANDLES) {
		handles = p.parseName()
	}
	var (
		mode    string
		timeout ast.Expr
		state   []*ast.Field
		steps   []*ast.SagaStep
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("mode"):
			p.advance()
			mode = p.parseIdentName()
			if p.atIdentLit("timeout") {
				p.advance()
				timeout = p.parseExpr()
			}
		case p.atIdentLit("state"):
			p.advance()
			state = p.parseFieldBlock()
		case p.atIdentLit("step"):
			steps = append(steps, p.parseSagaStep())
		default:
			p.errorf(p.cur().Pos, "membro de Saga inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewSagaDecl(name, handles, mode, timeout, state, steps, p.spanFrom(start))
}

func (p *parser) parseSagaStep() *ast.SagaStep {
	start := p.cur().Pos
	p.advance() // "step"
	name := p.parseName()
	var up, down, onInfraError *ast.Block
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("up"):
			p.advance()
			up = p.parseBlock()
		case p.atIdentLit("down"):
			p.advance()
			down = p.parseBlock()
		case p.atIdentLit("onInfraError"):
			p.advance()
			onInfraError = p.parseBlock()
		default:
			p.errorf(p.cur().Pos, "membro de step inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewSagaStep(name, up, down, onInfraError, p.spanFrom(start))
}

// parseNotification parseia "Notification Name { Fields }" (§9.1).
func (p *parser) parseNotification() ast.Decl {
	start := p.cur().Pos
	p.expect(token.NOTIFICATION)
	name := p.parseIdentName()
	fields := p.parseFieldBlock()
	return ast.NewNotificationDecl(name, fields, p.spanFrom(start))
}

// parseAdapter parseia "Adapter Name { mode/http/headers/body | foreign/from/
// function/map }" (§9.3).
func (p *parser) parseAdapter() ast.Decl {
	start := p.cur().Pos
	p.expect(token.ADAPTER)
	d := &ast.AdapterDecl{Name: p.parseIdentName()}
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("mode"):
			p.advance()
			d.Mode = p.parseIdentName()
		case p.atIdentLit("http"):
			p.advance()
			d.HTTPMethod = p.parseIdentName()
			d.HTTPUrl = p.parseExpr()
		case p.atIdentLit("headers"):
			p.advance()
			d.Headers = p.parseMapBlock()
		case p.atIdentLit("body"):
			p.advance()
			d.Body = p.parseMapBlock()
		case p.atIdentLit("foreign"):
			p.advance()
			d.Lang = p.parseExpr()
			if p.atIdentLit("from") {
				p.advance()
				d.From = p.parseExpr()
			}
		case p.atIdentLit("function"):
			p.advance()
			d.Function = p.parseExpr()
		case p.atIdentLit("map"):
			p.advance()
			d.Map = p.parseMapBlock()
		default:
			p.errorf(p.cur().Pos, "membro de Adapter inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewAdapterDecl(d, p.spanFrom(start))
}

// parseForeign parseia "Foreign \"lang\" from \"path\" { function ... }" (§9.4).
func (p *parser) parseForeign() ast.Decl {
	start := p.cur().Pos
	p.expect(token.FOREIGN)
	lang := p.parseExpr()
	var from ast.Expr
	if p.atIdentLit("from") {
		p.advance()
		from = p.parseExpr()
	}
	var fns []*ast.ForeignFunc
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		if p.atIdentLit("function") {
			fns = append(fns, p.parseForeignFunc())
		} else {
			p.errorf(p.cur().Pos, "esperava 'function' em Foreign, encontrei %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewForeignDecl(lang, from, fns, p.spanFrom(start))
}

func (p *parser) parseForeignFunc() *ast.ForeignFunc {
	start := p.cur().Pos
	p.advance() // "function"
	name := p.parseName()
	params := p.parseParamList()
	var ret *ast.TypeRef
	if p.accept(token.ARROW) {
		ret = p.parseTypeRef()
	}
	return ast.NewForeignFunc(name, params, ret, p.spanFrom(start))
}

// parsePolicy parseia "Policy Name on Event { delivery ...; execute {...} }" (§7).
func (p *parser) parsePolicy() ast.Decl {
	start := p.cur().Pos
	p.expect(token.POLICY)
	name := p.parseIdentName()
	on := ""
	if p.accept(token.ON) {
		on = p.parseIdentName()
	}
	var (
		delivery string
		execute  *ast.Block
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("delivery"):
			p.advance()
			delivery = p.parseIdentName()
		case p.atIdentLit("execute"):
			p.advance()
			execute = p.parseBlock()
		default:
			p.errorf(p.cur().Pos, "membro de Policy inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewPolicyDecl(name, on, delivery, execute, p.spanFrom(start))
}

// parseConfigBlock parseia "{ Key: Value (,)? ... }" — usado por cache, e
// generalizado pelos arquivos de infraestrutura na Fase 5.
func (p *parser) parseConfigBlock() []ast.ConfigEntry {
	p.expect(token.LBRACE)
	var entries []ast.ConfigEntry
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		key := p.parseIdentName()
		p.expect(token.COLON)
		val := p.parseExpr()
		entries = append(entries, ast.ConfigEntry{Key: key, Value: val})
		p.accept(token.COMMA)
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return entries
}

// parseMapBlock parseia "{ Name = Value (,)? ... }".
func (p *parser) parseMapBlock() []ast.MapEntry {
	p.expect(token.LBRACE)
	var entries []ast.MapEntry
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		name := p.parseMapKey()
		p.expect(token.ASSIGN)
		val := p.parseExpr()
		entries = append(entries, ast.MapEntry{Name: name, Value: val})
		p.accept(token.COMMA)
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return entries
}

// parseMapKey aceita uma chave de map: um nome (ident/soft keyword) ou uma string
// literal (ex.: headers de Adapter "Authorization" = ...).
func (p *parser) parseMapKey() string {
	if p.at(token.STRING) {
		return p.advance().Lit
	}
	return p.parseName()
}

// parseView parseia "View Name [From Source] [{ campos | visibility }]" (§6.1/6.2).
func (p *parser) parseView() ast.Decl {
	start := p.cur().Pos
	p.expect(token.VIEW)
	name := p.parseIdentName()
	from := ""
	if p.atIdentLit("From") {
		p.advance()
		from = p.parseIdentName()
	}
	var (
		fields     []*ast.Field
		visibility []*ast.AccessRule
	)
	if p.at(token.LBRACE) {
		p.expect(token.LBRACE)
		for !p.at(token.RBRACE) && !p.atEnd() {
			before := p.pos
			if p.atIdentLit("visibility") {
				p.advance()
				visibility = p.parseAccessBlock()
			} else {
				fields = append(fields, p.parseField())
				p.accept(token.COMMA)
			}
			p.ensureProgress(before)
		}
		p.expect(token.RBRACE)
	}
	return ast.NewViewDecl(name, from, fields, visibility, p.spanFrom(start))
}

// parseProjection parseia "Projection Name { source ...; map {...}; refreshOn [...] }" (§6.4).
func (p *parser) parseProjection() ast.Decl {
	start := p.cur().Pos
	p.expect(token.PROJECTION)
	name := p.parseIdentName()
	var (
		sources   []string
		mapping   []ast.MapEntry
		refreshOn ast.Expr
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("source"):
			p.advance()
			sources = append(sources, p.parseIdentName())
			for p.accept(token.COMMA) {
				sources = append(sources, p.parseIdentName())
			}
		case p.atIdentLit("map"):
			p.advance()
			mapping = p.parseMapBlock()
		case p.atIdentLit("refreshOn"):
			p.advance()
			refreshOn = p.parseExpr()
		default:
			p.errorf(p.cur().Pos, "membro de Projection inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewProjectionDecl(name, sources, mapping, refreshOn, p.spanFrom(start))
}

// parseQuery parseia "Query Name(Params) -> Ret { [cache {...}] statements }" (§6.3).
func (p *parser) parseQuery() ast.Decl {
	start := p.cur().Pos
	p.expect(token.QUERY)
	name := p.parseIdentName()
	params := p.parseParamList()
	var ret *ast.TypeRef
	if p.accept(token.ARROW) {
		ret = p.parseTypeRef()
	}
	var (
		cache []ast.ConfigEntry
		stmts []ast.Stmt
	)
	bodyStart := p.cur().Pos
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		if p.atIdentLit("cache") {
			p.advance()
			cache = p.parseConfigBlock()
		} else {
			stmts = append(stmts, p.parseStmt())
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	body := ast.NewBlock(stmts, p.spanFrom(bodyStart))
	return ast.NewQueryDecl(name, params, ret, cache, body, p.spanFrom(start))
}

// parseUseCase parseia "UseCase Name handles Cmd { timeout/idempotency/tenancy/
// execute }" (§5.2).
func (p *parser) parseUseCase() ast.Decl {
	start := p.cur().Pos
	p.expect(token.USECASE)
	name := p.parseIdentName()
	handles := ""
	if p.accept(token.HANDLES) {
		handles = p.parseIdentName()
	}
	var (
		timeout     ast.Expr
		idempotency ast.Expr
		tenancy     string
		execute     *ast.Block
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("timeout"):
			p.advance()
			timeout = p.parseExpr()
		case p.atIdentLit("idempotency"):
			p.advance()
			// idempotency (§14, G2) sempre vem como um objeto de config, ex.
			// "idempotency { required: true, window: 48h }" — a mesma forma
			// "Key { Object }" que mod.ds/Worker.onError já reconhecem
			// (parseConfigEntry). p.parseExpr() sozinho NUNCA soube ler um
			// ObjectExpr "{ ... }" (achado desta task: o campo existia desde
			// antes, mas nenhum teste jamais escreveu essa sintaxe — nenhum
			// programa real conseguia declarar idempotency; ver
			// codegen/decl_usecase.go, G2). parseConfigValue (parse_config.go)
			// é o mesmo despacho genérico já usado por mod.ds/Worker: objeto
			// "{...}"/lista "[...]"/senão cai em parseExpr — mudança aditiva,
			// não regride nenhuma forma que já parseava.
			idempotency = p.parseConfigValue()
		case p.atIdentLit("tenancy"):
			p.advance()
			p.accept(token.COLON)
			tenancy = p.parseIdentName()
		case p.atIdentLit("execute"):
			p.advance()
			execute = p.parseBlock()
		default:
			p.errorf(p.cur().Pos, "membro de UseCase inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewUseCaseDecl(name, handles, timeout, idempotency, tenancy, execute, p.spanFrom(start))
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
