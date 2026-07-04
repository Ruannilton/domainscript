package codegen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/program"
	"domainscript/token"
)

// ratelimit.go emite o rate limiting na borda HTTP (spec §16, G4): dimensões
// (perIp/perUser/perTenant/perApiKey/global — múltiplas exigem que TODAS
// passem), os três algoritmos (token_bucket com burst, sliding_window,
// fixed_window), tiers de RateLimitTier via "rateLimit: byTier"/tenant.tier,
// e onBackendFailure (open/closed, override por endpoint). O mecanismo de
// verdade (Limiter/CheckRateLimits) mora no runtime vendorado
// (codegen/rtsrc/ratelimit.go.txt, transporte-agnóstico); este
// arquivo só resolve a CONFIGURAÇÃO declarada (interface.ds + mod.ds +
// RateLimitTier) para um plano concreto por rota, e emite a wiring HTTP-
// específica (codegen/http.go chama emitRouteRateLimitChecks/
// emitRateLimitHelpers) — os headers/status 429 são um adapter à parte
// (writeRateLimitExceeded/writeRateLimitHeaders abaixo), exatamente o
// "transporte-agnóstico no núcleo, adapter HTTP por cima" que H1 (gRPC,
// RESOURCE_EXHAUSTED) vai precisar reusar sem mexer no núcleo.
//
// --- Onde a config mora (spec §16: "Política no interface.ds, backend no
// mod.ds") ---
//
// "rateLimit { perIp: ..., ... }" na Interface (ast.InterfaceDecl.Settings)
// é o default de TODAS as rotas; "rateLimit: { ... }"/"rateLimit: byTier"
// numa Route (ast.Route.Options) é o override por endpoint — mergeado por
// mergeRateLimitConfig (dimensão a dimensão: a rota herda o que não
// reescreve, o exemplo do spec §16 — perIp herdado da Interface, perUser/
// burst apertados na rota de deposit). O bloco "RateLimit { algorithm:
// ..., onBackendFailure: ... }" do mod.ds (program.Module.Decl.Blocks) dá o
// default de algoritmo/fail-behavior — resolvido por MÓDULO do alvo da
// rota (o mesmo padrão "infra por módulo" de Idempotency/Cache, G2/G3),
// não por Interface (que pode agrupar módulos de mod.ds diferentes).
//
// --- "Rotas sem tenant só perIp" (spec §16) generalizado ---
//
// A regra do spec vira, aqui, um princípio mais geral: qualquer dimensão
// cuja IDENTIDADE não está disponível NESTA requisição (sem
// runtime.CallerFrom para perUser, sem runtime.TenantFrom para
// perTenant/byTier, sem o header para perApiKey) é simplesmente OMITIDA da
// checagem — nunca um erro de geração nem uma falha de requisição. Hoje
// (antes de G5 aterrissar), runtime.TenantFrom(ctx) SEMPRE devolve
// (Tenant{}, false) — o mesmo placeholder documentado que G3 já consome
// para a chave de cache — então perTenant/byTier na prática nunca disparam
// ainda; o dia que G5 injetar um Tenant de verdade na borda, esta MESMA
// geração passa a aplicar as duas dimensões sem nenhuma mudança de código
// gerado (NFR-12/16, o mesmo espírito de seam reservado que G1/G2/G3 já
// estabeleceram para suas próprias dependências futuras).

// --- 1. Config bruta de UM "rateLimit" ConfigEntry. ---

// rateLimitDimensionNames são as 5 dimensões do spec §16 — as únicas chaves
// de um objeto "rateLimit { ... }" que carregam um literal RATE.
var rateLimitDimensionNames = map[string]bool{
	"perIp": true, "perUser": true, "perTenant": true, "perApiKey": true, "global": true,
}

// rateLimitDimensionOrder fixa a ordem de emissão das dimensões (NFR-13): a
// ordem de origem no .ds não é confiável (a Interface e a rota podem listar
// um subconjunto diferente, em ordem diferente) — uma ordem canônica única
// mantém as declarações de var/checagens do limitador determinísticas
// independentemente disso.
var rateLimitDimensionOrder = []string{"perIp", "perUser", "perTenant", "perApiKey", "global"}

// rateLimitConfig é a config bruta (ainda não mergeada) de UM ConfigEntry
// "rateLimit" — a Interface (default) ou UMA rota (override), lidas por
// parseRateLimitEntry.
type rateLimitConfig struct {
	byTier           bool
	dims             map[string]*ast.Literal // dimensão -> literal RATE
	burst            *ast.Literal            // literal INT, nil se ausente
	onBackendFailure string                  // "", "open" ou "closed"
}

