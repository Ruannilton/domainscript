package codegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/rtsrc"
	"domainscript/driver"
	"domainscript/program"
	"domainscript/types"
)

// decl_projection_test.go prova os critérios de conclusão da task E8.2
// (§design codegen 3.9, REQ-21.4) — EmitProjection não tem exemplo real no
// wallet (docs/examples/wallet não declara nenhuma Projection), então esta
// task é coberta por uma fixture SINTÉTICA: o exemplo canônico do spec §6.4
// (InvoiceWithHolderVW), com os dois Aggregates mínimos (Invoice/Wallet) que
// ela referencia — parseada e validada como qualquer programa real
// (driver.CheckProject, mesmo padrão de decl_aggregate_load_test.go/
// parseAggregateLoadFixture: EmitProjection exige um types.Model +
// symbols.SymbolTable RESOLVIDOS sobre um programa, então a fixture precisa
// de um projeto de verdade — driver.CheckSource não devolve a SymbolTable).

// projectionFixtureModDs declara o módulo e o banco que "gerencia" os dois
// Aggregates da fixture — mesma forma mínima de
// aggregateLoadFixtureModDs/docs/examples/wallet/mod.ds.
const projectionFixtureModDs = `Module Invoicing {
    Database InvoicingDb {
        provider: "postgres"
        manages: [Invoice, Wallet]
    }
}
`

// projectionFixtureSrc é o .ds sintético desta task: o exemplo canônico do
// spec §6.4 (Projection InvoiceWithHolderVW), precedido do mínimo de domínio
// que ele referencia — dois Aggregates com um único Handle "de abertura" cada
// (corpo vazio, sem emit/Apply). Nenhuma regra semântica exigiria Handle
// algum (checkAggregateAccess, sema/rules_domain.go, só itera Handlers
// declarados — zero Handlers não dispara nada), mas EmitAggregate (E6.1, o
// emissor usado pelo smoke desta task para o struct de state/Aggregate)
// sempre registra o import do runtime vendorado para a assinatura de Handle
// ("([]runtime.Event, error)"); sem NENHUM Handle esse import fica
// registrado e não usado, e emit.Emitter.Bytes recusa o arquivo (REQ-15.1) —
// por isso cada Aggregate precisa de pelo menos 1 Handle (com sua entrada de
// access correspondente, closed-by-default) para o smoke compile funcionar.
// "strategy StateStored" é só documentação — nem sema nem EmitAggregate
// (não usa Load<Nome> aqui) olham para Strategy.
//
// O front-end NÃO resolve nomes/tipos dentro de Sources/Map/RefreshOn (ver a
// doc de decl_projection.go) — por isso os dois Events do RefreshOn
// (InvoiceCreated/WalletUpdated) não precisam ser emitidos por nenhum Handle
// nem ter Apply correspondente: eles existem só para o comentário de doc de
// ComputeInvoiceWithHolderVW nomear algo real do domínio, mais perto do
// exemplo do spec do que uma string solta.
const projectionFixtureSrc = `
ValueObject InvoiceId(string) {
    Valid { value.length() > 0 }
}

ValueObject WalletId(string) {
    Valid { value.length() > 0 }
}

ValueObject HolderName(string) {
    Valid { value.length() > 0 }
}

ValueObject Money {
    amount decimal
    currency string

    Valid { amount >= 0 }
}

Event InvoiceCreated {
    id     InvoiceId
    amount Money
}

Event WalletUpdated {
    id     WalletId
    holder HolderName
}

Aggregate Invoice {
    strategy StateStored

    state {
        id     InvoiceId
        amount Money
    }

    access {
        Open requires caller.authenticated
    }

    Handle Open() {
    }
}

Aggregate Wallet {
    strategy StateStored

    state {
        id     WalletId
        holder HolderName
    }

    access {
        Open requires caller.authenticated
    }

    Handle Open() {
    }
}

Projection InvoiceWithHolderVW {
    source Invoice, Wallet
    map {
        invoiceId = Invoice.id
        amount    = Invoice.amount
        holder    = Wallet.holder
    }
    refreshOn [InvoiceCreated, WalletUpdated]
}
`

