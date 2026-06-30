package resolver

import (
	"testing"
)

// A tabela de receptores semeia os nomes implícitos corretos por construto
// (REQ-9.3). Verifica os casos-âncora do design §3.2.
func TestContextualReceivers(t *testing.T) {
	has := func(c construct, name string) bool {
		sc := NewScope()
		seedReceivers(sc, c)
		_, ok := sc.Lookup(name)
		return ok
	}

	cases := []struct {
		c     construct
		name  string
		grant bool
	}{
		{constructHandle, "self", true},
		{constructHandle, "state", true},
		{constructHandle, "caller", true},
		{constructHandle, "event", false}, // event não é receptor de Handle
		{constructApply, "event", true},
		{constructApply, "state", true},
		{constructApply, "self", false},
		{constructValid, "value", true},
		{constructValid, "ok", true},
		{constructUseCaseExecute, "cmd", true},
		{constructCoerce, "value", true},
		{constructQuery, "self", false}, // Query só enxerga params/símbolos
	}
	for _, tc := range cases {
		if got := has(tc.c, tc.name); got != tc.grant {
			t.Errorf("construct %d, receptor %q: esperava %v, obtive %v", tc.c, tc.name, tc.grant, got)
		}
	}
}
