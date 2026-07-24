package codegen

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/sqlrt"
	"domainscript/program"
	"domainscript/token"
)

// sqlProvider é uma entrada do registro único de provider SQL real que este
// gerador sabe montar (I7.0, REQ-40.2, §design read-side 3.9a): tudo que se
// precisa saber sobre um banco para (a) exigi-lo em go.mod, (b) escolher a
// versão mínima de Go e (c) instanciar o Dialect certo no wiring gerado.
// Adicionar um banco novo é implementar seu Dialect (codegen/sqlrt) +
// acrescentar uma entrada aqui — nenhuma outra mudança em lowering,
// decl_*.go ou no runtime núcleo.
type sqlProvider struct {
	driverModule  string // caminho do módulo Go a exigir em go.mod (EmitGoMod)
	driverVersion string
	minGoVersion  string // versão mínima de Go que o driver exige (EmitGoMod)
	dialectCtor   string // nome do construtor Dialect exportado por sqlruntime, ex. "SQLiteDialect"
	// openFunc é o nome da func exportada por sqlruntime que abre uma conexão
	// para este provider (ex. "Open", "OpenPostgres", J1.2) — não pode ser o
	// mesmo símbolo "Open" para dois providers porque generateSQLRuntimeFiles
	// (codegen.go) copia TODOS os *.go.txt de sqlrt.Sources() sempre que
	// qualquer provider real está ativo (não filtra por provider individual):
	// um projeto com Database "sqlite" E "postgres" ativos ao mesmo tempo tem
	// os dois open_*.go.txt no MESMO pacote sqlruntime gerado, então cada um
	// precisa de um nome próprio. emitXADatabaseWiring (abaixo) usa isto em
	// vez de literal ".Open(".
	openFunc string
}

// sqlProviders é o registro único de provider (REQ-40.2): o ÚNICO lugar do
// gerador que associa um Database.Provider a um adapter real. Antes desta
// task o mesmo reconhecimento ("sqlite", case-insensitive) estava repetido
// em programNeedsSQLAdapter e em usecase2PCPlan (decl_usecase.go) — ambos
// agora consultam recognizedSQLProvider.
var sqlProviders = map[string]sqlProvider{
	"sqlite": {
		driverModule:  sqliteDriverModule,
		driverVersion: sqliteDriverVersion,
		minGoVersion:  sqliteMinGoVersion,
		dialectCtor:   "SQLiteDialect",
		openFunc:      "Open",
	},
	// postgres (J1.2, REQ-41.2): segundo provider SQL real deste gerador —
	// mesmo seam Dialect de sqlite (dialect_postgres.go.txt, J1.1), driver
	// pgx/v5 stdlib atrás de database/sql (open_postgres.go.txt, §design
	// infra-providers 3.1). pgx/v5 (>= v5.7.x) exige Go >= 1.25 (mesma
	// versão mínima que modernc.org/sqlite já exige hoje — maxGoVersion,
	// project.go, não eleva o default além do que sqlite já elevaria).
	"postgres": {
		driverModule:  postgresDriverModule,
		driverVersion: postgresDriverVersion,
		minGoVersion:  postgresMinGoVersion,
		dialectCtor:   "PostgresDialect",
		openFunc:      "OpenPostgres",
	},
}

// recognizedSQLProvider devolve a entrada do registro para provider
// (case-insensitive) e ok=true quando é um adapter real que este gerador
// sabe montar — a única comparação de string contra program.Database.Provider
// em todo o gerador (REQ-40.2).
func recognizedSQLProvider(provider string) (sqlProvider, bool) {
	p, ok := sqlProviders[strings.ToLower(provider)]
	return p, ok
}

