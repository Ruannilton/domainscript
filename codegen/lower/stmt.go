package lower

import (
	"fmt"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/types"
)

// stmt.go traduz ast.Stmt para linhas Go (E5.2, REQ-22.1/2/3/4/8, §design
// codegen 3.6/4.3): ensure, break/break all/continue, match, for, emit,
// return/assign/log — mais o mecanismo de HOISTING que os sustenta.
//
// Descoberta central desta task: Lowerer.Expr (E5.1) REJEITA a construção de
// um VO composto (ou wrapper com args nomeados/múltiplos) em posição de
// expressão pura, porque NewX sempre devolve (X, error) — não cabe embutida
// numa expressão Go de 1 valor. Isso não acontece só como Value direto de um
// AssignStmt/ReturnStmt (o caso que o comentário de constructVO já cobre):
// acontece em QUALQUER posição de argumento, ex. "state.entries.add(
// StatementEntry(...))" — a construção de StatementEntry é argumento de uma
// chamada de método, não o Value de um Assign. O mesmo vale para um
// BinaryExpr cujo dispatch de operador de VO (§4.2 ramo a) chama um método
// declarado: esse método SEMPRE devolve (T, error) por convenção de E3.2
// (mesmo quando Lowerer.Expr não erra ao lowerizá-lo — binary() só produz o
// TEXTO da chamada, sem se importar se ela pode falhar).
//
// Decisão de engenharia (documentada aqui, única vez): hoisting é o CAMINHO
// ÚNICO E CANÔNICO (não "tenta Lowerer.Expr primeiro, cai pra hoisting no
// erro") — a alternativa mais simples ("tenta Expr, hoisting só no erro") é
// INSUFICIENTE: Lowerer.Expr NÃO erra num BinaryExpr cujo operador de VO é
// fallível (ele só devolve o texto da chamada, cego a erro), então "sucesso
// de Expr" não é sinal confiável de "não precisa hoisting". A rota canônica
// evita essa armadilha e evita duplicar a lógica de decisão em dois lugares.
//
// exprHoisted reescreve a árvore de e BOTTOM-UP, substituindo cada nó
// problemático por uma referência a uma variável temporária (via um
// *ast.Ident sintético, vinculado por texto em Lowerer.goNames e, quando o
// tipo é conhecido, por tipo em TypeEnv — o que permite que Lowerer.Expr
// resolva a árvore reescrita inteira normalmente ao final, sem duplicar a
// lógica de member/binary/construção já escrita em expr.go).

// StmtContext descreve como sair da função corrente quando uma construção
// hoisted ou um "ensure ... else Error" falha — difere por construto (§design
// 4.3): Handle devolve ([]runtime.Event, error) — ZeroValues=["nil"]; Apply
// não devolve erro algum — Panics=true (uma falha ali é corrupção de dados:
// o Handle que emitiu o evento já validou antes de emitir, então Apply é
// infalível por construção no caminho de replay — panic é a saída certa, não
// um "return" que não existe).
type StmtContext struct {
	// ZeroValues são os valores Go de zero para cada retorno ANTES do error
	// final (ex. ["nil"] para Handle). Ignorado se Panics.
	ZeroValues []string
	// Panics é true para corpos SEM nenhum retorno de erro (Apply).
	Panics bool
	// SuccessReturn é o texto Go do "return de sucesso" deste construto (ex.
	// "return events, nil" para Handle; "return" para um UseCase.execute que
	// só devolve error e já está em erro=nil implícito), usado por
	// ReturnStmt SEM Value (REQ-22.8) — ver a nota na doc de returnStmt sobre
	// por que isso não dá pra inferir sozinho nesta task (E6.1/E7.2 ainda não
	// existem para dizer a forma de sucesso do CONSTRUTO ao redor). Vazio ⇒
	// "return" cru.
	SuccessReturn string
}

// ExitOnError devolve a linha Go que sai da função ao encontrar errExpr (uma
// expressão Go já pronta que avalia pro erro, ex. "err" ou "ErrInactiveWallet").
func (ctx StmtContext) ExitOnError(errExpr string) string {
	if ctx.Panics {
		return fmt.Sprintf("panic(%s)", errExpr)
	}
	parts := append(append([]string{}, ctx.ZeroValues...), errExpr)
	return "return " + strings.Join(parts, ", ")
}

// stmtLowererShared é o estado que TODOS os StmtLowerer de um mesmo corpo
// (o raiz e os filhos abertos por ForStmt) precisam compartilhar: o contador
// de temporárias (nunca reinicia dentro do corpo inteiro — evita colisão de
// nome entre hoistings em blocos irmãos/aninhados) e o contador de labels de
// "for" (um por corpo, não por nível de aninhamento).
type stmtLowererShared struct {
	tmpCounter   int
	labelCounter int
}

