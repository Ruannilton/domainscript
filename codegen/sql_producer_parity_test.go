package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen/gentest"
)

// sql_producer_parity_test.go prova, sobre o pacote "ledger" GERADO + sqlite
// real (offline), o comportamento de runtime do PRODUTOR durável introduzido
// por K3.3 (ISSUE-9/REQ-51.1/51.4, §design correcoes-issues-9-10-11 4.2-P2):
// sqlruntime.NewOutboxUnitOfWork — a forma que emitSingleDatabaseWiring agora
// constrói para o produtor — enfileira no OUTBOX, na MESMA tx do Append, os
// eventos apensados cujo tipo o canal de saída carrega, e NÃO os publica (o
// relay do DurableOutbox publica depois). Substitui a versão K3.2 deste
// arquivo (que provava a publicação pós-commit de NewUnitOfWork(...,
// publisher)) — esse caminho continua existindo para outros usos, mas NÃO é
// mais o do produtor, então provar aqui a nova realidade (enqueue-in-tx +
// filtro REQ-51.4) é o correto. Reusa a fixture Ledger (sql_adapter_test.go) e
// seu UseCase de banco único (PerformDebit → AccountDebited). O teste
// comportamental fim-a-fim de "crash simulado" (relay re-publicando após
// falha) é K3.4, dedicado — aqui só o enqueue-in-tx e o filtro.
const ledgerProducerOutboxBehaviorTest = `package ledger

import (
	"context"
	"database/sql"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type outboxParityCaller struct{ id string }

func (c outboxParityCaller) Authenticated() bool { return true }
func (c outboxParityCaller) ID() string          { return c.id }
func (c outboxParityCaller) HasRole(string) bool { return false }

// mainDbDSNForOutboxTest é substituído por TestLedgerSingleDatabaseProducerOutbox
// antes de escrever o arquivo (strings.Replace sobre este template).
var mainDbDSNForOutboxTest = "__MAIN_DB_DSN_OUTBOX__"

// TestSQLOutboxUnitOfWorkEnqueuesInTxAndFilters prova o P2/REQ-51.1/51.4 de
// K3.3: com o conjunto de event_type do canal, NewOutboxUnitOfWork enfileira o
// evento no outbox ATÔMICO com o Append (linha em outbox E em events, mesma
// tx) e NÃO o publica (delivered_at continua NULL — quem publica é o relay);
// um evento cujo tipo NÃO está no conjunto do canal é apensado ao stream mas
// NUNCA enfileirado no outbox (filtro REQ-51.4).
func TestSQLOutboxUnitOfWorkEnqueuesInTxAndFilters(t *testing.T) {
	db, err := sqlruntime.Open(mainDbDSNForOutboxTest)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	// NewEventStore roda a migração de schema (events + outbox — ensureSchema,
	// eventstore.go.txt); NewOutboxUnitOfWork (banco único, sem EventStore
	// intermediário) não migra sozinho, então este passo é necessário mesmo
	// sem usar o *EventStore devolvido depois.
	if _, err := sqlruntime.NewEventStore(ctx, db, EventRegistry(), sqlruntime.SQLiteDialect()); err != nil {
		t.Fatalf("NewEventStore (migração de schema): %v", err)
	}

	// (1) AccountDebited ESTÁ no conjunto de event_type do canal → enfileirado
	// na MESMA tx do Append, sem publicar.
	uow = sqlruntime.NewOutboxUnitOfWork(db, EventRegistry(), sqlruntime.SQLiteDialect(), map[string]bool{"AccountDebited": true})
	inCtx := runtime.WithCaller(ctx, outboxParityCaller{id: "acc-in"})
	if err := PerformDebit(inCtx, Debit{AccountId: AccountId("acc-in"), Amount: EntryAmount(40)}); err != nil {
		t.Fatalf("PerformDebit (in-set): %v", err)
	}

	var events int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE aggregate_id = ?", "acc-in").Scan(&events); err != nil {
		t.Fatalf("consulta events (acc-in): %v", err)
	}
	if events != 1 {
		t.Fatalf("esperava 1 linha em events para acc-in (Append aconteceu), achei %d", events)
	}

	var outboxCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox WHERE event_type = 'AccountDebited'").Scan(&outboxCount); err != nil {
		t.Fatalf("consulta outbox (após in-set): %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("esperava 1 linha em outbox para AccountDebited (enfileirada na tx), achei %d", outboxCount)
	}
	var deliveredAt sql.NullTime
	if err := db.QueryRowContext(ctx, "SELECT delivered_at FROM outbox WHERE event_type = 'AccountDebited'").Scan(&deliveredAt); err != nil {
		t.Fatalf("consulta delivered_at: %v", err)
	}
	if deliveredAt.Valid {
		t.Fatal("a linha do outbox não deveria estar entregue: a UoW só ENFILEIRA (o relay do DurableOutbox publica depois), nunca publica inline no commit")
	}

	// (2) filtro REQ-51.4: com um conjunto que NÃO contém AccountDebited
	// (JournalPosted, um event type real da fixture que PerformDebit não
	// emite), o evento é apensado ao stream mas NÃO enfileirado no outbox — a
	// contagem do outbox NÃO aumenta.
	uow = sqlruntime.NewOutboxUnitOfWork(db, EventRegistry(), sqlruntime.SQLiteDialect(), map[string]bool{"JournalPosted": true})
	outCtx := runtime.WithCaller(ctx, outboxParityCaller{id: "acc-out"})
	if err := PerformDebit(outCtx, Debit{AccountId: AccountId("acc-out"), Amount: EntryAmount(25)}); err != nil {
		t.Fatalf("PerformDebit (out-of-set): %v", err)
	}

	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE aggregate_id = ?", "acc-out").Scan(&events); err != nil {
		t.Fatalf("consulta events (acc-out): %v", err)
	}
	if events != 1 {
		t.Fatalf("esperava 1 linha em events para acc-out (Append acontece independente do filtro), achei %d", events)
	}

	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM outbox WHERE event_type = 'AccountDebited'").Scan(&outboxCount); err != nil {
		t.Fatalf("consulta outbox (após out-of-set): %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("outbox deveria continuar com 1 linha (AccountDebited não está no conjunto do 2º Run — filtro REQ-51.4), achei %d", outboxCount)
	}
}
`

// TestLedgerSingleDatabaseProducerOutbox gera o projeto Ledger (fixture já
// provada por TestLedgerSingleDatabaseBehavior), substitui o placeholder do
// DSN pelo caminho real do arquivo sqlite e roda o teste comportamental de
// enqueue-in-tx/filtro acima via "go test" de verdade sobre o pacote "ledger"
// gerado.
func TestLedgerSingleDatabaseProducerOutbox(t *testing.T) {
	mainDSN, sideDSN := ledgerSQLitePaths(t)
	files := filesToMap(generateLedgerProject(t, mainDSN, sideDSN))
	testSrc := strings.Replace(ledgerProducerOutboxBehaviorTest, "__MAIN_DB_DSN_OUTBOX__", filepath.ToSlash(mainDSN), 1)
	files[filepath.Join("ledger", "producer_outbox_uow_test.go")] = []byte(testSrc)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
