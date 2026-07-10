package codegen

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/types"
)

// gentest.go emite o Go de *.test.ds (H4, REQ-31, §design codegen 3.14): cada
// ast.TestDecl vira um arquivo "<pkg>_test.go", package <pkg> (interno, NÃO
// "<pkg>_test" — precisa acessar state/id/applyX não-exportados do Aggregate,
// exatamente como os testes de comportamento hand-written de E6.1/E6.2 já
// fazem, ver decl_aggregate_test.go:newTestWallet), com um func TestX(t
// *testing.T) por scenario: monta o given, dispara o when, verifica o then.
//
// --- Escopo desta fase (documentado, não um esquecimento) ---
//
// §22 declara sete formas de cenário cruzando 4 famílias de alvo (Aggregate,
// UseCase, Policy/Query, Saga) mais mock/fail-step/property/Fixture (REQ-
// 31.1-4). Esta fase cobre §22.1 (Aggregate), §22.2 (UseCase) e §22.6
// (Fixture): um Test cujo Name resolve a um *ast.AggregateDecl OU a um
// *ast.UseCaseDecl DESTE módulo — achado por casamento de nome exato contra o
// mapa recebido (mesmo padrão de sema/rules_test_files.go:sagaSteps, que já faz
// esse casamento por nome para Saga) — e cada *ast.FixtureDecl "Subject from
// [eventos]" (helper reusável, ver a doc de emitFixtureDecl). Policy/Query
// (§22.4, emitted count sem Subject)/Saga (§22.3, mock/fail-step)/property
// (§22.5) ficam para fases seguintes — um Test cujo nome não resolve a um
// Aggregate NEM a um UseCase deste módulo, ou cujo cenário usa uma forma ainda
// não coberta, é um erro de geração claro agora, nunca gerado silenciosamente
// errado.
//
// --- given: seed direto, não replay de EventStore (achado documentado) ---
//
// Um given "[Evento(...), ...]" NÃO é reproduzido via runtime.EventStore+
// LoadX: LoadWallet real (E6.2) começa SEMPRE de um state Go zero-value antes
// do replay, e um VO composto com Operator (ex. Money.Add, §2.2) exige
// "currency == other.currency" — o wallet real só ganhou "Apply WalletCreated"
// (docs/examples/wallet/domain.ds, seedando balance/active) por causa desta
// task; mesmo assim, um given no MEIO de um replay (ex. "DepositPerformed"
// como 2º evento) precisaria que TODO evento anterior já tivesse Apply real —
// gerar via EventStore faria um given válido (§22.1: "dado que estes eventos
// aconteceram") depender de o domínio modelar cada transição, quando o
// PRÓPRIO propósito de um given é dispensar isso. Por construção, cada
// GivenClause é aplicado NA ORDEM diretamente sobre "w" (seedGivenEntities/
// seedGivenState, abaixo): um evento com Apply real chama o método (fidelidade
// semântica, NFR-15 — reusa a MESMA regra de negócio, não reimplementa);
// um evento SEM Apply (ou um "given state {...}") semeia w.state/w.id campo a
// campo por casamento de NOME (goname.ExportField) — nunca Go quebrado, nunca
// uma regra de negócio duplicada.
//
// --- when: sempre um Handle, nunca via Command/UseCase ---
//
// "when Action(...)" invoca o Handle de MESMO NOME no Aggregate diretamente
// (§22.1) — mesmo quando esse nome TAMBÉM nomeia um Command (convenção
// Command↔Handle do wallet, docs/examples/wallet/application.ds) — por isso
// os args de Action NUNCA passam por Lowerer.Expr como um todo (isso
// construiria o Command, não casaria com os parâmetros do Handle): cada arg é
// casado contra HandleDecl.Params por nome/posição (handleCallArgsGoOrder,
// mesma regra de voConstructArgsGoOrder) e lowerizado individualmente.
//
// --- caller: sempre autenticado, id = o do aggregate (achado documentado) ---
//
// A gramática de §22 não tem NENHUMA forma de expressar "como o caller X" —
// nem given, nem when, nem ScenarioDecl carregam essa informação. Um Handle
// cujo access usa caller.authenticated OU caller.id==self.id (Withdraw, no
// wallet) precisa de ALGUM caller para ser exercitado com sucesso. Toda
// chamada gerada usa runtime.NewTestCaller(string(w.state.Id)) (rtsrc/
// caller.go.txt) — autenticado, id igual ao do aggregate — satisfaz as duas
// formas de acesso uniformemente. Testar um cenário de acesso NEGADO
// (Forbidden) não é expressável nesta gramática — fora do escopo (não
// exercitado por wallet.test.ds nem por nenhum exemplo do spec §22).
//
// --- then: reflect.DeepEqual, não comparação campo a campo ---
//
// "then [eventos]" compara CADA evento esperado com o emitido via
// reflect.DeepEqual sobre o valor INTEIRO (ponteiro para o struct) — não
// field-by-field: genérico para QUALQUER shape de evento sem precisar
// conhecer seus campos aqui, e correto mesmo para runtime.Decimal (big.Int
// interno) DESDE que o valor esperado seja construído pelo MESMO caminho de
// lowering do valor real (NewDecimalFromInt/NewMoney) — o caso de todo
// exemplo real do spec (§21/§22 só constroem Money a partir de literais).
//
// --- UseCase (§22.2): EventStore de verdade, não seed direto ---
//
// Ao contrário de §22.1 (que constrói o Aggregate DIRETO, sem Store — ver a
// doc acima), um Test de UseCase invoca a função gerada do UseCase (ex.
// "PerformDeposit(ctx, cmd)", decl_usecase.go) como uma CAIXA-PRETA: ela
// carrega o Aggregate de dentro do seu próprio corpo ("load Wallet(cmd.
// walletId)", via LoadWallet+Tx), então "given Wallet(\"W1\") from [eventos]"
// PRECISA semear um runtime.EventStore de verdade (store.Append, sem
// nenhuma reconstrução de Apply manual da nossa parte — LoadWallet, já
// testado, faz isso quando o UseCase chamar "load") — mecanismo
// estruturalmente diferente de §22.1, não uma inconsistência.
//
// "committed"/"rolledback" (ThenAssert.Verb) são só "err == nil"/"err !=
// nil": rtsrc/uow.go.txt documenta que memoryUnitOfWork.Run não tem stage
// nenhum — um Tx.Append já é durável no instante em que retorna, e não há
// nada para desfazer num erro depois (comentário de memoryUnitOfWork) — ou
// seja, o UseCase só "roda até o fim sem Append nenhum a mais" quando falha
// ANTES de qualquer novo Append (o caminho comum: o Handle despachado
// devolve erro e o corpo do UseCase faz "return err" antes de "tx.Append").
// Verificar isso a mais fundo (reverter Appends específicos) exigiria um
// staging de verdade que o runtime não tem — fora do escopo aqui, coerente
// com o próprio runtime.
//
// "Subject emitted Evento" é resolvido por ÍNDICE ESTÁTICO, não por
// comparação de conteúdo: cada given "Subject from [N eventos]" já fixa, em
// tempo de GERAÇÃO, quantos eventos aquele Subject tinha ANTES do UseCase
// rodar; um "Subject emitted X" no then busca "store.Load(ctx, id)" UMA VEZ
// (cacheado na 1ª ocorrência DAQUELE Subject) e indexa a partir de
// givenCount — a mesma ordem declarada nos vários "Subject emitted" cobre
// vários eventos novos do MESMO Subject, um por linha (ver ucSubjectState).
//
// Caller: como um cenário de UseCase pode envolver MAIS de um Aggregate
// (§22.2, PerformTransfer sobre 2 wallets), não há um "self" único para
// "caller.id == self.id" combinar — o caller gerado é sempre
// runtime.NewTestCaller("test-caller") (autenticado, id fixo): cobre
// caller.authenticated (o caso do wallet real, PerformDeposit/
// PerformWithdrawal) mas NÃO exercita um Handle cujo access dependa de
// caller.id bater com um Aggregate específico — mesma lacuna documentada de
// §22.1 (a gramática não tem forma de expressar "como o caller X"),
// carregada para UseCase.

