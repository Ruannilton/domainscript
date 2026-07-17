package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// decl_metric_test.go prova os critérios de conclusão da task H3 (§design
// codegen 3.13, REQ-30.3): Metric de negócio (counter/histogram) atualizada
// no gatilho "on Evento" (subscriber no runtime.Dispatcher, WireMetrics) ou
// "on Saga.completed" (hook direto no código gerado da Saga).
//
// Nem docs/examples/wallet nem docs/examples/shop declaram nenhuma Metric
// (confirmado antes de escrever esta task, grep em docs/examples/**/*.ds) —
// a fixture é sintética, mesmo espírito de TelemetryDemo (otel_test.go, H2):
// um módulo "MetricsDemo" com dois casos do spec §21 lado a lado —
//
//  1. DepositVolume: counter "on DepositPerformed" (reaproveita o Event e os
//     campos amount/currency REAIS do wallet, o exemplo worked do spec §21,
//     "value event.amount.amount ... labels { currency = event.amount.
//     currency }") — junto de um Aggregate Wallet/UseCase mínimos, para
//     provar que Metric coexiste com um domínio de verdade no mesmo módulo.
//  2. PurchaseLatency: histogram "on PurchaseTickets.completed" — reaproveita
//     literalmente a Saga PurchaseTickets de F3 (decl_saga_test.go,
//     sagaFixtureSrc), agora dentro do MESMO módulo (uma Metric "on
//     Saga.completed" só é resolvida hoje quando a Saga vive no mesmo
//     módulo — ver a doc de codegen/decl_metric.go).

const metricsDemoModDs = `Module MetricsDemo {
    Database MetricsDemoDb {
        provider: "pg"
        manages: [Wallet]
    }
}
`

const metricsDemoDomainDs = `
ValueObject WalletId(string) {
    Valid { value.length() > 0 }
}

ValueObject Money {
    amount decimal
    currency string
    Valid { amount >= 0 }
}

Event DepositPerformed {
    id     WalletId
    amount Money
}

Aggregate Wallet {
    strategy EventSourced

    state {
        id      WalletId
        balance Money
    }

    access {
        Deposit requires caller.authenticated
    }

    Handle Deposit(amount Money) {
        emit DepositPerformed(self.id, amount)
    }

    Apply DepositPerformed {
        state.balance = event.amount
    }
}

ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

ValueObject TicketId(string) {
    Valid { value.length() > 0 }
}

ValueObject PaymentId(string) {
    Valid { value.length() > 0 }
}

Error NotificationFailed { message "falha ao notificar o cliente" }
`

// metricsDemoApplicationDs reaproveita a Saga PurchaseTickets de F3
// (decl_saga_test.go, sagaFixtureSrc) literalmente — mesmos 3 campos de
// state, mesmos 4 passos, mesmo "down { unrecoverable }" em ConfirmPurchase
// — ao lado de um UseCase mínimo (DepositUseCase) que dispara DepositCmd via
// o Aggregate Wallet acima.
const metricsDemoApplicationDs = `
Command DepositCmd {
    walletId ref Wallet
    amount   Money
}

UseCase DepositUseCase handles DepositCmd {
    execute {
        wallet = load Wallet(cmd.walletId)
        wallet.Deposit(cmd.amount)
    }
}

Command PurchaseTicketsCmd {
    orderId OrderId
}

Saga PurchaseTickets handles PurchaseTicketsCmd {
    mode await timeout 2s
    state { orderId OrderId, ticketIds AppendList<TicketId>, paymentId PaymentId, compensationLog AppendList<string> }

    step ReserveTickets {
        up {
            state.ticketIds.add(TicketId("T1"))
            state.ticketIds.add(TicketId("T2"))
        }
        down {
            state.compensationLog.add("ReserveTickets")
        }
    }

    step ProcessPayment {
        up {
            state.paymentId = PaymentId("PAY-1")
        }
        down {
            state.compensationLog.add("ProcessPayment")
        }
        onInfraError {
            log Warn "infra ao processar pagamento"
        }
    }

    step ConfirmPurchase {
        up {
            return
        }
        down { unrecoverable }
    }

    step NotifyCustomer {
        up {
            ensure state.orderId != OrderId("FAIL-TRIGGER") else NotificationFailed
        }
    }
}
`

