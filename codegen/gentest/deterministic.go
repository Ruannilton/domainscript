package gentest

import "bytes"

// Deterministic roda generate duas vezes e falha se os bytes produzidos
// diferirem: regenerar o mesmo programa deve produzir bytes idênticos
// (NFR-13).
func Deterministic(t TB, generate func() []byte) {
	t.Helper()
	first := generate()
	second := generate()
	if !bytes.Equal(first, second) {
		t.Fatalf("gentest: saída não é determinística entre duas gerações\n--- 1ª ---\n%s\n--- 2ª ---\n%s", first, second)
	}
}