// StmtLowerer traduz ast.Stmt para linhas Go, emitindo no *emit.Emitter
// fornecido (Line/Block). Usa um Lowerer (E5.1) embutido para expressões-
// folha e o mecanismo de hoisting (acima) para construções de VO em posição
// não-topo. loopDepth/outerLabel rastreiam o "for" sendo lowerizado no
// momento (0/"" fora de qualquer for) — necessário para "break"/"break all"/
// Nop (REQ-22.1/3): um StmtLowerer FILHO (aberto por ForStmt sobre o
// TypeEnv-filho da variável de iteração) herda outerLabel do pai e incrementa
// loopDepth, mas NUNCA recomputa outerLabel sozinho — só o for MAIS EXTERNO
// (loopDepth==0 no momento em que é lowerizado) decide se um label é
// necessário (§design 4.3, REQ-22.3).
type StmtLowerer struct {
	*Lowerer
	e          *emit.Emitter
	ctx        StmtContext
	shared     *stmtLowererShared
	loopDepth  int
	outerLabel string
}

// NewStmtLowerer cria um StmtLowerer raiz (loopDepth 0, sem label ativo, com
// um contador de temporárias/labels próprio para o corpo inteiro).
func NewStmtLowerer(l *Lowerer, e *emit.Emitter, ctx StmtContext) *StmtLowerer {
	return &StmtLowerer{Lowerer: l, e: e, ctx: ctx, shared: &stmtLowererShared{}}
}

// Block lowereiza cada Stmt de b em sequência, emitindo linhas via sl.e. b
// nil é um no-op seguro (alguns campos de AST, ex. corpo opcional, podem vir
// nulos).
func (sl *StmtLowerer) Block(b *ast.Block) error {
	if b == nil {
		return nil
	}
	for _, s := range b.Stmts {
		if err := sl.Stmt(s); err != nil {
			return err
		}
	}
	return nil
}

// Stmt lowereiza um único statement, despachando por tipo concreto (REQ-22).
func (sl *StmtLowerer) Stmt(s ast.Stmt) error {
	switch n := s.(type) {
	case *ast.Block:
		return sl.Block(n)
	case *ast.EnsureStmt:
		return sl.ensureStmt(n)
	case *ast.BreakStmt:
		return sl.breakStmt(n)
	case *ast.ContinueStmt:
		sl.e.Line("continue")
		return nil
	case *ast.MatchStmt:
		return sl.matchStmt(n)
	case *ast.ForStmt:
		return sl.forStmt(n)
	case *ast.EmitStmt:
		return sl.emitStmt(n)
	case *ast.ReturnStmt:
		return sl.returnStmt(n)
	case *ast.AssignStmt:
		return sl.assignStmt(n)
	case *ast.LogStmt:
		return sl.logStmt(n)
	case *ast.ExprStmt:
		return sl.exprStmt(n)
	default:
		return fmt.Errorf("codegen: statement %T não suportado por StmtLowerer (E5.2)", s)
	}
}

// emitLines escreve cada string de lines como uma linha CRUA — via "%s", não
// como pattern de fmt.Sprintf. As linhas já vêm prontas de
// exprHoisted/ensureAction/etc. e podem conter '%' legítimo (ex. um literal
// STRING lowerizado como "\"50% off\""); passá-las diretamente como pattern
// de Emitter.Line reinterpretaria esse '%' como um verbo de formatação — bug
// sutil evitado sistematicamente aqui.
func (sl *StmtLowerer) emitLines(lines []string) {
	for _, l := range lines {
		sl.e.Line("%s", l)
	}
}

// --- 0. Hoisting: o mecanismo central desta task. ---

// exprHoisted traduz e para o texto Go final, "hoisting" (extraindo para
// linhas ANTERIORES ao statement) toda construção de VO composto (ou wrapper
// com args nomeados/múltiplos) e todo BinaryExpr cujo dispatch de operador de
// VO chama um método declarado (§4.2 ramo a) — ambos potencialmente
// falíveis, encontrados em QUALQUER profundidade da árvore de e. Devolve o
// texto Go de e (com temporárias substituindo os nós hoisted) e as linhas
// hoisted, na ordem em que devem ser emitidas ANTES do statement que usa e.
func (sl *StmtLowerer) exprHoisted(e ast.Expr, ctx StmtContext) (string, []string, error) {
	rewritten, hoisted, err := sl.hoistSubtree(e, ctx)
	if err != nil {
		return "", nil, err
	}
	goExpr, err := sl.Expr(rewritten)
	if err != nil {
		return "", nil, err
	}
	return goExpr, hoisted, nil
}

