package resolver

import (
	"domainscript/ast"
	"domainscript/diag"
	"domainscript/symbols"
	"domainscript/token"
)

// builtinTypes são os tipos primitivos, coleções e tipos de File embutidos na
// linguagem (§2 do spec). Resolvem sem declaração; qualquer outro nome de tipo
// precisa de uma declaração na tabela de símbolos (REQ-4.2).
var builtinTypes = map[string]bool{
	// primitivos (§2.1)
	"integer": true, "decimal": true, "string": true, "boolean": true,
	"datetime": true, "bytes": true,
	// coleções (§2.4)
	"List": true, "AppendList": true, "Set": true, "Map": true,
	// tipos de File (§2.5)
	"File": true, "FileStream": true, "FileRef": true,
}

// Resolver coleta os símbolos de um ou mais arquivos e resolve as referências
// entre eles em duas passagens (REQ-4.1/4.2). Suporta múltiplos arquivos com
// módulo associado, base para a agregação de programa da Fase 7 (REQ-7).
type Resolver struct {
	tab   *symbols.SymbolTable
	bag   *diag.DiagnosticBag
	units []unit
}

// unit é um arquivo com o módulo a que pertence.
type unit struct {
	module string
	file   *ast.File
}

// New cria um resolver que acumula diagnósticos em bag.
func New(bag *diag.DiagnosticBag) *Resolver {
	return &Resolver{tab: symbols.New(), bag: bag}
}

// Table devolve a tabela de símbolos resultante (fonte de verdade para o checker).
func (r *Resolver) Table() *symbols.SymbolTable { return r.tab }

// Add executa a passagem de coleta sobre file no escopo de module, registrando
// cada declaração nomeada e reportando declarações duplicadas (REQ-4.1/4.3). O
// arquivo é guardado para a passagem de resolução posterior.
func (r *Resolver) Add(module string, file *ast.File) {
	r.units = append(r.units, unit{module, file})
	for _, d := range file.Decls {
		r.collect(module, d)
	}
}

// ResolveAll executa a passagem de resolução sobre todos os arquivos coletados,
// ligando referências (ref, handles, on, tipos de campo/parâmetro) aos símbolos e
// reportando as não resolvidas (REQ-4.2/4.4). Deve ser chamada após Add.
func (r *Resolver) ResolveAll() {
	for _, u := range r.units {
		for _, d := range u.file.Decls {
			r.resolveDecl(u.module, d)
		}
	}
}

// Resolve é o atalho de arquivo único: coleta e resolve um único arquivo num
// módulo anônimo, devolvendo a tabela. Usado pela API CheckSource (REQ-8.1).
func Resolve(file *ast.File, bag *diag.DiagnosticBag) *symbols.SymbolTable {
	r := New(bag)
	r.Add("", file)
	r.ResolveAll()
	return r.Table()
}

// --- passagem de coleta (REQ-4.1) ---

// collect registra a declaração d como símbolo, se for uma declaração nomeada de
// domínio. Nós de erro e declarações de configuração/teste sem identidade de
// símbolo são ignorados (REQ-4.5).
func (r *Resolver) collect(module string, d ast.Decl) {
	var name string
	var kind symbols.Kind
	public := false

	switch n := d.(type) {
	case *ast.ValueObjectDecl:
		name, kind = n.Name, symbols.KindValueObject
	case *ast.EnumDecl:
		name, kind = n.Name, symbols.KindEnum
	case *ast.ErrorTypeDecl:
		name, kind = n.Name, symbols.KindError
	case *ast.EventDecl:
		name, kind, public = n.Name, symbols.KindEvent, n.Public
	case *ast.AggregateDecl:
		name, kind = n.Name, symbols.KindAggregate
	case *ast.CommandDecl:
		name, kind = n.Name, symbols.KindCommand
	case *ast.UseCaseDecl:
		name, kind = n.Name, symbols.KindUseCase
	case *ast.ViewDecl:
		name, kind = n.Name, symbols.KindView
	case *ast.ProjectionDecl:
		name, kind = n.Name, symbols.KindProjection
	case *ast.QueryDecl:
		name, kind = n.Name, symbols.KindQuery
	case *ast.PolicyDecl:
		name, kind = n.Name, symbols.KindPolicy
	case *ast.WorkerDecl:
		name, kind = n.Name, symbols.KindWorker
	case *ast.NotificationDecl:
		name, kind = n.Name, symbols.KindNotification
	case *ast.AdapterDecl:
		name, kind = n.Name, symbols.KindAdapter
	case *ast.SagaDecl:
		name, kind = n.Name, symbols.KindSaga
	case *ast.MetricDecl:
		name, kind = n.Name, symbols.KindMetric
	case *ast.FixtureDecl:
		name, kind = n.Name, symbols.KindFixture
	default:
		// ErrorDecl, ForeignDecl, UpcastDecl, declarações de config (Module,
		// Interface, Topology, Version, RateLimitTier) e Test: sem símbolo de
		// domínio próprio. Ignorados (REQ-4.5).
		return
	}
	if name == "" {
		return // declaração sem nome (recuperação de sintaxe)
	}
	if existing, ok := r.tab.Define(&symbols.Symbol{
		Name: name, Kind: kind, Module: module, Decl: d, Public: public,
	}); !ok {
		if isNotificationAdapterPair(kind, existing.Kind) {
			// Uma Notification e seu Adapter compartilham o nome por design
			// (§9.1/9.3): o Adapter é a fronteira de infra ligada à Notification de
			// mesmo nome. Não é declaração duplicada (REQ-5.3).
			return
		}
		r.bag.Errorf(d.Pos(), "nome duplicado: %s %q já declarado como %s neste módulo",
			kind, name, existing.Kind)
	}
}

