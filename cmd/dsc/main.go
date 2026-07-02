package main

import (
	"fmt"
	"io"
	"os"

	"domainscript/codegen"
	"domainscript/diag"
	"domainscript/driver"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run é o dispatcher de subcomando (REQ-32, §design codegen 3.15): "check" e
// "gen" são subcomandos explícitos; qualquer outro 1º argumento é tratado como
// o caminho de "dsc check" — preserva o uso anterior de argumento único
// (retrocompatibilidade).
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}

	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "gen":
		return runGen(args[1:], stdout, stderr)
	default:
		return runCheck(args, stdout, stderr)
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "uso: dsc <arquivo.ds | diretório>")
	fmt.Fprintln(w, "     dsc check <arquivo.ds | diretório>")
	fmt.Fprintln(w, "     dsc gen <diretório> -o <saída>")
}

// runCheck valida um arquivo ou diretório e imprime o relatório de
// diagnósticos (REQ-8.2/8.4). Roteia por arquivo vs. diretório: um diretório é
// agregado num Program (regras cross-file); um arquivo é validado isoladamente.
func runCheck(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "uso: dsc check <arquivo.ds | diretório>")
		return 2
	}
	path := args[0]

	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(stderr, "dsc: não foi possível acessar %q: %v\n", path, err)
		return 2
	}

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

	return report(bag, stdout)
}

// runGen valida o projeto em <diretório> e, se válido, geraria o projeto Go em
// -o <saída> (REQ-14.1, REQ-32.2). A geração em si (codegen.Generate e a
// escrita dos arquivos) chega em fases posteriores deste ciclo — por ora o
// subcomando confirma a pré-condição e recusa programas com erro, sem gerar.
func runGen(args []string, stdout, stderr io.Writer) int {
	dir, out, ok := parseGenArgs(args)
	if !ok {
		fmt.Fprintln(stderr, "uso: dsc gen <diretório> -o <saída>")
		return 2
	}

	bag, err := driver.GenerateProject(dir, out, codegen.Options{})
	if code := report(bag, stdout); code != 0 {
		return code
	}
	if err != nil {
		fmt.Fprintf(stderr, "dsc: %v\n", err)
		return 2
	}
	return 0
}

// parseGenArgs extrai <diretório> e -o <saída> de "dsc gen <dir> -o <out>"
// (REQ-32.2). O diretório posicional vem antes da flag no uso canônico, o que
// o pacote flag da stdlib não suporta (ele para de reconhecer flags no 1º
// argumento não-flag); por isso um parser dedicado, aceitando "-o valor" e
// "-o=valor" em qualquer posição.
func parseGenArgs(args []string) (dir, out string, ok bool) {
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o" || a == "--o":
			if i+1 >= len(args) {
				return "", "", false
			}
			out = args[i+1]
			i++
		case len(a) > 3 && a[:3] == "-o=":
			out = a[3:]
		case len(a) > 4 && a[:4] == "--o=":
			out = a[4:]
		default:
			rest = append(rest, a)
		}
	}
	if len(rest) != 1 || out == "" {
		return "", "", false
	}
	return rest[0], out, true
}

// report imprime o relatório do bag (se houver) e devolve o exit code
// correspondente (REQ-6.7/8.3): 0 sem erros, 1 com ao menos um erro.
func report(bag *diag.DiagnosticBag, stdout io.Writer) int {
	if bag == nil {
		return 0
	}
	if r := bag.Render(); r != "" {
		fmt.Fprintln(stdout, r)
	}
	if bag.HasErrors() {
		return 1
	}
	return 0
}
