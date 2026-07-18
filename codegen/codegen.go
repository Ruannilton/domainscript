package codegen

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/rtsrc"
	"domainscript/diag"
	"domainscript/program"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// ErrHasDiagnostics é devolvido quando o programa tem ao menos um diagnóstico
// de severidade error: o gerador recusa produzir código (REQ-14.1).
var ErrHasDiagnostics = errors.New("codegen: programa com diagnósticos de erro, geração recusada")

// Options configura a geração de um projeto Go (REQ-14.5, REQ-15).
type Options struct {
	// ModulePath é o caminho do módulo Go gerado (1ª linha do go.mod). Vazio
	// deixa o chamador derivar um default a partir do diretório de saída.
	ModulePath string

	// GoVersion é a versão mínima declarada no go.mod gerado. Vazio usa o
	// default "1.22" (mínimo para os padrões de rota "METHOD /path/{param}"
	// do net/http.ServeMux — REQ-28).
	GoVersion string
}

// File é um arquivo do projeto Go gerado, com caminho relativo à raiz de
// saída, SEMPRE separado por "/" (como um import path Go, nunca
// filepath.Separator) — quem escreve em disco converte via
// filepath.FromSlash (é exatamente o contrato que gentest.SmokeCompile já
// assume: "path := filepath.Join(dir, filepath.FromSlash(rel))").
type File struct {
	Path    string
	Content []byte
}

// domainModuleRoot é a raiz de módulo Go que TODO import gerado (runtime E
// pacotes de domínio) assume hoje — derivada de RuntimeImportPath
// ("domainscript/generated/runtime" -> "domainscript/generated").
// RuntimeImportPath está fixo desde E3.1 (decl_value.go) — antes de
// Options.ModulePath existir de verdade nesta task; parametrizá-lo por
// Options.ModulePath em todo decl_*.go que o usa (8 arquivos, cada um com
// golden tests byte-a-byte) é trabalho futuro fora do orçamento de E9.1 (ver
// a doc de EmitGoMod em project.go). cmd/<service>/main.go IMPORTA os
// pacotes de domínio sob esta MESMA raiz (não Options.ModulePath) — do
// contrário os imports de runtime e de domínio divergiriam e o projeto
// gerado nunca resolveria. Para o gerado compilar de fato hoje, o chamador
// de Generate precisa passar Options.ModulePath == domainModuleRoot.
var domainModuleRoot = strings.TrimSuffix(RuntimeImportPath, "/runtime")

