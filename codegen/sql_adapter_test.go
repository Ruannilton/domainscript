package codegen_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// sql_adapter_test.go prova a task G1 (adapter database/sql plugável,
// REQ-20.5, REQ-26.2/26.3, NFR-12, §design 3.8/3.9/3.11/4.4): nem o wallet
// nem o shop (as duas fixtures reais deste repositório) declaram um Database
// com um provider que este gerador reconheça como adapter real (ambos usam
// "postgres", decorativo até esta task — ver program.Database.Provider) —
// então esta task precisa de uma fixture SINTÉTICA para exercitar o
// caminho de verdade, o mesmo padrão que meterFixtureSrc (decl_usecase_test.go,
// E7.2) já usou para o dispatch de Handle.
//
// A fixture, módulo "Ledger": dois Aggregates EventSourced (Account, gerido
// por MainDb; Journal, por SideDb), ambos os Database com
// provider:"sqlite" (o único provider real que este gerador sabe montar,
// codegen/sqlrt) e supportsXA:true. Um UseCase (PerformDebit) toca só
// Account — caminho de banco único; outro (TransferAndPost) toca Account E
// Journal — caminho 2PC (REQ-20.5, usecase2PCPlan em decl_usecase.go). Uma
// Query (GetAccount) e uma View (AccountView) provam REQ-21.5 ("mesmo
// lowering, back-ends distintos") sem NENHUMA mudança em decl_query.go: a
// função gerada recebe "store runtime.EventStore" — um *sqlruntime.EventStore
// satisfaz essa interface tal como runtime.NewMemoryEventStore() já satisfaz.

const ledgerDomainDs = `
ValueObject AccountId(string) { Valid { value.length() > 0 } }
ValueObject JournalId(string) { Valid { value.length() > 0 } }
ValueObject EntryAmount(integer) { Valid { ok } }
ValueObject JournalOpen(boolean) { Valid { ok } }

Error JournalClosed { message "O journal ainda não foi aberto." }

Event AccountDebited { id AccountId, amount EntryAmount }
Event JournalOpened  { id JournalId }
Event JournalPosted  { id JournalId, amount EntryAmount }

Aggregate Account {
    strategy EventSourced

    state {
        id      AccountId
        balance EntryAmount
    }

    access {
        Debit requires caller.authenticated
    }

    Handle Debit(amount EntryAmount) {
        emit AccountDebited(self.id, amount)
    }

    Apply AccountDebited {
        state.balance = event.amount
    }
}

Aggregate Journal {
    strategy EventSourced

    state {
        id         JournalId
        open       JournalOpen
        lastAmount EntryAmount
    }

    access {
        Post requires caller.authenticated
    }

    Handle Post(amount EntryAmount) {
        ensure state.open == JournalOpen(true) else JournalClosed
        emit JournalPosted(self.id, amount)
    }

    Apply JournalOpened {
        state.open = JournalOpen(true)
    }

    Apply JournalPosted {
        state.lastAmount = event.amount
    }
}
`

const ledgerApplicationDs = `
Command Debit {
    accountId ref Account
    amount    EntryAmount
}

Command Transfer {
    accountId ref Account
    journalId ref Journal
    amount    EntryAmount
}

UseCase PerformDebit handles Debit {
    execute {
        account = load Account(cmd.accountId)
        account.Debit(cmd.amount)
    }
}

UseCase TransferAndPost handles Transfer {
    execute {
        account = load Account(cmd.accountId)
        account.Debit(cmd.amount)
        journal = load Journal(cmd.journalId)
        journal.Post(cmd.amount)
    }
}
`

const ledgerReadDs = `
View AccountView {
    id      AccountId
    balance EntryAmount
}

Query GetAccount(id AccountId) -> AccountView {
    return load Account(id) as AccountView
}
`