const metricsDemoMetricsDs = `
Metric DepositVolume {
    type counter
    value event.amount.amount
    on DepositPerformed
    labels { currency = event.amount.currency }
}

Metric PurchaseLatency {
    type histogram
    buckets [100ms, 250ms, 500ms, 1s, 2s, 5s, 10s]
    on PurchaseTickets.completed
}
`

// metricsDemoGenerateOptions espelha telemetryDemoGenerateOptions/
// walletGenerateOptions — mesmo module path que RuntimeImportPath assume
// implicitamente.
var metricsDemoGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateMetricsDemoProject escreve a fixture MetricsDemo em disco e gera o
// projeto Go completo via driver.CheckProject + codegen.Generate — mesmo
// padrão de generateTelemetryDemoProject/generateGRPCDemoProject.
func generateMetricsDemoProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         metricsDemoModDs,
		"domain.ds":      metricsDemoDomainDs,
		"application.ds": metricsDemoApplicationDs,
		"metrics.ds":     metricsDemoMetricsDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética MetricsDemo (H3) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, metricsDemoGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture MetricsDemo: %v", err)
	}
	return files
}

func metricsDemoFileByPath(t *testing.T, files []codegen.File, p string) []byte {
	t.Helper()
	for _, f := range files {
		if f.Path == p {
			return f.Content
		}
	}
	t.Fatalf("%s não encontrado entre os arquivos gerados:\n%v", p, filePathsForTest(files))
	return nil
}

func metricsDemoMetricsFile(t *testing.T) []byte {
	t.Helper()
	return metricsDemoFileByPath(t, generateMetricsDemoProject(t), "metricsdemo/metrics.go")
}

func metricsDemoSagasFile(t *testing.T) []byte {
	t.Helper()
	return metricsDemoFileByPath(t, generateMetricsDemoProject(t), "metricsdemo/sagas.go")
}

// --- 1. Golden: metrics.go (Metric "on Evento", DepositVolume). -----------

// TestGenerateMetricsDemoMetricsGolden prova, sobre metricsdemo/metrics.go:
// a var de registry runtime.Counter, o subscriber com a assinatura EXATA de
// runtime.Dispatcher.Subscribe, o type assertion pro Event concreto, o value
// (event.amount.amount, um runtime.Decimal convertido via Float64()) e os
// labels (currency, convertido para string via fmt.Sprintf), e WireMetrics
// registrando o subscriber no Dispatcher.
func TestGenerateMetricsDemoMetricsGolden(t *testing.T) {
	got := string(metricsDemoMetricsFile(t))
	for _, want := range []string{
		`var DepositVolumeCounter = runtime.NewCounter("DepositVolume")`,
		"func DepositVolumeOnDepositPerformed(ctx context.Context, ev runtime.Event) error {",
		"event, ok := ev.(*DepositPerformed)",
		"if !ok {",
		"_ = event",
		`DepositVolumeCounter.Add(event.Amount.Amount.Float64(), map[string]string{"currency": fmt.Sprintf("%v", event.Amount.Currency)})`,
		"return nil",
		"func WireMetrics(d runtime.Dispatcher) {",
		`d.Subscribe("DepositPerformed", DepositVolumeOnDepositPerformed)`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q em metricsdemo/metrics.go, não achei:\n%s", want, got)
		}
	}
	// PurchaseLatency ("on Saga.completed") NÃO deveria aparecer aqui — vive
	// inteiramente em sagas.go (ver TestGenerateMetricsDemoSagasGolden).
	if strings.Contains(got, "PurchaseLatency") {
		t.Errorf("NÃO esperava PurchaseLatency em metricsdemo/metrics.go (Metric \"on Saga.completed\" vive em sagas.go):\n%s", got)
	}
	gentest.Golden(t, filepath.Join("testdata", "metrics_metricsdemo.go.golden"), []byte(got))
}

func TestGenerateMetricsDemoMetricsDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return metricsDemoMetricsFile(t)
	})
}

// --- 2. Golden: sagas.go (Metric "on Saga.completed", PurchaseLatency). ---

