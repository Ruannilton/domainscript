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
