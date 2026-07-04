package codegen

import (
	"fmt"
	"sort"
	"strings"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/program"
	"domainscript/symbols"
	"domainscript/types"
)

// decl_query.go emite o Go de um QueryDecl (E8.1, REQ-21.2/21.5, §design 3.9):
// uma função com os parâmetros declarados que executa o corpo e devolve o
// tipo declarado + error — leitura pura, SEM unit of work (uma Query nunca
// abre uma transação, ao contrário de UseCase/decl_usecase.go). Cobre, com
// suporte de verdade (golden + smoke + comportamental), exatamente os DOIS
// corpos que o wallet real exercita (docs/examples/wallet/read.ds):
//
//   - "return load Agg(id) as View" (REQ-21.2, a cláusula "as" de um load):
//     carrega o Aggregate normalmente — via Load<Agg> (E6.2), agora sobre o
//     adaptador runtime.NewEventLoader(ctx, store) (E8.1 acrescentou
//     EventLoader ao runtime exatamente para isto: uma Query não tem Tx) — e
//     mapeia campo a campo, POR NOME, do state carregado para o struct da
//     View (Field.Name do ViewDecl casado contra os membros do shape do
//     Aggregate carregado, mesmo padrão de resolução por nome usado alhures
//     no codegen).
//   - "return list <VO>" — um VO SEM Aggregate de origem explícito no corpo
//     (ex. "list StatementEntry"). A DEFINIÇÃO ADOTADA por esta task (não
//     havia uma prévia — a task pede explicitamente para fixá-la, ver
//     tasks.md E8.1): correlaciona o(s) parâmetro(s) da Query com o ÚNICO
//     Aggregate CONHECIDO (dentre os que o chamador passa em "aggregates",
//     mesmo padrão de EmitUseCase/E7.2) cujo "state" declara um campo do tipo
//     AppendList<VO> — e exige que a Query tenha EXATAMENTE 1 parâmetro cujo
//     tipo bate com o tipo do campo "id" desse mesmo Aggregate. Havendo essa
//     correlação única, "list VO" vira: carregar esse Aggregate pelo
//     parâmetro e devolver "...state.<Campo>.Items()". Mais de um Aggregate
//     candidato, nenhum (ou mais de um) parâmetro batendo → ERRO DE GERAÇÃO
//     claro — não um fallback "concatena tudo" inventado. Este é o suporte
//     MÍNIMO definido por esta task; mais casos (VO usado por múltiplos
//     Aggregates, paginação, distinct/sum/focus §20) entram quando surgir
//     necessidade real (Marco F+).
//
// Qualquer outro "return" cai num fallback de expressão PURA simples
// (lower.Lowerer.Expr — idents, membro, literal, construção de VO wrapper de
// 1 arg, binário) emitido como "return <expr>, nil". Formas que precisam de
// hoisting (ensure, load/list/count fora dos dois casos acima) não são
// suportadas por esta task — erro de geração claro. Statements que não são
// "return" (não exercitados pelas 2 Queries reais) delegam para
// lower.StmtLowerer normalmente.
//
// Cláusulas SQL-like (where/orderBy/skip/take) NÃO aparecem em nenhuma das
// duas Queries reais do wallet — não implementadas por esta task (suporte
// parcial documentado, não coberto): aplicá-las exigiria um mecanismo de
// query real sobre coleções em memória, que builtins.go (E5.3) já registrou
// como decisão explicitamente adiada para quando o Read Side tiver mais
// exemplos reais para desenhar a API por cima (E8 fase mínima).

// EmitQuery gera o Go de um único QueryDecl — a mesma forma de EmitQueries,
// mantendo o contrato uniforme entre as duas funções (mesmo padrão de
// EmitUseCase/EmitUseCases). aggregates é o mapa nome->decl de TODOS os
// Aggregates conhecidos do módulo (mesmo padrão de EmitUseCase/E7.2) — usado
// pela correlação de "list <VO>" (ver a doc do arquivo). prog (G1a) habilita
// o roteamento de FileStorage (§2.5) para signed_url/load File(ref) no corpo
// — nil preserva o comportamento anterior a G1a (nenhuma FileStorage
// disponível; só relevante se o corpo de fato usar uma op de arquivo).
func EmitQuery(pkg string, decl *ast.QueryDecl, aggregates map[string]*ast.AggregateDecl, prog *program.Program, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	return EmitQueries(pkg, []*ast.QueryDecl{decl}, aggregates, prog, model, tab, module, reg)
}

