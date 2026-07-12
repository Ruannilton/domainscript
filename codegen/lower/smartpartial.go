package lower

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/goname"
	"domainscript/types"
)

// smartpartial.go implementa o Smart Partial Loading do §20 (REQ-37, §design
// read-side 3.8): os métodos embutidos de coleção "distinct"/"sum"/"focus",
// reconhecidos por FORMA em hoistSubtree (stmt.go, CallExpr — ver o
// comentário lá) porque, como load/list/count, o Go que os substitui nunca
// cabe em posição de expressão pura: os três produzem STATEMENTS (um
// map+slice de vistos para distinct, um fold com possível erro para sum, um
// laço de busca para focus), nunca uma única chamada de 1 valor. Ao
// contrário de load/list/count (que viram uma ÚNICA chamada Go, "tmp, err :=
// f(...)"), os três aqui viram um BLOCO de várias linhas — a mesma técnica
// de hoistQueryPredicate/hoistOrderBy (stmt.go) de montar o texto como um
// `strings.Builder` multi-linha e devolvê-lo como UMA entrada da lista de
// "hoisted" (emit.Emitter.Line aceita uma string com '\n' embutido sem
// problema — quem reformata é go/format.Source em Emitter.Bytes, então a
// indentação aqui não importa, só a validade sintática).
//
// Diferença estrutural central em relação a hoistQueryPredicate/hoistOrderBy:
// aqueles produzem uma CLOSURE Go (Query[T].Where/Less), então precisam de um
// StmtContext SINTÉTICO (predCtx/lessCtx, sempre "return false, err") porque
// o corpo vira uma func Go separada com sua PRÓPRIA assinatura de erro. Os
// três métodos daqui são inlined DIRETO no corpo da função que os chamou — a
// falha de "sum" (Operator + fallível) sai pelo MESMO ctx.ExitOnError do
// statement ao redor (ensure/return/assign), sem closure nenhuma no meio.

// isSmartPartialMethod reporta se name é um dos três métodos embutidos de
// coleção do §20 — o filtro que hoistSubtree usa para desviar para este
// arquivo antes da recursão genérica de CallExpr.
func isSmartPartialMethod(name string) bool {
	switch name {
	case "distinct", "sum", "focus":
		return true
	default:
		return false
	}
}

// hoistSmartPartialMethod despacha para a implementação de cada método —
// chamado de hoistSubtree (stmt.go) já sabendo, por isSmartPartialMethod,
// que mem.Name é um dos três.
func (sl *StmtLowerer) hoistSmartPartialMethod(mem *ast.MemberExpr, call *ast.CallExpr, ctx StmtContext) (string, []string, error) {
	switch mem.Name {
	case "distinct":
		return sl.hoistDistinct(mem, call, ctx)
	case "sum":
		return sl.hoistSum(mem, call, ctx)
	case "focus":
		return sl.hoistFocus(mem, call, ctx)
	default:
		return "", nil, fmt.Errorf("codegen: método de coleção %q não é Smart Partial Loading (bug de geração — isSmartPartialMethod deveria ter filtrado antes)", mem.Name)
	}
}

// collectionItemType resolve o tipo do ITEM da coleção que recv (o receptor
// de distinct/sum/focus, ex. "state.tickets"/"soldTickets"/um parâmetro)
// denota — funciona uniformemente para os três receptores do §design
// read-side 3.8 (campo de state, resultado de "list" já vinculado no
// TypeEnv, parâmetro de coleção) porque todos passam por sl.inferType, que já
// cobre MemberExpr (via types.Model, o mesmo catálogo que "state.saldo"
// usa) e Ident vinculado (via TypeEnv.LookupType, o mesmo mecanismo que
// tipou "soldTickets" quando "list" o atribuiu). elementType (env.go) extrai
// o argumento de tipo de List<T>/AppendList<T>/Set<T> — a mesma função que
// TypeEnv.ChildForIter usa para a variável de "for".
func (sl *StmtLowerer) collectionItemType(recv ast.Expr) (types.Type, error) {
	recvType := sl.inferType(recv)
	itemType := elementType(recvType)
	if types.IsError(itemType) {
		return nil, fmt.Errorf("receptor de tipo %s não é uma coleção conhecida (esperava List/AppendList/Set — campo de state, resultado de \"list\", ou parâmetro de coleção, §design read-side 3.8)", recvType.String())
	}
	return itemType, nil
}