// activeSQLProviders devolve, em ordem alfabética (determinismo, NFR-13), as
// chaves de sqlProviders efetivamente usadas por prog: cada Database, em
// qualquer módulo, cujo Provider (case-insensitive) resolve via
// recognizedSQLProvider — deduplicado (dois Database com o mesmo provider
// contam uma vez só). EmitGoMod (project.go, REQ-40.2) consome isto para
// exigir o driver/versão mínima de Go de CADA provider ativo — nunca
// hardcoding "sqlite": um provider novo (uma entrada nova em sqlProviders)
// passa a aparecer em go.mod automaticamente quando um programa o usa, sem
// nenhuma mudança em EmitGoMod.
func activeSQLProviders(prog *program.Program) []string {
	seen := make(map[string]bool)
	for _, mod := range prog.Modules {
		for _, db := range mod.Databases {
			if _, ok := recognizedSQLProvider(db.Provider); ok {
				seen[strings.ToLower(db.Provider)] = true
			}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// programNeedsSQLAdapter devolve true se algum Database, em qualquer módulo
// do programa, declara um provider reconhecido em sqlProviders (hoje só
// "sqlite" — o único provider real que este gerador sabe montar,
// program.Database.Provider, G1). Usado por Generate (codegen.go) para
// decidir, uma única vez por projeto, se emite sqlruntime/*.go (codegen/
// sqlrt) — em QUALQUER outro caso (nenhum Database, ou só providers não
// reconhecidos como "postgres") devolve false, e o projeto gerado permanece
// exatamente como antes de G1 (NFR-12).
func programNeedsSQLAdapter(prog *program.Program) bool {
	return len(activeSQLProviders(prog)) > 0
}

// moduleOutboxDatabaseName devolve o nome do primeiro Database (ordem
// alfabética, NFR-13) do módulo moduleName cujo provider é reconhecido como
// SQL real (recognizedSQLProvider) — "" quando o módulo não declara nenhum
// (o caso comum: REQ-42.5 mantém o memoryOutbox stopgap para ele).
// emitPolicyWireFunc (decl_policy.go, task J2.5) usa isto para decidir se
// promove o Outbox local ("d") de uma Policy AtLeastOnce para um
// DurableOutbox real — um módulo que declare mais de um Database real (não
// exercitado por wallet/shop hoje) usa o primeiro em ordem alfabética,
// determinístico; a ambiguidade de "qual banco guarda o outbox" quando há
// mais de um fica para quando um exemplo real precisar escolher.
func moduleOutboxDatabaseName(prog *program.Program, moduleName string) string {
	mod := prog.Modules[moduleName]
	if mod == nil {
		return ""
	}
	var names []string
	for name, db := range mod.Databases {
		if _, ok := recognizedSQLProvider(db.Provider); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return names[0]
}

// durableProducer reporta se moduleName qualifica para o caminho de produtor
// durável do Outbox → canal cross-service (ISSUE-9/REQ-51, §design
// correcoes-issues-9-10-11 4.1): um PREDICADO PURO, sem nenhuma emissão —
// Fase K3.1 só decide "ativa ou não"; o wiring de verdade (abrir a conexão
// real, trocar o publisher da UoW, subir o relay) é K3.2+.
//
// Condição de ativação (validada na revisão da PR #37, §design 4.1), as DUAS
// precisam valer:
//  1. o módulo tem EXATAMENTE 1 Database "real" (recognizedSQLProvider —
//     hoje "sqlite"/"postgres", case-insensitive).
//  2. o módulo tem um canal de SAÍDA (producerChannelFor) cujo provider
//     resolve como "rabbitmq" (channelProviderKind) — não basta `via: queue`
//     sozinho: a QueueChannel in-memory (sem `provider:` real) não é um
//     transporte durável, então a condição fica falsa (o caso do `shop`,
//     REQ-51.6/NFR-25).
//
// "Exatamente 1" Database real (não "1 ou mais"): a leitura do design
// (§4.1 "Sem 2PC", §4.2-P1 "banco único, não-2PC") e de usecase2PCPlan
// (decl_usecase.go) mostra que 2+ Database reais no mesmo módulo já
// disparam o caminho XA existente (moduleMarks.xaDatabases,
// emitXADatabaseWiring) — um caso ortogonal, de coordenação distribuída
// entre bancos, que esta função não deve reconhecer como "produtor durável
// de banco único": um módulo com 2 Databases reais devolve false aqui,
// deixando-o inteiramente para o caminho 2PC já existente (nenhuma
// colisão/duplicação de wiring).
//
// producerChannelFor pode devolver erro (mais de um canal de saída via
// "queue" no mesmo módulo, ou um `via` não suportado — o guard F5
// pré-existente): seguindo a mesma convenção dos chamadores existentes de
// producerChannelFor (generateCmdMainFile, codegen.go), esse erro é
// propagado ao chamador de durableProducer, não silenciado como "false" —
// um erro de geração legítimo não deve ser mascarado por um predicado
// booleano.
func durableProducer(prog *program.Program, module string) (bool, error) {
	mod := prog.Modules[module]
	if mod == nil {
		return false, nil
	}

	var realDBs int
	for _, db := range mod.Databases {
		if _, ok := recognizedSQLProvider(db.Provider); ok {
			realDBs++
		}
	}
	if realDBs != 1 {
		return false, nil
	}

	ch, err := producerChannelFor(prog, module)
	if err != nil {
		return false, err
	}
	if ch == nil {
		return false, nil
	}

	kind, err := channelProviderKind(ch)
	if err != nil {
		return false, err
	}
	return kind == "rabbitmq", nil
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
func emitXADatabaseWiring(e *emit.Emitter, prog *program.Program, moduleName, pkgAlias string, dbNames []string, ctxAlias string, runMode bool) error {
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
		provider, ok := recognizedSQLProvider(db.Provider)
		if !ok {
			return fmt.Errorf("Database %s: provider %q não reconhecido (bug de geração — front-end já deveria ter barrado)", dbName, db.Provider)
		}

		varPrefix := strings.ToLower(dbName[:1]) + dbName[1:]
		dbVar := varPrefix + "DB"
		storeVar := varPrefix + "Store"
		storeVars[dbName] = storeVar

		connGo, err := databaseConnectionGo(e, db)
		if err != nil {
			return fmt.Errorf("Database %s: %w", dbName, err)
		}
		e.Line("%s, err := %s.%s(%s)", dbVar, sqlRuntimeAlias, provider.openFunc, connGo)
		emitFailFast(e, "err", logAlias, runMode)
		emitDeferClose(e, dbVar, runMode)
		e.Line("%s, err := %s.NewEventStore(%s.Background(), %s, %s.EventRegistry(), %s.%s())", storeVar, sqlRuntimeAlias, ctxAlias, dbVar, pkgAlias, sqlRuntimeAlias, provider.dialectCtor)
		emitFailFast(e, "err", logAlias, runMode)
	}

	e.Line("%s.Wire2PC(%s.NewUnitOfWork2PC(map[string]*%s.EventStore{", pkgAlias, sqlRuntimeAlias, sqlRuntimeAlias)
	for _, dbName := range names {
		e.Line("%s: %s,", strconv.Quote(dbName), storeVars[dbName])
	}
	e.Line("}))")
	return nil
}

// emitOutboxDatabaseWiring emite, em cmd/<service>/main.go, a conexão real
// que sustenta o outbox durável do módulo moduleName (task J2.5, REQ-42.5):
// abre dbName (mesma resolução de connection string que emitXADatabaseWiring
// já usa, databaseConnectionGo), monta um sqlruntime.OutboxStore sobre ela e
// chama "<pkgAlias>.WireOutboxStore(...)" — SEMPRE antes de
// "<pkgAlias>.Wire(...)" (o chamador, generateCmdMainFile, garante essa
// ordem): Wire lê outboxStore ao construir o DurableOutbox na 1ª Policy
// AtLeastOnce local do módulo (ver a doc de emitPolicyWireFunc,
// decl_policy.go). Independente de qualquer conexão que
// emitXADatabaseWiring abra para 2PC no MESMO módulo — um módulo que combine
// os dois (não exercitado por wallet/shop hoje) abriria duas conexões
// separadas para o mesmo Database; aceitável, generalizar fica para quando
// um exemplo real precisar.
func emitOutboxDatabaseWiring(e *emit.Emitter, prog *program.Program, moduleName, pkgAlias, dbName string, runMode bool) error {
	mod := prog.Modules[moduleName]
	if mod == nil {
		return fmt.Errorf("módulo %s não encontrado no Program (bug de geração)", moduleName)
	}
	db := mod.Databases[dbName]
	if db == nil {
		return fmt.Errorf("Database %s não encontrado no módulo %s (bug de geração)", dbName, moduleName)
	}
	provider, ok := recognizedSQLProvider(db.Provider)
	if !ok {
		return fmt.Errorf("Database %s: provider %q não reconhecido (bug de geração — front-end já deveria ter barrado)", dbName, db.Provider)
	}

	logAlias := e.Import("log")
	sqlRuntimeAlias := e.Import(path.Join(domainModuleRoot, "sqlruntime"))

	varPrefix := strings.ToLower(moduleName[:1]) + moduleName[1:]
	dbVar := varPrefix + "OutboxDB"
	storeVar := varPrefix + "OutboxStore"

	connGo, err := databaseConnectionGo(e, db)
	if err != nil {
		return fmt.Errorf("Database %s: %w", dbName, err)
	}
	e.Line("%s, err := %s.%s(%s)", dbVar, sqlRuntimeAlias, provider.openFunc, connGo)
	emitFailFast(e, "err", logAlias, runMode)
	emitDeferClose(e, dbVar, runMode)
	e.Line("%s := %s.NewOutboxStore(%s, %s.%s())", storeVar, sqlRuntimeAlias, dbVar, sqlRuntimeAlias, provider.dialectCtor)
	e.Line("%s.WireOutboxStore(%s)", pkgAlias, storeVar)
	return nil
}

// emitSingleDatabaseWiring emite, em cmd/<service>/main.go, a UnitOfWork
// database/sql de BANCO ÚNICO (sem 2PC) do módulo PRODUTOR durável (ISSUE-9/
// REQ-51.1/51.5, §design correcoes-issues-9-10-11 4.2-P1/P2/P3) — a
// PRÉ-CONDIÇÃO e o enqueue-in-tx de REQ-51: hoje generateCmdMainFile
// construía o "uow" do produtor sobre runtime.NewMemoryEventStore() (§design
// 4.1), onde Tx.EnqueueOutbox é um no-op documentado (rtsrc/uow.go.txt) — não
// havia onde persistir a linha do outbox. Este helper troca a STORE por uma
// conexão real (mesma resolução de emitXADatabaseWiring/
// emitOutboxDatabaseWiring — databaseConnectionGo + provider.openFunc) e
// constrói "uow := sqlruntime.NewOutboxUnitOfWork(db, EventRegistry(),
// dialect, <conjunto de event_type do canal>)".
//
// Ao contrário de emitXADatabaseWiring (que monta um *sqlruntime.EventStore
// POR Database para alimentar NewUnitOfWork2PC — coordenação entre 2+
// bancos), sqlruntime.NewOutboxUnitOfWork (sqlrt/uow.go.txt) já recebe o
// *sql.DB diretamente e abre sua PRÓPRIA *sql.Tx a cada Run — não há
// EventStore intermediário neste caminho de banco único, então nenhuma
// variável "*Store" é criada aqui (ao contrário de emitXADatabaseWiring).
//
// TROCA DE PUBLISHER (K3.3, REQ-51.3): o canal NÃO é mais passado como
// publisher da UoW (K3.2 passava). Em vez disso a UoW recebe o CONJUNTO de
// event_type que o canal carrega (outboxEventTypes, os PublicEvent do módulo,
// REQ-51.4) e enfileira esses apensados no outbox ANTES do commit (dentro de
// NewOutboxUnitOfWork.Run) — nunca publica pós-commit. Quem publica no canal
// é o relay do DurableOutbox montado por emitProducerOutboxRelay (abaixo), com
// o canal como seu publisher. Elimina a publicação dupla (uma inline no
// commit, outra pelo relay).
//
// A variável de pacote "store" que generateCmdMainFile sempre emite
// (runtime.NewMemoryEventStore()) continua existindo e sendo passada a
// newMux/newGRPCServer mesmo quando este helper roda — ela serve o lado de
// LEITURA do service inteiro (rotas de Query via "<pkg>.<Query>(ctx, store,
// ...)"), ortogonal ao "uow" de ESCRITA que este helper constrói: nenhuma
// rota de Query do módulo produtor muda de comportamento aqui, e nenhum
// outro módulo do mesmo service group é afetado.
func emitSingleDatabaseWiring(e *emit.Emitter, prog *program.Program, moduleName, pkgAlias, dbName string, outboxEventTypes []string, runMode bool) error {
	mod := prog.Modules[moduleName]
	if mod == nil {
		return fmt.Errorf("módulo %s não encontrado no Program (bug de geração)", moduleName)
	}
	db := mod.Databases[dbName]
	if db == nil {
		return fmt.Errorf("Database %s não encontrado no módulo %s (bug de geração)", dbName, moduleName)
	}
	provider, ok := recognizedSQLProvider(db.Provider)
	if !ok {
		return fmt.Errorf("Database %s: provider %q não reconhecido (bug de geração — front-end já deveria ter barrado)", dbName, db.Provider)
	}

	logAlias := e.Import("log")
	sqlRuntimeAlias := e.Import(path.Join(domainModuleRoot, "sqlruntime"))

	varPrefix := strings.ToLower(moduleName[:1]) + moduleName[1:]
	dbVar := varPrefix + "DB"

	connGo, err := databaseConnectionGo(e, db)
	if err != nil {
		return fmt.Errorf("Database %s: %w", dbName, err)
	}
	e.Line("%s, err := %s.%s(%s)", dbVar, sqlRuntimeAlias, provider.openFunc, connGo)
	emitFailFast(e, "err", logAlias, runMode)
	emitDeferClose(e, dbVar, runMode)
	e.Line("uow := %s.NewOutboxUnitOfWork(%s, %s.EventRegistry(), %s.%s(), %s)", sqlRuntimeAlias, dbVar, pkgAlias, sqlRuntimeAlias, provider.dialectCtor, outboxEventTypesSetLiteral(outboxEventTypes))
	return nil
}

// outboxEventTypesSetLiteral monta o literal Go do conjunto de event_type que
// o canal de saída carrega — map[string]bool{"WidgetMade": true, ...}, na
// ordem dada (o chamador ordena para determinismo, NFR-13). Vazio vira
// "map[string]bool{}" (a UoW não enfileira nada — só acontece numa fixture de
// teste isolada; no caminho real o conjunto é sempre não-vazio, garantido por
// durableProducer exigir um canal que carrega ao menos um PublicEvent).
func outboxEventTypesSetLiteral(eventTypes []string) string {
	if len(eventTypes) == 0 {
		return "map[string]bool{}"
	}
	parts := make([]string, len(eventTypes))
	for i, et := range eventTypes {
		parts[i] = fmt.Sprintf("%s: true", strconv.Quote(et))
	}
	return "map[string]bool{" + strings.Join(parts, ", ") + "}"
}

// emitProducerOutboxRelay emite, em cmd/<service>/main.go, o lado de ENTREGA
// do produtor durável (ISSUE-9/REQ-51.2/51.4, §design
// correcoes-issues-9-10-11 4.2-P4): monta o sqlruntime.OutboxStore sobre a
// MESMA conexão que emitSingleDatabaseWiring já abriu (dbVar = "<mod>DB", uma
// só conexão para o banco único do produtor), constrói o runtime.DurableOutbox
// com o canal (channelVarName) como publisher e um registry INLINE das
// factories dos PublicEvent que o canal carrega, e sobe o relay
// (DurableOutbox.Start) + a limpeza periódica (DurableOutbox.Cleanup) como
// goroutines.
//
// DECISÃO ARQUITETURAL (rota A, §design 4.2-P4 "em main.go, montar o
// OutboxStore, construir runtime.NewDurableOutbox(...)"): ao contrário do lado
// CONSUMIDOR (Policy AtLeastOnce), que emite StartOutboxRelay/
// StartOutboxCleanup/WireOutboxStore no PACOTE do módulo (decl_policy.go) e
// os chama de main.go, o produtor monta TUDO inline em main.go. Razão
// forçada: o canal (channelVarName) é uma var LOCAL de main()/run() (construída
// por emitChannelTransportVar), e o DurableOutbox precisa dele como publisher
// — só main.go tem o canal em escopo. Emitir funções de pacote como o
// consumidor exigiria passar o canal para dentro do pacote do módulo (um
// módulo produtor é UseCase-only, wirado por emitUOWWireFunc, que não conhece
// canal nenhum) — mudança maior e desnecessária. Manter inline deixa o pacote
// do módulo produtor byte-idêntico e concentra a mudança em main.go, como o
// design pede. O registry é inline (map[string]runtime.EventFactory sobre
// contracts.<Evt>), NÃO contracts.EventRegistry() (que não existe — contracts/
// só tem aliases de tipo, ver decl_event.go): mesmo raciocínio de
// emitDurableOutboxConstruction (consumidor) e do registry inline de
// channel_rabbitmq.go — o tipo é conhecido estaticamente aqui.
//
// Chamado DEPOIS de "workerCtx := context.Background()" (o relay/cleanup
// precisam de um context) e reusa dbVar aberto por emitSingleDatabaseWiring —
// o chamador (generateCmdMainFile) garante essa ordem.
func emitProducerOutboxRelay(e *emit.Emitter, prog *program.Program, moduleName, dbName, channelVarName string, outboxEventTypes []string, runtimeAlias, ctxVar string) error {
	mod := prog.Modules[moduleName]
	if mod == nil {
		return fmt.Errorf("módulo %s não encontrado no Program (bug de geração)", moduleName)
	}
	db := mod.Databases[dbName]
	if db == nil {
		return fmt.Errorf("Database %s não encontrado no módulo %s (bug de geração)", dbName, moduleName)
	}
	provider, ok := recognizedSQLProvider(db.Provider)
	if !ok {
		return fmt.Errorf("Database %s: provider %q não reconhecido (bug de geração — front-end já deveria ter barrado)", dbName, db.Provider)
	}

	sqlRuntimeAlias := e.Import(path.Join(domainModuleRoot, "sqlruntime"))
	contractsAlias := e.Import(path.Join(domainModuleRoot, "contracts"))
	timeAlias := e.Import("time")
	slogAlias := e.Import("log/slog")

	varPrefix := strings.ToLower(moduleName[:1]) + moduleName[1:]
	dbVar := varPrefix + "DB"
	storeVar := varPrefix + "OutboxStore"
	outboxVar := varPrefix + "Outbox"

	e.Line("// OutboxStore + DurableOutbox do produtor (ISSUE-9/REQ-51.2/51.4): o")
	e.Line("// relay abaixo publica no canal, a partir da tabela outbox, cada")
	e.Line("// PublicEvent que a UoW enfileirou dentro da tx (nunca inline no commit).")
	e.Line("%s := %s.NewOutboxStore(%s, %s.%s())", storeVar, sqlRuntimeAlias, dbVar, sqlRuntimeAlias, provider.dialectCtor)
	e.Line("%s := %s.NewDurableOutbox(%s, map[string]%s.EventFactory{", outboxVar, runtimeAlias, storeVar, runtimeAlias)
	for _, et := range outboxEventTypes {
		e.Line("%s: func() %s.Event { return &%s{} },", strconv.Quote(et), runtimeAlias, goname.QualifiedRef(contractsAlias, et))
	}
	e.Line("}, %s)", channelVarName)
	e.Line("go %s.Start(%s)", outboxVar, ctxVar)
	// Limpeza periódica das linhas já entregues (REQ-42.7) — mesma cadência/
	// retenção de StartOutboxCleanup do lado consumidor (outboxCleanupTickInterval/
	// outboxCleanupRetention, decl_policy.go), aqui inline como goroutine porque
	// o DurableOutbox é uma var local de main.go (ver a doc acima).
	e.BlockSuffix("go func()", "()", func() {
		e.Line("ticker := %s.NewTicker(%s)", timeAlias, outboxCleanupTickInterval)
		e.Line("defer ticker.Stop()")
		e.Block("for", func() {
			e.Block("select", func() {
				e.Line("case <-%s.Done():", ctxVar)
				e.Line("return")
				e.Line("case <-ticker.C:")
			})
			e.Block(fmt.Sprintf("if _, err := %s.Cleanup(%s, %s); err != nil", outboxVar, ctxVar, outboxCleanupRetention), func() {
				e.Line("%s.Error(%q, %q, err)", slogAlias, "outbox: falha na limpeza de linhas entregues", "error")
			})
		})
	})
	return nil
}

// databaseConnectionGo traduz a connection string de db para uma expressão
// Go (J1.3, R1, §design infra-providers 3.1): NUNCA usa
// strconv.Quote(db.DSN) diretamente — esse campo só é populado a partir do
// literal estático "dsn:" (program/graph.go) e fica "" para "env(...)", uma
// forma que db.DSN não resolve (a mesma razão documentada no próprio
// campo). Em vez disso lê a Expr crua de db.Decl.Entries (mesmo padrão de
// telemetryEndpointGo, decl_telemetry.go, para "Telemetry.endpoint"): a
// chave "connection" (spec §12, o nome canônico — ex. `connection:
// env("DB_URL")`) tem prioridade; "dsn" é aceita como sinônimo histórico
// (o mesmo campo semântico, nome mais antigo, ainda usado por fixtures
// sqlite existentes com um caminho de arquivo literal, ex.
// sql_adapter_test.go). Qualquer uma das duas resolve por FORMA: "env(KEY)"
// vira "os.Getenv(KEY)" (envCallKey, decl_io.go); um literal STRING vira
// ele mesmo, entre aspas Go — nunca um valor Go nativo diferente de string,
// já que uma connection string sempre É texto. Nenhuma das duas chaves
// presente cai no comportamento histórico (string vazia) — o mesmo default
// de antes desta task para um Database sem "connection"/"dsn" declarado.
func databaseConnectionGo(e *emit.Emitter, db *program.Database) (string, error) {
	var entries []ast.ConfigEntry
	if db.Decl != nil {
		entries = db.Decl.Entries
	}

	expr, ok := findConfigEntryExpr(entries, "connection")
	if !ok {
		expr, ok = findConfigEntryExpr(entries, "dsn")
	}
	if !ok {
		return strconv.Quote(db.DSN), nil
	}

	if key, isEnv := envCallKey(expr); isEnv {
		osAlias := e.Import("os")
		return fmt.Sprintf("%s.Getenv(%q)", osAlias, key), nil
	}
	if lit, isLit := expr.(*ast.Literal); isLit && lit.Kind == token.STRING {
		return strconv.Quote(lit.Value), nil
	}
	return "", fmt.Errorf(`connection/dsn: forma não suportada (%T) — esperava env("VAR") ou um literal string`, expr)
}