// EmitQueries gera o Go de vários QueryDecl num único arquivo — como um
// módulo real tem mais de uma Query (o wallet declara 2: GetWallet,
// ListEntries).
func EmitQueries(pkg string, decls []*ast.QueryDecl, aggregates map[string]*ast.AggregateDecl, prog *program.Program, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	e := emit.New(pkg)
	ctxAlias := e.Import("context")
	runtimeAlias := e.Import(RuntimeImportPath)

	// Roteamento de FileStorage (G1a, §2.5) — ver a doc análoga em
	// decl_usecase.go/EmitUseCases: calculado uma vez para o módulo.
	mod := programModule(prog, module)
	fsByField, err := moduleFileStorageRouting(aggregates, mod)
	if err != nil {
		return nil, fmt.Errorf("codegen: módulo %s: %w", module, err)
	}
	fsDefault := moduleFileStorageDefault(mod)

	// cached acumula, por Query com "cache" (G3, spec §15), o plano já
	// resolvido — emitQueryCacheWireFunc consome isso no final do arquivo
	// para montar "func WireQueryCache(d runtime.Dispatcher)" (ver
	// decl_query_cache.go).
	var cached []cachedQueryWire
	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		plan, err := emitQueryDecl(e, decl, aggregates, model, tab, module, reg, ctxAlias, runtimeAlias, fsByField, fsDefault, mod)
		if err != nil {
			return nil, err
		}
		if plan != nil {
			cached = append(cached, cachedQueryWire{name: decl.Name, plan: plan})
		}
	}
	if len(cached) > 0 {
		e.Line("")
		emitQueryCacheWireFunc(e, ctxAlias, runtimeAlias, cached)
	}
	return e.Bytes()
}

