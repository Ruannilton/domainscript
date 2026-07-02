package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/rtsrc"
	"domainscript/program"
	"domainscript/types"
)

// decl_view_test.go prova os critérios de conclusão da task E8.1 para View
// (§design codegen 3.9, REQ-21.1) sobre a View WalletView real
// (docs/examples/wallet/read.ds): golden, determinismo e smoke compile.

// findViewDecl acha o *ast.ViewDecl de nome name em qualquer arquivo do
// programa — espelha findAggregateDecl (decl_aggregate_test.go).
func findViewDecl(t *testing.T, prog *program.Program, name string) *ast.ViewDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if v, ok := d.(*ast.ViewDecl); ok && v.Name == name {
				return v
			}
		}
	}
	t.Fatalf("View %q não encontrada no wallet — o exemplo mudou?", name)
	return nil
}

// emitWalletView monta o Model+SymbolTable sobre o programa real e gera o Go
// da View WalletView (campos próprios: id/balance/holder — read.ds não usa
// "From Aggregate").
func emitWalletView(t *testing.T) []byte {
	t.Helper()
	prog := parseWalletProgram(t)
	view := findViewDecl(t, prog, "WalletView")
	model := types.NewModel(prog.Symbols)

	got, err := codegen.EmitView("wallet", view, model, prog.Symbols, "Wallet")
	if err != nil {
		t.Fatalf("EmitView: erro inesperado: %v", err)
	}
	return got
}

// TestEmitViewGolden gera o Go de WalletView e compara byte a byte com o
// artefato versionado — confirma o struct de leitura pura (campos exportados
// + tag json, sem validação, sem metadata).
func TestEmitViewGolden(t *testing.T) {
	got := emitWalletView(t)
	for _, want := range []string{
		"type WalletView struct",
		"Id      WalletId   `json:\"id\"`",
		"Balance Money      `json:\"balance\"`",
		"Holder  HolderName `json:\"holder\"`",
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("esperava a presença de %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "view_wallet.go.golden"), got)
}

// TestEmitViewDeterministic prova NFR-13.
func TestEmitViewDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitWalletView(t)
	})
}

// walletViewSmokeFiles monta os arquivos mínimos para compilar WalletView
// isoladamente: go.mod + runtime real + os 3 VOs que ela referencia (WalletId/
// HolderName/Money — Money precisa também dos Errors que seus Operators
// referenciam, via walletVOOperatorErrorsStub, já definido em
// decl_value_test.go/E3.2, mesmo pacote codegen_test) + o Go da View.
func walletViewSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	vos := parseWalletVOs(t)

	files := map[string][]byte{
		"go.mod": []byte(smokeGoMod),
		filepath.Join("wallet", "errors_stub.go"): []byte(walletVOOperatorErrorsStub),
	}
	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	for _, spec := range []struct{ name, file string }{
		{"WalletId", "wallet_id.go"},
		{"HolderName", "holder_name.go"},
		{"Money", "money.go"},
	} {
		decl, ok := vos[spec.name]
		if !ok {
			t.Fatalf("ValueObject %s não encontrado em wallet/domain.ds", spec.name)
		}
		got, err := codegen.EmitValueObject("wallet", decl)
		if err != nil {
			t.Fatalf("EmitValueObject(%s): erro inesperado: %v", spec.name, err)
		}
		files[filepath.Join("wallet", spec.file)] = got
	}

	files[filepath.Join("wallet", "view_wallet.go")] = emitWalletView(t)
	return files
}

// TestEmitViewSmokeCompile prova NFR-14: o Go de WalletView, junto dos VOs
// que referencia e do runtime vendorado real, compila e passa go vet num
// projeto isolado.
func TestEmitViewSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, walletViewSmokeFiles(t))
}
