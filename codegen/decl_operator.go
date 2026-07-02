package codegen

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
)

// decl_operator.go emite o método Go de cada Operator de ValueObject
// (§design 3.5/3.6, REQ-17.3, REQ-22.1/5/9) — E3.2. Reusa o lowering de
// expressão de vobody.go (lowerVOCondition/lowerDecimalOperand); aqui só
// entra o que é específico de Operator: a assinatura do método, o escopo por
// receptor (campos via "<recv>.<Campo>", não parâmetro cru) e o lowering das
// duas formas de statement que aparecem no corpo (ensure/return).

// emitOperators gera, em sequência, o método Go de cada decl.Operators —
// chamado logo após o construtor NewX (E3.1) tanto no wrapper quanto no
// composto. runtimeAlias é o alias já resolvido do import do runtime
// vendorado (pode ser "" se nada no VO precisou dele até aqui); os
// Operators reusam o mesmo alias quando também precisam (ex. decimal).
func emitOperators(e *emit.Emitter, runtimeAlias string, decl *ast.ValueObjectDecl) error {
	for _, op := range decl.Operators {
		e.Line("")
		if err := emitOperator(e, runtimeAlias, decl, op); err != nil {
			return err
		}
	}
	return nil
}

// emitOperator gera "func (<recv> <VOName>) <Método>(<params...>) (<Return>, error) { ... }"
// para um único Operator (§design catálogo 3.5): a assinatura vem de
// goname.OperatorMethod (nome do método) e goname.GoFieldType (tipo de
// retorno); o corpo é uma sequência de ensure/return lowerizada por
// lowerOperatorStmt.
func emitOperator(e *emit.Emitter, runtimeAlias string, vo *ast.ValueObjectDecl, decl *ast.OperatorDecl) error {
	method, ok := goname.OperatorMethod(decl.Op)
	if !ok {
		return fmt.Errorf("codegen: ValueObject %s: Operator %q: símbolo de operador não reconhecido", vo.Name, decl.Op)
	}
	goReturn, err := goname.GoFieldType(decl.Return)
	if err != nil {
		return fmt.Errorf("codegen: ValueObject %s: Operator %s: tipo de retorno: %w", vo.Name, decl.Op, err)
	}

	recv := operatorReceiverName(vo, decl.Params)

	scope := newVOScope(runtimeAlias)
	if err := bindOperatorReceiver(scope, vo, recv); err != nil {
		return fmt.Errorf("codegen: ValueObject %s: Operator %s: %w", vo.Name, decl.Op, err)
	}

	params := make([]string, len(decl.Params))
	for i, p := range decl.Params {
		paramType, err := goname.GoFieldType(p.Type)
		if err != nil {
			return fmt.Errorf("codegen: ValueObject %s: Operator %s: parâmetro %s: %w", vo.Name, decl.Op, p.Name, err)
		}
		paramName := goname.Ident(p.Name)
		scope.bind(p.Name, paramName, p.Type.Name)
		params[i] = fmt.Sprintf("%s %s", paramName, paramType)
	}

	e.Line("// %s é o Operator %s de %s (§2.2), gerado a partir do corpo declarado.", method, decl.Op, vo.Name)
	sig := fmt.Sprintf("func (%s %s) %s(%s) (%s, error)", recv, vo.Name, method, strings.Join(params, ", "), goReturn)

	var bodyErr error
	e.Block(sig, func() {
		for _, st := range decl.Body.Stmts {
			if bodyErr != nil {
				return
			}
			bodyErr = lowerOperatorStmt(e, scope, vo, decl.Return, goReturn, st)
		}
	})
	if bodyErr != nil {
		return fmt.Errorf("codegen: ValueObject %s: Operator %s: %w", vo.Name, decl.Op, bodyErr)
	}
	return nil
}

// operatorReceiverName escolhe um nome de receptor determinístico e
// idiomático: a 1ª letra minúscula do nome do VO (Money -> m). O
// front-end não dá nome ao receptor de um Operator composto (não é
// `self`); se a letra colidir com o nome Go de algum parâmetro declarado,
// cai para "recv".
func operatorReceiverName(vo *ast.ValueObjectDecl, params []*ast.Field) string {
	cand := strings.ToLower(vo.Name[:1])
	for _, p := range params {
		if goname.Ident(p.Name) == cand {
			return "recv"
		}
	}
	return cand
}

// bindOperatorReceiver preenche scope com os nomes que um corpo de Operator
// enxerga (§design 3.3, "Receptor de VO"): num composto, cada campo por nome
// nu vira "<recv>.<CampoExportado>" (acesso via receptor — diferente de
// NewX/E3.1, onde o nome nu é o parâmetro cru); num wrapper, "value" vira o
// próprio receptor (o valor embrulhado É o receptor, já que "type X Base"
// torna X e Base convertíveis).
func bindOperatorReceiver(scope voScope, vo *ast.ValueObjectDecl, recv string) error {
	switch {
	case vo.Base != nil:
		scope.bind("value", recv, vo.Base.Name)
	case len(vo.Fields) > 0:
		for _, f := range vo.Fields {
			scope.bind(f.Name, recv+"."+goname.ExportField(f.Name), f.Type.Name)
		}
	default:
		return fmt.Errorf("ValueObject %s não é wrapper (Base) nem composto (Fields)", vo.Name)
	}
	return nil
}

