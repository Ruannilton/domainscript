package resolver

// receivers.go define os receptores contextuais — os nomes implícitos disponíveis
// dentro de cada construto (self, state, event, caller, value, ok, cmd) — semeados
// no Scope raiz de um corpo antes de resolvê-lo (REQ-9.3, §design type-checking
// 3.2). Esta tabela é o único ponto a editar quando um construto novo ganha um
// receptor (NFR-5).

// construct identifica o corpo executável sendo resolvido, para selecionar seus
// receptores. Os parâmetros nomeados (de Handle/Operator/Query/Worker) não entram
// aqui: vêm da própria declaração e são semeados à parte pela resolução de corpos.
type construct int

const (
	constructHandle         construct = iota // Handle de Aggregate
	constructApply                           // Apply de Aggregate
	constructAccess                          // regra do bloco access de Aggregate
	constructValid                           // bloco Valid de ValueObject
	constructOperator                        // corpo de Operator de ValueObject
	constructUseCaseExecute                  // execute de UseCase
	constructPolicyExecute                   // execute de Policy
	constructCoerce                          // bloco coerce de Enum
	constructSagaStep                        // up/down/onInfraError de um passo de Saga
	constructQuery                           // corpo de Query (só params)
	constructWorkerSource                    // bloco source de Worker
	constructWorkerExecute                   // execute de Worker (param próprio)
)

// contextualReceiverNames mapeia cada construto aos receptores contextuais que
// ficam visíveis no seu corpo (§design type-checking 3.2). Construtos cujo corpo
// só enxerga parâmetros e símbolos do módulo (Query, Worker) não têm entrada.
var contextualReceiverNames = map[construct][]string{
	constructHandle:         {"self", "state", "caller"},
	constructApply:          {"state", "event"},
	constructAccess:         {"self", "caller"},
	constructValid:          {"value", "ok"},
	constructOperator:       {"value"},
	constructUseCaseExecute: {"cmd", "caller"},
	constructPolicyExecute:  {"event", "caller"},
	constructCoerce:         {"value"},
	constructSagaStep:       {"state"},
}

// seedReceivers define no escopo raiz os receptores contextuais do construto.
func seedReceivers(sc *Scope, c construct) {
	for _, name := range contextualReceiverNames[c] {
		sc.Define(name, Binding{Name: name, Kind: BindReceiver})
	}
}
