package lower

import (
	"fmt"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/symbols"
	"domainscript/token"
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
	// CtxVar é o nome Go do parâmetro/local context.Context em escopo no
	// corpo sendo lowerizado (ex. "ctx"), "" quando nenhum ctx alcança este
	// corpo (H2, REQ-30.1, §design 3.13). logStmt usa isto para decidir se
	// anexa um campo "trace_id" (via runtime.TraceIDFrom(CtxVar)) a cada log
	// — UseCase/Policy/Query/Worker/Saga step já recebem ctx por construção
	// (§3.1a) e por isso setam CtxVar; Handle/Apply (decl_aggregate.go) e um
	// Upcast/Downcast de versão (versioning.go) NÃO recebem ctx hoje —
	// CtxVar fica "" para eles, deliberadamente (ver a doc de
	// codegen/decl_aggregate.go e codegen/decl_telemetry.go sobre o porquê:
	// estender Handle para aceitar ctx sob demanda é uma mudança
	// arquitetural maior, fora do escopo mínimo de H2, e nem o wallet nem o
	// shop têm um Handle/Apply com "log" para exercitá-la).
	CtxVar string
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
	// handleDispatchDeclared marca se "events, err" (o par de locais Go do
	// dispatch de Handle, E7.2 — ver handleDispatchCall) já foi declarado
	// (":=") neste corpo: a 1ª ocorrência declara, ocorrências seguintes (um
	// UseCase que despacha mais de um Handle) reatribuem ("="), senão o Go
	// gerado tentaria um ":=" sem nenhuma variável nova (erro de compilação).
	handleDispatchDeclared bool
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
	// aggregates/txGoName habilitam o reconhecimento de dispatch de Handle
	// (E7.2, §design 3.8, ver handleDispatchCall) — anexados via
	// WithHandleDispatch. aggregates nil (o default) preserva o
	// comportamento anterior a esta task: "X.Nome(...)" só reconhece método
	// embutido (goname.GoBuiltinCall).
	aggregates map[string]*ast.AggregateDecl
	txGoName   string
	// txGoNameFor roteia o dispatch de Handle (tx.Append) para um Tx
	// DIFERENTE por Aggregate — habilitado por WithHandleDispatchRouted (G1,
	// §design 3.8, 2PC): um UseCase cross-database com todos os bancos XA
	// recebe "txs map[string]runtime.Tx" em vez de um "tx" único, e cada
	// dispatch precisa indexar pelo Database que gerencia AQUELE Aggregate.
	// nil (o default) preserva txGoName fixo — nenhuma mudança para
	// UseCases de um único banco (Marco E/F).
	txGoNameFor func(aggName string) string
	// notifyAdapters/ctxGoName habilitam o reconhecimento de notify/call de
	// Notification/Adapter (F4, REQ-25.3, ver notifyOrCallStmt) — anexados
	// via WithNotifyAdapters. notifyAdapters nil (o default) preserva o
	// comportamento anterior a esta task: "Xxx(...)" nomeando uma
	// Notification em posição de ExprStmt cai no erro "não suportado" já
	// existente (nenhum caminho de F1/F2/F3 reconhecia essa forma).
	notifyAdapters map[string]*ast.AdapterDecl
	ctxGoName      string
	// emitDispatch habilita emitStmt a traduzir "emit Evento(...)" para uma
	// chamada DIRETA a Dispatcher.Publish em vez do "events = append(events,
	// ...)" de sempre — anexado via WithEmitDispatch. nil (o default)
	// preserva o comportamento de sempre (Handle/Apply, que sempre têm um
	// local "events []runtime.Event" declarado pelo emissor ao redor).
	emitDispatch *emitDispatchConfig
}

// emitDispatchConfig descreve como lowerizar um "emit" FORA de um Handle
// (que sempre acumula em "events []runtime.Event", ver a doc de emitStmt) —
// o caso novo desta task (H4, §22.4): o execute de uma Policy. Investigação
// desta task (registrada aqui, única vez): nenhum Policy real do wallet/shop
// jamais usou "emit" (o único corpo real, "execute { return }", nunca o
// exercitou) — confirmado empiricamente que emitStmt SEMPRE assumiu um
// "events" local em escopo, o que quebraria a compilação se um corpo de
// Policy (cuja assinatura fixa é "(ctx, ev) error" — runtime.Dispatcher/
// Outbox.Subscribe exigem essa forma EXATA, decl_policy.go — sem devolver
// slice algum) contivesse um "emit" sem este mecanismo.
type emitDispatchConfig struct {
	// dispatcherGoName é a expressão Go (tipicamente um var de PACOTE escrito
	// só por Wire, ex. "policyDispatcher" — decl_policy.go) que expõe um
	// runtime.Dispatcher para publicar o evento construído.
	dispatcherGoName string
	// ctxGoName é o nome Go do parâmetro/local context.Context em escopo
	// (ex. "ctx" — o parâmetro de toda função de Policy gerada).
	ctxGoName string
}

// WithEmitDispatch anexa cfg a sl (encadeável) — ver a doc de
// emitDispatchConfig e de emitStmt. Devolve o próprio sl (mesmo padrão de
// WithHandleDispatch/WithNotifyAdapters).
func (sl *StmtLowerer) WithEmitDispatch(dispatcherGoName, ctxGoName string) *StmtLowerer {
	sl.emitDispatch = &emitDispatchConfig{dispatcherGoName: dispatcherGoName, ctxGoName: ctxGoName}
	return sl
}

// NewStmtLowerer cria um StmtLowerer raiz (loopDepth 0, sem label ativo, com
// um contador de temporárias/labels próprio para o corpo inteiro).
func NewStmtLowerer(l *Lowerer, e *emit.Emitter, ctx StmtContext) *StmtLowerer {
	// l.WithEmitter(e) (I4.1, §design read-side 3.6): todo StmtLowerer real
	// tem um Emitter (e, o parâmetro acima) — anexar ao Lowerer subjacente
	// aqui, num ÚNICO ponto central, habilita o operador "in" a registrar o
	// import "slices" (Lowerer.binaryIn, expr.go) sem precisar tocar as
	// dezenas de call sites de lower.NewLowerer espalhadas por codegen/*.go:
	// toda vez que ALGUÉM envolve um Lowerer com um StmtLowerer (o caminho
	// comum para todo corpo executável — Handle/Apply/execute/Query/Policy/
	// Worker), o Lowerer ganha o Emitter de graça.
	l.WithEmitter(e)
	return &StmtLowerer{Lowerer: l, e: e, ctx: ctx, shared: &stmtLowererShared{}}
}

// WithHandleDispatch anexa aggregates (nome do Aggregate -> decl, construído
// pelo CHAMADOR — ex. EmitUseCase, E7.2) e txGoName (o nome Go do local de
// runtime.Tx corrente, ex. "tx") a sl, habilitando exprStmtCall a reconhecer
// "receptor.Handle(args...)" como dispatch de Handle (ver handleDispatchCall)
// em vez de tentar resolvê-lo como método embutido. Devolve o próprio sl
// (encadeável: NewStmtLowerer(...).WithHandleDispatch(...)).
func (sl *StmtLowerer) WithHandleDispatch(aggregates map[string]*ast.AggregateDecl, txGoName string) *StmtLowerer {
	sl.aggregates = aggregates
	sl.txGoName = txGoName
	return sl
}

// WithHandleDispatchRouted é a variante de WithHandleDispatch usada pelo
// caminho de emissão 2PC (G1, §design 3.8, decl_usecase.go): em vez de um
// txGoName fixo, txGoNameFor(aggName) decide, POR Aggregate despachado, qual
// expressão Go referencia o runtime.Tx do Database que o gerencia (ex.
// "txs[\"MainDb\"]") — o mesmo mapa que StmtLowerer.WithBuiltins(...
// .WithPerAggregateStore(...)) já usa para "load T(id)", uma única fonte de
// verdade (aggName -> Database) compartilhada entre load e dispatch. Devolve
// o próprio sl (encadeável, mesmo padrão de WithHandleDispatch).
func (sl *StmtLowerer) WithHandleDispatchRouted(aggregates map[string]*ast.AggregateDecl, txGoNameFor func(aggName string) string) *StmtLowerer {
	sl.aggregates = aggregates
	sl.txGoNameFor = txGoNameFor
	return sl
}

// txNameFor devolve a expressão Go do Tx a usar para o dispatch de aggName:
// txGoNameFor(aggName) quando roteado (G1, 2PC), senão txGoName fixo (Marco
// E/F, todo o resto do gerador) — "" quando NENHUM dos dois foi anexado
// (WithHandleDispatch/WithHandleDispatchRouted nunca chamados).
func (sl *StmtLowerer) txNameFor(aggName string) string {
	if sl.txGoNameFor != nil {
		return sl.txGoNameFor(aggName)
	}
	return sl.txGoName
}

