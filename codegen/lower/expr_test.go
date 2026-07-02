package lower

import (
	"strings"
	"testing"

	"domainscript/ast"
	"domainscript/codegen/goname"
	"domainscript/program"
	"domainscript/token"
	"domainscript/types"
)

// expr_test.go prova os critérios de conclusão da task E5.1 (§design codegen
// 3.6/4.2) sobre o domain.ds real do wallet (docs/examples/wallet), na mesma
// convenção de env_test.go: a fixture é o programa de verdade, não uma AST
// puramente sintética, exceto onde o próprio wallet não exercita a forma
// (RangeExpr/LambdaExpr/IndexExpr — comentado em cada teste).

// findValueObject acha o *ast.ValueObjectDecl de nome name em qualquer
// arquivo do programa.
func findValueObject(t *testing.T, prog *program.Program, name string) *ast.ValueObjectDecl {
	t.Helper()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if vo, ok := d.(*ast.ValueObjectDecl); ok && vo.Name == name {
				return vo
			}
		}
	}
	t.Fatalf("ValueObject %q não encontrado no wallet — o exemplo mudou?", name)
	return nil
}

// buildFullVOOperatorRegistry registra todo ValueObject do programa —
// espelha o que o driver de geração real faria antes de lowerizar qualquer
// corpo (§design 4.2).
func buildFullVOOperatorRegistry(prog *program.Program) *goname.VOOperatorRegistry {
	reg := goname.NewVOOperatorRegistry()
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if vo, ok := d.(*ast.ValueObjectDecl); ok {
				reg.Register(vo)
			}
		}
	}
	return reg
}

// newWalletLowerer monta um Lowerer completo sobre o wallet real: env (do
// módulo Wallet) + registry de operadores de todos os VOs declarados.
func newWalletLowerer(t *testing.T) (*program.Program, *Lowerer) {
	t.Helper()
	prog, env := buildWalletEnv(t)
	reg := buildFullVOOperatorRegistry(prog)
	return prog, NewLowerer(env, reg, "runtime")
}

func member(x ast.Expr, name string) *ast.MemberExpr {
	return ast.NewMemberExpr(x, name, ast.Span{}.Start, ast.Span{})
}

func ident(name string) *ast.Ident { return ast.NewIdent(name, ast.Span{}) }

func lit(kind token.Kind, value string) *ast.Literal { return ast.NewLiteral(kind, value, ast.Span{}) }

// --- Critério de conclusão da task, literalmente: state.balance +
// event.amount, dentro de um Apply DepositPerformed, com "event"->"ev". ---

func TestExpr_BinaryVOOperatorDeclared_StateBalancePlusEventAmount(t *testing.T) {
	prog, l := newWalletLowerer(t)
	agg := findAggregate(t, prog, "Wallet")
	apply := agg.Appliers[0] // Apply DepositPerformed
	l.env.SeedApply(agg.Name, apply.Event)
	l.BindGoName("event", "ev")

	expr := ast.NewBinaryExpr(token.PLUS, member(ident("state"), "balance"), member(ident("event"), "amount"), ast.Span{})

	got, err := l.Expr(expr)
	if err != nil {
		t.Fatalf("Expr: erro inesperado: %v", err)
	}
	want := "state.Balance.Add(ev.Amount)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// --- 2. Literais ---

func TestExpr_Literals(t *testing.T) {
	_, l := newWalletLowerer(t)

	cases := []struct {
		name string
		lit  *ast.Literal
		want string
	}{
		{"int", lit(token.INT, "42"), "42"},
		{"string", lit(token.STRING, `he said "hi"`), `"he said \"hi\""`},
		{"true", lit(token.TRUE, ""), "true"},
		{"false", lit(token.FALSE, ""), "false"},
		{"duration_5s", lit(token.DURATION, "5s"), "time.Duration(5000000000)"},
		{"size_100MB", lit(token.SIZE, "100MB"), "104857600"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := l.Expr(c.lit)
			if err != nil {
				t.Fatalf("Expr(%s): erro inesperado: %v", c.name, err)
			}
			if got != c.want {
				t.Fatalf("Expr(%s): got %q, want %q", c.name, got, c.want)
			}
		})
	}
}

// --- 3. Ident: receptor sem override (self em Handle) e com override
// (event->ev). ---