// parseRateLimitEntry busca entries["rateLimit"] — um objeto de dimensões
// ("rateLimit { perIp: 1000/min, ... }") ou o identificador nu "byTier"
// (spec §16). ok=false (via (nil, nil)) quando a chave está ausente — o
// caso comum: nem a Interface nem a rota declaram rate limiting.
func parseRateLimitEntry(entries []ast.ConfigEntry) (*rateLimitConfig, error) {
	for _, entry := range entries {
		if entry.Key != "rateLimit" {
			continue
		}
		switch v := entry.Value.(type) {
		case *ast.Ident:
			if v.Name != "byTier" {
				return nil, fmt.Errorf("rateLimit: identificador nu desconhecido %q (só \"byTier\" é reconhecido, spec §16)", v.Name)
			}
			return &rateLimitConfig{byTier: true}, nil
		case *ast.ObjectExpr:
			cfg := &rateLimitConfig{dims: make(map[string]*ast.Literal)}
			for _, ce := range v.Entries {
				switch {
				case rateLimitDimensionNames[ce.Key]:
					lit, ok := ce.Value.(*ast.Literal)
					if !ok || lit.Kind != token.RATE {
						return nil, fmt.Errorf("rateLimit.%s: esperava um literal RATE (ex. \"100/min\"), veio %T", ce.Key, ce.Value)
					}
					cfg.dims[ce.Key] = lit
				case ce.Key == "burst":
					lit, ok := ce.Value.(*ast.Literal)
					if !ok || lit.Kind != token.INT {
						return nil, fmt.Errorf("rateLimit.burst: esperava um literal inteiro, veio %T", ce.Value)
					}
					cfg.burst = lit
				case ce.Key == "onBackendFailure":
					id, ok := ce.Value.(*ast.Ident)
					if !ok || (id.Name != "open" && id.Name != "closed") {
						return nil, fmt.Errorf("rateLimit.onBackendFailure: esperava o identificador \"open\" ou \"closed\", veio %T", ce.Value)
					}
					cfg.onBackendFailure = id.Name
				default:
					return nil, fmt.Errorf("rateLimit: chave desconhecida %q (spec §16: perIp/perUser/perTenant/perApiKey/global/burst/onBackendFailure)", ce.Key)
				}
			}
			return cfg, nil
		default:
			return nil, fmt.Errorf("rateLimit: esperava um objeto de dimensões ou o identificador \"byTier\", veio %T", entry.Value)
		}
	}
	return nil, nil
}

// mergeRateLimitConfig combina o default da Interface (ifaceCfg, pode ser
// nil) com o override de UMA rota (routeCfg, pode ser nil): "byTier" em
// QUALQUER um dos dois vence sozinho — uma rota participa do esquema de
// tiers OU do esquema flat, nunca dos dois. Caso contrário, as dimensões da
// rota SOBRESCREVEM as de mesmo nome vindas da Interface; as demais são
// HERDADAS (o exemplo do spec §16: a rota de deposit herda perIp da
// Interface, e aperta perUser/burst por conta própria). burst/
// onBackendFailure seguem a mesma prioridade (rota > Interface). Devolve
// nil quando os dois lados são nil — nenhum rate limiting configurado.
func mergeRateLimitConfig(ifaceCfg, routeCfg *rateLimitConfig) *rateLimitConfig {
	if ifaceCfg == nil && routeCfg == nil {
		return nil
	}
	if (routeCfg != nil && routeCfg.byTier) || (routeCfg == nil && ifaceCfg != nil && ifaceCfg.byTier) {
		return &rateLimitConfig{byTier: true}
	}

	merged := &rateLimitConfig{dims: make(map[string]*ast.Literal)}
	if ifaceCfg != nil {
		for k, v := range ifaceCfg.dims {
			merged.dims[k] = v
		}
		merged.burst = ifaceCfg.burst
		merged.onBackendFailure = ifaceCfg.onBackendFailure
	}
	if routeCfg != nil {
		for k, v := range routeCfg.dims {
			merged.dims[k] = v
		}
		if routeCfg.burst != nil {
			merged.burst = routeCfg.burst
		}
		if routeCfg.onBackendFailure != "" {
			merged.onBackendFailure = routeCfg.onBackendFailure
		}
	}
	return merged
}

// moduleRateLimitBlock devolve o bloco "RateLimit { ... }" do mod.ds de mod
// (§16), ou nil quando mod é nil ou não declara um — mesmo padrão de
// moduleCacheBlock/moduleIdempotencyBlock (decl_query_cache.go/
// usecase_idempotency.go) para outro Kind de ConfigBlock.
func moduleRateLimitBlock(mod *program.Module) *ast.ConfigBlock {
	if mod == nil || mod.Decl == nil {
		return nil
	}
	for _, b := range mod.Decl.Blocks {
		if b.Kind == "RateLimit" {
			return b
		}
	}
	return nil
}