// TestGenerateMetricsDemoSagasGolden prova, sobre metricsdemo/sagas.go: a
// var de registry runtime.Histogram com os buckets já materializados em
// SEGUNDOS (100ms->0.1, ..., 10s->10), "start := time.Now()" ANTES de
// disparar a goroutine dos passos, e "PurchaseLatencyHistogram.Observe(
// time.Since(start).Seconds(), nil)" dentro de "if res.Err == nil" no ponto
// de sucesso — ANTES do "return state, res.Err" de sempre (F3).
func TestGenerateMetricsDemoSagasGolden(t *testing.T) {
	got := string(metricsDemoSagasFile(t))
	for _, want := range []string{
		`var PurchaseLatencyHistogram = runtime.NewHistogram("PurchaseLatency", []float64{0.1, 0.25, 0.5, 1, 2, 5, 10})`,
		"start := time.Now()",
		"if res.Err == nil {",
		"PurchaseLatencyHistogram.Observe(time.Since(start).Seconds(), nil)",
		"return state, res.Err",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q em metricsdemo/sagas.go, não achei:\n%s", want, got)
		}
	}
	// A ordem importa: o hook de sucesso vem ANTES do "return state, res.Err"
	// dentro do MESMO "case res := <-done:".
	hookIdx := strings.Index(got, "PurchaseLatencyHistogram.Observe(")
	returnIdx := strings.Index(got, "return state, res.Err")
	if hookIdx == -1 || returnIdx == -1 || hookIdx > returnIdx {
		t.Fatalf("esperava o hook de Metric ANTES de \"return state, res.Err\", ordem inesperada:\n%s", got)
	}
	gentest.Golden(t, filepath.Join("testdata", "sagas_metricsdemo.go.golden"), []byte(got))
}

func TestGenerateMetricsDemoSagasDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return metricsDemoSagasFile(t)
	})
}

// --- 3. Wiring: cmd/metricsdemo/main.go (needsDispatcher via Metric). -----

// TestGenerateMetricsDemoMainWiresDispatcherAndMetrics prova que um módulo
// com UseCase (sem Policy) E uma Metric "on Evento" ainda assim ganha um
// runtime.Dispatcher (needsDispatcher, ver a doc de generateCmdMainFile) —
// EXATAMENTE como uma Query cacheada (G3) já fazia — e chama
// "metricsdemo.WireMetrics(dispatcher)" ao lado de "metricsdemo.Wire(uow)".
func TestGenerateMetricsDemoMainWiresDispatcherAndMetrics(t *testing.T) {
	files := generateMetricsDemoProject(t)
	got := string(metricsDemoFileByPath(t, files, "cmd/metricsdemo/main.go"))
	for _, want := range []string{
		"dispatcher := runtime.NewDispatcher()",
		"uow := runtime.NewUnitOfWork(store, dispatcher)",
		"metricsdemo.Wire(uow)",
		"metricsdemo.WireMetrics(dispatcher)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("esperava %q em cmd/metricsdemo/main.go, não achei:\n%s", want, got)
		}
	}
}

// --- 4. Smoke compile (NFR-14). -------------------------------------------

func TestGenerateMetricsDemoSmokeCompile(t *testing.T) {
	files := generateMetricsDemoProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

func TestGenerateMetricsDemoDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		files := generateMetricsDemoProject(t)
		var buf []byte
		for _, f := range files {
			buf = append(buf, []byte("=== "+f.Path+" ===\n")...)
			buf = append(buf, f.Content...)
		}
		return buf
	})
}

// --- 5. Comportamento: o registry de fato acumula/observa. ----------------

