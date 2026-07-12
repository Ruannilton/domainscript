package codegen

import (
	"fmt"
	"path"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/lower"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// decl_io.go emite o Go de Notification/Adapter/Foreign (F4, REQ-25, §design
// codegen 3.13/decl_io): a fronteira de saída do domínio para o mundo
// externo.
//
// --- O que o front-end já resolve, e o que este arquivo é a PRIMEIRA
// autoridade a interpretar (mesma situação de decl_projection.go, E8.2) ---
//
// resolver/resolver.go (REQ-4) registra Notification/Adapter como símbolos
// (symbols.KindNotification/KindAdapter) e resolve os Fields de Notification
// (tipos declarados) — mas NÃO desce em AdapterDecl.Headers/Body/Map (nenhum
// caso em resolver/resolve_body.go/resolve_config.go cobre Adapter): essas
// expressões referenciam um receptor "notification" que só EXISTE a partir
// daqui. sema/rules_domain.go (checkNotificationAdapters) já garante, antes
// deste emissor rodar, que toda Notification tem um Adapter de MESMO NOME no
// MESMO módulo (§9.1) — o inverso (Adapter sem Notification) não é validado
// pelo front-end; este arquivo trata isso como bug de geração (a forma comum
// e única suportada aqui é o par completo, com Notification definindo o
// shape que Headers/Body/Map de fato usam via "notification.<campo>").
//
// Notification/Adapter COMPARTILHAM O NOME (§9.1/9.3) — resolver.go só
// registra UM dos dois na SymbolTable (o primeiro coletado; a ordem usual do
// spec é Notification antes de Adapter, então é a Notification quem ocupa o
// slot). Por isso este arquivo NUNCA usa a SymbolTable para achar o
// AdapterDecl de um nome: os *ast.AdapterDecl de um módulo são coletados
// direto de prog.Files (codegen.go, bucketModuleDecls), o mesmo padrão que
// sema/rules_domain.go já usa para o par.
//
// --- notify (async) vs call (sync): de onde vem a distinção (REQ-25.3) ---
//
// AdapterDecl.Mode ("async"/"sync", já parseado pelo front-end) é a fonte de
// verdade — não um par de keywords "notify"/"call" na gramática (não
// existem: nem token/token.go nem parser/parse_stmt.go têm essa forma; o
// texto "call PaymentRequest(...)" do spec §18.2 é ILUSTRATIVO, no mesmo
// espírito elidido de "up { ... }" — decl_saga_test.go já documenta essa
// mesma leitura para a fixture de Saga). O corpo do domínio invoca uma
// Notification pelo NOME, exatamente como já invoca a construção de um
// Event/Command (CallExpr sobre um Ident que nomeia um tipo — E5.1); o
// LOWERING (codegen/lower/stmt.go, StmtLowerer.notifyOrCallStmt) reconhece
// esse CallExpr em posição de ExprStmt e decide Notify<Nome> (Mode async,
// fire-and-forget, erro logado — nunca propagado) ou Call<Nome> (Mode sync,
// erro propagado ao chamador, o mesmo mecanismo de "ensure ... else Error").
// Nenhuma mudança de parser/resolver foi necessária: "DepositNotification(to:
// ..., amount: ...)" já é sintaxe válida hoje (constrói o shape da
// Notification, E5.1); só o CODEGEN passa a reconhecer essa forma
// especificamente quando usada como statement solto.
//
// --- Resultado de uma chamada síncrona: por que só "error" (REQ-25.3) ---
//
// AdapterDecl não declara um tipo de retorno em lugar NENHUM da gramática
// (nem para HTTP, nem para FFI vinculado a Notification — ao contrário do
// bloco Foreign geral, §9.4, cujo "function Name(...) -> Ret" TEM retorno
// tipado). "result = call PaymentRequest(...)" no spec é ilustrativo por essa
// mesma razão (ver acima). Decisão desta task: Call<Nome> devolve só "error"
// — o chamador sabe se a chamada teve sucesso, mas não há um valor de
// domínio modelado para capturar. Um Adapter que precise devolver dado
// estruturado é trabalho futuro (exigiria uma extensão de gramática fora do
// escopo de F4, um back-end-only task).
//
// --- Adapter FFI: marshalling e o porquê de NÃO reusar tipos de domínio ---
//
// adapters/<pkg> é código Go ESCRITO À MÃO pelo usuário (nunca gerado/
// sobrescrito por "dsc gen" — evitaria destruir a implementação real a cada
// regeração). Se a função estrangeira aceitasse tipos do domínio (VOType/
// ShapeType do pacote do módulo) diretamente, adapters/<pkg> precisaria
// importar o pacote do módulo — e o pacote do módulo já importa
// adapters/<pkg> (para CHAMAR a função) — import cycle. A correção:
// "marshalling" (REQ-25.2) converte cada campo mapeado para sua forma
// primitiva antes da chamada — um ValueObject WRAPPER vira seu Base via
// conversão de tipo Go nativa (mesma identidade "type X Base" de E3.1); um
// primitivo passa direto. Um VO COMPOSTO (múltiplos campos) não tem uma
// única forma primitiva óbvia — erro de geração claro, escopo futuro (não
// exercitado pela fixture desta task).
//
// --- Bloco Foreign geral (§9.4): glue gerado, mas NÃO alcançável de corpos
// hoje (gap pré-existente do front-end, documentado — fora do escopo) ---
//
// resolver/resolver.go:collect nunca registra um símbolo para *ast.ForeignDecl
// (o switch cai no default, "sem símbolo de domínio próprio") — as funções de
// um Foreign geral (ex. "ComputeMerkleRoot") NUNCA entram na SymbolTable, e
// resolver/resolve_body.go:resolveIdent só aceita um nome solto que resolva
// via escopo léxico, símbolo do módulo, ou membro de Enum — logo
// "hash = ComputeMerkleRoot(items)" dentro de qualquer corpo hoje falha REQ-9
// ("nome não declarado"), mesmo que o Foreign esteja declarado. Corrigir isso
// é uma mudança de FRONT-END (resolver), fora do escopo de uma task de
// codegen — e nenhuma fixture real precisa disso ainda. EmitForeign (abaixo)
// ainda gera o glue de call-site (função Go real, testável isoladamente,
// REQ-25.2) — não fica bloqueado por esse gap, só documentado: quando/se o
// gap do resolver for corrigido, o glue já gerado funciona sem mudança
// nenhuma neste arquivo.

// --- 1. Notification: contrato de saída (REQ-25.1). ---

// notificationFieldInfo é a forma Go já resolvida de um campo de Notification
// — mesmo padrão de commandFieldInfo/eventFieldInfo.
type notificationFieldInfo struct {
	field      *ast.Field
	goType     string
	exportName string
}

// EmitNotification gera o Go de uma única NotificationDecl — a mesma forma de
// EmitNotifications, mantendo o contrato uniforme entre as duas funções.
func EmitNotification(pkg string, decl *ast.NotificationDecl) ([]byte, error) {
	e := emit.New(pkg)
	if err := emitNotificationDecl(e, decl); err != nil {
		return nil, fmt.Errorf("codegen: Notification %s: %w", decl.Name, err)
	}
	return e.Bytes()
}

// emitNotificationDecl emite o struct de uma única NotificationDecl: um
// campo exportado + tag json por Field, na ordem declarada — sem
// construtor/validação (uma Notification é um DTO de saída, como um Command;
// REQ-25.1 só pede "contrato", não invariantes).
func emitNotificationDecl(e *emit.Emitter, decl *ast.NotificationDecl) error {
	infos := make([]notificationFieldInfo, 0, len(decl.Fields))
	for _, f := range decl.Fields {
		if f == nil {
			continue
		}
		goType, err := goname.GoFieldType(f.Type)
		if err != nil {
			return fmt.Errorf("campo %s: %w", f.Name, err)
		}
		infos = append(infos, notificationFieldInfo{field: f, goType: goType, exportName: goname.ExportField(f.Name)})
	}

	e.Line("// %s é a Notification %s (§9.1): contrato de saída — %s.", decl.Name, decl.Name, notificationFieldSummary(decl.Fields))
	e.Line("// Sem Adapter correspondente seria erro de compilação (sema já garante isso).")
	e.Block(fmt.Sprintf("type %s struct", decl.Name), func() {
		for _, fi := range infos {
			e.Line("%s %s %s", fi.exportName, fi.goType, goname.JSONTag(fi.field.Name))
		}
	})
	return nil
}

func notificationFieldSummary(fields []*ast.Field) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		if f == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s", f.Name, f.Type.Name))
	}
	return strings.Join(parts, ", ")
}