// EmitTests emite o Go de todas as ast.TestDecl E ast.FixtureDecl de um módulo
// (H4, REQ-31) num único arquivo "<pkg>_test.go" — package pkg (interno, ver a
// doc do arquivo). aggregates/usecases são os mapas nome->Decl deste módulo
// (mesma forma que EmitUseCases já recebe) usados para resolver o alvo de cada
// Test (§22.1/22.2, ver a doc do arquivo) — aggregates checado primeiro
// (§22.1/§22.2 não colidem: um nome de módulo não nomeia as duas coisas ao
// mesmo tempo). fixtures (§22.6, ver a doc de emitFixtureDecl) viram helpers
// "func fixture<Nome>(t *testing.T) *<AggType>" ao lado dos Test — reusam o
// MESMO mapa aggregates para resolver o Aggregate do Subject. decls E fixtures
// vazios devolvem (nil, nil): o CHAMADOR (generateModuleFiles) decide, a partir
// disso, se escreve o arquivo.
func EmitTests(pkg string, decls []*ast.TestDecl, fixtures []*ast.FixtureDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, aggregates map[string]*ast.AggregateDecl, usecases map[string]*ast.UseCaseDecl) ([]byte, error) {
	if len(decls) == 0 && len(fixtures) == 0 {
		return nil, nil
	}
	e := emit.New(pkg)
	e.Import("testing")
	needsErrors, needsReflect, needsContext := scanTestNeeds(decls, aggregates, usecases)
	var errorsAlias, reflectAlias, contextAlias string
	if needsErrors {
		errorsAlias = e.Import("errors")
	}
	if needsReflect {
		reflectAlias = e.Import("reflect")
	}
	if needsContext {
		contextAlias = e.Import("context")
	}
	runtimeAlias := e.Import(RuntimeImportPath)

	for _, t := range decls {
		if len(t.Properties) > 0 {
			return nil, fmt.Errorf("Test %s: \"property\" (§22.5) ainda não é suportado nesta fase de H4", t.Name)
		}
		if agg, ok := aggregates[t.Name]; ok {
			if err := emitAggregateTestDecl(e, t, agg, model, tab, module, reg, runtimeAlias, errorsAlias, reflectAlias); err != nil {
				return nil, fmt.Errorf("Test %s: %w", t.Name, err)
			}
			continue
		}
		if uc, ok := usecases[t.Name]; ok {
			if err := emitUseCaseTestDecl(e, t, uc, model, tab, module, reg, runtimeAlias, errorsAlias, reflectAlias, contextAlias); err != nil {
				return nil, fmt.Errorf("Test %s: %w", t.Name, err)
			}
			continue
		}
		return nil, fmt.Errorf("Test %s: alvo não resolve a um Aggregate nem a um UseCase deste módulo — só §22.1/22.2 são suportados nesta fase de H4", t.Name)
	}

	for _, f := range fixtures {
		if err := emitFixtureDecl(e, f, model, tab, module, reg, runtimeAlias, aggregates); err != nil {
			return nil, fmt.Errorf("Fixture %s: %w", f.Name, err)
		}
	}
	return e.Bytes()
}

// scanTestNeeds varre decls procurando por formas de "then" que exigem um
// import condicional (evita "importado e não usado" quando nenhum cenário de
// nenhum Test do módulo usa aquela forma — mesmo padrão de EmitMetrics,
// decl_metric.go, runtimeAlias lazy). needsContext é true quando ao menos um
// Test resolve a um UseCase (§22.2 sempre usa context.Background(), ver a
// doc do arquivo) — independente da forma dos cenários dentro dele.
func scanTestNeeds(decls []*ast.TestDecl, aggregates map[string]*ast.AggregateDecl, usecases map[string]*ast.UseCaseDecl) (needsErrors, needsReflect, needsContext bool) {
	for _, t := range decls {
		if _, isAgg := aggregates[t.Name]; !isAgg {
			if _, isUC := usecases[t.Name]; isUC {
				needsContext = true
			}
		}
		for _, sc := range t.Scenarios {
			if sc.Then == nil {
				continue
			}
			if sc.Then.Error != "" {
				needsErrors = true
			}
			if len(sc.Then.Events) > 0 {
				needsReflect = true
			}
			for _, a := range sc.Then.Asserts {
				if a.Error != "" {
					needsErrors = true
				}
				if a.Verb == "emitted" && a.Subject != nil {
					needsReflect = true
				}
			}
		}
	}
	return needsErrors, needsReflect, needsContext
}

