package lower

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"domainscript/ast"
	"domainscript/codegen/goname"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// expr.go traduz ast.Expr para texto Go (REQ-22.5/22.6, §design codegen
// 3.6/4.2). Lowerer consulta um TypeEnv (tipos dos receptores/locais/
// parâmetros — E5.0) e um goname.VOOperatorRegistry (dispatch de operador de
// VO, §4.2), mais um pequeno mapa de overrides de nome — os poucos
// receptores contextuais cujo texto Go difere do nome DomainScript (ex.:
// "event"→"ev" em Apply, espelhando o catálogo de design.md §3.5). Qualquer
// outro Ident (locais, parâmetros, variável de for) não tem override: vira
// goname.Ident(nome) — o texto Go é o próprio nome DomainScript, só escapado
// de keyword. Não há necessidade de rastrear escopo de NOMES Go aninhados (só
// de TIPOS, via TypeEnv): o Go emitido tem escopo léxico nativo — variável
// sombreada num "for"/"match" aninhado funciona igual em Go sem esforço
// extra.

// Lowerer traduz ast.Expr para texto Go.
type Lowerer struct {
	env          *TypeEnv
	reg          *goname.VOOperatorRegistry
	runtimeAlias string            // alias do import do runtime vendorado (p/ construir runtime.NewDecimalFromInt etc. — não usado diretamente por esta task, ver nota REQ-22.5 sobre decimal no dispatch binário)
	goNames      map[string]string // override: nome DS -> texto Go (ex. "event"->"ev")
	builtins     *BuiltinLowerer   // E5.3 (builtins.go): now/uuid/random/random_str/load/list/count/exists. nil ⇒ essas formas continuam "não suportadas" (comportamento de E5.1/E5.2).
}

// NewLowerer cria um Lowerer sobre env (tipos), reg (dispatch de operador de
// VO) e runtimeAlias (alias já resolvido do import do runtime vendorado, via
// emit.Emitter.Import — usado por lowerings futuras que precisem construir um
// runtime.Decimal ou runtime.BusinessError a partir daqui).
func NewLowerer(env *TypeEnv, reg *goname.VOOperatorRegistry, runtimeAlias string) *Lowerer {
	return &Lowerer{env: env, reg: reg, runtimeAlias: runtimeAlias, goNames: make(map[string]string)}
}

// WithBuiltins anexa b (E5.3, builtins.go) ao Lowerer e devolve o próprio l
// (encadeável: NewLowerer(...).WithBuiltins(b)). Opcional — sem chamar isto,
// CallExpr de now()/uuid()/random(...)/random_str(...) e qualquer
// *ast.QueryExpr seguem "não suportados", o mesmo comportamento de antes
// desta task.
func (l *Lowerer) WithBuiltins(b *BuiltinLowerer) *Lowerer {
	l.builtins = b
	return l
}

// BindGoName registra que o nome nu dsName resolve para o texto Go goExpr —
// os poucos receptores contextuais cujo texto diverge do nome DomainScript
// (ex.: BindGoName("event", "ev")).
func (l *Lowerer) BindGoName(dsName, goExpr string) {
	if l.goNames == nil {
		l.goNames = make(map[string]string)
	}
	l.goNames[dsName] = goExpr
}

