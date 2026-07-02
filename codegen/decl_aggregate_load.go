package codegen

import (
	"fmt"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/types"
)

// decl_aggregate_load.go emite a reconstrução (Load<Nome>) de um Aggregate —
// E6.2, REQ-19.4/19.5, §design 3.7. ESTENDE a emissão de decl_aggregate.go
// (E6.1): aquele arquivo já gera o struct de state, o tipo do Aggregate
// (id+version+state) e os métodos Handle/Apply; esta task só ACRESCENTA a
// função Load<Nome> no mesmo pacote, sobre os MESMOS tipos — não redeclara
// nada.
//
// Sincronização do espelho "id" (ver o comentário de decl_aggregate.go sobre
// self/state/id): o campo Wallet.id só existe para infraestrutura
// (chave de stream/endereçamento) e Handle/Apply nunca o leem — só state.Id.
// EventSourced é o ÚNICO lugar que de fato sincroniza os dois, porque é o
// único caminho que constrói um Aggregate a partir de dados EXTERNOS (o
// stream) em vez de a partir de um Handle "de negócio" que nunca toca id.
//
// EventSourced (REQ-19.4): Load<Nome> lê o stream via Tx.Load — chave =
// aggregateId (metadata do store), nunca um campo "id" do payload do evento,
// mesma convenção de EventStore.Append/decl_aggregate.go — e aplica cada
// evento, NA ORDEM DO STREAM (não na ordem declarada de Appliers — essa ordem
// só decide quais "case"s o switch tem), via o applyX correspondente.
//
// StateStored (REQ-19.5, o padrão do spec quando "strategy" está ausente do
// bloco Aggregate, §4.5): lê o state direto de um Repository[<stateName>] —
// sem replay algum. Os mesmos Handle/Apply de E6.1 valem; só a fonte de
// persistência muda.
//
// Assimetria de ctx entre as duas formas (documentada aqui, única vez —
// §design 3.1a): runtime.Tx já carrega o context.Context com que a unit of
// work foi aberta (mesmo padrão que Append já usa, ver uow.go.txt) — por
// isso Load<Nome> EventSourced não precisa de um parâmetro ctx explícito.
// runtime.Repository[T], ao contrário, pede ctx em toda chamada
// (codegen/rtsrc/repository.go.txt: "Load(ctx, id) (T, bool, error)") — não
// existe hoje um equivalente a Tx para StateStored (só chegaria com
// E7.2/G1). Por isso Load<Nome> StateStored tem UM parâmetro a mais (ctx,
// convencionalmente o primeiro, idiomático) que a versão EventSourced não
// tem — é real, não um descuido desta task.
//
// Conversão id→string (decisão documentada): tanto EventStore quanto
// Repository chaveiam por um "string" cru. O "id" de um Aggregate, em todo
// exemplo do spec (incl. o wallet real), é um ValueObject WRAPPER de string
// (ex. WalletId(string)) — aggregateIDGoType (E6.1) já assume essa convenção
// em outro contexto. idToStreamKeyExpr converte via conversão nativa Go
// "string(id)", válida e sem custo quando o Go subjacente do wrapper É
// string. Um Aggregate cujo "id" seja um wrapper de um tipo NÃO-string (ex.
// integer) não é suportado por essa conversão — produziria uma string de
// runas, não os dígitos — e fica fora do escopo desta task: não é exercitado
// nem pelo wallet, nem pela fixture StateStored sintética (ambos usam id de
// string).
//
// Decisão de snapshot (REQ-19.4, opção (b) do prompt da task — documentada
// aqui e no corpo gerado quando aplicável): "snapshot every N events" pode
// estar declarado (decl.Snapshot != nil), mas o runtime (codegen/rtsrc) NÃO
// tem, hoje, nenhum SnapshotStore — E2.1 não construiu esse mecanismo, e
// construí-lo agora (opção (a): interface+impl in-memory análoga a
// Repository[T], mais decidir QUEM chama Save — nenhum emissor de Handle o
// faz hoje, isso só nasceria com um UseCase/E7.2 completo) inflaria esta task
// para além do que o wallet real exercita (ele nem usa snapshot). Esta task
// escolhe (b): Load<Nome> SEMPRE faz replay COMPLETO do stream,
// independentemente de Snapshot, com um comentário no Go gerado apontando o
// atalho de performance futuro. A fixture sintética (decl_aggregate_load_test.go)
// prova que o replay completo continua correto mesmo com Snapshot declarado.

