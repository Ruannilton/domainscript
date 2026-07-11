package codegen

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"domainscript/ast"
	"domainscript/astutil"
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
// 31.1-4). Esta fase cobre §22.1 (Aggregate), §22.2 (UseCase), §22.3 (Saga,
// mock/fail-step), §22.4 (Policy/Query, ver "--- Policy/Query" abaixo), §22.5
// (property, só sobre Aggregate — ver "--- property" abaixo) e §22.6
// (Fixture): um Test cujo Name resolve a um *ast.AggregateDecl,
// *ast.UseCaseDecl, *ast.SagaDecl OU *ast.PolicyDecl DESTE módulo — achado
// por casamento de nome exato contra o mapa recebido (mesmo padrão de
// sema/rules_test_files.go:sagaSteps, que já faz esse casamento por nome
// para Saga) — cada ast.PropertyDecl de um Test que resolveu a um Aggregate
// (ver a doc de emitAggregatePropertyDecls), e cada *ast.FixtureDecl
// "Subject from [eventos]" (helper reusável, ver a doc de emitFixtureDecl).
// Um Test cujo nome não resolve a um Aggregate, UseCase, Saga NEM Policy
// deste módulo, ou cujo cenário/property usa uma forma ainda não coberta, é
// um erro de geração claro agora, nunca gerado silenciosamente errado.
//
// --- Policy/Query (§22.4) ---
//
// "given <binding> [...]" semeia o runtime.Collection[T] de pacote que o
// "list T .../count T ..." da Policy sob teste lê (T = a cabeça de cada
// entidade, ex. "Ticket" — mesmo var que decl_policy.go declara e roteia via
// WithPerAggregateStore, ver a doc de lá; o nome do var é recalculado aqui
// pela MESMA função, policyCollectionVarName, nunca reimplementado). "when
// event Evento(...)" invoca a função gerada da Policy DIRETO (ex.
// "RefundAllOnEventCancelled(ctx, &ev)"), como uma CAIXA-PRETA — o mesmo
// espírito de §22.1/22.2 ("quando a função gerada É o alvo, chame-a
// direto"), nunca via Dispatcher.Publish (o Dispatcher, aqui, é o SEAM DE
// SAÍDA de uma Policy — o que "emit" escreve — não sua entrada). "then {
// emitted Evento(...), emitted count N }" reatribui "policyDispatcher" (var
// de pacote que decl_policy.go declara e Wire normalmente escreve, ver a doc
// de lá) para um runtime.NewDispatcher() PRÓPRIO deste cenário, com um
// Subscribe por tipo de Event que a Policy EMITE ESTATICAMENTE (varredura do
// PRÓPRIO corpo da Policy via policyEmittedEventNames — não do "then" do
// cenário, que sozinho não bastaria para um "then" só com "emitted count
// N"), coletando os eventos publicados numa slice local; "emitted
// Evento(...)" então busca, na slice, ALGUM evento estruturalmente igual
// (reflect.DeepEqual) — ORDEM-INDEPENDENTE, não por índice estático como
// "Subject emitted" de §22.2: uma Policy varre um Collection[T] cuja ordem é
// só a de inserção do given, sem garantia de negócio alguma sobre a ORDEM de
// emissão entre itens distintos, então comparar por conjunto é a leitura
// mais fiel do "then" declarativo do spec. Limitação documentada, deliberada
// e narrow (mesmo espírito de "Subject emitted"/"compensated" em UseCase/
// Saga): a checagem é de MEMBRO (não multiset) — duas asserções "emitted X"
// idênticas no mesmo cenário poderiam ambas casar contra o MESMO evento
// publicado sem detectar duplicata ausente; não exercitado pela fixture
// desta fase (cada "emitted" pede um evento distinto).
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
// doc do arquivo). aggregates/usecases/sagas/policies são os mapas nome->Decl
// deste módulo (mesma forma que EmitUseCases já recebe) usados para resolver o
// alvo de cada Test (§22.1/22.2/22.3/22.4, ver a doc do arquivo) — aggregates
// checado primeiro, depois usecases, depois sagas, depois policies (as quatro
// categorias não colidem: um nome de módulo não nomeia duas ao mesmo tempo).
// adapterByName (H4, REQ-31.3) é o registry de Adapter deste módulo (mesmo mapa
// que EmitPolicies/EmitUseCases/EmitSagas já recebem) — usado só pelo caminho
// de Saga, para resolver o alvo de "mock Target returns ..." (ver
// emitSagaMock). fixtures (§22.6, ver a doc de emitFixtureDecl) viram helpers
// "func fixture<Nome>(t *testing.T) *<AggType>" ao lado dos Test — reusam o
// MESMO mapa aggregates para resolver o Aggregate do Subject. decls E fixtures
// vazios devolvem (nil, nil): o CHAMADOR (generateModuleFiles) decide, a partir
// disso, se escreve o arquivo.
func EmitTests(pkg string, decls []*ast.TestDecl, fixtures []*ast.FixtureDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, aggregates map[string]*ast.AggregateDecl, usecases map[string]*ast.UseCaseDecl, sagas map[string]*ast.SagaDecl, policies map[string]*ast.PolicyDecl, adapterByName map[string]*ast.AdapterDecl) ([]byte, error) {
	if len(decls) == 0 && len(fixtures) == 0 {
		return nil, nil
	}
	e := emit.New(pkg)
	e.Import("testing")
	needsErrors, needsReflect, needsContext, needsRand := scanTestNeeds(decls, aggregates, usecases, sagas, policies)
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
	if needsRand {
		// Os helpers de property (§22.5, ver gentest_property.go) são
		// package-level, únicos por arquivo — emitidos aqui, uma vez, ANTES
		// de qualquer func TestX que os use (Go não exige ordem de
		// declaração, mas isso deixa o arquivo gerado mais legível: helpers
		// primeiro, testes depois). emitPropertyHelpers registra o import de
		// "math/rand" (Emitter.Import é idempotente por path — todo outro
		// ponto que precisa do alias, ex. emitAggregatePropertyDecls, chama
		// e.Import("math/rand") de novo e recebe o MESMO alias, sem precisar
		// que este seja passado adiante como parâmetro).
		emitPropertyHelpers(e)
	}

	for _, t := range decls {
		used := make(map[string]int)
		if agg, ok := aggregates[t.Name]; ok {
			if err := emitAggregateTestDecl(e, t, agg, model, tab, module, reg, runtimeAlias, errorsAlias, reflectAlias, used); err != nil {
				return nil, fmt.Errorf("Test %s: %w", t.Name, err)
			}
			if err := emitAggregatePropertyDecls(e, t, agg, model, tab, module, reg, runtimeAlias, used); err != nil {
				return nil, fmt.Errorf("Test %s: %w", t.Name, err)
			}
			continue
		}
		if uc, ok := usecases[t.Name]; ok {
			if len(t.Properties) > 0 {
				return nil, fmt.Errorf("Test %s: \"property\" (§22.5) só é suportado sobre um Aggregate nesta fase de H4 (Test %s resolve a um UseCase, ver a doc do arquivo)", t.Name, t.Name)
			}
			if err := emitUseCaseTestDecl(e, t, uc, model, tab, module, reg, runtimeAlias, errorsAlias, reflectAlias, contextAlias); err != nil {
				return nil, fmt.Errorf("Test %s: %w", t.Name, err)
			}
			continue
		}
		if saga, ok := sagas[t.Name]; ok {
			if len(t.Properties) > 0 {
				return nil, fmt.Errorf("Test %s: \"property\" (§22.5) só é suportado sobre um Aggregate nesta fase de H4 (Test %s resolve a uma Saga, ver a doc do arquivo)", t.Name, t.Name)
			}
			if err := emitSagaTestDecl(e, t, saga, model, tab, module, reg, adapterByName, runtimeAlias, errorsAlias, reflectAlias, contextAlias); err != nil {
				return nil, fmt.Errorf("Test %s: %w", t.Name, err)
			}
			continue
		}
		if policy, ok := policies[t.Name]; ok {
			if len(t.Properties) > 0 {
				return nil, fmt.Errorf("Test %s: \"property\" (§22.5) só é suportado sobre um Aggregate nesta fase de H4 (Test %s resolve a uma Policy, ver a doc do arquivo)", t.Name, t.Name)
			}
			if err := emitPolicyTestDecl(e, t, policy, model, tab, module, reg, runtimeAlias, errorsAlias, reflectAlias, contextAlias); err != nil {
				return nil, fmt.Errorf("Test %s: %w", t.Name, err)
			}
			continue
		}
		return nil, fmt.Errorf("Test %s: alvo não resolve a um Aggregate, UseCase, Saga nem Policy deste módulo — só §22.1/22.2/22.3/22.4/22.5 são suportados nesta fase de H4", t.Name)
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
// Test resolve a um UseCase, Saga OU Policy (§22.2 sempre usa context.
// Background(); §22.3 sempre chama <base>RunSteps(context.Background(),
// state); §22.4 sempre usa "ctx := context.Background()" — ver a doc do
// arquivo) — independente da forma dos cenários dentro dele. needsErrors
// também é true quando ao menos um scenario de Saga declara "fail step"
// (emite errors.New — ver emitSagaFailStep); needsReflect também quando ao
// menos um ThenAssert usa "compensated [...]" (reflect.DeepEqual sobre a
// ordem de compensação — ver emitSagaThenAssert) OU "emitted Evento(...)"
// com Object != nil (§22.2 "Subject emitted"/§22.4 "emitted", ver
// emitPolicyThenAssert — as duas formas compartilham o mesmo sinal, já que
// ambas comparam via reflect.DeepEqual). needsRand é true quando ao menos um
// Test resolve a um Aggregate E declara ao menos uma property (§22.5, ver
// gentest_property.go) — "math/rand" e os dois helpers package-level só são
// emitidos nesse caso.
func scanTestNeeds(decls []*ast.TestDecl, aggregates map[string]*ast.AggregateDecl, usecases map[string]*ast.UseCaseDecl, sagas map[string]*ast.SagaDecl, policies map[string]*ast.PolicyDecl) (needsErrors, needsReflect, needsContext, needsRand bool) {
	for _, t := range decls {
		if agg, isAgg := aggregates[t.Name]; isAgg {
			if len(t.Properties) > 0 && agg != nil {
				needsRand = true
			}
		} else if _, isUC := usecases[t.Name]; isUC {
			needsContext = true
		} else if _, isSaga := sagas[t.Name]; isSaga {
			needsContext = true
		} else if _, isPolicy := policies[t.Name]; isPolicy {
			needsContext = true
		}
		for _, sc := range t.Scenarios {
			if len(sc.Fails) > 0 {
				needsErrors = true
			}
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
				if a.Verb == "emitted" && a.Object != nil {
					needsReflect = true
				}
				if a.Verb == "compensated" {
					needsReflect = true
				}
			}
		}
	}
	return needsErrors, needsReflect, needsContext, needsRand
}

// emitAggregateTestDecl emite um func TestX por scenario de t (§22.1). used
// acumula os nomes de função já atribuídos (scenarioFuncName) — o CHAMADOR
// (EmitTests) cria um único mapa por Test e o compartilha com
// emitAggregatePropertyDecls (mesmo Test, mesmo prefixo "Test<Nome>_..."),
// para que um scenario e uma property de nomes iguais não colidam no mesmo
// nome de função Go.
func emitAggregateTestDecl(e *emit.Emitter, t *ast.TestDecl, agg *ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias, errorsAlias, reflectAlias string, used map[string]int) error {
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

// --- Saga (§22.3, REQ-31.3) ---
//
// --- RunSteps direto, não a função de entrada (decisão documentada) ---
//
// A forma "natural" seria "when Cmd(...)" invocar a função de entrada
// gerada (ex. "PurchaseTickets(ctx, cmd)", decl_saga.go) — mas a entrada
// "await" (emitSagaAwaitEntry) devolve só (*State, error): NENHUMA forma
// pública expõe runtime.SagaResult (Compensated/Unrecoverable/FinalState),
// exatamente o que "saga compensated"/"compensated [...]" (REQ-31.3)
// precisam observar. A ÚNICA função que expõe SagaResult é <base>RunSteps —
// a MESMA que a entrada pública chama por baixo (dentro de uma goroutine,
// para "await", ver a doc de emitSagaAwaitEntry). Chamar <base>RunSteps
// DIRETO (mesmo padrão que decl_saga_test.go's próprios testes
// comportamentais já usam — TestPurchaseTicketsFailureCompensatesInReverse...)
// reproduz IDENTICAMENTE a orquestração real, só pulando o wrapper de
// goroutine+timeout (que não está sob teste aqui) — e evita de graça a
// janela de corrida entre um encerramento por timeout e a goroutine em
// segundo plano que emitSagaAwaitEntry documenta. "when Cmd(...)" ainda
// constrói o Command de verdade (Lowerer.ExprHoisted sobre o CallExpr) —
// só a INVOCAÇÃO final troca de "entrada pública" para "RunSteps direto".
//
// --- given: seed direto do state (mesma filosofia de §22.1) ---
//
// "given state { campo: valor }" (a ÚNICA forma suportada aqui — uma Saga
// não tem Event nem EventStore, ver a doc de decl_saga.go sobre "state" ser
// o único receptor) sobrescreve "state.Campo" DIRETO, na ordem declarada,
// ANTES do seed de "when" — um given que colida em nome com um campo do
// Command é uma escolha rara e não exercitada pela fixture desta fase; a
// ORDEM escolhida (given primeiro, cmd depois) segue a leitura cronológica
// natural "dado que, quando" — o cmd vence em caso de colisão, documentado
// aqui por não haver um 2º given para desempatar como em §22.1.
//
// --- mock: instala o seam, mas SEMPRE sucede (decisão documentada) ---
//
// Call<Nome>/Notify<Nome> (decl_io.go, REQ-25.3) devolvem só "error" — não
// existe hoje um retorno estruturado que o corpo de um passo possa
// inspecionar ("result = PaymentRequest(...)" não é lowerizável, só
// ExprStmt solto — ver notifyOrCallStmt, lower/stmt.go). Por isso "mock
// Target returns X" AQUI constrói X (hoisted — prova que a expressão é Go
// válido, o mesmo tratamento que um "given"/"then" dá a qualquer expressão)
// mas não pode, por si só, desviar o fluxo de negócio: a var de pacote que
// Call<Nome>/Notify<Nome> invocam por baixo (adapterCallVarName, decl_io.go)
// é substituída por uma closure que registra a chamada (para "then { called
// Target }") e SEMPRE sucede (nil error) — simular uma falha causada por um
// Adapter é papel de "fail step ... with ..." (efeito preciso e determinado
// sobre RunSaga), não de "mock ... returns ...". Estender Adapter com um
// retorno estruturado (para que um "PaymentResult declinado" pudesse, por
// si só, desviar o passo) é trabalho futuro — mesma lacuna já documentada em
// decl_io.go sobre o retorno de um Adapter ser só "error" hoje.
//
// --- fail step: erro sintético de INFRA (nunca BusinessError) ---
//
// "fail step Name with Err" troca <base>Steps[idx].Up por uma função que
// devolve um erro sintético via errors.New — NUNCA runtime.BusinessError:
// simula uma falha de INFRAESTRUTURA, o único tipo de falha que a forma
// "with InfraError" do spec (§19: "Infraestrutura | Nunca no domínio")
// descreve. Err (o "With" de ast.FailStep) é texto livre — sema
// (rules_test_files.go) só valida que Step nomeia um step real desta Saga,
// nunca valida With contra nenhum Error declarado (não HÁ Error de infra no
// domínio) — With só é embutido na mensagem do erro sintético, para um
// t.Fatalf apontar a causa simulada quando uma asserção falhar.
//
// --- Reset entre cenários: <base>StepsOriginal ---
//
// <base>Steps (decl_saga.go) é uma var de PACOTE — mock/fail-step de UM
// cenário reatribui .Up/a var de Adapter, e essa mutação SOBREVIVERIA para
// o próximo cenário (os testes deste pacote rodam sequencialmente, sem
// t.Parallel(), então não há corrida — mas também não há isolamento
// automático). <base>StepsOriginal (uma var de pacote emitida UMA VEZ,
// capturada por um inicializador — roda ANTES de qualquer func de teste,
// preservando os passos REAIS) é a cópia pristina que CADA cenário restaura
// a partir dela, como a PRIMEIRA linha do seu corpo, antes de aplicar
// mock/fail-step — garante que a mutação de um cenário nunca vaze para o
// próximo, sem precisar de nenhuma ordem específica de execução dos testes.
//
// --- Segurança de dados sob "mode await" (goroutine) ---
//
// Uma reatribuição de <base>Steps[i]/de uma var de Adapter, feita pelo
// teste ANTES de chamar RunSteps, sempre HAPPENS-BEFORE a leitura dessas
// vars dentro de RunSaga: RunSteps não abre goroutine nenhuma (ao contrário
// da entrada pública "await" — mais um motivo para chamá-la direto, ver
// acima) — a leitura acontece na MESMA goroutine que a escrita, sequencial,
// sem corrida possível.
//
// --- Formas de then NÃO cobertas nesta fase (documentado) ---
//
// "Subject emitted Evento"/"Subject released" (a Saga tocando um Aggregate
// via Event, ex. "Order emitted OrderCancelled" do spec §22.3) ficam para
// uma fatia futura: um passo de Saga não tem acesso a nenhum
// runtime.Tx/Store/EventStore (só "state", ver a doc de decl_saga.go) —
// persistir um Event a partir de um passo exigiria um mecanismo novo
// (analógico ao ucSubjects de §22.2, mas para dentro de um passo de Saga),
// maior que o resto desta fatia; nenhuma fixture real (wallet/shop) tem uma
// Saga que emite eventos hoje. Um Test cujo cenário usa essas formas
// recebe um erro de geração claro, nunca gerado silenciosamente errado.

// sagaScenarioState é o estado acumulado ao emitir os cenários de UM Test
// de Saga: stepIndex resolve "fail step Name" para o índice ordinal em
// decl.Steps (mesma ordem de <base>Steps, decl_saga.go); backedUp marca se
// "<base>StepsOriginal" já foi emitida (uma vez por Saga, não por
// scenario).
type sagaScenarioState struct {
	base       string
	stateType  string
	runStepsFn string
	stepIndex  map[string]int
	seedLines  []string
}

// emitSagaTestDecl emite um func TestX por scenario de t (§22.3, REQ-31).
func emitSagaTestDecl(e *emit.Emitter, t *ast.TestDecl, saga *ast.SagaDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, adapterByName map[string]*ast.AdapterDecl, runtimeAlias, errorsAlias, reflectAlias, contextAlias string) error {
	stateFields, err := sagaStateFields(saga)
	if err != nil {
		return err
	}
	cmdDecl, err := resolveSagaCommand(tab, module, saga.Handles)
	if err != nil {
		return err
	}

	st := &sagaScenarioState{
		base:       sagaBase(saga.Name),
		stateType:  sagaStateTypeName(saga),
		stepIndex:  make(map[string]int, len(saga.Steps)),
		seedLines:  sagaSeedFromCommandLines(stateFields, cmdDecl.Fields),
		runStepsFn: sagaBase(saga.Name) + "RunSteps",
	}
	for i, s := range saga.Steps {
		if s != nil {
			st.stepIndex[s.Name] = i
		}
	}

	e.Line("")
	e.Line("// %sStepsOriginal preserva os passos REAIS de %sSteps (decl_saga.go) —", st.base, st.base)
	e.Line("// cada cenário desta suíte restaura a partir daqui antes de aplicar")
	e.Line("// mock/fail step (§22.3, REQ-31.3): garante que a mutação de UM cenário")
	e.Line("// nunca vaze para o próximo (ver a doc do arquivo).")
	e.Line("var %sStepsOriginal = append([]%s.Step[%s](nil), %sSteps...)", st.base, runtimeAlias, st.stateType, st.base)

	used := make(map[string]int)
	for i, sc := range t.Scenarios {
		fn := scenarioFuncName(t.Name, i, sc.Name, used)

		env := lower.New(model, tab, module)
		l := lower.NewLowerer(env, reg, runtimeAlias)
		sl := lower.NewStmtLowerer(l, e, lower.StmtContext{Panics: true})

		e.Line("")
		e.Line("// %s prova o cenário %q de Test %s (§22.3, REQ-31).", fn, sc.Name, t.Name)
		var bodyErr error
		e.Block(fmt.Sprintf("func %s(t *testing.T)", fn), func() {
			bodyErr = emitSagaScenarioBody(e, sl, sc, saga, st, adapterByName, runtimeAlias, errorsAlias, reflectAlias, contextAlias)
		})
		if bodyErr != nil {
			return fmt.Errorf("scenario %q: %w", sc.Name, bodyErr)
		}
	}
	return nil
}

// emitSagaScenarioBody emite o corpo de um func TestX de Saga: reset da
// tabela de passos + mock + fail step + given (seed de state) + when
// (Command, semeia state por nome) + res := RunSteps(...) + then (ver a
// doc do arquivo para o raciocínio de cada peça).
func emitSagaScenarioBody(e *emit.Emitter, sl *lower.StmtLowerer, sc *ast.ScenarioDecl, saga *ast.SagaDecl, st *sagaScenarioState, adapterByName map[string]*ast.AdapterDecl, runtimeAlias, errorsAlias, reflectAlias, contextAlias string) error {
	if sc.When == nil {
		return fmt.Errorf("cenário sem \"when\"")
	}
	if sc.When.IsEvent {
		return fmt.Errorf("\"when event ...\" (§22.4) é de Policy/Query — Test %s é de Saga, fora de escopo de H4", saga.Name)
	}
	if sc.Then == nil {
		return fmt.Errorf("cenário sem \"then\"")
	}
	if sc.Then.Error != "" || len(sc.Then.Events) > 0 {
		return fmt.Errorf("\"then [eventos]\"/\"then error\" (fora de um bloco {...}) são de Aggregate — Test %s é de Saga, use \"then { ... }\" (§22.3)", saga.Name)
	}

	// Reset: desfaz qualquer mock/fail-step de um cenário ANTERIOR (mesma
	// suíte, mesmo pacote — ver a doc do arquivo).
	e.Line("%sSteps = append([]%s.Step[%s](nil), %sStepsOriginal...)", st.base, runtimeAlias, st.stateType, st.base)

	callFlags := make(map[string]string) // nome do Adapter -> var Go "<nome>Called"
	for _, m := range sc.Mocks {
		if err := emitSagaMock(e, sl, m, adapterByName, callFlags, contextAlias); err != nil {
			return fmt.Errorf("mock: %w", err)
		}
	}
	for _, f := range sc.Fails {
		if err := emitSagaFailStep(e, st, f, errorsAlias, contextAlias); err != nil {
			return fmt.Errorf("fail step: %w", err)
		}
	}

	e.Line("state := &%s{}", st.stateType)
	for _, g := range sc.Givens {
		if err := emitSagaGiven(e, sl, g, saga); err != nil {
			return fmt.Errorf("given: %w", err)
		}
	}

	call, ok := sc.When.Action.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("when: esperava uma construção de Command (%q(...)), got %T", saga.Handles, sc.When.Action)
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok || id.Name != saga.Handles {
		return fmt.Errorf("when: esperava o Command %q (o que a Saga %s handles), got %T", saga.Handles, saga.Name, call.Fn)
	}
	cmdGo, hoisted, err := sl.ExprHoisted(call)
	if err != nil {
		return fmt.Errorf("when: %w", err)
	}
	emitLines(e, hoisted)
	e.Line("cmd := %s", cmdGo)
	emitSagaSeed(e, st.seedLines)

	e.Line("res := %s(%s.Background(), state)", st.runStepsFn, contextAlias)
	// _ = res garante que "res" está USADO mesmo quando TODO ThenAssert deste
	// cenário é "called ..." (o único verbo que não lê res — ver
	// emitSagaThenAssert): sem isto, um cenário só com "called" geraria "res
	// declared and not used", um erro de COMPILAÇÃO, não só um `go vet`
	// cosmético — achado sobre o Go de fato gerado, não uma hipótese.
	e.Line("_ = res")

	for _, a := range sc.Then.Asserts {
		if err := emitSagaThenAssert(e, a, saga, st, callFlags, runtimeAlias, reflectAlias); err != nil {
			return fmt.Errorf("then: %w", err)
		}
	}
	return nil
}

// emitSagaGiven aplica UMA GivenClause (§22.3): "given state {...}" semeia
// campos de state DIRETO (mesmo formato Go de emitStateOverlay, adaptado —
// aqui "state" É o receptor, sem um "receiver.state." aninhado, ao
// contrário de Aggregate). Outras formas (given [eventos]/given Subject
// from [...]/given binding [...]) não fazem sentido para Saga (uma Saga não
// tem Event/EventStore — ver a doc do arquivo) — erro de geração claro.
func emitSagaGiven(e *emit.Emitter, sl *lower.StmtLowerer, g *ast.GivenClause, saga *ast.SagaDecl) error {
	if g.State == nil || g.Entities != nil || g.Subject != nil || g.Binding != "" {
		return fmt.Errorf("\"given [eventos]\"/\"given Subject from [...]\"/\"given binding [...]\" não se aplicam a uma Saga — Test %s, use \"given state {...}\" (§22.3)", saga.Name)
	}
	for _, entry := range g.State.Entries {
		goExpr, hoisted, err := sl.ExprHoisted(entry.Value)
		if err != nil {
			return fmt.Errorf("state.%s: %w", entry.Key, err)
		}
		emitLines(e, hoisted)
		e.Line("state.%s = %s", goname.ExportField(entry.Key), goExpr)
	}
	return nil
}

// emitSagaMock instala um mock para "mock Target returns X" (§22.3):
// resolve Target a um Adapter deste módulo (adapterByName), constrói X
// (hoisted — ver a doc do arquivo sobre por que X não desvia o fluxo) e
// reatribui a var de pacote que Call<Nome>/Notify<Nome> invocam por baixo
// (adapterCallVarName, decl_io.go) para uma closure que registra a chamada
// (callFlags, para "then { called Target }") e sempre sucede.
func emitSagaMock(e *emit.Emitter, sl *lower.StmtLowerer, m *ast.MockClause, adapterByName map[string]*ast.AdapterDecl, callFlags map[string]string, contextAlias string) error {
	name := astutil.HeadName(m.Target)
	if name == "" {
		return fmt.Errorf("mock: esperava um Adapter nomeado, got %T", m.Target)
	}
	adapter, ok := adapterByName[name]
	if !ok {
		return fmt.Errorf("mock %s: não é um Adapter deste módulo (bug de geração — REQ-9/sema já deveriam ter barrado isso)", name)
	}
	fnVar := adapterCallVarName(adapter)
	if fnVar == "" {
		return fmt.Errorf("mock %s: Adapter sem forma HTTP nem FFI reconhecida (bug de geração)", name)
	}

	if m.Returns != nil {
		goExpr, hoisted, err := sl.ExprHoisted(m.Returns)
		if err != nil {
			return fmt.Errorf("mock %s: returns: %w", name, err)
		}
		emitLines(e, hoisted)
		// returns construído e validado (Go real) — Call<Nome>/Notify<Nome>
		// devolvem só error (REQ-25.3): o valor não influencia o fluxo de
		// negócio nesta fase (ver a doc do arquivo).
		e.Line("_ = %s", goExpr)
	}

	flagVar := strings.ToLower(name[:1]) + name[1:] + "Called"
	callFlags[name] = flagVar
	e.Line("var %s bool", flagVar)
	e.Block(fmt.Sprintf("%s = func(ctx %s.Context, n %s) error", fnVar, contextAlias, name), func() {
		e.Line("%s = true", flagVar)
		e.Line("return nil")
	})
	return nil
}

// emitSagaFailStep aplica "fail step Name with Err" (§22.3, ver a doc do
// arquivo): troca <base>Steps[idx].Up por uma função que devolve um erro
// SINTÉTICO (nunca runtime.BusinessError).
func emitSagaFailStep(e *emit.Emitter, st *sagaScenarioState, f *ast.FailStep, errorsAlias, contextAlias string) error {
	idx, ok := st.stepIndex[f.Step]
	if !ok {
		return fmt.Errorf("fail step %q: não é um step desta Saga (bug de geração — sema já deveria ter barrado isso)", f.Step)
	}
	msg := fmt.Sprintf("fail step %s: %s (simulado)", f.Step, f.With)
	e.Block(fmt.Sprintf("%sSteps[%d].Up = func(ctx %s.Context, state *%s) error", st.base, idx, contextAlias, st.stateType), func() {
		e.Line("return %s.New(%q)", errorsAlias, msg)
	})
	return nil
}

// emitSagaThenAssert emite UMA linha de "then { ... }" (§22.3): "saga
// compensated" (Verb=="saga", Object=Ident "compensated" —
// res.FinalState() == runtime.SagaCompensated), "compensated [steps]"
// (Verb=="compensated", List de Ident — compara res.Compensated, NA ORDEM,
// via reflect.DeepEqual) e "called Adapter" (Verb=="called", Object=Ident —
// o Adapter foi mockado E invocado nesta execução). "Subject emitted .../
// Subject released" ficam para uma fatia futura (ver a doc do arquivo).
func emitSagaThenAssert(e *emit.Emitter, a *ast.ThenAssert, saga *ast.SagaDecl, st *sagaScenarioState, callFlags map[string]string, runtimeAlias, reflectAlias string) error {
	switch {
	case a.Verb == "saga":
		obj, ok := a.Object.(*ast.Ident)
		if !ok || obj.Name != "compensated" {
			return fmt.Errorf("\"saga %v\" não suportado nesta fase de H4 (só \"saga compensated\")", a.Object)
		}
		e.Block(fmt.Sprintf("if res.FinalState() != %s.SagaCompensated", runtimeAlias), func() {
			e.Line("t.Fatalf(%q, res)", "esperava a Saga compensada (FinalState == SagaCompensated), got %+v")
		})
		return nil

	case a.Verb == "compensated":
		names := make([]string, 0, len(a.List))
		for _, el := range a.List {
			id, ok := el.(*ast.Ident)
			if !ok {
				return fmt.Errorf("\"compensated [...]\": esperava nomes de step (identificadores nus), got %T", el)
			}
			if _, ok := st.stepIndex[id.Name]; !ok {
				return fmt.Errorf("\"compensated [...]\": %q não é um step da Saga %s", id.Name, saga.Name)
			}
			names = append(names, id.Name)
		}
		quoted := make([]string, len(names))
		for i, n := range names {
			quoted[i] = strconv.Quote(n)
		}
		e.Line("wantCompensated := []string{%s}", strings.Join(quoted, ", "))
		// gotCompensated normaliza res.Compensated (nil no caminho 100% feliz —
		// RunSaga, rtsrc/saga.go.txt, nunca inicializa o slice quando o loop de
		// compensação não roda) para um slice NÃO nil antes de comparar: um
		// slice nil e um slice vazio são estruturalmente diferentes para
		// reflect.DeepEqual, o que quebraria "then { compensated [] }" — a
		// forma correta de provar "nenhum step foi compensado" (êxito total) —
		// mesmo com wantCompensated corretamente vazio.
		e.Line("gotCompensated := append([]string{}, res.Compensated...)")
		e.Block(fmt.Sprintf("if !%s.DeepEqual(gotCompensated, wantCompensated)", reflectAlias), func() {
			e.Line("t.Fatalf(%q, gotCompensated, wantCompensated)", "compensated: got %v, want %v (ordem REVERSA)")
		})
		return nil

	case a.Verb == "called":
		id, ok := a.Object.(*ast.Ident)
		if !ok {
			return fmt.Errorf("\"called ...\": esperava um Adapter nomeado, got %T", a.Object)
		}
		flagVar, ok := callFlags[id.Name]
		if !ok {
			return fmt.Errorf("\"called %s\": nenhum \"mock %s returns ...\" neste cenário (H4 exige mock antes de \"called\")", id.Name, id.Name)
		}
		e.Block(fmt.Sprintf("if !%s", flagVar), func() {
			e.Line("t.Fatalf(%q)", "esperava "+id.Name+" chamado, não foi")
		})
		return nil

	default:
		return fmt.Errorf("forma de then não suportada para Saga nesta fase de H4 (verbo %q)", a.Verb)
	}
}

// --- Policy/Query (§22.4, REQ-31.3) ---

// emitPolicyTestDecl emite um func TestX por scenario de t (§22.4). Mesma
// forma de emitUseCaseTestDecl/emitSagaTestDecl: um StmtLowerer NOVO por
// scenario (cada cenário é isolado — nenhum estado de lowering vaza entre
// eles). policyEmittedEventNames(policy) é calculado UMA VEZ aqui (o corpo da
// Policy não muda entre cenários do mesmo Test) e repassado a cada scenario —
// ver a doc do arquivo sobre por que a lista vem do CORPO da Policy, não do
// "then" de cada cenário individualmente.
func emitPolicyTestDecl(e *emit.Emitter, t *ast.TestDecl, policy *ast.PolicyDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias, errorsAlias, reflectAlias, contextAlias string) error {
	eventNames := policyEmittedEventNames(policy)

	used := make(map[string]int)
	for i, sc := range t.Scenarios {
		fn := scenarioFuncName(t.Name, i, sc.Name, used)

		env := lower.New(model, tab, module)
		l := lower.NewLowerer(env, reg, runtimeAlias)
		sl := lower.NewStmtLowerer(l, e, lower.StmtContext{Panics: true})

		e.Line("")
		e.Line("// %s prova o cenário %q de Test %s (§22.4, REQ-31).", fn, sc.Name, t.Name)
		var bodyErr error
		e.Block(fmt.Sprintf("func %s(t *testing.T)", fn), func() {
			bodyErr = emitPolicyScenarioBody(e, sl, sc, policy, eventNames, runtimeAlias, errorsAlias, reflectAlias, contextAlias)
		})
		if bodyErr != nil {
			return fmt.Errorf("scenario %q: %w", sc.Name, bodyErr)
		}
	}
	return nil
}

// policyEmittedEventNames devolve, na ordem de aparição no código-fonte
// (astutil.ForEachStmt visita em profundidade, incl. dentro de um "for" —
// determinístico por construção, sem precisar de sort), os nomes DISTINTOS
// de Event que policy.Execute emite ESTATICAMENTE ("emit Evento(...)", em
// qualquer profundidade — H4, §22.4). emitPolicyScenarioBody usa isto para
// saber a quais tipos assinar "policyDispatcher" antes de invocar a Policy
// sob teste (ver a doc do arquivo): calculado do CORPO REAL da Policy, não
// do "then" do scenario, porque um scenario cujo "then" só tem "emitted
// count N" (sem nenhuma "emitted Evento(...)" explícita) não revelaria tipo
// nenhum sozinho.
func policyEmittedEventNames(policy *ast.PolicyDecl) []string {
	seen := make(map[string]bool)
	var names []string
	astutil.ForEachStmt(policy.Execute, func(s ast.Stmt) {
		em, ok := s.(*ast.EmitStmt)
		if !ok {
			return
		}
		name := astutil.HeadName(em.Call)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	})
	return names
}

// emitPolicyScenarioBody emite o corpo de um func TestX de Policy: "given
// <binding> [...]" (semeia o Collection[T] de pacote que a Policy sob teste
// lê) + "when event Evento(...)" (chama a função gerada da Policy DIRETO,
// como caixa-preta) + "then { ... }" (ver a doc do arquivo).
func emitPolicyScenarioBody(e *emit.Emitter, sl *lower.StmtLowerer, sc *ast.ScenarioDecl, policy *ast.PolicyDecl, eventNames []string, runtimeAlias, errorsAlias, reflectAlias, contextAlias string) error {
	if len(sc.Mocks) > 0 || len(sc.Fails) > 0 {
		return fmt.Errorf("\"mock\"/\"fail step\" (§22.3) só cabem em cenário de Saga — Test %s é de Policy, use \"given\"/\"when event\"/\"then { ... }\" (§22.4)", policy.Name)
	}
	if sc.When == nil {
		return fmt.Errorf("cenário sem \"when\"")
	}
	if !sc.When.IsEvent {
		return fmt.Errorf("\"when Action(...)\" (sem \"event\") é de Aggregate/UseCase/Saga — Test %s é de Policy, use \"when event Evento(...)\" (§22.4)", policy.Name)
	}
	if sc.Then == nil {
		return fmt.Errorf("cenário sem \"then\"")
	}
	if sc.Then.Error != "" || len(sc.Then.Events) > 0 {
		return fmt.Errorf("\"then [eventos]\"/\"then error\" (fora de um bloco {...}) são de Aggregate — Test %s é de Policy, use \"then { ... }\" (§22.4)", policy.Name)
	}

	e.Line("ctx := %s.Background()", contextAlias)

	// Reset: cada Collection[T] referenciada por ESTE cenário é um var de
	// PACOTE (decl_policy.go) — compartilhado por TODOS os func TestX deste
	// arquivo, na MESMA execução de "go test" (Go roda os testes de um
	// pacote sequencialmente, no mesmo processo, por padrão). Sem isto, um
	// item semeado pelo cenário anterior sobreviveria e contaminaria a
	// contagem/filtragem deste — mesmo raciocínio de "%sStepsOriginal" em
	// emitSagaScenarioBody, adaptado: aqui reatribuímos o var de pacote a um
	// runtime.NewMemoryCollection[T]() NOVO, em vez de restaurar a partir de
	// uma cópia (Collection[T] não tem um "estado original" para restaurar —
	// cada scenario semeia do zero).
	emitPolicyGivenReset(e, sc, runtimeAlias)

	itemCounter := 0
	for _, g := range sc.Givens {
		if err := emitPolicyGiven(e, sl, g, &itemCounter); err != nil {
			return fmt.Errorf("given: %w", err)
		}
	}

	// scenarioNeedsDispatcher: só monta policyDispatcher/o coletor "published"
	// quando ESTE cenário de fato lê eventos publicados ("emitted ...",
	// qualquer forma) — um cenário cujo "then" só verifica "error Name" não
	// paga o custo de nenhuma dessas linhas (mesmo espírito das guardas
	// needsErrors/needsRand no topo do arquivo, agora por SCENARIO).
	scenarioNeedsDispatcher := false
	for _, a := range sc.Then.Asserts {
		if a.Verb == "emitted" {
			scenarioNeedsDispatcher = true
			break
		}
	}
	if scenarioNeedsDispatcher {
		if len(eventNames) == 0 {
			return fmt.Errorf("\"then { emitted ... }\": a Policy %s não tem nenhum \"emit\" estático no execute — nada para assinar/comparar", policy.Name)
		}
		emitPolicyDispatcherSetup(e, eventNames, runtimeAlias, contextAlias)
	}

	call, ok := sc.When.Action.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("when event: esperava uma construção de Event (\"Nome(...)\"), got %T", sc.When.Action)
	}
	evGo, hoisted, err := sl.ExprHoisted(call)
	if err != nil {
		return fmt.Errorf("when event: %w", err)
	}
	emitLines(e, hoisted)
	e.Line("ev := %s", evGo)

	// hasErrorAssert: se algum ThenAssert deste bloco verifica "error Name",
	// o err da chamada é consumido LÁ (errors.Is) — o auto-Fatalf-em-erro
	// abaixo fica de fora para não competir com essa checagem explícita
	// (mesmo raciocínio de emitAggregateThen: só uma das duas formas cabe por
	// cenário). Sem nenhum "error Name", err SEMPRE precisa ser consumido
	// (senão "declared and not used") — o auto-Fatalf cobre isso E expressa
	// a expectativa padrão de sucesso (nenhuma forma de §22.4 documentada no
	// spec testa falha de Policy).
	hasErrorAssert := false
	for _, a := range sc.Then.Asserts {
		if a.Error != "" {
			hasErrorAssert = true
			break
		}
	}
	e.Line("err := %s(ctx, &ev)", policy.Name)
	if !hasErrorAssert {
		e.Block("if err != nil", func() {
			e.Line("t.Fatalf(%q, err)", "esperava sucesso ao invocar "+policy.Name+", erro inesperado: %v")
		})
	}

	for idx, a := range sc.Then.Asserts {
		if err := emitPolicyThenAssert(e, sl, a, idx, scenarioNeedsDispatcher, errorsAlias, reflectAlias); err != nil {
			return fmt.Errorf("then: %w", err)
		}
	}
	return nil
}

// emitPolicyDispatcherSetup instala, ANTES de invocar a Policy sob teste, um
// runtime.Dispatcher PRÓPRIO deste cenário em "policyDispatcher" (o var de
// pacote que decl_policy.go declara e que Wire normalmente escreve — ver a
// doc de lá) com um Subscribe por nome de eventNames, todos apontando para o
// MESMO coletor: cada evento publicado (via "emit" dentro da Policy sob
// teste, StmtLowerer.WithEmitDispatch) é acumulado em "published" na ordem
// de chegada. emitPolicyThenAssert (abaixo) lê "published" para as duas
// formas de "then { emitted ... }".
func emitPolicyDispatcherSetup(e *emit.Emitter, eventNames []string, runtimeAlias, contextAlias string) {
	e.Line("var published []%s.Event", runtimeAlias)
	e.Line("policyDispatcher = %s.NewDispatcher()", runtimeAlias)
	e.Block(fmt.Sprintf("collect := func(ctx %s.Context, ev %s.Event) error", contextAlias, runtimeAlias), func() {
		e.Line("published = append(published, ev)")
		e.Line("return nil")
	})
	for _, name := range eventNames {
		e.Line("policyDispatcher.Subscribe(%q, collect)", name)
	}
}

// emitPolicyGivenReset reatribui, para cada tipo DISTINTO referenciado pelas
// GivenEntity de sc (ex. "Ticket"), o var de pacote Collection[T]
// correspondente (policyCollectionVarName) a um runtime.NewMemoryCollection[T]()
// NOVO — ver a doc de emitPolicyScenarioBody sobre por que isto é necessário
// (isolamento entre cenários que compartilham o MESMO var de pacote). Nomes
// ordenados alfabeticamente para determinismo (NFR-13) — não há relação de
// dependência entre resets de tipos diferentes, então a ordem de emissão não
// muda o comportamento, só a legibilidade do Go gerado.
func emitPolicyGivenReset(e *emit.Emitter, sc *ast.ScenarioDecl, runtimeAlias string) {
	seen := make(map[string]bool)
	var names []string
	for _, g := range sc.Givens {
		if g.Binding == "" {
			continue
		}
		for _, entity := range g.Entities {
			name := astutil.HeadName(entity.Entity)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		e.Line("%s = %s.NewMemoryCollection[%s]()", policyCollectionVarName(name), runtimeAlias, name)
	}
}

// emitPolicyGiven emite UMA GivenClause "given <binding> [...]" (§22.4) — a
// ÚNICA forma suportada nesta fase para Policy: cada entidade vira um
// runtime.Collection[T].Add sobre o var de pacote que a Policy sob teste lê
// (T = a cabeça de GivenEntity.Entity, ex. "Ticket" — ver
// emitPolicyGivenEntity). "given [eventos]"/"given Subject from [...]"/
// "given state{...}" (Aggregate/UseCase/Saga) não se aplicam a Policy (sem
// Aggregate único, sem EventStore) — erro de geração claro.
func emitPolicyGiven(e *emit.Emitter, sl *lower.StmtLowerer, g *ast.GivenClause, itemCounter *int) error {
	if g.Binding == "" {
		return fmt.Errorf("\"given [eventos]\"/\"given Subject from [...]\"/\"given state{...}\" não se aplicam a uma Policy — use \"given <binding> [...]\" (§22.4)")
	}
	for _, entity := range g.Entities {
		if err := emitPolicyGivenEntity(e, sl, entity, itemCounter); err != nil {
			return err
		}
	}
	return nil
}

// emitPolicyGivenEntity constrói UMA entidade de "given <binding> [...]"
// (§22.4): "itemN := <Tipo>{}" + um "itemN.Campo = valor" por entrada do
// overlay "{...}" (GivenEntity.State — idêntico, em espírito, a
// emitStateOverlay, mas sem o prefixo ".state." de Aggregate: aqui o item É
// o receptor) + "<tipo>Collection.Add(ctx, itemN)" no var de pacote
// resolvido por policyCollectionVarName (decl_policy.go — reusado, não
// reimplementado: EmitTests precisa semear EXATAMENTE o mesmo var que o
// corpo da Policy sob teste lê via list/count).
//
// O(s) argumento(s) POSICIONAL(IS) da própria construção (ex. o "T1" de
// "Ticket(\"T1\")") são ignorados aqui — deliberado, não esquecimento: o
// exemplo canônico do spec (§22.4) usa esse argumento só como rótulo legível
// no *.test.ds, sem equivalente em nenhum campo do item (ao contrário do
// given de Aggregate, cujo Event tem um campo "id" de verdade a espelhar);
// todo dado real do item vem do overlay "{...}". Uma fixture futura que
// precise do argumento posicional como um campo de verdade pode reusar
// emitFieldSeed (mesma máquina do given de Aggregate) — fora do escopo desta
// fatia porque o exemplo real (RefundAllOnEventCancelled) não precisa.
func emitPolicyGivenEntity(e *emit.Emitter, sl *lower.StmtLowerer, entity *ast.GivenEntity, itemCounter *int) error {
	call, ok := entity.Entity.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("esperava uma construção de item (\"Tipo(...)\"), got %T", entity.Entity)
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return fmt.Errorf("esperava um tipo nomeado, got %T", call.Fn)
	}
	typeName := id.Name

	*itemCounter++
	itemVar := fmt.Sprintf("item%d", *itemCounter)
	e.Line("%s := %s{}", itemVar, typeName)

	if entity.State != nil {
		for _, entry := range entity.State.Entries {
			goExpr, hoisted, err := sl.ExprHoisted(entry.Value)
			if err != nil {
				return fmt.Errorf("%s.%s: %w", typeName, entry.Key, err)
			}
			emitLines(e, hoisted)
			e.Line("%s.%s = %s", itemVar, goname.ExportField(entry.Key), goExpr)
		}
	}

	collVar := policyCollectionVarName(typeName)
	e.Block(fmt.Sprintf("if err := %s.Add(ctx, %s); err != nil", collVar, itemVar), func() {
		e.Line("t.Fatalf(%q, err)", fmt.Sprintf("given %s: %%v", typeName))
	})
	return nil
}

// emitPolicyThenAssert emite UMA linha de "then { ... }" (§22.4): "error
// Name" (errors.Is, mesma forma de §22.1/22.2), "emitted count N" (compara
// len(published)) e "emitted Evento(...)" (busca, em "published", ALGUM
// evento reflect.DeepEqual — ver a doc do arquivo sobre a checagem ser de
// MEMBRO, não por índice nem multiset). idx desambiguifica os nomes Go das
// variáveis auxiliares entre as várias linhas do MESMO cenário (want1/
// want2/..., mesmo espírito de emitAggregateThen's wantVar).
func emitPolicyThenAssert(e *emit.Emitter, sl *lower.StmtLowerer, a *ast.ThenAssert, idx int, hasDispatcher bool, errorsAlias, reflectAlias string) error {
	switch {
	case a.Error != "":
		e.Block(fmt.Sprintf("if !%s.Is(err, Err%s)", errorsAlias, a.Error), func() {
			e.Line("t.Fatalf(%q, err)", "esperava errors.Is(err, Err"+a.Error+"), got: %v")
		})
		return nil

	case a.Verb == "emitted" && a.Count != nil:
		if !hasDispatcher {
			return fmt.Errorf("\"emitted count ...\": scenarioNeedsDispatcher deveria ter sido true (bug de geração)")
		}
		countGo, hoisted, err := sl.ExprHoisted(a.Count)
		if err != nil {
			return fmt.Errorf("emitted count: %w", err)
		}
		emitLines(e, hoisted)
		wantVar := fmt.Sprintf("wantCount%d", idx+1)
		e.Line("%s := %s", wantVar, countGo)
		e.Block(fmt.Sprintf("if len(published) != %s", wantVar), func() {
			e.Line("t.Fatalf(%q, %s, len(published), published)", "esperava %d evento(s) publicado(s), got %d: %+v", wantVar)
		})
		return nil

	case a.Verb == "emitted" && a.Object != nil:
		if !hasDispatcher {
			return fmt.Errorf("\"emitted ...\": scenarioNeedsDispatcher deveria ter sido true (bug de geração)")
		}
		wantGo, hoisted, err := sl.ExprHoisted(a.Object)
		if err != nil {
			return fmt.Errorf("emitted: %w", err)
		}
		emitLines(e, hoisted)
		wantVar := fmt.Sprintf("want%d", idx+1)
		foundVar := fmt.Sprintf("found%d", idx+1)
		e.Line("%s := &%s", wantVar, wantGo)
		e.Line("%s := false", foundVar)
		e.Block("for _, got := range published", func() {
			e.Block(fmt.Sprintf("if %s.DeepEqual(got, %s)", reflectAlias, wantVar), func() {
				e.Line("%s = true", foundVar)
			})
		})
		e.Block(fmt.Sprintf("if !%s", foundVar), func() {
			e.Line("t.Fatalf(%q, %s, published)", "esperava emitted %+v entre os eventos publicados, got %+v", wantVar)
		})
		return nil

	default:
		return fmt.Errorf("forma de then não suportada para Policy nesta fase de H4 (verbo %q)", a.Verb)
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