// hoistSubtree percorre e recursivamente à mão — não reusa Lowerer.Expr como
// está, porque ele já ERRA (ou fica cego a erro, no caso do BinaryExpr) nos
// nós que esta função precisa interceptar. Desce nos nós compostos primeiro
// (hoisting propaga de baixo pra cima: uma construção aninhada dentro de
// outra também precisa ser hoisted primeiro), reconstrói o nó com os filhos
// já reescritos e só então decide se o NÓ EM SI precisa ser hoisted. Um nó
// hoisted é substituído, na árvore devolvida, por um *ast.Ident sintético
// referenciando a temporária — Lowerer.Expr resolve esse Ident normalmente
// via BindGoName (e, quando o tipo é conhecido, via TypeEnv.Bind), então o
// resto da árvore (member/binary/construção) não precisa ser reimplementado
// aqui: delega-se para Lowerer.Expr UMA VEZ, no final, em exprHoisted.
func (sl *StmtLowerer) hoistSubtree(e ast.Expr, ctx StmtContext) (ast.Expr, []string, error) {
	switch n := e.(type) {
	case nil:
		return nil, nil, nil

	case *ast.BinaryExpr:
		left, lh, err := sl.hoistSubtree(n.Left, ctx)
		if err != nil {
			return nil, nil, err
		}
		right, rh, err := sl.hoistSubtree(n.Right, ctx)
		if err != nil {
			return nil, nil, err
		}
		rebuilt := ast.NewBinaryExpr(n.Op, left, right, n.Span())
		hoisted := append(lh, rh...)
		if _, fallible := sl.fallibleVOOperator(rebuilt); fallible {
			goExpr, err := sl.Expr(rebuilt)
			if err != nil {
				return nil, nil, err
			}
			tmp := sl.newTmp()
			sl.BindGoName(tmp, tmp)
			hoisted = append(hoisted,
				fmt.Sprintf("%s, err := %s", tmp, goExpr),
				fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
			)
			// O tipo de retorno do Operator não é vinculado em TypeEnv aqui
			// (exigiria re-percorrer a declaração do VO para achar o
			// OperatorDecl.Return correspondente) — limitação documentada: um
			// acesso a MEMBRO subsequente sobre ESTE tmp específico (ex.
			// "(a + b).campo") não é suportado nesta task. Não exercitado
			// pelo wallet nem pelos testes desta task.
			return ast.NewIdent(tmp, n.Span()), hoisted, nil
		}
		return rebuilt, hoisted, nil

	case *ast.UnaryExpr:
		x, h, err := sl.hoistSubtree(n.X, ctx)
		if err != nil {
			return nil, nil, err
		}
		return ast.NewUnaryExpr(n.Op, x, n.Span()), h, nil

	case *ast.MemberExpr:
		x, h, err := sl.hoistSubtree(n.X, ctx)
		if err != nil {
			return nil, nil, err
		}
		return ast.NewMemberExpr(x, n.Name, n.NamePos, n.Span()), h, nil

	case *ast.IndexExpr:
		x, hx, err := sl.hoistSubtree(n.X, ctx)
		if err != nil {
			return nil, nil, err
		}
		idx, hi, err := sl.hoistSubtree(n.Index, ctx)
		if err != nil {
			return nil, nil, err
		}
		return ast.NewIndexExpr(x, idx, n.Span()), append(hx, hi...), nil

	case *ast.CallExpr:
		fn, fh, err := sl.hoistSubtree(n.Fn, ctx)
		if err != nil {
			return nil, nil, err
		}
		args := make([]ast.Arg, len(n.Args))
		hoisted := append([]string{}, fh...)
		for i, a := range n.Args {
			v, h, err := sl.hoistSubtree(a.Value, ctx)
			if err != nil {
				return nil, nil, err
			}
			args[i] = ast.Arg{Name: a.Name, Value: v}
			hoisted = append(hoisted, h...)
		}
		rebuilt := ast.NewCallExpr(fn, args, n.Span())
		if vo, needsHoist := sl.needsHoistVOConstruct(rebuilt); needsHoist {
			tmp, lines, err := sl.hoistVOConstruct(vo, rebuilt, ctx)
			if err != nil {
				return nil, nil, err
			}
			hoisted = append(hoisted, lines...)
			return ast.NewIdent(tmp, n.Span()), hoisted, nil
		}
		return rebuilt, hoisted, nil

	default:
		// Literal, Ident, RangeExpr, LambdaExpr, ListExpr, QueryExpr,
		// MatchExpr — nenhuma dessas formas contém, dentro do escopo desta
		// task, uma construção de VO em posição hoisted-relevante que
		// Lowerer.Expr não trate sozinho no ponto de uso apropriado
		// (RangeExpr só existe em nível de for — ForStmt trata; Lambda/
		// Match/Query têm forma Go própria fora de Lowerer.Expr). Devolvida
		// como está, sem reescrita.
		return e, nil, nil
	}
}

// fallibleVOOperator reporta se n (já com os filhos hoisted) é um BinaryExpr
// cujo dispatch de operador de VO (§4.2 ramo a) chama um método de Operator
// DECLARADO — sempre potencialmente falível (E3.2: todo Operator devolve
// (T, error)). Distinto do caso "VO sem operador declarado, comparação =='
// nativa" (esse não chama método nenhum, não é falível).
func (sl *StmtLowerer) fallibleVOOperator(n *ast.BinaryExpr) (*types.VOType, bool) {
	vo, ok := sl.inferType(n.Left).(*types.VOType)
	if !ok {
		return nil, false
	}
	return vo, sl.reg.HasOperator(vo.Name, n.Op.String())
}

