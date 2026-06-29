package ast

import "domainscript/token"

// Span é o intervalo [Start, End] que um nó ocupa no source. Guardar o fim além
// do início permite que diagnósticos semânticos sublinhem a extensão de um nó
// (REQ-2.6, §design 3.3).
type Span struct {
	Start token.Pos
	End   token.Pos
}

// Node é o nó genérico da AST. Todo nó conhece sua posição inicial e seu span.
// O método não-exportado node() restringe o conjunto de tipos a este pacote.
type Node interface {
	Pos() token.Pos // posição inicial (equivale a Span().Start)
	Span() Span
	node()
}

// Decl é uma declaração (ValueObject, Aggregate, UseCase, ...).
type Decl interface {
	Node
	declNode()
}

// Stmt é um statement (ensure, match, for, return, ...).
type Stmt interface {
	Node
	stmtNode()
}

// Expr é uma expressão.
type Expr interface {
	Node
	exprNode()
}

// baseNode fornece a implementação comum de Node (posição e span) para
// embutir nos nós concretos, evitando repetição.
type baseNode struct {
	span Span
}

func (b baseNode) Pos() token.Pos { return b.span.Start }
func (b baseNode) Span() Span     { return b.span }
func (baseNode) node()            {}
