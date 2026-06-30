package main

import (
	"fmt"
	"io"
	"os"

	"domainscript/diag"
	"domainscript/driver"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executa a CLI e devolve o exit code, recebendo args e os escritores para ser
// testável sem tocar em os.Exit/os.Args. Códigos: 0 = sem erros, 1 = há erros de
// validação (REQ-6.7/8.3), 2 = erro de uso ou de IO.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "uso: dsc <arquivo.ds | diretório>")
		return 2
	}
	path := args[0]

	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(stderr, "dsc: não foi possível acessar %q: %v\n", path, err)
		return 2
	}

	// Roteia por arquivo vs. diretório (REQ-8.2/8.4): um diretório é agregado num
	// Program (regras cross-file); um arquivo é validado isoladamente.
	var bag *diag.DiagnosticBag
	if info.IsDir() {
		_, bag = driver.CheckProject(path)
	} else {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			fmt.Fprintf(stderr, "dsc: não foi possível ler %q: %v\n", path, readErr)
			return 2
		}
		_, bag = driver.CheckSource(string(data))
	}

	if report := bag.Render(); report != "" {
		fmt.Fprintln(stdout, report)
	}
	if bag.HasErrors() {
		return 1
	}
	return 0
}
