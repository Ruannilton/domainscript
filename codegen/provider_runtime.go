package codegen

import (
	"fmt"
	"path"
	"sort"
)

// generateProviderRuntimeFiles é a versão genérica de generateSQLRuntimeFiles
// (sql_wiring.go) para as categorias de providerDep (Canal/Cache/RateLimit/
// FileStorage — Database continua em generateSQLRuntimeFiles, ver
// provider_registry.go): para cada dep ativa (activeProviderDeps) que tem uma
// função registrada em providerSources (chave dep.adapterDir), copia cada
// arquivo devolvido para <adapterDir>/<nome> no projeto gerado (J0.3,
// REQ-46.3, §design 2.3).
//
// Uma dep cujo adapterDir não está em providerSources é ignorada
// silenciosamente — hoje (antes de J1..J5) providerSources está vazio, então
// esta função sempre devolve nil, e nenhum projeto gerado muda (NFR-21). Duas
// deps que compartilhem o mesmo adapterDir (não deveria acontecer — dedup já
// colapsou entradas idênticas em activeProviderDeps, e adapterDir distintos
// por design de cada provider) apareceriam ambas aqui; não há necessidade de
// uma dedup própria além da de activeProviderDeps (mesmo espírito de "mais
// casos entram quando surgir necessidade real").
//
// A ordenação final é por adapterDir e, dentro dele, por nome de arquivo —
// determinismo (NFR-13), mesmo com múltiplas categorias ativas ao mesmo
// tempo.
func generateProviderRuntimeFiles(deps []providerDep) ([]File, error) {
	var files []File

	for _, dep := range deps {
		sourcesFn, ok := providerSources[dep.adapterDir]
		if !ok || sourcesFn == nil {
			continue
		}

		srcs, err := sourcesFn()
		if err != nil {
			return nil, fmt.Errorf("codegen: %s: %w", dep.adapterDir, err)
		}

		names := make([]string, 0, len(srcs))
		for name := range srcs {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			files = append(files, File{Path: path.Join(dep.adapterDir, name), Content: srcs[name]})
		}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
