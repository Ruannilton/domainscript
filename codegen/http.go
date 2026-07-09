package codegen

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/program"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// http.go emite a borda HTTP (E9.2, REQ-28.1/28.2, §design 3.12): a partir de
// um *ast.InterfaceDecl Kind=="HTTP" (achado por findGroupInterface,
// codegen.go), registra cada ast.Route num *http.ServeMux (Go 1.22+,
// "METHOD /path/{param}") dentro de func main() do cmd/<group>/main.go — o
// TODO(E9.2) que generateCmdMainFile deixou. Sem Interface HTTP no grupo (ou
// uma Interface HTTP sem Routes), emitHTTPRoutes não emite nada: o mux fica
// vazio e o servidor sobe do mesmo jeito (grupos só-worker, Marco F+).
//
// --- Resolução de alvo (Route.Target -> UseCase | Query) ---
//
// Cada Target é um NOME (ex. "PerformDeposit") que resolveRouteTarget procura
// nos moduleBucket de TODOS os módulos do grupo (buckets, já computados por
// Generate — ver bucketModuleDecls/moduleBucket em codegen.go): primeiro entre
// os UseCase, depois entre as Query. Não achar nenhum dos dois é erro de
// geração — não deveria acontecer sobre um programa validado (REQ-5.23 already
// bars a Route apontando para nada), mas REQ-14.4 pede defesa mesmo assim.
//
// --- Correlação path param -> campo de Command (REQ-28.1) ---
//
// O corpo da requisição (JSON) preenche o Command inteiro primeiro; DEPOIS os
// path params sobrescrevem os campos que eles correlacionam — nunca o
// contrário, então um cliente não pode usar o corpo para forjar o id da rota.
// correlatePathParamsToCommandFields decide, para cada path param da rota,
// QUAL campo do Command ele preenche:
//
//  1. Nome exato: um path param cujo nome bate literalmente com o nome de um
//     campo (ex. "{amount}" -> campo "amount") sempre correlaciona com esse
//     campo, ref ou não.
//  2. O caso que o wallet de fato exercita — um path param chamado
//     literalmente "id" que NÃO bate com nenhum campo por nome exato -
//     correlaciona com o ÚNICO campo "ref Aggregate" do Command (ex.: "{id}"
//     -> "walletId ref Wallet"): um campo ref é o handle do Aggregate da
//     rota, e a convenção REST nomeia o segmento de path pelo *recurso*, não
//     pelo campo — então "id" é o único nome que vale a pena tratar como
//     especial. Só se aplica havendo EXATAMENTE 1 campo ref (0 ou >1 é
//     ambíguo, a regra não se aplica).
//  3. Nenhuma das duas correlaciona -> ERRO DE GERAÇÃO claro (nunca uma
//     escolha silenciosa/errada, REQ-14.4): o path param da rota não tem
//     campo do Command para preencher.
//
// Todo campo do Command NÃO alvejado por nenhum path param vem só do corpo
// JSON — esta função não tem opinião sobre eles.
//
// --- Correlação path param -> parâmetro de Query (REQ-28.1) ---
//
// Mais simples: cada ast.Field de QueryDecl.Params precisa de um path param
// de MESMO NOME na rota (ex.: "Query GetWallet(id WalletId)" + "{id}" bate
// por "id" == "id"). Sem bater, é erro de geração — string de query (?x=y)
// fica para trabalho futuro (o wallet só exercita path params, que é o
// mínimo definido por esta task).
//
// --- Parsing de um path param para o tipo Go do parâmetro/campo alvo ---
//
// emitHTTPParseParam cobre exatamente as formas que path/query params (um
// único token string) conseguem representar sem ambiguidade: um ValueObject
// wrapper de string/integer (New<Nome>), um Enum de base string/integer
// (Parse<Nome>), e os primitivos nus string/integer/boolean (passthrough ou
// strconv). Qualquer outra forma (VO composto, decimal/datetime/bytes/
// duration/size/rate, Aggregate/Event/Command/View, List/Set/Map) é erro de
// geração — um path param é um único segmento string, não tem como
// desambiguar um tipo composto ou coleção a partir dele (suporte mínimo
// definido por esta task, mais casos entram quando surgir necessidade real).
//
// --- Caller de dev e mapeamento de erro a status (§design 3.12) ---
//
// Todo handler injeta um runtime.Caller de DEV a partir do header
// "X-Caller-Id" (devCallerFromRequest/devCaller, emitidos por
// emitHTTPHelpers — placeholder até auth real) e repassa "Idempotency-Key"
// via runtime.WithIdempotencyKey quando presente (efetivado só no runtime em
// G2 — aqui é só o carrier). writeBusinessError distingue o erro por
// errors.As(&runtime.BusinessError): negócio -> 422, exceto ErrForbidden
// (403) e ErrNotFound (404); qualquer outro error (infra) -> 503. Rate-limit
// (429, G4, ver emitRateLimitGate acima e codegen/ratelimit.go) e tenant
// ausente (400 fail-closed, G5, ver emitTenantGate abaixo) já estão
// cobertos; sunset (410) continua marco G6 — nem mencionado aqui de
// propósito, para não travar a extensão futura.
//
// --- Forma do corpo de resposta (decisão desta task) ---
//
// UseCase: a função gerada só devolve error (sem dado de retorno) — sucesso
// vira "204 No Content", sem corpo: não há dado de domínio para serializar,
// e 204 é a forma idiomática HTTP de "aceito, nada a devolver" (a alternativa
// de emitir "{}" só empurraria um corpo vazio sem significado para o
// cliente). Query: sucesso vira "200" + o valor de retorno serializado como
// JSON (Content-Type: application/json).
//
// --- Multi-tenancy na borda (G5, REQ-27, spec §13) ---
//
// "tenant { from: ... }" na Interface (ast.InterfaceDecl.Settings, §13.1) é o
// ÚNICO gatilho: uma Interface SEM esse bloco (o caso do wallet/shop, nenhum
// dos dois declara tenancy) nunca chama parseInterfaceTenantPlan em nada além
// de devolver (nil, nil), e toda linha de tenant-edge abaixo
// (emitTenantGate/emitTenantHelpers) fica OMITIDA — o Go gerado permanece
// byte a byte igual ao de antes de G5 para qualquer service sem tenancy.
// Quando presente, TODA rota do grupo exige tenant resolvido, EXCETO uma que
// declare "{ tenancy: none }" (routeOptsTenancyNone) — o "rotas sem tenant"
// do spec (login/register/health). tenantIDFromRequest (emitido por
// emitTenantHelpers) materializa a estratégia resolvida em tempo de GERAÇÃO
// (subdomain: 1º rótulo do Host; header(NAME): header nomeado — ver
// parseInterfaceTenantPlan) — nenhuma comparação de string em tempo de
// execução, o Go gerado já sabe qual estratégia usar. Tenant ausente numa
// rota que o exige -> 400 (fail-closed, §13.4, emitTenantGate) ANTES de
// qualquer decode de corpo/dispatch; resolvido -> runtime.WithTenant(ctx,
// ...) ANTES do rate-limit gate (G4 lê TenantFrom(ctx) para perTenant/
// byTier) e antes de qualquer Load/Append (G1/G5, row_level — ver
// rtsrc/eventstore.go.txt) rodar. Tier vem do header dev-placeholder
// "X-Tenant-Tier" (mesmo espírito de "X-Caller-Id"/devCallerFromRequest —
// nenhum diretório de tenants/planos de verdade existe ainda). jwt_claim/
// path (§13.1 os lista como alternativas de "from:") não são implementados
// por este gerador — parseInterfaceTenantPlan falha explicitamente
// (erro de geração, nunca um extractor silenciosamente vazio) quando
// declarados, em vez de fingir suportá-los.

