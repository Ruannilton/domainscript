package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/goname"
	"domainscript/codegen/rtsrc"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/types"
)

// decl_policy_test.go prova os critérios de conclusão da task F1 (§design
// codegen 3.10, REQ-23.1/23.5) sobre a Policy real do shop
// (docs/examples/shop/shipping/policy.ds — "NotifyShipping on OrderPlaced",
// delivery AtLeastOnce): golden, determinismo, smoke compile e um teste
// comportamental sobre o Go de fato gerado — mesmo padrão de
// decl_projection_test.go (E8.2), a task mais recente sem exemplo real no
// wallet antes desta.
//
// shopProjectDir espelha walletProjectDir (decl_aggregate_test.go): o shop é
// o exemplo de referência de dois services (docs/examples/shop) — já provado
// limpo por driver.TestShopExampleClean (driver/shop_regression_test.go).
var shopProjectDir = filepath.Join("..", "docs", "examples", "shop")

// parseShopProgram resolve o exemplo shop real via driver.CheckProject — a
// primeira vez que o pacote codegen_test usa essa fixture (E9.1 só exercitou
// wallet; shop tem dois services/módulos ligados por canal, o que F1 é a
// primeira task de codegen a precisar).
func parseShopProgram(t *testing.T) *program.Program {
	t.Helper()
	prog, bag := driver.CheckProject(shopProjectDir)
	if bag.HasErrors() {
		t.Fatalf("shop não deveria ter diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}
	return prog
}

// findPolicyDecl acha o *ast.PolicyDecl de nome name em qualquer arquivo do
// programa — espelha findAggregateDecl/findEventDecl/findProjectionDecl.
func findPolicyDecl(t *testing.T, prog *program.Program, name string) *ast.PolicyDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if p, ok := d.(*ast.PolicyDecl); ok && p.Name == name {
				return p
			}
		}
	}
	t.Fatalf("Policy %q não encontrada no shop — o exemplo mudou?", name)
	return nil
}

// emitShippingPolicies gera o Go da Policy NotifyShipping real do shop
// (módulo Shipping, reage ao PublicEvent OrderPlaced de Orders — cross-
// module, daí "contracts.OrderPlaced" no tipo do handler). O módulo Shipping
// não declara nenhum ValueObject (docs/examples/shop/shipping/mod.ds é só
// "Module Shipping {}"), então um VOOperatorRegistry vazio é suficiente — o
// corpo real ("execute { return }") não referencia nenhum.
func emitShippingPolicies(t *testing.T) []byte {
	t.Helper()
	prog := parseShopProgram(t)
	model := types.NewModel(prog.Symbols)
	policy := findPolicyDecl(t, prog, "NotifyShipping")

	got, err := codegen.EmitPolicies("shipping", []*ast.PolicyDecl{policy}, model, prog.Symbols, prog, "Shipping", goname.NewVOOperatorRegistry(), nil)
	if err != nil {
		t.Fatalf("EmitPolicies: erro inesperado: %v", err)
	}
	return got
}

// --- Golden + determinismo -------------------------------------------------

