package codegen

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/astutil"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// gentest_property.go ESTENDE gentest.go (H4, REQ-31.3, §22.5) — o MESMO
// padrão de decl_aggregate_load.go sobre decl_aggregate.go: este arquivo só
// ACRESCENTA a emissão de "property \"...\" { forall sequence of [...]
// invariant ... }" (ast.PropertyDecl, um TestDecl.Properties de um Test que
// resolve a um Aggregate — ver a doc de gentest.go sobre por que só esse alvo
// é suportado nesta fase); nada aqui redeclara o que gentest.go já emite.
//
// REQ-31.3: "WHEN há property, THE SYSTEM SHALL gerar um teste baseado em
// propriedades que gera sequências de comandos e checa a invariante,
// reportando o contra-exemplo." As quatro decisões de design abaixo cobrem,
// respectivamente, como uma sequência de comandos é gerada, como o Aggregate
// começa cada sequência, como a invariante é checada, e como o contra-exemplo
// é reportado.
//
// --- 1. Gerador de valores: type-driven, com retry, stdlib-only ---
//
// "forall sequence of [Deposit, Withdraw]" nomeia HANDLES, não CHAMADAS —
// ao contrário de "when Action(args...)" (§22.1), não há nenhum ast.Arg
// concreto aqui: os argumentos de CADA chamada, em CADA passo da sequência,
// são sintetizados por um gerador ALEATÓRIO type-driven (genValue, abaixo),
// escrito à mão sobre math/rand (NFR-12: zero dependência externa no Go
// gerado) — não um framework de QuickCheck.
//
// genValue percorre RECURSIVAMENTE um types.Type (o mesmo modelo estático
// que codegen/lower já usa — ver env.go, hoistVOConstruct): um primitivo
// (integer/decimal/string/boolean) vira uma expressão aleatória DIRETA, sem
// validação (não pode falhar); um ValueObject (wrapper OU composto) precisa
// de RETRY — sempre que o corpo Valid do VO existe (Money.Valid: amount >=
// 0), um candidato aleatório pode ser inválido, então genValue emite um "for
// { ... New<VO>(candidatos frescos...); if err == nil { break } }" limitado
// a propGenMaxAttempts tentativas (defesa contra um Valid tão restritivo que
// o gerador conservador nunca o satisfaça — nunca exercitado pelo wallet
// real, cujo único Valid não-trivial, Money.amount >= 0, o gerador de
// decimal já respeita por construção: só gera valores >= 0). Um tipo fora
// desse conjunto (Enum, Generic/coleção, Shape) é um erro de geração claro
// (fora de escopo desta fase — o wallet real não precisa de nenhum deles em
// nenhum parâmetro de Handle).
//
// --- 2. Seed inicial: campo-a-campo, não "given" (a gramática não tem um) ---
//
// ast.PropertyDecl não tem GivenClause NENHUMA (confirmado em ast/test.go e
// parser/parse_testfile.go: só Forall+Invariant) — diferente de §22.1/22.2/
// 22.6, não há "eventos passados" para semear o Aggregate antes da
// sequência. Começar de um "w := &Wallet{}" cru (zero-value) tornaria a
// property VAZIA na prática: state.Active seria false (zero-value de
// ActiveStatus), e TODO Handle do wallet real que depende de "state.active
// == ActiveStatus(true)" (Deposit/Withdraw) falharia com InactiveWallet em
// TODA chamada — a invariante nunca seria exercitada por uma transição bem-
// sucedida, uma prova vazia (tecnicamente "passa", mas não prova nada, o
// oposto do que REQ-31.3 pede).
//
// Por isso, ANTES de cada sequência, cada campo de agg.State que o gerador
// suporta (genValue) recebe um valor aleatório válido — a MESMA filosofia
// "seed direto" já documentada em gentest.go (emitFieldSeed): atribuição
// direta a "w.state.Campo", sem passar por nenhum Apply (não há evento
// nenhum para aplicar aqui, só um valor). Um campo cujo tipo o gerador NÃO
// suporta (ex. "entries AppendList<StatementEntry>" do wallet) fica no seu
// Go zero-value (nil slice) — uma AppendList vazia é um estado válido, o
// MESMO ponto de partida que toda given de §22.1 que não populava entries
// explicitamente já usava.
//
// --- 3. Consistência entre instâncias: "shared vars" colhidas do próprio Test ---
//
// Um Money tem DOIS campos (amount decimal, currency string); os Operators
// de Money (domain.ds) exigem "currency == other.currency" em TODA operação
// — um Money aleatório cujo currency diverge do currency já seedado em
// state.balance faria CADA "state.balance + event.amount" (Apply
// DepositPerformed) devolver CurrencyMismatch; como Apply é infalível-por-
// construção (StmtContext{Panics: true}, mesma convenção de gentest.go/
// decl_aggregate.go), isso teria PANICADO o teste gerado, não falhado
// graciosamente — inaceitável para uma property que deve rodar 100 iterações
// sem depender de sorte.
//
// A saída (sem entender "currency" como um conceito especial — o gerador
// continua type-driven, nenhum nome de campo é hardcoded): todo campo STRING
// de um ValueObject COMPOSTO usa um "shared var" — um valor sorteado UMA VEZ
// por iteração (não uma vez por construção) e reusado em TODA construção
// daquele (VOType, campo) durante aquela iteração, tanto no seed do state
// quanto em cada chamada de Handle subsequente. O valor em si vem de um POOL
// COLHIDO do próprio *.test.ds (literalPool, abaixo): toda construção
// concreta de um ValueObject composto dentro de pr.Forall/pr.Invariant (ex.
// "Money(0, \"BRL\")", o literal do PRÓPRIO invariant) contribui um exemplo
// válido — o invariant do wallet real só menciona "BRL", então o pool tem
// exatamente 1 valor, e o "shared var" é deterministicamente sempre "BRL"
// (dsPropPickString com 1 opção). Um (VOType, campo) sem NENHUM exemplo no
// pool cai de volta a texto puramente aleatório por construção (documentado
// como lacuna: uma property cujo invariant nunca menciona um Money — ou cujo
// domínio usa mais de uma currency legítima — arrisca CurrencyMismatch/
// panic; nenhum exemplo real do spec cai nesse caso).
//
// --- 4. Invariante e contra-exemplo ---
//
// A invariante ("state.balance >= Money(0, \"BRL\")") é uma ast.Expr comum —
// lowerizada UMA VEZ por sl.ExprHoisted (mesma máquina de hoisting de VO
// composto/Operator fallível que gentest.go já usa em todo "then"), com
// "state" vinculado a "<receiver>.state" (EXATAMENTE a convenção de
// emitHandle, decl_aggregate.go — reusada aqui, não reinventada) e
// StmtContext{Panics: true} (mesma convenção de teste de gentest.go — uma
// falha AQUI, dado que o pool de shared vars já neutraliza CurrencyMismatch,
// seria bug do próprio gerador, não do domínio). O texto resultante (+ as
// linhas hoisted) é emitido UMA VEZ no corpo do teste, DEPOIS de aplicar os
// eventos de uma chamada bem-sucedida — nunca depois de uma chamada que
// devolveu erro de negócio (REQ-31.3: "a invariante é sobre o state
// alcançado por transições bem-sucedidas, não sobre quais chamadas
// sucedem"), então "err != nil { continue }" pula a checagem sem falhar o
// passo.
//
// O contra-exemplo (REQ-31.3: "reportando o contra-exemplo") é a sequência
// COMPLETA de passos executados até e incluindo o que violou a invariante:
// cada passo vira um dsPropStep{Handle, Args, Err} (helper package-level,
// emitPropertyHelpers) acumulado num []dsPropStep; a violação chama
// "t.Fatalf(..., trail)" — %+v sobre o slice inteiro reproduz Handle+args
// concretos de cada passo, o suficiente para replay manual. Shrinking
// (encontrar o PREFIXO mínimo que ainda viola) NÃO é implementado — reportar
// a sequência exata já satisfaz REQ-31.3 ("reportando o contra-exemplo", sem
// exigir MÍNIMO); um shrinker de verdade precisaria re-executar prefixos
// candidatos contra o MESMO seed determinístico, o que é factível como
// evolução futura mas infla esta fase sem nenhum exemplo real do spec (§22)
// que precise dele.
//
// --- Determinismo (NFR-13): seed fixo derivado do nome, nunca de time.Now ---
//
// Cada property roda com "rand.New(rand.NewSource(<literal>))" — <literal> é
// um int64 calculado em TEMPO DE GERAÇÃO via FNV-1a sobre "Test.Name/
// Property.Name" (propertySeed, abaixo): o MESMO Test+property produz
// SEMPRE o mesmo literal no Go gerado (determinismo do TEXTO — regenerar não
// muda um único byte, mesmo padrão de scenarioFuncName) E o teste GERADO
// reproduz a MESMA sequência de valores aleatórios toda vez que RODA (a
// exploração é aleatória entre properties/execuções diferentes do gerador,
// nunca dentro da mesma property já gerada — uma falha é sempre
// reproduzível relendo o próprio arquivo _test.go).

