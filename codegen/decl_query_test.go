package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/goname"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/types"
)

// decl_query_test.go prova os critérios de conclusão da task E8.1 para Query
// (§design codegen 3.9, REQ-21.2/21.5) sobre as 2 Queries reais do wallet
// (docs/examples/wallet/read.ds): golden, determinismo, smoke compile e um
// teste comportamental — incl. a prova explícita, pedida pela task, de que a
// correlação de "list <VO>" por id não mistura dois wallets diferentes.

// findQueryDecl acha o *ast.QueryDecl de nome name em qualquer arquivo do
// programa — espelha findViewDecl/findAggregateDecl.
func findQueryDecl(t *testing.T, prog *program.Program, name string) *ast.QueryDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if q, ok := d.(*ast.QueryDecl); ok && q.Name == name {
				return q
			}
		}
	}
	t.Fatalf("Query %q não encontrada no wallet — o exemplo mudou?", name)
	return nil
}

// walletQueriesAndAggregates monta o Program+aggregates+reg+model comuns a
// todos os testes deste arquivo.
func walletQueriesAndAggregates(t *testing.T) (getWallet, listEntries *ast.QueryDecl, aggregates map[string]*ast.AggregateDecl, model *types.Model, reg *goname.VOOperatorRegistry, prog *program.Program) {
	t.Helper()
	prog = parseWalletProgram(t)
	agg := findAggregateDecl(t, prog, "Wallet")
	reg = walletVOOperatorRegistryFromProgram(prog)
	model = types.NewModel(prog.Symbols)
	aggregates = map[string]*ast.AggregateDecl{"Wallet": agg}
	getWallet = findQueryDecl(t, prog, "GetWallet")
	listEntries = findQueryDecl(t, prog, "ListEntries")
	return
}

// emitWalletQueries gera o Go das 2 Queries reais do wallet num único
// arquivo, na ordem de declaração de read.ds (GetWallet, ListEntries).
func emitWalletQueries(t *testing.T) []byte {
	t.Helper()
	getWallet, listEntries, aggregates, model, reg, prog := walletQueriesAndAggregates(t)

	got, err := codegen.EmitQueries("wallet", []*ast.QueryDecl{getWallet, listEntries}, aggregates, prog, model, prog.Symbols, "Wallet", reg, nil)
	if err != nil {
		t.Fatalf("EmitQueries: erro inesperado: %v", err)
	}
	return got
}

// TestEmitQueriesGolden gera o Go das 2 Queries reais do wallet e compara
// byte a byte com o artefato versionado — confirma os 3 elementos do
// critério de conclusão da task: LoadWallet sobre runtime.NewEventLoader
// (não runtime.Tx — GetWallet/ListEntries não abrem unit of work), o mapeamento
// campo-a-campo de "as WalletView" e ".Entries.Items()" da correlação de
// "list StatementEntry".
func TestEmitQueriesGolden(t *testing.T) {
	got := emitWalletQueries(t)
	for _, want := range []string{
		"func GetWallet(ctx context.Context, store runtime.EventStore, id WalletId) (WalletView, error)",
		"wallet, err := LoadWallet(runtime.NewEventLoader(ctx, store), id)",
		"return WalletView{Id: wallet.state.Id, Balance: wallet.state.Balance, Holder: wallet.state.Holder}, nil",
		"func ListEntries(ctx context.Context, store runtime.EventStore, id WalletId) ([]StatementEntry, error)",
		"return wallet.state.Entries.Items(), nil",
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("esperava a presença de %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "queries_wallet.go.golden"), got)
}

// TestEmitQueryGoldenSingle gera o Go de uma única Query (GetWallet) via
// EmitQuery e compara com um segundo artefato versionado — prova que a forma
// "uma de cada vez" também é suportada e estável (mesmo contrato de
// EmitUseCase/EmitUseCases).
func TestEmitQueryGoldenSingle(t *testing.T) {
	getWallet, _, aggregates, model, reg, prog := walletQueriesAndAggregates(t)

	got, err := codegen.EmitQuery("wallet", getWallet, aggregates, prog, model, prog.Symbols, "Wallet", reg)
	if err != nil {
		t.Fatalf("EmitQuery(GetWallet): erro inesperado: %v", err)
	}
	gentest.Golden(t, filepath.Join("testdata", "query_get_wallet.go.golden"), got)
}

// TestEmitQueriesDeterministic prova NFR-13.
func TestEmitQueriesDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitWalletQueries(t)
	})
}

// querySmokeFiles estende walletAggregateLoadSmokeFiles (decl_aggregate_
// load_test.go, E6.2 — go.mod, runtime real, VOs/Enum/Errors/Events/
// Aggregate/Load do wallet) com a View (E8.1) e as 2 Queries — o conjunto
// completo do read side do wallet.
func querySmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := walletAggregateLoadSmokeFiles(t)
	files[filepath.Join("wallet", "view_wallet.go")] = emitWalletView(t)
	files[filepath.Join("wallet", "queries.go")] = emitWalletQueries(t)
	return files
}

