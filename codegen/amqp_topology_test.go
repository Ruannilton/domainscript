package codegen

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
)

// amqp_topology_test.go prova a DoD de J3.2.c (REQ-43.3/43.4, §design
// infra-providers 3.3): a montagem de exchange consistent-hash/DLX/binding
// do adapter amqpruntime (codegen/amqprt/rabbitmq.go.txt) — as funções
// puras (partitionQueueNames/retryExchangeName/retryQueueName/dlqName/
// mainQueueArgs/retryQueueArgs/consistentHashBindingKey/xDeathCount/
// effectiveRetryTTL/effectiveMaxAttempts/effectiveConcurrency) nunca abrem
// uma conexão AMQP — só montam nomes/amqp.Table — mesmo padrão white-box
// (`package amqpruntime`, não `_test`) e mesma fixture (gentest.WriteFiles/
// RunTests sobre buildAMQPRuntimeProjectFiles) de amqp_envelope_test.go
// (J3.1.e).
const amqpTopologyTest = `package amqpruntime

import (
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

// TestPartitionQueueNames prova REQ-43.3: N nomes estáveis e distintos,
// "<base>-p0".."<base>-p(n-1)" — o que orderBy declarado usa para as filas
// de partição da exchange consistent-hash.
func TestPartitionQueueNames(t *testing.T) {
	got := partitionQueueNames("orders-shipping", 3)
	want := []string{"orders-shipping-p0", "orders-shipping-p1", "orders-shipping-p2"}
	if len(got) != len(want) {
		t.Fatalf("partitionQueueNames: len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("partitionQueueNames[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRetryTopologyNames prova que os três recursos extras (DLX de retry,
// retry queue, DLQ final) derivam deterministicamente do nome da exchange
// do canal, sem colidir entre si nem com a exchange original.
func TestRetryTopologyNames(t *testing.T) {
	base := "orders-shipping"
	rx := retryExchangeName(base)
	rq := retryQueueName(base)
	dlq := dlqName(base)

	names := map[string]string{"retryExchangeName": rx, "retryQueueName": rq, "dlqName": dlq}
	seen := make(map[string]string)
	for label, name := range names {
		if name == base {
			t.Fatalf("%s(%q) = %q, colide com a exchange original", label, base, name)
		}
		if other, dup := seen[name]; dup {
			t.Fatalf("%s(%q) = %q, colide com %s", label, base, name, other)
		}
		seen[name] = label
	}
}

// TestMainQueueArgsPointsToRetryExchange prova REQ-43.4: a fila principal
// (partição ou única) declara x-dead-letter-exchange apontando pra DLX de
// retry — um nack(requeue=false) passa a rotear pra lá em vez de descartar.
func TestMainQueueArgsPointsToRetryExchange(t *testing.T) {
	args := mainQueueArgs("orders-shipping-retry-dlx")
	got, ok := args["x-dead-letter-exchange"]
	if !ok {
		t.Fatal("mainQueueArgs: sem x-dead-letter-exchange")
	}
	if got != "orders-shipping-retry-dlx" {
		t.Fatalf("mainQueueArgs[x-dead-letter-exchange] = %v, want %q", got, "orders-shipping-retry-dlx")
	}
}

// TestRetryQueueArgsPointsBackToMainExchangeWithTTL prova REQ-43.4: a retry
// queue aponta x-dead-letter-exchange de VOLTA pra exchange ORIGINAL (não
// pra DLX de retry — senão giraria em círculo sem nunca voltar à fila
// principal) e declara x-message-ttl em milissegundos.
func TestRetryQueueArgsPointsBackToMainExchangeWithTTL(t *testing.T) {
	args := retryQueueArgs("orders-shipping", 5*time.Second)
	if got := args["x-dead-letter-exchange"]; got != "orders-shipping" {
		t.Fatalf("retryQueueArgs[x-dead-letter-exchange] = %v, want %q", got, "orders-shipping")
	}
	ttl, ok := args["x-message-ttl"]
	if !ok {
		t.Fatal("retryQueueArgs: sem x-message-ttl")
	}
	if ttl != int64(5000) {
		t.Fatalf("retryQueueArgs[x-message-ttl] = %v (%T), want int64(5000)", ttl, ttl)
	}
}

// TestConsistentHashBindingKeyIsNumeric prova que a binding key usada por
// TODA fila de partição é o formato que a exchange x-consistent-hash exige
// (peso textual, não um padrão de rota) — mesmo valor pra todas (peso
// uniforme).
func TestConsistentHashBindingKeyIsNumeric(t *testing.T) {
	got := consistentHashBindingKey()
	if got == "" {
		t.Fatal("consistentHashBindingKey: vazio")
	}
	for _, r := range got {
		if r < '0' || r > '9' {
			t.Fatalf("consistentHashBindingKey() = %q, contém caractere não numérico %q", got, r)
		}
	}
}

// TestXDeathCountSumsAcrossEntries prova que xDeathCount lê o header
// x-death (formato padrão do RabbitMQ: array de amqp.Table, cada um com um
// campo "count") e soma o total — o que consume usa pra decidir entre
// nack(requeue=false) (mais uma volta) ou DLQ final (REQ-43.4).
func TestXDeathCountSumsAcrossEntries(t *testing.T) {
	if got := xDeathCount(nil); got != 0 {
		t.Fatalf("xDeathCount(nil) = %d, want 0", got)
	}
	if got := xDeathCount(amqp.Table{}); got != 0 {
		t.Fatalf("xDeathCount(vazio) = %d, want 0", got)
	}

	headers := amqp.Table{
		"x-death": []interface{}{
			amqp.Table{"count": int64(2), "reason": "expired"},
			amqp.Table{"count": int64(1), "reason": "rejected"},
		},
	}
	if got := xDeathCount(headers); got != 3 {
		t.Fatalf("xDeathCount = %d, want 3 (2+1)", got)
	}
}

// TestEffectiveMaxAttemptsAndRetryTTLDefaults prova o clamping de
// configuração ausente/inválida (REQ-43.4): <= 0 sempre vira o default
// documentado, nunca um comportamento indefinido (retry infinito ou TTL
// zero, que faria a retry queue reencaminhar instantaneamente).
func TestEffectiveMaxAttemptsAndRetryTTLDefaults(t *testing.T) {
	if got := effectiveMaxAttempts(0); got != defaultMaxAttempts {
		t.Fatalf("effectiveMaxAttempts(0) = %d, want %d", got, defaultMaxAttempts)
	}
	if got := effectiveMaxAttempts(-1); got != defaultMaxAttempts {
		t.Fatalf("effectiveMaxAttempts(-1) = %d, want %d", got, defaultMaxAttempts)
	}
	if got := effectiveMaxAttempts(3); got != 3 {
		t.Fatalf("effectiveMaxAttempts(3) = %d, want 3", got)
	}
	if got := effectiveRetryTTL(0); got != defaultRetryTTL {
		t.Fatalf("effectiveRetryTTL(0) = %v, want %v", got, defaultRetryTTL)
	}
	if got := effectiveRetryTTL(10 * time.Second); got != 10*time.Second {
		t.Fatalf("effectiveRetryTTL(10s) = %v, want 10s", got)
	}
	if got := effectiveConcurrency(0); got != 1 {
		t.Fatalf("effectiveConcurrency(0) = %d, want 1", got)
	}
	if got := effectiveConcurrency(4); got != 4 {
		t.Fatalf("effectiveConcurrency(4) = %d, want 4", got)
	}
}
`

// TestAMQPTopologyAssembly roda amqpTopologyTest de verdade sobre um
// projeto Go mínimo (runtime + amqpruntime vendorados, mesmo material que
// amqp_envelope_test.go usa) — prova REQ-43.3/43.4 sem NENHUMA conexão AMQP
// real.
func TestAMQPTopologyAssembly(t *testing.T) {
	files := buildAMQPRuntimeProjectFiles(t)
	files[path.Join("amqpruntime", "topology_test.go")] = []byte(amqpTopologyTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}