// ledgerModDs monta mod.ds com os dois Database sqlite apontando para dsn
// reais (arquivos num diretório temporário — ver ledgerSQLitePaths) —
// supportsXA:true em ambos é o que torna TransferAndPost (toca Account E
// Journal) um caso 2PC válido no front-end (REQ-5.9) e reconhecido como tal
// pelo gerador (usecase2PCPlan, decl_usecase.go).
func ledgerModDs(mainDSN, sideDSN string) string {
	return fmt.Sprintf(`Module Ledger {
    Database MainDb {
        provider: "sqlite"
        dsn: %q
        supportsXA: true
        manages: [Account]
    }
    Database SideDb {
        provider: "sqlite"
        dsn: %q
        supportsXA: true
        manages: [Journal]
    }
}
`, filepath.ToSlash(mainDSN), filepath.ToSlash(sideDSN))
}

// ledgerSQLitePaths devolve dois caminhos de arquivo sqlite distintos, num
// diretório temporário PRÓPRIO (não o do projeto gerado) — bancos de
// verdade, não ":memory:", para que main.go/os testes comportamentais
// consigam abrir a MESMA base de dados em conexões diferentes sem as
// sutilezas de modo compartilhado de ":memory:" (ver codegen/sqlrt/
// open_sqlite.go.txt).
func ledgerSQLitePaths(t *testing.T) (mainDSN, sideDSN string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "main.db"), filepath.Join(dir, "side.db")
}

// ledgerOptions é o Options do projeto Ledger — GoVersion deliberadamente
// vazio (diferente de walletGenerateOptions): EmitGoMod (G1) deve escolher
// sozinho a versão mínima do driver sqlite (sqliteMinGoVersion) quando o
// programa precisa do adapter — é exatamente esse comportamento que
// TestLedgerGoModRequiresSQLiteAndBumpsGoVersion confirma.
var ledgerOptions = codegen.Options{ModulePath: "domainscript/generated"}

// generateLedgerProject escreve a fixture Ledger em disco, resolve via
// driver.CheckProject e gera o projeto Go completo — mesmo padrão de
// generateWalletProject (codegen_test.go).
func generateLedgerProject(t *testing.T, mainDSN, sideDSN string) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         ledgerModDs(mainDSN, sideDSN),
		"domain.ds":      ledgerDomainDs,
		"application.ds": ledgerApplicationDs,
		"read.ds":        ledgerReadDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética Ledger (G1) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, ledgerOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture Ledger: %v", err)
	}
	return files
}

func ledgerFileByPath(files []codegen.File, p string) ([]byte, bool) {
	for _, f := range files {
		if f.Path == p {
			return f.Content, true
		}
	}
	return nil, false
}

// --- 1. NFR-12: isolamento do go.mod (o coração da task). ---

// TestLedgerGoModRequiresSQLiteAndBumpsGoVersion prova REQ-26.2/26.3/NFR-12:
// um programa com Database provider:"sqlite" ganha "require modernc.org/sqlite"
// em go.mod E a versão de Go sobe para a mínima que o driver exige — mas SÓ
// quando o recurso é de fato usado.
func TestLedgerGoModRequiresSQLiteAndBumpsGoVersion(t *testing.T) {
	mainDSN, sideDSN := ledgerSQLitePaths(t)
	files := generateLedgerProject(t, mainDSN, sideDSN)

	goMod, ok := ledgerFileByPath(files, "go.mod")
	if !ok {
		t.Fatal("esperava go.mod entre os arquivos gerados")
	}
	content := string(goMod)
	if !strings.Contains(content, "require modernc.org/sqlite") {
		t.Fatalf("esperava \"require modernc.org/sqlite\" em go.mod, não achei:\n%s", content)
	}
	if !strings.Contains(content, "go 1.25") {
		t.Fatalf("esperava \"go 1.25\" (versão mínima do driver) em go.mod, não achei:\n%s", content)
	}

	if _, ok := ledgerFileByPath(files, "sqlruntime/eventstore.go"); !ok {
		t.Error("esperava sqlruntime/eventstore.go entre os arquivos gerados (programa usa Database sqlite)")
	}
	if _, ok := ledgerFileByPath(files, "sqlruntime/twophase.go"); !ok {
		t.Error("esperava sqlruntime/twophase.go entre os arquivos gerados")
	}
}

