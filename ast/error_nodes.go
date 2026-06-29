package ast

// Nós de erro tipados. O parser nunca retorna nil: quando uma subárvore não pode
// ser parseada, ele a substitui por um destes nós, preservando o span da região
// problemática. As fases posteriores (resolver, checker) reconhecem esses nós e
// pulam suas subárvores, evitando que um erro de sintaxe vire um erro semântico
// falso (REQ-2.7, REQ-4.5, §design 3.3).

// ErrorDecl substitui uma declaração de topo que não pôde ser parseada.
type ErrorDecl struct{ baseNode }

// ErrorStmt substitui um statement que não pôde ser parseado.
type ErrorStmt struct{ baseNode }

// ErrorExpr substitui uma expressão que não pôde ser parseada.
type ErrorExpr struct{ baseNode }

// NewErrorDecl cria um nó de erro de declaração cobrindo span.
func NewErrorDecl(span Span) *ErrorDecl { return &ErrorDecl{baseNode{span}} }

// NewErrorStmt cria um nó de erro de statement cobrindo span.
func NewErrorStmt(span Span) *ErrorStmt { return &ErrorStmt{baseNode{span}} }

// NewErrorExpr cria um nó de erro de expressão cobrindo span.
func NewErrorExpr(span Span) *ErrorExpr { return &ErrorExpr{baseNode{span}} }

func (*ErrorDecl) declNode() {}
func (*ErrorStmt) stmtNode() {}
func (*ErrorExpr) exprNode() {}

// Garantias em tempo de compilação de que os nós de erro satisfazem suas
// interfaces respectivas.
var (
	_ Decl = (*ErrorDecl)(nil)
	_ Stmt = (*ErrorStmt)(nil)
	_ Expr = (*ErrorExpr)(nil)
)
