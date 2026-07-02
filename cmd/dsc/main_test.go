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

// REQ-32: "dsc check <path>" é equivalente ao uso de argumento único.
func TestRunCheckSubcommand(t *testing.T) {
	path := writeFile(t, "ok.ds", `ValueObject Email(string) { Valid { ok } }`)
	var out, errOut bytes.Buffer
	if code := run([]string{"check", path}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, quero 0; saída:\n%s", code, out.String())
	}
}

// REQ-14.1/32.2: "dsc gen" sobre um projeto com erro recusa a geração, imprime
// os diagnósticos e sai com código 1 — nenhum arquivo é escrito em -o.
func TestRunGenRefusesInvalidProject(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "billing/mod.ds", `Module Billing { }`)
	write(t, dir, "billing/events.ds", "ValueObject OrderId(string) { Valid { ok } }\nEvent InvoicePaid { id OrderId }")
	write(t, dir, "shipping/mod.ds", `Module Shipping { }`)
	write(t, dir, "shipping/policy.ds", `Policy OnPaid on InvoicePaid { delivery AtLeastOnce execute { return } }`)

	outTarget := filepath.Join(t.TempDir(), "gen")
	var stdout, stderr bytes.Buffer
	code := run([]string{"gen", dir, "-o", outTarget}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d, quero 1; stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "PublicEvent") {
		t.Fatalf("esperava o diagnóstico cross-file na saída:\n%s", stdout.String())
	}
	if _, err := os.Stat(outTarget); !os.IsNotExist(err) {
		t.Fatalf("esperava que nada fosse escrito em %q (programa inválido)", outTarget)
	}
}

// A geração em si ainda não está implementada (scaffold do Marco E): um
// projeto válido é aceito na pré-condição mas a CLI sinaliza a limitação com
// código 2, sem imprimir diagnósticos (não há nenhum).
func TestRunGenValidProjectNotImplemented(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "domain.ds", `ValueObject Email(string) { Valid { ok } }`)

	var stdout, stderr bytes.Buffer
	code := run([]string{"gen", dir, "-o", filepath.Join(t.TempDir(), "gen")}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("exit = %d, quero 2; stdout:\n%s\nstderr:\n%s", code, stdout.String(), stderr.String())
	}
	if stdout.String() != "" {
		t.Fatalf("não esperava relatório de diagnósticos:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "ainda não implementada") {
		t.Fatalf("esperava mensagem de limitação em stderr:\n%s", stderr.String())
	}
}

// "dsc gen" sem "-o" é erro de uso (código 2), sem tocar o disco.
func TestRunGenMissingOutFlagExitsTwo(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "domain.ds", `ValueObject Email(string) { Valid { ok } }`)

	var stdout, stderr bytes.Buffer
	if code := run([]string{"gen", dir}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit = %d, quero 2", code)
	}
	if !strings.Contains(stderr.String(), "uso:") {
		t.Fatalf("esperava mensagem de uso em stderr:\n%s", stderr.String())
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
