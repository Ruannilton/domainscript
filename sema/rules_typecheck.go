package sema

import (
	"fmt"
	"sort"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/types"
)

// rules_typecheck.go é a regra de checagem de acesso a membro (REQ-12, §design
// type-checking 3.6). Para cada acesso X.nome num corpo executável, infere o tipo
// de X e valida que `nome` é um membro do seu catálogo; caso contrário, reporta um
// erro localizado no membro, sugerindo o membro mais próximo (REQ-12.3).
//
// A regra é conservadora por design: só reporta quando o tipo de X é uma shape de
// dados (Aggregate/Event/Command/View/Notification), cujos membros são estritamente
// campos. Valores de VO e Enum expõem métodos chamados por '.' (value.length(),
// s.touch()) que este estágio não modela; checá-los geraria falsos positivos, então
// são pulados. Receptores e locais sem tipo estático conhecido inferem o tipo de
// erro e também são pulados — preservando a anti-cascata (REQ-12.4). Isso cobre
// exatamente os receptores estáticos priorizados pela REQ-12.2 (self/state → campos
// do state; event/cmd → campos do Event/Command), origem do bug `self.i` do Wallet.

// checkMemberAccess roda a checagem de acesso a membro sobre todas as unidades,
// construindo um único Model (memoizado) sobre a tabela de símbolos.
func (c *Checker) checkMemberAccess() {
	m := types.NewModel(c.tab)
	for _, u := range c.units {
		for _, d := range u.File.Decls {
			c.checkDeclMembers(u.Module, m, d)
		}
	}
}

// checkDeclMembers monta o escopo de tipos apropriado a cada corpo do construto
// (espelhando os receptores de §3.2, agora com seus tipos) e checa os acessos a
// membro de cada bloco.
func (c *Checker) checkDeclMembers(module string, m *types.Model, d ast.Decl) {
	switch n := d.(type) {
	case *ast.AggregateDecl:
		selfT := c.typeOfName(module, m, n.Name) // shape do state (REQ-12.2)
		for _, h := range n.Handlers {
			sc := types.MapScope{}
			seed(sc, "self", selfT)
			seed(sc, "state", selfT)
			seedParams(sc, m, module, h.Params)
			c.checkMembersInBlock(module, m, sc, h.Body)
		}
		for _, a := range n.Appliers {
			sc := types.MapScope{}
			seed(sc, "state", selfT)
			seed(sc, "event", c.typeOfName(module, m, a.Event))
			c.checkMembersInBlock(module, m, sc, a.Body)
		}
		for _, rule := range n.Access {
			sc := types.MapScope{}
			seed(sc, "self", selfT)
			c.checkMembersInExpr(module, m, sc, rule.Condition)
		}

	case *ast.ValueObjectDecl:
		// Num VO composto, os campos são acessíveis por nome nu em Valid e nos
		// operadores; seus tipos vêm do catálogo do próprio VO.
		fields := m.Members(c.typeOfName(module, m, n.Name))
		valid := types.MapScope{}
		seedAll(valid, fields)
		c.checkMembersInBlock(module, m, valid, n.Valid)
		for _, op := range n.Operators {
			sc := types.MapScope{}
			seedAll(sc, fields)
			seedParams(sc, m, module, op.Params)
			c.checkMembersInBlock(module, m, sc, op.Body)
		}

	case *ast.QueryDecl:
		sc := types.MapScope{}
		seedParams(sc, m, module, n.Params)
		c.checkMembersInBlock(module, m, sc, n.Body)

	case *ast.UseCaseDecl:
		sc := types.MapScope{}
		seed(sc, "cmd", c.typeOfName(module, m, n.Handles))
		c.checkMembersInBlock(module, m, sc, n.Execute)

	case *ast.PolicyDecl:
		sc := types.MapScope{}
		seed(sc, "event", c.typeOfName(module, m, n.On))
		c.checkMembersInBlock(module, m, sc, n.Execute)
	}
}

