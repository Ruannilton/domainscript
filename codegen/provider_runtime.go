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
// esta função sempre devolve nil, e nenhum projeto gerado muda (NFR-21).
//
// Dedup por adapterDir (revisão da PR #13): DUAS categorias que usam o MESMO
// provider real (ex. redis em Cache e em RateLimit, §design 3.4) têm
// providerDep com o MESMO module/adapterDir mas ctor DIFERENTE
// (NewRedisQueryCache vs. NewRedisLimiter) — a dedup por struct inteira de
// activeProviderDeps (R5) NÃO as colapsa (é o comportamento certo para
// go.mod/ctor, cada categoria precisa do seu). Mas as fontes do adapter (o
// mesmo diretório redisruntime/) só podem ser copiadas UMA vez — copiar de
// novo reprocessaria e duplicaria as MESMAS entradas de File (mesmo Path) na
// lista devolvida. seenDirs garante que cada adapterDir só é materializado
// uma única vez, mesmo aparecendo em múltiplas providerDep ativas.
//
// A ordenação final é por adapterDir e, dentro dele, por nome de arquivo —
// determinismo (NFR-13), mesmo com múltiplas categorias ativas ao mesmo
// tempo.
func generateProviderRuntimeFiles(deps []providerDep) ([]File, error) {
	var files []File
	seenDirs := make(map[string]bool)

	for _, dep := range deps {
		if seenDirs[dep.adapterDir] {
			continue
		}
		sourcesFn, ok := providerSources[dep.adapterDir]
		if !ok || sourcesFn == nil {
			continue
		}
		seenDirs[dep.adapterDir] = true

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
