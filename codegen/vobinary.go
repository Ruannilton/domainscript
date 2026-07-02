package codegen

import (
	"fmt"

	"domainscript/ast"
	"domainscript/token"
)

// vobinary.go implementa o dispatch de operador binário de VO em forma
// isolada e reusável (§design 4.2, REQ-22.5) — a decisão de COMO combinar
// dois operandos JÁ lowerizados ("a OP b" sobre tipos possivelmente VO): via
// método de Operator, operador Go nativo, ou erro de geração. Não é ainda a
// lowering geral de BinaryExpr (isso é E5.1); aqui só o dispatch em si,
// testável isoladamente e reutilizado por quem fizer essa lowering.

// VOOperatorRegistry indexa, por nome de ValueObject, o conjunto de símbolos
// de Operator declarados (ex. "Money" -> {"+", "-", ">="}) — construído
// incrementalmente via Register conforme as declarações do programa são
// vistas. Usado por LowerVOBinaryDispatch para decidir se um "a OP b" sobre
// VOs vira chamada de método, comparação nativa, ou erro.
type VOOperatorRegistry struct {
	ops map[string]map[string]bool // nome do VO -> símbolo do operador -> declarado
}

// NewVOOperatorRegistry cria um VOOperatorRegistry vazio.
func NewVOOperatorRegistry() *VOOperatorRegistry {
	return &VOOperatorRegistry{ops: make(map[string]map[string]bool)}
}

// Register varre decl.Operators e indexa cada símbolo (OperatorDecl.Op) sob
// decl.Name. Chamar Register para um ValueObject sem Operators é um no-op
// seguro (HasOperator devolve false para qualquer símbolo depois).
func (r *VOOperatorRegistry) Register(decl *ast.ValueObjectDecl) {
	if len(decl.Operators) == 0 {
		return
	}
	set, ok := r.ops[decl.Name]
	if !ok {
		set = make(map[string]bool, len(decl.Operators))
		r.ops[decl.Name] = set
	}
	for _, op := range decl.Operators {
		set[op.Op] = true
	}
}

// HasOperator reporta se o ValueObject voName declara um Operator com o
// símbolo op (ex. "+", "==") — false se voName nunca foi Registered, ou foi
// Registered mas não declara esse símbolo.
func (r *VOOperatorRegistry) HasOperator(voName, op string) bool {
	set, ok := r.ops[voName]
	if !ok {
		return false
	}
	return set[op]
}

// LowerVOBinaryDispatch implementa o dispatch de §design 4.2: dado o texto Go
// já lowerizado dos dois operandos (leftGo, rightGo) e o nome do tipo
// DomainScript de cada um (leftType/rightType — "" ou um primitivo conhecido
// se não for VO), decide a forma final da expressão Go combinada:
//
//   - leftType é um VO (não vazio, não primitivo) e r.HasOperator(leftType, op)
//     → "leftGo.<Método>(rightGo)" (o método pode devolver (T, error); quem
//     gera a lowering geral ao redor decide como propagar isso — esta função
//     só devolve a EXPRESSÃO, não statements).
//   - leftType é "" (desconhecido, tratado como nativo por default) ou um
//     primitivo conhecido (via codegen.GoPrimitive) → "leftGo <opGo> rightGo"
//     (operador Go nativo).
//   - leftType é um VO sem o operador declarado, e o operador é "==" ou "!="
//     → "leftGo == rightGo" / "leftGo != rightGo" (VOs são comparáveis
//     nativamente em Go — wrapper e composto são structs/tipos nomeados
//     comparáveis).
//   - leftType é um VO sem o operador declarado, operador é
//     aritmético/relacional (qualquer outro símbolo) → erro de geração: não
//     há método a chamar (ver o caveat do Money/wallet, §design 7).
//
// Não tenta resolver COMO leftGo/rightGo foram lowerizados (ex. não sabe
// construir "ActiveStatus(true)") — isso é responsabilidade de uma lowering
// geral futura (E5.1). rightType não participa da decisão: o dispatch de
// operador de VO é sempre orientado pelo operando esquerdo (o front-end já
// garantiu compatibilidade de tipos entre os dois — REQ-13).
func LowerVOBinaryDispatch(reg *VOOperatorRegistry, op token.Kind, leftGo, leftType, rightGo, rightType string) (string, error) {
	opSymbol := op.String()

	if leftType != "" {
		if _, isPrimitive := GoPrimitive(leftType); !isPrimitive {
			return lowerVOOperatorDispatch(reg, op, opSymbol, leftGo, leftType, rightGo)
		}
	}

	// leftType é "" (desconhecido) ou um primitivo conhecido: operador Go
	// nativo direto.
	opGo, ok := nativeBinaryOps[op]
	if !ok {
		return "", fmt.Errorf("codegen: operador %q não suportado no dispatch de binário de VO", opSymbol)
	}
	return fmt.Sprintf("%s %s %s", leftGo, opGo, rightGo), nil
}

// lowerVOOperatorDispatch resolve os ramos (a)/(c)/(d) de LowerVOBinaryDispatch
// — chamado só quando leftType já foi determinado como um VO (não vazio, não
// primitivo).
func lowerVOOperatorDispatch(reg *VOOperatorRegistry, op token.Kind, opSymbol, leftGo, leftType, rightGo string) (string, error) {
	if reg.HasOperator(leftType, opSymbol) {
		method, ok := OperatorMethod(opSymbol)
		if !ok {
			return "", fmt.Errorf("codegen: ValueObject %s: Operator %q: símbolo de operador não reconhecido", leftType, opSymbol)
		}
		return fmt.Sprintf("%s.%s(%s)", leftGo, method, rightGo), nil
	}

	if opSymbol == "==" || opSymbol == "!=" {
		opGo, ok := nativeBinaryOps[op]
		if !ok {
			return "", fmt.Errorf("codegen: operador %q não suportado no dispatch de binário de VO", opSymbol)
		}
		return fmt.Sprintf("%s %s %s", leftGo, opGo, rightGo), nil
	}

	return "", fmt.Errorf("codegen: ValueObject %s não declara Operator %q — sem método a chamar (aritmético/relacional exige Operator declarado, §design 4.2/7)", leftType, opSymbol)
}
