package codegen

import (
	"fmt"
	"strings"

	"domainscript/ast"
	"domainscript/symbols"
	"domainscript/types"
)

// grpc_proto.go emite o artefato proto/<service>.proto (H1, REQ-29.1) a
// partir de uma Interface GRPC — texto puro proto3, NUNCA compilado por
// protoc dentro deste pipeline (ver a doc de topo de grpc.go sobre a decisão
// de não depender de protoc, e a de codegen/grpcrt/codec.go.txt sobre o
// servidor de verdade gerado NÃO usar este .proto como wire format — ele é
// documentação/interoperabilidade para quem quiser rodar protoc por conta
// própria). O mapeamento de tipo é melhor esforço, deliberadamente escopado
// ao que um Command (campos), Query (Params/Return) e ValueObject/Enum
// aninhados de fato precisam — qualquer forma fora disso (List/Set/Map,
// Aggregate/Event/Command usado como campo bruto fora de "ref") é erro de
// geração claro (REQ-14.4), mesma disciplina de emitHTTPParseParam (http.go)
// para path/query params: suporte mínimo definido por esta task, mais casos
// entram quando surgir necessidade real.

// protoField é um campo já resolvido de uma message proto3 — a POSIÇÃO
// (número do campo) é implícita (índice+1 na ordem declarada), nunca
// escolhida por outro critério, para determinismo (NFR-13).
type protoField struct {
	name      string
	protoType string
}

type protoMessageDef struct {
	name   string
	fields []protoField
}

type protoEnumDef struct {
	name    string
	members []string
}

// protoRegistry acumula, em ORDEM DE 1ª DESCOBERTA (determinismo, NFR-13),
// toda message/enum referenciada por um campo de Command/Query (ValueObject
// composto, Enum, ou uma View/Aggregate/etc. usada como campo aninhado) —
// sem duplicar uma mesma message/enum referenciada mais de uma vez (ex. duas
// rpcs cujos Commands compartilham o mesmo ValueObject).
type protoRegistry struct {
	messages  map[string]*protoMessageDef
	enums     map[string]*protoEnumDef
	order     []string // nomes, message OU enum, na ordem de 1ª descoberta
	isEnum    map[string]bool
	needEmpty bool // true assim que alguma rpc-para-UseCase é registrada (ver Empty, emitGRPCUseCaseHandlerBody/grpcrt/empty.go.txt)
}

func newProtoRegistry() *protoRegistry {
	return &protoRegistry{
		messages: make(map[string]*protoMessageDef),
		enums:    make(map[string]*protoEnumDef),
		isEnum:   make(map[string]bool),
	}
}

// protoScalar mapeia types.Primitive.Name para o scalar proto3 correspondente
// (melhor esforço, documentação — REQ-29.1): decimal/datetime/duration/rate
// viram "string" — o MESMO formato que a codificação JSON real do runtime já
// usa para eles (ex. Decimal.MarshalJSON, codegen/rtsrc/decimal.go.txt) —
// para não sugerir, no .proto, uma precisão binária que o servidor de fato
// gerado (JSON, não protobuf — ver a doc do arquivo) nunca teria.
func protoScalar(name string) (string, bool) {
	switch name {
	case "integer", "size":
		return "int64", true
	case "boolean":
		return "bool", true
	case "bytes":
		return "bytes", true
	case "string", "decimal", "datetime", "duration", "rate":
		return "string", true
	default:
		return "", false
	}
}

// fieldType traduz t para um tipo proto3: um scalar direto (Primitive), ou o
// nome de uma message/enum registrada (ValueObject/Enum/Shape), registrando-a
// recursivamente na 1ª referência (ver a doc de protoRegistry). Qualquer
// outra forma (List/Set/Map, tipo não resolvido) é erro de geração claro.
func (r *protoRegistry) fieldType(t types.Type) (string, error) {
	if types.IsError(t) {
		return "", fmt.Errorf("tipo não resolvido (bug de geração — REQ-9/13 já deveriam ter barrado isso)")
	}
	switch x := t.(type) {
	case *types.Primitive:
		s, ok := protoScalar(x.Name)
		if !ok {
			return "", fmt.Errorf("primitivo %s sem mapeamento proto3 (H1 cobre string/integer/boolean/decimal/datetime/bytes/duration/size/rate)", x.Name)
		}
		return s, nil
	case *types.VOType:
		if x.Base != nil {
			return r.fieldType(x.Base)
		}
		return r.voMessage(x)
	case *types.EnumType:
		return r.enumDef(x)
	case *types.ShapeType:
		return r.shapeMessage(x)
	default:
		return "", fmt.Errorf("tipo %s não suportado no artefato .proto (H1 cobre primitivo/ValueObject/Enum e Aggregate/View/Event/Command aninhados; List/Set/Map ficam para quando surgir necessidade real)", t.String())
	}
}

