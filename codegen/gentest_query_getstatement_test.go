package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// gentest_query_getstatement_test.go prova o critério "âncora 1" do ciclo
// Read Side (§design read-side, requirements §1.4, tasks.md I3.1+I3.2): a
// forma exata do spec v6 §6.3 —
//
//	Query GetStatement(walletId WalletId, page int) -> List<StatementEntryVW> {
//	    return load Wallet(walletId)
//	           .entries
//	           orderBy date descending
//	           skip page * 20
//	           take 20
//	           as StatementEntryVW
//	}
//
// — gera Go que compila e se comporta como o spec descreve.
//
// --- Por que uma fixture SINTÉTICA, não o wallet real ---
//
// Duas divergências, cada uma investigada e documentada (a task pede para só
// desviar quando genuinamente necessário, nunca por conveniência):
//
//  1. O StatementEntry REAL do wallet (docs/examples/wallet/domain.ds) não
//     tem campo "date" — só type/amount/description. O spec do Wallet (§4.5,
//     não o exemplo deste repo) POPULA "date" com "event.timestamp", o
//     metadata implícito readonly de todo Event (spec §4.3: "timestamp,
//     sequence, aggregateId, eventType"). Confirmado por grep: NENHUM lugar
//     do front-end (types/, sema/) reconhece "timestamp"/"sequence"/
//     "aggregateId"/"eventType" como membro de um Event — "event.timestamp"
//     não resolve hoje. Extender o wallet real para usar essa forma exigiria
//     IMPLEMENTAR o metadata implícito no front-end primeiro — fora do
//     escopo deste ciclo ("front-end inalterado", tasks.md).
//  2. A forma LITERAL do spec, "orderBy date descending" (SEM qualificador
//     de item — nenhum "e." na frente), só resolveria como "date" sendo um
//     NOME em escopo — mas resolver/resolve_body.go (REQ-9, INTOCADO por
//     este ciclo) só injeta um binder na cláusula quando QueryExpr.Binding
//     != "" (n.Binding == "" ⇒ NENHUM nome novo entra em escopo pelas
//     cláusulas — confirmado lendo resolve_body.go:245-259). Ou seja: o
//     front-end de HOJE não resolveria "orderBy date descending" sem
//     binding — só "load X(id).entries e orderBy e.date descending" (com um
//     binding explícito "e", a MESMA forma que toda fixture de I1.1/I2.1já
//     usa para "list"). Esta fixture usa a forma COM binding — o mesmo
//     ajuste que qualquer QueryExpr sem binding precisaria hoje.
//
// O resto da forma (load AGG(id).entries / orderBy K [dir] / skip E / take E
// / as V, encadeados nessa ordem textual) é EXATAMENTE a do spec.
//
// --- A fixture: módulo Ledger ---
//
// Account (Aggregate EventSourced) tem "entries AppendList<StatementEntry>";
// StatementEntry é um VO composto com "amount EntryAmount" (Money-like: value
// decimal + currency string — prova o achatamento de VO composto de REQ-34.1,
// a MESMA ideia de "amount_value"/"amount_currency" do spec §6.1) e "date
// EntryDate" (wrapper sobre datetime — a linha "VO wrapper sobre primitivo
// ordenável" da tabela de comparabilidade, §design read-side 3.2).
const getStatementFixtureModDs = `Module Ledger { }
`

const getStatementFixtureDomainDs = `
ValueObject AccountId(string) {
    Valid { value.length() > 0 }
}

ValueObject EntryAmount {
    value decimal
    currency string

    Valid { value >= 0 }
}

ValueObject EntryDate(datetime) {
    Valid { ok }
}

ValueObject StatementEntry {
    amount EntryAmount
    date   EntryDate

    Valid { ok }
}

Event EntryRecorded {
    id     AccountId
    amount EntryAmount
    date   EntryDate
}

Aggregate Account {
    strategy EventSourced

    state {
        id      AccountId
        entries AppendList<StatementEntry>
    }

    access {
        Record requires caller.authenticated
    }

    Handle Record(amount EntryAmount, date EntryDate) {
        emit EntryRecorded(self.id, amount, date)
    }

    Apply EntryRecorded {
        state.entries.add(StatementEntry(amount: event.amount, date: event.date))
    }
}
`

const getStatementFixtureReadDs = `
View StatementEntryVW {
    amount_value    decimal
    amount_currency string
    date            EntryDate
}

Query GetStatement(accountId AccountId, page integer) -> List<StatementEntryVW> {
    return load Account(accountId).entries e orderBy e.date descending skip page * 20 take 20 as StatementEntryVW
}
`

