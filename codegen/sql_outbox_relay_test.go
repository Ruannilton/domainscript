package codegen_test

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
)

// sql_outbox_relay_test.go prova a DoD de J2.3 (REQ-42.2/42.3, §design
// infra-providers 3.2): DurableOutbox (rtsrc/outbox.go.txt) entrega uma
// linha enfileirada por Tx.EnqueueOutbox (task J2.1) via o handler
// registrado em Subscribe, marcando-a entregue só depois do handler
// suceder; um handler que falha ("crash simulado") deixa a linha
// undelivered — attempts incrementa e a MESMA linha é re-entregue no
// próximo Tick (at-least-once). Roda de verdade sobre sqlite real (via
// gentest.WriteFiles/RunTests, mesmo padrão de TestSQLOutboxAtomicity) —
// sem depender de Postgres, já que o relay em si (ProcessBatch/Tick) não
// tem nenhuma variação por banco além do que o Dialect já isola.
const sqlOutboxRelayTest = `package sqlruntime_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type relayTestEvent struct {
	runtime.EventMeta
	Msg string
}

func (e *relayTestEvent) EventType() string { return "RelayTestEvent" }

func relayTestRegistry() map[string]runtime.EventFactory {
	return map[string]runtime.EventFactory{
		"RelayTestEvent": func() runtime.Event { return &relayTestEvent{} },
	}
}

func enqueueOneEvent(t *testing.T, db *sql.DB, msg string) {
	t.Helper()
	uow := sqlruntime.NewUnitOfWork(db, sqlruntimeEventFactories(), sqlruntime.SQLiteDialect())
	ctx := context.Background()
	err := uow.Run(ctx, func(tx runtime.Tx) error {
		if err := tx.Append("relay-agg", []runtime.Event{&relayTestEvent{Msg: msg}}); err != nil {
			return err
		}
		return tx.EnqueueOutbox([]runtime.Event{&relayTestEvent{Msg: msg}})
	})
	if err != nil {
		t.Fatalf("enqueueOneEvent: Run: %v", err)
	}
}

func sqlruntimeEventFactories() map[string]sqlruntime.EventFactory {
	return map[string]sqlruntime.EventFactory{
		"RelayTestEvent": func() runtime.Event { return &relayTestEvent{} },
	}
}

// TestDurableOutboxDeliversAndMarks prova REQ-42.2/42.3: uma linha
// enfileirada é entregue ao handler assinado, e um segundo Tick não a
// entrega de novo (já marcada) — nem-antes-do-handler, nem-duas-vezes no
// caminho feliz.
func TestDurableOutboxDeliversAndMarks(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "relay.db")
	db, err := sqlruntime.Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := sqlruntime.NewEventStore(ctx, db, sqlruntimeEventFactories(), sqlruntime.SQLiteDialect()); err != nil {
		t.Fatalf("NewEventStore: %v", err)
	}
	enqueueOneEvent(t, db, "hello")

	store := sqlruntime.NewOutboxStore(db, sqlruntime.SQLiteDialect())
	outbox := runtime.NewDurableOutbox(store, relayTestRegistry())

	var delivered []string
	outbox.Subscribe("RelayTestEvent", func(ctx context.Context, ev runtime.Event) error {
		delivered = append(delivered, ev.(*relayTestEvent).Msg)
		return nil
	})

	n, err := outbox.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick (1º): %v", err)
	}
	if n != 1 {
		t.Fatalf("Tick (1º) processou %d linhas, want 1", n)
	}
	if len(delivered) != 1 || delivered[0] != "hello" {
		t.Fatalf("delivered = %v, want [\"hello\"]", delivered)
	}

	var deliveredAt sql.NullTime
	if err := db.QueryRow("SELECT delivered_at FROM outbox WHERE event_type = 'RelayTestEvent'").Scan(&deliveredAt); err != nil {
		t.Fatalf("query delivered_at: %v", err)
	}
	if !deliveredAt.Valid {
		t.Fatal("delivered_at continua NULL após entrega bem-sucedida")
	}

	// 2º Tick: nada de novo a escanear (a linha já está marcada) — o
	// handler NÃO roda de novo.
	n, err = outbox.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick (2º): %v", err)
	}
	if n != 0 {
		t.Fatalf("Tick (2º) processou %d linhas, want 0 (já entregue)", n)
	}
	if len(delivered) != 1 {
		t.Fatalf("delivered após 2º Tick = %v, want ainda só [\"hello\"] (sem reentrega)", delivered)
	}
}

// TestDurableOutboxRetriesOnHandlerFailure prova o "crash simulado": um
// handler que falha na 1ª tentativa deixa a linha undelivered (attempts
// incrementa, delivered_at continua NULL) — a MESMA linha é reprocessada no
// próximo Tick, e um handler que sucede na 2ª tentativa a entrega
// (at-least-once, REQ-42.2).
func TestDurableOutboxRetriesOnHandlerFailure(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "relay-retry.db")
	db, err := sqlruntime.Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := sqlruntime.NewEventStore(ctx, db, sqlruntimeEventFactories(), sqlruntime.SQLiteDialect()); err != nil {
		t.Fatalf("NewEventStore: %v", err)
	}
	enqueueOneEvent(t, db, "flaky")

	store := sqlruntime.NewOutboxStore(db, sqlruntime.SQLiteDialect())
	outbox := runtime.NewDurableOutbox(store, relayTestRegistry())

	attempt := 0
	var delivered []string
	boom := errors.New("crash simulado")
	outbox.Subscribe("RelayTestEvent", func(ctx context.Context, ev runtime.Event) error {
		attempt++
		if attempt == 1 {
			return boom
		}
		delivered = append(delivered, ev.(*relayTestEvent).Msg)
		return nil
	})

	// 1º Tick: handler falha — linha continua undelivered, attempts sobe.
	n, err := outbox.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick (1º, handler falha): %v", err)
	}
	if n != 1 {
		t.Fatalf("Tick (1º) processou %d linhas, want 1 (escaneada, mesmo com falha)", n)
	}
	if len(delivered) != 0 {
		t.Fatalf("delivered após 1º Tick (falho) = %v, want vazio", delivered)
	}

	var deliveredAt sql.NullTime
	var attempts int
	if err := db.QueryRow("SELECT delivered_at, attempts FROM outbox WHERE event_type = 'RelayTestEvent'").Scan(&deliveredAt, &attempts); err != nil {
		t.Fatalf("query após 1º Tick: %v", err)
	}
	if deliveredAt.Valid {
		t.Fatal("delivered_at foi marcado mesmo com o handler tendo falhado")
	}
	if attempts != 1 {
		t.Fatalf("attempts após 1º Tick = %d, want 1", attempts)
	}

	// 2º Tick: MESMA linha (undelivered) é re-escaneada; handler sucede
	// desta vez — at-least-once cumprido.
	n, err = outbox.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick (2º, re-entrega): %v", err)
	}
	if n != 1 {
		t.Fatalf("Tick (2º) processou %d linhas, want 1 (re-entrega da mesma linha)", n)
	}
	if len(delivered) != 1 || delivered[0] != "flaky" {
		t.Fatalf("delivered após 2º Tick = %v, want [\"flaky\"]", delivered)
	}

	if err := db.QueryRow("SELECT delivered_at FROM outbox WHERE event_type = 'RelayTestEvent'").Scan(&deliveredAt); err != nil {
		t.Fatalf("query após 2º Tick: %v", err)
	}
	if !deliveredAt.Valid {
		t.Fatal("delivered_at continua NULL após a re-entrega bem-sucedida")
	}
}
`

// TestSQLOutboxRelay roda sqlOutboxRelayTest de verdade sobre um projeto Go
// mínimo (só runtime/sqlruntime vendorados, mesmo material que
// buildSQLRuntimeProjectFiles monta).
func TestSQLOutboxRelay(t *testing.T) {
	files := buildSQLRuntimeProjectFiles(t)
	files[path.Join("sqlruntime", "outbox_relay_test.go")] = []byte(sqlOutboxRelayTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
