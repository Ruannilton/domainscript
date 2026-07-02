package codegen_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"

	"domainscript/ast"
	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/codegen/rtsrc"
	"domainscript/diag"
	"domainscript/lexer"
	"domainscript/parser"
	"domainscript/resolver"
	"domainscript/sema"
	"domainscript/symbols"
	"domainscript/types"
)

// decl_command_test.go prova os critérios de conclusão da task E7.1 (§design
// codegen 3.5/3.8, REQ-20.1) sobre os Commands reais do wallet
// (docs/examples/wallet/application.ds): golden, determinismo, smoke compile
// e um teste comportamental (round-trip JSON) sobre o Go de fato gerado —
// mesmo padrão de decl_event_test.go/decl_aggregate_test.go. Reusa
// parseWalletProgram (decl_aggregate_test.go, mesmo pacote codegen_test):
// EmitCommand/EmitCommands precisam de um types.Model + symbols.SymbolTable
// RESOLVIDOS para achar o tipo Go do campo "id" do Aggregate referenciado por
// "ref" (§design 3.8) — a mesma necessidade de EmitAggregate.

// parseWalletCommands acha todos os CommandDecl do projeto wallet, na ordem
// de declaração de application.ds (Deposit, Withdraw) — ordenando os
// caminhos de arquivo primeiro (determinismo, NFR-13; program.Program.Files é
// um map, sem ordem própria).
func parseWalletCommands(t *testing.T) []*ast.CommandDecl {
	t.Helper()
	prog := parseWalletProgram(t)

	paths := make([]string, 0, len(prog.Files))
	for path := range prog.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var cmds []*ast.CommandDecl
	for _, path := range paths {
		for _, d := range prog.Files[path].Decls {
			if c, ok := d.(*ast.CommandDecl); ok {
				cmds = append(cmds, c)
			}
		}
	}
	return cmds
}

// emitWalletCommands monta o Model+SymbolTable sobre o programa real e gera
// o Go dos 2 Commands do wallet — o caminho comum à maioria dos testes deste
// arquivo.
func emitWalletCommands(t *testing.T) []byte {
	t.Helper()
	prog := parseWalletProgram(t)
	decls := parseWalletCommands(t)
	model := types.NewModel(prog.Symbols)

	got, err := codegen.EmitCommands("wallet", decls, model, prog.Symbols, "Wallet")
	if err != nil {
		t.Fatalf("EmitCommands: erro inesperado: %v", err)
	}
	return got
}

// TestEmitCommandsGolden gera o Go dos 2 Commands reais do wallet
// (Deposit/Withdraw) num único arquivo e compara byte a byte com o artefato
// versionado (REQ-20.1) — confirma, junto do golden, que o campo WalletId
// (de "walletId ref Wallet") tem tipo Go WalletId (o tipo do "id" do state de
// Wallet), não Wallet (§design 3.8).
func TestEmitCommandsGolden(t *testing.T) {
	decls := parseWalletCommands(t)
	if len(decls) != 2 {
		t.Fatalf("esperava 2 Commands em wallet/application.ds, achei %d", len(decls))
	}
	if decls[0].Name != "Deposit" || decls[1].Name != "Withdraw" {
		t.Fatalf("ordem inesperada dos Commands: got [%s, %s], want [Deposit, Withdraw]", decls[0].Name, decls[1].Name)
	}

	got := emitWalletCommands(t)
	gentest.Golden(t, filepath.Join("testdata", "commands_wallet.go.golden"), got)
}

// TestEmitCommandGoldenSingle gera o Go de um único Command (Deposit) via
// EmitCommand e compara com um segundo artefato versionado — prova que a
// forma "um de cada vez" também é suportada e estável (mesmo contrato de
// EmitEvent/EmitEvents, decl_event_test.go).
func TestEmitCommandGoldenSingle(t *testing.T) {
	prog := parseWalletProgram(t)
	decls := parseWalletCommands(t)
	model := types.NewModel(prog.Symbols)

	var deposit *ast.CommandDecl
	for _, d := range decls {
		if d.Name == "Deposit" {
			deposit = d
		}
	}
	if deposit == nil {
		t.Fatal("Command Deposit não encontrado em wallet/application.ds")
	}

	got, err := codegen.EmitCommand("wallet", deposit, model, prog.Symbols, "Wallet")
	if err != nil {
		t.Fatalf("EmitCommand(Deposit): erro inesperado: %v", err)
	}
	gentest.Golden(t, filepath.Join("testdata", "command_deposit.go.golden"), got)
}

