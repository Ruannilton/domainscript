package codegen

import (
	"fmt"
	"path/filepath"
)

// project.go emite o esqueleto de projeto do gerado (E9.1, REQ-14.5, §design
// codegen 3.4): go.mod hoje; o restante do layout (runtime/, contracts/,
// <módulo>/, cmd/<service>/) é montado pelo orquestrador (codegen.go), que
// reusa EmitGoMod.

// EmitGoMod gera o conteúdo de go.mod do projeto gerado: "module <path>\n\ngo
// <version>\n", SEM bloco require — o núcleo depende só de stdlib e do
// runtime vendorado (NFR-12), então não há nenhuma dependência externa a
// declarar aqui (isso muda só a partir do Marco G, quando um adapter opt-in
// como database/sql+driver concreto entra atrás de um seam — ainda fora deste
// go.mod nuclear).
//
// O caminho do módulo é opts.ModulePath quando não-vazio; senão, é derivado
// do nome-base de outDir (ex. "/tmp/out/wallet" -> "wallet") — a heurística
// mais previsível para quem só passa um diretório de saída sem pensar em
// nomes de módulo Go. Se outDir também não ajudar (vazio, "." ou só
// separadores — outDir não é sempre conhecido: Generate, por exemplo, não
// recebe outDir, só Options — ver a doc de Generate em codegen.go), o
// fallback final é o literal "generated".
//
// Acoplamento conhecido e documentado (não resolvido nesta task, ver
// domainModuleRoot em codegen.go): codegen.RuntimeImportPath está FIXO como
// "domainscript/generated/runtime" desde E3.1 (decl_value.go) — todo Go
// emitido por qualquer decl_*.go importa o runtime por esse caminho literal,
// independente do que EmitGoMod escreve aqui. Para o projeto gerado
// REALMENTE compilar hoje, o chamador precisa passar
// Options.ModulePath == "domainscript/generated" (== RuntimeImportPath sem o
// sufixo "/runtime"); qualquer outro valor produz um go.mod internamente
// consistente (module X, go Y — go/format aceita), mas cujo runtime/ nunca
// resolve contra os imports "domainscript/generated/runtime" espalhados
// pelos outros emissores. Parametrizar RuntimeImportPath a partir de
// Options.ModulePath em TODOS os decl_*.go (8 arquivos, cada um com golden
// tests byte-a-byte) é trabalho futuro fora do orçamento desta task — ver o
// resumo da task E9.1.
func EmitGoMod(opts Options, outDir string) []byte {
	modulePath := opts.ModulePath
	if modulePath == "" {
		modulePath = moduleNameFromOutDir(outDir)
	}

	version := opts.GoVersion
	if version == "" {
		version = "1.22"
	}

	return []byte(fmt.Sprintf("module %s\n\ngo %s\n", modulePath, version))
}

// moduleNameFromOutDir deriva um nome de módulo Go do nome-base de outDir
// (ex. "/tmp/out/wallet" -> "wallet"); "generated" quando outDir não oferece
// nada usável (vazio, "." ou raiz).
func moduleNameFromOutDir(outDir string) string {
	base := filepath.Base(filepath.Clean(outDir))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "generated"
	}
	return base
}
