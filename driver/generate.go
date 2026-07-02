package driver

import (
	"domainscript/codegen"
	"domainscript/diag"
)

// GenerateProject valida o projeto em dir e, se válido, geraria o projeto Go em
// out (REQ-14, REQ-32). Por ora só a pré-condição está implementada: um
// programa com diagnóstico de erro é recusado (REQ-14.1, ErrHasDiagnostics) — a
// geração em si (codegen.Generate e a escrita dos arquivos em out) chega nas
// fases seguintes deste ciclo (ErrNotImplemented). O chamador decide a saída
// (relatório, exit code) a partir do bag e do erro devolvidos, no mesmo espírito
// de CheckSource/CheckProject.
func GenerateProject(dir, out string, opts codegen.Options) (*diag.DiagnosticBag, error) {
	_, bag := CheckProject(dir)
	if bag.HasErrors() {
		return bag, codegen.ErrHasDiagnostics
	}
	_ = out
	_ = opts
	return bag, codegen.ErrNotImplemented
}
