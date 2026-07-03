package driver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen"
)

// generate_e2e_wallet_test.go prova os critérios de conclusão da task E10.2
// (§design codegen 5, DoD §5.2/5.3/5.4, NFR-12/13/14) — o smoke end-to-end
// que fecha o Marco E: gera o wallet real usando SÓ a API pública que `dsc
// gen` usa (GenerateProject, não codegen.Generate() in-memory como os demais
// *_test.go do pacote codegen), escreve em disco, compila/vet a saída DE
// VERDADE, roda um teste comportamental sobre o Go de fato gerado, confere
// go.mod sem require e regen byte-idêntico entre duas saídas distintas.
//
// Achado gap-de-domínio (idêntico ao já documentado em
// codegen/decl_aggregate_load_test.go:walletAggregateLoadBehaviorTest e
// codegen/http_test.go:TestGenerateWalletHTTPBehavior — não corrigido aqui,
// de propósito, ver design.md codegen §6 "Fixtures de exemplo não são fonte
// de verdade"): LoadWallet SEMPRE começa de um *Wallet zero-value
// (w := &Wallet{id: id}); nenhum Apply WalletCreated existe no domain.ds real
// para ligar state.active. Logo NENHUM Handle do wallet pode suceder via
// replay puro de eventos vindos do EventStore — Deposit/Withdraw retornam
// ErrInactiveWallet sempre. Por isso o teste comportamental abaixo usa as
// DUAS técnicas que a própria task pede: "given" (evento isolado seedado
// direto no EventStore, usado para provar que PerformDeposit/LoadWallet
// disputam corretamente sobre o Go gerado, com o resultado determinístico já
// documentado) e "construção completa" (o Wallet montado diretamente — mesma
// técnica de newTestWallet em codegen/decl_aggregate_test.go — usada para
// provar um evento efetivamente EMITIDO e depois persistido/recarregado via
// o runtime real).

