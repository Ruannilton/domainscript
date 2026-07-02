package codegen

import (
	"fmt"
	"strconv"

	"domainscript/ast"
	"domainscript/token"
)

// vobody.go traduz o pequeno subconjunto de expressões que aparece em corpos
// de ValueObject (Valid — E3.1 — e Operator — E3.2) para Go (§design 3.6,
// REQ-22.5/22.6, escopado — não é a lowering geral REQ-22, que é E5+).

// voName é o que um nome nu resolve dentro de um corpo de VO: a expressão Go
// correspondente (parâmetro do construtor, já escapado por Ident) e o nome do
// tipo DomainScript declarado (ex. "decimal", "string", "AppendList") — usado
// só para o dispatch decimal-vs-nativo do BinaryExpr (§design 4.2) e para
// achar o "shape" do receptor de um método embutido.
type voName struct {
	goExpr string
	dsType string
}

// voScope descreve o que cada nome nu resolve dentro de um corpo de VO:
// value (wrapper) ou os campos por nome nu (composto), mais o alias do
// import do runtime vendorado — necessário sempre que a checagem envolve
// decimal (runtime.NewDecimalFromInt) ou o erro de validação
// (runtime.BusinessError), registrado via emit.Emitter.Import antes de
// lowerVOCondition rodar.
type voScope struct {
	names        map[string]voName
	runtimeAlias string
}

// newVOScope cria um voScope vazio. runtimeAlias é o alias já resolvido do
// import do pacote runtime (via emit.Emitter.Import), usado pelas formas que
// precisam construir um runtime.Decimal a partir de um literal.
func newVOScope(runtimeAlias string) voScope {
	return voScope{names: make(map[string]voName), runtimeAlias: runtimeAlias}
}

// bind registra que o nome nu name resolve para a expressão Go goExpr, cujo
// tipo DomainScript declarado é dsType.
func (s voScope) bind(name, goExpr, dsType string) {
	s.names[name] = voName{goExpr: goExpr, dsType: dsType}
}

// nativeBinaryOps mapeia o token.Kind de um operador binário para o operador
// Go nativo correspondente — usado tanto no ramo "primitivos" do dispatch
// quanto (o subconjunto comparável) no ramo decimal, onde compara o int
// devolvido por Cmp contra 0.
var nativeBinaryOps = map[token.Kind]string{
	token.EQ:    "==",
	token.NEQ:   "!=",
	token.LT:    "<",
	token.GT:    ">",
	token.LE:    "<=",
	token.GE:    ">=",
	token.PLUS:  "+",
	token.MINUS: "-",
	token.STAR:  "*",
	token.SLASH: "/",
	token.AND:   "&&",
	token.OR:    "||",
}

// decimalCompareOps é o subconjunto de nativeBinaryOps válido sobre
// runtime.Decimal: só comparação (via Cmp), nunca aritmética nativa (§design
// 4.2 — "left.Cmp(right) <op0> 0").
var decimalCompareOps = map[token.Kind]bool{
	token.EQ:  true,
	token.NEQ: true,
	token.LT:  true,
	token.GT:  true,
	token.LE:  true,
	token.GE:  true,
}

// decimalArithOps mapeia o token.Kind de um operador aritmético para o
// método de runtime.Decimal correspondente (§design 4.2, ramo decimal
// aritmético) — necessário a partir de E3.2 para corpos de Operator: "amount
// + other.amount" vira "m.Amount.Add(other.Amount)". runtime.Decimal só
// expõe Add/Sub (codegen/rtsrc/decimal.go.txt) — Mul/Div não são suportados.
var decimalArithOps = map[token.Kind]string{
	token.PLUS:  "Add",
	token.MINUS: "Sub",
}

// lowerVOCondition traduz recursivamente uma expressão de corpo de VO (hoje,
// só o que aparece em Valid) para a string Go correspondente. O chamador é
// responsável por reconhecer o sentinela "ok" como condição inteira ANTES de
// chamar esta função (§design 3.3/3.6: "ok" só tem esse significado quando é
// a condição toda; nesta função ele é só mais um nome não vinculado — erro,
// se aparecer).
func lowerVOCondition(scope voScope, e ast.Expr) (string, error) {
	switch ex := e.(type) {
	case *ast.Ident:
		n, ok := scope.names[ex.Name]
		if !ok {
			return "", fmt.Errorf("codegen: identificador desconhecido em corpo de VO: %q", ex.Name)
		}
		return n.goExpr, nil

	case *ast.Literal:
		return lowerVOLiteral(ex)

	case *ast.CallExpr:
		return lowerVOCall(scope, ex)

	case *ast.BinaryExpr:
		return lowerVOBinary(scope, ex)

	case *ast.MemberExpr:
		return lowerVOFieldAccess(scope, ex)

	default:
		return "", fmt.Errorf("codegen: forma de expressão não suportada em corpo de VO: %T", e)
	}
}

