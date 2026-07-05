package codegen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// versioning.go emite o versionamento de API na borda HTTP (G6, REQ-28.4,
// spec §17): "versioning { strategy/current/supported }" na Interface +
// "Version vN { deprecated/sunset/upcast/downcast/route }" (versions/*.ds —
// o diretório herda o módulo do mod.ds mais próximo, ModuleOf, exatamente
// como contracts/; ver a doc de moduleBucket.versions, codegen.go).
//
// --- Onde a config mora ---
//
// "versioning" (ast.InterfaceDecl.Settings) é o ÚNICO gatilho — uma Interface
// SEM esse bloco (wallet/shop, hoje) nunca chama parseInterfaceVersioningPlan
// em nada além de devolver (nil, nil), e TODA linha de versionamento
// (apiVersionGate, upcast/downcast, override de rota) fica OMITIDA: o Go
// gerado permanece byte a byte igual ao de antes de G6 (mesmo princípio de
// G5, ver a doc de parseInterfaceTenantPlan, http.go). "Version vN { ... }" é
// um Decl de topo comum (parser/parse_decl.go, token.VERSION) — coletado por
// bucketModuleDecls em b.versions; collectVersionDecls os reconsulta por
// GRUPO de service (todos os módulos de um cmd/<service>, o mesmo escopo de
// httpRateLimitEnv/httpTenantPlan).
//
// --- Estratégia de resolução de versão (spec §17) ---
//
// Só "strategy: header(\"Nome\")" é suportado por este gerador.
// "strategy: path" (o exemplo PRIMÁRIO do spec, um prefixo "/v1/..." na
// URL) exigiria registrar múltiplos padrões de rota por endpoint versionado
// e despachar para um handler parametrizado pela versão — net/http.ServeMux
// (Go 1.22+) não tem roteamento por prefixo dinâmico nativo, e nenhuma
// fixture real fixou essa convenção ainda; erro de geração claro em vez de
// um extrator que nunca resolveria nada (mesma postura de
// parseInterfaceTenantPlan sobre "tenant { from: path }"/"jwt_claim(...)",
// http.go, G5).
//
// --- Sunset/deprecated (a espinha dorsal desta task) ---
//
// apiVersionGate (emitVersioningHelpers) roda no INÍCIO de todo handler
// versionado — ANTES até de emitCallerAndIdempotency (ver a doc de
// emitUseCaseRoute/emitQueryRoute, http.go): um cliente numa versão sunset
// nunca paga o custo de autenticação/tenant/rate-limit, e o handler de
// domínio (UseCase/Query) NUNCA roda (410 Gone imediato). Deprecated (sem
// sunset ainda, ou antes da data de sunset) só acrescenta os headers
// "Deprecation"/"Sunset" e segue normal.
//
// --- Versionamento esparso (spec §17: "endpoints inalterados passam
// direto") ---
//
// Uma rota cujo Command (UseCase) ou View (Query) não tem NENHUM upcast/
// downcast declarado em NENHUMA Version, e cujo Path não tem NENHUM
// VersionRoute, não ganha upcast/downcast/override algum — só o gate de
// sunset/deprecated (comum a toda rota quando a Interface declara
// versioning). Isso é o "esparso": a maioria das rotas de um domínio real
// não muda entre versões, e o Go gerado para elas reflete exatamente isso —
// zero branches de tradução.
//
// --- Upcast (request antiga -> Command atual) ---
//
// emitCommandUpcast gera, por (Command, versão), no PACOTE DE DOMÍNIO do
// Command (emitModuleAPIVersions, chamado por generateModuleFiles — nunca em
// cmd/<group>/main.go, ver a nota de arquitetura em httpVersioningEnv mais
// abaixo): (a) a struct "<Cmd><Ver>Request" (um campo por VersionUpcast.From,
// na ordem declarada) e (b) "Upcast<Cmd><Ver>(raw) (<Cmd>, error)", cujo corpo
// loweriza VersionUpcast.To (uma lista de "Nome = Valor", igual a
// ProjectionDecl.Map) via o MESMO mecanismo de hoisting de statement usado
// por Handle/UseCase/Operator (lower.StmtLowerer, E5.2): cada entrada vira
// um AssignStmt sintético sobre um Ident nu (o nome do campo do Command,
// convenção nu — igual a Operator de VO, NÃO "self."), o que faz a
// construção de VO composto (ex. "amount = Money(amount: v, currency: c)",
// o exemplo canônico do spec §17) passar pelo caminho validado (NewX +
// propagação de erro), exatamente como o corpo de um UseCase faria. Um campo
// do Command NÃO atribuído em "to" fica com o zero value Go — só acontece
// com campos que têm default (REQ-5.13/sema.checkVersionUpcastDefaults já
// barra um campo obrigatório sem default e sem atribuição, então isso nunca
// perde dado silenciosamente sobre um programa válido).
//
// --- Downcast (View atual -> response antiga) ---
//
// emitViewDowncast gera, por (View, versão), no MESMO pacote de domínio da
// View: a struct "<View><Ver>Response" e "Downcast<View><Ver>(v)
// <View><Ver>Response", cujo corpo loweriza
// VersionDowncast.To sobre um TypeEnv que semeia "self" como o PRÓPRIO valor
// da View (mesmo padrão de Sources em decl_projection.go) — mais simples que
// o upcast: como o spec só usa acesso a membro simples ("self.balance_amount"),
// NÃO precisa do mecanismo de hoisting (Lowerer.Expr basta, igual a
// projectionFieldInfos); uma construção de VO composto dentro de um downcast
// não é suportada por esta task (mesma limitação documentada de Projection).
//
// --- VersionRoute (mudança de comportamento, UseCase distinto) ---
//
// route "<path>" -> UseCase (VersionRoute) NÃO é tradução de shape — é uma
// substituição INTEIRA do alvo para uma versão específica (spec §17: "Mudança
// de comportamento não é versionável por shape"). emitVersionRouteOverrides
// emite, logo após o gate de sunset/deprecated (ANTES de qualquer outro
// gate/decode da rota BASE), um switch por versão cujo case dispatcha para o
// UseCase de override de forma INTEIRAMENTE auto-contida (seu próprio
// caller/decode/path-params/dispatch — emitVersionRouteOverrideBody) e
// retorna, ignorando tenant/rate-limit/idempotência/upcast da rota base
// (escopo documentado: um cliente legado numa rota de comportamento raramente
// precisa das MESMAS políticas de borda da rota atual). Só suportado quando o
// alvo BASE da rota é um UseCase (o alvo do override sempre é, per spec) —
// uma rota cujo alvo base é uma Query recusa com erro de geração claro
// (emitQueryRoute, http.go) em vez de ignorar silenciosamente.