// emitAggregateTestDecl emite um func TestX por scenario de t (§22.1).
func emitAggregateTestDecl(e *emit.Emitter, t *ast.TestDecl, agg *ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias, errorsAlias, reflectAlias string) error {
	applyByEvent := make(map[string]bool, len(agg.Appliers))
	for _, a := range agg.Appliers {
		applyByEvent[a.Event] = true
	}
	handleByName := make(map[string]*ast.HandleDecl, len(agg.Handlers))
	for _, h := range agg.Handlers {
		handleByName[h.Name] = h
	}
	stateFields := make(map[string]*ast.Field, len(agg.State))
	for _, f := range agg.State {
		if f != nil {
			stateFields[f.Name] = f
		}
	}

	used := make(map[string]int)
	for i, sc := range t.Scenarios {
		fn := scenarioFuncName(t.Name, i, sc.Name, used)

		env := lower.New(model, tab, module)
		l := lower.NewLowerer(env, reg, runtimeAlias)
		sl := lower.NewStmtLowerer(l, e, lower.StmtContext{Panics: true})

		e.Line("")
		e.Line("// %s prova o cenário %q de Test %s (§22.1, REQ-31).", fn, sc.Name, t.Name)
		var bodyErr error
		e.Block(fmt.Sprintf("func %s(t *testing.T)", fn), func() {
			bodyErr = emitAggregateScenarioBody(e, sl, sc, agg, applyByEvent, handleByName, stateFields, runtimeAlias, errorsAlias, reflectAlias)
		})
		if bodyErr != nil {
			return fmt.Errorf("scenario %q: %w", sc.Name, bodyErr)
		}
	}
	return nil
}

// emitAggregateScenarioBody emite o corpo de um func TestX: given (na ordem
// declarada) + when (1 Handle) + then.
func emitAggregateScenarioBody(e *emit.Emitter, sl *lower.StmtLowerer, sc *ast.ScenarioDecl, agg *ast.AggregateDecl, applyByEvent map[string]bool, handleByName map[string]*ast.HandleDecl, stateFields map[string]*ast.Field, runtimeAlias, errorsAlias, reflectAlias string) error {
	if len(sc.Mocks) > 0 || len(sc.Fails) > 0 {
		return fmt.Errorf("\"mock\"/\"fail step\" (§22.3) só cabem em cenário de Saga — Test %s é de Aggregate, fase futura de H4", agg.Name)
	}
	if sc.When == nil {
		return fmt.Errorf("cenário sem \"when\"")
	}
	if sc.When.IsEvent {
		return fmt.Errorf("\"when event ...\" (§22.4) é de Policy/Query — Test %s é de Aggregate, fase futura de H4", agg.Name)
	}
	if sc.Then == nil {
		return fmt.Errorf("cenário sem \"then\"")
	}
	if len(sc.Then.Asserts) > 0 {
		return fmt.Errorf("\"then { ... }\" (§22.2-3) é de UseCase/Saga — Test %s é de Aggregate, use \"then [eventos]\"/\"then error\", fase futura de H4", agg.Name)
	}

	receiver := aggregateReceiver(agg.Name)
	e.Line("%s := &%s{}", receiver, agg.Name)

	for _, g := range sc.Givens {
		if err := emitAggregateGiven(e, sl, g, agg, applyByEvent, stateFields, receiver); err != nil {
			return fmt.Errorf("given: %w", err)
		}
	}

	h, call, err := resolveAggregateWhen(sc.When, handleByName)
	if err != nil {
		return fmt.Errorf("when: %w", err)
	}
	argsGo, err := handleCallArgsGoOrder(e, sl, h, call.Args)
	if err != nil {
		return fmt.Errorf("when %s: %w", h.Name, err)
	}
	callerGo := fmt.Sprintf("%s.NewTestCaller(string(%s.state.Id))", runtimeAlias, receiver)
	allArgs := append([]string{callerGo}, argsGo...)
	e.Line("events, err := %s.%s(%s)", receiver, h.Name, strings.Join(allArgs, ", "))

	return emitAggregateThen(e, sl, sc.Then, errorsAlias, reflectAlias)
}

// emitAggregateGiven emite UMA GivenClause (uma de possivelmente várias no
// mesmo scenario, aplicadas em ordem — ver a doc do arquivo sobre a 2ª given
// de "carteira inativa", docs/examples/wallet/wallet.test.ds, que sobrescreve
// active DEPOIS do given de eventos): "given state {...}" vira overlay direto
// de campos; "given [entidades]" processa cada evento via Apply real (quando
// existe) ou seed direto por nome (quando não existe — ex. WalletCreated
// ANTES desta task, ou um evento hipotético de outro domínio sem Apply).
// "given Subject from [...]"/"given binding [...]" (UseCase/Policy, §22.2/4)
// são erro claro aqui (fase futura).
func emitAggregateGiven(e *emit.Emitter, sl *lower.StmtLowerer, g *ast.GivenClause, agg *ast.AggregateDecl, applyByEvent map[string]bool, stateFields map[string]*ast.Field, receiver string) error {
	if g.State != nil && g.Entities == nil && g.Subject == nil && g.Binding == "" {
		return emitStateOverlay(e, sl, receiver, g.State)
	}
	if g.Subject != nil || g.Binding != "" {
		return fmt.Errorf("\"given Subject from [...]\"/\"given binding [...]\" (§22.2/4) são de UseCase/Policy — Test %s é de Aggregate, fase futura de H4", agg.Name)
	}
	for _, entity := range g.Entities {
		if err := emitAggregateGivenEntity(e, sl, entity, applyByEvent, stateFields, receiver); err != nil {
			return err
		}
	}
	return nil
}