// propGenIterations é N — quantas sequências independentes cada property
// gera (cada uma com um seed de state fresco, ver a doc do arquivo, item 2).
// propGenMaxSteps é o teto de passos por sequência (o comprimento real de
// cada sequência é sorteado em [1, propGenMaxSteps]). Ambos fixos e
// conservadores (não configuráveis pela gramática de §22.5, que não declara
// nenhum parâmetro para isso) — 100 x até 20 passos já é o suficiente para
// que TestWallet_SaldoNuncaFicaNegativo explore dezenas de milhares de
// transições Deposit/Withdraw por execução, mantendo `go test` rápido.
const (
	propGenIterations  = 100
	propGenMaxSteps    = 20
	propGenMaxAttempts = 1000
)

// emitAggregatePropertyDecls emite um func TestX por ast.PropertyDecl de t
// (§22.5, REQ-31.3) — t já resolveu a agg (aggregates[t.Name], ver
// EmitTests). used é o MESMO mapa de nomes que emitAggregateTestDecl usa
// para os scenarios de t (ver a doc daquela função) — garante que um
// scenario e uma property do mesmo Test nunca colidam no mesmo nome de
// função Go. len(t.Properties) == 0 é um no-op (a maioria dos Test não
// declara property nenhuma).
func emitAggregatePropertyDecls(e *emit.Emitter, t *ast.TestDecl, agg *ast.AggregateDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias string, used map[string]int) error {
	if len(t.Properties) == 0 {
		return nil
	}
	handleByName := make(map[string]*ast.HandleDecl, len(agg.Handlers))
	for _, h := range agg.Handlers {
		handleByName[h.Name] = h
	}

	for i, pr := range t.Properties {
		handles, err := resolveForallHandles(pr, handleByName, agg.Name)
		if err != nil {
			return fmt.Errorf("property %q: forall: %w", pr.Name, err)
		}

		fn := scenarioFuncName(t.Name, i, pr.Name, used)
		e.Line("")
		e.Line("// %s prova a property %q de Test %s (§22.5, REQ-31.3): gera", fn, pr.Name, t.Name)
		e.Line("// %d sequências aleatórias de até %d passos (Deposit/Withdraw-like, ver a", propGenIterations, propGenMaxSteps)
		e.Line("// doc do arquivo) e checa a invariante após cada transição bem-sucedida,")
		e.Line("// reportando a sequência completa como contra-exemplo em caso de falha.")
		var bodyErr error
		e.Block(fmt.Sprintf("func %s(t *testing.T)", fn), func() {
			bodyErr = emitPropertyBody(e, t, pr, agg, handles, model, tab, module, reg, runtimeAlias)
		})
		if bodyErr != nil {
			return fmt.Errorf("property %q: %w", pr.Name, bodyErr)
		}
	}
	return nil
}