// EmitAggregateLoad gera a função de reconstrução Load<Nome> de decl (§design
// 3.7): EventSourced faz replay do stream via Tx.Load + applyX em ordem;
// StateStored lê o state direto de um Repository[<stateName>] (ver a doc do
// arquivo sobre a decisão de snapshot). model/tab/module mantêm a mesma
// assinatura de entrada de EmitAggregate (E6.1) — usados aqui só para uma
// checagem estática defensiva (REQ-14.4): cada "Apply <Event>" de
// decl.Appliers precisa resolver a um Event/PublicEvent de verdade antes de
// gerar um switch que o referencia como tipo Go.
func EmitAggregateLoad(pkg string, decl *ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string) ([]byte, error) {
	if err := validateAppliersResolveToEvents(decl, model, tab, module); err != nil {
		return nil, fmt.Errorf("codegen: Aggregate %s: %w", decl.Name, err)
	}

	idGoType, err := aggregateIDGoType(decl)
	if err != nil {
		return nil, fmt.Errorf("codegen: Aggregate %s: %w", decl.Name, err)
	}

	e := emit.New(pkg)
	runtimeAlias := e.Import(RuntimeImportPath)

	switch aggregateStrategy(decl) {
	case "EventSourced":
		emitLoadEventSourced(e, decl, idGoType, runtimeAlias)
	case "StateStored":
		emitLoadStateStored(e, decl, idGoType, runtimeAlias)
	default:
		return nil, fmt.Errorf("codegen: Aggregate %s: strategy %q desconhecida (esperava \"EventSourced\" ou \"StateStored\")", decl.Name, decl.Strategy)
	}

	return e.Bytes()
}

// aggregateStrategy devolve a estratégia efetiva de decl: StateStored é o
// padrão do spec (§4.5) quando o bloco Aggregate não declara "strategy".
func aggregateStrategy(decl *ast.AggregateDecl) string {
	if decl.Strategy == "" {
		return "StateStored"
	}
	return decl.Strategy
}

// validateAppliersResolveToEvents confere, ANTES de gerar Go, que cada "Apply
// <Event>" de decl.Appliers resolve a um símbolo Event/PublicEvent de verdade
// (symbols.KindEvent, via o mesmo lower.TypeEnv que o resto do lowering de
// Aggregate usa) — o switch de Load<Nome> referencia esses nomes como TIPOS
// Go (identidade de nome DS↔Go, §design catálogo), então um Apply cujo Event
// não resolvesse geraria Go quebrado sem esta checagem. Não deveria acontecer
// sobre um programa validado (REQ-9 já garantiu a resolução de nomes) —
// defensivo, REQ-14.4.
func validateAppliersResolveToEvents(decl *ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string) error {
	env := lower.New(model, tab, module)
	for _, a := range decl.Appliers {
		if a == nil {
			continue
		}
		t := env.TypeOfName(a.Event)
		st, ok := t.(*types.ShapeType)
		if !ok || st.Kind != symbols.KindEvent {
			return fmt.Errorf("Apply %s: %q não resolve a um Event conhecido (bug de geração — REQ-9 já deveria ter barrado isso)", a.Event, a.Event)
		}
	}
	return nil
}

// aggregateStateHasIDField reporta se decl.State declara um campo "id" —
// espelha a checagem de aggregateIDGoType (decl_aggregate.go). Usada para
// decidir se Load<Nome> EventSourced deve sincronizar state.Id: um Aggregate
// sem campo "id" declarado não tem esse campo no struct de state
// (emitAggregateStateStruct só emite os campos que decl.State de fato lista).
func aggregateStateHasIDField(decl *ast.AggregateDecl) bool {
	for _, f := range decl.State {
		if f != nil && f.Name == "id" {
			return true
		}
	}
	return false
}

// idToStreamKeyExpr devolve a expressão Go que converte idGoExpr (do tipo
// idGoType) para o "string" cru exigido pelas chaves de EventStore/
// Repository — ver a nota de conversão id→string no topo do arquivo.
// idGoType "string" (o fallback de um Aggregate sem campo "id" declarado,
// aggregateIDGoType) não precisa de conversão nenhuma.
func idToStreamKeyExpr(idGoType, idGoExpr string) string {
	if idGoType == "string" {
		return idGoExpr
	}
	return fmt.Sprintf("string(%s)", idGoExpr)
}

// --- EventSourced (REQ-19.4). ---

