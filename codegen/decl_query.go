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
	"domainscript/token"
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
// 1 arg, binário) emitido como "return <expr>, nil". Statements que não são
// "return" (não exercitados pelas 2 Queries reais) delegam para
// lower.StmtLowerer normalmente.
//
// --- Cláusulas SQL-like (where/orderBy/skip/take/as) — ciclo Read Side
// (REQ-33/REQ-34, §design read-side 3.4/3.5, tasks I3.1/I3.2) ---
//
// O "mínimo de E8.1" acima (exatamente "load Agg(id) as View" ou "list <VO>"
// SEM cláusula nenhuma) continua o caminho RÁPIDO — histórico, byte-idêntico
// — para as 2 Queries reais do wallet. QUALQUER outra forma de QueryExpr
// (cláusulas presentes em "load X(id).<campo>" ou em "list <VO>") delega ao
// hoisting de CORPO completo (lower.StmtLowerer.ExprHoisted — o MESMO
// mecanismo que Handle/Apply/UseCase/Policy usam, "hoist then return the
// temp", ver stmt.go/returnStmt) via emitHoistedQueryReturn — "list <VO>
// [cláusulas]" primeiro se REESCREVE para "load <Agg correlacionado>(<param>)
// .<campo> [cláusulas]" (tryEmitListVO, abaixo), a mesma forma que
// StmtLowerer.hoistLoadCollection (I3.1, stmt.go) já sabe hoistear por
// inteiro (where/orderBy/skip/take), sem nenhuma lógica de query nova aqui.
// A cláusula "as V" (REQ-34, a projeção para View) é tratada SEPARADAMENTE
// por emitHoistedQueryReturn: removida ANTES de hoistear (StmtLowerer não
// sabe o que é uma View) e aplicada NUM LOOP por item sobre o resultado
// materializado — reusando projectFieldAssignments, a MESMA rotina de
// mapeamento campo-a-campo (com achatamento de VO composto, ex.
// "amount_value"/"amount_currency" de um campo Money) que emitLoadAsView usa
// para a forma "load X(id) as V" de sempre (ambas chamam a mesma função,
// agora factorada — ver a doc de projectFieldAssignments).

// EmitQuery gera o Go de um único QueryDecl — a mesma forma de EmitQueries,
// mantendo o contrato uniforme entre as duas funções (mesmo padrão de
// EmitUseCase/EmitUseCases). aggregates é o mapa nome->decl de TODOS os
// Aggregates conhecidos do módulo (mesmo padrão de EmitUseCase/E7.2) — usado
// pela correlação de "list <VO>" (ver a doc do arquivo). prog (G1a) habilita
// o roteamento de FileStorage (§2.5) para signed_url/load File(ref) no corpo
// — nil preserva o comportamento anterior a G1a (nenhuma FileStorage
// disponível; só relevante se o corpo de fato usar uma op de arquivo).
func EmitQuery(pkg string, decl *ast.QueryDecl, aggregates map[string]*ast.AggregateDecl, prog *program.Program, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	return EmitQueries(pkg, []*ast.QueryDecl{decl}, aggregates, prog, model, tab, module, reg, nil)
}