// httpTenantPlan é a config "tenant { from: ... }" de UMA Interface (§13.1),
// já resolvida — nil (via parseInterfaceTenantPlan) quando a Interface não
// declara tenancy nenhuma (ver a doc do arquivo).
type httpTenantPlan struct {
	kind   string // "subdomain" | "header"
	header string // só quando kind == "header": o nome do header (ex. "X-Tenant-Id")
}

// parseInterfaceTenantPlan lê iface.Settings["tenant"] — nil, nil quando a
// chave está ausente (o caso comum, ver a doc do arquivo). Formas
// reconhecidas (§13.1): "from: subdomain" (Ident nu) e "from:
// header(\"Nome-Do-Header\")" (CallExpr de 1 arg string). "from: path" e
// "from: jwt_claim(...)" são sintaxe reconhecida do spec mas SEM
// implementação nesta task (ver a doc do arquivo) — erro de geração claro.
func parseInterfaceTenantPlan(iface *ast.InterfaceDecl) (*httpTenantPlan, error) {
	if iface == nil {
		return nil, nil
	}
	var raw ast.Expr
	found := false
	for _, e := range iface.Settings {
		if e.Key == "tenant" {
			raw, found = e.Value, true
		}
	}
	if !found {
		return nil, nil
	}
	obj, ok := raw.(*ast.ObjectExpr)
	if !ok {
		return nil, fmt.Errorf("Interface %s: %q: esperava um objeto (\"tenant { from: ... }\", spec §13.1), veio %T", iface.Kind, "tenant", raw)
	}
	var fromExpr ast.Expr
	hasFrom := false
	for _, e := range obj.Entries {
		if e.Key == "from" {
			fromExpr, hasFrom = e.Value, true
		}
	}
	if !hasFrom {
		return nil, fmt.Errorf(`Interface %s: "tenant": falta "from:" (spec §13.1: subdomain/header(...)/jwt_claim(...)/path)`, iface.Kind)
	}

	switch v := fromExpr.(type) {
	case *ast.Ident:
		switch v.Name {
		case "subdomain":
			return &httpTenantPlan{kind: "subdomain"}, nil
		case "path":
			return nil, fmt.Errorf(`Interface %s: "tenant { from: path }": resolução de tenant por segmento de path não é suportada por este gerador (G5) — nenhuma convenção de nome de path param foi fixada por uma fixture real; falha explícita em vez de gerar um extractor que nunca resolveria nada`, iface.Kind)
		default:
			return nil, fmt.Errorf(`Interface %s: "tenant { from: %s }": forma não reconhecida (spec §13.1: subdomain/header(...)/jwt_claim(...)/path)`, iface.Kind, v.Name)
		}
	case *ast.CallExpr:
		fn, ok := v.Fn.(*ast.Ident)
		if !ok || len(v.Args) != 1 {
			return nil, fmt.Errorf(`Interface %s: "tenant { from: ... }": forma de chamada inesperada (esperava "nome(\"literal\")")`, iface.Kind)
		}
		lit, ok := v.Args[0].Value.(*ast.Literal)
		if !ok || lit.Kind != token.STRING {
			return nil, fmt.Errorf(`Interface %s: "tenant { from: %s(...) }": esperava um único literal string`, iface.Kind, fn.Name)
		}
		switch fn.Name {
		case "header":
			return &httpTenantPlan{kind: "header", header: lit.Value}, nil
		case "jwt_claim":
			return nil, fmt.Errorf(`Interface %s: "tenant { from: jwt_claim(%q) }": resolução de tenant por claim JWT não é suportada por este gerador (G5) — nenhuma infraestrutura de autenticação JWT existe neste codebase (fora do orçamento desta task); falha explícita em vez de um extractor silenciosamente vazio`, iface.Kind, lit.Value)
		default:
			return nil, fmt.Errorf(`Interface %s: "tenant { from: %s(...) }": forma não reconhecida (spec §13.1: subdomain/header(...)/jwt_claim(...)/path)`, iface.Kind, fn.Name)
		}
	default:
		return nil, fmt.Errorf(`Interface %s: "tenant { from: ... }": forma não reconhecida (%T)`, iface.Kind, fromExpr)
	}
}

// routeOptsTenancyNone reporta se route declara "{ tenancy: none }" (§13.1:
// "Rotas sem tenant" — login/register/health no exemplo do spec) — a ÚNICA
// forma de uma rota escapar da exigência de tenant quando a Interface
// declara "tenant { from: ... }" (ver a doc do arquivo).
func routeOptsTenancyNone(route *ast.Route) bool {
	for _, e := range route.Options {
		if e.Key != "tenancy" {
			continue
		}
		id, ok := e.Value.(*ast.Ident)
		return ok && id.Name == "none"
	}
	return false
}

// emitTenantGate emite, dentro do handler de route (logo após
// emitCallerAndIdempotency, ANTES do rate-limit gate — ver a doc do
// arquivo), a resolução fail-closed de tenant (§13.4): nada quando plan==nil
// ou a rota declara "tenancy: none" (Go gerado idêntico ao de antes de G5);
// senão, "ctx, tenantOK := requireTenant(ctx, r)" e um 400 imediato quando
// tenantOK é false — ANTES de qualquer decode de corpo/path param, mesmo
// espírito de emitRateLimitGate rodar antes do trabalho de decodificação.
func emitTenantGate(e *emit.Emitter, plan *httpTenantPlan, route *ast.Route, httpAlias string) {
	if plan == nil || routeOptsTenancyNone(route) {
		return
	}
	e.Line("ctx, tenantOK := requireTenant(ctx, r)")
	e.Block("if !tenantOK", func() {
		e.Line(`%s.Error(w, "tenant required", %s.StatusBadRequest)`, httpAlias, httpAlias)
		e.Line("return")
	})
}

// emitHTTPRoutes emite, na posição ATUAL de e (chamado de dentro de func
// main(), logo após "mux := http.NewServeMux()" — ver
// codegen.go/generateCmdMainFile), um mux.HandleFunc(...) por ast.Route de
// iface. iface nil ou sem Routes não emite nada (hadRoutes=false) — o
// chamador usa hadRoutes para decidir se emite os helpers de borda
// (devCaller/writeBusinessError, emitHTTPHelpers): não faz sentido importar
// "errors"/"encoding/json" num service sem nenhuma rota. tenantPlan (G5,
// nil quando a Interface não declara "tenant { ... }") é resolvido UMA vez
// aqui e repassado a cada rota — o chamador usa tenantPlan != nil para
// decidir se emite tenantIDFromRequest/requireTenant (emitTenantHelpers).
// rlPending acumula as declarações de rate limit (G4, spec §16) que ALGUMA
// rota enfileirou — vazio quando nenhuma rota configura "rateLimit"; o
// chamador (generateCmdMainFile) as renderiza de volta no nível de pacote
// depois que este bloco (newMux) fecha (ver a doc de pendingRateLimitPlan,
// codegen/ratelimit.go, sobre por que isso não pode ser emitido aqui
// dentro). verEnv (G6, spec §17) é a mesma dança: resolvido UMA vez aqui
// (nil.plan quando a Interface não declara "versioning { ... }" — ver a doc
// de codegen/versioning.go) e repassado a cada rota, que enfileira nele os
// Upcast/Downcast por (Command|View, versão) que de fato usa; o chamador
// (generateCmdMainFile) os renderiza de volta no nível de pacote também
// depois que newMux fecha (emitVersioningHelpers).
func emitHTTPRoutes(e *emit.Emitter, muxVar string, iface *ast.InterfaceDecl, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable, prog *program.Program) (hadRoutes bool, tenantPlan *httpTenantPlan, rlPending []pendingRateLimitPlan, verEnv *httpVersioningEnv, err error) {
	if iface == nil || len(iface.Routes) == 0 {
		return false, nil, nil, nil, nil
	}
	tenantPlan, err = parseInterfaceTenantPlan(iface)
	if err != nil {
		return true, nil, nil, nil, err
	}
	verEnv, err = newHTTPVersioningEnv(iface, buckets, modules)
	if err != nil {
		return true, tenantPlan, nil, nil, err
	}
	rlEnv := newHTTPRateLimitEnv(prog, iface)
	for _, route := range iface.Routes {
		if err := emitRoute(e, muxVar, route, buckets, modules, model, tab, rlEnv, tenantPlan, verEnv); err != nil {
			return true, tenantPlan, *rlEnv.pending, verEnv, fmt.Errorf("rota %s %q -> %s: %w", route.Method, route.Path, route.Target, err)
		}
	}
	return true, tenantPlan, *rlEnv.pending, verEnv, nil
}

