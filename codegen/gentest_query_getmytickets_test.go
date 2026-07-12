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

// gentest_query_getmytickets_test.go prova o critério "âncora 2" do ciclo
// Read Side (§design read-side 3.7, requirements §1.4, tasks.md I5.1): a
// forma exata do spec v6 §6.3 —
//
//	Query GetMyTickets(userId UserId) -> List<TicketVW> {
//	    return list Ticket t
//	           join Order o on t.orderId == o.id
//	           where o.userId == userId
//	             and t.status in [TicketStatus.Sold, TicketStatus.Used]
//	           as TicketVW
//	}
//
// — gera Go que compila e se comporta como o spec descreve: a correlação
// in-memory das duas fontes (join.go/hoistJoin), o predicado where com "in"
// (I4.1, já fechado), e a projeção "as V" contra os DOIS aliases.
//
// --- Por que uma fixture SINTÉTICA, não o wallet real ---
//
// O wallet (docs/examples/wallet) não tem Ticket/Order — a fixture reproduz
// a forma LITERAL do spec (nomes, join/on/where/as, exatamente como escrito
// em §6.3). O ÚNICO campo inventado é o shape de TicketVW: o spec não lista
// os campos dessa View. "orderId"/"status" bastam para provar a correlação
// (orderId vem do lado a) e o filtro (status vem do lado a, testado pelo
// "in"); nenhum dos dois nomeia "id" — Ticket e Order TÊM, cada um, um campo
// "id" (Ticket precisa de "id" por ser um ValueObject com identidade
// própria; "o.id" é literal do spec), e um campo de View chamado "id" seria
// AMBÍGUO entre os dois aliases (§design read-side 3.7 ponto 3, NFR-20) —
// exatamente o comportamento que TestEmitGetMyTicketsJoinAmbiguousViewField,
// abaixo, prova em separado.
const getMyTicketsFixtureModDs = `Module Tickets { }
`

const getMyTicketsFixtureDomainDs = `
ValueObject TicketId(string) {
    Valid { value.length() > 0 }
}

ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

ValueObject UserId(string) {
    Valid { value.length() > 0 }
}

Enum TicketStatus : string {
    Available = "AVAILABLE"
    Sold      = "SOLD"
    Used      = "USED"
    Cancelled = "CANCELLED"
}

ValueObject Ticket {
    id      TicketId
    orderId OrderId
    status  TicketStatus
}

ValueObject Order {
    id     OrderId
    userId UserId
}
`

const getMyTicketsFixtureReadDs = `
View TicketVW {
    orderId OrderId
    status  TicketStatus
}

Query GetMyTickets(userId UserId) -> List<TicketVW> {
    return list Ticket t
           join Order o on t.orderId == o.id
           where o.userId == userId
             and t.status in [TicketStatus.Sold, TicketStatus.Used]
           as TicketVW
}
`

// generateGetMyTicketsFixtureProject roda o orquestrador COMPLETO
// (codegen.Generate) sobre o Program da fixture Tickets — mesmo padrão de
// generateGetStatementFixtureProject (gentest_query_getstatement_test.go): o
// teste comportamental abaixo aciona GetMyTickets de verdade, então precisa
// do projeto INTEIRO (runtime vendorado incluso).
func generateGetMyTicketsFixtureProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    getMyTicketsFixtureModDs,
		"domain.ds": getMyTicketsFixtureDomainDs,
		"read.ds":   getMyTicketsFixtureReadDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de GetMyTickets (I5.1) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture GetMyTickets: %v", err)
	}
	return files
}

// ticketsQueriesFile localiza, entre os arquivos gerados, o que carrega a
// Query GetMyTickets (codegen.go emite "<pkg>/queries.go" por módulo, pkg =
// goname.PackageName("Tickets") = "tickets").
func ticketsQueriesFile(t *testing.T, files map[string][]byte) (path string, content []byte) {
	t.Helper()
	for p, c := range files {
		if strings.HasPrefix(p, "tickets/") && strings.HasSuffix(p, "queries.go") {
			return p, c
		}
	}
	var ks []string
	for k := range files {
		ks = append(ks, k)
	}
	t.Fatalf("esperava um arquivo \"tickets/*queries.go\" entre os arquivos gerados, não achei; arquivos: %v", ks)
	return "", nil
}

func emitGetMyTicketsQueriesFixture(t *testing.T) []byte {
	t.Helper()
	_, content := ticketsQueriesFile(t, filesToMap(generateGetMyTicketsFixtureProject(t)))
	return content
}

// TestEmitGetMyTicketsGolden prova a forma EXATA do corpo gerado (§design
// read-side 3.7): dois Collection[T] var (um por fonte de join), as duas
// fontes materializadas por inteiro (Select com Query[T]{} vazia), o loop
// aninhado com o "on" como if de correlação, o "where" (com "in") como
// segundo if, e a projeção "as V" contra os dois aliases.
func TestEmitGetMyTicketsGolden(t *testing.T) {
	got := string(emitGetMyTicketsQueriesFixture(t))
	for _, want := range []string{
		"var orderCollection = runtime.NewMemoryCollection[Order]()",
		"var ticketCollection = runtime.NewMemoryCollection[Ticket]()",
		"func GetMyTickets(ctx context.Context, store runtime.EventStore, userId UserId) ([]TicketVW, error)",
		"tmp1, err := ticketCollection.Select(ctx, runtime.Query[Ticket]{})",
		"tmp2, err := orderCollection.Select(ctx, runtime.Query[Order]{})",
		"tmp3 := make([]TicketVW, 0)",
		"for _, t := range tmp1",
		"for _, o := range tmp2",
		"if t.OrderId == o.Id",
		"if o.UserId == userId && slices.Contains([]TicketStatus{TicketStatusSold, TicketStatusUsed}, t.Status)",
		"tmp3 = append(tmp3, TicketVW{OrderId: t.OrderId, Status: t.Status})",
		"return tmp3, nil",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava a presença de %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "query_get_my_tickets.go.golden"), []byte(got))
}