// emitAggregateGivenEntity processa uma entidade de "given [..., Entidade,
// ...]": Entity precisa ser uma construção de Event (CallExpr) — devolve erro
// claro para qualquer outra forma.
func emitAggregateGivenEntity(e *emit.Emitter, sl *lower.StmtLowerer, entity *ast.GivenEntity, applyByEvent map[string]bool, stateFields map[string]*ast.Field, receiver string) error {
	call, ok := entity.Entity.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("esperava uma construção de Event (\"Nome(...)\"), got %T", entity.Entity)
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return fmt.Errorf("esperava um Event nomeado, got %T", call.Fn)
	}
	eventName := id.Name

	if applyByEvent[eventName] {
		evGo, hoisted, err := sl.ExprHoisted(call)
		if err != nil {
			return fmt.Errorf("%s(...): %w", eventName, err)
		}
		emitLines(e, hoisted)
		e.Line("%s.apply%s(%s)", receiver, eventName, evGo)
	} else {
		// Sem Apply para este Event (ver a doc do arquivo): seed direto por
		// nome de campo — cada Arg nomeado que casa com um campo de
		// agg.State vira "receiver.state.Campo = valor"; "id" também
		// sincroniza o mirror "receiver.id" (mesma convenção de LoadWallet,
		// decl_aggregate_load.go).
		if err := emitFieldSeed(e, sl, call.Args, stateFields, receiver); err != nil {
			return fmt.Errorf("%s(...): %w", eventName, err)
		}
	}

	if entity.State != nil {
		if err := emitStateOverlay(e, sl, receiver, entity.State); err != nil {
			return err
		}
	}
	return nil
}

// emitFieldSeed atribui, para cada Arg nomeado em args que casa (por nome DS)
// com um campo de stateFields, "receiver.state.Campo = valor" — e, quando o
// nome é "id", também "receiver.id = valor" (mirror, ver a doc do arquivo).
// Args sem campo correspondente são ignorados silenciosamente (podem ser
// campos de negócio do Event que não têm equivalente direto no state — ex.
// DepositPerformed.amount vs. Wallet.balance — só relevantes via Apply real).
func emitFieldSeed(e *emit.Emitter, sl *lower.StmtLowerer, args []ast.Arg, stateFields map[string]*ast.Field, receiver string) error {
	for _, a := range args {
		field, ok := stateFields[a.Name]
		if !ok {
			continue
		}
		goExpr, hoisted, err := sl.ExprHoisted(a.Value)
		if err != nil {
			return fmt.Errorf("campo %q: %w", a.Name, err)
		}
		emitLines(e, hoisted)
		e.Line("%s.state.%s = %s", receiver, goname.ExportField(field.Name), goExpr)
		if a.Name == "id" {
			e.Line("%s.id = %s.state.%s", receiver, receiver, goname.ExportField(field.Name))
		}
	}
	return nil
}

// emitStateOverlay emite "receiver.state.Campo = valor" para cada entrada de
// obj (a forma "given state {...}"/GivenEntity.State, §22) — usado tanto para
// sobrescrever um campo específico (ex. "active: false" na 2ª given da
// carteira inativa) quanto, no futuro, para o given direto de um Aggregate
// StateStored.
func emitStateOverlay(e *emit.Emitter, sl *lower.StmtLowerer, receiver string, obj *ast.ObjectExpr) error {
	for _, entry := range obj.Entries {
		goExpr, hoisted, err := sl.ExprHoisted(entry.Value)
		if err != nil {
			return fmt.Errorf("state.%s: %w", entry.Key, err)
		}
		emitLines(e, hoisted)
		e.Line("%s.state.%s = %s", receiver, goname.ExportField(entry.Key), goExpr)
	}
	return nil
}

// resolveAggregateWhen valida e devolve o HandleDecl+CallExpr nomeados por
// "when Action(...)" (§22.1) — a ÚNICA forma suportada nesta fase (ver a doc
// do arquivo sobre por que "Action" nunca passa por Lowerer.Expr como um
// todo).
func resolveAggregateWhen(w *ast.WhenClause, handleByName map[string]*ast.HandleDecl) (*ast.HandleDecl, *ast.CallExpr, error) {
	call, ok := w.Action.(*ast.CallExpr)
	if !ok {
		return nil, nil, fmt.Errorf("esperava uma chamada de Handle (\"Nome(args...)\"), got %T", w.Action)
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return nil, nil, fmt.Errorf("esperava um Handle nomeado, got %T", call.Fn)
	}
	h, ok := handleByName[id.Name]
	if !ok {
		return nil, nil, fmt.Errorf("%q não é um Handle deste Aggregate", id.Name)
	}
	return h, call, nil
}

// handleCallArgsGoOrder casa args (de "when Action(args...)") contra
// h.Params na ORDEM DECLARADA (nomeados por nome; posicionais por posição —
// mistura é erro, mesma regra de voConstructArgsGoOrder/constructShapeNamed/
// Positional) e devolve o texto Go de cada um, já hoisted quando necessário
// (ex. um arg "amount: Money(100, \"BRL\")").
func handleCallArgsGoOrder(e *emit.Emitter, sl *lower.StmtLowerer, h *ast.HandleDecl, args []ast.Arg) ([]string, error) {
	named := false
	for _, a := range args {
		if a.Name != "" {
			named = true
			break
		}
	}

	ordered := make([]ast.Expr, len(h.Params))
	if named {
		byName := make(map[string]ast.Expr, len(args))
		for _, a := range args {
			if a.Name == "" {
				return nil, fmt.Errorf("mistura argumentos nomeados e posicionais")
			}
			byName[a.Name] = a.Value
		}
		for i, p := range h.Params {
			v, ok := byName[p.Name]
			if !ok {
				return nil, fmt.Errorf("não informa o parâmetro %q", p.Name)
			}
			ordered[i] = v
		}
	} else {
		if len(args) != len(h.Params) {
			return nil, fmt.Errorf("%d argumentos posicionais, Handle declara %d parâmetros", len(args), len(h.Params))
		}
		for i, a := range args {
			ordered[i] = a.Value
		}
	}

	out := make([]string, len(ordered))
	for i, v := range ordered {
		goExpr, hoisted, err := sl.ExprHoisted(v)
		if err != nil {
			return nil, fmt.Errorf("parâmetro %q: %w", h.Params[i].Name, err)
		}
		emitLines(e, hoisted)
		out[i] = goExpr
	}
	return out, nil
}