// --- 1. "versioning { strategy/current/supported }" (Interface.Settings). ---

// httpVersioningPlan é a config "versioning { ... }" de UMA Interface (§17),
// já resolvida — nil (via parseInterfaceVersioningPlan) quando a Interface
// não declara "versioning" (o caso comum, ver a doc do arquivo).
type httpVersioningPlan struct {
	header  string // nome do header (única estratégia suportada — ver a doc do arquivo)
	current string // versão corrente (ex. "v2")
	// supported é parseado e validado sintaticamente (rejeita "supported: ..."
	// malformado) mas NÃO é aplicado como allowlist em runtime nesta task —
	// uma versão fora da lista ainda passa (resolveAPIVersion não valida
	// contra ela; só apiVersionSunset/apiVersionDeprecated, indexados por
	// Version DECLARADA, têm efeito real). Escopo documentado: rejeitar
	// versão desconhecida exigiria decidir um status/erro que o spec não
	// especifica; "supported" aqui é, por ora, só metadado de documentação do
	// domínio.
	supported []string
}

// parseInterfaceVersioningPlan lê iface.Settings["versioning"] — nil, nil
// quando a chave está ausente. Forma reconhecida (§17): "strategy:
// header(\"Nome\"), current: vN, supported: [v1, v2, ...]". "strategy: path"
// é sintaxe reconhecida do spec mas SEM implementação nesta task (ver a doc
// do arquivo) — erro de geração claro.
func parseInterfaceVersioningPlan(iface *ast.InterfaceDecl) (*httpVersioningPlan, error) {
	if iface == nil {
		return nil, nil
	}
	var raw ast.Expr
	found := false
	for _, e := range iface.Settings {
		if e.Key == "versioning" {
			raw, found = e.Value, true
		}
	}
	if !found {
		return nil, nil
	}
	obj, ok := raw.(*ast.ObjectExpr)
	if !ok {
		return nil, fmt.Errorf("Interface %s: %q: esperava um objeto (\"versioning { strategy: ..., current: ..., supported: [...] }\", spec §17), veio %T", iface.Kind, "versioning", raw)
	}

	var strategyExpr, currentExpr, supportedExpr ast.Expr
	for _, ce := range obj.Entries {
		switch ce.Key {
		case "strategy":
			strategyExpr = ce.Value
		case "current":
			currentExpr = ce.Value
		case "supported":
			supportedExpr = ce.Value
		default:
			return nil, fmt.Errorf("Interface %s: versioning: chave desconhecida %q (spec §17: strategy/current/supported)", iface.Kind, ce.Key)
		}
	}
	if strategyExpr == nil {
		return nil, fmt.Errorf("Interface %s: versioning: falta \"strategy:\" (spec §17)", iface.Kind)
	}
	header, err := versioningStrategyHeader(iface, strategyExpr)
	if err != nil {
		return nil, err
	}
	if currentExpr == nil {
		return nil, fmt.Errorf("Interface %s: versioning: falta \"current:\" (spec §17)", iface.Kind)
	}
	current, err := versionLiteralValue(currentExpr)
	if err != nil {
		return nil, fmt.Errorf("Interface %s: versioning.current: %w", iface.Kind, err)
	}

	var supported []string
	if supportedExpr != nil {
		list, ok := supportedExpr.(*ast.ListExpr)
		if !ok {
			return nil, fmt.Errorf("Interface %s: versioning.supported: esperava uma lista de versões, veio %T", iface.Kind, supportedExpr)
		}
		for _, el := range list.Elems {
			v, verr := versionLiteralValue(el)
			if verr != nil {
				return nil, fmt.Errorf("Interface %s: versioning.supported: %w", iface.Kind, verr)
			}
			supported = append(supported, v)
		}
	}

	return &httpVersioningPlan{header: header, current: current, supported: supported}, nil
}