// TestEmitQueriesSmokeCompile prova NFR-14: GetWallet/ListEntries, junto de
// todo o restante do módulo wallet e do runtime vendorado real (incl. a
// extensão EventLoader/NewEventLoader desta task), compilam e passam go vet
// num projeto isolado — o critério de conclusão da task ("compilam e
// retornam o tipo declarado").
func TestEmitQueriesSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, querySmokeFiles(t))
}

// walletQueriesBehaviorTest roda dentro do projeto isolado gerado no smoke e
// prova, sobre o Go de fato gerado (não uma reimplementação): (a) GetWallet
// reconstrói via LoadWallet e projeta state -> WalletView corretamente; (b)
// GetWallet sobre um stream desconhecido devolve a View zero-value
// sincronizada em Id (mesmo gap documentado em decl_aggregate_load_test.go:
// LoadWallet sempre começa do zero-value, sem Apply de criação); (c) — o
// ponto central pedido pela task — ListEntries correlaciona por id e NÃO
// mistura eventos de dois wallets diferentes gravados no MESMO store.
const walletQueriesBehaviorTest = `package wallet

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
)

func mustDecimalQuery(t *testing.T, s string) runtime.Decimal {
	t.Helper()
	d, err := runtime.ParseDecimal(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestGetWalletReturnsProjectedView(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	ctx := context.Background()

	amount1, err := NewMoney(mustDecimalQuery(t, "10.00"), "")
	if err != nil {
		t.Fatal(err)
	}
	amount2, err := NewMoney(mustDecimalQuery(t, "5.00"), "")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Append(ctx, "W1", []runtime.Event{
		&DepositPerformed{Id: WalletId("W1"), Amount: amount1, Description: desc},
		&DepositPerformed{Id: WalletId("W1"), Amount: amount2, Description: desc},
	}); err != nil {
		t.Fatal(err)
	}

	view, err := GetWallet(ctx, store, WalletId("W1"))
	if err != nil {
		t.Fatalf("GetWallet: erro inesperado: %v", err)
	}
	if view.Id != WalletId("W1") {
		t.Fatalf("Id incorreto: got %v, want W1", view.Id)
	}
	want := mustDecimalQuery(t, "15.00")
	if view.Balance.Amount.Cmp(want) != 0 {
		t.Fatalf("Balance incorreto: got %s, want %s", view.Balance.Amount, want)
	}
}

func TestGetWalletUnknownStreamReturnsZeroValueSyncedView(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	ctx := context.Background()

	view, err := GetWallet(ctx, store, WalletId("desconhecido"))
	if err != nil {
		t.Fatalf("GetWallet: erro inesperado: %v", err)
	}
	if view.Id != WalletId("desconhecido") {
		t.Fatalf("Id deveria estar sincronizado mesmo sem eventos: got %v", view.Id)
	}
}

// TestListEntriesCorrelatesByIdAndDoesNotMixWallets prova o ponto central
// desta task: dois wallets diferentes (W1, W2) no MESMO store — ListEntries
// de cada um devolve só as entries do wallet pedido, nunca as do outro.
func TestListEntriesCorrelatesByIdAndDoesNotMixWallets(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	ctx := context.Background()

	amount1, err := NewMoney(mustDecimalQuery(t, "10.00"), "")
	if err != nil {
		t.Fatal(err)
	}
	amount2, err := NewMoney(mustDecimalQuery(t, "5.00"), "")
	if err != nil {
		t.Fatal(err)
	}
	amount3, err := NewMoney(mustDecimalQuery(t, "100.00"), "")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Append(ctx, "W1", []runtime.Event{
		&DepositPerformed{Id: WalletId("W1"), Amount: amount1, Description: desc},
		&WithdrawalPerformed{Id: WalletId("W1"), Amount: amount2, Description: desc},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append(ctx, "W2", []runtime.Event{
		&DepositPerformed{Id: WalletId("W2"), Amount: amount3, Description: desc},
	}); err != nil {
		t.Fatal(err)
	}

	entries1, err := ListEntries(ctx, store, WalletId("W1"))
	if err != nil {
		t.Fatalf("ListEntries(W1): erro inesperado: %v", err)
	}
	if len(entries1) != 2 {
		t.Fatalf("esperava 2 entries para W1, got %d", len(entries1))
	}

	entries2, err := ListEntries(ctx, store, WalletId("W2"))
	if err != nil {
		t.Fatalf("ListEntries(W2): erro inesperado: %v", err)
	}
	if len(entries2) != 1 {
		t.Fatalf("esperava 1 entry para W2 (não deveria ver as 2 de W1), got %d", len(entries2))
	}
	if entries2[0].Amount.Amount.Cmp(amount3.Amount) != 0 {
		t.Fatalf("entry de W2 incorreta (parece ter misturado com W1): got %s, want %s", entries2[0].Amount.Amount, amount3.Amount)
	}
}
`