// emitLoadEventSourced emite "func Load<Nome>(tx runtime.Tx, id <idGoType>)
// (*<Nome>, error)": lê o stream via tx.Load, sincroniza o espelho id (ver
// doc do arquivo) e aplica cada evento em ordem via um switch de type-assertion
// que despacha para o applyX correspondente. Um evento do stream sem Apply
// correspondente é uma falha explícita EM RUNTIME (não dá para garantir
// estaticamente que só esses tipos aparecem no stream — o stream é dado
// externo, possivelmente escrito por uma versão anterior/futura do gerador —
// então o "default" é a defesa em profundidade, não um caminho esperado).
func emitLoadEventSourced(e *emit.Emitter, decl *ast.AggregateDecl, idGoType, runtimeAlias string) {
	fmtAlias := e.Import("fmt")
	fnName := "Load" + decl.Name
	localVar := aggregateReceiver(decl.Name)

	e.Line("// %s reconstrói um %s a partir do stream de eventos de id", fnName, decl.Name)
	e.Line("// (EventSourced, §4.5): lê o stream via Tx.Load (chaveado por aggregateId,")
	e.Line("// nunca um campo \"id\" de payload) e aplica cada evento, na ORDEM DO STREAM,")
	e.Line("// via o método applyX correspondente. Um evento do stream sem Apply")
	e.Line("// correspondente é uma falha explícita (defesa em profundidade: o stream é")
	e.Line("// dado externo).")
	if decl.Snapshot != nil {
		e.Line("//")
		e.Line("// \"snapshot every N events\" está declarado, mas o runtime ainda não tem um")
		e.Line("// SnapshotStore (E2.1 não o construiu) — esta função sempre faz replay")
		e.Line("// COMPLETO do stream; o atalho de carregar do snapshot mais recente e aplicar")
		e.Line("// só o restante fica para quando esse mecanismo existir (decisão da task E6.2).")
	}

	sig := fmt.Sprintf("func %s(tx %s.Tx, id %s) (*%s, error)", fnName, runtimeAlias, idGoType, decl.Name)
	e.Block(sig, func() {
		e.Line("events, err := tx.Load(%s)", idToStreamKeyExpr(idGoType, "id"))
		e.Block("if err != nil", func() {
			e.Line("return nil, err")
		})
		e.Line("%s := &%s{id: id}", localVar, decl.Name)
		if aggregateStateHasIDField(decl) {
			e.Line("%s.state.Id = id", localVar)
		}
		e.Block("for _, ev := range events", func() {
			e.Block("switch ev := ev.(type)", func() {
				for _, a := range decl.Appliers {
					if a == nil {
						continue
					}
					e.Line("case *%s:", a.Event)
					e.Line("%s.apply%s(*ev)", localVar, a.Event)
				}
				e.Line("default:")
				e.Line("return nil, %s.Errorf(\"%s: tipo de evento inesperado no stream: %%T\", ev)", fmtAlias, fnName)
			})
		})
		e.Line("return %s, nil", localVar)
	})
}

// --- StateStored (REQ-19.5). ---

// emitLoadStateStored emite "func Load<Nome>(ctx context.Context, repo
// runtime.Repository[<stateName>], id <idGoType>) (*<Nome>, error)": lê o
// state direto do Repository, sem replay. found == false (não encontrado)
// devolve (nil, nil) — não é erro de infraestrutura; "ensure ... exists" é
// quem trata isso explicitamente depois (decisão já registrada em E5.3/G2).
func emitLoadStateStored(e *emit.Emitter, decl *ast.AggregateDecl, idGoType, runtimeAlias string) {
	ctxAlias := e.Import("context")
	fnName := "Load" + decl.Name
	stateName := aggregateStateStructName(decl.Name)
	localVar := aggregateReceiver(decl.Name)

	e.Line("// %s lê o state do Aggregate %s direto de um Repository (StateStored,", fnName, decl.Name)
	e.Line("// §4.5, o padrão do spec quando \"strategy\" está ausente): sem replay — os")
	e.Line("// mesmos Handle/Apply de %s valem, só a fonte de persistência muda (E6.1).", decl.Name)
	e.Line("// \"não encontrado\" (found=false) devolve (nil, nil): não é erro de")
	e.Line("// infraestrutura — tratado depois, explicitamente, por \"ensure ... exists\".")
	e.Line("//")
	e.Line("// Assimetria vs. a forma EventSourced: runtime.Repository[T].Load exige um")
	e.Line("// ctx explícito em toda chamada (ao contrário de runtime.Tx, que já o carrega")
	e.Line("// desde a unit of work) — por isso esta função recebe um parâmetro ctx a mais.")

	sig := fmt.Sprintf("func %s(ctx %s.Context, repo %s.Repository[%s], id %s) (*%s, error)",
		fnName, ctxAlias, runtimeAlias, stateName, idGoType, decl.Name)
	e.Block(sig, func() {
		e.Line("state, found, err := repo.Load(ctx, %s)", idToStreamKeyExpr(idGoType, "id"))
		e.Block("if err != nil", func() {
			e.Line("return nil, err")
		})
		e.Block("if !found", func() {
			e.Line("return nil, nil")
		})
		e.Line("%s := &%s{id: id, state: state}", localVar, decl.Name)
		e.Line("return %s, nil", localVar)
	})
}