// TestWalletGoModStaysDependencyFreeAfterG1 é o guarda de regressão mais
// importante desta task (NFR-12): o wallet real (provider:"postgres", nunca
// reconhecido como adapter real) precisa continuar gerando um go.mod SEM
// nenhum "require" e SEM sqlruntime/* — G1 é estritamente opt-in.
func TestWalletGoModStaysDependencyFreeAfterG1(t *testing.T) {
	files := generateWalletProject(t)

	goMod, ok := ledgerFileByPath(files, "go.mod")
	if !ok {
		t.Fatal("esperava go.mod entre os arquivos gerados do wallet")
	}
	content := string(goMod)
	if strings.Contains(content, "require") {
		t.Fatalf("NFR-12: wallet não deveria ter nenhuma dependência externa em go.mod, achei:\n%s", content)
	}
	if !strings.Contains(content, "go 1.22") {
		t.Fatalf("esperava a versão de Go do wallet inalterada (\"go 1.22\"), achei:\n%s", content)
	}

	for _, f := range files {
		if strings.HasPrefix(f.Path, "sqlruntime/") {
			t.Fatalf("NFR-12: wallet não deveria gerar nenhum arquivo sqlruntime/*, achei %q", f.Path)
		}
	}
}

// --- 2. Determinismo (NFR-13) e wiring de main.go (2PC, G1). ---

func TestLedgerGenerateDeterministic(t *testing.T) {
	mainDSN, sideDSN := ledgerSQLitePaths(t)
	first := generateLedgerProject(t, mainDSN, sideDSN)
	second := generateLedgerProject(t, mainDSN, sideDSN)

	if len(first) != len(second) {
		t.Fatalf("número de arquivos difere entre gerações: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path {
			t.Fatalf("Path[%d] difere entre gerações: %q vs %q", i, first[i].Path, second[i].Path)
		}
		if string(first[i].Content) != string(second[i].Content) {
			t.Fatalf("conteúdo de %q difere entre gerações", first[i].Path)
		}
	}
}

// TestLedgerMainWiresXADatabases prova que cmd/<service>/main.go de fato
// abre as duas conexões sqlite reais e wira Wire2PC com um
// sqlruntime.UnitOfWork2PC — a consequência de runtime do REQ-20.5 (2PC
// quando todos os bancos são XA) chegando ao wiring gerado (§design 3.8/3.11).
func TestLedgerMainWiresXADatabases(t *testing.T) {
	mainDSN, sideDSN := ledgerSQLitePaths(t)
	files := generateLedgerProject(t, mainDSN, sideDSN)

	var main []byte
	for _, f := range files {
		if strings.HasPrefix(f.Path, "cmd/") && strings.HasSuffix(f.Path, "main.go") {
			main = f.Content
		}
	}
	if main == nil {
		t.Fatal("esperava um cmd/<service>/main.go entre os arquivos gerados")
	}
	got := string(main)
	for _, want := range []string{
		"sqlruntime.Open(",
		"sqlruntime.NewEventStore(",
		"ledger.EventRegistry()",
		"ledger.Wire2PC(sqlruntime.NewUnitOfWork2PC(map[string]*sqlruntime.EventStore{",
		`"MainDb":`,
		`"SideDb":`,
		// o Wire(uow) de sempre continua presente (módulo também tem
		// PerformDebit, banco único) — G1 é aditivo, não substitui o
		// caminho de Marco E.
		"ledger.Wire(uow)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q em cmd/.../main.go, não achei:\n%s", want, got)
		}
	}
}

// --- 3. Smoke compile (NFR-14) — precisa de "go mod tidy" (SmokeCompile
// detecta o "require" e roda sozinho, ver gentest/smoke.go). ---

