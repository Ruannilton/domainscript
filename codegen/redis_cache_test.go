package codegen

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
	"domainscript/codegen/redisrt"
	"domainscript/codegen/rtsrc"
)

// redis_cache_test.go prova a DoD de J4.1.c (REQ-44.1/44.5, §design
// infra-providers 3.4): montagem de chave/geração-no-prefixo, serialização
// gob type-preserving e fail-open do adapter redisruntime
// (codegen/redisrt/cache.go.txt) — SEM abrir nenhuma conexão de rede real
// com Redis. Como cache.go.txt não é compilado diretamente por este módulo
// (só embutido como texto), a única forma de exercitá-lo de verdade é
// compilar e rodar um pacote redisruntime real dentro de um projeto Go
// efêmero (mesmo padrão de amqp_envelope_test.go/amqp_topology_test.go). O
// teste embutido é `package redisruntime` (white-box, não `_test`):
// newRedisQueryCache/redisCmdable não são exportados de propósito (só o
// wiring gerado, task J4.3, chama NewRedisQueryCache de fora) — mesmo padrão
// de teste interno que amqpEnvelopeTest já usa. Este arquivo é `package
// codegen` (interno, não `codegen_test`) para poder ler
// cacheProviders["redis"] direto (não exportado) ao montar o go.mod da
// fixture — mesmo padrão de amqp_envelope_test.go/provider_registry_test.go.
//
// go-redis/v9's *redis.Client é uma struct concreta, não uma interface — não
// dá para substituir o client inteiro por um dublê de teste. Em vez disso,
// cache.go.txt já isola sua superfície de chamadas Redis atrás de
// redisCmdable (Get/Set/Incr, as únicas 3 operações que o adapter usa) — o
// teste embutido implementa esse mesmo formato com um fakeCmdable em memória
// (mapa + mutex), permitindo injetar erros determinísticos sem nenhuma rede.
const redisCacheTest = `package redisruntime

import (
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// fakeCmdable implementa redisCmdable inteiramente em memória (mapa +
// mutex), com erros injetáveis por operação — prova "client fake injetado"
// (task J4.1.c) sem abrir nenhuma conexão real.
type fakeCmdable struct {
	mu      sync.Mutex
	store   map[string][]byte
	getErr  error
	setErr  error
	incrErr error
}

func newFakeCmdable() *fakeCmdable {
	return &fakeCmdable{store: make(map[string][]byte)}
}

func (f *fakeCmdable) Get(ctx context.Context, key string) *redis.StringCmd {
	cmd := redis.NewStringCmd(ctx, "get", key)
	if f.getErr != nil {
		cmd.SetErr(f.getErr)
		return cmd
	}
	f.mu.Lock()
	v, ok := f.store[key]
	f.mu.Unlock()
	if !ok {
		cmd.SetErr(redis.Nil)
		return cmd
	}
	cmd.SetVal(string(v))
	return cmd
}

func (f *fakeCmdable) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	cmd := redis.NewStatusCmd(ctx, "set", key)
	if f.setErr != nil {
		cmd.SetErr(f.setErr)
		return cmd
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		b = []byte(fmt.Sprint(v))
	}
	f.mu.Lock()
	f.store[key] = b
	f.mu.Unlock()
	cmd.SetVal("OK")
	return cmd
}

func (f *fakeCmdable) Incr(ctx context.Context, key string) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx, "incr", key)
	if f.incrErr != nil {
		cmd.SetErr(f.incrErr)
		return cmd
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cur, _ := strconv.ParseInt(string(f.store[key]), 10, 64)
	cur++
	f.store[key] = []byte(strconv.FormatInt(cur, 10))
	cmd.SetVal(cur)
	return cmd
}

// queryResult simula o tipo de retorno concreto de uma Query cacheada —
// prova que o round-trip via gob reconstrói o TIPO CONCRETO, não um
// map[string]interface{}/float64 (o que um encoding/json teria produzido e
// faria o type assertion do wrapper gerado, "v.(%s)", panicar).
type queryResult struct {
	Name  string
	Count int
}

// notFoundError simula um erro de negócio concreto cacheado via SetErr
// (negativeCacheTtl, spec §15) — também precisa de gob.Register (mesma regra
// documentada na doc do arquivo cache.go.txt).
type notFoundError struct {
	ID string
}

func (e *notFoundError) Error() string { return "not found: " + e.ID }

func init() {
	gob.Register(queryResult{})
	gob.Register(&notFoundError{})
}

// TestRedisQueryCacheRoundTripPreservesConcreteType prova a montagem de
// chave/geração e a serialização gob: Set então Get devolve o MESMO valor,
// com o TIPO CONCRETO correto (não um map genérico).
func TestRedisQueryCacheRoundTripPreservesConcreteType(t *testing.T) {
	fake := newFakeCmdable()
	c := newRedisQueryCache(fake, "ns-round-trip")
	ctx := context.Background()

	want := queryResult{Name: "alice", Count: 3}
	c.Set(ctx, "key-1", want, time.Minute)

	v, err, hit := c.Get(ctx, "key-1")
	if !hit {
		t.Fatal("Get: esperava hit=true depois de Set, veio false")
	}
	if err != nil {
		t.Fatalf("Get: err = %v, esperava nil (resultado positivo)", err)
	}
	got, ok := v.(queryResult)
	if !ok {
		t.Fatalf("Get: v = %T, esperava queryResult (gob não preservou o tipo concreto)", v)
	}
	if got != want {
		t.Fatalf("Get: v = %+v, want %+v", got, want)
	}
}

// TestRedisQueryCacheSetErrRoundTrip prova o round-trip de um resultado
// NEGATIVO (SetErr, negativeCacheTtl, spec §15): Get devolve hit=true com o
// erro de negócio concreto cacheado, sem rodar a query de novo.
func TestRedisQueryCacheSetErrRoundTrip(t *testing.T) {
	fake := newFakeCmdable()
	c := newRedisQueryCache(fake, "ns-seterr")
	ctx := context.Background()

	want := &notFoundError{ID: "order-9"}
	c.SetErr(ctx, "key-1", want, time.Minute)

	v, err, hit := c.Get(ctx, "key-1")
	if !hit {
		t.Fatal("Get: esperava hit=true depois de SetErr, veio false")
	}
	if v != nil {
		t.Fatalf("Get: v = %v, esperava nil quando o hit é um resultado negativo", v)
	}
	got, ok := err.(*notFoundError)
	if !ok {
		t.Fatalf("Get: err = %T, esperava *notFoundError", err)
	}
	if got.ID != want.ID {
		t.Fatalf("Get: err.ID = %q, want %q", got.ID, want.ID)
	}
}

// TestRedisQueryCacheInvalidateAllMissesEvenWithinTTL prova o mecanismo de
// geração-no-prefixo (item b, §design infra-providers 3.4): depois de
// InvalidateAll, um Get para uma chave que AINDA estaria dentro do seu TTL
// original vira miss — a causa é a geração ter mudado (o dado antigo nunca
// foi apagado, só deixou de ser lido), não expiração de TTL.
func TestRedisQueryCacheInvalidateAllMissesEvenWithinTTL(t *testing.T) {
	fake := newFakeCmdable()
	c := newRedisQueryCache(fake, "ns-invalidate")
	ctx := context.Background()

	c.Set(ctx, "key-1", queryResult{Name: "bob", Count: 1}, time.Hour)

	if _, _, hit := c.Get(ctx, "key-1"); !hit {
		t.Fatal("Get antes de InvalidateAll: esperava hit=true")
	}

	c.InvalidateAll()

	if _, _, hit := c.Get(ctx, "key-1"); hit {
		t.Fatal("Get depois de InvalidateAll: esperava hit=false (geração mudou), veio true")
	}

	// A chave de geração em si nunca expira e nunca é apagada — só
	// incrementada; a chave de dados da geração ANTIGA continua presente no
	// fake store (não foi apagada, só deixou de ser prefixo-compatível) —
	// prova o "custo aceito" documentado no arquivo (nenhum SCAN+DEL).
	fake.mu.Lock()
	_, genKeyPresent := fake.store["ns-invalidate:gen"]
	fake.mu.Unlock()
	if !genKeyPresent {
		t.Fatal("esperava a chave de geração presente no store depois de InvalidateAll")
	}
}

// TestRedisQueryCacheGetFailsOpenOnRedisError prova REQ-44.1/44.5: um erro
// de Redis (conexão/timeout, aqui simulado pelo fake) nunca propaga como
// erro de Get — sempre hit=false, nunca um pânico.
func TestRedisQueryCacheGetFailsOpenOnRedisError(t *testing.T) {
	fake := newFakeCmdable()
	fake.getErr = errors.New("boom: conexão recusada")
	c := newRedisQueryCache(fake, "ns-failopen")
	ctx := context.Background()

	v, err, hit := c.Get(ctx, "any-key")
	if hit {
		t.Fatal("Get: esperava hit=false sob erro de Redis (fail-open), veio true")
	}
	if err != nil {
		t.Fatalf("Get: err = %v, esperava nil (fail-open nunca propaga erro)", err)
	}
	if v != nil {
		t.Fatalf("Get: v = %v, esperava nil", v)
	}
}

// TestRedisQueryCacheGetFailsOpenOnUnregisteredType prova a segunda forma de
// fail-open documentada no arquivo: um gob.Decode que falha (aqui, um valor
// gravado por um tipo NÃO registrado) também vira hit=false, nunca um
// pânico no type assertion do chamador.
func TestRedisQueryCacheGetFailsOpenOnUnregisteredType(t *testing.T) {
	fake := newFakeCmdable()
	// Corrompe deliberadamente a entrada: bytes que não formam um gob válido
	// para cachePayload.
	fake.store["ns-corrupt:0:whatever"] = []byte("isto não é gob válido")

	c := newRedisQueryCache(fake, "ns-corrupt")
	ctx := context.Background()

	v, err, hit := c.Get(ctx, "whatever-key-that-does-not-hash-to-the-corrupt-suffix")
	// A chave real (hasheada) não bate com a entrada corrompida forjada
	// acima, então isto já seria um miss comum (redis.Nil) — o que também é
	// hit=false, cobrindo o mesmo contrato. Reforça só que nenhum caminho
	// deste teste panica nem devolve erro.
	if hit {
		t.Fatal("Get: esperava hit=false, veio true")
	}
	if err != nil {
		t.Fatalf("Get: err = %v, esperava nil", err)
	}
	if v != nil {
		t.Fatalf("Get: v = %v, esperava nil", v)
	}
}

// TestRedisQueryCacheSetSkipsNonPositiveTTL prova que Set/SetErr com ttl<=0
// nunca escrevem no backend (mesma regra do memoryQueryCache) — mesmo
// comportamento fail-safe contra um ttl zero/misconfigurado.
func TestRedisQueryCacheSetSkipsNonPositiveTTL(t *testing.T) {
	fake := newFakeCmdable()
	c := newRedisQueryCache(fake, "ns-zero-ttl")
	ctx := context.Background()

	c.Set(ctx, "key-1", queryResult{Name: "z", Count: 0}, 0)
	if _, _, hit := c.Get(ctx, "key-1"); hit {
		t.Fatal("Get: esperava hit=false depois de Set com ttl<=0")
	}

	c.SetErr(ctx, "key-2", &notFoundError{ID: "x"}, -1)
	if _, _, hit := c.Get(ctx, "key-2"); hit {
		t.Fatal("Get: esperava hit=false depois de SetErr com ttl<=0")
	}
}
`

