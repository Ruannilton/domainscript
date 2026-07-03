package resolver

import (
	"domainscript/ast"
	"domainscript/diag"
)

// resolve_body.go implementa a resolução de nomes em corpos executáveis (REQ-9,
// §design type-checking 3.3): para cada corpo de cada declaração, monta um Scope
// raiz com os receptores contextuais (§3.2) e os parâmetros do construto, percorre
// os statements abrindo escopos-filho nos binders (for, lambda, match, query) e
// reporta cada identificador solto que não resolve em escopo algum (REQ-9.4).
//
// Distingue uso de definição: um Ident em posição de valor é resolvido; um nome de
// campo de construção (Arg.Name), um alvo de atribuição nu (introduz um local), um
// nome de membro (X.nome — checado na Fase D) e um padrão de match são tratados à
// parte (§3.3). Subárvores de erro são puladas, preservando REQ-4.5 (REQ-9.6).

// builtinValues são identificadores nus aceitos em posição de valor sem
// declaração: o no-op `Nop` (ação sem efeito, §3.1), o wildcard `_` e o
// sentinela `unrecoverable` de um passo de Saga (`down { unrecoverable }`,
// §18.2/REQ-24.2 — marca compensação impossível; o parser aceita o bloco
// como um Block de 1 statement comum, um Ident nu, ver
// parser/parse_saga_test.go; codegen/decl_saga.go reconhece essa forma
// exata, isUnrecoverableDown). Booleanos (true/false) e controles de laço
// (break/continue) são nós próprios, não Idents, e não passam por aqui.
//
// Como resolveIdent (abaixo) não carrega o construto sendo resolvido,
// `unrecoverable` é aceito em QUALQUER posição de valor, não só dentro de
// `down` — o mesmo raciocínio já vale para `Nop` hoje (só é significativo
// dentro de um `for`, mas o front-end aceita a resolução do NOME em
// qualquer lugar; a validade posicional de `Nop` é responsabilidade de uma
// regra semântica à parte, sema/rules_flow.go, não da resolução de nomes).
var builtinValues = map[string]bool{
	"Nop":           true,
	"_":             true,
	"unrecoverable": true,
}

// resolveBodies executa a passagem de resolução de corpos sobre todas as unidades
// coletadas. Roda após a resolução de tipos/refs (ResolveAll), quando todos os
// símbolos do módulo/programa já estão na tabela.
func (r *Resolver) resolveBodies() {
	for _, u := range r.units {
		for _, d := range u.file.Decls {
			r.resolveDeclBodies(u.module, d)
		}
	}
}

// resolveDeclBodies resolve os corpos executáveis de uma declaração, cada um com o
// escopo raiz apropriado ao seu construto (§3.2).
func (r *Resolver) resolveDeclBodies(module string, d ast.Decl) {
	switch n := d.(type) {
	case *ast.ValueObjectDecl:
		// Num VO composto, os campos são acessíveis por nome nu dentro de Valid e
		// dos operadores (ex.: Money { amount decimal ... Valid { amount >= 0 } }).
		// No VO wrapper, n.Fields é vazio e `value` (receptor) cobre o caso.
		r.resolveBody(module, n.Valid, constructValid, n.Fields)
		for _, op := range n.Operators {
			r.resolveBody(module, op.Body, constructOperator, op.Params, n.Fields)
		}
	case *ast.EnumDecl:
		if n.Coerce != nil {
			r.resolveBody(module, n.Coerce.Body, constructCoerce)
		}
	case *ast.AggregateDecl:
		for _, h := range n.Handlers {
			r.resolveBody(module, h.Body, constructHandle, h.Params)
		}
		for _, a := range n.Appliers {
			r.resolveBody(module, a.Body, constructApply)
		}
		for _, rule := range n.Access {
			// As condições do bloco access são expressões, não blocos, mas enxergam
			// self/caller como um corpo de Aggregate (REQ-9.3).
			sc := NewScope()
			seedReceivers(sc, constructAccess)
			r.resolveExpr(module, sc, rule.Condition)
		}
	case *ast.UseCaseDecl:
		r.resolveBody(module, n.Execute, constructUseCaseExecute)
	case *ast.QueryDecl:
		r.resolveBody(module, n.Body, constructQuery, n.Params)
	case *ast.PolicyDecl:
		r.resolveBody(module, n.Execute, constructPolicyExecute)
	case *ast.WorkerDecl:
		r.resolveBody(module, n.Source, constructWorkerSource)
		// O execute de Worker tem um único parâmetro nomeado (o item da fonte).
		if n.Execute != nil {
			execScope := NewScope()
			if n.ExecuteParam != "" {
				execScope.Define(n.ExecuteParam, Binding{Name: n.ExecuteParam, Kind: BindParam})
			}
			r.resolveBlockIn(module, execScope, n.Execute)
		}
	case *ast.SagaDecl:
		for _, s := range n.Steps {
			r.resolveBody(module, s.Up, constructSagaStep)
			r.resolveBody(module, s.Down, constructSagaStep)
			r.resolveBody(module, s.OnInfraError, constructSagaStep)
		}
	}
}

