package codegen

import (
	"fmt"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/token"
)

// decl_error.go emite o Go de ErrorTypeDecl (E4.1, REQ-18.1, §design 3.5):
// var Err<Nome> = runtime.BusinessError{Code: "<Nome>", Msg: "<mensagem>"}
// — comparável e distinguível via errors.Is (BusinessError.Is compara só o
// Code, codegen/rtsrc/errors.go.txt). Code é literalmente decl.Name: já é o
// identificador estável que E3.2 (Operator de Money) e E3.3 (coerce de Enum)
// referenciam por convenção como Err<Nome> — esta task torna esses nomes
// reais. Msg vem do literal STRING de decl.Message (lowerVOLiteral,
// codegen/vobody.go).

// EmitError gera o Go de um único ErrorTypeDecl: var Err<Nome> =
// runtime.BusinessError{Code: "<Nome>", Msg: <mensagem>}.
func EmitError(pkg string, decl *ast.ErrorTypeDecl) ([]byte, error) {
	return EmitErrors(pkg, []*ast.ErrorTypeDecl{decl})
}

// EmitErrors gera o Go de vários ErrorTypeDecl num único arquivo. Um módulo
// real declara vários Error (o wallet declara 4) e o layout final do projeto
// os concatena num só errors.go (E9.1); emitir em lote aqui evita import
// duplicado do runtime e já produz o formato natural de "vários Error num
// arquivo" sem custo adicional.
func EmitErrors(pkg string, decls []*ast.ErrorTypeDecl) ([]byte, error) {
	e := emit.New(pkg)
	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		if err := emitErrorDecl(e, decl); err != nil {
			return nil, err
		}
	}
	return e.Bytes()
}

// emitErrorDecl emite a declaração Go de um único ErrorTypeDecl. decl.Message
// é sempre um *ast.Literal STRING garantido pelo parser
// (parser/parse_decl.go:parseErrorType); qualquer outra forma é erro de
// geração claro — nunca panic de type assertion (a AST pode, em teoria,
// chegar aqui construída à mão por um teste/fixture sintética).
func emitErrorDecl(e *emit.Emitter, decl *ast.ErrorTypeDecl) error {
	lit, ok := decl.Message.(*ast.Literal)
	if !ok || lit.Kind != token.STRING {
		return fmt.Errorf("codegen: Error %s: message deveria ser um literal string, tem %T", decl.Name, decl.Message)
	}
	msgGo, err := lowerVOLiteral(lit)
	if err != nil {
		return fmt.Errorf("codegen: Error %s: %w", decl.Name, err)
	}

	runtimeAlias := e.Import(RuntimeImportPath)

	e.Line("// Err%s é o Error %s (§4.1): %s.", decl.Name, decl.Name, msgGo)
	e.Line("var Err%s = %s.BusinessError{Code: %q, Msg: %s}", decl.Name, runtimeAlias, decl.Name, msgGo)
	return nil
}