func TestExpr_Ident_NoOverride_Self(t *testing.T) {
	prog, l := newWalletLowerer(t)
	agg := findAggregate(t, prog, "Wallet")
	deposit := findHandle(t, agg, "Deposit")
	l.env.SeedHandle(agg.Name, deposit.Params)

	got, err := l.Expr(ident("self"))
	if err != nil {
		t.Fatalf("Expr(self): erro inesperado: %v", err)
	}
	if got != "self" {
		t.Fatalf("got %q, want %q", got, "self")
	}
}

func TestExpr_Ident_WithOverride_EventToEv(t *testing.T) {
	prog, l := newWalletLowerer(t)
	agg := findAggregate(t, prog, "Wallet")
	apply := agg.Appliers[0]
	l.env.SeedApply(agg.Name, apply.Event)
	l.BindGoName("event", "ev")

	got, err := l.Expr(ident("event"))
	if err != nil {
		t.Fatalf("Expr(event): erro inesperado: %v", err)
	}
	if got != "ev" {
		t.Fatalf("got %q, want %q", got, "ev")
	}
}

// --- 4. MemberExpr: self.id (Aggregate, campo exportado) e
// caller.authenticated/caller.id (forma especial, vira chamada de método). ---

func TestExpr_Member_SelfId(t *testing.T) {
	prog, l := newWalletLowerer(t)
	agg := findAggregate(t, prog, "Wallet")
	l.env.SeedAccess(agg.Name)

	got, err := l.Expr(member(ident("self"), "id"))
	if err != nil {
		t.Fatalf("Expr(self.id): erro inesperado: %v", err)
	}
	if got != "self.Id" {
		t.Fatalf("got %q, want %q", got, "self.Id")
	}
}

func TestExpr_Member_CallerAuthenticatedAndId(t *testing.T) {
	_, l := newWalletLowerer(t)

	gotAuth, err := l.Expr(member(ident("caller"), "authenticated"))
	if err != nil {
		t.Fatalf("Expr(caller.authenticated): erro inesperado: %v", err)
	}
	if gotAuth != "caller.Authenticated()" {
		t.Fatalf("got %q, want %q", gotAuth, "caller.Authenticated()")
	}

	gotID, err := l.Expr(member(ident("caller"), "id"))
	if err != nil {
		t.Fatalf("Expr(caller.id): erro inesperado: %v", err)
	}
	if gotID != "caller.ID()" {
		t.Fatalf("got %q, want %q", gotID, "caller.ID()")
	}
}

// --- 5. CallExpr de construção: Event com args posicionais (real, do
// wallet) e VO com args nomeados (erro-nesta-task, prova a recusa). ---