// generateGetStatementFixtureProject roda o orquestrador COMPLETO
// (codegen.Generate) sobre o Program da fixture Ledger — mesmo padrão de
// generatePolicyOrderByFixtureProject (gentest_policy_orderby_test.go): o
// teste comportamental abaixo aciona GetStatement de verdade, então precisa
// do projeto INTEIRO (runtime vendorado incluso).
func generateGetStatementFixtureProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    getStatementFixtureModDs,
		"domain.ds": getStatementFixtureDomainDs,
		"read.ds":   getStatementFixtureReadDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de GetStatement (I3.1/I3.2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture GetStatement: %v", err)
	}
	return files
}

// ledgerQueriesFile localiza, entre os arquivos gerados, o que carrega a
// Query GetStatement (codegen.go emite "<pkg>/queries.go" por módulo, pkg =
// goname.PackageName("Ledger") = "ledger").
func ledgerQueriesFile(t *testing.T, files map[string][]byte) (path string, content []byte) {
	t.Helper()
	for p, c := range files {
		if strings.HasPrefix(p, "ledger/") && strings.HasSuffix(p, "queries.go") {
			return p, c
		}
	}
	var ks []string
	for k := range files {
		ks = append(ks, k)
	}
	t.Fatalf("esperava um arquivo \"ledger/*queries.go\" entre os arquivos gerados, não achei; arquivos: %v", ks)
	return "", nil
}

func emitGetStatementQueriesFixture(t *testing.T) []byte {
	t.Helper()
	_, content := ledgerQueriesFile(t, filesToMap(generateGetStatementFixtureProject(t)))
	return content
}

// TestEmitGetStatementGolden prova a forma EXATA do corpo gerado: carrega o
// Aggregate (LoadAccount, E6.2 intocado), aplica runtime.SelectSlice sobre
// ".entries.Items()" com a Query[StatementEntry] completa (Less/OrderField/
// OrderDesc/Skip/Take — SEM Where, GetStatement não tem "where"), e projeta
// item a item para StatementEntryVW (achatamento de VO composto incluso).
func TestEmitGetStatementGolden(t *testing.T) {
	got := string(emitGetStatementQueriesFixture(t))
	for _, want := range []string{
		"func GetStatement(ctx context.Context, store runtime.EventStore, accountId AccountId, page int64) ([]StatementEntryVW, error)",
		"tmp1, err := LoadAccount(runtime.NewEventLoader(ctx, store), accountId)",
		"tmp2, err := runtime.SelectSlice(tmp1.state.Entries.Items(), runtime.Query[StatementEntry]{" +
			"Less: func(a, b StatementEntry) (bool, error) { return time.Time(b.Date).Before(time.Time(a.Date)), nil }, " +
			"OrderField: \"date\", OrderDesc: true, Skip: int(page * 20), Take: 20})",
		"projected := make([]StatementEntryVW, 0, len(tmp2))",
		"for _, item := range tmp2",
		"projected = append(projected, StatementEntryVW{Amount_value: item.Amount.Value, Amount_currency: item.Amount.Currency, Date: item.Date})",
		"return projected, nil",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava a presença de %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "query_get_statement.go.golden"), []byte(got))
}

// TestEmitGetStatementDeterministic prova NFR-13: regerar duas vezes produz
// bytes idênticos.
func TestEmitGetStatementDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitGetStatementQueriesFixture(t)
	})
}

// TestEmitGetStatementSmokeCompile prova que o projeto INTEIRO gerado
// compila e passa go vet num diretório isolado (NFR-14/NFR-17).
func TestEmitGetStatementSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, filesToMap(generateGetStatementFixtureProject(t)))
}

