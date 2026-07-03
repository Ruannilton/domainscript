package lower

import (
	"fmt"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/symbols"
	"domainscript/types"
)

// env.go implementa o TypeEnv — o ambiente de tipos local do lowering (REQ-22.6,
// §design codegen 3.6a). A AST validada pelo front-end não anota o tipo de cada
// nó: a resolução de nomes (REQ-9) só validou que os nomes existem. O gerador
// reconsulta a SymbolTable e reconstrói o tipo de cada receptor/parâmetro/local
// via types.Model — exatamente como sema/rules_typecheck.go já faz para a
// checagem de acesso a membro (REQ-12), cujo padrão este arquivo reproduz.
//
// TypeEnv estende essa reconstrução para além do que o front-end precisou tipar:
// types.Model.Infer devolve types.ErrorType para *ast.QueryExpr (load/list/count/
// store/exists), *ast.MatchExpr e *ast.LambdaExpr (types/infer.go, ramo default),
// porque a checagem de tipos (REQ-13) nunca precisou saber a forma Go concreta
// desses locais — só validar compatibilidade. A geração precisa de uma forma Go
// para TODO nó, daí InferAssignRHS.
//
// Decisão sobre `caller` (documentada aqui, única vez): nenhum lugar de
// sema/rules_typecheck.go tipa o receptor `caller`, apesar de
// resolver/receivers.go listá-lo como nome válido em Handle/Access/UseCase.
// execute/Policy.execute — não existe catálogo de membros para `caller` em
// types.Model porque ele é um conceito só do runtime (`runtime.Caller`), não do
// sistema de tipos DomainScript. TypeEnv segue a mesma convenção: nunca semeamos
// `caller` com um tipo real. O acesso `caller.id`/`caller.authenticated`/
// `caller.hasRole(...)` será reconhecido por FORMA (nome literal "caller" + nome
// do método/campo chamado) quando o lowering de verdade for escrito (E5.1+), não
// por tipo.

// TypeEnv é o ambiente de tipos local do lowering: implementa types.Scope
// (LookupType) e estende types.Model.Infer com o tipo dos locais que
// load/list/count/store/match/lambda introduzem — formas que o front-end não
// precisou tipar (só validou nomes, REQ-9) porque não geram diagnóstico de tipo
// ali. Escopos formam uma cadeia léxica simples via Child/parent: um nome não
// achado no nível local é buscado no pai (nunca o contrário).
type TypeEnv struct {
	model  *types.Model
	tab    *symbols.SymbolTable
	module string
	parent *TypeEnv
	vars   map[string]types.Type
}

// New cria o TypeEnv raiz para o módulo module, sobre o Model e a SymbolTable já
// construídos pelo front-end (ex.: types.NewModel(prog.Symbols) e prog.Symbols).
func New(model *types.Model, tab *symbols.SymbolTable, module string) *TypeEnv {
	return &TypeEnv{model: model, tab: tab, module: module, vars: make(map[string]types.Type)}
}

// LookupType implementa types.Scope: procura name neste nível e, se ausente,
// sobe para o pai (escopo léxico — um filho enxerga o pai, nunca o inverso).
func (env *TypeEnv) LookupType(name string) (types.Type, bool) {
	if t, ok := env.vars[name]; ok {
		return t, true
	}
	if env.parent != nil {
		return env.parent.LookupType(name)
	}
	return nil, false
}

// Bind define name com o tipo t neste escopo (usado para receptores,
// parâmetros, locais de AssignStmt, variável de for, parâmetro de lambda).
func (env *TypeEnv) Bind(name string, t types.Type) {
	if env.vars == nil {
		env.vars = make(map[string]types.Type)
	}
	env.vars[name] = t
}

// Child abre um escopo-filho (usado em for/braço de match/lambda/alias de
// list-join) — LookupType nele cai pro pai se não achar localmente.
func (env *TypeEnv) Child() *TypeEnv {
	return &TypeEnv{model: env.model, tab: env.tab, module: env.module, parent: env, vars: make(map[string]types.Type)}
}

