package codegen

import (
	"fmt"
	"sort"

	"domainscript/ast"
	"domainscript/program"
)

// decl_aggregate_storage.go interpreta o bloco storage {} de um Aggregate
// (ast.AggregateDecl.Storage, spec §2.5, G1a — REQ-22.7(b)/REQ-25/REQ-26):
// cada entrada roteia um campo do state para o Database ("state: <Nome>") ou
// para um FileStorage ("<campo>: <Nome>") declarado no mod.ds do módulo. O
// front-end NUNCA resolve essas referências (ast.StorageEntry.Value é um
// Ident cru, nunca uma ref resolvida — mesma situação de Database.Provider,
// program/graph.go) — este arquivo é a PRIMEIRA autoridade a interpretar o
// bloco, o mesmo papel que decl_io.go/decl_projection.go já assumem para
// Notification/Adapter e Projection.
//
// A ÚNICA responsabilidade prática hoje é decidir o ROTEAMENTO (qual
// FileStorage cada campo FileRef usa): FileRef não precisa de nenhum
// tratamento especial de PERSISTÊNCIA — é só metadado leve (id/nome/
// contentType/tamanho/metadata/storedAt) embutido no state/evento como
// qualquer outro campo (ver codegen/rtsrc/filestorage.go.txt) — os bytes de
// verdade vivem só na FileStorage, nunca no Database. O mapa devolvido aqui
// é consultado por codegen/lower/builtins.go (BuiltinLowerer.
// resolveFileStorageName) para decidir, por NOME DE CAMPO, qual FileStorage
// "store"/"signed_url"/"delete file"/"load File(ref)" usam quando o alvo da
// operação referencia esse campo (ex. "store cmd.content" ou "person.state.
// document").

// aggregateFileStorageRouting devolve, a partir do bloco storage{} de decl, o
// mapa "nome de campo do state" -> "nome do FileStorage declarado"
// (ignorando a entrada "state", que roteia para um Database — já resolvido
// independentemente via mod.ds "manages", program.Database.Manages). decl
// sem bloco storage (a forma comum: Aggregate sem nenhum campo FileRef, ou
// sem bloco storage algum) devolve (nil, nil) — sem erro, sem ambiguidade.
//
// Validações (REQ-14.4, defensivas — não deveria ocorrer sobre um programa
// gerado a partir de um mod.ds consistente, mas nunca um erro silencioso):
//   - a entrada "state" precisa nomear um Database DECLARADO no módulo;
//   - qualquer outra entrada precisa nomear um campo de fato declarado em
//     decl.State, cujo TIPO seja exatamente "FileRef" (§2.5: só campos
//     FileRef fazem sentido rotear para uma FileStorage);
//   - essa entrada precisa nomear uma FileStorage DECLARADA no módulo.
func aggregateFileStorageRouting(decl *ast.AggregateDecl, mod *program.Module) (map[string]string, error) {
	if len(decl.Storage) == 0 {
		return nil, nil
	}

	stateFields := make(map[string]*ast.Field, len(decl.State))
	for _, f := range decl.State {
		if f != nil {
			stateFields[f.Name] = f
		}
	}

	routing := make(map[string]string, len(decl.Storage))
	for _, se := range decl.Storage {
		if se.Key == "state" {
			if mod == nil || mod.Databases[se.Value] == nil {
				return nil, fmt.Errorf("Aggregate %s: storage.state referencia Database %q, não declarado no mod.ds do módulo", decl.Name, se.Value)
			}
			continue
		}

		field, ok := stateFields[se.Key]
		if !ok {
			return nil, fmt.Errorf("Aggregate %s: storage.%s referencia um campo não declarado em state", decl.Name, se.Key)
		}
		gotType := "?"
		if field.Type != nil {
			gotType = field.Type.Name
		}
		if gotType != "FileRef" {
			return nil, fmt.Errorf("Aggregate %s: storage.%s roteia para FileStorage, mas o campo é %s, não FileRef (§2.5: só campos FileRef são roteados para FileStorage)", decl.Name, se.Key, gotType)
		}
		if mod == nil || mod.FileStorages[se.Value] == nil {
			return nil, fmt.Errorf("Aggregate %s: storage.%s referencia FileStorage %q, não declarada no mod.ds do módulo", decl.Name, se.Key, se.Value)
		}
		routing[se.Key] = se.Value
	}
	return routing, nil
}

