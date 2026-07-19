package codegen

import (
	"fmt"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/program"
	"domainscript/token"
)

// decl_filestorage.go emite o wiring de FileStorage de um módulo (G1a, §2.5,
// REQ-22.7(b)/REQ-25/REQ-26): o registro "fileStorages" que
// codegen/lower/builtins.go (BuiltinLowerer.fileStorageGoExpr) referencia em
// TODA chamada de store/signed_url/delete file/load File(ref) — mesmo
// padrão de "var uow runtime.UnitOfWork"/Wire (decl_usecase.go), agora
// indexado por NOME (não um único valor): um módulo pode declarar mais de
// uma FileStorage (ex. "avatar"/"document" do exemplo do spec §2.5), cada
// uma injetada separadamente por cmd/<service>/main.go na inicialização
// (ver emitFileStorageMainWiring, codegen.go).

// emitFileStorageWiring gera "filestorage.go": a declaração de pacote do
// registro (já inicializado com um map literal vazio — ao contrário de "var
// uow", não precisa de zero-value especial, então não há necessidade de uma
// checagem de nil-map em WireFileStorage) e WireFileStorage, a função que
// cmd/<service>/main.go chama, uma vez por FileStorage do módulo, na
// inicialização.
func emitFileStorageWiring(pkg string) ([]byte, error) {
	e := emit.New(pkg)
	runtimeAlias := e.Import(RuntimeImportPath)

	e.Line("// fileStorages é o registro de FileStorage do módulo (mod.ds, §2.5),")
	e.Line("// indexado pelo NOME declarado — store/signed_url/delete file/load File(ref)")
	e.Line("// (G1a) resolvem a instância certa por esse nome (ver")
	e.Line("// codegen/lower/builtins.go, BuiltinLowerer.fileStorageGoExpr). As instâncias")
	e.Line("// de verdade são injetadas por WireFileStorage, chamada por")
	e.Line("// cmd/<service>/main.go na inicialização (mesmo padrão de Wire/uow,")
	e.Line("// decl_usecase.go).")
	e.Line("var fileStorages = map[string]%s.FileStorage{}", runtimeAlias)
	e.Line("")
	e.Line("// WireFileStorage registra a instância de uma FileStorage declarada (mod.ds)")
	e.Line("// sob seu nome — chamada por cmd/<service>/main.go na inicialização, uma vez")
	e.Line("// por FileStorage do módulo.")
	e.Block(fmt.Sprintf("func WireFileStorage(name string, fs %s.FileStorage)", runtimeAlias), func() {
		e.Line("fileStorages[name] = fs")
	})

	return e.Bytes()
}

// fileStorageProvider lê "provider" de fs.Decl.Entries — "" quando ausente
// (backend in-memory de sempre). Mesmo helper (configStringLitEntry,
// decl_telemetry.go) que activeProviderDeps (provider_registry.go) já usa
// para resolver fileProviders["s3"] a partir de uma FileStorage (task
// J5.2, R2).
func fileStorageProvider(fs *program.FileStorage) (string, error) {
	if fs == nil || fs.Decl == nil {
		return "", nil
	}
	provider, ok, err := configStringLitEntry(fs.Decl.Entries, "provider")
	if err != nil {
		return "", fmt.Errorf("FileStorage %s: provider: %w", fs.Name, err)
	}
	if !ok {
		return "", nil
	}
	return provider, nil
}

// fileStorageProviderKind normaliza fileStorageProvider(fs) contra
// fileProviders (o registro real, J5.1) — "" quando ausente OU não
// reconhecido (o backend in-memory de sempre, NFR-21), "s3" quando
// reconhecido. Mesmo padrão de channelProviderKind/cacheBackendKind/
// rateLimitBackendKind: um provider declarado mas NÃO reconhecido (ex.
// "gcs", ainda não implementado) cai silenciosamente no caminho in-memory,
// nunca um erro de geração.
func fileStorageProviderKind(fs *program.FileStorage) (string, error) {
	provider, err := fileStorageProvider(fs)
	if err != nil || provider == "" {
		return "", err
	}
	if _, known := fileProviders[strings.ToLower(provider)]; known {
		return strings.ToLower(provider), nil
	}
	return "", nil
}

// fileStorageConfigGo traduz o valor de key ("bucket"/"region") no
// Decl.Entries de fs para uma expressão Go (task J5.2, R1) — mesmo padrão de
// channelConnectionGo/cacheConnectionGo/rateLimitConnectionGo:
// "env(VAR)" vira "os.Getenv(VAR)"; um literal STRING vira ele mesmo, entre
// aspas Go. Só chamada quando fileStorageProviderKind já confirmou "s3" — a
// chave ausente é um erro de geração claro (fail-closed), não uma string
// vazia silenciosa (spec §12: "DocumentStorage { provider: \"s3\", bucket:
// env(\"DOCUMENTS_BUCKET\"), region: env(\"AWS_REGION\") }").
func fileStorageConfigGo(e *emit.Emitter, fs *program.FileStorage, key string) (string, error) {
	var entries []ast.ConfigEntry
	if fs.Decl != nil {
		entries = fs.Decl.Entries
	}

	expr, ok := findConfigEntryExpr(entries, key)
	if !ok {
		return "", fmt.Errorf(`FileStorage %s: provider "s3" exige %q (ex. %s: env("..."))`, fs.Name, key, key)
	}

	if envKey, isEnv := envCallKey(expr); isEnv {
		osAlias := e.Import("os")
		return fmt.Sprintf("%s.Getenv(%q)", osAlias, envKey), nil
	}
	if lit, isLit := expr.(*ast.Literal); isLit && lit.Kind == token.STRING {
		return strconv.Quote(lit.Value), nil
	}
	return "", fmt.Errorf(`FileStorage %s: %s: forma não suportada (%T) — esperava env("VAR") ou um literal string`, fs.Name, key, expr)
}
