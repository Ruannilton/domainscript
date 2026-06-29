// Package sema aplica as regras semânticas da §23 do spec sobre a AST resolvida
// (REQ-5). Orquestra famílias de regras independentes, cada uma uma função que
// percorre os nós relevantes e emite diagnósticos.
//
// Regras locais operam por arquivo/declaração; regras cross-file exigem o
// modelo de programa agregado (REQ-7).
package sema
