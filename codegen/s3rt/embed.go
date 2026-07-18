package s3rt

import (
	"embed"
	"sort"
	"strings"
)

// sourceFS embeda cada arquivo-fonte do adapter s3runtime (o sufixo
// ".go.txt" evita que compilem junto do compilador — mesmo mecanismo de
// codegen/rtsrc/embed.go, codegen/sqlrt/embed.go, codegen/grpcrt/embed.go,
// codegen/amqprt/embed.go e codegen/redisrt/embed.go). Este arquivo não pode
// importar codegen nem qualquer outro pacote do compilador: s3rt é uma
// folha de dependência, stdlib only (mesma regra de
// rtsrc/sqlrt/grpcrt/amqprt/redisrt) — a dependência real
// (github.com/aws/aws-sdk-go-v2/...) só aparece DENTRO dos .go.txt
// embutidos, nunca aqui.
//
//go:embed *.go.txt
var sourceFS embed.FS

// Sources devolve o conteúdo de cada arquivo-fonte embutido, indexado pelo
// nome final ("filestorage.go", sem o sufixo ".go.txt") — pronto para ser
// escrito como s3runtime/<nome> no projeto gerado, quando uma FileStorage do
// módulo declara `provider: "s3"` (ver codegen/provider_registry.go,
// fileProviders["s3"]).
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
// mesmo padrão de rtsrc.Names/sqlrt.Names/grpcrt.Names/amqprt.Names/
// redisrt.Names.
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
