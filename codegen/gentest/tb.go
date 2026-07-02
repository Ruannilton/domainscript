package gentest

// TB é o subconjunto de testing.TB usado pelos helpers deste pacote. *testing.T
// e *testing.B já o satisfazem estruturalmente; a interface existe para
// permitir, nos testes do próprio gentest, um fake que observa Fatalf sem
// terminar o processo de teste — testing.TB não pode ser implementada fora do
// pacote testing (tem um método não-exportado).
type TB interface {
	Helper()
	Fatalf(format string, args ...any)
	TempDir() string
}
