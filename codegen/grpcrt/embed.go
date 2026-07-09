package grpcrt

import (
	"embed"
	"sort"
	"strings"
)

// sourceFS embeda cada arquivo-fonte do pacote de borda gRPC (o sufixo
// ".go.txt" evita que compilem junto do compilador — mesmo mecanismo de
// codegen/rtsrc/embed.go e codegen/sqlrt/embed.go). Este arquivo não pode
// importar codegen nem qualquer outro pacote do compilador: grpcrt é uma
// folha de dependência, stdlib only (mesma regra de rtsrc/sqlrt) — a
// dependência real (google.golang.org/grpc) só aparece DENTRO dos .go.txt
// embutidos, nunca aqui.
//
//go:embed *.go.txt
var sourceFS embed.FS

// Sources devolve o conteúdo de cada arquivo-fonte embutido, indexado pelo
// nome final ("codec.go", sem o sufixo ".go.txt") — pronto para ser escrito
// como grpcedge/<nome> no projeto gerado, quando o programa declara ao menos
// uma "Interface GRPC" (ver codegen/codegen.go, programNeedsGRPC).
func Sources() (map[string][]byte, error) {
	entries, err := sourceFS.ReadDir(".")
	if err != nil {
		return nil, err
	}

	out := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go.txt") {
			continue
		}
		content, err := sourceFS.ReadFile(name)
		if err != nil {
			return nil, err
		}
		out[strings.TrimSuffix(name, ".txt")] = content
	}
	return out, nil
}

// Names devolve a lista ordenada de nomes finais que Sources produziria —
// mesmo padrão de rtsrc.Names/sqlrt.Names.
func Names() ([]string, error) {
	srcs, err := Sources()
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(srcs))
	for name := range srcs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