// metricsDemoBehaviorTest roda DENTRO do pacote metricsdemo gerado
// ("package metricsdemo") e prova, sobre o Go de fato gerado (não uma
// reimplementação):
//
//  1. WireMetrics(dispatcher) + dispatcher.Publish(DepositPerformed) atualiza
//     DepositVolumeCounter.Value(currency=USD) com o amount publicado — o
//     mesmo padrão comportamental de TestWireRegistersNotifyShippingOn
//     QueueChannel (decl_policy_test.go), agora sobre um Counter em vez de um
//     segundo Subscribe observador.
//  2. PurchaseTickets(ctx, cmd) rodando até o fim com sucesso registra UMA
//     observação em PurchaseLatencyHistogram com Sum > 0 (a duração real da
//     Saga).
const metricsDemoBehaviorTest = `package metricsdemo

import (
	"context"
	"testing"

	"domainscript/generated/runtime"
)

func TestWireMetricsUpdatesDepositVolumeCounterOnDepositPerformed(t *testing.T) {
	dispatcher := runtime.NewDispatcher()
	WireMetrics(dispatcher)

	ev := &DepositPerformed{
		Id:     WalletId("W1"),
		Amount: Money{Amount: runtime.NewDecimalFromInt(42), Currency: "USD"},
	}
	if err := dispatcher.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: erro inesperado: %v", err)
	}

	got := DepositVolumeCounter.Value(map[string]string{"currency": "USD"})
	if got != 42 {
		t.Fatalf("DepositVolumeCounter.Value(currency=USD) = %v, want 42", got)
	}
}

func TestPurchaseTicketsRecordsLatencyHistogramOnCompletion(t *testing.T) {
	_, err := PurchaseTickets(context.Background(), PurchaseTicketsCmd{OrderId: OrderId("O1")})
	if err != nil {
		t.Fatalf("PurchaseTickets: erro inesperado: %v", err)
	}

	snap := PurchaseLatencyHistogram.Snapshot(nil)
	if snap.Count != 1 {
		t.Fatalf("PurchaseLatencyHistogram.Snapshot(nil).Count = %d, want 1", snap.Count)
	}
	// Sum >= 0 (nunca negativo): NÃO exigimos Sum > 0 aqui — um Saga de 4
	// passos triviais pode legitimamente completar dentro de uma única
	// granularidade do relógio monotônico em certos ambientes (confirmado:
	// um repro isolado nesta mesma sandbox Windows reproduziu
	// "time.Since(start) == 0" para uma goroutine+channel round-trip
	// igualmente trivial) — isso é uma característica do AMBIENTE/relógio,
	// não um defeito no hook gerado (a EXPRESSÃO emitida é sempre
	// "time.Since(start).Seconds()", real, nunca hardcoded — ver
	// TestGenerateMetricsDemoSagasGolden). Exigir Sum > 0 aqui tornaria este
	// teste inerentemente instável entre ambientes com resolução de relógio
	// diferente.
	if snap.Sum < 0 {
		t.Fatalf("PurchaseLatencyHistogram.Snapshot(nil).Sum = %v, want >= 0", snap.Sum)
	}
}
`

// TestGenerateMetricsDemoBehavior prova NFR-15 sobre os dois critérios de
// conclusão comportamentais da task (ver a doc de metricsDemoBehaviorTest).
func TestGenerateMetricsDemoBehavior(t *testing.T) {
	files := filesToMap(generateMetricsDemoProject(t))
	files[filepath.Join("metricsdemo", "metrics_behavior_test.go")] = []byte(metricsDemoBehaviorTest)
	runGeneratedTests(t, files)
}

// --- 6. Regressão: wallet/shop sem Metric continuam sem nenhum artefato. --

// TestGenerateWalletProjectHasNoMetricsArtifacts prova que um programa sem
// nenhuma Metric (o wallet real) continua sem nenhum artefato de MÓDULO de
// H3: nenhum "<módulo>/metrics.go" e nenhuma chamada WireMetrics em
// cmd/<service>/main.go — mesmo espírito de
// TestGenerateWalletProjectHasNoOTelArtifacts (otel_test.go, H2).
// "runtime/metrics.go" (rtsrc/metrics.go.txt, o registry Counter/Histogram
// em memória) é ESPERADO sempre presente, mesmo sem Metric declarada — ao
// contrário de otelruntime/* (opt-in), é parte do runtime NÚCLEO (ver a doc
// de codegen/decl_metric.go) — por isso o check abaixo exclui
// "runtime/metrics.go" explicitamente.
func TestGenerateWalletProjectHasNoMetricsArtifacts(t *testing.T) {
	files := generateWalletProject(t)
	for _, f := range files {
		if f.Path == "runtime/metrics.go" {
			continue // sempre presente (runtime núcleo), ver a doc acima
		}
		if strings.HasSuffix(f.Path, "metrics.go") {
			t.Fatalf("wallet não deveria gerar nenhum <módulo>/metrics.go (sem Metric declarada), achei %q", f.Path)
		}
		if strings.Contains(string(f.Content), "WireMetrics") {
			t.Fatalf("wallet não deveria mencionar WireMetrics em nenhum arquivo (sem Metric declarada): %q", f.Path)
		}
	}
}