// routeTarget é o resultado de resolveRouteTarget: exatamente um dos dois
// ponteiros é não-nil (a resolução bem-sucedida garante isso; um Target que
// não resolve a nenhum dos dois nunca chega a produzir um routeTarget — vira
// erro direto).
type routeTarget struct {
	usecase  *ast.UseCaseDecl
	ucModule string
	query    *ast.QueryDecl
	qModule  string
}

// resolveRouteTarget procura targetName entre os UseCase (primeiro) e as
// Query (depois) de TODOS os módulos do grupo (modules, já ordenado
// alfabeticamente pelo chamador — buildCmdGroups/Generate — determinismo,
// NFR-13). Um nome que não bate com nenhum dos dois é erro de geração (ver a
// doc do arquivo).
func resolveRouteTarget(buckets map[string]moduleBucket, modules []string, targetName string) (routeTarget, error) {
	for _, m := range modules {
		for _, u := range buckets[m].usecases {
			if u.Name == targetName {
				return routeTarget{usecase: u, ucModule: m}, nil
			}
		}
	}
	for _, m := range modules {
		for _, q := range buckets[m].queries {
			if q.Name == targetName {
				return routeTarget{query: q, qModule: m}, nil
			}
		}
	}
	return routeTarget{}, fmt.Errorf("alvo %q não resolve a nenhum UseCase ou Query dos módulos do grupo (REQ-5.23 já deveria ter barrado uma rota apontando para nada sobre um programa válido)", targetName)
}

// emitRoute despacha para emitUseCaseRoute ou emitQueryRoute conforme
// resolveRouteTarget achou. rlEnv (G4, spec §16) resolve/enfileira o rate
// limiting da rota, quando configurado — ver a doc de httpRateLimitEnv
// (codegen/ratelimit.go). tenantPlan (G5, §13) repassa a config de tenancy da
// Interface, se houver — ver a doc do arquivo. verEnv (G6, spec §17) repassa
// o versionamento resolvido da Interface — ver codegen/versioning.go.
func emitRoute(e *emit.Emitter, muxVar string, route *ast.Route, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable, rlEnv *httpRateLimitEnv, tenantPlan *httpTenantPlan, verEnv *httpVersioningEnv) error {
	target, err := resolveRouteTarget(buckets, modules, route.Target)
	if err != nil {
		return err
	}
	if target.usecase != nil {
		return emitUseCaseRoute(e, muxVar, route, target.usecase, target.ucModule, buckets, modules, model, tab, rlEnv, tenantPlan, verEnv)
	}
	return emitQueryRoute(e, muxVar, route, target.query, target.qModule, model, tab, rlEnv, tenantPlan, verEnv)
}

// findCommandInBuckets procura um CommandDecl de nome name entre TODOS os
// módulos do grupo (modules, já ordenado — determinismo). Usado para achar o
// Command que um UseCase "handles" (UseCaseDecl.Handles é só o NOME, não um
// ponteiro — a mesma forma que decl_usecase.go também precisa reconsultar).
func findCommandInBuckets(buckets map[string]moduleBucket, modules []string, name string) (*ast.CommandDecl, string, bool) {
	for _, m := range modules {
		for _, c := range buckets[m].commands {
			if c.Name == name {
				return c, m, true
			}
		}
	}
	return nil, "", false
}

// routePathParams extrai os placeholders "{name}" de um Route.Path, na ORDEM
// em que aparecem — ex. "/wallets/{id}/deposit" -> ["id"]. Ignora qualquer
// "{" sem "}" correspondente (forma inesperada; o front-end já validou a
// sintaxe de Route.Path — REQ-2/3 — então isso não deveria ocorrer sobre um
// programa válido; devolve os params já achados até ali, sem pânico).
func routePathParams(p string) []string {
	var params []string
	for {
		start := strings.IndexByte(p, '{')
		if start < 0 {
			break
		}
		end := strings.IndexByte(p[start:], '}')
		if end < 0 {
			break
		}
		params = append(params, p[start+1:start+end])
		p = p[start+end+1:]
	}
	return params
}

// correlatePathParamsToCommandFields implementa a regra de correlação
// descrita na doc do arquivo: devolve, para cada path param (na mesma ordem
// de pathParams), o *ast.Field do Command que ele preenche. Erro claro se
// algum path param não correlaciona com exatamente um campo livre.
func correlatePathParamsToCommandFields(pathParams []string, cmd *ast.CommandDecl) ([]*ast.Field, error) {
	fieldByName := make(map[string]*ast.Field, len(cmd.Fields))
	var refFields []*ast.Field
	for _, f := range cmd.Fields {
		if f == nil {
			continue
		}
		fieldByName[f.Name] = f
		if f.Ref {
			refFields = append(refFields, f)
		}
	}

	claimed := make(map[string]bool, len(pathParams))
	out := make([]*ast.Field, len(pathParams))
	for i, p := range pathParams {
		if f, ok := fieldByName[p]; ok {
			if claimed[f.Name] {
				return nil, fmt.Errorf("path param %q correlaciona com o campo %q do Command %s, mas outro path param já reivindicou esse campo", p, f.Name, cmd.Name)
			}
			claimed[f.Name] = true
			out[i] = f
			continue
		}
		if p == "id" && len(refFields) == 1 && !claimed[refFields[0].Name] {
			claimed[refFields[0].Name] = true
			out[i] = refFields[0]
			continue
		}
		return nil, fmt.Errorf("path param %q não correlaciona com nenhum campo do Command %s (nome exato, ou \"id\" com exatamente 1 campo \"ref Aggregate\" — achei %d campo(s) ref)", p, cmd.Name, len(refFields))
	}
	return out, nil
}

// aliasForSymbol resolve o módulo que declara name (Lookup local a
// homeModule, fallback Find global — mesma regra de lower.TypeEnv.TypeOfName/
// Checker.typeOfName) e devolve o alias Go do pacote de domínio desse módulo,
// registrando o import em e (idempotente — Emitter.Import já dedup). Erro
// claro se name não resolve (bug de geração — REQ-9 já deveria ter barrado
// isso sobre um programa válido).
func aliasForSymbol(e *emit.Emitter, tab *symbols.SymbolTable, homeModule, name string) (string, error) {
	sym, ok := tab.Lookup(homeModule, name)
	if !ok {
		sym, ok = tab.Find(name)
	}
	if !ok {
		return "", fmt.Errorf("símbolo %q não resolvido (bug de geração — REQ-9 já deveria ter barrado isso)", name)
	}
	pkg := goname.PackageName(sym.Module)
	return e.Import(path.Join(domainModuleRoot, pkg)), nil
}

// aggregateShape resolve name a um *types.ShapeType de Kind Aggregate (Lookup
// local a homeModule, fallback Find global) — mesma forma de refFieldGoType
// (decl_command.go), mas devolvendo o types.ShapeType em vez do nome Go, para
// que commandFieldParseType possa inspecionar o campo "id" e decidir COMO
// fazer o parse (VOType/EnumType/Primitive), não só seu nome.
func aggregateShape(model *types.Model, tab *symbols.SymbolTable, homeModule, name string) (*types.ShapeType, error) {
	sym, ok := tab.Lookup(homeModule, name)
	if !ok {
		sym, ok = tab.Find(name)
	}
	if !ok {
		return nil, fmt.Errorf("símbolo %q não resolvido (bug de geração — REQ-9 já deveria ter barrado isso)", name)
	}
	t := model.TypeOf(sym)
	shape, ok := t.(*types.ShapeType)
	if !ok || shape.Kind != symbols.KindAggregate {
		return nil, fmt.Errorf("%q não resolve a um Aggregate (got %T)", name, t)
	}
	return shape, nil
}