// WithNotifyAdapters anexa adapters (nome de Notification/Adapter -> o
// *ast.AdapterDecl parceiro, mesmo nome — construído pelo CHAMADOR a partir
// dos Adapter de um módulo, ex. codegen.go/decl_policy.go/decl_usecase.go) e
// ctxGoName (o nome Go do parâmetro context.Context corrente, ex. "ctx") a
// sl, habilitando exprStmt a reconhecer "Xxx(args...)" como notify/call de
// Notification/Adapter (F4, REQ-25.3, ver notifyOrCallStmt) em vez de cair
// no dispatch normal de ExprStmt. Devolve o próprio sl (encadeável, mesmo
// padrão de WithHandleDispatch/WithBuiltins).
func (sl *StmtLowerer) WithNotifyAdapters(adapters map[string]*ast.AdapterDecl, ctxGoName string) *StmtLowerer {
	sl.notifyAdapters = adapters
	sl.ctxGoName = ctxGoName
	return sl
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

// ExprHoisted expõe exprHoisted (o mecanismo de HOISTING do arquivo, ver a
// doc acima) para chamadores FORA do pacote lower que precisam de uma
// expressão Go de UM valor a partir de e — incl. uma construção de VO
// composto, que Lowerer.Expr sozinho rejeitaria (ver constructVO) — mesmo
// mecanismo que assignStmt/returnStmt/etc. já usam internamente. Usado por
// codegen/gentest.go (H4, REQ-31): um argumento de "when Action(...)" ou um
// evento construído em "given"/"then" (ex. "Money(100, \"BRL\")") passa por
// aqui para virar "tmp1" + as linhas hoisted (NewMoney + checagem de erro,
// conforme sl.ctx — StmtContext{Panics:true} para geração de teste, mesma
// convenção de Apply) a emitir ANTES do statement que usa o resultado.
func (sl *StmtLowerer) ExprHoisted(e ast.Expr) (string, []string, error) {
	return sl.exprHoisted(e, sl.ctx)
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
		// Smart Partial Loading (§20, REQ-37, §design read-side 3.8, I6.1):
		// "<coleção>.distinct(...)"/".sum(...)"/".focus(...)" são reconhecidos
		// AQUI, ANTES da recursão genérica de Fn/Args abaixo — igual em
		// espírito ao reconhecimento de "state.entries.add(...)" em
		// exprStmtCall (mas em posição de EXPRESSÃO, não statement solto: os
		// três produzem um VALOR, nunca void). smartpartial.go implementa o
		// hoisting inteiro (receptor + lambda + loop) por conta própria — não
		// reusa fn/args parcialmente hoisted abaixo, que não sabem lidar com
		// LambdaExpr nem com o binding do parâmetro em escopo-filho.
		if mem, ok := n.Fn.(*ast.MemberExpr); ok && isSmartPartialMethod(mem.Name) {
			tmp, lines, err := sl.hoistSmartPartialMethod(mem, n, ctx)
			if err != nil {
				return nil, nil, err
			}
			return ast.NewIdent(tmp, n.Span()), lines, nil
		}

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

	case *ast.QueryExpr:
		return sl.hoistQueryExpr(n, ctx)

	default:
		// Literal, Ident, RangeExpr, LambdaExpr, ListExpr, MatchExpr —
		// nenhuma dessas formas contém, dentro do escopo desta task, uma
		// construção de VO em posição hoisted-relevante que Lowerer.Expr não
		// trate sozinho no ponto de uso apropriado (RangeExpr só existe em
		// nível de for — ForStmt trata; Lambda/Match têm forma Go própria
		// fora de Lowerer.Expr). Devolvida como está, sem reescrita.
		return e, nil, nil
	}
}

// hoistQueryExpr trata *ast.QueryExpr dentro de hoistSubtree (E5.3,
// builtins.go, REQ-22.7(a)/(b)): load/list/count/store SEMPRE precisam de
// hoisting — a chamada Go que os substitui devolve (_, error), que não cabe
// em posição de expressão pura (mesmo motivo de needsHoistVOConstruct/
// hoistVOConstruct, agora para os seams de persistência do runtime em vez de
// New<VO>). exists é uma expressão pura (boolean, nunca falha por si) — só
// desce recursivamente no Target. "delete file(ref)" (G1a) não é reescrito
// aqui: só é suportado como STATEMENT solto (queryExprStmt/deleteFileStmt,
// abaixo) — não faz sentido de negócio usá-lo como sub-expressão (Delete não
// produz valor algum, só error). "call" não é reescrito aqui: continua fora
// do escopo desta task (ver a doc de BuiltinLowerer.QueryExprPure).
func (sl *StmtLowerer) hoistQueryExpr(n *ast.QueryExpr, ctx StmtContext) (ast.Expr, []string, error) {
	switch n.Op {
	case "load":
		return sl.hoistLoad(n, ctx)
	case "list":
		return sl.hoistList(n, ctx)
	case "count":
		return sl.hoistCount(n, ctx)
	case "store":
		return sl.hoistStore(n, ctx)
	case "exists":
		target, hoisted, err := sl.hoistSubtree(n.Target, ctx)
		if err != nil {
			return nil, nil, err
		}
		return ast.NewQueryExpr(n.Op, target, n.Binding, n.Clauses, n.Span()), hoisted, nil
	default:
		return n, nil, nil
	}
}

// hoistLoad traduz "load T(id)" (dentro de hoistSubtree) para
// "tmpN, err := Load<T>(<store>, <idGo>); if err != nil { <ctx.ExitOnError> }"
// — a chamada assume a convenção de nome Load<T> (§design 3.7: reconstrução
// de um Aggregate a partir do seam de persistência), cuja EXISTÊNCIA é E6.2
// (ainda não implementada); esta task só gera a chamada. tmpN é vinculado no
// TypeEnv ao tipo de T (via TypeEnv.InferAssignRHS, E5.0), para que um uso
// subsequente (ex. "wallet.Deposit(...)") resolva corretamente.
//
// "load T(id) as V" (mapeamento de load para View, ex. o "GetWallet" real do
// wallet: "return load Wallet(id) as WalletView") é Read Side — REQ-21, E8.1
// — e falha explicitamente aqui em vez de gerar Go com o tipo errado
// (NFR-14): não é responsabilidade desta task decidir a forma de mapeamento
// para View.
func (sl *StmtLowerer) hoistLoad(n *ast.QueryExpr, ctx StmtContext) (ast.Expr, []string, error) {
	if sl.builtins == nil {
		return nil, nil, fmt.Errorf("codegen: load ...: BuiltinLowerer não configurado — anexe um via Lowerer.WithBuiltins (E5.3)")
	}
	if hasQueryClause(n.Clauses, "as") {
		return nil, nil, fmt.Errorf("codegen: load ... as V: mapeamento de load para View é Read Side (REQ-21, E8.1) — fora do escopo de builtins.go (E5.3); \"load T(id)\" sem \"as\" é suportado aqui")
	}
	call, ok := n.Target.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, nil, fmt.Errorf("codegen: load ...: forma inesperada de Target (%T) — esperava \"load T(id)\"", n.Target)
	}
	if fnIdent, ok := call.Fn.(*ast.Ident); ok && fnIdent.Name == "File" {
		return sl.hoistLoadFile(call, n, ctx)
	}

	t, err := sl.env.InferAssignRHS(n)
	if err != nil {
		return nil, nil, fmt.Errorf("codegen: load ...: %w", err)
	}

	idExpr, idHoisted, err := sl.hoistSubtree(call.Args[0].Value, ctx)
	if err != nil {
		return nil, nil, err
	}
	idGo, err := sl.Expr(idExpr)
	if err != nil {
		return nil, nil, err
	}

	tmp := sl.newTmp()
	sl.bindTmp(tmp, t)
	lines := append(idHoisted,
		fmt.Sprintf("%s, err := %s", tmp, sl.builtins.LoadCall(t.String(), idGo)),
		fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
	)
	return ast.NewIdent(tmp, n.Span()), lines, nil
}

// hoistLoadFile traduz "load File(ref)" (§2.5, G1a — o caso especial de
// hoistLoad em que o Target nomeia o tipo OPACO embutido "File", não um
// Aggregate) para "tmpN, err := <FileStorage>.Load(<ctx>, <refGo>); if err
// != nil { <ctx.ExitOnError> }" — devolve runtime.File (types.Primitive
// "File", ver env.go/inferQueryExpr). call é o CallExpr já extraído por
// hoistLoad (Target de n); o único argumento é a FileRef a carregar.
func (sl *StmtLowerer) hoistLoadFile(call *ast.CallExpr, n *ast.QueryExpr, ctx StmtContext) (ast.Expr, []string, error) {
	if len(call.Args) != 1 {
		return nil, nil, fmt.Errorf("codegen: load File(...): esperava exatamente 1 argumento (a FileRef), recebeu %d", len(call.Args))
	}

	refExpr, refHoisted, err := sl.hoistSubtree(call.Args[0].Value, ctx)
	if err != nil {
		return nil, nil, err
	}
	refGo, err := sl.Expr(refExpr)
	if err != nil {
		return nil, nil, err
	}
	callGo, err := sl.builtins.LoadFileCall(call.Args[0].Value, refGo)
	if err != nil {
		return nil, nil, err
	}

	tmp := sl.newTmp()
	sl.bindTmp(tmp, &types.Primitive{Name: "File"})
	lines := append(refHoisted,
		fmt.Sprintf("%s, err := %s", tmp, callGo),
		fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
	)
	return ast.NewIdent(tmp, n.Span()), lines, nil
}

