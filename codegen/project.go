package codegen

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// project.go emite o esqueleto de projeto do gerado (E9.1, REQ-14.5, §design
// codegen 3.4): go.mod hoje; o restante do layout (runtime/, contracts/,
// <módulo>/, cmd/<service>/) é montado pelo orquestrador (codegen.go), que
// reusa EmitGoMod.

// sqliteDriverModule/sqliteDriverVersion identificam o ÚNICO driver real que
// este gerador sabe vendorar atrás do adapter sqlruntime (G1, NFR-12): puro
// Go (sem cgo — o projeto gerado continua buildável só com o toolchain,
// mesmo espírito de NFR-12 aplicado ao driver em si), testado end-to-end
// contra este exato par módulo/versão (ver o relatório da task G1). A versão
// é fixa (não "latest") para que o go.mod gerado seja determinístico
// (NFR-13) e para que `go mod tidy`/`go build` resolvam sempre o mesmo grafo
// de dependências.
const (
	sqliteDriverModule  = "modernc.org/sqlite"
	sqliteDriverVersion = "v1.53.0"
	// sqliteMinGoVersion é a versão mínima de Go que sqliteDriverVersion
	// exige (seu próprio go.mod declara "go 1.25.0") — EmitGoMod usa isto
	// como default de opts.GoVersion quando o adapter SQL está habilitado e
	// o chamador não pediu uma versão explícita.
	sqliteMinGoVersion = "1.25"

	// grpcModule/grpcVersion identificam o ÚNICO par módulo/versão que este
	// gerador sabe vendorar atrás do pacote de borda grpcedge (H1, NFR-12,
	// REQ-29.2): fixado (não "latest") pela mesma razão de
	// sqliteDriverModule/sqliteDriverVersion (determinismo, NFR-13). v1.67.0
	// foi escolhida (em vez da mais recente disponível) por ter uma árvore de
	// dependências transitivas deliberadamente mais enxuta (sem
	// opentelemetry/spiffe/gonum, presentes só a partir de versões bem mais
	// novas do grpc-go) — mais rápida de resolver via `go mod tidy`/`go build`
	// no smoke test (NFR-14) sem trocar nenhuma garantia de compatibilidade
	// que este gerador precisa (ServiceDesc/MethodDesc manuais +
	// encoding.Codec, toda API pública estável há várias versões).
	// google.golang.org/protobuf NÃO é listada aqui: nenhum Go emitido por
	// este gerador a importa diretamente (o codec de borda usa
	// encoding.Codec com `any`, nunca proto.Message — ver
	// codegen/grpcrt/codec.go.txt) — `go mod tidy` a resolve sozinha como
	// dependência INDIRETA do próprio grpc-go, exatamente como já acontece
	// com as dependências transitivas de modernc.org/sqlite acima.
	grpcModule  = "google.golang.org/grpc"
	grpcVersion = "v1.67.0"
	// grpcMinGoVersion é a versão mínima de Go que grpcVersion exige (seu
	// próprio go.mod declara "go 1.21") — não usada hoje como default de
	// opts.GoVersion (o default "1.22" abaixo já a excede), mas registrada
	// aqui pela mesma razão documental de sqliteMinGoVersion.
	grpcMinGoVersion = "1.21"
)

// EmitGoMod gera o conteúdo de go.mod do projeto gerado: "module <path>\n\ngo
// <version>\n", SEM bloco require no caso comum — o núcleo depende só de
// stdlib e do runtime vendorado (NFR-12), então não há nenhuma dependência
// externa a declarar. sqlAdapter (G1, REQ-26.2/26.3) é true quando
// programNeedsSQLAdapter (codegen.go) encontrou ao menos um Database
// provider:"sqlite" no programa; grpcAdapter (H1, REQ-29.2) é true quando
// programNeedsGRPC (codegen.go) encontrou ao menos uma "Interface GRPC": só
// ENTÃO cada um acrescenta sua própria linha "require <módulo> <versão>" (as
// únicas dependências externas que este gerador introduz, cada uma isolada e
// opt-in — NFR-12); as duas podem estar presentes ao mesmo tempo (um
// programa com Database sqlite E Interface GRPC), caso em que viram um único
// bloco "require (...)" com as duas linhas, ordenadas por caminho de módulo
// (determinismo, NFR-13). sqlAdapter também sobe o default de versão de Go
// para sqliteMinGoVersion (o driver exige go >= 1.25) — grpcAdapter sozinho
// NÃO precisa disso: grpcMinGoVersion (1.21) já é menor que o default "1.22"
// abaixo. Um programa sem Database sqlite nem Interface GRPC continua
// produzindo EXATAMENTE o go.mod de antes de G1/H1 (byte a byte) —
// wallet/shop incluídos.
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
func EmitGoMod(opts Options, outDir string, sqlAdapter, grpcAdapter bool) []byte {
	modulePath := opts.ModulePath
	if modulePath == "" {
		modulePath = moduleNameFromOutDir(outDir)
	}

	version := opts.GoVersion
	if version == "" {
		version = "1.22"
		if sqlAdapter {
			version = sqliteMinGoVersion
		}
	}

	var requires []string
	if grpcAdapter {
		requires = append(requires, fmt.Sprintf("%s %s", grpcModule, grpcVersion))
	}
	if sqlAdapter {
		requires = append(requires, fmt.Sprintf("%s %s", sqliteDriverModule, sqliteDriverVersion))
	}

	if len(requires) == 0 {
		return []byte(fmt.Sprintf("module %s\n\ngo %s\n", modulePath, version))
	}
	sort.Strings(requires)
	if len(requires) == 1 {
		return []byte(fmt.Sprintf("module %s\n\ngo %s\n\nrequire %s\n", modulePath, version, requires[0]))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "module %s\n\ngo %s\n\nrequire (\n", modulePath, version)
	for _, r := range requires {
		fmt.Fprintf(&b, "\t%s\n", r)
	}
	b.WriteString(")\n")
	return []byte(b.String())
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
