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
//   - list T [where ...] [as V] / count [where ...] → desde H4 (§22.4),
//     backed de verdade por runtime.Collection[T] (rtsrc/collection.go.txt):
//     "where" vira um PREDICADO POR ITEM ("func(item T) bool { ... }" — ver
//     stmt.go, hoistQueryPredicate), não mais um bool solto avaliado uma
//     única vez (a forma anterior a esta task, insuficiente para filtrar de
//     verdade — documentada como achado em hoistQueryPredicate). Ainda
//     deliberadamente NARROW: sem ordenação/paginação/joins, que continuam
//     Read Side de verdade (E8, Marco E fase 8) quando surgir necessidade
//     real de Query/View mais completas.
//   - exists (QueryExpr pós-fixo de "ensure X exists") → checagem estrutural
//     "X != nil" sobre um Aggregate já carregado; a semântica completa
//     (infra vs. não-encontrado vs. idempotência) é G2.
//
// Ops de arquivo (store/signed_url/delete file/load File(ref), §2.5, G1a,
// REQ-22.7(b)) — abaixo, seção 4: `store`/`delete file`/`load File(ref)`
// sempre precisam de hoisting (devolvem (_, error) em Go — o mesmo motivo de
// load/list/count) e vivem, respectivamente, em StmtLowerer.hoistStore/
// deleteFileStmt/hoistLoadFile (stmt.go); `signed_url` é a exceção: o runtime
// (rtsrc/filestorage.go.txt) o expõe como INFALÍVEL (só "string", sem
// error — coerente com o retorno declarado no spec, "signed_url(ref,
// expires:) -> string"), então cabe direto aqui, em CallFunc, como
// now()/uuid()/random(...).
//
// O mecanismo de HOISTING de load/list/count (porque a chamada Go que os
// substitui devolve (_, error), que não cabe em posição de expressão pura)
// mora em stmt.go (hoistQueryExpr/hoistLoad/hoistList/hoistCount) — o mesmo
// tratamento que hoistVOConstruct já dá à construção de VO composto (E5.2).
// Este arquivo só decide O TEXTO Go de cada chamada; a decisão de QUANDO
// hoistear é do StmtLowerer.

// BuiltinLowerer traduz as built-ins do núcleo (§2.6 do spec da linguagem,
// REQ-22.7(a)) e as ops de arquivo (§2.5, REQ-22.7(b), G1a) para chamadas Go:
// now/uuid/random/random_str (runtime, sem dependência), load/list/count/
// exists (operações de domínio, contra os seams de persistência do runtime),
// store/signed_url/delete file/load File(ref) (contra o seam FileStorage,
// roteado por fileStorageByField/fileStorageDefault). Usa os mesmos
// ctxGoName/storeGoName em toda chamada — nomes configuráveis (o Emitter que
// compõe o corpo final decide os nomes reais dos parâmetros ctx/tx, esta
// task só usa o que for passado).
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
	// fileStorageByField/fileStorageDefault roteiam store/signed_url/delete
	// file/load File(ref) (G1a, §2.5) para o FileStorage certo — anexados via
	// WithFileStorage. Ambos vazios (o default) preserva o comportamento de
	// antes de G1a: qualquer op de arquivo falha com um erro claro de
	// roteamento (ver resolveFileStorageName) em vez de gerar Go quebrado.
	fileStorageByField map[string]string
	fileStorageDefault string
	// wrapLoadWithEventLoader, quando true, faz LoadCall envolver storeGoName
	// num "<runtimeAlias>.NewEventLoader(<ctxGoName>, <storeGoName>)" em vez
	// de usá-lo cru — o adaptador que runtime.EventStore precisa para
	// satisfazer runtime.EventLoader (codegen/rtsrc/eventloader.go.txt),
	// necessário quando o "store" do construto NÃO é já um EventLoader/Tx por
	// si: o caso de Query (BuiltinLowerer aqui recebe "store", um
	// runtime.EventStore cru — ao contrário de UseCase, cujo "tx" é
	// runtime.Tx, que satisfaz EventLoader estruturalmente sem wrapper
	// nenhum). Habilitado por WithEventLoaderWrapping (decl_query.go) — gap
	// pré-existente descoberto por G1a: toda Query real anterior só usava
	// "load T(id) as View" (decl_query.go/emitLoadAsView, que já monta esse
	// MESMO wrapper à mão, texto hardcoded) — um "load T(id)" NU, sem "as",
	// dentro de um corpo de Query nunca tinha sido exercitado antes. Só se
	// aplica a Aggregate EventSourced (mesma suposição implícita, nunca
	// documentada antes, que emitLoadAsView já fazia) — StateStored dentro de
	// Query continua fora do escopo (nenhuma fixture, real ou sintética,
	// precisa disso ainda).
	wrapLoadWithEventLoader bool
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