// TestEmitQueriesBehavior prova NFR-15 sobre o Go de fato gerado.
func TestEmitQueriesBehavior(t *testing.T) {
	files := querySmokeFiles(t)
	files[filepath.Join("wallet", "queries_behavior_test.go")] = []byte(walletQueriesBehaviorTest)
	runGeneratedTests(t, files)
}

// --- Testes defensivos da correlação "list <VO>" (o cerne desta task). ---

// listVOFixtureSrc declara DOIS Aggregates (Board1/Board2) cujo state
// declara, cada um, um campo AppendList<Note> — uma correlação AMBÍGUA de
// propósito, para provar que EmitQuery recusa "list Note" quando não há
// exatamente 1 candidato (nem com os dois Aggregates conhecidos, nem com
// nenhum).
const listVOFixtureSrc = `
ValueObject NoteId(string) {
    Valid { value.length() > 0 }
}

ValueObject Note(string) {
    Valid { ok }
}

Event NoteAdded {
    id NoteId
    text Note
}

Aggregate Board1 {
    state {
        id    NoteId
        notes AppendList<Note>
    }

    access {
        Add requires caller.authenticated
    }

    Handle Add(text Note) {
        emit NoteAdded(self.id, text)
    }

    Apply NoteAdded {
        state.notes.add(event.text)
    }
}

Aggregate Board2 {
    state {
        id    NoteId
        notes AppendList<Note>
    }

    access {
        Add requires caller.authenticated
    }

    Handle Add(text Note) {
        emit NoteAdded(self.id, text)
    }

    Apply NoteAdded {
        state.notes.add(event.text)
    }
}

Query ListNotes(id NoteId) -> List<Note> {
    return list Note
}
`

const listVOFixtureModDs = `Module Notes {
    Database NotesDb {
        provider: "postgres"
        manages: [Board1, Board2]
    }
}
`

// parseListVOFixture monta o projeto sintético em disco e o resolve via
// driver.CheckProject (mesmo padrão de parseMeterFixture/parseAggregateLoadFixture,
// já em outros arquivos deste pacote).
func parseListVOFixture(t *testing.T) (*program.Program, *ast.QueryDecl, *ast.AggregateDecl, *ast.AggregateDecl) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    listVOFixtureModDs,
		"domain.ds": listVOFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de list<VO> (E8.1) tem diagnósticos de erro:\n%s", bag.Render())
	}
	query := findQueryDecl(t, prog, "ListNotes")
	board1 := findAggregateDecl(t, prog, "Board1")
	board2 := findAggregateDecl(t, prog, "Board2")
	return prog, query, board1, board2
}

// TestEmitQueryListVOZeroCandidatesFailsExplicitly prova que "list Note" sem
// NENHUM Aggregate conhecido correlacionável devolve um erro de geração
// claro (REQ-14.4), não Go arbitrário.
func TestEmitQueryListVOZeroCandidatesFailsExplicitly(t *testing.T) {
	prog, query, _, _ := parseListVOFixture(t)
	model := types.NewModel(prog.Symbols)
	reg := walletVOOperatorRegistryFromProgram(prog)

	_, err := codegen.EmitQuery("notes", query, map[string]*ast.AggregateDecl{}, prog, model, prog.Symbols, "Notes", reg)
	if err == nil {
		t.Fatal("esperava erro de geração: nenhum Aggregate conhecido declara AppendList<Note>")
	}
}

// TestEmitQueryListVOAmbiguousCandidatesFailsExplicitly prova que "list Note"
// com DOIS Aggregates candidatos (Board1 e Board2, ambos com
// AppendList<Note>) devolve um erro de geração claro em vez de escolher um
// dos dois arbitrariamente (§design 4.1: correção por construção; a task
// pede explicitamente para NÃO inventar um fallback aqui).
func TestEmitQueryListVOAmbiguousCandidatesFailsExplicitly(t *testing.T) {
	prog, query, board1, board2 := parseListVOFixture(t)
	model := types.NewModel(prog.Symbols)
	reg := walletVOOperatorRegistryFromProgram(prog)

	aggregates := map[string]*ast.AggregateDecl{"Board1": board1, "Board2": board2}
	_, err := codegen.EmitQuery("notes", query, aggregates, prog, model, prog.Symbols, "Notes", reg)
	if err == nil {
		t.Fatal("esperava erro de geração: 2 Aggregates candidatos (Board1, Board2) para AppendList<Note> é ambíguo")
	}
}
