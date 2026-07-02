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

// parseWalletErrors parseia o domain.ds real do wallet e devolve seus
// ErrorTypeDecl na ordem de declaração do arquivo (InactiveWallet,
// InsufficientBalance, CurrencyMismatch, NegativeResult) — as fixtures desta
// task são o programa de verdade, não ASTs sintéticas.
func parseWalletErrors(t *testing.T) []*ast.ErrorTypeDecl {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "docs", "examples", "wallet", "domain.ds"))
	if err != nil {
		t.Fatalf("não consegui ler o domain.ds do wallet: %v", err)
	}
	file, bag := driver.CheckSource(string(src))
	if bag.HasErrors() {
		t.Fatalf("wallet/domain.ds tem diagnósticos de erro (fixture deveria ser válida):\n%s", bag.Render())
	}

	var errs []*ast.ErrorTypeDecl
	for _, d := range file.Decls {
		if e, ok := d.(*ast.ErrorTypeDecl); ok {
			errs = append(errs, e)
		}
	}
	return errs
}

// TestEmitErrorsGolden gera o Go dos 4 Errors reais do wallet num único
// arquivo (EmitErrors, o formato de lote que E9.1 vai concatenar em
// errors.go) e compara byte a byte com o artefato versionado (REQ-18.1).
func TestEmitErrorsGolden(t *testing.T) {
	decls := parseWalletErrors(t)
	if len(decls) != 4 {
		t.Fatalf("esperava 4 Errors em wallet/domain.ds, achei %d", len(decls))
	}

	got, err := codegen.EmitErrors("wallet", decls)
	if err != nil {
		t.Fatalf("EmitErrors: erro inesperado: %v", err)
	}
	gentest.Golden(t, filepath.Join("testdata", "errors_wallet.go.golden"), got)
}

// TestEmitErrorGoldenSingle gera o Go de um único Error (InactiveWallet) via
// EmitError e compara com um segundo artefato versionado — prova que a forma
// "um de cada vez" também é suportada e estável (a task permite emitir em
// lote ou individualmente; aqui cobrimos as duas).
func TestEmitErrorGoldenSingle(t *testing.T) {
	decls := parseWalletErrors(t)
	var inactive *ast.ErrorTypeDecl
	for _, d := range decls {
		if d.Name == "InactiveWallet" {
			inactive = d
		}
	}
	if inactive == nil {
		t.Fatal("Error InactiveWallet não encontrado em wallet/domain.ds")
	}

	got, err := codegen.EmitError("wallet", inactive)
	if err != nil {
		t.Fatalf("EmitError(InactiveWallet): erro inesperado: %v", err)
	}
	gentest.Golden(t, filepath.Join("testdata", "error_inactive_wallet.go.golden"), got)
}

// TestEmitErrorsDeterministic prova NFR-13: gerar os mesmos 4 Errors duas
// vezes produz bytes idênticos.
func TestEmitErrorsDeterministic(t *testing.T) {
	decls := parseWalletErrors(t)
	gentest.Deterministic(t, func() []byte {
		got, err := codegen.EmitErrors("wallet", decls)
		if err != nil {
			t.Fatalf("EmitErrors: erro inesperado: %v", err)
		}
		return got
	})
}

// TestEmitErrorRejectsNonLiteralMessage prova que um ErrorTypeDecl cuja
// Message não é um *ast.Literal STRING falha com um erro de geração claro,
// nunca panic de type assertion (decl.Message é ast.Expr — a task pede
// explicitamente essa defesa).
func TestEmitErrorRejectsNonLiteralMessage(t *testing.T) {
	bad := ast.NewErrorTypeDecl("Bad", ast.NewIdent("notALiteral", ast.Span{}), ast.Span{})
	if _, err := codegen.EmitError("wallet", bad); err == nil {
		t.Fatal("esperava erro de geração para Message que não é um literal string")
	}
}

// errorsSmokeFiles monta o conjunto comum de arquivos usado pelo smoke
// compile e pelo teste de comportamento: go.mod, o runtime vendorado real
// (rtsrc) e os 4 Errors do wallet, emitidos em lote num único errors.go.
func errorsSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	decls := parseWalletErrors(t)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	got, err := codegen.EmitErrors("wallet", decls)
	if err != nil {
		t.Fatalf("EmitErrors: erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "errors.go")] = got

	return files
}

// TestEmitErrorsSmokeCompile prova NFR-14: o Go gerado dos 4 Errors do
// wallet, junto do runtime vendorado real, compila e passa go vet num
// projeto isolado.
func TestEmitErrorsSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, errorsSmokeFiles(t))
}

// walletErrorsBehaviorTest roda dentro do projeto isolado gerado no smoke e
// prova, sobre o Go de fato gerado (não uma reimplementação), que
// errors.Is distingue os Errors por Code — inclusive através de wrapping
// (%w), provando que BusinessError.Is funciona por Code e não só por
// igualdade direta de instância (NFR-15).
const walletErrorsBehaviorTest = `package wallet

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorsIsMatchesSameError(t *testing.T) {
	if !errors.Is(ErrInactiveWallet, ErrInactiveWallet) {
		t.Fatal("esperava errors.Is(ErrInactiveWallet, ErrInactiveWallet) == true")
	}
}

func TestErrorsIsDistinguishesDifferentErrors(t *testing.T) {
	if errors.Is(ErrInactiveWallet, ErrInsufficientBalance) {
		t.Fatal("esperava errors.Is(ErrInactiveWallet, ErrInsufficientBalance) == false")
	}
}

func TestErrorsIsMatchesThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf("ao processar deposito: %w", ErrInactiveWallet)
	if !errors.Is(wrapped, ErrInactiveWallet) {
		t.Fatal("esperava errors.Is reconhecer ErrInactiveWallet através de fmt.Errorf(%w)")
	}
	if errors.Is(wrapped, ErrInsufficientBalance) {
		t.Fatal("erro empacotado não deveria casar com um Error diferente")
	}
}

func TestErrorMessagesArePreserved(t *testing.T) {
	if ErrInactiveWallet.Error() != "A carteira está inativa." {
		t.Fatalf("mensagem incorreta: got %q", ErrInactiveWallet.Error())
	}
	if ErrCurrencyMismatch.Code != "CurrencyMismatch" {
		t.Fatalf("Code incorreto: got %q, want %q", ErrCurrencyMismatch.Code, "CurrencyMismatch")
	}
}
`

// TestEmitErrorsBehavior prova NFR-15 sobre o Go de fato gerado: escreve os
// mesmos arquivos do smoke mais um teste Go comportamental num diretório
// isolado e roda `go test ./...` de verdade.
func TestEmitErrorsBehavior(t *testing.T) {
	files := errorsSmokeFiles(t)
	files[filepath.Join("wallet", "behavior_test.go")] = []byte(walletErrorsBehaviorTest)

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