// resolveRateLimitAlgorithm lê "algorithm:" do bloco RateLimit do mod.ds —
// um dos três nomes do spec §16 ("token_bucket", o default quando o bloco
// ou a chave estão ausentes; "sliding_window"; "fixed_window").
func resolveRateLimitAlgorithm(modBlock *ast.ConfigBlock) (string, error) {
	if modBlock == nil {
		return "token_bucket", nil
	}
	name, has, err := configIdentEntry(modBlock.Entries, "algorithm")
	if err != nil {
		return "", fmt.Errorf("mod.ds RateLimit.%w", err)
	}
	if !has {
		return "token_bucket", nil
	}
	switch name {
	case "token_bucket", "sliding_window", "fixed_window":
		return name, nil
	default:
		return "", fmt.Errorf("mod.ds RateLimit.algorithm: %q não é um algoritmo reconhecido (spec §16: token_bucket/sliding_window/fixed_window)", name)
	}
}

// resolveRateLimitFailOpen combina o default do mod.ds RateLimit.
// onBackendFailure ("open" quando o bloco/chave estão ausentes — o default
// do próprio spec §16) com o override de cfg (Interface/rota, já mergeado
// por mergeRateLimitConfig) — "override por endpoint" vira, aqui, um bool
// já resolvido em tempo de GERAÇÃO (RateLimitCheck.FailOpen).
func resolveRateLimitFailOpen(modBlock *ast.ConfigBlock, cfg *rateLimitConfig) (bool, error) {
	failOpen := true
	if modBlock != nil {
		name, has, err := configIdentEntry(modBlock.Entries, "onBackendFailure")
		if err != nil {
			return false, fmt.Errorf("mod.ds RateLimit.%w", err)
		}
		if has {
			if name != "open" && name != "closed" {
				return false, fmt.Errorf("mod.ds RateLimit.onBackendFailure: esperava \"open\" ou \"closed\", veio %q", name)
			}
			failOpen = name == "open"
		}
	}
	if cfg.onBackendFailure != "" {
		failOpen = cfg.onBackendFailure == "open"
	}
	return failOpen, nil
}

// rateLimitPeriodNanos espelha durationUnitNanos (codegen/lower/expr.go,
// não exportado de lá) — o conjunto de unidades de um literal RATE é o
// MESMO de DURATION (lexer.go: durationUnits), então a mesma tabela de
// nanossegundos se aplica.
var rateLimitPeriodNanos = map[string]int64{
	"ms":  int64(time.Millisecond),
	"s":   int64(time.Second),
	"min": int64(time.Minute),
	"h":   int64(time.Hour),
	"d":   int64(24 * time.Hour),
}

// parseRateLiteral traduz o lexema de um literal RATE (ex. "300/min") na
// contagem (300) e na expressão Go do período ("time.Duration(60000000000)")
// — resolvidos em tempo de GERAÇÃO, mesma convenção de lowerDurationLiteral
// (codegen/lower/expr.go) para DURATION.
func parseRateLiteral(lit *ast.Literal) (count int64, periodGo string, err error) {
	if lit.Kind != token.RATE {
		return 0, "", fmt.Errorf("esperava um literal RATE, veio %s", lit.Kind)
	}
	idx := strings.IndexByte(lit.Value, '/')
	if idx < 0 {
		return 0, "", fmt.Errorf("literal RATE malformado: %q", lit.Value)
	}
	numPart, unit := lit.Value[:idx], lit.Value[idx+1:]
	count, err = strconv.ParseInt(numPart, 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("literal RATE %q: %w", lit.Value, err)
	}
	nanos, ok := rateLimitPeriodNanos[unit]
	if !ok {
		return 0, "", fmt.Errorf("literal RATE %q: unidade de tempo desconhecida %q", lit.Value, unit)
	}
	return count, fmt.Sprintf("time.Duration(%d)", nanos), nil
}

// --- 2. O plano resolvido de UMA rota. ---

// rateLimitRule é UMA dimensão já resolvida (contagem + período Go) — ou a
// entrada flat de uma rota, ou uma linha de um RateLimitTier.
type rateLimitRule struct {
	dim      string // "perIp"/"perUser"/"perTenant"/"perApiKey"/"global"
	count    int64
	periodGo string
}

// tierRateLimitPlan é UM RateLimitTierDecl do programa já resolvido — usado
// só quando routeRateLimitPlan.byTier.
type tierRateLimitPlan struct {
	name  string
	rules []rateLimitRule
}

// routeRateLimitPlan é a config de rate limit já resolvida (Interface +
// rota + mod.ds + tiers) de UMA rota — nil (via planRouteRateLimit) quando
// a rota não declara "rateLimit" em lugar nenhum (nem a Interface, nem a
// própria rota): o caso comum, nenhuma mudança de comportamento (mesmo
// contrato de queryCachePlan/usecaseIdempotencyPlan, G3/G2).
type routeRateLimitPlan struct {
	algorithm string // "token_bucket"/"sliding_window"/"fixed_window"
	failOpen  bool
	burstGo   string // "" quando burst não foi declarado em lugar nenhum (NewLimiter resolve o default sozinho)
	byTier    bool
	rules     []rateLimitRule     // dimensões flat — vazio quando byTier
	tiers     []tierRateLimitPlan // um por RateLimitTierDecl do programa, em ordem alfabética — vazio quando !byTier
}

