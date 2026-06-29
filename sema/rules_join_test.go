package sema

import (
	"strings"
	"testing"
)

// REQ-5.10 (positivo): um JOIN entre Aggregates geridos por bancos distintos não
// tem JOIN físico possível e dispara erro.
func TestCrossDatabaseJoinFires(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Shop {
			Database TicketDb { provider: "pg" manages: [Ticket] }
			Database OrderDb { provider: "pg" manages: [Order] }
		}`, "shop", "mod.ds"),
		pf(`
			ValueObject TicketId(string) { Valid { ok } }
			ValueObject OrderId(string) { Valid { ok } }
			Aggregate Ticket { state { id TicketId } }
			Aggregate Order { state { id OrderId } }
			Query FindTickets() -> List<Ticket> {
				list Ticket t join Order o on t.orderId == o.id
			}
		`, "shop", "domain.ds"),
	)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "JOIN cross-database") {
		t.Fatalf("esperava erro de JOIN cross-database:\n%s", r)
	}
}

// REQ-5.10 (negativo): o mesmo JOIN dentro de um único banco é válido — silêncio.
func TestSameDatabaseJoinIsSilent(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Shop {
			Database ShopDb { provider: "pg" manages: [Ticket, Order] }
		}`, "shop", "mod.ds"),
		pf(`
			ValueObject TicketId(string) { Valid { ok } }
			ValueObject OrderId(string) { Valid { ok } }
			Aggregate Ticket { state { id TicketId } }
			Aggregate Order { state { id OrderId } }
			Query FindTickets() -> List<Ticket> {
				list Ticket t join Order o on t.orderId == o.id
			}
		`, "shop", "domain.ds"),
		pf(`Interface HTTP { GET "/tickets" -> FindTickets }`, "shop", "interface.ds"),
	)
	mustBeSilent(t, bag, "REQ-5.10")
}
