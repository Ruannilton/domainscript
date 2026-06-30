package sema

import (
	"domainscript/ast"
	"domainscript/symbols"
)

// checkPolicyPublicEvent implementa REQ-5.8 (§7, §23): uma Policy só pode reagir,
// cross-module, a eventos exportados como PublicEvent. Quando o `on` de uma Policy
// nomeia um Event privado declarado em outro módulo, o consumo viola o
// encapsulamento do módulo produtor — é erro. Eventos do próprio módulo e
// PublicEvents resolvem normalmente (Lookup) e não são alvo. É cross-file: o Event
// privado é invisível ao Lookup module-scoped, então a busca é global (REQ-7.3).
func (c *Checker) checkPolicyPublicEvent() {
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			pol, ok := d.(*ast.PolicyDecl)
			if !ok || pol.On == "" {
				continue
			}
			if _, ok := c.tab.Lookup(u.Module, pol.On); ok {
				continue // resolve no próprio módulo ou como PublicEvent: permitido
			}
			sym, ok := c.tab.Find(pol.On)
			if !ok || sym.Kind != symbols.KindEvent || sym.Public || sym.Module == u.Module {
				continue
			}
			c.bag.Errorf(pol.Pos(),
				"Policy %q reage ao Event %q do módulo %q, que não é PublicEvent: exporte-o como PublicEvent para consumo cross-module (§7)",
				pol.Name, pol.On, sym.Module)
		}
	}
}

// checkNotificationAdapters implementa REQ-5.3 (§9.1): toda Notification precisa
// de um Adapter correspondente (mesmo nome, mesmo módulo); sem ele é erro. É uma
// regra cross-declaração: roda sobre todas as unidades após a coleta.
func (c *Checker) checkNotificationAdapters() {
	adapters := map[string]bool{}
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			if a, ok := d.(*ast.AdapterDecl); ok && a.Name != "" {
				adapters[u.Module+"\x00"+a.Name] = true
			}
		}
	}
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			n, ok := d.(*ast.NotificationDecl)
			if !ok || n.Name == "" {
				continue
			}
			if !adapters[u.Module+"\x00"+n.Name] {
				c.bag.Errorf(n.Pos(),
					"Notification %q não tem Adapter correspondente (§9.1): declare um Adapter de mesmo nome",
					n.Name)
			}
		}
	}
}

// checkAggregateAccess implementa REQ-5.2 (§4.5): o bloco access do Aggregate é
// closed-by-default — todo Handle precisa de uma regra de acesso correspondente
// (mesmo nome). Um Handle sem entrada em access é erro.
func (c *Checker) checkAggregateAccess(agg *ast.AggregateDecl) {
	allowed := make(map[string]bool, len(agg.Access))
	for _, rule := range agg.Access {
		if rule != nil {
			allowed[rule.Name] = true
		}
	}
	for _, h := range agg.Handlers {
		if h == nil || h.Name == "" {
			continue
		}
		if !allowed[h.Name] {
			c.bag.Errorf(h.Pos(),
				"Handle %q do Aggregate %q não tem entrada no bloco access (acesso é closed-by-default)",
				h.Name, agg.Name)
		}
	}
}
