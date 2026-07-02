package codegen

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/types"
)

// decl_command.go emite o Go de um CommandDecl (E7.1, REQ-20.1, §design
// 3.5/3.8): um struct DTO com campos exportados e tag json — estruturalmente
// muito parecido com Event (decl_event.go), só SEM runtime.EventMeta,
// EventType() e registry (um Command não é replayado nem versionado como um
// Event; a idempotência do §14 é modelada inteiramente no runtime, nunca como
// campo do DTO — REQ-20.1).
//
// Campo "ref Aggregate" (§design 3.8): o types.Model neutraliza campos ref
// para ErrorType (CtorParams/ctorFields não os modela — REQ-9 só precisava
// validar o nome, não um tipo de valor utilizável). Para o DTO do Command o
// gerador precisa de um tipo Go CONCRETO: a regra é "ref T" → o tipo do campo
// "id" do state de T (ex.: Command Deposit { walletId ref Wallet } + Aggregate
// Wallet { state { id WalletId … } } ⇒ campo Go "WalletId WalletId" — sim, o
// nome do campo Go fica igual ao nome do tipo Go; válido em Go, campo e tipo
// vivem em namespaces diferentes, mesma observação já registrada em
// decl_aggregate.go sobre Command/Handle não colidirem). O NOME do Aggregate
// nunca é usável como tipo de campo (não é um ValueObject com Regra de Ouro;
// é a fronteira de consistência inteira).
//
// refFieldGoType resolve isso reconsultando a SymbolTable + types.Model
// (mesmo padrão de lower.TypeEnv.TypeOfName: Lookup local, fallback Find
// cross-module) para achar o *types.ShapeType do Aggregate e procurar seu
// campo "id" — a MESMA forma que aggregateIDGoType (decl_aggregate.go) usa
// para o campo "id" espelhado do próprio Aggregate. Diferença deliberada
// daquela função: aggregateIDGoType cai para "string" quando o Aggregate não
// declara "id" (campo interno, só de infraestrutura); aqui, um Aggregate
// referenciado por "ref" sem "id" declarado é um ERRO DE GERAÇÃO explícito —
// um Command é um DTO na fronteira de confiança (serialização JSON, borda
// HTTP, E9.2); uma queda silenciosa para "string" produziria um contrato de
// fio incompatível com o tipo real do "id" do Aggregate sem nenhum sinal.
// Como todo Aggregate do spec tem identidade por construção (§4.5), este
// ramo não deveria disparar sobre um programa válido — mas o gerador se
// defende (REQ-14.4): erro claro, nunca panic.
//
// field.Type.String() (não goname.GoFieldType) porque VOType/EnumType/
// ShapeType já devolvem o próprio nome em String() — identidade, a mesma
// convenção de nomes de tipo DomainScript == nomes de tipo Go usada em todo o
// codegen (goname/fieldtype.go também cai nessa identidade para tipos
// declarados). Passar por GoFieldType seria redundante e exigiria reconstruir
// um *ast.TypeRef só para isso.

// EmitCommand gera o Go de um único CommandDecl: o struct DTO — a mesma forma
// de EmitCommands, para manter o contrato uniforme entre as duas funções
// (mesmo padrão de EmitEvent/EmitEvents).
func EmitCommand(pkg string, decl *ast.CommandDecl, model *types.Model, tab *symbols.SymbolTable, module string) ([]byte, error) {
	return EmitCommands(pkg, []*ast.CommandDecl{decl}, model, tab, module)
}

// EmitCommands gera o Go de vários CommandDecl num único arquivo — como um
// módulo real tem vários Commands (o wallet declara 2: Deposit e Withdraw).
func EmitCommands(pkg string, decls []*ast.CommandDecl, model *types.Model, tab *symbols.SymbolTable, module string) ([]byte, error) {
	e := emit.New(pkg)

	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		if err := emitCommandDecl(e, decl, model, tab, module); err != nil {
			return nil, err
		}
	}

	return e.Bytes()
}

// commandFieldInfo é a forma Go já resolvida de um campo de Command: o tipo
// Go do campo e o nome exportado do campo do struct (goname.ExportField) —
// mesmo padrão de eventFieldInfo (decl_event.go)/aggregateStateFieldInfo
// (decl_aggregate.go).
type commandFieldInfo struct {
	field      *ast.Field
	goType     string
	exportName string
}

