package ast

// Nós dos arquivos de teste nativo (*.test.ds, §22). O parser reconhece a
// estrutura Given-When-Then; as validações semânticas (evento/comando
// inexistente, shape errada, fail step inexistente, mock com tipo errado) são da
// fase semântica (REQ-5.14), não daqui (NFR-6).

// TestDecl é a declaração "Test Name { scenario... property... }" (§22).
type TestDecl struct {
	baseNode
	Name       string
	Scenarios  []*ScenarioDecl
	Properties []*PropertyDecl
}

func NewTestDecl(name string, scenarios []*ScenarioDecl, properties []*PropertyDecl, span Span) *TestDecl {
	return &TestDecl{baseNode{span}, name, scenarios, properties}
}
func (*TestDecl) declNode() {}

// ScenarioDecl é um cenário "scenario \"...\" { mock/fail/given/when/then }".
type ScenarioDecl struct {
	baseNode
	Name   string
	Mocks  []*MockClause
	Fails  []*FailStep
	Givens []*GivenClause
	When   *WhenClause
	Then   *ThenClause
}

func NewScenarioDecl(name string, mocks []*MockClause, fails []*FailStep, givens []*GivenClause, when *WhenClause, then *ThenClause, span Span) *ScenarioDecl {
	return &ScenarioDecl{baseNode{span}, name, mocks, fails, givens, when, then}
}

// MockClause é "mock Target returns Returns" (§22.3).
type MockClause struct {
	baseNode
	Target  Expr
	Returns Expr
}

func NewMockClause(target, returns Expr, span Span) *MockClause {
	return &MockClause{baseNode{span}, target, returns}
}

// FailStep é "fail step Name with Error" (§22.3): injeta falha de infra num step.
type FailStep struct {
	baseNode
	Step string
	With string
}

func NewFailStep(step, with string, span Span) *FailStep {
	return &FailStep{baseNode{span}, step, with}
}

// GivenEntity é um elemento da lista de um given: uma construção de evento ou
// entidade, com estado direto opcional (Ticket("T1") { ... }).
type GivenEntity struct {
	Entity Expr
	State  *ObjectExpr // nil quando não há "{ ... }"
}

// GivenClause é uma pré-condição "given ..." nas formas: "given [eventos]",
// "given Subject from [eventos]", "given binding [entidades]" e "given state
// { ... }".
type GivenClause struct {
	baseNode
	Subject  Expr   // Wallet("W1") em "given Wallet(..) from [...]"; nil caso contrário
	Binding  string // "tickets" em "given tickets [...]"; "" caso contrário
	Entities []*GivenEntity
	State    *ObjectExpr // "given state { ... }" (StateStored)
}

func NewGivenClause(subject Expr, binding string, entities []*GivenEntity, state *ObjectExpr, span Span) *GivenClause {
	return &GivenClause{baseNode{span}, subject, binding, entities, state}
}

// WhenClause é a ação "when Action" ou "when event Action" (§22).
type WhenClause struct {
	baseNode
	IsEvent bool
	Action  Expr
}

func NewWhenClause(isEvent bool, action Expr, span Span) *WhenClause {
	return &WhenClause{baseNode{span}, isEvent, action}
}

// ThenClause é a asserção esperada nas formas: "then [eventos]", "then error
// Name" e "then { asserts }".
type ThenClause struct {
	baseNode
	Error   string        // "then error Name"
	Events  []Expr        // "then [ ... ]"
	Asserts []*ThenAssert // "then { ... }"
}

func NewThenClause(errName string, events []Expr, asserts []*ThenAssert, span Span) *ThenClause {
	return &ThenClause{baseNode{span}, errName, events, asserts}
}

// ThenAssert é uma linha de asserção de um bloco then. Cobre as formas do §22:
// "committed"/"rolledback" (só Verb), "error Name" (Error), "emitted Expr",
// "emitted count N" (Count), "Subject emitted Expr", "saga compensated",
// "compensated [steps]" (List), "called Name", "Subject released".
type ThenAssert struct {
	Span    Span
	Subject Expr   // expressão à esquerda (Wallet("W1"), Order, tickets); nil se ausente
	Verb    string // emitted/committed/rolledback/released/compensated/called/saga/...
	Object  Expr   // expressão à direita; nil se ausente
	Count   Expr   // "emitted count N"
	List    []Expr // "compensated [ ... ]"
	Error   string // "error Name"
}

// PropertyDecl é um teste baseado em propriedade (§22.5): "property \"...\" {
// forall sequence of [...] invariant ... }".
type PropertyDecl struct {
	baseNode
	Name      string
	Forall    Expr // a lista de comandos de "forall sequence of [...]"
	Invariant Expr
}

func NewPropertyDecl(name string, forall, invariant Expr, span Span) *PropertyDecl {
	return &PropertyDecl{baseNode{span}, name, forall, invariant}
}

// FixtureDecl é uma fixture reutilizável (§22.6): "Fixture name { Subject from
// [...] ... }".
type FixtureDecl struct {
	baseNode
	Name   string
	Givens []*GivenClause
}

func NewFixtureDecl(name string, givens []*GivenClause, span Span) *FixtureDecl {
	return &FixtureDecl{baseNode{span}, name, givens}
}
func (*FixtureDecl) declNode() {}
