package sema

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"domainscript/diag"
)

// rules_audit_test.go é a auditoria de cobertura da §23 (tarefa 11.4, NFR-4): um
// único registro, independente dos testes por-regra, que exercita CADA regra ❌/⚠️
// com um par positivo+negativo. Serve como checklist vivo do Definition of Done
// (requirements.md §5.2/5.3): se uma regra é removida, deixa de disparar, ou uma
// nova regra da §23 não tem par de testes, esta auditoria falha. Foi ela que
// revelou a ausência da regra REQ-5.18 ao fechar a Fase 11.

// auditCase é uma linha da auditoria: a regra, sua severidade e os fontes positivo
// (deve disparar) e negativo (deve silenciar), em forma single-file (pos/neg) ou de
// projeto multi-arquivo (posProj/negProj). expect é uma substring que o diagnóstico
// positivo deve conter, garantindo que é a regra certa que disparou.
type auditCase struct {
	req     string // "REQ-5.N"
	num     int    // N, para a checagem de completude 1..23
	warn    bool   // true: ⚠️ (positivo não pode ter erro); false: ❌
	expect  string
	pos     string
	neg     string
	posProj []projFile
	negProj []projFile
}

func (c auditCase) run(t *testing.T) {
	t.Helper()
	var posBag, negBag *diag.DiagnosticBag
	if c.posProj != nil {
		posBag = checkProject(t, c.posProj...)
		negBag = checkProject(t, c.negProj...)
	} else {
		posBag = checkSrc(t, c.pos)
		negBag = checkSrc(t, c.neg)
	}

	r := posBag.Render()
	if c.warn {
		if posBag.HasErrors() {
			t.Errorf("%s: caso positivo deveria ser apenas aviso, veio erro:\n%s", c.req, r)
		}
		if posBag.Len() == 0 {
			t.Errorf("%s: caso positivo não emitiu nenhum aviso", c.req)
		}
	} else if !posBag.HasErrors() {
		t.Errorf("%s: caso positivo não emitiu erro:\n%s", c.req, r)
	}
	if !strings.Contains(r, c.expect) {
		t.Errorf("%s: diagnóstico positivo não cita %q:\n%s", c.req, c.expect, r)
	}

	if negBag.Len() != 0 {
		t.Errorf("%s: caso negativo (código correto) gerou diagnósticos:\n%s", c.req, negBag.Render())
	}
}

