package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen"
)

// generate_e2e_shop_test.go fecha um gap encontrado na auditoria da task H5
// (§design codegen 5, DoD §5.2/5.3/5.4, NFR-12/13): generate_e2e_wallet_test.go
// prova os critérios da DoD sobre o Wallet (módulo único, 1 service implícito),
// mas nenhum teste chamava GenerateProject sobre docs/examples/shop — o único
// exemplo MULTI-módulo/multi-service do repositório, citado explicitamente pela
// DoD §5.2 ("O gerado a partir de docs/examples/wallet e docs/examples/shop
// compila... e passa os testes de fumaça"). shop_regression_test.go (E?) só
// exercita o FRONT-END (CheckProject/diagnósticos) sobre o Shop — nunca chama
// o gerador. Este arquivo mesma estrutura/nomenclatura de
// generate_e2e_wallet_test.go, adaptado à topologia do Shop: dois services
// (Sales{Orders}, Delivery{Shipping}) ligados por um canal `queue` cross-service
// (topology.ds) — logo dois cmd/<service>/main.go, não um.
//
// Achado da auditoria (ver investigação registrada no fechamento H5,
// tasks.md): Orders/mod.ds declara `Database MainDb { provider: "postgres" }`,
// mas G1 (codegen/sql_wiring.go) só reconhece "sqlite" (case-insensitive) como
// adapter real — "postgres" é decorativo (mesmo achado documentado em
// codegen/sql_adapter_test.go). Não há Interface GRPC nem Telemetry em nenhum
// .ds do Shop. Logo o go.mod gerado do Shop, como o do Wallet, não tem NENHUM
// require — TestGenerateShopE2EGoModHasNoExternalRequire prova isso
// empiricamente (não apenas por leitura do código), porque uma mudança futura
// em sql_wiring.go que passasse a reconhecer "postgres" quebraria este teste.
//
// Julgamento sobre teste comportamental: o único Policy do Shop é
// `execute { return }` (sem lógica de negócio observável) e não há *.test.ds
// no exemplo — um teste comportamental "de verdade" exigiria sintetizar
// fixtures de negócio do zero (mesmo esforço documentado em
// codegen/decl_policy_test.go e na fatia H4 de Policy/Query, que preferiu
// sintetizar um módulo Refunds à parte a inflar o Shop). Isso não seria uma
// prova adicional sobre o Shop real, só sobre uma fixture paralela — fora do
// escopo de fechamento da H5. Em vez disso, TestGenerateShopE2ESmokeCompile
// roda `go build ./...`, `go vet ./...` E `go test ./...` (que passa "no test
// files" nos pacotes de domínio, mas efetivamente compila e roda a suíte real,
// incl. o pacote runtime vendorado) sobre a saída de verdade escrita em disco —
// closure honesto do gap sem inflar superfície de teste.