// emitQueryDecl emite a função de uma única Query: assinatura fixa "(ctx
// context.Context, store runtime.EventStore, <params...>) (<Return>, error)"
// — ctx/store sempre presentes (mesmo quando o corpo não os usa, ex. um
// fallback puro), o mesmo espírito de UseCase sempre receber ctx mesmo sem
// timeout (decl_usecase.go) — mais o corpo (ver queryBodyEmitter). Quando
// decl.Cache declara uma política (G3, spec §15), o corpo de sempre migra
// para um nome PRIVADO ("<nome>Run") e o nome público vira o wrapper de
// cache (decl_query_cache.go/emitQueryCacheWrapper) — mesmo padrão do
// wrapper de idempotência de UseCase (G2, usecase_idempotency.go). mod (G3)
// é o *program.Module do módulo — usado só para o fallback "defaultTtl" do
// bloco mod.ds Cache{} quando a Query não declara "ttl" própria; nil é
// seguro (nenhum fallback disponível, mesmo efeito de mod.ds sem Cache{}).
// Devolve o queryCachePlan resolvido (nil quando a Query não usa cache) para
// EmitQueries acumular e montar WireQueryCache.
func emitQueryDecl(e *emit.Emitter, decl *ast.QueryDecl, aggregates map[string]*ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, ctxAlias, runtimeAlias string, fsByField map[string]string, fsDefault string, mod *program.Module) (*queryCachePlan, error) {
	env := lower.New(model, tab, module)
	env.SeedQuery(decl.Params)
	l := lower.NewLowerer(env, reg, runtimeAlias)
	l.WithBuiltins(lower.NewBuiltinLowerer(runtimeAlias, "ctx", "store").WithFileStorage(fsByField, fsDefault).WithEventLoaderWrapping())

	// signed_url(ref, expires: <duração>) (G1a, §2.5) loweriza o argumento
	// "expires" via lowerDurationLiteral (lower/expr.go), que produz o texto
	// cru "time.Duration(...)" SEM jamais chamar e.Import sozinho (mesmo
	// contrato que decl_usecase.go já respeita para UseCaseDecl.Timeout) — o
	// EMISSOR precisa garantir o import ANTES de lowerizar o corpo, ou o Go
	// gerado referencia "time" sem importá-lo (erro de compilação só visível
	// no smoke, nunca em Emitter.Bytes, que só valida imports REGISTRADOS e
	// não usados — nunca o inverso).
	if bodyUsesSignedURL(decl.Body) {
		e.Import("time")
	}

	plan, err := planQueryCache(decl, aggregates, env, l, mod)
	if err != nil {
		return nil, fmt.Errorf("codegen: Query %s: %w", decl.Name, err)
	}
	if plan != nil {
		// ttl/negativeCacheTtl lowerizam para "time.Duration(...)" cru (mesma
		// razão do signed_url acima) — garantir o import ANTES do corpo.
		e.Import("time")
	}

	returnGoType, err := goname.GoFieldType(decl.Return)
	if err != nil {
		return nil, fmt.Errorf("codegen: Query %s: return: %w", decl.Name, err)
	}

	paramStrs, err := handleParamList(decl.Params)
	if err != nil {
		return nil, fmt.Errorf("codegen: Query %s: %w", decl.Name, err)
	}
	paramNames := queryParamNames(decl.Params)

	fnName := decl.Name
	if plan != nil {
		fnName = unexportedQueryRunName(decl.Name)
	}

	allParams := append([]string{
		fmt.Sprintf("ctx %s.Context", ctxAlias),
		fmt.Sprintf("store %s.EventStore", runtimeAlias),
	}, paramStrs...)
	sig := fmt.Sprintf("func %s(%s) (%s, error)", fnName, strings.Join(allParams, ", "), returnGoType)

	if plan != nil {
		e.Line("// %s carrega o corpo de sempre da Query %s (§6.3) — a borda com cache", fnName, decl.Name)
		e.Line("// (spec §15, G3) é %s, logo abaixo.", decl.Name)
	} else {
		e.Line("// %s é a Query %s (§6.3): leitura pura, sem unit of work.", decl.Name, decl.Name)
	}

	qc := &queryBodyEmitter{
		e:            e,
		env:          env,
		l:            l,
		decl:         decl,
		aggregates:   aggregates,
		model:        model,
		module:       module,
		runtimeAlias: runtimeAlias,
		returnGoType: returnGoType,
	}

	var bodyErr error
	e.Block(sig, func() {
		bodyErr = qc.emitBody(decl.Body)
	})
	if bodyErr != nil {
		return nil, fmt.Errorf("codegen: Query %s: %w", decl.Name, bodyErr)
	}

	if plan != nil {
		emitQueryCacheVar(e, decl, plan, runtimeAlias)
		emitQueryCacheWrapper(e, decl, plan, fnName, returnGoType, paramStrs, paramNames, ctxAlias, runtimeAlias)
	}

	return plan, nil
}

// queryBodyEmitter carrega o estado compartilhado pelas funções de lowering
// do corpo de uma Query (ver a doc do arquivo para as 2 formas suportadas).
type queryBodyEmitter struct {
	e            *emit.Emitter
	env          *lower.TypeEnv
	l            *lower.Lowerer
	decl         *ast.QueryDecl
	aggregates   map[string]*ast.AggregateDecl
	model        *types.Model
	module       string
	runtimeAlias string
	returnGoType string
}

// emitBody lowereiza cada statement do corpo em sequência. Statements que
// não são "return" (não exercitados pelas 2 Queries reais do wallet) delegam
// para lower.StmtLowerer; "var zero <T>" só é pré-declarado quando o corpo
// tem pelo menos um statement desses (evita "declared and not used": os 2
// corpos reais são um único ReturnStmt cada, e as formas especiais de
// emitReturn declaram seu próprio "zero" LOCAL, no ponto de uso).
func (qc *queryBodyEmitter) emitBody(b *ast.Block) error {
	if b == nil {
		return fmt.Errorf("corpo vazio")
	}

	needsZero := false
	for _, s := range b.Stmts {
		if _, ok := s.(*ast.ReturnStmt); !ok {
			needsZero = true
			break
		}
	}
	if needsZero {
		qc.e.Line("var zero %s", qc.returnGoType)
	}

	ctx := lower.StmtContext{ZeroValues: []string{"zero"}}
	sl := lower.NewStmtLowerer(qc.l, qc.e, ctx)
	for _, s := range b.Stmts {
		ret, ok := s.(*ast.ReturnStmt)
		if !ok {
			if err := sl.Stmt(s); err != nil {
				return err
			}
			continue
		}
		if err := qc.emitReturn(ret); err != nil {
			return err
		}
	}
	return nil
}