// singleLambdaArg extrai a lambda única de "distinct(...)"/"sum(...)" — os
// dois métodos que exigem exatamente 1 argumento posicional, uma
// *ast.LambdaExpr (ex. "x => x.k"). "focus" não usa isto (seu argumento é um
// id, não uma lambda).
func singleLambdaArg(method string, call *ast.CallExpr) (*ast.LambdaExpr, error) {
	if len(call.Args) != 1 {
		return nil, fmt.Errorf("codegen: %s(...): espera exatamente 1 argumento (uma lambda, ex. \"x => x.k\"), recebeu %d", method, len(call.Args))
	}
	le, ok := call.Args[0].Value.(*ast.LambdaExpr)
	if !ok {
		return nil, fmt.Errorf("codegen: %s(...): argumento precisa ser uma lambda (ex. \"x => x.k\"), recebeu %T", method, call.Args[0].Value)
	}
	return le, nil
}

// --- 1. distinct: map de vistos + slice na ordem de 1ª aparição. ---

// mapKeyablePrimitives é a linha "primitivo comparável" da tabela de
// comparabilidade de distinct (§design read-side 3.8) — DIFERENTE da tabela
// de orderBy (orderablePrimitives, stmt.go): distinct precisa de uma chave de
// MAPA Go válida, não de ordenação. "decimal" (runtime.Decimal, backed por
// big.Int — que embute um slice) e "bytes" ([]byte) NÃO são comparáveis em Go
// — usá-los como chave de map é erro de COMPILAÇÃO, não de execução; por
// isso ficam de fora aqui mesmo sendo "primitivos" (ao contrário de
// orderablePrimitives, que os INCLUI, porque lá a comparação é via
// Cmp/Before, não map key). "rate" também fica de fora: runtime.Rate não tem
// forma Go definida em rtsrc hoje (gap pré-existente, não desta task) —
// melhor um erro de geração claro do que arriscar uma forma nunca
// implementada.
var mapKeyablePrimitives = map[string]bool{
	"integer":  true,
	"string":   true,
	"boolean":  true,
	"datetime": true,
	"duration": true,
	"size":     true,
}

// isDistinctKeyComparable reporta se t pode ser chave de "distinct" (§design
// read-side 3.8): primitivo mapKeyable, VO wrapper sobre um primitivo
// mapKeyable (o Go de um wrapper é "type X Base" — comparável exatamente
// quando Base é), ou Enum (sempre comparável: "type X Base" sobre um
// primitivo que o front-end já restringe a string/integer, §23). VO
// composto (Base == nil) nunca é — sem forma nativa de igualdade/hash
// estrutural neste escopo (design.md, tabela de alternativas rejeitadas).
func isDistinctKeyComparable(t types.Type) bool {
	switch x := t.(type) {
	case *types.Primitive:
		return mapKeyablePrimitives[x.Name]
	case *types.VOType:
		if x.Base == nil {
			return false
		}
		base, ok := primitiveNameOf(x.Base)
		return ok && mapKeyablePrimitives[base]
	case *types.EnumType:
		return true
	default:
		return false
	}
}