func TestLedgerSmokeCompile(t *testing.T) {
	mainDSN, sideDSN := ledgerSQLitePaths(t)
	files := generateLedgerProject(t, mainDSN, sideDSN)
	gentest.SmokeCompile(t, filesToMap(files))
}

// --- 4. Comportamento de verdade contra SQLite real (o coração da task). ---
//
// Ambos os testes abaixo escrevem o projeto gerado + um arquivo de teste
// comportamental ADICIONAL dentro do pacote "ledger" (mesmo padrão de
// walletUseCaseBehaviorTest/meterUseCaseBehaviorTest, decl_usecase_test.go)
// e rodam "go test ./..." DE VERDADE sobre ele (gentest.RunTests) — a lição
// documentada de F5: não basta um harness que só compila, tem que EXECUTAR o
// caminho gerado. Os placeholders de DSN (ex. __MAIN_DB_DSN__) vivem dentro
// de literais de string Go, então o template já é Go válido ANTES da
// substituição (compilável, revisável) — strings.Replace só troca o texto
// entre aspas pelo caminho real do arquivo sqlite do teste.

// ledgerSingleDatabaseBehaviorTest prova, sobre o Go de fato gerado: (a)
// PerformDebit (UseCase de banco único) grava de verdade no arquivo sqlite
// de MainDb via sqlruntime.UnitOfWork — confirmado lendo a TABELA "events"
// direto por SQL puro, não só via runtime.EventStore.Load; (b) GetAccount
// (Query) lê o MESMO agregado através do MESMO sqlruntime.EventStore
// (runtime.NewEventLoader por baixo) SEM nenhuma mudança em decl_query.go —
// a prova de REQ-21.5 ("mesmo lowering, back-ends distintos").
const ledgerSingleDatabaseBehaviorTest = `package ledger

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type behaviorCaller struct{ id string }

func (c behaviorCaller) Authenticated() bool      { return true }
func (c behaviorCaller) ID() string                { return c.id }
func (c behaviorCaller) HasRole(role string) bool { return false }

func TestPerformDebitPersistsToRealSQLiteAndQueryReadsItBack(t *testing.T) {
	db, err := sqlruntime.Open(mainDbDSNForTest)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	store, err := sqlruntime.NewEventStore(ctx, db, EventRegistry(), sqlruntime.SQLiteDialect())
	if err != nil {
		t.Fatalf("NewEventStore: %v", err)
	}
	uow = sqlruntime.NewUnitOfWork(db, EventRegistry(), sqlruntime.SQLiteDialect())

	callerCtx := runtime.WithCaller(ctx, behaviorCaller{id: "acc-1"})
	cmd := Debit{AccountId: AccountId("acc-1"), Amount: EntryAmount(150)}
	if err := PerformDebit(callerCtx, cmd); err != nil {
		t.Fatalf("PerformDebit: %v", err)
	}

	// confirma via SQL puro (não só via runtime.EventStore.Load) que a
	// escrita é REAL, não uma simulação: uma linha de verdade na tabela
	// "events" da conexão MainDb.
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE aggregate_id = ?", "acc-1").Scan(&count); err != nil {
		t.Fatalf("consulta SQL direta: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperava 1 linha em events para acc-1, achei %d", count)
	}

	// GetAccount (Query, decl_query.go) roda inalterado sobre o MESMO store
	// SQL — a prova de REQ-21.5.
	view, err := GetAccount(ctx, store, AccountId("acc-1"))
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if view.Balance != EntryAmount(150) {
		t.Fatalf("esperava balance=150, veio %v", view.Balance)
	}
}

// mainDbDSNForTest é substituído por TestLedgerSingleDatabaseBehavior antes
// de escrever o arquivo (strings.Replace sobre este template).
var mainDbDSNForTest = "__MAIN_DB_DSN__"
`