// TestEmitPoliciesGolden prova os elementos centrais do critério de
// conclusão da task: NotifyShipping vira um handler com a assinatura EXATA
// de runtime.Dispatcher/Outbox.Subscribe (func(ctx, ev runtime.Event) error),
// faz o type assertion pro tipo concreto do evento via ponteiro qualificado
// por pacote ("*contracts.OrderPlaced" — PublicEvent, pacote compartilhado).
// NotifyShipping é cross-service (topology.ds: "Orders -> Shipping { via:
// queue orderBy: id }") — Wire (F5, REQ-26.5) constrói um
// runtime.ChannelTransport de verdade (runtime.NewQueueChannel, var de
// pacote notifyShippingChannel) em vez de assinar em "d" direto, com um
// KeyFunc que extrai orderBy (campo "id" -> "Id") do evento, e regista
// NotifyShipping nesse transporte via um Outbox PRÓPRIO (ainda
// "delivery AtLeastOnce", REQ-23.1) — não mais o Outbox(d) que F1 gerava.
func TestEmitPoliciesGolden(t *testing.T) {
	got := emitShippingPolicies(t)
	for _, want := range []string{
		"func NotifyShipping(ctx context.Context, ev runtime.Event) error",
		"event, ok := ev.(*contracts.OrderPlaced)",
		"if !ok {",
		"caller, _ := runtime.CallerFrom(ctx)",
		"_ = caller",
		"_ = event",
		"var notifyShippingChannel runtime.ChannelTransport",
		"func Wire(d runtime.Dispatcher) {",
		"notifyShippingChannel = runtime.NewQueueChannel(runtime.QueueChannelConfig{Concurrency: 1, MaxRate: 0, BatchSize: 1}, func(ev runtime.Event) string {",
		"switch e := ev.(type) {",
		"case *contracts.OrderPlaced:",
		"return fmt.Sprint(e.Id)",
		"notifyShippingChannelOutbox := runtime.NewOutbox(notifyShippingChannel)",
		`notifyShippingChannelOutbox.Subscribe("OrderPlaced", NotifyShipping)`,
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("esperava a presença de %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "policies_shipping.go.golden"), got)
}

// TestEmitPoliciesDeterministic prova NFR-13.
func TestEmitPoliciesDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitShippingPolicies(t)
	})
}

// --- Smoke compile + comportamento -----------------------------------------

// policySmokeFiles monta os arquivos mínimos para compilar NotifyShipping
// isoladamente: go.mod + runtime real + orders/{value_objects,events}.go (o
// PublicEvent OrderPlaced mora, DE VERDADE, no pacote do módulo que o
// declara — Orders, ver a doc de decl_event.go — junto dos ValueObjects que
// seus campos usam) + contracts/events.go (só o alias de OrderPlaced) +
// shipping/policies.go — sem nenhum Aggregate/UseCase de Orders (o handler
// de NotifyShipping não precisa deles, só do TIPO do evento).
func policySmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog := parseShopProgram(t)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	vos := make([]*ast.ValueObjectDecl, 0, 3)
	for _, name := range []string{"OrderId", "CustomerName", "Money"} {
		vos = append(vos, findValueObjectDecl(t, prog, name))
	}
	for _, vo := range vos {
		voGo, err := codegen.EmitValueObject("orders", vo)
		if err != nil {
			t.Fatalf("EmitValueObject(%s): erro inesperado: %v", vo.Name, err)
		}
		files[filepath.Join("orders", strings.ToLower(vo.Name)+".go")] = voGo
	}

	orderPlaced := findEventDecl(t, prog, "OrderPlaced")
	ordersEventsGo, err := codegen.EmitEvents("orders", []*ast.EventDecl{orderPlaced})
	if err != nil {
		t.Fatalf("EmitEvents(OrderPlaced): erro inesperado: %v", err)
	}
	files[filepath.Join("orders", "events.go")] = ordersEventsGo

	contractsGo, err := codegen.EmitPublicEvents([]*ast.EventDecl{orderPlaced}, map[string]string{"OrderPlaced": "Orders"})
	if err != nil {
		t.Fatalf("EmitPublicEvents(OrderPlaced): erro inesperado: %v", err)
	}
	files[filepath.Join("contracts", "events.go")] = contractsGo

	files[filepath.Join("shipping", "policies.go")] = emitShippingPolicies(t)
	return files
}

// TestEmitPoliciesSmokeCompile prova NFR-14: o Go de NotifyShipping, junto
// de contracts/OrderPlaced e do runtime vendorado real, compila e passa go
// vet num projeto isolado — metade do critério de conclusão da task
// ("compila", escopado à Policy isolada; a outra metade, o projeto shop
// inteiro com os dois services, é TestGenerateShopPolicyRegistersSubscriber
// AndCompiles abaixo).
func TestEmitPoliciesSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, policySmokeFiles(t))
}

