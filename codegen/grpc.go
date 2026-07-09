package codegen

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"domainscript/ast"
	"domainscript/codegen/emit"
	"domainscript/codegen/goname"
	"domainscript/codegen/grpcrt"
	"domainscript/program"
	"domainscript/symbols"
	"domainscript/token"
	"domainscript/types"
)

// grpc.go emite a borda gRPC (H1, REQ-29, §design 3.12): a partir de um
// *ast.InterfaceDecl Kind=="GRPC" (achado por findGroupGRPCInterface, mesmo
// padrão de findGroupInterface para HTTP), gera (a) um artefato .proto
// textual (proto/<service>.proto — REQ-29.1, ver grpc_proto.go) e (b) um
// grpc.Server DE VERDADE, falando gRPC-sobre-HTTP/2 real, que despacha para
// as MESMAS funções de UseCase/Query que a borda HTTP chama (REQ-29.3, mesmo
// resolveRouteTarget de http.go) — sem Interface GRPC no grupo, nada disto é
// emitido (grpcInterfaceHasServices, ausência ausente por completo, opt-in).
//
// --- Por que não protoc (a decisão de arquitetura desta task) ---
//
// Este pipeline não depende de nenhum binário externo para gerar código —
// nem sequer o parser depende de um gerador (é recursivo-descendente à mão,
// REQ-3/NFR-1); um passo `protoc` no meio da geração quebraria essa garantia
// e exigiria uma ferramenta que pode nem estar instalada no ambiente de quem
// roda `dsc gen`. A escolha desta task: emitir o .proto como um artefato
// TEXTUAL (documentação/interoperabilidade — para um usuário que queira
// rodar protoc por conta própria contra outro cliente/stack, ou só para ler
// o contrato), e gerar o SERVIDOR gRPC de verdade sem depender de nenhum
// tipo *.pb.go — via a API manual de baixo nível do grpc-go: um
// grpc.ServiceDesc feito à mão por GrpcService, com um grpc.MethodDesc por
// GrpcRPC (mesmo "shape" que protoc-gen-go-grpc geraria, só que escrito por
// este arquivo em vez de por protoc). Os DTOs de request/response são
// structs Go comuns com tag `json` — NÃO implementam proto.Message — e o
// grpc.Server é forçado (grpc.ForceServerCodec, ver newGRPCServer abaixo) a
// usar um encoding.Codec de JSON (grpcedge.JSONCodec, codegen/grpcrt/
// codec.go.txt) em vez do codec "proto" default. Isso é uma EXCEÇÃO
// DOCUMENTADA à NFR-12 (REQ-29.2): a dependência google.golang.org/grpc é
// real e isolada (só neste arquivo + no pacote vendorado grpcedge + na linha
// de wiring em func main(), nunca em pacote de domínio — ver
// programNeedsGRPC/EmitGoMod), mas o transporte gRPC-sobre-HTTP/2 e a
// semântica de RPC (status codes, streaming de metadata, etc.) são reais —
// só a codificação da MENSAGEM não é o protobuf binário de sempre. Ver o
// comentário de topo de codegen/grpcrt/codec.go.txt para o tradeoff completo
// (um cliente gerado por protoc a partir do .proto emitido NÃO fala com este
// servidor sem a mesma troca de codec).
//
// --- Reuso do MESMO dispatch de domínio que a borda HTTP chama (REQ-29.3) ---
//
// Uma rpc cujo Target resolve a um UseCase decodifica o corpo INTEIRO da
// mensagem gRPC diretamente no MESMO tipo Command que o UseCase espera
// (reusa o struct gerado por decl_command.go — nenhuma tradução de shape:
// gRPC não tem "path param" para correlacionar como a rota HTTP tem, então o
// Command inteiro vem do corpo da mensagem) e chama a MESMA função
// "<pkg>.<UseCase>(ctx, cmd)" que emitUseCaseRoute (http.go) chama — nunca
// uma segunda cópia da lógica de negócio. Uma rpc cujo Target resolve a uma
// Query gera um struct de request PRÓPRIO (um campo por Query.Params — não
// há um struct existente para reusar, ao contrário do Command) e chama a
// MESMA função "<pkg>.<Query>(ctx, store, ...)" que emitQueryRoute chama,
// devolvendo o MESMO tipo de retorno (a View) direto (sem downcast/upcast —
// ver a nota de escopo abaixo). A resolução de Target usa resolveRouteTarget
// (http.go) TAL QUAL — sua assinatura não referencia nada específico de
// HTTP (buckets/modules só), então é reusada diretamente em vez de
// duplicada (mesma decisão para findCommandInBuckets/aliasForSymbol/
// aggregateShape/commandFieldParseType, todos de http.go).
//
// --- Caller/idempotency-key: paridade mínima com a borda HTTP ---
//
// Toda função de UseCase gerada (decl_usecase.go) lê incondicionalmente
// "caller, _ := runtime.CallerFrom(ctx)" na primeira linha do corpo — sem
// ISSO, uma rpc que despacha para um UseCase com "access { requires
// caller.* }" receberia uma runtime.Caller nil e sofreria nil pointer
// dereference. Por isso todo handler de rpc-para-UseCase (nunca um
// handler de rpc-para-Query, que nunca referencia caller — ver
// decl_query.go) chama grpcedge.CallerFromContext(ctx) (extrai a
// metadata "x-caller-id" de entrada, o equivalente gRPC do header
// "X-Caller-Id" da borda HTTP — devCallerFromRequest, http.go) e injeta o
// resultado via runtime.WithCaller ANTES de despachar — e, no mesmo
// espírito, repassa "idempotency-key" da metadata via
// runtime.WithIdempotencyKey quando presente (carrier só, mesma ressalva de
// emitCallerAndIdempotency em http.go). Diferente de devCaller/
// writeBusinessError (emitidos POR PROGRAMA em http.go, por razões
// históricas de quando não havia um pacote de borda isolado para gRPC),
// CallerFromContext/IdempotencyKeyFromContext/StatusError são INVARIANTES
// entre programas e por isso moram vendorados em grpcedge (ver a doc de
// codegen/grpcrt) — nada disso é reemitido aqui por programa.
//
// --- Fora do escopo desta task (documentado de propósito) ---
//
// Tenant (G5), rate limit (G4) e versionamento de API (G6) NÃO têm
// equivalente na borda gRPC nesta task — REQ-29 pede só (1) o .proto, (2) um
// servidor gRPC real isolando a dependência, e (3) reuso do MESMO dispatch
// de domínio; nenhum dos três menciona tenant/rate-limit/versioning. Uma
// Interface GRPC cujo mesmo GRUPO também declare uma Interface HTTP com
// esses recursos continua tendo-os SÓ na borda HTTP — mencionado aqui, de
// propósito, para não travar a extensão futura (mesmo estilo de "sunset
// continua marco futuro" em http.go).
//
// --- Wiring em func main() (ver generateCmdMainFile, codegen.go) ---
//
// Quando o grupo declara Interface GRPC com ao menos 1 GrpcService com ao
// menos 1 GrpcRPC (grpcInterfaceHasServices), func main() abre um
// net.Listen na porta do setting "port:" da Interface GRPC (grpcPortGo,
// fallback "9090" — DIFERENTE do fallback HTTP "8080", de propósito: as
// duas bordas podem coexistir no MESMO service sem Interface declarar porta
// nenhuma, e usar o MESMO fallback faria as duas tentarem bind na mesma
// porta) e sobe grpcServer.Serve(lis) em sua PRÓPRIA goroutine — HTTP
// continua sendo a chamada bloqueante final de main() (ListenAndServe),
// então os dois servidores rodam no MESMO processo. Um grupo SÓ-gRPC (sem
// Interface HTTP) não muda nada dessa história: HTTP já sobe incondicional-
// mente hoje (mux vazio, "grupos só-worker"), então basta a gRPC não
// depender desse fato — ver a doc de generateCmdMainFile.