// emitReturn traduz "return <Value>": as 2 formas especiais desta task (load
// ... as V; list <VO> correlate — ver a doc do arquivo), com fallback para
// uma expressão pura simples emitida como "return <expr>, nil".
func (qc *queryBodyEmitter) emitReturn(ret *ast.ReturnStmt) error {
	if ret.Value == nil {
		return fmt.Errorf("return sem valor não suportado em Query (toda Query devolve um valor de domínio)")
	}

	if qe, ok := ret.Value.(*ast.QueryExpr); ok {
		if qe.Op == "load" {
			if _, hasAs := queryClauseExtra(qe.Clauses, "as"); hasAs {
				return qc.emitLoadAsView(qe)
			}
		}
		if qe.Op == "list" {
			if handled, err := qc.tryEmitListVO(qe); handled {
				return err
			}
		}
	}

	valueGo, err := qc.l.Expr(ret.Value)
	if err != nil {
		return fmt.Errorf("return: forma não suportada por EmitQuery (E8.1, ver a doc do arquivo): %w", err)
	}
	qc.e.Line("return %s, nil", valueGo)
	return nil
}

// --- 1. "return load Agg(id) as View" (REQ-21.2). ---

// emitLoadAsView traduz "load Agg(id) as View": carrega Agg via Load<Agg>
// sobre runtime.NewEventLoader(ctx, store) (a razão de EventLoader existir no
// runtime — ver a doc do arquivo) e mapeia, campo a campo POR NOME, o state
// carregado para o struct de View.
func (qc *queryBodyEmitter) emitLoadAsView(qe *ast.QueryExpr) error {
	viewName, _ := queryClauseExtra(qe.Clauses, "as")
	if viewName != qc.returnGoType {
		return fmt.Errorf("load ... as %s: não bate com o tipo de retorno declarado da Query (%s)", viewName, qc.returnGoType)
	}

	call, ok := qe.Target.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return fmt.Errorf("load ... as %s: forma inesperada de alvo (%T) — esperava \"load Agg(id)\"", viewName, qe.Target)
	}
	aggIdent, ok := call.Fn.(*ast.Ident)
	if !ok {
		return fmt.Errorf("load ... as %s: alvo de load não é um Aggregate nomeado (%T)", viewName, call.Fn)
	}
	aggName := aggIdent.Name

	aggShape, err := qc.shapeOf(aggName, symbols.KindAggregate)
	if err != nil {
		return fmt.Errorf("load %s(...) as %s: %w", aggName, viewName, err)
	}
	viewShape, err := qc.shapeOf(viewName, symbols.KindView)
	if err != nil {
		return fmt.Errorf("load %s(...) as %s: %w", aggName, viewName, err)
	}

	stateFields := make(map[string]bool, len(aggShape.Fields))
	for _, f := range aggShape.Fields {
		stateFields[f.Name] = true
	}
	for _, f := range viewShape.Fields {
		if !stateFields[f.Name] {
			return fmt.Errorf("load %s(...) as %s: campo %q da View não existe no state de %s", aggName, viewName, f.Name, aggName)
		}
	}

	idGo, err := qc.l.Expr(call.Args[0].Value)
	if err != nil {
		return fmt.Errorf("load %s(...): %w", aggName, err)
	}

	localVar := goname.Ident(strings.ToLower(aggName))
	qc.e.Line("%s, err := Load%s(%s.NewEventLoader(ctx, store), %s)", localVar, aggName, qc.runtimeAlias, idGo)
	qc.e.Block("if err != nil", func() {
		qc.e.Line("var zero %s", qc.returnGoType)
		qc.e.Line("return zero, err")
	})

	assigns := make([]string, len(viewShape.Fields))
	for i, f := range viewShape.Fields {
		assigns[i] = fmt.Sprintf("%s: %s.state.%s", goname.ExportField(f.Name), localVar, goname.ExportField(f.Name))
	}
	qc.e.Line("return %s{%s}, nil", viewName, strings.Join(assigns, ", "))
	return nil
}

