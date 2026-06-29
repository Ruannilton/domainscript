package token

import "testing"

func TestKindString(t *testing.T) {
	cases := map[Kind]string{
		EOF:         "EOF",
		IDENT:       "IDENT",
		INT:         "INT",
		LBRACE:      "{",
		ARROW:       "->",
		EQ:          "==",
		LE:          "<=",
		VALUEOBJECT: "ValueObject",
		ENSURE:      "ensure",
		TRUE:        "true",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, quero %q", int(k), got, want)
		}
	}
	if got := Kind(9999).String(); got != "Kind(9999)" {
		t.Errorf("Kind fora da tabela = %q, quero %q", got, "Kind(9999)")
	}
}

func TestLookupKeyword(t *testing.T) {
	keywordCases := map[string]Kind{
		"ValueObject": VALUEOBJECT,
		"Aggregate":   AGGREGATE,
		"Version":     VERSION,
		"match":       MATCH,
		"ref":         REF,
		"true":        TRUE,
	}
	for src, want := range keywordCases {
		if got := Lookup(src); got != want {
			t.Errorf("Lookup(%q) = %v, quero %v", src, got, want)
		}
		if !want.IsKeyword() {
			t.Errorf("%q (%v) deveria ser keyword", src, want)
		}
	}
}

func TestLookupIdent(t *testing.T) {
	// Não-keywords (incl. quase-keywords) resolvem para IDENT.
	for _, src := range []string{"walletId", "Wallet", "foo", "matchimum", "References"} {
		if got := Lookup(src); got != IDENT {
			t.Errorf("Lookup(%q) = %v, quero IDENT", src, got)
		}
	}
}

func TestPos(t *testing.T) {
	a := Pos{Line: 1, Col: 5}
	b := Pos{Line: 1, Col: 8}
	c := Pos{Line: 2, Col: 1}
	if !a.Less(b) {
		t.Errorf("%v deveria ser < %v", a, b)
	}
	if !b.Less(c) {
		t.Errorf("%v deveria ser < %v", b, c)
	}
	if a.Less(a) {
		t.Errorf("%v não deveria ser < si mesmo", a)
	}
	if got := a.String(); got != "1:5" {
		t.Errorf("Pos.String() = %q, quero %q", got, "1:5")
	}
}
