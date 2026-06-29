package lexer

import (
	"testing"

	"domainscript/token"
)

type want struct {
	kind token.Kind
	lit  string // "" = não verifica o lexema
}

// assertTokens compara a sequência de tokens de src com wants (Kind e, quando
// dado, Lit) e exige ausência de diagnósticos.
func assertTokens(t *testing.T, src string, wants []want) {
	t.Helper()
	toks, diags := Lex(src)
	if len(diags) != 0 {
		t.Fatalf("Lex(%q) produziu diagnósticos inesperados: %v", src, diags)
	}
	assertTokenSeq(t, src, toks, wants)
}

func assertTokenSeq(t *testing.T, src string, toks []token.Token, wants []want) {
	t.Helper()
	if len(toks) != len(wants) {
		t.Fatalf("Lex(%q) => %d tokens, quero %d\n%v", src, len(toks), len(wants), toks)
	}
	for i, w := range wants {
		if toks[i].Kind != w.kind {
			t.Errorf("token[%d].Kind = %v, quero %v", i, toks[i].Kind, w.kind)
		}
		if w.lit != "" && toks[i].Lit != w.lit {
			t.Errorf("token[%d].Lit = %q, quero %q", i, toks[i].Lit, w.lit)
		}
	}
}

func TestIdentAndKeyword(t *testing.T) {
	assertTokens(t, "Wallet walletId _x Aggregate match true false", []want{
		{token.IDENT, "Wallet"},
		{token.IDENT, "walletId"},
		{token.IDENT, "_x"},
		{token.AGGREGATE, ""},
		{token.MATCH, ""},
		{token.TRUE, ""},
		{token.FALSE, ""},
		{token.EOF, ""},
	})
}

func TestIntAndFloat(t *testing.T) {
	assertTokens(t, "42 0 3.14 100", []want{
		{token.INT, "42"},
		{token.INT, "0"},
		{token.FLOAT, "3.14"},
		{token.INT, "100"},
		{token.EOF, ""},
	})
}

func TestTriviaAndComments(t *testing.T) {
	src := "  Wallet // comentário até o fim\n  42\n// linha inteira\n true"
	assertTokens(t, src, []want{
		{token.IDENT, "Wallet"},
		{token.INT, "42"},
		{token.TRUE, ""},
		{token.EOF, ""},
	})
}

func TestBasicString(t *testing.T) {
	assertTokens(t, `"hello world" "" "x"`, []want{
		{token.STRING, "hello world"},
		{token.STRING, ""},
		{token.STRING, "x"},
		{token.EOF, ""},
	})
}

func TestPositions(t *testing.T) {
	toks, _ := Lex("ab\n  cd")
	wantPos := []token.Pos{
		{Line: 1, Col: 1}, // ab
		{Line: 2, Col: 3}, // cd
		{Line: 2, Col: 5}, // EOF
	}
	if len(toks) != len(wantPos) {
		t.Fatalf("=> %d tokens, quero %d (%v)", len(toks), len(wantPos), toks)
	}
	for i, p := range wantPos {
		if toks[i].Pos != p {
			t.Errorf("token[%d].Pos = %v, quero %v", i, toks[i].Pos, p)
		}
	}
}

func TestTerminatesWithEOF(t *testing.T) {
	toks, _ := Lex("")
	if len(toks) != 1 || toks[0].Kind != token.EOF {
		t.Fatalf("entrada vazia => %v, quero [EOF]", toks)
	}
}
