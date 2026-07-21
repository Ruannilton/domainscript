package codegen

import (
	"fmt"

	"domainscript/codegen/emit"
)

// run_error.go dá suporte ao wiring multi-recurso fail-closed de
// cmd/<service>/main.go (task J6.2, REQ-47.2/47.3, §design infra-providers
// 3.6): quando o corpo de main() abre 2+ recursos reais em sequência (uma
// Database XA, o Database do outbox durável, o canal produtor rabbitmq, uma
// FileStorage s3 — ver runMainAsFunc/generateCmdMainFile, codegen.go), um
// "log.Fatal" no meio pularia o "defer Close()" de todo recurso já aberto
// ANTES dele — por isso o corpo inteiro migra para um "func run() error"
// (cada passo faz "return err", os defers já registrados rodam no unwind) e
// "func main()" vira só "if err := run(); err != nil { log.Fatal(err) }" —
// o log.Fatal ÚNICO do programa. Com 0 ou 1 desses recursos (o caso comum —
// wallet, shop e toda fixture de J1-J5 hoje), nada disto se aplica: o corpo
// continua exatamente "func main() { ... log.Fatal(err) inline ... }", Go
// byte-idêntico ao de antes desta task (NFR-21/23) — emitFailFast/
// emitDeferOnClose/emitDeferChannelClose abaixo são todos no-ops nesse
// caso (runMode==false).

// emitFailFast emite a checagem de erro de UM passo fallível de abertura de
// recurso (uma Database XA, o Database do outbox, o canal produtor
// rabbitmq, uma FileStorage s3, o listener gRPC) — errVar é o nome da
// variável de erro já declarada pelo chamador (ex. "err", "listenErr",
// "<canal>Err"). runMode==false (o caso comum) preserva EXATAMENTE a forma
// de sempre ("log.Fatal(errVar)" inline) — nenhuma mudança de Go gerado
// para qualquer fixture com 0 ou 1 recurso (NFR-21/23). runMode==true
// (2+ recursos no mesmo corpo) troca para "return errVar": um log.Fatal
// aqui pularia o defer Close() de qualquer recurso já aberto ANTES deste
// passo — só "func main()" (que envolve a chamada a run()) chama log.Fatal,
// uma única vez, depois que todo defer já rodou.
func emitFailFast(e *emit.Emitter, errVar, logAlias string, runMode bool) {
	if !runMode {
		e.Line("if %s != nil { %s.Fatal(%s) }", errVar, logAlias, errVar)
		return
	}
	e.Line("if %s != nil { return %s }", errVar, errVar)
}

// emitDeferClose emite "defer <varName>.Close()" logo após um recurso do
// tipo *sql.DB (dbVar de emitXADatabaseWiring/emitOutboxDatabaseWiring) ter
// sido aberto com sucesso — só quando runMode (ver a doc do arquivo): sem
// isso, "func main()" nunca teve defer nenhum (NFR-21), e continuar sem
// emitir nada aqui preserva esse comportamento exatamente.
func emitDeferClose(e *emit.Emitter, varName string, runMode bool) {
	if !runMode {
		return
	}
	e.Line("defer %s.Close()", varName)
}

// emitDeferChannelClose emite o defer Close() do canal produtor rabbitmq —
// só quando runMode (ver a doc do arquivo). O var tem tipo estático
// runtime.ChannelTransport (a interface do seam, sem método Close — ver
// rtsrc/channel.go.txt); só a implementação REAL (amqpruntime.
// NewRabbitMQChannel) tem Close, então a asserção de tipo é feita direto,
// sem checar "ok" — esta função só é chamada quando o provider selecionado
// já é "rabbitmq" (channelProviderKind == "rabbitmq"), então a asserção
// SEMPRE sucede em tempo de execução; nunca panica.
func emitDeferChannelClose(e *emit.Emitter, varName string, runMode bool) {
	if !runMode {
		return
	}
	e.Line("defer %s.(interface{ Close() error }).Close()", varName)
}

// emitFailFastBlock é a variante de emitFailFast para o formato de bloco
// ("if errVar != nil { ... }") usado por sites que hoje emitem múltiplas
// linhas dentro do corpo do if (ex. o listener gRPC, que só faz log.Fatal)
// — mantida como e.Block em vez de e.Line para não alterar a doc/nota de
// código já presente nesses sites; gofmt normaliza as duas formas de
// qualquer forma (confirmado: format.Source produz bytes idênticos para
// "if x { y }" numa linha só e para a forma multi-linha equivalente), então
// esta função existe só por clareza no ponto de chamada, não por
// necessidade de determinismo.
func emitFailFastBlock(e *emit.Emitter, errVar, logAlias string, runMode bool) {
	if !runMode {
		e.Block(fmt.Sprintf("if %s != nil", errVar), func() {
			e.Line("%s.Fatal(%s)", logAlias, errVar)
		})
		return
	}
	e.Line("if %s != nil { return %s }", errVar, errVar)
}
