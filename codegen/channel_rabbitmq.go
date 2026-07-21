package codegen

import (
	"fmt"
	"path"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/program"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// channel_rabbitmq.go seleciona e materializa amqpruntime.NewRabbitMQChannel
// (Marco J, task J3.4, REQ-43.2/43.7, §design infra-providers 3.3) quando um
// canal da topologia declara `via: queue provider: "rabbitmq"` — o provider
// real que fecha a "known, documented Marco F limitation" que
// rtsrc/channel.go.txt já documenta (QueueChannel só entrega dentro do
// MESMO processo). Sem esse provider (ou um `via` diferente de "queue"),
// channel.go's caminho de sempre (runtime.NewQueueChannel/
// unsupportedChannelKindError) continua EXATAMENTE como antes desta task —
// NFR-21.
//
// --- R2: Channel não tem Provider/Connection tipados ---
//
// program.Channel só tem From/To/Via/Decl (mesmo formato de
// program.Database antes de Provider virar campo próprio) — o provider e a
// connection vivem em Decl.Entries, free-form, nunca validados contra um
// enum pelo front-end (mesma postura de Database.Provider). channelProvider/
// channelConnectionGo leem de lá via os MESMOS helpers genéricos que
// decl_telemetry.go/decl_io.go já usam para Telemetry/env(...) — nenhuma
// mudança de front-end.
//
// --- R1: connection por env(...), nunca um campo DSN vazio ---
//
// program.Channel não tem um campo "DSN" (ao contrário de Database) — não
// há risco de repetir o bug de R1 (usar um campo estático "" quando a forma
// real é env(...)); channelConnectionGo só lê a Expr crua de Decl.Entries e
// resolve por FORMA (env("VAR") -> os.Getenv("VAR"); literal string -> ele
// mesmo), o mesmo padrão que databaseConnectionGo (sql_wiring.go, J1.3) já
// estabeleceu.
//
// --- Publish-only no lado produtor (achado desta task) ---
//
// rabbitmqChannel (amqprt/rabbitmq.go.txt) declara fila(s) e sobe
// consumidores de verdade na construção — correto para o lado CONSUMIDOR
// (decl_policy.go:emitPolicyWireFunc), mas o lado PRODUTOR
// (generateCmdMainFile) só usa a instância para Publish, nunca Subscribe.
// Como a fila é um recurso COMPARTILHADO de verdade no broker (ao contrário
// do QueueChannel in-memory, onde cada processo tem sua própria cópia
// isolada), um consumidor espúrio do lado produtor competiria pelas
// mensagens com o consumidor real do outro service e as descartaria em
// silêncio. RabbitMQConfig.ConsumeDisabled (amqprt/rabbitmq.go.txt, ver a
// doc do campo) resolve isso — emitRabbitMQChannelVar passa
// consumeDisabled=true só na chamada de generateCmdMainFile.
//
// --- Registry inline, não contracts.EventRegistry() (R8/REQ-43.5) ---
//
// decodeEnvelope (amqprt/rabbitmq.go.txt) recebe um registry pronto — aqui
// montado INLINE a partir de candidates (o(s) evento(s) que de fato
// atravessam este canal, já resolvidos pelo CHAMADOR — o Policy.On no lado
// consumidor, todo PublicEvent do módulo produtor no lado produtor), nunca
// via contracts.EventRegistry()/EventRegistry(): o tipo já é conhecido
// ESTATICAMENTE neste ponto da geração, mesmo raciocínio que
// emitDurableOutboxConstruction (decl_policy.go, J2.5) já documenta para o
// outbox durável. Vazio no lado produtor (ConsumeDisabled, nunca decodifica
// nada).

// channelProvider lê "provider" de ch.Decl.Entries — "" quando ausente
// (canal in-memory de sempre). Mesmo helper (configStringLitEntry,
// decl_telemetry.go) que activeProviderDeps (provider_registry.go) já usa
// para resolver channelProviders["rabbitmq"] a partir de um canal.
func channelProvider(ch *program.Channel) (string, error) {
	if ch.Decl == nil {
		return "", nil
	}
	provider, ok, err := configStringLitEntry(ch.Decl.Entries, "provider")
	if err != nil {
		return "", fmt.Errorf("canal %s -> %s: provider: %w", ch.From, ch.To, err)
	}
	if !ok {
		return "", nil
	}
	return provider, nil
}

// channelProviderKind normaliza channelProvider(ch) contra channelProviders
// (o registro real, J3.1) — "" quando ausente OU não reconhecido (o canal
// in-memory de sempre, NFR-21), "rabbitmq" quando reconhecido. Um provider
// declarado mas NÃO reconhecido (ex. "kafka", ainda não implementado) cai
// silenciosamente no caminho in-memory — mesma postura de
// recognizedSQLProvider para "postgres" antes de J1.2 existir: nunca um
// erro de geração por um rótulo que o front-end já aceita livremente.
func channelProviderKind(ch *program.Channel) (string, error) {
	provider, err := channelProvider(ch)
	if err != nil || provider == "" {
		return "", err
	}
	if _, known := channelProviders[strings.ToLower(provider)]; known {
		return strings.ToLower(provider), nil
	}
	return "", nil
}

// channelConnectionGo traduz a connection string de ch para uma expressão Go
// (task J3.4, R1, §design infra-providers 3.3) — mesmo padrão de
// databaseConnectionGo (sql_wiring.go, J1.3): a chave "connection" (mesmo
// nome canônico usado por Database) tem prioridade; "url" é aceita como
// sinônimo (algumas fixtures de canal podem preferir esse nome, mais comum
// para brokers). "env(VAR)" vira "os.Getenv(VAR)"; um literal STRING vira
// ele mesmo, entre aspas Go; nenhuma das duas chaves presentes vira "" (sem
// campo DSN estático para cair de volta, ao contrário de Database — ver a
// doc do arquivo).
func channelConnectionGo(e *emit.Emitter, ch *program.Channel) (string, error) {
	var entries []ast.ConfigEntry
	if ch.Decl != nil {
		entries = ch.Decl.Entries
	}

	expr, ok := findConfigEntryExpr(entries, "connection")
	if !ok {
		expr, ok = findConfigEntryExpr(entries, "url")
	}
	if !ok {
		// Erro de geração claro (revisão da PR #27), não uma string vazia
		// silenciosa: sem connection/url, amqp.Dial("") falharia de forma
		// confusa só em runtime, na inicialização do processo — mesma
		// postura fail-closed de unsupportedChannelKindError/
		// rejectUnsupportedTenancyStrategies (nunca deixar um problema
		// detectável em tempo de geração vazar pra runtime).
		return "", fmt.Errorf(`canal %s -> %s: provider "rabbitmq" exige "connection" ou "url" (ex. connection: env("AMQP_URL"))`, ch.From, ch.To)
	}

	if key, isEnv := envCallKey(expr); isEnv {
		osAlias := e.Import("os")
		return fmt.Sprintf("%s.Getenv(%q)", osAlias, key), nil
	}
	if lit, isLit := expr.(*ast.Literal); isLit && lit.Kind == token.STRING {
		return strconv.Quote(lit.Value), nil
	}
	return "", fmt.Errorf(`canal %s -> %s: connection: forma não suportada (%T) — esperava env("VAR") ou um literal string`, ch.From, ch.To, expr)
}

// emitChannelTransportVar decide, por ch, entre o caminho de sempre
// (runtime.NewQueueChannel, channel.go:emitChannelQueueVar — byte-idêntico
// quando o provider é "" ou não reconhecido, NFR-21) e o provider real
// selecionado por channelProviderKind (só "rabbitmq" hoje) — chamada pelos
// DOIS lados (consumidor: decl_policy.go:emitPolicyWireFunc,
// consumeDisabled=false; produtor: codegen.go:generateCmdMainFile,
// consumeDisabled=true — ver a doc do arquivo).
func emitChannelTransportVar(e *emit.Emitter, varName, op string, ch *program.Channel, candidates []channelEventCandidate, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias string, consumeDisabled bool, runMode bool) error {
	kind, err := channelProviderKind(ch)
	if err != nil {
		return err
	}
	if kind != "rabbitmq" {
		return emitChannelQueueVar(e, varName, op, ch, candidates, model, tab, module, reg, runtimeAlias)
	}
	return emitRabbitMQChannelVar(e, varName, op, ch, candidates, model, tab, module, reg, runtimeAlias, consumeDisabled, runMode)
}

// emitRabbitMQChannelVar emite "<varName> <op> amqpruntime.NewRabbitMQChannel(
// <connection>, amqpruntime.RabbitMQConfig{...}, <registry>)" + a checagem
// de erro fail-closed (log.Fatal — mesma postura de emitXADatabaseWiring
// para uma falha de infraestrutura na inicialização). Exchange/Queue são
// nomeados "<ch.From>-<ch.To>" (únicos por canal — a topologia já garante
// From/To distintos por par ordenado); Concurrency de `workers.concurrency`
// (channelWorkersSettings — maxRate/batchSize são específicos de
// QueueChannel in-memory, sem equivalente em RabbitMQConfig, silenciosamente
// ignorados aqui: nenhuma mentira sendo contada, REQ-43 nunca promete
// maxRate/batchSize para o provider real); MaxAttempts/RetryTTL de
// `circuitBreaker.threshold`/`.cooldown` (mesmo bloco .ds que o in-memory já
// lê, reinterpretado — ver a doc de RabbitMQConfig.MaxAttempts/RetryTTL,
// amqprt/rabbitmq.go.txt, sobre threshold virar contador de tentativas em
// vez de acionar runtime.CircuitBreaker); KeyFunc de `orderBy`, MESMA
// validação de emitChannelQueueVar (um orderBy que nenhum candidate tem é
// erro de geração claro).
func emitRabbitMQChannelVar(e *emit.Emitter, varName, op string, ch *program.Channel, candidates []channelEventCandidate, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias string, consumeDisabled bool, runMode bool) error {
	env := lower.New(model, tab, module)
	l := lower.NewLowerer(env, reg, runtimeAlias).WithEmitter(e)

	amqpAlias := e.Import(path.Join(domainModuleRoot, "amqpruntime"))
	logAlias := e.Import("log")

	concurrency, _, _, err := channelWorkersSettings(ch)
	if err != nil {
		return err
	}
	thresholdGo, cooldownGo, hasBreaker, err := channelBreakerGo(ch, l)
	if err != nil {
		return err
	}
	if hasBreaker && cooldownGo != "" {
		e.Import("time")
	}

	connGo, err := channelConnectionGo(e, ch)
	if err != nil {
		return err
	}

	exchange := ch.From + "-" + ch.To

	orderByField := channelOrderByField(ch)
	var keyFuncGo string
	if orderByField != "" {
		var validCandidates []channelEventCandidate
		for _, c := range candidates {
			if channelEventHasField(c.evtDecl, orderByField) {
				validCandidates = append(validCandidates, c)
			}
		}
		if len(validCandidates) == 0 {
			names := make([]string, len(candidates))
			for i, c := range candidates {
				names[i] = c.evtDecl.Name
			}
			return fmt.Errorf("canal %s -> %s declara orderBy %q, mas nenhum dos eventos considerados (%s) tem esse campo", ch.From, ch.To, orderByField, strings.Join(names, ", "))
		}
		fmtAlias := e.Import("fmt")
		exportField := goname.ExportField(orderByField)
		var b strings.Builder
		fmt.Fprintf(&b, "func(ev %s.Event) string {\n", runtimeAlias)
		b.WriteString("switch e := ev.(type) {\n")
		for _, c := range validCandidates {
			fmt.Fprintf(&b, "case %s:\n", c.goPtrType)
			fmt.Fprintf(&b, "return %s.Sprint(e.%s)\n", fmtAlias, exportField)
		}
		b.WriteString("default:\nreturn \"\"\n}\n}")
		keyFuncGo = b.String()
	}

	registryGo := fmt.Sprintf("map[string]%s.EventFactory{}", amqpAlias)
	if !consumeDisabled && len(candidates) > 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "map[string]%s.EventFactory{\n", amqpAlias)
		for _, c := range candidates {
			ctor := "&" + strings.TrimPrefix(c.goPtrType, "*") + "{}"
			fmt.Fprintf(&b, "%s: func() %s.Event { return %s },\n", strconv.Quote(c.evtDecl.Name), runtimeAlias, ctor)
		}
		b.WriteString("}")
		registryGo = b.String()
	}

	cfgFields := []string{
		fmt.Sprintf("Exchange: %s", strconv.Quote(exchange)),
		fmt.Sprintf("Queue: %s", strconv.Quote(exchange)),
		fmt.Sprintf("Concurrency: %d", concurrency),
	}
	if consumeDisabled {
		cfgFields = append(cfgFields, "ConsumeDisabled: true")
	}
	if hasBreaker {
		cfgFields = append(cfgFields, fmt.Sprintf("MaxAttempts: %s", thresholdGo))
		if cooldownGo != "" {
			cfgFields = append(cfgFields, fmt.Sprintf("RetryTTL: %s", cooldownGo))
		}
	}
	if keyFuncGo != "" {
		cfgFields = append(cfgFields, fmt.Sprintf("KeyFunc: %s", keyFuncGo))
	}
	cfgGo := fmt.Sprintf("%s.RabbitMQConfig{%s}", amqpAlias, strings.Join(cfgFields, ", "))

	errVar := varName + "Err"
	if op == ":=" {
		e.Line("%s, %s := %s.NewRabbitMQChannel(%s, %s, %s)", varName, errVar, amqpAlias, connGo, cfgGo, registryGo)
	} else {
		e.Line("var %s error", errVar)
		e.Line("%s, %s = %s.NewRabbitMQChannel(%s, %s, %s)", varName, errVar, amqpAlias, connGo, cfgGo, registryGo)
	}
	emitFailFastBlock(e, errVar, logAlias, runMode)
	if op == ":=" {
		// defer Close() (task J6.2) só faz sentido do lado PRODUTOR (op
		// ":=", chamado por generateCmdMainFile) — o lado CONSUMIDOR (op
		// "=", chamado por emitPolicyWireFunc dentro de Wire(d)) sempre
		// passa runMode=false, então emitDeferChannelClose é um no-op ali
		// de qualquer forma; a checagem "op" aqui é só documental.
		emitDeferChannelClose(e, varName, runMode)
	}
	return nil
}