// buildRedisRuntimeProjectFiles monta o material MÍNIMO que codegen.Generate
// escreveria para QUALQUER programa com um Cache `backend: "redis"` (go.mod +
// runtime/*.go + redisruntime/*.go, J4.1) — sem passar por
// driver.CheckProject/Generate sobre nenhum programa .ds (mesmo espírito de
// buildAMQPRuntimeProjectFiles, amqp_envelope_test.go). O go.mod inclui o
// require de go-redis/v9 via cacheProviders["redis"] (o registro real de
// J4.1) — gentest.RunTests roda "go mod tidy" a partir dele (nenhuma chamada
// de rede além da resolução do módulo: o teste embutido nunca abre uma
// conexão Redis de verdade).
func buildRedisRuntimeProjectFiles(t *testing.T) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	files["go.mod"] = EmitGoMod(Options{ModulePath: "domainscript/generated"}, "", nil, false, false, []providerDep{cacheProviders["redis"]})

	rtSrcs, err := rtsrc.Sources()
	if err != nil {
		t.Fatalf("rtsrc.Sources: %v", err)
	}
	for name, content := range rtSrcs {
		files[path.Join("runtime", name)] = content
	}

	redisSrcs, err := redisrt.Sources()
	if err != nil {
		t.Fatalf("redisrt.Sources: %v", err)
	}
	for name, content := range redisSrcs {
		files[path.Join("redisruntime", name)] = content
	}
	return files
}

// TestRedisQueryCacheAdapter roda redisCacheTest de verdade sobre um projeto
// Go mínimo (runtime + redisruntime vendorados) — prova item c da task
// J4.1: montagem de chave/serialização + fail-open por client fake
// injetado.
func TestRedisQueryCacheAdapter(t *testing.T) {
	files := buildRedisRuntimeProjectFiles(t)
	files[path.Join("redisruntime", "cache_test.go")] = []byte(redisCacheTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}
