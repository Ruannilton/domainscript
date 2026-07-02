package codegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/goname"
	"domainscript/codegen/rtsrc"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/types"
)

// decl_aggregate_test.go prova os critérios de conclusão da task E6.1
// (§design codegen 3.7, REQ-19.1/2/3/6) sobre o Aggregate Wallet real
// (docs/examples/wallet): golden, determinismo, smoke compile e um teste
// comportamental que exercita Handle/access/Apply sobre o Go de fato gerado.

// walletProjectDir é o exemplo de referência (projeto multi-arquivo), usado
// aqui (ao contrário de driver.CheckSource sobre um único domain.ds, como as
// outras *_test.go deste pacote) porque EmitAggregate precisa de um
// types.Model + symbols.SymbolTable RESOLVIDOS sobre o programa — a mesma
// necessidade de codegen/lower (env_test.go:buildWalletEnv).
var walletProjectDir = filepath.Join("..", "docs", "examples", "wallet")

// parseWalletProgram carrega o projeto wallet completo via driver.CheckProject.
func parseWalletProgram(t *testing.T) *program.Program {
	t.Helper()
	prog, bag := driver.CheckProject(walletProjectDir)
	if bag.HasErrors() {
		t.Fatalf("wallet não deveria ter diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}
	return prog
}

// findAggregateDecl acha o *ast.AggregateDecl de nome name em qualquer
// arquivo do programa.
func findAggregateDecl(t *testing.T, prog *program.Program, name string) *ast.AggregateDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if agg, ok := d.(*ast.AggregateDecl); ok && agg.Name == name {
				return agg
			}
		}
	}
	t.Fatalf("Aggregate %q não encontrado no wallet — o exemplo mudou?", name)
	return nil
}

// walletVOOperatorRegistryFromProgram registra TODO ValueObject do programa —
// o mesmo que o driver de geração real faria antes de lowerizar qualquer
// corpo (§design 4.2): sem isso, "state.balance + event.amount" (Handle
// Apply) e "state.balance >= amount" (Handle Withdraw) não achariam o
// Operator declarado de Money.
func walletVOOperatorRegistryFromProgram(prog *program.Program) *goname.VOOperatorRegistry {
	reg := goname.NewVOOperatorRegistry()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if vo, ok := d.(*ast.ValueObjectDecl); ok {
				reg.Register(vo)
			}
		}
	}
	return reg
}

// emitWalletAggregate monta o Model+SymbolTable sobre o programa real e gera
// o Go do Aggregate Wallet — o caminho comum a todos os testes deste arquivo.
func emitWalletAggregate(t *testing.T) []byte {
	t.Helper()
	prog := parseWalletProgram(t)
	agg := findAggregateDecl(t, prog, "Wallet")
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)

	got, err := codegen.EmitAggregate("wallet", agg, model, prog.Symbols, "Wallet", reg)
	if err != nil {
		t.Fatalf("EmitAggregate: erro inesperado: %v", err)
	}
	return got
}

// TestEmitAggregateGolden gera o Go do Aggregate Wallet real e compara byte a
// byte com o artefato versionado (REQ-19.1/2/3/6).
func TestEmitAggregateGolden(t *testing.T) {
	got := emitWalletAggregate(t)
	gentest.Golden(t, filepath.Join("testdata", "aggregate_wallet.go.golden"), got)
}

// TestEmitAggregateDeterministic prova NFR-13: gerar o mesmo Aggregate duas
// vezes produz bytes idênticos.
func TestEmitAggregateDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitWalletAggregate(t)
	})
}

// TestEmitAggregateMissingAccessRuleFailsExplicitly prova o caso defensivo
// (§design 4.1/REQ-14.4): um Handle sem entrada correspondente em access não
// deveria acontecer sobre um programa validado (REQ-5 já barra isso), mas o
// gerador se defende com um erro claro em vez de gerar um access sempre-falso
// ou entrar em pânico.
func TestEmitAggregateMissingAccessRuleFailsExplicitly(t *testing.T) {
	prog := parseWalletProgram(t)
	agg := findAggregateDecl(t, prog, "Wallet")
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)

	// Cópia rasa do AggregateDecl com Access esvaziado — simula um programa
	// (hipoteticamente) inválido chegando ao gerador.
	broken := ast.NewAggregateDecl(agg.Name, agg.Strategy, agg.Snapshot, agg.Storage, agg.State, nil, agg.Handlers, agg.Appliers, agg.Span())

	if _, err := codegen.EmitAggregate("wallet", broken, model, prog.Symbols, "Wallet", reg); err == nil {
		t.Fatal("esperava erro de geração para Handle sem entrada em access")
	}
}

