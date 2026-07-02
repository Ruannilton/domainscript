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

// parseWalletEvents parseia o domain.ds real do wallet e devolve seus
// EventDecl na ordem de declaração do arquivo (WalletCreated,
// DepositPerformed, WithdrawalPerformed) — as fixtures desta task são o
// programa de verdade, não ASTs sintéticas.
func parseWalletEvents(t *testing.T) []*ast.EventDecl {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "docs", "examples", "wallet", "domain.ds"))
	if err != nil {
		t.Fatalf("não consegui ler o domain.ds do wallet: %v", err)
	}
	file, bag := driver.CheckSource(string(src))
	if bag.HasErrors() {
		t.Fatalf("wallet/domain.ds tem diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}

	var events []*ast.EventDecl
	for _, d := range file.Decls {
		if e, ok := d.(*ast.EventDecl); ok {
			events = append(events, e)
		}
	}
	return events
}

// TestEmitEventsGolden gera o Go dos 3 Events reais do wallet num único
// arquivo (EmitEvents, o formato de lote que E9.1 vai concatenar em
// events.go) e compara byte a byte com o artefato versionado (REQ-18.2/18.3).
func TestEmitEventsGolden(t *testing.T) {
	decls := parseWalletEvents(t)
	if len(decls) != 3 {
		t.Fatalf("esperava 3 Events em wallet/domain.ds, achei %d", len(decls))
	}

	got, err := codegen.EmitEvents("wallet", decls)
	if err != nil {
		t.Fatalf("EmitEvents: erro inesperado: %v", err)
	}
	gentest.Golden(t, filepath.Join("testdata", "events_wallet.go.golden"), got)
}

// TestEmitEventGoldenSingle gera o Go de um único Event (WalletCreated) via
// EmitEvent e compara com um segundo artefato versionado — prova que a forma
// "um de cada vez" também é suportada e estável, com um registry de 1
// entrada só (mesmo contrato de EmitEvents).
func TestEmitEventGoldenSingle(t *testing.T) {
	decls := parseWalletEvents(t)
	var created *ast.EventDecl
	for _, d := range decls {
		if d.Name == "WalletCreated" {
			created = d
		}
	}
	if created == nil {
		t.Fatal("Event WalletCreated não encontrado em wallet/domain.ds")
	}

	got, err := codegen.EmitEvent("wallet", created)
	if err != nil {
		t.Fatalf("EmitEvent(WalletCreated): erro inesperado: %v", err)
	}
	gentest.Golden(t, filepath.Join("testdata", "event_wallet_created.go.golden"), got)
}

// TestEmitEventsDeterministic prova NFR-13: gerar os mesmos 3 Events duas
// vezes produz bytes idênticos.
func TestEmitEventsDeterministic(t *testing.T) {
	decls := parseWalletEvents(t)
	gentest.Deterministic(t, func() []byte {
		got, err := codegen.EmitEvents("wallet", decls)
		if err != nil {
			t.Fatalf("EmitEvents: erro inesperado: %v", err)
		}
		return got
	})
}

// eventsSmokeFiles monta o conjunto comum de arquivos usado pelo smoke
// compile e pelo teste de comportamento: go.mod, o runtime vendorado real
// (rtsrc), os VOs que os 3 Events do wallet referenciam (WalletId,
// HolderName, Money, TransactionDescription — via EmitValueObject, já
// existe), os 4 Errors reais do wallet (Money.Add/Sub referenciam
// ErrCurrencyMismatch/ErrNegativeResult — via EmitErrors, já existe) e os 3
// Events em lote.
func eventsSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	vos := parseWalletVOs(t)
	errs := parseWalletErrors(t)
	events := parseWalletEvents(t)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	voFiles := []struct{ name, file string }{
		{"WalletId", "wallet_id.go"},
		{"HolderName", "holder_name.go"},
		{"Money", "money.go"},
		{"TransactionDescription", "transaction_description.go"},
	}
	for _, spec := range voFiles {
		decl, ok := vos[spec.name]
		if !ok {
			t.Fatalf("ValueObject %s não encontrado em wallet/domain.ds", spec.name)
		}
		got, err := codegen.EmitValueObject("wallet", decl)
		if err != nil {
			t.Fatalf("EmitValueObject(%s): erro inesperado: %v", spec.name, err)
		}
		files[filepath.Join("wallet", spec.file)] = got
	}

	errsGo, err := codegen.EmitErrors("wallet", errs)
	if err != nil {
		t.Fatalf("EmitErrors: erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "errors.go")] = errsGo

	eventsGo, err := codegen.EmitEvents("wallet", events)
	if err != nil {
		t.Fatalf("EmitEvents: erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "events.go")] = eventsGo

	return files
}

// TestEmitEventsSmokeCompile prova NFR-14: o Go gerado dos 3 Events do
// wallet, junto dos VOs/Errors que referenciam e do runtime vendorado real,
// compila e passa go vet num projeto isolado.
func TestEmitEventsSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, eventsSmokeFiles(t))
}

// walletEventsBehaviorTest roda dentro do projeto isolado gerado no smoke e
// prova, sobre o Go de fato gerado (não uma reimplementação): (a) EventType()
// devolve o nome estável; (b) round-trip encoding/json preserva os campos
// (Id/Amount/Description); (c) a ordem dos campos no JSON é a ordem declarada
// (id, amount, description — determinismo, NFR-13); (d) SetMeta promove
// AggregateID/Sequence/Timestamp através do embed runtime.EventMeta; (e) o
// registry constrói o tipo dinâmico correto (NFR-15).
const walletEventsBehaviorTest = `package wallet

import (
	"strings"
	"testing"
	"time"

	"encoding/json"

	"domainscript/generated/runtime"
)

func newTestDeposit(t *testing.T) *DepositPerformed {
	t.Helper()
	amount, err := runtime.ParseDecimal("10.00")
	if err != nil {
		t.Fatal(err)
	}
	money, err := NewMoney(amount, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}
	return &DepositPerformed{
		Id:          WalletId("W1"),
		Amount:      money,
		Description: desc,
	}
}

func TestDepositPerformedImplementsRuntimeEvent(t *testing.T) {
	var _ runtime.Event = &DepositPerformed{}
}

func TestDepositPerformedEventTypeIsStableName(t *testing.T) {
	ev := &DepositPerformed{}
	if ev.EventType() != "DepositPerformed" {
		t.Fatalf("EventType() = %q, want %q", ev.EventType(), "DepositPerformed")
	}
}

func TestDepositPerformedJSONRoundTrip(t *testing.T) {
	ev := newTestDeposit(t)

	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got DepositPerformed
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Id != ev.Id {
		t.Fatalf("Id não sobreviveu ao round-trip: got %v, want %v", got.Id, ev.Id)
	}
	if got.Amount.Currency != ev.Amount.Currency {
		t.Fatalf("Amount.Currency não sobreviveu ao round-trip: got %v, want %v", got.Amount.Currency, ev.Amount.Currency)
	}
	if got.Amount.Amount.Cmp(ev.Amount.Amount) != 0 {
		t.Fatalf("Amount.Amount não sobreviveu ao round-trip: got %s, want %s", got.Amount.Amount, ev.Amount.Amount)
	}
	if got.Description != ev.Description {
		t.Fatalf("Description não sobreviveu ao round-trip: got %v, want %v", got.Description, ev.Description)
	}
}

func TestDepositPerformedJSONFieldOrderIsDeclarationOrder(t *testing.T) {
	ev := newTestDeposit(t)
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	idxID := strings.Index(s, "\"id\"")
	idxAmount := strings.Index(s, "\"amount\"")
	idxDescription := strings.Index(s, "\"description\"")
	if idxID < 0 || idxAmount < 0 || idxDescription < 0 {
		t.Fatalf("campos esperados ausentes no JSON: %s", s)
	}
	if !(idxID < idxAmount && idxAmount < idxDescription) {
		t.Fatalf("ordem dos campos não é id, amount, description: %s", s)
	}
}

func TestSetMetaPromotesEmbeddedEventMetaFields(t *testing.T) {
	ev := &DepositPerformed{}
	ts := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	ev.SetMeta(runtime.EventMeta{AggregateID: "W1", Sequence: 3, Timestamp: ts})

	if ev.AggregateID != "W1" {
		t.Fatalf("AggregateID = %q, want %q", ev.AggregateID, "W1")
	}
	if ev.Sequence != 3 {
		t.Fatalf("Sequence = %d, want %d", ev.Sequence, 3)
	}
	if !ev.Timestamp.Equal(ts) {
		t.Fatalf("Timestamp = %v, want %v", ev.Timestamp, ts)
	}
}

func TestEventRegistryConstructsCorrectDynamicType(t *testing.T) {
	ctor, ok := eventRegistry["DepositPerformed"]
	if !ok {
		t.Fatal("eventRegistry não contém DepositPerformed")
	}
	ev := ctor()
	if _, ok := ev.(*DepositPerformed); !ok {
		t.Fatalf("tipo dinâmico incorreto: got %T, want *DepositPerformed", ev)
	}
}

func TestEventRegistryContainsAllThreeWalletEvents(t *testing.T) {
	for _, name := range []string{"WalletCreated", "DepositPerformed", "WithdrawalPerformed"} {
		if _, ok := eventRegistry[name]; !ok {
			t.Fatalf("eventRegistry não contém %q", name)
		}
	}
}
`

// TestEmitEventsBehavior prova NFR-15 sobre o Go de fato gerado: escreve os
// mesmos arquivos do smoke mais um teste Go comportamental num diretório
// isolado e roda `go test ./...` de verdade.
func TestEmitEventsBehavior(t *testing.T) {
	files := eventsSmokeFiles(t)
	files[filepath.Join("wallet", "behavior_test.go")] = []byte(walletEventsBehaviorTest)

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

// TestEmitEventRejectsUnmappableFieldType prova que um EventDecl com um
// campo cujo TypeRef não é resolvível (goname.GoFieldType falha) devolve um
// erro de geração claro, nunca panic (o front-end garante tipos válidos, mas
// o gerador se defende de AST construída à mão/sintética).
func TestEmitEventRejectsUnmappableFieldType(t *testing.T) {
	badField := ast.NewField("bad", nil, false, false, nil, ast.Span{})
	bad := ast.NewEventDecl("Bad", false, []*ast.Field{badField}, ast.Span{})
	if _, err := codegen.EmitEvent("wallet", bad); err == nil {
		t.Fatal("esperava erro de geração para campo com TypeRef nulo")
	}
}
