package types

import (
	"domainscript/ast"
	"domainscript/symbols"
)

// model.go constrói o Type de cada declaração e o catálogo de membros por tipo
// (REQ-11.1, §design type-checking 3.5). É o consumidor da SymbolTable: dado um
// símbolo, devolve o seu tipo; dado um tipo, devolve seus membros (nome → tipo).

// primitives são os tipos embutidos sem estrutura. Os tipos de File (§2.5) são
// opacos para o modelo e tratados como primitivos: não têm membros checáveis aqui.
var primitives = map[string]bool{
	"integer": true, "decimal": true, "string": true, "boolean": true,
	"datetime": true, "bytes": true,
	"File": true, "FileStream": true, "FileRef": true,
}

// collections são os construtores genéricos da linguagem (§2.4).
var collections = map[string]bool{
	"List": true, "AppendList": true, "Set": true, "Map": true,
}

// Model constrói e memoiza os tipos estáticos dos símbolos de um programa. A
// memoização com pré-inserção de um ponteiro placeholder torna a construção segura
// para tipos mutuamente recursivos (um campo que referencia o próprio tipo recebe o
// mesmo ponteiro, completado em seguida no lugar).
type Model struct {
	tab   *symbols.SymbolTable
	cache map[*symbols.Symbol]Type
}

// NewModel cria um modelo sobre a tabela de símbolos resolvida.
func NewModel(tab *symbols.SymbolTable) *Model {
	return &Model{tab: tab, cache: make(map[*symbols.Symbol]Type)}
}

// TypeOf devolve o tipo de uma declaração nomeada, construindo-o sob demanda e
// memoizando. Um símbolo nil (referência não resolvida) vira o tipo de erro,
// preservando a anti-cascata (REQ-11.3).
func (m *Model) TypeOf(sym *symbols.Symbol) Type {
	if sym == nil {
		return ErrorType
	}
	if t, ok := m.cache[sym]; ok {
		return t
	}
	switch n := sym.Decl.(type) {
	case *ast.ValueObjectDecl:
		vo := &VOType{Name: sym.Name}
		m.cache[sym] = vo // pré-insere para quebrar recursão
		if n.Base != nil {
			vo.Base = m.typeRef(sym.Module, n.Base)
		}
		vo.Fields = m.fields(sym.Module, n.Fields)
		return vo
	case *ast.EnumDecl:
		en := &EnumType{Name: sym.Name}
		m.cache[sym] = en
		if n.Base != nil {
			en.Base = m.typeRef(sym.Module, n.Base)
		}
		for _, mem := range n.Members {
			if mem != nil {
				en.Members = append(en.Members, mem.Name)
			}
		}
		return en
	case *ast.AggregateDecl:
		return m.shape(sym, n.State)
	case *ast.EventDecl:
		return m.shape(sym, n.Fields)
	case *ast.CommandDecl:
		return m.shape(sym, n.Fields)
	case *ast.ViewDecl:
		return m.shape(sym, n.Fields)
	case *ast.NotificationDecl:
		return m.shape(sym, n.Fields)
	case *ast.QueryDecl:
		fn := &FuncType{}
		m.cache[sym] = fn
		for _, p := range n.Params {
			if p != nil {
				fn.Params = append(fn.Params, m.typeRef(sym.Module, p.Type))
			}
		}
		fn.Result = m.typeRef(sym.Module, n.Return)
		return fn
	default:
		// UseCase/Policy/Saga/Worker/... não têm forma de campos própria relevante à
		// checagem de membro; um shape nominal sem campos é inócuo e estável.
		sh := &ShapeType{Name: sym.Name, Kind: sym.Kind}
		m.cache[sym] = sh
		return sh
	}
}

// shape constrói o ShapeType de uma declaração-registro (Aggregate/Event/Command/
// View/Notification) a partir dos seus campos, com pré-inserção para recursão.
func (m *Model) shape(sym *symbols.Symbol, fields []*ast.Field) Type {
	sh := &ShapeType{Name: sym.Name, Kind: sym.Kind}
	m.cache[sym] = sh
	sh.Fields = m.fields(sym.Module, fields)
	return sh
}

// fields resolve cada ast.Field ao seu Type.
func (m *Model) fields(module string, fields []*ast.Field) []Field {
	if len(fields) == 0 {
		return nil
	}
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if f == nil || f.Name == "" {
			continue
		}
		out = append(out, Field{Name: f.Name, Type: m.typeRef(module, f.Type)})
	}
	return out
}

// typeRef resolve uma referência de tipo da AST ao seu Type: primitivo, coleção
// genérica (recursiva nos argumentos) ou símbolo declarado (via TypeOf, que
// memoiza). Um nome não resolvido vira o tipo de erro (REQ-11.3).
func (m *Model) typeRef(module string, t *ast.TypeRef) Type {
	if t == nil || t.Name == "" {
		return ErrorType
	}
	if primitives[t.Name] {
		return &Primitive{Name: t.Name}
	}
	if collections[t.Name] {
		args := make([]Type, 0, len(t.Args))
		for _, a := range t.Args {
			args = append(args, m.typeRef(module, a))
		}
		return &Generic{Ctor: t.Name, Args: args}
	}
	if sym, ok := m.tab.Lookup(module, t.Name); ok {
		return m.TypeOf(sym)
	}
	// Fallback global: um tipo público de outro módulo (REQ-7.3).
	if sym, ok := m.tab.Find(t.Name); ok {
		return m.TypeOf(sym)
	}
	return ErrorType
}

// Members devolve o catálogo de membros acessíveis por '.' sobre um valor de t
// (nome → tipo do membro). Para um Aggregate, são os campos do state — a base de
// self/state (REQ-12.2). Para um Enum, cada membro tem o próprio tipo do Enum.
// Tipos sem membros (primitivos, erro) devolvem nil.
func (m *Model) Members(t Type) map[string]Type {
	switch x := t.(type) {
	case *VOType:
		return fieldMap(x.Fields)
	case *ShapeType:
		return fieldMap(x.Fields)
	case *EnumType:
		out := make(map[string]Type, len(x.Members))
		for _, name := range x.Members {
			out[name] = x
		}
		return out
	default:
		return nil
	}
}

// fieldMap indexa campos por nome.
func fieldMap(fields []Field) map[string]Type {
	if len(fields) == 0 {
		return nil
	}
	out := make(map[string]Type, len(fields))
	for _, f := range fields {
		out[f.Name] = f.Type
	}
	return out
}