// hoistStore traduz "store <file>" (§2.5, G1a) para "tmpN, err :=
// <FileStorage>.Store(<ctx>, <fileGo>); if err != nil { <ctx.ExitOnError> }"
// — devolve runtime.FileRef (types.Primitive "FileRef", já inferido por
// env.go/inferQueryExpr desde E5.0). n.Target é o valor File a armazenar
// (ex. "cmd.content") — também a Expr que decide QUAL FileStorage usar
// (BuiltinLowerer.resolveFileStorageName, por nome de campo).
func (sl *StmtLowerer) hoistStore(n *ast.QueryExpr, ctx StmtContext) (ast.Expr, []string, error) {
	if sl.builtins == nil {
		return nil, nil, fmt.Errorf("codegen: store ...: BuiltinLowerer não configurado — anexe um via Lowerer.WithBuiltins (G1a)")
	}

	fileExpr, fileHoisted, err := sl.hoistSubtree(n.Target, ctx)
	if err != nil {
		return nil, nil, err
	}
	fileGo, err := sl.Expr(fileExpr)
	if err != nil {
		return nil, nil, err
	}
	callGo, err := sl.builtins.StoreCall(n.Target, fileGo)
	if err != nil {
		return nil, nil, err
	}

	tmp := sl.newTmp()
	sl.bindTmp(tmp, &types.Primitive{Name: "FileRef"})
	lines := append(fileHoisted,
		fmt.Sprintf("%s, err := %s", tmp, callGo),
		fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
	)
	return ast.NewIdent(tmp, n.Span()), lines, nil
}

// hoistList traduz "list T [where Cond] [orderBy K [dir]] [skip N] [take M]
// [as V]" para "tmpN, err := <store>.Select(<ctx>, <query>); if err != nil {
// <ctx.ExitOnError> }" — desde H4 (§22.4), backed de verdade por
// runtime.Collection[T] (rtsrc/collection.go.txt), e desde o ciclo Read Side
// (REQ-33/REQ-36/REQ-38, §design read-side 2) sobre o seam Select/
// runtime.Query[T] (era List/predicado nu antes de I0.1 — ver a doc de
// ListCall/queryLiteral em builtins.go sobre a ponte). "<store>" é resolvido
// por BuiltinLowerer.store(typeName), roteado por TIPO (T, o nome nu de
// qe.Target) — o mesmo mecanismo que já roteava "load" por Aggregate em G1
// (WithPerAggregateStore), agora reusado para Policy rotear cada
// Collection[T] para o var de pacote certo (ver decl_policy.go). Desde I1.1,
// o predicado (hoistQueryPredicate, abaixo) já é sempre um "func(item T)
// (bool, error) { ... }" diretamente — a forma que Query[T].Where exige
// (REQ-36.2), sem embrulho nenhum. itemGoType (o T de Query[T]) é resolvido
// aqui — não dentro de hoistQueryPredicate/hoistOrderBy, que só o calculam
// quando HÁ "where"/"orderBy" — porque Query[T]{...} precisa de T mesmo sem
// nenhuma cláusula (o literal Go de um tipo genérico não infere argumento de
// tipo a partir de contexto como uma chamada genérica infere). tmpN é
// vinculado ao tipo List<V> (ou List<T> sem "as"), via TypeEnv.
// InferAssignRHS (E5.0), que já resolve essa forma.
//
// --- I2.1 (REQ-33.1/33.3, §design read-side 3.1/3.2): orderBy/skip/take ---
//
// A ORDEM SEMÂNTICA das cláusulas (where → orderBy → skip → take) é fixa
// independente da ordem TEXTUAL em que apareceram no fonte (§design
// read-side 3.1) — isso já é garantido no nível do RUNTIME por SelectSlice
// (rtsrc/collection.go.txt, desde I0.1); o trabalho desta função é só
// EXTRAIR cada cláusula do slice n.Clauses (que preserva a ordem textual,
// ver parser/parse_query.go) para o campo certo de Query[T], não importa em
// que posição do slice ela esteja — cada hoistXxx abaixo procura sua própria
// keyword via queryClauseByKw/queryClauseFull, ignorando a posição.
// ensureListClausesWellFormed (abaixo) recusa cláusula duplicada (dois
// "orderBy", etc. — NFR-20) e, para "count", recusa orderBy/skip/take de
// saída (REQ-33.5: sem efeito observável numa contagem).
func (sl *StmtLowerer) hoistList(n *ast.QueryExpr, ctx StmtContext) (ast.Expr, []string, error) {
	if sl.builtins == nil {
		return nil, nil, fmt.Errorf("codegen: list ...: BuiltinLowerer não configurado — anexe um via Lowerer.WithBuiltins (E5.3)")
	}
	if err := ensureListClausesWellFormed(n.Op, n.Clauses); err != nil {
		return nil, nil, err
	}

	t, err := sl.env.InferAssignRHS(n)
	if err != nil {
		return nil, nil, fmt.Errorf("codegen: list ...: %w", err)
	}

	itemGoType, err := sl.itemGoTypeOf(n)
	if err != nil {
		return nil, nil, err
	}

	predGo, hoisted, err := sl.hoistQueryPredicate(n, ctx)
	if err != nil {
		return nil, nil, err
	}

	lessGo, orderField, orderDesc, err := sl.hoistOrderBy(n)
	if err != nil {
		return nil, nil, err
	}

	skipGo, err := sl.hoistSkipTakeExpr(n, "skip")
	if err != nil {
		return nil, nil, err
	}
	takeGo, err := sl.hoistSkipTakeExpr(n, "take")
	if err != nil {
		return nil, nil, err
	}

	fields := queryLiteralFields{
		Where:      predGo,
		Less:       lessGo,
		OrderField: orderField,
		OrderDesc:  orderDesc,
		Skip:       skipGo,
		Take:       takeGo,
	}

	tmp := sl.newTmp()
	sl.bindTmp(tmp, t)
	lines := append(hoisted,
		fmt.Sprintf("%s, err := %s", tmp, sl.builtins.ListCall(astutil.HeadName(n.Target), itemGoType, fields)),
		fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
	)
	return ast.NewIdent(tmp, n.Span()), lines, nil
}

// hoistCount traduz "count [where Cond]" para "tmpN, err := <store>.Count(
// <ctx>, <query>); if err != nil { <ctx.ExitOnError> }" — mesmo roteamento
// por tipo, mesmo predicado por item e mesma ponte para Query[T] de
// hoistList (ver sua doc). tmpN é vinculado a integer (TypeEnv.
// InferAssignRHS já resolve "count" assim, independente do Target — env.go,
// inferQueryExpr). ensureListClausesWellFormed (chamada logo no início)
// recusa orderBy/skip/take em "count" com um erro claro (REQ-33.5, NFR-20):
// nenhum dos três tem efeito observável numa contagem, e aceitá-los em
// silêncio esconderia um engano de quem escreveu o "count".
func (sl *StmtLowerer) hoistCount(n *ast.QueryExpr, ctx StmtContext) (ast.Expr, []string, error) {
	if sl.builtins == nil {
		return nil, nil, fmt.Errorf("codegen: count ...: BuiltinLowerer não configurado — anexe um via Lowerer.WithBuiltins (E5.3)")
	}
	if err := ensureListClausesWellFormed(n.Op, n.Clauses); err != nil {
		return nil, nil, err
	}

	t, err := sl.env.InferAssignRHS(n)
	if err != nil {
		return nil, nil, fmt.Errorf("codegen: count ...: %w", err)
	}

	itemGoType, err := sl.itemGoTypeOf(n)
	if err != nil {
		return nil, nil, err
	}

	predGo, hoisted, err := sl.hoistQueryPredicate(n, ctx)
	if err != nil {
		return nil, nil, err
	}

	tmp := sl.newTmp()
	sl.bindTmp(tmp, t)
	lines := append(hoisted,
		fmt.Sprintf("%s, err := %s", tmp, sl.builtins.CountCall(astutil.HeadName(n.Target), itemGoType, predGo)),
		fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
	)
	return ast.NewIdent(tmp, n.Span()), lines, nil
}

// ensureNoDuplicateClause recusa uma cláusula kw que aparece mais de uma vez
// em clauses — um erro de geração claro (NFR-20), não um "usa só a última"
// silencioso: duas cláusulas "orderBy"/"where"/"skip"/"take" no mesmo
// list/count quase certamente é um engano de cópia-e-cola (a ordem
// SEMÂNTICA já é fixa, §design read-side 3.1 — "duas orderBy" não tem uma
// leitura razoável como "ordena por duas chaves", ao contrário do ORDER BY
// de SQL com vírgula).
func ensureNoDuplicateClause(op string, clauses []ast.QueryClause, kw string) error {
	n := 0
	for _, c := range clauses {
		if c.Kw == kw {
			n++
		}
	}
	if n > 1 {
		return fmt.Errorf("codegen: %s ...: cláusula %q duplicada — cada cláusula (where/orderBy/skip/take) pode aparecer no máximo uma vez por list/count (§design read-side 3.1, NFR-20)", op, kw)
	}
	return nil
}