// BoundLocally reporta se name já foi vinculado NESTE nível de escopo, sem
// subir para o pai (ao contrário de LookupType) — usado pelo lowering de
// AssignStmt (E5.2, REQ-22.8) para decidir ":=" (1ª atribuição no bloco Go
// imediato) vs "=" (reatribuição): um nome herdado do escopo léxico pai
// (receptor, parâmetro, ou variável de um bloco Go externo) não conta como
// "já atribuído AQUI" — declarar com ":=" nesse caso é uma NOVA variável Go,
// correta quando o alvo nu está de fato entrando em cena pela 1ª vez neste
// bloco (mesmo que sombreie um nome do escopo pai).
func (env *TypeEnv) BoundLocally(name string) bool {
	_, ok := env.vars[name]
	return ok
}

// ChildForIter abre um escopo-filho vinculando varName ao tipo do elemento de
// iterType (§design 3.6a): iterType é List<T>/AppendList<T>/Set<T> (Args[0]) ou
// já um Range convertido para List<integer> (types.Model.Infer já mapeia
// *ast.RangeExpr para essa forma — types/infer.go). iterType deve já ter sido
// inferido pelo chamador (via InferAssignRHS ou model.Infer). Se iterType não
// for um Generic com argumento (uma forma inesperada), o filho simplesmente não
// vincula varName — o mesmo comportamento conservador de seed() em
// sema/rules_typecheck.go: o nome fica desconhecido em vez de receber um
// palpite.
func (env *TypeEnv) ChildForIter(varName string, iterType types.Type) *TypeEnv {
	child := env.Child()
	child.seedIfKnown(varName, elementType(iterType))
	return child
}

// elementType devolve o tipo do elemento de um Generic (o último argumento de
// tipo — mesma convenção de types.Infer para IndexExpr: List<T>/AppendList<T>/
// Set<T> → T; Map<K,V> → V), ou types.ErrorType se t não for um Generic com
// argumento.
func elementType(t types.Type) types.Type {
	if g, ok := t.(*types.Generic); ok && len(g.Args) > 0 {
		return g.Args[len(g.Args)-1]
	}
	return types.ErrorType
}

// typeOfName devolve o tipo do símbolo de nome name, procurando primeiro no
// módulo do TypeEnv e, em fallback, globalmente — reproduzindo exatamente
// Checker.typeOfName de sema/rules_typecheck.go (Lookup local, depois Find
// cross-module). Devolve types.ErrorType (nunca nil) quando o nome não resolve:
// o erro de nome, se houver, já foi reportado pelo front-end (REQ-9); aqui é só
// o sentinela anti-cascata (REQ-11.3).
func (env *TypeEnv) typeOfName(name string) types.Type {
	if name == "" {
		return types.ErrorType
	}
	if sym, ok := env.tab.Lookup(env.module, name); ok {
		return env.model.TypeOf(sym)
	}
	if sym, ok := env.tab.Find(name); ok {
		return env.model.TypeOf(sym)
	}
	return types.ErrorType
}

// TypeOfName expõe typeOfName: o tipo do símbolo de nome name, procurado
// primeiro no módulo do TypeEnv e, em fallback, globalmente (mesma regra de
// Lookup local + Find cross-module de Checker.typeOfName). Devolve
// types.ErrorType (nunca nil) quando o nome não resolve. Usado pelo lowering
// (E5.1+) para descobrir se um *ast.Ident em CallExpr.Fn é um tipo declarado
// (VO/Event/Command) e obter o types.Type correspondente.
func (env *TypeEnv) TypeOfName(name string) types.Type {
	return env.typeOfName(name)
}

// Model devolve o *types.Model subjacente ao TypeEnv. Usado pelo lowering
// (E5.1+) para consultar model.Members(t) e para inspecionar os Fields
// ORDENADOS (pela ordem de declaração) de *types.VOType/*types.ShapeType — ao
// contrário de Members(), que é um mapa sem ordem — ao casar argumentos
// posicionais de uma construção contra os campos na ordem certa.
func (env *TypeEnv) Model() *types.Model {
	return env.model
}

