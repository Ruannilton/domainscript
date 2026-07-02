package codegen

import (
	"fmt"

	"domainscript/ast"
)

// GoFieldType resolve a forma Go de um ast.TypeRef (§design 3.3): primitivo
// (via GoPrimitive), tipo de arquivo opaco (via GoOpaqueType), genérico
// (via GoGeneric, resolvendo os Args recursivamente), ou — se nada disso
// bater — uma referência a um tipo declarado no mesmo pacote (VO/Enum/Shape),
// cujo nome Go é idêntico ao nome DomainScript (identidade; não precisa de
// Dedupe/Ident aqui porque nomes de TIPO já são PascalCase e não colidem com
// keywords Go minúsculas). Reusado por E4 (Events), E6 (Aggregate state), E7
// (Commands) e E8 (Views).
func GoFieldType(t *ast.TypeRef) (string, error) {
	if t == nil {
		return "", fmt.Errorf("codegen: TypeRef nulo")
	}

	if len(t.Args) > 0 {
		args := make([]string, len(t.Args))
		for i, a := range t.Args {
			s, err := GoFieldType(a)
			if err != nil {
				return "", err
			}
			args[i] = s
		}
		goType, ok := GoGeneric(t.Name, args)
		if !ok {
			return "", fmt.Errorf("codegen: construtor genérico desconhecido ou aridade inválida: %s (%d args)", t.Name, len(args))
		}
		return goType, nil
	}

	if s, ok := GoPrimitive(t.Name); ok {
		return s, nil
	}
	if s, ok := GoOpaqueType(t.Name); ok {
		return s, nil
	}

	// Identidade: referência a um tipo declarado no mesmo pacote Go
	// (ValueObject/Enum/Shape) — o nome DomainScript já é o nome Go.
	return t.Name, nil
}