// --- 2. Adapter: HTTP declarativo ou FFI (REQ-25.1/25.2/25.3). ---

// EmitAdapter gera o Go de um único AdapterDecl, dada a Notification
// parceira (mesmo nome, §9.1/9.3) — a mesma forma de emitNotificationsAndAdapters
// (codegen.go), exposta separadamente para teste isolado.
func EmitAdapter(pkg string, decl *ast.AdapterDecl, notif *ast.NotificationDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	e := emit.New(pkg)
	if err := emitAdapterDecl(e, decl, notif, model, tab, module, reg); err != nil {
		return nil, fmt.Errorf("codegen: Adapter %s: %w", decl.Name, err)
	}
	return e.Bytes()
}

// emitAdapterDecl monta o TypeEnv/Lowerer partilhado (semeando "notification"
// com o shape da Notification parceira — o receptor que Headers/Body/Map
// referenciam, ver a doc do arquivo) e roteia para HTTP declarativo ou FFI
// conforme os campos presentes: HTTPUrl != nil ⇒ HTTP; Function != nil ⇒
// FFI; nenhum dos dois é forma vazia (erro de geração).
func emitAdapterDecl(e *emit.Emitter, decl *ast.AdapterDecl, notif *ast.NotificationDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) error {
	env := lower.New(model, tab, module)
	notifShape, ok := env.TypeOfName(notif.Name).(*types.ShapeType)
	if !ok {
		return fmt.Errorf("Notification %s: não resolveu a um shape (bug de geração)", notif.Name)
	}
	env.Bind("notification", notifShape)

	l := lower.NewLowerer(env, reg, "").WithEmitter(e)
	l.BindGoName("notification", "n")
	ctxAlias := e.Import("context")

	switch {
	case decl.HTTPUrl != nil:
		return emitAdapterHTTP(e, decl, notif, l, ctxAlias)
	case decl.Function != nil:
		return emitAdapterFFI(e, decl, notif, l, env, model, module, ctxAlias)
	default:
		return fmt.Errorf("nem HTTP (\"http METHOD url\") nem FFI (\"foreign ... function ...\") declarado")
	}
}