// resolveForallHandles valida e resolve "forall sequence of [Nome, ...]"
// (§22.5): Forall precisa ser um ast.ListExpr não-vazio de ast.Ident, cada um
// nomeando um Handle DESTE Aggregate (mesmo casamento por nome de
// resolveAggregateWhen, §22.1) — devolvidos NA ORDEM declarada (o índice de
// cada Handle na lista é o "case" do switch de seleção aleatória, ver
// emitPropertyBody).
func resolveForallHandles(pr *ast.PropertyDecl, handleByName map[string]*ast.HandleDecl, aggName string) ([]*ast.HandleDecl, error) {
	list, ok := pr.Forall.(*ast.ListExpr)
	if !ok {
		return nil, fmt.Errorf("esperava \"forall sequence of [Handle, ...]\", got %T", pr.Forall)
	}
	if len(list.Elems) == 0 {
		return nil, fmt.Errorf("\"forall sequence of [...]\" sem nenhum Handle — nada para gerar")
	}
	out := make([]*ast.HandleDecl, 0, len(list.Elems))
	for _, el := range list.Elems {
		id, ok := el.(*ast.Ident)
		if !ok {
			return nil, fmt.Errorf("esperava um Handle nomeado em \"forall\", got %T", el)
		}
		h, ok := handleByName[id.Name]
		if !ok {
			return nil, fmt.Errorf("%q não é um Handle do Aggregate %s", id.Name, aggName)
		}
		out = append(out, h)
	}
	return out, nil
}