// ensureListClausesWellFormed valida o conjunto de cláusulas where/orderBy/
// skip/take de uma QueryExpr "list"/"count" ANTES de qualquer lowering
// (§design read-side 3.1, REQ-33.5, NFR-20): nenhuma das quatro pode se
// repetir (ensureNoDuplicateClause); em "count", só "where" é aceita —
// orderBy/skip/take não têm efeito observável numa contagem (REQ-33.5) e
// aceitá-los em silêncio esconderia um engano de quem escreveu a query.
// "join"/"on"/"as" não são cobertas aqui (fora do escopo desta task —
// I4/I5/I3.2 respectivamente).
func ensureListClausesWellFormed(op string, clauses []ast.QueryClause) error {
	for _, kw := range []string{"where", "orderBy", "skip", "take"} {
		if err := ensureNoDuplicateClause(op, clauses, kw); err != nil {
			return err
		}
	}
	if op != "count" {
		return nil
	}
	for _, kw := range []string{"orderBy", "skip", "take"} {
		if hasQueryClause(clauses, kw) {
			return fmt.Errorf("codegen: count ... %s ...: cláusula %q não tem efeito observável numa contagem — só \"where\" é válido em \"count\" (REQ-33.5, NFR-20)", kw, kw)
		}
	}
	return nil
}

// itemGoTypeOf resolve o tipo Go do item de n (TypeEnv.ItemTypeOf — o MESMO
// mecanismo que hoistQueryPredicate usa para vincular o binding de "where")
// e o converte para texto Go (goTypeString, expr.go) — usado por hoistList/
// hoistCount para montar o argumento de tipo de "runtime.Query[<T>]{...}"
// (BuiltinLowerer.ListCall/CountCall, builtins.go), independentemente de
// haver "where" ou não (diferente de hoistQueryPredicate, que só resolve o
// tipo do item quando HÁ cláusula "where" — aqui é sempre necessário, porque
// um composite literal genérico exige o argumento de tipo explícito mesmo
// vazio).
func (sl *StmtLowerer) itemGoTypeOf(n *ast.QueryExpr) (string, error) {
	itemType, err := sl.env.ItemTypeOf(n)
	if err != nil {
		return "", fmt.Errorf("codegen: %s ...: %w", n.Op, err)
	}
	return goTypeString(itemType)
}

// hoistQueryPredicate extrai a cláusula "where" de uma list/count e a
// traduz para um PREDICADO POR ITEM — "nil" (texto Go literal) quando
// ausente (Query[T].Where == nil, rtsrc/collection.go.txt, trata nil como
// "todo item passa"); com "where", "func(<item> <TipoDoItem>) (bool, error)
// { ... }", onde <item> é o nome do BINDING declarado (ex. "list Ticket t
// where t.eventId == event.id", QueryExpr.Binding == "t") ou o nome
// sintético "item" quando a query não declara um (ex. "list StatementEntry
// where true", sem binding) — em QUALQUER caso, um TypeEnv/StmtLowerer-FILHO
// vincula esse nome ao tipo do item (TypeEnv.ItemTypeOf, env.go) para a
// DURAÇÃO da lowering de "where", exatamente como ForStmt já abre um
// escopo-filho para a variável de iteração (childForLoop) — member access
// dentro da condição (ex. "t.eventId") resolve contra esse tipo normalmente,
// via o mesmo Lowerer.member já existente, sem nenhum código novo de
// resolução de membro.
//
// --- Achado documentado (H4, §22.4): o redesenho que criou este mecanismo ---
//
// Antes de H4, "where" era avaliado UMA VEZ como bool solto no escopo ATUAL
// (nenhum item vinculado): correto só para uma condição que não referencia
// nenhum campo do item. Uma condição de filtragem de verdade (ex.
// "eventId == 'E1'" varrendo vários Tickets) exige que cada item seja
// vinculado ANTES de resolver a condição — esta função é o ponto único onde
// isso acontece; ListCall/CountCall (builtins.go) continuam recebendo só o
// TEXTO do predicado já pronto, sem saber se é "nil" ou uma lambda de
// verdade.
//
// --- Predicado falível (REQ-36, fecha G-8): corpo em BLOCO ---
//
// Até o ciclo Read Side (I0.1/I1.1), a assinatura do predicado era
// "func(T) bool" (sem error) — uma condição que precisasse de HOISTING (uma
// construção de VO composto ou um operador de VO falível dentro do "where",
// ex. "e.amount == Money(amount: 10, currency: \"BRL\")") não tinha onde
// acomodar um "if err != nil { ... }" antes do "return", e a função
// RECUSAVA a condição com um erro de geração — essa era a limitação G-8.
//
// I0.1 trocou Collection[T].List/Count por Select/Count sobre Query[T],
// cujo campo Where já É "func(T) (bool, error)" (REQ-36.2) — mas só mudou a
// PONTE (builtins.go/queryLiteral embrulhava a saída "func(T) bool" desta
// função em runtime.Infallible), sem tocar hoistQueryPredicate em si; a
// recusa acima de hoisting continuava de pé. Esta task (I1.1) fecha G-8 de
// verdade: o corpo do predicado agora é sempre um BLOCO — quando "where" não
// precisa de hoisting, continua uma linha só ("return <cond>, nil", o caso
// comum, sem nenhum custo extra de legibilidade); quando precisa, as linhas
// hoisted (child.exprHoisted, EXATAMENTE o mesmo mecanismo que qualquer outro
// hoisting deste arquivo usa) entram ANTES do "return", cada uma com seu
// "if err != nil { return false, err }" já embutido pelo hoisting em si —
// não há tratamento especial aqui além de escolher a forma de retorno certa
// para o predCtx abaixo. O contrato de Query[T].Where (REQ-36.1) é "erro
// aborta a chamada inteira" — SelectSlice (rtsrc/collection.go.txt) já
// implementa esse lado; este comentário documenta só o lado da GERAÇÃO.
//
// predCtx (não ctx, o StmtContext recebido de fora) é o StmtContext usado
// para hoistear O CORPO do predicado: a assinatura "(bool, error)" é FIXA
// pelo contrato de Query[T].Where, então "return false, <err>" é SEMPRE a
// saída de erro certa aqui, INDEPENDENTE de qual construto envolve o
// list/count — ctx (o parâmetro recebido) descreve ESSE construto (ex.
// ZeroValues=["nil"] para Handle, que devolve ([]runtime.Event, error);
// Panics=true para Apply) e usá-lo aqui produziria a forma ERRADA (ex. "if
// err != nil { return nil, err }" dentro de uma closure que devolve
// (bool, error), quebrando a compilação). Um predicado hoisted dentro de
// Apply (Panics) continua devolvendo (bool, error) normalmente — Query[T].
// Where nunca panica, mesmo quando o CONSTRUTO ao redor do list/count o
// faria para outro tipo de falha.
func (sl *StmtLowerer) hoistQueryPredicate(n *ast.QueryExpr, ctx StmtContext) (string, []string, error) {
	where, ok := queryClauseByKw(n.Clauses, "where")
	if !ok {
		return "nil", nil, nil
	}

	itemType, err := sl.env.ItemTypeOf(n)
	if err != nil {
		return "", nil, fmt.Errorf("codegen: %s ... where ...: %w", n.Op, err)
	}
	itemGoType, err := goTypeString(itemType)
	if err != nil {
		return "", nil, fmt.Errorf("codegen: %s ... where ...: tipo do item: %w", n.Op, err)
	}

	paramName := n.Binding
	if paramName == "" {
		paramName = "item"
	}
	paramGo := goname.Ident(paramName)

	childEnv := sl.env.Child()
	childEnv.Bind(paramName, itemType)
	child := &StmtLowerer{
		Lowerer:        &Lowerer{env: childEnv, reg: sl.reg, runtimeAlias: sl.runtimeAlias, goNames: sl.goNames, builtins: sl.builtins, emitter: sl.emitter},
		e:              sl.e,
		ctx:            sl.ctx,
		shared:         sl.shared,
		loopDepth:      sl.loopDepth,
		outerLabel:     sl.outerLabel,
		aggregates:     sl.aggregates,
		txGoName:       sl.txGoName,
		txGoNameFor:    sl.txGoNameFor,
		notifyAdapters: sl.notifyAdapters,
		ctxGoName:      sl.ctxGoName,
		emitDispatch:   sl.emitDispatch,
	}

	predCtx := StmtContext{ZeroValues: []string{"false"}}
	condGo, hoisted, err := child.exprHoisted(where, predCtx)
	if err != nil {
		return "", nil, err
	}

	if len(hoisted) == 0 {
		return fmt.Sprintf("func(%s %s) (bool, error) { return %s, nil }", paramGo, itemGoType, condGo), nil, nil
	}

	var body strings.Builder
	fmt.Fprintf(&body, "func(%s %s) (bool, error) {\n", paramGo, itemGoType)
	for _, line := range hoisted {
		fmt.Fprintf(&body, "%s\n", line)
	}
	fmt.Fprintf(&body, "return %s, nil\n}", condGo)
	return body.String(), nil, nil
}

