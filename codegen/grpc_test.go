package codegen_test

import (
	"path/filepath"
	"strings"
	"testing"

	"domainscript/codegen"
	"domainscript/codegen/gentest"
	"domainscript/driver"
	"domainscript/types"
)

// grpc_test.go prova os critérios de conclusão da task H1 (§design codegen
// 3.12, REQ-29): borda gRPC — ".proto" (a partir de Interface GRPC +
// GrpcService/GrpcRPC) + um grpc.Server de verdade, com a dependência
// google.golang.org/grpc isolada num pacote de borda (grpcedge, opt-in,
// ausente sem "Interface GRPC" — ver codegen/grpc.go). Nem o wallet nem o
// shop declaram "Interface GRPC" (confirmado antes de escrever esta task —
// grep em docs/examples/*/interface.ds), então a fixture é sintética, no
// mesmo espírito de "Notes"/"Billing" (G5/G6, tenancy_test.go/
// versioning_test.go): um módulo "GrpcDemo" mínimo com um Aggregate (Item),
// um UseCase (TouchItemUseCase, escrita) e uma Query (GetItem, leitura),
// expostos por "Interface GRPC { service ItemService { rpc ... } }".

const grpcDemoModDs = `Module GrpcDemo {
    Database GrpcDemoDb {
        provider: "postgres"
        manages: [Item]
    }
}
`

const grpcDemoDomainDs = `
ValueObject ItemId(string) {
    Valid { value.length() > 0 }
}

ValueObject ItemNote(string) {
    Valid { value.length() <= 200 }
}

Event ItemTouched {
    id   ItemId
    note ItemNote
}

Aggregate Item {
    strategy EventSourced

    state {
        id   ItemId
        note ItemNote
    }

    access {
        Touch requires caller.authenticated
    }

    Handle Touch(note ItemNote) {
        emit ItemTouched(self.id, note)
    }

    Apply ItemTouched {
        state.note = event.note
    }
}
`

const grpcDemoApplicationDs = `
Command TouchItem {
    itemId ref Item
    note   ItemNote
}

UseCase TouchItemUseCase handles TouchItem {
    execute {
        item = load Item(cmd.itemId)
        item.Touch(cmd.note)
    }
}
`

const grpcDemoReadDs = `
View ItemView {
    id   ItemId
    note ItemNote
}

Query GetItem(id ItemId) -> ItemView {
    return load Item(id) as ItemView
}
`

// grpcDemoInterfaceDs: "port: 50051" (um literal INT, ao contrário de
// wallet/billing, que nunca declaram "port:" — exercita o ramo literal de
// grpcPortGo, não só o fallback "9090"). Dois rpc: Touch (UseCase, escrita) e
// GetItem (Query, leitura) — as duas formas que emitGRPCServer suporta.
const grpcDemoInterfaceDs = `Interface GRPC {
    port: 50051

    service ItemService {
        rpc Touch   -> TouchItemUseCase
        rpc GetItem -> GetItem
    }
}
`

// grpcDemoGenerateOptions espelha billingGenerateOptions/walletGenerateOptions
// — mesmo module path que RuntimeImportPath assume implicitamente.
var grpcDemoGenerateOptions = codegen.Options{ModulePath: "domainscript/generated", GoVersion: "1.22"}

// generateGRPCDemoProject escreve a fixture GrpcDemo em disco e gera o
// projeto Go completo via driver.CheckProject + codegen.Generate — mesmo
// padrão de generateBillingProject/generateWalletProject.
func generateGRPCDemoProject(t *testing.T) []codegen.File {
	t.Helper()
	dir := writeProjectDir(t, map[string]string{
		"mod.ds":         grpcDemoModDs,
		"domain.ds":      grpcDemoDomainDs,
		"application.ds": grpcDemoApplicationDs,
		"read.ds":        grpcDemoReadDs,
		"interface.ds":   grpcDemoInterfaceDs,
	})
	prog, bag := driver.CheckProject(dir)
	if bag.HasErrors() {
		t.Fatalf("fixture sintética GrpcDemo (H1) tem diagnósticos de erro:\n%s", bag.Render())
	}
	model := types.NewModel(prog.Symbols)

	files, err := codegen.Generate(prog, model, prog.Symbols, bag, grpcDemoGenerateOptions)
	if err != nil {
		t.Fatalf("Generate: erro inesperado sobre a fixture GrpcDemo: %v", err)
	}
	return files
}

