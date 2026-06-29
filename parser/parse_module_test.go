package parser

import (
	"testing"

	"domainscript/ast"
)

// findEntry devolve o valor da entrada de chave key, ou nil.
func findEntry(es []ast.ConfigEntry, key string) ast.Expr {
	for _, e := range es {
		if e.Key == key {
			return e.Value
		}
	}
	return nil
}

func parseModuleOK(t *testing.T, src string) *ast.ModuleDecl {
	t.Helper()
	d := parseDeclOK(t, src)
	m, ok := d.(*ast.ModuleDecl)
	if !ok {
		t.Fatalf("esperava ModuleDecl, veio %T", d)
	}
	return m
}

func TestModuleFull(t *testing.T) {
	src := `Module Carteira {
		timeout 30s

		Database WalletDb {
			provider: "Postgres"
			connection: env("DB_URL")
			supportsXA: true
			manages: [Wallet]
			retry: { attempts: 3, backoff: "exponential" }
			tenancy: { strategy: row_level, column: "tenant_id" }
		}

		FileStorage DocumentStorage {
			provider: "s3"
			bucket: env("DOCUMENTS_BUCKET")
		}

		Idempotency {
			storage: same
			window: 24h
			required: true
		}

		Cache {
			backend: layered
			layers: [
				{ type: memory, maxSize: 100MB, ttl: 30s },
				{ type: redis, connection: env("REDIS_URL"), ttl: 5min }
			]
			defaultTtl: 1min
		}

		Telemetry {
			exporter: "otlp"
			traces { sampler: "parentbased_traceidratio", sampleRate: 0.1 }
			metrics { interval: 30s }
		}
	}`
	m := parseModuleOK(t, src)
	if m.Name != "Carteira" {
		t.Errorf("nome = %q, quero Carteira", m.Name)
	}
	if v := findEntry(m.Settings, "timeout"); sexpr(v) != "30s" {
		t.Errorf("timeout = %v, quero 30s", v)
	}
	wantKinds := []struct{ kind, name string }{
		{"Database", "WalletDb"},
		{"FileStorage", "DocumentStorage"},
		{"Idempotency", ""},
		{"Cache", ""},
		{"Telemetry", ""},
	}
	if len(m.Blocks) != len(wantKinds) {
		t.Fatalf("=> %d blocos, quero %d", len(m.Blocks), len(wantKinds))
	}
	for i, w := range wantKinds {
		if m.Blocks[i].Kind != w.kind || m.Blocks[i].Name != w.name {
			t.Errorf("bloco[%d] = %s %q, quero %s %q", i, m.Blocks[i].Kind, m.Blocks[i].Name, w.kind, w.name)
		}
	}
	// Valores aninhados dentro de um bloco.
	db := m.Blocks[0]
	if got := sexpr(findEntry(db.Entries, "retry")); got != `{attempts:3 backoff:"exponential"}` {
		t.Errorf("retry => %s", got)
	}
	if got := sexpr(findEntry(db.Entries, "manages")); got != "[Wallet]" {
		t.Errorf("manages => %s", got)
	}
	cache := m.Blocks[3]
	if got := sexpr(findEntry(cache.Entries, "layers")); got != "[{type:memory maxSize:100MB ttl:30s} {type:redis connection:(call env \"REDIS_URL\") ttl:5min}]" {
		t.Errorf("layers => %s", got)
	}
	tel := m.Blocks[4]
	if got := sexpr(findEntry(tel.Entries, "traces")); got != `{sampler:"parentbased_traceidratio" sampleRate:0.1}` {
		t.Errorf("traces => %s", got)
	}
}

func TestModuleRecovers(t *testing.T) {
	p, bag := mk(`Module M { + + Database D { provider: "Postgres" } }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	m, ok := d.(*ast.ModuleDecl)
	if !ok {
		t.Fatalf("esperava ModuleDecl, veio %T", d)
	}
	if len(m.Blocks) != 1 || m.Blocks[0].Name != "D" {
		t.Errorf("bloco Database deveria ser reconhecido apesar do lixo; blocos=%v", m.Blocks)
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo; parou em %v", p.cur().Kind)
	}
}
