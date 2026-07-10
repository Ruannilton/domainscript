package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// decl_metric.go emite o Go de um MetricDecl (H3, REQ-30.3, §design 3.13):
// counter/histogram de negócio atualizado no gatilho declarado — "on Evento"
// (subscriber no runtime.Dispatcher, MESMO padrão de decl_policy.go) ou "on
// Saga.completed" (hook direto dentro do próprio código gerado da Saga,
// decl_saga.go — sem Dispatcher: uma Saga não publica nada ao concluir, só
// corre seus passos).
//
// --- O gap do front-end que este arquivo preenche sozinho ---
//
// Ao contrário de todo outro construto com gatilho/corpo (PolicyDecl.On,
// SagaDecl.Handles, Handle/Apply/UseCase.execute, ...), MetricDecl NÃO tem
// nenhuma entrada em resolver/receivers.go nem em
// resolver.resolveDeclBodies (resolve_body.go) — confirmado empiricamente
// antes de escrever esta task: um programa com "Metric M { value event.bogus
// on NaoExiste labels { x = event.tambemBogus } }" passa pelo front-end
// inteiro (driver.CheckSource) com ZERO diagnósticos, mesmo referenciando um
// Event inexistente e campos que não existem. Isso é estruturalmente
// diferente do padrão "REQ-9 já deveria ter barrado isso" que
// decl_policy.go/decl_saga.go usam nos comentários de suas próprias funções
// resolveXxx (ali, uma rede de segurança redundante sobre algo já validado)
// — aqui, resolveMetricOn (abaixo) é a ÚNICA validação de nomes que
// MetricDecl.On recebe em todo o pipeline, não uma segunda checagem.
//
// Value/Labels (expressões livres sobre o receptor "event"/"state") NÃO
// recebem essa mesma segurança de um jeito óbvio: Lowerer.member
// (lower/expr.go) sozinho NÃO valida se um nome de campo existe de verdade
// contra um VOType/ShapeType — normalmente delega essa checagem a
// sema/rules_typecheck.go (REQ-12), que roda ANTES de EmitXxx para todo
// outro corpo executável, mas nunca para Metric (mesmo gap). Por isso
// metricNumericGo/metricLabelsGo (abaixo) cruzam CADA expressão com
// types.Model (via TypeEnv.InferAssignRHS) ANTES de aceitar o texto Go de
// Lowerer.Expr: um MemberExpr sobre um nome que não existe devolve
// types.ErrorType (types/infer.go, ramo "membro inexistente" — a MESMA
// checagem que back REQ-12, só que consultada diretamente aqui, sem passar
// por sema) — rejeitado como erro de geração claro, nunca Go quebrado. A
// única forma que ESCAPA dessa rede (documentada, não corrigida: exigiria
// reimplementar mais do que a checagem de membro) é uma expressão cujo tipo
// types.Model.Infer não sabe computar por outra razão que não "nome/membro
// inexistente" (ex. formas de QueryExpr/MatchExpr/LambdaExpr dentro de
// Value/Labels — não exercitadas pelos exemplos do spec §21, que só usam
// acesso a membro simples).
//
// --- Registro em runtime.Counter/Histogram (rtsrc/metrics.go.txt) ---
//
// REQ-30.3 não cita nenhuma dependência externa (ao contrário de REQ-30.2,
// que documenta a exceção do OTel explicitamente) — por isso todo Metric
// atualiza um registry em memória, stdlib-only, sempre presente (parte do
// runtime NÚCLEO copiado por generateRuntimeFiles, nunca opt-in — mesmo
// nível de EventStore/Dispatcher). Um Metric NÃO exporta pelo adapter OTel
// de H2 (codegen/otelrt) hoje: aquele pacote já documenta, desde H2, que
// "Métricas... ficam FORA do escopo" (otelrt/observer.go.txt) — extendê-lo
// exigiria um Meter real do SDK do OTel (go.opentelemetry.io/otel/metric +
// sdk/metric + um exporter OTLP de métricas), uma dependência NOVA que hoje
// não existe em nenhum lugar deste gerador. Adicionar isso só para um
// "nice to have" quando REQ-30.3 não exige nenhuma integração externa é
// desproporcional ao orçamento desta task — deixado de fora, decisão
// consciente, não um esquecimento.
//
// --- counter vs. histogram, valor implícito ---
//
// decl.Type decide a forma: "counter" soma decl.Value (ou 1, quando Value
// está ausente — "conte quantas vezes o gatilho disparou", o default óbvio
// de um counter sem valor explícito) a cada disparo; "histogram" observa
// decl.Value quando presente, ou — só quando o gatilho é "on
// Saga.completed" — a DURAÇÃO da Saga (spec §21, exemplo PurchaseLatency:
// nenhum "value", só "buckets" de DURATION) desde o início da execução até
// a conclusão bem-sucedida dos passos. Um histogram "on Evento" (não Saga)
// sem "value" é erro de geração: não há duração nenhuma para observar
// nesse caso (só um Event publicado, sem início/fim próprios).
//
// --- Labels ---
//
// Cada MapEntry.Value (ex. event.amount.currency) é lowerizado e convertido
// para string via "fmt.Sprintf("%v", ...)" (metricLabelsGo, abaixo): funciona
// sem ajuste para um primitivo (string passa direto, %v de um Go string já é
// o texto cru) e para um ValueObject WRAPPER ("type X Base" sobre um
// primitivo — %v formata pelo Kind reflect.Kind subjacente, então imprime o
// valor de Base); um VO COMPOSTO como valor de label produziria a notação de
// struct Go ("{10.0000 USD}") em vez de algo dedicado — degrada sem quebrar,
// não exercitado pelos exemplos reais do spec (currency é sempre um campo
// primitivo de um VO composto, nunca o VO inteiro).

