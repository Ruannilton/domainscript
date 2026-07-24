package codegen

import (
	"fmt"
	"strings"
	"unicode"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// decl_aggregate.go emite o Go de um AggregateDecl (E6.1, REQ-19.1/2/3/6,
// §design 3.7): o struct de state, o tipo do aggregate (id+version+state),
// cada Handle como método público e cada Apply como método privado.
//
// Decisão de design — self/state/id (documentada aqui, única vez, ver o
// prompt da task E6.1 §"self/state/id"). sema/rules_typecheck.go tipa "self"
// e "state" de forma IDÊNTICA dentro de um Handle: o mesmo shape — TODOS os
// campos declarados em AggregateDecl.State, incluindo "id". Este emissor seue
// exatamente essa convenção: o struct de state (ex. walletState) inclui TODOS
// os campos de State, incluindo "id" — self.id/state.id SEMPRE viram
// "<receiver>.state.Id", nunca um campo "id" direto no nível do Aggregate.
// O campo "id" no nível do Aggregate (Wallet.id) é só um ESPELHO para uso de
// infraestrutura (chave de stream, endereçamento — LoadWallet/tx.Append,
// E6.2/E7.2) que precisa da identidade do aggregate sem depender do state
// completo já ter sido carregado; corpos de Handle/Apply nunca o leem
// diretamente. A sincronização de verdade desse espelho (no Apply que cria o
// aggregate, ex. Apply WalletCreated) é responsabilidade de E6.2 — esta task
// só declara o campo.
//
// Convenção de binding (self/state/caller/event) — precisa bater com o
// Lowerer usado por cada corpo:
//   - Handle/Access: self→"<receiver>.state", state→"<receiver>.state" (texto
//     Go idêntico para os dois, refletindo a decisão acima), caller→"caller".
//   - Apply: state→ o PARÂMETRO LOCAL "state" (não "<receiver>.state") — a
//     mesma convenção já validada por
//     codegen/lower/stmt_test.go:TestStmt_Apply_DepositPerformed_RealWallet
//     (que lowereiza sobre "func applyDepositPerformed(state Wallet, ev
//     DepositPerformed)"). Como esse "state" precisa MUTAR o state real do
//     aggregate (ex. "state.Balance = ..."), o método de verdade declara, na
//     1ª linha do corpo, "state := &<receiver>.state" — um PONTEIRO local
//     chamado "state" (não um parâmetro da assinatura, que só leva "ev"): o
//     texto Go "state.Campo = valor" lowerizado por StmtLowerer resolve
//     exatamente a esse ponteiro (Go desreferencia campos automaticamente),
//     então a mutação reflete em "<receiver>.state" sem precisar rebind nada
//     no Lowerer. event→"ev".

// EmitAggregate gera o Go de um AggregateDecl (§design 3.7): o struct de
// estado, o tipo do aggregate (id+version+state), cada Handle como método
// público e cada Apply como método privado. model+tab+module constroem o
// lower.TypeEnv de cada corpo; reg deve já ter TODOS os ValueObjectDecl do
// módulo registrados (goname.VOOperatorRegistry.Register), para que o
// dispatch de operador (§4.2) funcione nos corpos de Handle/Apply.
func EmitAggregate(pkg string, decl *ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	e := emit.New(pkg)
	runtimeAlias := e.Import(RuntimeImportPath)

	stateName := aggregateStateStructName(decl.Name)
	if err := emitAggregateStateStruct(e, stateName, decl); err != nil {
		return nil, fmt.Errorf("codegen: Aggregate %s: %w", decl.Name, err)
	}

	idGoType, err := aggregateIDGoType(decl)
	if err != nil {
		return nil, fmt.Errorf("codegen: Aggregate %s: %w", decl.Name, err)
	}
	e.Line("")
	emitAggregateStruct(e, decl.Name, stateName, idGoType)

	receiver := aggregateReceiver(decl.Name)

	for _, h := range decl.Handlers {
		e.Line("")
		if err := emitHandle(e, decl, h, model, tab, module, runtimeAlias, receiver, reg); err != nil {
			return nil, err
		}
	}
	for _, a := range decl.Appliers {
		e.Line("")
		if err := emitApply(e, decl, a, model, tab, module, runtimeAlias, receiver, reg); err != nil {
			return nil, err
		}
	}

	return e.Bytes()
}

// aggregateStateStructName deriva o nome do struct de state (não exportado) a
// partir do nome do Aggregate: "Wallet" -> "walletState".
func aggregateStateStructName(aggName string) string {
	if aggName == "" {
		return "state"
	}
	r := []rune(aggName)
	r[0] = unicode.ToLower(r[0])
	return string(r) + "State"
}

// aggregateReceiver deriva o nome do receptor Go de um método do Aggregate:
// a 1ª letra do nome, minúscula (ex. "Wallet" -> "w"), escapada de keyword Go.
func aggregateReceiver(aggName string) string {
	if aggName == "" {
		return "a"
	}
	r := []rune(aggName)
	return goname.Ident(strings.ToLower(string(r[0])))
}

// aggregateIDGoType devolve o tipo Go do campo "id" declarado em
// AggregateDecl.State — o tipo espelhado no campo "id" de nível do Aggregate
// (ver doc do arquivo). Um Aggregate sem campo "id" (não exercitado pelo
// wallet) cai para "string", mesma convenção-fallback de REQ-20.5/§design 3.8
// para um campo "ref Aggregate" sem "id" declarado.
func aggregateIDGoType(decl *ast.AggregateDecl) (string, error) {
	for _, f := range decl.State {
		if f != nil && f.Name == "id" {
			goType, err := goname.GoFieldType(f.Type)
			if err != nil {
				return "", fmt.Errorf("campo id: %w", err)
			}
			return goType, nil
		}
	}
	return "string", nil
}

// aggregateStateFieldInfo é a forma Go já resolvida de um campo de state: o
// tipo Go do campo e o nome exportado do campo do struct (mesmo padrão de
// voFieldInfo em decl_value.go/eventFieldInfo em decl_event.go).
type aggregateStateFieldInfo struct {
	field      *ast.Field
	goType     string
	exportName string
}

// emitAggregateStateStruct emite "type <stateName> struct { ... }" com os
// campos de decl.State, na ordem declarada (mesmo padrão de
// emitValueObjectComposite/emitEventDecl: campo exportado + tag json com o
// nome original). Resolve todos os tipos ANTES de abrir o bloco (o mesmo
// padrão de emitEventDecl) para poder propagar um erro de campo sem precisar
// de um canal de erro through o corpo de e.Block.
func emitAggregateStateStruct(e *emit.Emitter, stateName string, decl *ast.AggregateDecl) error {
	infos := make([]aggregateStateFieldInfo, 0, len(decl.State))
	for _, f := range decl.State {
		if f == nil {
			continue
		}
		goType, err := goname.GoFieldType(f.Type)
		if err != nil {
			return fmt.Errorf("campo %s: %w", f.Name, err)
		}
		infos = append(infos, aggregateStateFieldInfo{field: f, goType: goType, exportName: goname.ExportField(f.Name)})
	}

	e.Line("// %s é o struct dos campos declarados no bloco state de %s (§4.5).", stateName, decl.Name)
	e.Line("// Inclui TODOS os campos, incluindo \"id\" — self/state dentro de Handle/Apply")
	e.Line("// tipam self e state de forma idêntica (sema/rules_typecheck.go), e este emissor")
	e.Line("// reflete isso: state.Id é o único caminho de leitura, mesmo dentro do Aggregate.")
	e.Block(fmt.Sprintf("type %s struct", stateName), func() {
		for _, fi := range infos {
			e.Line("%s %s %s", fi.exportName, fi.goType, goname.JSONTag(fi.field.Name))
		}
	})
	return nil
}

// emitAggregateStruct emite "type <Name> struct { id <idGoType>; version int;
// state <stateName> }" (§design 3.7): id é um ESPELHO de state.Id para uso de
// infraestrutura (ver doc do arquivo); version é o controle de concorrência
// (não populado nesta task — E6.2); state guarda os campos de negócio.
func emitAggregateStruct(e *emit.Emitter, name, stateName, idGoType string) {
	e.Line("// %s é o Aggregate %s (§4.5): a fronteira de consistência.", name, name)
	e.Block(fmt.Sprintf("type %s struct", name), func() {
		e.Line("// id espelha state.Id (ver doc do pacote): só para infraestrutura")
		e.Line("// (chave de stream/endereçamento). Corpos de Handle/Apply NUNCA o leem")
		e.Line("// diretamente — sempre via state.Id. Sincronizado em E6.2.")
		e.Line("id %s", idGoType)
		e.Line("version int")
		e.Line("state %s", stateName)
	})
}

// findAccessRule acha, em decl.Access, a regra cujo Name casa com handleName
// (§4.5: toda entrada de access nomeia o Handle que protege).
func findAccessRule(decl *ast.AggregateDecl, handleName string) *ast.AccessRule {
	for _, a := range decl.Access {
		if a != nil && a.Name == handleName {
			return a
		}
	}
	return nil
}

// handleParamList traduz os parâmetros declarados de um Handle para a forma
// Go "nome tipo" (mesmo padrão de emitValueObjectComposite), na ordem
// declarada.
func handleParamList(params []*ast.Field) ([]string, error) {
	out := make([]string, 0, len(params))
	for _, p := range params {
		if p == nil {
			continue
		}
		goType, err := goname.GoFieldType(p.Type)
		if err != nil {
			return nil, fmt.Errorf("parâmetro %s: %w", p.Name, err)
		}
		out = append(out, fmt.Sprintf("%s %s", goname.Ident(p.Name), goType))
	}
	return out, nil
}

// emitHandle gera o método público de um Handle (§design 3.7, REQ-19.2/6):
// checa o access correspondente, executa o corpo e devolve os eventos
// emitidos mais um error de negócio.
func emitHandle(e *emit.Emitter, decl *ast.AggregateDecl, h *ast.HandleDecl, model *types.Model, tab *symbols.SymbolTable, module, runtimeAlias, receiver string, reg *goname.VOOperatorRegistry) error {
	access := findAccessRule(decl, h.Name)
	if access == nil {
		return fmt.Errorf("codegen: Aggregate %s: Handle %s sem entrada correspondente em access — bug de geração (access fechado-por-padrão já deveria ter sido garantido pelo front-end, REQ-5/REQ-19.6)", decl.Name, h.Name)
	}

	env := lower.New(model, tab, module)
	env.SeedHandle(decl.Name, h.Params)
	l := lower.NewLowerer(env, reg, runtimeAlias).WithEmitter(e)
	l.BindGoName("self", receiver+".state")
	l.BindGoName("state", receiver+".state")
	l.BindGoName("caller", "caller")

	condGo, err := lowerAccessCondition(l, env, access.Condition)
	if err != nil {
		return fmt.Errorf("codegen: Aggregate %s: Handle %s: access: %w", decl.Name, h.Name, err)
	}

	paramStrs, err := handleParamList(h.Params)
	if err != nil {
		return fmt.Errorf("codegen: Aggregate %s: Handle %s: %w", decl.Name, h.Name, err)
	}
	allParams := append([]string{fmt.Sprintf("caller %s.Caller", runtimeAlias)}, paramStrs...)
	sig := fmt.Sprintf("func (%s *%s) %s(%s) ([]%s.Event, error)", receiver, decl.Name, h.Name, strings.Join(allParams, ", "), runtimeAlias)

	e.Line("// %s é o Handle %s do Aggregate %s (§4.5): checa access, executa o corpo", h.Name, h.Name, decl.Name)
	e.Line("// e devolve os eventos emitidos.")

	var bodyErr error
	e.Block(sig, func() {
		e.Block("if !("+condGo+")", func() {
			e.Line("return nil, %s.ErrForbidden", runtimeAlias)
		})
		e.Line("var events []%s.Event", runtimeAlias)

		ctx := lower.StmtContext{ZeroValues: []string{"nil"}, SuccessReturn: "return events, nil"}
		sl := lower.NewStmtLowerer(l, e, ctx)
		if err := sl.Block(h.Body); err != nil {
			bodyErr = fmt.Errorf("codegen: Aggregate %s: Handle %s: %w", decl.Name, h.Name, err)
			return
		}
		e.Line("return events, nil")
	})
	return bodyErr
}

// emitApply gera o método privado de um Apply (§design 3.7, REQ-19.3): muta o
// state do Aggregate a partir do event. Infalível por construção (o Handle
// que emitiu já validou antes de emitir) — StmtContext{Panics: true}, a
// mesma decisão de E5.2.
func emitApply(e *emit.Emitter, decl *ast.AggregateDecl, a *ast.ApplyDecl, model *types.Model, tab *symbols.SymbolTable, module, runtimeAlias, receiver string, reg *goname.VOOperatorRegistry) error {
	env := lower.New(model, tab, module)
	env.SeedApply(decl.Name, a.Event)
	l := lower.NewLowerer(env, reg, runtimeAlias)
	l.BindGoName("event", "ev")
	// "state" NÃO é rebindado aqui: SeedApply já vincula "state" no TypeEnv
	// ao tipo do Aggregate, e Lowerer.ident (expr.go) resolve um Ident
	// vinculado no TypeEnv para goname.Ident(nome) = "state" (nenhum override
	// necessário) — o texto Go final "state" refere-se ao ponteiro local
	// declarado abaixo, não a um parâmetro da assinatura (ver doc do arquivo
	// e TestStmt_Apply_DepositPerformed_RealWallet, cuja convenção validada
	// esta função reproduz).

	// BuiltinLowerer: habilita as built-ins de FUNÇÃO (now()/uuid()/
	// random(...)/random_str(...)) dentro de um corpo de Apply (ISSUE-12 item
	// 2, L1.3c). emitApply era o ÚNICO emissor de corpo executável que não
	// anexava um BuiltinLowerer — emitUseCasesBytes/emitPolicyExecute/Saga/
	// Query sempre anexam. Sem isto, "state.createdAt = CreatedAt(now())"
	// (docs/examples/pizzeria/kitchen/domain.ds:104) falha com "CallExpr sobre
	// \"now\" não é construção de VO/Event/Command conhecida".
	//
	// A sutileza do ctx: entre as quatro built-ins, só now() usa ctxGoName —
	// BuiltinLowerer.CallFunc emite "runtime.Now(<ctxGoName>)". Os demais
	// emissores passam "ctx" porque UseCase/Policy/Saga/Query sempre têm um
	// parâmetro "ctx context.Context" em escopo; um Apply NÃO tem (sua
	// assinatura é "func (r *T) applyEvent(ev E)", sem ctx). runtime.Now
	// (rtsrc/util.go.txt) ignora o ctx hoje, então "context.Background()" é
	// funcionalmente idêntico a um ctx real. Só importamos "context" e
	// passamos "context.Background()" QUANDO o corpo de fato chama now() —
	// senão ctxGoName fica "" e "context" NÃO é importado, evitando o erro de
	// import-não-usado (emit.TestEmitterBytesFailsOnUnusedImport) e
	// preservando byte-identidade para todo Apply sem built-in (o caso comum,
	// wallet/shop). storeGoName é "" (mesmo padrão de decl_saga.go): um Apply
	// é infalível por construção — nunca faz load/list/count/store/delete.
	ctxGoName := ""
	if blockCallsFunc(a.Body, "now") {
		ctxGoName = e.Import("context") + ".Background()"
	}
	l.WithBuiltins(lower.NewBuiltinLowerer(runtimeAlias, ctxGoName, ""))

	methodName := "apply" + a.Event
	sig := fmt.Sprintf("func (%s *%s) %s(ev %s)", receiver, decl.Name, methodName, a.Event)

	e.Line("// %s aplica o evento %s ao state do Aggregate %s (§4.5): infalível", methodName, a.Event, decl.Name)
	e.Line("// por construção (o Handle que emitiu já validou antes de emitir).")

	var bodyErr error
	e.Block(sig, func() {
		// "state := &receiver.state" só é declarado quando o corpo de fato usa
		// "state" — um Apply de um evento puramente de MARCAÇÃO/auditoria (ex.
		// "ContentRemoved" do exemplo G1a, cujo Apply não muta campo algum: o
		// arquivo já foi removido da FileStorage por "delete file", nada no
		// state precisa mudar) tem um corpo VAZIO ou que não referencia
		// "state" nenhuma vez — "declared and not used" seria erro de
		// compilação Go se a declaração fosse incondicional (gap descoberto
		// por G1a: nenhuma fixture anterior tinha um Apply que não mutasse
		// state).
		if blockReferencesIdent(a.Body, "state") {
			e.Line("state := &%s.state", receiver)
		}

		ctx := lower.StmtContext{Panics: true}
		sl := lower.NewStmtLowerer(l, e, ctx)
		if err := sl.Block(a.Body); err != nil {
			bodyErr = fmt.Errorf("codegen: Aggregate %s: Apply %s: %w", decl.Name, a.Event, err)
		}
	})
	return bodyErr
}

// blockReferencesIdent reporta se b contém, em qualquer profundidade, um
// *ast.Ident de nome exatamente name — usado por emitApply para decidir se
// precisa declarar o ponteiro local "state" (ver a doc ali).
func blockReferencesIdent(b *ast.Block, name string) bool {
	found := false
	astutil.ForEachExprInBlock(b, func(e ast.Expr) {
		if astutil.IsIdent(e, name) {
			found = true
		}
	})
	return found
}

// blockCallsFunc reporta se b contém, em qualquer profundidade, uma chamada
// de FUNÇÃO por nome nu — um *ast.CallExpr cujo Fn é um *ast.Ident de nome
// exatamente name (a MESMA forma que Lowerer.call/BuiltinLowerer.CallFunc
// reconhecem como built-in de função, expr.go:388/builtins.go:279). Usado por
// emitApply (L1.3c) para detectar now() especificamente: é o único built-in
// de função que lê ctxGoName, e um Apply não tem parâmetro ctx próprio, então
// só quando now() aparece de fato é que "context" precisa ser importado (ver
// a doc em emitApply). Mesmo padrão de varredura de blockReferencesIdent.
func blockCallsFunc(b *ast.Block, name string) bool {
	found := false
	astutil.ForEachExprInBlock(b, func(e ast.Expr) {
		if call, ok := e.(*ast.CallExpr); ok && astutil.IsIdent(call.Fn, name) {
			found = true
		}
	})
	return found
}

// --- Condição de access: caso especial "caller.X == <VO wrapper>" (§7 do
// prompt da task E6.1). ---

// lowerAccessCondition loweriza a Condition de um AccessRule (§4.5): desce
// recursivamente por AND/OR (ambos os lados lowerizados pela mesma função),
// e trata de forma ESPECIAL toda igualdade (==/!=) cujo um lado é literalmente
// "caller.X" e o outro é uma expressão cujo tipo é um VO wrapper (§7): Go não
// deixa comparar o "string" nativo de caller.X (runtime.Caller não tem tipo
// DomainScript) com o tipo nomeado de um VO wrapper (ex. WalletId) sem
// conversão explícita — "caller.id == self.id" vira "WalletId(caller.ID()) ==
// w.state.Id", não "caller.ID() == w.state.Id" (que não compila). Qualquer
// outra forma (incl. igualdade sem "caller.X" de nenhum lado) delega para
// Lowerer.Expr, cujo dispatch normal (goname.LowerVOBinaryDispatch) já cobre
// o resto do §4.2.
func lowerAccessCondition(l *lower.Lowerer, env *lower.TypeEnv, cond ast.Expr) (string, error) {
	bin, ok := cond.(*ast.BinaryExpr)
	if !ok {
		if call, ok := cond.(*ast.CallExpr); ok {
			goExpr, handled, err := lowerCallerHasRole(l, call)
			if err != nil {
				return "", err
			}
			if handled {
				return goExpr, nil
			}
		}
		return l.Expr(cond)
	}

	switch bin.Op {
	case token.AND, token.OR:
		leftGo, err := lowerAccessCondition(l, env, bin.Left)
		if err != nil {
			return "", err
		}
		rightGo, err := lowerAccessCondition(l, env, bin.Right)
		if err != nil {
			return "", err
		}
		opGo, ok := goname.NativeBinaryOp(bin.Op)
		if !ok {
			return "", fmt.Errorf("codegen: access: operador lógico %q não suportado", bin.Op.String())
		}
		return fmt.Sprintf("%s %s %s", leftGo, opGo, rightGo), nil

	case token.EQ, token.NEQ:
		goExpr, handled, err := lowerCallerVOEquality(l, env, bin)
		if err != nil {
			return "", err
		}
		if handled {
			return goExpr, nil
		}
	}

	return l.Expr(cond)
}

// isCallerMemberExpr reporta se e é literalmente "caller.Name" (um MemberExpr
// cujo receptor é o Ident nu "caller") e devolve o próprio MemberExpr.
func isCallerMemberExpr(e ast.Expr) (*ast.MemberExpr, bool) {
	mem, ok := e.(*ast.MemberExpr)
	if !ok {
		return nil, false
	}
	id, ok := mem.X.(*ast.Ident)
	if !ok || id.Name != "caller" {
		return nil, false
	}
	return mem, true
}

// lowerCallerVOEquality implementa o caso especial de lowerAccessCondition:
// bin é uma igualdade (==/!=) com EXATAMENTE um dos lados "caller.X" e o
// outro lado de tipo VO wrapper (*types.VOType com Base != nil). handled=false
// (sem erro) quando bin não casa essa forma — o chamador segue para o
// dispatch normal.
func lowerCallerVOEquality(l *lower.Lowerer, env *lower.TypeEnv, bin *ast.BinaryExpr) (goExpr string, handled bool, err error) {
	leftMem, leftIsCaller := isCallerMemberExpr(bin.Left)
	rightMem, rightIsCaller := isCallerMemberExpr(bin.Right)
	if leftIsCaller == rightIsCaller {
		// Nenhum dos dois é "caller.X", ou os dois são (ex. "caller.id ==
		// caller.id", que não faz sentido de negócio mas não é este caso) —
		// não é a forma especial.
		return "", false, nil
	}

	var callerMem *ast.MemberExpr
	var otherExpr ast.Expr
	callerOnLeft := leftIsCaller
	if callerOnLeft {
		callerMem, otherExpr = leftMem, bin.Right
	} else {
		callerMem, otherExpr = rightMem, bin.Left
	}

	otherType, err := env.InferAssignRHS(otherExpr)
	if err != nil {
		return "", false, err
	}
	vo, ok := otherType.(*types.VOType)
	if !ok || vo.Base == nil {
		return "", false, nil // outro lado não é VO wrapper: dispatch normal decide
	}

	callerGo, err := l.Expr(callerMem)
	if err != nil {
		return "", false, err
	}
	otherGo, err := l.Expr(otherExpr)
	if err != nil {
		return "", false, err
	}
	opGo, ok := goname.NativeBinaryOp(bin.Op)
	if !ok {
		return "", false, fmt.Errorf("codegen: access: operador %q não suportado na comparação caller/VO", bin.Op.String())
	}

	convertedCaller := fmt.Sprintf("%s(%s)", vo.Name, callerGo)
	if callerOnLeft {
		return fmt.Sprintf("%s %s %s", convertedCaller, opGo, otherGo), true, nil
	}
	return fmt.Sprintf("%s %s %s", otherGo, opGo, convertedCaller), true, nil
}

// lowerCallerHasRole implementa o outro caso especial de lowerAccessCondition
// (ISSUE-12 item 1): call é literalmente "caller.hasRole(<role>)" — um
// CallExpr cujo Fn é o MemberExpr "caller.hasRole", fora do escopo de
// lowerCallerVOEquality (que só trata igualdade/desigualdade). Ao contrário
// de caller.id/caller.authenticated (MemberExpr puro, tratado por
// Lowerer.member/callerMember), caller.hasRole é uma chamada explícita — o
// próprio doc de callerMember (codegen/lower/expr.go) já apontava essa
// intenção, nunca conectada a um chamador. handled=false (sem erro) quando
// call não casa essa forma — o chamador segue para o dispatch normal
// (Lowerer.Expr), que hoje rejeita qualquer CallExpr cujo Fn não seja um
// Ident nu (construção de tipo) com um erro claro.
func lowerCallerHasRole(l *lower.Lowerer, call *ast.CallExpr) (goExpr string, handled bool, err error) {
	mem, ok := isCallerMemberExpr(call.Fn)
	if !ok || mem.Name != "hasRole" {
		return "", false, nil
	}

	if len(call.Args) != 1 {
		return "", false, fmt.Errorf("codegen: access: caller.hasRole espera exatamente 1 argumento (o papel), recebeu %d", len(call.Args))
	}

	callerGo, err := l.Expr(mem.X)
	if err != nil {
		return "", false, err
	}
	roleGo, err := l.Expr(call.Args[0].Value)
	if err != nil {
		return "", false, err
	}

	return fmt.Sprintf("%s.HasRole(%s)", callerGo, roleGo), true, nil
}
