package diag

// codes.go é o catálogo de códigos estáveis de diagnóstico (REQ-6, §design
// type-checking 3.7). Um código identifica a *família* de um diagnóstico de forma
// estável para tooling (IDE, supressão seletiva), independente do texto da
// mensagem. A família E1xx cobre a resolução completa de nomes e tipos (REQ-9..13);
// as regras §23 do front-end ainda não carregam código (campo vazio), o que é
// permitido — um código vazio simplesmente não é renderizado.
const (
	// CodeNameInBody marca um nome não declarado usado num corpo executável (REQ-9).
	CodeNameInBody Code = "E100"
	// CodeConfigRef marca uma referência de configuração inexistente ou de Kind
	// divergente (REQ-10).
	CodeConfigRef Code = "E101"
	// CodeUnknownMember marca um acesso X.nome a um membro inexistente do tipo de X
	// (REQ-12).
	CodeUnknownMember Code = "E102"
	// CodeTypeMismatch marca um uso com tipos incompatíveis — atribuição, argumento,
	// operador ou return (REQ-13).
	CodeTypeMismatch Code = "E103"
)
