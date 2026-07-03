package parser

import (
	"testing"

	"domainscript/ast"
)

func TestWorkerEvery(t *testing.T) {
	src := `Worker ProcessExpiredReservations {
		schedule every 1min
		concurrency: 1
		timeout 5min
		onError { retry: { attempts: 3, backoff: "exponential" } }
		execute {
			order.Cancel("Reserva expirada")
		}
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Worker ProcessExpiredReservations sched=every/1min" +
		" set[concurrency=1] set[timeout=5min]" +
		` set[onError={retry:{attempts:3 backoff:"exponential"}}]` +
		` exec()(block (call (. order Cancel) "Reserva expirada")))`
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestWorkerContinuousWithSource(t *testing.T) {
	src := `Worker ProcessOutboundNotifications {
		schedule continuous
		concurrency: 3
		source { list Notification n where n.status == NotificationStatus.Pending }
		execute(notification) { notification.send() }
	}`
	got := sdecl(parseDeclOK(t, src))
	want := "(Worker ProcessOutboundNotifications sched=continuous set[concurrency=3]" +
		" source(block (list Notification :n {where (== (. n status) (. NotificationStatus Pending))}))" +
		" exec(notification)(block (call (. notification send))))"
	if got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestWorkerCron(t *testing.T) {
	got := sdecl(parseDeclOK(t, `Worker DailySettlement { schedule cron "0 2 * * *" execute { return } }`))
	want := `(Worker DailySettlement sched=cron/"0 2 * * *" exec()(block (return)))`
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestWorkerRecovers(t *testing.T) {
	p, bag := mk(`Worker W { + + execute { return } }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	w, ok := d.(*ast.WorkerDecl)
	if !ok {
		t.Fatalf("esperava WorkerDecl, veio %T", d)
	}
	if w.Execute == nil {
		t.Errorf("execute deveria ser reconhecido apesar do lixo")
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo; parou em %v", p.cur().Kind)
	}
}
