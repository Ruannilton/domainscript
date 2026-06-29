package program

import (
	"testing"

	"domainscript/diag"
)

// projeto de exemplo com dois módulos, topologia em dois services e um canal
// entre eles. Exercita todo o grafo módulo→service→canal e o mapeamento de
// aggregates (REQ-7.2).
func sampleProgram(t *testing.T) *Program {
	t.Helper()
	carteiraMod := parseSrc(t, `Module Carteira {
		Database WalletDb { provider: "pg" supportsXA: true manages: [Wallet] }
	}`)
	carteiraDomain := parseSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		Aggregate Wallet { state { id WalletId } Handle Open(owner WalletId) { return } }
	`)
	notifMod := parseSrc(t, `Module Notificacoes {
		Database NotifDb { provider: "dynamo" supportsXA: false manages: [Inbox] }
	}`)
	notifDomain := parseSrc(t, `
		ValueObject InboxId(string) { Valid { ok } }
		Aggregate Inbox { state { id InboxId } Handle Open(owner InboxId) { return } }
	`)
	topology := parseSrc(t, `Topology {
		services {
			CarteiraService { modules: [Carteira] }
			NotificacoesService { modules: [Notificacoes] }
		}
		channels {
			Carteira -> Notificacoes { via: queue orderBy: aggregateId }
		}
	}`)

	bag := diag.New()
	prog := New([]Source{
		{Path: modPath("carteira", "mod.ds"), File: carteiraMod},
		{Path: modPath("carteira", "wallet.ds"), File: carteiraDomain},
		{Path: modPath("notificacoes", "mod.ds"), File: notifMod},
		{Path: modPath("notificacoes", "inbox.ds"), File: notifDomain},
		{Path: modPath("topology.ds"), File: topology},
	}, bag)
	if bag.HasErrors() {
		t.Fatalf("projeto correto não deveria gerar erros:\n%s", bag.Render())
	}
	return prog
}

// Os módulos e seus bancos são extraídos de mod.ds com supportsXA e manages.
func TestBuildGraphModulesAndDatabases(t *testing.T) {
	prog := sampleProgram(t)

	if len(prog.Modules) != 2 {
		t.Fatalf("esperava 2 módulos, obtive %d", len(prog.Modules))
	}
	wallet := prog.Modules["Carteira"]
	if wallet == nil {
		t.Fatal("módulo Carteira ausente")
	}
	db := wallet.Databases["WalletDb"]
	if db == nil {
		t.Fatal("banco WalletDb ausente")
	}
	if !db.SupportsXA {
		t.Errorf("WalletDb deveria ter supportsXA=true")
	}
	if len(db.Manages) != 1 || db.Manages[0] != "Wallet" {
		t.Errorf("WalletDb deveria gerenciar [Wallet], obtive %v", db.Manages)
	}
	if notif := prog.Modules["Notificacoes"].Databases["NotifDb"]; notif.SupportsXA {
		t.Errorf("NotifDb deveria ter supportsXA=false")
	}
}

// Services e canais são extraídos da topologia, e cada módulo aponta de volta
// para o seu service.
func TestBuildGraphServicesAndChannels(t *testing.T) {
	prog := sampleProgram(t)

	if len(prog.Services) != 2 {
		t.Fatalf("esperava 2 services, obtive %d", len(prog.Services))
	}
	svc := prog.Services["CarteiraService"]
	if svc == nil || len(svc.Modules) != 1 || svc.Modules[0] != "Carteira" {
		t.Fatalf("CarteiraService deveria conter [Carteira], obtive %+v", svc)
	}
	if got := prog.ServiceOfModule("Carteira"); got != "CarteiraService" {
		t.Errorf("Carteira deveria estar em CarteiraService, obtive %q", got)
	}

	if len(prog.Channels) != 1 {
		t.Fatalf("esperava 1 canal, obtive %d", len(prog.Channels))
	}
	ch := prog.ChannelBetween("Carteira", "Notificacoes")
	if ch == nil {
		t.Fatal("canal Carteira->Notificacoes ausente")
	}
	if ch.Via != "queue" {
		t.Errorf("canal deveria ter via=queue, obtive %q", ch.Via)
	}
	if prog.ChannelBetween("Notificacoes", "Carteira") != nil {
		t.Errorf("não deveria haver canal no sentido inverso")
	}
}

// A cadeia aggregate→Database→módulo→service resolve ponta a ponta (REQ-7.2).
func TestAggregateToServiceMapping(t *testing.T) {
	prog := sampleProgram(t)

	if got := prog.ModuleOfAggregate("Wallet"); got != "Carteira" {
		t.Errorf("Wallet deveria pertencer a Carteira, obtive %q", got)
	}
	if db := prog.DatabaseOfAggregate("Wallet"); db == nil || db.Name != "WalletDb" {
		t.Errorf("Wallet deveria ser gerido por WalletDb, obtive %+v", db)
	}
	if got := prog.ServiceOfAggregate("Wallet"); got != "CarteiraService" {
		t.Errorf("Wallet deveria estar em CarteiraService, obtive %q", got)
	}
	if got := prog.ServiceOfAggregate("Inbox"); got != "NotificacoesService" {
		t.Errorf("Inbox deveria estar em NotificacoesService, obtive %q", got)
	}

	// Aggregate inexistente não resolve a nada.
	if got := prog.ModuleOfAggregate("Inexistente"); got != "" {
		t.Errorf("aggregate inexistente deveria mapear para \"\", obtive %q", got)
	}
	if db := prog.DatabaseOfAggregate("Inexistente"); db != nil {
		t.Errorf("aggregate inexistente não deveria ter banco, obtive %+v", db)
	}
}