// seedIfKnown vincula name a t só quando t é um tipo conhecido (não nil, não
// ErrorType) — espelha seed() de sema/rules_typecheck.go: um receptor/parâmetro
// cujo tipo não se conhece simplesmente não entra no escopo, em vez de
// contaminá-lo com o sentinela de erro.
func (env *TypeEnv) seedIfKnown(name string, t types.Type) {
	if t != nil && !types.IsError(t) {
		env.Bind(name, t)
	}
}

// bindParams vincula cada parâmetro declarado ao seu tipo (Model.TypeOfRef) —
// usado por todo construto cujo corpo enxerga seus próprios parâmetros
// nomeados (Handle, Query, Operator futuro, etc.).
func (env *TypeEnv) bindParams(params []*ast.Field) {
	for _, f := range params {
		if f != nil && f.Name != "" {
			env.seedIfKnown(f.Name, env.model.TypeOfRef(env.module, f.Type))
		}
	}
}

// --- Seeding por construto (espelha resolver/receivers.go e
// sema/rules_typecheck.go — mesmos nomes, agora com tipo em vez de só nome). ---

// SeedHandle semeia o escopo raiz de um Handle de Aggregate (constructHandle):
// self e state resolvem ao mesmo tipo — o shape do state do Aggregate
// aggregateName (idêntico à convenção do front-end: self/state são tipados
// igual, o próprio shape do Aggregate) — e os params aos seus tipos declarados.
// caller não é semeado (ver doc do arquivo).
func (env *TypeEnv) SeedHandle(aggregateName string, params []*ast.Field) {
	t := env.typeOfName(aggregateName)
	env.seedIfKnown("self", t)
	env.seedIfKnown("state", t)
	env.bindParams(params)
}

// SeedApply semeia o escopo raiz de um Apply de Aggregate (constructApply):
// state = o tipo do Aggregate; event = o tipo do Event nomeado em
// `Apply <EventName>` (ast.ApplyDecl.Event). Apply não tem self nem caller.
func (env *TypeEnv) SeedApply(aggregateName, eventName string) {
	env.seedIfKnown("state", env.typeOfName(aggregateName))
	env.seedIfKnown("event", env.typeOfName(eventName))
}

// SeedAccess semeia o escopo raiz de uma regra de access (constructAccess):
// self = o tipo do Aggregate. caller não é semeado (ver doc do arquivo).
func (env *TypeEnv) SeedAccess(aggregateName string) {
	env.seedIfKnown("self", env.typeOfName(aggregateName))
}

// SeedUseCaseExecute semeia o escopo raiz do execute de um UseCase
// (constructUseCaseExecute): cmd = o tipo do Command declarado em `handles`
// (ast.UseCaseDecl.Handles). caller não é semeado.
func (env *TypeEnv) SeedUseCaseExecute(commandName string) {
	env.seedIfKnown("cmd", env.typeOfName(commandName))
}

// SeedPolicyExecute semeia o escopo raiz do execute de uma Policy
// (constructPolicyExecute): event = o tipo do Event/PublicEvent declarado em
// `on` (ast.PolicyDecl.On). caller não é semeado. (Policy só ganha emissor no
// Marco F, mas espelhar a tabela inteira de resolver/receivers.go aqui é barato
// e evita retrabalho quando F1 chegar — §design codegen 3.6a.)
func (env *TypeEnv) SeedPolicyExecute(eventName string) {
	env.seedIfKnown("event", env.typeOfName(eventName))
}

// SeedQuery semeia o escopo raiz de uma Query (constructQuery): só os
// parâmetros declarados — Query não tem receptor contextual algum
// (resolver/receivers.go não lista constructQuery em contextualReceiverNames).
func (env *TypeEnv) SeedQuery(params []*ast.Field) {
	env.bindParams(params)
}