// adapterValueGo traduz uma expressão de Headers/Body/Map/URL do Adapter:
// "env(\"KEY\")" (reconhecido por FORMA — Fn é um Ident literal "env", um
// argumento STRING — o mesmo espírito de builtins reconhecidas por nome de
// lower/builtins.go, E5.3) vira "os.Getenv(\"KEY\")" (REQ-25.1); qualquer
// outra expressão (literal, "notification.campo", ...) passa por
// Lowerer.Expr normalmente.
func adapterValueGo(e *emit.Emitter, l *lower.Lowerer, expr ast.Expr) (string, error) {
	if key, ok := envCallKey(expr); ok {
		osAlias := e.Import("os")
		return fmt.Sprintf("%s.Getenv(%q)", osAlias, key), nil
	}
	return l.Expr(expr)
}

// envCallKey reconhece "env(\"KEY\")" por forma: CallExpr cujo Fn é o Ident
// literal "env" com exatamente 1 argumento posicional STRING.
func envCallKey(expr ast.Expr) (string, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 || call.Args[0].Name != "" {
		return "", false
	}
	id, ok := call.Fn.(*ast.Ident)
	if !ok || id.Name != "env" {
		return "", false
	}
	lit, ok := call.Args[0].Value.(*ast.Literal)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return lit.Value, true
}

