package ast

// TypeRef é uma referência a um tipo, possivelmente genérico: Money, string,
// AppendList<StatementEntry>, Map<string, string>.
type TypeRef struct {
	baseNode
	Name string
	Args []*TypeRef
}

func NewTypeRef(name string, args []*TypeRef, span Span) *TypeRef {
	return &TypeRef{baseNode{span}, name, args}
}

// Field é um campo de declaração ou um parâmetro: Name Type, com modificadores
// opcionais. Ref marca a forma "ref Tipo" (type-safety de Command); Redactable
// marca campos GDPR; Default guarda o valor padrão (versionamento de evento).
type Field struct {
	baseNode
	Name       string
	Type       *TypeRef
	Ref        bool
	Redactable bool
	Default    Expr
}

func NewField(name string, typ *TypeRef, ref, redactable bool, def Expr, span Span) *Field {
	return &Field{baseNode{span}, name, typ, ref, redactable, def}
}

// OperatorDecl é um operador de ValueObject: Operator Op(Params) -> Return { Body }.
type OperatorDecl struct {
	baseNode
	Op     string
	Params []*Field
	Return *TypeRef
	Body   *Block
}

func NewOperatorDecl(op string, params []*Field, ret *TypeRef, body *Block, span Span) *OperatorDecl {
	return &OperatorDecl{baseNode{span}, op, params, ret, body}
}

// ValueObjectDecl é a declaração de um ValueObject (§2.2). Base é o tipo
// embrulhado na forma wrapper (ValueObject Email(string)); Fields, a forma
// composta. Valid é o bloco de auto-validação; Operators, os operadores.
type ValueObjectDecl struct {
	baseNode
	Name      string
	Base      *TypeRef
	Fields    []*Field
	Valid     *Block
	Operators []*OperatorDecl
}

func NewValueObjectDecl(name string, base *TypeRef, fields []*Field, valid *Block, ops []*OperatorDecl, span Span) *ValueObjectDecl {
	return &ValueObjectDecl{baseNode{span}, name, base, fields, valid, ops}
}
func (*ValueObjectDecl) declNode() {}
