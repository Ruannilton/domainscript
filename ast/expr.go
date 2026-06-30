package ast

import "domainscript/token"

// Ident é uma referência a um nome (variável, tipo, campo, membro de enum).
type Ident struct {
	baseNode
	Name string
}

func NewIdent(name string, span Span) *Ident { return &Ident{baseNode{span}, name} }
func (*Ident) exprNode()                     {}

// Literal é um literal léxico: INT, FLOAT, STRING, DURATION, RATE, SIZE,
// VERSIONID ou um booleano (TRUE/FALSE). Kind guarda o tipo léxico; Value, o
// lexema já decodificado.
type Literal struct {
	baseNode
	Kind  token.Kind
	Value string
}

func NewLiteral(kind token.Kind, value string, span Span) *Literal {
	return &Literal{baseNode{span}, kind, value}
}
func (*Literal) exprNode() {}

// BinaryExpr é uma operação binária (Left Op Right) — lógica, igualdade,
// relacional, aditiva ou multiplicativa.
type BinaryExpr struct {
	baseNode
	Op    token.Kind
	Left  Expr
	Right Expr
}

func NewBinaryExpr(op token.Kind, left, right Expr, span Span) *BinaryExpr {
	return &BinaryExpr{baseNode{span}, op, left, right}
}
func (*BinaryExpr) exprNode() {}

// UnaryExpr é uma operação unária prefixa (Op X): negação aritmética (-) ou
// lógica (not).
type UnaryExpr struct {
	baseNode
	Op token.Kind
	X  Expr
}

func NewUnaryExpr(op token.Kind, x Expr, span Span) *UnaryExpr {
	return &UnaryExpr{baseNode{span}, op, x}
}
func (*UnaryExpr) exprNode() {}

// MemberExpr é o acesso a um membro: X.Name (campo, propriedade ou método antes
// da chamada). NamePos é a posição do nome do membro (após o ponto), para que a
// checagem de acesso a membro sublinhe o próprio membro, não o receptor (REQ-12.3).
type MemberExpr struct {
	baseNode
	X       Expr
	Name    string
	NamePos token.Pos
}

func NewMemberExpr(x Expr, name string, namePos token.Pos, span Span) *MemberExpr {
	return &MemberExpr{baseNode{span}, x, name, namePos}
}
func (*MemberExpr) exprNode() {}

// Arg é um argumento de chamada/construção: posicional (Name == "") ou nomeado
// (Name: Value).
type Arg struct {
	Name  string
	Value Expr
}

// CallExpr é uma chamada de função/método ou a construção de um ValueObject:
// Fn(Args...). Chamada e construção compartilham a mesma forma sintática.
type CallExpr struct {
	baseNode
	Fn   Expr
	Args []Arg
}

func NewCallExpr(fn Expr, args []Arg, span Span) *CallExpr {
	return &CallExpr{baseNode{span}, fn, args}
}
func (*CallExpr) exprNode() {}

// IndexExpr é uma indexação: X[Index].
type IndexExpr struct {
	baseNode
	X     Expr
	Index Expr
}

func NewIndexExpr(x, index Expr, span Span) *IndexExpr {
	return &IndexExpr{baseNode{span}, x, index}
}
func (*IndexExpr) exprNode() {}

// RangeExpr é um intervalo Low..High, usado sobretudo em "for i in 1..n".
type RangeExpr struct {
	baseNode
	Low  Expr
	High Expr
}

func NewRangeExpr(low, high Expr, span Span) *RangeExpr {
	return &RangeExpr{baseNode{span}, low, high}
}
func (*RangeExpr) exprNode() {}

// LambdaExpr é uma função anônima de um parâmetro: Param => Body, usada em
// operações de coleção (ex.: t => t.orderId).
type LambdaExpr struct {
	baseNode
	Param string
	Body  Expr
}

func NewLambdaExpr(param string, body Expr, span Span) *LambdaExpr {
	return &LambdaExpr{baseNode{span}, param, body}
}
func (*LambdaExpr) exprNode() {}

// ListExpr é um literal de lista: [a, b, c].
type ListExpr struct {
	baseNode
	Elems []Expr
}

func NewListExpr(elems []Expr, span Span) *ListExpr { return &ListExpr{baseNode{span}, elems} }
func (*ListExpr) exprNode()                         {}

// QueryClause é uma cláusula estilo SQL de uma operação de domínio: where, join,
// on, orderBy, skip, take, as. Expr guarda a expressão principal (condição,
// valor, fonte do join, chave de ordenação); Extra guarda um modificador textual
// (direção do orderBy, alias do join, nome do tipo no as).
type QueryClause struct {
	Kw    string
	Expr  Expr
	Extra string
}

// QueryExpr representa uma operação embutida de domínio: load, list, count,
// store, call, delete (prefixas, sobre Target) ou exists (pós-fixa). Binding é o
// alias opcional (list Ticket t); Clauses são as cláusulas SQL-like (REQ-2.4).
// A semântica das cláusulas (ex.: JOIN cross-database) é verificada depois.
type QueryExpr struct {
	baseNode
	Op      string
	Target  Expr
	Binding string
	Clauses []QueryClause
}

func NewQueryExpr(op string, target Expr, binding string, clauses []QueryClause, span Span) *QueryExpr {
	return &QueryExpr{baseNode{span}, op, target, binding, clauses}
}
func (*QueryExpr) exprNode() {}

// MatchExprArm é um braço de um match-expressão: um ou mais padrões, um guard
// opcional (when) e o corpo (uma expressão-valor).
type MatchExprArm struct {
	Patterns []Expr
	Guard    Expr // nil quando não há 'when'
	Body     Expr
}

// MatchExpr é o pattern matching usado como expressão: cada braço produz um
// valor (§3.2 do spec). A exaustividade e as regras de wildcard/guard são
// verificadas na fase semântica (REQ-5.5), não aqui.
type MatchExpr struct {
	baseNode
	Subject Expr
	Arms    []MatchExprArm
}

func NewMatchExpr(subject Expr, arms []MatchExprArm, span Span) *MatchExpr {
	return &MatchExpr{baseNode{span}, subject, arms}
}
func (*MatchExpr) exprNode() {}
