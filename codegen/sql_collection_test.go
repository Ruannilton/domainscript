package codegen_test

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
)

// sql_collection_test.go prova o critério de conclusão de I7.1 (REQ-38,
// §design read-side 3.9): testes PAREADOS (NFR-18) — a MESMA Query[T], o
// MESMO seed de dados, rodada contra os dois backends de
// runtime.Collection[T] (runtime.NewMemoryCollection e
// sqlruntime.NewCollection), resultados IDÊNTICOS — incluindo um caso que
// força a degradação (uma closure Where não redutível a WhereEq, REQ-38.2)
// e um caso onde WhereEq desce de verdade (REQ-38.1). O tipo de item
// (ticketRow) é um struct Go comum com tags `json:"..."` — não precisa vir
// de uma fixture DomainScript de verdade: Collection[T] é agnóstico ao
// schema de T por construção (json.Marshal/Unmarshal), e o que este teste
// prova é a EQUIVALÊNCIA de comportamento entre os dois backends, não a
// lowering em si (essa é responsabilidade de codegen/lower/whereeq_test.go).
const sqlCollectionBehaviorTest = `package sqlruntime_test

import (
	"context"
	"reflect"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type ticketRow struct {
	EventID string ` + "`json:\"eventId\"`" + `
	Status  string ` + "`json:\"status\"`" + `
	OrderID string ` + "`json:\"orderId\"`" + `
}

var ticketSeed = []ticketRow{
	{EventID: "E1", Status: "SOLD", OrderID: "O1"},
	{EventID: "E1", Status: "SOLD", OrderID: "O2"},
	{EventID: "E1", Status: "AVAILABLE", OrderID: "O3"},
	{EventID: "E2", Status: "SOLD", OrderID: "O4"},
}

func seedTicketCollections(t *testing.T) (mem runtime.Collection[ticketRow], sq runtime.Collection[ticketRow]) {
	t.Helper()
	ctx := context.Background()

	mem = runtime.NewMemoryCollection[ticketRow]()

	db, err := sqlruntime.Open(t.TempDir() + "/tickets.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	sqlColl, err := sqlruntime.NewCollection[ticketRow](ctx, db, sqlruntime.SQLiteDialect(), "tickets")
	if err != nil {
		t.Fatalf("NewCollection: %v", err)
	}
	sq = sqlColl

	for _, row := range ticketSeed {
		if err := mem.Add(ctx, row); err != nil {
			t.Fatalf("mem.Add: %v", err)
		}
		if err := sq.Add(ctx, row); err != nil {
			t.Fatalf("sql.Add: %v", err)
		}
	}
	return mem, sq
}

// TestCollectionWhereEqDescendsIdentically prova REQ-38.1: um "where"
// redutível a WhereEq (EventID == "E1" AND Status == "SOLD") desce para SQL
// no backend real (WHERE json_extract(...) = ? AND ...) e produz o MESMO
// resultado que o backend in-memory (que ignora WhereEq e só roda a
// closure) — 2 tickets batem (O1, O2), na ordem de inserção.
func TestCollectionWhereEqDescendsIdentically(t *testing.T) {
	mem, sq := seedTicketCollections(t)
	ctx := context.Background()

	q := runtime.Query[ticketRow]{
		Where: func(r ticketRow) (bool, error) { return r.EventID == "E1" && r.Status == "SOLD", nil },
		WhereEq: []runtime.FieldEq{
			{Field: "eventId", Value: "E1"},
			{Field: "status", Value: "SOLD"},
		},
	}

	memGot, err := mem.Select(ctx, q)
	if err != nil {
		t.Fatalf("mem.Select: %v", err)
	}
	sqlGot, err := sq.Select(ctx, q)
	if err != nil {
		t.Fatalf("sql.Select: %v", err)
	}
	if !reflect.DeepEqual(memGot, sqlGot) {
		t.Fatalf("resultados divergem entre backends:\nmem: %+v\nsql: %+v", memGot, sqlGot)
	}
	want := []ticketRow{
		{EventID: "E1", Status: "SOLD", OrderID: "O1"},
		{EventID: "E1", Status: "SOLD", OrderID: "O2"},
	}
	if !reflect.DeepEqual(sqlGot, want) {
		t.Fatalf("sql.Select = %+v, want %+v", sqlGot, want)
	}

	memCount, err := mem.Count(ctx, q)
	if err != nil {
		t.Fatalf("mem.Count: %v", err)
	}
	sqlCount, err := sq.Count(ctx, q)
	if err != nil {
		t.Fatalf("sql.Count: %v", err)
	}
	if memCount != sqlCount || sqlCount != 2 {
		t.Fatalf("Count diverge: mem=%d sql=%d, want 2 e 2 iguais", memCount, sqlCount)
	}
}

// TestCollectionDegradesWithoutWhereEq prova REQ-38.2: uma closure Where que
// NÃO reduz a WhereEq (aqui, uma desigualdade — "EventID != \"E2\"", nunca
// convertida para uma coluna igual a um valor) SEM WhereEq preenchido ainda
// produz o MESMO resultado nos dois backends — o adapter SQL busca TODAS as
// linhas (nenhum WHERE) e aplica a closure in-memory, exatamente como o
// backend puramente in-memory.
func TestCollectionDegradesWithoutWhereEq(t *testing.T) {
	mem, sq := seedTicketCollections(t)
	ctx := context.Background()

	q := runtime.Query[ticketRow]{
		Where: func(r ticketRow) (bool, error) { return r.EventID != "E2", nil },
	}

	memGot, err := mem.Select(ctx, q)
	if err != nil {
		t.Fatalf("mem.Select: %v", err)
	}
	sqlGot, err := sq.Select(ctx, q)
	if err != nil {
		t.Fatalf("sql.Select: %v", err)
	}
	if !reflect.DeepEqual(memGot, sqlGot) {
		t.Fatalf("resultados divergem entre backends:\nmem: %+v\nsql: %+v", memGot, sqlGot)
	}
	if len(sqlGot) != 3 {
		t.Fatalf("esperava 3 tickets (todos menos o de E2), got %d: %+v", len(sqlGot), sqlGot)
	}

	memCount, err := mem.Count(ctx, q)
	if err != nil {
		t.Fatalf("mem.Count: %v", err)
	}
	sqlCount, err := sq.Count(ctx, q)
	if err != nil {
		t.Fatalf("sql.Count: %v", err)
	}
	if memCount != sqlCount || sqlCount != 3 {
		t.Fatalf("Count diverge: mem=%d sql=%d, want 3 e 3 iguais", memCount, sqlCount)
	}
}

// TestCollectionOrderBySkipTakeAlwaysInMemoryButIdentical prova que
// orderBy/skip/take (nunca descidos por este adapter, ver a doc do tipo em
// collection.go.txt) produzem o MESMO resultado nos dois backends: Less
// ordena por OrderID descendente (O4, O3, O2, O1), Skip 1 Take 1 pagina por
// cima — o 2º da ordem descendente, O3.
func TestCollectionOrderBySkipTakeAlwaysInMemoryButIdentical(t *testing.T) {
	mem, sq := seedTicketCollections(t)
	ctx := context.Background()

	q := runtime.Query[ticketRow]{
		Less: func(a, b ticketRow) (bool, error) { return a.OrderID > b.OrderID, nil },
		Skip: 1,
		Take: 1,
	}

	memGot, err := mem.Select(ctx, q)
	if err != nil {
		t.Fatalf("mem.Select: %v", err)
	}
	sqlGot, err := sq.Select(ctx, q)
	if err != nil {
		t.Fatalf("sql.Select: %v", err)
	}
	if !reflect.DeepEqual(memGot, sqlGot) {
		t.Fatalf("resultados divergem entre backends:\nmem: %+v\nsql: %+v", memGot, sqlGot)
	}
	want := []ticketRow{{EventID: "E1", Status: "AVAILABLE", OrderID: "O3"}}
	if !reflect.DeepEqual(sqlGot, want) {
		t.Fatalf("sql.Select = %+v, want %+v", sqlGot, want)
	}
}
`

// TestSQLCollectionParityWithMemory roda "go test ./..." de verdade sobre um
// projeto Go mínimo (mesmo material de buildSQLRuntimeProjectFiles,
// sql_tenancy_test.go: runtime/sqlruntime vendorados) com
// sqlCollectionBehaviorTest escrito em sqlruntime/collection_test.go — os 3
// testes pareados acima SÃO os testes que rodam aqui, sobre SQLite real
// (NFR-15: não confiar só no que compila).
func TestSQLCollectionParityWithMemory(t *testing.T) {
	files := buildSQLRuntimeProjectFiles(t)
	files[path.Join("sqlruntime", "collection_test.go")] = []byte(sqlCollectionBehaviorTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
