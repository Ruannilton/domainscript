package lower

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/token"
	"domainscript/types"
)

// whereeq.go extrai, de uma cláusula "where" de list/count, o subconjunto
// DECLARATIVO exigido por REQ-38.1 (§design read-side 3.9): Query[T].WhereEq
// — um AND de igualdades "<campo> == <valor>" que o adapter SQL (codegen/
// sqlrt/collection.go.txt, I7.1) traduz para "WHERE json_extract(payload,
// '$.<campo>') = ?", reduzindo as linhas buscadas ANTES de qualquer
// pós-processamento in-memory. hoistQueryPredicate (stmt.go) continua sendo
// a ÚNICA fonte de verdade (a closure Where roda SEMPRE, com ou sem
// WhereEq) — WhereEq é só uma OTIMIZAÇÃO opcional: hoistWhereEq nunca
// devolve erro, só "" (o sentinela de ausência de queryLiteralFields.WhereEq)
// quando o "where" não se encaixa EXATAMENTE na forma reconhecida — REQ-38.2
// exige degradar (nunca falhar, nunca produzir resultado incorreto) para
// QUALQUER cláusula não descível, e "" é exatamente essa degradação: o
// caminho in-memory (SelectSlice) já ignora WhereEq por completo, então "not
// descending" nunca muda o resultado, só deixa de habilitar a otimização SQL.

// hoistWhereEq devolve o texto Go de
// "[]<runtimeAlias>.FieldEq{{Field: "<campo>", Value: <exprGo>}, ...}"
// quando TODO o "where" de n é uma conjunção AND de "<binding>.<campo> ==
// <expr>" (em qualquer ordem dos dois lados) tal que: (a) <expr> NÃO
// referencia o binding do item (um valor independente do item — ex.
// "event.id", "TicketStatus.Sold" — nunca um campo do PRÓPRIO item, que não
// faria sentido como parâmetro de uma comparação de coluna); (b) o tipo do
// campo passa tanto inComparableGoType (comparável em GO — primitivo
// comparável exceto decimal/bytes, VO wrapper ou Enum sobre um desses, nunca
// um VO composto) QUANTO whereEqSafePrimitiveType (comparável em SQL — ver a
// doc de lá sobre por que datetime/duration/size/rate também ficam de fora,
// além de decimal/bytes). Qualquer outra forma (OR, comparação
// não-igualdade, RHS que referencia o item, campo de tipo não-seguro,
// "where" ausente, ausência total de where) devolve "" sem erro algum — ver
// a doc do arquivo sobre por que "" é sempre uma degradação segura.
func (sl *StmtLowerer) hoistWhereEq(n *ast.QueryExpr) string {
	whereExpr, ok := queryClauseByKw(n.Clauses, "where")
	if !ok {
		return ""
	}

	paramName := n.Binding
	if paramName == "" {
		paramName = "item"
	}
	itemType, err := sl.env.ItemTypeOf(n)
	if err != nil {
		return ""
	}
	fields, err := fieldsOfType(itemType)
	if err != nil {
		return ""
	}
	fieldTypes := make(map[string]bool, len(fields))
	for _, f := range fields {
		if _, err := inComparableGoType(f.Type); err != nil {
			continue
		}
		if !whereEqSafePrimitiveType(f.Type) {
			continue
		}
		fieldTypes[f.Name] = true
	}

	entries := make([]string, 0)
	for _, conjunct := range flattenAndConjuncts(whereExpr) {
		bin, ok := conjunct.(*ast.BinaryExpr)
		if !ok || bin.Op != token.EQ {
			return ""
		}
		fieldName, rhs, ok := fieldEqOperands(bin, paramName)
		if !ok || !fieldTypes[fieldName] {
			return ""
		}
		if exprReferencesIdent(rhs, paramName) {
			return ""
		}
		rhsGo, err := sl.Expr(rhs)
		if err != nil {
			return ""
		}
		entries = append(entries, fmt.Sprintf("{Field: %q, Value: %s}", fieldName, rhsGo))
	}
	if len(entries) == 0 {
		return ""
	}
	return fmt.Sprintf("[]%s.FieldEq{%s}", sl.runtimeAlias, strings.Join(entries, ", "))
}

// unsafeWhereEqPrimitives são os primitivos cujo valor Go, passado como
// argumento de uma query SQL (via database/sql), NÃO produz o MESMO texto/
// forma que json_extract(payload,'$.<campo>') devolve para o MESMO campo —
// uma igualdade "coluna = ?" contra um desses NUNCA bateria, silenciosamente
// devolvendo zero linhas em vez de um erro (REQ-38.2 exige "nunca resultado
// incorreto", não "falhar ruidosamente"). decimal/bytes já ficam de fora por
// inComparableGoType (goTypeString os mapeia para runtime.Decimal/[]byte,
// nenhum dos dois com forma de argumento SQL nativa); os três aqui têm o
// MESMO risco por uma razão diferente: datetime (time.Time) serializa em
// JSON como uma STRING RFC3339 (MarshalJSON, rtsrc), mas a maioria dos
// drivers database/sql vincula um valor time.Time NATIVAMENTE (não como
// esse mesmo texto formatado) — a comparação nunca bateria; duration/size/
// rate dependem de como CADA driver escolhe representar o tipo Go
// subjacente (não só do reflect.Kind), um risco não totalmente provado caso
// a caso — excluídos por precaução em vez de auditar driver a driver: a
// única perda é a otimização SQL (sempre disponível via degradação
// in-memory), nunca a correção.
var unsafeWhereEqPrimitives = map[string]bool{
	"decimal": true, "bytes": true, "rate": true,
	"datetime": true, "duration": true, "size": true,
}

