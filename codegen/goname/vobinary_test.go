package goname_test

import (
	"os"
	"path/filepath"
	"testing"

	"domainscript/ast"
	"domainscript/codegen/goname"
	"domainscript/driver"
	"domainscript/token"
)

// parseWalletVOs parseia o domain.ds real do wallet (docs/examples/wallet) e
// indexa seus ValueObjectDecl por nome — as fixtures desta task são o
// programa de verdade, não ASTs sintéticas. Cópia local do helper homônimo em
// codegen/decl_value_test.go: aquele vive no pacote codegen_test, este no
// pacote goname_test — pacotes de teste externos distintos não compartilham
// símbolos não exportados entre si.
func parseWalletVOs(t *testing.T) map[string]*ast.ValueObjectDecl {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "docs", "examples", "wallet", "domain.ds"))
	if err != nil {
		t.Fatalf("não consegui ler o domain.ds do wallet: %v", err)
	}
	file, bag := driver.CheckSource(string(src))
	if bag.HasErrors() {
		t.Fatalf("wallet/domain.ds tem diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}

	vos := make(map[string]*ast.ValueObjectDecl)
	for _, d := range file.Decls {
		if vo, ok := d.(*ast.ValueObjectDecl); ok {
			vos[vo.Name] = vo
		}
	}
	return vos
}

// TestLowerVOBinaryDispatchVOOperatorDeclared cobre o ramo (a) de §design
// 4.2: Money declara Operator >= (Gte); dispatch sobre dois operandos Money
// vira chamada de método.
func TestLowerVOBinaryDispatchVOOperatorDeclared(t *testing.T) {
	vos := parseWalletVOs(t)
	money, ok := vos["Money"]
	if !ok {
		t.Fatal("ValueObject Money não encontrado em wallet/domain.ds")
	}

	reg := goname.NewVOOperatorRegistry()
	reg.Register(money)

	got, err := goname.LowerVOBinaryDispatch(reg, token.GE, "m.Amount", "Money", "other.Amount", "Money")
	if err != nil {
		t.Fatalf("LowerVOBinaryDispatch: erro inesperado: %v", err)
	}
	want := "m.Amount.Gte(other.Amount)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestLowerVOBinaryDispatchVOEqualityWithoutOperatorIsNative é exatamente o
// critério de conclusão da task E3.2: ActiveStatus não declara nenhum
// Operator (só Valid); "state.active == ActiveStatus(true)" vira "=="
// nativo, não uma chamada de método inexistente.
func TestLowerVOBinaryDispatchVOEqualityWithoutOperatorIsNative(t *testing.T) {
	vos := parseWalletVOs(t)
	activeStatus, ok := vos["ActiveStatus"]
	if !ok {
		t.Fatal("ValueObject ActiveStatus não encontrado em wallet/domain.ds")
	}
	if len(activeStatus.Operators) != 0 {
		t.Fatalf("pré-condição do teste: ActiveStatus não deveria declarar Operators, tem %d", len(activeStatus.Operators))
	}

	reg := goname.NewVOOperatorRegistry()
	reg.Register(activeStatus)

	got, err := goname.LowerVOBinaryDispatch(reg, token.EQ, "state.Active", "ActiveStatus", "ActiveStatus(true)", "ActiveStatus")
	if err != nil {
		t.Fatalf("LowerVOBinaryDispatch: erro inesperado: %v", err)
	}
	want := "state.Active == ActiveStatus(true)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestLowerVOBinaryDispatchVOArithmeticWithoutOperatorIsError cobre o ramo
// (d): um VO fictício, sem nenhum Operator declarado (nem sequer Registered),
// não tem método para um operador aritmético — erro de geração, não Go
// inventado.
func TestLowerVOBinaryDispatchVOArithmeticWithoutOperatorIsError(t *testing.T) {
	reg := goname.NewVOOperatorRegistry()

	_, err := goname.LowerVOBinaryDispatch(reg, token.PLUS, "a", "Widget", "b", "Widget")
	if err == nil {
		t.Fatal("esperava erro de geração: Widget não declara Operator + e não é ==/!=")
	}
}

// TestLowerVOBinaryDispatchPrimitivesAreNativeWithoutRegistry prova o ramo
// (b): dois primitivos dispensam completamente o registry — nem Register é
// chamado.
func TestLowerVOBinaryDispatchPrimitivesAreNativeWithoutRegistry(t *testing.T) {
	reg := goname.NewVOOperatorRegistry()

	got, err := goname.LowerVOBinaryDispatch(reg, token.EQ, "a", "string", "b", "string")
	if err != nil {
		t.Fatalf("LowerVOBinaryDispatch: erro inesperado: %v", err)
	}
	want := "a == b"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
