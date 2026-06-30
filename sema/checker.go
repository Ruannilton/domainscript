package sema

import (
	"sort"

	"domainscript/ast"
	"domainscript/diag"
	"domainscript/program"
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
	// prog é o programa agregado (REQ-7). Não-nil só na checagem de projeto
	// (CheckProgram): habilita as regras cross-file da Fase 9, que precisam do
	// grafo módulo→service→canal e do mapeamento aggregate→banco. Na checagem de
	// arquivo único é nil e essas regras não rodam.
	prog *program.Program
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
	c.checkForeignSignatures()    // 8.10
	c.checkHandleErrorCoverage()  // 8.14

	// Regras cross-file (Fase 9): exigem o programa inteiro agregado (REQ-7) e só
	// rodam na checagem de projeto. Cada uma percorre as unidades consultando o
	// grafo de topologia do programa.
	if c.prog != nil {
		c.checkTransactions()      // 9.1
		c.checkCrossDatabaseJoin() // 9.2
		c.checkServiceChannels()   // 9.3
		c.checkCrossTenantOptIn()  // 9.4
		c.checkPolicyPublicEvent() // 9.5
		c.checkSagaAwaitQueue()    // 9.6
	}
}

// CheckProgram roda o checker sobre um Program agregado, habilitando as regras
// cross-file da Fase 9 (REQ-5.8–12/16–17/23) além das locais da Fase 8. Usa a
// tabela de símbolos global do programa e atribui cada arquivo ao seu módulo
// (REQ-7.3). É o ponto de entrada da API CheckProject (REQ-8.1).
func CheckProgram(prog *program.Program, bag *diag.DiagnosticBag) {
	c := New(prog.Symbols, bag)
	c.prog = prog
	// Ordena os caminhos para que a ordem de checagem seja determinística (NFR-3),
	// independentemente da iteração do mapa.
	paths := make([]string, 0, len(prog.Files))
	for path := range prog.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		c.AddFile(prog.ModuleOf(path), prog.Files[path])
	}
	c.Check()
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
	case *ast.ValueObjectDecl:
		c.checkValueObjectAsEnum(n) // 8.11
	case *ast.QueryDecl:
		c.checkCacheHighCardinality(n) // 8.13
	case *ast.TopologyDecl:
		c.checkChannelOrderBy(n) // 8.12
	case *ast.VersionDecl:
		c.checkVersionUpcastDefaults(module, n) // 8.8
	case *ast.TestDecl:
		c.checkTestFile(module, n) // 8.9
	}
	c.checkMatchExhaustiveness(module, d) // 8.3
	c.checkLoopControlDecl(d)             // 8.5
}