// SeedWorkerExecute semeia o escopo raiz do execute de um Worker
// (constructWorkerExecute, Marco F2): paramName (WorkerDecl.ExecuteParam) é
// vinculado a itemType — o tipo do item da fonte (schedule continuous,
// calculado pelo chamador a partir de Source, ver codegen/decl_worker.go).
// paramName == "" (every/cron, que não têm ExecuteParam) é um no-op. Worker
// não tem "caller"/"self" semeados — resolver/receivers.go não lista nenhum
// receptor contextual para constructWorkerSource/constructWorkerExecute (a
// mesma ausência documentada em env.go para os demais construtos).
func (env *TypeEnv) SeedWorkerExecute(paramName string, itemType types.Type) {
	if paramName == "" {
		return
	}
	env.seedIfKnown(paramName, itemType)
}

// SeedSagaStep semeia o escopo raiz de um passo de Saga (constructSagaStep,
// Marco F3): state resolve a um *types.ShapeType PRÓPRIO — não
// types.Model.TypeOf(sagaSymbol), que devolve, para uma Saga, um ShapeType
// SEM Fields (types/model.go, ramo default: "UseCase/Policy/Saga/Worker/...
// não têm forma de campos própria relevante à checagem de membro" — REQ-12
// não cobre Saga hoje, ver sema/rules_typecheck.go:checkDeclMembers, que só
// lista Aggregate/ValueObject/Query/UseCase/Policy). O lowering (member(),
// expr.go) só precisa que "state" resolva a ALGUM *types.ShapeType para gerar
// "state.<Campo>" — member() nunca consulta t.Fields para validar o nome (só
// o Kind do receptor decide a forma: campo exportado para ShapeType/VOType) —
// então construir um ShapeType com os Fields de sagaState (ast.SagaDecl.State)
// aqui é suficiente e mais preciso que reusar o ShapeType vazio do Model.
func (env *TypeEnv) SeedSagaStep(sagaName string, sagaState []*ast.Field) {
	sh := &types.ShapeType{Name: sagaName + "State", Kind: symbols.KindSaga}
	for _, f := range sagaState {
		if f == nil || f.Name == "" {
			continue
		}
		sh.Fields = append(sh.Fields, types.Field{Name: f.Name, Type: env.model.TypeOfRef(env.module, f.Type)})
	}
	env.Bind("state", sh)
}

// --- Núcleo: extensão de inferência para locais que types.Model.Infer não cobre. ---

// InferAssignRHS infere o tipo do lado direito de um AssignStmt de alvo nu
// (x = <rhs>), cobrindo as formas que types.Model.Infer devolve ErrorType
// (§design 3.6a): QueryExpr (load/list/count/store/exists), MatchExpr,
// LambdaExpr. Para qualquer outra forma, delega para model.Infer (o próprio
// TypeEnv serve de types.Scope nessa chamada, já que implementa LookupType).
//
// Um "nó realmente desconhecido" — uma forma de QueryExpr.Op não reconhecida,
// um MatchExpr sem braços — devolve um erro Go explícito: são formas
// estruturalmente inválidas que este método é a ÚNICA autoridade capaz de
// resolver (não há Infer de types para cair de volta), então não há tipo
// nenhum a devolver — nunca um palpite. Já um resultado types.ErrorType vindo
// do delegate (model.Infer) ou de dentro de um braço de match/corpo de lambda
// NÃO vira erro Go aqui: é o sentinela anti-cascata normal do sistema de tipos
// (REQ-11.3), que o chamador (o lowering de verdade, E5.1+) trata como já trata
// hoje qualquer ErrorType — é dado explícito, não um "type Go arbitrário".
func (env *TypeEnv) InferAssignRHS(rhs ast.Expr) (types.Type, error) {
	switch n := rhs.(type) {
	case *ast.QueryExpr:
		return env.inferQueryExpr(n)
	case *ast.MatchExpr:
		return env.inferMatchExpr(n)
	case *ast.LambdaExpr:
		return env.inferLambdaExpr(n)
	default:
		return env.model.Infer(env.module, rhs, env), nil
	}
}