// lowerOperatorStmt traduz um statement do corpo de Operator: só as duas
// formas garantidas pelo front-end nesse contexto (EnsureStmt e ReturnStmt —
// Operator não tem `for`, então nenhuma outra forma é válida). Qualquer
// outra forma é erro de geração, não uma tentativa de adivinhar.
func lowerOperatorStmt(e *emit.Emitter, scope voScope, vo *ast.ValueObjectDecl, retType *ast.TypeRef, goReturn string, st ast.Stmt) error {
	switch s := st.(type) {
	case *ast.EnsureStmt:
		return lowerOperatorEnsure(e, scope, goReturn, s)
	case *ast.ReturnStmt:
		return lowerOperatorReturn(e, scope, vo, retType, s)
	default:
		return fmt.Errorf("statement %T não suportado em corpo de Operator (só ensure/return)", st)
	}
}

// lowerOperatorEnsure traduz "ensure Cond else ErrorName" para
// "if !(Cond) { var zero T; return zero, ErrErrorName }". Else é sempre um
// *ast.ExprStmt{X: *ast.Ident} neste contexto — Operator não tem `for`, então
// Nop/break/continue (só válidos dentro de laço) não são formas válidas
// aqui; se aparecerem, é erro de geração.
func lowerOperatorEnsure(e *emit.Emitter, scope voScope, goReturn string, s *ast.EnsureStmt) error {
	condGo, err := lowerVOCondition(scope, s.Cond)
	if err != nil {
		return err
	}
	errName, err := operatorEnsureErrorName(s.Else)
	if err != nil {
		return err
	}
	var blockErr error
	e.Block("if !("+condGo+")", func() {
		e.Line("var zero %s", goReturn)
		e.Line("return zero, Err%s", errName)
	})
	return blockErr
}

// operatorEnsureErrorName extrai o nome do Error de "ensure … else Nome" —
// a única forma de Else válida em corpo de Operator (não é `for`, então
// Nop/break/continue não se aplicam).
func operatorEnsureErrorName(els ast.Stmt) (string, error) {
	exprStmt, ok := els.(*ast.ExprStmt)
	if !ok {
		return "", fmt.Errorf("ensure … else em corpo de Operator só suporta um Error, got %T", els)
	}
	id, ok := exprStmt.X.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("ensure … else em corpo de Operator só suporta um Error, got %T", exprStmt.X)
	}
	return id.Name, nil
}

// lowerOperatorReturn traduz "return Value" conforme o TypeRef de retorno do
// Operator (§design catálogo, decisão desta task):
//   - Return primitivo (goname.GoPrimitive, ex. "boolean") → "return
//     <Value lowerizado>, nil".
//   - Return é o próprio VO sendo definido (auto-construção, ex. Money dentro
//     de Operator de Money) e Value é "VOName(campo: expr, …)" → traduz para
//     "result, err := NewVOName(...); if err != nil {...}; return result, nil".
//   - Qualquer outra combinação → erro de geração (construir um VO diferente
//     dentro de um Operator fica fora de escopo desta task — E5.1 futuro).
func lowerOperatorReturn(e *emit.Emitter, scope voScope, vo *ast.ValueObjectDecl, retType *ast.TypeRef, s *ast.ReturnStmt) error {
	if s.Value == nil {
		return fmt.Errorf("return sem valor não suportado em corpo de Operator")
	}

	if _, ok := goname.GoPrimitive(retType.Name); ok {
		valGo, err := lowerVOCondition(scope, s.Value)
		if err != nil {
			return err
		}
		e.Line("return %s, nil", valGo)
		return nil
	}

	if retType.Name == vo.Name {
		call, ok := s.Value.(*ast.CallExpr)
		if !ok {
			return fmt.Errorf("return de %s em Operator: esperava construção %s(...), got %T", vo.Name, vo.Name, s.Value)
		}
		fnIdent, ok := call.Fn.(*ast.Ident)
		if !ok || fnIdent.Name != vo.Name {
			return fmt.Errorf("return de %s em Operator: só suporta auto-construção %s(...) nesta task (E3.2) — construir um VO diferente é trabalho futuro (E5.1)", vo.Name, vo.Name)
		}
		return lowerOperatorSelfConstruct(e, scope, vo, call)
	}

	return fmt.Errorf("Return %s não suportado em corpo de Operator de %s (só primitivo ou auto-construção)", retType.Name, vo.Name)
}

// lowerOperatorSelfConstruct traduz "return VOName(campo: expr, …)" para
// "result, err := NewVOName(args-na-ordem-dos-campos...); if err != nil {
// var zero VOName; return zero, err }; return result, nil". Os argumentos são
// casados por Arg.Name contra os campos declarados do VO composto, na ORDEM
// DECLARADA dos campos (não a ordem em que os args aparecem no CallExpr).
func lowerOperatorSelfConstruct(e *emit.Emitter, scope voScope, vo *ast.ValueObjectDecl, call *ast.CallExpr) error {
	byName := make(map[string]ast.Expr, len(call.Args))
	for _, a := range call.Args {
		if a.Name == "" {
			return fmt.Errorf("construção de %s em Operator precisa de argumentos nomeados, achei um posicional", vo.Name)
		}
		byName[a.Name] = a.Value
	}

	args := make([]string, 0, len(vo.Fields))
	for _, f := range vo.Fields {
		val, ok := byName[f.Name]
		if !ok {
			return fmt.Errorf("construção de %s em Operator não informa o campo %q", vo.Name, f.Name)
		}
		goVal, err := lowerVOCondition(scope, val)
		if err != nil {
			return err
		}
		args = append(args, goVal)
	}

	e.Line("result, err := New%s(%s)", vo.Name, strings.Join(args, ", "))
	e.Block("if err != nil", func() {
		e.Line("var zero %s", vo.Name)
		e.Line("return zero, err")
	})
	e.Line("return result, nil")
	return nil
}