// Expr é o ponto de entrada: traduz e para a expressão Go correspondente.
// Cobre literais, idents/receptores, acesso a membro, construção de VO/
// Event/Command, binário e indexação. RangeExpr e LambdaExpr têm formas Go
// que dependem de contexto que só o chamador tem (ver RangeExpr/LambdaExpr
// abaixo) — devolvem erro claro apontando para onde tratá-los.
func (l *Lowerer) Expr(e ast.Expr) (string, error) {
	switch n := e.(type) {
	case *ast.Literal:
		return l.literal(n)
	case *ast.Ident:
		return l.ident(n)
	case *ast.MemberExpr:
		return l.member(n)
	case *ast.CallExpr:
		return l.call(n)
	case *ast.BinaryExpr:
		return l.binary(n)
	case *ast.IndexExpr:
		return l.index(n)
	case *ast.QueryExpr:
		return l.queryExpr(n)
	case *ast.RangeExpr:
		return "", fmt.Errorf("codegen: RangeExpr só é válido dentro de um for (ex.: \"for i in 1..n\") — não tem forma de expressão Go isolada; tratado em nível de statement (lower/stmt.go, E5.2), não em Lowerer.Expr")
	case *ast.LambdaExpr:
		return "", fmt.Errorf("codegen: LambdaExpr precisa do tipo Go do parâmetro, que só o CHAMADOR conhece (o receptor da coleção que o usa, ex.: .distinct(t => t.orderId)) — use Lowerer.Lambda(le, paramGoType), não Lowerer.Expr")
	default:
		return "", fmt.Errorf("codegen: forma de expressão %T não suportada em Lowerer.Expr (E5.1)", e)
	}
}

// --- 1. Literais ---

func (l *Lowerer) literal(lit *ast.Literal) (string, error) {
	switch lit.Kind {
	case token.INT, token.FLOAT:
		return lit.Value, nil
	case token.STRING:
		return strconv.Quote(lit.Value), nil
	case token.TRUE:
		return "true", nil
	case token.FALSE:
		return "false", nil
	case token.DURATION:
		return lowerDurationLiteral(lit.Value)
	case token.SIZE:
		return lowerSizeLiteral(lit.Value)
	default:
		return "", fmt.Errorf("codegen: literal de kind %s não suportado em Lowerer.Expr (valor %q)", lit.Kind, lit.Value)
	}
}

// durationUnitNanos mapeia a unidade de um literal DURATION (lexer.go,
// durationUnits: "ms"/"s"/"min"/"h"/"d") para nanossegundos.
var durationUnitNanos = map[string]int64{
	"ms":  int64(time.Millisecond),
	"s":   int64(time.Second),
	"min": int64(time.Minute),
	"h":   int64(time.Hour),
	"d":   int64(24 * time.Hour),
}

// sizeUnitBytes mapeia a unidade de um literal SIZE (lexer.go, sizeUnits:
// "B"/"KB"/"MB"/"GB"/"TB") para bytes. Decisão desta task: base 1024
// (binária) — mais comum para tamanho de dado em bytes do que base 1000; sem
// convenção prévia fixada no design.md.
var sizeUnitBytes = map[string]int64{
	"B":  1,
	"KB": 1024,
	"MB": 1024 * 1024,
	"GB": 1024 * 1024 * 1024,
	"TB": 1024 * 1024 * 1024 * 1024,
}

// splitNumberUnit separa o lexema de um literal DURATION/SIZE (ex. "100ms",
// "5s", "100MB") na parte numérica (dígitos e um "." opcional) e a unidade
// (o resto). O lexer sempre produz essa forma (número colado à unidade, sem
// espaço) — ver lexer.go scanNumberSuffix.
func splitNumberUnit(lex string) (numPart, unit string) {
	i := 0
	for i < len(lex) && (lex[i] == '.' || (lex[i] >= '0' && lex[i] <= '9')) {
		i++
	}
	return lex[:i], lex[i:]
}

// scaleNumberToInt multiplica o número (inteiro ou decimal) representado por
// numPart pela escala scale: multiplicação inteira exata quando numPart não
// tem parte fracionária; arredondada (meio-para-cima) quando tem (ex.:
// "1.5h").
func scaleNumberToInt(numPart string, scale int64) (int64, error) {
	if !strings.Contains(numPart, ".") {
		n, err := strconv.ParseInt(numPart, 10, 64)
		if err != nil {
			return 0, err
		}
		return n * scale, nil
	}
	f, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, err
	}
	return int64(f*float64(scale) + 0.5), nil
}