// findGroupGRPCInterface acha o *ast.InterfaceDecl GRPC de qualquer arquivo
// dos módulos de modules (percorrido em ordem de path — determinismo), ou
// nil se nenhum módulo do grupo declara "Interface GRPC" — mesmo padrão de
// findGroupInterface (HTTP), Kind=="GRPC" em vez de "HTTP".
func findGroupGRPCInterface(prog *program.Program, modules []string) *ast.InterfaceDecl {
	inGroup := make(map[string]bool, len(modules))
	for _, m := range modules {
		inGroup[m] = true
	}

	var paths []string
	for p := range prog.Files {
		if inGroup[prog.ModuleOf(p)] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	for _, p := range paths {
		for _, d := range prog.Files[p].Decls {
			if id, ok := d.(*ast.InterfaceDecl); ok && id.Kind == "GRPC" {
				return id
			}
		}
	}
	return nil
}

// programNeedsGRPC devolve true se ALGUM arquivo do programa (em QUALQUER
// módulo/grupo) declara "Interface GRPC" — o único gatilho que acrescenta
// google.golang.org/grpc a go.mod (EmitGoMod) E emite grpcedge/*.go
// (generateGRPCRuntimeFiles), mirror de programNeedsSQLAdapter (G1,
// sql_wiring.go) para o adapter SQL.
func programNeedsGRPC(prog *program.Program) bool {
	for _, f := range prog.Files {
		for _, d := range f.Decls {
			if id, ok := d.(*ast.InterfaceDecl); ok && id.Kind == "GRPC" {
				return true
			}
		}
	}
	return false
}

// grpcInterfaceHasServices reporta se iface declara ao menos 1 GrpcService
// com ao menos 1 GrpcRPC — iface nil (nenhuma Interface GRPC no grupo, ou
// uma Interface GRPC sem nenhum service/rpc) devolve false. Pré-computado
// por generateCmdMainFile ANTES de emitir func main() (que decide, só com
// este booleano, se sobe o listener gRPC) — o registro de fato dos serviços
// (emitGRPCServer/newGRPCServer) só acontece bem mais tarde, depois de
// newMux/helpers (mesma ordem de emissão que HTTP já segue).
func grpcInterfaceHasServices(iface *ast.InterfaceDecl) bool {
	if iface == nil {
		return false
	}
	for _, svc := range iface.Services {
		if len(svc.RPCs) > 0 {
			return true
		}
	}
	return false
}

// grpcPortGo devolve o literal Go (já entre aspas) da porta gRPC — mesma
// forma de httpPortGo (http.go: setting "port:" de iface quando é um literal
// INT/STRING; fallback documentado em qualquer outro caso), duplicada aqui
// (em vez de reusada) só para poder ter um FALLBACK DIFERENTE — "9090" em
// vez de "8080" — e não arriscar as duas bordas colidindo na mesma porta
// quando NENHUMA das duas Interfaces declara "port:" explicitamente (ver a
// doc do arquivo).
func grpcPortGo(iface *ast.InterfaceDecl) string {
	if iface != nil {
		for _, entry := range iface.Settings {
			if entry.Key != "port" {
				continue
			}
			if lit, ok := entry.Value.(*ast.Literal); ok {
				switch lit.Kind {
				case token.INT, token.STRING:
					return strconv.Quote(lit.Value)
				}
			}
			break
		}
	}
	return `"9090"`
}

// generateGRPCRuntimeFiles copia grpcrt.Sources() (verbatim, mesmo padrão de
// generateRuntimeFiles/generateSQLRuntimeFiles) para grpcedge/*.go — só
// chamado quando programNeedsGRPC devolve true.
func generateGRPCRuntimeFiles() ([]File, error) {
	srcs, err := grpcrt.Sources()
	if err != nil {
		return nil, fmt.Errorf("codegen: grpcedge: %w", err)
	}
	names := make([]string, 0, len(srcs))
	for name := range srcs {
		names = append(names, name)
	}
	sort.Strings(names)

	files := make([]File, 0, len(names))
	for _, name := range names {
		files = append(files, File{Path: path.Join("grpcedge", name), Content: srcs[name]})
	}
	return files, nil
}

// grpcQueryRequestSpec é o request DTO (já resolvido) de uma rpc cujo Target
// resolve a uma Query: o nome do struct Go gerado e um campo por
// Query.Params, na ordem declarada — reusa commandFieldInfo (decl_command.go)
// para não duplicar a forma (field/goType/exportName).
type grpcQueryRequestSpec struct {
	structName string
	fields     []commandFieldInfo
}

// emitGRPCServer emite, como declarações de PACOTE (chamado no máximo 1 vez
// por cmd/<group>/main.go, depois que newMux/helpers fecham — ver
// generateCmdMainFile), func newGRPCServer(store runtime.EventStore)
// *grpc.Server, que registra um grpc.ServiceDesc por GrpcService de iface, e
// os structs de request de cada rpc-para-Query que precisou de um (ver a doc
// de grpcQueryRequestSpec). iface nil ou sem nenhum GrpcService/GrpcRPC não
// emite nada (mesma guarda que grpcInterfaceHasServices já aplicou no
// chamador — repetida aqui defensivamente).
func emitGRPCServer(e *emit.Emitter, protoPkg string, iface *ast.InterfaceDecl, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable) error {
	if !grpcInterfaceHasServices(iface) {
		return nil
	}

	ctxAlias := e.Import("context")
	grpcAlias := e.Import("google.golang.org/grpc")
	runtimeAlias := e.Import(RuntimeImportPath)
	grpcedgeAlias := e.Import(path.Join(domainModuleRoot, "grpcedge"))

	var pending []grpcQueryRequestSpec
	var bodyErr error

	e.Line("")
	e.Line("// newGRPCServer constrói o *grpc.Server do service %q, registrando um", protoPkg)
	e.Line("// grpc.ServiceDesc por \"service\" de Interface GRPC (H1, §design 3.12) —")
	e.Line("// função à parte de main() (mesmo espírito de newMux/HTTP) para poder ser")
	e.Line("// exercitada por teste via bufconn, sem subir um socket real.")
	e.Block(fmt.Sprintf("func newGRPCServer(store %s.EventStore) *%s.Server", runtimeAlias, grpcAlias), func() {
		e.Line("srv := %s.NewServer(%s.ForceServerCodec(%s.JSONCodec{}))", grpcAlias, grpcAlias, grpcedgeAlias)
		for _, svc := range iface.Services {
			if len(svc.RPCs) == 0 {
				continue
			}
			if err := emitGRPCServiceRegistration(e, protoPkg, svc, buckets, modules, model, tab, ctxAlias, grpcAlias, runtimeAlias, grpcedgeAlias, &pending); err != nil {
				bodyErr = fmt.Errorf("service %s: %w", svc.Name, err)
				return
			}
		}
		e.Line("return srv")
	})
	if bodyErr != nil {
		return bodyErr
	}

	for _, spec := range pending {
		e.Line("")
		emitGRPCQueryRequestStructDecl(e, spec)
	}
	return nil
}

// emitGRPCServiceRegistration emite "srv.RegisterService(&grpc.ServiceDesc{
// ... }, nil)" para UM GrpcService: ServiceName é "<protoPkg>.<svc.Name>" (o
// mesmo par package+service do .proto emitido por emitGRPCProtoFile,
// grpc_proto.go — para que FullMethod bata entre client/server de teste),
// HandlerType fica nil (o segundo argumento de RegisterService, o "ss"
// registrado, também é nil — grpc-go só valida a interface HandlerType
// quando ss != nil, ver server.go:RegisterService; nossos handlers são
// closures que já capturam tudo que precisam, nenhum "srv.(XServer)" é
// necessário), e Methods tem 1 grpc.MethodDesc por GrpcRPC.
func emitGRPCServiceRegistration(e *emit.Emitter, protoPkg string, svc *ast.GrpcService, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable, ctxAlias, grpcAlias, runtimeAlias, grpcedgeAlias string, pending *[]grpcQueryRequestSpec) error {
	fullServiceName := fmt.Sprintf("%s.%s", protoPkg, svc.Name)

	e.Line("srv.RegisterService(&%s.ServiceDesc{", grpcAlias)
	e.Line("ServiceName: %s,", strconv.Quote(fullServiceName))
	e.Line("HandlerType: nil,")

	var innerErr error
	e.BlockSuffix(fmt.Sprintf("Methods: []%s.MethodDesc", grpcAlias), ",", func() {
		for _, rpc := range svc.RPCs {
			if innerErr != nil {
				return
			}
			target, err := resolveRouteTarget(buckets, modules, rpc.Target)
			if err != nil {
				innerErr = fmt.Errorf("rpc %s -> %s: %w", rpc.Name, rpc.Target, err)
				return
			}
			fullMethod := fmt.Sprintf("/%s/%s", fullServiceName, rpc.Name)

			e.Line("{")
			e.Line("MethodName: %s,", strconv.Quote(rpc.Name))
			header := fmt.Sprintf("Handler: func(srv any, ctx %s.Context, dec func(any) error, interceptor %s.UnaryServerInterceptor) (any, error)", ctxAlias, grpcAlias)
			e.BlockSuffix(header, ",", func() {
				if target.usecase != nil {
					cmdDecl, cmdModule, ok := findCommandInBuckets(buckets, modules, target.usecase.Handles)
					if !ok {
						innerErr = fmt.Errorf("rpc %s: UseCase %s: Command %q (handles) não encontrado nos módulos do grupo", rpc.Name, target.usecase.Name, target.usecase.Handles)
						return
					}
					cmdAlias, err := aliasForSymbol(e, tab, cmdModule, cmdDecl.Name)
					if err != nil {
						innerErr = fmt.Errorf("rpc %s: %w", rpc.Name, err)
						return
					}
					ucAlias, err := aliasForSymbol(e, tab, target.ucModule, target.usecase.Name)
					if err != nil {
						innerErr = fmt.Errorf("rpc %s: %w", rpc.Name, err)
						return
					}
					emitGRPCUseCaseHandlerBody(e, cmdAlias, cmdDecl.Name, ucAlias, target.usecase.Name, fullMethod, ctxAlias, grpcAlias, runtimeAlias, grpcedgeAlias)
					return
				}

				structName := fmt.Sprintf("%s%sRequest", svc.Name, rpc.Name)
				spec, err := buildGRPCQueryRequestSpec(e, structName, target.query, model, tab, target.qModule)
				if err != nil {
					innerErr = fmt.Errorf("rpc %s: %w", rpc.Name, err)
					return
				}
				qAlias, err := aliasForSymbol(e, tab, target.qModule, target.query.Name)
				if err != nil {
					innerErr = fmt.Errorf("rpc %s: %w", rpc.Name, err)
					return
				}
				*pending = append(*pending, spec)
				emitGRPCQueryHandlerBody(e, spec, qAlias, target.query.Name, fullMethod, ctxAlias, grpcAlias, grpcedgeAlias)
			})
			e.Line("},")
		}
	})
	if innerErr != nil {
		return innerErr
	}
	e.Line("}, nil)")
	return nil
}

// emitGRPCUseCaseHandlerBody emite o corpo de um grpc.MethodHandler cujo
// Target resolve a um UseCase (ver a doc do arquivo): decodifica a mensagem
// INTEIRA no Command (nenhuma correlação de path param — gRPC não tem
// path), injeta caller/idempotency-key a partir da metadata de entrada, e
// despacha para "<ucAlias>.<ucName>(ctx, cmd)" — sucesso vira
// grpcedge.Empty{} (mirror do 204 HTTP sem corpo), erro vira
// grpcedge.StatusError(err) (mirror de writeBusinessError, mapeado a
// codes.Code em vez de status HTTP — ver codegen/grpcrt/status.go.txt).
func emitGRPCUseCaseHandlerBody(e *emit.Emitter, cmdAlias, cmdName, ucAlias, ucName, fullMethod, ctxAlias, grpcAlias, runtimeAlias, grpcedgeAlias string) {
	e.Line("var req %s.%s", cmdAlias, cmdName)
	e.Block("if err := dec(&req); err != nil", func() {
		e.Line("return nil, err")
	})
	e.Line("ctx = %s.WithCaller(ctx, %s.CallerFromContext(ctx))", runtimeAlias, grpcedgeAlias)
	e.Block(fmt.Sprintf("if key, ok := %s.IdempotencyKeyFromContext(ctx); ok", grpcedgeAlias), func() {
		e.Line("ctx = %s.WithIdempotencyKey(ctx, key)", runtimeAlias)
	})
	e.Block(fmt.Sprintf("call := func(ctx %s.Context, req any) (any, error)", ctxAlias), func() {
		e.Block(fmt.Sprintf("if err := %s.%s(ctx, req.(%s.%s)); err != nil", ucAlias, ucName, cmdAlias, cmdName), func() {
			e.Line("return nil, %s.StatusError(err)", grpcedgeAlias)
		})
		e.Line("return &%s.Empty{}, nil", grpcedgeAlias)
	})
	e.Block("if interceptor == nil", func() {
		e.Line("return call(ctx, req)")
	})
	e.Line("info := &%s.UnaryServerInfo{Server: srv, FullMethod: %s}", grpcAlias, strconv.Quote(fullMethod))
	e.Line("return interceptor(ctx, req, info, call)")
}

// emitGRPCQueryHandlerBody emite o corpo de um grpc.MethodHandler cujo
// Target resolve a uma Query (ver a doc do arquivo): decodifica a mensagem
// no struct de request PRÓPRIO desta rpc (spec.structName — um campo por
// Query.Params), e despacha para "<qAlias>.<qName>(ctx, store, ...)" —
// sucesso devolve o valor de retorno (a View) DIRETO, sem tradução; erro vira
// grpcedge.StatusError(err) (mesma forma de emitGRPCUseCaseHandlerBody).
// Query nunca referencia caller (decl_query.go não emite CallerFrom), então,
// ao contrário do handler de UseCase, nenhum caller/idempotency-key é
// injetado aqui.
func emitGRPCQueryHandlerBody(e *emit.Emitter, spec grpcQueryRequestSpec, qAlias, qName, fullMethod, ctxAlias, grpcAlias, grpcedgeAlias string) {
	e.Line("var req %s", spec.structName)
	e.Block("if err := dec(&req); err != nil", func() {
		e.Line("return nil, err")
	})
	e.Block(fmt.Sprintf("call := func(ctx %s.Context, req any) (any, error)", ctxAlias), func() {
		e.Line("r := req.(%s)", spec.structName)
		argNames := make([]string, len(spec.fields))
		for i, fi := range spec.fields {
			argNames[i] = "r." + fi.exportName
		}
		callArgs := append([]string{"ctx", "store"}, argNames...)
		e.Line("result, err := %s.%s(%s)", qAlias, qName, strings.Join(callArgs, ", "))
		e.Block("if err != nil", func() {
			e.Line("return nil, %s.StatusError(err)", grpcedgeAlias)
		})
		e.Line("return result, nil")
	})
	e.Block("if interceptor == nil", func() {
		e.Line("return call(ctx, req)")
	})
	e.Line("info := &%s.UnaryServerInfo{Server: srv, FullMethod: %s}", grpcAlias, strconv.Quote(fullMethod))
	e.Line("return interceptor(ctx, req, info, call)")
}

// buildGRPCQueryRequestSpec resolve, para uma rpc-para-Query, o request DTO:
// um campo por Query.Params. Ao contrário do Command reusado por uma
// rpc-para-UseCase (que já vive no pacote de domínio, EmitCommands,
// decl_command.go), este struct é declarado em cmd/<group>/main.go — pacote
// "main", DIFERENTE do pacote que declara os tipos de campo — por isso usa
// grpcQualifiedGoType (abaixo) em vez de commandFieldGoType: precisa
// QUALIFICAR todo VOType/EnumType/ShapeType com o alias do pacote de domínio,
// nunca a identidade nua que commandFieldGoType assume (correta só quando o
// struct gerado mora no MESMO pacote que os tipos que referencia).
// commandFieldParseType (http.go) resolve o *types.Type do campo — cobre
// "ref Aggregate" com a mesma regra de refFieldGoType, e o campo comum via
// model.TypeOfRef — ANTES de qualificá-lo para Go.
func buildGRPCQueryRequestSpec(e *emit.Emitter, structName string, q *ast.QueryDecl, model *types.Model, tab *symbols.SymbolTable, module string) (grpcQueryRequestSpec, error) {
	infos := make([]commandFieldInfo, 0, len(q.Params))
	for _, f := range q.Params {
		if f == nil {
			continue
		}
		t, err := commandFieldParseType(f, model, tab, module)
		if err != nil {
			return grpcQueryRequestSpec{}, fmt.Errorf("Query %s: parâmetro %s: %w", q.Name, f.Name, err)
		}
		goType, err := grpcQualifiedGoType(e, tab, module, t)
		if err != nil {
			return grpcQueryRequestSpec{}, fmt.Errorf("Query %s: parâmetro %s: %w", q.Name, f.Name, err)
		}
		infos = append(infos, commandFieldInfo{field: f, goType: goType, exportName: goname.ExportField(f.Name)})
	}
	return grpcQueryRequestSpec{structName: structName, fields: infos}, nil
}

// grpcQualifiedGoType devolve o tipo Go de t, QUALIFICADO pelo alias do
// pacote de domínio quando t é um VOType/EnumType/ShapeType (ver a doc de
// buildGRPCQueryRequestSpec sobre por que a qualificação é necessária aqui e
// não em commandFieldGoType). Primitivos usam o mesmo mapeamento de
// goname.GoPrimitive (já corretos sem qualificação: builtins Go nus, ou
// "runtime."/"time." — pacotes que qualquer arquivo pode importar
// igualmente). homeModule ancora aliasForSymbol (Lookup local, fallback Find
// cross-module — mesma regra de aliasForSymbol/commandFieldParseType,
// http.go). Qualquer outra forma (ex. *types.Generic — List/Set/Map) é erro
// de geração claro: suporte mínimo definido por esta task.
func grpcQualifiedGoType(e *emit.Emitter, tab *symbols.SymbolTable, homeModule string, t types.Type) (string, error) {
	switch x := t.(type) {
	case *types.Primitive:
		s, ok := goname.GoPrimitive(x.Name)
		if !ok {
			return "", fmt.Errorf("primitivo %s sem mapeamento Go (bug de geração)", x.Name)
		}
		if strings.HasPrefix(s, "runtime.") {
			e.Import(RuntimeImportPath)
		}
		return s, nil
	case *types.VOType:
		alias, err := aliasForSymbol(e, tab, homeModule, x.Name)
		if err != nil {
			return "", err
		}
		return goname.QualifiedRef(alias, x.Name), nil
	case *types.EnumType:
		alias, err := aliasForSymbol(e, tab, homeModule, x.Name)
		if err != nil {
			return "", err
		}
		return goname.QualifiedRef(alias, x.Name), nil
	case *types.ShapeType:
		alias, err := aliasForSymbol(e, tab, homeModule, x.Name)
		if err != nil {
			return "", err
		}
		return goname.QualifiedRef(alias, x.Name), nil
	default:
		return "", fmt.Errorf("tipo %s não suportado em request gRPC de Query (H1 cobre primitivo, ValueObject, Enum e \"ref Aggregate\" — List/Set/Map ficam para quando surgir necessidade real)", t.String())
	}
}

// emitGRPCQueryRequestStructDecl emite a declaração de PACOTE do struct de
// request de uma rpc-para-Query (ver a doc de grpcQueryRequestSpec) — mesma
// forma de emitCommandDecl (decl_command.go): campos exportados com tag
// json, na ordem declarada de Query.Params.
func emitGRPCQueryRequestStructDecl(e *emit.Emitter, spec grpcQueryRequestSpec) {
	needsRuntime := false
	for _, fi := range spec.fields {
		if strings.HasPrefix(fi.goType, "runtime.") {
			needsRuntime = true
		}
	}
	if needsRuntime {
		e.Import(RuntimeImportPath)
	}

	e.Line("// %s é o request gRPC (H1, REQ-29.1/29.2) de uma rpc que despacha para", spec.structName)
	e.Line("// uma Query — um campo por parâmetro declarado (mesma shape de Query.Params).")
	e.Block(fmt.Sprintf("type %s struct", spec.structName), func() {
		for _, fi := range spec.fields {
			e.Line("%s %s %s", fi.exportName, fi.goType, goname.JSONTag(fi.field.Name))
		}
	})
}