// metricTarget é a forma já resolvida do gatilho "on" de um MetricDecl:
// event != nil ⇒ "on Evento" (subscribe no runtime.Dispatcher do módulo,
// MESMA forma de policyEventInfo); sagaName != "" ⇒ "on Saga.completed"
// (hook direto no código gerado da Saga, decl_saga.go — sem Dispatcher).
type metricTarget struct {
	event    *policyEventInfo
	sagaName string
}

// resolveMetricOn resolve MetricDecl.On à sua forma concreta — a ÚNICA
// validação de nomes que este campo recebe em todo o pipeline (ver a doc do
// arquivo). Duas formas suportadas: um *ast.Ident nomeando um Event/
// PublicEvent conhecido (reaproveita resolvePolicyEvent, decl_policy.go), ou
// um *ast.MemberExpr "NomeDaSaga.completed" nomeando uma Saga conhecida.
// Qualquer outra forma (incl. On ausente) é um erro de geração claro.
func resolveMetricOn(e *emit.Emitter, tab *symbols.SymbolTable, module string, decl *ast.MetricDecl) (metricTarget, error) {
	switch on := decl.On.(type) {
	case *ast.Ident:
		info, err := resolvePolicyEvent(e, tab, module, on.Name)
		if err != nil {
			return metricTarget{}, fmt.Errorf("Metric %s: gatilho \"on %s\": %w", decl.Name, on.Name, err)
		}
		return metricTarget{event: &info}, nil

	case *ast.MemberExpr:
		sagaIdent, ok := on.X.(*ast.Ident)
		if !ok || on.Name != "completed" {
			return metricTarget{}, fmt.Errorf("Metric %s: gatilho \"on\" não suportado — esperava NomeDoEvento ou NomeDaSaga.completed", decl.Name)
		}
		sym, ok := tab.Lookup(module, sagaIdent.Name)
		if !ok {
			sym, ok = tab.Find(sagaIdent.Name)
		}
		if !ok {
			return metricTarget{}, fmt.Errorf("Metric %s: Saga %q não encontrada (gatilho \"on %s.completed\")", decl.Name, sagaIdent.Name, sagaIdent.Name)
		}
		if _, ok := sym.Decl.(*ast.SagaDecl); !ok {
			return metricTarget{}, fmt.Errorf("Metric %s: %q não resolve a uma Saga (gatilho \"on %s.completed\", got %T)", decl.Name, sagaIdent.Name, sagaIdent.Name, sym.Decl)
		}
		return metricTarget{sagaName: sagaIdent.Name}, nil

	default:
		return metricTarget{}, fmt.Errorf("Metric %s: gatilho \"on\" de forma inesperada (%T) — esperava NomeDoEvento ou NomeDaSaga.completed", decl.Name, decl.On)
	}
}

