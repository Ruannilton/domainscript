package codegen

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/otelrt"
	"domainscript/program"
	"domainscript/token"
)

// decl_telemetry.go emite a observabilidade opt-in (H2, REQ-30.2, §design
// 3.13): quando um mod.ds declara "Telemetry { ... }" (spec §12), monta o
// runtime.Observer real (codegen/otelrt, isolado, vendorado — mesmo padrão
// de codegen/sqlrt/G1 e codegen/grpcrt/H1) e o instala em
// cmd/<service>/main.go via runtime.SetObserver. Sem "Telemetry" declarado
// (o caso comum — nem wallet nem shop o declaram), NADA muda: nenhum arquivo
// otelruntime/*.go, nenhum "require go.opentelemetry.io/..." em go.mod,
// nenhuma linha de wiring em main.go — o Observer do processo permanece o
// no-op default de rtsrc/observer.go.txt (NFR-12).
//
// --- log/slog (REQ-30.1) não mora aqui ---
//
// A instrumentação DEFAULT (log/slog + trace_id, sem dep externa) é
// incondicional e vive em lower/stmt.go (logStmt) — este arquivo só cuida do
// caminho OPT-IN (REQ-30.2). O campo "trace_id" de cada log continua saindo
// mesmo sem Telemetry (via o id stdlib simples, runtime.NewTraceID/
// WithTrace, minted na borda — codegen/http.go/grpc.go); Telemetry só troca
// a FONTE desse id (do stdlib para o span OTel ativo, ver
// runtime.TraceIDFrom) e acrescenta spans de verdade — nunca desliga o
// caminho default.
//
// --- Escopo do "endpoint"/"traces" consumidos (spec §12) ---
//
// "exporter" só reconhece "otlp" (o único adapter que este gerador sabe
// montar, sobre OTLP sobre HTTP — ver codegen/otelrt/observer.go.txt para o
// porquê de HTTP em vez de gRPC); "endpoint" aceita "env(\"VAR\")" (mesma
// forma reconhecida por codegen/decl_io.go, envCallKey/adapterValueGo, para
// Adapter) ou um literal string cru; "traces { sampler, sampleRate }" só
// reconhece "always_on" (default, sem bloco "traces") e
// "parentbased_traceidratio" (com "sampleRate"). "logs { ... }" e "metrics {
// ... }" (spec §12) são aceitos pelo parser mas IGNORADOS por este gerador —
// logs continuam saindo por log/slog incondicionalmente (não há bridge para
// um exporter de logs OTel nesta task); "metrics" é escopo de Metric de
// negócio (H3), fora desta task — documentado aqui de propósito para não
// travar a extensão futura.

// moduleTelemetryBlock devolve o bloco "Telemetry { ... }" do mod.ds de mod
// (§12), ou nil quando mod é nil ou não declara um — mesmo padrão de
// moduleCacheBlock (decl_query_cache.go)/moduleIdempotencyBlock
// (usecase_idempotency.go) para outro Kind de ConfigBlock.
func moduleTelemetryBlock(mod *program.Module) *ast.ConfigBlock {
	if mod == nil || mod.Decl == nil {
		return nil
	}
	for _, b := range mod.Decl.Blocks {
		if b.Kind == "Telemetry" {
			return b
		}
	}
	return nil
}

// programNeedsOTel devolve true se ALGUM módulo do programa (em qualquer
// grupo/service) declara "Telemetry { ... }" — o único gatilho que
// acrescenta go.opentelemetry.io/... a go.mod (EmitGoMod) E emite
// otelruntime/*.go (generateOTelRuntimeFiles), mirror de
// programNeedsGRPC/programNeedsSQLAdapter para este adapter.
func programNeedsOTel(prog *program.Program) bool {
	for _, mod := range prog.Modules {
		if moduleTelemetryBlock(mod) != nil {
			return true
		}
	}
	return false
}

// generateOTelRuntimeFiles copia otelrt.Sources() (verbatim, mesmo padrão de
// generateSQLRuntimeFiles/generateGRPCRuntimeFiles) para otelruntime/*.go —
// só chamado quando programNeedsOTel devolve true.
func generateOTelRuntimeFiles() ([]File, error) {
	srcs, err := otelrt.Sources()
	if err != nil {
		return nil, fmt.Errorf("codegen: otelruntime: %w", err)
	}
	names := make([]string, 0, len(srcs))
	for name := range srcs {
		names = append(names, name)
	}
	sort.Strings(names)

	files := make([]File, 0, len(names))
	for _, name := range names {
		files = append(files, File{Path: path.Join("otelruntime", name), Content: srcs[name]})
	}
	return files, nil
}