// §23 errors ❌ (REQ-5.1–15) e warnings ⚠️ (REQ-5.16–23). Os fontes são os mesmos
// dos testes por-regra, reunidos aqui como fonte única de verdade da cobertura.
var auditCases = []auditCase{
	// ---- erros ❌ ----
	{
		req: "REQ-5.1", num: 1, expect: "Write Side",
		pos: `Command DepositCmd { amount decimal }`,
		neg: `
			ValueObject Money { amount decimal currency string Valid { ok } }
			ValueObject WalletId(string) { Valid { ok } }
			Command DepositCmd { walletId ref Wallet amount Money }
			Aggregate Wallet { state { id WalletId } }
		`,
	},
	{
		req: "REQ-5.2", num: 2, expect: "access",
		pos: `
			ValueObject WalletId(string) { Valid { ok } }
			Aggregate Wallet {
				state { id WalletId }
				access { Deposit requires caller.authenticated }
				Handle Deposit() { return }
				Handle Withdraw() { return }
			}
		`,
		neg: `
			ValueObject WalletId(string) { Valid { ok } }
			Aggregate Wallet {
				state { id WalletId }
				access {
					Deposit  requires caller.authenticated
					Withdraw requires caller.authenticated
				}
				Handle Deposit() { return }
				Handle Withdraw() { return }
			}
		`,
	},
	{
		req: "REQ-5.3", num: 3, expect: "Adapter",
		pos: `
			ValueObject Email(string) { Valid { ok } }
			Notification DepositNotification { to Email }
		`,
		neg: `
			ValueObject Email(string) { Valid { ok } }
			Notification DepositNotification { to Email }
			Adapter DepositNotification { mode async http POST "https://example.com" }
		`,
	},
	{
		req: "REQ-5.4", num: 4, expect: "AppendList",
		pos: `
			ValueObject Entry(string) { Valid { ok } }
			Aggregate Ledger {
				state { entries AppendList<Entry> }
				access { Wipe requires caller.authenticated }
				Handle Wipe() { state.entries.clear() }
			}
		`,
		neg: `
			ValueObject Entry(string) { Valid { ok } }
			Aggregate Ledger {
				state { entries AppendList<Entry> }
				access { Append requires caller.authenticated }
				Handle Append() { state.entries.add(Entry("x")) }
			}
		`,
	},
	{
		req: "REQ-5.5", num: 5, expect: "exaustivo",
		pos: matchSrc(`
			match s {
				Status.Open => s.touch()
				Status.Closed => s.touch()
			}
		`),
		neg: matchSrc(`
			match s {
				Status.Open => s.touch()
				Status.Closed => s.touch()
				Status.Done => s.touch()
			}
		`),
	},
	{
		req: "REQ-5.6", num: 6, expect: "Nop",
		pos: `
			Command Cmd { id WalletId }
			UseCase UC handles Cmd { execute { ensure cond else Nop } }
			ValueObject WalletId(string) { Valid { ok } }
		`,
		neg: `
			Error Inactive { message "x" }
			Aggregate Wallet {
				state { id WalletId }
				access { Freeze requires caller.authenticated }
				Handle Freeze() { ensure active else Inactive }
			}
			ValueObject WalletId(string) { Valid { ok } }
		`,
	},
	{
		req: "REQ-5.7", num: 7, expect: "break",
		pos: `
			Command Cmd { id Id }
			UseCase UC handles Cmd { execute { break } }
			ValueObject Id(string) { Valid { ok } }
		`,
		neg: `
			Command Cmd { id Id }
			UseCase UC handles Cmd {
				execute { for x in items { ensure x else continue break all } }
			}
			ValueObject Id(string) { Valid { ok } }
		`,
	},
	{
		req: "REQ-5.8", num: 8, expect: "PublicEvent",
		posProj: []projFile{
			pf(`Module Billing { }`, "billing", "mod.ds"),
			pf(`
				ValueObject OrderId(string) { Valid { ok } }
				Event InvoicePaid { id OrderId }
			`, "billing", "events.ds"),
			pf(`Module Shipping { }`, "shipping", "mod.ds"),
			pf(`Policy OnPaid on InvoicePaid { delivery AtLeastOnce execute { return } }`, "shipping", "policy.ds"),
		},
		negProj: []projFile{
			pf(`Module Billing { }`, "billing", "mod.ds"),
			pf(`
				ValueObject OrderId(string) { Valid { ok } }
				PublicEvent InvoicePaid { id OrderId }
			`, "billing", "events.ds"),
			pf(`Module Shipping { }`, "shipping", "mod.ds"),
			pf(`Policy OnPaid on InvoicePaid { delivery AtLeastOnce execute { return } }`, "shipping", "policy.ds"),
		},
	},
	{
		req: "REQ-5.9a", num: 9, expect: "cross-database",
		posProj: []projFile{
			pf(`Module Wallet {
				Database MainDb { provider: "pg" supportsXA: false manages: [Account] }
				Database SideDb { provider: "pg" supportsXA: false manages: [Ledger] }
			}`, "wallet", "mod.ds"),
			pf(`
				ValueObject AccId(string) { Valid { ok } }
				ValueObject LedId(string) { Valid { ok } }
				Aggregate Account { state { id AccId } }
				Aggregate Ledger { state { id LedId } }
				Command MoveCmd { from AccId }
				UseCase Move handles MoveCmd { execute { load Account(from) load Ledger(from) } }
			`, "wallet", "domain.ds"),
		},
		negProj: []projFile{
			pf(`Module Wallet {
				Database MainDb { provider: "pg" supportsXA: true manages: [Account] }
				Database SideDb { provider: "pg" supportsXA: true manages: [Ledger] }
			}`, "wallet", "mod.ds"),
			pf(`
				ValueObject AccId(string) { Valid { ok } }
				ValueObject LedId(string) { Valid { ok } }
				Aggregate Account { state { id AccId } }
				Aggregate Ledger { state { id LedId } }
				Command MoveCmd { from AccId }
				UseCase Move handles MoveCmd { execute { load Account(from) load Ledger(from) } }
			`, "wallet", "domain.ds"),
			pf(`Interface HTTP { POST "/move" -> Move }`, "wallet", "interface.ds"),
		},
	},
	{
		req: "REQ-5.9b", num: 9, expect: "cross-service",
		posProj: []projFile{
			pf(`Module Wallet { Database WDb { provider: "pg" supportsXA: true manages: [Account] } }`, "wallet", "mod.ds"),
			pf(`
				ValueObject AccId(string) { Valid { ok } }
				Aggregate Account { state { id AccId } }
				Command MoveCmd { from AccId }
				UseCase Move handles MoveCmd {
					tenancy: cross_tenant
					execute { load Account(from) load Entry(from) }
				}
			`, "wallet", "domain.ds"),
			pf(`Module Ledger { Database LDb { provider: "pg" supportsXA: true manages: [Entry] } }`, "ledger", "mod.ds"),
			pf(`
				ValueObject EntId(string) { Valid { ok } }
				Aggregate Entry { state { id EntId } }
			`, "ledger", "domain.ds"),
			pf(`Topology {
				services { A { modules: [Wallet] } B { modules: [Ledger] } }
				channels { Wallet -> Ledger { via: grpc } }
			}`, "topology.ds"),
		},
		negProj: []projFile{
			pf(`Module Wallet { Database WDb { provider: "pg" supportsXA: true manages: [Account] } }`, "wallet", "mod.ds"),
			pf(`
				ValueObject AccId(string) { Valid { ok } }
				Aggregate Account { state { id AccId } }
				Command MoveCmd { from AccId }
				Saga Move handles MoveCmd {
					mode async
					state { id AccId }
					step Do { up { load Account(from) load Entry(from) } }
				}
			`, "wallet", "domain.ds"),
			pf(`Module Ledger { Database LDb { provider: "pg" supportsXA: true manages: [Entry] } }`, "ledger", "mod.ds"),
			pf(`
				ValueObject EntId(string) { Valid { ok } }
				Aggregate Entry { state { id EntId } }
			`, "ledger", "domain.ds"),
			pf(`Topology {
				services { A { modules: [Wallet] } B { modules: [Ledger] } }
				channels { Wallet -> Ledger { via: grpc } }
			}`, "topology.ds"),
		},
	},
	{
		req: "REQ-5.10", num: 10, expect: "JOIN cross-database",
		posProj: []projFile{
			pf(`Module Shop {
				Database TicketDb { provider: "pg" manages: [Ticket] }
				Database OrderDb { provider: "pg" manages: [Order] }
			}`, "shop", "mod.ds"),
			pf(`
				ValueObject TicketId(string) { Valid { ok } }
				ValueObject OrderId(string) { Valid { ok } }
				Aggregate Ticket { state { id TicketId } }
				Aggregate Order { state { id OrderId } }
				Query FindTickets() -> List<Ticket> { list Ticket t join Order o on t.orderId == o.id }
			`, "shop", "domain.ds"),
		},
		negProj: []projFile{
			pf(`Module Shop { Database ShopDb { provider: "pg" manages: [Ticket, Order] } }`, "shop", "mod.ds"),
			pf(`
				ValueObject TicketId(string) { Valid { ok } }
				ValueObject OrderId(string) { Valid { ok } }
				Aggregate Ticket { state { id TicketId } }
				Aggregate Order { state { id OrderId } }
				Query FindTickets() -> List<Ticket> { list Ticket t join Order o on t.orderId == o.id }
			`, "shop", "domain.ds"),
			pf(`Interface HTTP { GET "/tickets" -> FindTickets }`, "shop", "interface.ds"),
		},
	},
	{
		req: "REQ-5.11", num: 11, expect: "sem canal",
		posProj: []projFile{
			pf(`Module Billing { }`, "billing", "mod.ds"),
			pf(`
				ValueObject OrderId(string) { Valid { ok } }
				PublicEvent OrderPlaced { id OrderId }
			`, "billing", "events.ds"),
			pf(`Module Shipping { }`, "shipping", "mod.ds"),
			pf(`Policy NotifyShip on OrderPlaced { delivery AtLeastOnce execute { return } }`, "shipping", "policy.ds"),
			pf(`Topology { services { A { modules: [Billing] } B { modules: [Shipping] } } }`, "topology.ds"),
		},
		negProj: []projFile{
			pf(`Module Billing { }`, "billing", "mod.ds"),
			pf(`
				ValueObject OrderId(string) { Valid { ok } }
				PublicEvent OrderPlaced { id OrderId }
			`, "billing", "events.ds"),
			pf(`Module Shipping { }`, "shipping", "mod.ds"),
			pf(`Policy NotifyShip on OrderPlaced { delivery AtLeastOnce execute { return } }`, "shipping", "policy.ds"),
			pf(`Topology {
				services { A { modules: [Billing] } B { modules: [Shipping] } }
				channels { Billing -> Shipping { via: queue orderBy: id } }
			}`, "topology.ds"),
		},
	},
	{
		req: "REQ-5.12", num: 12, expect: "cross_tenant",
		posProj: []projFile{
			pf(`Module Orders { }`, "orders", "mod.ds"),
			pf(`
				ValueObject Ref(string) { Valid { ok } }
				Command ReportCmd { r Ref }
				UseCase Report handles ReportCmd { execute { list Wallet take 10 } }
			`, "orders", "domain.ds"),
			pf(`Module Wallet { }`, "wallet", "mod.ds"),
			pf(`
				ValueObject WalletId(string) { Valid { ok } }
				Aggregate Wallet { state { id WalletId } }
			`, "wallet", "domain.ds"),
			pf(`Topology { services { S { modules: [Orders, Wallet] } } }`, "topology.ds"),
		},
		negProj: []projFile{
			pf(`Module Orders { }`, "orders", "mod.ds"),
			pf(`
				ValueObject Ref(string) { Valid { ok } }
				ValueObject OrderId(string) { Valid { ok } }
				Aggregate Order { state { id OrderId } }
				Command ReportCmd { r Ref }
				UseCase Report handles ReportCmd { execute { list Order take 10 } }
			`, "orders", "domain.ds"),
			pf(`Interface HTTP { GET "/report" -> Report }`, "orders", "interface.ds"),
		},
	},
	{
		req: "REQ-5.13", num: 13, expect: "note",
		pos: `
			ValueObject Money(integer) { Valid { ok } }
			ValueObject Note(string) { Valid { ok } }
			Command DepositCmd { amount Money, note Note }
			Version v1 { upcast DepositCmd { from { value integer } to { amount = Money(value) } } }
		`,
		neg: `
			ValueObject Money(integer) { Valid { ok } }
			ValueObject Note(string) { Valid { ok } }
			Command DepositCmd { amount Money, note Note }
			Version v1 {
				upcast DepositCmd {
					from { value integer }
					to { amount = Money(value) note = Note("legacy") }
				}
			}
		`,
	},
	{
		req: "REQ-5.14", num: 14, expect: "WalletCreatedTYPO",
		pos: `
			ValueObject WalletId(string) { Valid { ok } }
			Event WalletCreated { id WalletId }
			Command Withdraw { id WalletId }
			Test Wallet {
				scenario "saque" {
					given [ WalletCreatedTYPO(id: "W1") ]
					when Withdraw(id: "W1")
				}
			}
		`,
		neg: `
			ValueObject WalletId(string) { Valid { ok } }
			Event WalletCreated { id WalletId }
			Command Withdraw { id WalletId }
			Test Wallet {
				scenario "saque" {
					given [ WalletCreated(id: "W1") ]
					when Withdraw(id: "W1")
				}
			}
		`,
	},
	{
		req: "REQ-5.15", num: 15, expect: "ComputeMerkleRoot",
		pos: `
			ValueObject WalletId(string) { Valid { ok } }
			Foreign "go" from "internal/crypto" { function ComputeMerkleRoot(items List<bytes>) -> bytes }
			Aggregate Wallet {
				state { id WalletId }
				access { Seal requires ok }
				Handle Seal() { hash = ComputeMerkleRoot(a, b) }
			}
		`,
		neg: `
			ValueObject WalletId(string) { Valid { ok } }
			Foreign "go" from "internal/crypto" { function ComputeMerkleRoot(items List<bytes>) -> bytes }
			Aggregate Wallet {
				state { id WalletId }
				access { Seal requires ok }
				Handle Seal() { hash = ComputeMerkleRoot(items) }
			}
		`,
	},

	// ---- avisos ⚠️ ----
	{
		req: "REQ-5.16", num: 16, warn: true, expect: "orderBy",
		pos: `
			Topology {
				services { A { modules: [M1] } B { modules: [M2] } }
				channels { A -> B { via: queue provider: "rabbitmq" } }
			}
		`,
		neg: `
			Topology {
				services { A { modules: [M1] } B { modules: [M2] } }
				channels { A -> B { via: queue orderBy: aggregateId } B -> A { via: grpc } }
			}
		`,
	},
	{
		req: "REQ-5.17", num: 17, warn: true, expect: "await",
		posProj: []projFile{
			pf(`Module Orders { }`, "orders", "mod.ds"),
			pf(`
				ValueObject Ref(string) { Valid { ok } }
				Command PurchaseCmd { r Ref }
				Saga Purchase handles PurchaseCmd {
					mode await timeout 60s
					state { id Ref }
					step Do { up { return } }
				}
			`, "orders", "saga.ds"),
			pf(`Module Payments { }`, "payments", "mod.ds"),
			pf(`Topology {
				services { A { modules: [Orders] } B { modules: [Payments] } }
				channels { Orders -> Payments { via: queue orderBy: id } }
			}`, "topology.ds"),
		},
		negProj: []projFile{
			pf(`Module Orders { }`, "orders", "mod.ds"),
			pf(`
				ValueObject Ref(string) { Valid { ok } }
				Command PurchaseCmd { r Ref }
				Saga Purchase handles PurchaseCmd {
					mode await timeout 60s
					state { id Ref }
					step Do { up { return } }
				}
			`, "orders", "saga.ds"),
			pf(`Module Payments { }`, "payments", "mod.ds"),
			pf(`Topology {
				services { A { modules: [Orders] } B { modules: [Payments] } }
				channels { Orders -> Payments { via: grpc } }
			}`, "topology.ds"),
		},
	},
	{
		req: "REQ-5.18", num: 18, warn: true, expect: "default",
		pos: `Upcast DepositPerformed v1 -> v2 { channel = Channel("unknown") }`,
		neg: `Upcast TransferSent v1 -> v2 { fee = Money(amount: 0, currency: event.amount.currency) }`,
	},
	{
		req: "REQ-5.19", num: 19, warn: true, expect: "Enum",
		pos: `ValueObject Color(string) { Valid { value == "red" or value == "green" or value == "blue" } }`,
		neg: `ValueObject Email(string) { Valid { value.contains("@") } }`,
	},
	{
		req: "REQ-5.20", num: 20, warn: true, expect: "cardinalidade",
		pos: `
			ValueObject WalletId(string) { Valid { ok } }
			View TicketVW { id WalletId }
			Query GetMany(id WalletId) -> List<TicketVW> { cache { ttl: 5min } return list Ticket }
		`,
		neg: `
			ValueObject WalletId(string) { Valid { ok } }
			View WalletSummaryVW { id WalletId }
			Query GetOne(id WalletId) -> WalletSummaryVW { cache { ttl: 5min } return load Wallet(id) as WalletSummaryVW }
			Query ListAll(id WalletId) -> List<WalletSummaryVW> { return list Wallet }
		`,
	},
	{
		req: "REQ-5.21", num: 21, warn: true, expect: "auditoria",
		pos: `
			ValueObject Ref(string) { Valid { ok } }
			Command C { r Ref }
			UseCase U handles C { tenancy: cross_tenant execute { return } }
		`,
		neg: `
			ValueObject Ref(string) { Valid { ok } }
			Command C { r Ref }
			UseCase U handles C { execute { return } }
		`,
	},
	{
		req: "REQ-5.22", num: 22, warn: true, expect: "cobertura",
		pos: `
			ValueObject WalletId(string) { Valid { ok } }
			ValueObject Money(integer) { Valid { ok } }
			Error InsufficientBalance { message "x" }
			Event WithdrawalPerformed { id WalletId }
			Command Withdraw { amount Money }
			Aggregate Wallet {
				state { id WalletId }
				access { Withdraw requires ok }
				Handle Withdraw(amount Money) { ensure cond else InsufficientBalance }
			}
			Test Wallet {
				scenario "saque ok" {
					when Withdraw(amount: Money(10))
					then [ WithdrawalPerformed(id: "W1") ]
				}
			}
		`,
		neg: `
			ValueObject WalletId(string) { Valid { ok } }
			ValueObject Money(integer) { Valid { ok } }
			Error InsufficientBalance { message "x" }
			Event WithdrawalPerformed { id WalletId }
			Command Withdraw { amount Money }
			Aggregate Wallet {
				state { id WalletId }
				access { Withdraw requires ok }
				Handle Withdraw(amount Money) { ensure cond else InsufficientBalance }
			}
			Test Wallet {
				scenario "saque ok" {
					when Withdraw(amount: Money(10))
					then [ WithdrawalPerformed(id: "W1") ]
				}
				scenario "saldo insuficiente" {
					when Withdraw(amount: Money(50))
					then error InsufficientBalance
				}
			}
		`,
	},
	{
		req: "REQ-5.23", num: 23, warn: true, expect: "não é exposto",
		posProj: []projFile{
			pf(`Module Shop { }`, "shop", "mod.ds"),
			pf(`
				ValueObject OrderId(string) { Valid { ok } }
				Aggregate Order { state { id OrderId } }
				Query ListOrders() -> List<Order> { list Order take 10 }
			`, "shop", "domain.ds"),
		},
		negProj: []projFile{
			pf(`Module Shop { }`, "shop", "mod.ds"),
			pf(`
				ValueObject OrderId(string) { Valid { ok } }
				Aggregate Order { state { id OrderId } }
				Query ListOrders() -> List<Order> { list Order take 10 }
			`, "shop", "domain.ds"),
			pf(`Interface HTTP { GET "/orders" -> ListOrders }`, "shop", "interface.ds"),
		},
	},
}