// lowerDurationLiteral traduz o lexema de um literal DURATION (ex. "5s",
// "100ms") para "time.Duration(<nanossegundos>)" — o número e a unidade são
// resolvidos em tempo de GERAÇÃO, não delegados ao Go em runtime.
func lowerDurationLiteral(lex string) (string, error) {
	numPart, unit := splitNumberUnit(lex)
	unitNanos, ok := durationUnitNanos[unit]
	if numPart == "" || !ok {
		return "", fmt.Errorf("codegen: literal DURATION malformado ou unidade desconhecida: %q", lex)
	}
	nanos, err := scaleNumberToInt(numPart, unitNanos)
	if err != nil {
		return "", fmt.Errorf("codegen: literal DURATION %q: %w", lex, err)
	}
	return fmt.Sprintf("time.Duration(%d)", nanos), nil
}

// DurationLiteralSeconds converte o lexema de um literal DURATION (ex.
// "100ms", "1s") para segundos, como float64 — usado por
// codegen/decl_metric.go (H3, REQ-30.3) para materializar os buckets de um
// Metric histogram em tempo de GERAÇÃO como um literal Go nativo
// ([]float64{...}), em vez de reconstruir "time.Duration(N).Seconds()" em
// tempo de EXECUÇÃO para um valor que já é conhecido em tempo de compilação
// do transpiler. Reaproveita a MESMA tabela de unidades de
// lowerDurationLiteral (durationUnitNanos) — fonte única de verdade de
// quanto vale cada sufixo de DURATION.
func DurationLiteralSeconds(lex string) (float64, error) {
	numPart, unit := splitNumberUnit(lex)
	unitNanos, ok := durationUnitNanos[unit]
	if numPart == "" || !ok {
		return 0, fmt.Errorf("codegen: literal DURATION malformado ou unidade desconhecida: %q", lex)
	}
	nanos, err := scaleNumberToInt(numPart, unitNanos)
	if err != nil {
		return 0, fmt.Errorf("codegen: literal DURATION %q: %w", lex, err)
	}
	return float64(nanos) / 1e9, nil
}

// lowerSizeLiteral traduz o lexema de um literal SIZE (ex. "100MB") para o
// número cru de bytes (int64), resolvido em tempo de geração.
func lowerSizeLiteral(lex string) (string, error) {
	numPart, unit := splitNumberUnit(lex)
	unitBytes, ok := sizeUnitBytes[unit]
	if numPart == "" || !ok {
		return "", fmt.Errorf("codegen: literal SIZE malformado ou unidade desconhecida: %q", lex)
	}
	bytes, err := scaleNumberToInt(numPart, unitBytes)
	if err != nil {
		return "", fmt.Errorf("codegen: literal SIZE %q: %w", lex, err)
	}
	return strconv.FormatInt(bytes, 10), nil
}

// --- 2. Idents/receptores (via override + TypeEnv) ---

// ident resolve um *ast.Ident por prioridade: (1) override explícito em
// l.goNames (ex.: "event"->"ev"); (2) vinculado no TypeEnv (receptor/
// parâmetro/local conhecido) — texto Go é goname.Ident(nome); (3) símbolo do
// módulo (tipo declarado usado como valor nu, ex. um Enum referenciado sem
// membro ainda) — mesma forma textual de (2), goname.Ident(nome); a
// diferença entre (2) e (3) é só COMO o nome foi resolvido (local vs. global),
// não o texto produzido. Um nome que não resolve em nenhum dos três é erro de
// geração: o front-end já validou nomes (REQ-9), então isso não deveria
// acontecer sobre um programa válido.
func (l *Lowerer) ident(n *ast.Ident) (string, error) {
	if goExpr, ok := l.goNames[n.Name]; ok {
		return goExpr, nil
	}
	if _, ok := l.env.LookupType(n.Name); ok {
		return goname.Ident(n.Name), nil
	}
	if t := l.env.TypeOfName(n.Name); !types.IsError(t) {
		return goname.Ident(n.Name), nil
	}
	return "", fmt.Errorf("codegen: identificador %q não resolvido (sem override, não é local/receptor conhecido, nem símbolo do módulo)", n.Name)
}

// --- 3. MemberExpr: campo exportado vs. método embutido ---