// TestEmitCommandsDeterministic prova NFR-13: gerar os mesmos 2 Commands duas
// vezes produz bytes idênticos.
func TestEmitCommandsDeterministic(t *testing.T) {
	gentest.Deterministic(t, func() []byte {
		return emitWalletCommands(t)
	})
}

// commandsSmokeFiles monta o conjunto de arquivos usado pelo smoke compile e
// pelo teste de comportamento: go.mod, o runtime vendorado real (rtsrc), os
// VOs que os 2 Commands do wallet referenciam (WalletId — o tipo do campo
// "ref Wallet"; Money e TransactionDescription — os demais campos), os
// Errors que os operadores de Money referenciam (CurrencyMismatch/
// NegativeResult, via EmitErrors, já existe) e os 2 Commands em lote. Nota do
// prompt da task: NÃO precisa emitir o Aggregate Wallet inteiro — o campo
// gerado é do tipo WalletId (um ValueObject próprio), não Wallet.
func commandsSmokeFiles(t *testing.T) map[string][]byte {
	t.Helper()
	prog := parseWalletProgram(t)
	decls := parseWalletCommands(t)
	model := types.NewModel(prog.Symbols)

	vos := parseWalletVOs(t)
	errs := parseWalletErrors(t)

	files := map[string][]byte{"go.mod": []byte(smokeGoMod)}

	srcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources(): %v", err)
	}
	for name, content := range srcs {
		files[filepath.Join("runtime", name)] = content
	}

	voFiles := []struct{ name, file string }{
		{"WalletId", "wallet_id.go"},
		{"Money", "money.go"},
		{"TransactionDescription", "transaction_description.go"},
	}
	for _, spec := range voFiles {
		decl, ok := vos[spec.name]
		if !ok {
			t.Fatalf("ValueObject %s não encontrado em wallet/domain.ds", spec.name)
		}
		got, err := codegen.EmitValueObject("wallet", decl)
		if err != nil {
			t.Fatalf("EmitValueObject(%s): erro inesperado: %v", spec.name, err)
		}
		files[filepath.Join("wallet", spec.file)] = got
	}

	errsGo, err := codegen.EmitErrors("wallet", errs)
	if err != nil {
		t.Fatalf("EmitErrors: erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "errors.go")] = errsGo

	cmdsGo, err := codegen.EmitCommands("wallet", decls, model, prog.Symbols, "Wallet")
	if err != nil {
		t.Fatalf("EmitCommands: erro inesperado: %v", err)
	}
	files[filepath.Join("wallet", "commands.go")] = cmdsGo

	return files
}

// TestEmitCommandsSmokeCompile prova NFR-14: o Go gerado dos 2 Commands do
// wallet, junto dos VOs/Errors que referencia e do runtime vendorado real,
// compila e passa go vet num projeto isolado.
func TestEmitCommandsSmokeCompile(t *testing.T) {
	gentest.SmokeCompile(t, commandsSmokeFiles(t))
}

// walletCommandsBehaviorTest roda dentro do projeto isolado gerado no smoke e
// prova, sobre o Go de fato gerado (não uma reimplementação): (a) um Deposit
// construído com campos nomeados compila e o campo WalletId tem tipo Go
// WalletId (checagem em tempo de compilação); (b) round-trip encoding/json
// preserva os 3 campos — Commands cruzam a borda HTTP como JSON (E9.2), então
// precisam do mesmo contrato de serialização que Events.
const walletCommandsBehaviorTest = `package wallet

import (
	"encoding/json"
	"testing"

	"domainscript/generated/runtime"
)

// var estático: se WalletId não fosse o tipo do campo (ex. se tivesse ficado
// "Wallet", o nome do Aggregate), esta linha não compilaria.
var _ WalletId = Deposit{}.WalletId

func newTestDeposit(t *testing.T) Deposit {
	t.Helper()
	amount, err := runtime.ParseDecimal("10.00")
	if err != nil {
		t.Fatal(err)
	}
	money, err := NewMoney(amount, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("compra")
	if err != nil {
		t.Fatal(err)
	}
	return Deposit{
		WalletId:    WalletId("W1"),
		Amount:      money,
		Description: desc,
	}
}

func TestDepositCommandJSONRoundTrip(t *testing.T) {
	cmd := newTestDeposit(t)

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got Deposit
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.WalletId != cmd.WalletId {
		t.Fatalf("WalletId não sobreviveu ao round-trip: got %v, want %v", got.WalletId, cmd.WalletId)
	}
	if got.Amount.Currency != cmd.Amount.Currency {
		t.Fatalf("Amount.Currency não sobreviveu ao round-trip: got %v, want %v", got.Amount.Currency, cmd.Amount.Currency)
	}
	if got.Amount.Amount.Cmp(cmd.Amount.Amount) != 0 {
		t.Fatalf("Amount.Amount não sobreviveu ao round-trip: got %s, want %s", got.Amount.Amount, cmd.Amount.Amount)
	}
	if got.Description != cmd.Description {
		t.Fatalf("Description não sobreviveu ao round-trip: got %v, want %v", got.Description, cmd.Description)
	}
}

func TestWithdrawCommandJSONRoundTrip(t *testing.T) {
	amount, err := runtime.ParseDecimal("5.00")
	if err != nil {
		t.Fatal(err)
	}
	money, err := NewMoney(amount, "BRL")
	if err != nil {
		t.Fatal(err)
	}
	desc, err := NewTransactionDescription("saque")
	if err != nil {
		t.Fatal(err)
	}
	cmd := Withdraw{
		WalletId:    WalletId("W1"),
		Amount:      money,
		Description: desc,
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var got Withdraw
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got.WalletId != cmd.WalletId {
		t.Fatalf("WalletId não sobreviveu ao round-trip: got %v, want %v", got.WalletId, cmd.WalletId)
	}
	if got.Amount.Currency != cmd.Amount.Currency || got.Amount.Amount.Cmp(cmd.Amount.Amount) != 0 {
		t.Fatalf("Amount não sobreviveu ao round-trip: got %+v, want %+v", got.Amount, cmd.Amount)
	}
	if got.Description != cmd.Description {
		t.Fatalf("Description não sobreviveu ao round-trip: got %v, want %v", got.Description, cmd.Description)
	}
}
`

// TestEmitCommandsBehavior prova NFR-15 sobre o Go de fato gerado: escreve os
// mesmos arquivos do smoke mais um teste Go comportamental num diretório
// isolado e roda `go test ./...` de verdade.
func TestEmitCommandsBehavior(t *testing.T) {
	files := commandsSmokeFiles(t)
	files[filepath.Join("wallet", "commands_behavior_test.go")] = []byte(walletCommandsBehaviorTest)

	dir := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("não consegui criar %q: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatalf("não consegui escrever %q: %v", path, err)
		}
	}

	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("`go test ./...` falhou em %q: %v\n%s", dir, err, out)
	}
}