// emitAggregateThen emite as asserções de sc.Then (§22.1): "then error Name"
// ou "then [eventos]" (ver a doc do arquivo sobre reflect.DeepEqual).
func emitAggregateThen(e *emit.Emitter, sl *lower.StmtLowerer, then *ast.ThenClause, errorsAlias, reflectAlias string) error {
	if then.Error != "" {
		e.Block("if err == nil", func() {
			e.Line("t.Fatalf(%q, events)", "esperava erro "+then.Error+", sucesso com eventos: %+v")
		})
		e.Block(fmt.Sprintf("if !%s.Is(err, Err%s)", errorsAlias, then.Error), func() {
			e.Line("t.Fatalf(%q, err)", "esperava errors.Is(err, Err"+then.Error+"), got: %v")
		})
		return nil
	}

	e.Block("if err != nil", func() {
		e.Line("t.Fatalf(%q, err)", "esperava sucesso, erro inesperado: %v")
	})
	e.Block(fmt.Sprintf("if len(events) != %d", len(then.Events)), func() {
		e.Line("t.Fatalf(%q, len(events), events)", fmt.Sprintf("esperava %d evento(s), got %%d: %%+v", len(then.Events)))
	})
	for i, want := range then.Events {
		goExpr, hoisted, err := sl.ExprHoisted(want)
		if err != nil {
			return fmt.Errorf("then[%d]: %w", i, err)
		}
		emitLines(e, hoisted)
		wantVar := fmt.Sprintf("want%d", i)
		e.Line("%s := &%s", wantVar, goExpr)
		e.Block(fmt.Sprintf("if !%s.DeepEqual(events[%d], %s)", reflectAlias, i, wantVar), func() {
			e.Line("t.Fatalf(%q, %d, events[%d], %s)", "evento[%d]: got %+v, want %+v", i, i, wantVar)
		})
	}
	return nil
}

// --- Fixture (§22.6, REQ-31.4) ---
//
// Uma Fixture (§22.6) é uma pré-condição REUSÁVEL: "Fixture activeWallet {
// Wallet(\"W1\") from [eventos] }" descreve um Aggregate já semeado, sem when/
// then. Nada no front-end (parser/resolver/sema) liga uma Fixture a um Test —
// não há sintaxe "use Fixture X" na gramática de §22 (confirmado). Logo o
// helper gerado ("func fixtureActiveWallet(t *testing.T) *Wallet") não tem
// chamador DENTRO do projeto gerado — o que é esperado e OK (Go não recusa uma
// func de topo não usada). Sua corretude é provada por um chamador de teste
// escrito à mão (gentest_test.go), no mesmo espírito dos testes comportamentais
// hand-written do pacote.
//
// --- Forma suportada e escopo (documentado, não esquecimento) ---
//
// Suportada: a forma do PRÓPRIO exemplo do spec (§22.6), "Subject from
// [eventos]" — GivenClause.Subject == Wallet(\"W1\"), Entities == a lista. O
// Subject nomeia um Aggregate por TIPO (a cabeça do CallExpr, "Wallet"),
// resolvido contra o MESMO mapa aggregates de EmitTests/EmitUseCases; o helper
// constrói o Aggregate DIRETO ("w := &Wallet{}") e reusa EXATAMENTE a máquina
// de given de §22.1 (emitAggregateGivenEntity: Apply real quando existe, seed
// campo-a-campo quando não) — a MESMA filosofia "seed direto, não replay de
// EventStore" (ver a doc do arquivo sobre por quê: bootstrapping de um VO com
// Operator como Money a partir de um zero-value quebraria). O id vem dos
// próprios eventos (via Apply/seed), como em §22.1 — o Subject só resolve o
// TIPO. Várias givens acumulam sobre o MESMO receiver, na ordem declarada
// (idêntico a §22.1).
//
// Escopo deliberadamente recusado com erro claro (nunca gerado errado):
// (a) uma lista de eventos sem Subject ("given [eventos]") é ambígua (qual
// Aggregate?) — precisaria inferir o alvo casando os tipos de evento contra
// applyByEvent de TODOS os Aggregates, adiado; (b) "given state {...}"/"given
// binding [...]" (StateStored/Policy, §22.4); (c) uma Fixture que referencia
// MAIS de um Aggregate (dois Subjects de tipos diferentes) — um helper
// multi-Subject retornaria vários valores/um struct, adiado: use uma Fixture
// por Aggregate.

// emitFixtureDecl emite "func fixture<Nome>(t *testing.T) *<AggType>" para uma
// Fixture (§22.6) — reusa a máquina de given de §22.1 (ver a doc acima).
func emitFixtureDecl(e *emit.Emitter, f *ast.FixtureDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias string, aggregates map[string]*ast.AggregateDecl) error {
	agg, err := resolveFixtureAggregate(f, aggregates)
	if err != nil {
		return err
	}
	applyByEvent := make(map[string]bool, len(agg.Appliers))
	for _, a := range agg.Appliers {
		applyByEvent[a.Event] = true
	}
	stateFields := make(map[string]*ast.Field, len(agg.State))
	for _, fld := range agg.State {
		if fld != nil {
			stateFields[fld.Name] = fld
		}
	}

	env := lower.New(model, tab, module)
	l := lower.NewLowerer(env, reg, runtimeAlias)
	sl := lower.NewStmtLowerer(l, e, lower.StmtContext{Panics: true})

	fn := "fixture" + goname.ExportField(f.Name)
	receiver := aggregateReceiver(agg.Name)

	e.Line("")
	e.Line("// %s constrói o Aggregate %s semeado pela Fixture %s (§22.6, REQ-31).", fn, agg.Name, f.Name)
	var bodyErr error
	e.Block(fmt.Sprintf("func %s(t *testing.T) *%s", fn, agg.Name), func() {
		bodyErr = emitFixtureBody(e, sl, f, agg, applyByEvent, stateFields, receiver)
	})
	if bodyErr != nil {
		return bodyErr
	}
	return nil
}

// emitFixtureBody emite o corpo de um helper de Fixture: "w := &Agg{}", cada
// entidade de cada given semeada na ordem declarada (emitAggregateGivenEntity,
// reuso de §22.1), e "return w". t.Helper() marca o helper para o report de
// falha do testing apontar o CHAMADOR (idioma de helper de teste Go).
func emitFixtureBody(e *emit.Emitter, sl *lower.StmtLowerer, f *ast.FixtureDecl, agg *ast.AggregateDecl, applyByEvent map[string]bool, stateFields map[string]*ast.Field, receiver string) error {
	e.Line("t.Helper()")
	e.Line("%s := &%s{}", receiver, agg.Name)
	for _, g := range f.Givens {
		for _, entity := range g.Entities {
			if err := emitAggregateGivenEntity(e, sl, entity, applyByEvent, stateFields, receiver); err != nil {
				return fmt.Errorf("given: %w", err)
			}
		}
	}
	e.Line("return %s", receiver)
	return nil
}