// stringLiteralValue extrai o valor cru de um literal STRING — usado para
// Adapter.Lang/From/Function e Foreign.Lang/From, todos parseados como Expr
// (parser/parse_decl.go) mas sempre um literal de string na forma aceita
// aqui (§9.3/9.4).
func stringLiteralValue(e ast.Expr) (string, error) {
	lit, ok := e.(*ast.Literal)
	if !ok || lit.Kind != token.STRING {
		return "", fmt.Errorf("esperava um literal STRING, veio %T", e)
	}
	return lit.Value, nil
}

// emitAdapterHTTP emite o leaf "send<Nome>" (constrói e executa o
// http.Request declarado por decl: método/URL/headers/body — REQ-25.1) mais
// o wrapper Notify/Call (emitAdapterWrapper) conforme decl.Mode.
func emitAdapterHTTP(e *emit.Emitter, decl *ast.AdapterDecl, notif *ast.NotificationDecl, l *lower.Lowerer, ctxAlias string) error {
	httpAlias := e.Import("net/http")
	fmtAlias := e.Import("fmt")

	urlGo, err := adapterValueGo(e, l, decl.HTTPUrl)
	if err != nil {
		return fmt.Errorf("http url: %w", err)
	}

	type headerKV struct{ key, valGo string }
	headers := make([]headerKV, 0, len(decl.Headers))
	for _, h := range decl.Headers {
		vg, err := adapterValueGo(e, l, h.Value)
		if err != nil {
			return fmt.Errorf("headers.%s: %w", h.Name, err)
		}
		headers = append(headers, headerKV{h.Name, vg})
	}

	bodyAssigns := make([]string, 0, len(decl.Body))
	for _, b := range decl.Body {
		vg, err := adapterValueGo(e, l, b.Value)
		if err != nil {
			return fmt.Errorf("body.%s: %w", b.Name, err)
		}
		bodyAssigns = append(bodyAssigns, fmt.Sprintf("%q: %s", b.Name, vg))
	}

	leafName := "send" + decl.Name
	e.Line("// %s realiza a chamada HTTP declarada pelo Adapter %s (§9.3 Nível 1 —", leafName, decl.Name)
	e.Line("// HTTP declarativo): método/URL/headers/body mapeados a partir da")
	e.Line("// Notification %s.", notif.Name)
	e.Block(fmt.Sprintf("func %s(ctx %s.Context, n %s) error", leafName, ctxAlias, notif.Name), func() {
		var bodyExprGo string
		if len(bodyAssigns) > 0 {
			jsonAlias := e.Import("encoding/json")
			bytesAlias := e.Import("bytes")
			e.Line("payload := map[string]any{%s}", strings.Join(bodyAssigns, ", "))
			e.Line("bodyBytes, err := %s.Marshal(payload)", jsonAlias)
			e.Block("if err != nil", func() { e.Line("return err") })
			bodyExprGo = bytesAlias + ".NewReader(bodyBytes)"
		} else {
			bodyExprGo = "nil"
		}
		e.Line("req, err := %s.NewRequestWithContext(ctx, %q, %s, %s)", httpAlias, decl.HTTPMethod, urlGo, bodyExprGo)
		e.Block("if err != nil", func() { e.Line("return err") })
		for _, h := range headers {
			e.Line("req.Header.Set(%q, %s)", h.key, h.valGo)
		}
		e.Line("resp, err := %s.DefaultClient.Do(req)", httpAlias)
		e.Block("if err != nil", func() { e.Line("return err") })
		e.Line("defer resp.Body.Close()")
		e.Block("if resp.StatusCode >= 400", func() {
			e.Line("return %s.Errorf(%q, resp.StatusCode)", fmtAlias, "adapter "+decl.Name+": status HTTP %d")
		})
		e.Line("return nil")
	})

	e.Line("")
	emitAdapterWrapper(e, decl, notif, leafName, ctxAlias)
	return nil
}

