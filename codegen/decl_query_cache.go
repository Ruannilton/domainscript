package codegen

import (
	"fmt"
	"sort"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/program"
	"domainscript/symbols"
	"domainscript/types"
)

// decl_query_cache.go emite o cache de Query (QueryDecl.Cache, spec §15,
// G3): ttl, invalidação por evento (inferida dos aggregates tocados pelo
// corpo da Query; override "invalidateOn"), negativeCacheTtl, stampede
// protection (request coalescing, runtime.QueryCache.Coalesce), fail-open na
// falha do backend (runtime.QueryCache.Get nunca propaga um erro de
// backend — trata como miss, ver rtsrc/querycache.go.txt), bypass
// "Cache-Control: no-cache" (codegen/http.go, runtime.NoCacheFrom) e tenant
// na chave (runtime.TenantFrom — placeholder até a borda de tenancy real,
// G5, resolver o valor de verdade; ver a nota de G5 abaixo).
//
// --- Por que um wrapper, não um corpo reescrito (mesmo padrão de G2) ---
//
// emitQueryDecl (decl_query.go) continua emitindo EXATAMENTE o mesmo corpo
// de sempre — só que, quando decl.Cache != nil, sob um nome PRIVADO
// ("<nome>Run", unexportedQueryRunName abaixo) em vez do nome público da
// Query. A função pública (o nome de sempre, o único símbolo que
// http.go/o resto do gerado continuam chamando) vira o wrapper deste
// arquivo, que consulta o cache ANTES de rodar "<nome>Run" no caminho de
// miss. Uma Query SEM "cache" não muda NADA: o nome público continua sendo
// a própria função com o corpo, byte a byte igual à saída de antes desta
// task (mesmo contrato que usecase_idempotency.go já estabelece para
// UseCase/G2).
//
// --- Invalidação: 1 instância de runtime.QueryCache POR Query cacheada ---
//
// Cada Query cacheada ganha sua PRÓPRIA var de pacote (auto-inicializada,
// sem Wire — mesmo padrão de "var idem", usecase_idempotency.go). Como a
// instância nunca é compartilhada entre Queries, invalidar significa
// simplesmente varrer TUDO daquela instância (QueryCache.InvalidateAll) —
// não precisa de marcação por entrada de qual(is) evento(s) a evictam (ver
// rtsrc/querycache.go.txt). O conjunto de eventos que disparam essa
// invalidação é, por padrão, INFERIDO dos aggregates tocados pelo corpo da
// Query (todo Event com "Apply" no Aggregate que a Query carrega — ver
// inferQueryTouchedAggregate/aggregateAppliedEventNames abaixo); "invalidateOn"
// no bloco cache OVERRIDE esse conjunto por completo.
//
// A wiring de verdade (um "func WireQueryCache(d runtime.Dispatcher)" que
// registra "d.Subscribe(evento, func(...) { xCache.InvalidateAll(); return
// nil })" por evento) é gerada UMA VEZ por arquivo queries.go (ver
// emitQueryCacheWireFunc), reunindo todas as Queries cacheadas do módulo —
// chamada por cmd/<service>/main.go na inicialização (codegen.go), ao lado
// de Wire/StartWorkers/StartIdempotencyCleanup. "in-process imediata após
// emit, antes da fila externa" (spec §15) vem de graça do MESMO mecanismo
// que a entrega de Policy já usa (runtime.Dispatcher.Publish é síncrono,
// chamado de dentro de uow.Run logo após o commit — uow.go.txt — antes de
// qualquer publicação externa/assíncrona) — nenhuma mudança no runtime de
// unit of work foi necessária: WireQueryCache só assina o MESMO Dispatcher
// que Policies já usam (codegen.go força a existência de um Dispatcher para
// o service quando qualquer módulo do grupo tem Query cacheada, mesma
// condição que hoje só considerava Policy — ver generateCmdMainFile).
//
// --- Tenant na chave (spec §15) — placeholder documentado até G5 ---
//
// runtime.TenantFrom(ctx) já existe desde E2.1/§3.1a especificamente como
// esse "no-op até tenancy" seam (contextkeys.go.txt: "Filtering/enforcement
// by Tenant is a no-op until multi-tenancy lands"). Esta task só CONSOME
// esse acessor para compor a chave — não inventa nenhuma infraestrutura de
// tenant nova, e não conflita com o que G5 vai construir: G5 (tasks.md) é
// sobre INJETAR o tenant de verdade na borda + filtrar toda query/carga por
// estratégia (row_level/schema/database) + 404 cross-tenant — nada disso
// muda a ASSINATURA de TenantFrom, só QUEM chama WithTenant e quando. Até
// G5 aterrissar, TenantFrom(ctx) devolve sempre (Tenant{}, false); a chave
// carrega tenant="" para toda chamada — efetivamente sem particionamento
// por tenant ainda, exatamente o "reservar o slot" que a task pede.

