package ast

// TypeRef é uma referência a um tipo, possivelmente genérico: Money, string,
// AppendList<StatementEntry>, Map<string, string>.
type TypeRef struct {
	baseNode
	Name string
	Args []*TypeRef
}

func NewTypeRef(name string, args []*TypeRef, span Span) *TypeRef {
	return &TypeRef{baseNode{span}, name, args}
}

// Field é um campo de declaração ou um parâmetro: Name Type, com modificadores
// opcionais. Ref marca a forma "ref Tipo" (type-safety de Command); Redactable
// marca campos GDPR; Default guarda o valor padrão (versionamento de evento).
type Field struct {
	baseNode
	Name       string
	Type       *TypeRef
	Ref        bool
	Redactable bool
	Default    Expr
}

func NewField(name string, typ *TypeRef, ref, redactable bool, def Expr, span Span) *Field {
	return &Field{baseNode{span}, name, typ, ref, redactable, def}
}

// OperatorDecl é um operador de ValueObject: Operator Op(Params) -> Return { Body }.
type OperatorDecl struct {
	baseNode
	Op     string
	Params []*Field
	Return *TypeRef
	Body   *Block
}

func NewOperatorDecl(op string, params []*Field, ret *TypeRef, body *Block, span Span) *OperatorDecl {
	return &OperatorDecl{baseNode{span}, op, params, ret, body}
}

// ValueObjectDecl é a declaração de um ValueObject (§2.2). Base é o tipo
// embrulhado na forma wrapper (ValueObject Email(string)); Fields, a forma
// composta. Valid é o bloco de auto-validação; Operators, os operadores.
type ValueObjectDecl struct {
	baseNode
	Name      string
	Base      *TypeRef
	Fields    []*Field
	Valid     *Block
	Operators []*OperatorDecl
}

func NewValueObjectDecl(name string, base *TypeRef, fields []*Field, valid *Block, ops []*OperatorDecl, span Span) *ValueObjectDecl {
	return &ValueObjectDecl{baseNode{span}, name, base, fields, valid, ops}
}
func (*ValueObjectDecl) declNode() {}

// EnumMember é um valor nomeado de um Enum: Name = Value.
type EnumMember struct {
	baseNode
	Name  string
	Value Expr
}

func NewEnumMember(name string, value Expr, span Span) *EnumMember {
	return &EnumMember{baseNode{span}, name, value}
}

// CoerceBlock é a coerção explícita de um Enum: coerce from Type { Body }.
type CoerceBlock struct {
	baseNode
	From *TypeRef
	Body *Block
}

func NewCoerceBlock(from *TypeRef, body *Block, span Span) *CoerceBlock {
	return &CoerceBlock{baseNode{span}, from, body}
}

// EnumDecl é a declaração de um Enum (§2.3): conjunto fechado de membros sob um
// tipo base (após ':'), com bloco coerce opcional.
type EnumDecl struct {
	baseNode
	Name    string
	Base    *TypeRef
	Members []*EnumMember
	Coerce  *CoerceBlock
}

func NewEnumDecl(name string, base *TypeRef, members []*EnumMember, coerce *CoerceBlock, span Span) *EnumDecl {
	return &EnumDecl{baseNode{span}, name, base, members, coerce}
}
func (*EnumDecl) declNode() {}

// ErrorTypeDecl é a declaração de um Error de negócio (§4.1): Error Name {
// message "..." }. (O nome ErrorDecl é reservado ao nó de erro de sintaxe.)
type ErrorTypeDecl struct {
	baseNode
	Name    string
	Message Expr
}

func NewErrorTypeDecl(name string, message Expr, span Span) *ErrorTypeDecl {
	return &ErrorTypeDecl{baseNode{span}, name, message}
}
func (*ErrorTypeDecl) declNode() {}

// EventDecl é a declaração de um Event ou PublicEvent (§4.2). Public distingue o
// evento compartilhado (contracts/) do privado ao módulo (REQ-7.4).
type EventDecl struct {
	baseNode
	Name   string
	Public bool
	Fields []*Field
}

