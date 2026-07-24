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

// producer_outbox_test.go prova a DoD de K3.4 (ISSUE-9/REQ-51.7, §design
// correcoes-issues-9-10-11 4.4/4.5): uma fixture SINTÉTICA MÍNIMA e DEDICADA
// ao lado PRODUTOR do outbox durável — 1 módulo produtor (Database sqlite
// real + canal de saída `provider:"rabbitmq"`) + 1 módulo consumidor (Policy
// cross-service simples) — espelhando como o lado consumidor ganhou
// `decl_policy_outbox_test.go` (J2.5). Isolada de propósito da complexidade
// multi-provider da âncora de J6 (`anchor_fixture_test.go`, postgres+2PC+
// idempotência+telemetria) e da fixture Ledger de `sql_adapter_test.go`
// (2PC/multi-Database): aqui o único objetivo é o wiring do produtor
// (K3.1-K3.3) sobre a forma mais simples possível que ainda ativa
// `durableProducer` — Database sqlite ÚNICO (não-2PC) + canal rabbitmq.
//
// Provider deliberadamente "sqlite" (não "postgres" como a âncora/Alpha):
// `recognizedSQLProvider` reconhece os dois (sql_wiring.go), então sqlite já
// ativa o caminho produtor-durável — e usar sqlite com um DSN de arquivo real
// (mesmo truque de `ledgerModDs`/`ledgerSQLitePaths`, sql_adapter_test.go)
// deixa o teste COMPORTAMENTAL abaixo (TestProducerOutboxDurableRelayRetries
// ReDelivery) rodar sobre o MESMO wiring gerado que o teste de wiring prova
// — sem a decolagem "gera com postgres, testa com sqlite" que outras partes
// deste pacote precisam (postgres não abre conexão real em smoke compile).
//
// Módulo produtor "Orders": Aggregate Order com DOIS Handle — Place emite
// PublicEvent OrderPlaced (o único evento que o canal Orders->Shipping
// carrega) e Touch emite Event OrderTouched (interno, NUNCA cross-service) —
// para provar o filtro REQ-51.4 também sobre o caminho GERADO (o conjunto de
// event_type da UoW vem do canal real da fixture, não de um mapa escrito à
// mão como em sql_producer_parity_test.go). Módulo consumidor "Shipping":
// uma Policy AtLeastOnce reagindo a OrderPlaced — só para o canal ter dois
// lados reais (não exercita durabilidade do consumidor, já coberta por
// decl_policy_outbox_test.go).

// producerOutboxOrdersModDsTemplate: %q é o DSN do arquivo sqlite real (ver
// producerOutboxSQLitePaths) — mesmo padrão de ledgerModDs.
const producerOutboxOrdersModDsTemplate = `Module Orders {
    Database MainDb {
        provider: "sqlite"
        dsn: %q
        manages: [Order]
    }
}
`

const producerOutboxOrdersDomainDs = `
ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

PublicEvent OrderPlaced { id OrderId }
Event OrderTouched { id OrderId }

Aggregate Order {
    strategy EventSourced

    state {
        id OrderId
    }

    access {
        Place requires caller.authenticated
        Touch requires caller.authenticated
    }

    Handle Place() {
        emit OrderPlaced(self.id)
    }

    Handle Touch() {
        emit OrderTouched(self.id)
    }

    Apply OrderPlaced {
        state.id = event.id
    }

    Apply OrderTouched {
    }
}
`

const producerOutboxOrdersApplicationDs = `
Command PlaceOrder {
    id ref Order
}

Command TouchOrder {
    id ref Order
}

UseCase PlaceOrderUseCase handles PlaceOrder {
    execute {
        order = load Order(cmd.id)
        order.Place()
    }
}

UseCase TouchOrderUseCase handles TouchOrder {
    execute {
        order = load Order(cmd.id)
        order.Touch()
    }
}
`

const producerOutboxOrdersInterfaceDs = `
Interface HTTP {
    POST "/orders" -> PlaceOrderUseCase
    POST "/orders/touch" -> TouchOrderUseCase
}
`

const producerOutboxShippingModDs = `Module Shipping { }
`