// --- 1. queryCachePlan: a config de cache já resolvida de UMA Query. ---

// queryCachePlan é a config de cache já resolvida de um QueryDecl — nil
// (via planQueryCache) quando a Query não declara "cache" (o caso comum,
// nenhuma mudança de comportamento).
type queryCachePlan struct {
	ttlGo         string   // time.Duration já lowerizado (obrigatório — ver planQueryCache)
	negativeTTLGo string   // "" quando a Query não declara negativeCacheTtl (sem cache negativo)
	invalidateOn  []string // nomes de Event/PublicEvent, ordenados e sem repetição
	touchedAgg    string   // nome do Aggregate tocado — "" quando invalidateOn veio de override explícito
}

// invalidationSourceNote resume, para o comentário de doc gerado, se
// invalidateOn veio da inferência automática ou de um override explícito.
func (p *queryCachePlan) invalidationSourceNote() string {
	if p.touchedAgg != "" {
		return fmt.Sprintf("inferidos dos Applies do Aggregate %s tocado por esta Query", p.touchedAgg)
	}
	return "declarados explicitamente em \"invalidateOn\""
}

// planQueryCache resolve decl.Cache: ttl (obrigatório — da própria Query, ou
// do "defaultTtl" do bloco mod.ds Cache{}, spec §12), negativeCacheTtl
// (opcional) e o conjunto de invalidação (override "invalidateOn", senão
// inferido do Aggregate tocado pelo corpo, ver inferQueryTouchedAggregate).
// Devolve (nil, nil) quando decl.Cache está vazio (sem bloco "cache" algum).
func planQueryCache(decl *ast.QueryDecl, aggregates map[string]*ast.AggregateDecl, env *lower.TypeEnv, l *lower.Lowerer, mod *program.Module) (*queryCachePlan, error) {
	if len(decl.Cache) == 0 {
		return nil, nil
	}

	ttlGo, hasTTL, err := configDurationEntry(decl.Cache, "ttl", l)
	if err != nil {
		return nil, fmt.Errorf("cache.%w", err)
	}
	if !hasTTL {
		if cb := moduleCacheBlock(mod); cb != nil {
			ttlGo, hasTTL, err = configDurationEntry(cb.Entries, "defaultTtl", l)
			if err != nil {
				return nil, fmt.Errorf("mod.ds Cache.%w", err)
			}
		}
	}
	if !hasTTL {
		return nil, fmt.Errorf("cache: \"ttl\" é obrigatório (nem a Query nem o mod.ds Cache{} declaram \"defaultTtl\", §15)")
	}

	negativeTTLGo, _, err := configDurationEntry(decl.Cache, "negativeCacheTtl", l)
	if err != nil {
		return nil, fmt.Errorf("cache.%w", err)
	}

	overrideNames, hasOverride, err := cacheInvalidateOnOverride(decl.Cache)
	if err != nil {
		return nil, fmt.Errorf("cache.%w", err)
	}

	var touchedAgg string
	var names []string
	if hasOverride {
		names = overrideNames
	} else {
		touchedAgg, err = inferQueryTouchedAggregate(decl, aggregates, env)
		if err != nil {
			return nil, fmt.Errorf("cache: %w (ou declare \"invalidateOn\" explicitamente)", err)
		}
		names = aggregateAppliedEventNames(aggregates[touchedAgg])
	}

	for _, name := range names {
		t := env.TypeOfName(name)
		st, ok := t.(*types.ShapeType)
		if !ok || st.Kind != symbols.KindEvent {
			return nil, fmt.Errorf("cache.invalidateOn: %q não resolve a um Event conhecido", name)
		}
	}
	names = dedupSortedStrings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("cache: não há evento para invalidar (Aggregate %q sem nenhum Apply, e sem \"invalidateOn\" explícito) — cache sem invalidação nunca seria coerente com o Write Side (§15)", touchedAgg)
	}

	return &queryCachePlan{ttlGo: ttlGo, negativeTTLGo: negativeTTLGo, invalidateOn: names, touchedAgg: touchedAgg}, nil
}