// emitPropertyBody emite o corpo de um func TestX de property (§22.5): seed
// da RNG determinística, N iterações cada uma com (a) um Aggregate fresco
// seedado campo-a-campo, (b) uma sequência aleatória de chamadas de Handle
// que aplica os eventos de cada chamada bem-sucedida e checa a invariante, e
// (c) o contra-exemplo acumulado para reportar em caso de falha.
func emitPropertyBody(e *emit.Emitter, t *ast.TestDecl, pr *ast.PropertyDecl, agg *ast.AggregateDecl, handles []*ast.HandleDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry, runtimeAlias string) error {
	receiver := aggregateReceiver(agg.Name)

	// --- invariante: lowerizada UMA VEZ (ver a doc do arquivo, item 4). ---
	invEnv := lower.New(model, tab, module)
	invEnv.SeedHandle(agg.Name, nil)
	invL := lower.NewLowerer(invEnv, reg, runtimeAlias)
	invL.BindGoName("self", receiver+".state")
	invL.BindGoName("state", receiver+".state")
	invSl := lower.NewStmtLowerer(invL, e, lower.StmtContext{Panics: true})
	invGo, invHoisted, err := invSl.ExprHoisted(pr.Invariant)
	if err != nil {
		return fmt.Errorf("invariant: %w", err)
	}

	// --- pool de exemplos concretos colhidos de forall/invariant (ver a doc
	// do arquivo, item 3) + o conjunto de (VOType, campo) que a property
	// alcança (state + params de cada Handle usado) — decide QUAIS shared
	// vars declarar no topo de cada iteração.
	harvestEnv := lower.New(model, tab, module)
	pool := newLiteralPool(harvestEnv)
	pool.scan(pr.Forall)
	pool.scan(pr.Invariant)

	keysSeen := make(map[string]bool)
	var keyOrder []string
	visitedVO := make(map[string]bool)
	for _, f := range agg.State {
		if f == nil {
			continue
		}
		collectStringFieldKeys(model.TypeOfRef(module, f.Type), keysSeen, &keyOrder, visitedVO)
	}
	for _, h := range handles {
		for _, p := range h.Params {
			if p == nil {
				continue
			}
			collectStringFieldKeys(model.TypeOfRef(module, p.Type), keysSeen, &keyOrder, visitedVO)
		}
	}

	randAlias := e.Import("math/rand")
	e.Line("r := %s.New(%s.NewSource(%d))", randAlias, randAlias, propertySeed(t.Name, pr.Name))
	e.Block(fmt.Sprintf("for iter := 0; iter < %d; iter++", propGenIterations), func() {
		pg := &propGen{e: e, runtimeAlias: runtimeAlias, shared: make(map[string]string)}

		for _, key := range keyOrder {
			vals := pool.get(key)
			if len(vals) == 0 {
				continue // sem exemplo colhido: este campo cai para aleatório puro (ver a doc do arquivo, item 3)
			}
			varName := sharedVarName(key)
			pg.shared[key] = varName
			quoted := make([]string, len(vals))
			for i, v := range vals {
				quoted[i] = strconv.Quote(v)
			}
			e.Line("%s := dsPropPickString(r, []string{%s})", varName, strings.Join(quoted, ", "))
		}

		e.Line("%s := &%s{}", receiver, agg.Name)
		for _, f := range agg.State {
			if f == nil || f.Name == "" {
				continue
			}
			ft := model.TypeOfRef(module, f.Type)
			if !pg.supports(ft) {
				continue // tipo não suportado pelo gerador (ex. AppendList<...>): fica no zero-value Go (ver a doc do arquivo, item 2)
			}
			v, genErr := pg.genValue(ft)
			if genErr != nil {
				err = fmt.Errorf("seed do campo %q: %w", f.Name, genErr)
				return
			}
			e.Line("%s.state.%s = %s", receiver, goname.ExportField(f.Name), v)
		}
		if err != nil {
			return
		}

		e.Line("caller := %s.NewTestCaller(string(%s.state.Id))", runtimeAlias, receiver)
		e.Line("var trail []dsPropStep")
		e.Line("n := 1 + r.Intn(%d)", propGenMaxSteps)
		e.Block("for step := 0; step < n; step++", func() {
			e.Line("var err error")
			e.Line("var events []%s.Event", runtimeAlias)
			e.Block(fmt.Sprintf("switch r.Intn(%d)", len(handles)), func() {
				for i, h := range handles {
					e.Line("case %d:", i)
					if genErr := emitPropertyStepCall(e, pg, h, receiver, model, module); genErr != nil {
						err = fmt.Errorf("passo %s: %w", h.Name, genErr)
						return
					}
				}
			})
			if err != nil {
				return
			}
			e.Block("if err != nil", func() {
				e.Line("continue")
			})
			emitApplyDispatch(e, agg, receiver, "events")
			emitLines(e, invHoisted)
			e.Block("if !("+invGo+")", func() {
				e.Line("t.Fatalf(%q, %q, iter, trail)", "propriedade %q violada (iteração %d): sequência de passos %+v", pr.Name)
			})
		})
	})
	return err
}

