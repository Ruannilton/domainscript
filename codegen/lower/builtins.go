package lower

import (
	"fmt"

	"domainscript/ast"
)

// builtins.go traduz as built-ins do NÚCLEO transacional (REQ-22.7(a),
// §design codegen 3.6) para chamadas Go:
//
//   - now()/uuid()/random(min,max)/random_str(length) → runtime vendorado,
//     sem dependência externa (E2.1 já tem Now/UUID; esta task acrescenta
//     Random/RandomStr em codegen/rtsrc/util.go.txt).
//   - load T(id) → reconstrução de Aggregate via o seam de persistência
//     (convenção de nome Load<T>, cuja implementação real é E6.2).
//   - list T [where ...] [as V] / count [where ...] → operações de leitura
//     PROVISÓRIAS: nenhum mecanismo de query real existe no runtime ainda
//     (isso é E8, Read Side) — aqui só se fixa a FORMA sintática da
//     lowering, testável isoladamente, sem travar esta task no desenho do
//     Read Side inteiro.
//   - exists (QueryExpr pós-fixo de "ensure X exists") → checagem estrutural
//     "X != nil" sobre um Aggregate já carregado; a semântica completa
//     (infra vs. não-encontrado vs. idempotência) é G2.
//
// Ops de arquivo (store/signed_url/delete file/load File(ref), §2.5) ficam
// de fora — dependem do seam FileStorage do mod.ds e entram com G1a
// (REQ-22.7(b)).
//
// O mecanismo de HOISTING de load/list/count (porque a chamada Go que os
// substitui devolve (_, error), que não cabe em posição de expressão pura)
// mora em stmt.go (hoistQueryExpr/hoistLoad/hoistList/hoistCount) — o mesmo
// tratamento que hoistVOConstruct já dá à construção de VO composto (E5.2).
// Este arquivo só decide O TEXTO Go de cada chamada; a decisão de QUANDO
// hoistear é do StmtLowerer.

// BuiltinLowerer traduz as built-ins do núcleo (§2.6 do spec da linguagem,
// REQ-22.7(a)) para chamadas Go: now/uuid/random/random_str (runtime, sem
// dependência), load/list/count/exists (operações de domínio, contra os
// seams de persistência do runtime — REQ-22.7(b), as ops de arquivo, ficam
// para G1a). Usa os mesmos ctxGoName/storeGoName em toda chamada — nomes
// configuráveis (o Emitter que compõe o corpo final decide os nomes reais
// dos parâmetros ctx/tx, esta task só usa o que for passado).
type BuiltinLowerer struct {
	runtimeAlias string // alias do import do runtime vendorado (ex. "runtime", via emit.Emitter.Import)
	ctxGoName    string // nome Go do parâmetro de contexto (ex. "ctx")
	storeGoName  string // nome Go do parâmetro de acesso à persistência (EventStore/Tx/repo — ex. "tx")
	// storeGoNameFor roteia "load T(id)" (LoadCall) para um Tx DIFERENTE por
	// Aggregate (typeName) — habilitado por WithPerAggregateStore (G1,
	// §design 3.8): um UseCase 2PC cross-database recebe um "txs
	// map[string]runtime.Tx" em vez de um único "tx", e cada load precisa
	// indexar o mapa pelo Database que gerencia AQUELE Aggregate, não por um
	// nome fixo. nil (o default) preserva o comportamento de sempre usar
	// storeGoName — nenhuma mudança para UseCases de um único banco (Marco
	// E/F).
	storeGoNameFor func(typeName string) string
}

// NewBuiltinLowerer cria um BuiltinLowerer sobre runtimeAlias (alias já
// resolvido do import do runtime vendorado — mesma convenção do
// runtimeAlias de NewLowerer), ctxGoName (nome Go do parâmetro de
// context.Context, ex. "ctx") e storeGoName (nome Go do parâmetro de acesso
// à persistência, ex. "tx" — §design 3.8 usa esse nome no exemplo de UseCase
// e unit of work).
func NewBuiltinLowerer(runtimeAlias, ctxGoName, storeGoName string) *BuiltinLowerer {
	return &BuiltinLowerer{runtimeAlias: runtimeAlias, ctxGoName: ctxGoName, storeGoName: storeGoName}
}

// WithPerAggregateStore anexa storeGoNameFor a b (encadeável) — ver a doc do
// campo storeGoNameFor. Usado só pelo caminho de emissão 2PC de
// decl_usecase.go (G1); todo outro chamador (Query, UseCase de um único
// banco, Policy, Worker, Saga) nunca chama isto, preservando storeGoName
// fixo.
func (b *BuiltinLowerer) WithPerAggregateStore(f func(typeName string) string) *BuiltinLowerer {
	b.storeGoNameFor = f
	return b
}