// hoistDistinct traduz "<col>.distinct(x => x.k)" (§design read-side 3.8):
//
//	tmpSeen := map[K]struct{}{}
//	tmpResult := make([]K, 0, len(<col>))
//	for _, x := range <col> {
//	    [linhas hoisted do corpo de x.k, se precisar]
//	    if _, ok := tmpSeen[<k>]; !ok {
//	        tmpSeen[<k>] = struct{}{}
//	        tmpResult = append(tmpResult, <k>)
//	    }
//	}
//
// tmpResult (a única temporária REGISTRADA em TypeEnv — bindTmp — porque é a
// única que sobrevive ao statement) é a ordem de 1ª aparição (NFR-13,
// determinismo): a fonte de verdade é o SLICE, nunca a iteração do map
// (que o Go não garante ordenada). O parâmetro da lambda (x) é vinculado num
// escopo-filho ao tipo do item (o MESMO mecanismo de hoistQueryPredicate) e
// usado como o nome real da variável de laço — member access no corpo da
// lambda (ex. "x.orderId") resolve normalmente via Lowerer.member, sem
// código de resolução novo.
func (sl *StmtLowerer) hoistDistinct(mem *ast.MemberExpr, call *ast.CallExpr, ctx StmtContext) (string, []string, error) {
	recvGo, recvHoisted, err := sl.exprHoisted(mem.X, ctx)
	if err != nil {
		return "", nil, err
	}
	itemType, err := sl.collectionItemType(mem.X)
	if err != nil {
		return "", nil, fmt.Errorf("codegen: distinct(...): %w", err)
	}
	le, err := singleLambdaArg("distinct", call)
	if err != nil {
		return "", nil, err
	}

	childEnv := sl.env.Child()
	childEnv.Bind(le.Param, itemType)
	paramGo := goname.Ident(le.Param)
	child := sl.childForKeyEval(childEnv, le.Param, paramGo)

	keyType := child.inferType(le.Body)
	if types.IsError(keyType) {
		return "", nil, fmt.Errorf("codegen: distinct(%s => ...): não consegui inferir o tipo da chave", le.Param)
	}
	if !isDistinctKeyComparable(keyType) {
		return "", nil, fmt.Errorf("codegen: distinct(...): tipo da chave %s não é comparável — só primitivo (exceto decimal/bytes/rate), ValueObject wrapper sobre um primitivo comparável, ou Enum podem ser chave de distinct (§design read-side 3.8, NFR-20)", keyType.String())
	}
	keyGoType, err := goTypeString(keyType)
	if err != nil {
		return "", nil, fmt.Errorf("codegen: distinct(...): tipo da chave: %w", err)
	}

	keyGo, keyHoisted, err := child.exprHoisted(le.Body, ctx)
	if err != nil {
		return "", nil, err
	}

	seenVar := sl.newTmp()
	resultVar := sl.newTmp()
	sl.bindTmp(resultVar, &types.Generic{Ctor: "List", Args: []types.Type{keyType}})

	var body strings.Builder
	fmt.Fprintf(&body, "%s := map[%s]struct{}{}\n", seenVar, keyGoType)
	fmt.Fprintf(&body, "%s := make([]%s, 0, len(%s))\n", resultVar, keyGoType, recvGo)
	fmt.Fprintf(&body, "for _, %s := range %s {\n", paramGo, recvGo)
	for _, l := range keyHoisted {
		fmt.Fprintf(&body, "%s\n", l)
	}
	fmt.Fprintf(&body, "if _, ok := %s[%s]; !ok {\n", seenVar, keyGo)
	fmt.Fprintf(&body, "%s[%s] = struct{}{}\n", seenVar, keyGo)
	fmt.Fprintf(&body, "%s = append(%s, %s)\n", resultVar, resultVar, keyGo)
	fmt.Fprintf(&body, "}\n")
	fmt.Fprintf(&body, "}")

	lines := append(recvHoisted, body.String())
	return resultVar, lines, nil
}

// --- 2. sum: fold a partir do PRIMEIRO item (nunca de um zero value sintético). ---