// needsHoistVOConstruct reporta se n é uma construção de VO que
// Lowerer.constructVO REJEITARIA em posição de expressão pura: um VO
// composto, ou um wrapper com argumentos nomeados/múltiplos. O único caso
// aceito diretamente por Lowerer.Expr — wrapper com exatamente 1 argumento
// posicional, tratado como conversão nativa "X(v)" — NÃO precisa de
// hoisting (espelha exatamente a condição de aceitação de constructVO).
func (sl *StmtLowerer) needsHoistVOConstruct(n *ast.CallExpr) (*types.VOType, bool) {
	id, ok := n.Fn.(*ast.Ident)
	if !ok {
		return nil, false
	}
	if _, shadowed := sl.env.LookupType(id.Name); shadowed {
		return nil, false
	}
	vo, ok := sl.env.TypeOfName(id.Name).(*types.VOType)
	if !ok {
		return nil, false
	}
	if vo.Base != nil && len(n.Args) == 1 && n.Args[0].Name == "" {
		return nil, false
	}
	return vo, true
}

// hoistVOConstruct traduz a construção de vo (já com os args hoisted-
// recursivamente) para "tmpN, err := NewVO(args-na-ordem-dos-campos...)" +
// checagem de erro via ctx.ExitOnError, e vincula tmpN ao tipo vo em
// TypeEnv (permite que um MemberExpr subsequente sobre ele resolva
// corretamente — ex. se a temporária for usada depois como receptor de
// campo). Devolve o nome da temporária e as linhas a emitir antes do
// statement.
func (sl *StmtLowerer) hoistVOConstruct(vo *types.VOType, call *ast.CallExpr, ctx StmtContext) (string, []string, error) {
	argExprs, err := voConstructArgsGoOrder(vo, call.Args)
	if err != nil {
		return "", nil, err
	}
	argGo := make([]string, len(argExprs))
	for i, a := range argExprs {
		g, err := sl.Expr(a)
		if err != nil {
			return "", nil, err
		}
		argGo[i] = g
	}

	tmp := sl.newTmp()
	sl.bindTmp(tmp, vo)
	lines := []string{
		fmt.Sprintf("%s, err := New%s(%s)", tmp, vo.Name, strings.Join(argGo, ", ")),
		fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
	}
	return tmp, lines, nil
}

// voConstructArgsGoOrder devolve os Args de uma construção de VO na ORDEM
// DECLARADA dos campos de vo (a ordem que New<VO> exige, ver
// codegen/decl_value.go): args nomeados casam por nome contra vo.Fields;
// args posicionais casam pela posição (mistura de ambos é erro — a mesma
// regra de Lowerer.constructShapeNamed/Positional, E5.1).
func voConstructArgsGoOrder(vo *types.VOType, args []ast.Arg) ([]ast.Expr, error) {
	named := false
	for _, a := range args {
		if a.Name != "" {
			named = true
			break
		}
	}
	if named {
		byName := make(map[string]ast.Expr, len(args))
		for _, a := range args {
			if a.Name == "" {
				return nil, fmt.Errorf("codegen: construção de %s mistura argumentos nomeados e posicionais", vo.Name)
			}
			byName[a.Name] = a.Value
		}
		out := make([]ast.Expr, len(vo.Fields))
		for i, f := range vo.Fields {
			v, ok := byName[f.Name]
			if !ok {
				return nil, fmt.Errorf("codegen: construção de %s não informa o campo %q", vo.Name, f.Name)
			}
			out[i] = v
		}
		return out, nil
	}
	if len(args) != len(vo.Fields) {
		return nil, fmt.Errorf("codegen: construção de %s: %d argumentos posicionais, tipo declara %d campos", vo.Name, len(args), len(vo.Fields))
	}
	out := make([]ast.Expr, len(args))
	for i, a := range args {
		out[i] = a.Value
	}
	return out, nil
}

func (sl *StmtLowerer) newTmp() string {
	sl.shared.tmpCounter++
	return fmt.Sprintf("tmp%d", sl.shared.tmpCounter)
}

func (sl *StmtLowerer) bindTmp(name string, t types.Type) {
	sl.env.Bind(name, t)
	sl.BindGoName(name, name)
}

// --- 1. EnsureStmt (REQ-22.1). ---

func (sl *StmtLowerer) ensureStmt(n *ast.EnsureStmt) error {
	condGo, hoisted, err := sl.exprHoisted(n.Cond, sl.ctx)
	if err != nil {
		return err
	}
	actionLines, err := sl.ensureAction(n.Else)
	if err != nil {
		return err
	}
	sl.emitLines(hoisted)
	var bodyErr error
	sl.e.Block("if !("+condGo+")", func() {
		sl.emitLines(actionLines)
	})
	return bodyErr
}

