package parser

import "testing"

func TestEnsure(t *testing.T) {
	cases := map[string]string{
		"ensure wallet exists else WalletNotFound":     "(ensure (exists wallet) else WalletNotFound)",
		"ensure state.active == x else InactiveWallet": "(ensure (== (. state active) x) else InactiveWallet)",
		"ensure ok else Nop":                           "(ensure ok else Nop)",
		"ensure ok else break":                         "(ensure ok else (break))",
		"ensure ok else break all":                     "(ensure ok else (break-all))",
		"ensure ok else continue":                      "(ensure ok else (continue))",
	}
	for src, want := range cases {
		if got := sstmt(parseStmtOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

func TestReturn(t *testing.T) {
	cases := map[string]string{
		"return self.amount >= other.amount": "(return (>= (. self amount) (. other amount)))",
		"return signed_url(doc, expires: x)": "(return (call signed_url doc expires:x))",
	}
	for src, want := range cases {
		if got := sstmt(parseStmtOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
	// return sem valor (dentro de um bloco).
	blk := sstmt(parseStmtOK(t, "{ return }"))
	if blk != "(block (return))" {
		t.Errorf("=> %s, quero (block (return))", blk)
	}
}

func TestForAndRange(t *testing.T) {
	cases := map[string]string{
		"for ticket in availableTickets { ticket.Reserve(o) }": "(for ticket availableTickets (block (call (. ticket Reserve) o)))",
		"for i in 1..batch.quantity { emit X(i) }":             "(for i (.. 1 (. batch quantity)) (block (emit (call X i))))",
	}
	for src, want := range cases {
		if got := sstmt(parseStmtOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

func TestLog(t *testing.T) {
	got := sstmt(parseStmtOK(t, `log info "Saque realizado" { walletId = self.id amount = amount }`))
	want := `(log info "Saque realizado" walletId=(. self id) amount=amount)`
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestEmit(t *testing.T) {
	got := sstmt(parseStmtOK(t, "emit DepositPerformed(self.id, amount, description)"))
	want := "(emit (call DepositPerformed (. self id) amount description))"
	if got != want {
		t.Errorf("=> %s, quero %s", got, want)
	}
}

func TestLoopControlStandalone(t *testing.T) {
	cases := map[string]string{
		"break":     "(break)",
		"break all": "(break-all)",
		"continue":  "(continue)",
	}
	for src, want := range cases {
		if got := sstmt(parseStmtOK(t, src)); got != want {
			t.Errorf("%q => %s, quero %s", src, got, want)
		}
	}
}

// Recovery: 'else' ausente no ensure reporta erro sem travar.
func TestEnsureMissingElseRecovers(t *testing.T) {
	p, bag := mk("ensure cond")
	_ = p.parseStmt()
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico para 'else' ausente")
	}
}
