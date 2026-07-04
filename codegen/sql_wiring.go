package codegen

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"domainscript/codegen/emit"
	"domainscript/codegen/sqlrt"
	"domainscript/program"
)

// programNeedsSQLAdapter devolve true se algum Database, em qualquer módulo
// do programa, declara provider "sqlite" (case-insensitive) — o único
// provider real que este gerador sabe montar (program.Database.Provider,
// G1). Usado por Generate (codegen.go) para decidir, uma única vez por
// projeto: (a) emitir sqlruntime/*.go (codegen/sqlrt) e (b) acrescentar o
// require do driver ao go.mod (EmitGoMod) — em QUALQUER outro caso (nenhum
// Database, ou só providers não reconhecidos como "postgres") devolve false,
// e o projeto gerado permanece exatamente como antes de G1 (NFR-12).
func programNeedsSQLAdapter(prog *program.Program) bool {
	for _, mod := range prog.Modules {
		for _, db := range mod.Databases {
			if strings.EqualFold(db.Provider, "sqlite") {
				return true
			}
		}
	}
	return false
}

// generateSQLRuntimeFiles copia sqlrt.Sources() (verbatim, mesmo padrão de
// generateRuntimeFiles) para sqlruntime/*.go — só chamado quando
// programNeedsSQLAdapter devolve true.
func generateSQLRuntimeFiles() ([]File, error) {
	srcs, err := sqlrt.Sources()
	if err != nil {
		return nil, fmt.Errorf("codegen: sqlruntime: %w", err)
	}
	names := make([]string, 0, len(srcs))
	for name := range srcs {
		names = append(names, name)
	}
	sort.Strings(names)

	files := make([]File, 0, len(names))
	for _, name := range names {
		files = append(files, File{Path: path.Join("sqlruntime", name), Content: srcs[name]})
	}
	return files, nil
}

// sql_wiring.go emite, em cmd/<service>/main.go, o wiring do adapter
// database/sql (G1, REQ-20.5, REQ-26.2/26.3, §design 3.8/3.11) para um
// módulo cujos UseCases precisam coordenar 2PC entre 2+ Database "sqlite"
// (ver moduleMarks.xaDatabases/usecase2PCPlan, codegen.go/decl_usecase.go):
// abre uma conexão real por Database (sqlruntime.Open), monta seu EventStore
// (schema + registry de eventos do módulo, via "<pkg>.EventRegistry()" —
// decl_event.go, G1) e wira tudo via
// "<pkg>.Wire2PC(sqlruntime.NewUnitOfWork2PC(...))", ao lado do
// "<pkg>.Wire(uow)" de sempre (que continua apontando para a store em
// memória compartilhada do service — inofensivo mesmo quando nenhum UseCase
// do módulo o usa de fato, já que "uow" é uma var de pacote nunca exigida
// como "usada" pelo compilador Go).
//
// Só entra em jogo quando o programa declara 2+ Database "sqlite" com
// supportsXA e um UseCase que toca Aggregates geridos por ambos (a fixture
// sintética desta task) — wallet/shop (provider "postgres", nunca
// reconhecido como adapter real, ver program.Database.Provider) nunca
// disparam este caminho: seu cmd/<service>/main.go permanece byte-a-byte
// igual a antes de G1.
//
// Limitação documentada (fora do orçamento desta task): um módulo que
// misture, ao mesmo tempo, UseCases 2PC E UseCases de banco único sqlite
// (que precisariam de SUA PRÓPRIA sqlruntime.UnitOfWork single-DB via
// "Wire", não a em memória) não é distinguido aqui — hoje "Wire(uow)"
// SEMPRE aponta para a store em memória do service, então um UseCase de
// banco único cujo Database seja sqlite (sem 2PC) ainda rodaria sobre a
// store errada. Nenhum dos dois exemplos reais (wallet/shop) nem a fixture
// sintética desta task exercitam essa combinação — generalizar exige uma
// fixture real que a precise (mesmo espírito de "mais casos entram quando
// surgir necessidade real" já documentado em decl_query.go).
func emitXADatabaseWiring(e *emit.Emitter, prog *program.Program, moduleName, pkgAlias string, dbNames []string, ctxAlias string) error {
	mod := prog.Modules[moduleName]
	if mod == nil {
		return fmt.Errorf("módulo %s não encontrado no Program (bug de geração)", moduleName)
	}

	logAlias := e.Import("log")
	sqlRuntimeAlias := e.Import(path.Join(domainModuleRoot, "sqlruntime"))

	names := append([]string(nil), dbNames...)
	sort.Strings(names) // determinismo explícito aqui também (NFR-13), mesmo já vindo ordenado de usecase2PCPlan

	storeVars := make(map[string]string, len(names))
	for _, dbName := range names {
		db := mod.Databases[dbName]
		if db == nil {
			return fmt.Errorf("Database %s não encontrado no módulo %s (bug de geração)", dbName, moduleName)
		}
		varPrefix := strings.ToLower(dbName[:1]) + dbName[1:]
		dbVar := varPrefix + "DB"
		storeVar := varPrefix + "Store"
		storeVars[dbName] = storeVar

		e.Line("%s, err := %s.Open(%s)", dbVar, sqlRuntimeAlias, strconv.Quote(db.DSN))
		e.Line("if err != nil { %s.Fatal(err) }", logAlias)
		e.Line("%s, err := %s.NewEventStore(%s.Background(), %s, %s.EventRegistry())", storeVar, sqlRuntimeAlias, ctxAlias, dbVar, pkgAlias)
		e.Line("if err != nil { %s.Fatal(err) }", logAlias)
	}

	e.Line("%s.Wire2PC(%s.NewUnitOfWork2PC(map[string]*%s.EventStore{", pkgAlias, sqlRuntimeAlias, sqlRuntimeAlias)
	for _, dbName := range names {
		e.Line("%s: %s,", strconv.Quote(dbName), storeVars[dbName])
	}
	e.Line("}))")
	return nil
}
