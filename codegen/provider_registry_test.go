package codegen

import (
	"testing"

	"domainscript/ast"
	"domainscript/program"
	"domainscript/token"
)

// provider_registry_test.go prova a DoD de J0.1 (REQ-46.1, §design 2.1): com
// os quatro registros (channelProviders/cacheProviders/rateLimitProviders/
// fileProviders) vazios — o estado real de hoje, antes de J1..J5 popularem
// qualquer entrada —, activeProviderDeps devolve sempre vazio, mesmo diante
// de um Program que declara canal/Cache/RateLimit/FileStorage com um
// "provider"/"backend" desconhecido; e quando duas categorias apontam para o
// MESMO provider (mesmo module E mesmo adapterDir), a dedup (R5) colapsa as
// duas em uma única entrada.

func TestActiveProviderDepsEmptyRegistry(t *testing.T) {
	prog := &program.Program{
		Modules: map[string]*program.Module{
			"Shop": {
				Name: "Shop",
				Decl: ast.NewModuleDecl("Shop", nil, []*ast.ConfigBlock{
					ast.NewConfigBlock("Cache", "", []ast.ConfigEntry{
						{Key: "backend", Value: &ast.Literal{Kind: token.STRING, Value: "redis"}},
					}, ast.Span{}),
					ast.NewConfigBlock("RateLimit", "", []ast.ConfigEntry{
						{Key: "backend", Value: &ast.Literal{Kind: token.STRING, Value: "redis"}},
					}, ast.Span{}),
				}, ast.Span{}),
				FileStorages: map[string]*program.FileStorage{
					"Uploads": {
						Name: "Uploads",
						Decl: ast.NewConfigBlock("FileStorage", "Uploads", []ast.ConfigEntry{
							{Key: "provider", Value: &ast.Literal{Kind: token.STRING, Value: "s3"}},
						}, ast.Span{}),
					},
				},
			},
		},
		Channels: []*program.Channel{
			{
				From: "Shop",
				To:   "Billing",
				Via:  "queue",
				Decl: ast.NewChannelDef("Shop", "Billing", []ast.ConfigEntry{
					{Key: "provider", Value: &ast.Literal{Kind: token.STRING, Value: "rabbitmq"}},
				}, ast.Span{}),
			},
		},
	}

	deps := activeProviderDeps(prog)
	if len(deps) != 0 {
		t.Fatalf("activeProviderDeps: esperava vazio com registro vazio, veio %+v", deps)
	}
}

func TestActiveProviderDepsDedupSameModuleAndDir(t *testing.T) {
	origCache, origRateLimit := cacheProviders, rateLimitProviders
	defer func() { cacheProviders, rateLimitProviders = origCache, origRateLimit }()

	dep := providerDep{
		module:     "github.com/redis/go-redis/v9",
		version:    "v9.7.0",
		adapterDir: "redisruntime",
		ctor:       "NewRedisQueryCache",
	}
	cacheProviders = map[string]providerDep{"redis": dep}
	rateLimitProviders = map[string]providerDep{"redis": dep}

	prog := &program.Program{
		Modules: map[string]*program.Module{
			"Shop": {
				Name: "Shop",
				Decl: ast.NewModuleDecl("Shop", nil, []*ast.ConfigBlock{
					ast.NewConfigBlock("Cache", "", []ast.ConfigEntry{
						{Key: "backend", Value: &ast.Literal{Kind: token.STRING, Value: "redis"}},
					}, ast.Span{}),
					ast.NewConfigBlock("RateLimit", "", []ast.ConfigEntry{
						{Key: "backend", Value: &ast.Literal{Kind: token.STRING, Value: "redis"}},
					}, ast.Span{}),
				}, ast.Span{}),
			},
		},
	}

	deps := activeProviderDeps(prog)
	if len(deps) != 1 {
		t.Fatalf("activeProviderDeps: esperava 1 entrada deduplicada (mesmo module+adapterDir em 2 categorias), veio %d: %+v", len(deps), deps)
	}
	if deps[0].module != dep.module || deps[0].adapterDir != dep.adapterDir {
		t.Fatalf("activeProviderDeps: entrada inesperada %+v", deps[0])
	}
}

// TestActiveProviderDepsDistinctPartialOverlapSurvives prova a correção do
// bug apontado na revisão da PR #11: duas providerDep que compartilham UM dos
// dois campos (module OU adapterDir) mas não os dois são dependências
// DISTINTAS — nenhuma pode ser descartada por engano. A dedup (R5) só colapsa
// quando a struct inteira é igual (mesmo provider real usado em duas
// categorias), nunca por coincidência parcial de campo.
func TestActiveProviderDepsDistinctPartialOverlapSurvives(t *testing.T) {
	origCache, origRateLimit := cacheProviders, rateLimitProviders
	defer func() { cacheProviders, rateLimitProviders = origCache, origRateLimit }()

	// Mesmo "module", adapterDir DIFERENTE — duas entradas distintas.
	depSameModule := providerDep{module: "github.com/example/shared", adapterDir: "cacheruntime", ctor: "NewCache"}
	depSameModuleOtherDir := providerDep{module: "github.com/example/shared", adapterDir: "ratelimitruntime", ctor: "NewLimiter"}
	cacheProviders = map[string]providerDep{"shared": depSameModule}
	rateLimitProviders = map[string]providerDep{"shared": depSameModuleOtherDir}

	prog := &program.Program{
		Modules: map[string]*program.Module{
			"Shop": {
				Name: "Shop",
				Decl: ast.NewModuleDecl("Shop", nil, []*ast.ConfigBlock{
					ast.NewConfigBlock("Cache", "", []ast.ConfigEntry{
						{Key: "backend", Value: &ast.Literal{Kind: token.STRING, Value: "shared"}},
					}, ast.Span{}),
					ast.NewConfigBlock("RateLimit", "", []ast.ConfigEntry{
						{Key: "backend", Value: &ast.Literal{Kind: token.STRING, Value: "shared"}},
					}, ast.Span{}),
				}, ast.Span{}),
			},
		},
	}

	deps := activeProviderDeps(prog)
	if len(deps) != 2 {
		t.Fatalf("activeProviderDeps: esperava 2 entradas distintas (mesmo module, adapterDir diferente), veio %d: %+v", len(deps), deps)
	}
}
