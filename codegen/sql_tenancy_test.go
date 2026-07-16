package codegen_test

import (
	"path"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/rtsrc"
	"domainscript/codegen/sqlrt"
)

// sql_tenancy_test.go prova G5 (REQ-27, spec §13) sobre o adapter database/sql
// de verdade (codegen/sqlrt/eventstore.go.txt, G1): o MESMO esquema de
// tag-on-write/filtro-on-read do runtime.EventStore in-memory
// (codegen/rtsrc/eventstore.go.txt — ver TestRowLevelTenancyIsolatesAndCachesPerTenant/
// TestCrossTenantOptInBypassesFilterAndAudits, tenancy_test.go), agora sobre
// SQLite real: a coluna "tenant_id" (ensureSchema), o carimbo no Append
// (appendWithinTx) e o filtro no Load (loadWithinQuerier).
//
// Diferente da fixture "Notes" (tenancy_test.go, que usa provider "postgres"
// — nunca reconhecido como adapter real, G1 — para exercitar só o caminho
// in-memory), este teste NÃO passa pelo pipeline `codegen.Generate` sobre um
// programa DomainScript: monta um projeto Go mínimo direto de
// rtsrc.Sources()/sqlrt.Sources()/EmitGoMod (o MESMO material que Generate
// escreveria para QUALQUER programa que declare um Database "sqlite") e um
// _test.go escrito à mão no pacote sqlruntime, com um Event de teste mínimo —
// mais direto que construir uma fixture .ds só para chegar em
// sqlruntime.EventStore.Append/Load, e cobre exatamente a superfície que G1
// implementou (Append/Load; StateStored/Repository não tem adapter SQL,
// documentado em program.Database.Tenancy).
const sqlTenancyBehaviorTest = `package sqlruntime_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

// testEvent é o Event mínimo de teste — mesmo padrão de testEvent
// (rtsrc/runtime_test.go.txt).
type testEvent struct {
	runtime.EventMeta
	Msg string
}

func (e *testEvent) EventType() string { return "TestEvent" }

func testRegistry() map[string]sqlruntime.EventFactory {
	return map[string]sqlruntime.EventFactory{
		"TestEvent": func() runtime.Event { return &testEvent{} },
	}
}

func openTestStore(t *testing.T) (*sqlruntime.EventStore, *sql.DB) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "vault.db")
	db, err := sqlruntime.Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := sqlruntime.NewEventStore(context.Background(), db, testRegistry(), sqlruntime.SQLiteDialect())
	if err != nil {
		t.Fatalf("NewEventStore: %v", err)
	}
	return store, db
}

func TestSQLEventStoreRowLevelTenancy(t *testing.T) {
	store, db := openTestStore(t)
	ctx := context.Background()
	tenantA := runtime.WithTenant(ctx, runtime.Tenant{ID: "acme"})
	tenantB := runtime.WithTenant(ctx, runtime.Tenant{ID: "intruder"})

	if err := store.Append(tenantA, "secret-1", []runtime.Event{&testEvent{Msg: "hello"}}); err != nil {
		t.Fatalf("Append (tenant acme): %v", err)
	}

	// Mesmo tenant: enxerga.
	events, err := store.Load(tenantA, "secret-1")
	if err != nil || len(events) != 1 {
		t.Fatalf("Load (tenant acme, dono) = (%v, %v), want (1 evento, nil)", events, err)
	}

	// Tenant DIFERENTE: ErrNotFound, NUNCA vazio-e-ok (§13.2 — deixaria o
	// chamador achar que o id está livre para reuso/recriação).
	if _, err := store.Load(tenantB, "secret-1"); !errors.Is(err, runtime.ErrNotFound) {
		t.Fatalf("Load (tenant intruder, cross-tenant) = %v, want errors.Is(_, runtime.ErrNotFound)", err)
	}

	// SEM tenant nenhum no ctx: também ErrNotFound (fail-closed — nunca
	// enxerga dado marcado de um tenant só porque o chamador não apresentou
	// nenhum).
	if _, err := store.Load(ctx, "secret-1"); !errors.Is(err, runtime.ErrNotFound) {
		t.Fatalf("Load (sem tenant no ctx) = %v, want errors.Is(_, runtime.ErrNotFound)", err)
	}

	// Bypass (tenancy: cross_tenant, §13.3): enxerga apesar do mismatch.
	events, err = store.Load(runtime.WithCrossTenantBypass(tenantB), "secret-1")
	if err != nil || len(events) != 1 {
		t.Fatalf("Load (bypass) = (%v, %v), want (1 evento, nil)", events, err)
	}

	// Dado NUNCA tenant-marcado (criado fora de qualquer contexto de
	// tenant) continua visível para todo mundo — o service sem tenancy
	// nenhuma (wallet/shop) nunca escreve com um Tenant em ctx, então nunca
	// aciona o filtro; aqui provamos o mesmo dentro de um service QUE tem
	// tenancy para outros dados.
	if err := store.Append(ctx, "public-1", []runtime.Event{&testEvent{Msg: "open"}}); err != nil {
		t.Fatalf("Append (sem tenant): %v", err)
	}
	if _, err := store.Load(tenantA, "public-1"); err != nil {
		t.Fatalf("Load (tenant acme) de dado não-marcado: %v", err)
	}
	if _, err := store.Load(tenantB, "public-1"); err != nil {
		t.Fatalf("Load (tenant intruder) de dado não-marcado: %v", err)
	}

	// Confirma via SQL PURO (não só via EventStore.Load) que a coluna
	// tenant_id existe e foi carimbada de verdade — mesma técnica de
	// TestPerformDebitPersistsToRealSQLiteAndQueryReadsItBack
	// (sql_adapter_test.go, G1).
	var tenantCol string
	if err := db.QueryRowContext(ctx, "SELECT tenant_id FROM events WHERE aggregate_id = ?", "secret-1").Scan(&tenantCol); err != nil {
		t.Fatalf("consulta SQL direta: %v", err)
	}
	if tenantCol != "acme" {
		t.Fatalf("tenant_id (SQL puro) = %q, want %q", tenantCol, "acme")
	}
	var publicTenantCol string
	if err := db.QueryRowContext(ctx, "SELECT tenant_id FROM events WHERE aggregate_id = ?", "public-1").Scan(&publicTenantCol); err != nil {
		t.Fatalf("consulta SQL direta: %v", err)
	}
	if publicTenantCol != "" {
		t.Fatalf("tenant_id (SQL puro, dado não-marcado) = %q, want \"\"", publicTenantCol)
	}
}
`