// aggregateSmokeFiles monta o conjunto completo de arquivos para o smoke
// compile e o teste de comportamento: go.mod, o runtime vendorado real
// (rtsrc), TODOS os VOs/Enum/Errors/Events reais do wallet (via os emissores
// já existentes, E3/E4) e o Aggregate gerado por esta task.
func aggregateSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog := parseWalletProgram(t)
	agg := findAggregateDecl(t, prog, "Wallet")
	reg := walletVOOperatorRegistryFromProgram(prog)
	model := types.NewModel(prog.Symbols)

	vos := parseWalletVOs(t)
	enums := parseWalletEnums(t)
	errs := parseWalletErrors(t)
	events := parseWalletEvents(t)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	// Os 6 VOs que o state do Wallet e seus eventos referenciam (§4.5): id,
	// balance, active, holder, entries -- e o payload dos 3 Events.
	voFiles := []struct{ name, file string }{
		{"WalletId", "wallet_id.go"},
		{"HolderName", "holder_name.go"},
		{"TransactionDescription", "transaction_description.go"},
		{"ActiveStatus", "active_status.go"},
		{"Money", "money.go"},
		{"StatementEntry", "statement_entry.go"},
	}
	for _, spec := range voFiles {
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

	enumDecl, ok := enums["TransactionType"]
	if !ok {
		t.Fatal("Enum TransactionType não encontrado em wallet/domain.ds")
	}
	enumGo, err := codegen.EmitEnum("wallet", enumDecl)
	if err != nil {
		t.Fatalf("EmitEnum(TransactionType): erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "enums.go")] = enumGo

	errsGo, err := codegen.EmitErrors("wallet", errs)
	if err != nil {
		t.Fatalf("EmitErrors: erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "errors.go")] = errsGo

	eventsGo, err := codegen.EmitEvents("wallet", events)
	if err != nil {
		t.Fatalf("EmitEvents: erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "events.go")] = eventsGo

	aggGo, err := codegen.EmitAggregate("wallet", agg, model, prog.Symbols, "Wallet", reg)
	if err != nil {
		t.Fatalf("EmitAggregate: erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "aggregate_wallet.go")] = aggGo

	return files
}

// TestEmitAggregateSmokeCompile prova NFR-14: o Go gerado do Aggregate
// Wallet, junto de todo o restante do módulo (VOs/Enum/Errors/Events) e do
// runtime vendorado real, compila e passa go vet num projeto isolado.
func TestEmitAggregateSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, aggregateSmokeFiles(t))
}

// walletAggregateBehaviorTest roda dentro do projeto isolado gerado no smoke
// e prova, sobre o Go de fato gerado (não uma reimplementação): (a) Deposit
// autenticado com wallet ativo emite DepositPerformed e não erra; (b) Deposit
// não-autenticado falha com runtime.ErrForbidden (access fechado-por-padrão);
// (c) Deposit sobre wallet inativo falha com ErrInactiveWallet (o corpo do
// Handle, além do access); (d) Withdraw exercita o caso especial "caller.id ==
// self.id" (§7 do prompt da task): caller autenticado mas com id diferente do
// aggregate falha Forbidden, caller com o MESMO id sucede; (e) saldo
// insuficiente falha ErrInsufficientBalance; (f) applyDepositPerformed muta
// state.Balance/state.Entries diretamente (NFR-15).
const walletAggregateBehaviorTest = `package wallet

import (
	"errors"
	"testing"

	"domainscript/generated/runtime"
)

// stubCaller é um runtime.Caller mínimo para os testes de behavior — a borda
// HTTP real (E9.2) ainda não existe.
type stubCaller struct {
	authenticated bool
	id            string
}

func (c stubCaller) Authenticated() bool      { return c.authenticated }
func (c stubCaller) ID() string                { return c.id }
func (c stubCaller) HasRole(role string) bool { return false }

func mustDecimal(t *testing.T, s string) runtime.Decimal {
	t.Helper()
	d, err := runtime.ParseDecimal(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// newTestWallet constrói um *Wallet "manualmente" (populando state/id
// direto), sem passar por LoadWallet (E6.2, ainda não implementada) — a
// mesma ressalva do prompt da task.
func newTestWallet(t *testing.T, active bool) *Wallet {
	t.Helper()
	balance, err := NewMoney(mustDecimal(t, "100.00"), "BRL")
	if err != nil {
		t.Fatal(err)
	}
	holder, err := NewHolderName("Ana")
	if err != nil {
		t.Fatal(err)
	}
	activeStatus, err := NewActiveStatus(active)
	if err != nil {
		t.Fatal(err)
	}
	return &Wallet{
		id: WalletId("W1"),
		state: walletState{
			Id:      WalletId("W1"),
			Balance: balance,
			Active:  activeStatus,
			Holder:  holder,
		},
	}
}

func TestDepositAuthenticatedActiveWalletSucceedsAndEmitsEvent(t *testing.T) {
	w := newTestWallet(t, true)
	amount, err := NewMoney(mustDecimal(t, "10.00"), "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}

	events, err := w.Deposit(stubCaller{authenticated: true, id: "W1"}, amount, desc)
	if err != nil {
		t.Fatalf("Deposit não deveria falhar: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("esperava 1 evento, got %d", len(events))
	}
	ev, ok := events[0].(*DepositPerformed)
	if !ok {
		t.Fatalf("esperava *DepositPerformed, got %T", events[0])
	}
	if ev.Amount.Amount.Cmp(mustDecimal(t, "10.00")) != 0 {
		t.Fatalf("Amount incorreto: got %s", ev.Amount.Amount)
	}
	if ev.Id != WalletId("W1") {
		t.Fatalf("Id incorreto: got %v", ev.Id)
	}
}

func TestDepositUnauthenticatedFailsForbidden(t *testing.T) {
	w := newTestWallet(t, true)
	amount, err := NewMoney(mustDecimal(t, "10.00"), "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.Deposit(stubCaller{authenticated: false}, amount, desc); !errors.Is(err, runtime.ErrForbidden) {
		t.Fatalf("esperava runtime.ErrForbidden, got %v", err)
	}
}

func TestDepositInactiveWalletFails(t *testing.T) {
	w := newTestWallet(t, false)
	amount, err := NewMoney(mustDecimal(t, "10.00"), "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.Deposit(stubCaller{authenticated: true, id: "W1"}, amount, desc); !errors.Is(err, ErrInactiveWallet) {
		t.Fatalf("esperava ErrInactiveWallet, got %v", err)
	}
}

func TestWithdrawRequiresCallerIdEqualsSelfId(t *testing.T) {
	w := newTestWallet(t, true)
	amount, err := NewMoney(mustDecimal(t, "10.00"), "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("saque")
	if err != nil {
		t.Fatal(err)
	}

	// caller.id ("OTHER") != self.id ("W1"): Forbidden mesmo autenticado —
	// prova o caso especial "WalletId(caller.ID()) == w.state.Id" (§7).
	if _, err := w.Withdraw(stubCaller{authenticated: true, id: "OTHER"}, amount, desc); !errors.Is(err, runtime.ErrForbidden) {
		t.Fatalf("esperava runtime.ErrForbidden (caller.id != self.id), got %v", err)
	}

	events, err := w.Withdraw(stubCaller{authenticated: true, id: "W1"}, amount, desc)
	if err != nil {
		t.Fatalf("Withdraw com caller.id == self.id não deveria falhar: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("esperava 1 evento, got %d", len(events))
	}
	if _, ok := events[0].(*WithdrawalPerformed); !ok {
		t.Fatalf("esperava *WithdrawalPerformed, got %T", events[0])
	}
}

func TestWithdrawInsufficientBalanceFails(t *testing.T) {
	w := newTestWallet(t, true)
	amount, err := NewMoney(mustDecimal(t, "1000.00"), "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("saque grande")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := w.Withdraw(stubCaller{authenticated: true, id: "W1"}, amount, desc); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("esperava ErrInsufficientBalance, got %v", err)
	}
}

func TestApplyDepositPerformedMutatesState(t *testing.T) {
	w := newTestWallet(t, true)
	amount, err := NewMoney(mustDecimal(t, "10.00"), "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}
	ev := DepositPerformed{Id: WalletId("W1"), Amount: amount, Description: desc}

	w.applyDepositPerformed(ev)

	want := mustDecimal(t, "110.00")
	if w.state.Balance.Amount.Cmp(want) != 0 {
		t.Fatalf("Balance incorreto após apply: got %s, want %s", w.state.Balance.Amount, want)
	}
	entries := w.state.Entries.Items()
	if len(entries) != 1 {
		t.Fatalf("esperava 1 entry em state.Entries, got %d", len(entries))
	}
	if entries[0].Type != TransactionTypeDeposit {
		t.Fatalf("Type incorreto: got %v, want %v", entries[0].Type, TransactionTypeDeposit)
	}
}

func TestApplyWithdrawalPerformedMutatesState(t *testing.T) {
	w := newTestWallet(t, true)
	amount, err := NewMoney(mustDecimal(t, "10.00"), "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("saque")
	if err != nil {
		t.Fatal(err)
	}
	ev := WithdrawalPerformed{Id: WalletId("W1"), Amount: amount, Description: desc}

	w.applyWithdrawalPerformed(ev)

	want := mustDecimal(t, "90.00")
	if w.state.Balance.Amount.Cmp(want) != 0 {
		t.Fatalf("Balance incorreto após apply: got %s, want %s", w.state.Balance.Amount, want)
	}
	entries := w.state.Entries.Items()
	if len(entries) != 1 {
		t.Fatalf("esperava 1 entry em state.Entries, got %d", len(entries))
	}
	if entries[0].Type != TransactionTypeWithdrawal {
		t.Fatalf("Type incorreto: got %v, want %v", entries[0].Type, TransactionTypeWithdrawal)
	}
}
`

// TestEmitAggregateBehavior prova NFR-15 sobre o Go de fato gerado: escreve
// os mesmos arquivos do smoke mais o teste Go comportamental acima num
// diretório isolado e roda `go test ./...` de verdade.
func TestEmitAggregateBehavior(t *testing.T) {
	files := aggregateSmokeFiles(t)
	files[filepath.Join("wallet", "aggregate_behavior_test.go")] = []byte(walletAggregateBehaviorTest)

	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("não consegui criar %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("não consegui escrever %q: %v", path, err)
		}
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("`go test ./...` falhou em %q: %v\n%s", dir, err, out)
	}
}
