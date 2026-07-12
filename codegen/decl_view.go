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

// decl_view.go emite o Go de um ViewDecl (E8.1, REQ-21.1, §design 3.9): um
// struct de leitura, estruturalmente igual a Command/Event (decl_command.go/
// decl_event.go) — campos exportados + tag json, SEM validação (NewX) e SEM
// metadata (EventMeta/EventType/registry): uma View é um DTO de leitura pura.
//
// "From Aggregate" (ViewDecl.From != ""; o wallet só usa a forma de campos
// próprios — WalletView declara os 3 campos explicitamente, From == "") NÃO é
// exercitado pelo wallet, mas é barato o bastante para cobrir: quando
// From != "" e a View não declara nenhum campo próprio (a forma do spec v6
// §6.1, "View X From Aggregate", sem bloco de campos — só "visibility" pode
// acompanhar essa forma), os campos são projetados 1:1 do state do Aggregate
// referenciado, na ordem declarada do state (mesmo padrão de ref-resolution
// de decl_command.go/refFieldGoType: Lookup via lower.TypeEnv.TypeOfName,
// tipo concreto via types.Model). Quando a View declara campos PRÓPRIOS
// (independente de From), esses campos vencem — é a única forma exercitada
// por um teste real (E8.1). A combinação "From + campos próprios" (um
// subconjunto/renomeação da projeção) não tem exemplo no spec nem no wallet e
// fica documentada como não coberta: fica estrutural/não testada.
//
// "visibility" (bloco de field-level security, §6.2) não é implementado por
// esta task: nenhuma View do wallet o declara, e materializá-lo exigiria
// serialização condicional por caller (design §3.9, "campos omitidos
// condicionalmente na serialização") — fora do escopo mínimo desta task; o
// struct gerado sempre serializa todos os campos.

// EmitView gera o Go de um ViewDecl: struct de leitura com campos exportados
// + tag json — igual a Command/Event, sem validação. From != "" sem campos
// próprios projeta os campos do state do Aggregate referenciado (ver a doc do
// arquivo).
func EmitView(pkg string, decl *ast.ViewDecl, model *types.Model, tab *symbols.SymbolTable, module string) ([]byte, error) {
	e := emit.New(pkg)
	if err := emitViewDecl(e, decl, model, tab, module); err != nil {
		return nil, err
	}
	return e.Bytes()
}

// viewFieldInfo é a forma Go já resolvida de um campo de View: o tipo Go do
// campo, o nome original (para a tag json) e o nome exportado do campo do
// struct — mesmo padrão de commandFieldInfo/eventFieldInfo.
type viewFieldInfo struct {
	name       string
	goType     string
	exportName string
}

func emitViewDecl(e *emit.Emitter, decl *ast.ViewDecl, model *types.Model, tab *symbols.SymbolTable, module string) error {
	infos, err := viewFieldInfos(decl, model, tab, module)
	if err != nil {
		return fmt.Errorf("codegen: View %s: %w", decl.Name, err)
	}
	// Um campo cujo goType é "runtime.X" (ex. "decimal" → runtime.Decimal,
	// goname/types.go) precisa do import do runtime vendorado — gap
	// pré-existente nunca exercitado antes do ciclo Read Side (mesmo padrão
	// de needsRuntime em decl_value.go:emitValueObjectComposite): nenhuma
	// View real do wallet tinha um campo decimal direto até a projeção "as
	// V" (I3.2) achatar um Money (amount_value decimal) para dentro de
	// StatementEntryVW.
	for _, fi := range infos {
		if strings.HasPrefix(fi.goType, "runtime.") {
			e.Import(RuntimeImportPath)
			break
		}
	}

	e.Line("// %s é a View %s (§6.1): %s.", decl.Name, decl.Name, viewFieldSummary(decl, infos))
	e.Line("// Struct de leitura pura — sem validação, sem metadata.")
	e.Block(fmt.Sprintf("type %s struct", decl.Name), func() {
		for _, fi := range infos {
			e.Line("%s %s %s", fi.exportName, fi.goType, goname.JSONTag(fi.name))
		}
	})
	return nil
}