// resolveFixtureAggregate valida a forma de CADA given de f (só "Subject from
// [eventos]", §22.6 — ver a doc de emitFixtureDecl) e devolve o ÚNICO
// AggregateDecl que todos os Subjects nomeiam. Uma segunda given com Subject de
// tipo diferente, uma given sem Subject, ou "state {...}"/"binding [...]" são
// erro de geração claro (fase futura).
func resolveFixtureAggregate(f *ast.FixtureDecl, aggregates map[string]*ast.AggregateDecl) (*ast.AggregateDecl, error) {
	if len(f.Givens) == 0 {
		return nil, fmt.Errorf("Fixture sem nenhum given — nada a semear")
	}
	var agg *ast.AggregateDecl
	var aggName string
	for _, g := range f.Givens {
		if g.Binding != "" {
			return nil, fmt.Errorf("\"given binding [...]\" (§22.4, Policy) não é suportado em Fixture nesta fase de H4 — use \"Subject from [eventos]\" (§22.6)")
		}
		if g.Subject == nil {
			if g.State != nil {
				return nil, fmt.Errorf("\"state { ... }\" (StateStored) não é suportado em Fixture nesta fase de H4 — use \"Subject from [eventos]\" (§22.6)")
			}
			return nil, fmt.Errorf("lista de eventos sem Subject (\"[...]\") é ambígua (qual Aggregate?) — não suportada nesta fase de H4, use \"Subject from [eventos]\" (§22.6)")
		}
		name, err := fixtureSubjectAggregateName(g.Subject)
		if err != nil {
			return nil, err
		}
		if aggName == "" {
			a, ok := aggregates[name]
			if !ok {
				return nil, fmt.Errorf("Subject %q não resolve a um Aggregate deste módulo — só uma Fixture sobre um Aggregate local é suportada nesta fase de H4", name)
			}
			agg, aggName = a, name
		} else if name != aggName {
			return nil, fmt.Errorf("Fixture referencia mais de um Aggregate (%q e %q) — helper multi-Subject (vários valores/struct) não é suportado nesta fase de H4; use uma Fixture por Aggregate", aggName, name)
		}
	}
	return agg, nil
}

// fixtureSubjectAggregateName extrai o nome do Aggregate da cabeça de um
// Subject "Type(id)" (ex. Wallet(\"W1\") -> "Wallet"). O id em si não é usado
// no seed (os eventos o carregam, como em §22.1) — só o TIPO resolve o
// Aggregate. Qualquer outra forma é erro de geração claro.
func fixtureSubjectAggregateName(subject ast.Expr) (string, error) {
	call, ok := subject.(*ast.CallExpr)
	if !ok {
		return "", fmt.Errorf("esperava um Subject \"Type(id)\" (ex. Wallet(\"W1\")), got %T", subject)
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("esperava um Aggregate nomeado no Subject, got %T", call.Fn)
	}
	return id.Name, nil
}

// --- UseCase (§22.2, REQ-31.1) ---

// ucSubject rastreia, PARA UM Subject (ex. Wallet("W1")) dentro de UM
// scenario, o suficiente para resolver "Subject emitted Evento" por ÍNDICE
// ESTÁTICO (ver a doc do arquivo): idGo é o texto Go do id (ex. `"W1"`,
// usado como aggregateID de store.Append/Load); givenCount é quantos
// eventos esse Subject já tinha ANTES do UseCase rodar (somado por TODA
// given "Subject from [...]" que o referencia, na ordem declarada);
// afterVar é o nome Go (determinístico, "after1"/"after2"/... pela ORDEM de
// 1ª aparição no scenario) da variável que guarda store.Load(ctx, idGo)
// DEPOIS do UseCase rodar — só é de fato emitida (afterFetched) na 1ª
// asserção "emitted" que referencia este Subject; emittedIdx é o próximo
// deslocamento dentro de afterVar[givenCount+emittedIdx] a consumir.
type ucSubject struct {
	idGo         string
	givenCount   int
	afterVar     string
	afterFetched bool
	emittedIdx   int
}

// ucSubjects indexa ucSubject por idGo (o texto Go do id) — um scenario
// pode referenciar vários Subject (§22.2, PerformTransfer sobre 2 wallets).
// order preserva a ORDEM de 1ª aparição (determinismo, NFR-13) para nomear
// afterVar sequencialmente.
type ucSubjects struct {
	byID  map[string]*ucSubject
	order []string
}

func newUcSubjects() *ucSubjects {
	return &ucSubjects{byID: make(map[string]*ucSubject)}
}

// get devolve o ucSubject de idGo, criando um novo (afterVar determinístico
// pela ORDEM de 1ª aparição) se ainda não existir.
func (s *ucSubjects) get(idGo string) *ucSubject {
	if sub, ok := s.byID[idGo]; ok {
		return sub
	}
	sub := &ucSubject{idGo: idGo, afterVar: fmt.Sprintf("after%d", len(s.order)+1)}
	s.byID[idGo] = sub
	s.order = append(s.order, idGo)
	return sub
}

// emitUseCaseTestDecl emite um func TestX por scenario de t (§22.2).
func emitUseCaseTestDecl(e *emit.Emitter, t *ast.TestDecl, uc *ast.UseCaseDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias, errorsAlias, reflectAlias, contextAlias string) error {
	used := make(map[string]int)
	for i, sc := range t.Scenarios {
		fn := scenarioFuncName(t.Name, i, sc.Name, used)

		env := lower.New(model, tab, module)
		l := lower.NewLowerer(env, reg, runtimeAlias)
		sl := lower.NewStmtLowerer(l, e, lower.StmtContext{Panics: true})

		e.Line("")
		e.Line("// %s prova o cenário %q de Test %s (§22.2, REQ-31).", fn, sc.Name, t.Name)
		var bodyErr error
		e.Block(fmt.Sprintf("func %s(t *testing.T)", fn), func() {
			bodyErr = emitUseCaseScenarioBody(e, sl, sc, uc, runtimeAlias, errorsAlias, reflectAlias, contextAlias)
		})
		if bodyErr != nil {
			return fmt.Errorf("scenario %q: %w", sc.Name, bodyErr)
		}
	}
	return nil
}