// member traduz X.Name. "caller" é reconhecido por FORMA antes de qualquer
// outra coisa (TypeEnv nunca o semeia com um tipo real — decisão documentada
// em env.go); "X.state" sobre um valor de Aggregate CARREGADO (ex. "doc.
// state.content" num UseCase/Query, G1a — o mesmo padrão do exemplo do spec
// §2.5, "person.state.document") é reconhecido logo em seguida (ver
// stateFieldAccess); qualquer outro X é inferido via types.Model (o caminho
// comum: idents/membros/chamadas) para decidir se Name é campo exportado
// (Shape/VO), constante de Enum, ou — sobre um primitivo — uma forma
// inesperada (método embutido deveria chegar aqui como CallExpr, não
// MemberExpr puro).
func (l *Lowerer) member(n *ast.MemberExpr) (string, error) {
	if id, ok := n.X.(*ast.Ident); ok && id.Name == "caller" {
		return l.callerMember(n.Name)
	}

	xGo, err := l.Expr(n.X)
	if err != nil {
		return "", err
	}
	if goExpr, handled, err := l.stateFieldAccess(n, xGo); handled {
		return goExpr, err
	}
	xType := l.inferType(n.X)
	switch t := xType.(type) {
	case *types.ShapeType, *types.VOType:
		// Campo exportado por padrão: um VO composto também pode, em tese,
		// nomear um método de Operator igual a um campo, mas Operator não
		// aparece como MemberExpr puro (aparece como CallExpr/BinaryExpr) —
		// ver §design 4.2.
		return xGo + "." + goname.ExportField(n.Name), nil
	case *types.EnumType:
		// Acesso qualificado a membro de Enum (ex. "TransactionType.Deposit")
		// vira a const Go correspondente (goname.EnumConstName) — não usa
		// xGo (o "receptor" é o próprio tipo, não um valor).
		return goname.EnumConstName(t.Name, n.Name), nil
	case *types.Primitive:
		return "", fmt.Errorf("codegen: MemberExpr puro sobre primitivo %q (%s.%s) — método embutido deve aparecer como CallExpr, não como MemberExpr isolado", t.Name, xGo, n.Name)
	default:
		return "", fmt.Errorf("codegen: não sei traduzir %s.%s: tipo do receptor é %s", xGo, n.Name, xType.String())
	}
}

// stateFieldAccess reconhece "X.state" quando X é um valor de Aggregate
// CARREGADO (ex. "doc" de "doc = load Document(...)" num UseCase/Query, G1a
// — o mesmo padrão do exemplo do spec §2.5, "person.state.document" dentro
// de "Query GetDocumentUrl"): fora do Handle/Apply do próprio Aggregate (onde
// "self"/"state" já resolvem via BindGoName para "<receiver>.state",
// decl_aggregate.go), "doc" é um Ident comum, sem override — quem decide a
// forma aqui é o nome literal "state" sobre um receptor cujo tipo é o shape
// do PRÓPRIO Aggregate (types.Model.TypeOf de um símbolo Aggregate — o mesmo
// shape que sema/rules_typecheck.go, REQ-12, também usa para self/state
// DENTRO do Aggregate). Sem este caso especial, xType (o tipo desta
// MemberExpr) seria types.ErrorType: types.Model.Members(ShapeType) devolve
// os campos do state DIRETAMENTE (nunca um campo "state" a mais, ver
// types/model.go) — então nem esta função nem inferType (abaixo, chamado
// pelo ACESSO SEGUINTE, ex. ".content") saberiam o que fazer com "doc.state"
// sem este reconhecimento.
//
// Devolve o texto Go "<xGo>.state" (o campo NÃO-exportado do struct do
// Aggregate, decl_aggregate.go/emitAggregateStruct — legal porque o Go
// gerado vive no MESMO pacote) quando reconhecido; handled=false (sem erro)
// em qualquer outro caso — o chamador (member) segue o caminho normal.
func (l *Lowerer) stateFieldAccess(n *ast.MemberExpr, xGo string) (goExpr string, handled bool, err error) {
	if n.Name != "state" {
		return "", false, nil
	}
	shape, ok := l.inferType(n.X).(*types.ShapeType)
	if !ok || shape.Kind != symbols.KindAggregate {
		return "", false, nil
	}
	return xGo + ".state", true, nil
}

