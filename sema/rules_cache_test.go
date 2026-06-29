package sema

import (
	"strings"
	"testing"
)

// REQ-5.20 (positivo): cache sobre Query que retorna List (listagem) emite aviso
// de alta cardinalidade.
func TestCacheOnListingWarns(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		View TicketVW { id WalletId }
		Query GetMany(id WalletId) -> List<TicketVW> {
			cache { ttl: 5min }
			return list Ticket
		}
	`)
	r := bag.Render()
	if bag.HasErrors() {
		t.Fatalf("esperava aviso, não erro:\n%s", r)
	}
	if !strings.Contains(r, "cardinalidade") || !strings.Contains(r, "warning") {
		t.Fatalf("esperava aviso de cache de alta cardinalidade:\n%s", r)
	}
}

// REQ-5.20 (negativo): cache sobre Query que retorna um único VW (cirúrgica), e
// listagem sem cache, não disparam.
func TestCacheOnSingleResultIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject WalletId(string) { Valid { ok } }
		View WalletSummaryVW { id WalletId }
		Query GetOne(id WalletId) -> WalletSummaryVW {
			cache { ttl: 5min }
			return load Wallet(id) as WalletSummaryVW
		}
		Query ListAll(id WalletId) -> List<WalletSummaryVW> {
			return list Wallet
		}
	`)
	mustBeSilent(t, bag, "REQ-5.20")
}
