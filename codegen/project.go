package codegen

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
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

	// postgresDriverModule/postgresDriverVersion identificam o SEGUNDO driver
	// real que este gerador sabe vendorar atrás do adapter sqlruntime (J1.2,
	// REQ-41.2/41.3, §design infra-providers 3.1): pgx/v5 stdlib
	// (github.com/jackc/pgx/v5/stdlib registra o driver database/sql "pgx"),
	// escolhido em vez de lib/pq por ser mantido ativamente e ter suporte
	// nativo a jsonb — mesma razão documentada em open_postgres.go.txt. Fixo
	// (não "latest"), mesma razão de sqliteDriverVersion (determinismo,
	// NFR-13).
	postgresDriverModule  = "github.com/jackc/pgx/v5"
	postgresDriverVersion = "v5.10.0"
	// postgresMinGoVersion é a versão mínima de Go que postgresDriverVersion
	// exige (seu próprio go.mod declara "go 1.25.0", confirmado via
	// `go mod download`) — coincide com sqliteMinGoVersion, mesmo espírito
	// documental.
	postgresMinGoVersion = "1.25"

	// amqpDriverModule/amqpDriverVersion identificam o driver real que este
	// gerador sabe vendorar atrás do adapter amqpruntime (J3.1, REQ-43.1,
	// §design infra-providers 3.3): o fork oficial mantido
	// github.com/rabbitmq/amqp091-go (o antigo streadway/amqp está
	// arquivado/sem manutenção). Fixo (não "latest"), mesma razão de
	// sqliteDriverVersion/postgresDriverVersion (determinismo, NFR-13). Seu
	// próprio go.mod exige só "go 1.20" — abaixo do default "1.22" deste
	// gerador — então, ao contrário de sqlite/postgres, não precisa de uma
	// constante "MinGoVersion" própria (fica "" no providerDep, ver
	// provider_registry.go).
	amqpDriverModule  = "github.com/rabbitmq/amqp091-go"
	amqpDriverVersion = "v1.12.0"

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

	// otelModule/otelVersion, otelSDKModule, otelTraceModule e
	// otelExporterModule identificam o conjunto FIXO de módulos que este
	// gerador sabe vendorar atrás do pacote de borda otelruntime (H2, NFR-12,
	// REQ-30.2): fixados (não "latest") pela mesma razão de
	// sqliteDriverModule/grpcModule (determinismo, NFR-13) — confirmados
	// resolvíveis e compatíveis entre si (mesma versão v1.44.0 nos quatro,
	// `go build`/`go vet`/`go test` reais sobre um projeto de prova isolado,
	// ver o relatório da task H2). otlptracehttp (OTLP sobre HTTP) foi
	// escolhido em vez do exporter sobre gRPC para não introduzir
	// google.golang.org/grpc como dependência REAL de compilação deste
	// caminho — "go mod why" confirma que nenhum código deste adapter o
	// importa, mesmo aparecendo no grafo de módulos como indireto.
	otelModule         = "go.opentelemetry.io/otel"
	otelSDKModule      = "go.opentelemetry.io/otel/sdk"
	otelTraceModule    = "go.opentelemetry.io/otel/trace"
	otelExporterModule = "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelVersion        = "v1.44.0"
	// otelMinGoVersion é a versão mínima de Go que os módulos OTel acima
	// exigem (seus próprios go.mod declaram "go 1.25.0", confirmado no
	// probe da task H2) — coincide com sqliteMinGoVersion; EmitGoMod usa o
	// maior entre os adapters ativos.
	otelMinGoVersion = "1.25"
)

