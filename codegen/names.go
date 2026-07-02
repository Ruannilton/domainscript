package codegen

import (
	"fmt"
	"strings"
	"unicode"
)

// goKeywords são as 25 palavras reservadas de Go (nunca identificadores válidos).
var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
}

// operatorMethods mapeia o símbolo de um Operator de VO (ast.OperatorDecl.Op)
// para o nome do método Go correspondente (§design 3.3).
var operatorMethods = map[string]string{
	"+":  "Add",
	"-":  "Sub",
	"*":  "Mul",
	"/":  "Div",
	">=": "Gte",
	"<=": "Lte",
	">":  "Gt",
	"<":  "Lt",
	"==": "Eq",
	"!=": "Neq",
}

// ExportField capitaliza a 1ª letra de name (campo de VO/struct → nome
// exportado Go). Ex.: "amount" → "Amount".
func ExportField(name string) string {
	if name == "" {
		return name
	}
	r := []rune(name)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// JSONTag devolve a tag Go completa (com backticks) preservando originalName
// como chave "json", para colar após o tipo do campo exportado.
func JSONTag(originalName string) string {
	return fmt.Sprintf("`json:%q`", originalName)
}

// OperatorMethod mapeia o símbolo de um operador de VO para o nome do método
// Go. ok é false se op não é um dos 10 operadores suportados.
func OperatorMethod(op string) (string, bool) {
	name, ok := operatorMethods[op]
	return name, ok
}

// EnumConstName concatena o nome do tipo de Enum e o nome do membro, na
// convenção Go de const de enum. Ex.: ("TransactionType", "Deposit") →
// "TransactionTypeDeposit".
func EnumConstName(enumType, member string) string {
	return enumType + member
}

// PackageName normaliza o nome de um Module DomainScript para um nome de
// pacote Go (minúsculo). Assume que moduleName já é um identificador Go
// válido (letras/dígitos/underscore iniciando por letra).
func PackageName(moduleName string) string {
	return strings.ToLower(moduleName)
}

// Ident devolve name, ou name+"_" se name colide com uma palavra reservada
// de Go. É a desambiguação de keyword aplicada a todo identificador gerado
// (locais camelCase são os que mais colidem, mas roda-se por uniformidade).
func Ident(name string) string {
	if goKeywords[name] {
		return name + "_"
	}
	return name
}

// QualifiedRef produz uma referência cross-pacote simples "pkgAlias.name"
// (ex. QualifiedRef("contracts", "OrderPlaced") → "contracts.OrderPlaced").
// pkgAlias já vem pronto de emit.Import, chamado por quem usa esta função.
func QualifiedRef(pkgAlias, name string) string {
	return pkgAlias + "." + name
}

// Dedupe resolve colisões de identificador exportado dentro de um mesmo
// pacote Go: o 2º símbolo que mapearia para o mesmo nome recebe um sufixo
// numérico determinístico pela ordem de registro (Nome, depois Nome2,
// Nome3...).
type Dedupe struct {
	seen map[string]int // nome-base -> quantas vezes já registrado
}

// NewDedupe cria um resolvedor de colisão vazio.
func NewDedupe() *Dedupe {
	return &Dedupe{seen: make(map[string]int)}
}

// Register registra name como um novo símbolo e devolve o identificador
// final a usar: name na 1ª vez, name+"2" na 2ª, name+"3" na 3ª, etc.
func (d *Dedupe) Register(name string) string {
	d.seen[name]++
	if n := d.seen[name]; n > 1 {
		return fmt.Sprintf("%s%d", name, n)
	}
	return name
}
