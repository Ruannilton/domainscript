package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen/gentest"
)

// sql_producer_parity_test.go prova a paridade comportamental (NFR-22)
// exigida por K3.2 (ISSUE-9/REQ-51.5, §design correcoes-issues-9-10-11
// 4.2-P1): sqlruntime.NewUnitOfWork(db, registry, dialect, publisher) — a
// mesma forma que emitSingleDatabaseWiring agora constrói para o produtor
// durável — publica cada evento apensado após o commit, EXATAMENTE como a
// runtime.NewUnitOfWork(store, publisher) em memória já fazia (Marco F). Não
// exercita 2PC/outbox/relay (K3.3/K3.4, fora do escopo desta task) — só o
// publisher-após-commit sobre um *sql.DB real (sqlite, offline), reusando a
// fixture Ledger (sql_adapter_test.go) e seu UseCase de banco único
// (PerformDebit).
const ledgerProducerParityBehaviorTest = `package ledger

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type parityCaller struct{ id string }

func (c parityCaller) Authenticated() bool { return true }
func (c parityCaller) ID() string          { return c.id }
func (c parityCaller) HasRole(string) bool { return false }

// fakeChannelPublisher grava cada evento publicado — o mesmo papel que o
// canal de saída (amqpruntime.NewRabbitMQChannel) cumpre em produção, sem
// precisar de um broker de verdade neste teste (mesmo espírito de
// sql_outbox_channel_test.go).
type fakeChannelPublisher struct {
	published []runtime.Event
}

func (p *fakeChannelPublisher) Publish(_ context.Context, ev runtime.Event) error {
	p.published = append(p.published, ev)
	return nil
}

// mainDbDSNForParityTest é substituído por TestLedgerSingleDatabaseProducerParity
// antes de escrever o arquivo (strings.Replace sobre este template — mesma
// técnica de mainDbDSNForTest).
var mainDbDSNForParityTest = "__MAIN_DB_DSN_PARITY__"

// TestSQLUnitOfWorkPublishesAfterCommitLikeMemoryUnitOfWork prova a
// paridade comportamental (NFR-22) que K3.2 depende: com um publisher
// injetado, sqlruntime.NewUnitOfWork publica o evento apensado LOGO APÓS o
// commit, na MESMA ordem de apensação — igual a runtime.NewUnitOfWork(store,
// publisher) (rtsrc/uow.go.txt), só trocando a store de memória por sqlite
// real. PerformDebit continua byte-idêntico (nenhuma mudança em
// lower/stmt.go, §design 4.2-P2) — só a UnitOfWork por trás dele muda.
func TestSQLUnitOfWorkPublishesAfterCommitLikeMemoryUnitOfWork(t *testing.T) {
	db, err := sqlruntime.Open(mainDbDSNForParityTest)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// NewEventStore roda a migração de schema (CREATE TABLE IF NOT EXISTS
	// "events" — eventstore.go.txt); NewUnitOfWork (banco único, sem
	// EventStore intermediário, ver a doc de emitSingleDatabaseWiring) não
	// migra sozinho, então este passo continua necessário mesmo sem usar o
	// *EventStore devolvido depois — mesma ordem que
	// TestPerformDebitPersistsToRealSQLiteAndQueryReadsItBack já usa.
	if _, err := sqlruntime.NewEventStore(context.Background(), db, EventRegistry(), sqlruntime.SQLiteDialect()); err != nil {
		t.Fatalf("NewEventStore (migração de schema): %v", err)
	}

	pub := &fakeChannelPublisher{}
	uow = sqlruntime.NewUnitOfWork(db, EventRegistry(), sqlruntime.SQLiteDialect(), pub)

	callerCtx := runtime.WithCaller(context.Background(), parityCaller{id: "acc-parity"})
	cmd := Debit{AccountId: AccountId("acc-parity"), Amount: EntryAmount(75)}
	if err := PerformDebit(callerCtx, cmd); err != nil {
		t.Fatalf("PerformDebit: %v", err)
	}

	if len(pub.published) != 1 {
		t.Fatalf("esperava 1 evento publicado após o commit, veio %d: %v", len(pub.published), pub.published)
	}
	got, ok := pub.published[0].(*AccountDebited)
	if !ok {
		t.Fatalf("evento publicado tem tipo %T, want *AccountDebited", pub.published[0])
	}
	if got.Id != AccountId("acc-parity") || got.Amount != EntryAmount(75) {
		t.Fatalf("evento publicado = %+v, want Id=acc-parity Amount=75", got)
	}

	// confirma via SQL puro que o Append TAMBÉM aconteceu na mesma tx (a
	// escrita não depende do publisher, mesma independência que o caminho em
	// memória já garante).
	var count int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM events WHERE aggregate_id = ?", "acc-parity").Scan(&count); err != nil {
		t.Fatalf("consulta SQL direta: %v", err)
	}
	if count != 1 {
		t.Fatalf("esperava 1 linha em events para acc-parity, achei %d", count)
	}
}
`

// TestLedgerSingleDatabaseProducerParity gera o projeto Ledger (fixture já
// provada por TestLedgerSingleDatabaseBehavior), substitui o placeholder do
// DSN pelo caminho real do arquivo sqlite e roda o teste comportamental de
// paridade acima via "go test" de verdade sobre o pacote "ledger" gerado.
func TestLedgerSingleDatabaseProducerParity(t *testing.T) {
	mainDSN, sideDSN := ledgerSQLitePaths(t)
	files := filesToMap(generateLedgerProject(t, mainDSN, sideDSN))
	testSrc := strings.Replace(ledgerProducerParityBehaviorTest, "__MAIN_DB_DSN_PARITY__", filepath.ToSlash(mainDSN), 1)
	files[filepath.Join("ledger", "producer_parity_test.go")] = []byte(testSrc)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
