package sema

import "domainscript/ast"

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