const producerOutboxShippingPolicyDs = `Policy NotifyShipping on OrderPlaced {
    delivery AtLeastOnce
    execute { return }
}
`

// producerOutboxTopologyDs: 2 services, 1 canal `queue provider:"rabbitmq"`
// Orders -> Shipping — mesma forma de channelFixtureRabbitMQTopologyDs
// (channel_rabbitmq_test.go), workers/timeout/circuitBreaker inclusos porque
// o parser/checker os aceita/exige na mesma posição daquela fixture já
// provada.
const producerOutboxTopologyDs = `Topology {
    services {
        OrdersSvc { modules: [Orders] }
        ShippingSvc { modules: [Shipping] }
    }
    channels {
        Orders -> Shipping {
            via: queue
            provider: "rabbitmq"
            connection: env("AMQP_URL")
            orderBy: id
            workers { concurrency: 5 maxRate: 100 batchSize: 10 }
            timeout: 10s
            circuitBreaker: { threshold: 5 cooldown: 30s }
        }
    }
}
`

// producerOutboxSQLitePaths devolve um caminho de arquivo sqlite real (não
// ":memory:"), num diretório temporário PRÓPRIO — mesma razão de
// ledgerSQLitePaths: o teste comportamental abre a MESMA base em conexões
// diferentes (a do main.go simulado no teste de wiring nunca abre de fato,
// mas o teste comportamental abre via sqlruntime.Open/EventRegistry do
// pacote gerado).
func producerOutboxSQLitePaths(t *testing.T) (dsn string) {
	t.Helper()
	return filepath.Join(t.TempDir(), "orders.db")
}

var producerOutboxGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateProducerOutboxProject escreve a fixture em disco, resolve via
// driver.CheckProject e gera o projeto Go completo — mesmo padrão de
// generateLedgerProject/generateOutboxProject.
func generateProducerOutboxProject(t *testing.T, dsn string) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":           producerOutboxTopologyDs,
		"orders/mod.ds":         fmt.Sprintf(producerOutboxOrdersModDsTemplate, filepath.ToSlash(dsn)),
		"orders/domain.ds":      producerOutboxOrdersDomainDs,
		"orders/application.ds": producerOutboxOrdersApplicationDs,
		"orders/interface.ds":   producerOutboxOrdersInterfaceDs,
		"shipping/mod.ds":       producerOutboxShippingModDs,
		"shipping/policy.ds":    producerOutboxShippingPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture dedicada de produtor durável (K3.4) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, producerOutboxGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture dedicada de produtor durável: %v", err)
	}
	return files
}

// TestProducerOutboxFixtureWiringAndSmokeCompile prova, sobre esta fixture
// DEDICADA (não a âncora de J6 nem o Alpha/Beta de channel_rabbitmq_test.go),
// o wiring do produtor durável K3.1-K3.3: cmd/ordersSvc/main.go abre a
// conexão sqlite real, constrói a UoW via NewOutboxUnitOfWork com o conjunto
// de event_type do canal (só "OrderPlaced" — OrderTouched é interno e NUNCA
// entra nesse conjunto, REQ-51.4 já na GERAÇÃO), NÃO passa o canal para a
// UoW, e monta o OutboxStore + DurableOutbox com o canal como publisher +
// relay/cleanup. Fecha com gentest.SmokeCompile (DoD: "smoke compile
// limpo").
func TestProducerOutboxFixtureWiringAndSmokeCompile(t *testing.T) {
	dsn := producerOutboxSQLitePaths(t)
	files := generateProducerOutboxProject(t, dsn)
	m := filesToMap(files)

	main, ok := m["cmd/orderssvc/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/orderssvc/main.go (service OrdersSvc, módulo Orders), não achei:\n%v", filePathsForTest(files))
	}
	mainStr := string(main)

	for _, want := range []string{
		"ordersDB, err := sqlruntime.Open(",
		// K3.3-P2/P3: enqueue-in-tx pela UoW, SÓ com o conjunto de event_type
		// do canal (REQ-51.4) — OrderTouched (interno) não aparece aqui.
		`uow := sqlruntime.NewOutboxUnitOfWork(ordersDB, orders.EventRegistry(), sqlruntime.SQLiteDialect(), map[string]bool{"OrderPlaced": true})`,
		// K3.3-P4: OutboxStore + DurableOutbox com o canal como publisher + relay/cleanup.
		"ordersOutboxStore := sqlruntime.NewOutboxStore(ordersDB, sqlruntime.SQLiteDialect())",
		"ordersOutbox := runtime.NewDurableOutbox(ordersOutboxStore, map[string]runtime.EventFactory{",
		`"OrderPlaced": func() runtime.Event { return &contracts.OrderPlaced{} },`,
		"}, ordersChannel)",
		"go ordersOutbox.Start(workerCtx)",
	} {
		if !strings.Contains(mainStr, want) {
			t.Errorf("esperava %q em cmd/orderssvc/main.go, não achei:\n%s", want, mainStr)
		}
	}
	if strings.Contains(mainStr, "OrderTouched") {
		t.Errorf("cmd/orderssvc/main.go não deveria mencionar OrderTouched (evento interno, filtro REQ-51.4 na geração):\n%s", mainStr)
	}
	if strings.Contains(mainStr, "runtime.NewUnitOfWork(store, ordersChannel)") {
		t.Errorf("cmd/orderssvc/main.go ainda constrói a UoW do produtor sobre a store em memória (pré-condição K3.2 não aplicada):\n%s", mainStr)
	}
	if strings.Contains(mainStr, "sqlruntime.NewUnitOfWork(ordersDB") {
		t.Errorf("cmd/orderssvc/main.go ainda passa o canal como publisher da UoW (troca de publisher K3.3 não aplicada):\n%s", mainStr)
	}

	gentest.SmokeCompile(t, m)
}

