package parser

import "testing"

func TestRange(t *testing.T) {
	cases := map[string]string{
		"1..n":              "(.. 1 n)",
		"1..batch.quantity": "(.. 1 (. batch quantity))",
		"a..b":              "(.. a b)",
	}
	for src, want := range cases {
		if got := sexpr(parseExprOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

// O '.' de acesso a membro continua distinto do '..' de range.
func TestMemberStillWorksWithRange(t *testing.T) {
	if got := sexpr(parseExprOK(t, "foo.bar")); got != "(. foo bar)" {
		t.Errorf("foo.bar => %s, quero (. foo bar)", got)
	}
}

func TestLambda(t *testing.T) {
	cases := map[string]string{
		"t => t.x":         "(lambda t (. t x))",
		"i => i.price * 2": "(lambda i (* (. i price) 2))",
		"x => x":           "(lambda x x)",
	}
	for src, want := range cases {
		if got := sexpr(parseExprOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

func TestLambdaInCallArg(t *testing.T) {
	got := sexpr(parseExprOK(t, "items.distinct(t => t.orderId)"))
	want := "(call (. items distinct) (lambda t (. t orderId)))"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}
