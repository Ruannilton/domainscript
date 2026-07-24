package codegen

import (
	"fmt"
	"path"
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

// decl_policy.go emite o Go de um PolicyDecl (F1, REQ-23.1/23.5, §design
// 3.10): um subscriber com a assinatura EXATA que runtime.Dispatcher/
// runtime.Outbox.Subscribe esperam — func(ctx context.Context, ev
// runtime.Event) error — registrado DIRETO como handler pelo wiring do
// módulo (emitPolicyWireFunc abaixo), sem closure de adaptação no wiring: o
// type assertion pro tipo concreto do evento (runtime.Event é uma interface
// satisfeita só por PONTEIRO — SetMeta/EventType() têm receptor ponteiro,
// ver codegen/rtsrc/event.go.txt) mora dentro do próprio handler, o mesmo
// padrão que decl_aggregate_load.go já usa no switch de replay ("case *%s:
// …applyX(*ev)"), reaplicado aqui.
//
// --- Garantia de entrega (PolicyDecl.Delivery, REQ-23.1) ---
//
// "AtLeastOnce" registra no seam runtime.Outbox (codegen/rtsrc/outbox.go.txt,
// F1 acrescenta o seam mínimo — implementação real de durabilidade é F5);
// qualquer outro valor (o único outro caso real do front-end hoje é
// "BestEffort") registra em runtime.Dispatcher, in-process. sema NÃO valida
// o literal de Delivery hoje — um valor desconhecido cai no mesmo caminho de
// "BestEffort" (o mais conservador), nunca um erro de geração por um texto
// livre que o parser já aceitou.
//
// --- Corpo via lowering (REQ-23.5) ---
//
// A MESMA StmtLowerer/Lowerer usada por Handle/UseCase. event (o tipo do
// Event/PublicEvent de `on`) e caller (runtime.Caller, via
// runtime.CallerFrom(ctx) — a MESMA convenção de decl_usecase.go) são os
// dois receptores contextuais (resolver/receivers.go já lista os dois para
// constructPolicyExecute; lower/env.go.SeedPolicyExecute já existe desde
// E5.0, escrito ANTES de Policy ganhar emissor de propósito). Como o corpo
// real do shop ("execute { return }",
// docs/examples/shop/shipping/policy.ds) nunca referencia nem "event" nem
// "caller", os dois levam uma linha "_ = X" logo após a extração — sem isso,
// um corpo trivial deixaria uma variável local Go declarada e não usada,
// erro de compilação. Esse problema NÃO existe em UseCase: o dispatch de
// Handle de handleDispatchCall injeta "caller" como argumento sempre que
// dispara, então a var lá sempre é usada; Policy não tem essa garantia.
//
// --- list/count (H4, §22.4): runtime.Collection[T], roteado por tipo ---
//
// "load" continua NÃO conectado aqui (nenhum corpo real de Policy os
// exercita, e a forma certa de um load dentro de uma Policy é uma decisão de
// design maior deixada para quando um exemplo real precisar dela — mesmo
// espírito da nota de escopo de decl_query.go sobre where/orderBy/skip/take).
// "list"/"count", porém, ganharam um exemplo real com esta task (§22.4,
// RefundAllOnEventCancelled: "list Ticket t where t.eventId == event.id") —
// policyCollectionTypeNames varre CADA PolicyDecl.Execute do arquivo (todas
// as decls de EmitPolicies, não só a corrente: um var de pacote é
// compartilhado entre Policy do MESMO pacote que referenciam o MESMO tipo)
// por *ast.QueryExpr "list"/"count", coleta o nome NU do tipo (Target,
// astutil.HeadName) e emite, uma vez por tipo distinto (emitPolicyCollectionVars),
// "var <tipo>Collection = runtime.NewMemoryCollection[<Tipo>]()" — nomeado
// via policyCollectionVarName (lowerFirst + "Collection", mesma convenção de
// "sourceVar" em decl_worker.go:emitContinuous). Cada emitPolicyDecl anexa
// WithBuiltins(NewBuiltinLowerer(...).WithPerAggregateStore(typeToVar)) — o
// MESMO mecanismo de roteamento por tipo que WithPerAggregateStore já provê
// para o caminho 2PC de decl_usecase.go (G1) — reusado aqui para rotear cada
// "list T .../count T ..." para o Collection[T] certo. Uma Policy SEM
// nenhum list/count (o caso comum, ex. o shop de hoje) não ganha var de
// Collection nenhum e não anexa WithBuiltins — Go idêntico ao gerado antes
// desta task (guarda simétrica à de needsErrors/needsRand em gentest.go).
//
// --- emit (H4, §22.4): seam de Dispatcher, não "events" local ---
//
// Um "emit Evento(...)" dentro do execute de uma Policy não tem onde
// acumular (a assinatura fixa "(ctx, ev) error" não devolve um []runtime.
// Event, ao contrário de Handle/Apply) — StmtLowerer.WithEmitDispatch (E5.2,
// lower/stmt.go) publica DIRETO num runtime.Dispatcher em vez de fazer
// "events = append(...)". policyBodyHasEmit varre CADA decl.Execute (mesma
// varredura que policyCollectionTypeNames faz para list/count, agora por
// *ast.EmitStmt) — se ALGUMA Policy do arquivo usa "emit", um único var de
// pacote "var policyDispatcher runtime.Dispatcher" é declarado (guardado,
// mesmo espírito de needsErrors/needsRand) e CADA emitPolicyDecl anexa
// WithEmitDispatch("policyDispatcher", "ctx") ao seu StmtLowerer — mesmo
// quando a Policy corrente especificamente não usa emit (mais simples e
// inofensivo que rastrear "quais decls usam emit" individualmente: anexar
// o dispatch não muda nada para um corpo sem "emit"). Wire (abaixo) atribui
// "policyDispatcher = d" como a 1ª linha do corpo — incondicional (nunca
// dependente de canal/Delivery por Policy, ao contrário do resto de Wire) —
// "d" é sempre o runtime.Dispatcher que o chamador (cmd/<service>/main.go)
// já constrói para o parâmetro de Wire, então sempre disponível ali. Um
// teste do MESMO pacote gerado (gentest.go, EmitTests) reatribui
// policyDispatcher para um runtime.NewDispatcher() PRÓPRIO antes de invocar
// a Policy diretamente (sem passar por Wire/Subscribe) — ver a doc de
// gentest.go sobre "then { emitted ... }".
//
// --- Wire (emitPolicyWireFunc) ---
//
// Mesmo nome/padrão de emitUOWWireFunc (decl_usecase.go, E7.2): "func
// Wire(d runtime.Dispatcher)" registra cada Policy do pacote — chamada por
// cmd/<service>/main.go na inicialização (E9.1). Um módulo que declare
// UseCase E Policy teria DOIS "func Wire" no mesmo pacote Go (erro de
// compilação); codegen.go (generateModuleFiles) recusa esse caso HOJE com um
// erro de geração claro em vez de deixar o Go gerado não compilar —
// combinar as duas infra numa única Wire fica para quando um exemplo real
// (Marco F2+: Worker/Saga/Notification vão precisar do mesmo seam) precisar
// disso. Nem o wallet nem o shop de hoje combinam as duas categorias no
// mesmo módulo (Orders só tem UseCase, Shipping só tem Policy).
//
// NEM TODA Policy do pacote acaba assinada em "d" (F5, REQ-26.5): uma Policy
// cross-service via um canal "queue" da topologia assina no seu PRÓPRIO
// runtime.ChannelTransport (construído dentro do próprio Wire) em vez de em
// "d"/Outbox(d) — "d" continua o parâmetro, mas fica sem uso para essa
// Policy específica. Ver a doc de emitPolicyWireFunc para os detalhes de
// como cada Policy decide seu alvo de Subscribe.

// policyEventInfo é a forma já resolvida do Event/PublicEvent de
// PolicyDecl.On: o *ast.EventDecl, o tipo Go de referência já qualificado,
// SEMPRE um ponteiro (ver a doc do arquivo) — "*OrderPlaced" (mesmo pacote,
// Event privado) ou "*contracts.OrderPlaced" (import próprio, pacote
// compartilhado §design 3.4, PublicEvent) — e originModule, o módulo que
// DECLARA o evento (symbols.Symbol.Module), usado por
// emitPolicyWireFunc (F5, REQ-26.5) para decidir se esta Policy é
// cross-service e, se for, qual *program.Channel da topologia rege a
// entrega (prog.ChannelBetween(originModule, module)).
type policyEventInfo struct {
	decl         *ast.EventDecl
	goPtrType    string
	originModule string
}

// resolvePolicyEvent resolve PolicyDecl.On a um Event/PublicEvent conhecido —
// mesmo padrão de resolveApplyEvent (decl_aggregate_load.go): REQ-9
// (resolver.resolveOn, resolver/resolver.go) já validou que o nome existe em
// algum módulo do programa antes deste emissor rodar (Lookup local, fallback
// Find cross-module — a MESMA ordem de resolver.resolveOn); aqui só se
// reconsulta a SymbolTable para decidir o pacote Go de referência e o módulo
// de origem (symbols.Symbol.Module — F5). Um evento não resolvido ou que
// não seja EventDecl é bug de geração (REQ-9 já deveria ter barrado), não
// caminho normal.
func resolvePolicyEvent(e *emit.Emitter, tab *symbols.SymbolTable, module, eventName string) (policyEventInfo, error) {
	sym, ok := tab.Lookup(module, eventName)
	if !ok {
		sym, ok = tab.Find(eventName)
	}
	if !ok {
		return policyEventInfo{}, fmt.Errorf("evento %q não resolvido (bug de geração — REQ-9 já deveria ter barrado isso)", eventName)
	}
	ed, ok := sym.Decl.(*ast.EventDecl)
	if !ok {
		return policyEventInfo{}, fmt.Errorf("%q não resolve a um Event (got %T)", eventName, sym.Decl)
	}

	if !ed.Public {
		return policyEventInfo{decl: ed, goPtrType: "*" + ed.Name, originModule: sym.Module}, nil
	}
	contractsAlias := e.Import(path.Join(domainModuleRoot, "contracts"))
	return policyEventInfo{decl: ed, goPtrType: "*" + goname.QualifiedRef(contractsAlias, ed.Name), originModule: sym.Module}, nil
}

// policyCollectionVarName deriva o nome Go do var de pacote runtime.
// Collection[T] que guarda instâncias de typeName (H4, §22.4):
// lowerFirst(typeName) + "Collection" — mesma convenção de "sourceVar"
// (decl_worker.go:emitContinuous) e do "varName" de canal em
// resolvePolicyWireInfos, acima. Compartilhada (não reimplementada) entre
// esta emissão (emitPolicyCollectionVars, abaixo) e codegen/gentest.go (H4):
// um "given <binding> [...]" de Test precisa semear EXATAMENTE o mesmo var
// que o corpo da Policy sob teste lê via list/count.
func policyCollectionVarName(typeName string) string {
	if typeName == "" {
		return "collection"
	}
	return strings.ToLower(typeName[:1]) + typeName[1:] + "Collection"
}

// policyCollectionTypeNames varre CADA decl.Execute de decls (não só uma
// Policy — várias Policy do MESMO pacote podem referenciar o MESMO tipo, e
// só queremos UM var de Collection por tipo) por *ast.QueryExpr "list"/
// "count" (H4, §22.4) e devolve o conjunto de nomes NUS de tipo referenciados
// (astutil.HeadName do Target), ordenado alfabeticamente (determinismo,
// NFR-13 — mais simples e igualmente determinístico que preservar a ordem de
// 1ª aparição entre várias decls). Vazio quando nenhuma Policy do arquivo usa
// list/count (o caso comum) — ver a doc do arquivo sobre a guarda simétrica.
func policyCollectionTypeNames(decls []*ast.PolicyDecl) []string {
	seen := make(map[string]bool)
	for _, decl := range decls {
		astutil.ForEachExprInBlock(decl.Execute, func(e ast.Expr) {
			qe, ok := e.(*ast.QueryExpr)
			if !ok || (qe.Op != "list" && qe.Op != "count") {
				return
			}
			if name := astutil.HeadName(qe.Target); name != "" {
				seen[name] = true
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

// emitPolicyCollectionVars emite, uma vez por tipo distinto de names, "var
// <tipo>Collection = runtime.NewMemoryCollection[<Tipo>]()" (H4, §22.4) — o
// seam que list/count varrem dentro do execute de uma Policy deste pacote, e
// que "given <binding> [...]" de um Test (gentest.go) semeia. Devolve o mapa
// tipo->var, repassado a WithPerAggregateStore de cada emitPolicyDecl (ver a
// doc do arquivo) e reexposto para o CHAMADOR de EmitPolicies (nenhum hoje —
// gentest.go reconstrói a MESMA função via policyCollectionVarName em vez de
// receber o mapa, já que EmitTests roda numa passagem SEPARADA de
// EmitPolicies, sem acesso ao Emitter desta). No-op (mapa vazio, nenhuma
// linha emitida) quando names está vazio.
func emitPolicyCollectionVars(e *emit.Emitter, runtimeAlias string, names []string) map[string]string {
	typeToVar := make(map[string]string, len(names))
	for _, name := range names {
		v := policyCollectionVarName(name)
		typeToVar[name] = v
		e.Line("// %s é o runtime.Collection[%s] que \"list %s .../count %s ...\" (§22.4,", v, name, name, name)
		e.Line("// H4) varrem dentro do execute de uma Policy deste pacote — semeado por um")
		e.Line("// \"given <binding> [...]\" de um Test (gentest.go) nos testes gerados; um")
		e.Line("// wiring de produção real (popular a partir de um EventStore/projeção) fica")
		e.Line("// para quando um exemplo real precisar dele (mesmo espírito da nota de escopo")
		e.Line("// de decl_query.go sobre where/orderBy/skip/take).")
		e.Line("var %s = %s.NewMemoryCollection[%s]()", v, runtimeAlias, name)
		e.Line("")
	}
	return typeToVar
}

// policyBodyHasEmit reporta se b contém, em qualquer profundidade (inclusive
// dentro de um for aninhado — astutil.ForEachStmt já desce nesses níveis),
// um *ast.EmitStmt (H4, §22.4) — usado para guardar a declaração do var de
// pacote "policyDispatcher" (ver a doc do arquivo): um arquivo cujas Policy
// nunca usam "emit" não ganha esse var, gerando Go idêntico ao de antes desta
// task.
func policyBodyHasEmit(b *ast.Block) bool {
	found := false
	astutil.ForEachStmt(b, func(s ast.Stmt) {
		if _, ok := s.(*ast.EmitStmt); ok {
			found = true
		}
	})
	return found
}

// policyIsAtLeastOnce reporta se decl declara a garantia de entrega
// "AtLeastOnce" (REQ-23.1) — ver a doc do arquivo sobre o fallback para
// qualquer outro valor de Delivery.
func policyIsAtLeastOnce(decl *ast.PolicyDecl) bool {
	return decl.Delivery == "AtLeastOnce"
}

// EmitPolicy gera o Go de um único PolicyDecl — a mesma forma de
// EmitPolicies, mantendo o contrato uniforme entre as duas funções (mesmo
// padrão de EmitUseCase/EmitUseCases). adapters (F4, REQ-25.3) é o registry
// de Notification/Adapter do módulo — nil preserva o comportamento anterior
// a F4 (nenhum notify/call reconhecido no corpo). prog (F5, REQ-26.5) é o
// programa agregado — usado só para decidir, por Policy, se ela é
// cross-service via um canal da topologia (ver a doc de
// emitPolicyWireFunc); nil é seguro sobre um programa sem topology.ds (todo
// prog.ChannelBetween devolve nil), o mesmo efeito de nenhum canal
// declarado.
func EmitPolicy(pkg string, decl *ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, prog *program.Program, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl) ([]byte, error) {
	return EmitPolicies(pkg, []*ast.PolicyDecl{decl}, model, tab, prog, module, reg, adapters, nil)
}

// EmitPolicies gera o Go de várias PolicyDecl num único arquivo
// (policies.go), com "func Wire(d runtime.Dispatcher)" compartilhado (ver a
// doc do arquivo) — como um módulo real pode declarar mais de uma Policy.
// adapters (F4) é repassado a cada corpo via StmtLowerer.WithNotifyAdapters
// (ver emitPolicyDecl) — habilita "DepositNotification(...)" dentro de
// execute a reconhecer notify (Mode async)/call (Mode sync) do Adapter
// parceiro (§9.1/9.3, REQ-25.3). prog é repassado a emitPolicyWireFunc (F5).
// sharedCollectionVars (ISSUE-1, ver a doc de decl_collections.go) é o mapa
// tipo->var de runtime.Collection[T] JÁ declarado em collections.go pelo
// CHAMADOR (generateModuleFiles) para os tipos que TAMBÉM são fonte de join
// de alguma Query do mesmo módulo (a interseção calculada por
// sharedModuleCollectionTypeNames) — esta função continua calculando sozinha
// TODO o conjunto de tipos que alguma Policy do arquivo usa via list/count,
// mas, para um tipo presente em sharedCollectionVars, reusa o var de lá em
// vez de declarar o seu (evita a redeclaração, ISSUE-1); qualquer outro tipo
// (o caso comum: nil ou vazio) continua sendo declarado localmente em
// policies.go (emitPolicyCollectionVars), Go byte-idêntico ao de antes desta
// task.
func EmitPolicies(pkg string, decls []*ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, prog *program.Program, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl, sharedCollectionVars map[string]string) ([]byte, error) {
	e := emit.New(pkg)
	runtimeAlias, needsEmitDispatcher, outboxDBName, err := emitPolicyDeclsAndVars(e, decls, model, tab, prog, module, reg, adapters, sharedCollectionVars)
	if err != nil {
		return nil, err
	}
	if err := emitPolicyWireFunc(e, runtimeAlias, decls, model, tab, prog, module, reg, needsEmitDispatcher, outboxDBName); err != nil {
		return nil, fmt.Errorf("codegen: Policy Wire: %w", err)
	}
	return e.Bytes()
}

// emitPoliciesCombinedBytes gera policies.go de um módulo MISTO (UseCase E
// Policy no mesmo módulo, L1.1/REQ-52): idêntico a EmitPolicies até os vars de
// pacote e os subscribers, mas a "func Wire" final é a COMBINADA
// (emitCombinedWireFunc: "func Wire(u runtime.UnitOfWork, d runtime.Dispatcher)"
// fazendo "uow = u" ALÉM de assinar as Policies), não a "func Wire(d
// runtime.Dispatcher)" só-Policy — de forma que não colida com a Wire dos
// UseCases (usecases.go, gerado SEM Wire próprio no caso misto via
// emitUseCasesBytes(..., emitWire=false)). Chamada só por generateModuleFiles
// no ramo misto; os casos puros seguem por EmitUseCases/EmitPolicies, sem
// mudança de assinatura nem de saída.
func emitPoliciesCombinedBytes(pkg string, decls []*ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, prog *program.Program, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl, sharedCollectionVars map[string]string) ([]byte, error) {
	e := emit.New(pkg)
	runtimeAlias, needsEmitDispatcher, outboxDBName, err := emitPolicyDeclsAndVars(e, decls, model, tab, prog, module, reg, adapters, sharedCollectionVars)
	if err != nil {
		return nil, err
	}
	if err := emitCombinedWireFunc(e, runtimeAlias, decls, model, tab, prog, module, reg, needsEmitDispatcher, outboxDBName); err != nil {
		return nil, fmt.Errorf("codegen: Wire combinado (UseCase+Policy): %w", err)
	}
	return e.Bytes()
}

// emitPolicyDeclsAndVars emite, sobre e (policies.go), TUDO que EmitPolicies
// emite MENOS a "func Wire" final: os vars de pacote (runtime.Collection[T] de
// list/count, policyDispatcher de emit) e o subscriber de cada PolicyDecl.
// Devolve o alias do runtime, se algum corpo usa "emit" (needsEmitDispatcher) e
// o Database do outbox durável do módulo (outboxDBName, "" no caso comum) — os
// três que o CHAMADOR repassa ao emissor de Wire escolhido. Extraído de
// EmitPolicies (L1.1/REQ-52) para ser reusado tanto pela Wire só-Policy
// (EmitPolicies → emitPolicyWireFunc) quanto pela Wire combinada de um módulo
// misto (emitPoliciesCombinedBytes → emitCombinedWireFunc): a saída até aqui é
// idêntica nos dois casos, byte a byte igual ao que EmitPolicies sempre gerou.
func emitPolicyDeclsAndVars(e *emit.Emitter, decls []*ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, prog *program.Program, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl, sharedCollectionVars map[string]string) (runtimeAlias string, needsEmitDispatcher bool, outboxDBName string, err error) {
	ctxAlias := e.Import("context")
	runtimeAlias = e.Import(RuntimeImportPath)

	// list/count -> runtime.Collection[T] (H4, §22.4) e emit -> runtime.
	// Dispatcher (H4, §22.4): ver a doc do arquivo. Ambos guardados — um
	// arquivo cujas Policy não usam nenhuma das duas formas gera Go idêntico
	// ao de antes desta task. Para um tipo presente em sharedCollectionVars
	// (ISSUE-1), reusa o var já declarado em collections.go pelo CHAMADOR em
	// vez de declarar de novo aqui.
	var typeToVar map[string]string
	if names := policyCollectionTypeNames(decls); len(names) > 0 {
		typeToVar = make(map[string]string, len(names))
		var toDeclare []string
		for _, name := range names {
			if v, ok := sharedCollectionVars[name]; ok {
				typeToVar[name] = v
				continue
			}
			toDeclare = append(toDeclare, name)
		}
		if len(toDeclare) > 0 {
			for name, v := range emitPolicyCollectionVars(e, runtimeAlias, toDeclare) {
				typeToVar[name] = v
			}
		}
	}
	needsEmitDispatcher = false
	for _, decl := range decls {
		if policyBodyHasEmit(decl.Execute) {
			needsEmitDispatcher = true
			break
		}
	}
	if needsEmitDispatcher {
		e.Line("// policyDispatcher é o runtime.Dispatcher de verdade que Wire (abaixo)")
		e.Line("// recebe — o seam que \"emit\" usa dentro do execute de uma Policy deste")
		e.Line("// pacote (H4, §22.4): StmtLowerer.WithEmitDispatch publica DIRETO aqui, já")
		e.Line("// que uma Policy não tem \"events []runtime.Event\" local para acumular")
		e.Line("// (assinatura fixa \"(ctx, ev) error\") — ver a doc do arquivo/lower/stmt.go.")
		e.Line("var policyDispatcher %s.Dispatcher", runtimeAlias)
		e.Line("")
	}

	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		if derr := emitPolicyDecl(e, decl, model, tab, module, reg, adapters, ctxAlias, runtimeAlias, typeToVar, needsEmitDispatcher); derr != nil {
			return "", false, "", fmt.Errorf("codegen: Policy %s: %w", decl.Name, derr)
		}
	}

	e.Line("")
	// outboxDBName (task J2.5, REQ-42.5): "" quando prog é nil (testes fora
	// de um Program agregado, ex. decl_policy_test.go) ou quando o módulo não
	// declara nenhum Database real — o caminho de sempre, memoryOutbox,
	// nenhuma mudança de Go emitido (NFR-21/23). Ver a doc de
	// moduleOutboxDatabaseName (sql_wiring.go) e de emitPolicyWireFunc abaixo.
	if prog != nil {
		outboxDBName = moduleOutboxDatabaseName(prog, module)
	}
	return runtimeAlias, needsEmitDispatcher, outboxDBName, nil
}

// emitPolicyDecl emite o subscriber Go de um único PolicyDecl: assinatura
// EXATA de runtime.Dispatcher/Outbox.Subscribe, type assertion pro tipo
// concreto do evento, extração de caller, e o corpo via lowering (ver a doc
// do arquivo).
func emitPolicyDecl(e *emit.Emitter, decl *ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl, ctxAlias, runtimeAlias string, typeToVar map[string]string, needsEmitDispatcher bool) error {
	evt, err := resolvePolicyEvent(e, tab, module, decl.On)
	if err != nil {
		return err
	}
	fmtAlias := e.Import("fmt")

	env := lower.New(model, tab, module)
	env.SeedPolicyExecute(decl.On)
	l := lower.NewLowerer(env, reg, runtimeAlias).WithEmitter(e)
	l.BindGoName("caller", "caller")
	if len(typeToVar) > 0 {
		// WithPerAggregateStore roteia "list T .../count T ..." para o
		// runtime.Collection[T] certo (H4, §22.4) — mesmo mecanismo que o
		// caminho 2PC de decl_usecase.go já usa para rotear "load" por
		// Aggregate; storeGoName fica "" (nunca usado: toda chamada real
		// passa por typeToVar, que emitPolicyCollectionVars já populou com
		// todo tipo referenciado por list/count neste arquivo).
		l.WithBuiltins(lower.NewBuiltinLowerer(runtimeAlias, "ctx", "").WithPerAggregateStore(func(typeName string) string {
			return typeToVar[typeName]
		}))
	}

	deliveryNote := "BestEffort (in-process, runtime.Dispatcher)"
	if policyIsAtLeastOnce(decl) {
		deliveryNote = "AtLeastOnce (via runtime.Outbox)"
	}
	e.Line("// %s é a Policy %s (§7): reage ao Event %s, entrega %s.", decl.Name, decl.Name, decl.On, deliveryNote)
	e.Line("// Assinatura igual a runtime.Dispatcher/Outbox.Subscribe — registrada DIRETO")
	e.Line("// como handler pelo wiring do módulo (Wire, abaixo).")

	sig := fmt.Sprintf("func %s(ctx %s.Context, ev %s.Event) error", decl.Name, ctxAlias, runtimeAlias)
	errMsg := fmt.Sprintf("policy %s: evento inesperado %%T (esperava %s)", decl.Name, evt.goPtrType)

	// lastIsReturn: se o ÚLTIMO statement de nível superior do execute já é
	// um "return" (ex. o corpo real do shop, "execute { return }" —
	// docs/examples/shop/shipping/policy.ds), a lowering desse ReturnStmt já
	// emite "return nil" (via StmtContext.SuccessReturn abaixo, ver a doc de
	// lower/stmt.go:returnStmt) — um "return nil" INCONDICIONAL depois do
	// bloco duplicaria a linha (código morto, "unreachable code" de go vet).
	// Heurística deliberadamente simples (não analisa terminação geral de Go,
	// ex. um MatchStmt cujos braços todos retornam): cobre exatamente a forma
	// real exercitada aqui; qualquer outra forma (corpo que não termina em
	// "return", como um "ensure"/dispatch solto) continua precisando do
	// "return nil" de fechamento, a mesma convenção de decl_usecase.go.
	lastIsReturn := false
	if decl.Execute != nil && len(decl.Execute.Stmts) > 0 {
		_, lastIsReturn = decl.Execute.Stmts[len(decl.Execute.Stmts)-1].(*ast.ReturnStmt)
	}

	var bodyErr error
	e.Block(sig, func() {
		e.Line("event, ok := ev.(%s)", evt.goPtrType)
		e.Block("if !ok", func() {
			e.Line("return %s.Errorf(%q, ev)", fmtAlias, errMsg)
		})
		e.Line("caller, _ := %s.CallerFrom(ctx)", runtimeAlias)
		e.Line("_ = caller")
		e.Line("_ = event")

		stmtCtx := lower.StmtContext{ZeroValues: []string{}, SuccessReturn: "return nil", CtxVar: "ctx"}
		sl := lower.NewStmtLowerer(l, e, stmtCtx).WithNotifyAdapters(adapters, "ctx")
		if needsEmitDispatcher {
			// Anexado mesmo quando ESTA Policy especificamente não usa
			// "emit" (needsEmitDispatcher é por ARQUIVO, não por decl, ver a
			// doc do arquivo) — inofensivo: um corpo sem "emit" nunca chama
			// emitStmt, então o caminho de dispatch nunca é exercitado para
			// ele; mais simples que rastrear "quais decls usam emit"
			// individualmente.
			sl = sl.WithEmitDispatch("policyDispatcher", "ctx")
		}
		if bodyErr = sl.Block(decl.Execute); bodyErr != nil {
			return
		}
		if !lastIsReturn {
			e.Line("return nil")
		}
	})
	if bodyErr != nil {
		return fmt.Errorf("codegen: Policy %s: %w", decl.Name, bodyErr)
	}
	return nil
}

// policyWireInfo é o resultado, já resolvido, de UMA Policy para
// emitPolicyWireFunc: o evento (policyEventInfo, com originModule) e, se
// esta Policy é cross-service via canal "queue" (prog.ChannelBetween(
// originModule, module), REQ-26.5), o *program.Channel e o nome do var de
// pacote que vai carregar seu runtime.ChannelTransport. channel == nil
// significa "caminho de sempre" (F1): Subscribe direto em d/Outbox(d).
type policyWireInfo struct {
	decl    *ast.PolicyDecl
	evt     policyEventInfo
	channel *program.Channel
	varName string
}

// resolvePolicyWireInfos resolve, para cada decl, se é cross-service via um
// canal "queue" da topologia (ver a doc de policyWireInfo) — UMA ÚNICA vez,
// reusado tanto para declarar os vars de canal no nível de pacote quanto
// para montar o corpo de Wire (emitPolicyWireFunc), evitando resolver
// prog.ChannelBetween duas vezes por Policy.
func resolvePolicyWireInfos(e *emit.Emitter, tab *symbols.SymbolTable, prog *program.Program, module string, decls []*ast.PolicyDecl) ([]policyWireInfo, error) {
	infos := make([]policyWireInfo, 0, len(decls))
	for _, decl := range decls {
		evt, err := resolvePolicyEvent(e, tab, module, decl.On)
		if err != nil {
			return nil, err
		}
		info := policyWireInfo{decl: decl, evt: evt}
		if prog != nil {
			if ch := prog.ChannelBetween(evt.originModule, module); ch != nil {
				switch channelViaKind(ch) {
				case "direct":
					// Caminho de sempre (F1) — sem mudança.
				case "queue":
					info.channel = ch
					info.varName = strings.ToLower(decl.Name[:1]) + decl.Name[1:] + "Channel"
				default:
					return nil, unsupportedChannelKindError(ch)
				}
			}
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// moduleNeedsDurableOutbox reporta se decls contém ao menos uma Policy
// AtLeastOnce cujo alvo é "d" (o caminho local de sempre) — a MESMA condição
// que emitPolicyWireFunc usa para decidir se emite o wiring de outbox
// durável (ver a doc dele). generateModuleFiles (codegen.go) chama isto para
// decidir moduleMarks.outboxDatabase, de forma que a decisão "este módulo
// precisa do wiring de outbox durável em main.go" bata exatamente com a de
// emitPolicyWireFunc — uma divergência deixaria generateCmdMainFile chamando
// WireOutboxStore/StartOutboxRelay/StartOutboxCleanup num pacote de módulo
// que nunca os gerou (erro de compilação), ou o oposto.
func moduleNeedsDurableOutbox(tab *symbols.SymbolTable, prog *program.Program, module string, decls []*ast.PolicyDecl) (bool, error) {
	infos, err := resolvePolicyWireInfos(emit.New("_"), tab, prog, module, decls)
	if err != nil {
		return false, err
	}
	for _, info := range infos {
		if info.channel == nil && policyIsAtLeastOnce(info.decl) {
			return true, nil
		}
	}
	return false, nil
}

// emitPolicyWireFunc emite "func Wire(d runtime.Dispatcher)": um
// d.Subscribe/o.Subscribe por Policy, conforme a garantia de entrega de cada
// uma (ver a doc do arquivo sobre Dispatcher vs. Outbox) — chamada por
// cmd/<service>/main.go na inicialização (E9.1/§design 3.11), mesmo
// papel/mesmo nome de emitUOWWireFunc (decl_usecase.go).
//
// --- Canais da topologia (F5, REQ-25.3, REQ-26.1/26.5, §design 3.11) ---
//
// resolvePolicyWireInfos decide, por Policy, se ela é cross-service via um
// canal "queue" da topologia. policyWireInfo.channel == nil (sem topologia,
// nenhum canal entre os módulos, ou um canal "direct") segue o caminho de
// SEMPRE — Subscribe direto em "d"/Outbox(d), exatamente o que F1 já
// gerava, SEM NENHUMA mudança. Um canal "queue" faz esta Policy construir
// seu PRÓPRIO runtime.ChannelTransport e assinar nele em vez de em
// "d"/Outbox(d) — "d" simplesmente fica sem uso para essa Policy específica
// (parâmetro não utilizado é válido em Go; só variável local não é).
//
// O var do transporte é declarado no nível de PACOTE (var %s
// runtime.ChannelTransport, ANTES de Wire) e só ATRIBUÍDO dentro de Wire
// ("%s = runtime.NewQueueChannel(...)", nunca ":=") — o MESMO padrão de
// "uow"/Wire em decl_usecase.go: uma var de pacote que só Wire escreve.
// Isso existe para permitir que um teste do MESMO pacote gerado (ex.
// policies_behavior_test.go, "package shipping") publique um evento
// DIRETAMENTE no transporte após chamar Wire e observe a Policy rodar de
// verdade — a alternativa (var só local dentro de Wire) tornaria essa
// entrega inteiramente inacessível de fora, sem forma de provar
// comportamento (só compilação).
//
// Cada canal "queue" ganha seu PRÓPRIO Outbox quando a Policy é
// AtLeastOnce (não pode reusar o "o" que embrulha "d" — são transportes
// diferentes); o var de canal é nomeado a partir do NOME da Policy
// (garantidamente único dentro do módulo, mesma convenção de sourceVar em
// decl_worker.go:emitContinuous).
//
// --- Outbox durável (task J2.5, REQ-42.5/42.7) ---
//
// outboxDBName ("" no caso comum — ver moduleOutboxDatabaseName,
// sql_wiring.go) é o Database real deste módulo, quando houver um. Só entra
// em jogo para uma Policy AtLeastOnce cujo alvo é "d" (o caminho local de
// sempre) — uma Policy cross-service via canal "queue" continua com seu
// PRÓPRIO Outbox em memória, inalterado (fora do orçamento desta task, ver a
// doc do arquivo). Quando outboxDBName != "" E ao menos uma Policy cai nesse
// caso, "o" (o Outbox local) é promovido de var LOCAL de Wire para var de
// PACOTE, tipo concreto *runtime.DurableOutbox (não a interface
// runtime.Outbox: StartOutboxRelay/StartOutboxCleanup, abaixo, chamam
// Start/Cleanup, que só o tipo concreto expõe) — populada por
// runtime.NewDurableOutbox em vez de runtime.NewOutbox(d), sobre uma store
// que só existe depois que o cmd/<service>/main.go gerado chama
// WireOutboxStore (nova função exportada, abaixo) com uma
// sqlruntime.OutboxStore real, ANTES de Wire. Sem outboxDBName (o caso
// comum, incl. o shipping do shop — Policy AtLeastOnce mas SEM Database
// próprio), nada disto é emitido: "o := runtime.NewOutbox(d)" continua var
// LOCAL de Wire, Go byte-idêntico ao de antes desta task (NFR-21/23).
func emitPolicyWireFunc(e *emit.Emitter, runtimeAlias string, decls []*ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, prog *program.Program, module string, reg *goname.VOOperatorRegistry, needsEmitDispatcher bool, outboxDBName string) error {
	pre, err := emitPolicyWirePreamble(e, runtimeAlias, decls, tab, prog, module, outboxDBName)
	if err != nil {
		return err
	}

	e.Line("// Wire registra cada Policy deste pacote no runtime.Dispatcher/Outbox (ou,")
	e.Line("// para uma Policy cross-service via canal \"queue\" da topologia, no seu")
	e.Line("// próprio runtime.ChannelTransport, var de pacote acima — REQ-26.5) —")
	e.Line("// chamada por cmd/<service>/main.go na inicialização (wiring in-memory,")
	e.Line("// §design 3.11).")

	var funcErr error
	e.Block(fmt.Sprintf("func Wire(d %s.Dispatcher)", runtimeAlias), func() {
		funcErr = emitPolicyWireBody(e, runtimeAlias, pre, model, tab, module, reg, needsEmitDispatcher)
	})
	if funcErr != nil {
		return funcErr
	}

	if pre.needsDurableOutbox {
		emitOutboxRelayAndCleanupStarters(e, runtimeAlias)
	}
	return nil
}

// emitCombinedWireFunc emite a "func Wire" ÚNICA de um módulo MISTO (UseCase E
// Policy, L1.1/REQ-52.1/52.2, §design 2.2): "func Wire(u runtime.UnitOfWork, d
// runtime.Dispatcher)" cujo corpo faz "uow = u" como PRIMEIRO statement (injeta
// a var de pacote dos UseCases, o que emitUOWWireFunc fazia no seu Wire próprio,
// suprimido no caso misto) e, em seguida, EXATAMENTE o mesmo corpo de
// emitPolicyWireFunc (emitPolicyWireBody: policyDispatcher, canais, outbox,
// o.Subscribe(...)). As declarações de pacote pré-Wire (vars de canal,
// outboxStore/WireOutboxStore/o) e os starters de relay/cleanup são idênticos
// ao caso só-Policy (emitPolicyWirePreamble/emitOutboxRelayAndCleanupStarters,
// reusados) — a ÚNICA diferença em relação a emitPolicyWireFunc é a assinatura
// (mais o param "u") e o "uow = u" prependido. Evita as duas "func Wire"
// (usecases.go + policies.go) que colidiriam no mesmo pacote Go.
func emitCombinedWireFunc(e *emit.Emitter, runtimeAlias string, decls []*ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, prog *program.Program, module string, reg *goname.VOOperatorRegistry, needsEmitDispatcher bool, outboxDBName string) error {
	pre, err := emitPolicyWirePreamble(e, runtimeAlias, decls, tab, prog, module, outboxDBName)
	if err != nil {
		return err
	}

	e.Line("// Wire injeta a unit of work dos UseCases (\"uow = u\") E registra cada Policy")
	e.Line("// deste pacote no runtime.Dispatcher/Outbox — um único ponto de entrada")
	e.Line("// combinado para um módulo que declara UseCase E Policy (L1.1, REQ-52), sem")
	e.Line("// duas \"func Wire\" colidindo no mesmo pacote. Chamada por cmd/<service>/")
	e.Line("// main.go na inicialização (wiring in-memory, §design 3.11).")

	var funcErr error
	e.Block(fmt.Sprintf("func Wire(u %s.UnitOfWork, d %s.Dispatcher)", runtimeAlias, runtimeAlias), func() {
		e.Line("uow = u")
		funcErr = emitPolicyWireBody(e, runtimeAlias, pre, model, tab, module, reg, needsEmitDispatcher)
	})
	if funcErr != nil {
		return funcErr
	}

	if pre.needsDurableOutbox {
		emitOutboxRelayAndCleanupStarters(e, runtimeAlias)
	}
	return nil
}

// policyWirePreamble carrega o resultado de emitPolicyWirePreamble: os
// policyWireInfo já resolvidos (cada Policy + seu canal, se cross-service) e a
// decisão de outbox durável — reusados por emitPolicyWireFunc (só-Policy) e por
// emitCombinedWireFunc (módulo misto), que compartilham corpo de Wire idêntico
// (emitPolicyWireBody) e só diferem na assinatura da função.
type policyWirePreamble struct {
	infos              []policyWireInfo
	durableInfos       []policyWireInfo
	needsDurableOutbox bool
}

// emitPolicyWirePreamble emite as declarações de PACOTE que precisam vir ANTES
// da "func Wire" (vars de runtime.ChannelTransport das Policy cross-service, e,
// quando há outbox durável, outboxStore/WireOutboxStore/o) e devolve os infos
// resolvidos para o corpo de Wire. Extraído de emitPolicyWireFunc (L1.1) sem
// nenhuma mudança na ordem/no conteúdo emitido, para que o Wire combinado do
// módulo misto reuse EXATAMENTE estas declarações.
func emitPolicyWirePreamble(e *emit.Emitter, runtimeAlias string, decls []*ast.PolicyDecl, tab *symbols.SymbolTable, prog *program.Program, module string, outboxDBName string) (policyWirePreamble, error) {
	infos, err := resolvePolicyWireInfos(e, tab, prog, module, decls)
	if err != nil {
		return policyWirePreamble{}, err
	}

	// durableInfos: as Policy AtLeastOnce com alvo "d" — o subconjunto que
	// disputa o outbox durável quando outboxDBName != "" (ver a doc acima).
	// Calculado uma vez, ANTES do bloco de Wire, porque a decisão de emitir
	// os vars de pacote (outboxStore/WireOutboxStore/o) precisa vir ANTES do
	// "func Wire" que os usa.
	var durableInfos []policyWireInfo
	if outboxDBName != "" {
		for _, info := range infos {
			if info.channel == nil && policyIsAtLeastOnce(info.decl) {
				durableInfos = append(durableInfos, info)
			}
		}
	}
	needsDurableOutbox := len(durableInfos) > 0

	for _, info := range infos {
		if info.channel == nil {
			continue
		}
		e.Line("// %s é o runtime.ChannelTransport que %s consome (canal %s -> %s via", info.varName, info.decl.Name, info.channel.From, info.channel.To)
		e.Line("// queue, REQ-26.5) — só Wire (abaixo) escreve nele; var de pacote para um")
		e.Line("// teste do mesmo pacote poder publicar direto e observar a Policy rodar.")
		e.Line("var %s %s.ChannelTransport", info.varName, runtimeAlias)
	}

	if needsDurableOutbox {
		e.Line("// outboxStore é a store SQL durável do outbox deste módulo (Database %s,", outboxDBName)
		e.Line("// Marco J, REQ-42.5) — populada por WireOutboxStore (abaixo), chamada por")
		e.Line("// cmd/<service>/main.go ANTES de Wire, porque este módulo tem uma Database")
		e.Line("// real.")
		e.Line("var outboxStore %s.OutboxStore", runtimeAlias)
		e.Line("")
		e.Line("// WireOutboxStore conecta este módulo à store SQL durável do outbox — ver a")
		e.Line("// doc de outboxStore acima. Chamada por cmd/<service>/main.go antes de Wire.")
		e.Block(fmt.Sprintf("func WireOutboxStore(store %s.OutboxStore)", runtimeAlias), func() {
			e.Line("outboxStore = store")
		})
		e.Line("")
		e.Line("// o é o runtime.Outbox de entrega AtLeastOnce local (\"d\") deste módulo —")
		e.Line("// DurableOutbox de verdade (Marco J) desde que WireOutboxStore já tenha")
		e.Line("// rodado; var de pacote para StartOutboxRelay/StartOutboxCleanup (abaixo)")
		e.Line("// alcançarem a MESMA instância que Wire constrói.")
		e.Line("var o *%s.DurableOutbox", runtimeAlias)
		e.Line("")
	}

	return policyWirePreamble{infos: infos, durableInfos: durableInfos, needsDurableOutbox: needsDurableOutbox}, nil
}

// emitPolicyWireBody emite o CORPO de "func Wire" (posicionado dentro do bloco
// da função pelo chamador): a 1ª linha "policyDispatcher = d" quando algum
// corpo usa emit, e o d.Subscribe/o.Subscribe/<canal>Outbox.Subscribe por
// Policy. Extraído de emitPolicyWireFunc (L1.1) sem mudança de conteúdo, para
// ser reusado idêntico pela Wire combinada do módulo misto (que só prepende
// "uow = u" antes desta chamada).
func emitPolicyWireBody(e *emit.Emitter, runtimeAlias string, pre policyWirePreamble, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, needsEmitDispatcher bool) error {
	if needsEmitDispatcher {
		// 1ª linha, SEMPRE (H4, §22.4, ver a doc do arquivo): nunca
		// condicionada a canal/Delivery por Policy, ao contrário do
		// resto deste corpo — "d" é sempre o runtime.Dispatcher que o
		// chamador (cmd/<service>/main.go) já constrói para este
		// parâmetro, então sempre disponível aqui.
		e.Line("policyDispatcher = d")
	}
	outboxDeclared := false
	for _, info := range pre.infos {
		decl := info.decl
		target := "d"
		if info.channel != nil {
			candidates := []channelEventCandidate{{evtDecl: info.evt.decl, goPtrType: info.evt.goPtrType}}
			if err := emitChannelTransportVar(e, info.varName, "=", info.channel, candidates, model, tab, module, reg, runtimeAlias, false, false); err != nil {
				return fmt.Errorf("Policy %s: canal %s -> %s: %w", decl.Name, info.channel.From, info.channel.To, err)
			}
			target = info.varName
		}

		if policyIsAtLeastOnce(decl) {
			if target == "d" {
				if !outboxDeclared {
					if pre.needsDurableOutbox {
						emitDurableOutboxConstruction(e, runtimeAlias, pre.durableInfos)
					} else {
						e.Line("o := %s.NewOutbox(d)", runtimeAlias)
					}
					outboxDeclared = true
				}
				e.Line("o.Subscribe(%q, %s)", decl.On, decl.Name)
			} else {
				outboxVar := target + "Outbox"
				e.Line("%s := %s.NewOutbox(%s)", outboxVar, runtimeAlias, target)
				e.Line("%s.Subscribe(%q, %s)", outboxVar, decl.On, decl.Name)
			}
		} else {
			e.Line("%s.Subscribe(%q, %s)", target, decl.On, decl.Name)
		}
	}
	return nil
}

// emitDurableOutboxConstruction emite "o = runtime.NewDurableOutbox(outboxStore,
// map[string]runtime.EventFactory{...})" — o registry inline monta só os
// tipos de evento que ALGUMA Policy AtLeastOnce local ("d") deste módulo
// consome (durableInfos), nunca EventRegistry() do módulo (que só cobre
// Event/PublicEvent DECLARADOS por ele — uma Policy cross-módulo como o
// shipping do shop, reagindo a um PublicEvent de Orders, não teria a entrada
// ali). Atribuição "=", não ":=": "o" já foi declarado de pacote acima (ver
// a doc de emitPolicyWireFunc).
func emitDurableOutboxConstruction(e *emit.Emitter, runtimeAlias string, durableInfos []policyWireInfo) {
	e.Line("o = %s.NewDurableOutbox(outboxStore, map[string]%s.EventFactory{", runtimeAlias, runtimeAlias)
	for _, info := range durableInfos {
		ctor := "&" + strings.TrimPrefix(info.evt.goPtrType, "*") + "{}"
		e.Line("%q: func() %s.Event { return %s },", info.decl.On, runtimeAlias, ctor)
	}
	e.Line("})")
}

// emitOutboxRelayAndCleanupStarters emite StartOutboxRelay(ctx)/
// StartOutboxCleanup(ctx) (task J2.5, REQ-42.5/42.7) — só chamado quando
// needsDurableOutbox (ver a doc de emitPolicyWireFunc): nomes próprios, ao
// lado de StartWorkers/StartIdempotencyCleanup (mesma razão de não reusar
// "Wire", ver a doc do arquivo), chamados por cmd/<service>/main.go na
// inicialização deste módulo.
func emitOutboxRelayAndCleanupStarters(e *emit.Emitter, runtimeAlias string) {
	ctxAlias := e.Import("context")
	timeAlias := e.Import("time")
	slogAlias := e.Import("log/slog")

	e.Line("")
	e.Line("// StartOutboxRelay roda o relay do outbox durável deste módulo (Marco J,")
	e.Line("// REQ-42.2/42.3) — gerado automaticamente porque este módulo tem uma Policy")
	e.Line("// AtLeastOnce e uma Database real; chamado por cmd/<service>/main.go na")
	e.Line("// inicialização, ao lado de StartWorkers/Wire. Roda até ctx ser cancelado.")
	e.Line("//")
	e.Line("// A verificação \"o == nil\" é defensiva (revisão da PR #22): o wiring gerado")
	e.Line("// SEMPRE atribui \"o\" dentro de Wire antes de chamar isto (cmd/<service>/")
	e.Line("// main.go, ver emitOutboxDatabaseWiring), mas StartOutboxRelay é uma função")
	e.Line("// EXPORTADA — um chamador externo (ex. um teste) que a invoque sem ter")
	e.Line("// rodado Wire antes travaria a goroutine inteira num nil pointer panic.")
	e.Block(fmt.Sprintf("func StartOutboxRelay(ctx %s.Context)", ctxAlias), func() {
		e.Block("if o == nil", func() {
			e.Line("%s.Error(%q)", slogAlias, "outbox: DurableOutbox não inicializado (Wire ainda não rodou) — relay não iniciado")
			e.Line("return")
		})
		e.Line("o.Start(ctx)")
	})

	e.Line("")
	e.Line("// StartOutboxCleanup roda o worker de limpeza de linhas do outbox já")
	e.Line("// entregues além da janela de retenção (REQ-42.7, análogo a")
	e.Line("// StartIdempotencyCleanup, usecase_idempotency.go) — sem isto a tabela")
	e.Line("// outbox cresceria sem limite. Roda até ctx ser cancelado. Mesma verificação")
	e.Line("// defensiva \"o == nil\" de StartOutboxRelay, acima, e pela mesma razão.")
	e.Block(fmt.Sprintf("func StartOutboxCleanup(ctx %s.Context)", ctxAlias), func() {
		e.Block("if o == nil", func() {
			e.Line("%s.Error(%q)", slogAlias, "outbox: DurableOutbox não inicializado (Wire ainda não rodou) — limpeza não iniciada")
			e.Line("return")
		})
		e.Line("ticker := %s.NewTicker(%s)", timeAlias, outboxCleanupTickInterval)
		e.Line("defer ticker.Stop()")
		e.Block("for", func() {
			e.Block("select", func() {
				e.Line("case <-ctx.Done():")
				e.Line("return")
				e.Line("case <-ticker.C:")
			})
			e.Block(fmt.Sprintf("if _, err := o.Cleanup(ctx, %s); err != nil", outboxCleanupRetention), func() {
				e.Line("%s.Error(%q, %q, err)", slogAlias, "outbox: falha na limpeza de linhas entregues", "error")
			})
		})
	})
}

// outboxCleanupTickInterval/outboxCleanupRetention (REQ-42.7): mesmo espírito
// de idempotencyCleanupInterval (usecase_idempotency.go) — nenhuma
// declaração .ds controla isto hoje, sem um exemplo real que peça por uma
// janela configurável. Uma hora entre varreduras é frequente o bastante para
// a tabela outbox não crescer muito entre limpezas, sem martelar o banco; 7
// dias de retenção dá folga generosa para qualquer investigação/auditoria
// pós-entrega antes da linha sumir.
const (
	outboxCleanupTickInterval = "time.Hour"
	outboxCleanupRetention    = "7 * 24 * time.Hour"
)
