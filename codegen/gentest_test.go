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

// findFixtureDecl acha o *ast.FixtureDecl de nome name em qualquer arquivo do
// programa.
func findFixtureDecl(t *testing.T, prog *program.Program, name string) *ast.FixtureDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if fx, ok := d.(*ast.FixtureDecl); ok && fx.Name == name {
				return fx
			}
		}
	}
	t.Fatalf("Fixture %q não encontrada no wallet — o exemplo mudou?", name)
	return nil
}

// emitWalletTests gera o Go de wallet.test.ds (Test Wallet, §22.1 + Test
// PerformDeposit, §22.2 + Fixture activeWallet, §22.6) sobre o programa real do
// wallet.
func emitWalletTests(t *testing.T) []byte {
	t.Helper()
	prog := parseWalletProgram(t)
	tdWallet := findTestDecl(t, prog, "Wallet")
	tdDeposit := findTestDecl(t, prog, "PerformDeposit")
	fxActive := findFixtureDecl(t, prog, "activeWallet")
	agg := findAggregateDecl(t, prog, "Wallet")
	uc := findUseCaseDecl(t, prog, "PerformDeposit")
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)

	got, err := codegen.EmitTests("wallet", []*ast.TestDecl{tdWallet, tdDeposit}, []*ast.FixtureDecl{fxActive}, model, prog.Symbols, "Wallet", reg, map[string]*ast.AggregateDecl{"Wallet": agg}, map[string]*ast.UseCaseDecl{"PerformDeposit": uc}, nil, nil)
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
		"func fixtureActiveWallet(t *testing.T) *Wallet {",
		"w.applyWalletCreated(WalletCreated{Id: \"W1\", Holder: \"João\"})",
		"w.applyDepositPerformed(DepositPerformed{Id: \"W1\", Amount:",
		"return w",
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

// walletFixtureBehaviorTest é um teste Go escrito à mão que CHAMA o helper
// gerado fixtureActiveWallet (§22.6, H4) — a gramática de §22 não liga uma
// Fixture a um Test, então o helper não tem chamador no projeto gerado; este
// caller de teste (mesmo espírito de walletAggregateBehaviorTest, E6.1) prova a
// corretude do que foi semeado: id/holder/active vêm do Apply de WalletCreated,
// balance acumula pelo Apply de DepositPerformed.
const walletFixtureBehaviorTest = `package wallet

import (
	"testing"

	"domainscript/generated/runtime"
)

func TestFixtureActiveWalletSeeds(t *testing.T) {
	w := fixtureActiveWallet(t)
	if string(w.state.Id) != "W1" {
		t.Fatalf("state.Id: got %q, want W1", string(w.state.Id))
	}
	if string(w.state.Holder) != "João" {
		t.Fatalf("state.Holder: got %q, want João", string(w.state.Holder))
	}
	if !bool(w.state.Active) {
		t.Fatalf("state.Active: esperava true (Apply de WalletCreated seta active)")
	}
	if w.state.Balance.Currency != "BRL" {
		t.Fatalf("state.Balance.Currency: got %q, want BRL", w.state.Balance.Currency)
	}
	if w.state.Balance.Amount.Cmp(runtime.NewDecimalFromInt(100)) != 0 {
		t.Fatalf("state.Balance.Amount: got %s, want 100 (Apply de DepositPerformed acumula)", w.state.Balance.Amount)
	}
}
`

// TestEmitFixturesWalletBehavior prova NFR-15 sobre o helper de Fixture de fato
// gerado: gera o projeto wallet INTEIRO (que já inclui fixtureActiveWallet em
// wallet_test.go via EmitTests), acrescenta um teste comportamental à mão que o
// chama e roda `go test ./...` de verdade sobre o gerado.
func TestEmitFixturesWalletBehavior(t *testing.T) {
	files := filesToMap(generateWalletProject(t))
	files[filepath.Join("wallet", "fixture_behavior_test.go")] = []byte(walletFixtureBehaviorTest)
	runGeneratedTests(t, files)
}
