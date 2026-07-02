package codegen

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/token"
)

// decl_event_version.go estende a emissão de Event (decl_event.go, E4.2) com
// versionamento (E4.3, REQ-18.4/18.5/18.6, spec §4.3/§4.4). Três peças
// independentes, exercitadas por fixture sintética (nenhum Event do wallet
// usa nenhuma das 3):
//
//   - Field.Default → um UnmarshalJSON customizado no próprio Event, que só
//     é gerado quando ao menos 1 campo declara Default (emitEventUnmarshalJSON,
//     chamada por emitEventDecl em decl_event.go).
//   - UpcastDecl → EmitUpcast, uma função Go standalone (não presa a um
//     Event específico) que aplica as atribuições do corpo do Upcast sobre o
//     Event já desserializado na forma atual.
//   - Field.Redactable → um método Redact() no Event, gerado quando ao menos
//     1 campo é redactable (emitEventRedact, chamada por emitEventDecl).
//
// Nenhuma das 3 formas participa do lowering geral de corpo (isso é E5+): são
// tradutores pequenos e escopados, no mesmo espírito de vobody.go/
// decl_operator.go — reaproveitam lowerVOLiteral/lowerVOCondition/
// lowerDecimalOperand para o que já é comum, e só acrescentam o que falta
// (construção de VO por nome de tipo, não só chamada de método).

// --- Field.Default → UnmarshalJSON ------------------------------------

// eventDefaultSpec é a forma Go já resolvida do Default de 1 campo: um
// literal pronto para atribuição direta ("literal"), ou uma construção de
// VO/tipo que pode falhar e precisa do padrão New<Tipo>(args) + propagação
// de erro ("construct").
type eventDefaultSpec struct {
	kind      string // "literal" ou "construct"
	literalGo string // kind == "literal": a expressão Go pronta
	typeName  string // kind == "construct": o nome do tipo (New<typeName>)
	argsGo    []string
}

// computeEventDefault traduz Field.Default (uma Expr sem receptores livres —
// só literal ou construção de VO/tipo, ex. Channel("unknown")) para a forma
// Go que a alimenta. Um literal INT atribuído a um campo runtime.Decimal (ex.
// "fee decimal = 0") vira runtime.NewDecimalFromInt(N) — mesma conversão que
// lowerDecimalOperand faz em corpos de VO (codegen/vobody.go) — porque um
// literal Go bruto não é atribuível a um struct Decimal.
func computeEventDefault(runtimeAlias string, fi eventFieldInfo) (eventDefaultSpec, error) {
	switch ex := fi.field.Default.(type) {
	case *ast.Literal:
		if fi.goType == "runtime.Decimal" && ex.Kind == token.INT {
			return eventDefaultSpec{kind: "literal", literalGo: fmt.Sprintf("%s.NewDecimalFromInt(%s)", runtimeAlias, ex.Value)}, nil
		}
		lit, err := lowerVOLiteral(ex)
		if err != nil {
			return eventDefaultSpec{}, fmt.Errorf("codegen: Field.Default de %s: %w", fi.field.Name, err)
		}
		return eventDefaultSpec{kind: "literal", literalGo: lit}, nil

	case *ast.CallExpr:
		fnIdent, ok := ex.Fn.(*ast.Ident)
		if !ok {
			return eventDefaultSpec{}, fmt.Errorf("codegen: Field.Default de %s: construção não suportada, Fn é %T (esperava um nome de tipo)", fi.field.Name, ex.Fn)
		}
		args := make([]string, 0, len(ex.Args))
		for _, a := range ex.Args {
			if a.Name != "" {
				return eventDefaultSpec{}, fmt.Errorf("codegen: Field.Default de %s: argumento nomeado %q não suportado em %s(...) (só posicional)", fi.field.Name, a.Name, fnIdent.Name)
			}
			lit, ok := a.Value.(*ast.Literal)
			if !ok {
				return eventDefaultSpec{}, fmt.Errorf("codegen: Field.Default de %s: argumento de %s(...) deve ser um literal, got %T", fi.field.Name, fnIdent.Name, a.Value)
			}
			litGo, err := lowerVOLiteral(lit)
			if err != nil {
				return eventDefaultSpec{}, err
			}
			args = append(args, litGo)
		}
		return eventDefaultSpec{kind: "construct", typeName: fnIdent.Name, argsGo: args}, nil

	default:
		return eventDefaultSpec{}, fmt.Errorf("codegen: Field.Default de %s: forma de expressão não suportada: %T", fi.field.Name, fi.field.Default)
	}
}

