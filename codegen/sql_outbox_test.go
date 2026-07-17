package codegen_test

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
)

// sql_outbox_test.go prova a DoD de J2.1 (REQ-42.1, R4, §design
// infra-providers 3.2): Tx.EnqueueOutbox (sqlrt/uow.go.txt) grava na MESMA
// *sql.Tx que Tx.Append já usa — um UnitOfWork.Run que faz as duas coisas
// grava as DUAS tabelas quando fn devolve nil, e NENHUMA das duas quando fn
// devolve erro (rollback). Roda de verdade (gentest.WriteFiles/RunTests, o
// mesmo padrão de TestSQLEventStoreDialectPluggability/
// TestPostgresDialectSQLStrings) sobre sqlite real — sem depender de
// Postgres, já que a atomicidade é uma propriedade de *sql.Tx, a mesma para
// os dois dialetos.
const sqlOutboxAtomicityTest = `package sqlruntime_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type outboxTestEvent struct {
	runtime.EventMeta
	Msg string
}

func (e *outboxTestEvent) EventType() string { return "OutboxTestEvent" }

func outboxTestRegistry() map[string]sqlruntime.EventFactory {
	return map[string]sqlruntime.EventFactory{
		"OutboxTestEvent": func() runtime.Event { return &outboxTestEvent{} },
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// TestEnqueueOutboxAtomicWithAppend prova REQ-42.1: sucesso grava as duas
// tabelas ("events" e "outbox"); uma falha de fn desfaz as duas (nenhuma
// linha nova em nenhuma), nunca uma sem a outra.
func TestEnqueueOutboxAtomicWithAppend(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "outbox.db")
	db, err := sqlruntime.Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := sqlruntime.NewEventStore(ctx, db, outboxTestRegistry(), sqlruntime.SQLiteDialect()); err != nil {
		t.Fatalf("NewEventStore: %v", err)
	}
	uow := sqlruntime.NewUnitOfWork(db, outboxTestRegistry(), sqlruntime.SQLiteDialect())

	// Fase 1: sucesso — as duas tabelas ganham uma linha.
	err = uow.Run(ctx, func(tx runtime.Tx) error {
		if err := tx.Append("agg-1", []runtime.Event{&outboxTestEvent{Msg: "hello"}}); err != nil {
			return err
		}
		return tx.EnqueueOutbox([]runtime.Event{&outboxTestEvent{Msg: "hello"}})
	})
	if err != nil {
		t.Fatalf("Run (sucesso): %v", err)
	}
	if got, want := countRows(t, db, "events"), 1; got != want {
		t.Fatalf("events após sucesso = %d linhas, want %d", got, want)
	}
	if got, want := countRows(t, db, "outbox"), 1; got != want {
		t.Fatalf("outbox após sucesso = %d linhas, want %d", got, want)
	}

	// Fase 2: fn devolve erro — rollback desfaz as DUAS tabelas (nenhuma
	// linha nova em nenhuma), nunca uma delas isolada.
	boom := errors.New("boom")
	err = uow.Run(ctx, func(tx runtime.Tx) error {
		if err := tx.Append("agg-2", []runtime.Event{&outboxTestEvent{Msg: "world"}}); err != nil {
			return err
		}
		if err := tx.EnqueueOutbox([]runtime.Event{&outboxTestEvent{Msg: "world"}}); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Run (falha) = %v, want errors.Is(_, boom)", err)
	}
	if got, want := countRows(t, db, "events"), 1; got != want {
		t.Fatalf("events após rollback = %d linhas, want %d (sem mudança)", got, want)
	}
	if got, want := countRows(t, db, "outbox"), 1; got != want {
		t.Fatalf("outbox após rollback = %d linhas, want %d (sem mudança)", got, want)
	}
}
`

// TestSQLOutboxAtomicity roda sqlOutboxAtomicityTest de verdade sobre um
// projeto Go mínimo (só runtime/sqlruntime vendorados, mesmo material que
// buildSQLRuntimeProjectFiles monta).
func TestSQLOutboxAtomicity(t *testing.T) {
	files := buildSQLRuntimeProjectFiles(t)
	files[path.Join("sqlruntime", "outbox_atomicity_test.go")] = []byte(sqlOutboxAtomicityTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