// EmitQueries gera o Go de vários QueryDecl num único arquivo — como um
// módulo real tem mais de uma Query (o wallet declara 2: GetWallet,
// ListEntries). sharedCollectionVars (ISSUE-1, ver a doc de
// decl_collections.go) é o mapa tipo->var de runtime.Collection[T] JÁ
// declarado em collections.go pelo CHAMADOR (generateModuleFiles) para os
// tipos que TAMBÉM são usados por list/count de alguma Policy do mesmo
// módulo (a interseção calculada por sharedModuleCollectionTypeNames) — esta
// função continua calculando sozinha TODO o conjunto de tipos que alguma
// Query do arquivo usa como fonte de join, mas, para um tipo presente em
// sharedCollectionVars, reusa o var de lá em vez de declarar o seu (evita a
// redeclaração, ISSUE-1); qualquer outro tipo (o caso comum: nil ou vazio)
// continua sendo declarado localmente em queries.go
// (emitQueryJoinCollectionVars), Go byte-idêntico ao de antes desta task.
func EmitQueries(pkg string, decls []*ast.QueryDecl, aggregates map[string]*ast.AggregateDecl, prog *program.Program, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, sharedCollectionVars map[string]string) ([]byte, error) {
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

	// Collection[T] var por tipo referenciado como fonte de um "join" (I5.1,
	// §design read-side 3.7 passo 1) — ver a doc de emitQueryJoinCollectionVars.
	// Para um tipo presente em sharedCollectionVars (ISSUE-1, ver a doc de
	// decl_collections.go — o CHAMADOR já declarou esse var em collections.go
	// porque uma Policy do mesmo módulo também o usa via list/count), reusa o
	// var de lá em vez de declarar de novo aqui; qualquer outro tipo (o caso
	// comum: sharedCollectionVars nil ou sem esse tipo) continua declarado
	// localmente, em queries.go. joinTypeToVar fica nil (o default) quando
	// NENHUMA Query do arquivo usa join: preserva Go idêntico ao gerado antes
	// desta task para todo módulo sem join (GetStatement/GetWallet, ex.).
	var joinTypeToVar map[string]string
	if joinTypeNames := queryJoinCollectionTypeNames(decls); len(joinTypeNames) > 0 {
		joinTypeToVar = make(map[string]string, len(joinTypeNames))
		var toDeclare []string
		for _, name := range joinTypeNames {
			if v, ok := sharedCollectionVars[name]; ok {
				joinTypeToVar[name] = v
				continue
			}
			toDeclare = append(toDeclare, name)
		}
		if len(toDeclare) > 0 {
			for name, v := range emitQueryJoinCollectionVars(e, runtimeAlias, toDeclare) {
				joinTypeToVar[name] = v
			}
		}
	}

	// cached acumula, por Query com "cache" (G3, spec §15), o plano já
	// resolvido — emitQueryCacheWireFunc consome isso no final do arquivo
	// para montar "func WireQueryCache(d runtime.Dispatcher)" (ver
	// decl_query_cache.go).
	var cached []cachedQueryWire
	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		plan, err := emitQueryDecl(e, decl, aggregates, model, tab, module, reg, ctxAlias, runtimeAlias, fsByField, fsDefault, mod, joinTypeToVar)
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
func emitQueryDecl(e *emit.Emitter, decl *ast.QueryDecl, aggregates map[string]*ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, ctxAlias, runtimeAlias string, fsByField map[string]string, fsDefault string, mod *program.Module, joinTypeToVar map[string]string) (*queryCachePlan, error) {
	env := lower.New(model, tab, module)
	env.SeedQuery(decl.Params)
	l := lower.NewLowerer(env, reg, runtimeAlias).WithEmitter(e)
	builtins := lower.NewBuiltinLowerer(runtimeAlias, "ctx", "store").WithFileStorage(fsByField, fsDefault).WithEventLoaderWrapping()
	if len(joinTypeToVar) > 0 {
		// Roteia "list T ..." para o Collection[T] de join quando T é uma
		// fonte de join deste arquivo (I5.1); qualquer outro tipo (ex. "load
		// Agg(id)") cai no MESMO texto que WithEventLoaderWrapping já produzia
		// sozinho — byte-idêntico, preservado explicitamente aqui porque
		// WithPerAggregateStore, quando anexado, assume o roteamento POR
		// INTEIRO (ver a doc de BuiltinLowerer.store, lower/builtins.go).
		builtins = builtins.WithPerAggregateStore(func(typeName string) string {
			if v, ok := joinTypeToVar[typeName]; ok {
				return v
			}
			return fmt.Sprintf("%s.NewEventLoader(ctx, store)", runtimeAlias)
		})
	}
	l.WithBuiltins(builtins)

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
		if err := emitQueryCacheVar(e, decl, plan, returnGoType, runtimeAlias, mod); err != nil {
			return nil, err
		}
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

	ctx := lower.StmtContext{ZeroValues: []string{"zero"}, CtxVar: "ctx"}
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

// emitReturn traduz "return <Value>": os 2 fast-paths históricos de E8.1
// (load Agg(id) as V; list <VO> sem cláusula nenhuma), o caminho geral de
// I3.1/I3.2 para qualquer QueryExpr com cláusulas (ver a doc do arquivo), com
// fallback final para uma expressão pura simples emitida como "return
// <expr>, nil".
func (qc *queryBodyEmitter) emitReturn(ret *ast.ReturnStmt) error {
	if ret.Value == nil {
		return fmt.Errorf("return sem valor não suportado em Query (toda Query devolve um valor de domínio)")
	}

	if qe, ok := ret.Value.(*ast.QueryExpr); ok {
		if qe.Op == "load" {
			if _, hasAs := queryClauseExtra(qe.Clauses, "as"); hasAs {
				// emitLoadAsView só reconhece Target == CallExpr (o "load
				// Agg(id)" de sempre, um ÚNICO objeto). "load Agg(id).<campo>
				// ... as V" (Target == MemberExpr, I3.1) tem Target de forma
				// DIFERENTE e cai no caminho geral, abaixo.
				if _, isMemberTarget := qe.Target.(*ast.MemberExpr); !isMemberTarget {
					return qc.emitLoadAsView(qe)
				}
			}
		}
		if qe.Op == "list" {
			if queryExprHasJoin(qe.Clauses) {
				return qc.emitHoistedJoinReturn(qe)
			}
			if handled, err := qc.tryEmitListVO(qe); handled {
				return err
			}
		}
		if handled, err := qc.tryEmitHoistedQueryReturn(qe); handled {
			return err
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
// runtime — ver a doc do arquivo) e mapeia, campo a campo, o state carregado
// para o struct de View — via projectFieldAssignments (REQ-34.1, achatamento
// de VO composto incluso; para WalletView, cujos campos batem 1:1 por nome
// com o state de Wallet, produz o MESMO texto de sempre — a extração para
// uma rotina compartilhada com a projeção por item de I3.2 não muda esta
// forma, só deixa de duplicá-la).
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

	assigns, err := projectFieldAssignments(localVar+".state", aggShape.Fields, viewShape.Fields)
	if err != nil {
		return fmt.Errorf("load %s(...) as %s: %w", aggName, viewName, err)
	}
	qc.e.Line("return %s{%s}, nil", viewName, strings.Join(assigns, ", "))
	return nil
}

// --- 2. "return list <VO>" (definição desta task — ver a doc do arquivo). ---

// tryEmitListVO reconhece e traduz "list <VO> [cláusulas]" (Target é um
// Ident que resolve a um ValueObject, não a um Aggregate). handled=false
// (sem erro) quando Target não é essa forma — o chamador segue para o
// fallback genérico. handled=true com err!=nil é uma falha de geração de
// verdade que o chamador deve propagar (a forma FOI reconhecida como "list
// <VO>", mas a correlação não é possível).
//
// A correlação com o Aggregate único (correlateListVOAggregate, abaixo) é a
// MESMA nos dois ramos; só o que acontece DEPOIS diverge (§design read-side
// 3.5): SEM cláusula nenhuma, o caminho histórico de E8.1 (Load + ".Items()"
// direto, byte-idêntico ao golden anterior a este ciclo); COM cláusulas
// (where/orderBy/skip/take/as — I3.2), "list <VO> [cláusulas]" é REESCRITO
// para "load <Agg>(<param>).<campo> [MESMAS cláusulas]" — a forma que
// StmtLowerer.hoistLoadCollection (I3.1, stmt.go) já sabe hoistear por
// inteiro — e delegado a emitHoistedQueryReturn, exatamente como se o
// programa tivesse escrito a forma "load" diretamente.
func (qc *queryBodyEmitter) tryEmitListVO(qe *ast.QueryExpr) (handled bool, err error) {
	ident, ok := qe.Target.(*ast.Ident)
	if !ok {
		return false, nil
	}
	voName := ident.Name
	if _, ok := qc.env.TypeOfName(voName).(*types.VOType); !ok {
		return false, nil // não resolve a um VO — não é o caso definido por esta task
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

	if len(qe.Clauses) == 0 {
		wantElem, ok := listReturnElement(qc.decl.Return)
		if !ok || wantElem != voName {
			return true, fmt.Errorf("list %s: tipo de retorno da Query deveria ser List<%s>, declarado %s", voName, voName, qc.decl.Return.Name)
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

	synthesized := ast.NewQueryExpr("load",
		ast.NewMemberExpr(
			ast.NewCallExpr(ast.NewIdent(aggName, qe.Span()), []ast.Arg{{Value: ast.NewIdent(param.Name, qe.Span())}}, qe.Span()),
			fieldName, token.Pos{}, qe.Span()),
		qe.Binding, qe.Clauses, qe.Span())
	return true, qc.emitHoistedQueryReturn(synthesized)
}

// --- 3. Caminho geral de I3.1/I3.2 (§design read-side 3.4/3.5): qualquer
// QueryExpr com cláusulas que os 2 fast-paths acima não reconheceram. ---

// tryEmitHoistedQueryReturn reconhece "load T(id).<campo> [cláusulas]"
// (Target == MemberExpr sobre a construção de um Aggregate, I3.1) — a ÚNICA
// forma que chega aqui diretamente do corpo da Query (a forma "list <VO>
// [cláusulas]" já foi REESCRITA para esta mesma forma por tryEmitListVO antes
// de chamar emitHoistedQueryReturn). handled=false (sem erro) para qualquer
// outro Op/Target — o chamador segue para o fallback de expressão pura.
func (qc *queryBodyEmitter) tryEmitHoistedQueryReturn(qe *ast.QueryExpr) (handled bool, err error) {
	if qe.Op != "load" {
		return false, nil
	}
	if _, isMemberTarget := qe.Target.(*ast.MemberExpr); !isMemberTarget {
		return false, nil
	}
	return true, qc.emitHoistedQueryReturn(qe)
}

// emitHoistedQueryReturn traduz "return <QueryExpr>" pelo caminho GERAL de
// I3.1/I3.2 (§design read-side 3.4/3.5): delega ao MESMO hoisting de corpo
// (lower.StmtLowerer.ExprHoisted) usado em qualquer outro construto — "hoist
// then return the temp", o padrão de stmt.go/returnStmt — em vez de
// reimplementar orderBy/skip/take/predicado aqui. qe.Target já deve ser um
// MemberExpr (StmtLowerer.hoistLoadCollection, stmt.go, I3.1); o Op é sempre
// "load" (tryEmitListVO já reescreveu "list <VO> [cláusulas]" para essa
// forma antes de chegar aqui).
//
// A cláusula "as V" (REQ-34) é tratada FORA do hoisting: StmtLowerer/
// Query[T] não sabem o que é uma View (conceito Read Side) — esta função
// REMOVE "as" ANTES de hoistear (senão TypeEnv inferiria List<V> para uma
// variável Go que na verdade recebe []T — ver env.go/inferQueryExpr) e, com
// "as" presente, aplica a projeção campo-a-campo NUM LOOP sobre o resultado
// materializado (emitAsProjection, abaixo), produzindo []V.
//
// ZeroValues é "nil" (não "zero"): toda forma que chega aqui (load
// X(id).<campo> [...], list <VO> [...] reescrito por tryEmitListVO) sempre
// devolve uma LISTA (List<T> ou List<V>) — ao contrário de emitLoadAsView/
// tryEmitListVO's ramo sem cláusula (que devolvem um struct de View único e
// por isso precisam de "var zero <View>" local), o zero value Go de um
// slice É "nil" diretamente, sem declaração nenhuma — usar "zero" aqui
// referenciaria uma variável que NUNCA é declarada neste caminho
// (emitBody só declara "var zero" quando o corpo tem statement não-return
// ANTES do return, o que nunca é o caso de um corpo de 1 linha só).
func (qc *queryBodyEmitter) emitHoistedQueryReturn(qe *ast.QueryExpr) error {
	viewName, hasAs := queryClauseExtra(qe.Clauses, "as")
	stripped := qe
	if hasAs {
		stripped = ast.NewQueryExpr(qe.Op, qe.Target, qe.Binding, stripClause(qe.Clauses, "as"), qe.Span())
	}

	sl := lower.NewStmtLowerer(qc.l, qc.e, lower.StmtContext{ZeroValues: []string{"nil"}, CtxVar: "ctx"})
	itemsGo, hoisted, err := sl.ExprHoisted(stripped)
	if err != nil {
		return fmt.Errorf("return: %w", err)
	}
	for _, line := range hoisted {
		qc.e.Line("%s", line)
	}

	if !hasAs {
		qc.e.Line("return %s, nil", itemsGo)
		return nil
	}
	return qc.emitAsProjection(stripped, itemsGo, viewName)
}

// --- 4. "return list A a join B b on ... [where ...] [as V]" (I5.1,
// §design read-side 3.7). ---

// emitHoistedJoinReturn traduz "return list A a join B b on ... [where ...]
// [as V]". AO CONTRÁRIO de emitHoistedQueryReturn (que REMOVE "as" antes de
// hoistear e projeta NUM LOOP, por cima, porque a fonte tem UM tipo só —
// projectFieldAssignments/emitAsProjection), join resolve "as" INTEIRO
// dentro do próprio hoisting (lower.StmtLowerer.hoistJoin, codegen/lower/
// join.go): a projeção precisa dos DOIS aliases em escopo, que só existem
// DENTRO do loop que o hoisting já constrói — não há como separar essas duas
// etapas como no caminho de uma fonte só. Esta função só valida o tipo de
// retorno declarado da Query contra a forma esperada (List<V> com "as", ou
// List<AliasBase> sem "as", §design read-side 3.7 ponto 3) e emite o
// resultado, já pronto, que o hoisting devolve.
func (qc *queryBodyEmitter) emitHoistedJoinReturn(qe *ast.QueryExpr) error {
	viewName, hasAs := queryClauseExtra(qe.Clauses, "as")
	wantElem, ok := listReturnElement(qc.decl.Return)
	var declared string
	if qc.decl.Return != nil {
		declared = qc.decl.Return.Name
	} else {
		declared = "vazio"
	}
	if hasAs {
		if !ok || wantElem != viewName {
			return fmt.Errorf("... as %s: tipo de retorno da Query deveria ser List<%s>, declarado %s", viewName, viewName, declared)
		}
	} else {
		aliasTypeName := astutil.HeadName(qe.Target)
		if !ok || wantElem != aliasTypeName {
			return fmt.Errorf("list ... join ...: tipo de retorno da Query deveria ser List<%s> (o alias base, sem \"as\"), declarado %s", aliasTypeName, declared)
		}
	}

	sl := lower.NewStmtLowerer(qc.l, qc.e, lower.StmtContext{ZeroValues: []string{"nil"}, CtxVar: "ctx"})
	itemsGo, hoisted, err := sl.ExprHoisted(qe)
	if err != nil {
		return fmt.Errorf("return: %w", err)
	}
	for _, line := range hoisted {
		qc.e.Line("%s", line)
	}
	qc.e.Line("return %s, nil", itemsGo)
	return nil
}

// emitAsProjection projeta itemsGo ([]T, já materializado por
// emitHoistedQueryReturn) para []V (viewName) NUM LOOP, campo a campo
// (REQ-34.1) — reusa projectFieldAssignments (a MESMA rotina que
// emitLoadAsView usa para "load X(id) as V", agora aplicada por ITEM em vez
// de uma vez só). itemType vem de qc.env.ItemTypeOf(stripped) — o MESMO
// mecanismo que hoistQueryPredicate/hoistOrderBy (stmt.go) usam para tipar o
// binding do item, garantindo que "o item de origem" aqui é EXATAMENTE o
// tipo que o hoisting acabou de produzir.
func (qc *queryBodyEmitter) emitAsProjection(stripped *ast.QueryExpr, itemsGo, viewName string) error {
	wantElem, ok := listReturnElement(qc.decl.Return)
	if !ok || wantElem != viewName {
		return fmt.Errorf("... as %s: tipo de retorno da Query deveria ser List<%s>, declarado %s", viewName, viewName, qc.decl.Return.Name)
	}

	itemType, err := qc.env.ItemTypeOf(stripped)
	if err != nil {
		return fmt.Errorf("... as %s: %w", viewName, err)
	}
	sourceFields, err := shapeFieldsOf(itemType)
	if err != nil {
		return fmt.Errorf("... as %s: item de origem: %w", viewName, err)
	}

	viewShape, err := qc.shapeOf(viewName, symbols.KindView)
	if err != nil {
		return fmt.Errorf("... as %s: %w", viewName, err)
	}

	assigns, err := projectFieldAssignments("item", sourceFields, viewShape.Fields)
	if err != nil {
		return fmt.Errorf("... as %s: %w", viewName, err)
	}

	resultVar := "projected"
	qc.e.Line("%s := make([]%s, 0, len(%s))", resultVar, viewName, itemsGo)
	qc.e.Block(fmt.Sprintf("for _, item := range %s", itemsGo), func() {
		qc.e.Line("%s = append(%s, %s{%s})", resultVar, resultVar, viewName, strings.Join(assigns, ", "))
	})
	qc.e.Line("return %s, nil", resultVar)
	return nil
}

// stripClause devolve uma CÓPIA de clauses sem nenhuma cláusula de keyword
// kw — usado para remover "as" antes de hoistear via StmtLowerer, que não
// conhece Views (um conceito Read Side, fora do vocabulário de lower/stmt.go).
func stripClause(clauses []ast.QueryClause, kw string) []ast.QueryClause {
	out := make([]ast.QueryClause, 0, len(clauses))
	for _, c := range clauses {
		if c.Kw != kw {
			out = append(out, c)
		}
	}
	return out
}

// shapeFieldsOf devolve os Fields (nome + tipo, na ordem declarada) de t —
// *types.VOType (composto: Fields != nil quando Base == nil) ou
// *types.ShapeType (Aggregate/View/Event/...). Erro de geração claro para
// qualquer outro tipo (ex. um VO wrapper, um primitivo) — nenhum deles tem
// campos nomeados para casar contra uma View.
func shapeFieldsOf(t types.Type) ([]types.Field, error) {
	switch x := t.(type) {
	case *types.VOType:
		return x.Fields, nil
	case *types.ShapeType:
		return x.Fields, nil
	default:
		return nil, fmt.Errorf("tipo %s não tem campos nomeados (esperava ValueObject composto ou um shape com campos)", t.String())
	}
}

// projectFieldAssignments monta as atribuições "Campo: <expressão Go>" de um
// literal de View a partir de sourceFields (os campos, NA ORDEM DECLARADA, do
// tipo de origem — o state de um Aggregate ou o item de uma coleção) para
// viewFields (os campos da View, NA ORDEM DECLARADA) — REQ-34.1: casamento
// EXATO por nome primeiro (o caso de sempre, ex. WalletView.balance <-
// Wallet.state.balance); quando ausente, achatamento de UM nível de VO
// COMPOSTO (flattenedFieldExpr, abaixo) — "<campo>_<subcampo>", ex.
// "amount_value"/"amount_currency" <- um campo Money (spec §6.1). Campo da
// View sem correspondência (nem direto, nem achatado) é erro de geração
// claro nomeando o campo (REQ-34.2, NFR-20) — nunca um struct parcialmente
// preenchido. Compartilhada por emitLoadAsView (1 item) e emitAsProjection
// (N itens, por cima de um "for _, item := range ...") — a MESMA rotina de
// mapeamento, só o texto de sourceGo muda ("wallet.state" vs. "item").
func projectFieldAssignments(sourceGo string, sourceFields, viewFields []types.Field) ([]string, error) {
	bySourceName := make(map[string]bool, len(sourceFields))
	for _, f := range sourceFields {
		bySourceName[f.Name] = true
	}

	assigns := make([]string, len(viewFields))
	for i, vf := range viewFields {
		if bySourceName[vf.Name] {
			assigns[i] = fmt.Sprintf("%s: %s.%s", goname.ExportField(vf.Name), sourceGo, goname.ExportField(vf.Name))
			continue
		}
		expr, ok := flattenedFieldExpr(sourceGo, sourceFields, vf.Name)
		if !ok {
			return nil, fmt.Errorf("campo %q da View não existe no item de origem (nem direto, nem achatado de um ValueObject composto)", vf.Name)
		}
		assigns[i] = fmt.Sprintf("%s: %s", goname.ExportField(vf.Name), expr)
	}
	return assigns, nil
}

// flattenedFieldExpr tenta casar viewFieldName com "<campo>_<subcampo>"
// contra UM dos sourceFields cujo tipo é um VO COMPOSTO (types.VOType com
// Fields — Base == nil, nunca um wrapper) declarando subcampo — a forma de
// achatamento de REQ-34.1 (ex. "amount_value" <- Money.amount, "
// amount_currency" <- Money.currency, spec §6.1). Itera sourceFields NA
// ORDEM DECLARADA (determinismo, NFR-13): nenhum caso real tem prefixos
// ambíguos, e a ordem declarada torna o resultado determinístico mesmo se
// houvesse.
func flattenedFieldExpr(sourceGo string, sourceFields []types.Field, viewFieldName string) (string, bool) {
	for _, sf := range sourceFields {
		prefix := sf.Name + "_"
		if !strings.HasPrefix(viewFieldName, prefix) {
			continue
		}
		vo, ok := sf.Type.(*types.VOType)
		if !ok || vo.Base != nil {
			continue // wrapper (Base != nil) não tem sub-campos nomeados
		}
		subName := strings.TrimPrefix(viewFieldName, prefix)
		for _, subF := range vo.Fields {
			if subF.Name == subName {
				return fmt.Sprintf("%s.%s.%s", sourceGo, goname.ExportField(sf.Name), goname.ExportField(subName)), true
			}
		}
	}
	return "", false
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

// --- join (I5.1, §design read-side 3.7): Collection[T] var por fonte. ---

// queryExprHasJoin reporta se clauses (de um QueryExpr "list") tem uma
// cláusula "join" — o sinal que distingue "list A a join B b on ..." (I5.1,
// roteado para emitHoistedJoinReturn) de qualquer outra forma de "list"
// (tryEmitListVO/tryEmitHoistedQueryReturn).
func queryExprHasJoin(clauses []ast.QueryClause) bool {
	for _, c := range clauses {
		if c.Kw == "join" {
			return true
		}
	}
	return false
}

// queryJoinCollectionTypeNames varre CADA decl.Body de decls (todas as
// Queries do arquivo, não só uma — várias podem referenciar o MESMO tipo, e
// só queremos UM var de Collection por tipo, mesmo padrão de
// decl_policy.go:policyCollectionTypeNames) por *ast.QueryExpr "list" com
// cláusula "join" (I5.1) e devolve, em ordem alfabética (determinismo,
// NFR-13), o conjunto de nomes NUS de tipo referenciados como fonte de um
// join — tanto o alvo base ("list Ticket t join ...") quanto a fonte
// juntada ("join Order o ..."). Só join precisa deste roteamento: "list <VO>"
// SEM join continua coberto por tryEmitListVO (correlação com um Aggregate,
// I3.2) — nenhuma mudança para esse caminho.
func queryJoinCollectionTypeNames(decls []*ast.QueryDecl) []string {
	seen := make(map[string]bool)
	for _, d := range decls {
		if d == nil || d.Body == nil {
			continue
		}
		astutil.ForEachExprInBlock(d.Body, func(e ast.Expr) {
			qe, ok := e.(*ast.QueryExpr)
			if !ok || qe.Op != "list" || !queryExprHasJoin(qe.Clauses) {
				return
			}
			if name := astutil.HeadName(qe.Target); name != "" {
				seen[name] = true
			}
			for _, c := range qe.Clauses {
				if c.Kw == "join" {
					if name := astutil.HeadName(c.Expr); name != "" {
						seen[name] = true
					}
				}
			}
		})
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// emitQueryJoinCollectionVars emite, uma vez por tipo distinto de names,
// "var <tipo>Collection = runtime.NewMemoryCollection[<Tipo>]()" (I5.1,
// mesmo padrão de decl_policy.go:emitPolicyCollectionVars, generalizado para
// Query): um "list A a join B b on ..." dentro de um Query.Body materializa
// as duas fontes por inteiro (§design read-side 3.7 passo 1) sobre o MESMO
// seam runtime.Collection[T] que Policy já usa para list/count simples (H4,
// §22.4) — reusa policyCollectionVarName (mesma convenção de nome, ex.
// "ticketCollection") para que o var seja previsível independente de qual
// emissor o declarou. Nenhum wiring de produção real (popular a partir de
// um EventStore/projeção) ainda: um teste comportamental semeia via .Add
// diretamente — mesmo espírito da nota de escopo de decl_policy.go.
func emitQueryJoinCollectionVars(e *emit.Emitter, runtimeAlias string, names []string) map[string]string {
	typeToVar := make(map[string]string, len(names))
	for _, name := range names {
		v := policyCollectionVarName(name)
		typeToVar[name] = v
		e.Line("// %s é o runtime.Collection[%s] que \"list %s ... join ...\" (I5.1,", v, name, name)
		e.Line("// §design read-side 3.7) materializa por inteiro dentro de uma Query deste")
		e.Line("// pacote — semeado por um teste que aciona a Query gerada diretamente; um")
		e.Line("// wiring de produção real (popular a partir de um EventStore/projeção) fica")
		e.Line("// para quando um exemplo real precisar dele (mesmo espírito da nota de escopo")
		e.Line("// de decl_policy.go).")
		e.Line("var %s = %s.NewMemoryCollection[%s]()", v, runtimeAlias, name)
		e.Line("")
	}
	return typeToVar
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