func NewEventDecl(name string, public bool, fields []*Field, span Span) *EventDecl {
	return &EventDecl{baseNode{span}, name, public, fields}
}
func (*EventDecl) declNode() {}

// StorageEntry é uma linha do bloco storage de um Aggregate: Key: Value.
type StorageEntry struct {
	Key   string
	Value string
}

// AccessRule é uma regra do bloco access: Handle requires Condition.
type AccessRule struct {
	baseNode
	Name      string
	Condition Expr
}

func NewAccessRule(name string, cond Expr, span Span) *AccessRule {
	return &AccessRule{baseNode{span}, name, cond}
}

// HandleDecl é um Handle de Aggregate: Handle Name(Params) { Body }.
type HandleDecl struct {
	baseNode
	Name   string
	Params []*Field
	Body   *Block
}

func NewHandleDecl(name string, params []*Field, body *Block, span Span) *HandleDecl {
	return &HandleDecl{baseNode{span}, name, params, body}
}

// ApplyDecl é um Apply de Aggregate: Apply Event { Body }.
type ApplyDecl struct {
	baseNode
	Event string
	Body  *Block
}

func NewApplyDecl(event string, body *Block, span Span) *ApplyDecl {
	return &ApplyDecl{baseNode{span}, event, body}
}

// AggregateDecl é a declaração de um Aggregate (§4.5): estratégia, snapshot,
// storage, state, access (closed-by-default), Handles e Applies.
type AggregateDecl struct {
	baseNode
	Name     string
	Strategy string
	Snapshot Expr // contagem do "snapshot every N events", ou nil
	Storage  []StorageEntry
	State    []*Field
	Access   []*AccessRule
	Handlers []*HandleDecl
	Appliers []*ApplyDecl
}

func NewAggregateDecl(name string, strategy string, snapshot Expr, storage []StorageEntry, state []*Field, access []*AccessRule, handlers []*HandleDecl, appliers []*ApplyDecl, span Span) *AggregateDecl {
	return &AggregateDecl{baseNode{span}, name, strategy, snapshot, storage, state, access, handlers, appliers}
}
func (*AggregateDecl) declNode() {}

// CommandDecl é a declaração de um Command (§5.1): campos com ValueObjects/Enums
// e referências via ref.
type CommandDecl struct {
	baseNode
	Name   string
	Fields []*Field
}

func NewCommandDecl(name string, fields []*Field, span Span) *CommandDecl {
	return &CommandDecl{baseNode{span}, name, fields}
}
func (*CommandDecl) declNode() {}

// UseCaseDecl é a declaração de um UseCase (§5.2): trata um Command (Handles),
// com timeout/idempotency/tenancy opcionais e um bloco execute (Unit of Work).
type UseCaseDecl struct {
	baseNode
	Name        string
	Handles     string
	Timeout     Expr
	Idempotency Expr
	Tenancy     string
	Execute     *Block
}

func NewUseCaseDecl(name, handles string, timeout, idempotency Expr, tenancy string, execute *Block, span Span) *UseCaseDecl {
	return &UseCaseDecl{baseNode{span}, name, handles, timeout, idempotency, tenancy, execute}
}
func (*UseCaseDecl) declNode() {}

// ConfigEntry é uma linha "Key: Value" de um bloco de configuração (cache,
// mod.ds, interface.ds, ...). Value cobre literais, durações, listas e chamadas.
type ConfigEntry struct {
	Key   string
	Value Expr
}

// MapEntry é uma linha "Name = Value" de um bloco map (Projection) ou similar.
type MapEntry struct {
	Name  string
	Value Expr
}

// ViewDecl é a declaração de uma View (§6.1): projeção de leitura, derivada de um
// Aggregate (From) ou com campos próprios, com bloco visibility opcional (§6.2).
type ViewDecl struct {
	baseNode
	Name       string
	From       string
	Fields     []*Field
	Visibility []*AccessRule
}

func NewViewDecl(name, from string, fields []*Field, visibility []*AccessRule, span Span) *ViewDecl {
	return &ViewDecl{baseNode{span}, name, from, fields, visibility}
}
func (*ViewDecl) declNode() {}

