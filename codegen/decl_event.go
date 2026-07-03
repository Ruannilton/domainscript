package codegen

import (
	"fmt"
	"path"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
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
// --- PublicEvent: o tipo de VERDADE mora no módulo declarante, não em
// contracts/ (decisão desta task, F1, revista — ver o histórico do commit) ---
//
// A primeira tentativa desta task gerou o STRUCT do PublicEvent diretamente
// em contracts/events.go — mas um PublicEvent cujos campos usam ValueObjects
// do módulo de origem (o caso real do shop: "PublicEvent OrderPlaced { id
// OrderId, customer CustomerName, total Money }", todos VOs de Orders) faria
// contracts/ importar orders/ (para os TIPOS dos campos) — e orders/ precisa
// importar contracts/ de volta para o Apply/emit/replay do seu PRÓPRIO
// Aggregate referenciarem o tipo do evento que ele mesmo declara (§design
// 3.4/3.7). Import cycle.
//
// A correção: o STRUCT completo (EventMeta embutido, campos, EventType(),
// (de)serialização) continua sendo emitido no pacote do módulo que declara o
// PublicEvent — TRATADO IGUAL a um Event privado (mesma emitEventDecl, sem
// nenhuma qualificação cross-pacote: os VOs dos seus campos estão no MESMO
// pacote). contracts/events.go vira, por PublicEvent, um ALIAS DE TIPO Go
// ("type OrderPlaced = orders.OrderPlaced", note o "=": identidade de tipo,
// não um novo tipo) — um "índice" que deixa um consumidor cross-module (ex.
// Policy NotifyShipping, decl_policy.go) referenciar "contracts.OrderPlaced"
// sem precisar saber qual módulo de fato o declara, SEM criar um import na
// direção contrária: só contracts/ -> <módulo>, nunca o inverso. Como é
// alias (não um tipo novo), TODOS os métodos (EventType(), SetMeta,
// UnmarshalJSON/Redact quando existirem) promovem automaticamente — nenhuma
// duplicação de código, nenhum registry próprio de contracts/ necessário
// (eventRegistry do módulo de origem já cobre a (de)serialização).

// EmitEvent gera o Go de um único EventDecl: o struct (com runtime.EventMeta
// embutido), EventType(), e um registry de 1 entrada — a mesma forma de
// EmitEvents, para manter o contrato uniforme entre as duas funções. Serve
// tanto Event privado quanto PublicEvent: os dois viram um struct REAL no
// pacote do módulo declarante (ver a doc do arquivo) — a única diferença é o
// comentário de doc gerado ("Event"/"PublicEvent").
func EmitEvent(pkg string, decl *ast.EventDecl) ([]byte, error) {
	return EmitEvents(pkg, []*ast.EventDecl{decl})
}

// EmitEvents gera o Go de vários EventDecl num único arquivo, com um
// registry compartilhado (nome → construtor) — como um módulo real tem
// vários Events (o wallet declara 3). Privados e públicos juntos: ver a doc
// do arquivo — codegen.go passa TODOS os Events (privEvents+pubEvents) de um
// módulo aqui; o roteamento para contracts/ (alias por PublicEvent) é
// EmitPublicEvents, separado.
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

// EmitPublicEvents gera contracts/events.go: um ALIAS DE TIPO Go por
// PublicEvent do programa, apontando para o tipo de verdade no pacote do
// módulo que o declara (ver a doc do arquivo sobre por que não é um struct
// próprio — evita o import cycle contracts↔módulo). moduleOf devolve o
// módulo DomainScript que declarou cada decl (chave: decl.Name — nomes de
// PublicEvent são globalmente únicos, REQ-4.3 barra duplicata via o nível
// público da SymbolTable); um decl ausente de moduleOf é bug de geração (o
// chamador, codegen.go, sempre popula moduleOf a partir do mesmo laço que
// coleta decls).
func EmitPublicEvents(decls []*ast.EventDecl, moduleOf map[string]string) ([]byte, error) {
	e := emit.New("contracts")

	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		module, ok := moduleOf[decl.Name]
		if !ok {
			return nil, fmt.Errorf("codegen: contracts: PublicEvent %s sem módulo de origem conhecido (bug de geração)", decl.Name)
		}
		originAlias := e.Import(path.Join(domainModuleRoot, goname.PackageName(module)))
		qualifiedRef := goname.QualifiedRef(originAlias, decl.Name)

		e.Line("// %s é o PublicEvent %s (§4.2): alias de tipo para %s — o tipo de", decl.Name, decl.Name, qualifiedRef)
		e.Line("// verdade mora no pacote do módulo que o declara, não aqui (evita o import")
		e.Line("// cycle que um struct próprio criaria — ver a doc de decl_event.go).")
		e.Line("type %s = %s", decl.Name, qualifiedRef)
	}

	return e.Bytes()
}

// eventFieldInfo é a forma Go já resolvida de um campo de Event: o tipo Go
// do campo e o nome exportado do campo do struct (goname.ExportField).
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
		goType, err := goname.GoFieldType(f.Type)
		if err != nil {
			return fmt.Errorf("codegen: Event %s: campo %s: %w", decl.Name, f.Name, err)
		}
		infos = append(infos, eventFieldInfo{
			field:      f,
			goType:     goType,
			exportName: goname.ExportField(f.Name),
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
			e.Line("%s %s %s", fi.exportName, fi.goType, goname.JSONTag(fi.field.Name))
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