func TestExpr_Call_EventConstruction_Positional(t *testing.T) {
	prog, l := newWalletLowerer(t)
	agg := findAggregate(t, prog, "Wallet")
	deposit := findHandle(t, agg, "Deposit")
	l.env.SeedHandle(agg.Name, deposit.Params)

	// emit DepositPerformed(self.id, amount, description) — exatamente como
	// em Handle Deposit do wallet real.
	call := ast.NewCallExpr(ident("DepositPerformed"), []ast.Arg{
		{Value: member(ident("self"), "id")},
		{Value: ident("amount")},
		{Value: ident("description")},
	}, ast.Span{})

	got, err := l.Expr(call)
	if err != nil {
		t.Fatalf("Expr: erro inesperado: %v", err)
	}
	want := "DepositPerformed{Id: self.Id, Amount: amount, Description: description}"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpr_Call_VOConstructionNamedArgs_RejectedOutsideStatement(t *testing.T) {
	_, l := newWalletLowerer(t)

	// Money(amount: 10, currency: "BRL") — construção de VO COMPOSTO em
	// posição de expressão pura: Lowerer.Expr recusa (E5.2 trata em nível de
	// statement).
	call := ast.NewCallExpr(ident("Money"), []ast.Arg{
		{Name: "amount", Value: lit(token.INT, "10")},
		{Name: "currency", Value: lit(token.STRING, "BRL")},
	}, ast.Span{})

	_, err := l.Expr(call)
	if err == nil {
		t.Fatal("esperava erro: construção de VO composto em posição de expressão pura não é suportada por Lowerer.Expr")
	}
	if !strings.Contains(err.Error(), "Money") {
		t.Fatalf("mensagem de erro deveria mencionar o VO %q, got: %v", "Money", err)
	}
}

// --- 6. BinaryExpr: critério de conclusão (já coberto acima) + VO sem
// operador == declarado -> comparação nativa (ecoa o teste de goname,
// fim-a-fim através do Lowerer). ---

func TestExpr_Binary_VOEqualityWithoutOperatorIsNative(t *testing.T) {
	prog, l := newWalletLowerer(t)
	agg := findAggregate(t, prog, "Wallet")
	l.env.SeedAccess(agg.Name) // self: Wallet -> self.active: ActiveStatus

	// state.active == ActiveStatus(true) — a condição real de Handle Deposit/
	// Withdraw do wallet (lá escrita sobre "state", aqui sobre um Ident
	// "state" semeado manualmente com o mesmo tipo do Aggregate, já que
	// SeedAccess só semeia "self"; o efeito de tipo é idêntico).
	l.env.Bind("state", l.env.TypeOfName(agg.Name))
	expr := ast.NewBinaryExpr(token.EQ,
		member(ident("state"), "active"),
		ast.NewCallExpr(ident("ActiveStatus"), []ast.Arg{{Value: lit(token.TRUE, "")}}, ast.Span{}),
		ast.Span{})

	got, err := l.Expr(expr)
	if err != nil {
		t.Fatalf("Expr: erro inesperado: %v", err)
	}
	want := "state.Active == ActiveStatus(true)"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// --- 7. IndexExpr: caso simples sintético. ---

func TestExpr_Index_Synthetic(t *testing.T) {
	_, l := newWalletLowerer(t)
	l.env.Bind("xs", &types.Generic{Ctor: "List", Args: []types.Type{&types.Primitive{Name: "integer"}}})

	expr := ast.NewIndexExpr(ident("xs"), lit(token.INT, "0"), ast.Span{})
	got, err := l.Expr(expr)
	if err != nil {
		t.Fatalf("Expr: erro inesperado: %v", err)
	}
	if got != "xs[0]" {
		t.Fatalf("got %q, want %q", got, "xs[0]")
	}
}

// --- 8. RangeExpr: erro de geração explícito (não suportado em Expr()). ---

func TestExpr_Range_UnsupportedInExpr(t *testing.T) {
	_, l := newWalletLowerer(t)
	expr := ast.NewRangeExpr(lit(token.INT, "1"), lit(token.INT, "10"), ast.Span{})

	_, err := l.Expr(expr)
	if err == nil {
		t.Fatal("esperava erro: RangeExpr não tem forma de expressão Go isolada")
	}
}

// --- 9. LambdaExpr: Expr() recusa; Lambda(le, paramGoType) produz uma
// closure Go razoável (t => t.description, sobre StatementEntry — o VO
// composto real do wallet). ---

func TestExpr_Lambda_RejectedInExpr(t *testing.T) {
	_, l := newWalletLowerer(t)
	le := ast.NewLambdaExpr("t", member(ident("t"), "description"), ast.Span{})

	_, err := l.Expr(le)
	if err == nil {
		t.Fatal("esperava erro: LambdaExpr precisa de Lowerer.Lambda, não Lowerer.Expr")
	}
}

func TestExpr_Lambda_Produces_GoClosure(t *testing.T) {
	_, l := newWalletLowerer(t)
	le := ast.NewLambdaExpr("t", member(ident("t"), "description"), ast.Span{})

	got, err := l.Lambda(le, "StatementEntry")
	if err != nil {
		t.Fatalf("Lambda: erro inesperado: %v", err)
	}
	want := "func(t StatementEntry) TransactionDescription { return t.Description }"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// findValueObject é usado indiretamente via findAggregate/findHandle nos
// testes acima (env_test.go); este teste de sanidade só confirma que o VO
// Money existe e declara os 3 operadores usados pelo critério de conclusão,
// prevenindo uma fixture quebrada de mascarar um bug real.
func TestSanity_MoneyDeclaresExpectedOperators(t *testing.T) {
	prog, _ := newWalletLowerer(t)
	money := findValueObject(t, prog, "Money")
	if len(money.Operators) != 3 {
		t.Fatalf("esperava 3 Operators em Money (+, -, >=), achei %d", len(money.Operators))
	}
}
