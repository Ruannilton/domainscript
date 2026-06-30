package parser

import (
	"testing"
	"time"

	"domainscript/diag"
	"domainscript/lexer"
)

// runPipeline lexa e parseia src de forma robusta: recupera qualquer panic e
// reporta-o como falha de teste, devolvendo o número de diagnósticos acumulados.
// É o ponto único onde a Fase 11 exercita a invariante NFR-2 (sem crash).
func runPipeline(t *testing.T, src string) int {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic em entrada robusta %q: %v", src, r)
		}
	}()
	toks, lexDiags := lexer.Lex(src)
	bag := diag.New()
	for _, d := range lexDiags {
		bag.Add(d)
	}
	file := Parse(toks, bag)
	if file == nil {
		t.Fatalf("Parse devolveu nil para %q (deve sempre devolver uma AST, REQ-2.7)", src)
	}
	return bag.Len()
}

// mustTerminate roda fn com um teto de tempo, garantindo ausência de laço infinito
// (NFR-2). Um parser que não garante progresso travaria aqui em vez de retornar.
func mustTerminate(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: não terminou em 5s — possível laço infinito (NFR-2)", name)
	}
}

// robustnessCorpus reúne entradas truncadas e adversárias: blocos abertos,
// delimitadores desbalanceados, keywords soltas, lixo de pontuação e construtos
// cortados no meio. Nenhuma pode causar crash nem laço (NFR-2).
var robustnessCorpus = []string{
	"",
	" ",
	"\n\n\n",
	"\t",
	"{",
	"}",
	"{{{{{{{{{{",
	"}}}}}}}}}}",
	"((((((((((",
	"))))))))))",
	"[[[[[[[[[[",
	"Aggregate",
	"Aggregate ",
	"Aggregate {",
	"Aggregate Wallet {",
	"Aggregate Wallet { state {",
	"Aggregate Wallet { state { balance",
	"Aggregate Wallet { state { balance Money",
	"ValueObject",
	"ValueObject Email(",
	"ValueObject Email(string",
	"ValueObject Email(string) {",
	"ValueObject Email(string) { Valid {",
	"Enum Status {",
	"Enum Status { Active Inactive",
	"UseCase",
	"UseCase Deposit handles",
	"UseCase Deposit handles DepositCmd {",
	"UseCase Deposit handles DepositCmd { execute {",
	"Policy on",
	"Policy OnPaid on {",
	"Saga mode",
	"Saga Order mode await {",
	"match",
	"match x {",
	"match x { Active =>",
	"for",
	"for t in",
	"for t in items {",
	"ensure",
	"ensure x else",
	"-> -> -> ->",
	". . . . .",
	", , , , ,",
	":::::",
	"== != >= <=",
	"+ - * / =",
	"1 2 3 4 5",
	"\"unterminated string",
	"\"escape \\",
	"v1 v2 v3 5s 300/min 100MB",
	"@#$%^&",
	"Aggregate \x00 Wallet",
	"Aggregate 你好 Wallet { state {",
	"Module { Database {",
	"Interface HTTP { GET",
	"Topology { services {",
	"Test { scenario {",
	"Command Cmd { field ref",
	"Worker every {",
	"Adapter HTTP {",
	"Notification {",
	"Foreign {",
	"Metric {",
	"Upcast {",
	"Query () -> { list",
	"View From {",
	"Projection {",
	"Error {",
	"Event {",
	"PublicEvent {",
}

// TestParserRobustness garante que toda entrada truncada ou adversária produz
// diagnósticos (ou silêncio) sem crash e sem laço infinito (NFR-2). É o teste
// central da robustez exigida pela §5.7 do Definition of Done.
func TestParserRobustness(t *testing.T) {
	for _, src := range robustnessCorpus {
		src := src
		mustTerminate(t, src, func() {
			runPipeline(t, src)
		})
	}
}

// TestParserRobustnessRepeatedKeywords estressa o reâncoramento de topo: uma
// sequência longa de keywords soltas exercita o caminho de recovery muitas vezes
// e deve terminar (garantia de progresso, REQ-3.6/NFR-2).
func TestParserRobustnessRepeatedKeywords(t *testing.T) {
	var sb []byte
	for i := 0; i < 2000; i++ {
		sb = append(sb, "Aggregate Command Event Policy UseCase Query "...)
	}
	mustTerminate(t, "keywords repetidas", func() {
		runPipeline(t, string(sb))
	})
}

// TestParserRobustnessDeepNesting garante terminação com aninhamento profundo de
// blocos abertos — um caminho clássico de estouro de pilha/laço se o recovery não
// fechar os níveis corretamente (NFR-2).
func TestParserRobustnessDeepNesting(t *testing.T) {
	var sb []byte
	for i := 0; i < 5000; i++ {
		sb = append(sb, '{')
	}
	mustTerminate(t, "aninhamento profundo", func() {
		runPipeline(t, string(sb))
	})
}

// FuzzParse é fuzzing leve (NFR-2): o motor gera mutações dos seeds e o pipeline
// nunca pode entrar em panic nem devolver AST nil. Rode com
// `go test ./parser/ -run=^$ -fuzz=FuzzParse` para explorar além dos seeds.
func FuzzParse(f *testing.F) {
	seeds := append([]string{
		`ValueObject Email(string) { Valid { ok } }`,
		`Aggregate Wallet { state { id WalletId } Handle Deposit { Apply } }`,
		`UseCase Deposit handles DepositCmd { execute { return } }`,
		`match status { Active => 1 Inactive => 2 }`,
		`Policy OnPaid on InvoicePaid { delivery AtLeastOnce execute { return } }`,
	}, robustnessCorpus...)
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic ao parsear %q: %v", src, r)
			}
		}()
		toks, _ := lexer.Lex(src)
		bag := diag.New()
		if file := Parse(toks, bag); file == nil {
			t.Fatalf("Parse devolveu nil para %q", src)
		}
	})
}