// lowerVOFieldAccess traduz um acesso a campo puro X.name (ex.: "other.currency"
// dentro de um Operator, onde "other" é um parâmetro cujo tipo é o próprio VO
// composto) — distinto de lowerVOCall, que trata MemberExpr só quando é o Fn
// de uma chamada de método embutido. Regra (E3.2): se X lowerizar para uma
// expressão Go via lowerVOCondition, o acesso vira "<X-lowerizado>.<campo
// exportado>" — os campos do composto já são exportados (E3.1). Não valida
// se o campo existe de fato: o programa já foi validado pelo front-end
// (pré-condição REQ-14.1), o gerador confia nisso.
func lowerVOFieldAccess(scope voScope, e *ast.MemberExpr) (string, error) {
	xGo, err := lowerVOCondition(scope, e.X)
	if err != nil {
		return "", err
	}
	return xGo + "." + ExportField(e.Name), nil
}

// lowerVOLiteral traduz um *ast.Literal para o literal Go correspondente.
func lowerVOLiteral(l *ast.Literal) (string, error) {
	switch l.Kind {
	case token.INT, token.FLOAT:
		return l.Value, nil
	case token.STRING:
		return strconv.Quote(l.Value), nil
	case token.TRUE:
		return "true", nil
	case token.FALSE:
		return "false", nil
	default:
		return "", fmt.Errorf("codegen: literal %s não suportado em corpo de VO", l.Kind)
	}
}

// lowerVOCall traduz uma chamada X.method(args...) para o par (tipo-receptor,
// método) da tabela de built-ins de codegen.GoBuiltinCall. Só reconhece essa
// forma (Fn é um *ast.MemberExpr, sem argumentos nomeados) — qualquer outra
// forma de CallExpr é erro de geração nesta task.
func lowerVOCall(scope voScope, e *ast.CallExpr) (string, error) {
	mem, ok := e.Fn.(*ast.MemberExpr)
	if !ok {
		return "", fmt.Errorf("codegen: chamada não suportada em corpo de VO: Fn é %T, esperava acesso a membro", e.Fn)
	}

	recvGo, err := lowerVOCondition(scope, mem.X)
	if err != nil {
		return "", err
	}
	recvShape, err := voReceiverShape(scope, mem.X)
	if err != nil {
		return "", err
	}

	args := make([]string, 0, len(e.Args))
	for _, a := range e.Args {
		if a.Name != "" {
			return "", fmt.Errorf("codegen: argumento nomeado %q não suportado em chamada de corpo de VO", a.Name)
		}
		av, err := lowerVOCondition(scope, a.Value)
		if err != nil {
			return "", err
		}
		args = append(args, av)
	}

	bm := BuiltinMethod{Receiver: recvShape, Method: mem.Name}
	goExpr, ok := GoBuiltinCall(recvGo, bm, args)
	if !ok {
		return "", fmt.Errorf("codegen: método embutido desconhecido em corpo de VO: %s.%s", recvShape, mem.Name)
	}
	return goExpr, nil
}

// voReceiverShape devolve o "shape" de tipo (§design 3.6, mesmo sentido de
// BuiltinMethod.Receiver) do receptor de uma chamada de método embutido.
// Só reconhece um *ast.Ident vinculado no escopo — o único caso que aparece
// em Valid de VO (ex. "value" num wrapper string).
func voReceiverShape(scope voScope, e ast.Expr) (string, error) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("codegen: não consigo determinar o tipo do receptor de método em corpo de VO: %T", e)
	}
	n, ok := scope.names[id.Name]
	if !ok {
		return "", fmt.Errorf("codegen: identificador desconhecido em corpo de VO: %q", id.Name)
	}
	return n.dsType, nil
}

// operandDSType devolve o tipo DomainScript declarado de e, quando e é um
// *ast.Ident vinculado no escopo (o único jeito de um operando "ser decimal"
// dentro de Valid — literais isolados não carregam tipo próprio).
func operandDSType(scope voScope, e ast.Expr) (string, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return "", false
	}
	n, ok := scope.names[id.Name]
	if !ok {
		return "", false
	}
	return n.dsType, true
}

