package codegen

import "errors"

// ErrHasDiagnostics é devolvido quando o programa tem ao menos um diagnóstico
// de severidade error: o gerador recusa produzir código (REQ-14.1).
var ErrHasDiagnostics = errors.New("codegen: programa com diagnósticos de erro, geração recusada")

// ErrNotImplemented sinaliza que a geração de código ainda não está
// implementada. É o estado do scaffold do Marco E; some quando codegen.Generate
// ganha corpo nas fases seguintes deste ciclo.
var ErrNotImplemented = errors.New("codegen: geração de código ainda não implementada")

// Options configura a geração de um projeto Go (REQ-14.5, REQ-15).
type Options struct {
	// ModulePath é o caminho do módulo Go gerado (1ª linha do go.mod). Vazio
	// deixa o chamador derivar um default a partir do diretório de saída.
	ModulePath string

	// GoVersion é a versão mínima declarada no go.mod gerado. Vazio usa o
	// default "1.22" (mínimo para os padrões de rota "METHOD /path/{param}"
	// do net/http.ServeMux — REQ-28).
	GoVersion string
}

// File é um arquivo do projeto Go gerado, com caminho relativo à raiz de saída.
type File struct {
	Path    string
	Content []byte
}
