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

// Route é uma rota HTTP de interface.ds: METHOD "path" -> Target { opções }.
type Route struct {
	baseNode
	Method  string
	Path    string
	Target  string
	Options []ConfigEntry
}

func NewRoute(method, path, target string, options []ConfigEntry, span Span) *Route {
	return &Route{baseNode{span}, method, path, target, options}
}

// GrpcRPC é um método de um service gRPC: rpc Name -> Target.
type GrpcRPC struct {
	Name   string
	Target string
}

// GrpcService é um bloco "service Name { rpc ... }" de uma Interface GRPC.
type GrpcService struct {
	baseNode
	Name string
	RPCs []GrpcRPC
}

func NewGrpcService(name string, rpcs []GrpcRPC, span Span) *GrpcService {
	return &GrpcService{baseNode{span}, name, rpcs}
}

// InterfaceDecl é a declaração de uma interface de exposição (interface.ds,
// §10): protocolo (Kind: HTTP/GRPC/...), configurações e sub-blocos (port,
// basePath, versioning, tenant, rateLimit), rotas HTTP e services gRPC.
type InterfaceDecl struct {
	baseNode
	Kind     string
	Settings []ConfigEntry
	Routes   []*Route
	Services []*GrpcService
}

func NewInterfaceDecl(kind string, settings []ConfigEntry, routes []*Route, services []*GrpcService, span Span) *InterfaceDecl {
	return &InterfaceDecl{baseNode{span}, kind, settings, routes, services}
}
func (*InterfaceDecl) declNode() {}

// RateLimitTierDecl é um tier de rate limit por plano (§17): RateLimitTier Name
// { perUser: ..., perTenant: ... }.
type RateLimitTierDecl struct {
	baseNode
	Name    string
	Entries []ConfigEntry
}

func NewRateLimitTierDecl(name string, entries []ConfigEntry, span Span) *RateLimitTierDecl {
	return &RateLimitTierDecl{baseNode{span}, name, entries}
}
func (*RateLimitTierDecl) declNode() {}
