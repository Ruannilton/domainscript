package program

import (
	"domainscript/ast"
	"domainscript/token"
)

// Module é um módulo de domínio agregado: a declaração de mod.ds, os bancos de
// dados que ele configura e o service que o hospeda na topologia (REQ-7.2).
type Module struct {
	Name      string
	Decl      *ast.ModuleDecl      // declaração em mod.ds (nil se ausente)
	Databases map[string]*Database // por nome de banco
	Service   string               // service dono (de topology.ds), "" se nenhum
}

// Database é um banco configurado num mod.ds (§12): suporte a XA (transação
// distribuída) e a lista de aggregates que ele gerencia. É a ponte
// aggregate→banco→módulo das regras transacionais cross-file (REQ-5.9, REQ-7.2).
type Database struct {
	Name       string
	Module     string
	SupportsXA bool
	Manages    []string // nomes de Aggregate geridos por este banco
	// Provider é o valor textual de "provider:" (ex. "postgres", "sqlite"),
	// livre no front-end (nunca validado contra um enum fixo — qualquer string
	// é aceita, ver resolver/resolve_config_test.go). O codegen (G1,
	// §design 3.11) é quem dá semântica a valores reconhecidos: hoje só
	// "sqlite" seleciona o adapter real database/sql (codegen/sqlrt);
	// qualquer outro valor (incl. "postgres", ausente) mantém o fallback
	// in-memory do Marco E — nenhum driver correspondente é vendorado ainda
	// (NFR-12, mesmo espírito documentado de gRPC/OTel em §design 4.4).
	Provider string
	// DSN é o valor textual de "dsn:" (data source name/connection string do
	// adapter real, ex. um caminho de arquivo sqlite) — "" quando ausente ou
	// não-literal (ex. env(...), não resolvido estaticamente aqui, mesmo
	// espírito de httpPortGo em codegen/codegen.go).
	DSN  string
	Decl *ast.ConfigBlock
}

// Service é um service da topologia (§11): agrupa módulos. Um service =
// monólito; múltiplos services = microsserviços (REQ-7.2).
type Service struct {
	Name    string
	Modules []string
	Decl    *ast.ServiceDef
}

// Channel é um canal de comunicação entre dois módulos (§11): o transporte (Via:
// direct/queue/grpc/http/stream) declarado na topologia. As regras cross-service
// exigem um canal declarado entre módulos em services distintos (REQ-5.11).
type Channel struct {
	From string // módulo de origem
	To   string // módulo de destino
	Via  string // transporte: direct, queue, grpc, http, stream
	Decl *ast.ChannelDef
}

// buildGraph monta os módulos (de mod.ds), o grafo de services e canais (de
// topology.ds) e o mapeamento aggregate→módulo, ligando cada módulo ao seu
// service (REQ-7.2). Roda após a coleta de símbolos.
func (p *Program) buildGraph() {
	// Módulos e seus bancos, a partir das ModuleDecl de cada mod.ds.
	for _, file := range p.Files {
		for _, d := range file.Decls {
			m, ok := d.(*ast.ModuleDecl)
			if !ok || m.Name == "" {
				continue
			}
			p.Modules[m.Name] = newModule(m)
		}
	}

	// Mapeamento aggregate→módulo declarante (REQ-7.2): base de aggregate→banco→
	// módulo→service usada pelas regras transacionais.
	for path, file := range p.Files {
		mod := p.fileModule[path]
		for _, d := range file.Decls {
			if a, ok := d.(*ast.AggregateDecl); ok && a.Name != "" {
				p.aggModule[a.Name] = mod
			}
		}
	}

	// Services e canais, a partir das TopologyDecl.
	for _, file := range p.Files {
		for _, d := range file.Decls {
			t, ok := d.(*ast.TopologyDecl)
			if !ok {
				continue
			}
			for _, s := range t.Services {
				svc := &Service{Name: s.Name, Modules: identList(entry(s.Entries, "modules")), Decl: s}
				p.Services[s.Name] = svc
				for _, mod := range svc.Modules {
					if m := p.Modules[mod]; m != nil {
						m.Service = s.Name
					}
				}
			}
			for _, c := range t.Channels {
				p.Channels = append(p.Channels, &Channel{
					From: c.From, To: c.To, Via: identName(entry(c.Entries, "via")), Decl: c,
				})
			}
		}
	}
}

