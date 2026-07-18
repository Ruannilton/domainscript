package codegen_test

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
)

// sql_outbox_cleanup_test.go prova a DoD de J2.5.a (REQ-42.7, §design
// infra-providers "boas práticas de limpeza/retenção"): OutboxStore.
// PurgeDelivered (sqlrt/outbox.go.txt) e DurableOutbox.Cleanup
// (rtsrc/outbox.go.txt, que delega a ele) apagam só linhas entregues
// (delivered_at NOT NULL) mais velhas que o cutoff — uma linha entregue
// DENTRO da janela e qualquer linha undelivered sobrevivem. Roda de verdade
// sobre sqlite real (gentest.WriteFiles/RunTests, mesmo padrão dos demais
// testes de outbox).
const sqlOutboxCleanupTest = `package sqlruntime_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type cleanupTestEvent struct {
	runtime.EventMeta
	Msg string
}

func (e *cleanupTestEvent) EventType() string { return "CleanupTestEvent" }

func cleanupTestFactories() map[string]sqlruntime.EventFactory {
	return map[string]sqlruntime.EventFactory{
		"CleanupTestEvent": func() runtime.Event { return &cleanupTestEvent{} },
	}
}

func cleanupTestRegistry() map[string]runtime.EventFactory {
	return map[string]runtime.EventFactory{
		"CleanupTestEvent": func() runtime.Event { return &cleanupTestEvent{} },
	}
}

func enqueueOneCleanupEvent(t *testing.T, db *sql.DB, aggID, msg string) {
	t.Helper()
	uow := sqlruntime.NewUnitOfWork(db, cleanupTestFactories(), sqlruntime.SQLiteDialect())
	ctx := context.Background()
	err := uow.Run(ctx, func(tx runtime.Tx) error {
		if err := tx.Append(aggID, []runtime.Event{&cleanupTestEvent{Msg: msg}}); err != nil {
			return err
		}
		return tx.EnqueueOutbox([]runtime.Event{&cleanupTestEvent{Msg: msg}})
	})
	if err != nil {
		t.Fatalf("enqueueOneCleanupEvent(%q): Run: %v", msg, err)
	}
}

func countCleanupRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM outbox").Scan(&n); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	return n
}

// TestDurableOutboxCleanupPurgesOnlyOldDelivered prova REQ-42.7: três linhas
// — (a) entregue há muito tempo, (b) entregue recentemente (dentro da
// janela), (c) nunca entregue — Cleanup(ctx, retention) some SÓ com (a);
// (b) e (c) sobrevivem, e um 2º Cleanup imediato não apaga nada a mais (nada
// cruzou o cutoff desde então).
func TestDurableOutboxCleanupPurgesOnlyOldDelivered(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "cleanup.db")
	db, err := sqlruntime.Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := sqlruntime.NewEventStore(ctx, db, cleanupTestFactories(), sqlruntime.SQLiteDialect()); err != nil {
		t.Fatalf("NewEventStore: %v", err)
	}

	enqueueOneCleanupEvent(t, db, "agg-old", "old-delivered")
	enqueueOneCleanupEvent(t, db, "agg-recent", "recent-delivered")
	enqueueOneCleanupEvent(t, db, "agg-pending", "never-delivered")

	store := sqlruntime.NewOutboxStore(db, sqlruntime.SQLiteDialect())
	outbox := runtime.NewDurableOutbox(store, cleanupTestRegistry())

	delivered := make(map[string]bool)
	outbox.Subscribe("CleanupTestEvent", func(ctx context.Context, ev runtime.Event) error {
		msg := ev.(*cleanupTestEvent).Msg
		if msg == "never-delivered" {
			return errCleanupPending
		}
		delivered[msg] = true
		return nil
	})

	if _, err := outbox.Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if !delivered["old-delivered"] || !delivered["recent-delivered"] {
		t.Fatalf("delivered = %v, want old-delivered e recent-delivered marcados", delivered)
	}

	// old-delivered foi "entregue" há 10 dias (retroagindo delivered_at
	// diretamente, já que Tick sempre usa time.Now()); recent-delivered
	// entregue agora mesmo (delivered_at já é recente, sem ajuste).
	if _, err := db.ExecContext(ctx, "UPDATE outbox SET delivered_at = ? WHERE event_type = 'CleanupTestEvent' AND payload LIKE '%old-delivered%'", time.Now().Add(-10*24*time.Hour).UTC()); err != nil {
		t.Fatalf("retroagir delivered_at: %v", err)
	}

	if got := countCleanupRows(t, db); got != 3 {
		t.Fatalf("linhas na outbox antes do Cleanup = %d, want 3", got)
	}

	purged, err := outbox.Cleanup(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if purged != 1 {
		t.Fatalf("Cleanup purgou %d linhas, want 1 (só old-delivered)", purged)
	}
	if got := countCleanupRows(t, db); got != 2 {
		t.Fatalf("linhas na outbox depois do Cleanup = %d, want 2 (recent-delivered + never-delivered)", got)
	}

	// 2º Cleanup imediato: nada mais cruzou o cutoff — 0 linhas purgadas.
	purged, err = outbox.Cleanup(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup (2ª chamada): %v", err)
	}
	if purged != 0 {
		t.Fatalf("Cleanup (2ª chamada) purgou %d linhas, want 0", purged)
	}
}

var errCleanupPending = &cleanupPendingError{}

type cleanupPendingError struct{}

func (e *cleanupPendingError) Error() string { return "deliberately undelivered for cleanup test" }
`

// TestSQLOutboxCleanup roda sqlOutboxCleanupTest de verdade sobre um projeto
// Go mínimo (só runtime/sqlruntime vendorados, mesmo material que
// buildSQLRuntimeProjectFiles monta).
func TestSQLOutboxCleanup(t *testing.T) {
	files := buildSQLRuntimeProjectFiles(t)
	files[path.Join("sqlruntime", "outbox_cleanup_test.go")] = []byte(sqlOutboxCleanupTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