// rateLimitRulesFromDims resolve o subconjunto de dims presente, na ordem
// canônica rateLimitDimensionOrder (determinismo, NFR-13).
func rateLimitRulesFromDims(dims map[string]*ast.Literal) ([]rateLimitRule, error) {
	var rules []rateLimitRule
	for _, dim := range rateLimitDimensionOrder {
		lit, ok := dims[dim]
		if !ok {
			continue
		}
		count, periodGo, err := parseRateLiteral(lit)
		if err != nil {
			return nil, fmt.Errorf("rateLimit.%s: %w", dim, err)
		}
		rules = append(rules, rateLimitRule{dim: dim, count: count, periodGo: periodGo})
	}
	return rules, nil
}

// rateLimitRulesFromTierEntries resolve as entries de UM RateLimitTierDecl
// (ex. "perUser: 100/min, perTenant: 1000/min", spec §16/§17) — mesma forma
// de rateLimitRulesFromDims, mas a partir de []ast.ConfigEntry cru (um
// RateLimitTierDecl não embrulha suas entries num objeto "rateLimit").
func rateLimitRulesFromTierEntries(entries []ast.ConfigEntry) ([]rateLimitRule, error) {
	dims := make(map[string]*ast.Literal, len(entries))
	for _, e := range entries {
		if !rateLimitDimensionNames[e.Key] {
			return nil, fmt.Errorf("chave desconhecida %q (RateLimitTier só aceita perIp/perUser/perTenant/perApiKey/global, spec §16)", e.Key)
		}
		lit, ok := e.Value.(*ast.Literal)
		if !ok || lit.Kind != token.RATE {
			return nil, fmt.Errorf("%s: esperava um literal RATE, veio %T", e.Key, e.Value)
		}
		dims[e.Key] = lit
	}
	return rateLimitRulesFromDims(dims)
}

// collectRateLimitTiers colhe TODO RateLimitTierDecl do programa (spec
// §16/§17: "RateLimitTier Name { ... }" é um decl de TOPO, não escopado a
// um módulo — como um Aggregate/Command, pode viver em qualquer arquivo) —
// devolve o map por nome e a lista de nomes em ordem alfabética
// (determinismo, NFR-13: toda rota "byTier" enumera os tiers nessa MESMA
// ordem).
func collectRateLimitTiers(prog *program.Program) (map[string]*ast.RateLimitTierDecl, []string) {
	tiers := make(map[string]*ast.RateLimitTierDecl)
	if prog == nil {
		return tiers, nil
	}
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if t, ok := d.(*ast.RateLimitTierDecl); ok {
				tiers[t.Name] = t
			}
		}
	}
	names := make([]string, 0, len(tiers))
	for name := range tiers {
		names = append(names, name)
	}
	sort.Strings(names)
	return tiers, names
}

// planRouteRateLimit resolve o rate limiting de UMA rota: o default da
// Interface (iface, pode ser nil) mergeado com o override da própria rota
// (mergeRateLimitConfig), o algoritmo/onBackendFailure do mod.ds do módulo
// ALVO da rota (mod — o mesmo "infra por módulo" de Idempotency/Cache,
// G2/G3), e — só quando o resultado é "byTier" — os RateLimitTier do
// programa (tiers/tierNames, ver collectRateLimitTiers). Devolve (nil, nil)
// quando não há "rateLimit" configurado em lugar nenhum.
func planRouteRateLimit(iface *ast.InterfaceDecl, route *ast.Route, mod *program.Module, tiers map[string]*ast.RateLimitTierDecl, tierNames []string) (*routeRateLimitPlan, error) {
	var ifaceCfg *rateLimitConfig
	if iface != nil {
		var err error
		ifaceCfg, err = parseRateLimitEntry(iface.Settings)
		if err != nil {
			return nil, fmt.Errorf("Interface %s: %w", iface.Kind, err)
		}
	}
	routeCfg, err := parseRateLimitEntry(route.Options)
	if err != nil {
		return nil, fmt.Errorf("rota %s %q: %w", route.Method, route.Path, err)
	}
	cfg := mergeRateLimitConfig(ifaceCfg, routeCfg)
	if cfg == nil {
		return nil, nil
	}

	modBlock := moduleRateLimitBlock(mod)
	algorithm, err := resolveRateLimitAlgorithm(modBlock)
	if err != nil {
		return nil, err
	}
	failOpen, err := resolveRateLimitFailOpen(modBlock, cfg)
	if err != nil {
		return nil, err
	}
	burstGo := ""
	if cfg.burst != nil {
		n, perr := strconv.ParseInt(cfg.burst.Value, 10, 64)
		if perr != nil {
			return nil, fmt.Errorf("rateLimit.burst: %q: %w", cfg.burst.Value, perr)
		}
		burstGo = strconv.FormatInt(n, 10)
	}

	plan := &routeRateLimitPlan{algorithm: algorithm, failOpen: failOpen, burstGo: burstGo, byTier: cfg.byTier}
	if !cfg.byTier {
		rules, rerr := rateLimitRulesFromDims(cfg.dims)
		if rerr != nil {
			return nil, fmt.Errorf("rota %s %q: %w", route.Method, route.Path, rerr)
		}
		if len(rules) == 0 {
			return nil, fmt.Errorf("rota %s %q: rateLimit declarado sem nenhuma dimensão (perIp/perUser/perTenant/perApiKey/global, spec §16)", route.Method, route.Path)
		}
		plan.rules = rules
		return plan, nil
	}

	if len(tierNames) == 0 {
		return nil, fmt.Errorf("rota %s %q: \"rateLimit: byTier\" mas nenhuma RateLimitTier foi declarada no programa (spec §16)", route.Method, route.Path)
	}
	for _, name := range tierNames {
		rules, rerr := rateLimitRulesFromTierEntries(tiers[name].Entries)
		if rerr != nil {
			return nil, fmt.Errorf("RateLimitTier %s: %w", name, rerr)
		}
		if len(rules) == 0 {
			return nil, fmt.Errorf("RateLimitTier %s: nenhuma dimensão declarada (spec §16)", name)
		}
		plan.tiers = append(plan.tiers, tierRateLimitPlan{name: name, rules: rules})
	}
	return plan, nil
}

