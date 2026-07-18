//go:build integration

package codegen_test

import (
	"os"
	"testing"

	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"

	"domainscript/codegen"
)

// channel_rabbitmq_integration_test.go prova J3.4.c (REQ-43.2, NFR-22/24,
// §design infra-providers 3.3): um teste comportamental DE VERDADE contra um
// RabbitMQ vivo, atrás da build tag "integration" — NUNCA entra no caminho
// de "go test ./..." default (nem sequer compila sem -tags=integration) —
// e, além disso, guardado por env: sem AMQP_URL definida, pula (t.Skip),
// nunca falha (REQ-48.3/NFR-24). Mesmo padrão de
// sql_postgres_integration_test.go (J1.4.b). Rodar de propósito: "AMQP_URL=
// amqp://guest:guest@localhost:5672/ go test -tags=integration ./codegen/
// -run TestRabbitMQIntegration".
//
// Prova de paridade (NFR-22): a MESMA fixture multi-service de
// channel_rabbitmq_test.go (Alpha produz WidgetMade, Beta reage via
// ReactToWidget) é gerada de verdade; o teste embutido no pacote "beta"
// publica um evento a partir de uma instância PRODUTORA
// (ConsumeDisabled: true, mesma forma que cmd/alphasvc/main.go gera) e
// confirma que uma instância CONSUMIDORA separada (ConsumeDisabled: false,
// mesma forma que Wire gera) o recebe via um handler Subscribe — publicar→
// consumir cross-process (duas instâncias DISTINTAS de rabbitmqChannel,
// nenhum estado Go compartilhado entre elas, só o broker) == in-process (o
// mesmo par publish/subscribe já provado sem nenhuma infraestrutura por
// TestEmitPoliciesRabbitMQChannelGolden/TestGenerateRabbitMQChannelFixture
// ProducerAndConsumerCompile, que só verificam o Go GERADO, nunca rodam
// contra um broker).
func generateRabbitMQIntegrationProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"topology.ds":     channelFixtureRabbitMQTopologyDs,
		"alpha/mod.ds":    channelFixtureAlphaModDs,
		"alpha/domain.ds": channelFixtureAlphaDomainDs,
		"beta/mod.ds":     channelFixtureBetaModDs,
		"beta/policy.ds":  channelFixtureBetaPolicyDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de canal rabbitmq (J3.4, integração) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, shopGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture rabbitmq (integração): %v", err)
	}
	return files
}

const rabbitMQIntegrationBehaviorTest = `package beta

import (
	"context"
	"os"
	"testing"
	"time"

	"domainscript/generated/amqpruntime"
	"domainscript/generated/contracts"
	"domainscript/generated/runtime"
)

// TestRabbitMQIntegrationPublishAcrossInstancesIsDelivered prova NFR-22:
// duas instâncias SEPARADAS de rabbitmqChannel (nenhum estado Go
// compartilhado — só o broker as liga), uma produtora (ConsumeDisabled:
// true, mesma forma de cmd/alphasvc/main.go) e uma consumidora
// (ConsumeDisabled: false, mesma forma que Wire gera), sobre o MESMO
// exchange/queue "Alpha-Beta" — Publish na produtora chega no handler
// Subscribe da consumidora.
func TestRabbitMQIntegrationPublishAcrossInstancesIsDelivered(t *testing.T) {
	amqpURL := os.Getenv("AMQP_URL")
	if amqpURL == "" {
		t.Skip("AMQP_URL não definida — pulando teste de integração RabbitMQ (REQ-48.3/NFR-24)")
	}

	registry := map[string]amqpruntime.EventFactory{
		"WidgetMade": func() runtime.Event { return &contracts.WidgetMade{} },
	}

	consumerCfg := amqpruntime.RabbitMQConfig{
		Exchange:    "Alpha-Beta",
		Queue:       "Alpha-Beta",
		Concurrency: 1,
	}
	consumer, err := amqpruntime.NewRabbitMQChannel(amqpURL, consumerCfg, registry)
	if err != nil {
		t.Fatalf("NewRabbitMQChannel (consumidor): %v", err)
	}
	defer consumer.(interface{ Close() error }).Close()

	received := make(chan *contracts.WidgetMade, 1)
	consumer.Subscribe("WidgetMade", func(ctx context.Context, ev runtime.Event) error {
		received <- ev.(*contracts.WidgetMade)
		return nil
	})

	producerCfg := amqpruntime.RabbitMQConfig{
		Exchange:        "Alpha-Beta",
		Queue:           "Alpha-Beta",
		ConsumeDisabled: true,
	}
	producer, err := amqpruntime.NewRabbitMQChannel(amqpURL, producerCfg, nil)
	if err != nil {
		t.Fatalf("NewRabbitMQChannel (produtor): %v", err)
	}
	defer producer.(interface{ Close() error }).Close()

	ev := &contracts.WidgetMade{Id: "widget-integration-1"}
	ev.SetMeta(runtime.EventMeta{AggregateID: "widget-integration-1", Sequence: 1})
	if err := producer.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case got := <-received:
		if got.Id != "widget-integration-1" {
			t.Fatalf("evento recebido = %+v, want Id=widget-integration-1", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout esperando o evento chegar na instância consumidora — publicação cross-instance falhou")
	}
}
`

// TestRabbitMQIntegration roda rabbitMQIntegrationBehaviorTest de verdade
// sobre o projeto gerado inteiro (gentest.RunTests) — o subprocesso herda o
// ambiente do processo pai, então AMQP_URL (checada aqui e de novo dentro do
// teste gerado) chega ao teste comportamental sem nenhuma passagem
// explícita.
func TestRabbitMQIntegration(t *testing.T) {
	if os.Getenv("AMQP_URL") == "" {
		t.Skip("AMQP_URL não definida — pulando teste de integração RabbitMQ (REQ-48.3/NFR-24)")
	}

	files := filesToMap(generateRabbitMQIntegrationProject(t))
	files["beta/rabbitmq_integration_test.go"] = []byte(rabbitMQIntegrationBehaviorTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}
