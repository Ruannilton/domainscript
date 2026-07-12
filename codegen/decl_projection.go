package codegen

import (
	"fmt"
	"strings"
	"unicode"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/types"
)

// decl_projection.go emite o Go de um ProjectionDecl (E8.2, REQ-21.4, §design
// codegen 3.9, spec §6.4): uma view materializada cross-aggregate — um struct
// (um campo por MapEntry) e uma função PURA de recomputação que recebe os
// Aggregates de origem JÁ CARREGADOS (ponteiros) e devolve a projeção.
//
// Escopo reduzido, documentado (ver o prompt da task E8.2): a wiring reativa
// de verdade — recalcular a projeção automaticamente quando um evento de
// RefreshOn é despachado — depende de Policy/Dispatcher, que só existem a
// partir do Marco F (F1 constrói o Dispatcher de verdade). Esta task gera só
// as PEÇAS de cálculo puro: o struct + Compute<Nome>. RefreshOn entra só no
// comentário de doc da função (documenta o gatilho pretendido), nunca vira
// código — não há Dispatcher para registrar um subscriber ainda.
//
// O front-end (resolver/sema) NÃO resolve nomes nem tipos dentro de
// ProjectionDecl.Sources/Map/RefreshOn: resolver/resolver.go só registra o
// SÍMBOLO da própria Projection (symbols.KindProjection) — nenhum outro
// arquivo do front-end (resolver/sema) desce nos campos de ProjectionDecl.
// Este emissor é, portanto, a PRIMEIRA autoridade a interpretar essas formas.
// Ele espelha manualmente o padrão de resolver/receivers.go +
// lower.TypeEnv.SeedX (Handle/Apply/Access/UseCase/Policy, todos em env.go):
// cada nome em Sources vira um receptor de corpo do Map, vinculado ao shape
// do Aggregate homônimo (env.Bind), cujo texto Go é "<param>.state" — a MESMA
// convenção de self/state dentro de um Handle (decl_aggregate.go): os campos
// de um Aggregate moram dentro do struct de state privado, nunca no nível do
// próprio Aggregate. Por isso "Invoice.id" vira "invoice.state.Id", não
// "invoice.Id" — reaproveitando o dispatch normal de MemberExpr
// (lower.Lowerer.member) sem nenhum caso especial: o override de nome
// (BindGoName) + o tipo semeado (env.Bind) já bastam.
//
// Um MapEntry.Value que não seja um MemberExpr sobre um Source ainda
// funciona (Lowerer.Expr cobre qualquer expressão válida sobre o TypeEnv
// semeado) — só falha, com erro de geração claro, se referenciar algo fora
// do que os Sources oferecem (nenhum caso desses é exercitado pela fixture
// desta task, mas não há necessidade de restringir a forma aceita).

// EmitProjection gera o Go de um ProjectionDecl: o struct materializado (um
// campo por MapEntry, tipo inferido do Value via TypeEnv/Lowerer) e uma
// função de recomputação que recebe os Aggregates de origem já carregados e
// devolve a projeção. A wiring reativa via refreshOn/Dispatcher é Marco F+
// (Policy/Dispatcher ainda não existem) — aqui só a peça de cálculo puro.
func EmitProjection(pkg string, decl *ast.ProjectionDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	e := emit.New(pkg)
	if err := emitProjectionDecl(e, decl, model, tab, module, reg); err != nil {
		return nil, fmt.Errorf("codegen: Projection %s: %w", decl.Name, err)
	}
	return e.Bytes()
}

// projectionSource é a forma já resolvida de um nome de ProjectionDecl.Sources:
// o Aggregate homônimo (shape, para tipar o Map), o nome do parâmetro Go
// (camelCase, DISTINTO do nome do tipo — "invoice" vs. "Invoice") e o texto Go
// do seu state ("invoice.state" — ver a doc do arquivo).
type projectionSource struct {
	name    string // nome DomainScript do Aggregate (Sources[i])
	shape   *types.ShapeType
	param   string // nome do parâmetro Go (ex. "invoice")
	stateGo string // texto Go do state do parâmetro (ex. "invoice.state")
}

