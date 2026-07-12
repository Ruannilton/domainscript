package codegen

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/program"
	"domainscript/symbols"
	"domainscript/types"
)

// channel.go materializa os canais da topologia (topology.ds, §11; Marco F5,
// REQ-25.3, REQ-26.1/26.5, §design 3.11) atrás do seam runtime.
// ChannelTransport (codegen/rtsrc/channel.go.txt): lê `orderBy`/
// `workers{concurrency,maxRate,batchSize}`/`timeout`/`circuitBreaker` de um
// *program.Channel e monta a expressão Go que constrói o transporte certo
// para o `via` declarado.
//
// "direct" (ou ausência de `via`) não precisa de NADA daqui: continua sendo
// o runtime.Dispatcher/Outbox puro, exatamente como F1 já gerava — só
// "queue" (REQ-26.5) ganha um transporte de verdade
// (runtime.NewQueueChannel). Qualquer outro `via` (grpc/http/stream, ou um
// provider real como "rabbitmq") é um erro de geração claro
// (unsupportedChannelKindError) — o seam existe (ChannelTransport), mas
// nenhum construtor para esses ainda (trabalho de um marco posterior,
// NFR-12): nunca um fallback silencioso para despacho local.
//
// --- Produtor vs. consumidor (mesmo canal, duas instâncias) ---
//
// O MESMO canal é materializado duas vezes, uma de cada lado, cada
// processo/service com sua PRÓPRIA instância — ver a doc de
// rtsrc/channel.go.txt sobre por que (só entrega de verdade dentro do MESMO
// processo até um provider real, Marco G+):
//
//   - Lado consumidor (decl_policy.go, emitPolicyWireFunc): constrói a
//     instância dentro do PRÓPRIO Wire, para o tipo de evento exato do
//     Policy.On.
//   - Lado produtor (codegen.go, generateCmdMainFile): constrói a instância
//     em cmd/<service>/main.go e a injeta como publisher da unit of work
//     (runtime.NewUnitOfWork(store, <transporte>) — ver uow.go.txt), para
//     todo PublicEvent do módulo produtor que carregue o campo de orderBy.
//
// channelEventCandidate é um PublicEvent que pode viajar por um canal: seu
// tipo Go de referência (sempre um ponteiro qualificado por pacote, ex.
// "*contracts.OrderPlaced") e a *ast.EventDecl original (para validar que o
// campo de orderBy de fato existe nele).
type channelEventCandidate struct {
	evtDecl   *ast.EventDecl
	goPtrType string
}

// channelViaKind normaliza o transporte de ch: "" (ausente) e "direct" caem
// no mesmo caminho (in-process, sem transporte especial) — só "queue" pede
// um runtime.ChannelTransport de verdade (ver a doc do arquivo).
func channelViaKind(ch *program.Channel) string {
	if ch.Via == "" {
		return "direct"
	}
	return ch.Via
}

// unsupportedChannelKindError é o erro de geração claro para um `via` que
// REQ-26.5 nomeia mas este marco não implementa ainda (grpc/http/stream) ou
// que é explicitamente um provider real fora de escopo (rabbitmq, NFR-12) —
// nunca um fallback silencioso para despacho local.
func unsupportedChannelKindError(ch *program.Channel) error {
	return fmt.Errorf("codegen: canal %s -> %s via %q ainda não é suportado pelo gerador (só \"direct\"/\"queue\" neste marco — providers reais como \"rabbitmq\" e clientes grpc/http ficam para um marco posterior, atrás do seam runtime.ChannelTransport)", ch.From, ch.To, ch.Via)
}

// channelWorkersSettings lê `workers { concurrency, maxRate, batchSize }` de
// ch (ausente -> os defaults de QueueChannelConfig: concurrency 1, maxRate
// ilimitado, batchSize 1 — mesmos defaults e mesma leitura de Worker,
// decl_worker.go:workerConfigInt/workerConfigObject, reusados aqui como
// estão: operam sobre []ast.ConfigEntry genérico, nada específico de
// Worker).
func channelWorkersSettings(ch *program.Channel) (concurrency, maxRate, batchSize int64, err error) {
	concurrency, batchSize = 1, 1

	obj, ok, err := workerConfigObject(ch.Decl.Entries, "workers")
	if err != nil {
		return 0, 0, 0, fmt.Errorf("canal %s -> %s: workers: %w", ch.From, ch.To, err)
	}
	if !ok {
		return concurrency, maxRate, batchSize, nil
	}

	if n, has, err := workerConfigInt(obj.Entries, "concurrency"); err != nil {
		return 0, 0, 0, fmt.Errorf("canal %s -> %s: workers.concurrency: %w", ch.From, ch.To, err)
	} else if has && n >= 1 {
		concurrency = n
	}
	if n, _, err := workerConfigInt(obj.Entries, "maxRate"); err != nil {
		return 0, 0, 0, fmt.Errorf("canal %s -> %s: workers.maxRate: %w", ch.From, ch.To, err)
	} else {
		maxRate = n
	}
	if n, has, err := workerConfigInt(obj.Entries, "batchSize"); err != nil {
		return 0, 0, 0, fmt.Errorf("canal %s -> %s: workers.batchSize: %w", ch.From, ch.To, err)
	} else if has && n >= 1 {
		batchSize = n
	}
	return concurrency, maxRate, batchSize, nil
}