// TestRuleCoverageAudit roda o par positivo+negativo de cada regra da §23 (NFR-4).
func TestRuleCoverageAudit(t *testing.T) {
	for _, c := range auditCases {
		c := c
		t.Run(c.req, func(t *testing.T) { c.run(t) })
	}
}

// TestRuleCoverageComplete garante que a auditoria cobre TODAS as regras numeradas
// da §23 (REQ-5.1 a REQ-5.23): se o spec ganhar uma regra e ninguém adicionar o par
// de testes, esta verificação falha. É a trava do Definition of Done §5.2/5.3.
func TestRuleCoverageComplete(t *testing.T) {
	const total = 23
	seen := make(map[int]bool, total)
	reqRe := regexp.MustCompile(`^REQ-5\.(\d+)[a-z]?$`)
	for _, c := range auditCases {
		m := reqRe.FindStringSubmatch(c.req)
		if m == nil {
			t.Errorf("rótulo de regra mal-formado: %q", c.req)
			continue
		}
		n, _ := strconv.Atoi(m[1])
		if n != c.num {
			t.Errorf("%s: campo num=%d não bate com o rótulo", c.req, c.num)
		}
		seen[c.num] = true
	}
	var missing []int
	for n := 1; n <= total; n++ {
		if !seen[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("regras da §23 sem par de testes na auditoria: REQ-5.%v (esperava 1..%d completas)", missing, total)
	}
}