// resolveProjectionSources resolve cada nome de sources a um Aggregate
// conhecido — mesmo padrão de shapeOf (decl_query.go)/viewFieldInfosFromAggregate
// (decl_view.go): Lookup via lower.TypeEnv.TypeOfName (local ao módulo,
// fallback cross-module), *types.ShapeType de Kind Aggregate. Um nome
// repetido é erro de geração (dois parâmetros Go colidiriam).
func resolveProjectionSources(env *lower.TypeEnv, sources []string) ([]projectionSource, error) {
	seen := make(map[string]bool, len(sources))
	out := make([]projectionSource, 0, len(sources))
	for _, name := range sources {
		if seen[name] {
			return nil, fmt.Errorf("source %q repetido", name)
		}
		seen[name] = true

		t := env.TypeOfName(name)
		if types.IsError(t) {
			return nil, fmt.Errorf("source %s: símbolo não resolvido (bug de geração — o front-end não resolve Sources, ver a doc do arquivo)", name)
		}
		shape, ok := t.(*types.ShapeType)
		if !ok || shape.Kind != symbols.KindAggregate {
			return nil, fmt.Errorf("source %s: não resolve a um Aggregate (got %T)", name, t)
		}

		param := goname.Ident(projectionParamName(name))
		out = append(out, projectionSource{name: name, shape: shape, param: param, stateGo: param + ".state"})
	}
	return out, nil
}