// TestEmitGetMyTicketsDeterministic prova NFR-13: regerar duas vezes produz
// bytes idênticos.
func TestEmitGetMyTicketsDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitGetMyTicketsQueriesFixture(t)
	})
}

// TestEmitGetMyTicketsSmokeCompile prova que o projeto INTEIRO gerado
// compila e passa go vet num diretório isolado (NFR-14/NFR-17).
func TestEmitGetMyTicketsSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, filesToMap(generateGetMyTicketsFixtureProject(t)))
}

// getMyTicketsHandwrittenTest é o harness escrito à mão que aciona o código
// GERADO de verdade (mesmo espírito de getStatementHandwrittenTest,
// gentest_query_getstatement_test.go): Query não tem uma forma declarativa
// de "given" no Test DSL (esse é o caminho de Handle/Policy) — os dois
// Collection[T] (ticketCollection/orderCollection) são semeados via .Add
// diretamente. 2 Orders (O1 de U1, O2 de U2), 4 Tickets sob O1/O2 cobrindo
// as duas formas de exclusão: T3 (status errado, mesmo dono) e T4 (dono
// errado, status certo) — só T1/T2 (O1, Sold/Used) devem sobreviver.
const getMyTicketsHandwrittenTest = `package tickets

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
)

func TestGetMyTicketsHandwrittenRunGreen(t *testing.T) {
	ctx := context.Background()

	if err := orderCollection.Add(ctx, Order{Id: OrderId("O1"), UserId: UserId("U1")}); err != nil {
		t.Fatal(err)
	}
	if err := orderCollection.Add(ctx, Order{Id: OrderId("O2"), UserId: UserId("U2")}); err != nil {
		t.Fatal(err)
	}

	tickets := []Ticket{
		{Id: TicketId("T1"), OrderId: OrderId("O1"), Status: TicketStatusSold},
		{Id: TicketId("T2"), OrderId: OrderId("O1"), Status: TicketStatusUsed},
		{Id: TicketId("T3"), OrderId: OrderId("O1"), Status: TicketStatusAvailable},
		{Id: TicketId("T4"), OrderId: OrderId("O2"), Status: TicketStatusSold},
	}
	for _, tk := range tickets {
		if err := ticketCollection.Add(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}

	store := runtime.NewMemoryEventStore()
	got, err := GetMyTickets(ctx, store, UserId("U1"))
	if err != nil {
		t.Fatalf("GetMyTickets: erro inesperado: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("esperava 2 tickets de U1 (T1/T2, O1) — status certo e dono certo — got %d: %+v", len(got), got)
	}
	want := []TicketVW{
		{OrderId: OrderId("O1"), Status: TicketStatusSold},
		{OrderId: OrderId("O1"), Status: TicketStatusUsed},
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("got[%d] = %+v, want %+v", i, got[i], w)
		}
	}
}
`

// TestGetMyTicketsHandwrittenRunGreen roda "go test ./..." de VERDADE sobre
// o projeto gerado, com getMyTicketsHandwrittenTest injetado ao lado dos
// demais arquivos de tickets/.
func TestGetMyTicketsHandwrittenRunGreen(t *testing.T) {
	files := filesToMap(generateGetMyTicketsFixtureProject(t))
	files["tickets/tickets_handwritten_test.go"] = []byte(getMyTicketsHandwrittenTest)
	runGeneratedTests(t, files)
}

// TestEmitGetMyTicketsJoinAmbiguousViewField prova REQ-35.2/NFR-20: um campo
// de View presente nos DOIS aliases (aqui, "id" — Ticket E Order declaram
// "id") é recusado com um erro de geração claro em vez de escolher um dos
// dois em silêncio (§design read-side 3.7 ponto 3).
func TestEmitGetMyTicketsJoinAmbiguousViewField(t *testing.T) {
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    getMyTicketsFixtureModDs,
		"domain.ds": getMyTicketsFixtureDomainDs,
		"read.ds": `
View TicketAmbiguousVW {
    id     TicketId
    status TicketStatus
}

Query GetMyTicketsAmbiguous(userId UserId) -> List<TicketAmbiguousVW> {
    return list Ticket t
           join Order o on t.orderId == o.id
           where o.userId == userId
           as TicketAmbiguousVW
}
`,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de ambiguidade tem diagnósticos de erro inesperados:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	_, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err == nil {
		t.Fatal("esperava um erro de geração (campo \"id\" ambíguo entre os dois aliases do join), não houve erro")
	}
	if !strings.Contains(err.Error(), "ambíguo") {
		t.Fatalf("esperava um erro mencionando ambiguidade, got: %v", err)
	}
}