// resolveBody monta o escopo raiz (receptores do construto + os parâmetros de cada
// grupo de campos dado) e resolve o bloco. Vários grupos permitem semear, p.ex.,
// os parâmetros de um operator e os campos do VO no mesmo escopo. Um bloco nil
// (construto sem aquele corpo) é ignorado.
func (r *Resolver) resolveBody(module string, b *ast.Block, c construct, fieldGroups ...[]*ast.Field) {
	if b == nil {
		return
	}
	sc := NewScope()
	seedReceivers(sc, c)
	for _, fields := range fieldGroups {
		for _, f := range fields {
			if f != nil && f.Name != "" {
				sc.Define(f.Name, Binding{Name: f.Name, Kind: BindParam})
			}
		}
	}
	r.resolveBlockIn(module, sc, b)
}

// resolveBlockIn resolve cada statement do bloco no escopo dado.
func (r *Resolver) resolveBlockIn(module string, sc *Scope, b *ast.Block) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		r.resolveStmt(module, sc, s)
	}
}

// resolveStmt resolve um statement, abrindo escopos-filho nos binders (for, match)
// e introduzindo locais nas atribuições a nome nu.
func (r *Resolver) resolveStmt(module string, sc *Scope, s ast.Stmt) {
	switch n := s.(type) {
	case *ast.Block:
		r.resolveBlockIn(module, sc.Child(), n)
	case *ast.ExprStmt:
		r.resolveExpr(module, sc, n.X)
	case *ast.AssignStmt:
		r.resolveExpr(module, sc, n.Value)
		// Alvo nu (wallet = ...) introduz um local; alvo composto (state.x = ...)
		// é um uso a resolver (o receptor precisa existir).
		if id, ok := n.Target.(*ast.Ident); ok {
			sc.Define(id.Name, Binding{Name: id.Name, Kind: BindLocal})
		} else {
			r.resolveExpr(module, sc, n.Target)
		}
	case *ast.EnsureStmt:
		r.resolveExpr(module, sc, n.Cond)
		r.resolveStmt(module, sc, n.Else)
	case *ast.ReturnStmt:
		r.resolveExpr(module, sc, n.Value)
	case *ast.ForStmt:
		r.resolveExpr(module, sc, n.Iter)
		child := sc.Child()
		child.Define(n.Var, Binding{Name: n.Var, Kind: BindLocal})
		r.resolveBlockIn(module, child, n.Body)
	case *ast.EmitStmt:
		r.resolveExpr(module, sc, n.Call)
	case *ast.LogStmt:
		r.resolveExpr(module, sc, n.Message)
		for _, f := range n.Fields {
			r.resolveExpr(module, sc, f.Value)
		}
	case *ast.MatchStmt:
		r.resolveExpr(module, sc, n.Subject)
		for _, arm := range n.Arms {
			child := sc.Child()
			r.bindPatterns(module, child, arm.Patterns)
			r.resolveExpr(module, child, arm.Guard)
			r.resolveStmt(module, child, arm.Body)
		}
	}
	// BreakStmt, ContinueStmt, ErrorStmt: nada a resolver (REQ-9.6).
}