// generateWalletE2EProject roda GenerateProject sobre o wallet real
// (docs/examples/wallet), escrevendo em um diretório temporário isolado — o
// MESMO caminho que `dsc gen <dir> -o <out>` percorre (REQ-32, E10.1),
// diferente do codegen.Generate() in-memory que codegen_test.go (E9.1) e o
// resto do pacote codegen exercitam. Devolve o diretório de saída.
func generateWalletE2EProject(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	bag, err := GenerateProject(walletExampleDir, out, codegen.Options{})
	if err != nil {
		t.Fatalf("GenerateProject: erro inesperado sobre o wallet real: %v", err)
	}
	if bag.HasErrors() {
		t.Fatalf("wallet não deveria ter diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}
	return out
}

// runGoOverDir roda `go <args...>` com cwd=dir e falha o teste com a saída
// combinada em caso de erro. Mesmo espírito de gentest.SmokeCompile
// (codegen/gentest/smoke.go) e runGeneratedTests
// (codegen/decl_aggregate_load_test.go), mas operando sobre um diretório JÁ
// ESCRITO em disco por GenerateProject — não um map[string][]byte recriado
// num temp dir próprio: a distinção que esta task exige (o artefato real da
// CLI, não o Generate() in-memory).
func runGoOverDir(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("`go %s` falhou em %q: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return out
}

// TestGenerateWalletE2ESmokeCompile prova a 1ª/2ª parte do critério de
// conclusão da task (DoD §5.2, NFR-14): `go build ./...` e `go vet ./...`
// verdes sobre a saída REAL escrita em disco por GenerateProject — o
// artefato que um usuário final obtém rodando `dsc gen`.
func TestGenerateWalletE2ESmokeCompile(t *testing.T) {
	out := generateWalletE2EProject(t)
	runGoOverDir(t, out, "build", "./...")
	runGoOverDir(t, out, "vet", "./...")
}

// TestGenerateWalletE2EGoModHasNoExternalRequire prova a 4ª parte do
// critério de conclusão (NFR-12): o go.mod gerado não tem NENHUM bloco
// require — o núcleo transacional do Marco E depende só da stdlib Go e do
// runtime vendorado (EmitGoMod, codegen/project.go, sempre "module .../go
// X.Y", nunca "require").
func TestGenerateWalletE2EGoModHasNoExternalRequire(t *testing.T) {
	out := generateWalletE2EProject(t)
	content, err := os.ReadFile(filepath.Join(out, "go.mod"))
	if err != nil {
		t.Fatalf("não consegui ler go.mod: %v", err)
	}
	if strings.Contains(string(content), "require") {
		t.Fatalf("go.mod não deveria conter \"require\" (NFR-12 — zero dep externa):\n%s", content)
	}
}

// TestGenerateWalletE2ERegenTwoDirsByteIdentical prova a 5ª parte do
// critério de conclusão (DoD §5.3, NFR-13): duas gerações INDEPENDENTES do
// wallet real, cada uma no seu próprio diretório de saída, produzem
// exatamente os mesmos arquivos (mesmos caminhos, mesmos bytes). Redundante
// com TestGenerateProjectIdempotentSameBytes (generate_test.go, E10.1, que
// prova uma propriedade ainda mais forte — regenerar no MESMO out não
// reescreve um arquivo inalterado), mas mantido separado para rastrear o
// critério desta task isoladamente (mesmo padrão de
// TestGenerateWalletHTTPSmokeCompile, codegen/http_test.go) e para cobrir o
// ângulo específico que a task pede: duas saídas DIFERENTES.
func TestGenerateWalletE2ERegenTwoDirsByteIdentical(t *testing.T) {
	out1 := filepath.Join(t.TempDir(), "out1")
	out2 := filepath.Join(t.TempDir(), "out2")

	if _, err := GenerateProject(walletExampleDir, out1, codegen.Options{}); err != nil {
		t.Fatalf("1ª geração: erro inesperado: %v", err)
	}
	if _, err := GenerateProject(walletExampleDir, out2, codegen.Options{}); err != nil {
		t.Fatalf("2ª geração: erro inesperado: %v", err)
	}

	snap1 := snapshotDir(t, out1)
	snap2 := snapshotDir(t, out2)

	if len(snap1) != len(snap2) {
		t.Fatalf("número de arquivos difere entre gerações: %d vs %d\n1ª: %v\n2ª: %v", len(snap1), len(snap2), keys(snap1), keys(snap2))
	}
	for rel, content := range snap1 {
		got, ok := snap2[rel]
		if !ok {
			t.Fatalf("%q presente na 1ª geração e ausente na 2ª", rel)
		}
		if string(got) != string(content) {
			t.Fatalf("conteúdo de %q difere entre as duas gerações", rel)
		}
	}
}

// TestGenerateWalletE2EBehavior prova a 3ª parte do critério de conclusão
// (o teste comportamental in-memory, "sem subir socket"): escreve
// walletE2EBehaviorTest dentro do pacote wallet do projeto isolado gerado em
// disco e roda `go test ./...` de verdade sobre ele — não uma
// reimplementação, o Go de fato gerado pela CLI.
func TestGenerateWalletE2EBehavior(t *testing.T) {
	out := generateWalletE2EProject(t)
	path := filepath.Join(out, "wallet", "e2e_behavior_test.go")
	if err := os.WriteFile(path, []byte(walletE2EBehaviorTest), 0o644); err != nil {
		t.Fatalf("não consegui escrever %q: %v", path, err)
	}
	runGoOverDir(t, out, "test", "./...")
}

// walletE2EBehaviorTest roda DENTRO do pacote wallet do projeto isolado
// gerado (mesmo pacote de usecases.go/aggregate_wallet.go — acessa
// diretamente identificadores não-exportados como o campo "state" de
// Wallet, mesma técnica de newTestWallet em
// codegen/decl_aggregate_test.go):
//
//   - TestE2ESeedGivenEventThenPerformDepositWiresThroughRealStore usa
//     "given" (evento isolado): grava um DepositPerformed direto no
//     EventStore como história prévia da carteira, depois executa o UseCase
//     PerformDeposit de verdade (Command -> uow.Run -> LoadWallet -> Handle
//     Deposit). LoadWallet replayar o evento seedado SEM cair no branch
//     `default` (que devolve erro para tipo de evento inesperado) já prova
//     que o given tomou efeito. O resultado é ErrInactiveWallet — o gap de
//     domínio documentado no cabeçalho deste arquivo, não um bug desta task.
//
//   - TestE2EFullConstructionEmitsAndPersistsDepositPerformed usa
//     "construção completa" (fluxo): monta um Wallet ATIVO diretamente
//     (LoadWallet nunca alcança state.active==true por replay puro — ver
//     acima), chama o Handle Deposit exportado — o MESMO método que
//     PerformDeposit despacha internamente
//     (usecases_wallet.go.golden: "wallet.Deposit(caller, cmd.Amount,
//     cmd.Description)") — confere o evento *DepositPerformed emitido com o
//     Amount correto, e então persiste esse evento via tx.Append (a mesma
//     chamada que o corpo de PerformDeposit faz) e recarrega via LoadWallet
//     para provar que runtime + Aggregate gerado fecham o ciclo completo:
//     emitir -> persistir -> replay.
const walletE2EBehaviorTest = `package wallet

import (
	"context"
	"errors"
	"testing"

	"domainscript/generated/runtime"
)

// e2eCaller é um runtime.Caller mínimo para os testes E2E — mesmo padrão de
// stubCaller (decl_aggregate_test.go/http_test.go do pacote codegen).
type e2eCaller struct {
	authenticated bool
	id            string
}

func (c e2eCaller) Authenticated() bool { return c.authenticated }
func (c e2eCaller) ID() string          { return c.id }
func (c e2eCaller) HasRole(string) bool { return false }

func e2eDecimal(t *testing.T, s string) runtime.Decimal {
	t.Helper()
	d, err := runtime.ParseDecimal(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// e2eMoney constrói um Money com moeda "" de propósito (não "BRL"): o zero-
// value de Money (o Balance com que LoadWallet SEMPRE começa, sem Apply
// WalletCreated) tem Currency=="" — o 1º Apply de qualquer replay-do-zero
// precisa da MESMA moeda para não panicar com CurrencyMismatch dentro de
// applyDepositPerformed (Operator + exige currency==other.currency). Mesmo
// achado documentado em
// codegen/decl_aggregate_load_test.go:walletAggregateLoadBehaviorTest.
func e2eMoney(t *testing.T, amount string) Money {
	t.Helper()
	m, err := NewMoney(e2eDecimal(t, amount), "")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestE2ESeedGivenEventThenPerformDepositWiresThroughRealStore(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	localUow := runtime.NewUnitOfWork(store)
	ctx := context.Background()

	// given: um evento isolado gravado direto no store (não via UseCase) —
	// a história prévia da carteira "W-given".
	seedDesc, err := NewTransactionDescription("saldo inicial")
	if err != nil {
		t.Fatal(err)
	}
	err = localUow.Run(ctx, func(tx runtime.Tx) error {
		return tx.Append("W-given", []runtime.Event{
			&DepositPerformed{Id: WalletId("W-given"), Amount: e2eMoney(t, "50.00"), Description: seedDesc},
		})
	})
	if err != nil {
		t.Fatalf("seed via given: %v", err)
	}

	Wire(localUow)

	cmd := Deposit{
		WalletId:    WalletId("W-given"),
		Amount:      e2eMoney(t, "10.00"),
		Description: seedDesc,
	}
	ctx = runtime.WithCaller(ctx, e2eCaller{authenticated: true, id: "W-given"})

	if err := PerformDeposit(ctx, cmd); !errors.Is(err, ErrInactiveWallet) {
		t.Fatalf("PerformDeposit = %v, want ErrInactiveWallet (LoadWallet replayou o evento given sem erro de tipo inesperado; InactiveWallet é o gap de domínio documentado, não falha de wiring)", err)
	}
}

func TestE2EFullConstructionEmitsAndPersistsDepositPerformed(t *testing.T) {
	holder, err := NewHolderName("Ana")
	if err != nil {
		t.Fatal(err)
	}

	// construção completa: monta o Wallet direto, ativo — sem passar por
	// LoadWallet, que nunca alcançaria active==true neste domínio (ver doc
	// do arquivo).
	w := &Wallet{
		id: WalletId("W-flow"),
		state: walletState{
			Id:     WalletId("W-flow"),
			Active: ActiveStatus(true),
			Holder: holder,
		},
	}

	amount := e2eMoney(t, "10.00")
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}

	events, err := w.Deposit(e2eCaller{authenticated: true, id: "W-flow"}, amount, desc)
	if err != nil {
		t.Fatalf("Deposit não deveria falhar sobre uma carteira ativa construída diretamente: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("esperava 1 evento emitido, got %d", len(events))
	}
	ev, ok := events[0].(*DepositPerformed)
	if !ok {
		t.Fatalf("esperava *DepositPerformed, got %T", events[0])
	}
	if ev.Amount.Amount.Cmp(e2eDecimal(t, "10.00")) != 0 {
		t.Fatalf("Amount incorreto: got %s, want 10.00", ev.Amount.Amount)
	}
	if ev.Id != WalletId("W-flow") {
		t.Fatalf("Id incorreto: got %v, want W-flow", ev.Id)
	}

	// Persiste o evento emitido no store real (a mesma chamada que o corpo
	// de PerformDeposit faz: tx.Append(string(wallet.id), events)) e
	// recarrega via LoadWallet — fecha o ciclo emitir -> persistir ->
	// replay sobre o runtime de fato gerado.
	store := runtime.NewMemoryEventStore()
	localUow := runtime.NewUnitOfWork(store)
	ctx := context.Background()

	if err := localUow.Run(ctx, func(tx runtime.Tx) error {
		return tx.Append(string(w.id), events)
	}); err != nil {
		t.Fatalf("tx.Append: %v", err)
	}

	var reloaded *Wallet
	if err := localUow.Run(ctx, func(tx runtime.Tx) error {
		loaded, err := LoadWallet(tx, WalletId("W-flow"))
		if err != nil {
			return err
		}
		reloaded = loaded
		return nil
	}); err != nil {
		t.Fatalf("LoadWallet: %v", err)
	}
	if reloaded.state.Balance.Amount.Cmp(e2eDecimal(t, "10.00")) != 0 {
		t.Fatalf("Balance recarregado incorreto: got %s, want 10.00 (o evento emitido deveria ter sido replayado)", reloaded.state.Balance.Amount)
	}
}
`
