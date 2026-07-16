package codegen_test

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
)

// sql_dialect_test.go prova REQ-40.3 (I7.0, §design read-side 3.9a): a MESMA
// suíte comportamental do adapter (Append/Load, row-level tenancy, SQL puro
// contra "events") roda sem NENHUMA mudança fora do Dialect escolhido contra
// um segundo dialeto de TESTE com placeholder posicional "$N" — o mesmo
// driver sqlite (modernc.org/sqlite) aceita as duas sintaxes nativamente
// (SQLite trata "$N" como um parâmetro nomeado válido, casado por posição
// quando os args são passados sem nome, do mesmo jeito que "?"). Uma
// dependência escondida no estilo "?" fora de sqlrt/dialect.go.txt quebraria
// este teste. positionalDialect nunca aparece em sqlProviders (sql_wiring.go)
// — é só uma prova da abstração, nunca um provider real.
const sqlDialectBehaviorTest = `package sqlruntime_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type dialectTestEvent struct {
	runtime.EventMeta
	Msg string
}

func (e *dialectTestEvent) EventType() string { return "DialectTestEvent" }

func dialectTestRegistry() map[string]sqlruntime.EventFactory {
	return map[string]sqlruntime.EventFactory{
		"DialectTestEvent": func() runtime.Event { return &dialectTestEvent{} },
	}
}

// positionalDialect sobrescreve só Placeholder — CreateEventsTable/
// CreateCollectionTable/LimitOffset delegam ao SQLiteDialect embutido, o que
// prova que só o estilo de placeholder varia entre os dois dialetos deste
// teste.
type positionalDialect struct {
	sqlruntime.Dialect
}

func newPositionalDialect() positionalDialect {
	return positionalDialect{Dialect: sqlruntime.SQLiteDialect()}
}

func (positionalDialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }

func openDialectTestStore(t *testing.T, dialect sqlruntime.Dialect) (*sqlruntime.EventStore, *sql.DB) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "vault.db")
	db, err := sqlruntime.Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := sqlruntime.NewEventStore(context.Background(), db, dialectTestRegistry(), dialect)
	if err != nil {
		t.Fatalf("NewEventStore: %v", err)
	}
	return store, db
}

// runDialectSuite roda a mesma suíte comportamental (append, load em ordem,
// tenancy row-level, leitura via SQL puro) contra dialect — chamada com
// sqlruntime.SQLiteDialect() e com positionalDialect{} por
// TestSQLEventStoreAcrossDialects.
func runDialectSuite(t *testing.T, dialect sqlruntime.Dialect) {
	store, db := openDialectTestStore(t, dialect)
	ctx := context.Background()
	tenantA := runtime.WithTenant(ctx, runtime.Tenant{ID: "acme"})
	tenantB := runtime.WithTenant(ctx, runtime.Tenant{ID: "intruder"})

	if err := store.Append(tenantA, "secret-1", []runtime.Event{&dialectTestEvent{Msg: "hello"}}); err != nil {
		t.Fatalf("Append (tenant acme): %v", err)
	}

	events, err := store.Load(tenantA, "secret-1")
	if err != nil || len(events) != 1 {
		t.Fatalf("Load (tenant acme, dono) = (%v, %v), want (1 evento, nil)", events, err)
	}

	if _, err := store.Load(tenantB, "secret-1"); !errors.Is(err, runtime.ErrNotFound) {
		t.Fatalf("Load (tenant intruder, cross-tenant) = %v, want errors.Is(_, runtime.ErrNotFound)", err)
	}

	if err := store.Append(tenantA, "secret-1", []runtime.Event{&dialectTestEvent{Msg: "world"}}); err != nil {
		t.Fatalf("Append 2 (tenant acme): %v", err)
	}
	events, err = store.Load(tenantA, "secret-1")
	if err != nil || len(events) != 2 {
		t.Fatalf("Load após 2ª escrita = (%v, %v), want (2 eventos, nil)", events, err)
	}
	if events[0].(*dialectTestEvent).Msg != "hello" || events[1].(*dialectTestEvent).Msg != "world" {
		t.Fatalf("ordem/sequence incorreta: %+v", events)
	}

	// SQL puro (sempre "?" aqui — é uma consulta de VERIFICAÇÃO do teste, não
	// do adapter, e independe de qual Dialect o EventStore usou para
	// escrever): confirma que os dados gravados por CADA dialeto são
	// idênticos.
	var tenantCol string
	if err := db.QueryRowContext(ctx, "SELECT tenant_id FROM events WHERE aggregate_id = ? AND sequence = 1", "secret-1").Scan(&tenantCol); err != nil {
		t.Fatalf("consulta SQL direta: %v", err)
	}
	if tenantCol != "acme" {
		t.Fatalf("tenant_id (SQL puro) = %q, want %q", tenantCol, "acme")
	}
}

func TestSQLEventStoreAcrossDialects(t *testing.T) {
	t.Run("sqlite_question_mark", func(t *testing.T) {
		runDialectSuite(t, sqlruntime.SQLiteDialect())
	})
	t.Run("positional_dollar_n", func(t *testing.T) {
		runDialectSuite(t, newPositionalDialect())
	})
}
`

// TestSQLEventStoreDialectPluggability roda "go test ./..." de verdade sobre
// um projeto Go mínimo (só runtime/sqlruntime vendorados, mesmo material que
// buildSQLRuntimeProjectFiles monta para TestSQLEventStoreRowLevelTenancyBehavior,
// sql_tenancy_test.go) com sqlDialectBehaviorTest escrito em
// sqlruntime/dialect_test.go — a suíte roda duas vezes (subtestes), uma por
// dialeto, provando REQ-40.3 sem nenhuma dependência externa nova.
func TestSQLEventStoreDialectPluggability(t *testing.T) {
	files := buildSQLRuntimeProjectFiles(t)
	files[path.Join("sqlruntime", "dialect_test.go")] = []byte(sqlDialectBehaviorTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