// moduleFileStorageRouting agrega aggregateFileStorageRouting de TODOS os
// Aggregates de um módulo (aggregates, o mesmo mapa nome->decl que
// EmitUseCases/EmitQueries já recebem) num único mapa "campo -> FileStorage",
// percorrido em ordem alfabética de nome de Aggregate (determinismo, NFR-13,
// mesmo quando o resultado é um erro). Dois Aggregates que roteiam um campo
// de MESMO NOME para FileStorage DIFERENTES é uma ambiguidade que o
// roteamento por nome de campo (BuiltinLowerer.resolveFileStorageName) não
// consegue resolver sozinho — erro de geração claro em vez de escolher um
// dos dois arbitrariamente (não ocorre em nenhuma fixture desta task;
// documentado como limitação, mesmo espírito de
// correlateListVOAggregate/decl_query.go).
func moduleFileStorageRouting(aggregates map[string]*ast.AggregateDecl, mod *program.Module) (map[string]string, error) {
	if mod == nil {
		return nil, nil
	}
	names := make([]string, 0, len(aggregates))
	for name := range aggregates {
		names = append(names, name)
	}
	sort.Strings(names)

	merged := make(map[string]string)
	for _, name := range names {
		r, err := aggregateFileStorageRouting(aggregates[name], mod)
		if err != nil {
			return nil, err
		}
		for field, storage := range r {
			if existing, ok := merged[field]; ok && existing != storage {
				return nil, fmt.Errorf("campo %q roteado para FileStorage diferentes por Aggregates distintos do módulo (%q e %q) — ambiguidade não suportada pelo roteamento por nome de campo (G1a)", field, existing, storage)
			}
			merged[field] = storage
		}
	}
	if len(merged) == 0 {
		return nil, nil
	}
	return merged, nil
}

// moduleFileStorageDefault devolve o nome da ÚNICA FileStorage declarada no
// módulo (usado por BuiltinLowerer.resolveFileStorageName como fallback
// quando o alvo de uma operação não bate com nenhuma entrada de
// moduleFileStorageRouting) — "" quando o módulo declara 0 ou 2+ FileStorage
// (ambiguidade: exige correlação explícita por nome de campo).
func moduleFileStorageDefault(mod *program.Module) string {
	if mod == nil || len(mod.FileStorages) != 1 {
		return ""
	}
	for name := range mod.FileStorages {
		return name
	}
	return ""
}

// programModule devolve prog.Modules[module], ou nil quando prog é nil (o
// mesmo comportamento "nil preserva o comportamento anterior" documentado em
// EmitUseCase/EmitQuery para o parâmetro prog — nenhuma FileStorage
// disponível, resolveFileStorageName sempre falha se alguma op de arquivo
// for de fato usada).
func programModule(prog *program.Program, module string) *program.Module {
	if prog == nil {
		return nil
	}
	return prog.Modules[module]
}

// moduleFileStorageNames devolve os nomes (ordenados, NFR-13) das FileStorage
// declaradas no módulo — usado por codegen.go para decidir se o módulo
// precisa do arquivo de wiring (emitFileStorageWiring) e por
// generateCmdMainFile para instanciar/injetar cada uma.
func moduleFileStorageNames(mod *program.Module) []string {
	if mod == nil || len(mod.FileStorages) == 0 {
		return nil
	}
	names := make([]string, 0, len(mod.FileStorages))
	for name := range mod.FileStorages {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