// Generate produz os arquivos de um projeto Go completo a partir de um
// programa VALIDADO (REQ-14.1 — bag.HasErrors() já deveria ter sido checado
// pelo chamador; Generate não valida, só verifica defensivamente e recusa se
// bag tiver erros, ecoando REQ-14.1). tab é a SymbolTable usada para
// reconsultar tipos/nomes (§design 1.1) — tipicamente prog.Symbols, mas
// aceita separado porque é o que o chamador (driver, E10.1) já tem em mãos.
//
// Fluxo (§design 3.1): go.mod + runtime/ vendorado; um pacote Go por módulo
// de domínio (prog.Modules, em ordem alfabética — NFR-13), cada um com um
// arquivo por CATEGORIA de declaração (value_objects.go, errors.go,
// events.go, aggregate_<nome>.go[+_load.go] por Aggregate, commands.go,
// usecases.go, views.go, queries.go, projections.go — só emitidos quando o
// módulo de fato declara a categoria); contracts/events.go com os
// PublicEvent de TODOS os módulos (pacote compartilhado, quebra ciclos de
// import — §design 3.4); um cmd/<service>/main.go por grupo de módulos
// (prog.Services/ServiceOfModule — monólito implícito ⇒ um grupo default
// único) com wiring in-memory e um esqueleto de servidor HTTP (rotas
// chegam em E9.2). O resultado é ordenado por Path antes de devolver
// (determinismo, NFR-13).
func Generate(prog *program.Program, model *types.Model, tab *symbols.SymbolTable, bag *diag.DiagnosticBag, opts Options) ([]File, error) {
	if bag.HasErrors() {
		return nil, ErrHasDiagnostics
	}
	if err := rejectUnsupportedTenancyStrategies(prog); err != nil {
		return nil, err
	}

	activeSQL := activeSQLProviders(prog) // I7.0, NFR-12 — chaves de sqlProviders (sql_wiring.go) efetivamente usadas
	needsSQL := len(activeSQL) > 0
	needsGRPC := programNeedsGRPC(prog)         // H1, NFR-12 — true só quando ao menos um arquivo declara "Interface GRPC"
	needsOTel := programNeedsOTel(prog)         // H2, NFR-12 — true só quando ao menos um mod.ds declara "Telemetry { ... }"
	activeProviders := activeProviderDeps(prog) // J0.2, NFR-21 — providerDep de canal/cache/ratelimit/filestorage efetivamente ativas

	var files []File
	files = append(files, File{Path: "go.mod", Content: EmitGoMod(opts, "", activeSQL, needsGRPC, needsOTel, activeProviders)})

	runtimeFiles, err := generateRuntimeFiles()
	if err != nil {
		return nil, err
	}
	files = append(files, runtimeFiles...)

	if needsSQL {
		sqlFiles, err := generateSQLRuntimeFiles()
		if err != nil {
			return nil, err
		}
		files = append(files, sqlFiles...)
	}

	providerFiles, err := generateProviderRuntimeFiles(activeProviders) // J0.3, REQ-46.3 — no-op hoje (providerSources vazio)
	if err != nil {
		return nil, err
	}
	files = append(files, providerFiles...)

	if needsGRPC {
		grpcFiles, err := generateGRPCRuntimeFiles()
		if err != nil {
			return nil, err
		}
		files = append(files, grpcFiles...)
	}

	if needsOTel {
		otelFiles, err := generateOTelRuntimeFiles()
		if err != nil {
			return nil, err
		}
		files = append(files, otelFiles...)
	}

	moduleNames := make([]string, 0, len(prog.Modules))
	for name := range prog.Modules {
		moduleNames = append(moduleNames, name)
	}
	sort.Strings(moduleNames)

	// groups/moduleGroupVersions (G6, spec §17) precisam existir ANTES da
	// geração por módulo: um Version (versions/*.ds) pode viver em QUALQUER
	// módulo do mesmo cmd/<service> — não necessariamente o módulo que
	// declara o Command/View que traduz — então o upcast/downcast de um
	// Command/View de um módulo M precisa enxergar os Version de TODOS os
	// módulos do GRUPO de M, não só os de M. Por isso os buckets de TODOS os
	// módulos são computados numa passagem separada, ANTES da passagem que
	// gera arquivos por módulo (que hoje já precisa dos buckets de TODOS os
	// módulos para outras coisas, ex. findCommandInBuckets em cmd/main.go —
	// mas agora também DENTRO de generateModuleFiles).
	groups := buildCmdGroups(prog)
	moduleGroupModules := make(map[string][]string, len(moduleNames))
	for _, g := range groups {
		for _, m := range g.modules {
			moduleGroupModules[m] = g.modules
		}
	}

	buckets := make(map[string]moduleBucket, len(moduleNames))
	for _, name := range moduleNames {
		buckets[name] = bucketModuleDecls(prog, name)
	}

	var allPublicEvents []*ast.EventDecl
	publicEventModule := make(map[string]string) // nome do PublicEvent -> módulo de origem (EmitPublicEvents, F1)
	modulesWithUseCases := make(map[string]bool, len(moduleNames))
	modulesWithPolicies := make(map[string]bool, len(moduleNames))
	modulesWithWorkers := make(map[string]bool, len(moduleNames))
	modulesWithIdempotency := make(map[string]bool, len(moduleNames))   // G2, REQ-20.4
	modulesWithCachedQueries := make(map[string]bool, len(moduleNames)) // G3, REQ-21.3
	modulesWithMetrics := make(map[string]bool, len(moduleNames))       // H3, REQ-30.3
	modulesXADatabases := make(map[string][]string, len(moduleNames))   // G1, REQ-20.5
	modulesOutboxDatabase := make(map[string]string, len(moduleNames))  // Marco J, REQ-42.5

	for _, name := range moduleNames {
		b := buckets[name]
		groupVersions := collectVersionDecls(buckets, moduleGroupModules[name])
		modFiles, marks, err := generateModuleFiles(b, name, model, tab, prog, groupVersions)
		if err != nil {
			return nil, fmt.Errorf("codegen: módulo %s: %w", name, err)
		}
		files = append(files, modFiles...)
		modulesWithUseCases[name] = marks.hasUseCases
		modulesWithPolicies[name] = marks.hasPolicies
		modulesWithWorkers[name] = marks.hasWorkers
		modulesWithIdempotency[name] = marks.hasIdempotency
		modulesWithCachedQueries[name] = marks.hasCachedQueries
		modulesWithMetrics[name] = marks.hasMetrics
		modulesXADatabases[name] = marks.xaDatabases
		modulesOutboxDatabase[name] = marks.outboxDatabase
		allPublicEvents = append(allPublicEvents, b.pubEvents...)
		for _, ev := range b.pubEvents {
			publicEventModule[ev.Name] = name
		}
	}

	if len(allPublicEvents) > 0 {
		content, err := EmitPublicEvents(allPublicEvents, publicEventModule)
		if err != nil {
			return nil, fmt.Errorf("codegen: contracts: %w", err)
		}
		files = append(files, File{Path: "contracts/events.go", Content: content})
	}

	for _, group := range groups {
		fs, err := generateCmdMainFile(prog, group, modulesWithUseCases, modulesWithPolicies, modulesWithWorkers, modulesWithIdempotency, modulesWithCachedQueries, modulesWithMetrics, modulesXADatabases, modulesOutboxDatabase, buckets, model, tab)
		if err != nil {
			return nil, fmt.Errorf("codegen: cmd/%s: %w", group.dirName, err)
		}
		files = append(files, fs...)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// rejectUnsupportedTenancyStrategies recusa a geração (erro claro, ANTES de
// escrever qualquer arquivo) quando algum Database do programa declara
// "tenancy: { strategy: ... }" (§13.1, G5) com um valor que este gerador
// ainda não sabe honrar de verdade: "row_level" é o único totalmente
// implementado (tag+filtro uniforme em runtime.EventStore/Repository,
// codegen/rtsrc, e sqlruntime.EventStore, codegen/sqlrt — ver a doc de
// program.Database.Tenancy); "schema_per_tenant"/"database_per_tenant"
// exigiriam provisionar um schema/banco POR TENANT (CREATE SCHEMA/CREATE
// DATABASE via a built-in "provision tenant(id)", §13.4) — que o front-end
// explicitamente não modela neste ciclo (ver tasks.md G5, nota de escopo) —
// então este gerador não tem NENHUM caminho real para as duas. Gerar mesmo
// assim produziria um Database que finge isolar por tenant e na prática usa
// o MESMO schema/conexão para todos — pior que recusar (postura fail-closed
// desta task: nunca uma isolação silenciosamente ausente). Qualquer outro
// valor (incl. "" — nenhuma tenancy declarada) passa despercebido.
func rejectUnsupportedTenancyStrategies(prog *program.Program) error {
	moduleNames := make([]string, 0, len(prog.Modules))
	for name := range prog.Modules {
		moduleNames = append(moduleNames, name)
	}
	sort.Strings(moduleNames)

	for _, modName := range moduleNames {
		mod := prog.Modules[modName]
		dbNames := make([]string, 0, len(mod.Databases))
		for name := range mod.Databases {
			dbNames = append(dbNames, name)
		}
		sort.Strings(dbNames)
		for _, dbName := range dbNames {
			db := mod.Databases[dbName]
			switch db.Tenancy {
			case "", "row_level":
				// suportado (ou nenhuma tenancy declarada).
			default:
				return fmt.Errorf("codegen: módulo %s: Database %s: tenancy.strategy %q não é suportada por este gerador (G5 implementa só \"row_level\" — \"schema_per_tenant\"/\"database_per_tenant\" exigem provisionamento por tenant, \"provision tenant(id)\" §13.4, que o front-end não modela neste ciclo; ver tasks.md G5)", modName, dbName, db.Tenancy)
			}
		}
	}
	return nil
}

// generateRuntimeFiles copia rtsrc.Sources() (verbatim, REQ-16) para
// runtime/*.go, com os nomes de arquivo ordenados (rtsrc.Sources já devolve
// um map — a ordenação aqui é o que garante determinismo, NFR-13).
func generateRuntimeFiles() ([]File, error) {
	srcs, err := rtsrc.Sources()
	if err != nil {
		return nil, fmt.Errorf("codegen: runtime: %w", err)
	}
	names := make([]string, 0, len(srcs))
	for name := range srcs {
		names = append(names, name)
	}
	sort.Strings(names)

	files := make([]File, 0, len(names))
	for _, name := range names {
		files = append(files, File{Path: path.Join("runtime", name), Content: srcs[name]})
	}
	return files, nil
}

// --- 1. Coleta e categorização das Decl de um módulo. ---

// moduleBucket agrupa as Decl de um módulo por tipo concreto, na ORDEM DE
// ORIGEM (arquivo — já ordenado por path — depois posição no arquivo): a
// forma que generateModuleFiles consome para rotear cada categoria ao
// emissor correspondente.
type moduleBucket struct {
	vos           []*ast.ValueObjectDecl
	enums         []*ast.EnumDecl
	errors        []*ast.ErrorTypeDecl
	privEvents    []*ast.EventDecl
	pubEvents     []*ast.EventDecl
	aggregates    []*ast.AggregateDecl
	commands      []*ast.CommandDecl
	usecases      []*ast.UseCaseDecl
	views         []*ast.ViewDecl
	queries       []*ast.QueryDecl
	projections   []*ast.ProjectionDecl
	policies      []*ast.PolicyDecl
	workers       []*ast.WorkerDecl
	sagas         []*ast.SagaDecl
	notifications []*ast.NotificationDecl
	adapters      []*ast.AdapterDecl
	foreigns      []*ast.ForeignDecl
	// metrics (H3, REQ-30.3, spec §21) são as Metric de negócio do módulo —
	// consumidas por EmitMetrics (decl_metric.go), que emite metrics.go
	// (registry + subscriber/WireMetrics de cada Metric "on Evento") e
	// devolve o agrupamento por Saga de cada Metric "on Saga.completed", que
	// generateModuleFiles repassa a EmitSagas (hook direto no código gerado
	// da Saga, ver decl_saga.go).
	metrics []*ast.MetricDecl
	// versions (G6, spec §17) são os VersionDecl de versions/*.ds — o
	// diretório herda o módulo do mod.ds mais próximo (program.go), exatamente
	// como contracts/ (ver a doc de ModuleOf). Consumidos por
	// codegen/versioning.go (via collectVersionDecls, escopo de GRUPO — todos
	// os módulos de um cmd/<service>), nunca emitidos como arquivo próprio:
	// viram upcast/downcast/route/lifecycle wiring dentro de cmd/<group>/
	// main.go, ao lado das rotas HTTP que versionam (http.go).
	versions []*ast.VersionDecl
	// tests/fixtures (H4, REQ-31, spec §22) são os Test/Fixture de
	// *.test.ds do módulo — consumidos por EmitTests (gentest.go), que emite
	// um único "<pkg>_test.go" (package pkg, interno — precisa acessar
	// state/id/applyX não-exportados dos Aggregates testados, ver a doc de
	// gentest.go). fixtures (§22.6) viram helpers "func fixture<Nome>(...)"
	// no MESMO arquivo, ao lado dos Test (ver a doc de emitFixtureDecl).
	tests    []*ast.TestDecl
	fixtures []*ast.FixtureDecl
}

// bucketModuleDecls percorre os arquivos de moduleName (prog.Files filtrados
// por prog.ModuleOf, ORDENADOS por path — NFR-13) e roteia cada Decl por
// tipo concreto. *ast.ModuleDecl/*ast.InterfaceDecl/*ast.TopologyDecl/
// *ast.UpcastDecl e os construtos de Marco F+ que AINDA não têm emissor
// (Policy ganhou o dela em F1, Worker em F2, Saga em F3, Notification/
// Adapter/Foreign em F4, Metric em H3) não são emitidos por este
// orquestrador (wiring/borda tratados à parte) — caem no default, ignorados
// silenciosamente, junto de qualquer nó de erro (rede de segurança
// defensiva sobre um programa já validado sem erros, REQ-14.4).
// *ast.VersionDecl (G6, versions/*.ds, spec §17) É coletado (b.versions),
// mas também não vira arquivo próprio — collectVersionDecls
// (codegen/versioning.go) o reconsulta por GRUPO de service a partir daqui.
// *ast.TestDecl (H4, *.test.ds, spec §22) É coletado (b.tests) e vira
// "<pkg>_test.go" via EmitTests (gentest.go); *ast.FixtureDecl (b.fixtures,
// §22.6) entra no MESMO arquivo como helper via EmitTests (emitFixtureDecl).
func bucketModuleDecls(prog *program.Program, moduleName string) moduleBucket {
	var paths []string
	for p := range prog.Files {
		if prog.ModuleOf(p) == moduleName {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	var b moduleBucket
	for _, p := range paths {
		for _, d := range prog.Files[p].Decls {
			switch n := d.(type) {
			case *ast.ValueObjectDecl:
				b.vos = append(b.vos, n)
			case *ast.EnumDecl:
				b.enums = append(b.enums, n)
			case *ast.ErrorTypeDecl:
				b.errors = append(b.errors, n)
			case *ast.EventDecl:
				if n.Public {
					b.pubEvents = append(b.pubEvents, n)
				} else {
					b.privEvents = append(b.privEvents, n)
				}
			case *ast.AggregateDecl:
				b.aggregates = append(b.aggregates, n)
			case *ast.CommandDecl:
				b.commands = append(b.commands, n)
			case *ast.UseCaseDecl:
				b.usecases = append(b.usecases, n)
			case *ast.ViewDecl:
				b.views = append(b.views, n)
			case *ast.QueryDecl:
				b.queries = append(b.queries, n)
			case *ast.ProjectionDecl:
				b.projections = append(b.projections, n)
			case *ast.PolicyDecl:
				b.policies = append(b.policies, n)
			case *ast.WorkerDecl:
				b.workers = append(b.workers, n)
			case *ast.SagaDecl:
				b.sagas = append(b.sagas, n)
			case *ast.NotificationDecl:
				b.notifications = append(b.notifications, n)
			case *ast.AdapterDecl:
				b.adapters = append(b.adapters, n)
			case *ast.ForeignDecl:
				b.foreigns = append(b.foreigns, n)
			case *ast.VersionDecl:
				b.versions = append(b.versions, n)
			case *ast.MetricDecl:
				b.metrics = append(b.metrics, n)
			case *ast.TestDecl:
				b.tests = append(b.tests, n)
			case *ast.FixtureDecl:
				b.fixtures = append(b.fixtures, n)
			}
		}
	}
	return b
}

// --- 2. Emissão por módulo. ---

// moduleMarks reporta quais categorias "com wiring próprio" um módulo
// declarou — UseCase (hasUseCases), Policy (hasPolicies) e Worker
// (hasWorkers) — para generateCmdMainFile decidir o que chamar de
// cmd/<service>/main.go: "<pkg>.Wire(...)" só existe quando EmitUseCases/
// EmitPolicies de fato roda; "<pkg>.StartWorkers(ctx)" só existe quando
// EmitWorkers roda (ver a doc de decl_worker.go sobre por que Worker NÃO
// soma mais um "func Wire" ao invés de ganhar seu próprio nome).
type moduleMarks struct {
	hasUseCases, hasPolicies, hasWorkers bool
	// xaDatabases (G1, REQ-20.5, §design 3.8) são os nomes de Database
	// (ordenados) que ao menos um UseCase deste módulo precisa coordenar via
	// 2PC (usecase2PCPlan, decl_usecase.go) — vazio no caso comum (Marco
	// E/F, nenhuma mudança). generateCmdMainFile usa isto para decidir se
	// wira "<pkg>.Wire2PC(...)" com stores sqlite reais, ao lado do
	// "<pkg>.Wire(uow)" de sempre (que continua sendo chamado
	// incondicionalmente quando hasUseCases — inofensivo mesmo quando NENHUM
	// UseCase do módulo de fato usa a uow em memória, já que "uow" é uma var
	// de pacote nunca exigida como "usada").
	xaDatabases []string
	// fileStorages (G1a, §2.5) são os nomes (ordenados) de FileStorage
	// declaradas no mod.ds deste módulo — vazio no caso comum (nenhum
	// Aggregate com bloco storage{}/campo FileRef). generateCmdMainFile usa
	// isto para decidir se injeta "<pkg>.WireFileStorage(nome, ...)" — uma
	// chamada por nome, ao lado do wiring de UseCase/Policy/Worker/2PC.
	fileStorages []string
	// hasIdempotency (G2, REQ-20.4, spec §14) é true quando ao menos um
	// UseCase deste módulo declara "idempotency { ... }" — vazio no caso
	// comum (nenhum UseCase do wallet/shop declara). generateCmdMainFile usa
	// isto para decidir se chama "<pkg>.StartIdempotencyCleanup(ctx)", o
	// worker de limpeza gerado automaticamente (EmitUseCases/
	// emitIdempotencyCleanupStarter, usecase_idempotency.go) — nome próprio,
	// ao lado de StartWorkers (mesma razão de não reusar "Wire", ver a doc de
	// decl_worker.go).
	hasIdempotency bool
	// hasCachedQueries (G3, REQ-21.3, spec §15) é true quando ao menos uma
	// Query deste módulo declara "cache { ... }" — vazio no caso comum
	// (nenhuma Query do wallet/shop declara). generateCmdMainFile usa isto
	// para (a) garantir que o service tenha um runtime.Dispatcher mesmo sem
	// nenhuma Policy (a invalidação de cache precisa de um Dispatcher para
	// assinar, exatamente como Policy) e (b) chamar
	// "<pkg>.WireQueryCache(dispatcher)" — nome próprio, ao lado de
	// StartWorkers/StartIdempotencyCleanup (mesma razão de não reusar "Wire",
	// ver a doc de decl_worker.go).
	hasCachedQueries bool
	// hasMetrics (H3, REQ-30.3, spec §21) é true quando ao menos uma Metric
	// deste módulo dispara "on Evento" (EmitMetrics, decl_metric.go) — vazio
	// no caso comum (nenhuma Metric declarada, ou só "on Saga.completed",
	// que NÃO precisa de Dispatcher — hook direto no código gerado da
	// própria Saga, decl_saga.go). generateCmdMainFile usa isto EXATAMENTE
	// como hasCachedQueries: garante Dispatcher mesmo sem Policy e chama
	// "<pkg>.WireMetrics(dispatcher)" — nome próprio, ao lado de
	// WireQueryCache/StartWorkers/StartIdempotencyCleanup.
	hasMetrics bool
	// outboxDatabase (Marco J, task J2.5, REQ-42.5) é o Database real deste
	// módulo (moduleOutboxDatabaseName, sql_wiring.go) — "" no caso comum
	// (nenhum Database real, ou nenhuma Policy AtLeastOnce local o
	// disputando). generateCmdMainFile usa isto para decidir se abre a
	// conexão real, monta um sqlruntime.OutboxStore e chama
	// "<pkg>.WireOutboxStore(...)" ANTES de "<pkg>.Wire(...)", e depois
	// "go <pkg>.StartOutboxRelay(ctx)"/"go <pkg>.StartOutboxCleanup(ctx)" —
	// nomes próprios, ao lado de StartWorkers/StartIdempotencyCleanup (ver
	// decl_policy.go:emitPolicyWireFunc para o lado do módulo).
	outboxDatabase string
}

// generateModuleFiles emite os arquivos Go de um único módulo (um arquivo
// por categoria não-vazia — ver a doc de Generate) e devolve as moduleMarks
// da categorização (ver a doc de moduleMarks).
//
// Um módulo com UseCase E Policy é recusado com um erro de geração claro:
// tanto emitUOWWireFunc (decl_usecase.go) quanto emitPolicyWireFunc
// (decl_policy.go) emitem "func Wire(...)" — coexistindo no mesmo pacote Go,
// colidiriam (erro de compilação). Nem o wallet nem o shop (as duas fixtures
// reais de hoje) combinam UseCase e Policy no mesmo módulo; combinar as duas
// infra numa única Wire fica para quando um exemplo real precisar disso (ver
// a doc de decl_policy.go). Worker NÃO reproduz essa colisão mesmo quando
// coexiste com UseCase e/ou Policy no mesmo módulo: seu ponto de entrada é
// "StartWorkers", um nome próprio que nunca colide com "Wire" (ver a doc de
// decl_worker.go) — por isso não há guarda equivalente para Worker aqui.
//
// groupVersions (G6, spec §17) são os VersionDecl de TODOS os módulos do
// mesmo cmd/<service> que b.moduleName pertence (collectVersionDecls,
// calculado pelo CHAMADOR antes desta passagem — ver a doc de Generate) —
// não só os deste módulo: um versions/*.ds pode viver em outro módulo do
// grupo. Usado só para emitir api_versions.go (emitModuleAPIVersions,
// versioning.go), quando algum Command/View DESTE módulo tem upcast/downcast
// em groupVersions — vazio (o caso comum, wallet/shop) não emite nada.
func generateModuleFiles(b moduleBucket, moduleName string, model *types.Model, tab *symbols.SymbolTable, prog *program.Program, groupVersions []*ast.VersionDecl) ([]File, moduleMarks, error) {
	pkg := goname.PackageName(moduleName)
	var files []File

	hasUseCases := len(b.usecases) > 0
	hasPolicies := len(b.policies) > 0
	hasWorkers := len(b.workers) > 0
	if hasUseCases && hasPolicies {
		return nil, moduleMarks{}, fmt.Errorf("módulo %s: UseCase e Policy no mesmo módulo ainda não têm wiring combinado suportado (cada um gera seu próprio Wire — colidiriam); ver a doc de decl_policy.go", moduleName)
	}

	reg := goname.NewVOOperatorRegistry()
	for _, vo := range b.vos {
		reg.Register(vo)
	}
	aggregates := make(map[string]*ast.AggregateDecl, len(b.aggregates))
	for _, a := range b.aggregates {
		aggregates[a.Name] = a
	}
	// usecasesByName (H4, gentest.go, §22.2) resolve o alvo de um Test por
	// nome — mesmo mapa nome->decl de aggregates acima, indexando b.usecases
	// (NÃO "repaired", abaixo: EmitTests só precisa de Name/Handles, nunca do
	// corpo Execute que o repair corrige para EmitUseCases).
	usecasesByName := make(map[string]*ast.UseCaseDecl, len(b.usecases))
	for _, uc := range b.usecases {
		usecasesByName[uc.Name] = uc
	}
	// sagasByName (H4, gentest.go, §22.3) resolve o alvo de um Test por nome
	// — mesmo padrão de aggregates/usecasesByName acima.
	sagasByName := make(map[string]*ast.SagaDecl, len(b.sagas))
	for _, s := range b.sagas {
		sagasByName[s.Name] = s
	}
	// policiesByName (H4, gentest.go, §22.4) resolve o alvo de um Test por
	// nome — mesmo padrão de aggregates/usecasesByName/sagasByName acima.
	policiesByName := make(map[string]*ast.PolicyDecl, len(b.policies))
	for _, p := range b.policies {
		policiesByName[p.Name] = p
	}
	// adapterByName indexa os Adapter do módulo por nome (F4, REQ-25.3) — o
	// registry que StmtLowerer.WithNotifyAdapters consulta para reconhecer
	// "Xxx(...)" como notify/call de uma Notification (ver decl_io.go/
	// lower/stmt.go). Notification/Adapter compartilham nome por design
	// (§9.1/9.3, resolver.go:isNotificationAdapterPair) — indexar por
	// AdapterDecl.Name aqui é suficiente e não depende da SymbolTable (que só
	// guarda UM dos dois símbolos, ver a doc de decl_io.go).
	adapterByName := make(map[string]*ast.AdapterDecl, len(b.adapters))
	for _, a := range b.adapters {
		adapterByName[a.Name] = a
	}

	if len(b.vos) > 0 || len(b.enums) > 0 {
		content, err := emitValueObjectsAndEnums(pkg, b.vos, b.enums)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("value_objects.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "value_objects.go"), Content: content})
	}

	if len(b.errors) > 0 {
		content, err := EmitErrors(pkg, b.errors)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("errors.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "errors.go"), Content: content})
	}

	if len(b.privEvents) > 0 || len(b.pubEvents) > 0 {
		// O struct de verdade de um PublicEvent mora no pacote do módulo que
		// o declara, IGUAL a um Event privado (EmitEvents cobre os dois,
		// juntos) — contracts/events.go (EmitPublicEvents, abaixo) só
		// re-exporta um alias por PublicEvent, para não criar um import
		// cycle. Ver a doc de decl_event.go.
		allModuleEvents := make([]*ast.EventDecl, 0, len(b.privEvents)+len(b.pubEvents))
		allModuleEvents = append(allModuleEvents, b.privEvents...)
		allModuleEvents = append(allModuleEvents, b.pubEvents...)
		content, err := EmitEvents(pkg, allModuleEvents)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("events.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "events.go"), Content: content})
	}

	for _, agg := range b.aggregates {
		snake := toSnakeCase(agg.Name)

		aggContent, err := EmitAggregate(pkg, agg, model, tab, moduleName, reg)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("aggregate_%s.go: %w", snake, err)
		}
		files = append(files, File{Path: path.Join(pkg, "aggregate_"+snake+".go"), Content: aggContent})

		loadContent, err := EmitAggregateLoad(pkg, agg, model, tab, moduleName)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("aggregate_%s_load.go: %w", snake, err)
		}
		files = append(files, File{Path: path.Join(pkg, "aggregate_"+snake+"_load.go"), Content: loadContent})
	}

	if len(b.commands) > 0 {
		content, err := EmitCommands(pkg, b.commands, model, tab, moduleName)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("commands.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "commands.go"), Content: content})
	}

	var xaDatabases []string
	hasIdempotency := false
	if hasUseCases {
		repaired := make([]*ast.UseCaseDecl, len(b.usecases))
		for i, uc := range b.usecases {
			// Contorna o bug de gramática do parser sobre "load T(id)"
			// seguido de dispatch de Handle na linha seguinte — ver a doc
			// de usecase_repair.go. No-op sobre um Execute que não bate no
			// padrão exato.
			repaired[i] = repairLoadDispatchExecute(uc)
			if uc.Idempotency != nil {
				hasIdempotency = true
			}
		}
		content, err := EmitUseCases(pkg, repaired, aggregates, prog, model, tab, moduleName, reg, adapterByName)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("usecases.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "usecases.go"), Content: content})

		// xaDatabases (G1): a união, ordenada e sem repetição, dos nomes de
		// Database que ALGUM UseCase repaired precisa coordenar via 2PC — a
		// MESMA decisão que emitUseCaseDecl toma por dentro (usecase2PCPlan),
		// recalculada aqui só para generateCmdMainFile saber se este módulo
		// precisa de "<pkg>.Wire2PC(...)" (main.go não reprocessa UseCaseDecl,
		// só consulta esta marca).
		xaSet := make(map[string]bool)
		for _, uc := range repaired {
			if names, ok := usecase2PCPlan(uc, aggregates, prog); ok {
				for _, n := range names {
					xaSet[n] = true
				}
			}
		}
		if len(xaSet) > 0 {
			xaDatabases = make([]string, 0, len(xaSet))
			for n := range xaSet {
				xaDatabases = append(xaDatabases, n)
			}
			sort.Strings(xaDatabases)
		}
	}

	if len(b.views) > 0 {
		content, err := emitViews(pkg, b.views, model, tab, moduleName)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("views.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "views.go"), Content: content})
	}

	hasCachedQueries := false
	for _, q := range b.queries {
		if len(q.Cache) > 0 {
			hasCachedQueries = true
			break
		}
	}

	// collections.go (ISSUE-1, ver a doc de decl_collections.go): quando o
	// MESMO tipo é fonte de "join" numa Query E de "list"/"count" numa Policy
	// do MESMO módulo, os dois vars de runtime.Collection[T] colidiriam
	// (mesmo nome, um em queries.go outro em policies.go — "redeclared in
	// this block"). sharedModuleCollectionTypeNames calcula a INTERSEÇÃO;
	// quando não vazia, um único collections.go declara cada var disputado
	// uma vez só, e o mapa resultante é repassado a EmitQueries/EmitPolicies,
	// que continuam declarando localmente qualquer tipo NÃO disputado, como
	// sempre. Vazio (o caso comum — nenhum módulo hoje combina os dois lados
	// sobre o mesmo tipo) preserva o layout de sempre: nil para os dois,
	// cada emissor declara tudo o que precisa.
	var sharedCollectionVars map[string]string
	if names := sharedModuleCollectionTypeNames(b.queries, b.policies); len(names) > 0 {
		content, vars, err := EmitCollections(pkg, names)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("collections.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "collections.go"), Content: content})
		sharedCollectionVars = vars
	}

	if len(b.queries) > 0 {
		content, err := EmitQueries(pkg, b.queries, aggregates, prog, model, tab, moduleName, reg, sharedCollectionVars)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("queries.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "queries.go"), Content: content})
	}

	if len(b.projections) > 0 {
		content, err := emitProjections(pkg, b.projections, model, tab, moduleName, reg)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("projections.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "projections.go"), Content: content})
	}

	var outboxDatabase string
	if hasPolicies {
		content, err := EmitPolicies(pkg, b.policies, model, tab, prog, moduleName, reg, adapterByName, sharedCollectionVars)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("policies.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "policies.go"), Content: content})
		if prog != nil {
			if dbName := moduleOutboxDatabaseName(prog, moduleName); dbName != "" {
				needs, err := moduleNeedsDurableOutbox(tab, prog, moduleName, b.policies)
				if err != nil {
					return nil, moduleMarks{}, fmt.Errorf("policies.go: %w", err)
				}
				if needs {
					outboxDatabase = dbName
				}
			}
		}
	}

	if hasWorkers {
		content, err := EmitWorkers(pkg, b.workers, model, tab, moduleName, reg)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("workers.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "workers.go"), Content: content})
	}

	if len(b.notifications) > 0 || len(b.adapters) > 0 {
		notifByName := make(map[string]*ast.NotificationDecl, len(b.notifications))
		for _, n := range b.notifications {
			notifByName[n.Name] = n
		}
		content, err := emitNotificationsAndAdapters(pkg, b.notifications, b.adapters, notifByName, model, tab, moduleName, reg)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("notifications.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "notifications.go"), Content: content})
	}

	if len(b.foreigns) > 0 {
		content, err := emitForeigns(pkg, b.foreigns)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("foreign.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "foreign.go"), Content: content})
	}

	// Metric (H3, REQ-30.3, spec §21): calculado ANTES de Sagas — uma Metric
	// "on Saga.completed" precisa que EmitSagas receba o agrupamento
	// (sagaMetrics) para injetar o hook direto no código gerado da Saga (ver
	// decl_saga.go); uma Metric "on Evento" vira o subscriber/WireMetrics em
	// metrics.go, aqui. hasMetrics só conta o segundo caso (o primeiro não
	// precisa de Dispatcher algum — ver a doc de moduleMarks.hasMetrics).
	var sagaMetrics map[string][]*ast.MetricDecl
	hasMetrics := false
	if len(b.metrics) > 0 {
		content, sm, needsDisp, merr := EmitMetrics(pkg, b.metrics, model, tab, moduleName, reg)
		if merr != nil {
			return nil, moduleMarks{}, fmt.Errorf("metrics.go: %w", merr)
		}
		if needsDisp {
			// Só escreve metrics.go quando há ao menos uma Metric "on Evento"
			// (needsDisp=false ⇒ conteúdo vazio, todas as Metric do módulo
			// são "on Saga.completed" — ver a doc de EmitMetrics).
			files = append(files, File{Path: path.Join(pkg, "metrics.go"), Content: content})
		}
		sagaMetrics = sm
		hasMetrics = needsDisp
	}

	if len(b.sagas) > 0 {
		// Sagas não somam a moduleMarks/wireTargets (generateCmdMainFile): ao
		// contrário de UseCase/Policy/Worker, uma Saga não precisa de nenhum
		// ponto de entrada injetado por cmd/<service>/main.go — sua função de
		// entrada é diretamente chamável e seu SagaStore (mode async) é uma var
		// de pacote auto-inicializada (ver a doc de decl_saga.go). sagaMetrics
		// (H3) é repassado para o hook de Metric "on Saga.completed".
		content, err := EmitSagas(pkg, b.sagas, model, tab, moduleName, reg, adapterByName, sagaMetrics)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("sagas.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "sagas.go"), Content: content})
	}

	fsNames := moduleFileStorageNames(programModule(prog, moduleName))
	if len(fsNames) > 0 {
		content, err := emitFileStorageWiring(pkg)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("filestorage.go: %w", err)
		}
		files = append(files, File{Path: path.Join(pkg, "filestorage.go"), Content: content})
	}

	// api_versions.go (G6, spec §17): Upcast/Downcast de CADA Command/View
	// deste módulo que alguma Version do GRUPO traduz — nil quando nenhum
	// (o caso comum, wallet/shop: groupVersions vazio) ou quando nenhum
	// Command/View DESTE módulo específico é alvo de upcast/downcast.
	apiVersionsContent, err := emitModuleAPIVersions(pkg, b, groupVersions, model, tab, moduleName)
	if err != nil {
		return nil, moduleMarks{}, fmt.Errorf("api_versions.go: %w", err)
	}
	if apiVersionsContent != nil {
		files = append(files, File{Path: path.Join(pkg, "api_versions.go"), Content: apiVersionsContent})
	}

	// Testes nativos (H4, *.test.ds, spec §22, REQ-31): um único
	// "<pkg>_test.go" — package pkg (interno, ver a doc de gentest.go).
	// aggregates é o MESMO mapa já construído acima para EmitUseCases.
	// b.fixtures (§22.6) viram helpers "func fixture<Nome>(...)" no MESMO
	// arquivo, ao lado dos Test (ver a doc de emitFixtureDecl).
	if len(b.tests) > 0 || len(b.fixtures) > 0 {
		content, err := EmitTests(pkg, b.tests, b.fixtures, model, tab, moduleName, reg, aggregates, usecasesByName, sagasByName, policiesByName, adapterByName)
		if err != nil {
			return nil, moduleMarks{}, fmt.Errorf("%s_test.go: %w", pkg, err)
		}
		files = append(files, File{Path: path.Join(pkg, pkg+"_test.go"), Content: content})
	}

	return files, moduleMarks{hasUseCases: hasUseCases, hasPolicies: hasPolicies, hasWorkers: hasWorkers, xaDatabases: xaDatabases, fileStorages: fsNames, hasIdempotency: hasIdempotency, hasCachedQueries: hasCachedQueries, hasMetrics: hasMetrics, outboxDatabase: outboxDatabase}, nil
}

// emitValueObjectsAndEnums combina TODOS os ValueObject/Enum de um módulo
// num único arquivo (value_objects.go, §design 3.4) — reusa os corpos
// internos de EmitValueObject/EmitEnum (emitValueObjectDecl/emitEnumDecl,
// decl_value.go/decl_enum.go) sobre um ÚNICO *emit.Emitter compartilhado, o
// mesmo padrão que EmitEvents/EmitCommands/EmitUseCases já usam para
// combinar várias Decl num arquivo (a API pública EmitValueObject/EmitEnum é
// só-singular — não existe EmitValueObjects/EmitEnums; acessível aqui porque
// project.go/codegen.go vivem no MESMO pacote codegen).
func emitValueObjectsAndEnums(pkg string, vos []*ast.ValueObjectDecl, enums []*ast.EnumDecl) ([]byte, error) {
	e := emit.New(pkg)
	for i, vo := range vos {
		if i > 0 {
			e.Line("")
		}
		if err := emitValueObjectDecl(e, vo); err != nil {
			return nil, err
		}
	}
	for i, en := range enums {
		if i > 0 || len(vos) > 0 {
			e.Line("")
		}
		if err := emitEnumDecl(e, en); err != nil {
			return nil, err
		}
	}
	return e.Bytes()
}

// emitViews combina TODAS as View de um módulo num único arquivo
// (views.go) — mesmo padrão de emitValueObjectsAndEnums, reusando
// emitViewDecl (decl_view.go) sobre um Emitter compartilhado (EmitView, a
// API pública, é só-singular).
func emitViews(pkg string, views []*ast.ViewDecl, model *types.Model, tab *symbols.SymbolTable, module string) ([]byte, error) {
	e := emit.New(pkg)
	for i, v := range views {
		if i > 0 {
			e.Line("")
		}
		if err := emitViewDecl(e, v, model, tab, module); err != nil {
			return nil, err
		}
	}
	return e.Bytes()
}

// emitProjections combina TODAS as Projection de um módulo num único
// arquivo (projections.go) — mesmo padrão, reusando emitProjectionDecl
// (decl_projection.go; EmitProjection, a API pública, é só-singular).
func emitProjections(pkg string, projections []*ast.ProjectionDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	e := emit.New(pkg)
	for i, p := range projections {
		if i > 0 {
			e.Line("")
		}
		if err := emitProjectionDecl(e, p, model, tab, module, reg); err != nil {
			return nil, err
		}
	}
	return e.Bytes()
}

// toSnakeCase converte um nome PascalCase (nome de declaração DomainScript)
// para snake_case (nome de arquivo Go), inserindo "_" antes de cada letra
// maiúscula que não seja a primeira: "WalletId" -> "wallet_id",
// "TransactionType" -> "transaction_type" — a mesma convenção que os golden
// tests de decl_*.go já adotam para nomear seus artefatos
// (testdata/value_object_wallet_id.go.golden, testdata/enum_transaction_
// type.go.golden, testdata/projection_invoice_with_holder.go.golden, ...).
func toSnakeCase(s string) string {
	var b strings.Builder
	for i, r := range s {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// --- 3. contracts/ e cmd/<service>/main.go. ---

// cmdGroup é um grupo de módulos que compartilha um único cmd/<dirName>/
// main.go: um service da topologia (prog.Services) ou, na ausência de
// topologia (monólito implícito, o caso do wallet — prog.Services vazio),
// um único grupo default reunindo todos os módulos sem Service (§design
// 3.4/3.11).
type cmdGroup struct {
	dirName string
	modules []string // nomes de módulo DomainScript, em ordem alfabética
}

// buildCmdGroups agrupa prog.Modules por .Service (REQ-14.5): módulos com o
// mesmo Service (não-vazio) compartilham um grupo nomeado pelo Service;
// TODOS os módulos sem Service (Service == "", inclusive quando a topologia
// inteira está ausente, como no wallet) caem num único grupo default —
// "monólito ⇒ um cmd/". A ordem de iteração é alfabética por nome de
// Service (determinismo, NFR-13); dentro de um grupo, os módulos também
// vêm ordenados (bucketModuleDecls/moduleNames em Generate já garantem isso
// via prog.Modules ordenado).
func buildCmdGroups(prog *program.Program) []cmdGroup {
	byService := make(map[string][]string)
	var moduleNames []string
	for name := range prog.Modules {
		moduleNames = append(moduleNames, name)
	}
	sort.Strings(moduleNames)
	for _, name := range moduleNames {
		svc := prog.ServiceOfModule(name)
		byService[svc] = append(byService[svc], name)
	}

	var svcNames []string
	for svc := range byService {
		svcNames = append(svcNames, svc)
	}
	sort.Strings(svcNames)

	groups := make([]cmdGroup, 0, len(svcNames))
	for _, svc := range svcNames {
		mods := byService[svc]
		dir := svc
		if dir == "" {
			dir = defaultCmdDirName(mods)
		} else {
			dir = goname.PackageName(dir)
		}
		groups = append(groups, cmdGroup{dirName: dir, modules: mods})
	}
	return groups
}

// defaultCmdDirName nomeia o cmd/ do grupo default (módulos sem Service
// declarado): o nome do único módulo do grupo, normalizado como pacote Go
// (o caso do wallet: 1 módulo -> cmd/wallet/); "app" quando o monólito
// implícito reúne mais de um módulo (nenhum nome de módulo se destaca como
// "o" nome do service) — escolha documentada aqui, únicas vezes (§6 do
// design pede uma heurística explícita).
func defaultCmdDirName(modules []string) string {
	if len(modules) == 1 {
		return goname.PackageName(modules[0])
	}
	return "app"
}

// generateCmdMainFile emite cmd/<group.dirName>/main.go: importa runtime +
// os pacotes de domínio dos módulos do grupo que declaram UseCase, Policy
// e/ou Worker (modulesWithUseCases/modulesWithPolicies/modulesWithWorkers —
// UseCase/Policy têm Wire, ver decl_usecase.go/decl_policy.go; Worker tem
// StartWorkers, ver decl_worker.go), instancia o event store, a unit of work
// e (só quando algum módulo do grupo tem Policy — F1) o dispatcher
// in-memory, chama "<pkg>.Wire(...)" com os argumentos que a marca de cada
// módulo pede (uow, dispatcher, ou os dois — nunca os dois no MESMO módulo
// hoje, ver a doc de generateModuleFiles) e, à parte (nome próprio, nunca
// dentro de Wire — ver a doc de decl_worker.go), "<pkg>.StartWorkers(ctx)"
// para cada módulo com Worker, e sobe um servidor HTTP na porta do setting
// "port:" da Interface HTTP do grupo (fallback 8080) cujo Handler é
// newMux(store) — uma função de PACOTE separada (não inline em func main())
// que constrói o *http.ServeMux e registra cada ast.Route dessa Interface
// (emitHTTPRoutes, http.go, E9.2, §design 3.12). newMux existir à parte de
// main() é deliberado: deixa um teste gerado (E10.2+) construir o mux e
// exercitá-lo via httptest sem subir um socket de verdade — main() vira só o
// glue de processo (wiring + ListenAndServe). Um grupo sem Interface HTTP
// (findGroupInterface devolve nil) ainda sobe um servidor, só que com o mux
// vazio (grupos só-worker).
//
// --- Produtor de canal de saída (F5, REQ-25.3, REQ-26.1/26.5, §design 3.11) ---
//
// Quando um módulo do grupo PRODUZ PublicEvent que atravessam um canal
// "queue" da topologia (producerChannelFor), a unit of work do service
// recebe esse transporte como publisher (runtime.NewUnitOfWork(store,
// <canal>) em vez de "(store)" puro) — o lado "emissor" do mesmo mecanismo
// que decl_policy.go:emitPolicyWireFunc já cuida do lado "assinante". Cada
// service constrói sua PRÓPRIA instância (ver a doc de
// rtsrc/channel.go.txt sobre o limite de Marco F: só entrega de verdade
// dentro do MESMO processo). needsDispatcher (Policy local) e um canal de
// saída no MESMO service ainda não têm wiring combinado suportado — erro de
// geração claro (nem wallet nem shop combinam os dois hoje).
//
// --- Cache de Query (G3, REQ-21.3, spec §15) ---
//
// Um módulo com ao menos uma Query cacheada (modulesWithCachedQueries) força
// needsDispatcher=true mesmo sem nenhuma Policy: a invalidação de cache
// assina o MESMO runtime.Dispatcher que Policy já usa (WireQueryCache, ver
// decl_query_cache.go) — "in-process imediata após emit" vem de graça
// porque Dispatcher.Publish já roda de forma síncrona dentro de uow.Run,
// logo após o commit (uow.go.txt), antes de qualquer entrega externa. Isso
// reusa a MESMA guarda que já recusa combinar Policy local com um canal de
// saída no mesmo service (abaixo): uma Query cacheada num módulo que também
// produz para um canal "queue" cai no mesmo erro claro, documentado como
// wiring ainda não combinado (nem wallet nem shop hoje combinam nenhum dos
// dois casos).
//
// --- Metric de negócio (H3, REQ-30.3, spec §21) ---
//
// Um módulo com ao menos uma Metric "on Evento" (modulesWithMetrics) força
// needsDispatcher=true EXATAMENTE como modulesWithCachedQueries acima: o
// subscriber que atualiza o Counter/Histogram assina o MESMO
// runtime.Dispatcher que Policy/cache de Query já usam (WireMetrics, ver
// decl_metric.go). Uma Metric "on Saga.completed" NÃO passa por aqui: não
// precisa de Dispatcher algum (hook direto no código gerado da própria
// Saga, decl_saga.go) — só entra em modulesWithMetrics quando o módulo tem
// ao menos uma Metric do primeiro tipo.
func generateCmdMainFile(prog *program.Program, group cmdGroup, modulesWithUseCases, modulesWithPolicies, modulesWithWorkers, modulesWithIdempotency, modulesWithCachedQueries, modulesWithMetrics map[string]bool, modulesXADatabases map[string][]string, modulesOutboxDatabase map[string]string, buckets map[string]moduleBucket, model *types.Model, tab *symbols.SymbolTable) ([]File, error) {
	e := emit.New("main")
	runtimeAlias := e.Import(RuntimeImportPath)
	fmtAlias := e.Import("fmt")
	logAlias := e.Import("log")
	httpAlias := e.Import("net/http")

	type wireTarget struct {
		alias            string
		pkg              string
		module           string
		hasUseCases      bool
		hasPolicies      bool
		hasWorkers       bool
		hasIdempotency   bool     // G2, REQ-20.4 — chama StartIdempotencyCleanup (ver emitIdempotencyCleanupStarter)
		hasCachedQueries bool     // G3, REQ-21.3 — chama WireQueryCache(dispatcher) (ver decl_query_cache.go)
		hasMetrics       bool     // H3, REQ-30.3 — chama WireMetrics(dispatcher) (ver decl_metric.go)
		xaDatabases      []string // G1, REQ-20.5 — nomes de Database a coordenar via 2PC (ver emitXADatabaseWiring)
		fileStorages     []string // G1a, §2.5 — nomes de FileStorage a instanciar/injetar (ver WireFileStorage)
		outboxDatabase   string   // Marco J, REQ-42.5 — Database real do outbox durável (ver emitOutboxDatabaseWiring); "" no caso comum
	}
	var wireTargets []wireTarget
	needsDispatcher := false
	anyUseCases := false
	anyWorkers := false
	anyIdempotency := false
	anyOutboxDatabases := false
	for _, m := range group.modules {
		hu, hp, hw := modulesWithUseCases[m], modulesWithPolicies[m], modulesWithWorkers[m]
		hi := modulesWithIdempotency[m]
		hc := modulesWithCachedQueries[m] // G3
		hm := modulesWithMetrics[m]       // H3
		ob := modulesOutboxDatabase[m]    // Marco J, REQ-42.5
		fsNames := moduleFileStorageNames(programModule(prog, m))
		if !hu && !hp && !hw && !hc && !hm && len(fsNames) == 0 {
			continue
		}
		pkg := goname.PackageName(m)
		alias := e.Import(path.Join(domainModuleRoot, pkg))
		wireTargets = append(wireTargets, wireTarget{alias: alias, pkg: pkg, module: m, hasUseCases: hu, hasPolicies: hp, hasWorkers: hw, hasIdempotency: hi, hasCachedQueries: hc, hasMetrics: hm, xaDatabases: modulesXADatabases[m], fileStorages: fsNames, outboxDatabase: ob})
		if hp {
			needsDispatcher = true
		}
		if hc {
			needsDispatcher = true // G3: WireQueryCache também assina o Dispatcher
		}
		if hm {
			needsDispatcher = true // H3: WireMetrics também assina o Dispatcher
		}
		if hi {
			anyIdempotency = true
		}
		if hu {
			anyUseCases = true
		}
		if hw {
			anyWorkers = true
		}
		if ob != "" {
			anyOutboxDatabases = true
		}
	}

	// producerModule/producerChannel (F5, REQ-25.3, REQ-26.1/26.5, §design
	// 3.11): o módulo do grupo (no máximo 1, ver producerChannelFor) que
	// PRODUZ PublicEvent atravessando um canal de saída "queue" da
	// topologia — precisa que a unit of work do service publique todo
	// evento que emitir através desse transporte (runtime.NewUnitOfWork(
	// store, <canal>), ver uow.go.txt), exatamente o mecanismo que liga
	// "emit" a "dispatcher/notify" do lado de quem PUBLICA (o lado
	// CONSUMIDOR, Policy cross-service, já é decl_policy.go:
	// emitPolicyWireFunc). Um módulo que precise tanto de Dispatcher
	// (Policy local) quanto de canal de saída no MESMO service ainda não
	// tem wiring combinado suportado (não exercitado por wallet nem shop
	// hoje) — erro de geração claro, mesmo espírito da guarda de
	// generateModuleFiles sobre UseCase+Policy no mesmo módulo.
	var producerModule string
	var producerChannel *program.Channel
	for _, m := range group.modules {
		if !modulesWithUseCases[m] || len(buckets[m].pubEvents) == 0 {
			continue
		}
		ch, err := producerChannelFor(prog, m)
		if err != nil {
			return nil, fmt.Errorf("cmd/%s/main.go: %w", group.dirName, err)
		}
		if ch == nil {
			continue
		}
		if producerChannel != nil {
			return nil, fmt.Errorf("codegen: cmd/%s/main.go: mais de um módulo produtor de canal de saída via queue no mesmo service (%s, %s) — wiring combinado ainda não suportado (F5)", group.dirName, producerModule, m)
		}
		producerModule, producerChannel = m, ch
	}
	if producerChannel != nil && needsDispatcher {
		return nil, fmt.Errorf("codegen: cmd/%s/main.go: módulo com Policy/Query cacheada E módulo produtor de canal de saída no mesmo service ainda não têm wiring combinado suportado (F5/G3)", group.dirName)
	}

	anyXADatabases := false
	for _, wt := range wireTargets {
		if len(wt.xaDatabases) > 0 {
			anyXADatabases = true
			break
		}
	}

	var ctxAlias string
	if anyWorkers || anyXADatabases || anyIdempotency || anyOutboxDatabases {
		ctxAlias = e.Import("context")
	}

	iface := findGroupInterface(prog, group.modules)
	port := httpPortGo(iface)

	// grpcIface (H1, REQ-29, §design 3.12) é resolvido aqui — ANTES da emissão
	// de func main() — porque main() precisa decidir, na hora, se sobe o
	// listener gRPC (grpcInterfaceHasServices), mesmo o registro de fato dos
	// serviços (newGRPCServer) só sendo emitido bem mais abaixo, depois de
	// newMux/helpers (ver a doc de emitGRPCServer, grpc.go, sobre por que essa
	// ordem é a mesma de HTTP: main() -> newMux -> helpers de pacote).
	grpcIface := findGroupGRPCInterface(prog, group.modules)
	hasGRPC := grpcInterfaceHasServices(grpcIface)

	// telemetryPlan (H2, REQ-30.2, §design 3.13) é resolvido aqui — ANTES da
	// emissão de func main() — mesma razão de tenantPlan/verEnv (http.go):
	// resolveTelemetryPlan chama e.Import (para "os", quando "endpoint" usa
	// env(...)) e pode falhar com um erro de geração claro, e é mais simples
	// propagar esse erro ANTES de abrir o bloco de func main() do que dentro
	// dele. plan == nil (nenhum módulo do grupo declara "Telemetry" — o caso
	// comum, wallet/shop incluídos) não muda NADA: nenhuma linha de wiring é
	// emitida, o Observer do processo permanece o no-op default.
	telemetryBlock, telemetryModule, err := groupTelemetryBlock(prog, group.modules)
	if err != nil {
		return nil, fmt.Errorf("cmd/%s/main.go: %w", group.dirName, err)
	}
	var telemetryPlan *telemetryPlan
	if telemetryBlock != nil {
		telemetryPlan, err = resolveTelemetryPlan(e, telemetryBlock)
		if err != nil {
			return nil, fmt.Errorf("cmd/%s/main.go: módulo %s: %w", group.dirName, telemetryModule, err)
		}
	}

	e.Line("// main é o ponto de entrada do service %q — wiring in-memory a partir de", group.dirName)
	e.Line("// mod.ds/topology.ds (§design 3.11). Gerado por dsc gen — sobrescrito a cada")
	e.Line("// geração, não editar à mão.")
	var mainErr error
	e.Block("func main()", func() {
		if telemetryPlan != nil {
			emitOTelWiring(e, telemetryPlan, group.dirName)
		}
		e.Line("store := %s.NewMemoryEventStore()", runtimeAlias)

		var channelVarName string
		if producerChannel != nil {
			channelVarName = strings.ToLower(producerModule[:1]) + producerModule[1:] + "Channel"
			var candidates []channelEventCandidate
			if channelOrderByField(producerChannel) != "" {
				contractsAlias := e.Import(path.Join(domainModuleRoot, "contracts"))
				for _, ev := range buckets[producerModule].pubEvents {
					candidates = append(candidates, channelEventCandidate{evtDecl: ev, goPtrType: "*" + goname.QualifiedRef(contractsAlias, ev.Name)})
				}
			}
			if err := emitChannelTransportVar(e, channelVarName, ":=", producerChannel, candidates, model, tab, producerModule, goname.NewVOOperatorRegistry(), runtimeAlias, true); err != nil {
				mainErr = fmt.Errorf("canal de saída do módulo %s: %w", producerModule, err)
				return
			}
		}

		switch {
		case needsDispatcher:
			e.Line("dispatcher := %s.NewDispatcher()", runtimeAlias)
			e.Line("uow := %s.NewUnitOfWork(store, dispatcher)", runtimeAlias)
		case producerChannel != nil:
			e.Line("uow := %s.NewUnitOfWork(store, %s)", runtimeAlias, channelVarName)
		default:
			e.Line("uow := %s.NewUnitOfWork(store)", runtimeAlias)
		}
		if anyWorkers || anyIdempotency || anyOutboxDatabases {
			e.Line("workerCtx := %s.Background()", ctxAlias)
		}
		e.Line("")
		if len(wireTargets) == 0 {
			e.Line("_ = uow // nenhum módulo deste service declara UseCase/Policy/Worker ainda")
		} else {
			for _, wt := range wireTargets {
				if wt.outboxDatabase != "" {
					// ANTES de Wire (task J2.5, REQ-42.5): Wire lê outboxStore
					// ao construir o DurableOutbox na 1ª Policy AtLeastOnce
					// local (ver a doc de emitPolicyWireFunc, decl_policy.go).
					if err := emitOutboxDatabaseWiring(e, prog, wt.module, wt.alias, wt.outboxDatabase); err != nil {
						mainErr = fmt.Errorf("wiring do outbox durável do módulo %s: %w", wt.module, err)
						return
					}
				}
				var args []string
				if wt.hasUseCases {
					args = append(args, "uow")
				}
				if wt.hasPolicies {
					args = append(args, "dispatcher")
				}
				if wt.hasUseCases || wt.hasPolicies {
					e.Line("%s.Wire(%s)", wt.alias, strings.Join(args, ", "))
				}
				if wt.hasCachedQueries {
					// WireQueryCache (G3, decl_query_cache.go): nome PRÓPRIO,
					// nunca "Wire" (mesma razão de StartWorkers) — assina o
					// MESMO Dispatcher que Policy usa, garantido acima por
					// needsDispatcher.
					e.Line("%s.WireQueryCache(dispatcher)", wt.alias)
				}
				if wt.hasMetrics {
					// WireMetrics (H3, decl_metric.go): nome PRÓPRIO, mesma
					// razão de WireQueryCache — assina o MESMO Dispatcher,
					// garantido acima por needsDispatcher.
					e.Line("%s.WireMetrics(dispatcher)", wt.alias)
				}
				if len(wt.xaDatabases) > 0 {
					if err := emitXADatabaseWiring(e, prog, wt.module, wt.alias, wt.xaDatabases, ctxAlias); err != nil {
						mainErr = fmt.Errorf("wiring 2PC do módulo %s: %w", wt.module, err)
						return
					}
				}
				for _, name := range wt.fileStorages {
					// NewMemoryFileStorage (G1a, §2.5): o seam in-memory, sem
					// dependência externa — o mesmo espírito de NewMemoryEventStore
					// acima (Marco E). Um backend real (S3/GCS/...) entra atrás
					// deste MESMO seam (runtime.FileStorage) em marco posterior,
					// opt-in (NFR-12) — nenhuma mudança de codegen necessária.
					e.Line("%s.WireFileStorage(%s, %s.NewMemoryFileStorage())", wt.alias, strconv.Quote(name), runtimeAlias)
				}
				if wt.hasWorkers {
					e.Line("%s.StartWorkers(workerCtx)", wt.alias)
				}
				if wt.hasIdempotency {
					e.Line("go %s.StartIdempotencyCleanup(workerCtx)", wt.alias)
				}
				if wt.outboxDatabase != "" {
					// StartOutboxRelay/StartOutboxCleanup (task J2.5,
					// REQ-42.5/42.7): nomes próprios, mesma razão de
					// StartWorkers/StartIdempotencyCleanup.
					e.Line("go %s.StartOutboxRelay(workerCtx)", wt.alias)
					e.Line("go %s.StartOutboxCleanup(workerCtx)", wt.alias)
				}
			}
			if !anyUseCases {
				e.Line("_ = uow // nenhum módulo deste service declara UseCase")
			}
		}

		// gRPC (H1, REQ-29, §design 3.12): sobe ao lado do servidor HTTP, na
		// sua PRÓPRIA goroutine — "go server.Serve(lis)" — para que os dois
		// rodem no MESMO processo sem um bloquear o outro; um grupo gRPC-only
		// (sem Interface HTTP) continua subindo o servidor HTTP de sempre (mux
		// vazio, "grupos só-worker" já documentado acima) ADEMAIS do gRPC —
		// não há gate cruzado entre os dois. hasGRPC é pré-computado (ver a
		// doc acima) porque o registro de fato dos serviços (newGRPCServer)
		// só é emitido depois de newMux/helpers.
		if hasGRPC {
			netAlias := e.Import("net")
			e.Line("")
			e.Line("grpcPort := %s", grpcPortGo(grpcIface))
			e.Line("grpcLis, listenErr := %s.Listen(\"tcp\", %s.Sprintf(\":%%s\", grpcPort))", netAlias, fmtAlias)
			e.Block("if listenErr != nil", func() {
				e.Line("%s.Fatal(listenErr)", logAlias)
			})
			e.Line("grpcServer := newGRPCServer(store)")
			e.BlockSuffix("go func()", "()", func() {
				e.Line("%s.Fatal(grpcServer.Serve(grpcLis))", logAlias)
			})
		}

		e.Line("")
		e.Line("port := %s", port)
		e.Line("server := &%s.Server{Addr: %s.Sprintf(\":%%s\", port), Handler: newMux(store)}", httpAlias, fmtAlias)
		e.Line("%s.Fatal(server.ListenAndServe())", logAlias)
	})
	if mainErr != nil {
		return nil, fmt.Errorf("cmd/%s/main.go: %w", group.dirName, mainErr)
	}

	e.Line("")
	e.Line("// newMux constrói o *http.ServeMux do service %q, registrando cada rota de", group.dirName)
	e.Line("// Interface HTTP (§design 3.12) — função à parte de main() (ver a doc acima)")
	e.Line("// para poder ser exercitada por teste via httptest, sem subir um socket real.")
	var bodyErr error
	var hadRoutes bool
	var tenantPlan *httpTenantPlan
	var rlPending []pendingRateLimitPlan
	var verEnv *httpVersioningEnv
	e.Block(fmt.Sprintf("func newMux(store %s.EventStore) *%s.ServeMux", runtimeAlias, httpAlias), func() {
		e.Line("mux := %s.NewServeMux()", httpAlias)
		hadRoutes, tenantPlan, rlPending, verEnv, bodyErr = emitHTTPRoutes(e, "mux", iface, buckets, group.modules, model, tab, prog)
		if bodyErr != nil {
			return
		}
		e.Line("return mux")
	})
	if bodyErr != nil {
		return nil, fmt.Errorf("cmd/%s/main.go: %w", group.dirName, bodyErr)
	}
	if hadRoutes {
		emitHTTPHelpers(e, runtimeAlias)
	}
	// tenantIDFromRequest/requireTenant (G5, spec §13) — só quando a Interface
	// HTTP do grupo de fato declara "tenant { from: ... }" (tenantPlan != nil);
	// mesma razão de posição (depois que newMux fecha) que emitHTTPHelpers/
	// emitRateLimitHelpers abaixo já documentam.
	if tenantPlan != nil {
		emitTenantHelpers(e, tenantPlan, runtimeAlias, httpAlias)
	}
	// Declarações de rate limit (G4, spec §16) — vars de Limiter + a função
	// "<rota>RateLimitChecks" de CADA rota que configura "rateLimit" — só
	// depois que o bloco de newMux (acima) fecha: são declarações de PACOTE,
	// e emiti-las de DENTRO de newMux as aninharia dentro de outra func, Go
	// inválido (ver a doc de pendingRateLimitPlan, codegen/ratelimit.go).
	if len(rlPending) > 0 {
		emitRateLimitHelpers(e, runtimeAlias, httpAlias)
		for _, p := range rlPending {
			if err := emitRouteRateLimitChecks(e, p, runtimeAlias, httpAlias); err != nil {
				return nil, fmt.Errorf("cmd/%s/main.go: %w", group.dirName, err)
			}
		}
	}
	// Versionamento de API (G6, spec §17) — resolveAPIVersion/apiVersionGate +
	// os mapas de lifecycle (as funções Upcast/Downcast em si moram no pacote
	// de domínio do Command/View, emitModuleAPIVersions — ver a nota de
	// arquitetura em httpVersioningEnv, versioning.go) — só quando a
	// Interface HTTP do grupo de fato declara "versioning { ... }"
	// (verEnv.plan != nil); mesma razão de posição (depois que newMux fecha)
	// que emitTenantHelpers/emitRateLimitHelpers acima. Sem "versioning"
	// declarado, verEnv.plan é nil e NADA é emitido — Go gerado byte a byte
	// igual ao de antes de G6.
	if verEnv != nil && verEnv.plan != nil {
		if err := emitVersioningHelpers(e, verEnv, httpAlias); err != nil {
			return nil, fmt.Errorf("cmd/%s/main.go: %w", group.dirName, err)
		}
	}

	// gRPC (H1, REQ-29, §design 3.12): newGRPCServer + o grpc.ServiceDesc/
	// handlers de cada GrpcService, emitidos como declarações de PACOTE depois
	// que newMux/helpers fecham (mesma razão de posição que tenant/rate-limit/
	// versioning acima) — hasGRPC (pré-computado) já garantiu que func main()
	// só referencia "newGRPCServer" quando este bloco de fato o emite.
	if hasGRPC {
		if err := emitGRPCServer(e, group.dirName, grpcIface, buckets, group.modules, model, tab); err != nil {
			return nil, fmt.Errorf("cmd/%s/main.go: %w", group.dirName, err)
		}
	}

	content, err := e.Bytes()
	if err != nil {
		return nil, fmt.Errorf("cmd/%s/main.go: %w", group.dirName, err)
	}
	files := []File{{Path: path.Join("cmd", group.dirName, "main.go"), Content: content}}

	if hasGRPC {
		protoContent, err := emitGRPCProtoFile(group.dirName, grpcIface, buckets, group.modules, model, tab)
		if err != nil {
			return nil, fmt.Errorf("proto/%s.proto: %w", group.dirName, err)
		}
		files = append(files, File{Path: path.Join("proto", group.dirName+".proto"), Content: protoContent})
	}

	return files, nil
}

// findGroupInterface acha o *ast.InterfaceDecl HTTP de qualquer arquivo dos
// módulos de group (percorrido em ordem de path — determinismo), ou nil se
// nenhum módulo do grupo declara "Interface HTTP".
func findGroupInterface(prog *program.Program, modules []string) *ast.InterfaceDecl {
	inGroup := make(map[string]bool, len(modules))
	for _, m := range modules {
		inGroup[m] = true
	}

	var paths []string
	for p := range prog.Files {
		if inGroup[prog.ModuleOf(p)] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	for _, p := range paths {
		for _, d := range prog.Files[p].Decls {
			if id, ok := d.(*ast.InterfaceDecl); ok && id.Kind == "HTTP" {
				return id
			}
		}
	}
	return nil
}

// httpPortGo devolve o literal Go (já entre aspas) da porta HTTP: o valor do
// setting "port:" de iface quando é um literal INT/STRING (ex. "port: 8080"
// -> "\"8080\""); "\"8080\"" (fallback documentado, §design 3.12) em
// qualquer outro caso — iface nil (nenhuma Interface HTTP no grupo, o caso
// do wallet), setting "port" ausente, ou um valor dinâmico (ex.
// "port: env(\"HTTP_PORT\")") que esta task não resolve estaticamente
// (decisão do prompt de E9.1: repassar a expressão como está exigiria um
// lowering de config-expr que não existe aqui; usar o default é permitido
// explicitamente).
func httpPortGo(iface *ast.InterfaceDecl) string {
	if iface != nil {
		for _, entry := range iface.Settings {
			if entry.Key != "port" {
				continue
			}
			if lit, ok := entry.Value.(*ast.Literal); ok {
				switch lit.Kind {
				case token.INT, token.STRING:
					return strconv.Quote(lit.Value)
				}
			}
			break
		}
	}
	return `"8080"`
}