// emitPropertyStepCall emite UM "case" do switch de seleção aleatória de
// Handle (dentro de "for step"): gera um valor aleatório por parâmetro de h
// (na ordem declarada — mesma ordem que Go exige na chamada posicional),
// chama h.Name com o caller de teste (mesma convenção de §22.1, ver a doc do
// arquivo de gentest.go sobre "caller: sempre autenticado"), e acumula o
// passo em trail (Handle+Args+Err) para o contra-exemplo.
func emitPropertyStepCall(e *emit.Emitter, pg *propGen, h *ast.HandleDecl, receiver string, model *types.Model, module string) error {
	argVars := make([]string, 0, len(h.Params))
	for _, p := range h.Params {
		if p == nil || p.Name == "" {
			continue
		}
		v, err := pg.genValue(model.TypeOfRef(module, p.Type))
		if err != nil {
			return fmt.Errorf("parâmetro %q: %w", p.Name, err)
		}
		argVars = append(argVars, v)
	}
	allArgs := append([]string{"caller"}, argVars...)
	e.Line("events, err = %s.%s(%s)", receiver, h.Name, strings.Join(allArgs, ", "))
	argsListGo := "[]any{" + strings.Join(argVars, ", ") + "}"
	e.Line("trail = append(trail, dsPropStep{Handle: %q, Args: %s, Err: err})", h.Name, argsListGo)
	return nil
}

// emitApplyDispatch aplica cada evento de eventsVar ao receiver (§22.5, ver a
// doc do arquivo: uma chamada de Handle isolada NUNCA muda o state por si só
// — Apply é um método separado, ver decl_aggregate.go — então uma sequência
// que não reaplicasse os eventos nunca exercitaria estado nenhum). Reusa a
// MESMA correspondência Event->apply<Event> que Load<Nome> EventSourced já
// gera (decl_aggregate_load.go): fidelidade semântica (NFR-15), não uma
// segunda implementação. Um evento sem Apply correspondente (não deveria
// acontecer — todo Handle só emite eventos com Apply, REQ-5) é um t.Fatalf
// explícito, defesa em profundidade, mesmo espírito do "default" de Load.
func emitApplyDispatch(e *emit.Emitter, agg *ast.AggregateDecl, receiver, eventsVar string) {
	e.Block(fmt.Sprintf("for _, ev := range %s", eventsVar), func() {
		e.Block("switch ev := ev.(type)", func() {
			for _, a := range agg.Appliers {
				if a == nil {
					continue
				}
				e.Line("case *%s:", a.Event)
				e.Line("%s.apply%s(*ev)", receiver, a.Event)
			}
			e.Line("default:")
			e.Line("t.Fatalf(%q, ev)", "gerador de property (§22.5): evento inesperado retornado por um Handle: %T")
		})
	})
}

// propertySeed deriva um int64 determinístico de (testName, propName) via
// FNV-1a (ver a doc do arquivo sobre determinismo, NFR-13): a MESMA property
// sempre gera o MESMO literal de seed no Go — regenerar não muda um byte do
// texto, e o teste gerado reproduz sempre a mesma sequência aleatória.
func propertySeed(testName, propName string) int64 {
	h := fnv.New64a()
	h.Write([]byte(testName))
	h.Write([]byte{0})
	h.Write([]byte(propName))
	return int64(h.Sum64())
}

// --- Gerador de valores type-driven (ver a doc do arquivo, item 1). ---

// propGen agrupa o estado de UMA iteração do gerador: o Emitter (as linhas
// de geração são emitidas AO VIVO, à medida que genValue percorre a árvore
// de tipos — não construídas como string e impressas depois, mesmo estilo
// imperativo do resto de gentest.go), o alias de runtime (para
// runtime.NewDecimalFromInt no gerador de decimal) e os "shared vars" já
// declarados nesta iteração (ver a doc do arquivo, item 3) — populado ANTES
// de qualquer chamada a genValue por emitPropertyBody.
type propGen struct {
	e            *emit.Emitter
	runtimeAlias string
	shared       map[string]string
	tmpSeq       int
}

// tmp devolve um novo nome de variável Go único NESTA property (o contador é
// por propGen, uma instância por iteração — mas cada iteração é seu próprio
// bloco Go, então a unicidade só precisa valer DENTRO de cada bloco; nomes
// iguais em iterações/casos DIFERENTES não colidem, blocos Go distintos).
func (pg *propGen) tmp(prefix string) string {
	pg.tmpSeq++
	return fmt.Sprintf("%s%d", prefix, pg.tmpSeq)
}