// emitAdapterFFI emite o leaf "call<Nome>Foreign" (chama a função Go
// hand-written em adapters/<from>, com marshalling dos campos mapeados —
// REQ-25.2, ver a doc do arquivo sobre por que NÃO passa tipos de domínio
// direto) mais o wrapper Notify/Call conforme decl.Mode.
func emitAdapterFFI(e *emit.Emitter, decl *ast.AdapterDecl, notif *ast.NotificationDecl, l *lower.Lowerer, env *lower.TypeEnv, model *types.Model, module, ctxAlias string) error {
	langStr, err := stringLiteralValue(decl.Lang)
	if err != nil {
		return fmt.Errorf("foreign lang: %w", err)
	}
	if !strings.EqualFold(langStr, "go") {
		return fmt.Errorf("foreign %q: só \"go\" é suportado nesta task", langStr)
	}
	if decl.From == nil {
		return fmt.Errorf("foreign sem \"from\"")
	}
	fromStr, err := stringLiteralValue(decl.From)
	if err != nil {
		return fmt.Errorf("foreign from: %w", err)
	}
	fnName, err := stringLiteralValue(decl.Function)
	if err != nil {
		return fmt.Errorf("function: %w", err)
	}

	pkgAlias := e.Import(path.Join(domainModuleRoot, fromStr))

	argGoList := make([]string, 0, len(decl.Map))
	argNames := make([]string, 0, len(decl.Map))
	for _, me := range decl.Map {
		ag, err := ffiArgGo(model, module, env, l, me.Value)
		if err != nil {
			return fmt.Errorf("map.%s: %w", me.Name, err)
		}
		argGoList = append(argGoList, ag)
		argNames = append(argNames, me.Name)
	}

	leafName := "call" + decl.Name + "Foreign"
	e.Line("// %s chama %s.%s (§9.3 Nível 2 — FFI), com marshalling dos campos", leafName, pkgAlias, fnName)
	e.Line("// mapeados (%s). %s.%s é código HAND-WRITTEN esperado em \"%s\"", strings.Join(argNames, ", "), pkgAlias, fnName, fromStr)
	e.Line("// — NUNCA gerado/sobrescrito por \"dsc gen\" (ver a doc do arquivo); a")
	e.Line("// assinatura esperada é \"func %s(ctx context.Context, ...) error\"", fnName)
	e.Line("// (um parâmetro marshalado por entrada de map, na ordem declarada).")
	e.Block(fmt.Sprintf("func %s(ctx %s.Context, n %s) error", leafName, ctxAlias, notif.Name), func() {
		callArgs := append([]string{"ctx"}, argGoList...)
		e.Line("return %s.%s(%s)", pkgAlias, fnName, strings.Join(callArgs, ", "))
	})

	e.Line("")
	emitAdapterWrapper(e, decl, notif, leafName, ctxAlias)
	return nil
}