// emitUseCaseScenarioBody emite o corpo de um func TestX de UseCase: monta
// um runtime.EventStore de verdade a partir de "given Subject from [...]"
// (ver a doc do arquivo sobre por que — diferente de §22.1), roda o UseCase
// via a função gerada (decl_usecase.go), e verifica o then (§22.2: emitted/
// committed/rolledback/error, dentro de um bloco "{ ... }").
func emitUseCaseScenarioBody(e *emit.Emitter, sl *lower.StmtLowerer, sc *ast.ScenarioDecl, uc *ast.UseCaseDecl, runtimeAlias, errorsAlias, reflectAlias, contextAlias string) error {
	if len(sc.Mocks) > 0 || len(sc.Fails) > 0 {
		return fmt.Errorf("\"mock\"/\"fail step\" (§22.3) só cabem em cenário de Saga — Test %s é de UseCase, fase futura de H4", uc.Name)
	}
	if sc.When == nil {
		return fmt.Errorf("cenário sem \"when\"")
	}
	if sc.When.IsEvent {
		return fmt.Errorf("\"when event ...\" (§22.4) é de Policy/Query — Test %s é de UseCase, fase futura de H4", uc.Name)
	}
	if sc.Then == nil {
		return fmt.Errorf("cenário sem \"then\"")
	}
	if sc.Then.Error != "" || len(sc.Then.Events) > 0 {
		return fmt.Errorf("\"then [eventos]\"/\"then error\" (fora de um bloco {...}) são de Aggregate — Test %s é de UseCase, use \"then { Subject emitted ..., committed/rolledback }\" (§22.2)", uc.Name)
	}

	e.Line("store := %s.NewMemoryEventStore()", runtimeAlias)
	subs := newUcSubjects()
	for _, g := range sc.Givens {
		if err := emitUseCaseGiven(e, sl, g, subs, uc, runtimeAlias, contextAlias); err != nil {
			return fmt.Errorf("given: %w", err)
		}
	}
	e.Line("uow := %s.NewUnitOfWork(store)", runtimeAlias)
	e.Line("Wire(uow)")
	e.Line("ctx := %s.WithCaller(%s.Background(), %s.NewTestCaller(\"test-caller\"))", runtimeAlias, contextAlias, runtimeAlias)

	call, ok := sc.When.Action.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("when: esperava uma construção de Command (%q(...)), got %T", uc.Handles, sc.When.Action)
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok || id.Name != uc.Handles {
		return fmt.Errorf("when: esperava o Command %q (o que Test %s handles), got %T", uc.Handles, uc.Name, call.Fn)
	}
	// "var err error" + "=" (nunca ":=") na chamada do UseCase abaixo: um
	// arg hoisted (ex. "amount: Money(50, \"BRL\")") já pode ter declarado
	// "err" via "tmpN, err := ..." (hoistVOConstruct) ANTES desta linha —
	// ":=" com "err" sozinho a esquerda falharia ("no new variables") nesse
	// caso; "=" é sempre válido, hoisted ou não.
	e.Line("var err error")
	cmdGo, hoisted, err := sl.ExprHoisted(call)
	if err != nil {
		return fmt.Errorf("when: %w", err)
	}
	emitLines(e, hoisted)
	e.Line("err = %s(ctx, %s)", uc.Name, cmdGo)

	for _, a := range sc.Then.Asserts {
		if err := emitUseCaseThenAssert(e, sl, a, subs, errorsAlias, reflectAlias, contextAlias, runtimeAlias); err != nil {
			return fmt.Errorf("then: %w", err)
		}
	}
	return nil
}

// ucSubjectID extrai o texto Go do id de um Subject (ex. Wallet("W1") ->
// `"W1"`) — precisa ser uma construção de 1 argumento posicional (mesma
// forma em given/then, ver a doc do arquivo). Não valida que "Wallet" é de
// fato um Aggregate deste módulo (o front-end, REQ-5.14, já garante que
// resolve a ALGUM símbolo — checkTestRef, sema/rules_test_files.go);
// qualquer outra forma é erro de geração claro.
func ucSubjectID(sl *lower.StmtLowerer, subject ast.Expr) (string, []string, error) {
	call, ok := subject.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Args[0].Name != "" {
		return "", nil, fmt.Errorf("esperava um Subject de 1 argumento posicional (ex. \"Wallet(\\\"W1\\\")\"), got %T", subject)
	}
	return sl.ExprHoisted(call.Args[0].Value)
}

// emitUseCaseGiven emite UMA GivenClause "Subject from [eventos]" (§22.2) —
// a ÚNICA forma suportada nesta fase para UseCase (given [eventos]/given
// binding [...]/given state{...} são de Aggregate/Policy, fase futura).
// Semeia store.Append (não construção direta — ver a doc do arquivo) e
// acumula o givenCount do Subject correspondente em subs.
func emitUseCaseGiven(e *emit.Emitter, sl *lower.StmtLowerer, g *ast.GivenClause, subs *ucSubjects, uc *ast.UseCaseDecl, runtimeAlias, contextAlias string) error {
	if g.Subject == nil {
		return fmt.Errorf("\"given [eventos]\"/\"given binding [...]\"/\"given state{...}\" são de Aggregate/Policy — Test %s é de UseCase, use \"given Subject from [...]\" (§22.2)", uc.Name)
	}
	idGo, idHoisted, err := ucSubjectID(sl, g.Subject)
	if err != nil {
		return fmt.Errorf("given Subject: %w", err)
	}
	emitLines(e, idHoisted)
	sub := subs.get(idGo)

	eventsGo := make([]string, 0, len(g.Entities))
	for _, entity := range g.Entities {
		evGo, hoisted, err := sl.ExprHoisted(entity.Entity)
		if err != nil {
			return fmt.Errorf("evento: %w", err)
		}
		emitLines(e, hoisted)
		eventsGo = append(eventsGo, "&"+evGo)
		sub.givenCount++
	}
	e.Block(fmt.Sprintf("if err := store.Append(%s.Background(), %s, []%s.Event{%s}); err != nil", contextAlias, idGo, runtimeAlias, strings.Join(eventsGo, ", ")), func() {
		e.Line("t.Fatalf(%q, %s, err)", "given %v: %v", idGo)
	})
	return nil
}

