package sema

import "domainscript/ast"

// checkUpcastReplaceableByDefault implementa REQ-5.18 (⚠️, §4.3): a regra do spec
// é "campos novos com `default`; transformações complexas com `Upcast`". Um Upcast
// cujo corpo só atribui valores constantes — sem ler nada do evento de origem — não
// é uma transformação: cada campo novo poderia ser declarado com um `default`
// diretamente no Event. Avisa nesse caso.
//
// O critério de "constante" é não referenciar o evento de origem, exposto no corpo
// pelo identificador convencional `event` (ver §4.3 e os exemplos de Metric/Upcast).
// O exemplo canônico do spec — `fee = Money(amount: 0, currency: event.amount.currency)`
// — lê `event.amount.currency`, então é uma transformação legítima e não dispara.
func (c *Checker) checkUpcastReplaceableByDefault(u *ast.UpcastDecl) {
	if u.Body == nil {
		return
	}
	assigns := 0
	for _, s := range u.Body.Stmts {
		a, ok := s.(*ast.AssignStmt)
		if !ok {
			// Qualquer statement que não seja atribuição simples já caracteriza uma
			// lógica que um `default` não expressa: não é substituível.
			return
		}
		if referencesEvent(a.Value) {
			return // lê do evento de origem: transformação real, mantém o Upcast
		}
		assigns++
	}
	if assigns > 0 {
		c.bag.Warningf(u.Pos(),
			"Upcast %q (%s -> %s) só atribui valores constantes; declare os campos novos com `default` no Event em vez de um Upcast (§4.3)",
			u.Event, u.FromVer, u.ToVer)
	}
}

// referencesEvent reporta se a expressão lê o evento de origem do upcast, isto é,
// se em qualquer ponto referencia o identificador `event`.
func referencesEvent(e ast.Expr) bool {
	found := false
	forEachExpr(e, func(x ast.Expr) {
		if isIdent(x, "event") {
			found = true
		}
	})
	return found
}