// EmitGoMod gera o conteúdo de go.mod do projeto gerado: "module <path>\n\ngo
// <version>\n", SEM bloco require no caso comum — o núcleo depende só de
// stdlib e do runtime vendorado (NFR-12), então não há nenhuma dependência
// externa a declarar. sqlProviderKeys (I7.0, REQ-40.2) é activeSQLProviders
// (sql_wiring.go) — as chaves de sqlProviders efetivamente usadas pelo
// programa (hoje só pode ser ["sqlite"] ou vazio, mas EmitGoMod nunca
// hardcoda "sqlite": itera sqlProviderKeys e resolve CADA UM contra o
// registro sqlProviders — um provider novo, registrado ali, aparece aqui sem
// nenhuma mudança nesta função); grpcAdapter (H1, REQ-29.2) é true quando
// programNeedsGRPC (codegen.go) encontrou ao menos uma "Interface GRPC": só
// ENTÃO cada um acrescenta sua própria linha "require <módulo> <versão>" (as
// únicas dependências externas que este gerador introduz, cada uma isolada e
// opt-in — NFR-12); podem estar presentes ao mesmo tempo (um programa com
// Database sqlite E Interface GRPC), caso em que viram um único bloco
// "require (...)" com as linhas, ordenadas por caminho de módulo
// (determinismo, NFR-13). Cada provider ativo também sobe o default de
// versão de Go para o MAIOR minGoVersion entre eles e otelAdapter (ver
// maxGoVersion, abaixo) — grpcAdapter sozinho NÃO precisa disso:
// grpcMinGoVersion (1.21) já é menor que o default "1.22" abaixo. Um
// programa sem nenhum provider SQL, GRPC nem OTel continua produzindo
// EXATAMENTE o go.mod de antes de G1/H1 (byte a byte) — wallet/shop
// incluídos.
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
//
// otelAdapter (H2, REQ-30.2) é true quando programNeedsOTel (codegen.go)
// encontrou ao menos um mod.ds com "Telemetry { ... }": acrescenta as 4
// linhas "require" do adapter OTel (otel, otel/sdk, otel/trace, o exporter
// OTLP/HTTP — ver a doc das consts acima) ao MESMO bloco "require (...)" que
// os providers SQL/grpcAdapter já usam, ordenadas por caminho de módulo
// junto com as demais (determinismo, NFR-13), e sobe o default de versão de
// Go para otelMinGoVersion quando nenhum provider SQL ativo já exigiu uma
// versão maior (mesmo valor de sqliteMinGoVersion hoje — não há conflito a
// resolver). Um programa sem nenhum dos três adapters continua produzindo
// EXATAMENTE o go.mod de antes de G1/H1/H2 (byte a byte).
//
// providerDeps (J0.2, REQ-46.2, §design 2.2) é activeProviderDeps
// (provider_registry.go) — as providerDep ativas de QUALQUER categoria não-SQL
// (canal/cache/ratelimit/filestorage). Cada uma acrescenta "require
// <dep.module> <dep.version>" ao mesmo bloco (ordenado por módulo com as
// demais), e sobe o default de versão de Go para dep.minGo quando não-vazio —
// mesma mecânica de sqlProviderKeys/sqlProviders.minGoVersion, generalizada
// (REQ-46.1: um provider novo aparece aqui só por entrar no registro da sua
// categoria, sem nenhuma mudança nesta função). Vazio (o caso de hoje, antes
// de J1..J5 popularem qualquer registro) não muda nada (NFR-21).
//
// Dedup por module (revisão da PR #13): duas categorias que usam o MESMO
// provider real (ex. redis em Cache e em RateLimit) têm providerDep com o
// MESMO module mas ctor diferente — activeProviderDeps (dedup por struct
// inteira, R5) não as colapsa de propósito (cada categoria precisa do seu
// ctor). Sem dedup aqui, as duas gerariam DUAS linhas "require" idênticas
// para o mesmo módulo — inválido/redundante em go.mod. seenModules garante
// uma linha "require" por module, mantendo a primeira versão encontrada.
func EmitGoMod(opts Options, outDir string, sqlProviderKeys []string, grpcAdapter, otelAdapter bool, providerDeps []providerDep) []byte {
	modulePath := opts.ModulePath
	if modulePath == "" {
		modulePath = moduleNameFromOutDir(outDir)
	}

	version := opts.GoVersion
	if version == "" {
		version = "1.22"
		if otelAdapter {
			version = maxGoVersion(version, otelMinGoVersion)
		}
		for _, key := range sqlProviderKeys {
			if p, ok := sqlProviders[key]; ok {
				version = maxGoVersion(version, p.minGoVersion)
			}
		}
		for _, dep := range providerDeps {
			if dep.minGo != "" {
				version = maxGoVersion(version, dep.minGo)
			}
		}
	}

	var requires []string
	if grpcAdapter {
		requires = append(requires, fmt.Sprintf("%s %s", grpcModule, grpcVersion))
	}
	if otelAdapter {
		requires = append(requires,
			fmt.Sprintf("%s %s", otelModule, otelVersion),
			fmt.Sprintf("%s %s", otelSDKModule, otelVersion),
			fmt.Sprintf("%s %s", otelTraceModule, otelVersion),
			fmt.Sprintf("%s %s", otelExporterModule, otelVersion),
		)
	}
	for _, key := range sqlProviderKeys {
		if p, ok := sqlProviders[key]; ok {
			requires = append(requires, fmt.Sprintf("%s %s", p.driverModule, p.driverVersion))
		}
	}
	seenModules := make(map[string]bool)
	for _, dep := range providerDeps {
		if dep.module == "" || seenModules[dep.module] {
			continue
		}
		seenModules[dep.module] = true
		requires = append(requires, fmt.Sprintf("%s %s", dep.module, dep.version))
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

// maxGoVersion devolve a maior entre duas versões Go na forma "X.Y" (ex.
// "1.22", "1.25") — usada por EmitGoMod para escolher o default de
// opts.GoVersion entre múltiplos adapters ativos (I7.0: mais de um provider
// SQL registrado, cada um com seu próprio minGoVersion, mais otelAdapter).
// Comparação NUMÉRICA de cada componente (não lexicográfica pura — "1.9" vs
// "1.10" ordenaria errado como string) — nenhuma constante atual exercita
// dois dígitos no minor, mas um provider futuro poderia. Uma versão que não
// parseie como "X.Y" com os dois componentes inteiros é tratada como menor
// (nunca vence a comparação) — nenhuma constante deste arquivo tem essa
// forma, então esse ramo nunca é exercitado hoje.
func maxGoVersion(a, b string) string {
	pa, oka := parseGoVersion(a)
	pb, okb := parseGoVersion(b)
	switch {
	case oka && okb:
		if pa[0] != pb[0] {
			if pa[0] > pb[0] {
				return a
			}
			return b
		}
		if pa[1] >= pb[1] {
			return a
		}
		return b
	case oka:
		return a
	case okb:
		return b
	default:
		return a
	}
}

// parseGoVersion parseia "X.Y" em [X, Y]; ok=false para qualquer outra forma.
func parseGoVersion(v string) ([2]int, bool) {
	parts := strings.SplitN(v, ".", 2)
	if len(parts) != 2 {
		return [2]int{}, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return [2]int{}, false
	}
	return [2]int{major, minor}, true
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
