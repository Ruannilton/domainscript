// Package grpcrt embute o código-fonte do pacote de borda gRPC opt-in (H1,
// REQ-29, §design codegen 3.12): mesmo padrão de codegen/sqlrt (arquivos
// .go.txt via //go:embed, para não compilar junto do compilador), mas para a
// dependência google.golang.org/grpc em vez de database/sql — é o único
// lugar do gerado que importa google.golang.org/grpc diretamente para peças
// que NÃO variam por programa (o codec JSON, o tipo de resposta vazia de um
// UseCase, a extração de caller/idempotency-key da metadata de entrada, e o
// mapeamento de runtime.BusinessError para um status gRPC — ver a doc de cada
// .go.txt), mantendo o núcleo (runtime/, codegen/rtsrc) sem nenhuma
// dependência externa (NFR-12). O pacote gerado a partir daqui chama-se
// "grpcedge" (ver a doc de cada .go.txt) — nome deliberadamente distinto de
// "runtime" (o pacote core) e de "sqlruntime" (o adapter SQL, G1), para deixar
// o import claro em quem consome. As peças que VARIAM por programa (o
// grpc.ServiceDesc e os handlers wired a UseCases/Queries específicos) NÃO
// moram aqui — são emitidas por codegen/grpc.go, direto em
// cmd/<service>/main.go, ao lado de newMux (mesma separação de
// vendorado-verbatim vs. emissor-por-programa que sqlrt/decl_*.go já
// estabelece).
package grpcrt