func grpcDemoFileByPath(t *testing.T, files []codegen.File, p string) []byte {
	t.Helper()
	for _, f := range files {
		if f.Path == p {
			return f.Content
		}
	}
	t.Fatalf("%s não encontrado entre os arquivos gerados", p)
	return nil
}

func grpcDemoCmdMainFile(t *testing.T) []byte {
	t.Helper()
	return grpcDemoFileByPath(t, generateGRPCDemoProject(t), "cmd/grpcdemo/main.go")
}

func grpcDemoProtoFile(t *testing.T) []byte {
	t.Helper()
	return grpcDemoFileByPath(t, generateGRPCDemoProject(t), "proto/grpcdemo.proto")
}

// --- 1. Golden/determinismo/smoke — os critérios NFR-13/14 de sempre. ---

// TestGenerateGRPCDemoMainGolden prova, sobre o Go de fato gerado em
// cmd/grpcdemo/main.go: o listener gRPC na porta literal declarada (50051),
// newGRPCServer registrando ItemService com os 2 métodos (Touch, GetItem), o
// handler de Touch decodificando DIRETO no Command (TouchItem, reuso —
// nenhuma tradução de shape) e chamando o MESMO TouchItemUseCase que uma
// rota HTTP chamaria, o handler de GetItem com seu request DTO PRÓPRIO
// (ItemServiceGetItemRequest) chamando a MESMA função GetItem(ctx, store,
// ...), e o mapeamento de erro via grpcedge.StatusError (nunca uma segunda
// cópia de writeBusinessError).
func TestGenerateGRPCDemoMainGolden(t *testing.T) {
	got := string(grpcDemoCmdMainFile(t))
	for _, want := range []string{
		`grpcPort := "50051"`,
		`grpcLis, listenErr := net.Listen("tcp", fmt.Sprintf(":%s", grpcPort))`,
		"grpcServer := newGRPCServer(store)",
		"go func() {",
		"log.Fatal(grpcServer.Serve(grpcLis))",
		"func newGRPCServer(store runtime.EventStore) *grpc.Server",
		"srv := grpc.NewServer(grpc.ForceServerCodec(grpcedge.JSONCodec{}))",
		`ServiceName: "grpcdemo.ItemService"`,
		`MethodName: "Touch"`,
		"var req grpcdemo.TouchItem",
		"ctx = runtime.WithCaller(ctx, grpcedge.CallerFromContext(ctx))",
		"if key, ok := grpcedge.IdempotencyKeyFromContext(ctx); ok",
		"if err := grpcdemo.TouchItemUseCase(ctx, req.(grpcdemo.TouchItem)); err != nil",
		"return nil, grpcedge.StatusError(err)",
		"return &grpcedge.Empty{}, nil",
		`MethodName: "GetItem"`,
		"var req ItemServiceGetItemRequest",
		"r := req.(ItemServiceGetItemRequest)",
		"result, err := grpcdemo.GetItem(ctx, store, r.Id)",
		`FullMethod: "/grpcdemo.ItemService/Touch"`,
		`FullMethod: "/grpcdemo.ItemService/GetItem"`,
		"type ItemServiceGetItemRequest struct",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q em cmd/grpcdemo/main.go, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "cmd_grpcdemo_main.go.golden"), []byte(got))
}

// TestGenerateGRPCDemoProtoGolden prova o critério de conclusão #1 (REQ-29.1):
// proto/grpcdemo.proto declara "service ItemService" com as 2 rpc, a message
// do Command reusado por Touch (TouchItem — campo "ref Item" resolvido ao
// tipo do "id" do state, mesma regra de commandFieldParseType/http.go), a
// message de request PRÓPRIA de GetItem, a message de resposta (a View
// ItemView) e "message Empty {}" (a resposta de toda rpc-para-UseCase).
func TestGenerateGRPCDemoProtoGolden(t *testing.T) {
	got := string(grpcDemoProtoFile(t))
	for _, want := range []string{
		`syntax = "proto3";`,
		"package grpcdemo;",
		"service ItemService {",
		"rpc Touch (TouchItem) returns (Empty);",
		"rpc GetItem (ItemServiceGetItemRequest) returns (ItemView);",
		"message Empty {}",
		"message TouchItem {",
		"string item_id = 1;",
		"string note = 2;",
		"message ItemServiceGetItemRequest {",
		"string id = 1;",
		"message ItemView {",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("esperava %q em proto/grpcdemo.proto, não achei:\n%s", want, got)
		}
	}
	gentest.Golden(t, filepath.Join("testdata", "grpcdemo.proto.golden"), []byte(got))
}

