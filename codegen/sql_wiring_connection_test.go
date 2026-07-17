package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/driver"
	"domainscript/types"
)

// sql_wiring_connection_test.go prova a DoD de J1.3 (REQ-41.3, R1, §design
// infra-providers 3.1): o wiring 2PC (emitXADatabaseWiring, sql_wiring.go)
// lowerização "connection: env(\"X\")" para "os.Getenv(\"X\")" — NUNCA
// strconv.Quote(db.DSN), que fica "" para essa forma (program.Database.DSN
// só resolve o literal estático "dsn:"). Reusa a fixture Ledger
// (sql_adapter_test.go: domínio/aplicação/read compartilhados, 2 Database
// com supportsXA — o único caminho hoje que abre uma conexão real, ver a
// doc de emitXADatabaseWiring) trocando só o mod.ds: provider "postgres" +
// "connection: env(...)" em vez de "provider: sqlite" + "dsn: <caminho>".

// ledgerModDsWithPostgresConnectionEnv monta mod.ds com os dois Database
// "postgres" (reconhecido como adapter real desde J1.2), cada um com
// "connection: env(\"...\")" — a forma canônica do spec §12 — em vez do
// "dsn:" literal que ledgerModDs usa para sqlite.
const ledgerModDsWithPostgresConnectionEnv = `Module Ledger {
    Database MainDb {
        provider: "postgres"
        connection: env("LEDGER_MAIN_PG_URL")
        supportsXA: true
        manages: [Account]
    }
    Database SideDb {
        provider: "postgres"
        connection: env("LEDGER_SIDE_PG_URL")
        supportsXA: true
        manages: [Journal]
    }
}
`

func generateLedgerProjectWithPostgresConnectionEnv(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         ledgerModDsWithPostgresConnectionEnv,
		"domain.ds":      ledgerDomainDs,
		"application.ds": ledgerApplicationDs,
		"read.ds":        ledgerReadDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture Ledger (postgres + connection: env(...)) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, ledgerOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture Ledger (postgres): %v", err)
	}
	return files
}

// TestLedgerMainWiresConnectionFromEnv prova J1.3.b: com "connection:
// env(\"X\")", o main.go gerado chama os.Getenv("X") — nunca uma string
// vazia (o que strconv.Quote(db.DSN) emitiria, já que DSN só é populado a
// partir do literal "dsn:").
func TestLedgerMainWiresConnectionFromEnv(t *testing.T) {
	files := generateLedgerProjectWithPostgresConnectionEnv(t)

	var main []byte
	for _, f := range files {
		if strings.HasPrefix(f.Path, "cmd/") && strings.HasSuffix(f.Path, "main.go") {
			main = f.Content
		}
	}
	if main == nil {
		t.Fatal("esperava um cmd/<service>/main.go entre os arquivos gerados")
	}
	got := string(main)

	for _, want := range []string{
		`sqlruntime.OpenPostgres(os.Getenv("LEDGER_MAIN_PG_URL"))`,
		`sqlruntime.OpenPostgres(os.Getenv("LEDGER_SIDE_PG_URL"))`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q em cmd/.../main.go, não achei:\n%s", want, got)
		}
	}
	if strings.Contains(got, `OpenPostgres("")`) {
		t.Errorf(`main.go chamou OpenPostgres("") (string vazia) em vez de os.Getenv(...) — connection: env(...) não foi lowerizado:\n%s`, got)
	}
}
