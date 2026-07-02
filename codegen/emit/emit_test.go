package emit

import (
	"bytes"
	"go/format"
	"strings"
	"testing"
)

// TestEmitterBytesFormatsAndIsDeterministic prova o caminho feliz: dois
// imports efetivamente usados no corpo geram Go gofmt-ado, e duas chamadas de
// Bytes() sobre o mesmo Emitter devolvem bytes idênticos (NFR-13).
func TestEmitterBytesFormatsAndIsDeterministic(t *testing.T) {
	e := New("example")
	fmtAlias := e.Import("fmt")
	timeAlias := e.Import("time")

	e.Block("func Report() string", func() {
		e.Line("now := %s.Now()", timeAlias)
		e.Line("return %s.Sprintf(\"at %%v\", now)", fmtAlias)
	})

	got1, err := e.Bytes()
	if err != nil {
		t.Fatalf("Bytes: erro inesperado: %v", err)
	}
	got2, err := e.Bytes()
	if err != nil {
		t.Fatalf("Bytes (2ª chamada): erro inesperado: %v", err)
	}
	if !bytes.Equal(got1, got2) {
		t.Fatalf("Bytes não é determinístico:\n--- 1ª ---\n%s\n--- 2ª ---\n%s", got1, got2)
	}

	reformatted, err := format.Source(got1)
	if err != nil {
		t.Fatalf("saída não é Go válido: %v", err)
	}
	if !bytes.Equal(got1, reformatted) {
		t.Fatalf("saída não está gofmt-ada:\n--- got ---\n%s\n--- gofmt ---\n%s", got1, reformatted)
	}
}

// TestEmitterBytesFailsOnUnusedImport prova REQ-15.3 sem depender de
// go/format.Source, que empiricamente NÃO rejeita imports não usados (só
// formata sintaticamente): um import registrado e nunca referenciado no corpo
// faz Bytes() falhar com uma mensagem que nomeia o path.
func TestEmitterBytesFailsOnUnusedImport(t *testing.T) {
	e := New("example")
	e.Import("fmt")
	e.Line("var Answer = 42")

	_, err := e.Bytes()
	if err == nil {
		t.Fatal("esperava erro de import não usado, Bytes não falhou")
	}
	if !strings.Contains(err.Error(), "fmt") {
		t.Fatalf("mensagem de erro não menciona o path não usado: %v", err)
	}
}

// TestEmitterBlockNestedIndentationMatchesGofmt cobre Block/Line com
// indentação aninhada: a saída deve ser ponto fixo de gofmt (rodar
// format.Source de novo não muda nada) e refletir os dois níveis de indent.
func TestEmitterBlockNestedIndentationMatchesGofmt(t *testing.T) {
	e := New("example")
	e.Block("func Foo(n int) int", func() {
		e.Block("if n > 0", func() {
			e.Line("return n")
		})
		e.Line("return 0")
	})

	got, err := e.Bytes()
	if err != nil {
		t.Fatalf("Bytes: erro inesperado: %v", err)
	}

	reformatted, err := format.Source(got)
	if err != nil {
		t.Fatalf("saída não é Go válido: %v", err)
	}
	if !bytes.Equal(got, reformatted) {
		t.Fatalf("saída não é ponto fixo de gofmt:\n--- got ---\n%s\n--- gofmt ---\n%s", got, reformatted)
	}

	const wantSnippet = "\tif n > 0 {\n\t\treturn n\n\t}\n\treturn 0\n"
	if !strings.Contains(string(got), wantSnippet) {
		t.Fatalf("indentação aninhada inesperada:\n%s", got)
	}
}

// TestEmitterImportAliasCollisionIsDeterministic prova a desambiguação de
// alias: dois paths diferentes cujo último segmento colide recebem aliases
// distintos, decididos pela ordem de registro (não alfabética).
func TestEmitterImportAliasCollisionIsDeterministic(t *testing.T) {
	e := New("example")
	a1 := e.Import("foo/util")
	a2 := e.Import("bar/util")

	if a1 == a2 {
		t.Fatalf("esperava aliases diferentes para paths colidentes, ambos %q", a1)
	}
	if a1 != "util" {
		t.Fatalf("alias do 1º import: got %q, want %q", a1, "util")
	}
	if a2 != "util2" {
		t.Fatalf("alias do 2º import: got %q, want %q", a2, "util2")
	}
	if got := e.Import("foo/util"); got != a1 {
		t.Fatalf("Import não é idempotente: got %q, want %q", got, a1)
	}
}

// TestEmitterBytesFailsOnInvalidSyntax cobre o outro braço de erro de
// Bytes(): corpo sintaticamente inválido falha no parse (antes mesmo da
// checagem de imports), com o Go bruto anexado para depuração.
func TestEmitterBytesFailsOnInvalidSyntax(t *testing.T) {
	e := New("example")
	e.Line("func broken( {")

	_, err := e.Bytes()
	if err == nil {
		t.Fatal("esperava erro de sintaxe, Bytes não falhou")
	}
}