// callerMember reconhece caller.id/caller.authenticated por FORMA (o nome
// literal "caller"): o contrato runtime.Caller expõe ID()/Authenticated()
// como MÉTODOS (codegen/rtsrc/caller.go.txt), então ambos viram CHAMADA de
// método, não campo. caller.hasRole(role) é uma chamada explícita (CallExpr),
// fora do escopo de MemberExpr.
func (l *Lowerer) callerMember(name string) (string, error) {
	callerGo := l.callerGoName()
	switch name {
	case "id":
		return callerGo + ".ID()", nil
	case "authenticated":
		return callerGo + ".Authenticated()", nil
	default:
		return "", fmt.Errorf("codegen: caller.%s não suportado como MemberExpr (só id/authenticated viram método sem argumento aqui; hasRole é uma chamada explícita, CallExpr)", name)
	}
}

// callerGoName devolve o texto Go do receptor "caller": o override
// registrado em l.goNames, se houver, senão goname.Ident("caller").
func (l *Lowerer) callerGoName() string {
	if g, ok := l.goNames["caller"]; ok {
		return g
	}
	return goname.Ident("caller")
}

// --- 4. Construção de VO/Event/Command ---

// call traduz um *ast.CallExpr. Só a forma "Fn é um *ast.Ident que nomeia um
// tipo declarado (VO/Event/Command/...)" é suportada aqui — construção, não
// chamada de função/método comum. Chamadas de método embutido, built-ins e
// Query são E5.3/E6+ (fora do escopo desta task).
func (l *Lowerer) call(n *ast.CallExpr) (string, error) {
	id, ok := n.Fn.(*ast.Ident)
	if !ok {
		return "", fmt.Errorf("codegen: CallExpr com Fn %T não suportado em Lowerer.Expr (só construção de tipo via identificador nu; chamada de método/built-in é E5.3/E6+)", n.Fn)
	}
	// Um local que sombreia o nome do tipo é uma chamada de função, não
	// construção — mesma regra de types/infer.go inferCall.
	if _, shadowed := l.env.LookupType(id.Name); shadowed {
		return "", fmt.Errorf("codegen: %q está vinculado como local/receptor — CallExpr sobre um nome sombreado não é construção de tipo, e chamada de função não é suportada em Lowerer.Expr nesta task", id.Name)
	}

	// Built-ins de FUNÇÃO (now/uuid/random/random_str, E5.3) são reconhecidas
	// por NOME, não por tipo — "now" etc. não são símbolos declarados no
	// programa. Checadas antes do switch de construção de tipo abaixo.
	if l.builtins != nil {
		if goExpr, handled, err := l.builtins.CallFunc(l, id.Name, n.Args); handled {
			return goExpr, err
		}
	}

	switch t := l.env.TypeOfName(id.Name).(type) {
	case *types.VOType:
		return l.constructVO(t, n.Args)
	case *types.ShapeType:
		return l.constructShape(t, n.Args)
	default:
		return "", fmt.Errorf("codegen: CallExpr sobre %q não é construção de VO/Event/Command conhecida — chamada de função/built-in não é suportada em Lowerer.Expr nesta task (E5.3/E6+)", id.Name)
	}
}

