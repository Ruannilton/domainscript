package sema

import (
	"strings"
	"testing"
)

// REQ-5.7 (positivo): break/continue fora de for disparam erro.
func TestLoopControlOutsideForFires(t *testing.T) {
	brk := checkSrc(t, `
		Command Cmd { id Id }
		UseCase UC handles Cmd { execute { break } }
		ValueObject Id(string) { Valid { ok } }
	`)
	if !brk.HasErrors() || !strings.Contains(brk.Render(), "break") {
		t.Fatalf("esperava erro de break fora de for:\n%s", brk.Render())
	}

	cont := checkSrc(t, `
		Command Cmd { id Id }
		UseCase UC handles Cmd { execute { continue } }
		ValueObject Id(string) { Valid { ok } }
	`)
	if !cont.HasErrors() || !strings.Contains(cont.Render(), "continue") {
		t.Fatalf("esperava erro de continue fora de for:\n%s", cont.Render())
	}
}

// REQ-5.7 (negativo): break/continue dentro de for não disparam.
func TestLoopControlInsideForIsSilent(t *testing.T) {
	bag := checkSrc(t, `
		Command Cmd { id Id }
		UseCase UC handles Cmd {
			execute {
				for x in items {
					ensure x else continue
					break all
				}
			}
		}
		ValueObject Id(string) { Valid { ok } }
	`)
	mustBeSilent(t, bag, "REQ-5.7")
}