// telemetryPlan é a config "Telemetry { ... }" (spec §12) de UM módulo, já
// resolvida (ver resolveTelemetryPlan) — nil (o caso comum) quando o módulo
// não declara o bloco.
type telemetryPlan struct {
	// endpointGo é a expressão Go (string) do endpoint OTLP/HTTP — ou
	// "os.Getenv(\"VAR\")" (env(...) reconhecido por forma, mesma regra de
	// envCallKey/decl_io.go) ou um literal Go já entre aspas.
	endpointGo string
	// sampler é "always_on" (default) ou "parentbased_traceidratio".
	sampler string
	// sampleRate só importa quando sampler == "parentbased_traceidratio".
	sampleRate float64
}

// groupTelemetryBlock varre modules (os módulos de UM cmd/<service>, já
// ordenados — ver buildCmdGroups) por um bloco "Telemetry" — devolve
// (nil, "", nil) quando nenhum módulo do grupo declara um (o caso comum).
// Mais de um módulo do MESMO grupo declarando Telemetry é erro de geração
// claro: um processo só tem UM Observer instalado (runtime.SetObserver),
// então dois blocos no mesmo service seriam ambíguos — nem wallet nem shop
// combinam isso hoje.
func groupTelemetryBlock(prog *program.Program, modules []string) (block *ast.ConfigBlock, moduleName string, err error) {
	for _, m := range modules {
		b := moduleTelemetryBlock(prog.Modules[m])
		if b == nil {
			continue
		}
		if block != nil {
			return nil, "", fmt.Errorf("mais de um módulo do mesmo service declara \"Telemetry\" (%s, %s) — um processo só instala um runtime.Observer; wiring combinado não suportado (H2)", moduleName, m)
		}
		block, moduleName = b, m
	}
	return block, moduleName, nil
}

// resolveTelemetryPlan lê block (já achado por groupTelemetryBlock) e
// resolve os campos relevantes ao adapter OTel (endpoint/traces — ver a doc
// do arquivo sobre "logs"/"metrics" ficarem de fora). e é usado só para
// registrar o import de "os" quando "endpoint" usa a forma "env(...)"
// (mesmo padrão de adapterValueGo, decl_io.go).
func resolveTelemetryPlan(e *emit.Emitter, block *ast.ConfigBlock) (*telemetryPlan, error) {
	exporter := "otlp"
	if v, ok, err := configStringLitEntry(block.Entries, "exporter"); err != nil {
		return nil, err
	} else if ok {
		exporter = v
	}
	if !strings.EqualFold(exporter, "otlp") {
		return nil, fmt.Errorf("Telemetry: exporter %q não suportado por este gerador (H2 só sabe montar o adapter \"otlp\", sobre OTLP/HTTP — ver codegen/otelrt/observer.go.txt)", exporter)
	}

	endpointExpr, ok := findConfigEntryExpr(block.Entries, "endpoint")
	if !ok {
		return nil, fmt.Errorf(`Telemetry: falta "endpoint" (spec §12, ex.: endpoint: env("OTEL_EXPORTER_ENDPOINT"))`)
	}
	endpointGo, err := telemetryEndpointGo(e, endpointExpr)
	if err != nil {
		return nil, fmt.Errorf("Telemetry: endpoint: %w", err)
	}

	sampler := "always_on"
	sampleRate := 1.0
	if tracesEntries, ok := configObjectEntry(block.Entries, "traces"); ok {
		if v, ok, err := configStringLitEntry(tracesEntries, "sampler"); err != nil {
			return nil, fmt.Errorf("Telemetry: traces: %w", err)
		} else if ok {
			sampler = v
		}
		if v, ok, err := configFloatEntry(tracesEntries, "sampleRate"); err != nil {
			return nil, fmt.Errorf("Telemetry: traces: %w", err)
		} else if ok {
			sampleRate = v
		}
	}
	switch sampler {
	case "always_on", "parentbased_traceidratio":
	default:
		return nil, fmt.Errorf("Telemetry: traces.sampler %q não suportado por este gerador (H2 só sabe montar always_on/parentbased_traceidratio)", sampler)
	}

	return &telemetryPlan{endpointGo: endpointGo, sampler: sampler, sampleRate: sampleRate}, nil
}

// --- Helpers de leitura de []ast.ConfigEntry (compartilháveis, mas mantidos
// aqui: só este arquivo precisa de string/float/objeto aninhado hoje —
// configBoolEntry/configIdentEntry/configDurationEntry já cobrem os outros
// tipos em usecase_idempotency.go/decl_worker.go). ---

// findConfigEntryExpr procura entries[key] e devolve sua Expr crua, sem
// interpretar a forma — usado quando o CHAMADOR precisa decidir entre mais
// de uma forma aceita (ex. "endpoint": env(...) OU literal string).
func findConfigEntryExpr(entries []ast.ConfigEntry, key string) (ast.Expr, bool) {
	for _, entry := range entries {
		if entry.Key == key {
			return entry.Value, true
		}
	}
	return nil, false
}