// ensureAction traduz a ação de "ensure Cond else Action" (o corpo do
// "if !(Cond) { ... }") conforme a forma de Action, garantida pelo parser
// (parser/parse_stmt.go, parseEnsureAction): um *ast.ExprStmt cujo X é um
// Ident (nome de Error, ou o sentinela "Nop"), um *ast.BreakStmt, um
// *ast.ContinueStmt, ou nil (sem "else" — não deveria acontecer sobre um
// programa validado; o parser só devolve Else nil quando ELSE está ausente,
// um erro de sintaxe que o front-end já teria barrado antes de chegar aqui).
func (sl *StmtLowerer) ensureAction(els ast.Stmt) ([]string, error) {
	switch a := els.(type) {
	case nil:
		return nil, fmt.Errorf("codegen: ensure sem 'else' — forma inesperada sobre um programa validado (bug de geração)")

	case *ast.BreakStmt:
		line, err := sl.breakLine(a)
		if err != nil {
			return nil, err
		}
		return []string{line}, nil

	case *ast.ContinueStmt:
		return []string{"continue"}, nil

	case *ast.ExprStmt:
		id, ok := a.X.(*ast.Ident)
		if !ok {
			return nil, fmt.Errorf("codegen: ensure ... else: ação não suportada (%T)", a.X)
		}
		if id.Name == "Nop" {
			if sl.loopDepth == 0 {
				return nil, fmt.Errorf("codegen: ensure ... else Nop fora de um for sendo lowerizado — bug de geração (a semântica do front-end só permite Nop dentro de laço, REQ-5)")
			}
			return []string{"continue"}, nil
		}
		return []string{sl.ctx.ExitOnError("Err" + id.Name)}, nil

	default:
		return nil, fmt.Errorf("codegen: ensure ... else: ação não suportada (%T)", els)
	}
}

// --- 2. break/break all/continue (REQ-22.3). ---

func (sl *StmtLowerer) breakStmt(n *ast.BreakStmt) error {
	line, err := sl.breakLine(n)
	if err != nil {
		return err
	}
	sl.e.Line("%s", line)
	return nil
}

// breakLine traduz um *ast.BreakStmt para a linha Go correspondente:
// "break" para "break" simples (defensivamente exige estar dentro de um for
// sendo lowerizado); "break <outerLabel>" para "break all" (exige um label
// já calculado pelo for mais externo, ver forStmt).
func (sl *StmtLowerer) breakLine(b *ast.BreakStmt) (string, error) {
	if !b.All {
		if sl.loopDepth == 0 {
			return "", fmt.Errorf("codegen: break fora de um for sendo lowerizado — bug de geração")
		}
		return "break", nil
	}
	if sl.outerLabel == "" {
		return "", fmt.Errorf("codegen: break all fora de um for sendo lowerizado (ou sem label pré-calculado) — bug de geração")
	}
	return "break " + sl.outerLabel, nil
}

// --- 3. MatchStmt (REQ-22.2) — switch exaustivo. ---

func (sl *StmtLowerer) matchStmt(n *ast.MatchStmt) error {
	for _, arm := range n.Arms {
		if arm.Guard != nil {
			return sl.matchStmtGuarded(n)
		}
	}
	return sl.matchStmtEnum(n)
}

// matchStmtEnum traduz um match SEM guard, exaustivo sobre um Enum, para
// "switch Subject { case EnumConstA: BodyA; ... }" — SEM default (o
// front-end já garantiu exaustividade, REQ-5.5; um switch Go sem default
// sobre um Enum-tipo-nomeado não reporta exaustividade formalmente do ponto
// de vista do compilador Go, o que é aceitável, §design 4.3).
func (sl *StmtLowerer) matchStmtEnum(n *ast.MatchStmt) error {
	subjectGo, hoisted, err := sl.exprHoisted(n.Subject, sl.ctx)
	if err != nil {
		return err
	}
	enumType, ok := sl.inferType(n.Subject).(*types.EnumType)
	if !ok {
		return fmt.Errorf("codegen: match sem guard: sujeito não é Enum (%s) — forma não suportada nesta task", sl.inferType(n.Subject).String())
	}
	sl.emitLines(hoisted)

	var bodyErr error
	sl.e.Block("switch "+subjectGo, func() {
		for _, arm := range n.Arms {
			if bodyErr != nil {
				return
			}
			caseLits := make([]string, 0, len(arm.Patterns))
			for _, p := range arm.Patterns {
				mem, ok := p.(*ast.MemberExpr)
				if !ok {
					bodyErr = fmt.Errorf("codegen: match sobre Enum: padrão não suportado (%T), esperava Enum.Membro", p)
					return
				}
				caseLits = append(caseLits, goname.EnumConstName(enumType.Name, mem.Name))
			}
			sl.e.Line("case %s:", strings.Join(caseLits, ", "))
			if bodyErr = sl.Stmt(arm.Body); bodyErr != nil {
				return
			}
		}
	})
	return bodyErr
}

