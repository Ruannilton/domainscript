package resolver

// scope.go implementa o modelo de escopo léxico usado na resolução de nomes em
// corpos executáveis (REQ-9.2/9.5, §design type-checking 3.1). Um Scope é uma
// cadeia de mapas nome→Binding: a raiz de um corpo é semeada com os parâmetros do
// construto e seus receptores contextuais (§3.2); cada binder aninhado (for,
// lambda, braço de match, binding de query) abre um Child(), descartado ao sair.
// Push/pop é estrutural (não uma pilha global mutável), o que mantém a resolução
// determinística (NFR-9).

// BindingKind classifica a origem de um nome introduzido no escopo. Distinguir a
// origem permite mensagens mais precisas e prepara o terreno para a checagem de
// tipos (Fase C/D), que tratará receptores e parâmetros de forma diferente.
type BindingKind int

const (
	// BindParam é um parâmetro nomeado do construto (Handle/Operator/UseCase/Query)
	// ou de uma lambda.
	BindParam BindingKind = iota
	// BindLocal é um nome introduzido por um binder local: variável de for, alias
	// de binding de query (list T t, as) ou binding de braço de match.
	BindLocal
	// BindReceiver é um receptor contextual implícito do construto: self, state,
	// event, caller, value, ok (§3.2).
	BindReceiver
)

// Binding é o que um nome liga num escopo. Por ora carrega só nome e origem; a
// Fase C anexará o tipo do nome (para que a checagem de membro/compatibilidade o
// consuma direto), sem mudar a forma de uso na resolução de nomes.
type Binding struct {
	Name string
	Kind BindingKind
}

// Scope é um nível de escopo léxico, encadeado ao pai. A raiz tem parent nil.
type Scope struct {
	parent *Scope
	names  map[string]Binding
}

// NewScope cria um escopo raiz (sem pai).
func NewScope() *Scope {
	return &Scope{names: make(map[string]Binding)}
}

// Child abre um novo escopo aninhado sobre s (push: entra num binder). O filho
// enxerga os nomes do pai, mas nomes nele definidos somem ao ser descartado
// (REQ-9.5).
func (s *Scope) Child() *Scope {
	return &Scope{parent: s, names: make(map[string]Binding)}
}

// Define registra name no nível atual, sombreando qualquer ligação de mesmo nome
// num escopo ancestral enquanto este nível existir.
func (s *Scope) Define(name string, b Binding) {
	if name == "" {
		return
	}
	s.names[name] = b
}

// Lookup procura name a partir deste escopo subindo a cadeia até a raiz. Devolve a
// ligação mais interna (sombra) e se foi encontrada.
func (s *Scope) Lookup(name string) (Binding, bool) {
	for sc := s; sc != nil; sc = sc.parent {
		if b, ok := sc.names[name]; ok {
			return b, true
		}
	}
	return Binding{}, false
}