// configStringLitEntry busca entries[key] e exige um literal STRING — mesmo
// padrão de configBoolEntry (usecase_idempotency.go), para outro tipo de
// literal. ok=false sem erro quando a chave está ausente.
func configStringLitEntry(entries []ast.ConfigEntry, key string) (val string, ok bool, err error) {
	for _, entry := range entries {
		if entry.Key != key {
			continue
		}
		lit, isLit := entry.Value.(*ast.Literal)
		if !isLit || lit.Kind != token.STRING {
			return "", false, fmt.Errorf("%s: esperava um literal string, veio %T", key, entry.Value)
		}
		return lit.Value, true, nil
	}
	return "", false, nil
}

// configFloatEntry busca entries[key] e exige um literal numérico (INT ou
// FLOAT, ex. "sampleRate: 0.1") — devolve o float64 correspondente. ok=false
// sem erro quando a chave está ausente.
func configFloatEntry(entries []ast.ConfigEntry, key string) (val float64, ok bool, err error) {
	for _, entry := range entries {
		if entry.Key != key {
			continue
		}
		lit, isLit := entry.Value.(*ast.Literal)
		if !isLit || (lit.Kind != token.FLOAT && lit.Kind != token.INT) {
			return 0, false, fmt.Errorf("%s: esperava um literal numérico, veio %T", key, entry.Value)
		}
		f, perr := strconv.ParseFloat(lit.Value, 64)
		if perr != nil {
			return 0, false, fmt.Errorf("%s: %q não é um número válido: %w", key, lit.Value, perr)
		}
		return f, true, nil
	}
	return 0, false, nil
}

// configObjectEntry busca entries[key] e exige um sub-bloco objeto (a forma
// "Key { ... }"/"Key: { ... }" que parse_config.go reconhece, ex.
// "traces { sampler: ..., sampleRate: ... }") — devolve as Entries de dentro.
// ok=false quando a chave está ausente (sem erro: um bloco "Telemetry" sem
// "traces" é válido, ver resolveTelemetryPlan).
func configObjectEntry(entries []ast.ConfigEntry, key string) ([]ast.ConfigEntry, bool) {
	for _, entry := range entries {
		if entry.Key != key {
			continue
		}
		obj, ok := entry.Value.(*ast.ObjectExpr)
		if !ok {
			return nil, false
		}
		return obj.Entries, true
	}
	return nil, false
}

// telemetryEndpointGo traduz a Expr de "endpoint" (§12) para uma expressão
// Go string: "env(\"KEY\")" (por FORMA, mesma regra de envCallKey/
// adapterValueGo, decl_io.go) vira "os.Getenv(\"KEY\")"; um literal STRING
// cru vira ele mesmo, entre aspas Go. Qualquer outra forma é erro de geração
// claro (REQ-14.4) — endpoint não é uma expressão de domínio lowerizável.
func telemetryEndpointGo(e *emit.Emitter, expr ast.Expr) (string, error) {
	if key, ok := envCallKey(expr); ok {
		osAlias := e.Import("os")
		return fmt.Sprintf("%s.Getenv(%q)", osAlias, key), nil
	}
	if lit, ok := expr.(*ast.Literal); ok && lit.Kind == token.STRING {
		return strconv.Quote(lit.Value), nil
	}
	return "", fmt.Errorf("forma não suportada (%T) — esperava env(\"VAR\") ou um literal string", expr)
}

// emitOTelWiring emite, dentro de func main() (chamado ANTES de qualquer
// outra linha — ver a doc de generateCmdMainFile), a construção do adapter
// OTel real (codegen/otelrt) e sua instalação via runtime.SetObserver — só
// chamado quando groupTelemetryBlock achou um bloco para este grupo.
// serviceName vira o atributo "service.name" dos spans exportados (ver
// codegen/otelrt/observer.go.txt, Config.ServiceName).
func emitOTelWiring(e *emit.Emitter, plan *telemetryPlan, serviceName string) {
	ctxAlias := e.Import("context")
	logAlias := e.Import("log")
	runtimeAlias := e.Import(RuntimeImportPath)
	otelAlias := e.Import(path.Join(domainModuleRoot, "otelruntime"))

	e.Line("// Observabilidade (H2, REQ-30.2, §design 3.13): \"Telemetry\" declarado no")
	e.Line("// mod.ds — instala o adapter OTel real; sem isso (o caso comum), o Observer")
	e.Line("// do processo permanece o no-op default (runtime/observer.go).")
	e.Line("otelObserver, otelShutdown, err := %s.NewObserver(%s.Background(), %s.Config{", otelAlias, ctxAlias, otelAlias)
	e.Line("Endpoint: %s,", plan.endpointGo)
	e.Line("ServiceName: %s,", strconv.Quote(serviceName))
	e.Line("Sampler: %s,", strconv.Quote(plan.sampler))
	e.Line("SampleRate: %s,", strconv.FormatFloat(plan.sampleRate, 'g', -1, 64))
	e.Line("})")
	e.Block("if err != nil", func() {
		e.Line("%s.Fatal(err)", logAlias)
	})
	e.Line("defer otelShutdown(%s.Background())", ctxAlias)
	e.Line("%s.SetObserver(otelObserver)", runtimeAlias)
	e.Line("")
}