// viewFieldInfos resolve os campos Go de decl: campos próprios quando
// declarados (independente de From — ver a doc do arquivo); senão, projeção
// do state do Aggregate de From, se houver; senão, nenhum campo (struct
// vazio — defensivo, não deveria acontecer sobre um programa validado).
func viewFieldInfos(decl *ast.ViewDecl, model *types.Model, tab *symbols.SymbolTable, module string) ([]viewFieldInfo, error) {
	if len(decl.Fields) > 0 {
		infos := make([]viewFieldInfo, 0, len(decl.Fields))
		for _, f := range decl.Fields {
			if f == nil {
				continue
			}
			goType, err := goname.GoFieldType(f.Type)
			if err != nil {
				return nil, fmt.Errorf("campo %s: %w", f.Name, err)
			}
			infos = append(infos, viewFieldInfo{name: f.Name, goType: goType, exportName: goname.ExportField(f.Name)})
		}
		return infos, nil
	}
	if decl.From != "" {
		return viewFieldInfosFromAggregate(decl, model, tab, module)
	}
	return nil, nil
}

// viewFieldInfosFromAggregate projeta os campos do state do Aggregate
// decl.From (§6.1: "View X From Aggregate", sem bloco de campos próprios) —
// mesmo padrão de refFieldGoType (decl_command.go): Lookup via
// lower.TypeEnv.TypeOfName, *types.ShapeType de Kind Aggregate, campos NA
// ORDEM declarada do state (types.Model já preserva essa ordem).
func viewFieldInfosFromAggregate(decl *ast.ViewDecl, model *types.Model, tab *symbols.SymbolTable, module string) ([]viewFieldInfo, error) {
	env := lower.New(model, tab, module)
	t := env.TypeOfName(decl.From)
	if types.IsError(t) {
		return nil, fmt.Errorf("From %s: símbolo não resolvido (bug de geração — REQ-9 já deveria ter barrado isso)", decl.From)
	}
	shape, ok := t.(*types.ShapeType)
	if !ok || shape.Kind != symbols.KindAggregate {
		return nil, fmt.Errorf("From %s: não resolve a um Aggregate (got %T)", decl.From, t)
	}

	infos := make([]viewFieldInfo, 0, len(shape.Fields))
	for _, f := range shape.Fields {
		goType, err := viewMemberGoType(f.Type)
		if err != nil {
			return nil, fmt.Errorf("From %s: campo %s: %w", decl.From, f.Name, err)
		}
		infos = append(infos, viewFieldInfo{name: f.Name, goType: goType, exportName: goname.ExportField(f.Name)})
	}
	return infos, nil
}

// viewMemberGoType mapeia um types.Type JÁ RESOLVIDO (não um *ast.TypeRef —
// por isso não dá para reusar goname.GoFieldType aqui) para a forma Go
// correspondente: primitivo/opaco via goname.GoPrimitive/GoOpaqueType,
// VO/Enum/Shape por identidade de nome (mesma convenção do resto do
// codegen), Generic recursivamente via goname.GoGeneric. Espelha
// lower.goTypeString (não exportado por aquele pacote) — pequena duplicação
// documentada, aceitável para não expor uma API interna do lowering só para
// este uso pontual (projeção de campos de state, sem lowering de corpo).
func viewMemberGoType(t types.Type) (string, error) {
	switch x := t.(type) {
	case *types.Primitive:
		if s, ok := goname.GoPrimitive(x.Name); ok {
			return s, nil
		}
		if s, ok := goname.GoOpaqueType(x.Name); ok {
			return s, nil
		}
		return "", fmt.Errorf("primitivo desconhecido: %s", x.Name)
	case *types.VOType, *types.EnumType, *types.ShapeType:
		return t.String(), nil // identidade: nome DS == nome Go
	case *types.Generic:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			s, err := viewMemberGoType(a)
			if err != nil {
				return "", err
			}
			args[i] = s
		}
		s, ok := goname.GoGeneric(x.Ctor, args)
		if !ok {
			return "", fmt.Errorf("construtor genérico desconhecido ou aridade inválida: %s", x.Ctor)
		}
		return s, nil
	default:
		return "", fmt.Errorf("tipo sem forma Go conhecida: %s (%T)", t.String(), t)
	}
}

// viewFieldSummary resume os campos de decl para o comentário de doc — mesmo
// padrão de eventFieldSummary/commandFieldSummary; para a forma "From
// Aggregate" sem campos próprios, resume como projeção em vez de listar
// campo a campo (infos só é conhecido depois da resolução).
func viewFieldSummary(decl *ast.ViewDecl, infos []viewFieldInfo) string {
	if decl.From != "" && len(decl.Fields) == 0 {
		return fmt.Sprintf("projeção do state de %s", decl.From)
	}
	parts := make([]string, len(infos))
	for i, fi := range infos {
		parts[i] = fmt.Sprintf("%s %s", fi.name, fi.goType)
	}
	return strings.Join(parts, ", ")
}
