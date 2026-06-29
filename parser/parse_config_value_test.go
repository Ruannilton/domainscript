package parser

import (
	"testing"

	"domainscript/ast"
)

// parseConfigValueOK lexa src, parseia um valor de configuração e exige zero
// diagnósticos e cursor no EOF.
func parseConfigValueOK(t *testing.T, src string) ast.Expr {
	t.Helper()
	p, bag := mk(src)
	v := p.parseConfigValue()
	if bag.Len() != 0 {
		t.Fatalf("parseConfigValue(%q) gerou diagnósticos: %s", src, bag.Render())
	}
	if !p.atEnd() {
		t.Fatalf("parseConfigValue(%q) não consumiu tudo; parou em %v", src, p.cur().Kind)
	}
	return v
}

func TestConfigValueScalars(t *testing.T) {
	cases := map[string]string{
		`"otlp"`:        `"otlp"`,
		`30s`:           `30s`,
		`300/min`:       `300/min`,
		`100MB`:         `100MB`,
		`true`:          `true`,
		`row_level`:     `row_level`,
		`v2`:            `v2`,
		`env("DB_URL")`: `(call env "DB_URL")`,
		`0.1`:           `0.1`,
		`[v1, v2]`:      `[v1 v2]`,
	}
	for src, want := range cases {
		if got := sexpr(parseConfigValueOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

func TestConfigValueNestedObject(t *testing.T) {
	got := sexpr(parseConfigValueOK(t, `{ attempts: 3, backoff: "exponential" }`))
	want := `{attempts:3 backoff:"exponential"}`
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestConfigValueListOfObjects(t *testing.T) {
	src := `[ { type: memory, maxSize: 100MB, ttl: 30s }, { type: redis, ttl: 5min } ]`
	got := sexpr(parseConfigValueOK(t, src))
	want := `[{type:memory maxSize:100MB ttl:30s} {type:redis ttl:5min}]`
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestConfigEntrySubBlockWithoutColon(t *testing.T) {
	// "traces { ... }" — sub-bloco nomeado sem dois-pontos (Telemetry).
	got := sexpr(parseConfigValueOK(t, `{ exporter: "otlp", traces { sampler: "x", sampleRate: 0.1 } }`))
	want := `{exporter:"otlp" traces:{sampler:"x" sampleRate:0.1}}`
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestConfigObjectRecovers(t *testing.T) {
	// Lixo no meio do objeto não trava o parser e não engole o '}' externo.
	p, bag := mk(`{ a: 1 + + b: 2 }`)
	v := p.parseConfigValue()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico para o lixo")
	}
	if _, ok := v.(*ast.ObjectExpr); !ok {
		t.Fatalf("esperava ObjectExpr, veio %T", v)
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo após recovery; parou em %v", p.cur().Kind)
	}
}