// findProjectionDecl acha o *ast.ProjectionDecl de nome name em qualquer
// arquivo do programa — espelha findAggregateDecl/findViewDecl/findQueryDecl.
func findProjectionDecl(t *testing.T, prog *program.Program, name string) *ast.ProjectionDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if p, ok := d.(*ast.ProjectionDecl); ok && p.Name == name {
				return p
			}
		}
	}
	t.Fatalf("Projection %q não encontrada na fixture — o exemplo mudou?", name)
	return nil
}

// parseProjectionFixture monta o projeto sintético em disco (mod.ds +
// domain.ds) e o resolve via driver.CheckProject — devolve o Program, os dois
// AggregateDecl e o ProjectionDecl.
func parseProjectionFixture(t *testing.T) (prog *program.Program, invoice, wallet *ast.AggregateDecl, proj *ast.ProjectionDecl) {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    projectionFixtureModDs,
		"domain.ds": projectionFixtureSrc,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de Projection (E8.2) tem diagnósticos de erro:\n%s", bag.Render())
	}
	invoice = findAggregateDecl(t, prog, "Invoice")
	wallet = findAggregateDecl(t, prog, "Wallet")
	proj = findProjectionDecl(t, prog, "InvoiceWithHolderVW")
	return
}

// emitProjectionFixture gera o Go da Projection InvoiceWithHolderVW sobre a
// fixture sintética.
func emitProjectionFixture(t *testing.T) []byte {
	t.Helper()
	prog, _, _, proj := parseProjectionFixture(t)
	model := types.NewModel(prog.Symbols)
	reg := walletVOOperatorRegistryFromProgram(prog)

	got, err := codegen.EmitProjection("invoicing", proj, model, prog.Symbols, "Invoicing", reg)
	if err != nil {
		t.Fatalf("EmitProjection: erro inesperado: %v", err)
	}
	return got
}

// --- Golden + determinismo -------------------------------------------------

// TestEmitProjectionGolden gera o Go de InvoiceWithHolderVW e compara byte a
// byte com o artefato versionado: confirma o struct materializado (3 campos,
// tipo inferido do Value) e a função ComputeInvoiceWithHolderVW (parâmetros
// invoice */Invoice, wallet */Wallet — nomes distintos dos tipos — e o acesso
// aos campos via ".state.Campo", a mesma convenção de self/state de Handle).
func TestEmitProjectionGolden(t *testing.T) {
	got := emitProjectionFixture(t)
	for _, want := range []string{
		"type InvoiceWithHolderVW struct",
		"InvoiceId InvoiceId  `json:\"invoiceId\"`",
		"Amount    Money      `json:\"amount\"`",
		"Holder    HolderName `json:\"holder\"`",
		"func ComputeInvoiceWithHolderVW(invoice *Invoice, wallet *Wallet) InvoiceWithHolderVW",
		"return InvoiceWithHolderVW{InvoiceId: invoice.state.Id, Amount: invoice.state.Amount, Holder: wallet.state.Holder}",
	} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("esperava a presença de %q no Go gerado, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "projection_invoice_with_holder.go.golden"), got)
}

// TestEmitProjectionDeterministic prova NFR-13.
func TestEmitProjectionDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitProjectionFixture(t)
	})
}

// --- Smoke compile + comportamento -----------------------------------------

