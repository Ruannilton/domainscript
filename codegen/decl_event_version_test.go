package codegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/rtsrc"
	"domainscript/driver"
)

// decl_event_version_test.go cobre E4.3 (REQ-18.4/18.5/18.6, spec §4.3/§4.4):
// Field.Default → UnmarshalJSON customizado, UpcastDecl → EmitUpcast, e
// Field.Redactable → Redact(). Nenhum Event do wallet usa nenhuma das 3
// features (E4.2's doc.go já observa isso), então esta task é coberta por uma
// fixture SINTÉTICA — um .ds pequeno, parseado e validado como qualquer
// programa real (driver.CheckSource), não uma AST construída à mão: mais
// direto e prova que o front-end de fato aceita default/redactable/Upcast.

// versioningFixtureSrc é o .ds sintético desta task: 4 ValueObjects
// (EntityId/Channel/HolderName servem só para satisfazer a Regra de Ouro nos
// campos de Event — REQ-5.1 — e Money é o composto do exemplo do spec §4.3),
// e os 3 construtos sob teste:
//   - DepositPerformed.channel tem Default (Channel("unknown"), §4.3).
//   - TransferSent/Upcast é o exemplo literal do spec §4.3 (fee computado a
//     partir de event.amount.currency).
//   - WalletCreated.holder é redactable (§4.4).
const versioningFixtureSrc = `
ValueObject EntityId(string) {
    Valid { value.length() > 0 }
}

ValueObject Channel(string) {
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

Event DepositPerformed {
    id EntityId
    amount Money
    channel Channel = Channel("unknown")
}

Event TransferSent {
    id EntityId
    amount Money
    fee Money
}

Upcast TransferSent v1 -> v2 {
    fee = Money(amount: 0, currency: event.amount.currency)
}

Event WalletCreated {
    id EntityId
    holder HolderName redactable
}
`

// versioningFixture indexa os decls do versioningFixtureSrc por construto —
// espelha parseWalletVOs/parseWalletEvents (decl_value_test.go/
// decl_event_test.go): a fixture é o programa de verdade, parseado uma vez.
type versioningFixture struct {
	entityID, channel, holderName, money          *ast.ValueObjectDecl
	depositPerformed, transferSent, walletCreated *ast.EventDecl
	upcast                                        *ast.UpcastDecl
}

func parseVersioningFixture(t *testing.T) versioningFixture {
	t.Helper()
	file, bag := driver.CheckSource(versioningFixtureSrc)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética de versionamento tem diagnósticos de erro:\n%s", bag.Render())
	}

	var fx versioningFixture
	for _, d := range file.Decls {
		switch n := d.(type) {
		case *ast.ValueObjectDecl:
			switch n.Name {
			case "EntityId":
				fx.entityID = n
			case "Channel":
				fx.channel = n
			case "HolderName":
				fx.holderName = n
			case "Money":
				fx.money = n
			}
		case *ast.EventDecl:
			switch n.Name {
			case "DepositPerformed":
				fx.depositPerformed = n
			case "TransferSent":
				fx.transferSent = n
			case "WalletCreated":
				fx.walletCreated = n
			}
		case *ast.UpcastDecl:
			fx.upcast = n
		}
	}
	if fx.entityID == nil || fx.channel == nil || fx.holderName == nil || fx.money == nil {
		t.Fatal("fixture: ValueObjects ausentes (EntityId/Channel/HolderName/Money)")
	}
	if fx.depositPerformed == nil || fx.transferSent == nil || fx.walletCreated == nil {
		t.Fatal("fixture: Events ausentes (DepositPerformed/TransferSent/WalletCreated)")
	}
	if fx.upcast == nil {
		t.Fatal("fixture: Upcast ausente (TransferSent v1->v2)")
	}
	return fx
}

// --- Parte 1: Field.Default → UnmarshalJSON -----------------------------

// TestEmitEventDefaultGolden gera o Go de DepositPerformed (channel tem
// Default) e compara byte a byte com o artefato versionado: prova que
// emitEventUnmarshalJSON só entra em cena quando há Default, e produz a forma
// "desserializa via alias, depois checa presença via RawMessage" descrita na
// task.
func TestEmitEventDefaultGolden(t *testing.T) {
	fx := parseVersioningFixture(t)
	got, err := codegen.EmitEvent("versioning", fx.depositPerformed)
	if err != nil {
		t.Fatalf("EmitEvent(DepositPerformed): erro inesperado: %v", err)
	}
	gentest.Golden(t, filepath.Join("testdata", "event_deposit_performed_default.go.golden"), got)
}

