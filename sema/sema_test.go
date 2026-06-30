package sema

import (
	"path/filepath"
	"testing"

	"domainscript/ast"
	"domainscript/diag"
	"domainscript/lexer"
	"domainscript/parser"
	"domainscript/program"
	"domainscript/resolver"
)

// parseSrc lexa e parseia src, falhando o teste em erro léxico ou de sintaxe —
// os testes do checker isolam erros semânticos, então a entrada deve ser
// sintaticamente limpa (NFR-6).
func parseSrc(t *testing.T, src string) *ast.File {
	t.Helper()
	toks, lexDiags := lexer.Lex(src)
	if len(lexDiags) > 0 {
		t.Fatalf("erro léxico inesperado: %v", lexDiags)
	}
	bag := diag.New()
	file := parser.Parse(toks, bag)
	if bag.Len() != 0 {
		t.Fatalf("erro de sintaxe inesperado:\n%s", bag.Render())
	}
	return file
}

// checkSrc resolve e roda o checker sobre um único arquivo, devolvendo apenas os
// diagnósticos do checker. A resolução usa um bag separado, descartado: o teste
// isola a regra semântica sob exame da resolução de nomes (como checkFiles e
// checkProject). Vários fixtures de regra usam corpos mínimos com nomes-placeholder
// (ex.: `for x in items`) que a resolução de corpos (REQ-9) não liga; a resolução
// de nomes tem cobertura própria nos testes do resolver e na regressão do Wallet.
func checkSrc(t *testing.T, src string) *diag.DiagnosticBag {
	t.Helper()
	file := parseSrc(t, src)
	rbag := diag.New()
	tab := resolver.Resolve(file, rbag)
	sbag := diag.New()
	Check(tab, file, sbag)
	return sbag
}

// checkFiles resolve e checa um conjunto de arquivos num módulo anônimo,
// exercitando as regras que precisam da visão agregada do programa (REQ-7). Só
// os diagnósticos do checker são devolvidos.
func checkFiles(t *testing.T, srcs ...string) *diag.DiagnosticBag {
	t.Helper()
	rbag := diag.New()
	r := resolver.New(rbag)
	var files []*ast.File
	for _, s := range srcs {
		file := parseSrc(t, s)
		files = append(files, file)
		r.Add("", file)
	}
	r.ResolveAll()

	sbag := diag.New()
	c := New(r.Table(), sbag)
	for _, file := range files {
		c.AddFile("", file)
	}
	c.Check()
	return sbag
}

// projFile é um arquivo de um projeto de teste: o caminho (que define o módulo
// dono pelo mod.ds mais próximo) e a fonte.
type projFile struct {
	path string
	src  string
}

// pf monta um projFile com o caminho independente do separador do SO.
func pf(src string, parts ...string) projFile {
	return projFile{path: filepath.Join(parts...), src: src}
}

// checkProject agrega os arquivos num Program e roda o checker com as regras
// cross-file habilitadas (REQ-7, Fase 9). A resolução usa um bag separado: as
// regras cross-file deliberadamente exercitam referências que o resolver
// module-scoped não liga (corpos de execute cruzando módulos), então só os
// diagnósticos do checker são devolvidos. A tabela de símbolos global do programa
// continua enxergando todas as declarações (coletadas independentemente da
// resolução), que é o que as regras consultam.
func checkProject(t *testing.T, files ...projFile) *diag.DiagnosticBag {
	t.Helper()
	rbag := diag.New()
	srcs := make([]program.Source, 0, len(files))
	for _, f := range files {
		srcs = append(srcs, program.Source{Path: f.path, File: parseSrc(t, f.src)})
	}
	prog := program.New(srcs, rbag)

	sbag := diag.New()
	CheckProgram(prog, sbag)
	return sbag
}

// mustBeSilent falha se o bag tem qualquer diagnóstico (teste negativo: código
// correto não dispara a regra, NFR-4).
func mustBeSilent(t *testing.T, bag *diag.DiagnosticBag, rule string) {
	t.Helper()
	if bag.Len() != 0 {
		t.Fatalf("%s: código correto não deveria gerar diagnósticos:\n%s", rule, bag.Render())
	}
}