// TestLedgerSingleDatabaseBehavior é o smoke comportamental do caminho de
// banco único (item (a) do prompt da task): gera o projeto, substitui o
// placeholder do DSN pelo caminho real do arquivo sqlite e roda "go test"
// de verdade sobre o pacote "ledger" gerado.
func TestLedgerSingleDatabaseBehavior(t *testing.T) {
	mainDSN, sideDSN := ledgerSQLitePaths(t)
	files := filesToMap(generateLedgerProject(t, mainDSN, sideDSN))
	testSrc := strings.Replace(ledgerSingleDatabaseBehaviorTest, "__MAIN_DB_DSN__", filepath.ToSlash(mainDSN), 1)
	files[filepath.Join("ledger", "behavior_test.go")] = []byte(testSrc)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}

// ledgerTwoPCBehaviorTest prova o 2PC de verdade (item (b) do prompt da
// task): TransferAndPost toca Account (MainDb) E Journal (SideDb); "given" —
// um evento JournalOpened semeado direto no SideDb (o mesmo padrão de
// "seedar via evento isolado" documentado em CLAUDE.md/design §5) — decide o
// destino de cada chamada:
//
//   - Journal já aberto (JournalOpened semeado) → prepare sucede nos DOIS
//     bancos → COMMIT nos dois (confirmado por linhas reais em AMBOS os
//     arquivos sqlite).
//   - Journal FECHADO (nenhum JournalOpened) → Post falha com
//     ErrJournalClosed DEPOIS que Debit já teria "preparado" com sucesso em
//     MainDb → UnitOfWork2PC.Run rollbacka os DOIS bancos (confirmado: ZERO
//     linhas em MainDb também, não só em SideDb — a prova de que o rollback
//     é atômico entre bancos, não só local).
const ledgerTwoPCBehaviorTest = `package ledger

import (
	"context"
	"errors"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type twoPCCaller struct{ id string }

func (c twoPCCaller) Authenticated() bool      { return true }
func (c twoPCCaller) ID() string                { return c.id }
func (c twoPCCaller) HasRole(role string) bool { return false }

// mainDbDSN2PC/sideDbDSN2PC são substituídos por TestLedgerTwoPCBehavior
// antes de escrever o arquivo (strings.Replace sobre este template) — mesma
// técnica de mainDbDSNForTest (ledgerSingleDatabaseBehaviorTest).
var mainDbDSN2PC = "__MAIN_DB_DSN_2PC__"
var sideDbDSN2PC = "__SIDE_DB_DSN_2PC__"

func openStores(t *testing.T) (mainStore, sideStore *sqlruntime.EventStore) {
	t.Helper()
	ctx := context.Background()

	mainDB, err := sqlruntime.Open(mainDbDSN2PC)
	if err != nil {
		t.Fatalf("Open(MainDb): %v", err)
	}
	t.Cleanup(func() { mainDB.Close() })
	mainStore, err = sqlruntime.NewEventStore(ctx, mainDB, EventRegistry(), sqlruntime.SQLiteDialect())
	if err != nil {
		t.Fatalf("NewEventStore(MainDb): %v", err)
	}

	sideDB, err := sqlruntime.Open(sideDbDSN2PC)
	if err != nil {
		t.Fatalf("Open(SideDb): %v", err)
	}
	t.Cleanup(func() { sideDB.Close() })
	sideStore, err = sqlruntime.NewEventStore(ctx, sideDB, EventRegistry(), sqlruntime.SQLiteDialect())
	if err != nil {
		t.Fatalf("NewEventStore(SideDb): %v", err)
	}
	return mainStore, sideStore
}

func TestTwoPCCommitsBothDatabasesWhenJournalIsOpen(t *testing.T) {
	mainStore, sideStore := openStores(t)
	uow2pc = sqlruntime.NewUnitOfWork2PC(map[string]*sqlruntime.EventStore{"MainDb": mainStore, "SideDb": sideStore})

	ctx := context.Background()
	// given: Journal J1 já aberto (evento semeado direto, sem UseCase).
	if err := sideStore.Append(ctx, "J1", []runtime.Event{&JournalOpened{Id: JournalId("J1")}}); err != nil {
		t.Fatalf("seed JournalOpened: %v", err)
	}

	callerCtx := runtime.WithCaller(ctx, twoPCCaller{id: "A1"})
	cmd := Transfer{AccountId: AccountId("A1"), JournalId: JournalId("J1"), Amount: EntryAmount(42)}
	if err := TransferAndPost(callerCtx, cmd); err != nil {
		t.Fatalf("esperava sucesso (Journal aberto), veio erro: %v", err)
	}

	mainEvents, err := mainStore.Load(ctx, "A1")
	if err != nil {
		t.Fatalf("Load(MainDb, A1): %v", err)
	}
	sideEvents, err := sideStore.Load(ctx, "J1")
	if err != nil {
		t.Fatalf("Load(SideDb, J1): %v", err)
	}
	if len(mainEvents) != 1 {
		t.Fatalf("esperava 1 evento commitado em MainDb (A1), achei %d", len(mainEvents))
	}
	if len(sideEvents) != 2 { // JournalOpened (seed) + JournalPosted
		t.Fatalf("esperava 2 eventos commitados em SideDb (J1: opened+posted), achei %d", len(sideEvents))
	}
}

func TestTwoPCRollsBackBothDatabasesWhenJournalIsClosed(t *testing.T) {
	mainStore, sideStore := openStores(t)
	uow2pc = sqlruntime.NewUnitOfWork2PC(map[string]*sqlruntime.EventStore{"MainDb": mainStore, "SideDb": sideStore})

	ctx := context.Background()
	// SEM seed: Journal J2 nunca foi aberto (state.open zero-value = false)
	// — Post recusa com ErrJournalClosed DEPOIS que Debit já "preparou" com
	// sucesso em MainDb (a ordem de execute: account.Debit primeiro,
	// journal.Post depois).

	callerCtx := runtime.WithCaller(ctx, twoPCCaller{id: "A2"})
	cmd := Transfer{AccountId: AccountId("A2"), JournalId: JournalId("J2"), Amount: EntryAmount(99)}
	err := TransferAndPost(callerCtx, cmd)
	if err == nil {
		t.Fatal("esperava erro (Journal fechado) — 2PC deveria rollback tudo")
	}
	if !errors.Is(err, ErrJournalClosed) {
		t.Fatalf("esperava ErrJournalClosed, veio %v", err)
	}

	mainEvents, err := mainStore.Load(ctx, "A2")
	if err != nil {
		t.Fatalf("Load(MainDb, A2): %v", err)
	}
	sideEvents, err := sideStore.Load(ctx, "J2")
	if err != nil {
		t.Fatalf("Load(SideDb, J2): %v", err)
	}
	if len(mainEvents) != 0 {
		t.Fatalf("esperava ROLLBACK em MainDb (0 eventos para A2, o prepare que teria sucedido sozinho), achei %d — 2PC não está atômico entre bancos", len(mainEvents))
	}
	if len(sideEvents) != 0 {
		t.Fatalf("esperava 0 eventos em SideDb (J2, nunca aberto), achei %d", len(sideEvents))
	}
}
`

// TestLedgerTwoPCBehavior é o smoke comportamental do 2PC (item (b) do
// prompt da task G1): substitui os placeholders de DSN pelos caminhos reais
// e roda "go test" de verdade sobre o pacote "ledger" gerado, contra DOIS
// arquivos sqlite reais e distintos.
func TestLedgerTwoPCBehavior(t *testing.T) {
	mainDSN, sideDSN := ledgerSQLitePaths(t)
	files := filesToMap(generateLedgerProject(t, mainDSN, sideDSN))

	testSrc := strings.Replace(ledgerTwoPCBehaviorTest, "__MAIN_DB_DSN_2PC__", filepath.ToSlash(mainDSN), 1)
	testSrc = strings.Replace(testSrc, "__SIDE_DB_DSN_2PC__", filepath.ToSlash(sideDSN), 1)
	files[filepath.Join("ledger", "behavior_test.go")] = []byte(testSrc)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
