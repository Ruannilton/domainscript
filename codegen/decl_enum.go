package codegen

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/codegen/emit"
	"domainscript/token"
)

// decl_enum.go emite o Go de um EnumDecl (E3.3, REQ-17.4, §design 3.5): o
// tipo nomeado sobre o Base, uma const por membro, e ParseX — coerção
// implícita (valor desconhecido → erro) ou, quando decl.Coerce != nil, o
// corpo do Parse lowerizado a partir do match restrito que um `coerce`
// sempre tem (não é a lowering geral de match — isso é E5.2 — mas um
// tradutor pequeno, escopado só à forma que `coerce` de Enum garante: um
// único MatchStmt, sujeito `value` ou `value.<método embutido>()`, braços
// com padrões STRING mapeando para um membro do próprio Enum, e um braço
// final wildcard `_` mapeando para um Error).

// coerceSubjectImports mapeia o nome de um método embutido usado como
// sujeito do coerce (ex. "uppercase") para o import stdlib que sua emissão
// Go exige (§design 3.6: strings.ToUpper). Só registrado quando o sujeito
// de fato usa o método — nunca um import não utilizado (emit.Bytes rejeita).
var coerceSubjectImports = map[string]string{
	"uppercase": "strings",
}

// EmitEnum gera o Go de um EnumDecl: o tipo nomeado sobre o Base, uma const
// por membro, e ParseX (coerção implícita ou, se decl.Coerce != nil, o corpo
// do coerce lowerizado a partir do match).
func EmitEnum(pkg string, decl *ast.EnumDecl) ([]byte, error) {
	e := emit.New(pkg)
	if err := emitEnumDecl(e, decl); err != nil {
		return nil, err
	}
	return e.Bytes()
}

// enumConst é a forma Go já resolvida de um EnumMember: o nome da const
// (codegen.EnumConstName) e o literal Go do seu valor (lowerVOLiteral).
type enumConst struct {
	name    string
	valueGo string
}

func emitEnumDecl(e *emit.Emitter, decl *ast.EnumDecl) error {
	if decl.Base == nil {
		return fmt.Errorf("codegen: Enum %s sem tipo base", decl.Name)
	}
	goBase, ok := GoPrimitive(decl.Base.Name)
	if !ok {
		return fmt.Errorf("codegen: Enum %s: tipo base %q não mapeável para Go (só primitivos, §design 3.3)", decl.Name, decl.Base.Name)
	}

	consts := make([]enumConst, 0, len(decl.Members))
	for _, mem := range decl.Members {
		lit, ok := mem.Value.(*ast.Literal)
		if !ok {
			return fmt.Errorf("codegen: Enum %s: membro %s: valor não é um literal (%T)", decl.Name, mem.Name, mem.Value)
		}
		valueGo, err := lowerVOLiteral(lit)
		if err != nil {
			return fmt.Errorf("codegen: Enum %s: membro %s: %w", decl.Name, mem.Name, err)
		}
		consts = append(consts, enumConst{name: EnumConstName(decl.Name, mem.Name), valueGo: valueGo})
	}

	e.Line("// %s é o Enum %s (§2.3): conjunto fechado de valores nomeados.", decl.Name, decl.Name)
	e.Line("// Construa via Parse%s para validar a coerção.", decl.Name)
	e.Line("type %s %s", decl.Name, goBase)
	e.Line("")

	e.Line("const (")
	for _, c := range consts {
		e.Line("%s %s = %s", c.name, decl.Name, c.valueGo)
	}
	e.Line(")")
	e.Line("")

	if decl.Coerce == nil {
		runtimeAlias := e.Import(RuntimeImportPath)
		emitEnumParseImplicit(e, decl, goBase, consts, runtimeAlias)
		return nil
	}
	return emitEnumParseCoerce(e, decl, goBase)
}

// emitEnumParseImplicit gera ParseX para um Enum sem coerce: switch v sobre
// o valor de cada membro; valor desconhecido → runtime.BusinessError
// (REQ-17.4, convenção "Invalid<Nome>" de codegen/decl_value.go).
func emitEnumParseImplicit(e *emit.Emitter, decl *ast.EnumDecl, goBase string, consts []enumConst, runtimeAlias string) {
	e.Line("// Parse%s coage v para um %s válido (coerção implícita — valor", decl.Name, decl.Name)
	e.Line("// desconhecido é erro, §2.3).")
	e.Block(fmt.Sprintf("func Parse%s(v %s) (%s, error)", decl.Name, goBase, decl.Name), func() {
		e.Block("switch v", func() {
			for _, c := range consts {
				e.Line("case %s:", c.valueGo)
				e.Line("return %s, nil", c.name)
			}
			e.Line("default:")
			e.Line("var zero %s", decl.Name)
			e.Line("return zero, %s", enumCoercionError(runtimeAlias, decl.Name))
		})
	})
}

