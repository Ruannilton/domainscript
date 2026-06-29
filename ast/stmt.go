package ast

// Block é uma sequência de statements entre chaves: { ... }.
type Block struct {
	baseNode
	Stmts []Stmt
}

func NewBlock(stmts []Stmt, span Span) *Block { return &Block{baseNode{span}, stmts} }
func (*Block) stmtNode()                      {}

// ExprStmt é uma expressão usada como statement: chamada de método, ação de
// match (incl. Nop), nome de Error em ensure, etc.
type ExprStmt struct {
	baseNode
	X Expr
}

func NewExprStmt(x Expr, span Span) *ExprStmt { return &ExprStmt{baseNode{span}, x} }
func (*ExprStmt) stmtNode()                   {}

// AssignStmt é uma atribuição: Target = Value (ex.: state.balance = ...,
// wallet = load Wallet(...)).
type AssignStmt struct {
	baseNode
	Target Expr
	Value  Expr
}

func NewAssignStmt(target, value Expr, span Span) *AssignStmt {
	return &AssignStmt{baseNode{span}, target, value}
}
func (*AssignStmt) stmtNode() {}

// MatchStmtArm é um braço de um match-statement: padrões, guard opcional e um
// corpo (statement ou bloco, incl. Nop).
type MatchStmtArm struct {
	Patterns []Expr
	Guard    Expr // nil quando não há 'when'
	Body     Stmt
}

// MatchStmt é o pattern matching usado como statement: cada braço executa uma
// ação. As regras semânticas de exaustividade/wildcard são da fase semântica.
type MatchStmt struct {
	baseNode
	Subject Expr
	Arms    []MatchStmtArm
}

func NewMatchStmt(subject Expr, arms []MatchStmtArm, span Span) *MatchStmt {
	return &MatchStmt{baseNode{span}, subject, arms}
}
func (*MatchStmt) stmtNode() {}