// voMessage registra (se ainda não registrada) a message de um ValueObject
// COMPOSTO (Base nil — a forma wrapper já é achatada ao tipo base por
// fieldType acima, nunca chega aqui) — um campo por VOType.Fields, na ordem
// declarada.
func (r *protoRegistry) voMessage(v *types.VOType) (string, error) {
	if _, ok := r.messages[v.Name]; ok {
		return v.Name, nil
	}
	def := &protoMessageDef{name: v.Name}
	r.messages[v.Name] = def
	r.order = append(r.order, v.Name)

	fields := make([]protoField, 0, len(v.Fields))
	for _, f := range v.Fields {
		pt, err := r.fieldType(f.Type)
		if err != nil {
			return "", fmt.Errorf("ValueObject %s: campo %s: %w", v.Name, f.Name, err)
		}
		fields = append(fields, protoField{name: toSnakeCase(f.Name), protoType: pt})
	}
	def.fields = fields
	return v.Name, nil
}

// shapeMessage registra (se ainda não registrada) a message de um
// *types.ShapeType (Aggregate/Event/Command/View/Projection) usado como
// campo — hoje só alcançada pelo retorno de uma Query (sempre uma View, ver
// queryResponseMessage) ou por um campo aninhado que referencie uma dessas
// formas; um campo por ShapeType.Fields, na ordem declarada.
func (r *protoRegistry) shapeMessage(s *types.ShapeType) (string, error) {
	if _, ok := r.messages[s.Name]; ok {
		return s.Name, nil
	}
	def := &protoMessageDef{name: s.Name}
	r.messages[s.Name] = def
	r.order = append(r.order, s.Name)

	fields := make([]protoField, 0, len(s.Fields))
	for _, f := range s.Fields {
		pt, err := r.fieldType(f.Type)
		if err != nil {
			return "", fmt.Errorf("%s %s: campo %s: %w", s.Kind, s.Name, f.Name, err)
		}
		fields = append(fields, protoField{name: toSnakeCase(f.Name), protoType: pt})
	}
	def.fields = fields
	return s.Name, nil
}

// enumDef registra (se ainda não registrada) o enum proto3 de um EnumType —
// ver writeDefs sobre a convenção de nome dos values (prefixo
// <ENUM>_, "_UNSPECIFIED = 0" obrigatório por proto3).
func (r *protoRegistry) enumDef(en *types.EnumType) (string, error) {
	if _, ok := r.enums[en.Name]; ok {
		return en.Name, nil
	}
	r.enums[en.Name] = &protoEnumDef{name: en.Name, members: append([]string(nil), en.Members...)}
	r.order = append(r.order, en.Name)
	r.isEnum[en.Name] = true
	return en.Name, nil
}

// commandMessage registra (se ainda não registrada) a message do Command
// alvo de uma rpc-para-UseCase — um campo por CommandDecl.Fields, na ordem
// declarada; campo "ref Aggregate" resolve ao tipo do "id" do state do
// Aggregate referenciado (mesma regra de commandFieldParseType, http.go,
// reusada aqui tal qual).
func (r *protoRegistry) commandMessage(cmd *ast.CommandDecl, model *types.Model, tab *symbols.SymbolTable, module string) (string, error) {
	if _, ok := r.messages[cmd.Name]; ok {
		return cmd.Name, nil
	}
	def := &protoMessageDef{name: cmd.Name}
	r.messages[cmd.Name] = def
	r.order = append(r.order, cmd.Name)

	fields := make([]protoField, 0, len(cmd.Fields))
	for _, f := range cmd.Fields {
		if f == nil {
			continue
		}
		t, err := commandFieldParseType(f, model, tab, module)
		if err != nil {
			return "", fmt.Errorf("Command %s: campo %s: %w", cmd.Name, f.Name, err)
		}
		pt, err := r.fieldType(t)
		if err != nil {
			return "", fmt.Errorf("Command %s: campo %s: %w", cmd.Name, f.Name, err)
		}
		fields = append(fields, protoField{name: toSnakeCase(f.Name), protoType: pt})
	}
	def.fields = fields
	return cmd.Name, nil
}