// constructVO traduz a construção de um ValueObject. NewX (E3.1) sempre
// devolve (X, error) — não cabe embutida numa expressão maior (Go não aceita
// uma chamada de 2 valores em contexto de 1 valor). Por isso:
//
//   - VO WRAPPER (Base != nil) com exatamente 1 argumento posicional (a
//     única forma de construção de um wrapper) vira uma CONVERSÃO DE TIPO Go
//     nativa "X(v)" — não "NewX(v)". Isso é seguro porque o Go gerado para um
//     wrapper é "type X Base" (goname.GoFieldType, identidade): "X(v)" é uma
//     conversão de UM valor, sintaticamente válida em qualquer expressão
//     (ex.: "state.Active == ActiveStatus(true)", REQ-22.5). O trade-off
//     documentado: a conversão NÃO roda Valid (ao contrário de NewX) — uma
//     lacuna aceitável para essa forma aninhada (o caso comum é comparar
//     contra um literal já type-checked pelo front-end, REQ-13); o caminho
//     validado (NewX + propagação de erro) é sempre usado em posição de
//     STATEMENT (E5.2 intercepta ReturnStmt/AssignStmt antes de chamar
//     Lowerer.Expr sobre uma construção assim).
//   - Qualquer outra forma (VO composto, ou wrapper com args nomeados/
//     múltiplos) é erro de geração: precisa de NewX + tratamento de erro,
//     que só cabe em nível de statement (E5.2) — não tente resolver aqui.
func (l *Lowerer) constructVO(vo *types.VOType, args []ast.Arg) (string, error) {
	if vo.Base != nil && len(args) == 1 && args[0].Name == "" {
		argGo, err := l.Expr(args[0].Value)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s(%s)", vo.Name, argGo), nil
	}
	return "", fmt.Errorf("codegen: construção de ValueObject %q em posição de expressão pura não é suportada por Lowerer.Expr — New%s devolve (%s, error) e precisa de tratamento em nível de statement (E5.2 intercepta ReturnStmt/AssignStmt cujo Value é uma construção de VO antes de chamar Lowerer.Expr sobre ela)", vo.Name, vo.Name, vo.Name)
}

// constructShape traduz a construção de um Event/Command (ou qualquer outro
// ShapeType construído via CallExpr) para um literal de struct Go com campos
// nomeados: "Nome{Campo1: v1, Campo2: v2}". Sem validação (E4.2 não gera NewX
// para Event/Command, só o struct puro) — por isso, ao contrário de VO, cabe
// como expressão pura de 1 valor sem ressalvas.
//
// Args nomeados (Arg.Name != "" em QUALQUER arg) casam por nome contra
// t.Fields; args posicionais (nenhum nomeado) casam pela ORDEM DECLARADA de
// t.Fields (o slice ordenado, não o mapa de Members()).
func (l *Lowerer) constructShape(t *types.ShapeType, args []ast.Arg) (string, error) {
	named := false
	for _, a := range args {
		if a.Name != "" {
			named = true
			break
		}
	}

	if named {
		return l.constructShapeNamed(t, args)
	}
	return l.constructShapePositional(t, args)
}

func (l *Lowerer) constructShapeNamed(t *types.ShapeType, args []ast.Arg) (string, error) {
	fieldSet := make(map[string]bool, len(t.Fields))
	for _, f := range t.Fields {
		fieldSet[f.Name] = true
	}

	byName := make(map[string]ast.Expr, len(args))
	for _, a := range args {
		if a.Name == "" {
			return "", fmt.Errorf("codegen: construção de %s mistura argumentos nomeados e posicionais", t.Name)
		}
		if !fieldSet[a.Name] {
			return "", fmt.Errorf("codegen: construção de %s: campo %q desconhecido", t.Name, a.Name)
		}
		byName[a.Name] = a.Value
	}

	assigns := make([]string, 0, len(byName))
	for _, f := range t.Fields {
		val, ok := byName[f.Name]
		if !ok {
			continue // campo não informado: zero value Go
		}
		goVal, err := l.Expr(val)
		if err != nil {
			return "", err
		}
		assigns = append(assigns, fmt.Sprintf("%s: %s", goname.ExportField(f.Name), goVal))
	}
	return fmt.Sprintf("%s{%s}", t.Name, strings.Join(assigns, ", ")), nil
}