// buildSQLRuntimeProjectFiles monta o material MÍNIMO que codegen.Generate
// escreveria para QUALQUER programa com um Database "sqlite" (go.mod +
// runtime/*.go + sqlruntime/*.go, G1) — sem passar por driver.CheckProject/
// codegen.Generate sobre nenhum programa .ds (não há nenhum construto de
// domínio envolvido no que este teste prova: só o adapter, ver a doc do
// arquivo).
func buildSQLRuntimeProjectFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	files["go.mod"] = codegen.EmitGoMod(codegen.Options{ModulePath: "domainscript/generated"}, "", []string{"sqlite"}, false, false)

	rtSrcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources: %v", err)
	}
	for name, content := range rtSrcs {
		files[path.Join("runtime", name)] = content
	}

	sqlSrcs, err := sqlrt.Sources()
	if err != nil {
		t.Fatalf("sqlrt.Sources: %v", err)
	}
	for name, content := range sqlSrcs {
		files[path.Join("sqlruntime", name)] = content
	}
	return files
}

// TestSQLEventStoreRowLevelTenancyBehavior roda `go test ./...` de verdade
// sobre um projeto Go mínimo (só runtime/sqlruntime vendorados, G1/G5) com
// sqlTenancyBehaviorTest escrito em sqlruntime/tenancy_test.go — prova o
// filtro row_level sobre SQLite real, a mesma disciplina de todo teste
// comportamental deste pacote (NFR-15): não confiar só no que compila.
func TestSQLEventStoreRowLevelTenancyBehavior(t *testing.T) {
	files := buildSQLRuntimeProjectFiles(t)
	files[path.Join("sqlruntime", "tenancy_test.go")] = []byte(sqlTenancyBehaviorTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
