package emit

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Emitter monta um arquivo Go: buffer de corpo indentado + imports geridos
// (nunca escritos à mão), formatado por go/format.Source no fechamento
// (REQ-15, §design codegen 3.2).
type Emitter struct {
	pkg    string
	body   strings.Builder
	indent int

	importOrder []string          // paths, na ordem de registro (Import)
	importAlias map[string]string // path -> alias já decidido
	aliasSeq    map[string]int    // alias-base -> quantas vezes já foi atribuído
}

// New cria um Emitter para um arquivo do pacote Go pkg (nome curto, ex. "wallet").
func New(pkg string) *Emitter {
	return &Emitter{
		pkg:         pkg,
		importAlias: make(map[string]string),
		aliasSeq:    make(map[string]int),
	}
}

// Import registra o import de importPath e devolve o identificador Go a ser
// usado para referenciá-lo no corpo. Chamar Import duas vezes com o mesmo
// path devolve sempre o mesmo alias (idempotente). Se dois paths diferentes
// colidirem no mesmo nome-padrão (último segmento do path), o import
// registrado depois recebe um sufixo numérico determinístico pela ordem de
// registro (ex. "json", depois "json2").
func (e *Emitter) Import(importPath string) string {
	if alias, ok := e.importAlias[importPath]; ok {
		return alias
	}
	base := aliasBase(importPath)
	e.aliasSeq[base]++
	alias := base
	if n := e.aliasSeq[base]; n > 1 {
		alias = fmt.Sprintf("%s%d", base, n)
	}
	e.importAlias[importPath] = alias
	e.importOrder = append(e.importOrder, importPath)
	return alias
}

// aliasBase deriva o alias-padrão do último segmento de um import path,
// restrito a caracteres válidos em identificador Go (paths podem conter
// pontos/hifens, ex. "gopkg.in/yaml.v2").
func aliasBase(importPath string) string {
	var b strings.Builder
	for _, r := range path.Base(importPath) {
		switch {
		case r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		}
	}
	clean := b.String()
	if clean == "" || (clean[0] >= '0' && clean[0] <= '9') {
		clean = "pkg" + clean
	}
	return clean
}

// Line escreve uma linha indentada no nível de indentação atual no corpo do
// arquivo. pattern/args funcionam como fmt.Sprintf.
func (e *Emitter) Line(pattern string, args ...any) {
	e.writeLine(fmt.Sprintf(pattern, args...))
}

// Block escreve "head {", executa body() com indentação +1, depois "}".
func (e *Emitter) Block(head string, body func()) {
	e.writeLine(head + " {")
	e.indent++
	body()
	e.indent--
	e.writeLine("}")
}

func (e *Emitter) writeLine(s string) {
	e.body.WriteString(strings.Repeat("\t", e.indent))
	e.body.WriteString(s)
	e.body.WriteByte('\n')
}

// Bytes monta o arquivo final ("package <pkg>" + imports ordenados por path +
// o corpo acumulado), valida que todo import registrado via Import é
// referenciado no corpo e roda go/format.Source. Não muta o Emitter: chamar
// Bytes mais de uma vez devolve sempre os mesmos bytes (NFR-13).
func (e *Emitter) Bytes() ([]byte, error) {
	paths := append([]string(nil), e.importOrder...)
	sort.Strings(paths)

	var header strings.Builder
	header.WriteString("package " + e.pkg + "\n\n")
	if len(paths) > 0 {
		header.WriteString("import (\n")
		for _, p := range paths {
			header.WriteString("\t" + e.importAlias[p] + " " + strconv.Quote(p) + "\n")
		}
		header.WriteString(")\n\n")
	}
	header.WriteString(e.body.String())
	src := []byte(header.String())

	fset := token.NewFileSet()
	astFile, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("emit: corpo Go inválido: %w\n--- Go bruto ---\n%s", err, src)
	}

	used := make(map[string]bool)
	ast.Inspect(astFile, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := sel.X.(*ast.Ident); ok {
				used[id.Name] = true
			}
		}
		return true
	})

	var unused []string
	for _, p := range paths {
		if !used[e.importAlias[p]] {
			unused = append(unused, p)
		}
	}
	if len(unused) > 0 {
		return nil, fmt.Errorf("emit: import(s) registrado(s) e não usado(s) no corpo: %s", strings.Join(unused, ", "))
	}

	formatted, err := format.Source(src)
	if err != nil {
		return nil, fmt.Errorf("emit: go/format.Source falhou: %w\n--- Go bruto ---\n%s", err, src)
	}
	return formatted, nil
}
