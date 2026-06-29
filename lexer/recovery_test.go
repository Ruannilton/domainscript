package lexer

import (
	"testing"

	"domainscript/token"
)

func TestInvalidCharReportsAndContinues(t *testing.T) {
	toks, diags := Lex("a @ b")
	if len(diags) != 1 {
		t.Fatalf("=> %d diagnósticos, quero 1 (%v)", len(diags), diags)
	}
	if diags[0].Pos != (token.Pos{Line: 1, Col: 3}) {
		t.Errorf("diagnóstico em %v, quero 1:3", diags[0].Pos)
	}
	// O caractere inválido não vira token: a tokenização segue em frente.
	assertTokenSeq(t, "a @ b", toks, []want{
		{token.IDENT, "a"},
		{token.IDENT, "b"},
		{token.EOF, ""},
	})
}

func TestMultipleInvalidChars(t *testing.T) {
	toks, diags := Lex("@#$")
	if len(diags) != 3 {
		t.Errorf("=> %d diagnósticos, quero 3 (%v)", len(diags), diags)
	}
	if len(toks) != 1 || toks[0].Kind != token.EOF {
		t.Errorf("=> %v, quero apenas [EOF]", toks)
	}
}

// '!' isolado (sem '=') não é operador válido: vira diagnóstico, não NEQ.
func TestLoneBangIsInvalid(t *testing.T) {
	toks, diags := Lex("a ! b")
	if len(diags) != 1 {
		t.Fatalf("=> %d diagnósticos, quero 1 (%v)", len(diags), diags)
	}
	assertTokenSeq(t, "a ! b", toks, []want{
		{token.IDENT, "a"},
		{token.IDENT, "b"},
		{token.EOF, ""},
	})
}

// Garantia de progresso (REQ-1.6, NFR-2): qualquer entrada termina, sempre com
// um EOF como último token e sem laço infinito (o teste só retorna se Lex
// retornar).
func TestAlwaysTerminatesWithEOF(t *testing.T) {
	inputs := []string{
		"",
		"\x00\x01\x02",
		"@@@@@@@@",
		"\"aberta sem fim",
		"\\\\\\",
		"123abc456",
		"日本語 @ café",
		"!!!===",
		"////",
		"v1v2v3 100MB/min",
	}
	for _, src := range inputs {
		toks, _ := Lex(src)
		if len(toks) == 0 || toks[len(toks)-1].Kind != token.EOF {
			t.Errorf("Lex(%q) não terminou com EOF: %v", src, toks)
		}
	}
}
