package codegen

import (
	"path"
	"testing"

	"domainscript/codegen/amqprt"
	"domainscript/codegen/gentest"
	"domainscript/codegen/rtsrc"
)

// amqp_envelope_test.go prova a DoD de J3.1.e (REQ-43.1/43.5, §design
// infra-providers 3.3): o round-trip encodeEnvelope→decodeEnvelope do
// adapter amqpruntime (codegen/amqprt/rabbitmq.go.txt) — SEM abrir nenhuma
// conexão AMQP real (as funções são livres, só JSON, nunca tocam
// *amqp.Connection). Como rabbitmq.go.txt não é compilado diretamente por
// este módulo (só embutido como texto), a única forma de exercitá-lo de
// verdade é compilar e rodar um pacote amqpruntime real dentro de um
// projeto Go efêmero (mesmo padrão de sql_outbox_dialect_test.go/
// sql_postgres_dialect_test.go). O teste embutido é `package amqpruntime`
// (white-box, não `_test`): encodeEnvelope/decodeEnvelope não são
// exportadas de propósito (só o wiring gerado, dentro do MESMO pacote, as
// chama de verdade) — mesmo padrão de teste interno que o próprio Go stdlib
// usa (`export_test.go`), só que aqui o teste inteiro já é interno, sem
// precisar de nenhum helper de reexportação. Este arquivo é `package
// codegen` (interno, não `codegen_test`) para poder ler
// channelProviders["rabbitmq"] direto (não exportado) ao montar o go.mod da
// fixture — mesmo padrão de provider_registry_test.go/provider_runtime_test.go.
//
// Prova especificamente R8 (REQ-43.5): decodeEnvelope não distingue "de que
// pacote" veio a factory de um eventType — um registry que MISTURA
// factories "locais" (simulando o EventRegistry() do módulo) e "de
// contracts" (simulando contracts.EventRegistry(), o PublicEvent produzido
// por OUTRO módulo/service) decodifica os dois corretamente a partir do
// MESMO registry map — exatamente a forma que o wiring do consumidor (task
// J3.4) precisa montar (contracts.EventRegistry() mesclado ao registry do
// módulo). Um eventType fora do registry (nem local nem contracts) é uma
// falha PERMANENTE (erro claro), nunca um pânico nem uma decodificação
// silenciosamente errada.
const amqpEnvelopeTest = `package amqpruntime

import (
	"encoding/json"
	"strings"
	"testing"

	"domainscript/generated/runtime"
)

// localEvent simula um Event PRIVADO, declarado pelo próprio módulo
// consumidor — coberto pelo registry "local" (o EventRegistry() do pacote
// gerado do módulo, decl_event.go).
type localEvent struct {
	runtime.EventMeta
	Msg string
}

func (e *localEvent) EventType() string { return "LocalEvent" }

// contractsEvent simula um PublicEvent que vive em contracts/ (E4.2) — o
// caso que R8/REQ-43.5 existe para fechar: um evento produzido por OUTRO
// módulo/service, cuja factory só aparece no registry quando o wiring do
// consumidor mescla contracts.EventRegistry() (task J3.4).
type contractsEvent struct {
	runtime.EventMeta
	OrderID string
}

func (e *contractsEvent) EventType() string { return "ContractsEvent" }

// mergedRegistry espelha o que o wiring do consumidor (task J3.4) vai
// montar de verdade: o EventRegistry() do módulo + contracts.EventRegistry()
// — aqui simulados por dois map literais concatenados num só, já que este
// teste não depende de nenhum código gerado por decl_event.go/EmitPublicEvents.
func mergedRegistry() map[string]EventFactory {
	local := map[string]EventFactory{
		"LocalEvent": func() runtime.Event { return &localEvent{} },
	}
	contracts := map[string]EventFactory{
		"ContractsEvent": func() runtime.Event { return &contractsEvent{} },
	}
	merged := make(map[string]EventFactory, len(local)+len(contracts))
	for k, v := range local {
		merged[k] = v
	}
	for k, v := range contracts {
		merged[k] = v
	}
	return merged
}

// TestEnvelopeRoundTripLocalAndContractsEvents prova o envelope JSON
// {eventType, payload} (task J3.1.b) e R8/REQ-43.5 (um registry mesclado
// decodifica tanto um evento local quanto um "de contracts" a partir do
// MESMO map).
func TestEnvelopeRoundTripLocalAndContractsEvents(t *testing.T) {
	registry := mergedRegistry()

	local := &localEvent{Msg: "hello"}
	local.SetMeta(runtime.EventMeta{AggregateID: "agg-1", Sequence: 1})

	wire, err := encodeEnvelope(local)
	if err != nil {
		t.Fatalf("encodeEnvelope(local): %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(wire, &raw); err != nil {
		t.Fatalf("envelope não é um JSON object válido: %v", err)
	}
	if _, ok := raw["eventType"]; !ok {
		t.Fatal("envelope sem a chave \"eventType\"")
	}
	if _, ok := raw["payload"]; !ok {
		t.Fatal("envelope sem a chave \"payload\"")
	}

	decoded, err := decodeEnvelope(wire, registry)
	if err != nil {
		t.Fatalf("decodeEnvelope(local): %v", err)
	}
	got, ok := decoded.(*localEvent)
	if !ok {
		t.Fatalf("decoded = %T, want *localEvent", decoded)
	}
	if got.Msg != "hello" {
		t.Fatalf("got.Msg = %q, want \"hello\"", got.Msg)
	}

	contractsEv := &contractsEvent{OrderID: "order-42"}
	contractsEv.SetMeta(runtime.EventMeta{AggregateID: "agg-2", Sequence: 1})

	wire2, err := encodeEnvelope(contractsEv)
	if err != nil {
		t.Fatalf("encodeEnvelope(contracts): %v", err)
	}
	decoded2, err := decodeEnvelope(wire2, registry)
	if err != nil {
		t.Fatalf("decodeEnvelope(contracts): %v", err)
	}
	got2, ok := decoded2.(*contractsEvent)
	if !ok {
		t.Fatalf("decoded2 = %T, want *contractsEvent", decoded2)
	}
	if got2.OrderID != "order-42" {
		t.Fatalf("got2.OrderID = %q, want \"order-42\"", got2.OrderID)
	}
}

// TestEnvelopeUnknownEventTypeIsPermanentError prova a doc de decodeEnvelope
// sobre "poison pill" (mesma classificação de DurableOutbox.deliver, Marco
// J): um eventType fora do registry (nem local nem contracts) é um erro
// claro, nunca um pânico.
func TestEnvelopeUnknownEventTypeIsPermanentError(t *testing.T) {
	registry := mergedRegistry()
	wire := []byte(` + "`" + `{"eventType":"NeverRegistered","payload":{}}` + "`" + `)

	_, err := decodeEnvelope(wire, registry)
	if err == nil {
		t.Fatal("decodeEnvelope: esperava erro para eventType desconhecido, veio nil")
	}
	if !strings.Contains(err.Error(), "NeverRegistered") {
		t.Fatalf("decodeEnvelope: erro %q não menciona o eventType desconhecido", err.Error())
	}
}
`

