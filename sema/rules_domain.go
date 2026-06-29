package sema

import "domainscript/ast"

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