// channelTimeoutGo loweriza `timeout: <duração>` de ch via l — "" (sem
// erro) quando ausente (sem timeout).
func channelTimeoutGo(ch *program.Channel, l *lower.Lowerer) (string, error) {
	goExpr, _, err := workerDurationSetting(ch.Decl.Entries, "timeout", l)
	if err != nil {
		return "", fmt.Errorf("canal %s -> %s: timeout: %w", ch.From, ch.To, err)
	}
	return goExpr, nil
}

// channelBreakerGo lê `circuitBreaker { threshold, cooldown }` de ch —
// ok=false quando ausente (breaker desabilitado, BreakerThreshold <= 0 no
// literal de QueueChannelConfig).
func channelBreakerGo(ch *program.Channel, l *lower.Lowerer) (thresholdGo, cooldownGo string, ok bool, err error) {
	obj, has, err := workerConfigObject(ch.Decl.Entries, "circuitBreaker")
	if err != nil {
		return "", "", false, fmt.Errorf("canal %s -> %s: circuitBreaker: %w", ch.From, ch.To, err)
	}
	if !has {
		return "", "", false, nil
	}

	threshold, hasThreshold, err := workerConfigInt(obj.Entries, "threshold")
	if err != nil {
		return "", "", false, fmt.Errorf("canal %s -> %s: circuitBreaker.threshold: %w", ch.From, ch.To, err)
	}
	if !hasThreshold {
		threshold = 1
	}
	cooldownGo, _, err = workerDurationSetting(obj.Entries, "cooldown", l)
	if err != nil {
		return "", "", false, fmt.Errorf("canal %s -> %s: circuitBreaker.cooldown: %w", ch.From, ch.To, err)
	}
	return fmt.Sprintf("%d", threshold), cooldownGo, true, nil
}

