package sema

import (
	"strings"
	"testing"
)

// REQ-5.23 (positivo): uma Query que não é alvo de nenhuma interface não é
// alcançável de fora — aviso de exposição.
func TestUnexposedQueryWarns(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Shop { }`, "shop", "mod.ds"),
		pf(`
			ValueObject OrderId(string) { Valid { ok } }
			Aggregate Order { state { id OrderId } }
			Query ListOrders() -> List<Order> { list Order take 10 }
		`, "shop", "domain.ds"),
	)
	r := bag.Render()
	if bag.HasErrors() {
		t.Fatalf("esperava apenas aviso, veio erro:\n%s", r)
	}
	if !strings.Contains(r, "não é exposto") || !strings.Contains(r, "ListOrders") {
		t.Fatalf("esperava aviso de Query não exposta:\n%s", r)
	}
}

// REQ-5.23 (negativo): a mesma Query exposta numa rota HTTP é alcançável —
// silêncio.
func TestExposedQueryIsSilent(t *testing.T) {
	bag := checkProject(t,
		pf(`Module Shop { }`, "shop", "mod.ds"),
		pf(`
			ValueObject OrderId(string) { Valid { ok } }
			Aggregate Order { state { id OrderId } }
			Query ListOrders() -> List<Order> { list Order take 10 }
		`, "shop", "domain.ds"),
		pf(`Interface HTTP { GET "/orders" -> ListOrders }`, "shop", "interface.ds"),
	)
	mustBeSilent(t, bag, "REQ-5.23")
}
