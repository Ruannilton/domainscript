package sema

import (
	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/symbols"
)

// checkTestFile implementa REQ-5.14 (§22): valida um arquivo de teste contra o
// domínio. As checagens estáticas tratáveis são as de existência:
//
//   - eventos/comandos referenciados em given/when/then que não foram declarados;
//   - `then error X` onde X não é um Error declarado;
//   - `fail step X` onde X não é um step do Saga sob teste;
//   - `mock Alvo` onde Alvo não é um Adapter/Notification declarado.
//
// A validação de shape de evento e de tipo de retorno de mock depende de
// informação de tipo que esta fase ainda não modela; fica para uma evolução.
func (c *Checker) checkTestFile(module string, t *ast.TestDecl) {
	steps := c.sagaSteps(module, t.Name)
	for _, sc := range t.Scenarios {
		if sc == nil {
			continue
		}
		for _, f := range sc.Fails {
			if f == nil || f.Step == "" {
				continue
			}
			if !steps[f.Step] {
				c.bag.Errorf(f.Pos(),
					"fail step %q não corresponde a nenhum step do Saga %q (§22.3)", f.Step, t.Name)
			}
		}
		for _, m := range sc.Mocks {
			if m != nil {
				c.checkTestRef(module, m.Target, "mock referencia")
			}
		}
		for _, g := range sc.Givens {
			if g == nil {
				continue
			}
			c.checkTestRef(module, g.Subject, "given referencia")
			for _, ent := range g.Entities {
				if ent != nil {
					c.checkTestRef(module, ent.Entity, "given referencia")
				}
			}
		}
		if sc.When != nil {
			c.checkTestRef(module, sc.When.Action, "when referencia")
		}
		if sc.Then != nil {
			for _, e := range sc.Then.Events {
				c.checkTestRef(module, e, "then referencia")
			}
			if sc.Then.Error != "" {
				if sym, ok := c.tab.Lookup(module, sc.Then.Error); !ok || sym.Kind != symbols.KindError {
					c.bag.Errorf(sc.Then.Pos(),
						"then error %q não corresponde a nenhum Error declarado (§22)", sc.Then.Error)
				}
			}
		}
	}
}

// checkTestRef reporta se a cabeça de uma referência de teste (construção ou
// identificador nu) não resolve a um símbolo declarado. Formas sem cabeça
// (literais, acessos a membro) são ignoradas.
func (c *Checker) checkTestRef(module string, e ast.Expr, ctx string) {
	name := astutil.HeadName(e)
	if name == "" {
		return
	}
	if _, ok := c.tab.Lookup(module, name); !ok {
		c.bag.Errorf(e.Pos(), "%s símbolo não declarado: %q (§22)", ctx, name)
	}
}

// sagaSteps devolve o conjunto de nomes de step do Saga chamado name (vazio se
// name não é um Saga). Base para validar `fail step`.
func (c *Checker) sagaSteps(module, name string) map[string]bool {
	steps := map[string]bool{}
	sym, ok := c.tab.Lookup(module, name)
	if !ok {
		return steps
	}
	saga, ok := sym.Decl.(*ast.SagaDecl)
	if !ok {
		return steps
	}
	for _, s := range saga.Steps {
		if s != nil && s.Name != "" {
			steps[s.Name] = true
		}
	}
	return steps
}

// checkForeignSignatures implementa REQ-5.15 (§9.4): uma chamada a uma função
// foreign com número de argumentos diferente do declarado na assinatura é
// incompatível. Coleta as assinaturas Foreign de todo o programa e percorre os
// sítios de chamada nos corpos de execução de todas as declarações.
func (c *Checker) checkForeignSignatures() {
	arity := map[string]int{}
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			f, ok := d.(*ast.ForeignDecl)
			if !ok {
				continue
			}
			for _, fn := range f.Functions {
				if fn != nil && fn.Name != "" {
					arity[fn.Name] = len(fn.Params)
				}
			}
		}
	}
	if len(arity) == 0 {
		return
	}
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			for _, b := range astutil.DeclBlocks(d) {
				astutil.ForEachExprInBlock(b, func(e ast.Expr) {
					call, ok := e.(*ast.CallExpr)
					if !ok {
						return
					}
					id, ok := call.Fn.(*ast.Ident)
					if !ok {
						return
					}
					want, ok := arity[id.Name]
					if !ok {
						return
					}
					if len(call.Args) != want {
						c.bag.Errorf(call.Pos(),
							"chamada a função foreign %q com %d argumento(s); a assinatura declara %d (§9.4)",
							id.Name, len(call.Args), want)
					}
				})
			}
		}
	}
}
