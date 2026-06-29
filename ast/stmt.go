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

// EnsureStmt é uma guard clause: ensure Cond else Else. Else é a ação por
// contexto (um Error, Nop, ou um controle de laço); as regras de qual ação é
// permitida em cada contexto são da fase semântica (§3.1 do spec).
type EnsureStmt struct {
	baseNode
	Cond Expr
	Else Stmt
}

func NewEnsureStmt(cond Expr, els Stmt, span Span) *EnsureStmt {
	return &EnsureStmt{baseNode{span}, cond, els}
}
func (*EnsureStmt) stmtNode() {}

// ReturnStmt é "return [Value]"; Value é nil num return sem valor.
type ReturnStmt struct {
	baseNode
	Value Expr
}

func NewReturnStmt(value Expr, span Span) *ReturnStmt {
	return &ReturnStmt{baseNode{span}, value}
}
func (*ReturnStmt) stmtNode() {}

// ForStmt é "for Var in Iter Body" — o único construto de iteração (§3.3).
type ForStmt struct {
	baseNode
	Var  string
	Iter Expr
	Body *Block
}

func NewForStmt(v string, iter Expr, body *Block, span Span) *ForStmt {
	return &ForStmt{baseNode{span}, v, iter, body}
}
func (*ForStmt) stmtNode() {}

// LogField é uma entrada "Name = Value" do bloco opcional de um log.
type LogField struct {
	Name  string
	Value Expr
}

// LogStmt é "log Level Message { Fields }" (§3.4).
type LogStmt struct {
	baseNode
	Level   string
	Message Expr
	Fields  []LogField
}

func NewLogStmt(level string, message Expr, fields []LogField, span Span) *LogStmt {
	return &LogStmt{baseNode{span}, level, message, fields}
}
func (*LogStmt) stmtNode() {}

// EmitStmt é "emit Call" — a publicação de um evento (Call é a construção do
// evento).
type EmitStmt struct {
	baseNode
	Call Expr
}

func NewEmitStmt(call Expr, span Span) *EmitStmt { return &EmitStmt{baseNode{span}, call} }
func (*EmitStmt) stmtNode()                      {}

// BreakStmt é "break" ou "break all" (controle de laço; All distingue a forma
// aninhada). Validade fora de for é checada pela semântica (REQ-5.7).
type BreakStmt struct {
	baseNode
	All bool
}

func NewBreakStmt(all bool, span Span) *BreakStmt { return &BreakStmt{baseNode{span}, all} }
func (*BreakStmt) stmtNode()                      {}

// ContinueStmt é "continue".
type ContinueStmt struct {
	baseNode
}

func NewContinueStmt(span Span) *ContinueStmt { return &ContinueStmt{baseNode{span}} }
func (*ContinueStmt) stmtNode()               {}

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