// matchStmtGuarded traduz um match COM guard (when) para "switch { case
// guard1: Body1; ...; default: BodyWildcard }" (switch sem subject, cada
// case é uma condição booleana) — o braço wildcard ('_') vira o default.
func (sl *StmtLowerer) matchStmtGuarded(n *ast.MatchStmt) error {
	var bodyErr error
	sl.e.Block("switch", func() {
		for _, arm := range n.Arms {
			if bodyErr != nil {
				return
			}
			if isWildcardMatchStmtArm(arm) {
				sl.e.Line("default:")
				bodyErr = sl.Stmt(arm.Body)
				continue
			}
			if arm.Guard == nil {
				bodyErr = fmt.Errorf("codegen: match com guard: braço sem 'when' e sem wildcard '_' não suportado")
				return
			}
			condGo, hoisted, err := sl.exprHoisted(arm.Guard, sl.ctx)
			if err != nil {
				bodyErr = err
				return
			}
			if len(hoisted) > 0 {
				// Um switch Go SEM subject só aceita 'case'/'default'
				// diretamente entre chaves — não cabem statements soltos
				// (as linhas hoisted) entre um 'case' e o próximo. Erro
				// claro em vez de Go inválido (NFR-14).
				bodyErr = fmt.Errorf("codegen: guard de match com construção que precisa de hoisting não é suportado (statements não cabem entre 'case's de um switch sem subject)")
				return
			}
			sl.e.Line("case %s:", condGo)
			bodyErr = sl.Stmt(arm.Body)
		}
	})
	return bodyErr
}

func isWildcardMatchStmtArm(arm ast.MatchStmtArm) bool {
	for _, p := range arm.Patterns {
		if astutil.IsIdent(p, "_") {
			return true
		}
	}
	return false
}

// --- 4. ForStmt (REQ-22.3) — for ... range / for i := lo; i <= hi; i++. ---

func (sl *StmtLowerer) forStmt(n *ast.ForStmt) error {
	label := sl.outerLabel
	emitLabel := false
	if sl.loopDepth == 0 {
		label = ""
		if containsBreakAll(n.Body) {
			sl.shared.labelCounter++
			label = fmt.Sprintf("outer%d", sl.shared.labelCounter)
		}
		emitLabel = label != ""
	}

	if r, ok := n.Iter.(*ast.RangeExpr); ok {
		return sl.forRange(n, r, label, emitLabel)
	}
	return sl.forCollection(n, label, emitLabel)
}

// forRange traduz "for Var in Low..High" para "for i := Low; i <= High;
// i++" — Var é vinculado como integer no TypeEnv-filho do corpo.
func (sl *StmtLowerer) forRange(n *ast.ForStmt, r *ast.RangeExpr, label string, emitLabel bool) error {
	lowGo, lowHoisted, err := sl.exprHoisted(r.Low, sl.ctx)
	if err != nil {
		return err
	}
	highGo, highHoisted, err := sl.exprHoisted(r.High, sl.ctx)
	if err != nil {
		return err
	}
	sl.emitLines(lowHoisted)
	sl.emitLines(highHoisted)

	childEnv := sl.env.Child()
	childEnv.Bind(n.Var, &types.Primitive{Name: "integer"})
	child := sl.childForLoop(childEnv, label)

	idx := goname.Ident(n.Var)
	if emitLabel {
		sl.e.Line("%s:", label)
	}
	var bodyErr error
	sl.e.Block(fmt.Sprintf("for %s := %s; %s <= %s; %s++", idx, lowGo, idx, highGo, idx), func() {
		bodyErr = child.Block(n.Body)
	})
	return bodyErr
}

// forCollection traduz "for Var in Iter" (Iter é uma coleção) para "for _,
// Var := range Iter". O tipo do elemento é inferido via
// TypeEnv.InferAssignRHS + ChildForIter (§design 3.6a) para que o corpo
// resolva Var corretamente.
func (sl *StmtLowerer) forCollection(n *ast.ForStmt, label string, emitLabel bool) error {
	iterGo, iterHoisted, err := sl.exprHoisted(n.Iter, sl.ctx)
	if err != nil {
		return err
	}
	sl.emitLines(iterHoisted)

	iterType, err := sl.env.InferAssignRHS(n.Iter)
	if err != nil {
		return fmt.Errorf("codegen: for %s in ...: %w", n.Var, err)
	}
	child := sl.childForLoop(sl.env.ChildForIter(n.Var, iterType), label)

	if emitLabel {
		sl.e.Line("%s:", label)
	}
	var bodyErr error
	sl.e.Block(fmt.Sprintf("for _, %s := range %s", goname.Ident(n.Var), iterGo), func() {
		bodyErr = child.Block(n.Body)
	})
	return bodyErr
}

// childForLoop abre um StmtLowerer FILHO para o corpo de um for: usa env
// (o TypeEnv-filho que tipa a variável de iteração), mas o MESMO
// ctx/shared (não reinicia contagem de temporárias/labels) e loopDepth+1;
// outerLabel é herdado (label), nunca recomputado pelo filho.
func (sl *StmtLowerer) childForLoop(env *TypeEnv, label string) *StmtLowerer {
	childLowerer := &Lowerer{env: env, reg: sl.reg, runtimeAlias: sl.runtimeAlias, goNames: sl.goNames}
	return &StmtLowerer{
		Lowerer:    childLowerer,
		e:          sl.e,
		ctx:        sl.ctx,
		shared:     sl.shared,
		loopDepth:  sl.loopDepth + 1,
		outerLabel: label,
	}
}

