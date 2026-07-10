package codegen

import (
	"fmt"
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
// 31.1-4). Esta fase cobre só §22.1: um Test cujo Name resolve a um
// *ast.AggregateDecl DESTE módulo — dado o Test tem exatamente o mesmo nome
// do Aggregate, achado por symbols.SymbolTable (mesmo padrão de
// sema/rules_test_files.go:sagaSteps, que já faz esse casamento por nome
// para Saga). UseCase (§22.2, then commit/rollback)/Policy-Query (§22.3-4,
// emitted count)/Saga (§22.3, mock/fail-step) ficam para fases seguintes —
// um Test cujo nome não resolve a um Aggregate deste módulo, ou cujo cenário
// usa uma forma ainda não coberta (mock/fail/when event/then{...}), é um erro
// de geração claro agora, nunca gerado silenciosamente errado.
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

// EmitTests emite o Go de todas as ast.TestDecl de um módulo (H4, REQ-31) num
// único arquivo "<pkg>_test.go" — package pkg (interno, ver a doc do
// arquivo). aggregates é o mapa nome->AggregateDecl deste módulo (mesma forma
// que EmitUseCases já recebe) usado para resolver o alvo de cada Test (§22.1,
// ver a doc do arquivo). decls vazio devolve (nil, nil): o CHAMADOR
// (generateModuleFiles) decide, a partir disso, se escreve o arquivo.
func EmitTests(pkg string, decls []*ast.TestDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, aggregates map[string]*ast.AggregateDecl) ([]byte, error) {
	if len(decls) == 0 {
		return nil, nil
	}
	e := emit.New(pkg)
	e.Import("testing")
	needsErrors, needsReflect := scanTestNeeds(decls)
	var errorsAlias, reflectAlias string
	if needsErrors {
		errorsAlias = e.Import("errors")
	}
	if needsReflect {
		reflectAlias = e.Import("reflect")
	}
	runtimeAlias := e.Import(RuntimeImportPath)

	for _, t := range decls {
		agg, ok := aggregates[t.Name]
		if !ok {
			return nil, fmt.Errorf("Test %s: alvo não resolve a um Aggregate deste módulo — só cenários de Aggregate (§22.1) são suportados nesta fase de H4", t.Name)
		}
		if len(t.Properties) > 0 {
			return nil, fmt.Errorf("Test %s: \"property\" (§22.5) ainda não é suportado nesta fase de H4", t.Name)
		}
		if err := emitAggregateTestDecl(e, t, agg, model, tab, module, reg, runtimeAlias, errorsAlias, reflectAlias); err != nil {
			return nil, fmt.Errorf("Test %s: %w", t.Name, err)
		}
	}
	return e.Bytes()
}

// scanTestNeeds varre decls procurando por formas de "then" que exigem um
// import condicional (evita "importado e não usado" quando nenhum cenário de
// nenhum Test do módulo usa aquela forma — mesmo padrão de EmitMetrics,
// decl_metric.go, runtimeAlias lazy).
func scanTestNeeds(decls []*ast.TestDecl) (needsErrors, needsReflect bool) {
	for _, t := range decls {
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
		}
	}
	return needsErrors, needsReflect
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
		if err := emitAggregateGivenEntity(e, sl, entity, agg, applyByEvent, stateFields, receiver); err != nil {
			return err
		}
	}
	return nil
}

// emitAggregateGivenEntity processa uma entidade de "given [..., Entidade,
// ...]": Entity precisa ser uma construção de Event (CallExpr) — devolve erro
// claro para qualquer outra forma.
func emitAggregateGivenEntity(e *emit.Emitter, sl *lower.StmtLowerer, entity *ast.GivenEntity, agg *ast.AggregateDecl, applyByEvent map[string]bool, stateFields map[string]*ast.Field, receiver string) error {
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
