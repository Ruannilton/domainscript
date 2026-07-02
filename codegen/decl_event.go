package codegen

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
)

// decl_event.go emite o Go de um EventDecl (E4.2, REQ-18.2/18.3, §design
// 3.4/3.5/3.7): um struct dos campos com o metadata implícito
// (timestamp/sequence/aggregateId/eventType) via embed runtime.EventMeta —
// atribuído no `append` do EventStore (codegen/rtsrc/eventstore.go.txt); a
// chave de stream é o id do aggregate emissor, não o campo `id` do payload
// (§design 3.7) — mais EventType() string (receptor ponteiro, coerente com
// EventMeta.SetMeta, também ponteiro: só *EventDecl satisfaz runtime.Event) e
// um registro num registry (nome → construtor) para (de)serialização quando
// o tipo concreto não é conhecido estaticamente. (De)serialização usa
// encoding/json (stdlib) via as tags `json` de cada campo, com o nome
// original preservado (REQ-18.3).
//
// Field.Default e Field.Redactable (E4.3, REQ-18.4/18.5/18.6, spec
// §4.3/§4.4): quando ao menos 1 campo do Event declara Default, um
// UnmarshalJSON customizado é gerado; quando ao menos 1 campo é Redactable,
// um método Redact() é gerado. Ambos em codegen/decl_event_version.go — nenhum
// Event do wallet usa nenhuma das 2 features (cobertas por fixture sintética
// nos testes daquela task). Events sem nenhuma delas continuam exatamente
// como antes (sem custo extra).
//
// O roteamento de pacote de fato para PublicEvent (pacote compartilhado
// contracts/, §design 3.4) é wiring de projeto (E9.1); aqui pkg só decide o
// nome do pacote Go do arquivo emitido, como em EmitValueObject/EmitError.

// EmitEvent gera o Go de um único EventDecl: o struct (com runtime.EventMeta
// embutido), EventType(), e um registry de 1 entrada — a mesma forma de
// EmitEvents, para manter o contrato uniforme entre as duas funções.
func EmitEvent(pkg string, decl *ast.EventDecl) ([]byte, error) {
	return EmitEvents(pkg, []*ast.EventDecl{decl})
}

// EmitEvents gera o Go de vários EventDecl num único arquivo, com um
// registry compartilhado (nome → construtor) — como um módulo real tem
// vários Events (o wallet declara 3).
func EmitEvents(pkg string, decls []*ast.EventDecl) ([]byte, error) {
	e := emit.New(pkg)
	runtimeAlias := e.Import(RuntimeImportPath)

	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		if err := emitEventDecl(e, runtimeAlias, decl); err != nil {
			return nil, err
		}
	}

	e.Line("")
	emitEventRegistry(e, runtimeAlias, decls)

	return e.Bytes()
}

// eventFieldInfo é a forma Go já resolvida de um campo de Event: o tipo Go
// do campo e o nome exportado do campo do struct (codegen.ExportField).
type eventFieldInfo struct {
	field      *ast.Field
	goType     string
	exportName string
}

// emitEventDecl emite o struct de um único EventDecl (embed runtime.EventMeta
// + campos na ordem declarada) e seu EventType().
func emitEventDecl(e *emit.Emitter, runtimeAlias string, decl *ast.EventDecl) error {
	infos := make([]eventFieldInfo, 0, len(decl.Fields))
	for _, f := range decl.Fields {
		goType, err := GoFieldType(f.Type)
		if err != nil {
			return fmt.Errorf("codegen: Event %s: campo %s: %w", decl.Name, f.Name, err)
		}
		infos = append(infos, eventFieldInfo{
			field:      f,
			goType:     goType,
			exportName: ExportField(f.Name),
		})
	}

	kind := "Event"
	if decl.Public {
		kind = "PublicEvent"
	}
	e.Line("// %s é o %s %s (§4.2): %s.", decl.Name, kind, decl.Name, eventFieldSummary(decl.Fields))
	e.Block(fmt.Sprintf("type %s struct", decl.Name), func() {
		e.Line("%s.EventMeta", runtimeAlias)
		for _, fi := range infos {
			e.Line("%s %s %s", fi.exportName, fi.goType, JSONTag(fi.field.Name))
		}
	})
	e.Line("")
	e.Line("// EventType implementa %s.Event.", runtimeAlias)
	e.Line("func (*%s) EventType() string { return %q }", decl.Name, decl.Name)

	if err := emitEventUnmarshalJSON(e, runtimeAlias, infos, decl); err != nil {
		return err
	}
	emitEventRedact(e, infos, decl)

	return nil
}

// eventFieldSummary resume os campos de um Event para o comentário de doc,
// na ordem declarada: "id WalletId, holder HolderName".
func eventFieldSummary(fields []*ast.Field) string {
	parts := make([]string, len(fields))
	for i, f := range fields {
		parts[i] = fmt.Sprintf("%s %s", f.Name, f.Type.Name)
	}
	return strings.Join(parts, ", ")
}

// emitEventRegistry emite o var eventRegistry: map[string]func() runtime.Event,
// nome do Event → construtor, na ordem de decls (determinismo, NFR-13).
func emitEventRegistry(e *emit.Emitter, runtimeAlias string, decls []*ast.EventDecl) {
	e.Line("// eventRegistry mapeia o nome estável do evento ao seu construtor Go, para")
	e.Line("// (de)serialização quando o tipo concreto não é conhecido estaticamente.")
	e.Block(fmt.Sprintf("var eventRegistry = map[string]func() %s.Event", runtimeAlias), func() {
		for _, decl := range decls {
			e.Line("%q: func() %s.Event { return &%s{} },", decl.Name, runtimeAlias, decl.Name)
		}
	})
}
