//go:build integration

package codegen_test

import (
	"os"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// sql_postgres_integration_test.go prova J1.4.b (REQ-41, REQ-48.3, NFR-22/
// 24, §design infra-providers 3.1): um teste comportamental DE VERDADE
// contra um Postgres vivo, atrás da build tag "integration" — NUNCA entra
// no caminho de "go test ./..." default (nem sequer compila sem
// -tags=integration) — e, além disso, guardado por env: sem PG_URL
// definida, pula (t.Skip), nunca falha (REQ-48.3/NFR-24). Rodar de
// propósito: "PG_URL=postgres://user:pass@host:5432/db?sslmode=disable go
// test -tags=integration ./codegen/ -run TestPostgresIntegration".
//
// A prova de paridade (NFR-22) reusa a MESMA fixture Ledger (domínio/
// aplicação/read de sql_adapter_test.go) com um mod.ds de UM Database só
// ("MainDb", provider "postgres", "connection: env(\"PG_URL\")", manages
// os dois Aggregates — sem supportsXA/2PC, já coberto por
// TestLedgerTwoPCBehavior sobre sqlite) e roda o MESMO par UseCase/Query
// (PerformDebit/GetAccount) duas vezes: uma vez wireada a
// runtime.NewMemoryEventStore(), outra a sqlruntime.NewEventStore(...,
// PostgresDialect()) — o resultado observável (o balance lido de volta por
// GetAccount) tem que ser o mesmo (REQ-21.5 + NFR-22).

const ledgerPostgresIntegrationModDs = `Module Ledger {
    Database MainDb {
        provider: "postgres"
        connection: env("PG_URL")
        manages: [Account, Journal]
    }
}
`

func generateLedgerPostgresIntegrationProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         ledgerPostgresIntegrationModDs,
		"domain.ds":      ledgerDomainDs,
		"application.ds": ledgerApplicationDs,
		"read.ds":        ledgerReadDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture Ledger (postgres integração) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, ledgerOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture Ledger (postgres integração): %v", err)
	}
	return files
}

// ledgerPostgresParityBehaviorTest roda PerformDebit+GetAccount UMA vez
// contra runtime.NewMemoryEventStore() e uma segunda vez contra um Postgres
// real (PG_URL), e compara o balance lido de volta pelos dois caminhos — a
// prova de NFR-22. Skip (não Fail) quando PG_URL não está no ambiente:
// REQ-48.3/NFR-24, o dobro de guarda além da build tag "integration" do
// arquivo que escreve isto (só entram testes que EXIGEM infra viva, um
// PG_URL ausente aqui teria que ser um erro de configuração do runner de
// integração, não do compilador — ainda assim, skip é o comportamento
// seguro).
const ledgerPostgresParityBehaviorTest = `package ledger

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type parityCaller struct{ id string }

func (c parityCaller) Authenticated() bool      { return true }
func (c parityCaller) ID() string                { return c.id }
func (c parityCaller) HasRole(role string) bool { return false }

func TestPostgresIntegrationPersistenceParityWithMemory(t *testing.T) {
	pgURL := os.Getenv("PG_URL")
	if pgURL == "" {
		t.Skip("PG_URL não definida — pulando teste de integração Postgres (REQ-48.3/NFR-24)")
	}

	ctx := context.Background()

	// Fase 1: caminho in-memory — o baseline de comportamento.
	memStore := runtime.NewMemoryEventStore()
	uow = runtime.NewUnitOfWork(memStore)
	memCtx := runtime.WithCaller(ctx, parityCaller{id: "acc-mem"})
	memCmd := Debit{AccountId: AccountId("acc-mem"), Amount: EntryAmount(150)}
	if err := PerformDebit(memCtx, memCmd); err != nil {
		t.Fatalf("PerformDebit (memory): %v", err)
	}
	memView, err := GetAccount(ctx, memStore, AccountId("acc-mem"))
	if err != nil {
		t.Fatalf("GetAccount (memory): %v", err)
	}

	// Fase 2: MESMA operação contra Postgres real. ID único por execução
	// (não ":memory:", uma tabela "events" persistente entre corridas) para
	// nunca colidir com uma linha de uma execução anterior.
	pgID := fmt.Sprintf("acc-pg-%d", time.Now().UnixNano())

	db, err := sqlruntime.OpenPostgres(pgURL)
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	defer db.Close()

	pgStore, err := sqlruntime.NewEventStore(ctx, db, EventRegistry(), sqlruntime.PostgresDialect())
	if err != nil {
		t.Fatalf("NewEventStore (postgres): %v", err)
	}
	uow = sqlruntime.NewUnitOfWork(db, EventRegistry(), sqlruntime.PostgresDialect())

	pgCtx := runtime.WithCaller(ctx, parityCaller{id: pgID})
	pgCmd := Debit{AccountId: AccountId(pgID), Amount: EntryAmount(150)}
	if err := PerformDebit(pgCtx, pgCmd); err != nil {
		t.Fatalf("PerformDebit (postgres): %v", err)
	}
	pgView, err := GetAccount(ctx, pgStore, AccountId(pgID))
	if err != nil {
		t.Fatalf("GetAccount (postgres): %v", err)
	}

	if pgView.Balance != memView.Balance {
		t.Fatalf("paridade quebrada (NFR-22): memory balance=%v, postgres balance=%v", memView.Balance, pgView.Balance)
	}
	if pgView.Balance != EntryAmount(150) {
		t.Fatalf("esperava balance=150 no caminho postgres, veio %v", pgView.Balance)
	}
}
`

// TestPostgresIntegrationParity gera o projeto Ledger (postgres, connection
// via env("PG_URL")), acrescenta ledgerPostgresParityBehaviorTest ao pacote
// "ledger" gerado, e roda "go test" DE VERDADE sobre ele (gentest.RunTests)
// — o subprocesso herda o ambiente do processo pai, então PG_URL (checada
// aqui e de novo dentro do teste gerado) chega ao teste comportamental sem
// nenhuma passagem explícita.
func TestPostgresIntegrationParity(t *testing.T) {
	if os.Getenv("PG_URL") == "" {
		t.Skip("PG_URL não definida — pulando teste de integração Postgres (REQ-48.3/NFR-24)")
	}

	files := filesToMap(generateLedgerPostgresIntegrationProject(t))
	files["ledger/behavior_test.go"] = []byte(ledgerPostgresParityBehaviorTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