// commandFieldParseType devolve o types.Type a partir do qual
// emitHTTPParseParam decide como fazer o parse de um path param que
// correlaciona com o campo f de um Command declarado em module: para um
// campo comum, model.TypeOfRef(module, f.Type) (mesma forma de
// commandFieldGoType, decl_command.go); para um campo "ref Aggregate", o tipo
// do campo "id" do state do Aggregate referenciado (mesma resolução de
// refFieldGoType, mas devolvendo o Type, não o nome Go — ver a doc de
// aggregateShape).
func commandFieldParseType(f *ast.Field, model *types.Model, tab *symbols.SymbolTable, module string) (types.Type, error) {
	if !f.Ref {
		return model.TypeOfRef(module, f.Type), nil
	}
	if f.Type == nil {
		return nil, fmt.Errorf("campo ref sem TypeRef")
	}
	shape, err := aggregateShape(model, tab, module, f.Type.Name)
	if err != nil {
		return nil, fmt.Errorf("ref %s: %w", f.Type.Name, err)
	}
	idType, ok := aggregateIDFieldType(shape)
	if !ok {
		return nil, fmt.Errorf("ref %s: Aggregate sem campo \"id\" declarado em state (todo Aggregate deveria ter identidade, §4.5)", f.Type.Name)
	}
	return idType, nil
}

// baseName devolve o Name de t quando t é um *types.Primitive, "" caso
// contrário — usado por emitHTTPParseParam para decidir a estratégia de
// parse de um VOType/EnumType pelo tipo base embrulhado.
func baseName(t types.Type) string {
	if p, ok := t.(*types.Primitive); ok {
		return p.Name
	}
	return ""
}

// emitParamErrCheck emite o "if err != nil { http.Error(...); return }"
// compartilhado por toda forma de parse de path param que pode falhar
// (strconv.Parse*, New<VO>, Parse<Enum>) — sempre a MESMA resposta (400,
// "bad request shape", permitido agora — distinto do 400 de tenant ausente,
// G5, e do 410 de sunset, G6, §design 3.12).
func emitParamErrCheck(e *emit.Emitter, paramLabel string) {
	httpAlias := e.Import("net/http")
	fmtAlias := e.Import("fmt")
	e.Block("if err != nil", func() {
		e.Line("%s.Error(w, %s.Sprintf(%q, %q, err), %s.StatusBadRequest)", httpAlias, fmtAlias, "invalid path param %q: %s", paramLabel, httpAlias)
		e.Line("return")
	})
}

// emitHTTPParseParam emite Go que faz o parse de rawGo (uma expressão Go que
// devolve string, ex. "r.PathValue(\"id\")") para o tipo t, vinculando o
// resultado a uma NOVA variável local varName — ver a doc do arquivo para as
// formas suportadas (VOType wrapper de string/integer, EnumType de base
// string/integer, Primitive string/integer/boolean). homeModule ancora a
// resolução de símbolo (aliasForSymbol) quando t é um VOType/EnumType.
// Qualquer outra forma é erro de GERAÇÃO (não de requisição) — REQ-14.4.
func emitHTTPParseParam(e *emit.Emitter, tab *symbols.SymbolTable, homeModule, varName, paramLabel, rawGo string, t types.Type) error {
	if types.IsError(t) {
		return fmt.Errorf("parâmetro %q: tipo não resolvido (bug de geração — REQ-9/13 já deveriam ter barrado isso)", paramLabel)
	}

	switch x := t.(type) {
	case *types.VOType:
		if x.Base == nil {
			return fmt.Errorf("parâmetro %q: ValueObject composto %s não é suportado em path/query param (E9.2 só aceita wrapper de string/integer — um path param é um único token string)", paramLabel, x.Name)
		}
		alias, err := aliasForSymbol(e, tab, homeModule, x.Name)
		if err != nil {
			return err
		}
		switch baseName(x.Base) {
		case "string":
			e.Line("%s, err := %s.New%s(%s)", varName, alias, x.Name, rawGo)
			emitParamErrCheck(e, paramLabel)
		case "integer":
			strconvAlias := e.Import("strconv")
			rawInt := varName + "Int"
			e.Line("%s, err := %s.ParseInt(%s, 10, 64)", rawInt, strconvAlias, rawGo)
			emitParamErrCheck(e, paramLabel)
			e.Line("%s, err := %s.New%s(%s)", varName, alias, x.Name, rawInt)
			emitParamErrCheck(e, paramLabel)
		default:
			return fmt.Errorf("parâmetro %q: ValueObject %s embrulha %s, não suportado em path/query param (E9.2 só aceita string/integer)", paramLabel, x.Name, x.Base.String())
		}
	case *types.EnumType:
		alias, err := aliasForSymbol(e, tab, homeModule, x.Name)
		if err != nil {
			return err
		}
		switch baseName(x.Base) {
		case "string":
			e.Line("%s, err := %s.Parse%s(%s)", varName, alias, x.Name, rawGo)
			emitParamErrCheck(e, paramLabel)
		case "integer":
			strconvAlias := e.Import("strconv")
			rawInt := varName + "Int"
			e.Line("%s, err := %s.ParseInt(%s, 10, 64)", rawInt, strconvAlias, rawGo)
			emitParamErrCheck(e, paramLabel)
			e.Line("%s, err := %s.Parse%s(%s)", varName, alias, x.Name, rawInt)
			emitParamErrCheck(e, paramLabel)
		default:
			return fmt.Errorf("parâmetro %q: Enum %s tem base %s, não suportado em path/query param (E9.2 só aceita string/integer)", paramLabel, x.Name, x.Base.String())
		}
	case *types.Primitive:
		switch x.Name {
		case "string":
			e.Line("%s := %s", varName, rawGo)
		case "integer":
			strconvAlias := e.Import("strconv")
			e.Line("%s, err := %s.ParseInt(%s, 10, 64)", varName, strconvAlias, rawGo)
			emitParamErrCheck(e, paramLabel)
		case "boolean":
			strconvAlias := e.Import("strconv")
			e.Line("%s, err := %s.ParseBool(%s)", varName, strconvAlias, rawGo)
			emitParamErrCheck(e, paramLabel)
		default:
			return fmt.Errorf("parâmetro %q: primitivo %s não suportado em path/query param (E9.2 só aceita string/integer/boolean)", paramLabel, x.Name)
		}
	default:
		return fmt.Errorf("parâmetro %q: tipo %s não suportado em path/query param (E9.2 cobre ValueObject wrapper, Enum e primitivos string/integer/boolean)", paramLabel, t.String())
	}
	return nil
}

// emitCallerAndIdempotency emite as linhas comuns a TODO handler (§design
// 3.12/3.13): o caller de dev extraído do header "X-Caller-Id" posto em ctx,
// "Idempotency-Key" repassado via runtime.WithIdempotencyKey quando presente
// (carrier só — enforcement é G2, ver contextkeys.go.txt), e um trace id
// mintado por requisição (H2, REQ-30.1) via runtime.WithTrace(ctx,
// runtime.NewTraceID()) — o mecanismo DEFAULT, stdlib-only, de correlação de
// logs (ver TraceIDFrom/logStmt): presente em TODA rota, com ou sem
// "Telemetry" declarado. Quando "Telemetry" está declarado, esse id stdlib
// vira só o FALLBACK — o Observer real (instalado via runtime.SetObserver,
// ver emitOTelWiring) troca a fonte para o trace id do span OTel ativo assim
// que emitUseCaseRoute/emitQueryRoute abrirem um (runtime.RecordSpan,
// abaixo) — nunca dois ids divergentes ao mesmo tempo.
func emitCallerAndIdempotency(e *emit.Emitter, runtimeAlias string) {
	e.Line("caller := devCallerFromRequest(r)")
	e.Line("ctx := %s.WithCaller(r.Context(), caller)", runtimeAlias)
	e.Line("ctx = %s.WithTrace(ctx, %s.NewTraceID())", runtimeAlias, runtimeAlias)
	e.Block(`if key := r.Header.Get("Idempotency-Key"); key != ""`, func() {
		e.Line("ctx = %s.WithIdempotencyKey(ctx, key)", runtimeAlias)
	})
}