// TestEmitEventDefaultDeterministic prova NFR-13 sobre o UnmarshalJSON
// gerado: duas gerações do mesmo Event produzem bytes idênticos.
func TestEmitEventDefaultDeterministic(t *testing.T) {
	fx := parseVersioningFixture(t)
	gentest.Deterministic(t, func() []byte {
		got, err := codegen.EmitEvent("versioning", fx.depositPerformed)
		if err != nil {
			t.Fatalf("EmitEvent(DepositPerformed): erro inesperado: %v", err)
		}
		return got
	})
}

// --- Parte 2: UpcastDecl → EmitUpcast ------------------------------------

// TestEmitUpcastDeterministic prova NFR-13 sobre EmitUpcast: duas gerações do
// mesmo Upcast produzem bytes idênticos.
func TestEmitUpcastDeterministic(t *testing.T) {
	fx := parseVersioningFixture(t)
	gentest.Deterministic(t, func() []byte {
		got, err := codegen.EmitUpcast("versioning", fx.upcast, fx.transferSent.Fields, []*ast.ValueObjectDecl{fx.money})
		if err != nil {
			t.Fatalf("EmitUpcast: erro inesperado: %v", err)
		}
		return got
	})
}

// TestEmitUpcastFuncNameCapitalizesVersion prova a convenção de nome de
// EmitUpcast/UpcastFuncName (§design catálogo desta task): "v1"/"v2" viram
// "V1"/"V2" — capitalização sensata de um identificador que já começa
// minúsculo por convenção de versão.
func TestEmitUpcastFuncNameCapitalizesVersion(t *testing.T) {
	fx := parseVersioningFixture(t)
	got := codegen.UpcastFuncName(fx.upcast)
	want := "UpcastTransferSentV1ToV2"
	if got != want {
		t.Fatalf("UpcastFuncName = %q, want %q", got, want)
	}
}

// --- Parte 3: Field.Redactable → Redact() --------------------------------

// TestEmitEventRedactDeterministic prova NFR-13 sobre o Redact() gerado: duas
// gerações do mesmo Event produzem bytes idênticos.
func TestEmitEventRedactDeterministic(t *testing.T) {
	fx := parseVersioningFixture(t)
	gentest.Deterministic(t, func() []byte {
		got, err := codegen.EmitEvent("versioning", fx.walletCreated)
		if err != nil {
			t.Fatalf("EmitEvent(WalletCreated): erro inesperado: %v", err)
		}
		return got
	})
}

// --- Smoke compile + comportamento (as 3 partes juntas) ------------------

// versioningSmokeFiles monta o conjunto comum de arquivos usado pelo smoke
// compile e pelo teste de comportamento: go.mod (smokeGoMod, definido em
// decl_value_test.go), o runtime vendorado real (rtsrc), os 4 ValueObjects da
// fixture e os 3 Events + a função de Upcast, todos no pacote "versioning".
func versioningSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	fx := parseVersioningFixture(t)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	voFiles := []struct {
		decl *ast.ValueObjectDecl
		file string
	}{
		{fx.entityID, "entity_id.go"},
		{fx.channel, "channel.go"},
		{fx.holderName, "holder_name.go"},
		{fx.money, "money.go"},
	}
	for _, spec := range voFiles {
		got, err := codegen.EmitValueObject("versioning", spec.decl)
		if err != nil {
			t.Fatalf("EmitValueObject(%s): erro inesperado: %v", spec.decl.Name, err)
		}
		files[filepath.Join("versioning", spec.file)] = got
	}

	eventsGo, err := codegen.EmitEvents("versioning", []*ast.EventDecl{fx.depositPerformed, fx.transferSent, fx.walletCreated})
	if err != nil {
		t.Fatalf("EmitEvents: erro inesperado: %v", err)
	}
	files[filepath.Join("versioning", "events.go")] = eventsGo

	upcastGo, err := codegen.EmitUpcast("versioning", fx.upcast, fx.transferSent.Fields, []*ast.ValueObjectDecl{fx.money})
	if err != nil {
		t.Fatalf("EmitUpcast: erro inesperado: %v", err)
	}
	files[filepath.Join("versioning", "upcast_transfer_sent.go")] = upcastGo

	return files
}

// TestEmitEventVersioningSmokeCompile prova NFR-14: o Go gerado das 3 partes
// desta task, junto do runtime vendorado real, compila e passa go vet num
// projeto isolado (item 5 da task: "smoke compile de tudo junto").
func TestEmitEventVersioningSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, versioningSmokeFiles(t))
}