// TestEmitCommandRejectsUnmappableFieldType prova que um CommandDecl com um
// campo normal (sem ref) cujo TypeRef não é resolvível (goname.GoFieldType
// falha) devolve um erro de geração claro, nunca panic — mesmo caso
// defensivo de TestEmitEventRejectsUnmappableFieldType (decl_event_test.go).
// model/tab/module ficam nil/vazio: esse ramo de erro nunca os toca.
func TestEmitCommandRejectsUnmappableFieldType(t *testing.T) {
	badField := ast.NewField("bad", nil, false, false, nil, ast.Span{})
	bad := ast.NewCommandDecl("Bad", []*ast.Field{badField}, ast.Span{})
	if _, err := codegen.EmitCommand("wallet", bad, nil, nil, ""); err == nil {
		t.Fatal("esperava erro de geração para campo com TypeRef nulo")
	}
}

// TestEmitCommandRefFieldUnknownAggregateFailsExplicitly prova que um campo
// "ref" apontando para um nome que não resolve a símbolo nenhum (não
// deveria acontecer sobre um programa validado — REQ-9 já garantiu a
// resolução de nomes) devolve um erro de geração claro, não panic (REQ-14.4).
func TestEmitCommandRefFieldUnknownAggregateFailsExplicitly(t *testing.T) {
	prog := parseWalletProgram(t)
	model := types.NewModel(prog.Symbols)

	refField := ast.NewField("target", ast.NewTypeRef("Ghost", nil, ast.Span{}), true, false, nil, ast.Span{})
	bad := ast.NewCommandDecl("Bad", []*ast.Field{refField}, ast.Span{})

	if _, err := codegen.EmitCommand("wallet", bad, model, prog.Symbols, "Wallet"); err == nil {
		t.Fatal("esperava erro de geração para ref a um nome desconhecido")
	}
}