// ProjectionDecl é a declaração de uma Projection cross-aggregate (§6.4).
type ProjectionDecl struct {
	baseNode
	Name      string
	Sources   []string
	Map       []MapEntry
	RefreshOn Expr
}

func NewProjectionDecl(name string, sources []string, m []MapEntry, refreshOn Expr, span Span) *ProjectionDecl {
	return &ProjectionDecl{baseNode{span}, name, sources, m, refreshOn}
}
func (*ProjectionDecl) declNode() {}

// QueryDecl é a declaração de uma Query (§6.3): parâmetros, tipo de retorno,
// política de cache opcional (§15) e corpo.
type QueryDecl struct {
	baseNode
	Name   string
	Params []*Field
	Return *TypeRef
	Cache  []ConfigEntry
	Body   *Block
}

func NewQueryDecl(name string, params []*Field, ret *TypeRef, cache []ConfigEntry, body *Block, span Span) *QueryDecl {
	return &QueryDecl{baseNode{span}, name, params, ret, cache, body}
}
func (*QueryDecl) declNode() {}

// PolicyDecl é a declaração de uma Policy (§7): reage a um Event (On) com uma
// garantia de entrega (Delivery) e um bloco execute.
type PolicyDecl struct {
	baseNode
	Name     string
	On       string
	Delivery string
	Execute  *Block
}

func NewPolicyDecl(name, on, delivery string, execute *Block, span Span) *PolicyDecl {
	return &PolicyDecl{baseNode{span}, name, on, delivery, execute}
}
func (*PolicyDecl) declNode() {}

// WorkerDecl é a declaração de um Worker (§8): schedule (every/cron/continuous),
// settings (concurrency, batchSize, maxRate, timeout), scope, source e execute.
type WorkerDecl struct {
	baseNode
	Name         string
	Schedule     string
	ScheduleArg  Expr
	Scope        string
	Settings     []ConfigEntry
	Source       *Block
	ExecuteParam string
	Execute      *Block
}

func NewWorkerDecl(name, schedule string, scheduleArg Expr, scope string, settings []ConfigEntry, source *Block, executeParam string, execute *Block, span Span) *WorkerDecl {
	return &WorkerDecl{baseNode{span}, name, schedule, scheduleArg, scope, settings, source, executeParam, execute}
}
func (*WorkerDecl) declNode() {}

// NotificationDecl é a declaração de uma Notification (§9.1): contrato de saída.
type NotificationDecl struct {
	baseNode
	Name   string
	Fields []*Field
}

func NewNotificationDecl(name string, fields []*Field, span Span) *NotificationDecl {
	return &NotificationDecl{baseNode{span}, name, fields}
}
func (*NotificationDecl) declNode() {}

// AdapterDecl é a declaração de um Adapter (§9.3): HTTP declarativo (mode, http,
// headers, body) ou FFI vinculado a Notification (mode, foreign/from, function,
// map).
type AdapterDecl struct {
	baseNode
	Name       string
	Mode       string
	HTTPMethod string
	HTTPUrl    Expr
	Headers    []MapEntry
	Body       []MapEntry
	Lang       Expr
	From       Expr
	Function   Expr
	Map        []MapEntry
}

func NewAdapterDecl(d *AdapterDecl, span Span) *AdapterDecl {
	d.baseNode = baseNode{span}
	return d
}
func (*AdapterDecl) declNode() {}

// ForeignFunc é uma assinatura de função de um bloco Foreign.
type ForeignFunc struct {
	baseNode
	Name   string
	Params []*Field
	Return *TypeRef
}

func NewForeignFunc(name string, params []*Field, ret *TypeRef, span Span) *ForeignFunc {
	return &ForeignFunc{baseNode{span}, name, params, ret}
}

// ForeignDecl é a declaração de FFI geral (§9.4): Foreign "lang" from "path" {
// function ... }.
type ForeignDecl struct {
	baseNode
	Lang      Expr
	From      Expr
	Functions []*ForeignFunc
}

func NewForeignDecl(lang, from Expr, fns []*ForeignFunc, span Span) *ForeignDecl {
	return &ForeignDecl{baseNode{span}, lang, from, fns}
}
func (*ForeignDecl) declNode() {}
