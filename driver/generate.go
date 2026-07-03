package driver

import (
	"strings"

	"domainscript/codegen"
	"domainscript/diag"
	"domainscript/types"
)

// defaultModulePath é o module path Go usado quando o chamador não passa
// Options.ModulePath: TEM que bater com a raiz de import que codegen usa
// internamente para o runtime vendorado e os pacotes de domínio
// (codegen.RuntimeImportPath, "domainscript/generated/runtime" — fixo desde
// E3.1, decl_value.go) — ver a doc de domainModuleRoot em codegen/codegen.go
// e de EmitGoMod em codegen/project.go: aquele acoplamento documentado exige
// Options.ModulePath == "domainscript/generated" para o projeto gerado
// REALMENTE compilar hoje. Sem esse default aqui, um "dsc gen" sem flag de
// module path (a única forma que a CLI expõe, REQ-32.1) escreveria um go.mod
// cujo "module" diverge dos imports que todo decl_*.go gera, e "go build"
// falharia com "no required module provides package" no projeto gerado.
var defaultModulePath = strings.TrimSuffix(codegen.RuntimeImportPath, "/runtime")

// GenerateProject valida o projeto em dir (REQ-14, CheckProject) e, se
// válido, gera o projeto Go completo (codegen.Generate) e escreve os
// arquivos em out de forma idempotente, removendo artefatos órfãos de
// declarações removidas do .ds (REQ-32, §design 3.15/4.1 — ver writeOutput
// em write.go pelo mecanismo). Um programa com diagnóstico de erro é
// recusado sem gerar nem tocar out (REQ-14.1/32.2, ErrHasDiagnostics). O
// chamador decide a saída (relatório, exit code) a partir do bag e do erro
// devolvidos, no mesmo espírito de CheckSource/CheckProject.
func GenerateProject(dir, out string, opts codegen.Options) (*diag.DiagnosticBag, error) {
	prog, bag := CheckProject(dir)
	if bag.HasErrors() {
		return bag, codegen.ErrHasDiagnostics
	}

	if opts.ModulePath == "" {
		opts.ModulePath = defaultModulePath
	}

	model := types.NewModel(prog.Symbols)
	files, err := codegen.Generate(prog, model, prog.Symbols, bag, opts)
	if err != nil {
		return bag, err
	}

	if err := writeOutput(out, files); err != nil {
		return bag, err
	}
	return bag, nil
}
