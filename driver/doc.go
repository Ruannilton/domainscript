// Package driver orquestra o pipeline completo (lexer → parser → resolver →
// checker) e expõe a API pública do front-end (REQ-8):
//
//	CheckSource(src)  — valida uma fonte única
//	CheckProject(dir) — valida um diretório de projeto inteiro
//
// Cada função retorna a AST/programa e o DiagnosticBag acumulado.
package driver
