package sema

import "domainscript/ast"

// checkVersionUpcastDefaults implementa REQ-5.13 (§17): um upcast de versão
// traduz a shape antiga de uma request para o Command atual. Todo campo
// obrigatório do Command-alvo (sem default próprio) precisa receber um valor no
// bloco `to`; um campo obrigatório não atribuído e sem default não tem como ser
// preenchido na tradução — é erro de compilação.
func (c *Checker) checkVersionUpcastDefaults(module string, v *ast.VersionDecl) {
	for _, up := range v.Upcasts {
		if up == nil || up.Target == "" {
			continue
		}
		sym, ok := c.tab.Lookup(module, up.Target)
		if !ok {
			continue // alvo não declarado: erro de resolução, não desta regra
		}
		cmd, ok := sym.Decl.(*ast.CommandDecl)
		if !ok {
			continue
		}
		assigned := make(map[string]bool, len(up.To))
		for _, e := range up.To {
			assigned[e.Name] = true
		}
		for _, f := range cmd.Fields {
			if f == nil || f.Name == "" || f.Default != nil {
				continue // campo ausente, sem nome, ou com default: não é obrigatório aqui
			}
			if !assigned[f.Name] {
				c.bag.Errorf(up.Pos(),
					"upcast de %q na versão %s não atribui o campo obrigatório %q e ele não tem default: forneça um valor no bloco `to` (§17)",
					up.Target, v.Version, f.Name)
			}
		}
	}
}