// store devolve a expressão Go de acesso à persistência a usar para
// typeName, na ordem: storeGoNameFor(typeName) quando roteado (G1, 2PC);
// senão, storeGoName envolto em runtime.NewEventLoader quando
// wrapLoadWithEventLoader está ligado (Query, ver a doc do campo); senão,
// storeGoName cru (Marco E/F, todo o resto do gerador).
func (b *BuiltinLowerer) store(typeName string) string {
	if b.storeGoNameFor != nil {
		return b.storeGoNameFor(typeName)
	}
	if b.wrapLoadWithEventLoader {
		return fmt.Sprintf("%s.NewEventLoader(%s, %s)", b.runtimeAlias, b.ctxGoName, b.storeGoName)
	}
	return b.storeGoName
}

// WithEventLoaderWrapping liga wrapLoadWithEventLoader (encadeável) — ver a
// doc do campo. Usado só por decl_query.go (Query.Body): o "store" ali é um
// runtime.EventStore cru, que precisa do adaptador runtime.NewEventLoader
// para satisfazer o parâmetro runtime.EventLoader de Load<Aggregate>.
func (b *BuiltinLowerer) WithEventLoaderWrapping() *BuiltinLowerer {
	b.wrapLoadWithEventLoader = true
	return b
}

// WithFileStorage anexa o roteamento de FileStorage (G1a, §2.5, REQ-22.7(b))
// a b: byField mapeia nome de campo (do bloco storage{} de QUALQUER
// Aggregate do módulo — ver codegen/decl_aggregate_storage.go,
// moduleFileStorageRouting) para o nome do FileStorage declarado em mod.ds;
// defaultName (pode ser "") é o nome da ÚNICA FileStorage do módulo, usado
// quando o alvo de uma operação não é um MemberExpr reconhecível ou seu
// campo não bate com nenhuma entrada de byField. Devolve o próprio b
// (encadeável, mesmo padrão de WithPerAggregateStore).
func (b *BuiltinLowerer) WithFileStorage(byField map[string]string, defaultName string) *BuiltinLowerer {
	b.fileStorageByField = byField
	b.fileStorageDefault = defaultName
	return b
}

// resolveFileStorageName decide, para o alvo de uma operação de arquivo
// (Target de "store"/"delete file(ref)"/"load File(ref)", ou o 1º argumento
// de "signed_url"), qual FileStorage (nome declarado em mod.ds) atende essa
// operação (§2.5): (1) se target é um MemberExpr cujo Name bate com uma
// entrada de fileStorageByField (o campo roteado pelo storage{} de algum
// Aggregate do módulo) — a resposta mais específica, e a única que desambigua
// quando o módulo declara 2+ FileStorage; (2) senão, fileStorageDefault,
// quando o módulo declara exatamente 1 FileStorage; (3) erro claro em
// qualquer outro caso — nunca uma escolha silenciosa (REQ-14.4).
func (b *BuiltinLowerer) resolveFileStorageName(target ast.Expr) (string, error) {
	if mem, ok := target.(*ast.MemberExpr); ok {
		if name, ok := b.fileStorageByField[mem.Name]; ok {
			return name, nil
		}
	}
	if b.fileStorageDefault != "" {
		return b.fileStorageDefault, nil
	}
	return "", fmt.Errorf("codegen: não consegui determinar qual FileStorage usar (§2.5): o alvo desta operação não corresponde a nenhum campo roteado pelo bloco storage{} de um Aggregate do módulo, e o módulo não declara uma única FileStorage sem ambiguidade — WithFileStorage nunca foi anexado, ou o mod.ds declara 0 ou 2+ FileStorage sem uma rota de campo que bata")
}

