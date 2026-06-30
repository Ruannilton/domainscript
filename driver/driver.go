package driver

import (
	"domainscript/ast"
	"domainscript/diag"
	"domainscript/lexer"
	"domainscript/parser"
	"domainscript/program"
	"domainscript/resolver"
	"domainscript/sema"
	"domainscript/token"
)

// CheckSource roda o pipeline completo sobre uma única fonte DomainScript —
// léxico → sintaxe → resolução de nomes → regras semânticas locais (REQ-8.1). Os
// diagnósticos de todas as fases são acumulados num único bag (REQ-6.1). Devolve a
// AST (nunca nil, REQ-2.7) e o bag; o chamador decide o sucesso por
// bag.HasErrors() (REQ-6.7). As regras cross-file (Fase 9) não se aplicam a um
// arquivo isolado — use CheckProject para um projeto.
func CheckSource(src string) (*ast.File, *diag.DiagnosticBag) {
	bag := diag.New()
	toks, lexDiags := lexer.Lex(src)
	for _, d := range lexDiags {
		bag.Add(d)
	}
	file := parser.Parse(toks, bag)
	tab := resolver.Resolve(file, bag)
	sema.Check(tab, file, bag)
	return file, bag
}

// CheckProject roda o pipeline sobre um diretório de projeto inteiro: agrega todos
// os arquivos .ds (léxico + sintaxe + resolução cross-module) num Program e roda as
// regras semânticas locais e cross-file (REQ-8.1/8.4, REQ-7). Todos os diagnósticos
// vão para um único bag (REQ-6.1). Uma falha de IO ao varrer o diretório é
// reportada como um diagnóstico no bag, mantendo a API uniforme (o resultado é
// sempre (programa, bag)); o Program devolvido pode ser parcial nesse caso.
func CheckProject(dir string) (*program.Program, *diag.DiagnosticBag) {
	bag := diag.New()
	prog, err := program.Build(dir, bag)
	if err != nil {
		bag.Errorf(token.Pos{Line: 1, Col: 1}, "falha ao ler o projeto %q: %v", dir, err)
		return prog, bag
	}
	sema.CheckProgram(prog, bag)
	return prog, bag
}