// --- 2. "return list <VO>" (definição desta task — ver a doc do arquivo). ---

// tryEmitListVO reconhece e traduz "list <VO>" (Target é um Ident que resolve
// a um ValueObject, não a um Aggregate). handled=false (sem erro) quando
// Target não é essa forma — o chamador segue para o fallback genérico.
// handled=true com err!=nil é uma falha de geração de verdade que o chamador
// deve propagar (a forma FOI reconhecida como "list <VO>", mas a correlação
// ou as cláusulas não são suportadas).
func (qc *queryBodyEmitter) tryEmitListVO(qe *ast.QueryExpr) (handled bool, err error) {
	ident, ok := qe.Target.(*ast.Ident)
	if !ok {
		return false, nil
	}
	voName := ident.Name
	if _, ok := qc.env.TypeOfName(voName).(*types.VOType); !ok {
		return false, nil // não resolve a um VO — não é o caso definido por esta task
	}

	if len(qe.Clauses) > 0 {
		return true, fmt.Errorf("list %s: cláusulas SQL-like (where/orderBy/skip/take/as) sobre \"list <VO>\" não são suportadas nesta task (E8.1) — suporte mínimo definido pela task; mais casos entram quando surgir necessidade real", voName)
	}

	wantElem, ok := listReturnElement(qc.decl.Return)
	if !ok || wantElem != voName {
		return true, fmt.Errorf("list %s: tipo de retorno da Query deveria ser List<%s>, declarado %s", voName, voName, qc.decl.Return.Name)
	}

	aggName, fieldName, err := correlateListVOAggregate(qc.aggregates, voName)
	if err != nil {
		return true, fmt.Errorf("list %s: %w", voName, err)
	}
	if len(qc.decl.Params) != 1 {
		return true, fmt.Errorf("list %s: correlação com %s exige exatamente 1 parâmetro na Query (achei %d)", voName, aggName, len(qc.decl.Params))
	}
	param := qc.decl.Params[0]

	aggShape, err := qc.shapeOf(aggName, symbols.KindAggregate)
	if err != nil {
		return true, fmt.Errorf("list %s: %w", voName, err)
	}
	idFieldType, ok := aggregateIDFieldType(aggShape)
	if !ok {
		return true, fmt.Errorf("list %s: Aggregate %s não declara campo \"id\" em state", voName, aggName)
	}
	paramType := qc.model.TypeOfRef(qc.module, param.Type)
	if !types.Identical(paramType, idFieldType) {
		return true, fmt.Errorf("list %s: parâmetro %s (%s) não bate com o tipo do id de %s (%s)", voName, param.Name, paramType.String(), aggName, idFieldType.String())
	}

	paramGo := goname.Ident(param.Name)
	localVar := goname.Ident(strings.ToLower(aggName))
	qc.e.Line("%s, err := Load%s(%s.NewEventLoader(ctx, store), %s)", localVar, aggName, qc.runtimeAlias, paramGo)
	qc.e.Block("if err != nil", func() {
		qc.e.Line("var zero %s", qc.returnGoType)
		qc.e.Line("return zero, err")
	})
	qc.e.Line("return %s.state.%s.Items(), nil", localVar, goname.ExportField(fieldName))
	return true, nil
}