// lowerVOBinary implementa o dispatch de §design 4.2 restrito ao que aparece
// em corpos de VO (Valid e, desde E3.2, Operator): se um dos operandos é
// decimal, vira comparação via Cmp ou aritmética via Add/Sub (Money.Add soma
// "amount" e "other.amount"); senão, operador Go nativo direto.
func lowerVOBinary(scope voScope, e *ast.BinaryExpr) (string, error) {
	leftType, leftKnown := operandDSType(scope, e.Left)
	rightType, rightKnown := operandDSType(scope, e.Right)
	isDecimal := (leftKnown && leftType == "decimal") || (rightKnown && rightType == "decimal")

	if isDecimal {
		if decimalCompareOps[e.Op] {
			return lowerVODecimalCompare(scope, e)
		}
		if method, ok := decimalArithOps[e.Op]; ok {
			return lowerVODecimalArith(scope, e, method)
		}
		return "", fmt.Errorf("codegen: operador %s não suportado sobre decimal em corpo de VO (só comparação via Cmp ou +/- via Add/Sub)", e.Op)
	}

	left, err := lowerVOCondition(scope, e.Left)
	if err != nil {
		return "", err
	}
	right, err := lowerVOCondition(scope, e.Right)
	if err != nil {
		return "", err
	}
	opGo, ok := nativeBinaryOps[e.Op]
	if !ok {
		return "", fmt.Errorf("codegen: operador %s não suportado em corpo de VO", e.Op)
	}
	return fmt.Sprintf("%s %s %s", left, opGo, right), nil
}

// lowerVODecimalCompare traduz uma comparação onde ao menos um operando é
// decimal para "left.Cmp(right) <op0> 0" (§design 4.2), convertendo o outro
// lado para runtime.Decimal quando necessário.
func lowerVODecimalCompare(scope voScope, e *ast.BinaryExpr) (string, error) {
	if !decimalCompareOps[e.Op] {
		return "", fmt.Errorf("codegen: operador %s não suportado sobre decimal em corpo de VO (só comparação via Cmp)", e.Op)
	}

	left, err := lowerDecimalOperand(scope, e.Left)
	if err != nil {
		return "", err
	}
	right, err := lowerDecimalOperand(scope, e.Right)
	if err != nil {
		return "", err
	}
	opGo := nativeBinaryOps[e.Op]
	return fmt.Sprintf("%s.Cmp(%s) %s 0", left, right, opGo), nil
}

// lowerVODecimalArith traduz uma operação aritmética (+/-) onde ao menos um
// operando é decimal para "left.<Method>(right)" (§design 4.2, ex.:
// Money.Add faz "m.Amount.Add(other.Amount)").
func lowerVODecimalArith(scope voScope, e *ast.BinaryExpr, method string) (string, error) {
	left, err := lowerDecimalOperand(scope, e.Left)
	if err != nil {
		return "", err
	}
	right, err := lowerDecimalOperand(scope, e.Right)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s.%s(%s)", left, method, right), nil
}

// lowerDecimalOperand traduz um operando de uma expressão decimal (comparação
// ou aritmética) para uma expressão Go do tipo runtime.Decimal: um Ident
// vinculado já é Decimal (devolvido como está); um literal INT vira
// runtime.NewDecimalFromInt(N); qualquer outra forma (ex. "other.amount" — um
// acesso a campo puro sobre um parâmetro VO, E3.2) delega para
// lowerVOCondition — se o programa é válido (pré-condição REQ-14.1), o
// resultado já é uma expressão Go do tipo runtime.Decimal.
func lowerDecimalOperand(scope voScope, e ast.Expr) (string, error) {
	switch ex := e.(type) {
	case *ast.Ident:
		n, ok := scope.names[ex.Name]
		if !ok {
			return "", fmt.Errorf("codegen: identificador desconhecido em corpo de VO: %q", ex.Name)
		}
		return n.goExpr, nil
	case *ast.Literal:
		if ex.Kind == token.INT {
			return fmt.Sprintf("%s.NewDecimalFromInt(%s)", scope.runtimeAlias, ex.Value), nil
		}
		return lowerVOCondition(scope, e)
	default:
		return lowerVOCondition(scope, e)
	}
}
