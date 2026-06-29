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

// signed_url é uma chamada de função comum, não uma operação prefixa.
func TestSignedUrlIsCall(t *testing.T) {
	got := sexpr(parseExprOK(t, "signed_url(doc, expires: x)"))
	want := "(call signed_url doc expires:x)"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}
