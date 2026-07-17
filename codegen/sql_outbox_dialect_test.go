package codegen_test

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
)

// sql_outbox_dialect_test.go prova a DoD de J2.2 (REQ-42.4, §design
// infra-providers 3.2): ScanUndelivered/MarkDelivered/PurgeDelivered
// devolvem exatamente as strings SQL esperadas — FIFO ("ORDER BY id")
// sempre, "FOR UPDATE SKIP LOCKED" só no Postgres, sqlite com "LIMIT"
// simples. Como dialect.go.txt/dialect_postgres.go.txt não são compilados
// diretamente por este módulo, a única forma de testar as strings é
// compilar e rodar um pacote sqlruntime real dentro de um projeto Go
// efêmero (mesmo padrão de TestPostgresDialectSQLStrings/
// TestSQLEventStoreDialectPluggability) — nenhuma conexão de banco real é
// aberta ou necessária: são só asserções de string sobre Go que compila.
const sqlOutboxDialectStringsTest = `package sqlruntime_test

import (
	"strings"
	"testing"

	"domainscript/generated/sqlruntime"
)

// TestSQLiteOutboxDialectStrings prova que o sqlite usa LIMIT simples (sem
// SKIP LOCKED — single-writer, a trava de escrita do próprio banco já
// serializa) e sempre ORDER BY id (FIFO).
func TestSQLiteOutboxDialectStrings(t *testing.T) {
	d := sqlruntime.SQLiteDialect()

	scan := d.ScanUndelivered(10)
	for _, want := range []string{
		"SELECT id, event_type, payload, attempts FROM outbox",
		"WHERE delivered_at IS NULL",
		"ORDER BY id",
		"LIMIT 10",
	} {
		if !strings.Contains(scan, want) {
			t.Fatalf("ScanUndelivered(10) = %q, faltando %q", scan, want)
		}
	}
	if strings.Contains(scan, "SKIP LOCKED") {
		t.Fatalf("ScanUndelivered(10) (sqlite) = %q, não deveria conter SKIP LOCKED (isso é postgres)", scan)
	}

	mark := d.MarkDelivered("?", "?")
	if want := "UPDATE outbox SET delivered_at = ? WHERE id = ?"; mark != want {
		t.Fatalf("MarkDelivered(%q, %q) = %q, want %q", "?", "?", mark, want)
	}

	purge := d.PurgeDelivered("?")
	if want := "DELETE FROM outbox WHERE delivered_at IS NOT NULL AND delivered_at < ?"; purge != want {
		t.Fatalf("PurgeDelivered(%q) = %q, want %q", "?", purge, want)
	}
}

// TestPostgresOutboxDialectStrings prova que o Postgres acrescenta FOR
// UPDATE SKIP LOCKED (lote exclusivo entre réplicas do relay) mantendo
// ORDER BY id (FIFO), e que MarkDelivered/PurgeDelivered usam o placeholder
// posicional passado (nunca "?" cru).
func TestPostgresOutboxDialectStrings(t *testing.T) {
	d := sqlruntime.PostgresDialect()

	scan := d.ScanUndelivered(25)
	for _, want := range []string{
		"SELECT id, event_type, payload, attempts FROM outbox",
		"WHERE delivered_at IS NULL",
		"ORDER BY id",
		"FOR UPDATE SKIP LOCKED",
		"LIMIT 25",
	} {
		if !strings.Contains(scan, want) {
			t.Fatalf("ScanUndelivered(25) = %q, faltando %q", scan, want)
		}
	}

	mark := d.MarkDelivered("$1", "$2")
	if want := "UPDATE outbox SET delivered_at = $2 WHERE id = $1"; mark != want {
		t.Fatalf("MarkDelivered(%q, %q) = %q, want %q", "$1", "$2", mark, want)
	}

	purge := d.PurgeDelivered("$1")
	if want := "DELETE FROM outbox WHERE delivered_at IS NOT NULL AND delivered_at < $1"; purge != want {
		t.Fatalf("PurgeDelivered(%q) = %q, want %q", "$1", purge, want)
	}
}
`

// TestSQLOutboxDialectStrings roda sqlOutboxDialectStringsTest de verdade
// sobre um projeto Go mínimo (só runtime/sqlruntime vendorados) — prova
// REQ-42.4 sem NENHUMA conexão de banco real.
func TestSQLOutboxDialectStrings(t *testing.T) {
	files := buildSQLRuntimeProjectFiles(t)
	files[path.Join("sqlruntime", "outbox_dialect_test.go")] = []byte(sqlOutboxDialectStringsTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