// generateShopE2EProject roda GenerateProject sobre o Shop real
// (docs/examples/shop), escrevendo em um diretório temporário isolado — o
// MESMO caminho que `dsc gen docs/examples/shop -o <out>` percorre.
func generateShopE2EProject(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out")
	bag, err := GenerateProject(shopExampleDir, out, codegen.Options{})
	if err != nil {
		t.Fatalf("GenerateProject: erro inesperado sobre o shop real: %v", err)
	}
	if bag.HasErrors() {
		t.Fatalf("shop não deveria ter diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}
	return out
}

// TestGenerateShopE2ELayout prova que a saída reflete a topologia multi-
// service do Shop (REQ-14.5, §design codegen 3.4): dois cmd/<service>/main.go
// (um por Service da topology.ds — Sales e Delivery, não "shop" nem um cmd
// por módulo), o pacote de cada módulo de domínio (orders/, shipping/) e o
// pacote contracts/ com o PublicEvent compartilhado cross-módulo (OrderPlaced).
func TestGenerateShopE2ELayout(t *testing.T) {
	out := generateShopE2EProject(t)

	for _, rel := range []string{
		"go.mod",
		"runtime/eventstore.go",
		"contracts/events.go",
		"orders/commands.go",
		"orders/usecases.go",
		"shipping/policies.go",
		"cmd/sales/main.go",
		"cmd/delivery/main.go",
	} {
		p := filepath.Join(out, filepath.FromSlash(rel))
		info, statErr := os.Stat(p)
		if statErr != nil {
			t.Errorf("esperava %q em %s, não achei: %v", rel, out, statErr)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%q foi escrito vazio", rel)
		}
	}

	// Não deveria existir um cmd/shop/ (o Shop não tem service default sem
	// nome — todo módulo pertence a um service declarado na topology.ds) nem
	// um cmd por módulo (orders e shipping compartilham... na verdade cada um
	// tem seu próprio service aqui, mas o ponto é: o nome do dir vem do
	// Service, não do módulo).
	if _, err := os.Stat(filepath.Join(out, "cmd", "shop")); !os.IsNotExist(err) {
		t.Fatalf("não esperava cmd/shop/ (o dir de cmd/ vem do Service da topology, não do nome do exemplo)")
	}
}

// TestGenerateShopE2ESmokeCompile prova a DoD §5.2/NFR-14 sobre o Shop: a
// saída REAL escrita em disco por GenerateProject compila, passa go vet e
// roda go test (a suíte real, ainda que sem *.test.ds no exemplo — cobre o
// runtime vendorado e qualquer teste gerado por outros módulos do repo).
func TestGenerateShopE2ESmokeCompile(t *testing.T) {
	out := generateShopE2EProject(t)
	runGoOverDir(t, out, "build", "./...")
	runGoOverDir(t, out, "vet", "./...")
	runGoOverDir(t, out, "test", "./...")
}

// TestGenerateShopE2EGoModHasNoExternalRequire prova empiricamente (NFR-12)
// que o go.mod gerado do Shop não tem require: apesar de Orders/mod.ds
// declarar `provider: "postgres"`, G1 só reconhece "sqlite" como adapter SQL
// real (codegen/sql_wiring.go) — "postgres" é decorativo neste exemplo — e não
// há Interface GRPC nem Telemetry em nenhum .ds do Shop.
func TestGenerateShopE2EGoModHasNoExternalRequire(t *testing.T) {
	out := generateShopE2EProject(t)
	content, err := os.ReadFile(filepath.Join(out, "go.mod"))
	if err != nil {
		t.Fatalf("não consegui ler go.mod: %v", err)
	}
	if strings.Contains(string(content), "require") {
		t.Fatalf("go.mod não deveria conter \"require\" (NFR-12 — zero dep externa, provider \"postgres\" é decorativo neste exemplo):\n%s", content)
	}
}

// TestGenerateShopE2ERegenTwoDirsByteIdentical prova a DoD §5.3/NFR-13 sobre
// o Shop: duas gerações INDEPENDENTES, cada uma no seu próprio diretório de
// saída, produzem exatamente os mesmos arquivos (mesmos caminhos, mesmos
// bytes) — inclusive a ordenação estável entre os dois services/cmd groups.
func TestGenerateShopE2ERegenTwoDirsByteIdentical(t *testing.T) {
	out1 := filepath.Join(t.TempDir(), "out1")
	out2 := filepath.Join(t.TempDir(), "out2")

	if _, err := GenerateProject(shopExampleDir, out1, codegen.Options{}); err != nil {
		t.Fatalf("1ª geração: erro inesperado: %v", err)
	}
	if _, err := GenerateProject(shopExampleDir, out2, codegen.Options{}); err != nil {
		t.Fatalf("2ª geração: erro inesperado: %v", err)
	}

	snap1 := snapshotDir(t, out1)
	snap2 := snapshotDir(t, out2)

	if len(snap1) != len(snap2) {
		t.Fatalf("número de arquivos difere entre gerações: %d vs %d\n1ª: %v\n2ª: %v", len(snap1), len(snap2), keys(snap1), keys(snap2))
	}
	for rel, content := range snap1 {
		got, ok := snap2[rel]
		if !ok {
			t.Fatalf("%q presente na 1ª geração e ausente na 2ª", rel)
		}
		if string(got) != string(content) {
			t.Fatalf("conteúdo de %q difere entre as duas gerações", rel)
		}
	}
}
