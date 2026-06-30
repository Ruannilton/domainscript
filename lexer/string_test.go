package lexer

import (
	"strings"
	"testing"

	"domainscript/token"
)

func TestStringEscapes(t *testing.T) {
	// Fonte Go: \\n etc. representam os 2 caracteres \ e n no source DomainScript.
	src := `"linha1\nlinha2\ttab\"aspas\\barra"`
	want := "linha1\nlinha2\ttab\"aspas\\barra"
	toks, diags := Lex(src)
	if len(diags) != 0 {
		t.Fatalf("diagnósticos inesperados: %v", diags)
	}
	if toks[0].Kind != token.STRING || toks[0].Lit != want {
		t.Errorf("STRING = %q, quero %q", toks[0].Lit, want)
	}
}

func TestUnterminatedStringAtEOF(t *testing.T) {
	toks, diags := Lex(`"sem fim`)
	if len(diags) != 1 {
		t.Fatalf("=> %d diagnósticos, quero 1 (%v)", len(diags), diags)
	}
	if diags[0].Pos != (token.Pos{Line: 1, Col: 1}) {
		t.Errorf("diagnóstico em %v, quero 1:1 (início da string)", diags[0].Pos)
	}
	if toks[0].Kind != token.STRING || toks[0].Lit != "sem fim" {
		t.Errorf("token = {%v %q}, quero STRING 'sem fim'", toks[0].Kind, toks[0].Lit)
	}
	// Mensagem acionável (REQ-6.8): aponta o que faltou (aspas) e a causa (EOF).
	if msg := diags[0].Msg; !strings.Contains(msg, "aspas de fechamento") || !strings.Contains(msg, "arquivo") {
		t.Errorf("mensagem pouco acionável: %q", msg)
	}
}

func TestUnterminatedStringAtNewline(t *testing.T) {
	// A quebra de linha encerra a string com erro; o conteúdo após continua.
	toks, diags := Lex("\"ab\ncd")
	if len(diags) != 1 {
		t.Fatalf("=> %d diagnósticos, quero 1 (%v)", len(diags), diags)
	}
	assertTokenSeq(t, "\"ab\\ncd", toks, []want{
		{token.STRING, "ab"},
		{token.IDENT, "cd"},
		{token.EOF, ""},
	})
	// A causa concreta aqui é o fim da linha, não do arquivo (REQ-6.8).
	if msg := diags[0].Msg; !strings.Contains(msg, "aspas de fechamento") || !strings.Contains(msg, "linha") {
		t.Errorf("mensagem pouco acionável: %q", msg)
	}
}

func TestInvalidEscape(t *testing.T) {
	toks, diags := Lex(`"a\qb"`)
	if len(diags) != 1 {
		t.Fatalf("=> %d diagnósticos, quero 1 (%v)", len(diags), diags)
	}
	// O escape inválido é reportado na posição da barra (coluna 3).
	if diags[0].Pos != (token.Pos{Line: 1, Col: 3}) {
		t.Errorf("diagnóstico em %v, quero 1:3", diags[0].Pos)
	}
	// O caractere é mantido literalmente no valor.
	if toks[0].Lit != "aqb" {
		t.Errorf("STRING = %q, quero %q", toks[0].Lit, "aqb")
	}
}