// store devolve o nome Go do parâmetro de acesso à persistência a usar para
// typeName: storeGoNameFor(typeName) quando roteado (G1, 2PC), senão o
// storeGoName fixo (Marco E/F, todo o resto do gerador).
func (b *BuiltinLowerer) store(typeName string) string {
	if b.storeGoNameFor != nil {
		return b.storeGoNameFor(typeName)
	}
	return b.storeGoName
}

// --- 1. now()/uuid()/random(min,max)/random_str(length) — CallExpr. ---

// builtinFuncArity valida a quantidade de argumentos de cada built-in de
// FUNÇÃO (reconhecida por nome, não por tipo declarado — "now" etc. não são
// símbolos do programa). Usada só para checar a chamada; a emissão em si é
// o switch de CallFunc.
var builtinFuncArity = map[string]int{
	"now":        0,
	"uuid":       0,
	"random":     2,
	"random_str": 1,
}

// CallFunc traduz uma chamada de built-in de FUNÇÃO — now()/uuid()/
// random(min,max)/random_str(length) — para o texto Go correspondente.
// Chamada por Lowerer.call (expr.go) ANTES do caminho normal de construção
// de tipo, porque essas built-ins são reconhecidas por NOME, não por
// símbolo declarado. handled=false quando name não é nenhuma das quatro —
// o chamador segue para o caminho normal (construção de VO/Event/Command, ou
// erro se também não for isso). handled=true com err!=nil é um erro de
// geração de verdade (aridade errada, argumento nomeado) que o chamador deve
// propagar, não ignorar.
func (b *BuiltinLowerer) CallFunc(l *Lowerer, name string, args []ast.Arg) (goExpr string, handled bool, err error) {
	want, known := builtinFuncArity[name]
	if !known {
		return "", false, nil
	}
	if len(args) != want {
		return "", true, fmt.Errorf("codegen: %s(...): esperava %d argumento(s), recebeu %d", name, want, len(args))
	}
	argsGo := make([]string, len(args))
	for i, a := range args {
		if a.Name != "" {
			return "", true, fmt.Errorf("codegen: %s(...): argumento nomeado %q não suportado em built-in", name, a.Name)
		}
		g, exprErr := l.Expr(a.Value)
		if exprErr != nil {
			return "", true, exprErr
		}
		argsGo[i] = g
	}

	switch name {
	case "now":
		return fmt.Sprintf("%s.Now(%s)", b.runtimeAlias, b.ctxGoName), true, nil
	case "uuid":
		return fmt.Sprintf("%s.UUID()", b.runtimeAlias), true, nil
	case "random":
		return fmt.Sprintf("%s.Random(%s, %s)", b.runtimeAlias, argsGo[0], argsGo[1]), true, nil
	case "random_str":
		return fmt.Sprintf("%s.RandomStr(%s)", b.runtimeAlias, argsGo[0]), true, nil
	default:
		return "", false, nil // inalcançável: todo nome em builtinFuncArity é tratado acima
	}
}

// --- 2. QueryExpr em posição de expressão PURA: só "exists" cabe aqui. ---

// QueryExprPure traduz um *ast.QueryExpr em posição de expressão PURA (1
// valor, nunca falha). Chamada por Lowerer.queryExpr (expr.go).
//
//   - "exists" → "<X> != nil" (existsExpr).
//   - "load"/"list"/"count" → erro claro: essas formas devolvem (_, error)
//     em Go e SÓ são suportadas via hoisting em nível de statement
//     (StmtLowerer.hoistQueryExpr, stmt.go, intercepta ANTES de chegar
//     aqui — ver exprHoisted/hoistSubtree). Alcançar este ramo significa que
//     o chamador não passou por hoisting (bug de geração).
//   - "store"/"call"/"delete" (ops de arquivo, §2.5) → fora de escopo desta
//     task: dependem do seam FileStorage do mod.ds, G1a (REQ-22.7(b)).
func (b *BuiltinLowerer) QueryExprPure(l *Lowerer, n *ast.QueryExpr) (string, error) {
	switch n.Op {
	case "exists":
		return b.existsExpr(l, n)
	case "load", "list", "count":
		return "", fmt.Errorf("codegen: %s ... em posição de expressão pura não é suportado por Lowerer.Expr — devolve (_, error) e precisa de hoisting em nível de statement (StmtLowerer intercepta antes de chamar Lowerer.Expr; ver hoistQueryExpr em stmt.go, E5.3)", n.Op)
	case "store", "call", "delete":
		return "", fmt.Errorf("codegen: QueryExpr.Op %q (operação de arquivo) fica para G1a (depende do seam FileStorage do mod.ds) — fora do escopo do núcleo transacional (E5.3, REQ-22.7(b))", n.Op)
	default:
		return "", fmt.Errorf("codegen: QueryExpr.Op desconhecido: %q", n.Op)
	}
}