// emitEventUnmarshalJSON gera, quando decl tem ao menos 1 Field.Default !=
// nil, um UnmarshalJSON customizado (chamado por emitEventDecl, decl_event.go
// — E4.2): desserializa via um type alias (evita recursão infinita, já que
// alias não herda os métodos do próprio Event) para os valores normais, e um
// 2º passe num map[string]json.RawMessage só para checar quais chaves
// estavam de fato presentes no payload bruto — o Default só é aplicado
// quando a chave está AUSENTE (não só zero-value: um evento gravado antes do
// campo existir nunca teve essa chave; presente-mas-zero não é sobrescrito).
// Events sem nenhum Field.Default não pagam este custo: nenhuma função é
// gerada, e a (de)serialização padrão de encoding/json continua valendo.
func emitEventUnmarshalJSON(e *emit.Emitter, runtimeAlias string, infos []eventFieldInfo, decl *ast.EventDecl) error {
	type defaulted struct {
		info eventFieldInfo
		spec eventDefaultSpec
	}
	var withDefault []defaulted
	for _, fi := range infos {
		if fi.field.Default == nil {
			continue
		}
		spec, err := computeEventDefault(runtimeAlias, fi)
		if err != nil {
			return err
		}
		withDefault = append(withDefault, defaulted{info: fi, spec: spec})
	}
	if len(withDefault) == 0 {
		return nil
	}

	jsonAlias := e.Import("encoding/json")

	e.Line("")
	e.Line("// UnmarshalJSON desserializa %s aplicando os Field.Default (spec §4.3) aos", decl.Name)
	e.Line("// campos cuja chave está ausente do payload bruto — não só zero-value: um")
	e.Line("// evento gravado antes do campo existir nunca teve essa chave; presente-mas-")
	e.Line("// zero não é sobrescrito pelo default.")
	e.Block(fmt.Sprintf("func (e *%s) UnmarshalJSON(data []byte) error", decl.Name), func() {
		e.Line("type alias %s", decl.Name)
		e.Block(fmt.Sprintf("if err := %s.Unmarshal(data, (*alias)(e)); err != nil", jsonAlias), func() {
			e.Line("return err")
		})
		e.Line("")
		e.Line("var raw map[string]%s.RawMessage", jsonAlias)
		e.Block(fmt.Sprintf("if err := %s.Unmarshal(data, &raw); err != nil", jsonAlias), func() {
			e.Line("return err")
		})
		for _, d := range withDefault {
			e.Line("")
			e.Block(fmt.Sprintf("if _, present := raw[%q]; !present", d.info.field.Name), func() {
				emitEventDefaultAssign(e, d.info.exportName, d.spec)
			})
		}
		e.Line("")
		e.Line("return nil")
	})
	return nil
}

// emitEventDefaultAssign emite a atribuição do Default já resolvido
// (computeEventDefault) dentro do "if !present" de um campo. Um Default
// "construct" (ex. New Channel("unknown")) pode falhar — o erro é
// propagado pelo UnmarshalJSON (mais idiomático que panic; o Default
// declarado no .ds DEVERIA ser sempre válido, mas nada garante isso
// estaticamente hoje, então tratamos como um erro de runtime normal).
func emitEventDefaultAssign(e *emit.Emitter, exportName string, spec eventDefaultSpec) {
	switch spec.kind {
	case "literal":
		e.Line("e.%s = %s", exportName, spec.literalGo)
	case "construct":
		e.Line("defaultVal, err := New%s(%s)", spec.typeName, strings.Join(spec.argsGo, ", "))
		e.Block("if err != nil", func() {
			e.Line("return err")
		})
		e.Line("e.%s = defaultVal", exportName)
	}
}

// --- Field.Redactable → Redact() ---------------------------------------