// ffiArgGo traduz um Value de Adapter.Map para uma forma marshalável por
// FFI: um primitivo passa direto (Lowerer.Expr já produz a forma Go certa);
// um ValueObject WRAPPER converte para seu Base via conversão de tipo Go
// nativa ("X(n.Campo)" — mesma identidade "type X Base" de E3.1). Qualquer
// outro tipo (VO composto, Enum, Shape) é erro de geração claro — ver a doc
// do arquivo sobre por que a marshalling desta task cobre só esses dois
// casos.
func ffiArgGo(model *types.Model, module string, env *lower.TypeEnv, l *lower.Lowerer, expr ast.Expr) (string, error) {
	goExpr, err := l.Expr(expr)
	if err != nil {
		return "", err
	}
	t := model.Infer(module, expr, env)
	switch x := t.(type) {
	case *types.Primitive:
		return goExpr, nil
	case *types.VOType:
		prim, ok := x.Base.(*types.Primitive)
		if x.Base == nil || !ok {
			return "", fmt.Errorf("ValueObject composto %q não é marshalável para FFI nesta task (só wrapper sobre primitivo)", x.Name)
		}
		goType, ok := goname.GoPrimitive(prim.Name)
		if !ok {
			return "", fmt.Errorf("primitivo %q sem mapeamento Go conhecido", prim.Name)
		}
		return fmt.Sprintf("%s(%s)", goType, goExpr), nil
	default:
		return "", fmt.Errorf("tipo %s não suportado como argumento marshalado para FFI (só primitivo ou ValueObject wrapper)", t.String())
	}
}

// adapterCallVarName (H4, REQ-31.3) devolve o nome Go da var de pacote que
// Call<Nome>/Notify<Nome> invocam por baixo (ver emitAdapterWrapper) em vez
// de chamar o leaf direto — o seam que um teste de Saga gerado (gentest.go,
// §22.3) reatribui para instalar um mock ANTES de rodar o cenário, sem
// tocar em nenhum wiring de produção. "" quando decl não é nem HTTP nem FFI
// (forma vazia — já um erro de geração em emitAdapterDecl, nunca alcançado
// por um Adapter real).
func adapterCallVarName(decl *ast.AdapterDecl) string {
	switch {
	case decl.HTTPUrl != nil:
		return "send" + decl.Name + "Fn"
	case decl.Function != nil:
		return "call" + decl.Name + "ForeignFn"
	default:
		return ""
	}
}

// emitAdapterWrapper emite o ponto de entrada público conforme decl.Mode
// (REQ-25.3, distinção notify/call — ver a doc do arquivo): "sync" ⇒
// Call<Nome>(ctx, n) error, erro propagado; qualquer outro valor (o único
// outro caso real do front-end é "async") ⇒ Notify<Nome>(ctx, n), sem
// retorno — fire-and-forget, erro só logado (mesma postura conservadora de
// decl_policy.go sobre Delivery: um valor desconhecido cai no caminho mais
// seguro, nunca um erro de geração por um texto livre que o parser aceitou).
//
// Call<Nome>/Notify<Nome> invocam leafName através de uma VAR DE PACOTE
// (adapterCallVarName(decl), acima) em vez de uma chamada direta ao leaf —
// o seam de mock que um teste de Saga gerado (H4, REQ-31.3) precisa: por
// default a var aponta para a implementação real (leafName), preservando o
// comportamento de produção byte a byte fora de um cenário mockado; só um
// teste no MESMO pacote (a convenção "package pkg" interno de gentest.go)
// pode reatribuí-la. Nenhuma outra mudança de contrato: Call<Nome>/
// Notify<Nome> continuam com a mesma assinatura pública de antes de H4.
func emitAdapterWrapper(e *emit.Emitter, decl *ast.AdapterDecl, notif *ast.NotificationDecl, leafName, ctxAlias string) {
	fnVar := adapterCallVarName(decl)
	e.Line("// %s invoca a implementação real de %s por padrão — var de pacote", fnVar, leafName)
	e.Line("// (não uma chamada direta) para que um teste de Saga gerado (H4,")
	e.Line("// REQ-31.3, \"mock ... returns ...\") possa substituí-la e simular a")
	e.Line("// resposta do Adapter sem tocar no wiring de produção.")
	e.Line("var %s = %s", fnVar, leafName)
	e.Line("")

	if decl.Mode == "sync" {
		e.Line("// Call%s é a chamada SÍNCRONA do Adapter %s (REQ-25.3): o erro É", decl.Name, decl.Name)
		e.Line("// propagado ao chamador.")
		e.Block(fmt.Sprintf("func Call%s(ctx %s.Context, n %s) error", decl.Name, ctxAlias, notif.Name), func() {
			e.Line("return %s(ctx, n)", fnVar)
		})
		return
	}

	slogAlias := e.Import("log/slog")
	e.Line("// Notify%s dispara o Adapter %s de forma ASSÍNCRONA (REQ-25.3,", decl.Name, decl.Name)
	e.Line("// fire-and-forget): falhas são logadas, NUNCA propagadas ao chamador. A")
	e.Line("// fila/outbox de verdade (dispatch desacoplado do caller) é Marco F5 — este")
	e.Line("// seam já roda a chamada de forma síncrona por trás da cortina, só sem")
	e.Line("// devolver erro ao chamador (o contrato que o Marco F5 precisa preservar).")
	e.Block(fmt.Sprintf("func Notify%s(ctx %s.Context, n %s)", decl.Name, ctxAlias, notif.Name), func() {
		e.Block(fmt.Sprintf("if err := %s(ctx, n); err != nil", fnVar), func() {
			e.Line("%s.Default().ErrorContext(ctx, \"notify falhou\", \"notification\", %q, \"error\", err)", slogAlias, decl.Name)
		})
	})
}