// cacheInvalidateOnOverride busca a entrada "invalidateOn" de entries — uma
// lista de identificadores nus de Event (ex. "[DepositPerformed,
// WithdrawalPerformed]", mesma forma de ProjectionDecl.RefreshOn, ver
// refreshOnSummary/decl_projection.go). ok=false sem erro quando a chave está
// ausente (o caso comum: invalidação totalmente inferida).
func cacheInvalidateOnOverride(entries []ast.ConfigEntry) ([]string, bool, error) {
	for _, entry := range entries {
		if entry.Key != "invalidateOn" {
			continue
		}
		list, ok := entry.Value.(*ast.ListExpr)
		if !ok {
			return nil, false, fmt.Errorf("invalidateOn: esperava uma lista de eventos, veio %T", entry.Value)
		}
		names := make([]string, 0, len(list.Elems))
		for _, el := range list.Elems {
			id, ok := el.(*ast.Ident)
			if !ok {
				return nil, false, fmt.Errorf("invalidateOn: esperava um identificador de Event nu, veio %T", el)
			}
			names = append(names, id.Name)
		}
		return names, true, nil
	}
	return nil, false, nil
}

// inferQueryTouchedAggregate infere qual Aggregate o corpo de decl toca,
// reconhecendo as MESMAS duas formas de retorno que decl_query.go já
// suporta (ver a doc daquele arquivo): "return load Agg(...) as View" (o
// Ident de Agg precisa ser um Aggregate conhecido) e "return list <VO>"
// (correlacionado via correlateListVOAggregate, a MESMA correlação que
// tryEmitListVO usa). Erro claro quando nenhum statement de retorno bate com
// nenhuma das duas formas — o autor do .ds deve then declarar
// "invalidateOn" explicitamente (ver planQueryCache).
func inferQueryTouchedAggregate(decl *ast.QueryDecl, aggregates map[string]*ast.AggregateDecl, env *lower.TypeEnv) (string, error) {
	if decl.Body != nil {
		for _, s := range decl.Body.Stmts {
			ret, ok := s.(*ast.ReturnStmt)
			if !ok || ret.Value == nil {
				continue
			}
			qe, ok := ret.Value.(*ast.QueryExpr)
			if !ok {
				continue
			}
			switch qe.Op {
			case "load":
				call, ok := qe.Target.(*ast.CallExpr)
				if !ok {
					continue
				}
				id, ok := call.Fn.(*ast.Ident)
				if !ok {
					continue
				}
				if _, known := aggregates[id.Name]; known {
					return id.Name, nil
				}
			case "list":
				id, ok := qe.Target.(*ast.Ident)
				if !ok {
					continue
				}
				if _, isVO := env.TypeOfName(id.Name).(*types.VOType); !isVO {
					continue
				}
				if aggName, _, err := correlateListVOAggregate(aggregates, id.Name); err == nil {
					return aggName, nil
				}
			}
		}
	}
	return "", fmt.Errorf("não consegui inferir automaticamente o Aggregate tocado pelo corpo desta Query (formas reconhecidas: \"return load Agg(...) as View\"/\"return list <VO>\", E8.1)")
}

// aggregateAppliedEventNames devolve os nomes (com repetição, ainda não
// ordenados/deduplicados — planQueryCache faz isso) de todo evento com
// "Apply" declarado em decl — o conjunto "aggregates tocados" da invalidação
// automática (spec §15): qualquer evento que MUTA o Aggregate que esta Query
// lê é, por definição, capaz de invalidar um resultado cacheado dela.
func aggregateAppliedEventNames(decl *ast.AggregateDecl) []string {
	if decl == nil {
		return nil
	}
	names := make([]string, 0, len(decl.Appliers))
	for _, a := range decl.Appliers {
		if a != nil && a.Event != "" {
			names = append(names, a.Event)
		}
	}
	return names
}