// enumCoercionError devolve a expressão Go do runtime.BusinessError da
// coerção implícita falhada: Code "Invalid<Nome>", Msg "<Nome>: valor
// desconhecido" (§design catálogo 3.5).
func enumCoercionError(runtimeAlias, name string) string {
	return fmt.Sprintf("%s.BusinessError{Code: %q, Msg: %q}", runtimeAlias, "Invalid"+name, name+": valor desconhecido")
}

// emitEnumParseCoerce gera ParseX para um Enum com coerce: o switch Go
// reflete o único MatchStmt garantido pelo front-end no corpo de coerce
// (resolver/receivers.go, constructCoerce: {"value"}).
func emitEnumParseCoerce(e *emit.Emitter, decl *ast.EnumDecl, goBase string) error {
	match, err := coerceMatch(decl)
	if err != nil {
		return err
	}

	subjectGo, err := lowerCoerceSubject(e, decl, match.Subject)
	if err != nil {
		return fmt.Errorf("codegen: Enum %s: coerce: %w", decl.Name, err)
	}

	members := make(map[string]bool, len(decl.Members))
	for _, mem := range decl.Members {
		members[mem.Name] = true
	}

	e.Line("// Parse%s coage v para um %s válido, executando o coerce declarado", decl.Name, decl.Name)
	e.Line("// (§2.3).")
	var bodyErr error
	e.Block(fmt.Sprintf("func Parse%s(v %s) (%s, error)", decl.Name, goBase, decl.Name), func() {
		e.Block("switch "+subjectGo, func() {
			for _, arm := range match.Arms {
				if bodyErr != nil {
					return
				}
				bodyErr = emitCoerceArm(e, decl, members, arm)
			}
		})
	})
	if bodyErr != nil {
		return fmt.Errorf("codegen: Enum %s: coerce: %w", decl.Name, bodyErr)
	}
	return nil
}

// coerceMatch extrai e valida a forma restrita do MatchStmt de um coerce:
// exatamente 1 statement no corpo (o match em si), ao menos 1 braço, o
// braço wildcard `_` (se algum) é sempre o único padrão do último braço, e
// nenhum outro braço é wildcard. Qualquer forma fora disso é erro de
// geração claro — esta NÃO é a lowering geral de match (E5.2 futuro).
func coerceMatch(decl *ast.EnumDecl) (*ast.MatchStmt, error) {
	if decl.Coerce.Body == nil || len(decl.Coerce.Body.Stmts) != 1 {
		return nil, fmt.Errorf("codegen: Enum %s: coerce deveria ter exatamente 1 statement (um match)", decl.Name)
	}
	m, ok := decl.Coerce.Body.Stmts[0].(*ast.MatchStmt)
	if !ok {
		return nil, fmt.Errorf("codegen: Enum %s: coerce: statement não suportado (%T), esperava match", decl.Name, decl.Coerce.Body.Stmts[0])
	}
	if len(m.Arms) == 0 {
		return nil, fmt.Errorf("codegen: Enum %s: coerce: match sem braços", decl.Name)
	}
	for i, arm := range m.Arms {
		last := i == len(m.Arms)-1
		wc := isWildcardArm(arm)
		switch {
		case wc && !last:
			return nil, fmt.Errorf("codegen: Enum %s: coerce: braço wildcard '_' precisa ser o último braço", decl.Name)
		case wc && len(arm.Patterns) != 1:
			return nil, fmt.Errorf("codegen: Enum %s: coerce: braço wildcard '_' precisa ser o único padrão do braço", decl.Name)
		case !wc && last:
			return nil, fmt.Errorf("codegen: Enum %s: coerce: o último braço deveria ser o wildcard '_'", decl.Name)
		}
	}
	return m, nil
}

// isWildcardArm reporta se algum padrão do braço é o wildcard `_`.
func isWildcardArm(arm ast.MatchStmtArm) bool {
	for _, p := range arm.Patterns {
		if astutil.IsIdent(p, "_") {
			return true
		}
	}
	return false
}

