package rtsrc

import (
	"embed"
	"sort"
	"strings"
)

// sourceFS embeds every runtime source file (the ".go.txt" suffix keeps
// them from compiling alongside the compiler itself — see doc.go/§design
// codegen 2). This file must not import codegen or any other compiler
// package: rtsrc is a dependency leaf, stdlib only.
//
//go:embed *.go.txt
var sourceFS embed.FS

// Sources returns the content of every embedded runtime source file, keyed
// by its final name ("eventstore.go", without the ".go.txt" suffix) —
// ready to be written as runtime/<name> in the generated project.
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

// Names returns the sorted list of final file names Sources would produce
// (e.g. "decimal.go", "eventstore.go", ...) — useful wherever callers need
// deterministic iteration over the runtime file set.
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