func (l *Lowerer) constructShapePositional(t *types.ShapeType, args []ast.Arg) (string, error) {
	if len(args) > len(t.Fields) {
		return "", fmt.Errorf("codegen: construção de %s: %d argumentos posicionais, tipo só declara %d campos", t.Name, len(args), len(t.Fields))
	}
	assigns := make([]string, 0, len(args))
	for i, a := range args {
		goVal, err := l.Expr(a.Value)
		if err != nil {
			return "", err
		}
		assigns = append(assigns, fmt.Sprintf("%s: %s", goname.ExportField(t.Fields[i].Name), goVal))
	}
	return fmt.Sprintf("%s{%s}", t.Name, strings.Join(assigns, ", ")), nil
}

// --- 5. BinaryExpr: dispatch §4.2 via goname.LowerVOBinaryDispatch ---

func (l *Lowerer) binary(n *ast.BinaryExpr) (string, error) {
	leftGo, err := l.Expr(n.Left)
	if err != nil {
		return "", err
	}
	rightGo, err := l.Expr(n.Right)
	if err != nil {
		return "", err
	}
	leftType := l.inferType(n.Left)
	rightType := l.inferType(n.Right)
	return goname.LowerVOBinaryDispatch(l.reg, n.Op, leftGo, leftType.String(), rightGo, rightType.String())
}

// --- 6. IndexExpr ---

// index traduz X[Index] por passthrough direto: Go indexa nativamente sobre
// []T/map[K]V, a forma de List<T>/AppendList<T>/Set<T>/Map<K,V>.
func (l *Lowerer) index(n *ast.IndexExpr) (string, error) {
	xGo, err := l.Expr(n.X)
	if err != nil {
		return "", err
	}
	idxGo, err := l.Expr(n.Index)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s[%s]", xGo, idxGo), nil
}

// --- 7. QueryExpr (E5.3, builtins.go): load/list/count/exists/store/... ---

// queryExpr delega para l.builtins a forma Go de um *ast.QueryExpr em
// posição de expressão PURA (REQ-22.7(a)). Sem um BuiltinLowerer anexado
// (WithBuiltins nunca chamado), toda QueryExpr é "não suportada" — mesmo
// comportamento de antes de E5.3. Só "exists" cabe aqui de fato:
// load/list/count devolvem (_, error) em Go e exigem hoisting em nível de
// statement — l.builtins.QueryExprPure devolve o erro claro apontando para
// isso quando alcançado nesta posição.
func (l *Lowerer) queryExpr(n *ast.QueryExpr) (string, error) {
	if l.builtins == nil {
		return "", fmt.Errorf("codegen: QueryExpr (%s ...) não suportado sem BuiltinLowerer configurado — anexe um via Lowerer.WithBuiltins (E5.3)", n.Op)
	}
	return l.builtins.QueryExprPure(l, n)
}

// --- 8. LambdaExpr ---

// Lambda traduz um LambdaExpr para uma closure Go, dado o tipo Go já
// resolvido do parâmetro (paramGoType) — informação que só o CHAMADOR (quem
// sabe qual coleção está sendo mapeada) tem. Não exercitado pelo wallet;
// implementado para Marco F (distinct/sum/focus, §20).
//
// paramGoType também nomeia um tipo DomainScript declarado (VO/Enum/Shape) —
// a identidade de nome entre DS e Go para esses tipos (goname.GoFieldType) —
// então, quando resolve via TypeEnv.TypeOfName, o parâmetro é vinculado num
// escopo-filho para que o corpo do lambda possa usar MemberExpr sobre ele
// normalmente. Para um paramGoType primitivo (ex. "string"), TypeOfName não
// resolve nada: o parâmetro fica sem tipo no escopo-filho (mesma postura
// conservadora de TypeEnv.ChildForIter — sem palpite).
func (l *Lowerer) Lambda(le *ast.LambdaExpr, paramGoType string) (string, error) {
	childEnv := l.env.Child()
	if t := l.env.TypeOfName(paramGoType); !types.IsError(t) {
		childEnv.Bind(le.Param, t)
	}
	child := &Lowerer{env: childEnv, reg: l.reg, runtimeAlias: l.runtimeAlias, goNames: l.goNames, builtins: l.builtins}

	bodyGo, err := child.Expr(le.Body)
	if err != nil {
		return "", fmt.Errorf("codegen: lambda %s => ...: %w", le.Param, err)
	}

	resultType, err := goTypeString(child.inferType(le.Body))
	if err != nil {
		return "", fmt.Errorf("codegen: lambda %s => ...: tipo de retorno: %w", le.Param, err)
	}

	return fmt.Sprintf("func(%s %s) %s { return %s }", goname.Ident(le.Param), paramGoType, resultType, bodyGo), nil
}