// --- 3. Foreign geral (§9.4): call-site glue. ---

// EmitForeign gera o Go de um único ForeignDecl: uma função de glue por
// "function Name(params) -> Ret" declarada, chamando <pkgAlias>.Name(args...)
// (REQ-25.2). Ver a doc do arquivo sobre por que essas funções não são
// alcançáveis de corpos hoje (gap pré-existente do front-end, fora do
// escopo) — o glue em si é gerado e testável isoladamente.
func EmitForeign(pkg string, decl *ast.ForeignDecl) ([]byte, error) {
	e := emit.New(pkg)
	if err := emitForeignDecl(e, decl); err != nil {
		return nil, fmt.Errorf("codegen: Foreign: %w", err)
	}
	return e.Bytes()
}

func emitForeignDecl(e *emit.Emitter, decl *ast.ForeignDecl) error {
	langStr, err := stringLiteralValue(decl.Lang)
	if err != nil {
		return fmt.Errorf("lang: %w", err)
	}
	if !strings.EqualFold(langStr, "go") {
		return fmt.Errorf("%q: só \"go\" é suportado nesta task", langStr)
	}
	if decl.From == nil {
		return fmt.Errorf("sem \"from\"")
	}
	fromStr, err := stringLiteralValue(decl.From)
	if err != nil {
		return fmt.Errorf("from: %w", err)
	}
	pkgAlias := e.Import(path.Join(domainModuleRoot, fromStr))

	for i, fn := range decl.Functions {
		if i > 0 {
			e.Line("")
		}
		if fn == nil {
			continue
		}
		if err := emitForeignFuncGlue(e, pkgAlias, fn); err != nil {
			return fmt.Errorf("function %s: %w", fn.Name, err)
		}
	}
	return nil
}

