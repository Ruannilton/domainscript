package lower

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/codegen/goname"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// join.go traduz "list A a join B b on <cond-igualdade> [where C] [as V]"
// (I5.1, REQ-35.1/35.2, §design read-side 3.7) — a correlação IN-MEMORY de
// duas fontes do MESMO banco (o front-end, REQ-5.11/sema/rules_crossfile.go:
// checkCrossDatabaseJoin, já barra join cross-database; hoistJoin sempre
// assume mesmo-banco). Dispatchada por hoistQueryExpr (stmt.go) quando
// n.Op == "list" e n.Clauses tem uma cláusula "join".
//
// Fora do escopo desta task (documentado, não um bug): orderBy/skip/take
// sobre um join (§design read-side 3.7 ponto 4 diz que aplicariam
// SelectSlice sobre o RESULTADO PROJETADO) — nenhuma fixture real exercita
// essa combinação ainda (a âncora do ciclo, GetMyTickets, não tem nenhuma
// das três), e a semântica de a que tipo/binding a chave de ordenação se
// referiria pós-projeção não está fechada no design — ensureJoinClausesWellFormed
// recusa as três com um erro de geração claro em vez de adivinhar.

// hoistJoin materializa as duas fontes, abre um loop aninhado com os dois
// aliases tipados em escopos-filho, aplica "on" (igualdade membro-a-membro,
// validateJoinOnEquality) como o if de correlação e "where" (se houver) como
// um segundo if — hoisting NORMAL, sem assinatura de closure (o código já
// está dentro do loop, então um "if err != nil { <ExitOnError> }" hoisted
// sai DIRETO da função que envolve o "return list ... join ...", igual a
// qualquer outro hoisting deste arquivo). Sem "as", cada par casado é
// acumulado como o item do alias BASE (a); com "as V", cada par é projetado
// campo a campo contra os DOIS aliases (joinProjectFieldAssignments).
func (sl *StmtLowerer) hoistJoin(n *ast.QueryExpr, ctx StmtContext) (ast.Expr, []string, error) {
	if sl.builtins == nil {
		return nil, nil, fmt.Errorf("codegen: list ... join ...: BuiltinLowerer não configurado — anexe um via Lowerer.WithBuiltins (E5.3)")
	}
	if err := ensureJoinClausesWellFormed(n.Clauses); err != nil {
		return nil, nil, err
	}

	aliasA := n.Binding
	if aliasA == "" {
		return nil, nil, fmt.Errorf("codegen: list ... join ...: a fonte base precisa de um alias (ex. \"list Ticket t join Order o on ...\") — join sempre referencia os dois lados por nome (§design read-side 3.7, REQ-35.2)")
	}
	aTypeName := astutil.HeadName(n.Target)
	aType := sl.env.TypeOfName(aTypeName)
	if types.IsError(aType) {
		return nil, nil, fmt.Errorf("codegen: list %s %s join ...: não consegui resolver o tipo de %q", aTypeName, aliasA, aTypeName)
	}

	joinClause, _ := queryClauseFull(n.Clauses, "join")
	aliasB := joinClause.Extra
	if aliasB == "" {
		return nil, nil, fmt.Errorf("codegen: ... join %s ...: a fonte juntada precisa de um alias (ex. \"join Order o\") (§design read-side 3.7, REQ-35.2)", astutil.HeadName(joinClause.Expr))
	}
	bTypeName := astutil.HeadName(joinClause.Expr)
	bType := sl.env.TypeOfName(bTypeName)
	if types.IsError(bType) {
		return nil, nil, fmt.Errorf("codegen: ... join %s %s ...: não consegui resolver o tipo de %q", bTypeName, aliasB, bTypeName)
	}

	onExpr, hasOn := queryClauseByKw(n.Clauses, "on")
	if !hasOn {
		return nil, nil, fmt.Errorf("codegen: list %s %s join %s %s ...: join exige uma cláusula \"on\" (§design read-side 3.7, REQ-35.2)", aTypeName, aliasA, bTypeName, aliasB)
	}
	if err := validateJoinOnEquality(onExpr, aliasA, aliasB); err != nil {
		return nil, nil, err
	}

	aItemGoType, err := goTypeString(aType)
	if err != nil {
		return nil, nil, fmt.Errorf("codegen: list %s %s join ...: tipo do item: %w", aTypeName, aliasA, err)
	}
	bItemGoType, err := goTypeString(bType)
	if err != nil {
		return nil, nil, fmt.Errorf("codegen: ... join %s %s ...: tipo do item: %w", bTypeName, aliasB, err)
	}

	// --- Passo 1: materializa as duas fontes (Select com Query[T]{} vazia —
	// filtros vão no "where", nunca no "on", §design read-side 3.7). ---
	aTmp := sl.newTmp()
	sl.bindTmp(aTmp, &types.Generic{Ctor: "List", Args: []types.Type{aType}})
	bTmp := sl.newTmp()
	sl.bindTmp(bTmp, &types.Generic{Ctor: "List", Args: []types.Type{bType}})
	lines := []string{
		fmt.Sprintf("%s, err := %s", aTmp, sl.builtins.ListCall(aTypeName, aItemGoType, queryLiteralFields{})),
		fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
		fmt.Sprintf("%s, err := %s", bTmp, sl.builtins.ListCall(bTypeName, bItemGoType, queryLiteralFields{})),
		fmt.Sprintf("if err != nil { %s }", ctx.ExitOnError("err")),
	}

	// --- Passo 2: escopo-filho com os dois aliases tipados (o mesmo padrão
	// de hoistQueryPredicate, agora com DOIS bindings de uma vez). ---
	childEnv := sl.env.Child()
	childEnv.Bind(aliasA, aType)
	childEnv.Bind(aliasB, bType)
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

	onGo, err := child.Expr(onExpr)
	if err != nil {
		return nil, nil, fmt.Errorf("codegen: on ...: %w", err)
	}

	// --- Passo 3: "where", se houver — hoisting NORMAL, inline no corpo do
	// loop (sem assinatura de closure para limitar, §design read-side 3.7). ---
	var whereHoisted []string
	var whereCondGo string
	hasWhere := false
	if whereExpr, ok := queryClauseByKw(n.Clauses, "where"); ok {
		hasWhere = true
		whereCondGo, whereHoisted, err = child.exprHoisted(whereExpr, ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("codegen: where ...: %w", err)
		}
	}

	// --- Passo 4: projeção — sem "as", a lista do PRIMEIRO alias; com
	// "as V", cada par projetado campo a campo contra os DOIS aliases. ---
	viewName, hasAs := listAsClause(n.Clauses)
	resultElemGoType := aItemGoType
	resultType := aType
	var viewFields []types.Field
	if hasAs {
		viewType := sl.env.TypeOfName(viewName)
		viewShape, ok := viewType.(*types.ShapeType)
		if !ok || viewShape.Kind != symbols.KindView {
			return nil, nil, fmt.Errorf("codegen: ... as %s: não resolve a uma View (got %T)", viewName, viewType)
		}
		viewFields = viewShape.Fields
		resultElemGoType, err = goTypeString(viewType)
		if err != nil {
			return nil, nil, fmt.Errorf("codegen: ... as %s: %w", viewName, err)
		}
		resultType = viewType
	}

	aliasAGo := goname.Ident(aliasA)
	aliasBGo := goname.Ident(aliasB)

	resultTmp := sl.newTmp()
	sl.bindTmp(resultTmp, &types.Generic{Ctor: "List", Args: []types.Type{resultType}})

	lines = append(lines,
		fmt.Sprintf("%s := make([]%s, 0)", resultTmp, resultElemGoType),
		fmt.Sprintf("for _, %s := range %s {", aliasAGo, aTmp),
		fmt.Sprintf("for _, %s := range %s {", aliasBGo, bTmp),
		fmt.Sprintf("if %s {", onGo),
	)
	if hasWhere {
		lines = append(lines, whereHoisted...)
		lines = append(lines, fmt.Sprintf("if %s {", whereCondGo))
	}
	if hasAs {
		aFields, err := fieldsOfType(aType)
		if err != nil {
			return nil, nil, fmt.Errorf("codegen: ... as %s: alias %s: %w", viewName, aliasA, err)
		}
		bFields, err := fieldsOfType(bType)
		if err != nil {
			return nil, nil, fmt.Errorf("codegen: ... as %s: alias %s: %w", viewName, aliasB, err)
		}
		assigns, err := joinProjectFieldAssignments(aliasAGo, aFields, aliasBGo, bFields, viewFields)
		if err != nil {
			return nil, nil, fmt.Errorf("codegen: ... as %s: %w", viewName, err)
		}
		lines = append(lines, fmt.Sprintf("%s = append(%s, %s{%s})", resultTmp, resultTmp, resultElemGoType, strings.Join(assigns, ", ")))
	} else {
		lines = append(lines, fmt.Sprintf("%s = append(%s, %s)", resultTmp, resultTmp, aliasAGo))
	}
	if hasWhere {
		lines = append(lines, "}")
	}
	lines = append(lines, "}", "}", "}")

	return ast.NewIdent(resultTmp, n.Span()), lines, nil
}