// newModule extrai o modelo de um módulo de sua ModuleDecl, incluindo os bancos
// declarados (blocos Database) com supportsXA e a lista manages.
func newModule(m *ast.ModuleDecl) *Module {
	mod := &Module{Name: m.Name, Decl: m, Databases: make(map[string]*Database)}
	for _, b := range m.Blocks {
		if b.Kind != "Database" {
			continue
		}
		mod.Databases[b.Name] = &Database{
			Name:       b.Name,
			Module:     m.Name,
			SupportsXA: boolValue(entry(b.Entries, "supportsXA")),
			Manages:    identList(entry(b.Entries, "manages")),
			Provider:   stringValue(entry(b.Entries, "provider")),
			DSN:        stringValue(entry(b.Entries, "dsn")),
			Decl:       b,
		}
	}
	return mod
}

// --- consultas do grafo (REQ-7.2/7.3) ---

// ModuleOfAggregate devolve o módulo que declara o Aggregate dado, ou "".
func (p *Program) ModuleOfAggregate(agg string) string { return p.aggModule[agg] }

// ServiceOfModule devolve o service que hospeda o módulo dado, ou "".
func (p *Program) ServiceOfModule(module string) string {
	if m := p.Modules[module]; m != nil {
		return m.Service
	}
	return ""
}

// DatabaseOfAggregate devolve o Database que gerencia o Aggregate dado (via a
// lista manages do mod.ds), ou nil se nenhum banco o reivindica (REQ-7.2).
func (p *Program) DatabaseOfAggregate(agg string) *Database {
	for _, m := range p.Modules {
		for _, db := range m.Databases {
			for _, managed := range db.Manages {
				if managed == agg {
					return db
				}
			}
		}
	}
	return nil
}

// ServiceOfAggregate segue a cadeia aggregate→módulo→service e devolve o service
// dono do Aggregate, ou "" (REQ-7.2).
func (p *Program) ServiceOfAggregate(agg string) string {
	return p.ServiceOfModule(p.ModuleOfAggregate(agg))
}

// ChannelBetween devolve o canal declarado de from para to, ou nil se não há
// canal direto entre esses módulos (REQ-5.11).
func (p *Program) ChannelBetween(from, to string) *Channel {
	for _, c := range p.Channels {
		if c.From == from && c.To == to {
			return c
		}
	}
	return nil
}

// --- extração de valores de configuração ---

// entry procura a ConfigEntry de chave key numa lista e devolve seu valor, ou nil.
func entry(entries []ast.ConfigEntry, key string) ast.Expr {
	for _, e := range entries {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

// identName devolve o nome se expr é um Ident, senão "".
func identName(expr ast.Expr) string {
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// boolValue devolve true se expr é o literal booleano `true`.
func boolValue(expr ast.Expr) bool {
	lit, ok := expr.(*ast.Literal)
	return ok && lit.Kind == token.TRUE
}

// stringValue devolve o conteúdo (já sem aspas — mesma convenção de
// httpPortGo em codegen/codegen.go) de expr quando é um literal STRING, ou ""
// para qualquer outra forma (ausente, env(...), não-literal).
func stringValue(expr ast.Expr) string {
	lit, ok := expr.(*ast.Literal)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	return lit.Value
}

// identList devolve os nomes dos elementos Ident de uma lista [a, b, c]; elementos
// que não são identificadores são ignorados.
func identList(expr ast.Expr) []string {
	list, ok := expr.(*ast.ListExpr)
	if !ok {
		return nil
	}
	var out []string
	for _, el := range list.Elems {
		if name := identName(el); name != "" {
			out = append(out, name)
		}
	}
	return out
}