// metricIsCounter reporta se decl.Type é "counter" (true) ou "histogram"
// (false) — qualquer outro valor é erro de geração: ao contrário de
// PolicyDecl.Delivery (cujo literal desconhecido cai num fallback
// conservador, ver decl_policy.go), o Type de um Metric decide a FORMA do Go
// gerado (Counter vs. Histogram, Add vs. Observe) — não há fallback seguro
// possível.
func metricIsCounter(decl *ast.MetricDecl) (bool, error) {
	switch decl.Type {
	case "counter":
		return true, nil
	case "histogram":
		return false, nil
	default:
		return false, fmt.Errorf("Metric %s: type %q não suportado (esperava \"counter\" ou \"histogram\")", decl.Name, decl.Type)
	}
}

// metricRegistryVarName devolve o nome Go da var de registry de decl:
// "<Nome>Counter" ou "<Nome>Histogram".
func metricRegistryVarName(decl *ast.MetricDecl, isCounter bool) string {
	if isCounter {
		return decl.Name + "Counter"
	}
	return decl.Name + "Histogram"
}

// metricBucketsGoLiteral traduz MetricDecl.Buckets (uma lista de literais
// DURATION ou INT/FLOAT, spec §21) para um literal Go "[]float64{...}" — os
// limites de bucket já materializados em SEGUNDOS em tempo de GERAÇÃO
// (lower.DurationLiteralSeconds), nunca reconstruídos em tempo de execução.
func metricBucketsGoLiteral(bucketsExpr ast.Expr) (string, error) {
	list, ok := bucketsExpr.(*ast.ListExpr)
	if !ok {
		return "", fmt.Errorf("buckets: esperava uma lista de literais, got %T", bucketsExpr)
	}
	parts := make([]string, 0, len(list.Elems))
	for _, el := range list.Elems {
		lit, ok := el.(*ast.Literal)
		if !ok {
			return "", fmt.Errorf("buckets: elemento não é literal (%T) — só DURATION/INT/FLOAT são suportados", el)
		}
		switch lit.Kind {
		case token.DURATION:
			secs, err := lower.DurationLiteralSeconds(lit.Value)
			if err != nil {
				return "", fmt.Errorf("buckets: %w", err)
			}
			parts = append(parts, strconv.FormatFloat(secs, 'g', -1, 64))
		case token.INT, token.FLOAT:
			parts = append(parts, lit.Value)
		default:
			return "", fmt.Errorf("buckets: literal de kind %s não suportado", lit.Kind)
		}
	}
	return "[]float64{" + strings.Join(parts, ", ") + "}", nil
}

