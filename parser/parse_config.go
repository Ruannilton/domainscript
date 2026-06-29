package parser

import (
	"domainscript/ast"
	"domainscript/token"
)

// parse_config.go isola a gramática dos arquivos de infraestrutura não-`.ds`
// (mod.ds, interface.ds, topology.ds, versions/*.ds) (REQ-2.2, §design 3.5). O
// núcleo é o config_entry genérico: uma linha "Key: Value" cujo valor cobre
// literais, env(...), durações/taxas/tamanhos, listas e objetos aninhados.

// parseConfigValue parseia um valor de configuração: um objeto "{ ... }", uma
// lista "[ ... ]" (cujos elementos são, por sua vez, valores de configuração) ou
// qualquer expressão (literal, ident, env(...), duração, version_id, ...). É o
// que torna o config_entry genérico e aninhável (REQ-2.2).
func (p *parser) parseConfigValue() ast.Expr {
	switch {
	case p.at(token.LBRACE):
		return p.parseConfigObject()
	case p.at(token.LBRACK):
		return p.parseConfigList()
	default:
		return p.parseExpr()
	}
}

// parseConfigObject parseia "{ Key: Value (,)? ... }" como um ObjectExpr. Aceita
// tanto "Key: Value" quanto a forma sem dois-pontos "Key { ... }" (sub-bloco
// nomeado, ex.: traces { sampler: ... } em Telemetry).
func (p *parser) parseConfigObject() *ast.ObjectExpr {
	start := p.cur().Pos
	entries := p.parseConfigEntries()
	return ast.NewObjectExpr(entries, p.spanFrom(start))
}

// parseConfigEntries parseia o miolo "{ entry (,)? ... }" e devolve as linhas.
// Reusado pelos blocos folha dos arquivos de infra (Database, Cache, versioning,
// tenant, rateLimit, services, channels, ...).
func (p *parser) parseConfigEntries() []ast.ConfigEntry {
	p.expect(token.LBRACE)
	var entries []ast.ConfigEntry
	for !p.at(token.RBRACE) && !p.atEnd() {
		before := p.pos
		entries = append(entries, p.parseConfigEntry())
		p.accept(token.COMMA)
		p.ensureProgress(before)
	}
	p.expect(token.RBRACE)
	return entries
}

// parseConfigEntry parseia uma linha de configuração nas formas "Key: Value" ou
// "Key { Object }" (sub-bloco sem dois-pontos).
func (p *parser) parseConfigEntry() ast.ConfigEntry {
	key := p.parseName()
	switch {
	case p.accept(token.COLON):
		return ast.ConfigEntry{Key: key, Value: p.parseConfigValue()}
	case p.at(token.LBRACE):
		return ast.ConfigEntry{Key: key, Value: p.parseConfigObject()}
	default:
		// Tolerante: aceita "Key Value" sem dois-pontos (ex.: timeout 30s).
		return ast.ConfigEntry{Key: key, Value: p.parseConfigValue()}
	}
}

// parseConfigList parseia "[ Value (,)? ... ]" onde cada elemento é um valor de
// configuração (incl. objetos: layers: [ { type: memory }, ... ]).
func (p *parser) parseConfigList() ast.Expr {
	start := p.cur().Pos
	p.expect(token.LBRACK)
	var elems []ast.Expr
	for !p.at(token.RBRACK) && !p.atEnd() {
		before := p.pos
		elems = append(elems, p.parseConfigValue())
		if !p.accept(token.COMMA) {
			break
		}
		p.ensureProgress(before)
	}
	p.expect(token.RBRACK)
	return ast.NewListExpr(elems, p.spanFrom(start))
}
