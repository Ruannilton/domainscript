package symbols

import "domainscript/ast"

// Kind classifica um símbolo declarado pela linguagem. É a categoria sob a qual
// um nome vive na tabela; o resolver e o checker a consultam para distinguir, por
// exemplo, um Command de um Event ao resolver handles/on (REQ-4.4).
type Kind int

const (
	KindValueObject Kind = iota
	KindEnum
	KindError
	KindEvent
	KindAggregate
	KindCommand
	KindUseCase
	KindView
	KindProjection
	KindQuery
	KindPolicy
	KindWorker
	KindNotification
	KindAdapter
	KindSaga
	KindMetric
	KindFixture
)

var kindNames = map[Kind]string{
	KindValueObject:  "ValueObject",
	KindEnum:         "Enum",
	KindError:        "Error",
	KindEvent:        "Event",
	KindAggregate:    "Aggregate",
	KindCommand:      "Command",
	KindUseCase:      "UseCase",
	KindView:         "View",
	KindProjection:   "Projection",
	KindQuery:        "Query",
	KindPolicy:       "Policy",
	KindWorker:       "Worker",
	KindNotification: "Notification",
	KindAdapter:      "Adapter",
	KindSaga:         "Saga",
	KindMetric:       "Metric",
	KindFixture:      "Fixture",
}

// String devolve o nome legível do Kind para mensagens de diagnóstico.
func (k Kind) String() string {
	if s, ok := kindNames[k]; ok {
		return s
	}
	return "Kind(?)"
}

// Symbol é uma declaração nomeada registrada na tabela. Decl aponta para o nó
// declarante (para spans e detalhes); Module é o módulo dono; Public distingue um
// PublicEvent (compartilhado via contracts/) de um Event privado (REQ-7.4).
type Symbol struct {
	Name   string
	Kind   Kind
	Module string
	Decl   ast.Decl
	Public bool
}

// SymbolTable guarda os símbolos com escopo por módulo e um nível público para
// os símbolos compartilhados entre módulos (PublicEvent). É a fonte única de
// verdade para resolução de nomes e regras semânticas (§design 3.6, REQ-4.1).
type SymbolTable struct {
	byModule map[string]map[string]*Symbol
	public   map[string]*Symbol
}

// New cria uma tabela vazia.
func New() *SymbolTable {
	return &SymbolTable{
		byModule: make(map[string]map[string]*Symbol),
		public:   make(map[string]*Symbol),
	}
}

// Define registra sym no seu módulo. Se já existe um símbolo de mesmo nome no
// mesmo módulo, não sobrescreve e devolve (símbolo existente, false) — o chamador
// reporta a declaração duplicada (REQ-4.3). Caso contrário devolve (sym, true).
// Símbolos públicos também ficam acessíveis no nível público (REQ-7.4).
func (t *SymbolTable) Define(sym *Symbol) (*Symbol, bool) {
	mod := t.byModule[sym.Module]
	if mod == nil {
		mod = make(map[string]*Symbol)
		t.byModule[sym.Module] = mod
	}
	if existing, dup := mod[sym.Name]; dup {
		return existing, false
	}
	mod[sym.Name] = sym
	if sym.Public {
		t.public[sym.Name] = sym
	}
	return sym, true
}

// Lookup procura name no módulo dado e, em seguida, no nível público (para
// resolver referências a PublicEvent de outro módulo). Reporta o símbolo e se foi
// encontrado.
func (t *SymbolTable) Lookup(module, name string) (*Symbol, bool) {
	if mod := t.byModule[module]; mod != nil {
		if s, ok := mod[name]; ok {
			return s, true
		}
	}
	if s, ok := t.public[name]; ok {
		return s, true
	}
	return nil, false
}

// LookupPublic procura um símbolo apenas no nível público.
func (t *SymbolTable) LookupPublic(name string) (*Symbol, bool) {
	s, ok := t.public[name]
	return s, ok
}

// Module devolve o mapa nome→símbolo de um módulo (nil se o módulo não tem
// símbolos). O mapa não deve ser modificado pelo chamador.
func (t *SymbolTable) Module(module string) map[string]*Symbol {
	return t.byModule[module]
}