// --- Núcleo: inferência de tipo para decidir a forma Go. ---

// inferType infere o tipo estático de e (§design 3.6a). O caminho comum é
// types.Model.Infer (cobre literal/ident/membro/chamada/binário/índice/
// lista), usando o TypeEnv como types.Scope; QueryExpr/MatchExpr/LambdaExpr —
// que Model.Infer sempre devolve como ErrorType (types/infer.go, ramo
// default) — passam por TypeEnv.InferAssignRHS, que os cobre. "X.state"
// sobre um Aggregate carregado (G1a, ver stateFieldAccess/member) é o
// terceiro caso especial: types.Model.Infer devolveria ErrorType (Members()
// de um Aggregate nunca inclui um campo "state" a mais — só os campos do
// state diretamente), mas o texto Go PRECISA da mesma forma type-checável de
// X para que um acesso mais externo (ex. o ".content" de "doc.state.
// content") resolva corretamente — self/state são tipados de forma
// IDÊNTICA ao próprio Aggregate (mesma convenção de sema/rules_typecheck.go,
// REQ-12), então "X.state" aqui devolve o MESMO ShapeType de X. Nunca
// devolve nil: no pior caso, types.ErrorType (o sentinela anti-cascata).
func (l *Lowerer) inferType(e ast.Expr) types.Type {
	switch n := e.(type) {
	case *ast.QueryExpr, *ast.MatchExpr, *ast.LambdaExpr:
		t, err := l.env.InferAssignRHS(e)
		if err != nil || t == nil {
			return types.ErrorType
		}
		return t
	case *ast.MemberExpr:
		if n.Name == "state" {
			if shape, ok := l.inferType(n.X).(*types.ShapeType); ok && shape.Kind == symbols.KindAggregate {
				return shape
			}
		}
		return l.env.model.Infer(l.env.module, e, l.env)
	default:
		return l.env.model.Infer(l.env.module, e, l.env)
	}
}

// goTypeString mapeia um types.Type já inferido para a forma Go
// correspondente — usado só por Lambda (o resultado do corpo precisa de uma
// anotação de tipo Go explícita na closure, já que Go não infere o tipo de
// retorno de um func literal a partir do corpo). Primitivos via
// goname.GoPrimitive/GoOpaqueType; VO/Enum/Shape por identidade de nome
// (goname.GoFieldType); Generic recursivamente via goname.GoGeneric. Um tipo
// sem forma Go conhecida (ex. types.ErrorType, *types.FuncType) é erro de
// geração.
func goTypeString(t types.Type) (string, error) {
	switch x := t.(type) {
	case *types.Primitive:
		if s, ok := goname.GoPrimitive(x.Name); ok {
			return s, nil
		}
		if s, ok := goname.GoOpaqueType(x.Name); ok {
			return s, nil
		}
		return "", fmt.Errorf("codegen: primitivo desconhecido: %s", x.Name)
	case *types.VOType, *types.EnumType, *types.ShapeType:
		return t.String(), nil // identidade: nome DS == nome Go (goname.GoFieldType)
	case *types.Generic:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			s, err := goTypeString(a)
			if err != nil {
				return "", err
			}
			args[i] = s
		}
		s, ok := goname.GoGeneric(x.Ctor, args)
		if !ok {
			return "", fmt.Errorf("codegen: construtor genérico desconhecido ou aridade inválida: %s", x.Ctor)
		}
		return s, nil
	default:
		return "", fmt.Errorf("codegen: não sei o tipo Go de %s (%T)", t.String(), t)
	}
}