// versioningStrategyHeader resolve "strategy: header(\"Nome\")" para o nome
// do header — a ÚNICA estratégia suportada (ver a doc do arquivo).
func versioningStrategyHeader(iface *ast.InterfaceDecl, e ast.Expr) (string, error) {
	switch v := e.(type) {
	case *ast.CallExpr:
		fn, ok := v.Fn.(*ast.Ident)
		if !ok || fn.Name != "header" || len(v.Args) != 1 {
			return "", fmt.Errorf("Interface %s: versioning.strategy: forma de chamada não reconhecida (esperava header(\"Nome\"), spec §17)", iface.Kind)
		}
		lit, ok := v.Args[0].Value.(*ast.Literal)
		if !ok || lit.Kind != token.STRING {
			return "", fmt.Errorf("Interface %s: versioning.strategy: header(...) espera um único literal string", iface.Kind)
		}
		return lit.Value, nil
	case *ast.Ident:
		if v.Name == "path" {
			return "", fmt.Errorf("Interface %s: versioning { strategy: path }: resolução de versão por prefixo de path não é suportada por este gerador (G6) — exigiria registrar múltiplos padrões de rota por endpoint versionado e net/http.ServeMux não tem roteamento por prefixo dinâmico nativo; use \"strategy: header(...)\" (spec §17, forma alternativa) — mesma postura de parseInterfaceTenantPlan sobre \"tenant { from: path }\" (http.go, G5)", iface.Kind)
		}
		return "", fmt.Errorf("Interface %s: versioning.strategy: identificador não reconhecido %q (spec §17: path/header(...))", iface.Kind, v.Name)
	default:
		return "", fmt.Errorf("Interface %s: versioning.strategy: forma não reconhecida (%T)", iface.Kind, e)
	}
}

// versionLiteralValue devolve o lexema de um literal VERSIONID (ex. "v1").
func versionLiteralValue(e ast.Expr) (string, error) {
	lit, ok := e.(*ast.Literal)
	if !ok || lit.Kind != token.VERSIONID {
		return "", fmt.Errorf("esperava um version_id (ex. v1), veio %T", e)
	}
	return lit.Value, nil
}

// --- 2. Coleta de Version (versions/*.ds) e índices por Command/View/Path. ---