// emitForeignFuncGlue emite "func Name(params...) (Ret, error) { return
// pkgAlias.Name(params...) }" — SEM ctx (funções puras, ex. hashing/parsing,
// §9.4; ao contrário do Adapter FFI vinculado a Notification, §9.3, uma
// fronteira de I/O que sempre carrega ctx). "(Ret, error)" é a convenção Go
// idiomática adotada aqui: o spec não fixa o contrato de erro ("Assinatura
// incompatível → erro de compilação. (Mecanismo em evolução.)", §9.4) — ret
// ausente (função sem "-> Tipo") vira só "error".
func emitForeignFuncGlue(e *emit.Emitter, pkgAlias string, fn *ast.ForeignFunc) error {
	params := make([]string, 0, len(fn.Params))
	args := make([]string, 0, len(fn.Params))
	for _, p := range fn.Params {
		if p == nil {
			continue
		}
		goType, err := goname.GoFieldType(p.Type)
		if err != nil {
			return fmt.Errorf("parâmetro %s: %w", p.Name, err)
		}
		params = append(params, fmt.Sprintf("%s %s", goname.Ident(p.Name), goType))
		args = append(args, goname.Ident(p.Name))
	}
	paramsGo := strings.Join(params, ", ")
	argsGo := strings.Join(args, ", ")

	e.Line("// %s chama %s.%s (§9.4 Foreign geral) — função hand-written esperada", fn.Name, pkgAlias, fn.Name)
	e.Line("// em adapters/, nunca gerada/sobrescrita por \"dsc gen\".")
	if fn.Return == nil {
		e.Block(fmt.Sprintf("func %s(%s) error", fn.Name, paramsGo), func() {
			e.Line("return %s.%s(%s)", pkgAlias, fn.Name, argsGo)
		})
		return nil
	}
	retGo, err := goname.GoFieldType(fn.Return)
	if err != nil {
		return fmt.Errorf("retorno: %w", err)
	}
	e.Block(fmt.Sprintf("func %s(%s) (%s, error)", fn.Name, paramsGo, retGo), func() {
		e.Line("return %s.%s(%s)", pkgAlias, fn.Name, argsGo)
	})
	return nil
}

// --- 4. Combinação por módulo (mesma forma de emitValueObjectsAndEnums). ---

// emitNotificationsAndAdapters combina TODAS as Notification e Adapter de um
// módulo num único arquivo (notifications.go) — mesmo padrão de
// emitValueObjectsAndEnums/emitViews/emitProjections (codegen.go). Cada
// Adapter precisa de sua Notification parceira (notifByName, indexado por
// codegen.go a partir do mesmo bucket) — ausência é bug de geração (o
// inverso, Notification sem Adapter, já é barrado por sema, mas o front-end
// não valida Adapter sem Notification; ver a doc do arquivo).
func emitNotificationsAndAdapters(pkg string, notifications []*ast.NotificationDecl, adapters []*ast.AdapterDecl, notifByName map[string]*ast.NotificationDecl, model *types.Model, tab *symbols.SymbolTable, module string, reg *goname.VOOperatorRegistry) ([]byte, error) {
	e := emit.New(pkg)
	for i, n := range notifications {
		if i > 0 {
			e.Line("")
		}
		if err := emitNotificationDecl(e, n); err != nil {
			return nil, fmt.Errorf("Notification %s: %w", n.Name, err)
		}
	}
	for i, a := range adapters {
		if i > 0 || len(notifications) > 0 {
			e.Line("")
		}
		notif, ok := notifByName[a.Name]
		if !ok {
			return nil, fmt.Errorf("Adapter %s: nenhuma Notification correspondente neste módulo (bug de geração — ver a doc do arquivo)", a.Name)
		}
		if err := emitAdapterDecl(e, a, notif, model, tab, module, reg); err != nil {
			return nil, fmt.Errorf("Adapter %s: %w", a.Name, err)
		}
	}
	return e.Bytes()
}

// emitForeigns combina TODOS os ForeignDecl de um módulo num único arquivo
// (foreign.go) — mesmo padrão de emitNotificationsAndAdapters.
func emitForeigns(pkg string, decls []*ast.ForeignDecl) ([]byte, error) {
	e := emit.New(pkg)
	for i, decl := range decls {
		if i > 0 {
			e.Line("")
		}
		if err := emitForeignDecl(e, decl); err != nil {
			return nil, fmt.Errorf("Foreign: %w", err)
		}
	}
	return e.Bytes()
}
