package parser

import (
	"domainscript/ast"
	"domainscript/token"
)

// parse_testfile.go isola a gramática dos arquivos de teste nativo (*.test.ds,
// §22, REQ-2.3). O parser reconhece a estrutura Given-When-Then; as validações
// (evento/comando inexistente, fail step inexistente, ...) ficam na semântica
// (REQ-5.14), preservando a separação de fases (NFR-6).
//
// Nota: o design nomeia este arquivo `parse_test.go`, mas em Go esse sufixo é
// reservado a arquivos de teste; usamos `parse_testfile.go` para que o parser de
// *.test.ds seja código de produção.

// parseTest parseia "Test Name { scenario... property... }" (§22).
func (p *parser) parseTest() ast.Decl {
	start := p.cur().Pos
	p.expect(token.TEST)
	name := p.parseIdentName()
	var (
		scenarios  []*ast.ScenarioDecl
		properties []*ast.PropertyDecl
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("scenario"):
			scenarios = append(scenarios, p.parseScenario())
		case p.atIdentLit("property"):
			properties = append(properties, p.parseProperty())
		default:
			p.errorf(p.cur().Pos, "membro de Test inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewTestDecl(name, scenarios, properties, p.spanFrom(start))
}

// parseScenario parseia "scenario \"...\" { mock/fail/given/when/then }".
func (p *parser) parseScenario() *ast.ScenarioDecl {
	start := p.cur().Pos
	p.advance() // "scenario"
	name := p.parseStringLit()
	var (
		mocks  []*ast.MockClause
		fails  []*ast.FailStep
		givens []*ast.GivenClause
		when   *ast.WhenClause
		then   *ast.ThenClause
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("mock"):
			mocks = append(mocks, p.parseMock())
		case p.atIdentLit("fail"):
			fails = append(fails, p.parseFailStep())
		case p.atIdentLit("given"):
			p.advance()
			givens = append(givens, p.parseGivenBody(p.cur().Pos))
		case p.at(token.WHEN):
			when = p.parseWhen()
		case p.atIdentLit("then"):
			then = p.parseThen()
		default:
			p.errorf(p.cur().Pos, "membro de scenario inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewScenarioDecl(name, mocks, fails, givens, when, then, p.spanFrom(start))
}

// parseMock parseia "mock Target returns Returns" (§22.3).
func (p *parser) parseMock() *ast.MockClause {
	start := p.cur().Pos
	p.advance() // "mock"
	target := p.parsePostfix()
	var returns ast.Expr
	if p.atIdentLit("returns") {
		p.advance()
		returns = p.parseExpr()
	}
	return ast.NewMockClause(target, returns, p.spanFrom(start))
}

// parseFailStep parseia "fail step Name with Error" (§22.3).
func (p *parser) parseFailStep() *ast.FailStep {
	start := p.cur().Pos
	p.advance() // "fail"
	if p.atIdentLit("step") {
		p.advance()
	}
	step := p.parseName()
	with := ""
	if p.atIdentLit("with") {
		p.advance()
		with = p.parseName()
	}
	return ast.NewFailStep(step, with, p.spanFrom(start))
}

// parseWhen parseia "when [event] Action" (§22).
func (p *parser) parseWhen() *ast.WhenClause {
	start := p.cur().Pos
	p.expect(token.WHEN)
	isEvent := false
	if p.atIdentLit("event") {
		p.advance()
		isEvent = true
	}
	action := p.parseExpr()
	return ast.NewWhenClause(isEvent, action, p.spanFrom(start))
}

// parseGivenBody parseia o miolo de um given (depois da keyword) nas formas:
// "[eventos]", "state { ... }", "binding [entidades]" e "Subject from [eventos]".
func (p *parser) parseGivenBody(start token.Pos) *ast.GivenClause {
	switch {
	case p.at(token.LBRACK):
		return ast.NewGivenClause(nil, "", p.parseEntityList(), nil, p.spanFrom(start))
	case p.atIdentLit("state"):
		p.advance()
		return ast.NewGivenClause(nil, "", nil, p.parseConfigObject(), p.spanFrom(start))
	case p.at(token.IDENT) && p.peek().Kind == token.LBRACK:
		// "binding [entidades]" — ident seguido diretamente de '['.
		binding := p.advance().Lit
		return ast.NewGivenClause(nil, binding, p.parseEntityList(), nil, p.spanFrom(start))
	default:
		subj := p.parsePostfix()
		if p.atIdentLit("from") {
			p.advance()
			return ast.NewGivenClause(subj, "", p.parseEntityList(), nil, p.spanFrom(start))
		}
		// Forma degenerada: um único sujeito sem lista.
		return ast.NewGivenClause(nil, "", []*ast.GivenEntity{{Entity: subj}}, nil, p.spanFrom(start))
	}
}

// parseEntityList parseia "[ Entidade [{ estado }] (,)? ... ]". Cada entidade é
// uma construção de evento/entidade com bloco de estado direto opcional.
func (p *parser) parseEntityList() []*ast.GivenEntity {
	p.expect(token.LBRACK)
	var entities []*ast.GivenEntity
	for !p.at(token.RBRACK) && !p.atEnd() {
		before := p.pos
		e := p.parseExpr()
		var state *ast.ObjectExpr
		if p.at(token.LBRACE) {
			state = p.parseConfigObject()
		}
		entities = append(entities, &ast.GivenEntity{Entity: e, State: state})
		if !p.accept(token.COMMA) {
			break
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACK)
	return entities
}

// parseThen parseia a asserção "then" nas formas "[eventos]", "error Name" e
// "{ asserts }".
func (p *parser) parseThen() *ast.ThenClause {
	start := p.cur().Pos
	p.advance() // "then"
	switch {
	case p.atIdentLit("error"):
		p.advance()
		return ast.NewThenClause(p.parseName(), nil, nil, p.spanFrom(start))
	case p.at(token.LBRACK):
		return ast.NewThenClause("", p.parseBracketExprs(), nil, p.spanFrom(start))
	case p.at(token.LBRACE):
		return ast.NewThenClause("", nil, p.parseThenAsserts(), p.spanFrom(start))
	default:
		p.errorf(p.cur().Pos, "esperava '[', 'error' ou '{' após then, encontrei %s", p.cur().Kind)
		return ast.NewThenClause("", nil, nil, p.spanFrom(start))
	}
}

// parseBracketExprs parseia "[ Expr (,)? ... ]" e devolve os elementos.
func (p *parser) parseBracketExprs() []ast.Expr {
	if le, ok := p.parseListLiteral().(*ast.ListExpr); ok {
		return le.Elems
	}
	return nil
}

// parseThenAsserts parseia o bloco "{ assert (,)? ... }".
func (p *parser) parseThenAsserts() []*ast.ThenAssert {
	p.expect(token.LBRACE)
	var asserts []*ast.ThenAssert
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		asserts = append(asserts, p.parseThenAssert())
		p.accept(token.COMMA)
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return asserts
}

// parseThenAssert parseia uma linha de asserção. Reconhece as formas com verbo
// inicial (committed, error, emitted, compensated, called, saga) e a forma com
// sujeito à esquerda (Subject emitted Expr, Subject released).
func (p *parser) parseThenAssert() *ast.ThenAssert {
	start := p.cur().Pos
	switch {
	case p.atIdentLit("error"):
		p.advance()
		return &ast.ThenAssert{Span: p.spanFrom(start), Error: p.parseName()}
	case p.atIdentLit("saga"):
		p.advance()
		return &ast.ThenAssert{Span: p.spanFrom(start), Verb: "saga", Object: p.parseExpr()}
	case p.atIdentLit("committed"), p.atIdentLit("rolledback"):
		verb := p.advance().Lit
		return &ast.ThenAssert{Span: p.spanFrom(start), Verb: verb}
	case p.atIdentLit("emitted"):
		p.advance()
		a := &ast.ThenAssert{Verb: "emitted"}
		if p.atIdentLit("count") {
			p.advance()
			a.Count = p.parseExpr()
		} else {
			a.Object = p.parseExpr()
		}
		a.Span = p.spanFrom(start)
		return a
	case p.atIdentLit("compensated"):
		p.advance()
		a := &ast.ThenAssert{Verb: "compensated"}
		if p.at(token.LBRACK) {
			a.List = p.parseBracketExprs()
		}
		a.Span = p.spanFrom(start)
		return a
	case p.atIdentLit("called"):
		p.advance()
		return &ast.ThenAssert{Span: p.spanFrom(start), Verb: "called", Object: p.parseExpr()}
	default:
		subj := p.parsePostfix()
		a := &ast.ThenAssert{Subject: subj}
		if p.at(token.IDENT) {
			a.Verb = p.advance().Lit
			switch a.Verb {
			case "emitted":
				if p.atIdentLit("count") {
					p.advance()
					a.Count = p.parseExpr()
				} else {
					a.Object = p.parseExpr()
				}
			case "called":
				a.Object = p.parseExpr()
			}
		}
		a.Span = p.spanFrom(start)
		return a
	}
}

// parseProperty parseia "property \"...\" { forall sequence of [...] invariant ... }"
// (§22.5).
func (p *parser) parseProperty() *ast.PropertyDecl {
	start := p.cur().Pos
	p.advance() // "property"
	name := p.parseStringLit()
	var (
		forall    ast.Expr
		invariant ast.Expr
	)
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		switch {
		case p.atIdentLit("forall"):
			p.advance()
			if p.atIdentLit("sequence") {
				p.advance()
			}
			if p.atIdentLit("of") {
				p.advance()
			}
			forall = p.parseExpr()
		case p.atIdentLit("invariant"):
			p.advance()
			invariant = p.parseExpr()
		default:
			p.errorf(p.cur().Pos, "membro de property inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewPropertyDecl(name, forall, invariant, p.spanFrom(start))
}

// parseFixture parseia "Fixture name { Subject from [...] ... }" (§22.6).
func (p *parser) parseFixture() ast.Decl {
	start := p.cur().Pos
	p.expect(token.FIXTURE)
	name := p.parseIdentName()
	var givens []*ast.GivenClause
	p.expect(token.LBRACE)
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		if p.atConfigKey() || p.at(token.LBRACK) {
			givens = append(givens, p.parseGivenBody(p.cur().Pos))
		} else {
			p.errorf(p.cur().Pos, "membro de Fixture inesperado: %s", p.cur().Kind)
			p.advance()
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return ast.NewFixtureDecl(name, givens, p.spanFrom(start))
}
