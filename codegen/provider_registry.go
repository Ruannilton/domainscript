package codegen

import (
	"sort"
	"strings"

	"domainscript/program"
)

// providerDep é o que uma categoria (Canal/Cache/RateLimit/FileStorage)
// precisa saber de um provider real para (a) exigi-lo em go.mod e (b) copiar
// as fontes do seu adapter opt-in — a generalização de sqlProvider
// (sql_wiring.go, I7.0) para as demais categorias (J0.1, REQ-46.1, §design
// 2.1). Database continua em sqlProviders/sqlProvider (mantém dialectCtor,
// um campo específico de SQL que as outras categorias não têm — fundir os
// dois registros foi considerado e descartado, ver §design 4).
type providerDep struct {
	module     string // caminho do módulo Go a exigir em go.mod (EmitGoMod)
	version    string
	minGo      string // versão mínima de Go que o driver exige; "" quando não eleva o default
	adapterDir string // dir de fontes .txt opt-in a copiar (ex. "amqpruntime")
	ctor       string // construtor do adapter no pacote gerado (ex. "NewRabbitMQChannel")
}

// channelProviders/cacheProviders/rateLimitProviders/fileProviders são os
// registros únicos de provider real por categoria (REQ-46.1, §design 2.1) —
// mesma mecânica de sqlProviders, uma categoria por mapa porque cada uma
// seleciona seu provider a partir de um lugar diferente do programa (canal
// via topology.ds, Cache/RateLimit via o mod.ds do módulo, FileStorage via
// seu próprio bloco). Vazios nesta task (J0.1): cada entrada real chega numa
// task própria (J3.1 rabbitmq, J4.1/J4.2 redis, J5.1 s3) — até lá,
// activeProviderDeps devolve sempre vazio, e nenhum projeto gerado muda
// (NFR-21).
var (
	channelProviders   = map[string]providerDep{}
	cacheProviders     = map[string]providerDep{}
	rateLimitProviders = map[string]providerDep{}
	fileProviders      = map[string]providerDep{}
)

// activeProviderDeps varre prog por categoria (canal, Cache, RateLimit,
// FileStorage — Database fica de fora, já coberto por activeSQLProviders) e
// devolve, deduplicada e ordenada (NFR-23), toda providerDep efetivamente
// ativa: um Channel cujo "provider:" resolve em channelProviders, um bloco
// Cache/RateLimit cujo "backend:" resolve nos respectivos registros, uma
// FileStorage cujo "provider:" resolve em fileProviders. É a fonte única que
// EmitGoMod (go.mod) e generateCategoryRuntimeFiles (cópia de fontes) vão
// consumir (J0.2/J0.3) — mesma mecânica de activeSQLProviders (sql_wiring.go),
// elevada a todas as categorias (REQ-46.1).
//
// Dedup (R5, §design 7): duas categorias que apontam para o MESMO provider
// (ex. redis em Cache e em RateLimit) colapsam numa única providerDep — tanto
// por "module" (um único require em go.mod) quanto por "adapterDir" (uma
// única cópia de fontes), mesmo quando as duas entradas de registro são
// structs distintas (uma por mapa) com os mesmos valores.
func activeProviderDeps(prog *program.Program) []providerDep {
	seenModule := make(map[string]bool)
	seenDir := make(map[string]bool)
	var deps []providerDep

	add := func(dep providerDep, ok bool) {
		if !ok {
			return
		}
		if dep.module != "" {
			if seenModule[dep.module] {
				return
			}
			seenModule[dep.module] = true
		}
		if dep.adapterDir != "" {
			if seenDir[dep.adapterDir] {
				return
			}
			seenDir[dep.adapterDir] = true
		}
		deps = append(deps, dep)
	}

	for _, ch := range prog.Channels {
		if ch == nil || ch.Decl == nil {
			continue
		}
		provider, ok, err := configStringLitEntry(ch.Decl.Entries, "provider")
		if err != nil || !ok {
			continue
		}
		dep, known := channelProviders[strings.ToLower(provider)]
		add(dep, known)
	}

	moduleNames := make([]string, 0, len(prog.Modules))
	for name := range prog.Modules {
		moduleNames = append(moduleNames, name)
	}
	sort.Strings(moduleNames)

	for _, name := range moduleNames {
		mod := prog.Modules[name]

		if block := moduleCacheBlock(mod); block != nil {
			if backend, ok, err := configStringLitEntry(block.Entries, "backend"); err == nil && ok {
				dep, known := cacheProviders[strings.ToLower(backend)]
				add(dep, known)
			}
		}

		if block := moduleRateLimitBlock(mod); block != nil {
			if backend, ok, err := configStringLitEntry(block.Entries, "backend"); err == nil && ok {
				dep, known := rateLimitProviders[strings.ToLower(backend)]
				add(dep, known)
			}
		}

		fsNames := make([]string, 0, len(mod.FileStorages))
		for fsName := range mod.FileStorages {
			fsNames = append(fsNames, fsName)
		}
		sort.Strings(fsNames)
		for _, fsName := range fsNames {
			fs := mod.FileStorages[fsName]
			if fs == nil || fs.Decl == nil {
				continue
			}
			provider, ok, err := configStringLitEntry(fs.Decl.Entries, "provider")
			if err != nil || !ok {
				continue
			}
			dep, known := fileProviders[strings.ToLower(provider)]
			add(dep, known)
		}
	}

	sort.Slice(deps, func(i, j int) bool { return deps[i].module < deps[j].module })
	return deps
}
