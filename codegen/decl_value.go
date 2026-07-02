package codegen

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
)

// RuntimeImportPath é o caminho de import do pacote runtime vendorado
// (codegen/rtsrc) dentro do projeto Go gerado, usado por todo código que
// referencia runtime.* (runtime.Decimal, runtime.BusinessError, ...). Fixo
// nesta task — o wiring completo de pacote-por-módulo, com um caminho
// derivado de Options.ModulePath por projeto, é E9.1 (§design 3.4).
const RuntimeImportPath = "domainscript/generated/runtime"

// EmitValueObject gera o Go de um ValueObjectDecl (wrapper ou composto): o
// tipo, e NewX com a checagem de Valid (REQ-17.1/17.2, §design 3.3/3.5).
// Devolve o Go formatado (via emit.Emitter) ou erro se o corpo de Valid usa
// uma forma não suportada por lowerVOCondition (codegen/vobody.go).
func EmitValueObject(pkg string, decl *ast.ValueObjectDecl) ([]byte, error) {
	e := emit.New(pkg)
	if err := emitValueObjectDecl(e, decl); err != nil {
		return nil, err
	}
	return e.Bytes()
}

func emitValueObjectDecl(e *emit.Emitter, decl *ast.ValueObjectDecl) error {
	switch {
	case decl.Base != nil:
		return emitValueObjectWrapper(e, decl)
	case len(decl.Fields) > 0:
		return emitValueObjectComposite(e, decl)
	default:
		return fmt.Errorf("codegen: ValueObject %s não é wrapper (Base) nem composto (Fields)", decl.Name)
	}
}

// extractValidCondition devolve a condição booleana do bloco Valid de decl, o
// tradutor recursivo (lowerVOCondition, escopado a corpos de VO). Devolve
// (nil, nil) quando não há checagem a gerar: Valid ausente, ou Valid { ok }
// (o sentinela "validação sempre passa" — §design 3.3/3.6). Devolve erro se
// Valid não tem a forma garantida pelo front-end (bloco com um único
// ExprStmt).
func extractValidCondition(decl *ast.ValueObjectDecl) (ast.Expr, error) {
	if decl.Valid == nil {
		return nil, nil
	}
	if len(decl.Valid.Stmts) != 1 {
		return nil, fmt.Errorf("codegen: bloco Valid de %s deveria ter exatamente 1 statement, tem %d", decl.Name, len(decl.Valid.Stmts))
	}
	st, ok := decl.Valid.Stmts[0].(*ast.ExprStmt)
	if !ok {
		return nil, fmt.Errorf("codegen: bloco Valid de %s: statement não suportado (%T), esperava ExprStmt", decl.Name, decl.Valid.Stmts[0])
	}
	if id, ok := st.X.(*ast.Ident); ok && id.Name == "ok" {
		return nil, nil // sentinela "validação sempre passa" (NewX nunca falha)
	}
	return st.X, nil
}

// validationError devolve a expressão Go de construção do erro de validação
// de decl (§design catálogo de mapeamento, decisão desta task): não há um
// Error nomeado associado à falha de Valid (isso é só de `ensure ... else
// ErrorName`, em Operator — E3.2), então usa-se um runtime.BusinessError
// derivado do próprio nome do ValueObject.
func validationError(runtimeAlias, name string) string {
	return fmt.Sprintf("%s.BusinessError{Code: %q, Msg: %q}", runtimeAlias, "Invalid"+name, name+": valor inválido")
}