// containsBreakAll reporta se b contém, em qualquer profundidade (inclusive
// dentro de for/match/ensure aninhados — astutil.ForEachStmt já desce nesses
// níveis), um *ast.BreakStmt{All: true}.
func containsBreakAll(b *ast.Block) bool {
	found := false
	astutil.ForEachStmt(b, func(s ast.Stmt) {
		if brk, ok := s.(*ast.BreakStmt); ok && brk.All {
			found = true
		}
	})
	return found
}

// --- 5. EmitStmt (REQ-22.4). ---

// emitStmt traduz "emit Evento(args)" para "events = append(events,
// &Evento{...})". Assume que uma variável Go "events" (\[\]runtime.Event)
// já está declarada no escopo do Handle sendo lowerizado — é responsabilidade
// do EMISSOR do Handle (E6.1) declará-la antes de lowerizar o corpo;
// StmtLowerer só emite o append.
func (sl *StmtLowerer) emitStmt(n *ast.EmitStmt) error {
	if _, ok := n.Call.(*ast.CallExpr); !ok {
		return fmt.Errorf("codegen: emit: esperava construção de Event (CallExpr), got %T", n.Call)
	}
	goExpr, hoisted, err := sl.exprHoisted(n.Call, sl.ctx)
	if err != nil {
		return fmt.Errorf("codegen: emit: %w", err)
	}
	sl.emitLines(hoisted)
	sl.e.Line("events = append(events, &%s)", goExpr)
	return nil
}

// --- 6. ReturnStmt/AssignStmt/LogStmt (REQ-22.8). ---

// returnStmt traduz "return [Value]". SEM Value, a forma de "sucesso" do
// construto ao redor é ambígua sem mais contexto do chamador — um Handle
// bem-sucedido precisa de "return events, nil", não um "return" vazio, mas
// StmtLowerer não sabe disso sozinho (E6.1/E7.2, que decidem a assinatura da
// função ao redor, ainda não existem). Decisão desta task (documentada em
// §design 4.3/1.3 do prompt): StmtContext.SuccessReturn é o texto Go que o
// CHAMADOR de NewStmtLowerer decide para esse caso (ex. "return events,
// nil"); vazio ⇒ "return" cru.
//
// COM Value: hoisting sobre Value, depois "return <valor>" — não compõe
// com ctx.ZeroValues (que só faz sentido no caminho de ERRO). Limitação
// documentada: um construto cujo "return <value>" de sucesso precisa
// compor com valores adicionais (ex. um Handle que também devolvesse um
// segundo valor along the value) não é coberto aqui — implementa-se o caso
// mais comum e testável (Query.execute, que devolve exatamente o tipo
// declarado): "return <value>" direto.
func (sl *StmtLowerer) returnStmt(n *ast.ReturnStmt) error {
	if n.Value == nil {
		if sl.ctx.SuccessReturn == "" {
			sl.e.Line("return")
			return nil
		}
		sl.e.Line("%s", sl.ctx.SuccessReturn)
		return nil
	}
	valueGo, hoisted, err := sl.exprHoisted(n.Value, sl.ctx)
	if err != nil {
		return err
	}
	sl.emitLines(hoisted)
	sl.e.Line("return %s", valueGo)
	return nil
}

func (sl *StmtLowerer) assignStmt(n *ast.AssignStmt) error {
	if id, ok := n.Target.(*ast.Ident); ok {
		return sl.assignBareIdent(id, n.Value)
	}
	return sl.assignCompound(n.Target, n.Value)
}

// assignBareIdent traduz "Name = Value" quando Name é um alvo nu: ":=" na
// 1ª atribuição NESTE escopo Go imediato (TypeEnv.BoundLocally — não conta
// um nome herdado do escopo léxico pai), "=" numa reatribuição. Depois de um
// ":=", registra o tipo do novo local via TypeEnv.Bind (InferAssignRHS)
// para que usos subsequentes do nome resolvam corretamente (§design 3.6a).
func (sl *StmtLowerer) assignBareIdent(target *ast.Ident, value ast.Expr) error {
	valueGo, hoisted, err := sl.exprHoisted(value, sl.ctx)
	if err != nil {
		return err
	}
	sl.emitLines(hoisted)

	op := "="
	if !sl.env.BoundLocally(target.Name) {
		op = ":="
		rhsType, err := sl.env.InferAssignRHS(value)
		if err != nil {
			return fmt.Errorf("codegen: %s = ...: %w", target.Name, err)
		}
		sl.env.Bind(target.Name, rhsType)
	}
	sl.e.Line("%s %s %s", goname.Ident(target.Name), op, valueGo)
	return nil
}

// assignCompound traduz "Target = Value" quando Target é um alvo composto
// (ex. "state.balance = ..."): sempre "=" (mutação de campo, nunca
// declaração); hoisting só sobre Value (Target é lowerizado direto — um
// MemberExpr não tem construção de VO a hoistar em si).
func (sl *StmtLowerer) assignCompound(target, value ast.Expr) error {
	valueGo, hoisted, err := sl.exprHoisted(value, sl.ctx)
	if err != nil {
		return err
	}
	targetGo, err := sl.Expr(target)
	if err != nil {
		return err
	}
	sl.emitLines(hoisted)
	sl.e.Line("%s = %s", targetGo, valueGo)
	return nil
}

