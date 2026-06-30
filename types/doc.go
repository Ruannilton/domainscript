// Package types é o modelo de tipos estático do DomainScript (REQ-11): a
// representação de Type e suas variantes (type.go), a construção do tipo de uma
// declaração e seu catálogo de membros (model.go) e a inferência do tipo de uma
// expressão (infer.go).
//
// É a base das checagens de acesso a membro (REQ-12) e de compatibilidade
// (REQ-13), consumido por sema/rules_typecheck.go. Por isso depende apenas de
// ast/symbols/token — nenhuma seta aponta "para cima" (§design type-checking 1.1).
//
// O sentinela errorType absorve a cascata: qualquer operação sobre um tipo de
// erro produz um tipo de erro e nunca emite diagnóstico (REQ-11.3, NFR-9).
package types