// inferQueryExpr cobre load/list/count/store/exists (§design 3.6a, REQ-22.6).
func (env *TypeEnv) inferQueryExpr(qe *ast.QueryExpr) (types.Type, error) {
	switch qe.Op {
	case "load":
		// load T(id) → o tipo de T, resolvido via typeOfName.
		name := astutil.HeadName(qe.Target)
		t := env.typeOfName(name)
		if types.IsError(t) {
			return nil, fmt.Errorf("lower: load: não consegui resolver o tipo de %q", name)
		}
		return t, nil

	case "list":
		// list T … as V → List<V> (V do clause "as"); sem "as" → List<T>.
		if v, ok := listAsClause(qe.Clauses); ok {
			vt := env.typeOfName(v)
			if types.IsError(vt) {
				return nil, fmt.Errorf("lower: list ... as %s: não consegui resolver o tipo", v)
			}
			return &types.Generic{Ctor: "List", Args: []types.Type{vt}}, nil
		}
		name := astutil.HeadName(qe.Target)
		t := env.typeOfName(name)
		if types.IsError(t) {
			return nil, fmt.Errorf("lower: list: não consegui resolver o tipo de %q", name)
		}
		return &types.Generic{Ctor: "List", Args: []types.Type{t}}, nil

	case "count":
		return &types.Primitive{Name: "integer"}, nil

	case "store":
		// FileRef é primitivo opaco no types.Model (types/model.go: primitives
		// inclui "File"/"FileStream"/"FileRef") — mesma forma que typeRef
		// produziria para uma ast.TypeRef{Name: "FileRef"}.
		return &types.Primitive{Name: "FileRef"}, nil

	case "exists":
		// exists (QueryExpr pós-fixo, ex. "ensure x exists") → boolean.
		return &types.Primitive{Name: "boolean"}, nil

	default:
		return nil, fmt.Errorf("lower: QueryExpr.Op desconhecido: %q", qe.Op)
	}
}

// listAsClause procura a cláusula "as" entre as QueryClause de um QueryExpr e
// devolve o nome do tipo (Extra), se houver.
func listAsClause(clauses []ast.QueryClause) (string, bool) {
	for _, c := range clauses {
		if c.Kw == "as" {
			return c.Extra, true
		}
	}
	return "", false
}

// inferMatchExpr infere o tipo de um MatchExpr pelo tipo do Body do primeiro
// braço — todo braço de um match-expressão exaustivo produz o mesmo tipo
// estático (o front-end já validou a exaustividade, REQ-5.5), então o primeiro
// braço basta. Um MatchExpr sem braços é um erro de geração explícito: não
// deveria acontecer sobre um programa válido.
func (env *TypeEnv) inferMatchExpr(me *ast.MatchExpr) (types.Type, error) {
	if len(me.Arms) == 0 {
		return nil, fmt.Errorf("lower: MatchExpr sem braços")
	}
	return env.InferAssignRHS(me.Arms[0].Body)
}

// inferLambdaExpr infere o tipo de um LambdaExpr pelo tipo do seu Body. O tipo
// do Param não é conhecido aqui isoladamente — depende do receptor da coleção
// que usa o lambda (ex.: .distinct(t => t.orderId)), informação que só existe
// no ponto de chamada do método de coleção. Isso não é exercitado pelo wallet e
// entra de verdade só quando distinct/sum/focus forem implementados (§20,
// Marco F2/shop). Por ora abrimos um Child() sem vincular Param a um tipo
// conhecido: se o corpo o referenciar, ele cai em types.ErrorType (via Ident →
// LookupType/model.symbol, sem gerar erro Go) — aceitável agora.
func (env *TypeEnv) inferLambdaExpr(le *ast.LambdaExpr) (types.Type, error) {
	child := env.Child()
	return child.InferAssignRHS(le.Body)
}