// lowerCoerceSubject traduz o sujeito do match de um coerce: `value` em si
// (o parâmetro v de ParseX) ou `value.<método embutido>()` (via a tabela de
// codegen.GoBuiltinCall, ex. value.uppercase() → strings.ToUpper(v)).
// Qualquer outra forma é erro de geração — não é a lowering geral de
// expressão (E5.1 futuro).
func lowerCoerceSubject(e *emit.Emitter, decl *ast.EnumDecl, subject ast.Expr) (string, error) {
	if astutil.IsIdent(subject, "value") {
		return "v", nil
	}

	call, ok := subject.(*ast.CallExpr)
	if !ok {
		return "", fmt.Errorf("sujeito não suportado (%T); esperava 'value' ou 'value.<método>()'", subject)
	}
	if len(call.Args) != 0 {
		return "", fmt.Errorf("sujeito não suportado: chamada com argumentos")
	}
	mem, ok := call.Fn.(*ast.MemberExpr)
	if !ok || !astutil.IsIdent(mem.X, "value") {
		return "", fmt.Errorf("sujeito não suportado (%T); esperava value.<método>()", call.Fn)
	}

	bm := BuiltinMethod{Receiver: decl.Base.Name, Method: mem.Name}
	goExpr, ok := GoBuiltinCall("v", bm, nil)
	if !ok {
		return "", fmt.Errorf("método embutido desconhecido em sujeito de coerce: value.%s()", mem.Name)
	}
	if imp, ok := coerceSubjectImports[mem.Name]; ok {
		e.Import(imp)
	}
	return goExpr, nil
}

// emitCoerceArm emite um braço do switch já validado por coerceMatch: um
// braço de valor vira "case <literais>: return <Membro>, nil"; o braço
// wildcard vira "default: var zero <Enum>; return zero, Err<Nome>".
func emitCoerceArm(e *emit.Emitter, decl *ast.EnumDecl, members map[string]bool, arm ast.MatchStmtArm) error {
	if isWildcardArm(arm) {
		errName, err := coerceWildcardBody(arm.Body)
		if err != nil {
			return err
		}
		e.Line("default:")
		e.Line("var zero %s", decl.Name)
		e.Line("return zero, Err%s", errName)
		return nil
	}

	caseLits, err := coerceArmPatternLiterals(arm.Patterns)
	if err != nil {
		return err
	}
	memberName, err := coerceArmMemberBody(arm.Body, members)
	if err != nil {
		return err
	}
	e.Line("case %s:", strings.Join(caseLits, ", "))
	e.Line("return %s, nil", EnumConstName(decl.Name, memberName))
	return nil
}

// coerceArmPatternLiterals traduz os padrões de um braço de valor: sempre 1+
// literais STRING (a única forma garantida pelo front-end nesse contexto de
// coerce de Enum). Qualquer outra forma (não-literal, não-STRING) é erro.
func coerceArmPatternLiterals(patterns []ast.Expr) ([]string, error) {
	if len(patterns) == 0 {
		return nil, fmt.Errorf("braço sem padrões")
	}
	lits := make([]string, 0, len(patterns))
	for _, p := range patterns {
		lit, ok := p.(*ast.Literal)
		if !ok || lit.Kind != token.STRING {
			return nil, fmt.Errorf("padrão de braço não suportado (%T); esperava literal STRING", p)
		}
		goLit, err := lowerVOLiteral(lit)
		if err != nil {
			return nil, err
		}
		lits = append(lits, goLit)
	}
	return lits, nil
}

// coerceArmMemberBody extrai o nome do membro do próprio Enum referenciado
// pelo corpo de um braço de valor (*ast.ExprStmt{X: *ast.Ident}) — erro se a
// forma não bater, ou se o nome não for membro do Enum sendo definido.
func coerceArmMemberBody(body ast.Stmt, members map[string]bool) (string, error) {
	exprStmt, ok := body.(*ast.ExprStmt)
	if !ok {
		return "", fmt.Errorf("corpo de braço não suportado (%T); esperava referência a um membro do Enum", body)
	}
	id, ok := exprStmt.X.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("corpo de braço não suportado (%T); esperava um identificador", exprStmt.X)
	}
	if !members[id.Name] {
		return "", fmt.Errorf("corpo de braço referencia %q, que não é membro deste Enum", id.Name)
	}
	return id.Name, nil
}

// coerceWildcardBody extrai o nome do Error referenciado pelo corpo do
// braço wildcard (*ast.ExprStmt{X: *ast.Ident}) — o gerador só referencia
// o nome Go convencionado Err<Nome> (o Error em si é gerado por E4.1).
func coerceWildcardBody(body ast.Stmt) (string, error) {
	exprStmt, ok := body.(*ast.ExprStmt)
	if !ok {
		return "", fmt.Errorf("corpo de braço wildcard não suportado (%T); esperava referência a um Error", body)
	}
	id, ok := exprStmt.X.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("corpo de braço wildcard não suportado (%T); esperava um identificador (nome de Error)", exprStmt.X)
	}
	return id.Name, nil
}