// emitCommandDecl emite o struct de um único CommandDecl (campos na ordem
// declarada). Resolve todos os tipos ANTES de abrir o bloco (mesmo padrão de
// emitEventDecl) para poder propagar um erro de campo sem precisar de um
// canal de erro através de e.Block.
func emitCommandDecl(e *emit.Emitter, decl *ast.CommandDecl, model *types.Model, tab *symbols.SymbolTable, module string) error {
	infos := make([]commandFieldInfo, 0, len(decl.Fields))
	for _, f := range decl.Fields {
		if f == nil {
			continue
		}
		goType, err := commandFieldGoType(f, model, tab, module)
		if err != nil {
			return fmt.Errorf("codegen: Command %s: campo %s: %w", decl.Name, f.Name, err)
		}
		infos = append(infos, commandFieldInfo{
			field:      f,
			goType:     goType,
			exportName: goname.ExportField(f.Name),
		})
	}

	e.Line("// %s é o Command %s (§5.1): %s.", decl.Name, decl.Name, commandFieldSummary(decl.Fields))
	e.Line("// Idempotência não é campo — modelada no runtime (§14).")
	e.Block(fmt.Sprintf("type %s struct", decl.Name), func() {
		for _, fi := range infos {
			e.Line("%s %s %s", fi.exportName, fi.goType, goname.JSONTag(fi.field.Name))
		}
	})
	return nil
}

// commandFieldGoType devolve o tipo Go de um campo de Command: para um campo
// "ref Aggregate", o tipo do "id" do state do Aggregate referenciado (via
// refFieldGoType, ver doc do arquivo); para um campo normal, o mapeamento
// usual de goname.GoFieldType (mesma forma que decl_event.go já usa).
func commandFieldGoType(f *ast.Field, model *types.Model, tab *symbols.SymbolTable, module string) (string, error) {
	if f.Ref {
		return refFieldGoType(f, model, tab, module)
	}
	return goname.GoFieldType(f.Type)
}

// refFieldGoType resolve o tipo Go de um campo "ref Aggregate" (§design 3.8,
// ver doc do arquivo): acha o símbolo do Aggregate referenciado (mesmo padrão
// de lower.TypeEnv.TypeOfName: Lookup local ao módulo, fallback Find
// cross-module), pega seu *types.ShapeType e procura o campo "id". Devolve
// erro de geração claro (nunca panic, REQ-14.4) quando o Aggregate não
// resolve, não é de fato um Aggregate, ou não declara "id" em seu state.
func refFieldGoType(f *ast.Field, model *types.Model, tab *symbols.SymbolTable, module string) (string, error) {
	if f.Type == nil {
		return "", fmt.Errorf("campo ref sem TypeRef")
	}
	aggName := f.Type.Name

	env := lower.New(model, tab, module)
	t := env.TypeOfName(aggName)
	if types.IsError(t) {
		return "", fmt.Errorf("ref %s: símbolo não resolvido (bug de geração — REQ-9 já deveria ter barrado isso)", aggName)
	}

	shape, ok := t.(*types.ShapeType)
	if !ok || shape.Kind != symbols.KindAggregate {
		return "", fmt.Errorf("ref %s: não resolve a um Aggregate (got %T)", aggName, t)
	}

	for _, field := range shape.Fields {
		if field.Name == "id" {
			// Identidade: VOType/EnumType/ShapeType já devolvem o próprio
			// nome em String() — o nome DomainScript É o nome Go (ver doc do
			// arquivo). Não precisa passar por goname.GoFieldType aqui.
			return field.Type.String(), nil
		}
	}
	return "", fmt.Errorf("ref %s: Aggregate sem campo \"id\" declarado em state (todo Aggregate deveria ter identidade, §4.5 — bug de geração sobre um programa válido)", aggName)
}

// commandFieldSummary resume os campos de um Command para o comentário de
// doc, na ordem declarada: "walletId ref Wallet, amount Money" — mesmo
// padrão de eventFieldSummary (decl_event.go), estendido para marcar "ref".
func commandFieldSummary(fields []*ast.Field) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		if f.Ref {
			parts[i] = fmt.Sprintf("%s ref %s", f.Name, f.Type.Name)
		} else {
			parts[i] = fmt.Sprintf("%s %s", f.Name, f.Type.Name)
		}
	}
	return strings.Join(parts, ", ")
}
