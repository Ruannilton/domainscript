package types

import (
	"domainscript/ast"
	"domainscript/token"
)

// infer.go infere o tipo de uma expressão a partir do tipo das suas subexpressões
// (REQ-11.2, §design type-checking 3.5). Não emite diagnósticos: é consultada pelas
// regras de membro (REQ-12) e compatibilidade (REQ-13), que reportam. Toda
// subexpressão de erro produz ErrorType, absorvendo a cascata (REQ-11.3 / NFR-9).

// Scope é o ambiente léxico visto pela inferência: nome → tipo. A resolução de
// nomes (resolver/) já validou os nomes; aqui só precisamos do tipo de cada um.
// Mantido como interface para honrar a direção de dependências (types não importa
// resolver): o escopo da Fase D implementa LookupType sobre seus Bindings.
type Scope interface {
	LookupType(name string) (Type, bool)
}

// MapScope é uma implementação trivial de Scope sobre um mapa, útil em testes e
// como base para o escopo de tipos da checagem (Fase D).
type MapScope map[string]Type

// LookupType procura name no mapa.
func (s MapScope) LookupType(name string) (Type, bool) {
	t, ok := s[name]
	return t, ok
}

// Infer devolve o tipo de e no escopo sc, dentro de module. Um e nil, um nó de
// erro, ou qualquer subexpressão de erro produzem ErrorType sem diagnóstico.
func (m *Model) Infer(module string, e ast.Expr, sc Scope) Type {
	switch n := e.(type) {
	case nil, *ast.ErrorExpr:
		return ErrorType

	case *ast.Literal:
		return literalType(n.Kind)

	case *ast.Ident:
		if sc != nil {
			if t, ok := sc.LookupType(n.Name); ok {
				return t
			}
		}
		// Um nome solto que não está no escopo léxico pode ser um símbolo do módulo
		// (ex.: membro de Enum por nome, ou um tipo usado como valor). Não resolveu →
		// ErrorType (o erro de nome, se houver, já foi reportado na resolução).
		if sym, ok := m.symbol(module, n.Name); ok {
			return m.TypeOf(sym)
		}
		return ErrorType

	case *ast.MemberExpr:
		base := m.Infer(module, n.X, sc)
		if IsError(base) {
			return ErrorType // anti-cascata (REQ-11.3)
		}
		if mem := m.Members(base); mem != nil {
			if t, ok := mem[n.Name]; ok {
				return t
			}
		}
		return ErrorType // membro inexistente: o diagnóstico é da Fase D

	case *ast.CallExpr:
		return m.inferCall(module, n, sc)

	case *ast.BinaryExpr:
		l := m.Infer(module, n.Left, sc)
		r := m.Infer(module, n.Right, sc)
		if IsError(l) || IsError(r) {
			return ErrorType
		}
		return binaryResult(n.Op, l)

	case *ast.UnaryExpr:
		x := m.Infer(module, n.X, sc)
		if IsError(x) {
			return ErrorType
		}
		if n.Op == token.NOT {
			return &Primitive{Name: "boolean"}
		}
		return x // negação aritmética preserva o tipo

	case *ast.IndexExpr:
		base := m.Infer(module, n.X, sc)
		if IsError(base) {
			return ErrorType
		}
		// List<T>[i] → T; Map<K,V>[k] → V: o último argumento de tipo é o elemento.
		if g, ok := base.(*Generic); ok && len(g.Args) > 0 {
			return g.Args[len(g.Args)-1]
		}
		return ErrorType

	case *ast.ListExpr:
		var elem Type = ErrorType
		if len(n.Elems) > 0 {
			elem = m.Infer(module, n.Elems[0], sc)
		}
		return &Generic{Ctor: "List", Args: []Type{elem}}

	case *ast.RangeExpr:
		return &Generic{Ctor: "List", Args: []Type{&Primitive{Name: "integer"}}}

	default:
		// LambdaExpr, QueryExpr, MatchExpr: a inferência depende de contexto que esta
		// etapa ainda não modela; tratados como erro (sem cascata).
		return ErrorType
	}
}

// inferCall infere o tipo de uma chamada/construção. Quando Fn nomeia um símbolo
// de tipo, é uma construção e o resultado é o próprio tipo (VO/Command/Event/...);
// quando Fn tem tipo de função, o resultado é o tipo de retorno.
func (m *Model) inferCall(module string, n *ast.CallExpr, sc Scope) Type {
	if id, ok := n.Fn.(*ast.Ident); ok {
		// Um local que sombreia o nome é uma chamada de função, não construção;
		// só tratamos como construção quando o nome resolve a um símbolo de tipo.
		shadowed := false
		if sc != nil {
			_, shadowed = sc.LookupType(id.Name)
		}
		if !shadowed {
			if sym, ok := m.symbol(module, id.Name); ok {
				return constructionResult(m.TypeOf(sym))
			}
		}
	}
	ft := m.Infer(module, n.Fn, sc)
	if f, ok := ft.(*FuncType); ok {
		return f.Result
	}
	return ErrorType
}

// constructionResult dá o tipo do valor produzido por uma construção/chamada de um
// símbolo: um FuncType (Query/Operator) produz o seu resultado; os demais tipos
// (VO/Command/Event/...) são produzidos diretamente.
func constructionResult(t Type) Type {
	if f, ok := t.(*FuncType); ok {
		return f.Result
	}
	return t
}

// literalType mapeia o tipo léxico de um literal ao seu primitivo.
func literalType(k token.Kind) Type {
	switch k {
	case token.INT:
		return &Primitive{Name: "integer"}
	case token.FLOAT:
		return &Primitive{Name: "decimal"}
	case token.STRING:
		return &Primitive{Name: "string"}
	case token.TRUE, token.FALSE:
		return &Primitive{Name: "boolean"}
	case token.DURATION:
		return &Primitive{Name: "duration"}
	case token.RATE:
		return &Primitive{Name: "rate"}
	case token.SIZE:
		return &Primitive{Name: "size"}
	case token.VERSIONID:
		return &Primitive{Name: "version"}
	default:
		return ErrorType
	}
}

// binaryResult dá o tipo do resultado de um operador binário: comparações e
// operadores lógicos produzem boolean; operadores aritméticos preservam o tipo do
// operando esquerdo (ambos os lados já foram validados como não-erro).
func binaryResult(op token.Kind, left Type) Type {
	switch op {
	case token.AND, token.OR, token.EQ, token.NEQ,
		token.LT, token.GT, token.LE, token.GE:
		return &Primitive{Name: "boolean"}
	case token.PLUS, token.MINUS, token.STAR, token.SLASH:
		return left
	default:
		return ErrorType
	}
}
