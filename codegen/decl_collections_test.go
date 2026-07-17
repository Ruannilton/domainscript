package codegen_test

import (
	"strings"
	"testing"

	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"

	"domainscript/codegen"
)

// decl_collections_test.go prova a correção de ISSUE-1 (.claude/issues.md):
// antes desta task, EmitQueries (decl_query.go, join, I5.1) e EmitPolicies
// (decl_policy.go, list/count, H4) declaravam CADA UM o seu próprio "var
// <tipo>Collection = runtime.NewMemoryCollection[<Tipo>]()" independentemente
// — um em queries.go, outro em policies.go. Os dois arquivos compartilham o
// MESMO pacote Go; quando o MESMO tipo (aqui, Ticket) é fonte de "join" numa
// Query E alvo de "list"/"count" numa Policy do MESMO módulo, os dois
// emissores geravam a MESMA declaração de var, e "go build" falhava com
// "ticketCollection redeclared in this block". Nenhum exemplo real
// exercitava essa combinação antes (por isso o bug não foi pego pelos testes
// existentes).
//
// A fixture abaixo (módulo Tickets) combina, de propósito, as DUAS formas
// sobre o MESMO tipo Ticket:
//
//   - Query GetMyTickets: "list Ticket t join Order o on ... as TicketVW"
//     (mesma forma de gentest_query_getmytickets_test.go, I5.1).
//   - Policy ReportSoldTicketsCount: "count Ticket t where ..." (mesma forma
//     de H4/§22.4) reagindo a um Event síncrono do próprio módulo.
//
// generateModuleFiles (codegen.go) agora calcula a INTERSEÇÃO dos tipos
// usados pelos dois lados (sharedModuleCollectionTypeNames,
// decl_collections.go) e, quando não vazia, declara "ticketCollection" UMA
// ÚNICA VEZ em collections.go — TestSharedCollectionTypeCompiles prova que o
// projeto inteiro gerado por essa fixture compila de verdade (o smoke que
// teria falhado com "redeclared in this block" antes da correção).
const sharedCollectionFixtureModDs = `Module Tickets { }
`

const sharedCollectionFixtureDomainDs = `
ValueObject TicketId(string) {
    Valid { value.length() > 0 }
}

ValueObject OrderId(string) {
    Valid { value.length() > 0 }
}

ValueObject UserId(string) {
    Valid { value.length() > 0 }
}

Enum TicketStatus : string {
    Available = "AVAILABLE"
    Sold      = "SOLD"
    Used      = "USED"
    Cancelled = "CANCELLED"
}

ValueObject Ticket {
    id      TicketId
    orderId OrderId
    status  TicketStatus
}

ValueObject Order {
    id     OrderId
    userId UserId
}

ValueObject TicketCount(integer) {
    Valid { value >= 0 }
}

Event SoldTicketsCountRequested {
    requestedBy UserId
}

Event SoldTicketsCountReported {
    total TicketCount
}

Policy ReportSoldTicketsCount on SoldTicketsCountRequested {
    delivery BestEffort
    execute {
        total = count Ticket t where t.status == TicketStatus.Sold
        emit SoldTicketsCountReported(total: TicketCount(total))
    }
}
`

const sharedCollectionFixtureReadDs = `
View TicketVW {
    orderId OrderId
    status  TicketStatus
}

Query GetMyTickets(userId UserId) -> List<TicketVW> {
    return list Ticket t
           join Order o on t.orderId == o.id
           where o.userId == userId
             and t.status in [TicketStatus.Sold, TicketStatus.Used]
           as TicketVW
}
`

// generateSharedCollectionFixtureProject roda o orquestrador COMPLETO
// (codegen.Generate) sobre o Program da fixture Tickets acima — mesmo padrão
// de generateGetMyTicketsFixtureProject (gentest_query_getmytickets_test.go).
func generateSharedCollectionFixtureProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":    sharedCollectionFixtureModDs,
		"domain.ds": sharedCollectionFixtureDomainDs,
		"read.ds":   sharedCollectionFixtureReadDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture de ISSUE-1 (Query com join + Policy com count sobre o mesmo tipo) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, walletGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture de ISSUE-1: %v", err)
	}
	return files
}

// TestSharedCollectionTypeNoDuplicateVar prova diretamente que "var
// ticketCollection = ..." aparece EXATAMENTE UMA VEZ entre todos os arquivos
// gerados do módulo (não uma vez em queries.go E outra em policies.go, como
// antes da correção) — e que mora em collections.go, o arquivo compartilhado.
func TestSharedCollectionTypeNoDuplicateVar(t *testing.T) {
	files := filesToMap(generateSharedCollectionFixtureProject(t))

	const decl = "var ticketCollection = runtime.NewMemoryCollection[Ticket]()"
	count := 0
	var declaredIn []string
	for path, content := range files {
		if !strings.HasPrefix(path, "tickets/") {
			continue
		}
		occurrences := strings.Count(string(content), decl)
		count += occurrences
		if occurrences > 0 {
			declaredIn = append(declaredIn, path)
		}
	}
	if count != 1 {
		t.Fatalf("esperava exatamente 1 declaração de %q entre os arquivos de tickets/, achei %d (em %v) — ISSUE-1 não corrigida", decl, count, declaredIn)
	}
	wantPath := "tickets/collections.go"
	if len(declaredIn) != 1 || declaredIn[0] != wantPath {
		t.Fatalf("esperava %q declarado em %q, achei em %v", decl, wantPath, declaredIn)
	}

	collectionsGo, ok := files[wantPath]
	if !ok {
		t.Fatalf("esperava um arquivo %q entre os arquivos gerados, não achei", wantPath)
	}
	if !strings.Contains(string(collectionsGo), decl) {
		t.Fatalf("esperava %q em %s, got:\n%s", decl, wantPath, collectionsGo)
	}
}

// TestSharedCollectionTypeCompiles prova NFR-14 sobre o projeto INTEIRO
// gerado por esta fixture: compila e passa go vet num diretório isolado — a
// prova de que a colisão de ISSUE-1 ("ticketCollection redeclared in this
// block") não acontece mais quando uma Query com join e uma Policy com
// list/count do mesmo módulo referenciam o mesmo tipo.
func TestSharedCollectionTypeCompiles(t *testing.T) {
	gentest.SmokeCompile(t, filesToMap(generateSharedCollectionFixtureProject(t)))
}

// TestSharedCollectionTypeDeterministic prova NFR-13: regerar a mesma fixture
// duas vezes produz bytes idênticos.
func TestSharedCollectionTypeDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		files := filesToMap(generateSharedCollectionFixtureProject(t))
		return files["tickets/collections.go"]
	})
}