// --- 3. Emissão: vars de limitador + função de checagem por rota. ---

// rateLimitDimSuffix devolve o fragmento PascalCase de uma dimensão —
// "perIp" -> "PerIp" — usado para compor nomes de var determinísticos e
// legíveis (ex. "performDepositPerIpLimiter").
func rateLimitDimSuffix(dim string) string {
	if dim == "" {
		return ""
	}
	return strings.ToUpper(dim[:1]) + dim[1:]
}

// rateLimitBaseName deriva o prefixo Go (minúsculo-inicial) do Target de uma
// rota para nomear seus limitadores/checagens — "PerformDeposit" ->
// "performDeposit" — desambiguado por used (mesmo padrão de aliasBase,
// codegen/emit/file.go) no raro caso de duas rotas com rate limiting
// apontando para o MESMO Target.
func rateLimitBaseName(target string, used map[string]int) string {
	base := target
	if base != "" {
		base = strings.ToLower(base[:1]) + base[1:]
	}
	used[base]++
	if n := used[base]; n > 1 {
		base = fmt.Sprintf("%s%d", base, n)
	}
	return base
}

// pendingRateLimitPlan é o que emitRoute/emitUseCaseRoute/emitQueryRoute
// ACUMULAM (em vez de emitir na hora): as vars de limitador e a função
// "<base>RateLimitChecks" de uma rota são declarações de PACOTE, mas
// emitRoute roda de DENTRO do corpo de func newMux (codegen.go) — emiti-las
// ali as aninharia dentro de outra func, Go inválido. O chamador
// (emitHTTPRoutes) devolve a lista acumulada; generateCmdMainFile
// (codegen.go) as renderiza de volta no nível de pacote, DEPOIS que o bloco
// de newMux fecha — exatamente a mesma dança que emitHTTPHelpers já faz
// para devCallerFromRequest/writeBusinessError.
type pendingRateLimitPlan struct {
	baseName string
	plan     *routeRateLimitPlan
	describe string // "POST /path -> Target", para os comentários gerados
}

// httpRateLimitEnv agrupa o que emitRoute/emitUseCaseRoute/emitQueryRoute
// precisam para resolver e enfileirar o rate limiting de uma rota (G4) —
// agrupado numa struct para não inflar ainda mais as assinaturas dessas
// funções (E9.2 já as deixou compridas).
type httpRateLimitEnv struct {
	prog      *program.Program
	iface     *ast.InterfaceDecl
	tiers     map[string]*ast.RateLimitTierDecl
	tierNames []string
	used      map[string]int
	pending   *[]pendingRateLimitPlan
}

// newHTTPRateLimitEnv monta o ambiente compartilhado por TODAS as rotas de
// uma Interface — collectRateLimitTiers roda uma ÚNICA vez (não por rota).
func newHTTPRateLimitEnv(prog *program.Program, iface *ast.InterfaceDecl) *httpRateLimitEnv {
	tiers, tierNames := collectRateLimitTiers(prog)
	return &httpRateLimitEnv{
		prog: prog, iface: iface, tiers: tiers, tierNames: tierNames,
		used: make(map[string]int), pending: &[]pendingRateLimitPlan{},
	}
}