// emitEventRedact gera, quando decl tem ao menos 1 Field.Redactable == true,
// um método Redact() (chamado por emitEventDecl, decl_event.go — E4.2) que
// zera cada campo redactable com o valor zero do seu tipo Go — preservando a
// estrutura do evento (spec §4.4, GDPR): o campo continua lá, só com um
// placeholder neutro, então (de)serialização/replay não quebram. "var zero T"
// é usado em vez de "T{}" porque T pode ser um wrapper de VO sobre um
// primitivo (ex. "type HolderName string"), para o qual "T{}" não é um
// literal composto válido em Go — "var zero T" vale para qualquer tipo.
func emitEventRedact(e *emit.Emitter, infos []eventFieldInfo, decl *ast.EventDecl) {
	var redactable []eventFieldInfo
	for _, fi := range infos {
		if fi.field.Redactable {
			redactable = append(redactable, fi)
		}
	}
	if len(redactable) == 0 {
		return
	}

	e.Line("")
	e.Line("// Redact substitui os campos redactable de %s por um placeholder tipado", decl.Name)
	e.Line("// neutro (spec §4.4, GDPR): a estrutura do evento continua válida para replay.")
	e.Block(fmt.Sprintf("func (e *%s) Redact()", decl.Name), func() {
		for _, fi := range redactable {
			zeroVar := "zero" + fi.exportName
			e.Line("var %s %s", zeroVar, fi.goType)
			e.Line("e.%s = %s", fi.exportName, zeroVar)
		}
	})
}

// --- UpcastDecl → EmitUpcast --------------------------------------------

// upcastAssign é a forma Go já resolvida de 1 atribuição do corpo de Upcast:
// ou uma expressão simples atribuída direto ("event.<Campo> = <simple>"), ou
// uma construção de VO que pode falhar ("<localVar>, err := New<typeName>
// (argsGo...); if err != nil {...}; event.<Campo> = <localVar>").
type upcastAssign struct {
	targetExport string
	simple       string
	isConstruct  bool
	localVar     string
	typeName     string
	argsGo       []string
}

// EmitUpcast gera a função Go de upcast a partir de um UpcastDecl (spec
// §4.3): recebe o Event já desserializado na forma ATUAL (o struct que
// EmitEvent gera — sempre a forma mais recente) e aplica as atribuições do
// corpo do Upcast, preenchendo os campos que a versão antiga não tinha.
//
// currentFields é a lista de campos do Event na forma atual — usada para
// validar que cada alvo de atribuição do corpo (`fee = ...`) de fato existe
// nessa forma (defesa contra AST inconsistente; o front-end garante isso num
// programa válido). vos é o conjunto de ValueObjectDecl que o corpo do
// Upcast referencia em construções (ex. Money(amount: ..., currency: ...)):
// como o construtor New<Tipo> recebe argumentos POSICIONAIS mas o corpo do
// Upcast os passa NOMEADOS, EmitUpcast precisa da ordem declarada dos campos
// de cada VO para casar nome→posição (mesmo padrão de
// lowerOperatorSelfConstruct em decl_operator.go, generalizado para um VO
// que não é o Event sendo upcastado).
func EmitUpcast(pkg string, decl *ast.UpcastDecl, currentFields []*ast.Field, vos []*ast.ValueObjectDecl) ([]byte, error) {
	vosByName := make(map[string]*ast.ValueObjectDecl, len(vos))
	for _, vo := range vos {
		vosByName[vo.Name] = vo
	}
	currentByName := make(map[string]*ast.Field, len(currentFields))
	for _, f := range currentFields {
		currentByName[f.Name] = f
	}

	e := emit.New(pkg)
	var runtimeAlias string
	if upcastNeedsRuntimeImport(decl, vosByName) {
		runtimeAlias = e.Import(RuntimeImportPath)
	}

	scope := newVOScope(runtimeAlias)
	scope.bind("event", "event", decl.Event)

	assigns := make([]upcastAssign, 0, len(decl.Body.Stmts))
	for _, st := range decl.Body.Stmts {
		assignStmt, ok := st.(*ast.AssignStmt)
		if !ok {
			return nil, fmt.Errorf("codegen: Upcast %s %s->%s: corpo só suporta atribuição (*ast.AssignStmt), got %T", decl.Event, decl.FromVer, decl.ToVer, st)
		}
		a, err := computeUpcastAssign(scope, vosByName, currentByName, assignStmt)
		if err != nil {
			return nil, fmt.Errorf("codegen: Upcast %s %s->%s: %w", decl.Event, decl.FromVer, decl.ToVer, err)
		}
		assigns = append(assigns, a)
	}

	funcName := UpcastFuncName(decl)
	e.Line("// %s aplica o Upcast declarado de %s para %s (spec §4.3):", funcName, decl.FromVer, decl.ToVer)
	e.Line("// preenche os campos que a versão %s não tinha.", decl.FromVer)
	e.Block(fmt.Sprintf("func %s(event *%s) (*%s, error)", funcName, decl.Event, decl.Event), func() {
		for _, a := range assigns {
			emitUpcastAssign(e, a)
		}
		e.Line("return event, nil")
	})

	return e.Bytes()
}