// getStatementHandwrittenTest é o harness escrito à mão que aciona o código
// GERADO de verdade (mesmo espírito de policyOrderByHandwrittenTest,
// gentest_policy_orderby_test.go, e walletQueriesBehaviorTest,
// decl_query_test.go): 25 StatementEntry seedados via EventStore.Append, com
// datas Jan/1..Jan/25 de 2024 inseridos em ordem EMBARALHADA (para provar que
// a ordenação vem de "orderBy e.date descending", nunca da ordem de
// inserção) — GetStatement(ctx, store, "A1", page=0) deve devolver os 20 MAIS
// RECENTES (Jan25..Jan6) e GetStatement(..., page=1) deve devolver os 5
// RESTANTES mais antigos (Jan5..Jan1) — a prova pedida pela task: "skip N
// take M lands on the right window", além da projeção "as V" (achatamento de
// EntryAmount incluso).
const getStatementHandwrittenTest = `package ledger

import (
	"context"
	"testing"
	"time"

	"domainscript/generated/runtime"
)

func mustDecimalGetStatement(t *testing.T, s string) runtime.Decimal {
	t.Helper()
	d, err := runtime.ParseDecimal(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestGetStatementHandwrittenRunGreen(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	ctx := context.Background()

	const n = 25
	// Ordem de inserção EMBARALHADA (ímpares primeiro, depois pares) —
	// deliberadamente diferente da ordem cronológica, para que um teste que
	// passasse "por acidente" (ordem de inserção == ordem esperada) não
	// pudesse mascarar uma lowering de orderBy quebrada.
	var order []int
	for i := 1; i < n; i += 2 {
		order = append(order, i)
	}
	for i := 0; i < n; i += 2 {
		order = append(order, i)
	}

	amount, err := NewEntryAmount(mustDecimalGetStatement(t, "1.00"), "BRL")
	if err != nil {
		t.Fatal(err)
	}

	events := make([]runtime.Event, 0, n)
	for _, i := range order {
		date := EntryDate(time.Date(2024, 1, i+1, 0, 0, 0, 0, time.UTC))
		events = append(events, &EntryRecorded{Id: AccountId("A1"), Amount: amount, Date: date})
	}
	if err := store.Append(ctx, "A1", events); err != nil {
		t.Fatal(err)
	}

	page0, err := GetStatement(ctx, store, AccountId("A1"), 0)
	if err != nil {
		t.Fatalf("GetStatement(page 0): erro inesperado: %v", err)
	}
	if len(page0) != 20 {
		t.Fatalf("esperava 20 itens na página 0 (skip 0 take 20 sobre 25 entries), got %d", len(page0))
	}
	// Página 0, ordenada DESCENDENTE por date, começa no dia MAIS RECENTE
	// (Jan 25) e termina no dia 6.
	for idx, wantDay := 0, 25; idx < 20; idx, wantDay = idx+1, wantDay-1 {
		want := time.Date(2024, 1, wantDay, 0, 0, 0, 0, time.UTC)
		got := time.Time(page0[idx].Date)
		if !got.Equal(want) {
			t.Fatalf("page0[%d].Date = %v, want dia %d (%v) — ordenação/paginação incorreta", idx, got, wantDay, want)
		}
	}
	if page0[0].Amount_value.Cmp(mustDecimalGetStatement(t, "1.00")) != 0 {
		t.Fatalf("projeção as V incorreta: Amount_value = %s, want 1.00", page0[0].Amount_value)
	}
	if page0[0].Amount_currency != "BRL" {
		t.Fatalf("projeção as V incorreta: Amount_currency = %s, want BRL", page0[0].Amount_currency)
	}

	page1, err := GetStatement(ctx, store, AccountId("A1"), 1)
	if err != nil {
		t.Fatalf("GetStatement(page 1): erro inesperado: %v", err)
	}
	if len(page1) != 5 {
		t.Fatalf("esperava 5 itens na página 1 (skip 20 take 20 sobre 25 entries — os 5 restantes), got %d", len(page1))
	}
	// Página 1 continua a MESMA ordem descendente, dias 5 a 1.
	for idx, wantDay := 0, 5; idx < 5; idx, wantDay = idx+1, wantDay-1 {
		want := time.Date(2024, 1, wantDay, 0, 0, 0, 0, time.UTC)
		got := time.Time(page1[idx].Date)
		if !got.Equal(want) {
			t.Fatalf("page1[%d].Date = %v, want dia %d (%v)", idx, got, wantDay, want)
		}
	}

	page2, err := GetStatement(ctx, store, AccountId("A1"), 2)
	if err != nil {
		t.Fatalf("GetStatement(page 2): erro inesperado: %v", err)
	}
	if len(page2) != 0 {
		t.Fatalf("esperava página 2 vazia (skip 40 além dos 25 itens), got %d", len(page2))
	}
}
`

// TestGetStatementHandwrittenRunGreen roda "go test ./..." de VERDADE sobre o
// projeto gerado, com getStatementHandwrittenTest injetado ao lado dos
// demais arquivos de ledger/ — path.Join usa sempre "/" (o generateGetStatementFixtureProject
// já produz paths "/"; o arquivo extra abaixo segue a mesma convenção,
// NUNCA filepath.Join em Windows).
func TestGetStatementHandwrittenRunGreen(t *testing.T) {
	files := filesToMap(generateGetStatementFixtureProject(t))
	files["ledger/ledger_handwritten_test.go"] = []byte(getStatementHandwrittenTest)
	runGeneratedTests(t, files)
}