// resolveAndQueue resolve o plano de rate limit de route (módulo alvo
// targetModule — o mesmo módulo cujo mod.ds RateLimit{} dá o default de
// algoritmo/onBackendFailure, mesmo "infra por módulo" de Idempotency/
// Cache) e, se houver algum, enfileira as declarações de pacote
// pendentes e devolve o nome (já único) da função de checagem para o
// chamador referenciar na hora (o handler ainda está sendo montado,
// aninhado dentro de newMux — ver a doc de pendingRateLimitPlan). ""
// (sem erro) quando a rota não configura rate limiting.
func (env *httpRateLimitEnv) resolveAndQueue(route *ast.Route, targetModule string) (checksFuncName string, err error) {
	mod := programModule(env.prog, targetModule)
	plan, err := planRouteRateLimit(env.iface, route, mod, env.tiers, env.tierNames)
	if err != nil {
		return "", err
	}
	if plan == nil {
		return "", nil
	}
	base := rateLimitBaseName(route.Target, env.used)
	describe := fmt.Sprintf("%s %q -> %s", route.Method, route.Path, route.Target)
	*env.pending = append(*env.pending, pendingRateLimitPlan{baseName: base, plan: plan, describe: describe})
	return base + "RateLimitChecks", nil
}

// emitRateLimitCheckAppend emite UMA linha "checks = append(checks,
// runtime.RateLimitCheck{...})" para a dimensão dim, embrulhada na checagem
// de disponibilidade de identidade apropriada quando dim depende de
// contexto que pode faltar NESTA requisição (perUser/perTenant/perApiKey —
// ver a doc do arquivo: "omitida, nunca um erro"). perIp/global estão
// sempre disponíveis (vêm direto da requisição), por isso nunca são
// condicionais.
func emitRateLimitCheckAppend(e *emit.Emitter, dim, varName, runtimeAlias string, failOpen bool) {
	appendLine := func() {
		e.Line("checks = append(checks, %s.RateLimitCheck{Limiter: %s, Key: %s, FailOpen: %t})", runtimeAlias, varName, rateLimitKeyExpr(dim), failOpen)
	}
	switch dim {
	case "perIp", "global":
		appendLine()
	case "perUser":
		e.Block(fmt.Sprintf("if caller, ok := %s.CallerFrom(ctx); ok && caller.Authenticated()", runtimeAlias), appendLine)
	case "perTenant":
		e.Block(fmt.Sprintf("if tenant, ok := %s.TenantFrom(ctx); ok", runtimeAlias), appendLine)
	case "perApiKey":
		e.Block(`if key := rateLimitAPIKey(r); key != ""`, appendLine)
	}
}

// rateLimitKeyExpr devolve a expressão Go que extrai a chave (identidade)
// de dim DENTRO do escopo que emitRateLimitCheckAppend já abriu (ex.
// "caller.ID()" dentro do "if caller, ok := ..." de perUser).
func rateLimitKeyExpr(dim string) string {
	switch dim {
	case "perIp":
		return "rateLimitClientIP(r)"
	case "global":
		return `""`
	case "perUser":
		return "caller.ID()"
	case "perTenant":
		return "tenant.ID"
	case "perApiKey":
		return "key"
	default:
		return `""`
	}
}

// emitRateLimitLimiterVar emite "var <varName> runtime.Limiter =
// runtime.NewLimiter(...)" para UMA regra de dimensão já resolvida.
func emitRateLimitLimiterVar(e *emit.Emitter, varName, describe, algoLit, burstGo, runtimeAlias string, rule rateLimitRule) {
	e.Line("")
	e.Line("// %s é o limitador %s de %s (spec §16, G4).", varName, rule.dim, describe)
	e.Line("var %s %s.Limiter = %s.NewLimiter(%s, %d, %s, %s)", varName, runtimeAlias, runtimeAlias, algoLit, rule.count, rule.periodGo, burstGo)
}

// emitRouteRateLimitChecks emite, no nível de PACOTE (ver a doc de
// pendingRateLimitPlan sobre por que isso roda depois de newMux fechar), a
// var de limitador de cada dimensão configurada (flat) ou de cada
// (tier, dimensão) — byTier — e a função "<baseName>RateLimitChecks(ctx,
// r) []runtime.RateLimitCheck" que emitUseCaseRoute/emitQueryRoute já
// chamou pelo nome (resolveAndQueue) enquanto montava o handler.
func emitRouteRateLimitChecks(e *emit.Emitter, p pendingRateLimitPlan, runtimeAlias, httpAlias string) {
	ctxAlias := e.Import("context")
	algoLit := strconv.Quote(p.plan.algorithm)
	burstGo := p.plan.burstGo
	if burstGo == "" {
		burstGo = "0"
	}
	funcName := p.baseName + "RateLimitChecks"

	if p.plan.byTier {
		emitByTierRateLimitChecks(e, p, funcName, algoLit, burstGo, runtimeAlias, httpAlias, ctxAlias)
		return
	}
	emitFlatRateLimitChecks(e, p, funcName, algoLit, burstGo, runtimeAlias, httpAlias, ctxAlias)
}