// UpcastFuncName constrói o nome Go da função de upcast (convenção desta
// task): "Upcast" + Event + FromVer capitalizado + "To" + ToVer capitalizado.
// Ex.: UpcastFuncName({Event: "TransferSent", FromVer: "v1", ToVer: "v2"}) →
// "UpcastTransferSentV1ToV2". ExportField já capitaliza só a 1ª letra, que é
// exatamente "v1" → "V1".
func UpcastFuncName(decl *ast.UpcastDecl) string {
	return fmt.Sprintf("Upcast%s%sTo%s", decl.Event, ExportField(decl.FromVer), ExportField(decl.ToVer))
}

// upcastNeedsRuntimeImport faz uma pré-varredura do corpo do Upcast: só
// precisamos do import do runtime vendorado se alguma construção de VO
// composto passar um literal INT para um campo decimal (aí a emissão usa
// runtime.NewDecimalFromInt — a mesma conversão de lowerDecimalOperand,
// codegen/vobody.go). Feito ANTES de qualquer emit.Emitter.Line/Block, porque
// emit.Bytes() rejeita um import registrado e nunca referenciado no corpo.
func upcastNeedsRuntimeImport(decl *ast.UpcastDecl, vosByName map[string]*ast.ValueObjectDecl) bool {
	for _, st := range decl.Body.Stmts {
		assign, ok := st.(*ast.AssignStmt)
		if !ok {
			continue
		}
		call, ok := assign.Value.(*ast.CallExpr)
		if !ok {
			continue
		}
		fnIdent, ok := call.Fn.(*ast.Ident)
		if !ok {
			continue
		}
		vo, ok := vosByName[fnIdent.Name]
		if !ok || vo.Base != nil {
			continue
		}
		byName := make(map[string]ast.Expr, len(call.Args))
		for _, a := range call.Args {
			byName[a.Name] = a.Value
		}
		for _, f := range vo.Fields {
			if f.Type == nil || f.Type.Name != "decimal" {
				continue
			}
			val, ok := byName[f.Name]
			if !ok {
				continue
			}
			if lit, ok := val.(*ast.Literal); ok && lit.Kind == token.INT {
				return true
			}
		}
	}
	return false
}

// computeUpcastAssign traduz 1 *ast.AssignStmt do corpo de Upcast (Target é
// sempre um *ast.Ident nu — o nome do campo NOVO que a versão de origem não
// tinha) para a forma Go: construção de VO (Value é Type(args...), Fn não é
// um MemberExpr — logo não é chamada de método) ou atribuição simples
// (qualquer outra forma, delegada a lowerVOCondition — cobre o acesso a
// membro "event.amount.currency" via lowerVOFieldAccess, vobody.go).
func computeUpcastAssign(scope voScope, vosByName map[string]*ast.ValueObjectDecl, currentByName map[string]*ast.Field, st *ast.AssignStmt) (upcastAssign, error) {
	targetIdent, ok := st.Target.(*ast.Ident)
	if !ok {
		return upcastAssign{}, fmt.Errorf("alvo de atribuição deve ser um nome nu, got %T", st.Target)
	}
	if _, ok := currentByName[targetIdent.Name]; !ok {
		return upcastAssign{}, fmt.Errorf("atribui o campo %q, que não existe na forma atual do Event (currentFields)", targetIdent.Name)
	}
	exportName := ExportField(targetIdent.Name)

	if call, ok := st.Value.(*ast.CallExpr); ok {
		if _, isMethodCall := call.Fn.(*ast.MemberExpr); !isMethodCall {
			args, typeName, err := lowerUpcastConstructArgs(scope, vosByName, call)
			if err != nil {
				return upcastAssign{}, err
			}
			return upcastAssign{
				targetExport: exportName,
				isConstruct:  true,
				localVar:     Ident(targetIdent.Name),
				typeName:     typeName,
				argsGo:       args,
			}, nil
		}
	}

	valGo, err := lowerVOCondition(scope, st.Value)
	if err != nil {
		return upcastAssign{}, err
	}
	return upcastAssign{targetExport: exportName, simple: valGo}, nil
}

