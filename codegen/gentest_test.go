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

// findUseCaseDecl acha o *ast.UseCaseDecl de nome name em qualquer arquivo do
// programa.
func findUseCaseDecl(t *testing.T, prog *program.Program, name string) *ast.UseCaseDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if uc, ok := d.(*ast.UseCaseDecl); ok && uc.Name == name {
				return uc
			}
		}
	}
	t.Fatalf("UseCase %q não encontrado no wallet — o exemplo mudou?", name)
	return nil
}

// emitWalletTests gera o Go de wallet.test.ds (Test Wallet, §22.1 + Test
// PerformDeposit, §22.2) sobre o programa real do wallet.
func emitWalletTests(t *testing.T) []byte {
	t.Helper()
	prog := parseWalletProgram(t)
	tdWallet := findTestDecl(t, prog, "Wallet")
	tdDeposit := findTestDecl(t, prog, "PerformDeposit")
	agg := findAggregateDecl(t, prog, "Wallet")
	uc := findUseCaseDecl(t, prog, "PerformDeposit")
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)

	got, err := codegen.EmitTests("wallet", []*ast.TestDecl{tdWallet, tdDeposit}, model, prog.Symbols, "Wallet", reg, map[string]*ast.AggregateDecl{"Wallet": agg}, map[string]*ast.UseCaseDecl{"PerformDeposit": uc})
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
		"func TestPerformDeposit_DepositoBemSucedidoCommita(t *testing.T) {",
		"func TestPerformDeposit_CarteiraNuncaCriadaFalhaENaoCommita(t *testing.T) {",
		"store := runtime.NewMemoryEventStore()",
		"uow := runtime.NewUnitOfWork(store)",
		"Wire(uow)",
		"ctx := runtime.WithCaller(context.Background(), runtime.NewTestCaller(\"test-caller\"))",
		"err = PerformDeposit(ctx, Deposit{",
		"after1, after1Err := store.Load(context.Background(), \"W1\")",
		"if err != nil {",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q no Go gerado de Test Wallet/PerformDeposit, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "tests_wallet.go.golden"), []byte(got))
}

func TestEmitTestsWalletDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitWalletTests(t)
	})
}

// TestEmitTestsWalletRunsGreen prova o alvo de conclusão de H4 (tasks.md):
// "wallet.test.ds gera _test.go que roda go test verde sobre o gerado —
// fidelidade semântica (NFR-15)". Gera o projeto wallet INTEIRO (EmitTests já
// vem wired em codegen.go, ver generateModuleFiles) e roda `go test ./...`
// de verdade sobre ele — precisa do projeto completo (não só aggregateSmoke
// Files) porque Test PerformDeposit (§22.2) chama a função gerada do
// UseCase/Wire (usecases.go), não só o Aggregate.
func TestEmitTestsWalletRunsGreen(t *testing.T) {
	runGeneratedTests(t, filesToMap(generateWalletProject(t)))
}