// emitFlatRateLimitChecks é o caso comum (não byTier): uma var de
// limitador por dimensão configurada, e a função de checagem monta
// []runtime.RateLimitCheck append-ando cada uma (pulando as que não têm
// identidade disponível, ver emitRateLimitCheckAppend).
func emitFlatRateLimitChecks(e *emit.Emitter, p pendingRateLimitPlan, funcName, algoLit, burstGo, runtimeAlias, httpAlias, ctxAlias string) {
	varNames := make(map[string]string, len(p.plan.rules))
	for _, rule := range p.plan.rules {
		varName := p.baseName + rateLimitDimSuffix(rule.dim) + "Limiter"
		varNames[rule.dim] = varName
		emitRateLimitLimiterVar(e, varName, p.describe, algoLit, burstGo, runtimeAlias, rule)
	}

	e.Line("")
	e.Line("// %s monta as dimensões de rate limit de %s (spec §16, G4).", funcName, p.describe)
	sig := fmt.Sprintf("func %s(ctx %s.Context, r *%s.Request) []%s.RateLimitCheck", funcName, ctxAlias, httpAlias, runtimeAlias)
	e.Block(sig, func() {
		e.Line("var checks []%s.RateLimitCheck", runtimeAlias)
		for _, rule := range p.plan.rules {
			emitRateLimitCheckAppend(e, rule.dim, varNames[rule.dim], runtimeAlias, p.plan.failOpen)
		}
		e.Line("return checks")
	})
}

// emitByTierRateLimitChecks é o caso "rateLimit: byTier" (spec §16): uma
// var de limitador por (RateLimitTier, dimensão declarada naquele tier), e
// a função de checagem resolve o tier de tenant.Tier (runtime.TenantFrom)
// — sem tenant/tier resolvido, devolve nil (nenhuma dimensão aplicada,
// "rotas sem tenant" generalizado, ver a doc do arquivo) em vez de um erro.
func emitByTierRateLimitChecks(e *emit.Emitter, p pendingRateLimitPlan, funcName, algoLit, burstGo, runtimeAlias, httpAlias, ctxAlias string) {
	tierVarNames := make(map[string]map[string]string, len(p.plan.tiers)) // tier -> dim -> varName
	for _, tp := range p.plan.tiers {
		vars := make(map[string]string, len(tp.rules))
		for _, rule := range tp.rules {
			varName := p.baseName + tp.name + rateLimitDimSuffix(rule.dim) + "Limiter"
			vars[rule.dim] = varName
			emitRateLimitLimiterVar(e, varName, fmt.Sprintf("%s (tier %s)", p.describe, tp.name), algoLit, burstGo, runtimeAlias, rule)
		}
		tierVarNames[tp.name] = vars
	}

	e.Line("")
	e.Line("// %s resolve o RateLimitTier de tenant.Tier (spec §16: \"byTier\") e monta", funcName)
	e.Line("// as dimensões QUE ESSE TIER declara — sem tenant/tier resolvido, ou com um")
	e.Line("// nome de tier desconhecido, devolve nil: nenhuma dimensão de tier se")
	e.Line("// aplica a esta requisição (mesmo princípio de \"rotas sem tenant só perIp\"")
	e.Line("// generalizado, ver a doc do arquivo) em vez de um erro.")
	sig := fmt.Sprintf("func %s(ctx %s.Context, r *%s.Request) []%s.RateLimitCheck", funcName, ctxAlias, httpAlias, runtimeAlias)
	e.Block(sig, func() {
		e.Line("tenant, ok := %s.TenantFrom(ctx)", runtimeAlias)
		e.Block("if !ok || tenant.Tier == \"\"", func() {
			e.Line("return nil")
		})
		e.Line("switch tenant.Tier {")
		for _, tp := range p.plan.tiers {
			// Cada "case" ganha seu próprio escopo automaticamente (regra do
			// Go) — não precisa de "{ }" explícito para "checks" não colidir
			// entre tiers diferentes.
			e.Line("case %s:", strconv.Quote(tp.name))
			e.Line("var checks []%s.RateLimitCheck", runtimeAlias)
			for _, rule := range tp.rules {
				emitRateLimitCheckAppend(e, rule.dim, tierVarNames[tp.name][rule.dim], runtimeAlias, p.plan.failOpen)
			}
			e.Line("return checks")
		}
		e.Line("default:")
		e.Line("return nil")
		e.Line("}")
	})
}

// --- 4. Helpers HTTP compartilhados (identidade + resposta 429). ---