// lowerUpcastConstructArgs traduz "Type(args...)" (Fn é um *ast.Ident, não
// chamada de método) para a lista de argumentos POSICIONAIS de New<Type> —
// casando args nomeados contra vo.Fields na ORDEM DECLARADA (como
// lowerOperatorSelfConstruct, decl_operator.go, mas para um VO que não é o
// que está sendo definido — por isso precisa de vosByName, resolvido a
// partir do parâmetro vos de EmitUpcast). Campo decimal recebendo um literal
// INT (ex. "amount: 0") vira runtime.NewDecimalFromInt(0) via
// lowerDecimalOperand; qualquer outra forma (ex. "event.amount.currency")
// delega a lowerVOCondition, que já produz o tipo Go certo por construção
// (acesso a campo puro sobre um valor já tipado).
func lowerUpcastConstructArgs(scope voScope, vosByName map[string]*ast.ValueObjectDecl, call *ast.CallExpr) (args []string, typeName string, err error) {
	fnIdent, ok := call.Fn.(*ast.Ident)
	if !ok {
		return nil, "", fmt.Errorf("construção não suportada em corpo de Upcast: Fn é %T (esperava um nome de tipo)", call.Fn)
	}
	vo, ok := vosByName[fnIdent.Name]
	if !ok {
		return nil, "", fmt.Errorf("construção em corpo de Upcast referencia ValueObject desconhecido: %s (passe o ValueObjectDecl correspondente em EmitUpcast)", fnIdent.Name)
	}

	if vo.Base != nil {
		if len(call.Args) != 1 || call.Args[0].Name != "" {
			return nil, "", fmt.Errorf("construção de %s (wrapper) em Upcast espera exatamente 1 argumento posicional", fnIdent.Name)
		}
		goVal, err := lowerVOCondition(scope, call.Args[0].Value)
		if err != nil {
			return nil, "", err
		}
		return []string{goVal}, fnIdent.Name, nil
	}

	byName := make(map[string]ast.Expr, len(call.Args))
	for _, a := range call.Args {
		if a.Name == "" {
			return nil, "", fmt.Errorf("construção de %s em Upcast precisa de argumentos nomeados, achei um posicional", fnIdent.Name)
		}
		byName[a.Name] = a.Value
	}

	out := make([]string, 0, len(vo.Fields))
	for _, f := range vo.Fields {
		val, ok := byName[f.Name]
		if !ok {
			return nil, "", fmt.Errorf("construção de %s em Upcast não informa o campo %q", fnIdent.Name, f.Name)
		}
		var goVal string
		var lowerErr error
		if f.Type != nil && f.Type.Name == "decimal" {
			goVal, lowerErr = lowerDecimalOperand(scope, val)
		} else {
			goVal, lowerErr = lowerVOCondition(scope, val)
		}
		if lowerErr != nil {
			return nil, "", lowerErr
		}
		out = append(out, goVal)
	}
	return out, fnIdent.Name, nil
}

// emitUpcastAssign emite 1 upcastAssign já resolvido. Nenhuma chamada aqui
// pode falhar: todo erro possível já foi checado em computeUpcastAssign/
// lowerUpcastConstructArgs, ANTES de qualquer emissão — o mesmo motivo de
// emitEventDefaultAssign (emit.Emitter.Block não devolve erro).
func emitUpcastAssign(e *emit.Emitter, a upcastAssign) {
	if a.isConstruct {
		e.Line("%s, err := New%s(%s)", a.localVar, a.typeName, strings.Join(a.argsGo, ", "))
		e.Block("if err != nil", func() {
			e.Line("return nil, err")
		})
		e.Line("event.%s = %s", a.targetExport, a.localVar)
		return
	}
	e.Line("event.%s = %s", a.targetExport, a.simple)
}
