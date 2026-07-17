package codegen

import (
	"sort"

	"domainscript/ast"
	"domainscript/codegen/emit"
)

// decl_collections.go centraliza, num único arquivo por módulo
// (collections.go), a declaração dos vars de pacote "runtime.Collection[T]"
// que EmitQueries (decl_query.go, join, I5.1) e EmitPolicies (decl_policy.go,
// list/count, H4) precisariam declarar CADA UM independentemente para o
// MESMO tipo — ISSUE-1: os dois arquivos (queries.go/policies.go)
// compartilham o MESMO pacote Go; se o MESMO tipo (ex. Ticket) aparecesse
// tanto numa fonte de join de Query quanto num list/count de Policy do MESMO
// módulo, os dois emissores gerariam "var ticketCollection = ..." em
// arquivos diferentes — "go build" falhava com "redeclared in this block".
// Nenhum exemplo real exercitava essa combinação (por isso não foi pego
// antes); ver TestQueryJoinPolicyListSharedCollectionType
// (decl_collections_test.go) para o caso que reproduz e corrige o bug.
//
// A correção é cirúrgica de propósito — só o tipo REALMENTE disputado por
// ambos os lados muda de arquivo:
//
//   - sharedModuleCollectionTypeNames calcula a INTERSEÇÃO entre os tipos
//     que alguma Query do módulo usa como fonte de join
//     (queryJoinCollectionTypeNames) e os que alguma Policy do módulo usa via
//     list/count (policyCollectionTypeNames). Vazia no caso comum (nenhuma
//     colisão possível) — nenhum módulo hoje (wallet/shop, e nenhum golden
//     existente) tem essa interseção, então generateModuleFiles não emite
//     collections.go nesse caso e cada emissor continua declarando os seus
//     próprios vars, exatamente como antes — Go byte-idêntico a antes desta
//     task para TODO caso já coberto por golden (incl. a fixture GetMyTickets
//     de I5.1, que tem join mas NENHUMA Policy no mesmo módulo).
//   - Quando a interseção não é vazia, generateModuleFiles emite
//     collections.go com só ESSES tipos (EmitCollections) e repassa o mapa
//     resultante (tipo->var) a EmitQueries e EmitPolicies via o parâmetro
//     sharedCollectionVars: cada emissor continua calculando sozinho TODO o
//     conjunto de tipos que precisa (join / list-count), mas, para um tipo
//     presente em sharedCollectionVars, reusa o var já declarado em
//     collections.go em vez de declarar o seu — só os tipos NÃO
//     compartilhados continuam sendo declarados localmente em
//     queries.go/policies.go, como sempre.
//
// A convenção de nome do var (policyCollectionVarName, decl_policy.go) nunca
// muda — só ONDE ele mora quando os dois lados precisam do mesmo tipo.

// sharedModuleCollectionTypeNames devolve, em ordem alfabética (determinismo,
// NFR-13), a INTERSEÇÃO entre os nomes NUS de tipo que alguma Query deste
// módulo referencia como fonte de "join" (queryJoinCollectionTypeNames,
// decl_query.go, I5.1) e os que alguma Policy deste módulo referencia via
// "list"/"count" (policyCollectionTypeNames, decl_policy.go, H4) — os ÚNICOS
// tipos que colidiriam se cada emissor declarasse o seu var independentemente
// (ver a doc do arquivo). Vazio sempre que só um dos dois lados (ou nenhum)
// usa um tipo dado — o caso comum, incluindo qualquer módulo com só Query-com-
// join OU só Policy-com-list/count, nunca os dois sobre o MESMO tipo.
func sharedModuleCollectionTypeNames(queries []*ast.QueryDecl, policies []*ast.PolicyDecl) []string {
	policySet := make(map[string]bool)
	for _, name := range policyCollectionTypeNames(policies) {
		policySet[name] = true
	}
	var shared []string
	for _, name := range queryJoinCollectionTypeNames(queries) {
		if policySet[name] {
			shared = append(shared, name)
		}
	}
	sort.Strings(shared)
	return shared
}

// EmitCollections gera collections.go: um único "var <tipo>Collection =
// runtime.NewMemoryCollection[<Tipo>]()" por nome distinto de names (na
// ordem recebida — sharedModuleCollectionTypeNames já entrega em ordem
// alfabética, NFR-13) — só os tipos que tanto uma Query (join) quanto uma
// Policy (list/count) do mesmo módulo precisam (ver a doc do arquivo sobre
// ISSUE-1). Devolve também o mapa tipo->nome-do-var, repassado pelo CHAMADOR
// (generateModuleFiles) tanto a EmitQueries quanto a EmitPolicies. Nunca
// chamada com names vazio (generateModuleFiles só invoca quando
// sharedModuleCollectionTypeNames devolve algo).
func EmitCollections(pkg string, names []string) ([]byte, map[string]string, error) {
	e := emit.New(pkg)
	runtimeAlias := e.Import(RuntimeImportPath)
	typeToVar := make(map[string]string, len(names))
	for _, name := range names {
		v := policyCollectionVarName(name)
		typeToVar[name] = v
		e.Line("// %s é o runtime.Collection[%s], compartilhado (ISSUE-1) porque tanto uma", v, name)
		e.Line("// Query (\"list %s ... join ...\", I5.1) quanto uma Policy (\"list %s .../", name, name)
		e.Line("// count %s ...\", H4/§22.4) deste pacote referenciam %s — semeado por um", name, name)
		e.Line("// teste que aciona a Query/Policy gerada diretamente; um wiring de produção")
		e.Line("// real (popular a partir de um EventStore/projeção) fica para quando um")
		e.Line("// exemplo real precisar dele.")
		e.Line("var %s = %s.NewMemoryCollection[%s]()", v, runtimeAlias, name)
		e.Line("")
	}
	content, err := e.Bytes()
	if err != nil {
		return nil, nil, err
	}
	return content, typeToVar, nil
}