// emitUseCaseThenAssert emite UMA linha de "then { ... }" (§22.2): "Subject
// emitted Evento" (por índice estático, ver ucSubject), "committed"/
// "rolledback" (err == nil / err != nil — ver a doc do arquivo sobre por
// que isso BASTA para o runtime in-memory) ou "error Name" (errors.Is,
// mesma forma de §22.1).
func emitUseCaseThenAssert(e *emit.Emitter, sl *lower.StmtLowerer, a *ast.ThenAssert, subs *ucSubjects, errorsAlias, reflectAlias, contextAlias, runtimeAlias string) error {
	switch {
	case a.Error != "":
		e.Block(fmt.Sprintf("if !%s.Is(err, Err%s)", errorsAlias, a.Error), func() {
			e.Line("t.Fatalf(%q, err)", "esperava errors.Is(err, Err"+a.Error+"), got: %v")
		})
		return nil

	case a.Verb == "committed":
		e.Block("if err != nil", func() {
			e.Line("t.Fatalf(%q, err)", "esperava committed (sucesso), erro inesperado: %v")
		})
		return nil

	case a.Verb == "rolledback":
		e.Block("if err == nil", func() {
			e.Line("t.Fatalf(%q)", "esperava rolledback (erro), sucesso")
		})
		return nil

	case a.Verb == "emitted" && a.Subject != nil:
		idGo, idHoisted, err := ucSubjectID(sl, a.Subject)
		if err != nil {
			return fmt.Errorf("Subject: %w", err)
		}
		emitLines(e, idHoisted)
		sub := subs.get(idGo)
		if !sub.afterFetched {
			loadErrVar := sub.afterVar + "Err"
			e.Line("%s, %s := store.Load(%s.Background(), %s)", sub.afterVar, loadErrVar, contextAlias, idGo)
			// idGo é texto Go (ex. `"W1"`, com aspas) — nunca concatenado
			// direto numa string de mensagem (quebraria a sintaxe); sempre
			// via strconv.Quote sobre o TEXTO JÁ MONTADO (ver a doc do
			// arquivo), preservando "%v"/"%d" literais para o t.Fatalf de
			// verdade consumir em tempo de execução.
			loadMsg := strconv.Quote(fmt.Sprintf("Load(%s): %%v", idGo))
			e.Block(fmt.Sprintf("if %s != nil", loadErrVar), func() {
				e.Line("t.Fatalf(%s, %s)", loadMsg, loadErrVar)
			})
			sub.afterFetched = true
		}
		wantGo, hoisted, err := sl.ExprHoisted(a.Object)
		if err != nil {
			return fmt.Errorf("emitted: %w", err)
		}
		emitLines(e, hoisted)
		idx := sub.givenCount + sub.emittedIdx
		sub.emittedIdx++
		wantVar := fmt.Sprintf("%sWant%d", sub.afterVar, sub.emittedIdx)
		e.Line("%s := &%s", wantVar, wantGo)
		countMsg := strconv.Quote(fmt.Sprintf("esperava ao menos %d evento(s) novo(s) em %s, got %%d", idx+1, idGo))
		e.Block(fmt.Sprintf("if len(%s) <= %d", sub.afterVar, idx), func() {
			e.Line("t.Fatalf(%s, len(%s))", countMsg, sub.afterVar)
		})
		// EventMeta (AggregateID/Sequence/Timestamp) é carimbada por
		// store.Append no momento da escrita (rtsrc/event.go.txt) — o então
		// declarado (§22) nunca a conhece de antemão, então a asserção
		// compara só o PAYLOAD de negócio: zera a metadata do evento
		// persistido antes do DeepEqual (SetMeta, do próprio runtime.Event —
		// genérico, não precisa saber o tipo concreto do evento).
		e.Line("%s[%d].SetMeta(%s.EventMeta{})", sub.afterVar, idx, runtimeAlias)
		eqMsg := strconv.Quote(fmt.Sprintf("%s[%d]: got %%+v, want %%+v", idGo, idx))
		e.Block(fmt.Sprintf("if !%s.DeepEqual(%s[%d], %s)", reflectAlias, sub.afterVar, idx, wantVar), func() {
			e.Line("t.Fatalf(%s, %s[%d], %s)", eqMsg, sub.afterVar, idx, wantVar)
		})
		return nil

	default:
		return fmt.Errorf("forma de then não suportada nesta fase de H4 (verbo %q)", a.Verb)
	}
}

// emitLines emite cada linha de lines em e, na ordem — helper trivial para as
// linhas hoisted que sl.ExprHoisted devolve (ver a doc do arquivo).
func emitLines(e *emit.Emitter, lines []string) {
	for _, ln := range lines {
		e.Line("%s", ln)
	}
}

// scenarioFuncName deriva um nome de função Go único a partir do nome do Test
// e do texto livre do scenario (ex. "depósito numa carteira ativa" ->
// "TestWallet_DepositoNumaCarteiraAtiva"). used desambigua colisões
// (determinístico: sufixo "_2", "_3", ... pela ORDEM de aparição, NFR-13).
func scenarioFuncName(testName string, idx int, scenarioName string, used map[string]int) string {
	base := fmt.Sprintf("Test%s_%s", testName, slugify(scenarioName))
	used[base]++
	if n := used[base]; n > 1 {
		return fmt.Sprintf("%s_%d", base, n)
	}
	return base
}

// asciiFold dobra os acentos comuns do português (e alguns latinos) para o
// ASCII sem acento — slugify (abaixo) usa isto para que um nome de scenario
// em português vire um identificador Go válido e legível.
var asciiFold = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
	'Á': 'A', 'À': 'A', 'Â': 'A', 'Ã': 'A', 'Ä': 'A',
	'É': 'E', 'È': 'E', 'Ê': 'E', 'Ë': 'E',
	'Í': 'I', 'Ì': 'I', 'Î': 'I', 'Ï': 'I',
	'Ó': 'O', 'Ò': 'O', 'Ô': 'O', 'Õ': 'O', 'Ö': 'O',
	'Ú': 'U', 'Ù': 'U', 'Û': 'U', 'Ü': 'U',
	'Ç': 'C', 'Ñ': 'N',
}

// slugify converte um texto livre (nome de scenario) num fragmento de
// identificador Go em PascalCase: dobra acentos (asciiFold), descarta
// qualquer caractere que não seja letra/dígito ASCII e capitaliza a letra
// seguinte a cada separador descartado.
func slugify(name string) string {
	var b strings.Builder
	capNext := true
	for _, r := range name {
		if folded, ok := asciiFold[r]; ok {
			r = folded
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			if capNext {
				r = unicode.ToUpper(r)
				capNext = false
			}
			b.WriteRune(r)
		} else {
			capNext = true
		}
	}
	if b.Len() == 0 {
		return "Scenario"
	}
	return b.String()
}