// TestGenerateGRPCDemoDeterministic prova NFR-13 escopado a cmd/grpcdemo/
// main.go e a proto/grpcdemo.proto — mesmo padrão de
// TestGenerateWalletHTTPRoutesDeterministic (http_test.go)/
// TestGenerateBillingVersioningDeterministic (versioning_test.go).
func TestGenerateGRPCDemoDeterministic(t *testing.T) {
	t.Run("main.go", func(t *testing.T) {
		gentest.Deterministic(t, func() []byte {
			return grpcDemoCmdMainFile(t)
		})
	})
	t.Run("proto", func(t *testing.T) {
		gentest.Deterministic(t, func() []byte {
			return grpcDemoProtoFile(t)
		})
	})
}

// TestGenerateGRPCDemoSmokeCompile prova NFR-14: o projeto gerado inteiro —
// incl. grpcedge/*.go (vendorado) e go.mod com "require google.golang.org/grpc"
// — compila e passa go vet. gentest.SmokeCompile detecta o bloco "require" e
// roda `go mod tidy` primeiro (ver a doc de SmokeCompile/needsModTidy,
// gentest/smoke.go) — precisa de acesso à rede ao proxy de módulos Go; já
// confirmado disponível neste ambiente antes de escrever esta task (`go get
// google.golang.org/grpc@v1.67.0` resolveu e `go build`/`go vet` passaram
// sobre um projeto de prova isolado).
func TestGenerateGRPCDemoSmokeCompile(t *testing.T) {
	files := generateGRPCDemoProject(t)
	gentest.SmokeCompile(t, filesToMap(files))
}

// --- 2. Comportamento de verdade: round-trip via bufconn (o coração da task). ---
//
// grpcDemoBehaviorTest roda DENTRO de cmd/grpcdemo (mesmo pacote de
// newGRPCServer, "package main") e prova, sobre o *grpc.Server de fato
// gerado — SEM subir nenhum socket real (google.golang.org/grpc/test/bufconn,
// mesmo espírito "sem socket" dos demais testes comportamentais deste
// pacote):
//
//  1. Um client gRPC real (grpc.NewClient) discando via bufconn, com a MESMA
//     troca de codec JSON do lado do servidor (grpc.ForceCodec(grpcedge.
//     JSONCodec{})) — nenhum tipo *.pb.go em lugar nenhum.
//  2. "Touch" (rpc-para-UseCase) despacha de fato para TouchItemUseCase: o
//     caller vem da metadata gRPC "x-caller-id" (o equivalente ao X-Caller-Id
//     HTTP — sem ele, "access { Touch requires caller.authenticated }" barra
//     com Forbidden -> codes.PermissionDenied), e o evento ItemTouched é
//     persistido de verdade (uow real, runtime.NewUnitOfWork).
//  3. "GetItem" (rpc-para-Query) lê o MESMO Aggregate, através do MESMO
//     EventStore, e devolve a View com o note que "Touch" gravou — a prova
//     de ponta a ponta de que as duas rpc, a Query e o UseCase, e a borda
//     HTTP (se houvesse) chamam o MESMO domínio.
const grpcDemoBehaviorTest = `package main

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"domainscript/generated/grpcdemo"
	"domainscript/generated/grpcedge"
	"domainscript/generated/runtime"
)

func dialGRPCDemo(t *testing.T, store runtime.EventStore) (*grpc.ClientConn, func()) {
	t.Helper()
	grpcdemo.Wire(runtime.NewUnitOfWork(store))

	srv := newGRPCServer(store)
	lis := bufconn.Listen(1024 * 1024)
	go func() {
		_ = srv.Serve(lis)
	}()

	dialer := func(context.Context, string) (net.Conn, error) { return lis.Dial() }
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(grpcedge.JSONCodec{})),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient: %v", err)
	}
	return conn, func() {
		conn.Close()
		srv.Stop()
	}
}

func TestGRPCTouchThenGetItemRoundTrip(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	conn, closeAll := dialGRPCDemo(t, store)
	defer closeAll()

	itemID, err := grpcdemo.NewItemId("item-1")
	if err != nil {
		t.Fatalf("NewItemId: %v", err)
	}
	note, err := grpcdemo.NewItemNote("hello from grpc")
	if err != nil {
		t.Fatalf("NewItemNote: %v", err)
	}

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-caller-id", "tester"))
	touchResp := &grpcedge.Empty{}
	if err := conn.Invoke(ctx, "/grpcdemo.ItemService/Touch", grpcdemo.TouchItem{ItemId: itemID, Note: note}, touchResp); err != nil {
		t.Fatalf("Invoke Touch: %v", err)
	}

	getResp := &grpcdemo.ItemView{}
	if err := conn.Invoke(context.Background(), "/grpcdemo.ItemService/GetItem", ItemServiceGetItemRequest{Id: itemID}, getResp); err != nil {
		t.Fatalf("Invoke GetItem: %v", err)
	}
	if string(getResp.Id) != "item-1" {
		t.Fatalf("Id = %q, want item-1", string(getResp.Id))
	}
	if string(getResp.Note) != "hello from grpc" {
		t.Fatalf("Note = %q, want %q", string(getResp.Note), "hello from grpc")
	}
}

func TestGRPCTouchWithoutCallerIsForbidden(t *testing.T) {
	store := runtime.NewMemoryEventStore()
	conn, closeAll := dialGRPCDemo(t, store)
	defer closeAll()

	itemID, err := grpcdemo.NewItemId("item-2")
	if err != nil {
		t.Fatalf("NewItemId: %v", err)
	}
	note, err := grpcdemo.NewItemNote("sem caller")
	if err != nil {
		t.Fatalf("NewItemNote: %v", err)
	}

	touchResp := &grpcedge.Empty{}
	err = conn.Invoke(context.Background(), "/grpcdemo.ItemService/Touch", grpcdemo.TouchItem{ItemId: itemID, Note: note}, touchResp)
	if err == nil {
		t.Fatal("esperava erro (Forbidden — access requires caller.authenticated, sem metadata x-caller-id)")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %v, want PermissionDenied; err: %v", status.Code(err), err)
	}
}
`

