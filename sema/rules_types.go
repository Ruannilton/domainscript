package sema

import "domainscript/ast"

// primitiveTypes são os tipos primitivos proibidos no Write Side (a "Regra de
// Ouro" do §2.1: integer, decimal, string, boolean, datetime). Devem ser
// embrulhados em ValueObject ou Enum (REQ-5.1).
var primitiveTypes = map[string]bool{
	"integer":  true,
	"decimal":  true,
	"string":   true,
	"boolean":  true,
	"datetime": true,
}

// checkWriteSidePrimitives implementa REQ-5.1: um tipo primitivo usado
// diretamente como campo de Aggregate, Command ou Event (o Write Side) é erro.
// Coleções de domínio (AppendList<StatementEntry>, Map<...>) são permitidas — só
// o tipo declarado do campo é inspecionado, não seus argumentos genéricos.
func (c *Checker) checkWriteSidePrimitives(kind, owner string, fields []*ast.Field) {
	for _, f := range fields {
		if f == nil || f.Type == nil {
			continue
		}
		if primitiveTypes[f.Type.Name] {
			c.bag.Errorf(f.Type.Pos(),
				"primitivo %q proibido no Write Side: o campo %q de %s %q deve usar um ValueObject ou Enum",
				f.Type.Name, f.Name, kind, owner)
		}
	}
}
