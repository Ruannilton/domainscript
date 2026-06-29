package sema

import (
	"testing"

	"domainscript/ast"
	"domainscript/diag"
	"domainscript/lexer"
	"domainscript/parser"
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
// diagnósticos do checker. A resolução usa um bag separado, que não pode ter
// erros: assim o teste isola a regra semântica sob exame da resolução de nomes.
func checkSrc(t *testing.T, src string) *diag.DiagnosticBag {
	t.Helper()
	file := parseSrc(t, src)
	rbag := diag.New()
	tab := resolver.Resolve(file, rbag)
	if rbag.HasErrors() {
		t.Fatalf("erro de resolução inesperado (a entrada do teste deve resolver):\n%s", rbag.Render())
	}
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

// mustBeSilent falha se o bag tem qualquer diagnóstico (teste negativo: código
// correto não dispara a regra, NFR-4).
func mustBeSilent(t *testing.T, bag *diag.DiagnosticBag, rule string) {
	t.Helper()
	if bag.Len() != 0 {
		t.Fatalf("%s: código correto não deveria gerar diagnósticos:\n%s", rule, bag.Render())
	}
}