// metricNumericGo lowereiza valueExpr via l e coage o resultado para uma
// expressão Go float64 — o tipo que Counter.Add/Histogram.Observe exigem.
// Só "decimal" (runtime.Decimal, via o método Float64 acrescentado a
// rtsrc/decimal.go.txt exatamente para isto) e "integer" (conversão nativa
// "float64(...)") são suportados: os dois primitivos numéricos que um
// MetricDecl.Value realisticamente nomeia (spec §21, "value
// event.amount.amount"). Qualquer outro tipo é erro de geração — um Metric
// precisa de um valor numérico.
func metricNumericGo(env *lower.TypeEnv, l *lower.Lowerer, valueExpr ast.Expr) (string, error) {
	goExpr, err := l.Expr(valueExpr)
	if err != nil {
		return "", err
	}
	t, err := env.InferAssignRHS(valueExpr)
	if err != nil {
		return "", err
	}
	pt, ok := t.(*types.Primitive)
	if !ok {
		return "", fmt.Errorf("precisa ser numérico (decimal/integer) — tipo inferido é %s", t.String())
	}
	switch pt.Name {
	case "decimal":
		return goExpr + ".Float64()", nil
	case "integer":
		return "float64(" + goExpr + ")", nil
	default:
		return "", fmt.Errorf("precisa ser numérico (decimal/integer) — tipo inferido é %q", pt.Name)
	}
}

// metricLabelsGo lowereiza cada MapEntry.Value de labels via l e monta um
// literal Go "map[string]string{...}", convertendo cada valor lowerizado
// para string via "fmtAlias.Sprintf("%v", ...)" (ver a doc do arquivo sobre
// por que isso basta para os casos reais do spec). Cada valor é primeiro
// cruzado com types.Model (env.InferAssignRHS) — um MemberExpr sobre um
// nome/membro inexistente vira types.ErrorType, rejeitado aqui como erro de
// geração ANTES de gerar qualquer texto Go (ver a doc do arquivo sobre o
// gap do front-end que isso fecha). labels vazio nunca chama esta função (o
// chamador usa o literal "nil" direto).
func metricLabelsGo(env *lower.TypeEnv, l *lower.Lowerer, fmtAlias string, labels []ast.MapEntry) (string, error) {
	parts := make([]string, 0, len(labels))
	for _, entry := range labels {
		t, terr := env.InferAssignRHS(entry.Value)
		if terr != nil {
			return "", fmt.Errorf("label %q: %w", entry.Name, terr)
		}
		if types.IsError(t) {
			return "", fmt.Errorf("label %q: não consegui resolver o tipo da expressão (nome ou membro inexistente)", entry.Name)
		}
		goExpr, err := l.Expr(entry.Value)
		if err != nil {
			return "", fmt.Errorf("label %q: %w", entry.Name, err)
		}
		parts = append(parts, fmt.Sprintf("%s: %s.Sprintf(\"%%v\", %s)", strconv.Quote(entry.Name), fmtAlias, goExpr))
	}
	return "map[string]string{" + strings.Join(parts, ", ") + "}", nil
}

// emitMetricRegistryVar emite a var de package-level de registry de decl
// (runtime.Counter ou runtime.Histogram) e devolve seu nome Go — chamada
// tanto para uma Metric "on Evento" (aqui, EmitMetrics) quanto "on
// Saga.completed" (via resolveSagaCompletionHooks, decl_saga.go): as duas
// precisam da MESMA var, só o que a atualiza (subscriber vs. hook direto)
// difere.
func emitMetricRegistryVar(e *emit.Emitter, decl *ast.MetricDecl, runtimeAlias string) (string, error) {
	isCounter, err := metricIsCounter(decl)
	if err != nil {
		return "", err
	}
	varName := metricRegistryVarName(decl, isCounter)

	e.Line("")
	if isCounter {
		e.Line("// %s é o registry runtime da Metric %s (§21, REQ-30.3): counter em", varName, decl.Name)
		e.Line("// memória, sempre presente (sem dependência externa — ver a doc do arquivo).")
		e.Line("var %s = %s.NewCounter(%q)", varName, runtimeAlias, decl.Name)
		return varName, nil
	}

	if decl.Buckets == nil {
		return "", fmt.Errorf("Metric %s: histogram requer \"buckets\"", decl.Name)
	}
	bucketsGo, err := metricBucketsGoLiteral(decl.Buckets)
	if err != nil {
		return "", fmt.Errorf("Metric %s: %w", decl.Name, err)
	}
	e.Line("// %s é o registry runtime da Metric %s (§21, REQ-30.3): histogram em", varName, decl.Name)
	e.Line("// memória, sempre presente (sem dependência externa — ver a doc do arquivo).")
	e.Line("var %s = %s.NewHistogram(%q, %s)", varName, runtimeAlias, decl.Name, bucketsGo)
	return varName, nil
}