// projectionSmokeFiles monta os arquivos mínimos para compilar
// InvoiceWithHolderVW junto do restante da fixture: go.mod + runtime real +
// os 4 ValueObjects + os dois Aggregates (EmitAggregate, E6.1 — só o struct
// de state/Aggregate; esta task não precisa de Load<Nome>, ComputeX recebe os
// Aggregates JÁ CARREGADOS) + o Go da Projection, todos no pacote
// "invoicing".
func projectionSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog, invoice, wallet, _ := parseProjectionFixture(t)
	model := types.NewModel(prog.Symbols)
	reg := walletVOOperatorRegistryFromProgram(prog)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	vos := map[string]string{
		"InvoiceId":  "invoice_id.go",
		"WalletId":   "wallet_id.go",
		"HolderName": "holder_name.go",
		"Money":      "money.go",
	}
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			vo, ok := d.(*ast.ValueObjectDecl)
			if !ok {
				continue
			}
			file, want := vos[vo.Name]
			if !want {
				continue
			}
			got, err := codegen.EmitValueObject("invoicing", vo)
			if err != nil {
				t.Fatalf("EmitValueObject(%s): erro inesperado: %v", vo.Name, err)
			}
			files[filepath.Join("invoicing", file)] = got
		}
	}

	invoiceGo, err := codegen.EmitAggregate("invoicing", invoice, model, prog.Symbols, "Invoicing", reg)
	if err != nil {
		t.Fatalf("EmitAggregate(Invoice): erro inesperado: %v", err)
	}
	files[filepath.Join("invoicing", "aggregate_invoice.go")] = invoiceGo

	walletGo, err := codegen.EmitAggregate("invoicing", wallet, model, prog.Symbols, "Invoicing", reg)
	if err != nil {
		t.Fatalf("EmitAggregate(Wallet): erro inesperado: %v", err)
	}
	files[filepath.Join("invoicing", "aggregate_wallet.go")] = walletGo

	files[filepath.Join("invoicing", "projection_invoice_with_holder.go")] = emitProjectionFixture(t)
	return files
}

// TestEmitProjectionSmokeCompile prova NFR-14: o Go de InvoiceWithHolderVW,
// junto dos Aggregates/VOs que referencia e do runtime vendorado real,
// compila e passa go vet num projeto isolado.
func TestEmitProjectionSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, projectionSmokeFiles(t))
}

// projectionBehaviorTest roda dentro do projeto isolado gerado no smoke e
// prova, sobre o Go de fato gerado (não uma reimplementação): constrói um
// *Invoice e um *Wallet à mão (populando seus states não-exportados via
// struct literal — Compute recebe Aggregates JÁ CARREGADOS, sem passar por
// Load), chama ComputeInvoiceWithHolderVW e confere que os 3 campos mapeados
// batem com os valores de origem.
const projectionBehaviorTest = `package invoicing

import (
	"testing"

	"domainscript/generated/runtime"
)

func TestComputeInvoiceWithHolderVWMapsFieldsFromSources(t *testing.T) {
	invoiceID, err := NewInvoiceId("INV-1")
	if err != nil {
		t.Fatal(err)
	}
	walletID, err := NewWalletId("W-1")
	if err != nil {
		t.Fatal(err)
	}
	holder, err := NewHolderName("Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	// Money embute runtime.Decimal, que NÃO é comparável com == (big.Int
	// embute uma slice) — comparar Currency direto e Amount via Cmp.
	amount, err := NewMoney(runtime.NewDecimalFromInt(10), "BRL")
	if err != nil {
		t.Fatal(err)
	}

	inv := &Invoice{}
	inv.state = invoiceState{Id: invoiceID, Amount: amount}

	w := &Wallet{}
	w.state = walletState{Id: walletID, Holder: holder}

	got := ComputeInvoiceWithHolderVW(inv, w)

	if got.InvoiceId != invoiceID {
		t.Fatalf("InvoiceId = %v, want %v", got.InvoiceId, invoiceID)
	}
	if got.Amount.Currency != amount.Currency || got.Amount.Amount.Cmp(amount.Amount) != 0 {
		t.Fatalf("Amount = %+v, want %+v", got.Amount, amount)
	}
	if got.Holder != holder {
		t.Fatalf("Holder = %v, want %v", got.Holder, holder)
	}
}
`

// TestEmitProjectionBehavior prova NFR-15 sobre o Go de fato gerado: escreve
// os mesmos arquivos do smoke mais o teste comportamental acima num diretório
// isolado e roda `go test ./...` de verdade.
func TestEmitProjectionBehavior(t *testing.T) {
	files := projectionSmokeFiles(t)
	files[filepath.Join("invoicing", "behavior_test.go")] = []byte(projectionBehaviorTest)

	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("não consegui criar %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("não consegui escrever %q: %v", path, err)
		}
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("`go test ./...` falhou em %q: %v\n%s", dir, err, out)
	}
}
