package ast

// ObjectExpr é um literal de objeto de configuração: { Key: Value, ... }. É o
// valor composto genérico dos arquivos de infraestrutura (mod.ds, interface.ds,
// topology.ds), reusável dentro de listas e aninhado em si mesmo (ex.: retry: {
// attempts: 3, backoff: "exponential" }). Reaproveita ConfigEntry como linha.
type ObjectExpr struct {
	baseNode
	Entries []ConfigEntry
}

func NewObjectExpr(entries []ConfigEntry, span Span) *ObjectExpr {
	return &ObjectExpr{baseNode{span}, entries}
}
func (*ObjectExpr) exprNode() {}

// ConfigBlock é um bloco de configuração nomeado ou anônimo dentro de um arquivo
// de infraestrutura: Kind [Name] { entries }. Cobre os blocos de mod.ds
// (Database WalletDb {...}, Idempotency {...}, Cache {...}, ...) e os de
// interface.ds (versioning, tenant, ...). Name é "" para blocos anônimos.
type ConfigBlock struct {
	baseNode
	Kind    string
	Name    string
	Entries []ConfigEntry
}

func NewConfigBlock(kind, name string, entries []ConfigEntry, span Span) *ConfigBlock {
	return &ConfigBlock{baseNode{span}, kind, name, entries}
}

// ModuleDecl é a declaração de um módulo (mod.ds, §12): configurações de topo
// (ex.: timeout) e os blocos de infraestrutura (Database, FileStorage,
// Idempotency, Cache, RateLimit, Outbox, Telemetry).
type ModuleDecl struct {
	baseNode
	Name     string
	Settings []ConfigEntry
	Blocks   []*ConfigBlock
}

func NewModuleDecl(name string, settings []ConfigEntry, blocks []*ConfigBlock, span Span) *ModuleDecl {
	return &ModuleDecl{baseNode{span}, name, settings, blocks}
}
func (*ModuleDecl) declNode() {}