// emitNoCacheBypass emite o bypass do cache de Query via "Cache-Control:
// no-cache" (G3, spec §15): só chamado por emitQueryRoute quando o alvo da
// rota é uma Query com "cache" declarado — para uma Query sem cache, marcar
// ctx não teria nenhum efeito (a Query descartaria o valor sem nunca chamar
// runtime.NoCacheFrom), então nem vale emitir a checagem (mantém o Go de
// rotas sem cache byte-a-byte igual ao de antes desta task). O bypass pula
// só a LEITURA do cache (a Query cacheada ainda REPOPULA com o resultado
// fresco — revalidar, não desligar o cache, a semântica padrão de
// "no-cache" HTTP).
func emitNoCacheBypass(e *emit.Emitter, runtimeAlias string) {
	stringsAlias := e.Import("strings")
	e.Block(fmt.Sprintf(`if %s.Contains(%s.ToLower(r.Header.Get("Cache-Control")), "no-cache")`, stringsAlias, stringsAlias), func() {
		e.Line("ctx = %s.WithNoCache(ctx)", runtimeAlias)
	})
}

// emitRateLimitGate emite a checagem de rate limit (G4, spec §16) comum a
// Query e a um UseCase SEM idempotência: chama a função de checagem da
// rota, e ou escreve o 429 + headers e retorna, ou escreve os headers de
// sucesso e segue.
func emitRateLimitGate(e *emit.Emitter, checksFuncName, runtimeAlias string) {
	e.Line("rlOK, rlResult := %s.CheckRateLimits(ctx, %s(ctx, r))", runtimeAlias, checksFuncName)
	e.Block("if !rlOK", func() {
		e.Line("writeRateLimitExceeded(w, rlResult)")
		e.Line("return")
	})
	e.Line("writeRateLimitHeaders(w, rlResult)")
}