// emitRateLimitHelpers emite, como declarações de PACOTE (mesmo momento de
// emitHTTPHelpers — ver codegen.go/generateCmdMainFile), os helpers
// compartilhados por TODA rota com rate limiting deste arquivo: extração de
// identidade (perIp/perApiKey — perUser/perTenant já vêm prontos de
// runtime.Caller/Tenant) e o adapter HTTP de resposta (headers
// "X-RateLimit-*" e o 429 + "Retry-After", spec §16). Chamado no máximo 1
// vez por cmd/<group>/main.go, só quando ao menos UMA rota de fato
// configura "rateLimit" (mesmo padrão condicional de emitHTTPHelpers).
func emitRateLimitHelpers(e *emit.Emitter, runtimeAlias, httpAlias string) {
	netAlias := e.Import("net")
	stringsAlias := e.Import("strings")
	strconvAlias := e.Import("strconv")
	timeAlias := e.Import("time")

	e.Line("")
	e.Line("// rateLimitClientIP extrai o identificador de rede do chamador para a")
	e.Line("// dimensão perIp (spec §16): prioriza \"X-Forwarded-For\" (1º endereço da")
	e.Line("// lista — convenção padrão de proxy reverso) e cai para r.RemoteAddr (só o")
	e.Line("// host, sem a porta) na ausência do header. Nenhum dos dois é validado")
	e.Line("// contra spoofing — confiar (ou não) num proxy é decisão de deploy, fora do")
	e.Line("// escopo deste gerador.")
	e.Block(fmt.Sprintf("func rateLimitClientIP(r *%s.Request) string", httpAlias), func() {
		e.Block(`if fwd := r.Header.Get("X-Forwarded-For"); fwd != ""`, func() {
			e.Block(fmt.Sprintf("if i := %s.IndexByte(fwd, ','); i >= 0", stringsAlias), func() {
				e.Line("fwd = fwd[:i]")
			})
			e.Line("return %s.TrimSpace(fwd)", stringsAlias)
		})
		e.Block(fmt.Sprintf("if host, _, err := %s.SplitHostPort(r.RemoteAddr); err == nil", netAlias), func() {
			e.Line("return host")
		})
		e.Line("return r.RemoteAddr")
	})

	e.Line("")
	e.Line("// rateLimitAPIKey extrai a chave de API para a dimensão perApiKey (spec")
	e.Line("// §16) do header \"X-Api-Key\" — placeholder documentado até o projeto ter")
	e.Line("// um sistema de autenticação por API key de verdade (nenhum existe ainda;")
	e.Line("// mesmo espírito do X-Caller-Id de dev, devCallerFromRequest acima).")
	e.Block(fmt.Sprintf("func rateLimitAPIKey(r *%s.Request) string", httpAlias), func() {
		e.Line(`return r.Header.Get("X-Api-Key")`)
	})

	e.Line("")
	e.Line("// writeRateLimitHeaders escreve os headers \"X-RateLimit-*\" (spec §16) a")
	e.Line("// partir do resultado MAIS RESTRITIVO entre as dimensões checadas")
	e.Line("// (runtime.CheckRateLimits já resolve qual) — emitido tanto no caminho de")
	e.Line("// sucesso quanto no 429, para o chamador sempre poder ver sua cota.")
	e.Block(fmt.Sprintf("func writeRateLimitHeaders(w %s.ResponseWriter, res %s.RateLimitResult)", httpAlias, runtimeAlias), func() {
		e.Line(`w.Header().Set("X-RateLimit-Limit", %s.Itoa(res.Limit))`, strconvAlias)
		e.Line(`w.Header().Set("X-RateLimit-Remaining", %s.Itoa(res.Remaining))`, strconvAlias)
		e.Line(`w.Header().Set("X-RateLimit-Reset", %s.FormatInt(res.ResetAt.Unix(), 10))`, strconvAlias)
	})

	e.Line("")
	e.Line("// writeRateLimitExceeded escreve a resposta automática do spec §16: os")
	e.Line("// mesmos headers X-RateLimit-* de sempre, mais \"Retry-After\" (segundos,")
	e.Line("// arredondado para cima) e 429.")
	e.Block(fmt.Sprintf("func writeRateLimitExceeded(w %s.ResponseWriter, res %s.RateLimitResult)", httpAlias, runtimeAlias), func() {
		e.Line("writeRateLimitHeaders(w, res)")
		e.Line("retryAfterSeconds := int64(res.RetryAfter / %s.Second)", timeAlias)
		e.Block(fmt.Sprintf("if res.RetryAfter%%%s.Second != 0", timeAlias), func() {
			e.Line("retryAfterSeconds++")
		})
		e.Block("if retryAfterSeconds < 0", func() {
			e.Line("retryAfterSeconds = 0")
		})
		e.Line(`w.Header().Set("Retry-After", %s.FormatInt(retryAfterSeconds, 10))`, strconvAlias)
		e.Line(`%s.Error(w, "rate limit exceeded", %s.StatusTooManyRequests)`, httpAlias, httpAlias)
	})
}