// projectionParamName deriva o nome do parâmetro Go de um Source: a 1ª letra
// minúscula, resto preservado (ex. "Invoice" -> "invoice") — distinto do nome
// do TIPO Go correspondente (que preserva PascalCase, identidade de nome DS
// == Go), como pede a task ("parâmetro invoice *Invoice").
func projectionParamName(sourceName string) string {
	if sourceName == "" {
		return "src"
	}
	r := []rune(sourceName)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// projectionFieldInfo é a forma Go já resolvida de um campo da Projection (um
// MapEntry): o tipo Go do campo (inferido do Value), o nome original (tag
// json) e a expressão Go já lowerizada do Value — mesmo padrão de
// viewFieldInfo (decl_view.go)/commandFieldInfo (decl_command.go).
type projectionFieldInfo struct {
	name       string // nome original do MapEntry (chave da tag json)
	exportName string
	goType     string
	valueGo    string // expressão Go já lowerizada do MapEntry.Value
}

// projectionFieldInfos loweriza cada MapEntry de entries (na ordem
// declarada): a expressão Go do Value (Lowerer.Expr, sobre o TypeEnv já
// semeado com os Sources) e o tipo Go do campo, inferido via
// types.Model.Infer sobre o mesmo TypeEnv (que implementa types.Scope) e
// convertido para a forma Go por viewMemberGoType (decl_view.go — mesma
// função, reusada: ambas resolvem um types.Type JÁ INFERIDO, não um
// *ast.TypeRef).
func projectionFieldInfos(l *lower.Lowerer, env *lower.TypeEnv, module string, entries []ast.MapEntry) ([]projectionFieldInfo, error) {
	infos := make([]projectionFieldInfo, 0, len(entries))
	for _, entry := range entries {
		valueGo, err := l.Expr(entry.Value)
		if err != nil {
			return nil, fmt.Errorf("map %s = ...: %w", entry.Name, err)
		}

		t := env.Model().Infer(module, entry.Value, env)
		if types.IsError(t) {
			return nil, fmt.Errorf("map %s = ...: não consegui inferir o tipo do valor (bug de geração — o front-end não valida Map, ver a doc do arquivo)", entry.Name)
		}
		goType, err := viewMemberGoType(t)
		if err != nil {
			return nil, fmt.Errorf("map %s = ...: %w", entry.Name, err)
		}

		infos = append(infos, projectionFieldInfo{
			name:       entry.Name,
			exportName: goname.ExportField(entry.Name),
			goType:     goType,
			valueGo:    valueGo,
		})
	}
	return infos, nil
}

// emitProjectionDecl emite o struct materializado + a função Compute<Nome> de
// um único ProjectionDecl.
func emitProjectionDecl(e *emit.Emitter, decl *ast.ProjectionDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) error {
	env := lower.New(model, tab, module)
	srcs, err := resolveProjectionSources(env, decl.Sources)
	if err != nil {
		return err
	}

	l := lower.NewLowerer(env, reg, "").WithEmitter(e)
	for _, s := range srcs {
		env.Bind(s.name, s.shape)
		l.BindGoName(s.name, s.stateGo)
	}

	fields, err := projectionFieldInfos(l, env, module, decl.Map)
	if err != nil {
		return err
	}

	emitProjectionStruct(e, decl, fields)
	e.Line("")
	emitProjectionCompute(e, decl, srcs, fields)
	return nil
}

// emitProjectionStruct emite "type <Nome> struct { ... }": um campo
// exportado + tag json por MapEntry, na ordem declarada — mesma forma de
// emitViewDecl (decl_view.go), sem validação (uma Projection é um DTO de
// leitura materializado, como uma View).
func emitProjectionStruct(e *emit.Emitter, decl *ast.ProjectionDecl, fields []projectionFieldInfo) {
	e.Line("// %s é a Projection %s (§6.4): view materializada cross-aggregate,", decl.Name, decl.Name)
	e.Line("// recomputada a partir de %s.", strings.Join(decl.Sources, "/"))
	e.Block(fmt.Sprintf("type %s struct", decl.Name), func() {
		for _, f := range fields {
			e.Line("%s %s %s", f.exportName, f.goType, goname.JSONTag(f.name))
		}
	})
}

// emitProjectionCompute emite "func Compute<Nome>(<Sources...>) <Nome> { ... }":
// um parâmetro ponteiro por Source, na ordem declarada, e um "return
// <Nome>{...}" com um campo por MapEntry, na ordem declarada.
func emitProjectionCompute(e *emit.Emitter, decl *ast.ProjectionDecl, srcs []projectionSource, fields []projectionFieldInfo) {
	funcName := "Compute" + decl.Name
	params := make([]string, len(srcs))
	for i, s := range srcs {
		params[i] = fmt.Sprintf("%s *%s", s.param, s.name)
	}

	e.Line("// %s recomputa a projeção a partir dos Aggregates de origem já", funcName)
	e.Line("// carregados. A atualização reativa em refreshOn (%s) chega quando", refreshOnSummary(decl.RefreshOn))
	e.Line("// Policy/Dispatcher existirem (Marco F).")
	e.Block(fmt.Sprintf("func %s(%s) %s", funcName, strings.Join(params, ", "), decl.Name), func() {
		assigns := make([]string, len(fields))
		for i, f := range fields {
			assigns[i] = fmt.Sprintf("%s: %s", f.exportName, f.valueGo)
		}
		e.Line("return %s{%s}", decl.Name, strings.Join(assigns, ", "))
	})
}

// refreshOnSummary resume ProjectionDecl.RefreshOn para o comentário de doc
// da função Compute<Nome>: o parser sempre produz um *ast.ListExpr de
// *ast.Ident para "refreshOn [A, B]" (parser/parse_decl.go, parseProjection),
// mas esta função é defensiva — qualquer outra forma cai num resumo genérico
// em vez de falhar a geração por causa de um comentário.
func refreshOnSummary(refreshOn ast.Expr) string {
	list, ok := refreshOn.(*ast.ListExpr)
	if !ok {
		return "eventos declarados"
	}
	names := make([]string, 0, len(list.Elems))
	for _, el := range list.Elems {
		if id, ok := el.(*ast.Ident); ok {
			names = append(names, id.Name)
		}
	}
	if len(names) == 0 {
		return "eventos declarados"
	}
	return strings.Join(names, ", ")
}