// emitMetricEventSubscriber emite o subscriber Go de uma Metric acionada por
// Event: assinatura EXATA de runtime.Dispatcher.Subscribe (MESMO padrão de
// emitPolicyDecl, decl_policy.go), type assertion pro tipo concreto do
// evento, e a atualização do registry (varName) com o value/labels
// declarados, lowerizados contra "event" (TypeEnv.SeedMetricEvent).
func emitMetricEventSubscriber(e *emit.Emitter, decl *ast.MetricDecl, evt policyEventInfo, varName string, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias, ctxAlias, fmtAlias string) (funcName string, err error) {
	onIdent, ok := decl.On.(*ast.Ident) // já validado por resolveMetricOn
	if !ok {
		return "", fmt.Errorf("bug de geração: emitMetricEventSubscriber chamado com On não-Ident (%T)", decl.On)
	}
	funcName = decl.Name + "On" + onIdent.Name

	env := lower.New(model, tab, module)
	env.SeedMetricEvent(onIdent.Name)
	l := lower.NewLowerer(env, reg, runtimeAlias)

	isCounter, err := metricIsCounter(decl)
	if err != nil {
		return "", err
	}

	valueGo := ""
	if decl.Value != nil {
		valueGo, err = metricNumericGo(env, l, decl.Value)
		if err != nil {
			return "", fmt.Errorf("value: %w", err)
		}
	} else if !isCounter {
		return "", fmt.Errorf("Metric %s: histogram sem \"value\" só é implícito no gatilho \"on Saga.completed\" (duração) — declare \"value\" explicitamente para um gatilho de Event", decl.Name)
	}
	if valueGo == "" {
		valueGo = "1" // counter sem value: conta disparos (ver a doc do arquivo)
	}

	labelsGo := "nil"
	if len(decl.Labels) > 0 {
		labelsGo, err = metricLabelsGo(env, l, fmtAlias, decl.Labels)
		if err != nil {
			return "", fmt.Errorf("labels: %w", err)
		}
	}

	method := "Observe"
	if isCounter {
		method = "Add"
	}

	e.Line("")
	e.Line("// %s é o subscriber Go da Metric %s (§21, REQ-30.3): reage a %s", funcName, decl.Name, onIdent.Name)
	e.Line("// publicado no Dispatcher e atualiza %s com o value/labels declarados.", varName)
	e.Line("// Assinatura igual a runtime.Dispatcher.Subscribe — registrada por WireMetrics")
	e.Line("// (abaixo).")
	sig := fmt.Sprintf("func %s(ctx %s.Context, ev %s.Event) error", funcName, ctxAlias, runtimeAlias)
	errMsg := fmt.Sprintf("metric %s: evento inesperado %%T (esperava %s)", decl.Name, evt.goPtrType)
	e.Block(sig, func() {
		e.Line("event, ok := ev.(%s)", evt.goPtrType)
		e.Block("if !ok", func() {
			e.Line("return %s.Errorf(%q, ev)", fmtAlias, errMsg)
		})
		e.Line("_ = event")
		e.Line("%s.%s(%s, %s)", varName, method, valueGo, labelsGo)
		e.Line("return nil")
	})
	return funcName, nil
}

