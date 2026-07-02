package codegen

import "domainscript/ast"

// usecase_repair.go trabalha em torno de um bug de gramática PRÉ-EXISTENTE do
// parser, documentado em decl_usecase_test.go (E7.2, achado daquela task):
// "load T(id)" aceita um identificador opcional logo em seguida como
// "binding" (a mesma sintaxe de "list Ticket t where ..."). Isso significa
// que, ao parsear de verdade
//
//	wallet = load Wallet(cmd.walletId)
//	wallet.Deposit(cmd.amount, cmd.description)
//
// (o corpo real de docs/examples/wallet/application.ds), o parser consome o
// "wallet" da 2ª linha como o BINDING da QueryExpr da 1ª (a gramática não
// fecha statement por quebra de linha), e a chamada ".Deposit(...)" encadeia
// como postfix sobre a própria QueryExpr — o Execute inteiro vira UM ÚNICO
// AssignStmt:
//
//	AssignStmt{
//	  Target: Ident("wallet"),
//	  Value: CallExpr{
//	    Fn:   MemberExpr{X: QueryExpr{Op:"load", Target: Wallet(cmd.walletId), Binding:"wallet"}, Name:"Deposit"},
//	    Args: [cmd.amount, cmd.description],
//	  },
//	}
//
// em vez dos dois statements que um leitor humano do .ds esperaria (e que os
// testes de E7.2 constroem à mão para contornar o bug — ver
// handDispatchExecuteBlock em decl_usecase_test.go). Consertar isso é
// literalmente fora do escopo desta task (mudaria parser/parse_query.go, um
// componente do front-end "pronto" por CLAUDE.md, arriscando regressão na
// sintaxe legítima "list T t where ...").
//
// E9.1 é a PRIMEIRA task a rodar o gerador sobre o Program REAL do wallet
// (via program.Build/driver.CheckProject, não uma AST reconstruída à mão nos
// testes) — então é a primeira a precisar, de verdade, de uma saída para essa
// forma. repairLoadDispatchExecute desfaz a colisão de gramática: reescreve,
// só nos poucos statements de nível superior de um UseCaseDecl.Execute que
// batem exatamente com o formato acima, o par de statements que o parser
// DEVERIA ter produzido. É uma normalização estrutural em cima da AST já
// parseada — não re-parseia nem re-lexa nada (não viola §design 1.1: o
// gerador continua consumindo só o que as fases anteriores produziram); é o
// mesmo tipo de reescrita de árvore que lower/stmt.go já faz para hoisting.
// Qualquer Execute que NÃO bater no padrão exato passa inalterado — o reparo
// é cirúrgico, não uma reescrita geral de Execute.

// repairLoadDispatchExecute devolve um UseCaseDecl equivalente a decl, com
// cada statement de nível superior de Execute que bate no padrão do bug
// (ver a doc do arquivo) desmembrado em dois: o "load" isolado (Binding
// limpo) e a chamada de dispatch como ExprStmt. decl não é mutado; um novo
// UseCaseDecl é devolvido só quando ao menos um statement precisou de
// reparo (o caminho comum — nenhum reparo necessário — devolve decl como
// está, sem alocação extra).
func repairLoadDispatchExecute(decl *ast.UseCaseDecl) *ast.UseCaseDecl {
	if decl == nil || decl.Execute == nil {
		return decl
	}

	changed := false
	repaired := make([]ast.Stmt, 0, len(decl.Execute.Stmts))
	for _, s := range decl.Execute.Stmts {
		split, ok := splitLoadDispatchStmt(s)
		if !ok {
			repaired = append(repaired, s)
			continue
		}
		changed = true
		repaired = append(repaired, split...)
	}
	if !changed {
		return decl
	}

	execute := ast.NewBlock(repaired, decl.Execute.Span())
	return ast.NewUseCaseDecl(decl.Name, decl.Handles, decl.Timeout, decl.Idempotency, decl.Tenancy, execute, decl.Span())
}

// splitLoadDispatchStmt reconhece o padrão exato do bug (ver a doc do
// arquivo) num único statement: um AssignStmt cujo alvo é um Ident simples
// (ex. "wallet") e cujo Value é um CallExpr{Fn: MemberExpr{X: QueryExpr}}
// onde a QueryExpr é um "load" cujo Binding é IGUAL ao nome do alvo (a
// assinatura inequívoca de "o parser engoliu o binding da 2ª linha"). Quando
// bate, devolve os dois statements equivalentes (load isolado + dispatch) e
// ok=true; caso contrário devolve (nil, false) — o statement original deve
// ser preservado como está.
func splitLoadDispatchStmt(s ast.Stmt) ([]ast.Stmt, bool) {
	assign, ok := s.(*ast.AssignStmt)
	if !ok {
		return nil, false
	}
	target, ok := assign.Target.(*ast.Ident)
	if !ok {
		return nil, false
	}
	call, ok := assign.Value.(*ast.CallExpr)
	if !ok {
		return nil, false
	}
	mem, ok := call.Fn.(*ast.MemberExpr)
	if !ok {
		return nil, false
	}
	qe, ok := mem.X.(*ast.QueryExpr)
	if !ok || qe.Op != "load" || qe.Binding == "" || qe.Binding != target.Name {
		return nil, false
	}

	loadOnly := ast.NewQueryExpr(qe.Op, qe.Target, "", qe.Clauses, qe.Span())
	loadAssign := ast.NewAssignStmt(target, loadOnly, assign.Span())

	dispatchRecv := ast.NewIdent(target.Name, mem.X.Span())
	dispatchFn := ast.NewMemberExpr(dispatchRecv, mem.Name, mem.NamePos, mem.Span())
	dispatchCall := ast.NewCallExpr(dispatchFn, call.Args, call.Span())
	dispatchStmt := ast.NewExprStmt(dispatchCall, s.Span())

	return []ast.Stmt{loadAssign, dispatchStmt}, true
}
