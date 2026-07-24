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
// ver run_error.go), e constrói "uow := sqlruntime.NewOutboxUnitOfWork(db,
// EventRegistry(), dialect, map[string]bool{...})" — K3.3 trocou o publisher:
// a UoW NÃO recebe mais o canal (agora recebe o conjunto de event_type que o
// canal carrega e os enfileira no outbox dentro da tx; quem publica é o relay
// do DurableOutbox, montado por emitProducerOutboxRelay).
func TestEmitSingleDatabaseWiringShape(t *testing.T) {
	prog := singleDatabaseWiringFixture("postgres")
	e := emit.New("main")

	var wiringErr error
	e.Block("func run() error", func() {
		wiringErr = emitSingleDatabaseWiring(e, prog, "Orders", "orders", "OrdersDb", []string{"OrderPlaced"}, false)
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
		`uow := sqlruntime.NewOutboxUnitOfWork(ordersDB, orders.EventRegistry(), sqlruntime.PostgresDialect(), map[string]bool{"OrderPlaced": true})`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q na emissão de emitSingleDatabaseWiring, não achei:\n%s", want, got)
		}
	}
	// K3.3: o canal NÃO é mais passado como publisher da UoW (é o publisher do
	// relay do DurableOutbox, montado à parte por emitProducerOutboxRelay).
	if strings.Contains(got, "ordersChannel") {
		t.Fatalf("emitSingleDatabaseWiring não deveria mais referenciar o canal (a troca de publisher K3.3):\n%s", got)
	}
	// runMode=false: nenhum defer Close() (emitDeferClose é no-op fora de
	// runMode, mesma convenção de emitXADatabaseWiring/emitOutboxDatabaseWiring).
	if strings.Contains(got, "defer ordersDB.Close()") {
		t.Fatalf("runMode=false não deveria emitir defer Close():\n%s", got)
	}
	// emitSingleDatabaseWiring não constrói NENHUM EventStore/OutboxStore — só a
	// UnitOfWork direto sobre o *sql.DB (uow.go.txt, diferente de
	// emitXADatabaseWiring). O OutboxStore do produtor é emitido à parte, por
	// emitProducerOutboxRelay (após workerCtx, em generateCmdMainFile).
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
		wiringErr = emitSingleDatabaseWiring(e, prog, "Orders", "orders", "OrdersDb", []string{"OrderPlaced"}, true)
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
		`uow := sqlruntime.NewOutboxUnitOfWork(ordersDB, orders.EventRegistry(), sqlruntime.PostgresDialect(), map[string]bool{"OrderPlaced": true})`,
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

	err := emitSingleDatabaseWiring(e, prog, "Orders", "orders", "NoSuchDb", []string{"OrderPlaced"}, false)
	if err == nil {
		t.Fatal("emitSingleDatabaseWiring(dbName inexistente) = nil, want erro")
	}
}
