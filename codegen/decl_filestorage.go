package codegen

import (
	"fmt"

	"domainscript/codegen/emit"
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
