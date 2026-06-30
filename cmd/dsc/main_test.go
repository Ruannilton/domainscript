package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-8.3/6.7: um arquivo válido sai com código 0 e sem relatório de erro.
func TestRunCleanFileExitsZero(t *testing.T) {
	path := writeFile(t, "ok.ds", `ValueObject Email(string) { Valid { ok } }`)
	var out, errOut bytes.Buffer
	if code := run([]string{path}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, quero 0; saída:\n%s", code, out.String())
	}
}

// REQ-8.3/6.7: um arquivo com erro sai com código 1 e imprime o diagnóstico.
func TestRunFileWithErrorExitsOne(t *testing.T) {
	path := writeFile(t, "bad.ds", `Aggregate Account { state { balance integer } }`)
	var out, errOut bytes.Buffer
	code := run([]string{path}, &out, &errOut)
	if code != 1 {
		t.Fatalf("exit = %d, quero 1", code)
	}
	if !strings.Contains(out.String(), "error:") {
		t.Fatalf("esperava um diagnóstico de erro na saída:\n%s", out.String())
	}
}

// REQ-8.2/8.4: um diretório é validado como projeto agregado, disparando regras
// cross-file e saindo com código 1.
func TestRunDirectoryExitsOne(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "billing/mod.ds", `Module Billing { }`)
	write(t, dir, "billing/events.ds", "ValueObject OrderId(string) { Valid { ok } }\nEvent InvoicePaid { id OrderId }")
	write(t, dir, "shipping/mod.ds", `Module Shipping { }`)
	write(t, dir, "shipping/policy.ds", `Policy OnPaid on InvoicePaid { delivery AtLeastOnce execute { return } }`)

	var out, errOut bytes.Buffer
	if code := run([]string{dir}, &out, &errOut); code != 1 {
		t.Fatalf("exit = %d, quero 1; saída:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "PublicEvent") {
		t.Fatalf("esperava o diagnóstico cross-file na saída:\n%s", out.String())
	}
}

// REQ-8.2: sem argumentos a CLI explica o uso e sai com código 2.
func TestRunNoArgsExitsTwo(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 {
		t.Fatalf("exit = %d, quero 2", code)
	}
	if !strings.Contains(errOut.String(), "uso:") {
		t.Fatalf("esperava mensagem de uso em stderr:\n%s", errOut.String())
	}
}

// Caminho inexistente sai com código 2.
func TestRunMissingPathExitsTwo(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{filepath.Join(t.TempDir(), "nope.ds")}, &out, &errOut); code != 2 {
		t.Fatalf("exit = %d, quero 2", code)
	}
}

func writeFile(t *testing.T, name, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func write(t *testing.T, dir, rel, src string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}