// --- orderBy/skip/take (I2.1, REQ-33.1/33.3, §design read-side 3.1/3.2). ---

// hoistOrderBy extrai a cláusula "orderBy" de n (list/count — mas count já
// foi barrado por ensureListClausesWellFormed antes de chegar aqui) e
// traduz sua chave K para os campos Less/OrderField/OrderDesc de um literal
// Query[T] (§design read-side 3.2). Devolve lessGo == "" (e orderField ==
// "", orderDesc == false) quando NÃO há cláusula "orderBy" — o chamador
// (hoistList) então omite os três campos do literal inteiramente (Go zero
// value: nil/""/false, que SelectSlice já documenta como "ordem de
// inserção", rtsrc/collection.go.txt — nenhuma mudança de contrato aqui).
//
// --- Escopo-filho: o MESMO mecanismo de hoistQueryPredicate, mas usado
// DUAS vezes ---
//
// K é lowerizado num TypeEnv-filho com o binding vinculado ao tipo do item
// (igual a hoistQueryPredicate) — mas, ao contrário de "where" (que só
// precisa avaliar a condição UMA vez, sobre o item corrente), "Less(a, b)"
// compara DOIS itens: por isso K é lowerizado DUAS vezes, uma com o binding
// apontando para o texto Go "a" e outra para "b" (childForKeyEval, abaixo) —
// cada evocação abre seu PRÓPRIO StmtLowerer-filho com uma CÓPIA do mapa
// goNames (nunca o mapa original: mutar um mapa compartilhado entre as duas
// evocações vazaria "binding -> a" para a lowering de "b", e além disso para
// o que quer que sl lowerize DEPOIS desta chamada — goNames é um mapa,
// tipo-referência em Go).
//
// --- Direção: convenção adotada aqui (design.md deixa a cargo da task) ---
//
// Less SEMPRE significa "a deve vir ANTES de b na ordem FINAL" — a direção
// é embutida no CORPO de Less, não lida de Query[T].OrderDesc em runtime
// (SelectSlice nunca lê OrderDesc; só o futuro adapter SQL, tarefa I7, o lê
// como descritor declarativo, §design read-side 3.9). A implementação:
// calcula keyA (K com o binding apontando para "a") e keyB (K apontando
// para "b") uma única vez cada; para "ascending" (o default — Extra vazio,
// "ascending" ou "asc"), a comparação "menor que" roda com (keyA, keyB) na
// ordem natural; para "descending"/"desc", os MESMOS DOIS textos entram
// TROCADOS (keyB, keyA) na mesmíssima lógica de "menor que" — álgebra
// elementar (a > b ⇔ b < a) que funciona uniformemente sobre os quatro
// ramos da tabela de comparabilidade (nativo/Decimal.Cmp/time.Before/
// dispatch de Operator) sem duplicar a decisão de COMO comparar em cada um.
//
// --- OrderField: só quando K é um acesso de membro NU sobre o binding ---
//
// "<binding>.<campo>" (um *ast.MemberExpr cujo X é o *ast.Ident do PRÓPRIO
// binding, não um acesso aninhado tipo "<binding>.<a>.<b>" nem uma chave
// computada tipo "<binding>.<a> + <binding>.<b>") popula OrderField com o
// nome de campo DomainScript NU — a forma que um futuro adapter SQL (tarefa
// I7) mapeia para nome de coluna; qualquer outra forma de K deixa
// OrderField vazio (só a closure Less desce, §design read-side 3.2).
func (sl *StmtLowerer) hoistOrderBy(n *ast.QueryExpr) (lessGo, orderField string, orderDesc bool, err error) {
	clause, ok := queryClauseFull(n.Clauses, "orderBy")
	if !ok {
		return "", "", false, nil
	}
	key := clause.Expr
	desc := isDescendingDirection(clause.Extra)

	itemType, err := sl.env.ItemTypeOf(n)
	if err != nil {
		return "", "", false, fmt.Errorf("codegen: %s ... orderBy ...: %w", n.Op, err)
	}
	itemGoType, err := goTypeString(itemType)
	if err != nil {
		return "", "", false, fmt.Errorf("codegen: %s ... orderBy ...: tipo do item: %w", n.Op, err)
	}

	paramName := n.Binding
	if paramName == "" {
		paramName = "item"
	}

	childEnv := sl.env.Child()
	childEnv.Bind(paramName, itemType)
	childA := sl.childForKeyEval(childEnv, paramName, "a")
	childB := sl.childForKeyEval(childEnv, paramName, "b")

	keyType := childA.inferType(key)
	if types.IsError(keyType) {
		return "", "", false, fmt.Errorf("codegen: %s ... orderBy ...: não consegui inferir o tipo da chave de ordenação (%T)", n.Op, key)
	}

	lessCtx := StmtContext{ZeroValues: []string{"false"}}
	keyAGo, hoistedA, err := childA.exprHoisted(key, lessCtx)
	if err != nil {
		return "", "", false, fmt.Errorf("codegen: %s ... orderBy ...: %w", n.Op, err)
	}
	keyBGo, hoistedB, err := childB.exprHoisted(key, lessCtx)
	if err != nil {
		return "", "", false, fmt.Errorf("codegen: %s ... orderBy ...: %w", n.Op, err)
	}

	xGo, yGo := keyAGo, keyBGo
	if desc {
		xGo, yGo = keyBGo, keyAGo
	}
	cmpGo, fallible, err := sl.buildLess(keyType, xGo, yGo)
	if err != nil {
		return "", "", false, fmt.Errorf("codegen: %s ... orderBy: %w", n.Op, err)
	}

	// Forma compacta de uma linha só quando nada precisa de linha extra —
	// nem hoisting da chave (K não é uma construção falível) nem o dispatch
	// de Less em si (fallible, ex. Operator < de VO composto): o MESMO
	// espírito de hoistQueryPredicate (acima) de não pagar custo de
	// legibilidade extra no caso comum.
	if len(hoistedA) == 0 && len(hoistedB) == 0 && !fallible {
		return fmt.Sprintf("func(a, b %s) (bool, error) { return %s, nil }", itemGoType, cmpGo),
			bareMemberField(key, paramName), desc, nil
	}

	var body strings.Builder
	fmt.Fprintf(&body, "func(a, b %s) (bool, error) {\n", itemGoType)
	for _, l := range hoistedA {
		fmt.Fprintf(&body, "%s\n", l)
	}
	for _, l := range hoistedB {
		fmt.Fprintf(&body, "%s\n", l)
	}
	if fallible {
		fmt.Fprintf(&body, "lt, err := %s\n", cmpGo)
		fmt.Fprintf(&body, "if err != nil { return false, err }\n")
		fmt.Fprintf(&body, "return lt, nil\n}")
	} else {
		fmt.Fprintf(&body, "return %s, nil\n}", cmpGo)
	}

	return body.String(), bareMemberField(key, paramName), desc, nil
}

// childForKeyEval constrói um StmtLowerer-filho de sl que compartilha
// runtimeAlias/reg/builtins/emitter/estado-de-loop/dispatch (tudo que
// hoistQueryPredicate também herda ao montar seu próprio filho), mas com o
// PRÓPRIO TypeEnv (childEnv, já vinculado: paramName -> o tipo do item) e a
// PRÓPRIA CÓPIA do mapa goNames de sl, sobrepondo paramName -> goParamName
// ("a" ou "b"). Uma cópia (nunca sl.goNames em si) é obrigatória: hoistOrderBy
// chama esta função DUAS vezes (uma por lado de Less) — mutar um mapa
// COMPARTILHADO vazaria "paramName -> a" para a lowering de "b" (e além
// disso para qualquer lowering que sl faça DEPOIS desta chamada, já que
// mapas em Go são tipos-referência) — diferente de hoistQueryPredicate, que
// nunca sobrepõe o nome do binding (o parâmetro Go da closure de "where" É
// o próprio nome DS escapado) e por isso pode reusar sl.goNames sem cópia.
func (sl *StmtLowerer) childForKeyEval(childEnv *TypeEnv, paramName, goParamName string) *StmtLowerer {
	goNames := make(map[string]string, len(sl.goNames)+1)
	for k, v := range sl.goNames {
		goNames[k] = v
	}
	goNames[paramName] = goParamName
	return &StmtLowerer{
		Lowerer:        &Lowerer{env: childEnv, reg: sl.reg, runtimeAlias: sl.runtimeAlias, goNames: goNames, builtins: sl.builtins, emitter: sl.emitter},
		e:              sl.e,
		ctx:            sl.ctx,
		shared:         sl.shared,
		loopDepth:      sl.loopDepth,
		outerLabel:     sl.outerLabel,
		aggregates:     sl.aggregates,
		txGoName:       sl.txGoName,
		txGoNameFor:    sl.txGoNameFor,
		notifyAdapters: sl.notifyAdapters,
		ctxGoName:      sl.ctxGoName,
		emitDispatch:   sl.emitDispatch,
	}
}