// dedupSortedStrings devolve in sem repetição e ordenado — determinismo
// (NFR-13) da lista final de invalidateOn, tanto no comentário de doc quanto
// na ordem dos d.Subscribe emitidos por emitQueryCacheWireFunc.
func dedupSortedStrings(in []string) []string {
	set := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !set[s] {
			set[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// moduleCacheBlock devolve o bloco "Cache { ... }" do mod.ds de mod (§12), ou
// nil quando mod é nil ou não declara um — mesmo padrão de
// moduleIdempotencyBlock (usecase_idempotency.go) para outro Kind de
// ConfigBlock.
func moduleCacheBlock(mod *program.Module) *ast.ConfigBlock {
	if mod == nil || mod.Decl == nil {
		return nil
	}
	for _, b := range mod.Decl.Blocks {
		if b.Kind == "Cache" {
			return b
		}
	}
	return nil
}

// --- 2. Nomes derivados (mesma convenção de unexportedRunName, G2). ---

// unexportedQueryRunName devolve o nome privado da função que carrega o
// corpo de verdade de uma Query cacheada — "GetWallet" -> "getWalletRun" —
// mesma convenção de unexportedRunName (usecase_idempotency.go).
func unexportedQueryRunName(name string) string {
	if name == "" {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:] + "Run"
}

// queryCacheVarName devolve o nome da var de pacote do cache de uma Query —
// "GetWallet" -> "getWalletCache".
func queryCacheVarName(name string) string {
	if name == "" {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:] + "Cache"
}

// queryParamNames devolve os nomes Go (goname.Ident, na ordem declarada) dos
// parâmetros de uma Query — paralelo a handleParamList (decl_aggregate.go),
// que devolve a forma "nome Tipo"; aqui só o "nome", para montar as chamadas
// (CacheKey/runFn) do wrapper.
func queryParamNames(params []*ast.Field) []string {
	names := make([]string, 0, len(params))
	for _, p := range params {
		if p == nil {
			continue
		}
		names = append(names, goname.Ident(p.Name))
	}
	return names
}

// --- 3. Emissão: var de cache + wrapper público + WireQueryCache. ---

// emitQueryCacheVar emite a var de pacote do cache de uma Query
// (auto-inicializada, sem Wire — mesmo padrão de "var idem",
// usecase_idempotency.go).
func emitQueryCacheVar(e *emit.Emitter, decl *ast.QueryDecl, plan *queryCachePlan, runtimeAlias string) {
	varName := queryCacheVarName(decl.Name)
	e.Line("")
	e.Line("// %s é o cache da Query %s (spec §15, G3): invalidado (in-process,", varName, decl.Name)
	e.Line("// imediatamente após emit, antes de qualquer fila externa) pelos eventos")
	e.Line("// [%s] — %s.", strings.Join(plan.invalidateOn, ", "), plan.invalidationSourceNote())
	e.Line("var %s = %s.NewMemoryQueryCache()", varName, runtimeAlias)
}

// cachedQueryWire é o que EmitQueries acumula, por Query cacheada, para
// emitQueryCacheWireFunc montar "func WireQueryCache(d runtime.Dispatcher)"
// no final do arquivo.
type cachedQueryWire struct {
	name string
	plan *queryCachePlan
}

// emitQueryCacheWrapper emite "func <decl.Name>(...) (<Return>, error)" (o
// nome PÚBLICO da Query — ver a doc do arquivo): consulta o cache
// (runtime.NoCacheFrom pula a LEITURA, nunca a escrita — "no-cache"
// revalida, não desliga o cache), stampede protection via Coalesce em torno
// de UMA chamada a runFn, e populo do cache (Set/SetErr) no caminho de miss.
func emitQueryCacheWrapper(e *emit.Emitter, decl *ast.QueryDecl, plan *queryCachePlan, runFn, returnGoType string, paramStrs, paramNames []string, ctxAlias, runtimeAlias string) {
	varName := queryCacheVarName(decl.Name)

	e.Line("")
	e.Line("// %s é a Query %s (§6.3) com cache (spec §15, G3): ttl/negativeCacheTtl,", decl.Name, decl.Name)
	e.Line("// invalidação por [%s], stampede protection (Coalesce — só 1 execução", strings.Join(plan.invalidateOn, ", "))
	e.Line("// real de %s por chave, mesmo sob N chamadas concorrentes) e bypass", runFn)
	e.Line("// \"Cache-Control: no-cache\" (runtime.NoCacheFrom pula a LEITURA; ainda")
	e.Line("// repopula o cache com o resultado fresco).")

	allParams := append([]string{
		fmt.Sprintf("ctx %s.Context", ctxAlias),
		fmt.Sprintf("store %s.EventStore", runtimeAlias),
	}, paramStrs...)
	sig := fmt.Sprintf("func %s(%s) (%s, error)", decl.Name, strings.Join(allParams, ", "), returnGoType)

	keyArgs := append([]string{fmt.Sprintf("%q", decl.Name), "tenant.ID"}, paramNames...)
	runArgs := append([]string{"ctx", "store"}, paramNames...)

	e.Block(sig, func() {
		e.Line("tenant, _ := %s.TenantFrom(ctx)", runtimeAlias)
		e.Line("key := %s.CacheKey(%s)", runtimeAlias, strings.Join(keyArgs, ", "))
		e.Block(fmt.Sprintf("if !%s.NoCacheFrom(ctx)", runtimeAlias), func() {
			e.Block(fmt.Sprintf("if v, cachedErr, hit := %s.Get(ctx, key); hit", varName), func() {
				e.Block("if cachedErr != nil", func() {
					e.Line("var zero %s", returnGoType)
					e.Line("return zero, cachedErr")
				})
				e.Line("return v.(%s), nil", returnGoType)
			})
		})
		e.BlockSuffix(fmt.Sprintf("result, err := %s.Coalesce(key, func() (any, error)", varName), ")", func() {
			e.Line("return %s(%s)", runFn, strings.Join(runArgs, ", "))
		})
		e.Block("if err != nil", func() {
			if plan.negativeTTLGo != "" {
				e.Block(fmt.Sprintf("if %s.IsBusinessError(err)", runtimeAlias), func() {
					e.Line("%s.SetErr(ctx, key, err, %s)", varName, plan.negativeTTLGo)
				})
			}
			e.Line("var zero %s", returnGoType)
			e.Line("return zero, err")
		})
		e.Line("%s.Set(ctx, key, result, %s)", varName, plan.ttlGo)
		e.Line("return result.(%s), nil", returnGoType)
	})
}

// emitQueryCacheWireFunc emite "func WireQueryCache(d runtime.Dispatcher)":
// um d.Subscribe por evento do conjunto de invalidação de CADA Query
// cacheada deste arquivo (uma Query cujo evento coincide com o de outra só
// ganha Subscribe's independentes — cada handler invalida SÓ a sua própria
// instância). Nome PRÓPRIO, nunca "Wire" — mesma razão de "StartWorkers"
// (decl_worker.go): evita colidir com o "Wire" que UseCase/Policy já podem
// emitir no mesmo pacote (generateModuleFiles recusa UseCase+Policy juntos
// justamente por essa colisão; WireQueryCache nunca participa dela).
// Chamada por cmd/<service>/main.go na inicialização (ver codegen.go).
func emitQueryCacheWireFunc(e *emit.Emitter, ctxAlias, runtimeAlias string, cached []cachedQueryWire) {
	e.Line("")
	e.Line("// WireQueryCache registra a invalidação de cache das Queries deste pacote no")
	e.Line("// runtime.Dispatcher (spec §15, G3): cada evento do conjunto inferido/")
	e.Line("// invalidateOn de uma Query com cache dispara InvalidateAll na respectiva")
	e.Line("// instância — in-process, imediatamente após emit (antes de qualquer fila")
	e.Line("// externa, §15), porque runtime.Dispatcher.Publish já roda de forma síncrona")
	e.Line("// dentro de uow.Run logo após o commit (uow.go.txt). Chamada por")
	e.Line("// cmd/<service>/main.go na inicialização, ao lado de")
	e.Line("// Wire/StartWorkers/StartIdempotencyCleanup.")
	e.Block(fmt.Sprintf("func WireQueryCache(d %s.Dispatcher)", runtimeAlias), func() {
		for _, cq := range cached {
			varName := queryCacheVarName(cq.name)
			for _, evName := range cq.plan.invalidateOn {
				header := fmt.Sprintf("d.Subscribe(%q, func(ctx %s.Context, ev %s.Event) error", evName, ctxAlias, runtimeAlias)
				e.BlockSuffix(header, ")", func() {
					e.Line("%s.InvalidateAll()", varName)
					e.Line("return nil")
				})
			}
		}
	})
}