// ensureJoinClausesWellFormed valida o conjunto de cláusulas de um "list A a
// join B b [...]" ANTES de qualquer lowering: exatamente 1 "join", exatamente
// 1 "on", no máximo 1 "where"/"as" — e recusa orderBy/skip/take (fora do
// escopo desta task, ver a doc do arquivo) com um erro de geração claro.
func ensureJoinClausesWellFormed(clauses []ast.QueryClause) error {
	var joinCount, onCount, whereCount, asCount int
	for _, c := range clauses {
		switch c.Kw {
		case "join":
			joinCount++
		case "on":
			onCount++
		case "where":
			whereCount++
		case "as":
			asCount++
		case "orderBy", "skip", "take":
			return fmt.Errorf("codegen: list ... join ...: cláusula %q sobre um join ainda não é suportada por este gerador — fora do escopo de I5.1 (§design read-side 3.7 ponto 4 fica para quando um exemplo real precisar); use where/on/as", c.Kw)
		}
	}
	if joinCount != 1 {
		return fmt.Errorf("codegen: list ... join ...: esperava exatamente 1 cláusula \"join\" (achei %d) — join múltiplo é fora do escopo desta fase (§design read-side 3.7)", joinCount)
	}
	if onCount != 1 {
		return fmt.Errorf("codegen: list ... join ...: join exige exatamente 1 cláusula \"on\" (achei %d)", onCount)
	}
	if whereCount > 1 {
		return fmt.Errorf("codegen: list ... join ...: cláusula \"where\" duplicada (§design read-side 3.1, NFR-20)")
	}
	if asCount > 1 {
		return fmt.Errorf("codegen: list ... join ...: cláusula \"as\" duplicada")
	}
	return nil
}