// TestGenerateGRPCDemoBehavior prova NFR-15 sobre a borda gRPC: escreve o
// projeto isolado gerado + grpcDemoBehaviorTest em cmd/grpcdemo, e roda `go
// test ./...` de VERDADE sobre ele — gentest.RunTests (não o runGeneratedTests
// mais antigo deste pacote) porque este projeto tem um bloco "require" em
// go.mod (google.golang.org/grpc) e por isso precisa de `go mod tidy` antes
// de compilar (ver a doc de RunTests/needsModTidy, gentest/smoke.go).
func TestGenerateGRPCDemoBehavior(t *testing.T) {
	files := filesToMap(generateGRPCDemoProject(t))
	files[filepath.Join("cmd", "grpcdemo", "main_grpc_behavior_test.go")] = []byte(grpcDemoBehaviorTest)
	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}

// --- 3. Regressão: sem "Interface GRPC", nada muda (REQ-29.2, NFR-12). ---

// TestGenerateWalletProjectHasNoGRPCArtifacts prova que um programa sem
// nenhuma "Interface GRPC" (o wallet real) continua sem NENHUM artefato de
// H1: nenhum arquivo grpcedge/*.go, nenhum proto/*.proto, e go.mod sem
// "google.golang.org/grpc" — mesmo espírito de
// TestGenerateWalletSmokeCompileNoSQLAdapterFiles (sql_adapter_test.go, G1).
// generateWalletProject/filesToMap já existem no pacote codegen_test
// (codegen_test.go, E9.1) — reusados aqui tal qual os demais *_test.go.
func TestGenerateWalletProjectHasNoGRPCArtifacts(t *testing.T) {
	files := generateWalletProject(t)
	for _, f := range files {
		if strings.HasPrefix(f.Path, "grpcedge/") || strings.HasPrefix(f.Path, "proto/") {
			t.Fatalf("NFR-12: wallet não deveria gerar nenhum arquivo grpcedge/*/proto/* (sem Interface GRPC), achei %q", f.Path)
		}
	}
	goMod := grpcDemoFileByPathOrNil(files, "go.mod")
	if goMod == nil {
		t.Fatal("esperava go.mod entre os arquivos gerados do wallet")
	}
	if strings.Contains(string(goMod), "google.golang.org/grpc") {
		t.Fatalf("NFR-12: wallet não deveria ter \"google.golang.org/grpc\" em go.mod (sem Interface GRPC), achei:\n%s", goMod)
	}
}

// grpcDemoFileByPathOrNil é a variante sem t.Fatalf de grpcDemoFileByPath —
// usada quando a ausência do arquivo já é verificada por outro caminho
// (aqui, só para achar go.mod, que Generate sempre produz).
func grpcDemoFileByPathOrNil(files []codegen.File, p string) []byte {
	for _, f := range files {
		if f.Path == p {
			return f.Content
		}
	}
	return nil
}