// producerOutboxCrashSimulationTest é embutido no pacote "orders" GERADO
// (white-box: `package orders`, não `orders_test`) — precisa da var de
// pacote não-exportada `uow` (var uow runtime.UnitOfWork, decl_usecase.go) e
// de EventRegistry()/PlaceOrderUseCase/TouchOrderUseCase exportados. Mesmo
// padrão de ledgerProducerOutboxBehaviorTest (sql_producer_parity_test.go),
// mas o passo genuinamente NOVO de K3.4 é a Parte 2: um `fakePublisher` (o
// MESMO idioma de sql_outbox_channel_test.go, redeclarado aqui porque é um
// pacote/projeto gerado diferente) falhando na 1ª tentativa sobre a linha
// que o caminho GERADO do produtor (NewOutboxUnitOfWork, através de
// PlaceOrderUseCase real) enfileirou — não um `tx.EnqueueOutbox` manual.
//
// Parte 1 (reafirma K3.3, agora sobre esta fixture dedicada): PlaceOrder
// enfileira em outbox+events na mesma tx (o event_type "OrderPlaced" está no
// conjunto que o canal da fixture carrega); TouchOrder — mesmo `uow`, mesmo
// conjunto de event_type — apensa OrderTouched a events mas NUNCA enfileira
// no outbox (REQ-51.4: o evento interno não está no conjunto).
//
// Parte 2 (o gap que K3.4 fecha): sem broker vivo, um `runtime.DurableOutbox`
// construído manualmente (mesma forma que emitProducerOutboxRelay monta em
// main.go, mas orquestrado aqui para controlar o timing via Tick em vez de
// Start) sobre o MESMO OutboxStore processa a linha que PlaceOrderUseCase
// enfileirou: o 1º Tick tenta publicar, o fakePublisher falha (crash
// simulado), a linha fica com attempts=1 e delivered_at NULL; o 2º Tick
// re-escaneia a MESMA linha e o Publish sucede — delivered_at é marcado,
// nenhum evento cross-service é perdido.
var producerOutboxCrashSimulationTest = `package orders

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type producerOutboxCrashCaller struct{ id string }

func (c producerOutboxCrashCaller) Authenticated() bool { return true }
func (c producerOutboxCrashCaller) ID() string          { return c.id }
func (c producerOutboxCrashCaller) HasRole(string) bool { return false }

// producerOutboxCrashDSN é substituído por
// TestProducerOutboxDurableRelayRetriesAfterCrash antes de escrever o
// arquivo (strings.Replace sobre este template) — mesma técnica de
// mainDbDSNForOutboxTest em sql_producer_parity_test.go.
var producerOutboxCrashDSN = "__PRODUCER_OUTBOX_CRASH_DSN__"

// producerOutboxFakePublisher espelha fakePublisher de
// sql_outbox_channel_test.go (mesmo pacote sqlruntime_test não está
// acessível daqui — pacotes gerados diferentes — então é redeclarado): falha
// exatamente uma vez (failNext), depois publica normalmente.
type producerOutboxFakePublisher struct {
	published []string
	failNext  bool
}

func (p *producerOutboxFakePublisher) Publish(ctx context.Context, ev runtime.Event) error {
	if p.failNext {
		p.failNext = false
		return errors.New("publish falhou (crash simulado)")
	}
	p.published = append(p.published, string(ev.(*OrderPlaced).Id))
	return nil
}

func TestProducerOutboxDurableRelayRetriesAfterCrash(t *testing.T) {
	db, err := sqlruntime.Open(producerOutboxCrashDSN)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	// NewEventStore roda a migração de schema (events + outbox); a UoW de
	// banco único (NewOutboxUnitOfWork) não migra sozinha.
	if _, err := sqlruntime.NewEventStore(ctx, db, EventRegistry(), sqlruntime.SQLiteDialect()); err != nil {
		t.Fatalf("NewEventStore (migração de schema): %v", err)
	}

	// Parte 1: caminho GERADO do produtor — mesmo conjunto de event_type que
	// o canal Orders->Shipping da fixture carrega (só "OrderPlaced").
	uow = sqlruntime.NewOutboxUnitOfWork(db, EventRegistry(), sqlruntime.SQLiteDialect(), map[string]bool{"OrderPlaced": true})
	callerCtx := runtime.WithCaller(ctx, producerOutboxCrashCaller{id: "ord-1"})

	if err := PlaceOrderUseCase(callerCtx, PlaceOrder{Id: OrderId("ord-1")}); err != nil {
		t.Fatalf("PlaceOrderUseCase: %v", err)
	}

	var eventsCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE aggregate_id = ?", "ord-1").Scan(&eventsCount); err != nil {
		t.Fatalf("consulta events (após Place): %v", err)
	}
	if eventsCount != 1 {
		t.Fatalf("esperava 1 linha em events para ord-1 após Place, achei %d", eventsCount)
	}

	var outboxCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox WHERE event_type = 'OrderPlaced'").Scan(&outboxCount); err != nil {
		t.Fatalf("consulta outbox (após Place): %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("esperava 1 linha em outbox para OrderPlaced (enfileirada na tx), achei %d", outboxCount)
	}
	var deliveredAt sql.NullTime
	if err := db.QueryRowContext(ctx, "SELECT delivered_at FROM outbox WHERE event_type = 'OrderPlaced'").Scan(&deliveredAt); err != nil {
		t.Fatalf("consulta delivered_at (após Place): %v", err)
	}
	if deliveredAt.Valid {
		t.Fatal("a linha do outbox não deveria estar entregue ainda: a UoW só ENFILEIRA, quem publica é o relay")
	}

	// REQ-51.4: Touch emite um evento INTERNO (OrderTouched) — apensado ao
	// stream, mas o conjunto de event_type da MESMA uow não o contém, então
	// nunca enfileirado no outbox.
	if err := TouchOrderUseCase(callerCtx, TouchOrder{Id: OrderId("ord-1")}); err != nil {
		t.Fatalf("TouchOrderUseCase: %v", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE aggregate_id = ?", "ord-1").Scan(&eventsCount); err != nil {
		t.Fatalf("consulta events (após Touch): %v", err)
	}
	if eventsCount != 2 {
		t.Fatalf("esperava 2 linhas em events para ord-1 após Touch (Append aconteceu independente do filtro), achei %d", eventsCount)
	}
	var orderTouchedOutboxCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox WHERE event_type = 'OrderTouched'").Scan(&orderTouchedOutboxCount); err != nil {
		t.Fatalf("consulta outbox (OrderTouched): %v", err)
	}
	if orderTouchedOutboxCount != 0 {
		t.Fatalf("OrderTouched (evento interno) não deveria ter sido enfileirado no outbox (filtro REQ-51.4), achei %d linha(s)", orderTouchedOutboxCount)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox WHERE event_type = 'OrderPlaced'").Scan(&outboxCount); err != nil {
		t.Fatalf("consulta outbox (OrderPlaced, após Touch): %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox deveria continuar com 1 linha de OrderPlaced após Touch, achei %d", outboxCount)
	}

	// Parte 2 — o gap que K3.4 fecha: sem broker vivo, um DurableOutbox
	// manual (mesma forma que emitProducerOutboxRelay monta em main.go)
	// sobre o MESMO OutboxStore processa a linha que PlaceOrderUseCase
	// enfileirou de verdade — "o caminho gerado do produtor", não um
	// tx.EnqueueOutbox manual como em sql_outbox_channel_test.go.
	store := sqlruntime.NewOutboxStore(db, sqlruntime.SQLiteDialect())
	registry := map[string]runtime.EventFactory{
		"OrderPlaced": func() runtime.Event { return &OrderPlaced{} },
	}
	pub := &producerOutboxFakePublisher{failNext: true}
	outbox := runtime.NewDurableOutbox(store, registry, pub)

	// 1º Tick: Publish falha (crash simulado) — a linha continua undelivered,
	// attempts sobe.
	n, err := outbox.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick (1º, publish falha): %v", err)
	}
	if n != 1 {
		t.Fatalf("Tick (1º) processou %d linhas, want 1", n)
	}
	if len(pub.published) != 0 {
		t.Fatalf("published após 1º Tick (falho) = %v, want vazio", pub.published)
	}

	var attempts int
	if err := db.QueryRowContext(ctx, "SELECT delivered_at, attempts FROM outbox WHERE event_type = 'OrderPlaced'").Scan(&deliveredAt, &attempts); err != nil {
		t.Fatalf("consulta após 1º Tick: %v", err)
	}
	if deliveredAt.Valid {
		t.Fatal("delivered_at foi marcado mesmo com Publish tendo falhado")
	}
	if attempts != 1 {
		t.Fatalf("attempts após 1º Tick = %d, want 1", attempts)
	}

	// 2º Tick: a MESMA linha (undelivered) é re-escaneada; Publish sucede
	// desta vez — nenhum evento cross-service foi perdido.
	n, err = outbox.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick (2º, re-entrega): %v", err)
	}
	if n != 1 {
		t.Fatalf("Tick (2º) processou %d linhas, want 1 (re-entrega da mesma linha)", n)
	}
	if len(pub.published) != 1 || pub.published[0] != "ord-1" {
		t.Fatalf("published após 2º Tick = %v, want [\"ord-1\"]", pub.published)
	}

	if err := db.QueryRowContext(ctx, "SELECT delivered_at FROM outbox WHERE event_type = 'OrderPlaced'").Scan(&deliveredAt); err != nil {
		t.Fatalf("consulta após 2º Tick: %v", err)
	}
	if !deliveredAt.Valid {
		t.Fatal("delivered_at continua NULL após a re-entrega bem-sucedida")
	}
}
`

// TestProducerOutboxDurableRelayRetriesAfterCrash gera a fixture DEDICADA
// deste arquivo, substitui o placeholder do DSN pelo caminho real do arquivo
// sqlite e roda producerOutboxCrashSimulationTest de verdade via "go test"
// sobre o pacote "orders" GERADO (gentest.WriteFiles/RunTests) — o "crash
// simulado" fim-a-fim que K3.4 pede: nenhum evento cross-service perdido
// entre o commit e a publicação, exercitando o caminho gerado do produtor
// (NewOutboxUnitOfWork via um UseCase real), não só o seam manual de
// sql_outbox_channel_test.go.
func TestProducerOutboxDurableRelayRetriesAfterCrash(t *testing.T) {
	dsn := producerOutboxSQLitePaths(t)
	files := filesToMap(generateProducerOutboxProject(t, dsn))
	testSrc := strings.Replace(producerOutboxCrashSimulationTest, "__PRODUCER_OUTBOX_CRASH_DSN__", filepath.ToSlash(dsn), 1)
	files[filepath.Join("orders", "producer_outbox_crash_test.go")] = []byte(testSrc)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
