package resolver

import (
	"domainscript/ast"
	"domainscript/symbols"
	"domainscript/token"
)

// resolve_config.go implementa a resolução de referências em arquivos de
// configuração (REQ-10, §design type-checking 3.4): os nomes que amarram a
// topologia do sistema — `manages` de um Database, alvos de rota/rpc, módulos de
// um service, endpoints de canal e alvos de versão — são resolvidos contra a
// tabela de símbolos (no caso de declarações de domínio) ou contra os módulos
// declarados (no caso da topologia). Um nome inexistente vira erro localizado
// (REQ-10.2); um nome que resolve ao Kind errado vira erro esperado-vs-encontrado
// (REQ-10.3).
//
// Nota de discrepância com a spec: o design original previa `ChannelDef.From/To`
// → Service, mas o front-end modela canais como ligações módulo→módulo (um service
// agrupa módulos; os canais ligam os módulos que vivem em services distintos).
// Resolvemos From/To contra os módulos declarados, coerente com `program/graph.go`.
// O design.md foi atualizado para refletir essa realidade.

// refSpace distingue o espaço de nomes em que uma referência de configuração
// resolve: a tabela de símbolos de domínio, ou o conjunto de módulos declarados.
type refSpace int

const (
	spaceSymbol refSpace = iota // tabela de símbolos (Aggregate, UseCase, Query, ...)
	spaceModule                 // módulos declarados em mod.ds (não têm símbolo próprio)
)

// expectedRef é uma entrada do catálogo (REQ-10.1): descreve o alvo aceitável de
// uma referência de configuração — um rótulo legível para o diagnóstico e, no
// espaço de símbolos, o conjunto de Kinds aceitos. É o "tipo esperado" da ref.
type expectedRef struct {
	label string         // rótulo para mensagens: "Aggregate", "UseCase ou Query", ...
	space refSpace       // onde a ref resolve
	kinds []symbols.Kind // Kinds aceitos (só no spaceSymbol)
}

// Catálogo declarativo config-ref → alvo esperado (§design type-checking 3.4). É o
// ponto único de extensão: ligar um construto novo de config a um alvo é editar
// aqui e em collectConfigRefs.
var (
	refAggregate   = expectedRef{label: "Aggregate", space: spaceSymbol, kinds: []symbols.Kind{symbols.KindAggregate}}
	refOperation   = expectedRef{label: "UseCase ou Query", space: spaceSymbol, kinds: []symbols.Kind{symbols.KindUseCase, symbols.KindQuery}}
	refUseCase     = expectedRef{label: "UseCase", space: spaceSymbol, kinds: []symbols.Kind{symbols.KindUseCase}}
	refCommandView = expectedRef{label: "Command ou View", space: spaceSymbol, kinds: []symbols.Kind{symbols.KindCommand, symbols.KindView}}
	refModule      = expectedRef{label: "Module", space: spaceModule}
)

// configRef é uma referência extraída de um arquivo de config: o nome citado, sua
// posição (para o diagnóstico) e o alvo esperado do catálogo.
type configRef struct {
	name   string
	pos    token.Pos
	expect expectedRef
}

// collectConfigRefs extrai todas as referências de configuração de uma declaração,
// já anotadas com o alvo esperado (REQ-10.1). Declarações sem refs de config (toda
// a domain) devolvem nil. É a metade de "extração de nomes" da Fase B.
func collectConfigRefs(d ast.Decl) []configRef {
	switch n := d.(type) {
	case *ast.ModuleDecl:
		var refs []configRef
		for _, b := range n.Blocks {
			if b == nil || b.Kind != "Database" {
				continue
			}
			refs = append(refs, listRefs(b.Entries, "manages", refAggregate)...)
		}
		return refs
	case *ast.InterfaceDecl:
		var refs []configRef
		for _, rt := range n.Routes {
			refs = appendRef(refs, rt.Target, rt.Pos(), refOperation)
		}
		for _, svc := range n.Services {
			for _, rpc := range svc.RPCs {
				// GrpcRPC não tem span próprio; usa-se a posição do service que o contém.
				refs = appendRef(refs, rpc.Target, svc.Pos(), refOperation)
			}
		}
		return refs
	case *ast.TopologyDecl:
		var refs []configRef
		for _, s := range n.Services {
			refs = append(refs, listRefs(s.Entries, "modules", refModule)...)
		}
		for _, c := range n.Channels {
			refs = appendRef(refs, c.From, c.Pos(), refModule)
			refs = appendRef(refs, c.To, c.Pos(), refModule)
		}
		return refs
	case *ast.VersionDecl:
		var refs []configRef
		for _, rt := range n.Routes {
			refs = appendRef(refs, rt.Target, rt.Pos(), refUseCase)
		}
		for _, up := range n.Upcasts {
			refs = appendRef(refs, up.Target, up.Pos(), refCommandView)
		}
		for _, dn := range n.Downcasts {
			refs = appendRef(refs, dn.Target, dn.Pos(), refCommandView)
		}
		return refs
	}
	return nil
}