// validateJoinOnEquality garante que onExpr é "<alias>.<campo> ==
// <outroAlias>.<campo>" (em qualquer ordem, um de cada alias) — a ÚNICA
// forma aceita para "on" (REQ-35.2, §design read-side 3.7): igualdade
// membro-a-membro entre os DOIS aliases do join. Qualquer outra forma
// (expressão computada, comparação entre membros do MESMO alias, operador
// diferente de "==") é um erro de geração claro (NFR-20) — nunca uma
// tentativa de adivinhar.
func validateJoinOnEquality(onExpr ast.Expr, aliasA, aliasB string) error {
	bin, ok := onExpr.(*ast.BinaryExpr)
	if !ok || bin.Op != token.EQ {
		return fmt.Errorf("codegen: on ...: join exige uma igualdade (\"%s.<campo> == %s.<campo>\"), não %T (§design read-side 3.7, REQ-35.2)", aliasA, aliasB, onExpr)
	}
	leftAlias, leftOk := memberAliasOf(bin.Left)
	rightAlias, rightOk := memberAliasOf(bin.Right)
	if !leftOk || !rightOk {
		return fmt.Errorf("codegen: on ...: join exige acesso de membro NU dos dois lados (\"%s.<campo> == %s.<campo>\") (§design read-side 3.7, REQ-35.2)", aliasA, aliasB)
	}
	sameAliasPair := (leftAlias == aliasA && rightAlias == aliasB) || (leftAlias == aliasB && rightAlias == aliasA)
	if !sameAliasPair {
		return fmt.Errorf("codegen: on %s.%s == %s.%s: precisa referenciar EXATAMENTE os dois aliases do join (%s e %s), um de cada lado (§design read-side 3.7, REQ-35.2)",
			leftAlias, bin.Left.(*ast.MemberExpr).Name, rightAlias, bin.Right.(*ast.MemberExpr).Name, aliasA, aliasB)
	}
	return nil
}