// isDescendingDirection reporta se dir (QueryClause.Extra de "orderBy",
// parser/parse_query.go: "" quando ausente, senão "ascending"/"descending"/
// "asc"/"desc") pede ordem descendente. Qualquer outro valor (ausente,
// "ascending", "asc") é ascendente — o default do spec (§6.3).
func isDescendingDirection(dir string) bool {
	return dir == "descending" || dir == "desc"
}

// bareMemberField devolve o nome de campo DS de key quando key é EXATAMENTE
// "<paramName>.<campo>" (um *ast.MemberExpr cujo X é o *ast.Ident do
// binding, diretamente) — a única forma simples o bastante para dobrar como
// referência de coluna SQL (OrderField, §design read-side 3.2); qualquer
// outra forma (acesso aninhado, expressão computada) devolve "".
func bareMemberField(key ast.Expr, paramName string) string {
	mem, ok := key.(*ast.MemberExpr)
	if !ok {
		return ""
	}
	id, ok := mem.X.(*ast.Ident)
	if !ok || id.Name != paramName {
		return ""
	}
	return mem.Name
}

// orderablePrimitives é a linha "primitivo ordenável" da tabela de
// comparabilidade (§design read-side 3.2): integer/decimal/string/datetime/
// duration/size. boolean/bytes/rate (e os três tipos opacos de arquivo) NÃO
// são ordenáveis — exclusão deliberada, espelhando a tabela do design
// literalmente: "ordenar por boolean" ou "ordenar por File" é um engano de
// modelagem que este gerador recusa em vez de inventar uma forma Go.
var orderablePrimitives = map[string]bool{
	"integer": true, "decimal": true, "string": true,
	"datetime": true, "duration": true, "size": true,
}

// primitiveLess devolve a comparação "xGo < yGo" para o primitivo name —
// nativa para integer/string/duration/size (named types Go sobre um
// underlying comparável aceitam operadores relacionais diretamente, mesmo
// quando xGo/yGo têm um tipo NOMEADO diferente do primitivo cru, ex. um VO
// wrapper — daí baseGoCompare não precisar converter esses quatro casos);
// via runtime.Decimal.Cmp para decimal e time.Time.Before para datetime —
// os dois únicos primitivos cujo Go NÃO é nativamente ordenável com "<"
// (Decimal é backed por big.Int, ver rtsrc/decimal.go.txt; time.Time não
// tem operador relacional algum em Go). Um name fora de orderablePrimitives
// é erro de geração claro (NFR-20) — nunca Go que não compila.
func (sl *StmtLowerer) primitiveLess(name, xGo, yGo string) (string, bool, error) {
	if !orderablePrimitives[name] {
		return "", false, fmt.Errorf("codegen: orderBy: tipo primitivo %q não é ordenável — só integer/decimal/string/datetime/duration/size (§design read-side 3.2, NFR-20)", name)
	}
	switch name {
	case "decimal":
		return fmt.Sprintf("%s.Cmp(%s) < 0", xGo, yGo), false, nil
	case "datetime":
		return fmt.Sprintf("%s.Before(%s)", xGo, yGo), false, nil
	default: // integer, string, duration, size
		return fmt.Sprintf("%s < %s", xGo, yGo), false, nil
	}
}

// baseGoCompare prepara xGo/yGo (expressões Go de um tipo NOMEADO que
// embrulha o primitivo baseName — um VO wrapper "type X Base" ou um Enum
// "type X Base", goname/types.go) para primitiveLess: integer/string/
// duration/size passam DIRETO (Go aceita operador relacional sobre QUALQUER
// named type cujo underlying o suporte, mesmo que X não seja o próprio
// Base); decimal/datetime exigem uma CONVERSÃO explícita para o tipo Go do
// runtime/stdlib primeiro — Cmp/Before são métodos declarados sobre
// runtime.Decimal/time.Time, e um "type X runtime.Decimal" (defined type,
// não alias) NÃO herda o method-set de Decimal em Go. A conversão de
// datetime referencia o literal "time.Time" — por isso e.Import("time")
// aqui (mesma disciplina do resto do pacote: sl.e.Import é chamado no PONTO
// em que o texto "time.X" é de fato emitido, ver logStmt/log/slog acima).
func (sl *StmtLowerer) baseGoCompare(baseName, xGo, yGo string) (string, string) {
	switch baseName {
	case "decimal":
		return fmt.Sprintf("%s.Decimal(%s)", sl.runtimeAlias, xGo), fmt.Sprintf("%s.Decimal(%s)", sl.runtimeAlias, yGo)
	case "datetime":
		sl.e.Import("time")
		return fmt.Sprintf("time.Time(%s)", xGo), fmt.Sprintf("time.Time(%s)", yGo)
	default:
		return xGo, yGo
	}
}

// primitiveNameOf devolve o Name de t quando t é um *types.Primitive.
func primitiveNameOf(t types.Type) (string, bool) {
	p, ok := t.(*types.Primitive)
	if !ok {
		return "", false
	}
	return p.Name, true
}

// buildLess decide a comparação "xGo < yGo" (semântica: xGo vem antes de
// yGo na ordem ascendente) para keyType, pela tabela de comparabilidade
// (§design read-side 3.2): primitivo ordenável e VO wrapper/Enum sobre um
// primitivo ordenável são infalíveis (fallible=false); VO composto com
// Operator "<" ou ">" declarado dispara o método correspondente — SEMPRE
// falível (E3.2: todo Operator devolve (T, error), goname/vobinary.go) — o
// CHAMADOR (hoistOrderBy) decide como propagar esse erro dentro do corpo de
// Less. Qualquer outro tipo (VO composto sem "<"/">" declarado, Shape,
// Generic, tipo primitivo não-ordenável, etc.) é erro de geração claro
// (NFR-20), nomeando o tipo e a alternativa.
func (sl *StmtLowerer) buildLess(keyType types.Type, xGo, yGo string) (string, bool, error) {
	switch t := keyType.(type) {
	case *types.Primitive:
		return sl.primitiveLess(t.Name, xGo, yGo)

	case *types.VOType:
		if t.Base != nil {
			baseName, ok := primitiveNameOf(t.Base)
			if !ok {
				return "", false, fmt.Errorf("codegen: orderBy: ValueObject %q embrulha um tipo não-primitivo — sem forma de ordenar (§design read-side 3.2, NFR-20)", t.Name)
			}
			bx, by := sl.baseGoCompare(baseName, xGo, yGo)
			return sl.primitiveLess(baseName, bx, by)
		}
		if sl.reg.HasOperator(t.Name, "<") {
			return fmt.Sprintf("%s.Lt(%s)", xGo, yGo), true, nil
		}
		if sl.reg.HasOperator(t.Name, ">") {
			return fmt.Sprintf("%s.Gt(%s)", yGo, xGo), true, nil
		}
		return "", false, fmt.Errorf("codegen: orderBy: ValueObject composto %q não declara Operator < nem > — sem forma de ordenar; declare um dos dois Operators, ou use uma chave computada sobre um campo ordenável (§design read-side 3.2, NFR-20)", t.Name)

	case *types.EnumType:
		baseName, ok := primitiveNameOf(t.Base)
		if !ok {
			return "", false, fmt.Errorf("codegen: orderBy: Enum %q: tipo base não é primitivo — sem forma de ordenar (§design read-side 3.2, NFR-20)", t.Name)
		}
		bx, by := sl.baseGoCompare(baseName, xGo, yGo)
		return sl.primitiveLess(baseName, bx, by)

	default:
		return "", false, fmt.Errorf("codegen: orderBy: tipo %s não é comparável — só primitivo ordenável, VO wrapper/Enum sobre primitivo ordenável, ou VO composto com Operator </> declarado (§design read-side 3.2, NFR-20)", keyType.String())
	}
}

