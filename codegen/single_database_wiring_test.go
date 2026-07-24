package codegen

import (
	"strings"
	"testing"

	"domainscript/codegen/emit"
	"domainscript/program"
)

// single_database_wiring_test.go prova a DoD de K3.2 (ISSUE-9/REQ-51.5,
// §design correcoes-issues-9-10-11 4.2-P1): emitSingleDatabaseWiring, o
// helper novo desta task, isolado do resto de generateCmdMainFile — mesmo
// padrão leve de sql_wiring_test.go/durable_producer_test.go (construção
// direta de *program.Program/*emit.Emitter, sem passar pelo driver/parser
// inteiro).

// singleDatabaseWiringFixture monta um *program.Program de um único módulo
// "Orders" com um Database "OrdersDb" real (provider dado) — o mínimo que
// emitSingleDatabaseWiring precisa para resolver connection/provider.
func singleDatabaseWiringFixture(provider string) *program.Program {
	return &program.Program{
		Modules: map[string]*program.Module{
			"Orders": {
				Name: "Orders",
				Databases: map[string]*program.Database{
					"OrdersDb": {Name: "OrdersDb", Module: "Orders", Provider: provider, DSN: "orders.db", Manages: []string{"Order"}},
				},
			},
		},
	}
}

// TestEmitSingleDatabaseWiringShape prova a forma de emissão (runMode=false,
// o caso comum): abre a conexão real (provider.openFunc), sem defer/fail-
// fast em bloco (emitFailFast/emitDeferClose são no-ops fora de runMode,
// ver run_error.go), e constrói "uow := sqlruntime.NewUnitOfWork(db,
// EventRegistry(), dialect, <canal>)" com o CANAL como publisher —
// inalterado (K3.2 não troca o publisher, isso é K3.3).
func TestEmitSingleDatabaseWiringShape(t *testing.T) {
	prog := singleDatabaseWiringFixture("postgres")
	e := emit.New("main")

	var wiringErr error
	e.Block("func run() error", func() {
		wiringErr = emitSingleDatabaseWiring(e, prog, "Orders", "orders", "OrdersDb", "ordersChannel", false)
	})
	if wiringErr != nil {
		t.Fatalf("emitSingleDatabaseWiring: erro inesperado: %v", wiringErr)
	}
	src, err := e.Bytes()
	if err != nil {
		t.Fatalf("e.Bytes(): erro inesperado: %v", err)
	}
	got := string(src)

	for _, want := range []string{
		`ordersDB, err := sqlruntime.OpenPostgres("orders.db")`,
		"uow := sqlruntime.NewUnitOfWork(ordersDB, orders.EventRegistry(), sqlruntime.PostgresDialect(), ordersChannel)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q na emissão de emitSingleDatabaseWiring, não achei:\n%s", want, got)
		}
	}
	// runMode=false: nenhum defer Close() (emitDeferClose é no-op fora de
	// runMode, mesma convenção de emitXADatabaseWiring/emitOutboxDatabaseWiring).
	if strings.Contains(got, "defer ordersDB.Close()") {
		t.Fatalf("runMode=false não deveria emitir defer Close():\n%s", got)
	}
	// K3.2 não constrói NENHUM EventStore/OutboxStore intermediário — só a
	// UnitOfWork direto sobre o *sql.DB (uow.go.txt:68, diferente de
	// emitXADatabaseWiring).
	if strings.Contains(got, "NewEventStore") || strings.Contains(got, "NewOutboxStore") {
		t.Fatalf("emitSingleDatabaseWiring não deveria montar EventStore/OutboxStore intermediário:\n%s", got)
	}
}

// TestEmitSingleDatabaseWiringRunModeEmitsFailFastAndDeferClose prova a
// variante runMode=true (2+ recursos fallíveis no mesmo main.go, J6.2): o
// erro de abertura vira "return err" (não log.Fatal inline) e o defer
// Close() aparece — mesma convenção de emitXADatabaseWiring/
// emitOutboxDatabaseWiring.
func TestEmitSingleDatabaseWiringRunModeEmitsFailFastAndDeferClose(t *testing.T) {
	prog := singleDatabaseWiringFixture("postgres")
	e := emit.New("main")

	var wiringErr error
	e.Block("func run() error", func() {
		wiringErr = emitSingleDatabaseWiring(e, prog, "Orders", "orders", "OrdersDb", "ordersChannel", true)
		// generateCmdMainFile já importa "log" incondicionalmente (usado por
		// func main() { if err := run(); err != nil { log.Fatal(err) } },
		// codegen.go) — reproduzido aqui só para satisfazer a validação de
		// "import registrado e usado" do Emitter isolado deste teste.
		e.Line("log.Println(\"wired\")")
		e.Line("return nil")
	})
	if wiringErr != nil {
		t.Fatalf("emitSingleDatabaseWiring: erro inesperado: %v", wiringErr)
	}
	src, err := e.Bytes()
	if err != nil {
		t.Fatalf("e.Bytes(): erro inesperado: %v", err)
	}
	got := string(src)

	for _, want := range []string{
		"if err != nil {\n\t\treturn err\n\t}",
		"defer ordersDB.Close()",
		"uow := sqlruntime.NewUnitOfWork(ordersDB, orders.EventRegistry(), sqlruntime.PostgresDialect(), ordersChannel)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q (runMode=true), não achei:\n%s", want, got)
		}
	}
	if strings.Contains(got, "log.Fatal") {
		t.Fatalf("runMode=true não deveria usar log.Fatal (o err propaga via return, ver emitFailFast):\n%s", got)
	}
}

// TestEmitSingleDatabaseWiringUnknownDatabaseIsGenerationBug prova o guard
// de bug-de-geração (mesma convenção de emitXADatabaseWiring/
// emitOutboxDatabaseWiring): um dbName que não existe no módulo é erro
// claro, não panic.
func TestEmitSingleDatabaseWiringUnknownDatabaseIsGenerationBug(t *testing.T) {
	prog := singleDatabaseWiringFixture("postgres")
	e := emit.New("main")

	err := emitSingleDatabaseWiring(e, prog, "Orders", "orders", "NoSuchDb", "ordersChannel", false)
	if err == nil {
		t.Fatal("emitSingleDatabaseWiring(dbName inexistente) = nil, want erro")
	}
}