// buildSumAccumulate devolve as linhas Go que acumulam valGo (o valor do
// item CORRENTE do laço) em accGo (a variável acumuladora, já inicializada
// com o valor do primeiro item — ver hoistSum) — a decisão de COMO somar
// segue a tabela do §design read-side 3.8:
//
//   - "integer" → "+" nativo (int64 aceita operador nativamente).
//   - "decimal" → runtime.Decimal NÃO é numérico nativo do Go (struct sobre
//     big.Int, rtsrc/decimal.go.txt) — "+" não compilaria; soma via o método
//     Decimal.Add, que é INFALÍVEL (decimal.go.txt: "func (d Decimal)
//     Add(other Decimal) Decimal", sem error — ao contrário do Operator "+"
//     de um VO composto, E3.2, que SEMPRE devolve (T, error)).
//   - qualquer outro primitivo (boolean/string/bytes/datetime/rate/...) → sem
//     soma com sentido — erro de geração.
//   - VOType composto com Operator "+" declarado (goname.VOOperatorRegistry,
//     o MESMO registry que orderBy usa para "<"/">") → dispatch via o método
//     do operador (goname.OperatorMethod("+") == "Add") — SEMPRE falível
//     (E3.2), erro propagado pelo ctx.ExitOnError do statement ao redor
//     (nunca uma closure sintética — ver a doc do arquivo).
//   - VOType wrapper (Base != nil) ou composto SEM "+" → erro de geração
//     claro, nomeando o tipo (NFR-20) — design read-side 3.8 só enumera
//     "primitivo" e "VO com Operator +"; um wrapper nunca declara Operator, e
//     por isso nunca soma.
func (sl *StmtLowerer) buildSumAccumulate(valType types.Type, accGo, valGo string, ctx StmtContext) ([]string, error) {
	switch t := valType.(type) {
	case *types.Primitive:
		switch t.Name {
		case "integer":
			return []string{fmt.Sprintf("%s = %s + %s", accGo, accGo, valGo)}, nil
		case "decimal":
			return []string{fmt.Sprintf("%s = %s.Add(%s)", accGo, accGo, valGo)}, nil
		default:
			return nil, fmt.Errorf("codegen: sum(...): tipo primitivo %q não suporta soma — só integer/decimal (§design read-side 3.8, NFR-20)", t.Name)
		}

	case *types.VOType:
		if sl.reg.HasOperator(t.Name, "+") {
			method, ok := goname.OperatorMethod("+")
			if !ok {
				return nil, fmt.Errorf("codegen: sum(...): símbolo de operador \"+\" não reconhecido (bug de geração)")
			}
			return []string{
				"var err error",
				fmt.Sprintf("%s, err = %s.%s(%s)", accGo, accGo, method, valGo),
				fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
			}, nil
		}
		return nil, fmt.Errorf("codegen: sum(...): ValueObject %q não declara Operator + — sem forma de somar (§design read-side 3.8, NFR-20)", t.Name)

	default:
		return nil, fmt.Errorf("codegen: sum(...): tipo %s não suporta soma — só numérico primitivo (integer/decimal) ou ValueObject composto com Operator + declarado (§design read-side 3.8, NFR-20)", valType.String())
	}
}

