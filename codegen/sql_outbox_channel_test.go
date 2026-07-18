package codegen_test

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
)

// sql_outbox_channel_test.go prova a DoD de J2.4 (REQ-42.6, R9, §design
// infra-providers 3.2a): um DurableOutbox construído COM publisher roteia
// TODA linha entregue por publisher.Publish em vez de rodar os handlers
// localmente assinados via Subscribe — e uma falha de Publish (simulando o
// "crash entre commit e publish" que REQ-42.6 existe para fechar) deixa a
// linha undelivered, re-entregue no próximo Tick assim que Publish suceder
// (a mesma garantia at-least-once de TestDurableOutboxRetriesOnHandlerFailure,
// agora do lado do publisher). Roda de verdade sobre sqlite real (via
// gentest.WriteFiles/RunTests, mesmo padrão dos demais testes de outbox) —
// o "publisher" aqui é um fake local (Publish com um contador de chamadas),
// já que RabbitMQ (Marco J3) ainda não existe; o mecanismo de roteamento
// não depende de qual ChannelTransport concreto está por trás dele
// (Dispatcher e ChannelTransport já satisfazem Publisher com a MESMA
// assinatura, ver a doc de runtime.Publisher).
const sqlOutboxChannelTest = `package sqlruntime_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"domainscript/generated/runtime"
	"domainscript/generated/sqlruntime"
)

type channelTestEvent struct {
	runtime.EventMeta
	Msg string
}

func (e *channelTestEvent) EventType() string { return "ChannelTestEvent" }

func channelTestFactories() map[string]sqlruntime.EventFactory {
	return map[string]sqlruntime.EventFactory{
		"ChannelTestEvent": func() runtime.Event { return &channelTestEvent{} },
	}
}

func channelTestRegistry() map[string]runtime.EventFactory {
	return map[string]runtime.EventFactory{
		"ChannelTestEvent": func() runtime.Event { return &channelTestEvent{} },
	}
}

func enqueueOneChannelEvent(t *testing.T, db *sql.DB, msg string) {
	t.Helper()
	uow := sqlruntime.NewUnitOfWork(db, channelTestFactories(), sqlruntime.SQLiteDialect())
	ctx := context.Background()
	err := uow.Run(ctx, func(tx runtime.Tx) error {
		if err := tx.Append("channel-agg", []runtime.Event{&channelTestEvent{Msg: msg}}); err != nil {
			return err
		}
		return tx.EnqueueOutbox([]runtime.Event{&channelTestEvent{Msg: msg}})
	})
	if err != nil {
		t.Fatalf("enqueueOneChannelEvent: Run: %v", err)
	}
}

// fakePublisher é um runtime.Publisher mínimo — todo o mecanismo de
// roteamento de DurableOutbox.deliver só depende da assinatura
// Publish(ctx, ev) error, nunca de um ChannelTransport concreto (RabbitMQ,
// Marco J3, ainda não existe).
type fakePublisher struct {
	published []string
	failNext  bool
}

func (p *fakePublisher) Publish(ctx context.Context, ev runtime.Event) error {
	if p.failNext {
		p.failNext = false
		return errors.New("publish falhou (crash simulado)")
	}
	p.published = append(p.published, ev.(*channelTestEvent).Msg)
	return nil
}

// TestDurableOutboxRoutesToPublisherInsteadOfLocalHandlers prova REQ-42.6:
// com um publisher configurado, deliver chama publisher.Publish — NUNCA os
// handlers localmente assinados via Subscribe, mesmo quando o event type
// TEM um handler local registrado (o publisher tem prioridade total,
// roteamento mutuamente exclusivo por instância, não por evento).
func TestDurableOutboxRoutesToPublisherInsteadOfLocalHandlers(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "channel.db")
	db, err := sqlruntime.Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := sqlruntime.NewEventStore(ctx, db, channelTestFactories(), sqlruntime.SQLiteDialect()); err != nil {
		t.Fatalf("NewEventStore: %v", err)
	}
	enqueueOneChannelEvent(t, db, "cross-service")

	store := sqlruntime.NewOutboxStore(db, sqlruntime.SQLiteDialect())
	pub := &fakePublisher{}
	outbox := runtime.NewDurableOutbox(store, channelTestRegistry(), pub)

	localHandlerCalled := false
	outbox.Subscribe("ChannelTestEvent", func(ctx context.Context, ev runtime.Event) error {
		localHandlerCalled = true
		return nil
	})

	n, err := outbox.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if n != 1 {
		t.Fatalf("Tick processou %d linhas, want 1", n)
	}
	if localHandlerCalled {
		t.Fatal("o handler local rodou — deveria ter roteado só para o publisher")
	}
	if len(pub.published) != 1 || pub.published[0] != "cross-service" {
		t.Fatalf("pub.published = %v, want [\"cross-service\"]", pub.published)
	}

	var deliveredAt sql.NullTime
	if err := db.QueryRow("SELECT delivered_at FROM outbox WHERE event_type = 'ChannelTestEvent'").Scan(&deliveredAt); err != nil {
		t.Fatalf("query delivered_at: %v", err)
	}
	if !deliveredAt.Valid {
		t.Fatal("delivered_at continua NULL após Publish bem-sucedido")
	}
}

// TestDurableOutboxRetriesOnPublishFailure prova a garantia de REQ-42.6 (o
// "crash entre commit e publish"): uma falha de Publish deixa a linha
// undelivered (attempts sobe, delivered_at continua NULL) — a MESMA linha é
// re-entregue no próximo Tick, e um Publish que sucede na 2ª tentativa a
// entrega. Nenhum evento é perdido.
func TestDurableOutboxRetriesOnPublishFailure(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "channel-retry.db")
	db, err := sqlruntime.Open(dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	ctx := context.Background()
	if _, err := sqlruntime.NewEventStore(ctx, db, channelTestFactories(), sqlruntime.SQLiteDialect()); err != nil {
		t.Fatalf("NewEventStore: %v", err)
	}
	enqueueOneChannelEvent(t, db, "flaky-publish")

	store := sqlruntime.NewOutboxStore(db, sqlruntime.SQLiteDialect())
	pub := &fakePublisher{failNext: true}
	outbox := runtime.NewDurableOutbox(store, channelTestRegistry(), pub)

	// 1º Tick: Publish falha — linha continua undelivered, attempts sobe.
	n, err := outbox.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick (1º, publish falha): %v", err)
	}
	if n != 1 {
		t.Fatalf("Tick (1º) processou %d linhas, want 1", n)
	}
	if len(pub.published) != 0 {
		t.Fatalf("published após 1º Tick (falho) = %v, want vazio", pub.published)
	}

	var deliveredAt sql.NullTime
	var attempts int
	if err := db.QueryRow("SELECT delivered_at, attempts FROM outbox WHERE event_type = 'ChannelTestEvent'").Scan(&deliveredAt, &attempts); err != nil {
		t.Fatalf("query após 1º Tick: %v", err)
	}
	if deliveredAt.Valid {
		t.Fatal("delivered_at foi marcado mesmo com Publish tendo falhado")
	}
	if attempts != 1 {
		t.Fatalf("attempts após 1º Tick = %d, want 1", attempts)
	}

	// 2º Tick: MESMA linha (undelivered) é re-escaneada; Publish sucede
	// desta vez — o evento cross-service não foi perdido.
	n, err = outbox.Tick(ctx)
	if err != nil {
		t.Fatalf("Tick (2º, re-entrega): %v", err)
	}
	if n != 1 {
		t.Fatalf("Tick (2º) processou %d linhas, want 1 (re-entrega da mesma linha)", n)
	}
	if len(pub.published) != 1 || pub.published[0] != "flaky-publish" {
		t.Fatalf("published após 2º Tick = %v, want [\"flaky-publish\"]", pub.published)
	}

	if err := db.QueryRow("SELECT delivered_at FROM outbox WHERE event_type = 'ChannelTestEvent'").Scan(&deliveredAt); err != nil {
		t.Fatalf("query após 2º Tick: %v", err)
	}
	if !deliveredAt.Valid {
		t.Fatal("delivered_at continua NULL após a re-entrega bem-sucedida")
	}
}
`

// TestSQLOutboxChannelRouting roda sqlOutboxChannelTest de verdade sobre um
// projeto Go mínimo (só runtime/sqlruntime vendorados, mesmo material que
// buildSQLRuntimeProjectFiles monta).
func TestSQLOutboxChannelRouting(t *testing.T) {
	files := buildSQLRuntimeProjectFiles(t)
	files[path.Join("sqlruntime", "outbox_channel_test.go")] = []byte(sqlOutboxChannelTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "30s")
}