// fileStorageGoExpr devolve o texto Go de acesso à instância de FileStorage
// resolvida para target (ver resolveFileStorageName): "fileStorages[%q]",
// indexando o registro wired pelo módulo (ver codegen/decl_usecase.go,
// emitFileStorageWiring — G1a).
func (b *BuiltinLowerer) fileStorageGoExpr(target ast.Expr) (string, error) {
	name, err := b.resolveFileStorageName(target)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("fileStorages[%q]", name), nil
}

// StoreCall devolve o texto Go (sem "tmp, err :=") de "store <file>" (§2.5):
// "<FileStorage>.Store(<ctx>, <fileGo>)" — devolve (runtime.FileRef, error),
// SEMPRE hoisted (StmtLowerer.hoistStore, stmt.go). target é a Expr ORIGINAL
// de QueryExpr.Target (ex. "cmd.content") — usada só para resolver QUAL
// FileStorage (resolveFileStorageName); fileGo já é o texto Go pronto do
// arquivo a armazenar, produzido pelo chamador.
func (b *BuiltinLowerer) StoreCall(target ast.Expr, fileGo string) (string, error) {
	storageGo, err := b.fileStorageGoExpr(target)
	if err != nil {
		return "", fmt.Errorf("codegen: store %s: %w", fileGo, err)
	}
	return fmt.Sprintf("%s.Store(%s, %s)", storageGo, b.ctxGoName, fileGo), nil
}

// LoadFileCall devolve o texto Go de "load File(ref)" (§2.5):
// "<FileStorage>.Load(<ctx>, <refGo>)" — devolve (runtime.File, error),
// SEMPRE hoisted (StmtLowerer.hoistLoadFile). refExpr é a Expr ORIGINAL do
// argumento (ex. "doc.state.content") — só para roteamento.
func (b *BuiltinLowerer) LoadFileCall(refExpr ast.Expr, refGo string) (string, error) {
	storageGo, err := b.fileStorageGoExpr(refExpr)
	if err != nil {
		return "", fmt.Errorf("codegen: load File(%s): %w", refGo, err)
	}
	return fmt.Sprintf("%s.Load(%s, %s)", storageGo, b.ctxGoName, refGo), nil
}

// DeleteFileCall devolve o texto Go de "delete file(ref)" (§2.5):
// "<FileStorage>.Delete(<ctx>, <refGo>)" — devolve só error, propagado como
// um statement solto (StmtLowerer.deleteFileStmt), nunca em posição de
// expressão pura.
func (b *BuiltinLowerer) DeleteFileCall(refExpr ast.Expr, refGo string) (string, error) {
	storageGo, err := b.fileStorageGoExpr(refExpr)
	if err != nil {
		return "", fmt.Errorf("codegen: delete file(%s): %w", refGo, err)
	}
	return fmt.Sprintf("%s.Delete(%s, %s)", storageGo, b.ctxGoName, refGo), nil
}

