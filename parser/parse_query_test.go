package parser

import "testing"

func TestQuerySimpleOps(t *testing.T) {
	cases := map[string]string{
		"store cmd.document":     "(store (. cmd document))",
		"load Wallet(id)":        "(load (call Wallet id))",
		"call PaymentRequest(x)": "(call (call PaymentRequest x))",
		"wallet exists":          "(exists wallet)",
	}
	for src, want := range cases {
		if got := sexpr(parseExprOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

func TestQueryInAndList(t *testing.T) {
	got := sexpr(parseExprOK(t, "t.status in [a, b]"))
	want := "(in (. t status) [a b])"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestQueryLoadChainWithClauses(t *testing.T) {
	src := "load Wallet(id).entries orderBy date descending skip page * 20 take 20 as StatementEntryVW"
	want := "(load (. (call Wallet id) entries)" +
		" {orderBy date descending} {skip (* page 20)} {take 20} {as StatementEntryVW})"
	if got := sexpr(parseExprOK(t, src)); got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

func TestQueryListJoinWhere(t *testing.T) {
	src := "list Ticket t join Order o on t.orderId == o.id " +
		"where o.userId == userId and t.status in [TicketStatus.Sold, TicketStatus.Used] as TicketVW"
	want := "(list Ticket :t" +
		" {join Order o}" +
		" {on (== (. t orderId) (. o id))}" +
		" {where (and (== (. o userId) userId) (in (. t status) [(. TicketStatus Sold) (. TicketStatus Used)]))}" +
		" {as TicketVW})"
	if got := sexpr(parseExprOK(t, src)); got != want {
		t.Errorf("=> %s\nquero %s", got, want)
	}
}

// Regressão ISSUE-11 (REQ-49.1/49.4): duas atribuições consecutivas no mesmo
// bloco, a 1ª com RHS de operação de domínio, não devem fazer o binding opcional
// roubar o identificador do 2º statement através da quebra de linha.
func TestConsecutiveAssignsDoNotStealBinding(t *testing.T) {
	cases := map[string]string{
		"{ order = load Bar(id)\nx = id }": "(block (= order (load (call Bar id))) (= x id))",
		"{ order = load Bar(id)\nx = 1 }":  "(block (= order (load (call Bar id))) (= x 1))",
	}
	for src, want := range cases {
		if got := sstmt(parseStmtOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

// REQ-49.2/49.5: a guarda de linha não pode quebrar o binding legítimo, que
// segue o alvo na MESMA linha.
func TestLegitimateBindingPreserved(t *testing.T) {
	src := "list Ticket t where t.active"
	want := "(list Ticket :t {where (. t active)})"
	if got := sexpr(parseExprOK(t, src)); got != want {
		t.Errorf("%q => %s, quero %s", src, got, want)
	}
}

// Regressão ISSUE-11 (REQ-49.3): o alias opcional de `join` não pode cruzar a
// quebra de linha e roubar o identificador do statement seguinte.
func TestConsecutiveAssignsDoNotStealJoinAlias(t *testing.T) {
	cases := map[string]string{
		"{ y = list Ticket join Foo\nx = id }": "(block (= y (list Ticket {join Foo})) (= x id))",
		"{ y = list Ticket join Foo\nx = 1 }":  "(block (= y (list Ticket {join Foo})) (= x 1))",
	}
	for src, want := range cases {
		if got := sstmt(parseStmtOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

// REQ-49.3: a guarda de linha não pode quebrar o alias legítimo de join, que
// segue a fonte na MESMA linha.
func TestLegitimateJoinAliasPreserved(t *testing.T) {
	src := "list Ticket t join Order o on t.orderId == o.id"
	want := "(list Ticket :t {join Order o} {on (== (. t orderId) (. o id))})"
	if got := sexpr(parseExprOK(t, src)); got != want {
		t.Errorf("%q => %s, quero %s", src, got, want)
	}
}

// signed_url é uma chamada de função comum, não uma operação prefixa.
func TestSignedUrlIsCall(t *testing.T) {
	got := sexpr(parseExprOK(t, "signed_url(doc, expires: x)"))
	want := "(call signed_url doc expires:x)"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}