// EmitMetrics gera o Go de todas as Metric de negócio de um módulo (H3,
// REQ-30.3, §design 3.13), num único arquivo (metrics.go): a var de registry
// de CADA Metric (independente do gatilho), o subscriber + entrada em
// WireMetrics de cada Metric "on Evento", e a coleta de cada Metric "on
// Saga.completed" em sagaMetrics — devolvida para o CHAMADOR (
// generateModuleFiles) repassar a EmitSagas, que emite o hook de atualização
// direto no código gerado da Saga (decl_saga.go) em vez de aqui (ver a doc
// do arquivo). needsDispatcher reporta se este módulo precisa de um
// runtime.Dispatcher só por causa de Metric — mesma forma de
// moduleMarks.hasCachedQueries (decl_query_cache.go/codegen.go).
func EmitMetrics(pkg string, decls []*ast.MetricDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) (content []byte, sagaMetrics map[string][]*ast.MetricDecl, needsDispatcher bool, err error) {
	e := emit.New(pkg)

	type eventSub struct {
		decl    *ast.MetricDecl
		evt     policyEventInfo
		varName string
	}
	var subs []eventSub
	sagaMetrics = make(map[string][]*ast.MetricDecl)

	// runtimeAlias só é importado quando de fato há uma Metric "on Evento"
	// neste módulo (Import lazy) — um módulo cujas Metric são TODAS "on
	// Saga.completed" não precisa de nada deste arquivo (a var de registry e
	// o import de runtime dessas vivem em sagas.go, ver a doc do arquivo);
	// importar aqui incondicionalmente produziria "import ... e não usado"
	// em e.Bytes() nesse caso.
	var runtimeAlias string

	for _, decl := range decls {
		target, terr := resolveMetricOn(e, tab, module, decl)
		if terr != nil {
			return nil, nil, false, terr
		}
		if target.sagaName != "" {
			sagaMetrics[target.sagaName] = append(sagaMetrics[target.sagaName], decl)
			continue
		}

		if runtimeAlias == "" {
			runtimeAlias = e.Import(RuntimeImportPath)
		}
		varName, verr := emitMetricRegistryVar(e, decl, runtimeAlias)
		if verr != nil {
			return nil, nil, false, verr
		}
		subs = append(subs, eventSub{decl: decl, evt: *target.event, varName: varName})
	}

	if len(subs) == 0 {
		// Nenhuma Metric "on Evento" — só "on Saga.completed" (ou nenhuma
		// Metric de verdade). Arquivo fica vazio (só "package pkg"), válido:
		// o CHAMADOR (generateModuleFiles) decide, via needsDispatcher=false,
		// nem escrever este arquivo.
		b, berr := e.Bytes()
		if berr != nil {
			return nil, nil, false, berr
		}
		return b, sagaMetrics, false, nil
	}

	ctxAlias := e.Import("context")
	fmtAlias := e.Import("fmt")

	type wired struct {
		eventName string
		funcName  string
	}
	wires := make([]wired, 0, len(subs))
	for _, s := range subs {
		funcName, serr := emitMetricEventSubscriber(e, s.decl, s.evt, s.varName, model, tab, module, reg, runtimeAlias, ctxAlias, fmtAlias)
		if serr != nil {
			return nil, nil, false, fmt.Errorf("Metric %s: %w", s.decl.Name, serr)
		}
		wires = append(wires, wired{eventName: s.decl.On.(*ast.Ident).Name, funcName: funcName})
	}

	e.Line("")
	e.Line("// WireMetrics registra cada Metric de negócio acionada por evento deste")
	e.Line("// pacote no runtime.Dispatcher (§21, REQ-30.3) — chamada por")
	e.Line("// cmd/<service>/main.go na inicialização, ao lado de Wire/WireQueryCache.")
	e.Line("// Uma Metric \"on Saga.completed\" NÃO passa por aqui: é atualizada direto no")
	e.Line("// código gerado da própria Saga (ver codegen/decl_saga.go).")
	e.Block(fmt.Sprintf("func WireMetrics(d %s.Dispatcher)", runtimeAlias), func() {
		for _, w := range wires {
			e.Line("d.Subscribe(%q, %s)", w.eventName, w.funcName)
		}
	})

	b, berr := e.Bytes()
	if berr != nil {
		return nil, nil, false, berr
	}
	return b, sagaMetrics, true, nil
}