// correlateListVOAggregate implementa a correlação de "list <VO>" definida
// por esta task (ver a doc do arquivo): dentre aggregates (iterado em ordem
// alfabética de nome, para determinismo — NFR-13, mesmo quando o resultado é
// um erro que lista candidatos), acha o Aggregate cujo state declara um campo
// do tipo AppendList<voName>. Exige exatamente 1 candidato (1 Aggregate, 1
// campo) — mais de um é ambíguo, zero não correlaciona.
func correlateListVOAggregate(aggregates map[string]*ast.AggregateDecl, voName string) (aggName, fieldName string, err error) {
	names := make([]string, 0, len(aggregates))
	for name := range aggregates {
		names = append(names, name)
	}
	sort.Strings(names)

	type candidate struct{ aggName, fieldName string }
	var candidates []candidate
	for _, name := range names {
		decl := aggregates[name]
		if decl == nil {
			continue
		}
		for _, f := range decl.State {
			if f == nil || f.Type == nil {
				continue
			}
			if f.Type.Name == "AppendList" && len(f.Type.Args) == 1 && f.Type.Args[0].Name == voName {
				candidates = append(candidates, candidate{aggName: name, fieldName: f.Name})
			}
		}
	}

	if len(candidates) != 1 {
		return "", "", fmt.Errorf("não consegui correlacionar com exatamente 1 Aggregate cujo state declare um campo AppendList<%s> (achei %d candidato(s))", voName, len(candidates))
	}
	return candidates[0].aggName, candidates[0].fieldName, nil
}

// --- helpers compartilhados. ---

// bodyUsesSignedURL reporta se b contém, em qualquer profundidade, uma
// chamada "signed_url(...)" (§2.5, G1a) — ver a doc de emitQueryDecl sobre
// por que isso precisa ser decidido ANTES de lowerizar o corpo (import de
// "time"). Reusada por decl_usecase.go pela mesma razão (signed_url também
// pode aparecer dentro de um UseCase.execute, não só de Query.Body).
func bodyUsesSignedURL(b *ast.Block) bool {
	found := false
	astutil.ForEachExprInBlock(b, func(e ast.Expr) {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return
		}
		if id, ok := call.Fn.(*ast.Ident); ok && id.Name == "signed_url" {
			found = true
		}
	})
	return found
}

// listReturnElement devolve o nome do elemento de um TypeRef "List<Elem>",
// ok=false para qualquer outra forma (incl. Args com aridade != 1).
func listReturnElement(ret *ast.TypeRef) (string, bool) {
	if ret == nil || ret.Name != "List" || len(ret.Args) != 1 {
		return "", false
	}
	return ret.Args[0].Name, true
}

// aggregateIDFieldType acha o tipo (já resolvido via types.Model) do campo
// "id" do state de shape — mesma convenção de aggregateIDGoType
// (decl_aggregate.go), mas devolvendo o types.Type (não a forma Go) para
// comparação de identidade contra o tipo de um parâmetro.
func aggregateIDFieldType(shape *types.ShapeType) (types.Type, bool) {
	for _, f := range shape.Fields {
		if f.Name == "id" {
			return f.Type, true
		}
	}
	return nil, false
}

// shapeOf resolve name a um *types.ShapeType de Kind kind (Lookup via
// lower.TypeEnv.TypeOfName — Lookup local ao módulo, fallback Find
// cross-module) — mesmo padrão de refFieldGoType (decl_command.go)/
// validateAppliersResolveToEvents (decl_aggregate_load.go), agora
// parametrizado pelo Kind esperado (Aggregate ou View).
func (qc *queryBodyEmitter) shapeOf(name string, kind symbols.Kind) (*types.ShapeType, error) {
	t := qc.env.TypeOfName(name)
	if types.IsError(t) {
		return nil, fmt.Errorf("%s: símbolo não resolvido (bug de geração — REQ-9 já deveria ter barrado isso)", name)
	}
	shape, ok := t.(*types.ShapeType)
	if !ok || shape.Kind != kind {
		return nil, fmt.Errorf("%s: não resolve a um %s (got %T)", name, kind, t)
	}
	return shape, nil
}

// queryClauseExtra procura a cláusula de keyword kw entre clauses e devolve
// seu Extra (ex.: o nome do tipo de "as") — mesma forma de hasQueryClause/
// queryClauseByKw (codegen/lower/builtins.go), reimplementada aqui porque
// aquelas são não-exportadas do pacote lower.
func queryClauseExtra(clauses []ast.QueryClause, kw string) (string, bool) {
	for _, c := range clauses {
		if c.Kw == kw {
			return c.Extra, true
		}
	}
	return "", false
}