// whereEqSafePrimitiveType reporta se t é seguro para virar WhereEq — além
// de já ter passado inComparableGoType (comparável em GO), o primitivo de
// base (direto, ou de um VO wrapper/Enum) não pode estar em
// unsafeWhereEqPrimitives (comparável em SQL). t que não resolve a nenhum
// primitivo (nunca deveria acontecer aqui — inComparableGoType já teria
// recusado um VO composto antes desta função ser chamada) é tratado como
// seguro por omissão: não há NADA na tabela para recusar.
func whereEqSafePrimitiveType(t types.Type) bool {
	name, ok := primitiveBaseName(t)
	if !ok {
		return true
	}
	return !unsafeWhereEqPrimitives[name]
}

// primitiveBaseName devolve o Name do primitivo de base de t: o próprio,
// para um *types.Primitive; o Base, para um *types.VOType wrapper ou
// *types.EnumType (ambos sempre sobre um primitivo, §design read-side 3.2).
// ok=false para qualquer outra forma (ex. um VO composto, cujo Base é nil).
func primitiveBaseName(t types.Type) (string, bool) {
	switch x := t.(type) {
	case *types.Primitive:
		return x.Name, true
	case *types.VOType:
		if p, ok := x.Base.(*types.Primitive); ok {
			return p.Name, true
		}
	case *types.EnumType:
		if p, ok := x.Base.(*types.Primitive); ok {
			return p.Name, true
		}
	}
	return "", false
}

// flattenAndConjuncts descende recursivamente por BinaryExpr(token.AND),
// devolvendo cada folha na ordem textual esquerda->direita; qualquer outra
// forma (incl. um BinaryExpr(token.OR)) é devolvida como sua PRÓPRIA folha —
// hoistWhereEq recusa o conjunto inteiro assim que uma folha não é uma
// igualdade de membro simples (nunca tenta decompor um OR parcialmente).
func flattenAndConjuncts(e ast.Expr) []ast.Expr {
	if bin, ok := e.(*ast.BinaryExpr); ok && bin.Op == token.AND {
		return append(flattenAndConjuncts(bin.Left), flattenAndConjuncts(bin.Right)...)
	}
	return []ast.Expr{e}
}

// fieldEqOperands reconhece "<paramName>.<campo> == <rhs>" OU "<rhs> ==
// <paramName>.<campo>" (em qualquer ordem) — devolve o nome do campo e o
// lado RHS (o operando que NÃO é o acesso de membro sobre paramName).
// ok=false para qualquer outra forma (nenhum lado é um MemberExpr sobre
// paramName, ou AMBOS são — "item.a == item.b" não tem RHS independente).
func fieldEqOperands(bin *ast.BinaryExpr, paramName string) (field string, rhs ast.Expr, ok bool) {
	leftField, leftIsMember := memberFieldOf(bin.Left, paramName)
	rightField, rightIsMember := memberFieldOf(bin.Right, paramName)
	switch {
	case leftIsMember && !rightIsMember:
		return leftField, bin.Right, true
	case rightIsMember && !leftIsMember:
		return rightField, bin.Left, true
	default:
		return "", nil, false
	}
}

// memberFieldOf devolve o nome do campo quando e é EXATAMENTE
// "<paramName>.<campo>" (um MemberExpr cujo X é o Ident paramName).
func memberFieldOf(e ast.Expr, paramName string) (string, bool) {
	mem, ok := e.(*ast.MemberExpr)
	if !ok {
		return "", false
	}
	id, ok := mem.X.(*ast.Ident)
	if !ok || id.Name != paramName {
		return "", false
	}
	return mem.Name, true
}

// exprReferencesIdent reporta se e contém, em qualquer profundidade, um
// *ast.Ident de nome name — usado para garantir que o RHS de uma igualdade
// candidata a WhereEq não referencia o binding do item (um valor que só
// existe DENTRO do predicado por item não pode virar um parâmetro de coluna
// SQL avaliado uma única vez fora do loop).
func exprReferencesIdent(e ast.Expr, name string) bool {
	found := false
	astutil.ForEachExpr(e, func(sub ast.Expr) {
		if id, ok := sub.(*ast.Ident); ok && id.Name == name {
			found = true
		}
	})
	return found
}
