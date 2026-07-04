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
// (429, G4, ver emitRateLimitGate acima e codegen/ratelimit.go) já está
// coberto; tenant ausente (400) e sunset (410) continuam marcos G5/G6 —
// nem mencionados aqui de propósito, para não travar a extensão futura.
//
// --- Forma do corpo de resposta (decisão desta task) ---
//
// UseCase: a função gerada só devolve error (sem dado de retorno) — sucesso
// vira "204 No Content", sem corpo: não há dado de domínio para serializar,
// e 204 é a forma idiomática HTTP de "aceito, nada a devolver" (a alternativa
// de emitir "{}" só empurraria um corpo vazio sem significado para o
// cliente). Query: sucesso vira "200" + o valor de retorno serializado como
// JSON (Content-Type: application/json).

// emitHTTPRoutes emite, na posição ATUAL de e (chamado de dentro de func
// main(), logo após "mux := http.NewServeMux()" — ver
// codegen.go/generateCmdMainFile), um mux.HandleFunc(...) por ast.Route de
// iface. iface nil ou sem Routes não emite nada (hadRoutes=false) — o
// chamador usa hadRoutes para decidir se emite os helpers de borda
// (devCaller/writeBusinessError, emitHTTPHelpers): não faz sentido importar
// "errors"/"encoding/json" num service sem nenhuma rota. rlPending acumula as
// declarações de rate limit (G4, spec §16) que ALGUMA rota enfileirou —
// vazio quando nenhuma rota configura "rateLimit"; o chamador
// (generateCmdMainFile) as renderiza de volta no nível de pacote depois que
// este bloco (newMux) fecha (ver a doc de pendingRateLimitPlan,
// codegen/ratelimit.go, sobre por que isso não pode ser emitido aqui
// dentro).
func emitHTTPRoutes(e *emit.Emitter, muxVar string, iface *ast.InterfaceDecl, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable, prog *program.Program) (hadRoutes bool, rlPending []pendingRateLimitPlan, err error) {
	if iface == nil || len(iface.Routes) == 0 {
		return false, nil, nil
	}
	rlEnv := newHTTPRateLimitEnv(prog, iface)
	for _, route := range iface.Routes {
		if err := emitRoute(e, muxVar, route, buckets, modules, model, tab, rlEnv); err != nil {
			return true, *rlEnv.pending, fmt.Errorf("rota %s %q -> %s: %w", route.Method, route.Path, route.Target, err)
		}
	}
	return true, *rlEnv.pending, nil
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
// (codegen/ratelimit.go).
func emitRoute(e *emit.Emitter, muxVar string, route *ast.Route, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable, rlEnv *httpRateLimitEnv) error {
	target, err := resolveRouteTarget(buckets, modules, route.Target)
	if err != nil {
		return err
	}
	if target.usecase != nil {
		return emitUseCaseRoute(e, muxVar, route, target.usecase, target.ucModule, buckets, modules, model, tab, rlEnv)
	}
	return emitQueryRoute(e, muxVar, route, target.query, target.qModule, model, tab, rlEnv)
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

// emitCallerAndIdempotency emite as duas linhas comuns a TODO handler (§design
// 3.12): o caller de dev extraído do header "X-Caller-Id" posto em ctx, e
// "Idempotency-Key" repassado via runtime.WithIdempotencyKey quando presente
// (carrier só — enforcement é G2, ver contextkeys.go.txt).
func emitCallerAndIdempotency(e *emit.Emitter, runtimeAlias string) {
	e.Line("caller := devCallerFromRequest(r)")
	e.Line("ctx := %s.WithCaller(r.Context(), caller)", runtimeAlias)
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
func emitUseCaseRoute(e *emit.Emitter, muxVar string, route *ast.Route, uc *ast.UseCaseDecl, ucModule string, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable, rlEnv *httpRateLimitEnv) error {
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

	pattern := route.Method + " " + route.Path
	header := fmt.Sprintf("%s.HandleFunc(%s, func(w %s.ResponseWriter, r *%s.Request)", muxVar, strconv.Quote(pattern), httpAlias, httpAlias)

	var bodyErr error
	e.BlockSuffix(header, ")", func() {
		emitCallerAndIdempotency(e, runtimeAlias)
		if checksFuncName != "" && !hasIdempotency {
			emitRateLimitGate(e, checksFuncName, runtimeAlias)
		}

		e.Line("var cmd %s.%s", cmdAlias, cmdDecl.Name)
		e.Block("if r.ContentLength != 0", func() {
			jsonAlias := e.Import("encoding/json")
			e.Block(fmt.Sprintf("if err := %s.NewDecoder(r.Body).Decode(&cmd); err != nil", jsonAlias), func() {
				fmtAlias := e.Import("fmt")
				e.Line("%s.Error(w, %s.Sprintf(%q, err), %s.StatusBadRequest)", httpAlias, fmtAlias, "invalid request body: %s", httpAlias)
				e.Line("return")
			})
		})

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

		e.Block(fmt.Sprintf("if err := %s.%s(ctx, cmd); err != nil", ucAlias, uc.Name), func() {
			e.Line("writeBusinessError(w, err)")
			e.Line("return")
		})
		e.Line("w.WriteHeader(%s.StatusNoContent)", httpAlias)
	})
	return bodyErr
}

// emitQueryRoute emite um mux.HandleFunc para uma rota cujo Target resolve a
// uma Query (ver a doc do arquivo: cada parâmetro declarado vem de um path
// param de mesmo nome, na ordem declarada; sucesso 200 + JSON, erro via
// writeBusinessError). rlEnv (G4, spec §16) resolve/enfileira o rate
// limiting da rota, quando configurado — ver emitRateLimitGate. Query não
// tem conceito de idempotência (REQ-20.4/G2 só cobre Command), então a
// checagem sempre roda ANTES de tudo, sem exceção.
func emitQueryRoute(e *emit.Emitter, muxVar string, route *ast.Route, decl *ast.QueryDecl, qModule string, model *types.Model, tab *symbols.SymbolTable, rlEnv *httpRateLimitEnv) error {
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

	pattern := route.Method + " " + route.Path
	header := fmt.Sprintf("%s.HandleFunc(%s, func(w %s.ResponseWriter, r *%s.Request)", muxVar, strconv.Quote(pattern), httpAlias, httpAlias)

	var bodyErr error
	e.BlockSuffix(header, ")", func() {
		emitCallerAndIdempotency(e, runtimeAlias)
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

		callArgs := append([]string{"ctx", "store"}, argNames...)
		e.Line("result, err := %s.%s(%s)", qAlias, decl.Name, strings.Join(callArgs, ", "))
		e.Block("if err != nil", func() {
			e.Line("writeBusinessError(w, err)")
			e.Line("return")
		})
		e.Line(`w.Header().Set("Content-Type", "application/json")`)
		jsonAlias := e.Import("encoding/json")
		e.Block(fmt.Sprintf("if err := %s.NewEncoder(w).Encode(result); err != nil", jsonAlias), func() {
			fmtAlias := e.Import("fmt")
			e.Line("%s.Error(w, %s.Sprintf(%q, err), %s.StatusInternalServerError)", httpAlias, fmtAlias, "failed to encode response: %s", httpAlias)
		})
	})
	return bodyErr
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
	e.Line("// Rate-limit (429, G4) é resolvido À PARTE, ANTES desta função sequer ser")
	e.Line("// chamada (writeRateLimitExceeded, ver as rotas com \"rateLimit\" acima) —")
	e.Line("// nunca chega a err aqui. Tenant ausente (400) e sunset (410) continuam")
	e.Line("// marcos posteriores (G5/G6) — deliberadamente não tratados aqui, para não")
	e.Line("// travar a extensão futura com suposições erradas.")
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