// resolveExpr resolve uma expressão e suas subexpressões, abrindo escopos-filho em
// lambda e query (binders de expressão).
func (r *Resolver) resolveExpr(module string, sc *Scope, e ast.Expr) {
	switch n := e.(type) {
	case nil, *ast.ErrorExpr:
		return // ausente ou subárvore de erro: pula (REQ-9.6)
	case *ast.Ident:
		r.resolveIdent(module, sc, n)
	case *ast.Literal:
		return
	case *ast.BinaryExpr:
		r.resolveExpr(module, sc, n.Left)
		r.resolveExpr(module, sc, n.Right)
	case *ast.UnaryExpr:
		r.resolveExpr(module, sc, n.X)
	case *ast.MemberExpr:
		// Só o receptor é resolvido; o nome do membro é checado contra o tipo na
		// Fase D (acesso a membro), não aqui.
		r.resolveExpr(module, sc, n.X)
	case *ast.CallExpr:
		r.resolveExpr(module, sc, n.Fn)
		for _, a := range n.Args {
			// a.Name é um nome de campo (named arg), não um ident a resolver.
			r.resolveExpr(module, sc, a.Value)
		}
	case *ast.IndexExpr:
		r.resolveExpr(module, sc, n.X)
		r.resolveExpr(module, sc, n.Index)
	case *ast.RangeExpr:
		r.resolveExpr(module, sc, n.Low)
		r.resolveExpr(module, sc, n.High)
	case *ast.LambdaExpr:
		child := sc.Child()
		child.Define(n.Param, Binding{Name: n.Param, Kind: BindParam})
		r.resolveExpr(module, child, n.Body)
	case *ast.ListExpr:
		for _, el := range n.Elems {
			r.resolveExpr(module, sc, el)
		}
	case *ast.QueryExpr:
		r.resolveExpr(module, sc, n.Target)
		child := sc.Child()
		if n.Binding != "" {
			child.Define(n.Binding, Binding{Name: n.Binding, Kind: BindLocal})
		}
		// Aliases de join também são binders das cláusulas seguintes.
		for _, cl := range n.Clauses {
			if cl.Kw == "join" && cl.Extra != "" {
				child.Define(cl.Extra, Binding{Name: cl.Extra, Kind: BindLocal})
			}
		}
		for _, cl := range n.Clauses {
			r.resolveExpr(module, child, cl.Expr)
		}
	case *ast.MatchExpr:
		r.resolveExpr(module, sc, n.Subject)
		for _, arm := range n.Arms {
			child := sc.Child()
			r.bindPatterns(module, child, arm.Patterns)
			r.resolveExpr(module, child, arm.Guard)
			r.resolveExpr(module, child, arm.Body)
		}
	}
}

// bindPatterns trata os padrões de um braço de match. Um padrão nu (Ident) é um
// binding (ou o wildcard `_`): introduz um nome, nunca um erro. Um padrão
// qualificado (Enum.Membro) tem o receptor resolvido (a cabeça do enum); o nome do
// membro fica para a Fase D. Padrões de construção (Some(x)) ligam seus argumentos
// nus. Literais não ligam nada.
func (r *Resolver) bindPatterns(module string, sc *Scope, patterns []ast.Expr) {
	for _, p := range patterns {
		switch pat := p.(type) {
		case *ast.Ident:
			if pat.Name != "_" {
				sc.Define(pat.Name, Binding{Name: pat.Name, Kind: BindLocal})
			}
		case *ast.MemberExpr:
			r.resolveExpr(module, sc, pat.X)
		case *ast.CallExpr:
			r.resolveExpr(module, sc, pat.Fn)
			for _, a := range pat.Args {
				if id, ok := a.Value.(*ast.Ident); ok {
					sc.Define(id.Name, Binding{Name: id.Name, Kind: BindLocal})
				} else {
					r.resolveExpr(module, sc, a.Value)
				}
			}
		}
	}
}

// resolveIdent resolve um identificador solto em posição de valor: escopo léxico,
// depois símbolos do módulo (incl. nível público). Não resolvido → erro localizado
// (REQ-9.4). Identificadores embutidos (Nop, _) são aceitos sem declaração.
func (r *Resolver) resolveIdent(module string, sc *Scope, id *ast.Ident) {
	name := id.Name
	if name == "" || builtinValues[name] {
		return
	}
	if _, ok := sc.Lookup(name); ok {
		return
	}
	if _, ok := r.tab.Lookup(module, name); ok {
		return
	}
	if r.isEnumMember(module, name) {
		return // membro de Enum acessível por nome (REQ-9.2)
	}
	r.bag.CodedErrorf(id.Pos(), diag.CodeNameInBody, "nome não declarado: %q", name)
}

// isEnumMember reporta se name é um membro de algum Enum declarado no módulo. Os
// membros de Enum não têm símbolo próprio na tabela, mas são acessíveis por nome
// nu em posição de valor (ex.: dentro de um coerce), por isso a resolução de nomes
// os reconhece aqui (REQ-9.2). Só é consultado no caminho de falha (raro).
func (r *Resolver) isEnumMember(module, name string) bool {
	for _, sym := range r.tab.Module(module) {
		ed, ok := sym.Decl.(*ast.EnumDecl)
		if !ok {
			continue
		}
		for _, m := range ed.Members {
			if m.Name == name {
				return true
			}
		}
	}
	return false
}
