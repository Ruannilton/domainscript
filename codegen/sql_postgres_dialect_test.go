package codegen_test

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
)

// sql_postgres_dialect_test.go prova J1.1 (REQ-41.1/41.4, §design
// infra-providers 3.1): PostgresDialect devolve exatamente as strings SQL
// esperadas — placeholder posicional "$N", DDL com tipos Postgres
// (BIGINT/JSONB/TIMESTAMPTZ) e JSONFieldEq via o operador "->>" no lugar
// do json_extract(...) do sqlite. Como dialect.go.txt/dialect_postgres.go.txt
// não são compilados diretamente por este módulo (são ".go.txt" vendorados
// pelo gerador, sqlrt.Sources()), a única forma de testar as strings que
// PostgresDialect() produz é compilar e rodar um pacote sqlruntime real
// dentro de um projeto Go efêmero (mesmo padrão de
// TestSQLEventStoreDialectPluggability em sql_dialect_test.go) — nenhuma
// conexão Postgres de verdade é aberta ou necessária: são só asserções de
// string sobre Go que compila.
const sqlPostgresDialectStringsTest = `package sqlruntime_test

import (
	"strings"
	"testing"

	"domainscript/generated/sqlruntime"
)

// TestPostgresDialectStrings prova REQ-41.1/41.4: cada método de
// PostgresDialect() devolve a forma Postgres esperada, nunca a forma
// sqlite (nenhum "?"/json_extract vazando para o dialeto postgres).
func TestPostgresDialectStrings(t *testing.T) {
	d := sqlruntime.PostgresDialect()

	if got, want := d.Placeholder(1), "$1"; got != want {
		t.Fatalf("Placeholder(1) = %q, want %q", got, want)
	}
	if got, want := d.Placeholder(2), "$2"; got != want {
		t.Fatalf("Placeholder(2) = %q, want %q", got, want)
	}

	ddl := d.CreateEventsTable()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS events",
		"aggregate_id TEXT",
		"sequence     BIGINT",
		"event_type   TEXT",
		"payload      JSONB",
		"recorded_at  TIMESTAMPTZ",
		"tenant_id    TEXT NOT NULL DEFAULT ''",
		"PRIMARY KEY (aggregate_id, sequence)",
	} {
		if !strings.Contains(ddl, want) {
			t.Fatalf("CreateEventsTable() = %q, faltando %q", ddl, want)
		}
	}
	if strings.Contains(ddl, "INTEGER") {
		t.Fatalf("CreateEventsTable() = %q, não deveria conter o tipo sqlite INTEGER para sequence", ddl)
	}

	coll := d.CreateCollectionTable("tickets")
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS tickets",
		"rowid   BIGSERIAL",
		"id      TEXT",
		"payload JSONB",
		"PRIMARY KEY (id)",
	} {
		if !strings.Contains(coll, want) {
			t.Fatalf("CreateCollectionTable(%q) = %q, faltando %q", "tickets", coll, want)
		}
	}

	if got, want := d.LimitOffset(10, 20), "LIMIT 10 OFFSET 20"; got != want {
		t.Fatalf("LimitOffset(10, 20) = %q, want %q", got, want)
	}

	if got, want := d.JSONFieldEq("status", "$3"), "payload->>'status' = $3"; got != want {
		t.Fatalf("JSONFieldEq(%q, %q) = %q, want %q", "status", "$3", got, want)
	}
	if strings.Contains(d.JSONFieldEq("status", "$3"), "json_extract") {
		t.Fatalf("JSONFieldEq não deveria usar json_extract (isso é sqlite)")
	}
}

// TestSQLiteDialectJSONFieldEqUnchanged prova que o refactor de
// collection.go.txt (whereEqClause agora chama c.dialect.JSONFieldEq em vez
// de um literal hardcoded) preserva EXATAMENTE a string que sqlite emitia
// antes desta task — comportamento sqlite inalterado (J1.1, critério
// "behavior-preserving").
func TestSQLiteDialectJSONFieldEqUnchanged(t *testing.T) {
	d := sqlruntime.SQLiteDialect()
	got := d.JSONFieldEq("status", "?")
	want := "json_extract(payload,'$.status') = ?"
	if got != want {
		t.Fatalf("SQLiteDialect().JSONFieldEq(%q, %q) = %q, want %q", "status", "?", got, want)
	}
}
`

// TestPostgresDialectSQLStrings roda "go test ./..." de verdade sobre um
// projeto Go mínimo (só runtime/sqlruntime vendorados, mesmo material que
// buildSQLRuntimeProjectFiles monta) com sqlPostgresDialectStringsTest
// escrito em sqlruntime/postgres_dialect_test.go — prova REQ-41.1/41.4/R7
// sem NENHUMA conexão Postgres real (J1.1: só o dialeto, driver real e
// registro de provider ficam para J1.2).
func TestPostgresDialectSQLStrings(t *testing.T) {
	files := buildSQLRuntimeProjectFiles(t)
	files[path.Join("sqlruntime", "postgres_dialect_test.go")] = []byte(sqlPostgresDialectStringsTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
