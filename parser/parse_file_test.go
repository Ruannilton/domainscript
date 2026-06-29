package parser

import (
	"fmt"
	"testing"

	"domainscript/ast"
	"domainscript/diag"
	"domainscript/lexer"
)

func parseFileSrc(src string) (*ast.File, *diag.DiagnosticBag) {
	toks, _ := lexer.Lex(src)
	bag := diag.New()
	return Parse(toks, bag), bag
}

func TestParseFileMultiDecl(t *testing.T) {
	src := `
		ValueObject Email(string) { Valid { self.contains("@") } }
		Enum Priority : integer { Low = 1 High = 3 }
		Command C { walletId ref Wallet }
		Event E { id WalletId }
		Aggregate A { state { x Money } }
	`
	file, bag := parseFileSrc(src)
	if bag.Len() != 0 {
		t.Fatalf("diagnósticos inesperados: %s", bag.Render())
	}
	if len(file.Decls) != 5 {
		t.Fatalf("=> %d declarações, quero 5", len(file.Decls))
	}
	wantTypes := []string{"*ast.ValueObjectDecl", "*ast.EnumDecl", "*ast.CommandDecl", "*ast.EventDecl", "*ast.AggregateDecl"}
	for i, d := range file.Decls {
		if got := fmt.Sprintf("%T", d); got != wantTypes[i] {
			t.Errorf("decl[%d] = %s, quero %s", i, got, wantTypes[i])
		}
	}
}

// Reancoragem (REQ-3.7): lixo de topo e uma declaração quebrada não impedem o
// reconhecimento das declarações seguintes; cada uma é reportada independentemente.
func TestParseFileReanchors(t *testing.T) {
	src := `
		+ + +
		Command C { walletId ref Wallet }
		ValueObject V { Valid { ok } }
	`
	file, bag := parseFileSrc(src)
	if bag.Len() == 0 {
		t.Errorf("esperava diagnóstico para o lixo de topo")
	}
	// As duas declarações válidas após o lixo foram reconhecidas.
	var cmd, vo int
	for _, d := range file.Decls {
		switch d.(type) {
		case *ast.CommandDecl:
			cmd++
		case *ast.ValueObjectDecl:
			vo++
		}
	}
	if cmd != 1 || vo != 1 {
		t.Errorf("declarações após o lixo não foram reancoradas: cmd=%d vo=%d\n%s", cmd, vo, bag.Render())
	}
}

func TestParseFileEmpty(t *testing.T) {
	file, bag := parseFileSrc("   // só comentário\n")
	if bag.Len() != 0 {
		t.Errorf("arquivo vazio não deveria ter erros: %s", bag.Render())
	}
	if len(file.Decls) != 0 {
		t.Errorf("=> %d declarações, quero 0", len(file.Decls))
	}
}