// shippingPolicyBehaviorTest roda dentro do projeto isolado gerado no smoke
// e prova, sobre o Go de fato gerado (não uma reimplementação), o critério
// de conclusão literal da task: "NotifyShipping on OrderPlaced registra o
// subscriber" — mas agora (F5, REQ-26.5) através do canal REAL da
// topologia: Wire(dispatcher) assina NotifyShipping no runtime.
// ChannelTransport que ele mesmo constrói (notifyShippingChannel, var de
// pacote — ver decl_policy.go), não em "dispatcher" (que fica sem uso para
// esta Policy cross-service). Publicar um *contracts.OrderPlaced
// DIRETAMENTE em notifyShippingChannel — exatamente o que um produtor real
// (cmd/sales/main.go) faria — alcança NotifyShipping sem erro; um segundo
// Subscribe no MESMO transporte (um observador de teste, sem mudar
// NotifyShipping) prova que a entrega REALMENTE aconteceu (não só que
// Publish não retornou erro — Publish é assíncrono, "enfileira e retorna",
// então só o retorno de Publish não provaria que o handler rodou).
const shippingPolicyBehaviorTest = `package shipping

import (
	"context"
	"testing"
	"time"

	"domainscript/generated/contracts"
	"domainscript/generated/runtime"
)

func TestWireRegistersNotifyShippingOnQueueChannel(t *testing.T) {
	dispatcher := runtime.NewDispatcher()
	Wire(dispatcher)

	delivered := make(chan struct{}, 1)
	notifyShippingChannel.Subscribe("OrderPlaced", func(ctx context.Context, ev runtime.Event) error {
		delivered <- struct{}{}
		return nil
	})

	ev := &contracts.OrderPlaced{Id: "order-1"}
	if err := notifyShippingChannel.Publish(context.Background(), ev); err != nil {
		t.Fatalf("Publish: erro inesperado: %v", err)
	}

	select {
	case <-delivered:
		// NotifyShipping rodou sem erro (senão deliver, rtsrc/channel.go.txt,
		// teria parado no primeiro handler e nunca chamado o observador).
	case <-time.After(time.Second):
		t.Fatal("esperava que notifyShippingChannel entregasse o evento a NotifyShipping (via Wire) dentro de 1s")
	}
}
`

// TestEmitPoliciesBehaviorWireRegistersSubscriber prova NFR-15 sobre o
// critério de conclusão literal da task (ver a doc de
// shippingPolicyBehaviorTest).
func TestEmitPoliciesBehaviorWireRegistersSubscriber(t *testing.T) {
	files := policySmokeFiles(t)
	files[filepath.Join("shipping", "policies_behavior_test.go")] = []byte(shippingPolicyBehaviorTest)
	runGeneratedTests(t, files)
}

// --- Projeto shop inteiro (os dois services) --------------------------------