// buildAMQPRuntimeProjectFiles monta o material MÍNIMO que codegen.Generate
// escreveria para QUALQUER programa com um canal `provider: "rabbitmq"`
// (go.mod + runtime/*.go + amqpruntime/*.go, J3.1) — sem passar por
// driver.CheckProject/Generate sobre nenhum programa .ds (mesmo espírito de
// buildSQLRuntimeProjectFiles, sql_tenancy_test.go). O go.mod inclui o
// require de amqp091-go via channelProviders["rabbitmq"] (o registro real
// de J3.1) — gentest.RunTests roda "go mod tidy" a partir dele (nenhuma
// chamada de rede além da resolução do módulo: o teste embutido nunca abre
// uma conexão AMQP de verdade).
func buildAMQPRuntimeProjectFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	files["go.mod"] = EmitGoMod(Options{ModulePath: "domainscript/generated"}, "", nil, false, false, []providerDep{channelProviders["rabbitmq"]})

	rtSrcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources: %v", err)
	}
	for name, content := range rtSrcs {
		files[path.Join("runtime", name)] = content
	}

	amqpSrcs, err := amqprt.Sources()
	if err != nil {
		t.Fatalf("amqprt.Sources: %v", err)
	}
	for name, content := range amqpSrcs {
		files[path.Join("amqpruntime", name)] = content
	}
	return files
}

// TestAMQPEnvelopeRoundTrip roda amqpEnvelopeTest de verdade sobre um
// projeto Go mínimo (runtime + amqpruntime vendorados).
func TestAMQPEnvelopeRoundTrip(t *testing.T) {
	files := buildAMQPRuntimeProjectFiles(t)
	files[path.Join("amqpruntime", "envelope_test.go")] = []byte(amqpEnvelopeTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}