// signedURLCall traduz "signed_url(ref, expires: <duração>)" (§2.5) para
// "<FileStorage>.SignedURL(<ctx>, <refGo>, <expiresGo>)" — devolve só
// "string" (SEM error, ver rtsrc/filestorage.go.txt: uma signed URL é
// derivada deterministicamente da FileRef + expiração, sem round-trip ao
// backend), então cabe direto em posição de expressão pura (chamada por
// CallFunc, como now()/uuid()) — ao contrário de store/load File/delete
// file, que sempre falham de verdade em Go e por isso vivem em stmt.go.
// Exige exatamente 2 argumentos: o 1º posicional (a FileRef) e "expires"
// nomeado (uma duração); qualquer outra forma é erro de geração claro.
func (b *BuiltinLowerer) signedURLCall(l *Lowerer, args []ast.Arg) (string, error) {
	if len(args) != 2 || args[0].Name != "" || args[1].Name != "expires" {
		return "", fmt.Errorf("codegen: signed_url(...): esperava (ref, expires: <duração>), forma recebida não bate")
	}
	refGo, err := l.Expr(args[0].Value)
	if err != nil {
		return "", err
	}
	expiresGo, err := l.Expr(args[1].Value)
	if err != nil {
		return "", err
	}
	storageGo, err := b.fileStorageGoExpr(args[0].Value)
	if err != nil {
		return "", fmt.Errorf("codegen: signed_url(%s, ...): %w", refGo, err)
	}
	return fmt.Sprintf("%s.SignedURL(%s, %s, %s)", storageGo, b.ctxGoName, refGo, expiresGo), nil
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
	if name == "signed_url" {
		goExpr, err := b.signedURLCall(l, args)
		return goExpr, true, err
	}
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
//   - "load"/"list"/"count"/"store"/"delete" → erro claro: essas formas
//     devolvem (_, error) em Go e SÓ são suportadas via hoisting em nível de
//     statement (StmtLowerer.hoistQueryExpr/hoistStore/hoistLoadFile/
//     deleteFileStmt, stmt.go, interceptam ANTES de chegar aqui — ver
//     exprHoisted/hoistSubtree). Alcançar este ramo significa que o
//     chamador não passou por hoisting (bug de geração).
//   - "call" (chamada síncrona via Adapter/Notification, §18.2) → fora do
//     escopo de G1a: o mecanismo de F4 (decl_io.go) reconhece "Xxx(...)"
//     como notify/call por FORMA (CallExpr sobre um nome de Notification),
//     não via QueryExpr.Op "call" — esta forma nunca foi implementada e
//     continua fora do escopo desta task.
func (b *BuiltinLowerer) QueryExprPure(l *Lowerer, n *ast.QueryExpr) (string, error) {
	switch n.Op {
	case "exists":
		return b.existsExpr(l, n)
	case "load", "list", "count", "store", "delete":
		return "", fmt.Errorf("codegen: %s ... em posição de expressão pura não é suportado por Lowerer.Expr — devolve (_, error) e precisa de hoisting em nível de statement (StmtLowerer intercepta antes de chamar Lowerer.Expr; ver hoistQueryExpr/hoistStore/deleteFileStmt em stmt.go)", n.Op)
	case "call":
		return "", fmt.Errorf("codegen: QueryExpr.Op %q (chamada síncrona via Adapter/Notification) não é suportado — fora do escopo de G1a", n.Op)
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

// ListCall devolve o texto Go (sem "tmp, err :=") de uma "list T [where
// Cond] [as V]": "<store>.List(<ctx>, <predicado>)". predGo é "nil" (texto
// Go literal) quando a QueryExpr não tem cláusula "where", ou um
// "func(item T) bool { ... }" de verdade quando tem (hoistQueryPredicate,
// stmt.go — H4, §22.4, ver a doc lá sobre o redesenho desta task). typeName
// é o nome nu do tipo listado (Target de qe, ex. "Ticket") — roteado por
// b.store(typeName), o MESMO mecanismo que LoadCall já usa (WithPerAggregate
// Store, G1): antes desta task, ListCall/CountCall usavam b.storeGoName
// direto, sem roteamento por tipo — inofensivo enquanto o único chamador
// real era um único "store"/"tx" fixo (UseCase/Query), mas insuficiente para
// Policy (H4), que precisa de um runtime.Collection[T] DIFERENTE por T
// (decl_policy.go: um var de pacote por tipo referenciado, ex.
// "ticketCollection").
//
// API ainda deliberadamente NARROW (documentado desde E5.3): runtime.
// Collection[T] (rtsrc/collection.go.txt, H4) só filtra por predicado —
// sem ordenação/paginação/joins, que continuam Read Side de verdade (E8,
// Marco E fase 8) quando surgir necessidade real de Query/View mais
// completas.
func (b *BuiltinLowerer) ListCall(typeName, predGo string) string {
	return fmt.Sprintf("%s.List(%s, %s)", b.store(typeName), b.ctxGoName, predGo)
}

// CountCall é o análogo de ListCall para "count [where Cond]":
// "<store>.Count(<ctx>, <predicado>)", devolvendo (int64, error). Mesmo
// roteamento por typeName e mesma ressalva de escopo narrow documentada em
// ListCall.
func (b *BuiltinLowerer) CountCall(typeName, predGo string) string {
	return fmt.Sprintf("%s.Count(%s, %s)", b.store(typeName), b.ctxGoName, predGo)
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
