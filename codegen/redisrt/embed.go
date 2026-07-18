package redisrt

import (
	"embed"
	"sort"
	"strings"
)

// sourceFS embeda cada arquivo-fonte do adapter redisruntime (o sufixo
// ".go.txt" evita que compilem junto do compilador — mesmo mecanismo de
// codegen/rtsrc/embed.go, codegen/sqlrt/embed.go, codegen/grpcrt/embed.go e
// codegen/amqprt/embed.go). Este arquivo não pode importar codegen nem
// qualquer outro pacote do compilador: redisrt é uma folha de dependência,
// stdlib only (mesma regra de rtsrc/sqlrt/grpcrt/amqprt) — a dependência
// real (github.com/redis/go-redis/v9) só aparece DENTRO dos .go.txt
// embutidos, nunca aqui.
//
//go:embed *.go.txt
var sourceFS embed.FS

// Sources devolve o conteúdo de cada arquivo-fonte embutido, indexado pelo
// nome final ("cache.go", sem o sufixo ".go.txt") — pronto para ser escrito
// como redisruntime/<nome> no projeto gerado, quando um bloco Cache/
// RateLimit do módulo declara `backend: "redis"` (ver
// codegen/provider_registry.go, cacheProviders["redis"]).
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
// mesmo padrão de rtsrc.Names/sqlrt.Names/grpcrt.Names/amqprt.Names.
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