// versioningBehaviorTest roda dentro do projeto isolado gerado no smoke e
// prova, sobre o Go de fato gerado (não uma reimplementação), o
// comportamento pedido pela task (itens 1/2/3):
//   - Default: chave ausente no JSON bruto aplica o default; chave presente
//     (mesmo com outro valor) NÃO é sobrescrita pelo default.
//   - Upcast: aplicado sobre um evento "v1" (Fee zero-value, como se nunca
//     tivesse sido desserializado), computa Fee corretamente a partir de
//     Amount.Currency.
//   - Redact: zera o campo redactable (valor zero do tipo Go do campo) e o
//     evento ainda serializa/desserializa em JSON sem erro (round-trip não
//     quebra).
const versioningBehaviorTest = `package versioning

import (
	"encoding/json"
	"testing"

	"domainscript/generated/runtime"
)

func TestEventDefaultAppliedWhenKeyAbsent(t *testing.T) {
	data := []byte(` + "`" + `{"id":"E1","amount":{"amount":"10.0000","currency":"BRL"}}` + "`" + `)
	var ev DepositPerformed
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if string(ev.Channel) != "unknown" {
		t.Fatalf("Channel = %q, want %q (Field.Default deveria ter sido aplicado)", ev.Channel, "unknown")
	}
}

func TestEventDefaultNotAppliedWhenKeyPresent(t *testing.T) {
	data := []byte(` + "`" + `{"id":"E1","amount":{"amount":"10.0000","currency":"BRL"},"channel":"sms"}` + "`" + `)
	var ev DepositPerformed
	if err := json.Unmarshal(data, &ev); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if string(ev.Channel) != "sms" {
		t.Fatalf("Channel = %q, want %q (valor presente no JSON não deveria ser sobrescrito pelo default)", ev.Channel, "sms")
	}
}

func TestUpcastTransferSentV1ToV2ComputesFeeFromAmountCurrency(t *testing.T) {
	amount, err := runtime.ParseDecimal("100.00")
	if err != nil {
		t.Fatal(err)
	}
	money, err := NewMoney(amount, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	// ev representa um evento "v1" desserializado: Fee nunca existiu, então
	// carrega o zero-value de Money.
	ev := &TransferSent{Id: EntityId("E1"), Amount: money}

	got, err := UpcastTransferSentV1ToV2(ev)
	if err != nil {
		t.Fatalf("UpcastTransferSentV1ToV2: %v", err)
	}
	if got.Fee.Currency != "BRL" {
		t.Fatalf("Fee.Currency = %q, want %q (event.amount.currency)", got.Fee.Currency, "BRL")
	}
	zero, err := runtime.ParseDecimal("0")
	if err != nil {
		t.Fatal(err)
	}
	if got.Fee.Amount.Cmp(zero) != 0 {
		t.Fatalf("Fee.Amount = %s, want 0 (amount: 0 do corpo do Upcast)", got.Fee.Amount)
	}
}

func TestWalletCreatedRedactZeroesFieldWithoutBreakingRoundTrip(t *testing.T) {
	holder, err := NewHolderName("Ada Lovelace")
	if err != nil {
		t.Fatal(err)
	}
	ev := &WalletCreated{Id: EntityId("E1"), Holder: holder}

	ev.Redact()

	if string(ev.Holder) != "" {
		t.Fatalf("Holder = %q após Redact, want valor zero (\"\")", ev.Holder)
	}

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("Marshal após Redact não deveria falhar: %v", err)
	}
	var got WalletCreated
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal após Redact não deveria falhar (round-trip não pode quebrar): %v", err)
	}
	if got.Id != ev.Id {
		t.Fatalf("Id não sobreviveu ao round-trip pós-Redact: got %v, want %v", got.Id, ev.Id)
	}
	if string(got.Holder) != "" {
		t.Fatalf("Holder pós round-trip = %q, want valor zero preservado", got.Holder)
	}
}
`

// TestEmitEventVersioningBehavior prova NFR-15 sobre o Go de fato gerado:
// escreve os mesmos arquivos do smoke mais o teste comportamental acima num
// diretório isolado e roda `go test ./...` de verdade.
func TestEmitEventVersioningBehavior(t *testing.T) {
	files := versioningSmokeFiles(t)
	files[filepath.Join("versioning", "behavior_test.go")] = []byte(versioningBehaviorTest)

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