// existsExpr traduz "<X> exists" (QueryExpr pós-fixo, ex. "ensure wallet
// exists else WalletNotFound") para "<X lowerizado> != nil" — a forma mais
// direta dado que Load<T> (hoistLoad, stmt.go) devolve um ponteiro de
// Aggregate e EventStore.Load não distingue "não encontrado" de "stream
// vazia" (codegen/rtsrc/eventstore.go.txt). A semântica completa
// (distinguir erro de infra vs. não-encontrado vs. idempotência) é G2 —
// aqui só a tradução sintática básica.
func (b *BuiltinLowerer) existsExpr(l *Lowerer, n *ast.QueryExpr) (string, error) {
	xGo, err := l.Expr(n.Target)
	if err != nil {
		return "", fmt.Errorf("codegen: ... exists: %w", err)
	}
	return xGo + " != nil", nil
}

// --- 3. load/list/count: o TEXTO Go de cada chamada (o hoisting em si mora
// em stmt.go — hoistLoad/hoistList/hoistCount chamam os métodos abaixo). ---

// loadFuncName devolve o nome Go da função de reconstrução assumida pela
// convenção desta task para "load T(id)": Load<T>. A EXISTÊNCIA dessa
// função (reconstrução de um Aggregate via EventStore/repositório, §design
// 3.7) é responsabilidade de E6.2, ainda não implementada — esta task só
// GERA A CHAMADA, assumindo a convenção de nome.
func (b *BuiltinLowerer) loadFuncName(typeName string) string {
	return "Load" + typeName
}

// LoadCall devolve o texto Go (sem "tmp, err :=") de "Load<T>(<store>,
// <idGo>)" — a chamada que "load T(id)" loweriza para (§design 3.7/3.8).
// Devolve (*T, error): SEMPRE falível na convenção assumida aqui (mesmo que
// o T não exista — ver doc de existsExpr sobre a distinção "não encontrado"
// vs. erro de infra, adiada a G2), então SEMPRE precisa do padrão
// "tmp, err := ...; if err != nil { ... }" — por isso só é chamada de dentro
// de hoistLoad (stmt.go), nunca em posição de expressão pura.
func (b *BuiltinLowerer) LoadCall(typeName, idGo string) string {
	return fmt.Sprintf("%s(%s, %s)", b.loadFuncName(typeName), b.store(typeName), idGo)
}

// ListCall devolve o texto Go PROVISÓRIO (sem "tmp, err :=") de uma
// "list T [where Cond] [as V]": "<store>.List(<ctx>, <predicado>)".
// predGo é "nil" (texto Go literal) quando a QueryExpr não tem cláusula
// "where" — hoistList (stmt.go) decide isso.
//
// DECISÃO EXPLICITAMENTE PROVISÓRIA (documentada aqui e no prompt da task
// E5.3): não existe NENHUM mecanismo de query real no runtime ainda — nem
// Repository nem EventStore (codegen/rtsrc) têm um jeito de listar/filtrar.
// Construir isso de verdade é o Read Side inteiro (E8, Marco E fase 8), que
// pode escolher uma API bem diferente depois de ver Query/View reais (ex.
// cláusulas orderBy/skip/take completas, joins). Esta task só estabelece que
// a FORMA da lowering existe e é testável isoladamente (com um stub
// sintético de <store> no teste, o mesmo padrão que E3.2 usou pra Errors) —
// sem comprometer a API real de E8. O receptor reusa storeGoName (o mesmo
// parâmetro de acesso à persistência de LoadCall): não há, hoje, um conceito
// de "repositório de leitura" distinto no runtime; E8 decide se isso muda.
func (b *BuiltinLowerer) ListCall(predGo string) string {
	return fmt.Sprintf("%s.List(%s, %s)", b.storeGoName, b.ctxGoName, predGo)
}

// CountCall é o análogo de ListCall para "count [where Cond]":
// "<store>.Count(<ctx>, <predicado>)", devolvendo (int64, error). Mesma
// ressalva de API provisória documentada em ListCall — E8 decide a forma
// final.
func (b *BuiltinLowerer) CountCall(predGo string) string {
	return fmt.Sprintf("%s.Count(%s, %s)", b.storeGoName, b.ctxGoName, predGo)
}

// --- helpers de QueryClause compartilhados por builtins.go/stmt.go. ---

// queryClauseByKw procura a cláusula de keyword kw entre as QueryClause de
// uma QueryExpr e devolve sua Expr (ex.: a condição de "where"), se houver.
func queryClauseByKw(clauses []ast.QueryClause, kw string) (ast.Expr, bool) {
	for _, c := range clauses {
		if c.Kw == kw {
			return c.Expr, true
		}
	}
	return nil, false
}

// hasQueryClause reporta se clauses contém uma cláusula de keyword kw, sem
// se importar com seu conteúdo — usado para "as", cujo dado relevante é
// Extra (o nome do tipo), não Expr (queryClauseByKw devolve (nil, true) para
// uma cláusula "as", já que ela não guarda Expr).
func hasQueryClause(clauses []ast.QueryClause, kw string) bool {
	_, ok := queryClauseByKw(clauses, kw)
	return ok
}
