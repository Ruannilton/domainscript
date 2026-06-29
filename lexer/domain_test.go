package lexer

import (
	"testing"

	"domainscript/token"
)

func TestDurations(t *testing.T) {
	assertTokens(t, "5s 48h 100ms 30min 7d", []want{
		{token.DURATION, "5s"},
		{token.DURATION, "48h"},
		{token.DURATION, "100ms"},
		{token.DURATION, "30min"},
		{token.DURATION, "7d"},
		{token.EOF, ""},
	})
}

func TestSizes(t *testing.T) {
	assertTokens(t, "100MB 2GB 512KB 1B 4TB", []want{
		{token.SIZE, "100MB"},
		{token.SIZE, "2GB"},
		{token.SIZE, "512KB"},
		{token.SIZE, "1B"},
		{token.SIZE, "4TB"},
		{token.EOF, ""},
	})
}

func TestRates(t *testing.T) {
	assertTokens(t, "300/min 10/s 5/h", []want{
		{token.RATE, "300/min"},
		{token.RATE, "10/s"},
		{token.RATE, "5/h"},
		{token.EOF, ""},
	})
}

func TestVersionIDs(t *testing.T) {
	assertTokens(t, "v1 v12 v0", []want{
		{token.VERSIONID, "v1"},
		{token.VERSIONID, "v12"},
		{token.VERSIONID, "v0"},
		{token.EOF, ""},
	})
}

// version_id não captura identificadores comuns nem a keyword Version.
func TestVersionIDBoundaries(t *testing.T) {
	assertTokens(t, "value Version v vNext", []want{
		{token.IDENT, "value"},
		{token.VERSION, ""},
		{token.IDENT, "v"},
		{token.IDENT, "vNext"},
		{token.EOF, ""},
	})
}

// Sem unidade reconhecida, o número fica INT e o resto segue normalmente:
// a divisão "10 / 2" e a multiplicação "3*4" não viram RATE/sufixo.
func TestNoSuffixFallback(t *testing.T) {
	assertTokens(t, "10/2 3*4 100 abc", []want{
		{token.INT, "10"}, {token.SLASH, ""}, {token.INT, "2"},
		{token.INT, "3"}, {token.STAR, ""}, {token.INT, "4"},
		{token.INT, "100"},
		{token.IDENT, "abc"},
		{token.EOF, ""},
	})
}