// TestEmitCommandRefFieldNonAggregateFailsExplicitly prova que um campo
// "ref" apontando para um símbolo que existe mas não é um Aggregate (ex. um
// ValueObject) devolve um erro de geração claro — o front-end (REQ-4) só
// valida que "ref" nomeia ALGO existente; a checagem "é mesmo um Aggregate" é
// deste emissor (§design 3.8).
func TestEmitCommandRefFieldNonAggregateFailsExplicitly(t *testing.T) {
	prog := parseWalletProgram(t)
	model := types.NewModel(prog.Symbols)

	refField := ast.NewField("target", ast.NewTypeRef("Money", nil, ast.Span{}), true, false, nil, ast.Span{})
	bad := ast.NewCommandDecl("Bad", []*ast.Field{refField}, ast.Span{})

	if _, err := codegen.EmitCommand("wallet", bad, model, prog.Symbols, "Wallet"); err == nil {
		t.Fatal("esperava erro de geração para ref a um símbolo que não é Aggregate (Money é ValueObject)")
	}
}

// noIDAggregateSource é uma fixture SINTÉTICA (não o wallet real): um
// Aggregate sem campo "id" no state, referenciado por "ref" num Command — o
// caso defensivo do item 5 do prompt da task E7.1. O front-end não impõe
// estaticamente que todo Aggregate declare "id" (nenhuma regra de sema.rules
// exige isso hoje), então esse programa passa limpo pelo front-end; é o
// gerador quem precisa se defender aqui (REQ-14.4), já que em todo Aggregate
// real do spec a identidade é obrigatória por convenção (§4.5), não por
// checagem estática.
const noIDAggregateSource = `
ValueObject Foo(string) {
    Valid { value.length() > 0 }
}

Event Touched { note Foo }

Aggregate NoId {
    state {
        name Foo
    }

    access {
        Touch requires caller.authenticated
    }

    Handle Touch(note Foo) {
        emit Touched(note)
    }

    Apply Touched {
        state.name = event.note
    }
}

Command DoTouch {
    target ref NoId
}
`

// buildNoIDAggregateProgram roda o mesmo pipeline de driver.CheckSource
// (lexer → parser → resolver → sema) sobre noIDAggregateSource, mas SEM
// passar por driver.CheckSource — esta função precisa da SymbolTable
// (resolver.Resolve), que CheckSource não expõe no seu contrato público.
func buildNoIDAggregateProgram(t *testing.T) (*ast.File, *symbols.SymbolTable) {
	t.Helper()
	bag := diag.New()
	toks, lexDiags := lexer.Lex(noIDAggregateSource)
	for _, d := range lexDiags {
		bag.Add(d)
	}
	file := parser.Parse(toks, bag)
	tab := resolver.Resolve(file, bag)
	sema.Check(tab, file, bag)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética noIDAggregateSource não deveria ter diagnósticos de erro:\n%s", bag.Render())
	}
	return file, tab
}

// TestEmitCommandRefFieldMissingAggregateIDFailsExplicitly prova o item 5 do
// prompt da task E7.1: um campo "ref" apontando para um Aggregate SEM campo
// "id" declarado em seu state devolve um erro de geração claro, nunca panic.
func TestEmitCommandRefFieldMissingAggregateIDFailsExplicitly(t *testing.T) {
	file, tab := buildNoIDAggregateProgram(t)
	model := types.NewModel(tab)

	var cmd *ast.CommandDecl
	for _, d := range file.Decls {
		if c, ok := d.(*ast.CommandDecl); ok && c.Name == "DoTouch" {
			cmd = c
		}
	}
	if cmd == nil {
		t.Fatal("Command DoTouch não encontrado na fixture sintética")
	}

	if _, err := codegen.EmitCommand("m", cmd, model, tab, ""); err == nil {
		t.Fatal(`esperava erro de geração para ref a Aggregate sem campo "id" em state`)
	}
}
