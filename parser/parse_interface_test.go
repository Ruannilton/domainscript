package parser

import (
	"testing"

	"domainscript/ast"
)

func TestInterfaceHTTP(t *testing.T) {
	src := `Interface HTTP {
		port: env("HTTP_PORT")
		basePath: "/api"

		versioning {
			strategy: path
			current: v2
			supported: [v1, v2]
		}

		tenant { from: subdomain }

		rateLimit {
			perIp: 1000/min
			perUser: 300/min
		}

		POST "/wallets"                    -> CreateWallet
		POST "/wallets/{walletId}/deposit" -> PerformDeposit {
			rateLimit { perUser: 60/min, burst: 10 }
		}
		GET  "/wallets/{walletId}"         -> GetWalletSummary
		POST "/login" -> Login { tenancy: none, rateLimit: { perIp: 10/min, onBackendFailure: closed } }
	}`
	d := parseDeclOK(t, src)
	in, ok := d.(*ast.InterfaceDecl)
	if !ok {
		t.Fatalf("esperava InterfaceDecl, veio %T", d)
	}
	if in.Kind != "HTTP" {
		t.Errorf("kind = %q, quero HTTP", in.Kind)
	}
	if got := sexpr(findEntry(in.Settings, "port")); got != `(call env "HTTP_PORT")` {
		t.Errorf("port => %s", got)
	}
	if got := sexpr(findEntry(in.Settings, "versioning")); got != "{strategy:path current:v2 supported:[v1 v2]}" {
		t.Errorf("versioning => %s", got)
	}
	if len(in.Routes) != 4 {
		t.Fatalf("=> %d rotas, quero 4", len(in.Routes))
	}
	r := in.Routes[1]
	if r.Method != "POST" || r.Path != "/wallets/{walletId}/deposit" || r.Target != "PerformDeposit" {
		t.Errorf("rota[1] = %s %q -> %s", r.Method, r.Path, r.Target)
	}
	if got := sexpr(findEntry(r.Options, "rateLimit")); got != "{perUser:60/min burst:10}" {
		t.Errorf("rota[1] rateLimit => %s", got)
	}
	login := in.Routes[3]
	if got := sexpr(findEntry(login.Options, "tenancy")); got != "none" {
		t.Errorf("login tenancy => %s", got)
	}
}

func TestInterfaceGRPC(t *testing.T) {
	src := `Interface GRPC {
		port: env("GRPC_PORT")
		service WalletService {
			rpc Deposit -> PerformDeposit
			rpc GetWallet -> GetWalletSummary
		}
	}`
	d := parseDeclOK(t, src)
	in := d.(*ast.InterfaceDecl)
	if len(in.Services) != 1 {
		t.Fatalf("=> %d services, quero 1", len(in.Services))
	}
	svc := in.Services[0]
	if svc.Name != "WalletService" || len(svc.RPCs) != 2 {
		t.Fatalf("service = %q com %d rpcs", svc.Name, len(svc.RPCs))
	}
	if svc.RPCs[0].Name != "Deposit" || svc.RPCs[0].Target != "PerformDeposit" {
		t.Errorf("rpc[0] = %s -> %s", svc.RPCs[0].Name, svc.RPCs[0].Target)
	}
}

func TestRateLimitTier(t *testing.T) {
	d := parseDeclOK(t, `RateLimitTier Free { perUser: 100/min, perTenant: 1000/min }`)
	tier, ok := d.(*ast.RateLimitTierDecl)
	if !ok {
		t.Fatalf("esperava RateLimitTierDecl, veio %T", d)
	}
	if tier.Name != "Free" {
		t.Errorf("nome = %q, quero Free", tier.Name)
	}
	if got := sexpr(findEntry(tier.Entries, "perUser")); got != "100/min" {
		t.Errorf("perUser => %s", got)
	}
}

func TestInterfaceRecovers(t *testing.T) {
	p, bag := mk(`Interface HTTP { + + GET "/health" -> HealthCheck }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	in, ok := d.(*ast.InterfaceDecl)
	if !ok {
		t.Fatalf("esperava InterfaceDecl, veio %T", d)
	}
	if len(in.Routes) != 1 || in.Routes[0].Target != "HealthCheck" {
		t.Errorf("rota deveria ser reconhecida apesar do lixo; rotas=%v", in.Routes)
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo; parou em %v", p.cur().Kind)
	}
}
