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

// smokeGoMod é o go.mod do projeto isolado usado pelo smoke compile e pelo
// teste de comportamento: module "domainscript/generated" casa com
// codegen.RuntimeImportPath ("domainscript/generated/runtime"), sem require
// externo (o núcleo depende só de stdlib — REQ-16.2).
const smokeGoMod = "module domainscript/generated\n\ngo 1.22\n"

// transactionTypeStub substitui, só para este smoke/teste, o Go que o
// gerador de Enum (E3.3, ainda não implementado) produziria para "Enum
// TransactionType : string { ... }". StatementEntry referencia
// TransactionType por identidade (codegen.GoFieldType) — precisa do tipo
// existir no pacote para compilar.
const transactionTypeStub = `package wallet

// TransactionType é um placeholder mínimo até o codegen de Enum (E3.3) gerar
// o tipo real. Necessário só para o smoke compile: StatementEntry referencia
// TransactionType.
type TransactionType string
`

// walletVOBehaviorTest é o teste Go comportamental do E3.1 (padrão de
// codegen/rtsrc/runtime_test.go.txt): roda dentro do projeto isolado gerado
// no smoke e prova, sobre o Go de fato gerado (não uma reimplementação), que
// Valid tem efeito em runtime — a Regra de Ouro sobrevivendo à geração
// (NFR-15).
const walletVOBehaviorTest = `package wallet

import (
	"errors"
	"testing"

	"domainscript/generated/runtime"
)

func TestNewWalletIdRejectsEmpty(t *testing.T) {
	if _, err := NewWalletId(""); err == nil {
		t.Fatal("esperava erro para WalletId vazio (value.length() > 0)")
	}
}

func TestNewWalletIdAcceptsNonEmpty(t *testing.T) {
	id, err := NewWalletId("W1")
	if err != nil {
		t.Fatalf("NewWalletId(%q) não deveria falhar: %v", "W1", err)
	}
	if string(id) != "W1" {
		t.Fatalf("valor embrulhado incorreto: got %q, want %q", id, "W1")
	}
}

func TestNewActiveStatusFalseIsValid(t *testing.T) {
	// Valid { ok } é o sentinela "validação sempre passa" (§design 3.3):
	// mesmo NewActiveStatus(false) não falha.
	if _, err := NewActiveStatus(false); err != nil {
		t.Fatalf("NewActiveStatus(false) não deveria falhar: %v", err)
	}
	if _, err := NewActiveStatus(true); err != nil {
		t.Fatalf("NewActiveStatus(true) não deveria falhar: %v", err)
	}
}

func TestNewMoneyRejectsNegativeAmount(t *testing.T) {
	neg, err := runtime.ParseDecimal("-1.00")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewMoney(neg, "BRL"); err == nil {
		t.Fatal("esperava erro para amount negativo (amount >= 0)")
	}
}

func TestNewMoneyAcceptsNonNegativeAmount(t *testing.T) {
	amount, err := runtime.ParseDecimal("10.00")
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMoney(amount, "BRL")
	if err != nil {
		t.Fatalf("NewMoney com amount >= 0 não deveria falhar: %v", err)
	}
	if m.Currency != "BRL" {
		t.Fatalf("Currency incorreto: got %q, want %q", m.Currency, "BRL")
	}
	if m.Amount.Cmp(amount) != 0 {
		t.Fatalf("Amount incorreto: got %s, want %s", m.Amount, amount)
	}
}

func TestNewMoneyZeroAmountIsValid(t *testing.T) {
	zero, err := runtime.ParseDecimal("0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewMoney(zero, "BRL"); err != nil {
		t.Fatalf("NewMoney com amount == 0 não deveria falhar (amount >= 0): %v", err)
	}
}

func TestNewStatementEntryOkSentinelIsAlwaysValid(t *testing.T) {
	amount, err := runtime.ParseDecimal("5.00")
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
	if _, err := NewStatementEntry(TransactionType("DEPOSIT"), money, desc); err != nil {
		t.Fatalf("NewStatementEntry (Valid { ok }) não deveria falhar: %v", err)
	}
}

func TestNewWalletIdBusinessErrorCodeIsStable(t *testing.T) {
	_, err := NewWalletId("")
	var be runtime.BusinessError
	if !errors.As(err, &be) {
		t.Fatalf("esperava um runtime.BusinessError, got %T", err)
	}
	if be.Code != "InvalidWalletId" {
		t.Fatalf("Code = %q, want %q", be.Code, "InvalidWalletId")
	}
}
`

