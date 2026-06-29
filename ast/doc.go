// Package ast define a árvore sintática da DomainScript: as interfaces
// marcadoras Node/Decl/Stmt/Expr, o tipo Span (início+fim) e os nós de erro
// tipados (ErrorDecl/ErrorStmt/ErrorExpr) usados na recuperação do parser.
//
// O parser nunca retorna nil: subárvores não parseáveis viram nós de erro, que
// as fases posteriores pulam (REQ-2.6, REQ-2.7, REQ-4.5).
package ast
