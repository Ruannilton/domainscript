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
	"domainscript/diag"
	"domainscript/driver"
	"domainscript/lexer"
	"domainscript/parser"
)

// parseWalletEnums parseia o domain.ds real do wallet e indexa seus
// EnumDecl por nome — TransactionType é o Enum real do exemplo (sem coerce).
func parseWalletEnums(t *testing.T) map[string]*ast.EnumDecl {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "docs", "examples", "wallet", "domain.ds"))
	if err != nil {
		t.Fatalf("não consegui ler o domain.ds do wallet: %v", err)
	}
	file, bag := driver.CheckSource(string(src))
	if bag.HasErrors() {
		t.Fatalf("wallet/domain.ds tem diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}

	enums := make(map[string]*ast.EnumDecl)
	for _, d := range file.Decls {
		if en, ok := d.(*ast.EnumDecl); ok {
			enums[en.Name] = en
		}
	}
	return enums
}

// paymentMethodSrc é um Enum com `coerce` construído para esta task (E3.3):
// o spec v6 §2.3 usa PaymentMethod como exemplo de coerce, mas com o
// receptor `self` — o front-end real usa `value` (resolver/receivers.go,
// constructCoerce: {"value"}), então o fixture usa `value.uppercase()`, não
// `self.uppercase()` (esse último só existe no teste de sintaxe do parser,
// parser/parse_enum_test.go:TestEnumCoerce).
const paymentMethodSrc = `Enum PaymentMethod : string {
	CreditCard = "CREDIT_CARD"
	Pix = "PIX"
	coerce from string {
		match value.uppercase() {
			"CREDIT_CARD", "CC" => CreditCard
			"PIX" => Pix
			_ => InvalidPaymentMethod
		}
	}
}`

// parsePaymentMethodEnum parseia paymentMethodSrc só até a AST (lexer.Lex +
// parser.Parse, sem resolver/checker) — a task não roda o resolver aqui, só
// precisa da forma sintática de um EnumDecl com Coerce != nil (§design 3.6,
// "não é a lowering geral de match, E5.2, mas um tradutor escopado").
func parsePaymentMethodEnum(t *testing.T) *ast.EnumDecl {
	t.Helper()
	bag := diag.New()
	toks, lexDiags := lexer.Lex(paymentMethodSrc)
	for _, d := range lexDiags {
		bag.Add(d)
	}
	file := parser.Parse(toks, bag)
	if bag.HasErrors() {
		t.Fatalf("fixture PaymentMethod tem erro de sintaxe:\n%s", bag.Render())
	}
	for _, d := range file.Decls {
		if en, ok := d.(*ast.EnumDecl); ok && en.Name == "PaymentMethod" {
			return en
		}
	}
	t.Fatal("EnumDecl PaymentMethod não encontrado no fixture")
	return nil
}

// TestEmitEnumGolden gera o Go de TransactionType (sem coerce, wallet real)
// e de PaymentMethod (com coerce, fixture desta task) e compara byte a byte
// com os artefatos versionados (REQ-17.4).
func TestEmitEnumGolden(t *testing.T) {
	t.Run("TransactionType", func(t *testing.T) {
		enums := parseWalletEnums(t)
		decl, ok := enums["TransactionType"]
		if !ok {
			t.Fatal("Enum TransactionType não encontrado em wallet/domain.ds")
		}
		got, err := codegen.EmitEnum("wallet", decl)
		if err != nil {
			t.Fatalf("EmitEnum(TransactionType): erro inesperado: %v", err)
		}
		gentest.Golden(t, filepath.Join("testdata", "enum_transaction_type.go.golden"), got)
	})

	t.Run("PaymentMethod", func(t *testing.T) {
		decl := parsePaymentMethodEnum(t)
		got, err := codegen.EmitEnum("wallet", decl)
		if err != nil {
			t.Fatalf("EmitEnum(PaymentMethod): erro inesperado: %v", err)
		}
		gentest.Golden(t, filepath.Join("testdata", "enum_payment_method.go.golden"), got)
	})
}

// TestEmitEnumDeterministic prova NFR-13 sobre o emissor de Enum: gerar o
// mesmo TransactionType duas vezes produz bytes idênticos.
func TestEmitEnumDeterministic(t *testing.T) {
	enums := parseWalletEnums(t)
	decl, ok := enums["TransactionType"]
	if !ok {
		t.Fatal("Enum TransactionType não encontrado em wallet/domain.ds")
	}
	gentest.Deterministic(t, func() []byte {
		got, err := codegen.EmitEnum("wallet", decl)
		if err != nil {
			t.Fatalf("EmitEnum(TransactionType): erro inesperado: %v", err)
		}
		return got
	})
}

// transactionTypeBehaviorTest roda dentro do projeto isolado gerado no
// smoke e prova, sobre o Go de fato gerado, que a coerção implícita de
// TransactionType tem efeito em runtime (NFR-15).
const transactionTypeBehaviorTest = `package wallet

import "testing"

func TestParseTransactionTypeAcceptsKnownValue(t *testing.T) {
	got, err := ParseTransactionType("DEPOSIT")
	if err != nil {
		t.Fatalf("ParseTransactionType(DEPOSIT) não deveria falhar: %v", err)
	}
	if got != TransactionTypeDeposit {
		t.Fatalf("got %v, want %v", got, TransactionTypeDeposit)
	}
}

func TestParseTransactionTypeRejectsUnknownValue(t *testing.T) {
	if _, err := ParseTransactionType("XXX"); err == nil {
		t.Fatal("esperava erro para valor desconhecido (coerção implícita)")
	}
}
`

// paymentMethodErrorsStub substitui, só para este smoke/teste, o Go que o
// emissor de Error (E4.1, ainda não implementado) produziria para o Error
// InvalidPaymentMethod referenciado pelo braço wildcard do coerce.
const paymentMethodErrorsStub = `package wallet

import "domainscript/generated/runtime"

var ErrInvalidPaymentMethod = runtime.BusinessError{Code: "InvalidPaymentMethod", Msg: "PaymentMethod: valor desconhecido"}
`

// paymentMethodBehaviorTest prova, sobre o Go de fato gerado, que o coerce
// explícito de PaymentMethod tem efeito em runtime: strings.ToUpper no
// sujeito (aceita "cc" minúsculo), múltiplos padrões por braço, e o
// wildcard mapeando para o Error do fixture (NFR-15).
const paymentMethodBehaviorTest = `package wallet

import (
	"errors"
	"testing"
)

func TestParsePaymentMethodAcceptsShortAliasCaseInsensitive(t *testing.T) {
	got, err := ParsePaymentMethod("cc")
	if err != nil {
		t.Fatalf("ParsePaymentMethod(cc) não deveria falhar: %v", err)
	}
	if got != PaymentMethodCreditCard {
		t.Fatalf("got %v, want %v", got, PaymentMethodCreditCard)
	}
}

func TestParsePaymentMethodAcceptsPix(t *testing.T) {
	got, err := ParsePaymentMethod("PIX")
	if err != nil {
		t.Fatalf("ParsePaymentMethod(PIX) não deveria falhar: %v", err)
	}
	if got != PaymentMethodPix {
		t.Fatalf("got %v, want %v", got, PaymentMethodPix)
	}
}

func TestParsePaymentMethodRejectsUnknownValue(t *testing.T) {
	_, err := ParsePaymentMethod("???")
	if !errors.Is(err, ErrInvalidPaymentMethod) {
		t.Fatalf("esperava ErrInvalidPaymentMethod, got %v", err)
	}
}
`

// enumSmokeFiles monta o conjunto comum de arquivos usado pelo smoke
// compile e pelo teste de comportamento: go.mod, o runtime vendorado real
// (rtsrc) e os dois Enums desta task, cada um no seu próprio arquivo Go.
func enumSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	enums := parseWalletEnums(t)
	ttDecl, ok := enums["TransactionType"]
	if !ok {
		t.Fatal("Enum TransactionType não encontrado em wallet/domain.ds")
	}
	tt, err := codegen.EmitEnum("wallet", ttDecl)
	if err != nil {
		t.Fatalf("EmitEnum(TransactionType): erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "transaction_type.go")] = tt

	pmDecl := parsePaymentMethodEnum(t)
	pm, err := codegen.EmitEnum("wallet", pmDecl)
	if err != nil {
		t.Fatalf("EmitEnum(PaymentMethod): erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "payment_method.go")] = pm
	files[filepath.Join("wallet", "payment_method_errors_stub.go")] = []byte(paymentMethodErrorsStub)

	return files
}

// TestEmitEnumSmokeCompile prova NFR-14: o Go gerado dos dois Enums, junto
// do runtime vendorado real, compila e passa go vet num projeto isolado.
func TestEmitEnumSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, enumSmokeFiles(t))
}

// TestEmitEnumBehavior prova NFR-15 sobre o Go de fato gerado: escreve os
// mesmos arquivos do smoke mais os testes comportamentais num diretório
// isolado e roda `go test ./...` de verdade.
func TestEmitEnumBehavior(t *testing.T) {
	files := enumSmokeFiles(t)
	files[filepath.Join("wallet", "transaction_type_behavior_test.go")] = []byte(transactionTypeBehaviorTest)
	files[filepath.Join("wallet", "payment_method_behavior_test.go")] = []byte(paymentMethodBehaviorTest)

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