// emitUseCaseRoute emite um mux.HandleFunc para uma rota cujo Target resolve
// a um UseCase (ver a doc do arquivo: decodifica o corpo JSON no Command,
// sobrescreve os campos correlacionados a path params, despacha o UseCase, e
// mapeia o resultado — sucesso 204 sem corpo, erro via writeBusinessError).
//
// --- Rate limiting (G4, spec §16) ---
//
// Quando rlEnv.resolveAndQueue devolve um nome de função (a rota configura
// "rateLimit" em algum lugar) e o UseCase NÃO declara idempotency, a
// checagem roda logo ANTES do corpo/path params serem sequer parseados —
// nenhum trabalho de decodificação para uma requisição que vai ser 429 de
// qualquer jeito.
//
// Quando o UseCase TAMBÉM declara "idempotency { ... }" (G2), a ordem
// inverte: o corpo/path params são decodificados PRIMEIRO (o cmd precisa
// existir para "<Nome>IsReplay(ctx, cmd)" poder responder), e só quando
// IsReplay devolve false a checagem de rate limit roda. Um replay
// idempotente conhecido NUNCA passa por runtime.CheckRateLimits — spec
// §14/§16: "retry idempotente não consome cota" (ver a doc de
// codegen/usecase_idempotency.go sobre por que "consumir e depois
// estornar" não FUNCIONA aqui: um estorno só rodaria DEPOIS do despacho,
// tarde demais para a checagem da PRÓPRIA requisição já ter negado por
// cota esgotada).
//
// Uma rota sem "rateLimit" configurado (checksFuncName == "") não muda
// NADA: o handler continua byte a byte igual ao de antes desta task.
//
// --- Tenant (G5, §13) ---
//
// emitTenantGate roda logo após emitCallerAndIdempotency e ANTES do rate
// limit gate (para que "perTenant"/"byTier", G4, já enxerguem o tenant
// resolvido) — 400 fail-closed antes de qualquer decode de corpo/path param
// quando a rota exige tenant e a borda não consegue resolvê-lo. Uma rota sem
// tenancy configurada em lugar nenhum (tenantPlan == nil) não muda NADA.
//
// --- Versionamento (G6, spec §17) ---
//
// verEnv.plan == nil (Interface sem "versioning { ... }", o caso do
// wallet/shop) não muda NADA — nenhuma linha de versão é emitida. Quando a
// Interface declara versioning, TRÊS coisas acontecem, nesta ordem, logo no
// INÍCIO do handler (antes até de emitCallerAndIdempotency — um cliente
// numa versão sunset nunca deveria pagar o custo de autenticação/tenant/rate
// limit, ver a doc de apiVersionGate, codegen/versioning.go):
//
//  1. apiVersionGate resolve a versão e aplica sunset (410 imediato, handler
//     nunca roda) e deprecated (headers Deprecation/Sunset, segue normal).
//  2. Se ALGUMA Version declara "route <esta rota> -> UseCase" (VersionRoute,
//     mudança de comportamento, não de shape), um switch despacha para o
//     UseCase de override — inteiramente auto-contido (seu próprio caller/
//     decode/dispatch), ignorando upcast/downcast/tenant/rate-limit da rota
//     BASE — e retorna, sem cair no fluxo abaixo (emitVersionRouteOverrides).
//  3. Se a Version tem "upcast <este Command>", o decode do corpo (mais
//     abaixo) ganha um switch: a versão com upcast decodifica a shape legada
//     e chama Upcast<Cmd><Ver>; qualquer outra versão (incl. a corrente)
//     decodifica direto no Command atual — EXATAMENTE como antes de G6
//     (emitPlainCommandDecode, reusado em ambos os ramos).
//
// Endpoints sem NENHUM upcast/override para o Command desta rota passam
// direto, byte a byte igual ao handler sem versionamento (versionamento
// esparso, spec §17) — só ganham o gate de sunset/deprecated.
func emitUseCaseRoute(e *emit.Emitter, muxVar string, route *ast.Route, uc *ast.UseCaseDecl, ucModule string, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable, rlEnv *httpRateLimitEnv, tenantPlan *httpTenantPlan, verEnv *httpVersioningEnv) error {
	cmdDecl, cmdModule, ok := findCommandInBuckets(buckets, modules, uc.Handles)
	if !ok {
		return fmt.Errorf("UseCase %s: Command %q (handles) não encontrado nos módulos do grupo", uc.Name, uc.Handles)
	}

	pp := routePathParams(route.Path)
	fields, err := correlatePathParamsToCommandFields(pp, cmdDecl)
	if err != nil {
		return fmt.Errorf("UseCase %s: %w", uc.Name, err)
	}

	cmdAlias, err := aliasForSymbol(e, tab, cmdModule, cmdDecl.Name)
	if err != nil {
		return err
	}
	ucAlias, err := aliasForSymbol(e, tab, ucModule, uc.Name)
	if err != nil {
		return err
	}
	httpAlias := e.Import("net/http")
	runtimeAlias := e.Import(RuntimeImportPath)

	checksFuncName, err := rlEnv.resolveAndQueue(route, ucModule)
	if err != nil {
		return fmt.Errorf("UseCase %s: %w", uc.Name, err)
	}
	hasIdempotency := uc.Idempotency != nil

	upcasts := verEnv.upcastsForCommand(cmdDecl.Name)
	overrides, err := verEnv.routeOverridesFor(route.Path)
	if err != nil {
		return fmt.Errorf("UseCase %s: %w", uc.Name, err)
	}
	apiVersionVar := "_"
	if len(upcasts) > 0 || len(overrides) > 0 {
		apiVersionVar = "apiVersion"
	}

	pattern := route.Method + " " + route.Path
	header := fmt.Sprintf("%s.HandleFunc(%s, func(w %s.ResponseWriter, r *%s.Request)", muxVar, strconv.Quote(pattern), httpAlias, httpAlias)

	var bodyErr error
	e.BlockSuffix(header, ")", func() {
		if verEnv.plan != nil {
			e.Line("%s, verOK := apiVersionGate(w, r)", apiVersionVar)
			e.Block("if !verOK", func() {
				e.Line("return")
			})
			if err := emitVersionRouteOverrides(e, overrides, route, buckets, modules, model, tab, runtimeAlias, httpAlias); err != nil {
				bodyErr = err
				return
			}
		}

		emitCallerAndIdempotency(e, runtimeAlias)
		emitTenantGate(e, tenantPlan, route, httpAlias)
		if checksFuncName != "" && !hasIdempotency {
			emitRateLimitGate(e, checksFuncName, runtimeAlias)
		}

		e.Line("var cmd %s.%s", cmdAlias, cmdDecl.Name)
		if len(upcasts) > 0 {
			versions := sortedStringKeys(upcasts)
			e.Line("switch apiVersion {")
			for _, version := range versions {
				e.Line("case %s:", strconv.Quote(version))
				// reqStructName/fnName são QUALIFICADOS pelo alias do pacote de
				// domínio de cmdDecl (cmdAlias) — a struct+função em si mora lá
				// (emitModuleAPIVersions, versioning.go), não aqui em cmd/main.go
				// (ver a nota de arquitetura em httpVersioningEnv, versioning.go).
				reqStructName := fmt.Sprintf("%s.%s", cmdAlias, cmdDecl.Name+versionSuffixGo(version)+"Request")
				fnName := fmt.Sprintf("%s.%s", cmdAlias, "Upcast"+cmdDecl.Name+versionSuffixGo(version))
				e.Block("if r.ContentLength != 0", func() {
					jsonAlias := e.Import("encoding/json")
					e.Line("var legacy %s", reqStructName)
					e.Block(fmt.Sprintf("if err := %s.NewDecoder(r.Body).Decode(&legacy); err != nil", jsonAlias), func() {
						fmtAlias := e.Import("fmt")
						e.Line("%s.Error(w, %s.Sprintf(%q, err), %s.StatusBadRequest)", httpAlias, fmtAlias, "invalid request body: %s", httpAlias)
						e.Line("return")
					})
					e.Line("var uerr error")
					e.Line("cmd, uerr = %s(legacy)", fnName)
					e.Block("if uerr != nil", func() {
						e.Line("%s.Error(w, uerr.Error(), %s.StatusBadRequest)", httpAlias, httpAlias)
						e.Line("return")
					})
				})
			}
			e.Line("default:")
			emitPlainCommandDecode(e, httpAlias)
			e.Line("}")
		} else {
			emitPlainCommandDecode(e, httpAlias)
		}

		for i, param := range pp {
			field := fields[i]
			varName := goname.Ident(param) + "Val"
			rawGo := fmt.Sprintf("r.PathValue(%q)", param)
			t, terr := commandFieldParseType(field, model, tab, cmdModule)
			if terr != nil {
				bodyErr = fmt.Errorf("path param %q: %w", param, terr)
				return
			}
			if perr := emitHTTPParseParam(e, tab, cmdModule, varName, param, rawGo, t); perr != nil {
				bodyErr = perr
				return
			}
			e.Line("cmd.%s = %s", goname.ExportField(field.Name), varName)
		}

		if checksFuncName != "" && hasIdempotency {
			// O cmd só existe a partir daqui — precisa vir DEPOIS do decode/
			// path params (ver a doc do arquivo acima).
			e.Block(fmt.Sprintf("if !%s.%sIsReplay(ctx, cmd)", ucAlias, uc.Name), func() {
				emitRateLimitGate(e, checksFuncName, runtimeAlias)
			})
		}

		// RecordSpan (H2, REQ-30.2, §design 3.13): um span de verdade em torno
		// do despacho quando o adapter OTel está instalado (Telemetry
		// declarado, ver emitOTelWiring) — no-op (mesmo ctx, sem custo) caso
		// contrário. ctx é reatribuído (o span, quando real, precisa fluir
		// para dentro do UseCase — log/trace_id dentro dele lê o mesmo ctx).
		// "ucErr" (em vez de "err") é proposital: um path param "ref
		// Aggregate" já pode ter declarado "err" mais acima neste MESMO
		// escopo (emitHTTPParseParam, ex. "idVal, err := wallet.NewWalletId(
		// ...)") — reusar "err" aqui bateria de frente com a regra de Go
		// "no new variables on left side of :=" quando NENHUM outro nome à
		// esquerda for novo.
		e.Line("ctx, ucSpanEnd := %s.RecordSpan(ctx, %s)", runtimeAlias, strconv.Quote("UseCase."+uc.Name))
		e.Line("ucErr := %s.%s(ctx, cmd)", ucAlias, uc.Name)
		e.Line("ucSpanEnd(ucErr)")
		e.Block("if ucErr != nil", func() {
			e.Line("writeBusinessError(w, ucErr)")
			e.Line("return")
		})
		e.Line("w.WriteHeader(%s.StatusNoContent)", httpAlias)
	})
	return bodyErr
}

// emitPlainCommandDecode emite "if r.ContentLength != 0 { ... Decode(&cmd) ...
// }" — o decode direto do corpo JSON no Command ATUAL, sem tradução alguma.
// É o comportamento de SEMPRE (antes de G6 só existia este caminho); com
// versionamento (G6), continua sendo o "default" do switch por versão
// (nenhum upcast declarado para esta versão) e o único caminho quando o
// Command não tem NENHUM upcast em lugar nenhum (versionamento esparso,
// spec §17) — extraído para função à parte para que os dois pontos de
// chamada (emitUseCaseRoute e emitVersionRouteOverrideBody, versioning.go)
// produzam o MESMO Go, byte a byte.
func emitPlainCommandDecode(e *emit.Emitter, httpAlias string) {
	e.Block("if r.ContentLength != 0", func() {
		jsonAlias := e.Import("encoding/json")
		e.Block(fmt.Sprintf("if err := %s.NewDecoder(r.Body).Decode(&cmd); err != nil", jsonAlias), func() {
			fmtAlias := e.Import("fmt")
			e.Line("%s.Error(w, %s.Sprintf(%q, err), %s.StatusBadRequest)", httpAlias, fmtAlias, "invalid request body: %s", httpAlias)
			e.Line("return")
		})
	})
}

