package codegen

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
)

// amqp_reconnect_test.go prova a DoD de J3.3.b (REQ-43.6, §design
// infra-providers 3.3): a sequência de backoff que reconnectLoop usa entre
// tentativas de redial malsucedidas (codegen/amqprt/rabbitmq.go.txt) — a
// única peça do supervisor de reconexão que é uma função LIVRE, testável sem
// nenhuma conexão AMQP real.
//
// O resto do supervisor (supervise/reconnectLoop de verdade, disparados por
// Connection.NotifyClose) não é exercitado aqui de propósito: `*amqp.
// Connection`/`*amqp.Channel` (github.com/rabbitmq/amqp091-go) são STRUCTS
// concretos, não interfaces — não há como fabricar um NotifyClose "fake" sem
// abrir um socket de verdade contra um broker real. Esse teste de integração
// (gated por uma env var tipo AMQP_URL) fica fora do orçamento desta task —
// ver a doc de "Reconexão" em rabbitmq.go.txt e as tasks J3.4/J6.
const amqpReconnectBackoffTest = `package amqpruntime

import (
	"testing"
	"time"
)

// TestNextReconnectBackoffDoublesAndCaps prova REQ-43.6: a sequência de
// espera do reconnectLoop dobra a cada tentativa malsucedida, nunca
// ultrapassando reconnectMaxBackoff — o mesmo idioma de backoff exponencial
// que DurableOutbox.Start (rtsrc/outbox.go.txt) já usa para
// ProcessBatch falhando repetidamente.
func TestNextReconnectBackoffDoublesAndCaps(t *testing.T) {
	got := reconnectInitialBackoff
	for i := 0; i < 10; i++ {
		next := nextReconnectBackoff(got)
		if next < got {
			t.Fatalf("nextReconnectBackoff(%v) = %v, foi para trás", got, next)
		}
		if next > reconnectMaxBackoff {
			t.Fatalf("nextReconnectBackoff(%v) = %v, ultrapassou reconnectMaxBackoff (%v)", got, next, reconnectMaxBackoff)
		}
		got = next
	}
	if got != reconnectMaxBackoff {
		t.Fatalf("depois de dobrar repetidamente, backoff = %v, want reconnectMaxBackoff (%v)", got, reconnectMaxBackoff)
	}
}

// TestNextReconnectBackoffNeverExceedsCapFromLargeInput prova o clamping
// defensivo contra overflow: mesmo partindo de um valor já maior que o
// cap (ou perto do limite de time.Duration), nextReconnectBackoff nunca
// devolve um valor negativo nem maior que reconnectMaxBackoff.
func TestNextReconnectBackoffNeverExceedsCapFromLargeInput(t *testing.T) {
	if got := nextReconnectBackoff(reconnectMaxBackoff); got != reconnectMaxBackoff {
		t.Fatalf("nextReconnectBackoff(reconnectMaxBackoff) = %v, want %v", got, reconnectMaxBackoff)
	}
	if got := nextReconnectBackoff(time.Duration(1) << 62); got <= 0 || got > reconnectMaxBackoff {
		t.Fatalf("nextReconnectBackoff(valor perto do overflow) = %v, want algo em (0, reconnectMaxBackoff]", got)
	}
}
`

// TestAMQPReconnectBackoffSequence roda amqpReconnectBackoffTest de verdade
// sobre um projeto Go mínimo (runtime + amqpruntime vendorados, mesmo
// material que amqp_envelope_test.go/amqp_topology_test.go usam) — prova
// REQ-43.6 sem NENHUMA conexão AMQP real.
func TestAMQPReconnectBackoffSequence(t *testing.T) {
	files := buildAMQPRuntimeProjectFiles(t)
	files[path.Join("amqpruntime", "reconnect_test.go")] = []byte(amqpReconnectBackoffTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}
