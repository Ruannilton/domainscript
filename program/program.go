package program

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"domainscript/ast"
	"domainscript/diag"
	"domainscript/lexer"
	"domainscript/parser"
	"domainscript/resolver"
	"domainscript/symbols"
)

// Source é um arquivo já parseado com o caminho de onde veio. É a entrada de New,
// que separa a agregação (testável, sem IO) da leitura de disco (Build).
type Source struct {
	Path string
	File *ast.File
}

// Program é o modelo unificado de um projeto: todas as ASTs agregadas num só
// lugar, com a tabela de símbolos global e o grafo módulo→service→canal. É a base
// das regras semânticas cross-file (REQ-7.1/7.3, §design 3.8). Construído antes da
// validação cross-file por New (a partir de ASTs) ou Build (a partir de um
// diretório).
type Program struct {
	// Files mapeia caminho → AST de cada arquivo do projeto (REQ-7.1).
	Files map[string]*ast.File
	// Symbols é a tabela global, com acesso simultâneo a todos os símbolos de
	// todos os módulos (REQ-7.3).
	Symbols *symbols.SymbolTable

	// fileModule mapeia caminho → módulo dono, derivado da estrutura de diretórios
	// (o mod.ds mais próximo na árvore). "" para arquivos fora de qualquer módulo.
	fileModule map[string]string
}

// New agrega um conjunto de arquivos já parseados num Program: determina o módulo
// dono de cada arquivo pela árvore de diretórios, constrói a tabela de símbolos
// global (coleta + resolução cross-module) e o grafo de topologia (REQ-7). Os
// diagnósticos de resolução são acumulados em bag. Não toca em disco.
func New(sources []Source, bag *diag.DiagnosticBag) *Program {
	// Ordena por caminho para determinismo (NFR-3): a ordem de coleta de símbolos
	// e de iteração não pode variar entre execuções.
	srcs := make([]Source, len(sources))
	copy(srcs, sources)
	sort.Slice(srcs, func(i, j int) bool { return srcs[i].Path < srcs[j].Path })

	p := &Program{
		Files:      make(map[string]*ast.File, len(srcs)),
		fileModule: make(map[string]string, len(srcs)),
	}

	// 1. Mapeia cada diretório que contém um mod.ds ao nome do módulo declarado.
	dirModule := make(map[string]string)
	for _, s := range srcs {
		path := filepath.Clean(s.Path)
		p.Files[path] = s.File
		if m := moduleDecl(s.File); m != nil && m.Name != "" {
			dirModule[filepath.Dir(path)] = m.Name
		}
	}

	// 2. Atribui cada arquivo ao módulo do mod.ds mais próximo na árvore (o de
	//    contracts/ e versions/ herda o módulo do diretório-pai).
	for path := range p.Files {
		p.fileModule[path] = moduleForPath(path, dirModule)
	}

	// 3. Coleta e resolve símbolos no escopo do módulo de cada arquivo (REQ-4 +
	//    REQ-7.3/7.4): resolução cross-module enxerga o nível público.
	r := resolver.New(bag)
	for _, s := range srcs {
		path := filepath.Clean(s.Path)
		r.Add(p.fileModule[path], s.File)
	}
	r.ResolveAll()
	p.Symbols = r.Table()

	return p
}

// Build lê, lexa e parseia todo arquivo .ds sob dir (recursivamente) e agrega o
// resultado num Program. Diagnósticos léxicos e de sintaxe de cada arquivo são
// acumulados em bag (REQ-6.1); o erro devolvido é apenas para falha de IO
// (REQ-7.1, REQ-8.4). É o ponto de entrada da CLI/driver para um diretório.
func Build(dir string, bag *diag.DiagnosticBag) (*Program, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".ds") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths) // determinismo (NFR-3)

	var sources []Source
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, readErr
		}
		toks, lexDiags := lexer.Lex(string(data))
		for _, d := range lexDiags {
			bag.Add(d)
		}
		file := parser.Parse(toks, bag)
		sources = append(sources, Source{Path: path, File: file})
	}
	return New(sources, bag), nil
}

// ModuleOf devolve o nome do módulo dono de um arquivo (o do mod.ds mais próximo
// na árvore de diretórios), ou "" se o arquivo está fora de qualquer módulo.
func (p *Program) ModuleOf(path string) string {
	return p.fileModule[filepath.Clean(path)]
}

// moduleDecl devolve a ModuleDecl de um arquivo (mod.ds tem exatamente uma), ou
// nil se o arquivo não declara um módulo.
func moduleDecl(file *ast.File) *ast.ModuleDecl {
	if file == nil {
		return nil
	}
	for _, d := range file.Decls {
		if m, ok := d.(*ast.ModuleDecl); ok {
			return m
		}
	}
	return nil
}

// moduleForPath sobe pelos diretórios-ancestrais de path até achar um que tenha
// um módulo declarado (mod.ds); devolve "" se nenhum ancestral declara módulo.
func moduleForPath(path string, dirModule map[string]string) string {
	dir := filepath.Dir(path)
	for {
		if m, ok := dirModule[dir]; ok {
			return m
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