// queryRequestMessage registra a message de request de uma rpc-para-Query —
// um campo por QueryDecl.Params, na ordem declarada; name é o MESMO nome do
// struct Go gerado por buildGRPCQueryRequestSpec (grpc.go), para as duas
// formas ficarem consistentes lado a lado (mesmo par nome+shape).
func (r *protoRegistry) queryRequestMessage(name string, q *ast.QueryDecl, model *types.Model, tab *symbols.SymbolTable, module string) (string, error) {
	if _, ok := r.messages[name]; ok {
		return name, nil
	}
	def := &protoMessageDef{name: name}
	r.messages[name] = def
	r.order = append(r.order, name)

	fields := make([]protoField, 0, len(q.Params))
	for _, f := range q.Params {
		if f == nil {
			continue
		}
		t, err := commandFieldParseType(f, model, tab, module)
		if err != nil {
			return "", fmt.Errorf("Query %s: parâmetro %s: %w", q.Name, f.Name, err)
		}
		pt, err := r.fieldType(t)
		if err != nil {
			return "", fmt.Errorf("Query %s: parâmetro %s: %w", q.Name, f.Name, err)
		}
		fields = append(fields, protoField{name: toSnakeCase(f.Name), protoType: pt})
	}
	def.fields = fields
	return name, nil
}

// queryResponseMessage registra a message de resposta de uma rpc-para-Query
// — o retorno da Query precisa resolver a uma View (o único caso real hoje,
// mirror do JSON que a borda HTTP devolve — emitQueryRoute, http.go);
// qualquer outra forma (List<T>, primitivo nu, etc.) é erro de geração claro
// — suporte mínimo definido por esta task.
func (r *protoRegistry) queryResponseMessage(q *ast.QueryDecl, model *types.Model, tab *symbols.SymbolTable, module string) (string, error) {
	t := model.TypeOfRef(module, q.Return)
	shape, ok := t.(*types.ShapeType)
	if !ok || shape.Kind != symbols.KindView {
		return "", fmt.Errorf("Query %s: retorno %s não resolve a uma View (H1 só suporta resposta gRPC de Query como View — List/Set/Map/primitivo ficam para quando surgir necessidade real)", q.Name, t.String())
	}
	return r.shapeMessage(shape)
}

// writeDefs escreve, em ORDEM DE DESCOBERTA (determinismo), cada message/enum
// acumulada — um enum sempre ganha "<NOME>_UNSPECIFIED = 0" primeiro (regra
// proto3: o primeiro value tem que ser 0), seguido dos membros declarados a
// partir de 1.
func (r *protoRegistry) writeDefs(b *strings.Builder) {
	for _, name := range r.order {
		if r.isEnum[name] {
			en := r.enums[name]
			upper := strings.ToUpper(toSnakeCase(en.name))
			fmt.Fprintf(b, "enum %s {\n", en.name)
			fmt.Fprintf(b, "  %s_UNSPECIFIED = 0;\n", upper)
			for i, m := range en.members {
				fmt.Fprintf(b, "  %s_%s = %d;\n", upper, strings.ToUpper(toSnakeCase(m)), i+1)
			}
			b.WriteString("}\n\n")
			continue
		}
		msg := r.messages[name]
		fmt.Fprintf(b, "message %s {\n", msg.name)
		for i, f := range msg.fields {
			fmt.Fprintf(b, "  %s %s = %d;\n", f.protoType, f.name, i+1)
		}
		b.WriteString("}\n\n")
	}
}