// isNotificationAdapterPair reporta se os dois kinds formam o par
// Notification↔Adapter, que por design partilha o mesmo nome no módulo.
func isNotificationAdapterPair(a, b symbols.Kind) bool {
	return (a == symbols.KindNotification && b == symbols.KindAdapter) ||
		(a == symbols.KindAdapter && b == symbols.KindNotification)
}

// --- passagem de resolução (REQ-4.2/4.4/4.5) ---

func (r *Resolver) resolveDecl(module string, d ast.Decl) {
	switch n := d.(type) {
	case *ast.ValueObjectDecl:
		r.resolveType(module, n.Base)
		r.resolveFields(module, n.Fields)
		for _, op := range n.Operators {
			r.resolveFields(module, op.Params)
			r.resolveType(module, op.Return)
		}
	case *ast.EnumDecl:
		r.resolveType(module, n.Base)
	case *ast.EventDecl:
		r.resolveFields(module, n.Fields)
	case *ast.CommandDecl:
		r.resolveFields(module, n.Fields)
	case *ast.AggregateDecl:
		r.resolveFields(module, n.State)
		for _, h := range n.Handlers {
			r.resolveFields(module, h.Params)
		}
	case *ast.UseCaseDecl:
		r.resolveHandles(module, "UseCase", n.Name, n.Handles, n.Pos())
	case *ast.SagaDecl:
		r.resolveHandles(module, "Saga", n.Name, n.Handles, n.Pos())
		r.resolveFields(module, n.State)
	case *ast.PolicyDecl:
		r.resolveOn(module, n.Name, n.On, n.Pos())
	case *ast.QueryDecl:
		r.resolveFields(module, n.Params)
		r.resolveType(module, n.Return)
	case *ast.ViewDecl:
		r.resolveFields(module, n.Fields)
	case *ast.NotificationDecl:
		r.resolveFields(module, n.Fields)
	}
	// As demais declarações não têm referências resolvíveis nesta fase; nós de
	// erro (*ast.ErrorDecl) caem aqui e são ignorados (REQ-4.5).
}

// resolveFields resolve o tipo de cada campo/parâmetro (REQ-4.4). Cobre tanto
// campos comuns quanto os "ref Tipo" (Type aponta para o Aggregate referenciado).
func (r *Resolver) resolveFields(module string, fields []*ast.Field) {
	for _, f := range fields {
		if f == nil {
			continue
		}
		r.resolveType(module, f.Type)
	}
}

// resolveType verifica que o nome de tipo (e seus argumentos genéricos) resolve a
// um tipo embutido ou a um símbolo declarado; do contrário, reporta um erro na
// posição do tipo (REQ-4.2).
func (r *Resolver) resolveType(module string, t *ast.TypeRef) {
	if t == nil || t.Name == "" {
		return // ausente ou nó de recuperação: nada a resolver (REQ-4.5)
	}
	if !builtinTypes[t.Name] {
		if _, ok := r.tab.Lookup(module, t.Name); !ok {
			r.bag.Errorf(t.Pos(), "tipo não declarado: %q", t.Name)
		}
	}
	for _, arg := range t.Args {
		r.resolveType(module, arg)
	}
}

// resolveHandles resolve o Command tratado por um UseCase/Saga (REQ-4.4).
func (r *Resolver) resolveHandles(module, kw, owner, command string, pos token.Pos) {
	if command == "" {
		return
	}
	if _, ok := r.tab.Lookup(module, command); !ok {
		r.bag.Errorf(pos, "%s %q trata Command não declarado: %q", kw, owner, command)
	}
}

// resolveOn resolve o Event ao qual uma Policy reage (REQ-4.4).
func (r *Resolver) resolveOn(module, owner, event string, pos token.Pos) {
	if event == "" {
		return
	}
	if _, ok := r.tab.Lookup(module, event); !ok {
		r.bag.Errorf(pos, "Policy %q reage a Event não declarado: %q", owner, event)
	}
}