// memberAliasOf devolve o nome do Ident base de e quando e é um MemberExpr
// NU sobre um Ident (ex. "t.orderId" → "t") — ok=false para qualquer outra
// forma (acesso aninhado, expressão computada).
func memberAliasOf(e ast.Expr) (string, bool) {
	mem, ok := e.(*ast.MemberExpr)
	if !ok {
		return "", false
	}
	id, ok := mem.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return id.Name, true
}

// fieldsOfType devolve os Fields (nome + tipo, ORDEM declarada) de t —
// *types.VOType (composto: Fields != nil quando Base == nil) ou
// *types.ShapeType (Aggregate/View/Event/...) — o mesmo par de casos que
// codegen/decl_query.go:shapeFieldsOf cobre para a projeção de UMA fonte só;
// duplicado aqui (não importado) porque a direção de dependência do
// projeto é codegen → codegen/lower, nunca o inverso (CLAUDE.md).
func fieldsOfType(t types.Type) ([]types.Field, error) {
	switch x := t.(type) {
	case *types.VOType:
		return x.Fields, nil
	case *types.ShapeType:
		return x.Fields, nil
	default:
		return nil, fmt.Errorf("tipo %s não tem campos nomeados (esperava ValueObject composto ou um shape com campos)", t.String())
	}
}

// joinProjectFieldAssignments monta as atribuições "Campo: <expressão Go>"
// de um literal de View V a partir de DOIS conjuntos de campos de origem —
// os dois aliases de um join, na ORDEM DE DECLARAÇÃO exigida pelo design
// (aGo/aFields primeiro, bGo/bFields depois, §design read-side 3.7 ponto 3):
// cada campo da View casa por nome exato ou por achatamento de UM nível de
// VO composto (matchJoinField) contra CADA fonte; casar em AMBAS é
// ambiguidade (erro, NFR-20); não casar em nenhuma também é erro, nomeando o
// campo.
func joinProjectFieldAssignments(aGo string, aFields []types.Field, bGo string, bFields []types.Field, viewFields []types.Field) ([]string, error) {
	assigns := make([]string, len(viewFields))
	for i, vf := range viewFields {
		aExpr, aOk := matchJoinField(aGo, aFields, vf.Name)
		bExpr, bOk := matchJoinField(bGo, bFields, vf.Name)
		switch {
		case aOk && bOk:
			return nil, fmt.Errorf("campo %q da View é ambíguo: casa tanto em %s quanto em %s — nenhum dos dois aliases pode ser preferido silenciosamente (§design read-side 3.7, NFR-20)", vf.Name, aGo, bGo)
		case aOk:
			assigns[i] = fmt.Sprintf("%s: %s", goname.ExportField(vf.Name), aExpr)
		case bOk:
			assigns[i] = fmt.Sprintf("%s: %s", goname.ExportField(vf.Name), bExpr)
		default:
			return nil, fmt.Errorf("campo %q da View não existe em nenhum dos dois aliases do join (nem direto, nem achatado de um ValueObject composto)", vf.Name)
		}
	}
	return assigns, nil
}

// matchJoinField tenta casar viewFieldName contra sourceFields — exato por
// nome primeiro, senão achatamento de UM nível de VO composto (a mesma regra
// de codegen/decl_query.go:flattenedFieldExpr, reimplementada aqui pela
// mesma razão de fieldsOfType: lower não importa codegen).
func matchJoinField(sourceGo string, sourceFields []types.Field, viewFieldName string) (string, bool) {
	for _, f := range sourceFields {
		if f.Name == viewFieldName {
			return fmt.Sprintf("%s.%s", sourceGo, goname.ExportField(viewFieldName)), true
		}
	}
	for _, sf := range sourceFields {
		prefix := sf.Name + "_"
		if !strings.HasPrefix(viewFieldName, prefix) {
			continue
		}
		vo, ok := sf.Type.(*types.VOType)
		if !ok || vo.Base != nil {
			continue // wrapper (Base != nil) não tem sub-campos nomeados
		}
		subName := strings.TrimPrefix(viewFieldName, prefix)
		for _, subF := range vo.Fields {
			if subF.Name == subName {
				return fmt.Sprintf("%s.%s.%s", sourceGo, goname.ExportField(sf.Name), goname.ExportField(subName)), true
			}
		}
	}
	return "", false
}