// listRefs extrai os elementos Ident da entry key (ex.: manages: [A, B], modules:
// [M]), cada um com sua própria posição. Elementos que não são identificadores
// (recuperação de sintaxe) são ignorados.
func listRefs(entries []ast.ConfigEntry, key string, expect expectedRef) []configRef {
	list, ok := configValue(entries, key).(*ast.ListExpr)
	if !ok {
		return nil
	}
	var refs []configRef
	for _, el := range list.Elems {
		if id, ok := el.(*ast.Ident); ok && id.Name != "" {
			refs = append(refs, configRef{name: id.Name, pos: id.Pos(), expect: expect})
		}
	}
	return refs
}

// appendRef adiciona a ref de um alvo textual (Route.Target, Channel.From) à lista,
// cuja posição é a do nó declarante — o nome não é um nó próprio com span. Um alvo
// vazio (recuperação de sintaxe) é ignorado.
func appendRef(refs []configRef, name string, pos token.Pos, expect expectedRef) []configRef {
	if name == "" {
		return refs
	}
	return append(refs, configRef{name: name, pos: pos, expect: expect})
}

// configValue devolve o valor da ConfigEntry de chave key, ou nil.
func configValue(entries []ast.ConfigEntry, key string) ast.Expr {
	for _, e := range entries {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

// resolveConfig resolve as referências de configuração de todas as unidades
// coletadas (REQ-10). Roda como passagem do resolver, após a resolução de tipos e
// de corpos, quando todos os símbolos e módulos do programa já estão disponíveis.
func (r *Resolver) resolveConfig() {
	modules := r.declaredModules()
	for _, u := range r.units {
		for _, d := range u.file.Decls {
			for _, ref := range collectConfigRefs(d) {
				r.resolveConfigRef(u.module, modules, ref)
			}
		}
	}
}

// declaredModules é o conjunto de nomes de módulo declarados (mod.ds) no programa.
// Módulos não vivem na tabela de símbolos, então a resolução de refs de topologia
// (modules, canais) os consulta aqui (REQ-10.1).
func (r *Resolver) declaredModules() map[string]bool {
	mods := make(map[string]bool)
	for _, u := range r.units {
		for _, d := range u.file.Decls {
			if m, ok := d.(*ast.ModuleDecl); ok && m.Name != "" {
				mods[m.Name] = true
			}
		}
	}
	return mods
}

// resolveConfigRef resolve uma única referência: existência e Kind esperado
// (REQ-10.2/10.3). Refs de símbolo procuram primeiro no módulo do arquivo e depois
// globalmente (config liga a topologia entre módulos); refs de módulo consultam o
// conjunto de módulos declarados.
func (r *Resolver) resolveConfigRef(module string, modules map[string]bool, ref configRef) {
	switch ref.expect.space {
	case spaceModule:
		if !modules[ref.name] {
			r.bag.Errorf(ref.pos, "referência de configuração não declarada: %q (esperava %s)", ref.name, ref.expect.label)
		}
	case spaceSymbol:
		sym, ok := r.tab.Lookup(module, ref.name)
		if !ok {
			sym, ok = r.tab.Find(ref.name)
		}
		if !ok {
			r.bag.Errorf(ref.pos, "referência de configuração não declarada: %q (esperava %s)", ref.name, ref.expect.label)
			return
		}
		if !kindAllowed(sym.Kind, ref.expect.kinds) {
			r.bag.Errorf(ref.pos, "referência de configuração %q: esperava %s, encontrou %s",
				ref.name, ref.expect.label, sym.Kind)
		}
	}
}

// kindAllowed reporta se k está entre os Kinds aceitos pelo alvo.
func kindAllowed(k symbols.Kind, allowed []symbols.Kind) bool {
	for _, a := range allowed {
		if k == a {
			return true
		}
	}
	return false
}