// channelOrderByField devolve o nome nu do campo de `orderBy` de ch ("" se
// ausente — REQ-5.16 já avisou nesse caso; a geração segue com uma única
// partição global, ver KeyFunc em rtsrc/channel.go.txt).
func channelOrderByField(ch *program.Channel) string {
	for _, entry := range ch.Decl.Entries {
		if entry.Key != "orderBy" {
			continue
		}
		if id, ok := entry.Value.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// channelEventHasField reporta se decl declara um campo de nome name — usa
// channelOrderByField para validar, ANTES de gerar código, que o campo que
// `orderBy` referencia de fato existe no(s) evento(s) que atravessam o
// canal (erro de geração claro, em vez de um KeyFunc que sempre devolve "").
func channelEventHasField(decl *ast.EventDecl, name string) bool {
	for _, f := range decl.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

// channelQueueConfigGo monta o literal Go de runtime.QueueChannelConfig a
// partir de ch (workers/timeout/circuitBreaker) — compartilhado pelos dois
// lados (produtor/consumidor, ver a doc do arquivo). usesTime reporta se o
// literal referencia "time." (timeout e/ou circuitBreaker.cooldown
// presentes) — o CHAMADOR precisa e.Import("time") quando (e só quando)
// usesTime é true (Emitter.Bytes recusa um import registrado e nunca
// referenciado no corpo).
func channelQueueConfigGo(ch *program.Channel, l *lower.Lowerer, runtimeAlias string) (configGo string, usesTime bool, err error) {
	concurrency, maxRate, batchSize, err := channelWorkersSettings(ch)
	if err != nil {
		return "", false, err
	}
	timeoutGo, err := channelTimeoutGo(ch, l)
	if err != nil {
		return "", false, err
	}
	thresholdGo, cooldownGo, hasBreaker, err := channelBreakerGo(ch, l)
	if err != nil {
		return "", false, err
	}

	fields := []string{
		fmt.Sprintf("Concurrency: %d", concurrency),
		fmt.Sprintf("MaxRate: %d", maxRate),
		fmt.Sprintf("BatchSize: %d", batchSize),
	}
	if timeoutGo != "" {
		fields = append(fields, fmt.Sprintf("Timeout: %s", timeoutGo))
		usesTime = true
	}
	if hasBreaker {
		fields = append(fields, fmt.Sprintf("BreakerThreshold: %s", thresholdGo))
		if cooldownGo != "" {
			fields = append(fields, fmt.Sprintf("BreakerCooldown: %s", cooldownGo))
			usesTime = true
		}
	}
	return fmt.Sprintf("%s.QueueChannelConfig{%s}", runtimeAlias, strings.Join(fields, ", ")), usesTime, nil
}

// emitChannelQueueVar emite "<varName> <op> runtime.NewQueueChannel(cfg,
// keyFunc)" — o transporte "queue" de ch, compartilhado pelos dois lados
// (consumidor: candidates tem 1 elemento, o Policy.On; produtor: candidates
// tem todo PublicEvent do módulo que produz ch). op é ":=" (produtor,
// generateCmdMainFile — var local de main()) ou "=" (consumidor,
// emitPolicyWireFunc — var já declarada no nível de PACOTE por
// emitPolicyChannelVarDecl, para um teste do MESMO pacote gerado poder
// publicar nele diretamente e observar a Policy rodar — ver a doc de
// emitPolicyWireFunc). Constrói seu PRÓPRIO TypeEnv/Lowerer (module/reg
// escopam só a resolução de literais de duração — nenhum receptor de corpo
// é necessário aqui).
func emitChannelQueueVar(e *emit.Emitter, varName, op string, ch *program.Channel, candidates []channelEventCandidate, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias string) error {
	env := lower.New(model, tab, module)
	l := lower.NewLowerer(env, reg, runtimeAlias).WithEmitter(e)

	cfgGo, usesTime, err := channelQueueConfigGo(ch, l, runtimeAlias)
	if err != nil {
		return err
	}
	if usesTime {
		e.Import("time")
	}

	orderByField := channelOrderByField(ch)
	if orderByField == "" {
		e.Line("%s %s %s.NewQueueChannel(%s, nil)", varName, op, runtimeAlias, cfgGo)
		return nil
	}

	// validCandidates são os eventos que DE FATO têm o campo de orderBy —
	// no lado consumidor (1 candidate, o Policy.On) isso preserva o
	// comportamento estrito de sempre (validCandidates vazio == o único
	// candidato não tem o campo == erro, exatamente como antes); no lado
	// produtor (candidates = TODO PublicEvent do módulo, nem todos
	// necessariamente atravessam este canal específico) um evento SEM o
	// campo simplesmente não ganha um "case" no switch — cai no "default"
	// (partição "", ver KeyFunc em rtsrc/channel.go.txt) em vez de barrar a
	// geração inteira por causa de um evento que talvez nem use este canal.
	// Só erra quando NENHUM candidato tem o campo: aí o "orderBy" declarado
	// não descreve nada de verdade neste canal, sinal de erro de digitação.
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

	exportField := goname.ExportField(orderByField)
	fmtAlias := e.Import("fmt")
	head := fmt.Sprintf("%s %s %s.NewQueueChannel(%s, func(ev %s.Event) string", varName, op, runtimeAlias, cfgGo, runtimeAlias)
	e.BlockSuffix(head, ")", func() {
		e.Block("switch e := ev.(type)", func() {
			for _, c := range validCandidates {
				e.Line("case %s:", c.goPtrType)
				e.Line("return %s.Sprint(e.%s)", fmtAlias, exportField)
			}
			e.Line("default:")
			e.Line("return \"\"")
		})
	})
	return nil
}

// producerChannelFor devolve o único canal de SAÍDA (Channel.From == module)
// que precisa de wiring de produtor em cmd/<service>/main.go
// (generateCmdMainFile): nil quando module não produz nenhum, ou quando
// todo canal de saída é "direct"/sem `via` (nada a fazer — já é o caminho
// in-process existente desde F1). Um `via` "queue" vira o canal devolvido;
// qualquer outro `via` (grpc/http/stream/rabbitmq) é
// unsupportedChannelKindError; mais de um canal de saída via "queue" a
// partir do MESMO módulo é um erro de geração claro — wiring combinado
// (mais de um transporte por unit of work) fica para quando um exemplo real
// precisar disso (nem o wallet nem o shop de hoje têm essa forma).
func producerChannelFor(prog *program.Program, module string) (*program.Channel, error) {
	var found *program.Channel
	for _, ch := range prog.Channels {
		if ch.From != module {
			continue
		}
		switch channelViaKind(ch) {
		case "direct":
			continue
		case "queue":
			if found != nil {
				return nil, fmt.Errorf("codegen: módulo %s tem mais de um canal de saída via queue (%s->%s e %s->%s) — wiring combinado ainda não suportado (F5)", module, found.From, found.To, ch.From, ch.To)
			}
			found = ch
		default:
			return nil, unsupportedChannelKindError(ch)
		}
	}
	return found, nil
}
