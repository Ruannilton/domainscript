package sema

import (
	"domainscript/ast"
	"domainscript/diag"
	"domainscript/symbols"
)

// Unit é um arquivo a checar com o módulo a que pertence. As regras cross-file
// precisam saber o módulo dono de cada declaração (REQ-7.3).
type Unit struct {
	Module string
	File   *ast.File
}

// Checker aplica as regras semânticas da §23 sobre a AST já resolvida (REQ-5).
// Opera sobre o conjunto de arquivos adicionados (single-file ou um projeto
// inteiro), usando a SymbolTable como fonte de verdade para tipos e nomes. Cada
// regra é independente e acumula seus diagnósticos no bag compartilhado (NFR-4/6).
type Checker struct {
	tab   *symbols.SymbolTable
	bag   *diag.DiagnosticBag
	units []Unit
}

// New cria um checker que consulta tab e acumula diagnósticos em bag.
func New(tab *symbols.SymbolTable, bag *diag.DiagnosticBag) *Checker {
	return &Checker{tab: tab, bag: bag}
}

// AddFile registra um arquivo, no escopo de module, para checagem.
func (c *Checker) AddFile(module string, file *ast.File) {
	c.units = append(c.units, Unit{module, file})
}

// Check roda todas as regras locais da Fase 8. As regras por-declaração rodam num
// único percurso; as regras que exigem visão agregada (Notification↔Adapter,
// cobertura de erro por Handle) rodam depois, sobre todas as unidades.
func (c *Checker) Check() {
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			c.checkDecl(u.Module, d)
		}
	}
	c.checkNotificationAdapters() // 8.7
}

// Check é o atalho de arquivo único: roda o checker sobre um único arquivo num
// módulo anônimo. Usado pela API CheckSource (REQ-8.1).
func Check(tab *symbols.SymbolTable, file *ast.File, bag *diag.DiagnosticBag) {
	c := New(tab, bag)
	c.AddFile("", file)
	c.Check()
}

// checkDecl despacha cada declaração para as regras locais aplicáveis a ela.
func (c *Checker) checkDecl(module string, d ast.Decl) {
	switch n := d.(type) {
	case *ast.AggregateDecl:
		c.checkWriteSidePrimitives("Aggregate", n.Name, n.State) // 8.1
		c.checkAppendListMutation(n)                             // 8.2
		c.checkAggregateAccess(n)                                // 8.6
		for _, h := range n.Handlers {
			c.checkNop(h.Body, "Handle") // 8.4
		}
	case *ast.CommandDecl:
		c.checkWriteSidePrimitives("Command", n.Name, n.Fields) // 8.1
	case *ast.EventDecl:
		c.checkWriteSidePrimitives("Event", n.Name, n.Fields) // 8.1
	case *ast.UseCaseDecl:
		c.checkNop(n.Execute, "UseCase") // 8.4
	case *ast.VersionDecl:
		c.checkVersionUpcastDefaults(module, n) // 8.8
	case *ast.TestDecl:
		c.checkTestFile(module, n) // 8.9
	}
	c.checkMatchExhaustiveness(module, d) // 8.3
	c.checkLoopControlDecl(d)             // 8.5
}