// hoistSkipTakeExpr traduz a cláusula "skip"/"take" de n (kw é "skip" ou
// "take") para uma expressão Go inteira PURA — ao contrário de where/
// orderBy, skip/take NUNCA precisam de hoisting (REQ-33: são expressões
// inteiras comuns no spec, ex. "skip page * 20 take 20" — nenhuma
// construção de VO composto nem operador falível faz sentido de negócio
// nessa posição). Devolve "" quando a cláusula está ausente — o chamador
// (hoistList) então omite Query[T].Skip/Take do literal (Go zero value 0,
// que SelectSlice trata como "ausente" também, rtsrc/collection.go.txt).
func (sl *StmtLowerer) hoistSkipTakeExpr(n *ast.QueryExpr, kw string) (string, error) {
	e, ok := queryClauseByKw(n.Clauses, kw)
	if !ok {
		return "", nil
	}
	goExpr, err := sl.Expr(e)
	if err != nil {
		return "", fmt.Errorf("codegen: %s ... %s ...: %w", n.Op, kw, err)
	}
	return goExpr, nil
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
		if g, ok := sl.decimalLiteralArg(vo, i, a); ok {
			argGo[i] = g
			continue
		}
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

// decimalLiteralArg reporta se o argumento de índice i de uma construção de
// VO composto (vo.Fields[i].Type == "decimal") é um literal INT/FLOAT nu
// (ex. "Money(0, \"BRL\")") — caso em que Lowerer.Expr sozinho devolveria o
// texto cru do literal ("0"), que NÃO compila contra o parâmetro
// runtime.Decimal de NewX (struct, sem conversão implícita de constante).
// Mesma lacuna que codegen/vobody.go:lowerDecimalOperand já resolve para
// corpos de VO (Valid/Operator) — aqui é o EQUIVALENTE para argumentos de
// construção de VO composto em corpos de Handle/Apply/UseCase (E5.2+), nunca
// exercitada antes de H4 (nenhum *.ds existente construía um VO composto com
// um literal decimal nu num desses corpos). Só INT é tratado (runtimeAlias +
// ".NewDecimalFromInt(N)"): FLOAT decimal nesta posição continua não
// suportado, mesma lacuna documentada de lowerDecimalOperand — não
// exercitada por nenhum caso real (§21/§22).
func (sl *StmtLowerer) decimalLiteralArg(vo *types.VOType, i int, a ast.Expr) (string, bool) {
	if i >= len(vo.Fields) {
		return "", false
	}
	prim, ok := vo.Fields[i].Type.(*types.Primitive)
	if !ok || prim.Name != "decimal" {
		return "", false
	}
	lit, ok := a.(*ast.Literal)
	if !ok || lit.Kind != token.INT {
		return "", false
	}
	return fmt.Sprintf("%s.NewDecimalFromInt(%s)", sl.runtimeAlias, lit.Value), true
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
// outerLabel é herdado (label), nunca recomputado pelo filho. emitDispatch
// (H4, §22.4) é propagado: um "emit" dentro de "for x in xs { emit ... }" no
// execute de uma Policy (ex. RefundAllOnEventCancelled, um refund por
// Ticket casado) precisa da MESMA configuração do pai — sem isto, o corpo
// do for cairia de volta no "events = append(...)" de sempre (quebrado
// dentro de uma Policy, que não declara "events").
func (sl *StmtLowerer) childForLoop(env *TypeEnv, label string) *StmtLowerer {
	childLowerer := &Lowerer{env: env, reg: sl.reg, runtimeAlias: sl.runtimeAlias, goNames: sl.goNames, builtins: sl.builtins, emitter: sl.emitter}
	return &StmtLowerer{
		Lowerer:      childLowerer,
		e:            sl.e,
		ctx:          sl.ctx,
		shared:       sl.shared,
		loopDepth:    sl.loopDepth + 1,
		outerLabel:   label,
		aggregates:   sl.aggregates,
		txGoName:     sl.txGoName,
		emitDispatch: sl.emitDispatch,
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

// emitStmt traduz "emit Evento(args)". Dois caminhos, por CONSTRUTO (ver
// emitDispatchConfig): (a) o de sempre (Handle/Apply) — "events =
// append(events, &Evento{...})", assumindo uma variável Go "events"
// (\[\]runtime.Event) já declarada no escopo pelo EMISSOR do Handle (E6.1)
// antes de lowerizar o corpo; (b) desde H4 (§22.4), quando sl.emitDispatch
// foi anexado (WithEmitDispatch) — o caso de uma Policy, cuja assinatura
// fixa "(ctx, ev) error" NUNCA tem um "events" para acumular — "if err :=
// <dispatcher>.Publish(<ctx>, &Evento{...}); err != nil { <ExitOnError> }",
// publicando o evento construído DIRETO no runtime.Dispatcher exposto por
// emitDispatch (ver decl_policy.go sobre o var de pacote "policyDispatcher"
// que Wire escreve).
func (sl *StmtLowerer) emitStmt(n *ast.EmitStmt) error {
	if _, ok := n.Call.(*ast.CallExpr); !ok {
		return fmt.Errorf("codegen: emit: esperava construção de Event (CallExpr), got %T", n.Call)
	}
	goExpr, hoisted, err := sl.exprHoisted(n.Call, sl.ctx)
	if err != nil {
		return fmt.Errorf("codegen: emit: %w", err)
	}
	sl.emitLines(hoisted)
	if sl.emitDispatch != nil {
		sl.e.Line("if err := %s.Publish(%s, &%s); err != nil { %s }", sl.emitDispatch.dispatcherGoName, sl.emitDispatch.ctxGoName, goExpr, sl.ctx.ExitOnError("err"))
		return nil
	}
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

// --- 6a. Dispatch de Handle de Aggregate (E7.2, §design 3.8). ---

// handleDispatchCall reconhece "receptor.Handle(args...)" como dispatch de
// Handle de Aggregate: quando mem.X infere (sl.inferType) para o shape de um
// Aggregate CONHECIDO (uma entrada em sl.aggregates, anexado via
// WithHandleDispatch) e mem.Name bate com o nome de um Handle desse
// Aggregate, a chamada vira o padrão do §design 3.8 (o corpo de
// PerformDeposit):
//
//	events, err := wallet.Deposit(caller, cmd.Amount, cmd.Description)
//	if err != nil { return err }
//	if err := tx.Append(string(wallet.id), events); err != nil { return err }
//
// "wallet.id" acessa o campo NÃO-exportado espelho de identidade
// (decl_aggregate.go) — válido porque o UseCase gerado vive no MESMO pacote
// Go do Aggregate (mesmo módulo DomainScript); "string(...)" converte para a
// chave crua de stream exigida por Tx.Append (mesma convenção de
// idToStreamKeyExpr, decl_aggregate_load.go — sempre segura mesmo quando o
// id já é "string": conversão de string para string é um no-op válido em
// Go). "events"/"err" são declarados (":=") na 1ª ocorrência do corpo e
// reatribuídos ("=") em qualquer dispatch seguinte (sl.shared, compartilhado
// entre todo o corpo — mesma técnica de contagem de tmpCounter/labelCounter).
//
// handled=false (sem erro) quando mem.X não resolve a um Aggregate conhecido
// ou mem.Name não é um dos Handlers dele: o CHAMADOR (exprStmtCall) segue
// para o dispatch normal de método embutido. handled=true com err!=nil é uma
// falha de geração de verdade (ex. WithHandleDispatch nunca chamado) que o
// chamador deve propagar.
func (sl *StmtLowerer) handleDispatchCall(mem *ast.MemberExpr, call *ast.CallExpr, ctx StmtContext) ([]string, bool, error) {
	if sl.aggregates == nil {
		return nil, false, nil
	}
	shape, ok := sl.inferType(mem.X).(*types.ShapeType)
	if !ok || shape.Kind != symbols.KindAggregate {
		return nil, false, nil
	}
	aggDecl, ok := sl.aggregates[shape.Name]
	if !ok {
		return nil, false, nil
	}
	if findHandleDecl(aggDecl, mem.Name) == nil {
		return nil, false, nil
	}
	txName := sl.txNameFor(shape.Name)
	if txName == "" {
		return nil, true, fmt.Errorf("codegen: dispatch de Handle %s.%s: nenhum runtime.Tx anexado — chame StmtLowerer.WithHandleDispatch (E7.2)", shape.Name, mem.Name)
	}

	recvGo, hoisted, err := sl.exprHoisted(mem.X, ctx)
	if err != nil {
		return nil, true, err
	}

	args := []string{sl.callerGoName()}
	for _, a := range call.Args {
		if a.Name != "" {
			return nil, true, fmt.Errorf("codegen: dispatch de Handle %s.%s: argumento nomeado %q não suportado", shape.Name, mem.Name, a.Name)
		}
		argGo, argHoisted, err := sl.exprHoisted(a.Value, ctx)
		if err != nil {
			return nil, true, err
		}
		hoisted = append(hoisted, argHoisted...)
		args = append(args, argGo)
	}

	op := ":="
	if sl.shared.handleDispatchDeclared {
		op = "="
	}
	sl.shared.handleDispatchDeclared = true

	lines := append(hoisted,
		fmt.Sprintf("events, err %s %s.%s(%s)", op, recvGo, mem.Name, strings.Join(args, ", ")),
		fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
		fmt.Sprintf("if err := %s.Append(string(%s.id), events); err != nil { %s }", txName, recvGo, ctx.ExitOnError("err")),
	)
	return lines, true, nil
}

// findHandleDecl acha, em decl.Handlers, o *ast.HandleDecl cujo Name casa com
// name — espelha findAccessRule (decl_aggregate.go), agora dentro do pacote
// lower (que não importa codegen, a direção de dependência do projeto é
// sempre "para baixo").
func findHandleDecl(decl *ast.AggregateDecl, name string) *ast.HandleDecl {
	for _, h := range decl.Handlers {
		if h != nil && h.Name == name {
			return h
		}
	}
	return nil
}

// exprStmt traduz um *ast.ExprStmt cujo X é uma chamada: método embutido
// (goname.GoBuiltinCall, ex. AppendList.add, exercitado por Apply do wallet
// real: "state.entries.add(StatementEntry(...))") ou dispatch de Handle de
// Aggregate (E7.2, ex. "wallet.Deposit(...)" — ver handleDispatchCall). Um X
// que é *ast.QueryExpr (ex. "delete file(ref)", §2.5, G1a) é a única forma de
// operação de domínio usada como STATEMENT solto (load/list/count/exists são
// sempre valor — hoisting, hoistQueryExpr) — ver queryExprStmt.
func (sl *StmtLowerer) exprStmt(s *ast.ExprStmt) error {
	if qe, ok := s.X.(*ast.QueryExpr); ok {
		lines, err := sl.queryExprStmt(qe)
		if err != nil {
			return err
		}
		sl.emitLines(lines)
		return nil
	}

	call, ok := s.X.(*ast.CallExpr)
	if !ok {
		return fmt.Errorf("codegen: ExprStmt de %T não suportado por StmtLowerer (E5.2)", s.X)
	}
	if lines, handled, err := sl.notifyOrCallStmt(call); handled {
		if err != nil {
			return err
		}
		sl.emitLines(lines)
		return nil
	}
	lines, err := sl.exprStmtCall(call, sl.ctx)
	if err != nil {
		return err
	}
	sl.emitLines(lines)
	return nil
}

// queryExprStmt traduz um *ast.QueryExpr usado como STATEMENT solto (§2.5,
// G1a): hoje só "delete file(ref)" tem essa forma (Delete não produz valor
// algum, só error — nunca cabe em posição de expressão/atribuição). Qualquer
// outro Op alcançando aqui (ex. "wallet exists" solto, sem "ensure" ao redor)
// é erro de geração claro — não suportado por esta task.
func (sl *StmtLowerer) queryExprStmt(n *ast.QueryExpr) ([]string, error) {
	switch n.Op {
	case "delete":
		return sl.deleteFileStmt(n)
	default:
		return nil, fmt.Errorf("codegen: %s ... como statement solto não é suportado (E5.2/G1a só cobre \"delete file(ref)\")", n.Op)
	}
}

// deleteFileStmt traduz "delete file(ref)" (§2.5, G1a) para "if err :=
// <FileStorage>.Delete(<ctx>, <refGo>); err != nil { <ctx.ExitOnError> }".
// n.Target precisa ser exatamente "file(ref)" — um CallExpr cujo Fn é o
// Ident literal "file" (o marcador fixo da forma, §2.5; não um tipo/local
// declarado) com exatamente 1 argumento posicional (a FileRef a remover).
func (sl *StmtLowerer) deleteFileStmt(n *ast.QueryExpr) ([]string, error) {
	if sl.builtins == nil {
		return nil, fmt.Errorf("codegen: delete file(...): BuiltinLowerer não configurado — anexe um via Lowerer.WithBuiltins (G1a)")
	}
	call, ok := n.Target.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return nil, fmt.Errorf("codegen: delete ...: esperava \"delete file(ref)\", forma inesperada de alvo (%T)", n.Target)
	}
	fnIdent, ok := call.Fn.(*ast.Ident)
	if !ok || fnIdent.Name != "file" {
		return nil, fmt.Errorf("codegen: delete ...: esperava o marcador \"file\" (ex. \"delete file(ref)\"), got %T", call.Fn)
	}

	refExpr, refHoisted, err := sl.hoistSubtree(call.Args[0].Value, sl.ctx)
	if err != nil {
		return nil, err
	}
	refGo, err := sl.Expr(refExpr)
	if err != nil {
		return nil, err
	}
	callGo, err := sl.builtins.DeleteFileCall(call.Args[0].Value, refGo)
	if err != nil {
		return nil, err
	}

	return append(refHoisted,
		fmt.Sprintf("if err := %s; err != nil { %s }", callGo, sl.ctx.ExitOnError("err")),
	), nil
}

// notifyOrCallStmt reconhece "Xxx(args...)" como ExprStmt onde Xxx nomeia uma
// Notification cujo Adapter parceiro (mesmo nome, §9.1/9.3) foi anexado via
// WithNotifyAdapters (F4, REQ-25.3): constrói o valor da Notification
// (Lowerer.Expr já cobre isso — CallExpr sobre um *types.ShapeType é
// "construção de struct", E5.1; hoisting via exprHoisted cobre campos
// aninhados falíveis, o mesmo mecanismo de emitStmt para Event) e despacha
// via Call<Nome> (Mode "sync": erro propagado ao chamador, o mesmo mecanismo
// de "ensure ... else Error" — ctx.ExitOnError) ou Notify<Nome> (qualquer
// outro Mode, o único caso real do front-end é "async": fire-and-forget, sem
// linha de checagem de erro nenhuma — a própria FORMA do Go gerado já
// distingue notify de call, REQ-25.3).
//
// handled=false (sem erro) quando não há registry anexado, call.Fn não é um
// Ident, o nome está sombreado por um local (mesma regra de
// needsHoistVOConstruct), ou não resolve a uma Notification conhecida —
// nesses casos o CHAMADOR (exprStmt) segue para o dispatch normal de
// ExprStmt. handled=true com err!=nil é uma falha de geração de verdade (ex.
// WithNotifyAdapters nunca chamado com um ctxGoName) que o chamador deve
// propagar.
func (sl *StmtLowerer) notifyOrCallStmt(call *ast.CallExpr) ([]string, bool, error) {
	if sl.notifyAdapters == nil {
		return nil, false, nil
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok {
		return nil, false, nil
	}
	if _, shadowed := sl.env.LookupType(id.Name); shadowed {
		return nil, false, nil
	}
	shape, ok := sl.env.TypeOfName(id.Name).(*types.ShapeType)
	if !ok || shape.Kind != symbols.KindNotification {
		return nil, false, nil
	}
	adapter, ok := sl.notifyAdapters[id.Name]
	if !ok {
		return nil, false, nil
	}
	if sl.ctxGoName == "" {
		return nil, true, fmt.Errorf("codegen: %s(...): nenhum nome de contexto Go anexado — chame StmtLowerer.WithNotifyAdapters(adapters, ctxGoName) (F4)", id.Name)
	}

	notifGo, hoisted, err := sl.exprHoisted(call, sl.ctx)
	if err != nil {
		return nil, true, fmt.Errorf("codegen: %s(...): %w", id.Name, err)
	}

	lines := append([]string{}, hoisted...)
	if adapter.Mode == "sync" {
		lines = append(lines, fmt.Sprintf("if err := Call%s(%s, %s); err != nil { %s }", adapter.Name, sl.ctxGoName, notifGo, sl.ctx.ExitOnError("err")))
	} else {
		lines = append(lines, fmt.Sprintf("Notify%s(%s, %s)", adapter.Name, sl.ctxGoName, notifGo))
	}
	return lines, true, nil
}

// exprStmtCall traduz uma chamada "X.method(args...)" (Fn é um
// *ast.MemberExpr) para as linhas Go equivalentes: primeiro tenta dispatch
// de Handle (handleDispatchCall, E7.2 — só ativo quando WithHandleDispatch
// foi chamado), senão cai para método embutido, hoisting tanto o receptor
// quanto os argumentos em ambos os casos.
func (sl *StmtLowerer) exprStmtCall(call *ast.CallExpr, ctx StmtContext) ([]string, error) {
	mem, ok := call.Fn.(*ast.MemberExpr)
	if !ok {
		return nil, fmt.Errorf("codegen: ExprStmt de CallExpr com Fn %T não suportado nesta task (só método embutido sobre um MemberExpr, ex. state.entries.add(...), ou dispatch de Handle de Aggregate, ex. wallet.Deposit(...))", call.Fn)
	}

	if lines, handled, err := sl.handleDispatchCall(mem, call, ctx); handled {
		return lines, err
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

// logStmt traduz "log Level Message { Fields }" para uma chamada de log/slog
// (REQ-22.1/30.1, §design 3.13): slog.<Level>(<Message>, "trace_id", <id>,
// "campo1", valor1, ...) — o campo "trace_id" só aparece quando o corpo
// sendo lowerizado TEM um ctx em escopo (sl.ctx.CtxVar != "", ver a doc de
// StmtContext.CtxVar): UseCase/Policy/Query/Worker/Saga step sempre o têm;
// Handle/Apply e Upcast/Downcast de versão não, hoje (limitação documentada,
// mesmo lugar) — um log dentro deles continua saindo sem "trace_id", byte a
// byte igual a antes de H2. runtime.TraceIDFrom (rtsrc/observer.go.txt)
// resolve o id de fato: o do span OTel ativo quando o adapter real está
// instalado (Telemetry declarado), senão o id stdlib simples mintado na
// borda (runtime.WithTrace) — logStmt não precisa saber qual dos dois é.
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
	if sl.ctx.CtxVar != "" {
		args = append(args, strconv.Quote("trace_id"), fmt.Sprintf("%s.TraceIDFrom(%s)", sl.runtimeAlias, sl.ctx.CtxVar))
	}
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