// supports reporta se genValue sabe construir um valor de t (ver a doc do
// arquivo, item 1: só integer/decimal/string/boolean e ValueObject wrapper/
// composto recursivamente sobre esses) — usado por emitPropertyBody para
// decidir, SEM emitir nada, se um campo de state entra no seed ou fica no
// Go zero-value.
func (pg *propGen) supports(t types.Type) bool {
	switch v := t.(type) {
	case *types.Primitive:
		switch v.Name {
		case "integer", "decimal", "string", "boolean":
			return true
		}
		return false
	case *types.VOType:
		if v.Base != nil {
			return pg.supports(v.Base)
		}
		for _, f := range v.Fields {
			if !pg.supports(f.Type) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// genValue emite as linhas Go que constroem um valor aleatório VÁLIDO de
// tipo t e devolve o nome da variável Go que o guarda (ver a doc do arquivo,
// item 1). Chama pg.e.Line/Block diretamente — as linhas aparecem no ponto
// exato em que genValue é invocado (dentro do "for attempt" de um VO pai,
// se houver), preservando a ordem/indentação corretas sem precisar de um
// buffer intermediário.
func (pg *propGen) genValue(t types.Type) (string, error) {
	switch v := t.(type) {
	case *types.Primitive:
		return pg.genPrimitive(v)
	case *types.VOType:
		if v.Base != nil {
			return pg.genWrapper(v)
		}
		if len(v.Fields) > 0 {
			return pg.genComposite(v)
		}
		return "", fmt.Errorf("ValueObject %s não é wrapper nem composto — bug de geração", v.Name)
	default:
		return "", fmt.Errorf("tipo %s não suportado pelo gerador de property (§22.5) nesta fase de H4 (só integer/decimal/string/boolean e ValueObject wrapper/composto sobre esses)", t.String())
	}
}

// genPrimitive gera um valor aleatório de um tipo primitivo — nunca falha
// (nenhum primitivo tem Valid a checar), então nunca precisa de retry.
// decimal só gera valores >= 0 (ver a doc do arquivo, item 1: suficiente
// para Money.amount, o único campo decimal de VO composto de todo o wallet
// real — um domínio que legitimamente precisasse de decimal negativo em
// posição de parâmetro de Handle exigiria um gerador mais esperto, lacuna
// documentada, não exercitada aqui).
func (pg *propGen) genPrimitive(p *types.Primitive) (string, error) {
	name := pg.tmp("v")
	switch p.Name {
	case "integer":
		pg.e.Line("%s := r.Int63n(1_000_000)", name)
	case "decimal":
		pg.e.Line("%s := %s.NewDecimalFromInt(r.Int63n(1_000_000))", name, pg.runtimeAlias)
	case "string":
		pg.e.Line("%s := dsPropRandString(r)", name)
	case "boolean":
		pg.e.Line("%s := r.Intn(2) == 0", name)
	default:
		return "", fmt.Errorf("primitivo %q não suportado pelo gerador de property (§22.5) nesta fase de H4", p.Name)
	}
	return name, nil
}

// genWrapper gera um wrapper de VO (ValueObject X(Base)) com retry: um novo
// valor de Base é sorteado a CADA tentativa (dentro do "for attempt"), até
// New<VO> aceitar — ver a doc do arquivo, item 1, sobre o limite
// propGenMaxAttempts.
func (pg *propGen) genWrapper(vo *types.VOType) (string, error) {
	name := pg.tmp("v")
	pg.e.Line("var %s %s", name, vo.Name)
	var genErr error
	pg.e.Block("for attempt := 0; ; attempt++", func() {
		pg.e.Block(fmt.Sprintf("if attempt > %d", propGenMaxAttempts), func() {
			pg.e.Line("t.Fatalf(%q)", fmt.Sprintf("gerador de property (§22.5): não consegui produzir um %s válido em %d tentativas", vo.Name, propGenMaxAttempts))
		})
		baseVar, err := pg.genValue(vo.Base)
		if err != nil {
			genErr = err
			return
		}
		candVar := pg.tmp("cand")
		pg.e.Line("%s, err := New%s(%s)", candVar, vo.Name, baseVar)
		pg.e.Block("if err == nil", func() {
			pg.e.Line("%s = %s", name, candVar)
			pg.e.Line("break")
		})
	})
	if genErr != nil {
		return "", genErr
	}
	return name, nil
}

// genComposite gera um ValueObject composto com retry: TODOS os campos são
// regenerados frescos a cada tentativa, EXCETO um campo string cujo (VOType,
// campo) tem um shared var já declarado nesta iteração (ver a doc do
// arquivo, item 3) — esse é FIXO durante toda a iteração, nunca regenerado.
func (pg *propGen) genComposite(vo *types.VOType) (string, error) {
	name := pg.tmp("v")
	pg.e.Line("var %s %s", name, vo.Name)
	var genErr error
	pg.e.Block("for attempt := 0; ; attempt++", func() {
		pg.e.Block(fmt.Sprintf("if attempt > %d", propGenMaxAttempts), func() {
			pg.e.Line("t.Fatalf(%q)", fmt.Sprintf("gerador de property (§22.5): não consegui produzir um %s válido em %d tentativas", vo.Name, propGenMaxAttempts))
		})
		fieldVars := make([]string, len(vo.Fields))
		for i, f := range vo.Fields {
			if shared, ok := pg.sharedStringVar(vo.Name, f); ok {
				fieldVars[i] = shared
				continue
			}
			v, err := pg.genValue(f.Type)
			if err != nil {
				genErr = err
				return
			}
			fieldVars[i] = v
		}
		if genErr != nil {
			return
		}
		candVar := pg.tmp("cand")
		pg.e.Line("%s, err := New%s(%s)", candVar, vo.Name, strings.Join(fieldVars, ", "))
		pg.e.Block("if err == nil", func() {
			pg.e.Line("%s = %s", name, candVar)
			pg.e.Line("break")
		})
	})
	if genErr != nil {
		return "", genErr
	}
	return name, nil
}

// sharedStringVar devolve o nome Go do shared var de (vo.Name, f.Name), se
// já foi declarado nesta iteração (ver a doc do arquivo, item 3) — só campos
// string são elegíveis (a consistência entre instâncias só importa para
// campos "identity-like", tipicamente string, ex. Money.currency; um campo
// numérico é a própria dimensão que a property quer explorar, nunca
// compartilhado).
func (pg *propGen) sharedStringVar(voName string, f types.Field) (string, bool) {
	if !isStringPrimitive(f.Type) {
		return "", false
	}
	v, ok := pg.shared[voName+"."+f.Name]
	return v, ok
}

// isStringPrimitive reporta se t é o primitivo "string".
func isStringPrimitive(t types.Type) bool {
	p, ok := t.(*types.Primitive)
	return ok && p.Name == "string"
}

// sharedVarName deriva um nome de variável Go legível e determinístico do
// key "VOName.field" (ex. "Money.currency" -> "sharedMoneyCurrency").
func sharedVarName(key string) string {
	parts := strings.SplitN(key, ".", 2)
	return "shared" + goname.ExportField(parts[0]) + goname.ExportField(parts[1])
}

// collectStringFieldKeys anda recursivamente por t coletando toda combinação
// (ValueObject composto, campo string) alcançável a partir dele — usada por
// emitPropertyBody para saber, ANTES de emitir qualquer seed/chamada, quais
// shared vars declarar no topo de cada iteração (ver a doc do arquivo, item
// 3: um shared var precisa estar em escopo Go visível tanto ao seed quanto a
// TODO passo do "for step", então não pode ser declarado sob demanda dentro
// de um switch-case). visitedVO evita recursão infinita em VOs mutuamente
// recursivos (não exercitado pelo wallet real, defensivo).
func collectStringFieldKeys(t types.Type, seen map[string]bool, order *[]string, visitedVO map[string]bool) {
	vo, ok := t.(*types.VOType)
	if !ok || vo.Base != nil {
		return // primitivo, wrapper, ou outro Type: sem campo aninhado a coletar
	}
	if visitedVO[vo.Name] {
		return
	}
	visitedVO[vo.Name] = true
	for _, f := range vo.Fields {
		if isStringPrimitive(f.Type) {
			key := vo.Name + "." + f.Name
			if !seen[key] {
				seen[key] = true
				*order = append(*order, key)
			}
			continue
		}
		collectStringFieldKeys(f.Type, seen, order, visitedVO)
	}
}

// --- Pool de exemplos concretos (ver a doc do arquivo, item 3). ---

// literalPool colhe, de qualquer CallExpr encontrado numa árvore escaneada
// (scan), os literais STRING passados a campos string de um ValueObject
// COMPOSTO — o "corpus" de valores conhecidos-válidos que genComposite usa
// para os shared vars.
type literalPool struct {
	env   *lower.TypeEnv
	byKey map[string][]string        // "VOName.field" -> valores únicos, ordem de 1ª aparição
	seen  map[string]map[string]bool // "VOName.field" -> conjunto já visto
}

// newLiteralPool cria um pool vazio sobre env (usado só para TypeOfName —
// resolver o Fn de um CallExpr a um *types.VOType).
func newLiteralPool(env *lower.TypeEnv) *literalPool {
	return &literalPool{env: env, byKey: make(map[string][]string), seen: make(map[string]map[string]bool)}
}

// scan varre root (qualquer subárvore de expressão — Forall ou Invariant de
// uma property) via astutil.ForEachExpr, colhendo cada CallExpr que constrói
// um ValueObject composto conhecido.
func (p *literalPool) scan(root ast.Expr) {
	astutil.ForEachExpr(root, func(e ast.Expr) {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return
		}
		id, ok := call.Fn.(*ast.Ident)
		if !ok {
			return
		}
		vo, ok := p.env.TypeOfName(id.Name).(*types.VOType)
		if !ok || vo.Base != nil {
			return
		}
		for _, f := range vo.Fields {
			if !isStringPrimitive(f.Type) {
				continue
			}
			val, ok := literalFieldArg(vo, f.Name, call.Args)
			if !ok {
				continue
			}
			key := vo.Name + "." + f.Name
			if p.seen[key] == nil {
				p.seen[key] = make(map[string]bool)
			}
			if !p.seen[key][val] {
				p.seen[key][val] = true
				p.byKey[key] = append(p.byKey[key], val)
			}
		}
	})
}

// get devolve o pool colhido de key ("VOName.field"), na ordem de 1ª
// aparição — nil (pool vazio) se nenhum exemplo foi colhido.
func (p *literalPool) get(key string) []string { return p.byKey[key] }

// literalFieldArg devolve o valor decodificado (sem aspas — ast.Literal.Value
// já vem assim, ver codegen/lower/expr.go:literal) do argumento de fieldName
// em args (nomeado por nome OU posicional por índice, mesma regra de
// voConstructArgsGoOrder), se for um ast.Literal de STRING. Qualquer outra
// forma (uma variável, uma expressão computada) não é um "exemplo colhível"
// — devolve ok=false silenciosamente, o campo cai no fallback documentado.
func literalFieldArg(vo *types.VOType, fieldName string, args []ast.Arg) (string, bool) {
	idx := -1
	for i, f := range vo.Fields {
		if f.Name == fieldName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", false
	}
	named := false
	for _, a := range args {
		if a.Name != "" {
			named = true
			break
		}
	}
	var valExpr ast.Expr
	if named {
		for _, a := range args {
			if a.Name == fieldName {
				valExpr = a.Value
				break
			}
		}
	} else if idx < len(args) {
		valExpr = args[idx].Value
	}
	lit, ok := valExpr.(*ast.Literal)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return lit.Value, true
}

// --- Helpers package-level emitidos uma vez por arquivo (ver EmitTests). ---

// emitPropertyHelpers emite os tipos/funções auxiliares que toda property
// gerada compartilha (§22.5): dsPropStep (contra-exemplo, ver a doc do
// arquivo item 4), dsPropRandString (gerador de string, item 1) e
// dsPropPickString (shared vars, item 3). Chamado no máximo uma vez por
// arquivo (EmitTests, guardado por needsRand) — dois Test com property no
// mesmo módulo reusam os MESMOS três símbolos, nunca duplicados (evitaria
// "redeclared" do compilador Go).
func emitPropertyHelpers(e *emit.Emitter) {
	randAlias := e.Import("math/rand")

	e.Line("")
	e.Line("// dsPropStep guarda um passo executado durante uma property (§22.5, H4): o")
	e.Line("// Handle chamado, os argumentos concretos e o erro de negócio (se houver) —")
	e.Line("// usado só para reportar o contra-exemplo em caso de falha (t.Fatalf com")
	e.Line("// \"%%+v\" sobre o []dsPropStep acumulado), nunca lido para nenhuma outra")
	e.Line("// finalidade.")
	e.Block("type dsPropStep struct", func() {
		e.Line("Handle string")
		e.Line("Args   []any")
		e.Line("Err    error")
	})

	e.Line("")
	e.Line("// dsPropRandString gera uma string ASCII curta e não vazia — heurística")
	e.Line("// conservadora do gerador de property-based testing (§22.5): comprimento")
	e.Line("// 1-16, caracteres 'a'-'z', suficiente para os Valid predicates de")
	e.Line("// comprimento mais comuns (ex. \"value.length() > 0\"/\"<= 140\").")
	e.Block(fmt.Sprintf("func dsPropRandString(r *%s.Rand) string", randAlias), func() {
		e.Line("n := 1 + r.Intn(16)")
		e.Line("b := make([]byte, n)")
		e.Block("for i := range b", func() {
			e.Line("b[i] = byte('a' + r.Intn(26))")
		})
		e.Line("return string(b)")
	})

	e.Line("")
	e.Line("// dsPropPickString escolhe um valor de opts (não vazio, garantido pelo")
	e.Line("// chamador) — os \"shared vars\" de campo string de ValueObject composto")
	e.Line("// (ver a doc do arquivo) usam isto: um pool de 1 valor (o caso comum, ex.")
	e.Line("// só \"BRL\" aparece no *.test.ds) escolhe deterministicamente esse único")
	e.Line("// valor; mais de um explora entre eles por iteração.")
	e.Block(fmt.Sprintf("func dsPropPickString(r *%s.Rand, opts []string) string", randAlias), func() {
		e.Line("return opts[r.Intn(len(opts))]")
	})
}
