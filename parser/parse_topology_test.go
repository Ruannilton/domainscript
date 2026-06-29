package parser

import (
	"testing"

	"domainscript/ast"
)

func TestTopology(t *testing.T) {
	src := `Topology {
		services {
			CarteiraService { modules: [Carteira] }
			NotificacoesService { modules: [Notificacoes] }
		}
		channels {
			Carteira -> Notificacoes {
				via: queue
				provider: "rabbitmq"
				connection: env("RABBITMQ_URL")
				orderBy: aggregateId
				workers { concurrency: 5, maxRate: 100, batchSize: 10 }
			}
			Carteira -> Pagamentos {
				via: grpc
				timeout: 10s
				circuitBreaker: { threshold: 5, cooldown: 30s }
			}
		}
	}`
	d := parseDeclOK(t, src)
	top, ok := d.(*ast.TopologyDecl)
	if !ok {
		t.Fatalf("esperava TopologyDecl, veio %T", d)
	}
	if len(top.Services) != 2 {
		t.Fatalf("=> %d services, quero 2", len(top.Services))
	}
	if top.Services[0].Name != "CarteiraService" {
		t.Errorf("service[0] = %q", top.Services[0].Name)
	}
	if got := sexpr(findEntry(top.Services[0].Entries, "modules")); got != "[Carteira]" {
		t.Errorf("modules => %s", got)
	}
	if len(top.Channels) != 2 {
		t.Fatalf("=> %d channels, quero 2", len(top.Channels))
	}
	ch := top.Channels[0]
	if ch.From != "Carteira" || ch.To != "Notificacoes" {
		t.Errorf("channel[0] = %s -> %s", ch.From, ch.To)
	}
	if got := sexpr(findEntry(ch.Entries, "via")); got != "queue" {
		t.Errorf("via => %s", got)
	}
	if got := sexpr(findEntry(ch.Entries, "workers")); got != "{concurrency:5 maxRate:100 batchSize:10}" {
		t.Errorf("workers => %s", got)
	}
	if got := sexpr(findEntry(top.Channels[1].Entries, "circuitBreaker")); got != "{threshold:5 cooldown:30s}" {
		t.Errorf("circuitBreaker => %s", got)
	}
}

func TestTopologyRecovers(t *testing.T) {
	p, bag := mk(`Topology { + + channels { A -> B { via: direct } } }`)
	d := p.parseDecl()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico")
	}
	top, ok := d.(*ast.TopologyDecl)
	if !ok {
		t.Fatalf("esperava TopologyDecl, veio %T", d)
	}
	if len(top.Channels) != 1 || top.Channels[0].From != "A" {
		t.Errorf("canal deveria ser reconhecido apesar do lixo; canais=%v", top.Channels)
	}
	if !p.atEnd() {
		t.Errorf("não consumiu tudo; parou em %v", p.cur().Kind)
	}
}