// parseWalletVOs parseia o domain.ds real do wallet (docs/examples/wallet) e
// indexa seus ValueObjectDecl por nome — as fixtures desta task são o
// programa de verdade, não ASTs sintéticas.
func parseWalletVOs(t *testing.T) map[string]*ast.ValueObjectDecl {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "docs", "examples", "wallet", "domain.ds"))
	if err != nil {
		t.Fatalf("não consegui ler o domain.ds do wallet: %v", err)
	}
	file, bag := driver.CheckSource(string(src))
	if bag.HasErrors() {
		t.Fatalf("wallet/domain.ds tem diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}

	vos := make(map[string]*ast.ValueObjectDecl)
	for _, d := range file.Decls {
		if vo, ok := d.(*ast.ValueObjectDecl); ok {
			vos[vo.Name] = vo
		}
	}
	return vos
}

// TestEmitValueObjectGolden gera o Go de cada um dos 4 VOs reais do wallet e
// compara byte a byte com o artefato versionado (REQ-17.1/17.2).
func TestEmitValueObjectGolden(t *testing.T) {
	vos := parseWalletVOs(t)

	cases := []struct {
		name   string
		golden string
	}{
		{"WalletId", filepath.Join("testdata", "value_object_wallet_id.go.golden")},
		{"ActiveStatus", filepath.Join("testdata", "value_object_active_status.go.golden")},
		{"Money", filepath.Join("testdata", "value_object_money.go.golden")},
		{"StatementEntry", filepath.Join("testdata", "value_object_statement_entry.go.golden")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			decl, ok := vos[c.name]
			if !ok {
				t.Fatalf("ValueObject %s não encontrado em wallet/domain.ds", c.name)
			}
			got, err := codegen.EmitValueObject("wallet", decl)
			if err != nil {
				t.Fatalf("EmitValueObject(%s): erro inesperado: %v", c.name, err)
			}
			gentest.Golden(t, c.golden, got)
		})
	}
}

// TestEmitValueObjectDeterministic prova NFR-13 sobre o emissor de VO: gerar
// o mesmo Money duas vezes produz bytes idênticos.
func TestEmitValueObjectDeterministic(t *testing.T) {
	vos := parseWalletVOs(t)
	decl, ok := vos["Money"]
	if !ok {
		t.Fatal("ValueObject Money não encontrado em wallet/domain.ds")
	}
	gentest.Deterministic(t, func() []byte {
		got, err := codegen.EmitValueObject("wallet", decl)
		if err != nil {
			t.Fatalf("EmitValueObject(Money): erro inesperado: %v", err)
		}
		return got
	})
}

// voSmokeFiles monta o conjunto comum de arquivos usado pelo smoke compile e
// pelo teste de comportamento: go.mod, o runtime vendorado real (rtsrc,
// como no padrão de E2.1) e os 5 VOs do wallet necessários para o pacote
// compilar (os 4 da task + TransactionDescription, referenciada por
// StatementEntry — gerada pelo mesmo EmitValueObject, não um stub) mais o
// stub de TransactionType (Enum, E3.3, ainda não implementado).
func voSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	vos := parseWalletVOs(t)

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
		{"ActiveStatus", "active_status.go"},
		{"Money", "money.go"},
		{"StatementEntry", "statement_entry.go"},
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
	files[filepath.Join("wallet", "transaction_type_stub.go")] = []byte(transactionTypeStub)

	return files
}

// TestEmitValueObjectSmokeCompile prova NFR-14: o Go gerado dos VOs do
// wallet, junto do runtime vendorado real, compila e passa go vet num
// projeto isolado.
func TestEmitValueObjectSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, voSmokeFiles(t))
}

// TestEmitValueObjectBehavior prova NFR-15 (fidelidade semântica) sobre o Go
// de fato gerado: escreve os mesmos arquivos do smoke mais um teste Go
// comportamental (walletVOBehaviorTest) num diretório isolado e roda `go
// test ./...` de verdade — mesmo padrão de
// codegen/rtsrc/rtsrc_test.go:TestSourcesBehavioralTestsPass.
func TestEmitValueObjectBehavior(t *testing.T) {
	files := voSmokeFiles(t)
	files[filepath.Join("wallet", "behavior_test.go")] = []byte(walletVOBehaviorTest)

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
