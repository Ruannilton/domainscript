package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/program"
	"domainscript/types"
)

// gentest_test.go prova os critérios de conclusão da fase 1 de H4 (§design
// codegen 3.14, REQ-31.1): um Test cujo Name resolve a um Aggregate deste
// módulo (§22.1) vira um arquivo Go de teste que roda `go test` verde sobre o
// wallet.test.ds REAL (docs/examples/wallet) — o alvo de conclusão nomeado
// pela própria task H4 (tasks.md).

// findTestDecl acha o *ast.TestDecl de nome name em qualquer arquivo do
// programa.
func findTestDecl(t *testing.T, prog *program.Program, name string) *ast.TestDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if td, ok := d.(*ast.TestDecl); ok && td.Name == name {
				return td
			}
		}
	}
	t.Fatalf("Test %q não encontrado no wallet — o exemplo mudou?", name)
	return nil
}

// emitWalletTests gera o Go de wallet.test.ds (Test Wallet) sobre o programa
// real do wallet.
func emitWalletTests(t *testing.T) []byte {
	t.Helper()
	prog := parseWalletProgram(t)
	td := findTestDecl(t, prog, "Wallet")
	agg := findAggregateDecl(t, prog, "Wallet")
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)

	got, err := codegen.EmitTests("wallet", []*ast.TestDecl{td}, model, prog.Symbols, "Wallet", reg, map[string]*ast.AggregateDecl{"Wallet": agg})
	if err != nil {
		t.Fatalf("EmitTests: erro inesperado: %v", err)
	}
	return got
}

func TestEmitTestsWalletGolden(t *testing.T) {
	got := string(emitWalletTests(t))
	for _, want := range []string{
		"package wallet",
		"func TestWallet_DepositoNumaCarteiraAtiva(t *testing.T) {",
		"func TestWallet_DepositoNumaCarteiraInativaFalha(t *testing.T) {",
		"func TestWallet_SaqueComSaldoSuficiente(t *testing.T) {",
		"func TestWallet_SaqueComSaldoInsuficienteFalha(t *testing.T) {",
		"w := &Wallet{}",
		"w.applyWalletCreated(WalletCreated{",
		"runtime.NewTestCaller(string(w.state.Id))",
		"events, err := w.Deposit(",
		"events, err := w.Withdraw(",
		"errors.Is(err, ErrInactiveWallet)",
		"errors.Is(err, ErrInsufficientBalance)",
		"reflect.DeepEqual(events[0], want0)",
		"w.state.Active = false",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q no Go gerado de Test Wallet, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "tests_wallet.go.golden"), []byte(got))
}

func TestEmitTestsWalletDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitWalletTests(t)
	})
}

// walletTestsSmokeFiles estende aggregateSmokeFiles (decl_aggregate_test.go)
// com o Go gerado desta task (wallet_test.go) — o cenário de smoke prova que
// o teste NATIVO gerado compila e RODA (go test, não só go build) sobre o
// restante do módulo wallet real.
func walletTestsSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := aggregateSmokeFiles(t)
	files[filepath.Join("wallet", "wallet_test.go")] = emitWalletTests(t)
	return files
}

// TestEmitTestsWalletRunsGreen prova o alvo de conclusão de H4 (tasks.md):
// "wallet.test.ds gera _test.go que roda go test verde sobre o gerado —
// fidelidade semântica (NFR-15)". Roda `go test ./...` de verdade (via
// gentest.RunTests) sobre o Go gerado.
func TestEmitTestsWalletRunsGreen(t *testing.T) {
	runGeneratedTests(t, walletTestsSmokeFiles(t))
}