// emitQueryRoute emite um mux.HandleFunc para uma rota cujo Target resolve a
// uma Query (ver a doc do arquivo: cada parâmetro declarado vem de um path
// param de mesmo nome, na ordem declarada; sucesso 200 + JSON, erro via
// writeBusinessError). rlEnv (G4, spec §16) resolve/enfileira o rate
// limiting da rota, quando configurado — ver emitRateLimitGate. Query não
// tem conceito de idempotência (REQ-20.4/G2 só cobre Command), então a
// checagem sempre roda ANTES de tudo, sem exceção. tenantPlan (G5, §13) roda
// logo após emitCallerAndIdempotency e ANTES do rate limit gate, mesma ordem
// e mesmo motivo de emitUseCaseRoute.
//
// --- Versionamento (G6, spec §17) ---
//
// verEnv.plan == nil não muda NADA (ver a doc de emitUseCaseRoute). Quando
// declarado, o mesmo apiVersionGate roda primeiro (sunset/deprecated); se a
// View de retorno tem "downcast" em alguma Version, o encode da resposta
// (mais abaixo) ganha um switch: a versão com downcast serializa a shape
// legada (Downcast<View><Ver>(result)); qualquer outra versão serializa
// "result" direto — igual a antes de G6 (emitPlainViewEncode, reusado nos
// dois ramos). VersionRoute (override de UseCase) apontando para uma rota
// cujo alvo BASE é uma Query não é suportado por este gerador — spec §17
// descreve route como troca para "UseCase distinto", nunca para outra Query;
// erro de geração claro em vez de ignorar silenciosamente.
func emitQueryRoute(e *emit.Emitter, muxVar string, route *ast.Route, decl *ast.QueryDecl, qModule string, model *types.Model, tab *symbols.SymbolTable, rlEnv *httpRateLimitEnv, tenantPlan *httpTenantPlan, verEnv *httpVersioningEnv) error {
	pp := routePathParams(route.Path)
	pathSet := make(map[string]bool, len(pp))
	for _, p := range pp {
		pathSet[p] = true
	}

	qAlias, err := aliasForSymbol(e, tab, qModule, decl.Name)
	if err != nil {
		return err
	}
	httpAlias := e.Import("net/http")
	runtimeAlias := e.Import(RuntimeImportPath)

	checksFuncName, err := rlEnv.resolveAndQueue(route, qModule)
	if err != nil {
		return fmt.Errorf("Query %s: %w", decl.Name, err)
	}

	overrides, err := verEnv.routeOverridesFor(route.Path)
	if err != nil {
		return fmt.Errorf("Query %s: %w", decl.Name, err)
	}
	if len(overrides) > 0 {
		return fmt.Errorf("Query %s: rota %q tem VersionRoute (versão %s -> %s), mas o alvo BASE desta rota é uma Query — não suportado por este gerador (spec §17: route troca para um UseCase distinto)", decl.Name, route.Path, overrides[0].version, overrides[0].usecase.Name)
	}
	downcasts := verEnv.downcastsForView(decl.Return)
	var downcastModule string
	if len(downcasts) > 0 {
		// A struct+função Downcast<View><Ver> mora no pacote de domínio da
		// View (emitModuleAPIVersions, versioning.go) — resolve o módulo que a
		// declara pela MESMA regra de aliasForSymbol (Lookup local a qModule,
		// fallback Find global) para qualificar a chamada mais abaixo.
		sym, ok := tab.Lookup(qModule, decl.Return.Name)
		if !ok {
			sym, ok = tab.Find(decl.Return.Name)
		}
		if !ok {
			return fmt.Errorf("Query %s: View %q (retorno) não resolvida (bug de geração — REQ-9 já deveria ter barrado isso)", decl.Name, decl.Return.Name)
		}
		downcastModule = sym.Module
	}
	apiVersionVar := "_"
	if len(downcasts) > 0 {
		apiVersionVar = "apiVersion"
	}

	pattern := route.Method + " " + route.Path
	header := fmt.Sprintf("%s.HandleFunc(%s, func(w %s.ResponseWriter, r *%s.Request)", muxVar, strconv.Quote(pattern), httpAlias, httpAlias)

	var bodyErr error
	e.BlockSuffix(header, ")", func() {
		if verEnv.plan != nil {
			e.Line("%s, verOK := apiVersionGate(w, r)", apiVersionVar)
			e.Block("if !verOK", func() {
				e.Line("return")
			})
		}

		emitCallerAndIdempotency(e, runtimeAlias)
		emitTenantGate(e, tenantPlan, route, httpAlias)
		if checksFuncName != "" {
			emitRateLimitGate(e, checksFuncName, runtimeAlias)
		}
		if len(decl.Cache) > 0 {
			emitNoCacheBypass(e, runtimeAlias)
		}

		argNames := make([]string, len(decl.Params))
		for i, param := range decl.Params {
			if param == nil {
				continue
			}
			if !pathSet[param.Name] {
				bodyErr = fmt.Errorf("Query %s: parâmetro %q não tem path param correspondente na rota %q (E9.2 só suporta path params — query string fica para trabalho futuro)", decl.Name, param.Name, route.Path)
				return
			}
			varName := goname.Ident(param.Name) + "Val"
			rawGo := fmt.Sprintf("r.PathValue(%q)", param.Name)
			t := model.TypeOfRef(qModule, param.Type)
			if perr := emitHTTPParseParam(e, tab, qModule, varName, param.Name, rawGo, t); perr != nil {
				bodyErr = fmt.Errorf("Query %s: %w", decl.Name, perr)
				return
			}
			argNames[i] = varName
		}
		if bodyErr != nil {
			return
		}

		// RecordSpan (H2, REQ-30.2, §design 3.13): mesma técnica de
		// emitUseCaseRoute — span de verdade quando o adapter OTel está
		// instalado, no-op caso contrário.
		e.Line("ctx, qSpanEnd := %s.RecordSpan(ctx, %s)", runtimeAlias, strconv.Quote("Query."+decl.Name))
		callArgs := append([]string{"ctx", "store"}, argNames...)
		e.Line("result, err := %s.%s(%s)", qAlias, decl.Name, strings.Join(callArgs, ", "))
		e.Line("qSpanEnd(err)")
		e.Block("if err != nil", func() {
			e.Line("writeBusinessError(w, err)")
			e.Line("return")
		})
		e.Line(`w.Header().Set("Content-Type", "application/json")`)
		if len(downcasts) > 0 {
			viewAlias, aerr := aliasForSymbol(e, tab, downcastModule, decl.Return.Name)
			if aerr != nil {
				bodyErr = aerr
				return
			}
			versions := sortedStringKeys(downcasts)
			e.Line("switch apiVersion {")
			for _, version := range versions {
				e.Line("case %s:", strconv.Quote(version))
				fnName := fmt.Sprintf("%s.%s", viewAlias, "Downcast"+decl.Return.Name+versionSuffixGo(version))
				emitPlainViewEncode(e, httpAlias, fmt.Sprintf("%s(result)", fnName))
			}
			e.Line("default:")
			emitPlainViewEncode(e, httpAlias, "result")
			e.Line("}")
		} else {
			emitPlainViewEncode(e, httpAlias, "result")
		}
	})
	return bodyErr
}

// emitPlainViewEncode emite "if err := json.NewEncoder(w).Encode(valueGo);
// err != nil { ... }" — o encode da resposta, EXATAMENTE como antes de G6.
// valueGo é "result" (o valor de retorno da Query, sem tradução) ou uma
// chamada a Downcast<View><Ver>(result) (G6, spec §17) — extraído para função
// à parte para que os dois ramos do switch por versão (emitQueryRoute)
// produzam o MESMO Go.
func emitPlainViewEncode(e *emit.Emitter, httpAlias, valueGo string) {
	jsonAlias := e.Import("encoding/json")
	e.Block(fmt.Sprintf("if err := %s.NewEncoder(w).Encode(%s); err != nil", jsonAlias, valueGo), func() {
		fmtAlias := e.Import("fmt")
		e.Line("%s.Error(w, %s.Sprintf(%q, err), %s.StatusInternalServerError)", httpAlias, fmtAlias, "failed to encode response: %s", httpAlias)
	})
}

