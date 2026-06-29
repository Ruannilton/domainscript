package sema

import (
	"strings"
	"testing"
)

const enumPreamble = `
	Enum Status : string { Open = "O" Closed = "C" Done = "D" }
`

func matchSrc(body string) string {
	return enumPreamble + `
		Aggregate Task {
			state { s Status }
			Handle Step(s Status) {
				` + body + `
			}
		}
	`
}

// REQ-5.5 (positivo): match sobre enum sem cobrir todos os membros é não-exaustivo.
func TestMatchNonExhaustiveFires(t *testing.T) {
	bag := checkSrc(t, matchSrc(`
		match s {
			Status.Open => s.touch()
			Status.Closed => s.touch()
		}
	`))
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "exaustivo") || !strings.Contains(r, "Done") {
		t.Fatalf("esperava erro de match não-exaustivo citando Done:\n%s", r)
	}
}

// REQ-5.5 (positivo): wildcard `_` sobre enum coberto é proibido.
func TestMatchWildcardOverEnumFires(t *testing.T) {
	bag := checkSrc(t, matchSrc(`
		match s {
			Status.Open => s.touch()
			_ => s.touch()
		}
	`))
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "_") {
		t.Fatalf("esperava erro de wildcard proibido sobre enum:\n%s", r)
	}
}

// REQ-5.5 (positivo): match com guards (when) sem `_` é erro.
func TestMatchGuardsWithoutWildcardFires(t *testing.T) {
	bag := checkSrc(t, matchSrc(`
		match s {
			x when x == s => s.touch()
		}
	`))
	r := bag.Render()
	if !bag.HasErrors() || !strings.Contains(r, "guards") {
		t.Fatalf("esperava erro de guards sem wildcard:\n%s", r)
	}
}

// REQ-5.5 (negativo): match exaustivo sobre enum, e match com guards + `_`, não
// disparam.
func TestMatchExhaustiveIsSilent(t *testing.T) {
	exhaustive := checkSrc(t, matchSrc(`
		match s {
			Status.Open => s.touch()
			Status.Closed => s.touch()
			Status.Done => s.touch()
		}
	`))
	mustBeSilent(t, exhaustive, "REQ-5.5 (exaustivo)")

	guarded := checkSrc(t, matchSrc(`
		match s {
			x when x == s => s.touch()
			_ => s.touch()
		}
	`))
	mustBeSilent(t, guarded, "REQ-5.5 (guards + wildcard)")
}
