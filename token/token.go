package token

import "strconv"

// Kind classifica uma unidade léxica da DomainScript: literais, pontuação,
// operadores e keywords. O valor zero é ILLEGAL.
type Kind int

const (
	ILLEGAL Kind = iota // token desconhecido (ex.: caractere inválido)
	EOF                 // fim do arquivo

	literal_beg
	IDENT     // identificador: Wallet, walletId
	INT       // 42
	FLOAT     // 3.14
	STRING    // "texto" (Lit já sem aspas e com escapes resolvidos)
	DURATION  // 5s, 48h
	RATE      // 300/min
	SIZE      // 100MB
	VERSIONID // v1 (version_id)
	literal_end

	operator_beg
	LBRACE // {
	RBRACE // }
	LPAREN // (
	RPAREN // )
	LBRACK // [
	RBRACK // ]
	COMMA  // ,
	DOT    // .
	COLON  // :
	ARROW  // ->
	PLUS   // +
	MINUS  // -
	STAR   // *
	SLASH  // /
	ASSIGN // =
	EQ     // ==
	NEQ    // !=
	LT     // <
	GT     // >
	LE     // <=
	GE     // >=
	operator_end

	keyword_beg
	// declarações de topo e de configuração (REQ-2.1/2.2)
	VALUEOBJECT
	ENUM
	ERROR
	EVENT
	PUBLICEVENT
	UPCAST
	AGGREGATE
	COMMAND
	USECASE
	VIEW
	PROJECTION
	QUERY
	POLICY
	WORKER
	NOTIFICATION
	ADAPTER
	FOREIGN
	SAGA
	METRIC
	MODULE
	INTERFACE
	TOPOLOGY
	VERSION
	TEST
	FIXTURE
	// controle de fluxo (REQ-2.4)
	ENSURE
	MATCH
	FOR
	IN
	LOG
	EMIT
	RETURN
	BREAK
	CONTINUE
	ELSE
	WHEN
	// lógicos e literais booleanos
	AND
	OR
	NOT
	TRUE
	FALSE
	// referências de domínio (REQ-4.4)
	REF
	HANDLES
	ON
	keyword_end
)

// kinds mapeia cada Kind ao seu texto. Operadores e keywords usam o lexema
// literal; demais usam o nome do Kind, para mensagens de diagnóstico legíveis.
var kinds = [...]string{
	ILLEGAL: "ILLEGAL",
	EOF:     "EOF",

	IDENT:     "IDENT",
	INT:       "INT",
	FLOAT:     "FLOAT",
	STRING:    "STRING",
	DURATION:  "DURATION",
	RATE:      "RATE",
	SIZE:      "SIZE",
	VERSIONID: "VERSIONID",

	LBRACE: "{",
	RBRACE: "}",
	LPAREN: "(",
	RPAREN: ")",
	LBRACK: "[",
	RBRACK: "]",
	COMMA:  ",",
	DOT:    ".",
	COLON:  ":",
	ARROW:  "->",
	PLUS:   "+",
	MINUS:  "-",
	STAR:   "*",
	SLASH:  "/",
	ASSIGN: "=",
	EQ:     "==",
	NEQ:    "!=",
	LT:     "<",
	GT:     ">",
	LE:     "<=",
	GE:     ">=",

	VALUEOBJECT:  "ValueObject",
	ENUM:         "Enum",
	ERROR:        "Error",
	EVENT:        "Event",
	PUBLICEVENT:  "PublicEvent",
	UPCAST:       "Upcast",
	AGGREGATE:    "Aggregate",
	COMMAND:      "Command",
	USECASE:      "UseCase",
	VIEW:         "View",
	PROJECTION:   "Projection",
	QUERY:        "Query",
	POLICY:       "Policy",
	WORKER:       "Worker",
	NOTIFICATION: "Notification",
	ADAPTER:      "Adapter",
	FOREIGN:      "Foreign",
	SAGA:         "Saga",
	METRIC:       "Metric",
	MODULE:       "Module",
	INTERFACE:    "Interface",
	TOPOLOGY:     "Topology",
	VERSION:      "Version",
	TEST:         "Test",
	FIXTURE:      "Fixture",

	ENSURE:   "ensure",
	MATCH:    "match",
	FOR:      "for",
	IN:       "in",
	LOG:      "log",
	EMIT:     "emit",
	RETURN:   "return",
	BREAK:    "break",
	CONTINUE: "continue",
	ELSE:     "else",
	WHEN:     "when",

	AND:   "and",
	OR:    "or",
	NOT:   "not",
	TRUE:  "true",
	FALSE: "false",

	REF:     "ref",
	HANDLES: "handles",
	ON:      "on",
}

// String devolve o texto associado ao Kind (lexema para operadores/keywords,
// nome para os demais), ou "Kind(n)" para um valor fora da tabela.
func (k Kind) String() string {
	if k >= 0 && int(k) < len(kinds) && kinds[k] != "" {
		return kinds[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// IsLiteral reporta se k é um literal (IDENT, INT, STRING, ...).
func (k Kind) IsLiteral() bool { return literal_beg < k && k < literal_end }

// IsOperator reporta se k é pontuação ou operador.
func (k Kind) IsOperator() bool { return operator_beg < k && k < operator_end }

// IsKeyword reporta se k é uma palavra reservada.
func (k Kind) IsKeyword() bool { return keyword_beg < k && k < keyword_end }

// keywords indexa o texto de cada keyword ao seu Kind; construído de kinds.
var keywords map[string]Kind

func init() {
	keywords = make(map[string]Kind, keyword_end-keyword_beg-1)
	for k := keyword_beg + 1; k < keyword_end; k++ {
		keywords[kinds[k]] = k
	}
}

// Lookup devolve o Kind de keyword correspondente a ident, ou IDENT se ident
// não for uma palavra reservada.
func Lookup(ident string) Kind {
	if k, ok := keywords[ident]; ok {
		return k
	}
	return IDENT
}

// Pos é uma posição no source, com linha e coluna 1-based (REQ-1.4).
type Pos struct {
	Line int
	Col  int
}

// String formata a posição como "linha:coluna".
func (p Pos) String() string {
	return strconv.Itoa(p.Line) + ":" + strconv.Itoa(p.Col)
}

// Less ordena posições por linha e, em empate, por coluna. Base da ordenação
// determinística de diagnósticos (REQ-6.3, NFR-3).
func (p Pos) Less(q Pos) bool {
	if p.Line != q.Line {
		return p.Line < q.Line
	}
	return p.Col < q.Col
}

// Token é uma unidade léxica com seu lexema e posição inicial.
type Token struct {
	Kind Kind
	Lit  string // lexema; vazio quando redundante com Kind (ex.: operadores)
	Pos  Pos
}

// String descreve o token para mensagens e depuração.
func (t Token) String() string {
	if t.Lit != "" && (t.Kind.IsLiteral() || t.Kind == ILLEGAL) {
		return t.Kind.String() + "(" + t.Lit + ")"
	}
	return t.Kind.String()
}