// hoistSum traduz "<col>.sum(x => x.v)" (§design read-side 3.8):
//
//	var tmpAcc <VGoType>
//	if len(<col>) > 0 {
//	    tmpFirst := <col>[0]
//	    [linhas hoisted do corpo de x.v sobre tmpFirst]
//	    tmpAcc = <valor de tmpFirst.v>
//	    for _, x := range <col>[1:] {
//	        [linhas hoisted do corpo de x.v]
//	        [acumulação — ver buildSumAccumulate]
//	    }
//	}
//
// O fold começa do PRIMEIRO item (design read-side 3.8 é explícito: NÃO de
// um zero value sintético) — daí a estrutura em duas partes (o primeiro item
// fora do laço, o resto dentro): "x => x.v" é lowerizado DUAS vezes (mesma
// técnica de hoistOrderBy para keyA/keyB), uma com o parâmetro sobrepondo
// para "tmpFirst" (childFirst) e outra para o nome real da variável de laço
// (childLoop) — cada evocação com sua PRÓPRIA cópia do mapa goNames (ver a
// doc de childForKeyEval sobre por que a cópia é obrigatória).
//
// Coleção vazia (o "if len(...) > 0" nunca entra) → tmpAcc fica no ZERO
// VALUE Go de V (design read-side 3.8, decisão deliberada: o uso canônico de
// "sum" é comparação em "ensure", onde o zero value se comporta como
// esperado — ex. "ensure state.items.sum(i => i.price) < limit" numa
// coleção vazia soma 0, uma comparação sensata; um erro de runtime aqui
// criaria um erro de infra onde o domínio esperava um booleano).
func (sl *StmtLowerer) hoistSum(mem *ast.MemberExpr, call *ast.CallExpr, ctx StmtContext) (string, []string, error) {
	recvGo, recvHoisted, err := sl.exprHoisted(mem.X, ctx)
	if err != nil {
		return "", nil, err
	}
	itemType, err := sl.collectionItemType(mem.X)
	if err != nil {
		return "", nil, fmt.Errorf("codegen: sum(...): %w", err)
	}
	le, err := singleLambdaArg("sum", call)
	if err != nil {
		return "", nil, err
	}

	childEnv := sl.env.Child()
	childEnv.Bind(le.Param, itemType)
	paramGo := goname.Ident(le.Param)

	childLoop := sl.childForKeyEval(childEnv, le.Param, paramGo)
	valType := childLoop.inferType(le.Body)
	if types.IsError(valType) {
		return "", nil, fmt.Errorf("codegen: sum(%s => ...): não consegui inferir o tipo do valor somado", le.Param)
	}
	valGoType, err := goTypeString(valType)
	if err != nil {
		return "", nil, fmt.Errorf("codegen: sum(...): tipo do valor: %w", err)
	}
	loopValGo, loopValHoisted, err := childLoop.exprHoisted(le.Body, ctx)
	if err != nil {
		return "", nil, err
	}

	resultVar := sl.newTmp()
	sl.bindTmp(resultVar, valType)

	firstVar := sl.newTmp()
	childFirst := sl.childForKeyEval(childEnv, le.Param, firstVar)
	firstValGo, firstValHoisted, err := childFirst.exprHoisted(le.Body, ctx)
	if err != nil {
		return "", nil, err
	}

	accumLines, err := sl.buildSumAccumulate(valType, resultVar, loopValGo, ctx)
	if err != nil {
		return "", nil, err
	}

	var body strings.Builder
	fmt.Fprintf(&body, "var %s %s\n", resultVar, valGoType)
	fmt.Fprintf(&body, "if len(%s) > 0 {\n", recvGo)
	fmt.Fprintf(&body, "%s := %s[0]\n", firstVar, recvGo)
	for _, l := range firstValHoisted {
		fmt.Fprintf(&body, "%s\n", l)
	}
	fmt.Fprintf(&body, "%s = %s\n", resultVar, firstValGo)
	fmt.Fprintf(&body, "for _, %s := range %s[1:] {\n", paramGo, recvGo)
	for _, l := range loopValHoisted {
		fmt.Fprintf(&body, "%s\n", l)
	}
	for _, l := range accumLines {
		fmt.Fprintf(&body, "%s\n", l)
	}
	fmt.Fprintf(&body, "}\n")
	fmt.Fprintf(&body, "}")

	lines := append(recvHoisted, body.String())
	return resultVar, lines, nil
}

// --- 3. focus: busca linear pelo campo "id". ---