// exprStmt traduz um *ast.ExprStmt cujo X é uma chamada — hoje, só o pequeno
// conjunto de métodos embutidos que goname.GoBuiltinCall conhece (ex.
// AppendList.add, exercitado por Apply do wallet real: "state.entries.add(
// StatementEntry(...))"). Chamada de método de domínio (dispatch de Handle,
// ex. "wallet.Deposit(...)") e built-ins de Query (load/list/count/exists)
// são E5.3/E6+ — fora do escopo desta task; Lowerer.Expr também não os
// suporta hoje (call() só reconhece Fn=Ident como construção de tipo).
func (sl *StmtLowerer) exprStmt(s *ast.ExprStmt) error {
	call, ok := s.X.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("codegen: ExprStmt de %T não suportado por StmtLowerer (E5.2)", s.X)
	}
	lines, err := sl.exprStmtCall(call, sl.ctx)
	if err != nil {
		return err
	}
	sl.emitLines(lines)
	return nil
}

// exprStmtCall traduz uma chamada de método embutido "X.method(args...)"
// (Fn é um *ast.MemberExpr) para as linhas Go equivalentes, hoisting tanto o
// receptor quanto os argumentos.
func (sl *StmtLowerer) exprStmtCall(call *ast.CallExpr, ctx StmtContext) ([]string, error) {
	mem, ok := call.Fn.(*ast.MemberExpr)
	if !ok {
		return nil, fmt.Errorf("codegen: ExprStmt de CallExpr com Fn %T não suportado nesta task (só método embutido sobre um MemberExpr, ex. state.entries.add(...); chamada de método de domínio/built-in de Query é E5.3/E6+)", call.Fn)
	}

	recvGo, hoisted, err := sl.exprHoisted(mem.X, ctx)
	if err != nil {
		return nil, err
	}
	recvType := sl.inferType(mem.X)
	receiverShape := builtinReceiverShape(recvType)
	if receiverShape == "" {
		return nil, fmt.Errorf("codegen: método %q sobre receptor de tipo %s não suportado em ExprStmt (E5.3/E6+)", mem.Name, recvType.String())
	}

	args := make([]string, len(call.Args))
	for i, a := range call.Args {
		if a.Name != "" {
			return nil, fmt.Errorf("codegen: argumento nomeado %q não suportado em chamada de método embutido", a.Name)
		}
		argGo, argHoisted, err := sl.exprHoisted(a.Value, ctx)
		if err != nil {
			return nil, err
		}
		hoisted = append(hoisted, argHoisted...)
		args[i] = argGo
	}

	bm := goname.BuiltinMethod{Receiver: receiverShape, Method: mem.Name}
	goExpr, ok := goname.GoBuiltinCall(recvGo, bm, args)
	if !ok {
		return nil, fmt.Errorf("codegen: método embutido desconhecido: %s.%s", receiverShape, mem.Name)
	}
	return append(hoisted, goExpr), nil
}

// builtinReceiverShape devolve o "shape" de tipo (mesmo sentido de
// goname.BuiltinMethod.Receiver) de t: o Ctor de um Generic (ex.
// "AppendList"), ou o nome de um Primitive (ex. "string"). "" para qualquer
// outro tipo (VO/Enum/Shape — sem método embutido definido sobre eles).
func builtinReceiverShape(t types.Type) string {
	switch x := t.(type) {
	case *types.Generic:
		return x.Ctor
	case *types.Primitive:
		return x.Name
	default:
		return ""
	}
}

// logStmt traduz "log Level Message { Fields }" para uma chamada simples de
// log/slog (REQ-22.8) — sem contexto de trace completo (isso é observabilidade
// avançada, H2); aqui só o básico: slog.<Level>(<Message>, "campo1", valor1, ...).
func (sl *StmtLowerer) logStmt(n *ast.LogStmt) error {
	slogAlias := sl.e.Import("log/slog")

	msgGo := `""`
	if n.Message != nil {
		g, hoisted, err := sl.exprHoisted(n.Message, sl.ctx)
		if err != nil {
			return err
		}
		sl.emitLines(hoisted)
		msgGo = g
	}

	args := []string{msgGo}
	for _, f := range n.Fields {
		fg, hoisted, err := sl.exprHoisted(f.Value, sl.ctx)
		if err != nil {
			return err
		}
		sl.emitLines(hoisted)
		args = append(args, strconv.Quote(f.Name), fg)
	}

	sl.e.Line("%s.%s(%s)", slogAlias, logLevelMethod(n.Level), strings.Join(args, ", "))
	return nil
}

// logLevelMethod mapeia o nível textual de um LogStmt para o método de
// log/slog correspondente; default (nível ausente/desconhecido) é Info.
func logLevelMethod(level string) string {
	switch strings.ToLower(level) {
	case "debug":
		return "Debug"
	case "warn", "warning":
		return "Warn"
	case "error":
		return "Error"
	default:
		return "Info"
	}
}
