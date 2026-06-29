package sema

import (
	"strings"
	"testing"
)

// REQ-5.3 (positivo): Notification sem Adapter correspondente dispara erro.
func TestNotificationWithoutAdapterFires(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject Email(string) { Valid { ok } }
		Notification DepositNotification { to Email }
	`)
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "DepositNotification") || !strings.Contains(r, "Adapter") {
		t.Fatalf("esperava erro de Notification sem Adapter:\n%s", r)
	}
}

// REQ-5.3 (negativo): Notification com Adapter de mesmo nome não dispara.
func TestNotificationWithAdapterIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		ValueObject Email(string) { Valid { ok } }
		Notification DepositNotification { to Email }
		Adapter DepositNotification {
			mode async
			http POST "https://example.com"
		}
	`)
	mustBeSilent(t, bag, "REQ-5.3")
}