// shopGenerateOptions usa o MESMO module path que o resto do pacote codegen
// assume implicitamente via RuntimeImportPath (ver walletGenerateOptions,
// codegen_test.go).
var shopGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateShopProject roda Generate sobre o Program real do shop (dois
// services, Sales/Orders e Delivery/Shipping, ligados pelo canal queue de
// topology.ds) — o pipeline completo que um usuário final rodaria
// (driver.CheckProject -> codegen.Generate), mesmo padrão de
// generateWalletProject (codegen_test.go, E9.1), agora sobre um projeto
// multi-módulo/multi-service pela primeira vez neste pacote.
func generateShopProject(t *testing.T) []codegen.File {
	t.Helper()
	prog, bag := driver.CheckProject(shopProjectDir)
	if bag.HasErrors() {
		t.Fatalf("shop não deveria ter diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, shopGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre o shop real: %v", err)
	}
	return files
}

// TestGenerateShopPolicyRegistersSubscriberAndCompiles é o critério de
// conclusão literal da task, de ponta a ponta sobre o orquestrador completo
// (Generate, não EmitPolicies isolado): "NotifyShipping on OrderPlaced
// registra o subscriber e compila" — shipping/policies.go existe com o
// handler e o Wire (F5, REQ-26.5: Wire constrói o canal "queue" da
// topologia — notifyShippingChannel — em vez de assinar em "d" direto),
// cmd/delivery/main.go (o service Delivery, dono do módulo Shipping —
// topology.ds) continua instanciando o dispatcher e chamando "shipping.
// Wire(dispatcher)" (d fica sem uso PARA ESSA Policy especificamente, mas
// Wire's assinatura não muda — ver a doc de emitPolicyWireFunc),
// cmd/sales/main.go (o service Sales, dono do módulo Orders, produtor do
// PublicEvent OrderPlaced que atravessa o MESMO canal) agora constrói sua
// PRÓPRIA instância do canal e injeta como publisher da unit of work
// (runtime.NewUnitOfWork(store, ordersChannel) — o "outbox in-memory
// ligando emit->dispatcher/notify" do lado de quem publica), e o projeto
// gerado INTEIRO — os dois services juntos — compila.
func TestGenerateShopPolicyRegistersSubscriberAndCompiles(t *testing.T) {
	files := generateShopProject(t)
	m := filesToMap(files)

	policiesGo, ok := m["shipping/policies.go"]
	if !ok {
		t.Fatalf("esperava shipping/policies.go entre os arquivos gerados, não achei:\n%v", filePathsForTest(files))
	}
	for _, want := range []string{
		"func NotifyShipping(ctx context.Context, ev runtime.Event) error",
		"var notifyShippingChannel runtime.ChannelTransport",
		"func Wire(d runtime.Dispatcher) {",
		"notifyShippingChannel = runtime.NewQueueChannel(runtime.QueueChannelConfig{Concurrency: 1, MaxRate: 0, BatchSize: 1}, func(ev runtime.Event) string {",
		"notifyShippingChannelOutbox := runtime.NewOutbox(notifyShippingChannel)",
		`notifyShippingChannelOutbox.Subscribe("OrderPlaced", NotifyShipping)`,
	} {
		if !strings.Contains(string(policiesGo), want) {
			t.Errorf("esperava %q em shipping/policies.go, não achei:\n%s", want, policiesGo)
		}
	}

	deliveryMain, ok := m["cmd/delivery/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/delivery/main.go (service Delivery, módulo Shipping), não achei:\n%v", filePathsForTest(files))
	}
	for _, want := range []string{
		"dispatcher := runtime.NewDispatcher()",
		"shipping.Wire(dispatcher)",
	} {
		if !strings.Contains(string(deliveryMain), want) {
			t.Errorf("esperava %q em cmd/delivery/main.go, não achei:\n%s", want, deliveryMain)
		}
	}

	salesMain, ok := m["cmd/sales/main.go"]
	if !ok {
		t.Fatalf("esperava cmd/sales/main.go (service Sales, módulo Orders), não achei:\n%v", filePathsForTest(files))
	}
	if strings.Contains(string(salesMain), "NewDispatcher") {
		t.Errorf("NÃO esperava runtime.Dispatcher em cmd/sales/main.go (Orders não declara Policy — o canal de saída usa runtime.ChannelTransport, não Dispatcher):\n%s", salesMain)
	}
	for _, want := range []string{
		"ordersChannel := runtime.NewQueueChannel(runtime.QueueChannelConfig{Concurrency: 1, MaxRate: 0, BatchSize: 1}, func(ev runtime.Event) string {",
		"case *contracts.OrderPlaced:",
		"return fmt.Sprint(e.Id)",
		"uow := runtime.NewUnitOfWork(store, ordersChannel)",
		"orders.Wire(uow)",
	} {
		if !strings.Contains(string(salesMain), want) {
			t.Errorf("esperava %q em cmd/sales/main.go, não achei:\n%s", want, salesMain)
		}
	}

	gentest.SmokeCompile(t, m)
}

// filePathsForTest devolve só os Path de files, para mensagem de erro.
func filePathsForTest(files []codegen.File) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

// TestGenerateShopDeterministic prova NFR-13 sobre o projeto shop inteiro —
// mesmo padrão de TestGenerateWalletHTTPRoutesDeterministic, agora sobre os
// dois services.
func TestGenerateShopDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		files := generateShopProject(t)
		var buf []byte
		for _, f := range files {
			buf = append(buf, []byte("=== "+f.Path+" ===\n")...)
			buf = append(buf, f.Content...)
		}
		return buf
	})
}
