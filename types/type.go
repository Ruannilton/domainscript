package types

import (
	"strings"

	"domainscript/symbols"
)

// Type é o modelo estático de um valor. O método não-exportado typeNode restringe
// o conjunto de variantes a este pacote; String dá a forma legível para
// mensagens de diagnóstico (esperado-vs-encontrado, REQ-6.8).
type Type interface {
	typeNode()
	String() string
}

// Field é um membro nomeado de um tipo composto, com o seu próprio tipo. Espelha
// ast.Field, mas já resolvido a um Type (não mais a um nome textual).
type Field struct {
	Name string
	Type Type
}

// Primitive é um tipo embutido sem estrutura: integer, decimal, string, boolean,
// datetime, bytes e os literais especializados (duration, rate, size, version).
type Primitive struct{ Name string }

func (*Primitive) typeNode()        {}
func (p *Primitive) String() string { return p.Name }

// VOType é um ValueObject. Na forma wrapper, Base é o tipo embrulhado e Fields é
// vazio (ValueObject Email(string)); na forma composta, Fields lista os campos e
// Base é nil (§design 3.5).
type VOType struct {
	Name   string
	Base   Type
	Fields []Field
}

func (*VOType) typeNode()        {}
func (v *VOType) String() string { return v.Name }

// EnumType é um Enum: um conjunto fechado de membros nomeados sob um tipo base.
type EnumType struct {
	Name    string
	Base    Type
	Members []string
}

func (*EnumType) typeNode()        {}
func (e *EnumType) String() string { return e.Name }

// ShapeType é a forma de campos de um Aggregate, Event, Command, View ou
// Notification — declarações cujo valor é um registro de campos. Kind distingue
// qual delas, para mensagens e para regras que dependem do tipo de declaração.
type ShapeType struct {
	Name   string
	Kind   symbols.Kind
	Fields []Field
}

func (*ShapeType) typeNode()        {}
func (s *ShapeType) String() string { return s.Name }

// Generic é uma coleção genérica aplicada: List<T>, AppendList<T>, Set<T>,
// Map<K,V>. Ctor é o construtor; Args, os argumentos de tipo.
type Generic struct {
	Ctor string
	Args []Type
}

func (*Generic) typeNode() {}
func (g *Generic) String() string {
	if len(g.Args) == 0 {
		return g.Ctor
	}
	parts := make([]string, len(g.Args))
	for i, a := range g.Args {
		parts[i] = a.String()
	}
	return g.Ctor + "<" + strings.Join(parts, ", ") + ">"
}

// FuncType é a assinatura de um operador, Query ou método: parâmetros e
// resultado.
type FuncType struct {
	Params []Type
	Result Type
}

func (*FuncType) typeNode() {}
func (f *FuncType) String() string {
	parts := make([]string, len(f.Params))
	for i, p := range f.Params {
		parts[i] = p.String()
	}
	res := "()"
	if f.Result != nil {
		res = f.Result.String()
	}
	return "(" + strings.Join(parts, ", ") + ") -> " + res
}

// errorType é o sentinela anti-cascata (REQ-11.3): toda operação sobre ele produz
// um tipo de erro, sem emitir diagnóstico. É um valor único, exposto por ErrorType.
type errorType struct{}

func (errorType) typeNode()      {}
func (errorType) String() string { return "<error>" }

// ErrorType é o único valor do tipo de erro. Compará-lo por identidade (==) ou via
// IsError reconhece a propagação de um erro anterior.
var ErrorType Type = errorType{}

// IsError reporta se t é o tipo de erro sentinela (ou nil, tratado como erro).
func IsError(t Type) bool {
	if t == nil {
		return true
	}
	_, ok := t.(errorType)
	return ok
}

// Identical reporta se a e b são o mesmo tipo. Tipos nomeados (Primitive, VOType,
// EnumType, ShapeType) usam identidade nominal — comparar só o nome evita recursão
// infinita em tipos mutuamente recursivos. Generic e FuncType são estruturais
// (List<integer> difere de List<string>). errorType é idêntico só a si mesmo.
func Identical(a, b Type) bool {
	switch x := a.(type) {
	case *Primitive:
		y, ok := b.(*Primitive)
		return ok && x.Name == y.Name
	case *VOType:
		y, ok := b.(*VOType)
		return ok && x.Name == y.Name
	case *EnumType:
		y, ok := b.(*EnumType)
		return ok && x.Name == y.Name
	case *ShapeType:
		y, ok := b.(*ShapeType)
		return ok && x.Name == y.Name
	case *Generic:
		y, ok := b.(*Generic)
		if !ok || x.Ctor != y.Ctor || len(x.Args) != len(y.Args) {
			return false
		}
		for i := range x.Args {
			if !Identical(x.Args[i], y.Args[i]) {
				return false
			}
		}
		return true
	case *FuncType:
		y, ok := b.(*FuncType)
		if !ok || len(x.Params) != len(y.Params) {
			return false
		}
		for i := range x.Params {
			if !Identical(x.Params[i], y.Params[i]) {
				return false
			}
		}
		return Identical(x.Result, y.Result)
	case errorType:
		_, ok := b.(errorType)
		return ok
	}
	return false
}