// typeOfName devolve o tipo do símbolo de nome name no módulo, ou nil se não
// existir (nome ausente ou não resolvido — o erro de nome já foi reportado).
func (c *Checker) typeOfName(module string, m *types.Model, name string) types.Type {
	if name == "" {
		return nil
	}
	if sym, ok := c.tab.Lookup(module, name); ok {
		return m.TypeOf(sym)
	}
	if sym, ok := c.tab.Find(name); ok {
		return m.TypeOf(sym)
	}
	return nil
}

// seed define name no escopo com o tipo t, ignorando um tipo ausente/erro (o
// receptor cujo tipo não se conhece simplesmente não é checado).
func seed(sc types.MapScope, name string, t types.Type) {
	if t != nil && !types.IsError(t) {
		sc[name] = t
	}
}

// seedAll copia um catálogo de membros (nome→tipo) para o escopo.
func seedAll(sc types.MapScope, members map[string]types.Type) {
	for name, t := range members {
		seed(sc, name, t)
	}
}

// seedParams define cada parâmetro com o seu tipo declarado.
func seedParams(sc types.MapScope, m *types.Model, module string, params []*ast.Field) {
	for _, f := range params {
		if f != nil && f.Name != "" {
			seed(sc, f.Name, m.TypeOfRef(module, f.Type))
		}
	}
}

// checkMembersInBlock checa todo acesso a membro que aparece no bloco.
func (c *Checker) checkMembersInBlock(module string, m *types.Model, sc types.Scope, b *ast.Block) {
	astutil.ForEachExprInBlock(b, func(e ast.Expr) {
		c.checkMemberExpr(module, m, sc, e)
	})
}

// checkMembersInExpr checa todo acesso a membro que aparece numa expressão (ex.: a
// condição de uma regra de access, que é expressão e não bloco).
func (c *Checker) checkMembersInExpr(module string, m *types.Model, sc types.Scope, e ast.Expr) {
	astutil.ForEachExpr(e, func(sub ast.Expr) {
		c.checkMemberExpr(module, m, sc, sub)
	})
}

// checkMemberExpr valida um único nó: se e é um acesso a membro cujo receptor tem
// um catálogo de membros conhecido e `nome` não está nele, reporta o erro.
func (c *Checker) checkMemberExpr(module string, m *types.Model, sc types.Scope, e ast.Expr) {
	me, ok := e.(*ast.MemberExpr)
	if !ok {
		return
	}
	base := m.Infer(module, me.X, sc)
	if types.IsError(base) {
		return // anti-cascata: receptor desconhecido ou erro anterior (REQ-12.4)
	}
	// Só shapes de dados são checadas (ver doc do arquivo): VO/Enum têm métodos
	// via '.' fora do nosso modelo, e primitivos/coleções não têm catálogo.
	if _, ok := base.(*types.ShapeType); !ok {
		return
	}
	members := m.Members(base)
	if members == nil {
		return // shape sem campos: nada a checar
	}
	if _, found := members[me.Name]; found {
		return
	}
	msg := fmt.Sprintf("membro inexistente: %q em %s", me.Name, base.String())
	if sug := closestMember(me.Name, members); sug != "" {
		msg += fmt.Sprintf(" (você quis dizer %q?)", sug)
	}
	c.bag.Errorf(me.NamePos, "%s", msg)
}

// closestMember devolve o nome de membro mais próximo de name por distância de
// edição, ou "" se nenhum está próximo o bastante. Itera em ordem alfabética para
// que a sugestão seja determinística em caso de empate (NFR-9).
func closestMember(name string, members map[string]types.Type) string {
	names := make([]string, 0, len(members))
	for n := range members {
		names = append(names, n)
	}
	sort.Strings(names)

	best := ""
	bestDist := 0
	// Tolerância: até 1/3 do tamanho do nome, no mínimo 1 — perto o bastante para um
	// typo, longe o bastante para não sugerir algo arbitrário.
	limit := len(name) / 3
	if limit < 1 {
		limit = 1
	}
	for _, cand := range names {
		d := levenshtein(name, cand)
		if d <= limit && (best == "" || d < bestDist) {
			best, bestDist = cand, d
		}
	}
	return best
}

// levenshtein calcula a distância de edição entre a e b (uma linha de
// programação dinâmica).
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}
