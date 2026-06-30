package driver

import (
	"testing"
)

// TestCheckSourceDeterministic roda o pipeline completo várias vezes sobre a mesma
// fonte e exige saída byte-a-byte idêntica em toda execução (NFR-3). A fonte mistura
// erros de sintaxe, de resolução e semânticos, garantindo que diagnósticos de fases
// distintas se mesclam de forma determinística.
func TestCheckSourceDeterministic(t *testing.T) {
	const src = `
		Aggregate Account { state { balance integer } }
		Command DepositCmd { amount decimal }
		UseCase Deposit handles MissingCmd { execute { Nop } }
		match status { Active => 1 }
		ValueObject {
	`
	const runs = 25
	var want string
	for i := 0; i < runs; i++ {
		_, bag := CheckSource(src)
		got := bag.Render()
		if i == 0 {
			want = got
			if got == "" {
				t.Fatal("esperava diagnósticos na fonte de teste")
			}
			continue
		}
		if got != want {
			t.Fatalf("CheckSource não-determinístico na execução %d (NFR-3):\n--- esperado ---\n%s\n--- obtido ---\n%s",
				i, want, got)
		}
	}
}

// TestCheckProjectDeterministic exercita o determinismo cross-file: a iteração de
// mapas (arquivos do programa, símbolos) não pode vazar para a ordem dos
// diagnósticos. O projeto é escrito uma vez e checado várias, exigindo saída
// idêntica (NFR-3).
func TestCheckProjectDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeProject(t, dir, map[string]string{
		"billing/mod.ds": `Module Billing { }`,
		"billing/events.ds": `
			ValueObject OrderId(string) { Valid { ok } }
			Event InvoicePaid { id OrderId }
			Aggregate Invoice { state { id OrderId } }
		`,
		"shipping/mod.ds":    `Module Shipping { }`,
		"shipping/policy.ds": `Policy OnPaid on InvoicePaid { delivery AtLeastOnce execute { return } }`,
		"shipping/agg.ds": `
			ValueObject ShipId(string) { Valid { ok } }
			Aggregate Shipment { state { id ShipId } }
		`,
	})

	const runs = 25
	var want string
	for i := 0; i < runs; i++ {
		_, bag := CheckProject(dir)
		got := bag.Render()
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("CheckProject não-determinístico na execução %d (NFR-3):\n--- esperado ---\n%s\n--- obtido ---\n%s",
				i, want, got)
		}
	}
}
