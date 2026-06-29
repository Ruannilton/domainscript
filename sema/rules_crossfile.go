package sema

import "domainscript/ast"

// rules_crossfile.go reúne as regras semânticas que dependem do programa inteiro
// agregado (REQ-7): transações cross-database/service, JOIN cross-database,
// canais entre services e opt-in cross-tenant (§23, REQ-5.9–12). Todas consultam
// o grafo de topologia em c.prog e só rodam na checagem de projeto.

// referencedAggregates devolve, em ordem de aparição, os Aggregates do programa
// que o bloco b referencia — pelo nome usado como identificador nu (ex.: alvo de
// `store account`... não; aqui o gatilho é a cabeça de uma construção/operação,
// ex.: `load Account(id)`, `list Entry`). Um nome conta como aggregate quando o
// programa o conhece como tal (prog.ModuleOfAggregate != ""), o que funciona
// inclusive cross-module — a base das regras transacionais (REQ-5.9, §4.3).
func (c *Checker) referencedAggregates(b *ast.Block) []string {
	seen := map[string]bool{}
	var out []string
	record := func(name string) {
		if name == "" || seen[name] {
			return
		}
		if c.prog.ModuleOfAggregate(name) != "" {
			seen[name] = true
			out = append(out, name)
		}
	}
	forEachExprInBlock(b, func(e ast.Expr) {
		switch n := e.(type) {
		case *ast.Ident:
			record(n.Name)
		case *ast.CallExpr:
			if id, ok := n.Fn.(*ast.Ident); ok {
				record(id.Name)
			}
		}
	})
	return out
}

// distinct devolve os valores únicos não-vazios de uma fatia, preservando a ordem.
func distinct(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// checkCrossDatabaseJoin implementa REQ-5.10 (§6.3, §23): um JOIN só é válido
// dentro do mesmo banco. Quando a fonte base e a fonte juntada de uma operação
// (`list X join Y on ...`) são Aggregates geridos por bancos distintos, não há
// JOIN físico possível — o caminho correto é uma Projection. Percorre os corpos de
// todas as declarações procurando QueryExpr com cláusula `join` cross-database.
func (c *Checker) checkCrossDatabaseJoin() {
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			for _, b := range declBlocks(d) {
				forEachExprInBlock(b, func(e ast.Expr) {
					qe, ok := e.(*ast.QueryExpr)
					if !ok {
						return
					}
					baseDB := c.prog.DatabaseOfAggregate(headName(qe.Target))
					if baseDB == nil {
						return
					}
					for _, cl := range qe.Clauses {
						if cl.Kw != "join" {
							continue
						}
						joinDB := c.prog.DatabaseOfAggregate(headName(cl.Expr))
						if joinDB != nil && joinDB.Name != baseDB.Name {
							c.bag.Errorf(qe.Pos(),
								"JOIN cross-database entre %q (%s) e %q (%s) não é possível: use uma Projection (§6.4)",
								headName(qe.Target), baseDB.Name, headName(cl.Expr), joinDB.Name)
						}
					}
				})
			}
		}
	}
}

// checkTransactions implementa REQ-5.9 (§23, §design 4.3): um UseCase é a
// fronteira transacional do Write Side. Se ele toca Aggregates de bancos
// distintos, a transação só é segura com suporte XA em todos eles — sem isso,
// exige modelagem como Saga (erro cross-database). Se toca Aggregates de services
// distintos, não há transação distribuída possível num UseCase simples: exige
// Saga (erro cross-service). Sagas, por coordenarem passos compensáveis, estão
// isentas — a regra mira apenas o UseCase.
func (c *Checker) checkTransactions() {
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			uc, ok := d.(*ast.UseCaseDecl)
			if !ok || uc.Execute == nil {
				continue
			}
			aggs := c.referencedAggregates(uc.Execute)
			if len(aggs) < 2 {
				continue // uma única fronteira: nenhuma regra transacional se aplica
			}

			// Cross-database: bancos distintos sem XA universal → erro.
			var dbs []string
			allXA := true
			for _, agg := range aggs {
				db := c.prog.DatabaseOfAggregate(agg)
				if db == nil {
					continue // aggregate sem banco declarado: fora do alcance da regra
				}
				dbs = append(dbs, db.Name)
				if !db.SupportsXA {
					allXA = false
				}
			}
			if len(distinct(dbs)) > 1 && !allXA {
				c.bag.Errorf(uc.Pos(),
					"UseCase %q opera cross-database (%v) sem suporte XA em todos os bancos: modele como Saga (§23)",
					uc.Name, distinct(dbs))
			}

			// Cross-service: services distintos → não há transação possível num
			// UseCase, exige Saga.
			var svcs []string
			for _, agg := range aggs {
				svcs = append(svcs, c.prog.ServiceOfAggregate(agg))
			}
			if len(distinct(svcs)) > 1 {
				c.bag.Errorf(uc.Pos(),
					"UseCase %q opera cross-service (%v): transações distribuídas exigem uma Saga (§23)",
					uc.Name, distinct(svcs))
			}
		}
	}
}