// emitTenantHelpers emite, como declarações de PACOTE, tenantIDFromRequest
// (a estratégia de extração resolvida em tempo de GERAÇÃO — ver
// parseInterfaceTenantPlan) e requireTenant (o fail-closed §13.4: resolve o
// tenant e devolve ctx com ele, runtime.WithTenant, ou ok=false quando não
// consegue) — chamado no máximo 1 vez por cmd/<group>/main.go, só quando a
// Interface HTTP do grupo de fato declara "tenant { from: ... }"
// (generateCmdMainFile, codegen.go, tenantPlan != nil).
func emitTenantHelpers(e *emit.Emitter, plan *httpTenantPlan, runtimeAlias, httpAlias string) {
	ctxAlias := e.Import("context")

	e.Line("")
	e.Line("// tenantIDFromRequest extrai o tenant ambiente da requisição (spec §13.1),")
	switch plan.kind {
	case "subdomain":
		stringsAlias := e.Import("strings")
		e.Line(`// estratégia "subdomain": o primeiro rótulo do Host (antes do primeiro`)
		e.Line(`// "."), sem a porta — "" quando o Host não tem nenhum rótulo separado por`)
		e.Line(`// ponto (ex. "localhost", "127.0.0.1:8080").`)
		e.Block(fmt.Sprintf("func tenantIDFromRequest(r *%s.Request) string", httpAlias), func() {
			e.Line("host := r.Host")
			e.Block(fmt.Sprintf("if i := %s.IndexByte(host, ':'); i >= 0", stringsAlias), func() {
				e.Line("host = host[:i]")
			})
			e.Block(fmt.Sprintf("if i := %s.IndexByte(host, '.'); i >= 0", stringsAlias), func() {
				e.Line("return host[:i]")
			})
			e.Line(`return ""`)
		})
	case "header":
		e.Line("// estratégia %q: o header %q.", "header", plan.header)
		e.Block(fmt.Sprintf("func tenantIDFromRequest(r *%s.Request) string", httpAlias), func() {
			e.Line("return r.Header.Get(%s)", strconv.Quote(plan.header))
		})
	}

	e.Line("")
	e.Line("// requireTenant resolve o tenant ambiente de r (spec §13.1) e devolve ctx com")
	e.Line("// ele (runtime.WithTenant) — ok=false (fail-closed, §13.4) quando a borda não")
	e.Line("// consegue resolver nenhum tenant; o chamador escreve 400 e não despacha. Tier")
	e.Line(`// vem do header dev-placeholder "X-Tenant-Tier" (mesmo espírito de`)
	e.Line(`// "X-Caller-Id"/devCallerFromRequest, abaixo — nenhum diretório de`)
	e.Line("// tenants/planos de verdade existe ainda; habilita \"rateLimit: byTier\"")
	e.Line("// (G4) a de fato diferenciar tenants reais, quando presente).")
	e.Block(fmt.Sprintf("func requireTenant(ctx %s.Context, r *%s.Request) (%s.Context, bool)", ctxAlias, httpAlias, ctxAlias), func() {
		e.Line("id := tenantIDFromRequest(r)")
		e.Block(`if id == ""`, func() {
			e.Line("return ctx, false")
		})
		e.Line(`return %s.WithTenant(ctx, %s.Tenant{ID: id, Tier: r.Header.Get("X-Tenant-Tier")}), true`, runtimeAlias, runtimeAlias)
	})
}

// emitHTTPHelpers emite, como declarações de PACOTE (fora de func main()), o
// caller de dev (devCallerFromRequest/devCaller) e o mapeamento de erro a
// status (writeBusinessError) compartilhados por TODOS os handlers deste
// arquivo — chamado no máximo 1 vez por cmd/<group>/main.go, só quando o
// grupo de fato registrou ao menos 1 rota (generateCmdMainFile, codegen.go).
func emitHTTPHelpers(e *emit.Emitter, runtimeAlias string) {
	httpAlias := e.Import("net/http")
	errorsAlias := e.Import("errors")

	e.Line("")
	e.Line("// devCallerFromRequest constrói um runtime.Caller de DESENVOLVIMENTO a partir")
	e.Line("// do header \"X-Caller-Id\" (§design 3.12): presente e não-vazio ->")
	e.Line("// Authenticated()==true, ID()==valor do header; ausente -> Authenticated()==")
	e.Line("// false, ID()==\"\". Placeholder até a borda de auth real chegar; HasRole")
	e.Line("// sempre devolve false.")
	e.Block(fmt.Sprintf("func devCallerFromRequest(r *%s.Request) %s.Caller", httpAlias, runtimeAlias), func() {
		e.Line(`id := r.Header.Get("X-Caller-Id")`)
		e.Line(`return devCaller{authenticated: id != "", id: id}`)
	})

	e.Line("")
	e.Line("// devCaller implementa runtime.Caller para o caller de dev acima.")
	e.Block("type devCaller struct", func() {
		e.Line("authenticated bool")
		e.Line("id            string")
	})

	e.Line("")
	e.Block("func (c devCaller) Authenticated() bool", func() {
		e.Line("return c.authenticated")
	})
	e.Line("")
	e.Block("func (c devCaller) ID() string", func() {
		e.Line("return c.id")
	})
	e.Line("")
	e.Block("func (c devCaller) HasRole(role string) bool", func() {
		e.Line("return false")
	})

	e.Line("")
	e.Line("// writeBusinessError mapeia err a um status HTTP (§design 3.12, REQ-28.2):")
	e.Line("// distingue por errors.As(&runtime.BusinessError) — negócio -> 422, exceto")
	e.Line("// runtime.ErrForbidden (403), runtime.ErrNotFound (404) e")
	e.Line("// runtime.ErrIdempotencyInFlight (409, G2, spec §14 — corrida da mesma")
	e.Line("// Idempotency-Key sob \"concurrentRetry: reject\"); qualquer outro error")
	e.Line("// (infra) -> 503. runtime.ErrIdempotencyKeyConflict/ErrIdempotencyKeyRequired")
	e.Line("// (G2) caem no default (422) DE PROPÓSITO — REQ-20.4 pede 422 para o")
	e.Line("// conflito, que já é o que o default produz; não precisam de um case à")
	e.Line("// parte, ao contrário de Forbidden/NotFound/InFlight, que DEVIAM do default.")
	e.Line("// runtime.ErrNotFound (404) também é o que um Load/Query devolve para um")
	e.Line("// aggregate de OUTRO tenant (G5, spec §13.2, row_level) — o mesmo caminho de")
	e.Line("// \"não encontrado\", de propósito: cross-tenant nunca deve vazar um 403 (evita")
	e.Line("// enumeração). Rate-limit (429, G4) e tenant ausente (400 fail-closed, G5) são")
	e.Line("// resolvidos À PARTE, ANTES desta função sequer ser chamada (writeRateLimitExceeded/")
	e.Line("// requireTenant, ver as rotas acima) — nenhum dos dois chega a err aqui. Sunset")
	e.Line("// (410) continua marco posterior (G6) — deliberadamente não tratado aqui, para")
	e.Line("// não travar a extensão futura com suposições erradas.")
	e.Block(fmt.Sprintf("func writeBusinessError(w %s.ResponseWriter, err error)", httpAlias), func() {
		e.Line("var be %s.BusinessError", runtimeAlias)
		e.Block(fmt.Sprintf("if %s.As(err, &be)", errorsAlias), func() {
			e.Line("switch {")
			e.Line("case %s.Is(err, %s.ErrForbidden):", errorsAlias, runtimeAlias)
			e.Line("%s.Error(w, be.Msg, %s.StatusForbidden)", httpAlias, httpAlias)
			e.Line("case %s.Is(err, %s.ErrNotFound):", errorsAlias, runtimeAlias)
			e.Line("%s.Error(w, be.Msg, %s.StatusNotFound)", httpAlias, httpAlias)
			e.Line("case %s.Is(err, %s.ErrIdempotencyInFlight):", errorsAlias, runtimeAlias)
			e.Line("%s.Error(w, be.Msg, %s.StatusConflict)", httpAlias, httpAlias)
			e.Line("default:")
			e.Line("%s.Error(w, be.Msg, %s.StatusUnprocessableEntity)", httpAlias, httpAlias)
			e.Line("}")
			e.Line("return")
		})
		e.Line(`%s.Error(w, "internal error", %s.StatusServiceUnavailable)`, httpAlias, httpAlias)
	})
}
