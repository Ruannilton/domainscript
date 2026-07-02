package driver

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"domainscript/codegen"
)

// REQ-14.1: um programa com diagnóstico de erro é recusado — GenerateProject
// devolve ErrHasDiagnostics e o bag carrega os diagnósticos para o chamador
// reportar.
func TestGenerateProjectRefusesInvalidProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.ds"), []byte(`Aggregate Account { state { balance integer } }`), 0o644); err != nil {
		t.Fatal(err)
	}

	bag, err := GenerateProject(dir, filepath.Join(t.TempDir(), "out"), codegen.Options{})
	if !errors.Is(err, codegen.ErrHasDiagnostics) {
		t.Fatalf("err = %v, quero codegen.ErrHasDiagnostics", err)
	}
	if !bag.HasErrors() {
		t.Fatalf("esperava o bag com os diagnósticos do programa inválido:\n%s", bag.Render())
	}
}

// A geração em si ainda não está implementada: um programa válido passa pela
// pré-condição mas GenerateProject sinaliza a limitação do scaffold.
func TestGenerateProjectValidProgramNotImplemented(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "domain.ds"), []byte(`ValueObject Email(string) { Valid { ok } }`), 0o644); err != nil {
		t.Fatal(err)
	}

	bag, err := GenerateProject(dir, filepath.Join(t.TempDir(), "out"), codegen.Options{})
	if !errors.Is(err, codegen.ErrNotImplemented) {
		t.Fatalf("err = %v, quero codegen.ErrNotImplemented", err)
	}
	if bag.HasErrors() {
		t.Fatalf("não esperava diagnósticos de erro:\n%s", bag.Render())
	}
}