// collectVersionDecls reconsulta os VersionDecl já coletados por
// bucketModuleDecls (b.versions) através de TODOS os módulos de modules (o
// grupo de um cmd/<service> — mesmo escopo de httpRateLimitEnv/
// collectRateLimitTiers), ordenados por Version (determinismo, NFR-13).
func collectVersionDecls(buckets map[string]moduleBucket, modules []string) []*ast.VersionDecl {
	var out []*ast.VersionDecl
	for _, m := range modules {
		out = append(out, buckets[m].versions...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out
}

// versionLifecycle é a janela de depreciação de UMA Version, já validada
// (versionDateLiteral) — deprecatedDate/sunsetDate são "" quando a Version
// não declara a chave correspondente (uma Version pode existir só por
// upcast/downcast/route, sem NENHUM lifecycle).
type versionLifecycle struct {
	version        string
	deprecatedDate string // "YYYY-MM-DD", ou ""
	sunsetDate     string // "YYYY-MM-DD", ou ""
}

// buildVersionLifecycles resolve o lifecycle de cada Version (ordenado por
// versão, determinismo).
func buildVersionLifecycles(versions []*ast.VersionDecl) ([]versionLifecycle, error) {
	out := make([]versionLifecycle, 0, len(versions))
	for _, v := range versions {
		lc := versionLifecycle{version: v.Version}
		if v.Deprecated != nil {
			s, err := versionDateLiteral(v.Deprecated)
			if err != nil {
				return nil, fmt.Errorf("Version %s: deprecated: %w", v.Version, err)
			}
			lc.deprecatedDate = s
		}
		if v.Sunset != nil {
			s, err := versionDateLiteral(v.Sunset)
			if err != nil {
				return nil, fmt.Errorf("Version %s: sunset: %w", v.Version, err)
			}
			lc.sunsetDate = s
		}
		out = append(out, lc)
	}
	return out, nil
}

// versionDateLiteral valida um literal string "YYYY-MM-DD" (a ÚNICA forma
// que deprecated/sunset aceitam, spec §17) em tempo de GERAÇÃO — uma data
// malformada é erro de geração claro, nunca um panic em runtime sobre o
// projeto gerado (REQ-14.4).
func versionDateLiteral(e ast.Expr) (string, error) {
	lit, ok := e.(*ast.Literal)
	if !ok || lit.Kind != token.STRING {
		return "", fmt.Errorf("esperava um literal string de data (\"YYYY-MM-DD\", spec §17), veio %T", e)
	}
	if _, err := time.Parse("2006-01-02", lit.Value); err != nil {
		return "", fmt.Errorf("data %q inválida (esperava YYYY-MM-DD, spec §17): %w", lit.Value, err)
	}
	return lit.Value, nil
}

// indexUpcastsByCommand indexa todo VersionUpcast por (Command alvo,
// versão) — usado por httpVersioningEnv.upcastsForCommand.
func indexUpcastsByCommand(versions []*ast.VersionDecl) map[string]map[string]*ast.VersionUpcast {
	idx := make(map[string]map[string]*ast.VersionUpcast)
	for _, v := range versions {
		for _, up := range v.Upcasts {
			if up == nil || up.Target == "" {
				continue
			}
			if idx[up.Target] == nil {
				idx[up.Target] = make(map[string]*ast.VersionUpcast)
			}
			idx[up.Target][v.Version] = up
		}
	}
	return idx
}

// indexDowncastsByView indexa todo VersionDowncast por (View alvo, versão).
func indexDowncastsByView(versions []*ast.VersionDecl) map[string]map[string]*ast.VersionDowncast {
	idx := make(map[string]map[string]*ast.VersionDowncast)
	for _, v := range versions {
		for _, dc := range v.Downcasts {
			if dc == nil || dc.Target == "" {
				continue
			}
			if idx[dc.Target] == nil {
				idx[dc.Target] = make(map[string]*ast.VersionDowncast)
			}
			idx[dc.Target][v.Version] = dc
		}
	}
	return idx
}

// versionRouteOverrideRaw é UM VersionRoute ainda não resolvido a um
// UseCaseDecl concreto — só o texto (versão, path, nome do alvo).
type versionRouteOverrideRaw struct {
	version string
	target  string
}

// indexRouteOverridesByPath indexa todo VersionRoute por Path (uma rota pode
// ter overrides de mais de uma versão).
func indexRouteOverridesByPath(versions []*ast.VersionDecl) map[string][]versionRouteOverrideRaw {
	idx := make(map[string][]versionRouteOverrideRaw)
	for _, v := range versions {
		for _, r := range v.Routes {
			if r == nil || r.Path == "" {
				continue
			}
			idx[r.Path] = append(idx[r.Path], versionRouteOverrideRaw{version: v.Version, target: r.Target})
		}
	}
	return idx
}

// --- 3. httpVersioningEnv: o ambiente compartilhado por TODAS as rotas. ---
//
// Nota de arquitetura: a struct+função Upcast/Downcast em si NÃO são emitidas
// por este ambiente (nem por http.go) — elas moram no PACOTE DE DOMÍNIO do
// Command/View que traduzem (emitModuleAPIVersions, chamado por
// generateModuleFiles, codegen.go), pela MESMA razão que qualquer VO/Command
// mora no pacote do módulo que o declara: o corpo do upcast CONSTRÓI VOs
// (Money{...}, AccountId(...), ...) via o mecanismo normal de lowering
// (lower.Lowerer), que emite nomes de tipo NUS — válido só dentro do PRÓPRIO
// pacote do módulo, nunca de cmd/<group>/main.go (um pacote "main" distinto).
// httpVersioningEnv só decide, por rota, SE um switch por versão é
// necessário (upcastsForCommand/downcastsForView, ambos consultas puras,
// sem side-effect) — o Go de fato chamado (ex. "billing.UpcastChargeCmdV1")
// é montado por emitUseCaseRoute/emitQueryRoute usando o MESMO cmdAlias/
// viewAlias que already resolvem via aliasForSymbol.

// httpVersioningEnv agrupa o que emitUseCaseRoute/emitQueryRoute precisam
// para resolver o versionamento de UMA rota (G6) — mesmo padrão de
// httpRateLimitEnv (codegen/ratelimit.go). plan == nil (Interface sem
// "versioning") faz TODO método devolver zero-values sem erro — nenhuma
// rota muda de comportamento (ver a doc do arquivo).
type httpVersioningEnv struct {
	plan             *httpVersioningPlan
	lifecycles       []versionLifecycle
	upcastIdx        map[string]map[string]*ast.VersionUpcast
	downcastIdx      map[string]map[string]*ast.VersionDowncast
	routeOverrideIdx map[string][]versionRouteOverrideRaw
	buckets          map[string]moduleBucket
	modules          []string
}

// newHTTPVersioningEnv monta o ambiente compartilhado por TODAS as rotas de
// uma Interface — parseInterfaceVersioningPlan roda uma ÚNICA vez (não por
// rota), igual a newHTTPRateLimitEnv.
func newHTTPVersioningEnv(iface *ast.InterfaceDecl, buckets map[string]moduleBucket, modules []string) (*httpVersioningEnv, error) {
	plan, err := parseInterfaceVersioningPlan(iface)
	if err != nil {
		return nil, err
	}
	env := &httpVersioningEnv{plan: plan, buckets: buckets, modules: modules}
	if plan == nil {
		return env, nil
	}
	versions := collectVersionDecls(buckets, modules)
	lifecycles, err := buildVersionLifecycles(versions)
	if err != nil {
		return nil, err
	}
	env.lifecycles = lifecycles
	env.upcastIdx = indexUpcastsByCommand(versions)
	env.downcastIdx = indexDowncastsByView(versions)
	env.routeOverrideIdx = indexRouteOverridesByPath(versions)
	return env, nil
}

// upcastsForCommand devolve o mapa versão->upcast de cmdName — nil quando
// env.plan é nil ou nenhuma Version declara upcast para este Command (o caso
// esparso comum). Consulta pura: a struct+função Upcast<Cmd><Ver> em si já
// foi (ou será) emitida por emitModuleAPIVersions no pacote de domínio do
// Command — ver a nota de arquitetura acima.
func (env *httpVersioningEnv) upcastsForCommand(cmdName string) map[string]*ast.VersionUpcast {
	if env.plan == nil {
		return nil
	}
	return env.upcastIdx[cmdName]
}

// downcastsForView devolve o mapa versão->downcast da View de retorno de uma
// Query (viewRef, o TypeRef de QueryDecl.Return) — nil quando env.plan é nil
// ou nenhuma Version declara downcast para esta View. Consulta pura — ver a
// nota de arquitetura acima.
func (env *httpVersioningEnv) downcastsForView(viewRef *ast.TypeRef) map[string]*ast.VersionDowncast {
	if env.plan == nil || viewRef == nil {
		return nil
	}
	return env.downcastIdx[viewRef.Name]
}

// versionRouteOverride é UM VersionRoute já resolvido a um UseCaseDecl
// concreto (findUseCaseInBuckets) — o Target de VersionRoute SEMPRE é um
// UseCase (spec §17: "Mudança de comportamento" -> "UseCase distinto"), nunca
// uma Query.
type versionRouteOverride struct {
	version string
	usecase *ast.UseCaseDecl
	module  string
}

// routeOverridesFor devolve os VersionRoute cujo Path bate com path,
// ordenados por versão (determinismo) — nil quando env.plan é nil ou nenhuma
// Version declara "route" para este path (o caso esparso comum).
func (env *httpVersioningEnv) routeOverridesFor(path string) ([]versionRouteOverride, error) {
	if env.plan == nil {
		return nil, nil
	}
	raws := env.routeOverrideIdx[path]
	if len(raws) == 0 {
		return nil, nil
	}
	out := make([]versionRouteOverride, 0, len(raws))
	for _, raw := range raws {
		uc, mod, ok := findUseCaseInBuckets(env.buckets, env.modules, raw.target)
		if !ok {
			return nil, fmt.Errorf("Version %s: route %q -> %q não resolve a nenhum UseCase dos módulos do grupo (spec §17: route de versão sempre aponta para um UseCase distinto)", raw.version, path, raw.target)
		}
		out = append(out, versionRouteOverride{version: raw.version, usecase: uc, module: mod})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// findUseCaseInBuckets procura um UseCaseDecl de nome name entre TODOS os
// módulos do grupo (modules, já ordenado — determinismo) — mesmo padrão de
// findCommandInBuckets (http.go).
func findUseCaseInBuckets(buckets map[string]moduleBucket, modules []string, name string) (*ast.UseCaseDecl, string, bool) {
	for _, m := range modules {
		for _, u := range buckets[m].usecases {
			if u.Name == name {
				return u, m, true
			}
		}
	}
	return nil, "", false
}

// versionSuffixGo deriva o sufixo PascalCase de um version_id — "v1" ->
// "V1" — usado para compor nomes determinísticos (ex. "UpcastDepositCmdV1",
// "DepositCmdV1Request").
func versionSuffixGo(version string) string {
	if version == "" {
		return ""
	}
	return strings.ToUpper(version[:1]) + version[1:]
}

// sortedStringKeys devolve as chaves de m em ordem alfabética (determinismo,
// NFR-13) — usado para iterar os mapas versão->upcast/downcast na mesma
// ordem sempre, independente da ordem de declaração dos arquivos Version.
func sortedStringKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- 4. Emissão: gate de sunset/deprecated + as funções Upcast/Downcast. ---

// emitVersioningHelpers emite, como declarações de PACOTE (mesmo momento de
// emitTenantHelpers/emitRateLimitHelpers — ver codegen.go/
// generateCmdMainFile), resolveAPIVersion/apiVersionGate + os mapas de
// lifecycle e mustParseAPIVersionDate (só quando alguma Version de fato
// declara deprecated/sunset). As funções Upcast/Downcast em si NÃO são
// emitidas aqui — elas moram no pacote de domínio do Command/View que
// traduzem (emitModuleAPIVersions, chamado por generateModuleFiles — ver a
// nota de arquitetura em httpVersioningEnv). Chamado no máximo 1 vez por
// cmd/<group>/main.go, só quando a Interface HTTP do grupo de fato declara
// "versioning { ... }" (verEnv.plan != nil — ver a doc do arquivo).
func emitVersioningHelpers(e *emit.Emitter, verEnv *httpVersioningEnv, httpAlias string) error {
	plan := verEnv.plan
	timeAlias := e.Import("time")

	e.Line("")
	e.Line("// resolveAPIVersion extrai a versão de API requisitada (spec §17) do header")
	e.Line("// %s — ausente ou vazio cai para a versão corrente (%s).", strconv.Quote(plan.header), strconv.Quote(plan.current))
	e.Block(fmt.Sprintf("func resolveAPIVersion(r *%s.Request) string", httpAlias), func() {
		e.Line("v := r.Header.Get(%s)", strconv.Quote(plan.header))
		e.Block(`if v == ""`, func() {
			e.Line("return %s", strconv.Quote(plan.current))
		})
		e.Line("return v")
	})

	hasDeprecated, hasSunset := false, false
	for _, lc := range verEnv.lifecycles {
		if lc.deprecatedDate != "" {
			hasDeprecated = true
		}
		if lc.sunsetDate != "" {
			hasSunset = true
		}
	}

	if hasDeprecated || hasSunset {
		e.Line("")
		e.Line("// mustParseAPIVersionDate parseia uma data \"YYYY-MM-DD\" declarada em Version")
		e.Line("// (spec §17) — já validada em tempo de GERAÇÃO (versionDateLiteral), então um")
		e.Line("// erro aqui é bug de geração, não de dado do usuário.")
		e.Block(fmt.Sprintf("func mustParseAPIVersionDate(s string) %s.Time", timeAlias), func() {
			e.Line(`t, err := %s.Parse("2006-01-02", s)`, timeAlias)
			e.Block("if err != nil", func() {
				e.Line("panic(err)")
			})
			e.Line("return t")
		})
	}

	if hasSunset {
		e.Line("")
		e.Line("// apiVersionSunset mapeia versão -> data em que deixou de funcionar (410 Gone")
		e.Line("// a partir dela, spec §17).")
		e.Block(fmt.Sprintf("var apiVersionSunset = map[string]%s.Time", timeAlias), func() {
			for _, lc := range verEnv.lifecycles {
				if lc.sunsetDate != "" {
					e.Line("%s: mustParseAPIVersionDate(%s),", strconv.Quote(lc.version), strconv.Quote(lc.sunsetDate))
				}
			}
		})

		e.Line("")
		e.Line("// apiVersionSunsetHeader formata t no formato HTTP-date (RFC 7231/8594) para o")
		e.Line("// header \"Sunset\".")
		e.Block(fmt.Sprintf("func apiVersionSunsetHeader(t %s.Time) string", timeAlias), func() {
			e.Line("return t.UTC().Format(%s.TimeFormat)", httpAlias)
		})
	}

	if hasDeprecated {
		e.Line("")
		e.Line("// apiVersionDeprecated mapeia versão -> data em que passou a ser deprecated")
		e.Line("// (headers Deprecation/Sunset a partir dela, spec §17).")
		e.Block(fmt.Sprintf("var apiVersionDeprecated = map[string]%s.Time", timeAlias), func() {
			for _, lc := range verEnv.lifecycles {
				if lc.deprecatedDate != "" {
					e.Line("%s: mustParseAPIVersionDate(%s),", strconv.Quote(lc.version), strconv.Quote(lc.deprecatedDate))
				}
			}
		})
	}

	e.Line("")
	e.Line("// apiVersionGate resolve a versão requisitada (spec §17), aplica sunset (410")
	e.Line("// Gone, o handler de domínio NUNCA roda) e deprecated (headers Deprecation/")
	e.Line("// Sunset, segue normal) — chamado no INÍCIO de todo handler versionado, antes")
	e.Line("// de qualquer outro gate (caller/tenant/rate-limit) — ver a doc de")
	e.Line("// emitUseCaseRoute/emitQueryRoute (http.go).")
	e.Block(fmt.Sprintf("func apiVersionGate(w %s.ResponseWriter, r *%s.Request) (string, bool)", httpAlias, httpAlias), func() {
		e.Line("version := resolveAPIVersion(r)")
		if hasSunset {
			e.Block(fmt.Sprintf("if sunset, ok := apiVersionSunset[version]; ok && !%s.Now().Before(sunset)", timeAlias), func() {
				e.Line(`%s.Error(w, "API version sunset (spec §17)", %s.StatusGone)`, httpAlias, httpAlias)
				e.Line(`return "", false`)
			})
		}
		if hasDeprecated {
			e.Block(fmt.Sprintf("if dep, ok := apiVersionDeprecated[version]; ok && !%s.Now().Before(dep)", timeAlias), func() {
				e.Line(`w.Header().Set("Deprecation", "true")`)
				if hasSunset {
					e.Block("if sunset, ok := apiVersionSunset[version]; ok", func() {
						e.Line(`w.Header().Set("Sunset", apiVersionSunsetHeader(sunset))`)
					})
				}
			})
		}
		e.Line("return version, true")
	})
	return nil
}

// emitModuleAPIVersions emite <pkg>/api_versions.go (G6, spec §17): a
// struct+função Upcast<Cmd><Ver>/Downcast<View><Ver> de CADA Command/View
// DECLARADO NESTE MÓDULO (b.commands/b.views) que alguma Version do GRUPO
// (groupVersions — todos os módulos do mesmo cmd/<service>, não só este
// módulo: um versions/*.ds pode viver em qualquer módulo do grupo) traduz.
// Vive no PACOTE DE DOMÍNIO do Command/View (não em cmd/<group>/main.go) —
// ver a nota de arquitetura em httpVersioningEnv (mais acima): o corpo do
// upcast CONSTRÓI VOs via o lowering normal (nomes de tipo NUS, só válidos
// dentro do PRÓPRIO pacote do módulo). nil, nil quando nenhum Command/View
// deste módulo tem upcast/downcast em groupVersions (o caso comum — chamado
// por generateModuleFiles independentemente de a Interface do grupo
// declarar "versioning": a MECÂNICA de tradução é uma propriedade do
// Command/View + Version, não da borda HTTP).
func emitModuleAPIVersions(pkg string, b moduleBucket, groupVersions []*ast.VersionDecl, model *types.Model, tab *symbols.SymbolTable, moduleName string) ([]byte, error) {
	if len(groupVersions) == 0 {
		return nil, nil
	}
	upcastIdx := indexUpcastsByCommand(groupVersions)
	downcastIdx := indexDowncastsByView(groupVersions)

	e := emit.New(pkg)
	wrote := false
	for _, cmd := range b.commands {
		versionMap, ok := upcastIdx[cmd.Name]
		if !ok {
			continue
		}
		for _, version := range sortedStringKeys(versionMap) {
			if err := emitCommandUpcast(e, cmd.Name, version, versionMap[version], model, tab, moduleName); err != nil {
				return nil, fmt.Errorf("upcast %s (versão %s): %w", cmd.Name, version, err)
			}
			wrote = true
		}
	}
	for _, view := range b.views {
		versionMap, ok := downcastIdx[view.Name]
		if !ok {
			continue
		}
		for _, version := range sortedStringKeys(versionMap) {
			if err := emitViewDowncast(e, view.Name, version, versionMap[version], model, tab, moduleName); err != nil {
				return nil, fmt.Errorf("downcast %s (versão %s): %w", view.Name, version, err)
			}
			wrote = true
		}
	}
	if !wrote {
		return nil, nil
	}
	return e.Bytes()
}

// emitCommandUpcast emite "<Cmd><Ver>Request" (um campo por
// VersionUpcast.From) e "Upcast<Cmd><Ver>(raw) (<Cmd>, error)" — ver a doc do
// arquivo sobre o mecanismo de hoisting (lower.StmtLowerer) reusado aqui.
// Emitido no MESMO pacote de cmdName (module) — por isso, ao contrário de
// toda referência cross-pacote deste gerador, os nomes de tipo (Money,
// AccountId, ...) e o próprio cmdName aparecem NUS, sem alias.
func emitCommandUpcast(e *emit.Emitter, cmdName, version string, upcast *ast.VersionUpcast, model *types.Model, tab *symbols.SymbolTable, module string) error {
	suffix := versionSuffixGo(version)
	reqStructName := cmdName + suffix + "Request"
	fnName := "Upcast" + cmdName + suffix

	e.Line("")
	e.Line("// %s é a shape legada (versão %s) do corpo de requisição de %s (spec §17:", reqStructName, version, cmdName)
	e.Line("// upcast).")
	var fieldErr error
	needsRuntime := false
	e.Block("type "+reqStructName+" struct", func() {
		for _, f := range upcast.From {
			if f == nil {
				continue
			}
			goType, gerr := goname.GoFieldType(f.Type)
			if gerr != nil {
				fieldErr = fmt.Errorf("campo %s: %w", f.Name, gerr)
				return
			}
			if strings.HasPrefix(goType, "runtime.") {
				// Um campo "decimal"/File/FileRef/FileStream resolve para
				// "runtime.X" — texto CRU (goname.GoFieldType, mesma convenção
				// de decl_command.go/commandFieldGoType), então o import
				// precisa ser registrado explicitamente aqui (nunca colide,
				// mesma garantia de sempre).
				needsRuntime = true
			}
			e.Line("%s %s %s", goname.ExportField(f.Name), goType, goname.JSONTag(f.Name))
		}
	})
	if fieldErr != nil {
		return fieldErr
	}
	if needsRuntime {
		e.Import(RuntimeImportPath)
	}

	// rootEnv semeia os receptores "from" (a shape legada, "raw.Campo" via
	// BindGoName); bodyEnv é um ESCOPO-FILHO (mesmo padrão de childForLoop,
	// lower/stmt.go) onde o switch de AssignStmt sintético (abaixo) de fato
	// declara os locais "to" — se um campo "from" e um alvo "to" tiverem o
	// MESMO nome (ex. "id = id" sem tradução nenhuma), TypeEnv.BoundLocally
	// (que só olha o nível LOCAL, nunca o pai) ainda reconhece corretamente a
	// 1ª atribuição do alvo como ":=" (uma variável Go NOVA), em vez de
	// confundi-la com o receptor "from" já vinculado no pai e emitir "=" sobre
	// um nome nunca declarado (bug de sombra que uma única árvore de escopo
	// teria).
	rootEnv := lower.New(model, tab, module)
	for _, f := range upcast.From {
		if f == nil {
			continue
		}
		rootEnv.Bind(f.Name, model.TypeOfRef(module, f.Type))
	}
	bodyEnv := rootEnv.Child()
	l := lower.NewLowerer(bodyEnv, goname.NewVOOperatorRegistry(), "")
	for _, f := range upcast.From {
		if f == nil {
			continue
		}
		l.BindGoName(f.Name, "raw."+goname.ExportField(f.Name))
	}

	zeroVal := cmdName + "{}"
	stmtCtx := lower.StmtContext{ZeroValues: []string{zeroVal}}
	sl := lower.NewStmtLowerer(l, e, stmtCtx)

	span := upcast.Span()
	stmts := make([]ast.Stmt, 0, len(upcast.To))
	for _, entry := range upcast.To {
		stmts = append(stmts, ast.NewAssignStmt(ast.NewIdent(entry.Name, span), entry.Value, span))
	}
	synthetic := ast.NewBlock(stmts, span)

	e.Line("")
	e.Line("// %s traduz o corpo legado (versão %s) para %s (spec §17: upcast). Um campo", fnName, version, cmdName)
	e.Line("// de %s não atribuído em \"to\" fica com o zero value Go — só acontece com um", cmdName)
	e.Line("// campo que tem default (REQ-5.13/sema.checkVersionUpcastDefaults já barra um")
	e.Line("// campo obrigatório sem default e sem atribuição).")
	var bodyErr error
	e.Block(fmt.Sprintf("func %s(raw %s) (%s, error)", fnName, reqStructName, cmdName), func() {
		if bodyErr = sl.Block(synthetic); bodyErr != nil {
			return
		}
		assigns := make([]string, 0, len(upcast.To))
		for _, entry := range upcast.To {
			assigns = append(assigns, fmt.Sprintf("%s: %s", goname.ExportField(entry.Name), goname.Ident(entry.Name)))
		}
		e.Line("return %s{%s}, nil", cmdName, strings.Join(assigns, ", "))
	})
	return bodyErr
}

// emitViewDowncast emite "<View><Ver>Response" e "Downcast<View><Ver>(v)
// <View><Ver>Response" — mais simples que o upcast: como o spec só usa
// acesso a membro simples sobre "self", não precisa de hoisting
// (Lowerer.Expr basta, o mesmo padrão de projectionFieldInfos,
// decl_projection.go). Emitido no MESMO pacote de viewName (module) — o
// parâmetro "v" e o tipo de retorno aparecem NUS, sem alias.
func emitViewDowncast(e *emit.Emitter, viewName, version string, downcast *ast.VersionDowncast, model *types.Model, tab *symbols.SymbolTable, module string) error {
	env := lower.New(model, tab, module)
	shape, err := viewShapeOf(env, viewName)
	if err != nil {
		return err
	}
	l := lower.NewLowerer(env, goname.NewVOOperatorRegistry(), "")
	env.Bind("self", shape)
	l.BindGoName("self", "v")

	type downcastField struct {
		name, exportName, goType, valueGo string
	}
	infos := make([]downcastField, 0, len(downcast.To))
	needsRuntime := false
	for _, entry := range downcast.To {
		valueGo, verr := l.Expr(entry.Value)
		if verr != nil {
			return fmt.Errorf("map %s = ...: %w", entry.Name, verr)
		}
		t := env.Model().Infer(module, entry.Value, env)
		if types.IsError(t) {
			return fmt.Errorf("map %s = ...: não consegui inferir o tipo do valor (bug de geração)", entry.Name)
		}
		goType, gerr := viewMemberGoType(t)
		if gerr != nil {
			return fmt.Errorf("map %s = ...: %w", entry.Name, gerr)
		}
		if strings.HasPrefix(goType, "runtime.") {
			needsRuntime = true // mesma ressalva de emitCommandUpcast acima.
		}
		infos = append(infos, downcastField{name: entry.Name, exportName: goname.ExportField(entry.Name), goType: goType, valueGo: valueGo})
	}
	if needsRuntime {
		e.Import(RuntimeImportPath)
	}

	suffix := versionSuffixGo(version)
	respStructName := viewName + suffix + "Response"
	fnName := "Downcast" + viewName + suffix

	e.Line("")
	e.Line("// %s é a shape legada (versão %s) da resposta de %s (spec §17: downcast).", respStructName, version, viewName)
	e.Block("type "+respStructName+" struct", func() {
		for _, fi := range infos {
			e.Line("%s %s %s", fi.exportName, fi.goType, goname.JSONTag(fi.name))
		}
	})

	e.Line("")
	e.Line("// %s traduz %s atual de volta à shape legada (versão %s, spec §17: downcast).", fnName, viewName, version)
	e.Block(fmt.Sprintf("func %s(v %s) %s", fnName, viewName, respStructName), func() {
		assigns := make([]string, len(infos))
		for i, fi := range infos {
			assigns[i] = fmt.Sprintf("%s: %s", fi.exportName, fi.valueGo)
		}
		e.Line("return %s{%s}", respStructName, strings.Join(assigns, ", "))
	})
	return nil
}

// viewShapeOf resolve name a um *types.ShapeType de Kind View (Lookup via
// lower.TypeEnv.TypeOfName — Lookup local ao módulo, fallback Find
// cross-module) — mesmo padrão de aggregateShape (http.go)/
// queryBodyEmitter.shapeOf (decl_query.go), parametrizado para View.
func viewShapeOf(env *lower.TypeEnv, name string) (*types.ShapeType, error) {
	t := env.TypeOfName(name)
	if types.IsError(t) {
		return nil, fmt.Errorf("View %s: símbolo não resolvido (bug de geração — REQ-9 já deveria ter barrado isso)", name)
	}
	shape, ok := t.(*types.ShapeType)
	if !ok || shape.Kind != symbols.KindView {
		return nil, fmt.Errorf("View %s: não resolve a uma View (got %T)", name, t)
	}
	return shape, nil
}

// --- 5. VersionRoute: override inteiro de UseCase para uma versão. ---

// emitVersionRouteOverrides emite, logo após o gate de sunset/deprecated
// (ANTES de qualquer outro gate/decode da rota BASE — ver a doc do arquivo),
// um switch por versão cujo case dispatcha INTEIRAMENTE para o UseCase de
// override (emitVersionRouteOverrideBody) e retorna. No-op quando overrides
// está vazio (o caso esparso comum — nenhuma Version declara "route" para
// este path).
func emitVersionRouteOverrides(e *emit.Emitter, overrides []versionRouteOverride, route *ast.Route, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable, runtimeAlias, httpAlias string) error {
	if len(overrides) == 0 {
		return nil
	}
	e.Line("switch apiVersion {")
	for _, ov := range overrides {
		e.Line("case %s:", strconv.Quote(ov.version))
		if err := emitVersionRouteOverrideBody(e, ov, route, buckets, modules, model, tab, runtimeAlias, httpAlias); err != nil {
			return fmt.Errorf("Version %s: route %q -> %s: %w", ov.version, route.Path, ov.usecase.Name, err)
		}
		e.Line("return")
	}
	e.Line("}")
	return nil
}

// emitVersionRouteOverrideBody emite o corpo de UM case de
// emitVersionRouteOverrides: um dispatch de UseCase INTEIRAMENTE
// auto-contido (seu próprio caller/decode/path-params/dispatch), ignorando
// tenant/rate-limit/idempotência/upcast da rota BASE — escopo documentado
// (ver a doc do arquivo): VersionRoute é a troca de comportamento mais rara
// e mais "legada" das três formas de versionamento; as políticas de borda da
// rota ATUAL não necessariamente fazem sentido para um cliente que nunca vai
// migrar.
func emitVersionRouteOverrideBody(e *emit.Emitter, ov versionRouteOverride, route *ast.Route, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable, runtimeAlias, httpAlias string) error {
	cmdDecl, cmdModule, ok := findCommandInBuckets(buckets, modules, ov.usecase.Handles)
	if !ok {
		return fmt.Errorf("Command %q (handles) não encontrado nos módulos do grupo", ov.usecase.Handles)
	}
	pp := routePathParams(route.Path)
	fields, err := correlatePathParamsToCommandFields(pp, cmdDecl)
	if err != nil {
		return err
	}
	cmdAlias, err := aliasForSymbol(e, tab, cmdModule, cmdDecl.Name)
	if err != nil {
		return err
	}
	ucAlias, err := aliasForSymbol(e, tab, ov.module, ov.usecase.Name)
	if err != nil {
		return err
	}

	emitCallerAndIdempotency(e, runtimeAlias)
	e.Line("var cmd %s.%s", cmdAlias, cmdDecl.Name)
	emitPlainCommandDecode(e, httpAlias)
	for i, param := range pp {
		field := fields[i]
		varName := goname.Ident(param) + "OverrideVal"
		rawGo := fmt.Sprintf("r.PathValue(%q)", param)
		t, terr := commandFieldParseType(field, model, tab, cmdModule)
		if terr != nil {
			return fmt.Errorf("path param %q: %w", param, terr)
		}
		if perr := emitHTTPParseParam(e, tab, cmdModule, varName, param, rawGo, t); perr != nil {
			return perr
		}
		e.Line("cmd.%s = %s", goname.ExportField(field.Name), varName)
	}
	e.Block(fmt.Sprintf("if err := %s.%s(ctx, cmd); err != nil", ucAlias, ov.usecase.Name), func() {
		e.Line("writeBusinessError(w, err)")
		e.Line("return")
	})
	e.Line("w.WriteHeader(%s.StatusNoContent)", httpAlias)
	return nil
}
