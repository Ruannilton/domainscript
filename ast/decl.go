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

// EnumMember é um valor nomeado de um Enum: Name = Value.
type EnumMember struct {
	baseNode
	Name  string
	Value Expr
}

func NewEnumMember(name string, value Expr, span Span) *EnumMember {
	return &EnumMember{baseNode{span}, name, value}
}

// CoerceBlock é a coerção explícita de um Enum: coerce from Type { Body }.
type CoerceBlock struct {
	baseNode
	From *TypeRef
	Body *Block
}

func NewCoerceBlock(from *TypeRef, body *Block, span Span) *CoerceBlock {
	return &CoerceBlock{baseNode{span}, from, body}
}

// EnumDecl é a declaração de um Enum (§2.3): conjunto fechado de membros sob um
// tipo base (após ':'), com bloco coerce opcional.
type EnumDecl struct {
	baseNode
	Name    string
	Base    *TypeRef
	Members []*EnumMember
	Coerce  *CoerceBlock
}

func NewEnumDecl(name string, base *TypeRef, members []*EnumMember, coerce *CoerceBlock, span Span) *EnumDecl {
	return &EnumDecl{baseNode{span}, name, base, members, coerce}
}
func (*EnumDecl) declNode() {}