// itemHasIDField reporta se itemType (o tipo do item de uma coleção) declara
// um campo literalmente chamado "id" — a convenção FIXA do §20 (não
// configurável: não há sintaxe no spec para declarar outra chave). Reusa
// types.Model.Members, o MESMO catálogo que REQ-12 (member-access) já usa —
// devolve nil para um tipo sem campos (primitivo, VO wrapper, Enum), então
// "id" nunca é encontrado ali (correto: nenhum desses tem campo algum).
func itemHasIDField(model *types.Model, itemType types.Type) bool {
	_, ok := model.Members(itemType)["id"]
	return ok
}

// hoistFocus traduz "<col>.focus(<id>)" (§design read-side 3.8):
//
//	var tmpFocus *<ItemGoType>
//	for i := range <col> {
//	    if <col>[i].Id == <id> {
//	        tmpFocus = &<col>[i]
//	        break
//	    }
//	}
//
// Devolve um PONTEIRO (*ItemGoType), não o valor — a MESMA convenção que
// "load T(id)" já usa (hoistLoad, acima): TypeEnv registra o tipo VALOR do
// item (itemType), mas o Go de verdade é um ponteiro, nil quando não
// encontrado. Essa escolha é o que permite a composição com "ensure ...
// exists" que design read-side 3.8 pede: "exists" (QueryExpr pós-fixo
// qualquer, builtins.go/existsExpr) traduz "<X> exists" para "<X> != nil"
// sobre QUALQUER X já hoisted — "state.tickets.focus(id) exists" funciona
// SEM nenhum código novo em existsExpr, porque hoistQueryExpr (stmt.go) já
// hoisteia o Target de "exists" através de hoistSubtree ANTES de chamar
// existsExpr, e hoistSubtree é exatamente quem despacha para esta função.
// Um acesso a campo subsequente (ex. "ticket.status") funciona igual —
// Go desreferencia um ponteiro em acesso de campo automaticamente, o mesmo
// motivo por que "wallet.state.saldo" já funciona sobre o ponteiro que
// Load<T> devolve.
func (sl *StmtLowerer) hoistFocus(mem *ast.MemberExpr, call *ast.CallExpr, ctx StmtContext) (string, []string, error) {
	if len(call.Args) != 1 {
		return "", nil, fmt.Errorf("codegen: focus(...): espera exatamente 1 argumento (o id a buscar), recebeu %d", len(call.Args))
	}

	recvGo, recvHoisted, err := sl.exprHoisted(mem.X, ctx)
	if err != nil {
		return "", nil, err
	}
	itemType, err := sl.collectionItemType(mem.X)
	if err != nil {
		return "", nil, fmt.Errorf("codegen: focus(...): %w", err)
	}
	itemGoType, err := goTypeString(itemType)
	if err != nil {
		return "", nil, fmt.Errorf("codegen: focus(...): tipo do item: %w", err)
	}
	if !itemHasIDField(sl.env.model, itemType) {
		return "", nil, fmt.Errorf("codegen: focus(...): o item de tipo %s não declara um campo \"id\" — focus busca pelo campo literalmente chamado \"id\" (a convenção fixa do §20); sem esse campo, não há forma de buscar (§design read-side 3.8, NFR-20)", itemGoType)
	}

	idGo, idHoisted, err := sl.exprHoisted(call.Args[0].Value, ctx)
	if err != nil {
		return "", nil, err
	}

	tmp := sl.newTmp()
	sl.bindTmp(tmp, itemType)

	var body strings.Builder
	fmt.Fprintf(&body, "var %s *%s\n", tmp, itemGoType)
	fmt.Fprintf(&body, "for i := range %s {\n", recvGo)
	fmt.Fprintf(&body, "if %s[i].%s == %s {\n", recvGo, goname.ExportField("id"), idGo)
	fmt.Fprintf(&body, "%s = &%s[i]\n", tmp, recvGo)
	fmt.Fprintf(&body, "break\n")
	fmt.Fprintf(&body, "}\n")
	fmt.Fprintf(&body, "}")

	lines := append(append(append([]string{}, recvHoisted...), idHoisted...), body.String())
	return tmp, lines, nil
}
