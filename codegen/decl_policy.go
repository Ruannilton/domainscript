package codegen

import (
	"fmt"
	"path"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
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
// Builtins (load/list/count/exists, E5.3) NÃO são conectados aqui: nenhum
// corpo real de Policy os exercita (o shop só tem "return"), e a forma certa
// de um load dentro de uma Policy é uma decisão de design maior deixada para
// quando um exemplo real precisar dela (mesmo espírito da nota de escopo de
// decl_query.go sobre where/orderBy/skip/take). Sem WithBuiltins, um
// load/list/count no corpo de uma Policy falha com o erro claro já existente
// de E5.1/E5.2 ("não suportado"), nunca Go incorreto.
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

// policyEventInfo é a forma já resolvida do Event/PublicEvent de
// PolicyDecl.On: o *ast.EventDecl e o tipo Go de referência já qualificado,
// SEMPRE um ponteiro (ver a doc do arquivo) — "*OrderPlaced" (mesmo pacote,
// Event privado) ou "*contracts.OrderPlaced" (import próprio, pacote
// compartilhado §design 3.4, PublicEvent).
type policyEventInfo struct {
	decl      *ast.EventDecl
	goPtrType string
}

// resolvePolicyEvent resolve PolicyDecl.On a um Event/PublicEvent conhecido —
// mesmo padrão de resolveApplyEvent (decl_aggregate_load.go): REQ-9
// (resolver.resolveOn, resolver/resolver.go) já validou que o nome existe em
// algum módulo do programa antes deste emissor rodar (Lookup local, fallback
// Find cross-module — a MESMA ordem de resolver.resolveOn); aqui só se
// reconsulta a SymbolTable para decidir o pacote Go de referência. Um evento
// não resolvido ou que não seja EventDecl é bug de geração (REQ-9 já deveria
// ter barrado), não caminho normal.
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
		return policyEventInfo{decl: ed, goPtrType: "*" + ed.Name}, nil
	}
	contractsAlias := e.Import(path.Join(domainModuleRoot, "contracts"))
	return policyEventInfo{decl: ed, goPtrType: "*" + goname.QualifiedRef(contractsAlias, ed.Name)}, nil
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
// a F4 (nenhum notify/call reconhecido no corpo).
func EmitPolicy(pkg string, decl *ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl) ([]byte, error) {
	return EmitPolicies(pkg, []*ast.PolicyDecl{decl}, model, tab, module, reg, adapters)
}

// EmitPolicies gera o Go de várias PolicyDecl num único arquivo
// (policies.go), com "func Wire(d runtime.Dispatcher)" compartilhado (ver a
// doc do arquivo) — como um módulo real pode declarar mais de uma Policy.
// adapters (F4) é repassado a cada corpo via StmtLowerer.WithNotifyAdapters
// (ver emitPolicyDecl) — habilita "DepositNotification(...)" dentro de
// execute a reconhecer notify (Mode async)/call (Mode sync) do Adapter
// parceiro (§9.1/9.3, REQ-25.3).
func EmitPolicies(pkg string, decls []*ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl) ([]byte, error) {
	e := emit.New(pkg)
	ctxAlias := e.Import("context")
	runtimeAlias := e.Import(RuntimeImportPath)

	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		if err := emitPolicyDecl(e, decl, model, tab, module, reg, adapters, ctxAlias, runtimeAlias); err != nil {
			return nil, fmt.Errorf("codegen: Policy %s: %w", decl.Name, err)
		}
	}

	e.Line("")
	emitPolicyWireFunc(e, runtimeAlias, decls)

	return e.Bytes()
}

// emitPolicyDecl emite o subscriber Go de um único PolicyDecl: assinatura
// EXATA de runtime.Dispatcher/Outbox.Subscribe, type assertion pro tipo
// concreto do evento, extração de caller, e o corpo via lowering (ver a doc
// do arquivo).
func emitPolicyDecl(e *emit.Emitter, decl *ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, adapters map[string]*ast.AdapterDecl, ctxAlias, runtimeAlias string) error {
	evt, err := resolvePolicyEvent(e, tab, module, decl.On)
	if err != nil {
		return err
	}
	fmtAlias := e.Import("fmt")

	env := lower.New(model, tab, module)
	env.SeedPolicyExecute(decl.On)
	l := lower.NewLowerer(env, reg, runtimeAlias)
	l.BindGoName("caller", "caller")

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

		stmtCtx := lower.StmtContext{ZeroValues: []string{}, SuccessReturn: "return nil"}
		sl := lower.NewStmtLowerer(l, e, stmtCtx).WithNotifyAdapters(adapters, "ctx")
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

// emitPolicyWireFunc emite "func Wire(d runtime.Dispatcher)": um
// d.Subscribe/o.Subscribe por Policy, conforme a garantia de entrega de cada
// uma (ver a doc do arquivo sobre Dispatcher vs. Outbox) — chamada por
// cmd/<service>/main.go na inicialização (E9.1/§design 3.11), mesmo
// papel/mesmo nome de emitUOWWireFunc (decl_usecase.go).
func emitPolicyWireFunc(e *emit.Emitter, runtimeAlias string, decls []*ast.PolicyDecl) {
	e.Line("// Wire registra cada Policy deste pacote no runtime.Dispatcher/Outbox —")
	e.Line("// chamada por cmd/<service>/main.go na inicialização (wiring in-memory,")
	e.Line("// §design 3.11).")

	var outboxDeclared bool
	e.Block(fmt.Sprintf("func Wire(d %s.Dispatcher)", runtimeAlias), func() {
		for _, decl := range decls {
			target := "d"
			if policyIsAtLeastOnce(decl) {
				if !outboxDeclared {
					e.Line("o := %s.NewOutbox(d)", runtimeAlias)
					outboxDeclared = true
				}
				target = "o"
			}
			e.Line("%s.Subscribe(%q, %s)", target, decl.On, decl.Name)
		}
	})
}