// protoFileDocComment é o cabeçalho explicativo escrito em TODO
// proto/<service>.proto gerado (H1) — a mesma ressalva do tradeoff de codec
// documentada em codegen/grpcrt/codec.go.txt, resumida para quem só olha o
// artefato .proto sem ler o compilador.
const protoFileDocComment = `// Gerado por dsc gen a partir de "Interface GRPC" (spec §10) — sobrescrito a
// cada geração, não editar à mão.
//
// AVISO (REQ-29.2, exceção documentada à NFR-12): este arquivo é um artefato
// TEXTUAL — documentação/interoperabilidade para quem queira rodar protoc
// por conta própria. O servidor gRPC de fato gerado por este compilador NÃO
// usa protoc nem tipos *.pb.go: ele fala gRPC-sobre-HTTP/2 real, mas
// serializa cada mensagem como JSON (não o wire format binário protobuf
// usual) via um encoding.Codec customizado. Um cliente gerado por protoc a
// partir DESTE arquivo, sem a mesma troca de codec, NÃO consegue conversar
// com o servidor gerado — ver codegen/grpcrt/codec.go.txt (o compilador) para
// o tradeoff completo.

`

// emitGRPCProtoFile monta o texto de proto/<groupDirName>.proto para iface (a
// Interface GRPC do grupo — ver a doc do arquivo): "package <groupDirName>"
// (o MESMO nome que emitGRPCServiceRegistration usa para compor
// ServiceName/FullMethod, grpc.go — para que o .proto e o servidor de fato
// gerado concordem no nome completo do serviço), um "service { rpc ... }" por
// GrpcService (na ordem declarada), e as message/enum descobertas durante a
// tradução, em ordem de descoberta.
func emitGRPCProtoFile(groupDirName string, iface *ast.InterfaceDecl, buckets map[string]moduleBucket, modules []string, model *types.Model, tab *symbols.SymbolTable) ([]byte, error) {
	reg := newProtoRegistry()

	type svcRPCEntry struct {
		rpcName, reqType, respType string
	}
	byService := make(map[string][]svcRPCEntry, len(iface.Services))
	var svcOrder []string

	for _, svc := range iface.Services {
		if len(svc.RPCs) == 0 {
			continue
		}
		svcOrder = append(svcOrder, svc.Name)
		for _, rpc := range svc.RPCs {
			target, err := resolveRouteTarget(buckets, modules, rpc.Target)
			if err != nil {
				return nil, fmt.Errorf("service %s: rpc %s -> %s: %w", svc.Name, rpc.Name, rpc.Target, err)
			}

			if target.usecase != nil {
				cmdDecl, cmdModule, ok := findCommandInBuckets(buckets, modules, target.usecase.Handles)
				if !ok {
					return nil, fmt.Errorf("service %s: rpc %s: UseCase %s: Command %q (handles) não encontrado nos módulos do grupo", svc.Name, rpc.Name, target.usecase.Name, target.usecase.Handles)
				}
				reqName, err := reg.commandMessage(cmdDecl, model, tab, cmdModule)
				if err != nil {
					return nil, fmt.Errorf("service %s: rpc %s: %w", svc.Name, rpc.Name, err)
				}
				reg.needEmpty = true
				byService[svc.Name] = append(byService[svc.Name], svcRPCEntry{rpcName: rpc.Name, reqType: reqName, respType: "Empty"})
				continue
			}

			reqName, err := reg.queryRequestMessage(fmt.Sprintf("%s%sRequest", svc.Name, rpc.Name), target.query, model, tab, target.qModule)
			if err != nil {
				return nil, fmt.Errorf("service %s: rpc %s: %w", svc.Name, rpc.Name, err)
			}
			respName, err := reg.queryResponseMessage(target.query, model, tab, target.qModule)
			if err != nil {
				return nil, fmt.Errorf("service %s: rpc %s: %w", svc.Name, rpc.Name, err)
			}
			byService[svc.Name] = append(byService[svc.Name], svcRPCEntry{rpcName: rpc.Name, reqType: reqName, respType: respName})
		}
	}

	var b strings.Builder
	b.WriteString("syntax = \"proto3\";\n\n")
	fmt.Fprintf(&b, "package %s;\n\n", groupDirName)
	b.WriteString(protoFileDocComment)

	for _, svcName := range svcOrder {
		fmt.Fprintf(&b, "service %s {\n", svcName)
		for _, r := range byService[svcName] {
			fmt.Fprintf(&b, "  rpc %s (%s) returns (%s);\n", r.rpcName, r.reqType, r.respType)
		}
		b.WriteString("}\n\n")
	}

	if reg.needEmpty {
		b.WriteString("message Empty {}\n\n")
	}
	reg.writeDefs(&b)

	return []byte(b.String()), nil
}