// emitValueObjectWrapper gera "type X Base" + "func NewX(value Base) (X, error)"
// para a forma wrapper (ValueObject Email(string)).
func emitValueObjectWrapper(e *emit.Emitter, decl *ast.ValueObjectDecl) error {
	goType, err := GoFieldType(decl.Base)
	if err != nil {
		return fmt.Errorf("codegen: ValueObject %s: %w", decl.Name, err)
	}

	cond, err := extractValidCondition(decl)
	if err != nil {
		return err
	}

	var condGo, runtimeAlias string
	if cond != nil {
		runtimeAlias = e.Import(RuntimeImportPath)
		scope := newVOScope(runtimeAlias)
		scope.bind("value", "value", decl.Base.Name)
		condGo, err = lowerVOCondition(scope, cond)
		if err != nil {
			return fmt.Errorf("codegen: ValueObject %s: %w", decl.Name, err)
		}
	}

	e.Line("// %s é um ValueObject que embrulha %s (§2.2). Imutável; construa", decl.Name, goType)
	e.Line("// sempre via New%s para preservar a Regra de Ouro.", decl.Name)
	e.Line("type %s %s", decl.Name, goType)
	e.Line("")
	e.Line("// New%s constrói um %s validado.", decl.Name, decl.Name)
	e.Block(fmt.Sprintf("func New%s(value %s) (%s, error)", decl.Name, goType, decl.Name), func() {
		if condGo != "" {
			e.Block("if !("+condGo+")", func() {
				e.Line("var zero %s", decl.Name)
				e.Line("return zero, %s", validationError(runtimeAlias, decl.Name))
			})
		}
		e.Line("return %s(value), nil", decl.Name)
	})
	if err := emitOperators(e, runtimeAlias, decl); err != nil {
		return err
	}
	return nil
}

// voFieldInfo é a forma Go já resolvida de um campo de VO composto: o tipo Go
// do campo, o nome do parâmetro do construtor (nu, escapado de keyword Go —
// codegen.Ident) e o nome exportado do campo do struct (codegen.ExportField).
type voFieldInfo struct {
	field      *ast.Field
	goType     string
	paramName  string
	exportName string
}

// emitValueObjectComposite gera "type X struct{...}" + "func NewX(...) (X, error)"
// para a forma composta (ValueObject Money{...}), sem setters (imutável).
func emitValueObjectComposite(e *emit.Emitter, decl *ast.ValueObjectDecl) error {
	infos := make([]voFieldInfo, 0, len(decl.Fields))
	for _, f := range decl.Fields {
		goType, err := GoFieldType(f.Type)
		if err != nil {
			return fmt.Errorf("codegen: ValueObject %s: campo %s: %w", decl.Name, f.Name, err)
		}
		infos = append(infos, voFieldInfo{
			field:      f,
			goType:     goType,
			paramName:  Ident(f.Name),
			exportName: ExportField(f.Name),
		})
	}

	cond, err := extractValidCondition(decl)
	if err != nil {
		return err
	}

	needsRuntime := cond != nil
	for _, fi := range infos {
		if strings.HasPrefix(fi.goType, "runtime.") {
			needsRuntime = true
		}
	}

	var runtimeAlias string
	if needsRuntime {
		runtimeAlias = e.Import(RuntimeImportPath)
	}

	var condGo string
	if cond != nil {
		scope := newVOScope(runtimeAlias)
		for _, fi := range infos {
			scope.bind(fi.field.Name, fi.paramName, fi.field.Type.Name)
		}
		condGo, err = lowerVOCondition(scope, cond)
		if err != nil {
			return fmt.Errorf("codegen: ValueObject %s: %w", decl.Name, err)
		}
	}

	e.Line("// %s é um ValueObject composto e imutável (§2.2): sem setters,", decl.Name)
	e.Line("// construa sempre via New%s para preservar a Regra de Ouro.", decl.Name)
	e.Block(fmt.Sprintf("type %s struct", decl.Name), func() {
		for _, fi := range infos {
			e.Line("%s %s %s", fi.exportName, fi.goType, JSONTag(fi.field.Name))
		}
	})
	e.Line("")

	params := make([]string, len(infos))
	for i, fi := range infos {
		params[i] = fmt.Sprintf("%s %s", fi.paramName, fi.goType)
	}

	e.Line("// New%s constrói um %s validado.", decl.Name, decl.Name)
	e.Block(fmt.Sprintf("func New%s(%s) (%s, error)", decl.Name, strings.Join(params, ", "), decl.Name), func() {
		if condGo != "" {
			e.Block("if !("+condGo+")", func() {
				e.Line("var zero %s", decl.Name)
				e.Line("return zero, %s", validationError(runtimeAlias, decl.Name))
			})
		}
		assigns := make([]string, len(infos))
		for i, fi := range infos {
			assigns[i] = fmt.Sprintf("%s: %s", fi.exportName, fi.paramName)
		}
		e.Line("return %s{%s}, nil", decl.Name, strings.Join(assigns, ", "))
	})
	if err := emitOperators(e, runtimeAlias, decl); err != nil {
		return err
	}
	return nil
}
