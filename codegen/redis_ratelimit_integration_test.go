//go:build integration

package codegen

import (
	"os"
	"path"
	"testing"

	"domainscript/codegen/gentest"
	"domainscript/codegen/redisrt"
	"domainscript/codegen/rtsrc"
)

// redis_ratelimit_integration_test.go prova J4.2.c (REQ-44.2/44.3, NFR-22/
// 24, §design infra-providers 3.4): os três scripts Lua
// (token_bucket/sliding_window/fixed_window) admitindo/negando DE VERDADE
// contra um Redis vivo — atrás da build tag "integration" (nunca entra em
// "go test ./..." default) — e guardado por env: sem REDIS_URL definida,
// pula (t.Skip), nunca falha (REQ-48.3/NFR-24). Mesmo padrão de
// sql_postgres_integration_test.go/channel_rabbitmq_integration_test.go.
// Rodar de propósito: "REDIS_URL=redis://localhost:6379/0 go test
// -tags=integration ./codegen/ -run TestRedisRateLimitIntegration".
const redisRateLimitIntegrationTest = `package redisruntime

import (
	"context"
	"os"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// TestFixedWindowAdmitsUpToLimitThenDenies prova REQ-44.3 (mesma semântica
// do in-memory) de verdade: fixed_window admite exatamente "limit"
// requisições dentro da janela, nega a próxima, e NUNCA incrementa o
// contador na negação (a mesma escolha de fixedWindowLimiter.Allow,
// rtsrc/ratelimit.go.txt — replicada em Lua).
func TestFixedWindowAdmitsUpToLimitThenDenies(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL não definida — pulando teste de integração RateLimit Redis (REQ-48.3/NFR-24)")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("redis.ParseURL: %v", err)
	}
	client := redis.NewClient(opts)
	defer client.Close()

	key := "integration-fixed-" + time.Now().Format("20060102150405.000000000")
	l := newRedisLimiter(client, key, "fixed_window", 3, time.Minute, 0)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		allowed, result, err := l.Allow(ctx, "caller-1")
		if err != nil {
			t.Fatalf("Allow (%d): %v", i, err)
		}
		if !allowed {
			t.Fatalf("Allow (%d): esperava true (dentro do limite 3), veio false — result=%+v", i, result)
		}
	}

	allowed, result, err := l.Allow(ctx, "caller-1")
	if err != nil {
		t.Fatalf("Allow (4ª): %v", err)
	}
	if allowed {
		t.Fatal("Allow (4ª): esperava false (limite 3 esgotado), veio true")
	}
	if result.Remaining != 0 {
		t.Fatalf("Remaining na negação = %d, want 0", result.Remaining)
	}
	if result.RetryAfter <= 0 {
		t.Fatalf("RetryAfter na negação = %v, want > 0", result.RetryAfter)
	}
}

// TestTokenBucketAdmitsBurstThenDenies prova token_bucket de verdade: admite
// até "burst" requisições instantâneas, nega a próxima (sem tempo pra
// refill nenhum entre elas).
func TestTokenBucketAdmitsBurstThenDenies(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL não definida — pulando teste de integração RateLimit Redis (REQ-48.3/NFR-24)")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("redis.ParseURL: %v", err)
	}
	client := redis.NewClient(opts)
	defer client.Close()

	key := "integration-bucket-" + time.Now().Format("20060102150405.000000000")
	l := newRedisLimiter(client, key, "token_bucket", 60, time.Minute, 2)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		allowed, _, err := l.Allow(ctx, "caller-1")
		if err != nil {
			t.Fatalf("Allow (%d): %v", i, err)
		}
		if !allowed {
			t.Fatalf("Allow (%d): esperava true (dentro do burst 2), veio false", i)
		}
	}
	allowed, result, err := l.Allow(ctx, "caller-1")
	if err != nil {
		t.Fatalf("Allow (3ª): %v", err)
	}
	if allowed {
		t.Fatal("Allow (3ª): esperava false (burst 2 esgotado, sem tempo pra refill), veio true")
	}
	if result.RetryAfter <= 0 {
		t.Fatalf("RetryAfter na negação = %v, want > 0", result.RetryAfter)
	}
}

// TestSlidingWindowAdmitsUpToLimitThenDenies prova sliding_window de
// verdade: admite até "limit" requisições dentro da janela, nega a próxima.
func TestSlidingWindowAdmitsUpToLimitThenDenies(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL não definida — pulando teste de integração RateLimit Redis (REQ-48.3/NFR-24)")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("redis.ParseURL: %v", err)
	}
	client := redis.NewClient(opts)
	defer client.Close()

	key := "integration-sliding-" + time.Now().Format("20060102150405.000000000")
	l := newRedisLimiter(client, key, "sliding_window", 3, time.Minute, 0)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		allowed, _, err := l.Allow(ctx, "caller-1")
		if err != nil {
			t.Fatalf("Allow (%d): %v", i, err)
		}
		if !allowed {
			t.Fatalf("Allow (%d): esperava true (dentro do limite 3), veio false", i)
		}
	}
	allowed, _, err := l.Allow(ctx, "caller-1")
	if err != nil {
		t.Fatalf("Allow (4ª): %v", err)
	}
	if allowed {
		t.Fatal("Allow (4ª): esperava false (limite 3 esgotado), veio true")
	}
}

// TestRedisLimiterIsolatesDistinctKeys prova que duas chaves distintas (dois
// callers) têm cotas INDEPENDENTES sob o MESMO namespace — a cota de uma
// não consome a da outra.
func TestRedisLimiterIsolatesDistinctKeys(t *testing.T) {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL não definida — pulando teste de integração RateLimit Redis (REQ-48.3/NFR-24)")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("redis.ParseURL: %v", err)
	}
	client := redis.NewClient(opts)
	defer client.Close()

	ns := "integration-isolation-" + time.Now().Format("20060102150405.000000000")
	l := newRedisLimiter(client, ns, "fixed_window", 1, time.Minute, 0)
	ctx := context.Background()

	allowedA1, _, _ := l.Allow(ctx, "caller-A")
	if !allowedA1 {
		t.Fatal("caller-A (1ª): esperava true")
	}
	allowedB1, _, _ := l.Allow(ctx, "caller-B")
	if !allowedB1 {
		t.Fatal("caller-B (1ª): esperava true (cota independente de caller-A)")
	}
	allowedA2, _, _ := l.Allow(ctx, "caller-A")
	if allowedA2 {
		t.Fatal("caller-A (2ª): esperava false (limite 1 já esgotado)")
	}
}
`

// TestRedisRateLimitIntegration roda redisRateLimitIntegrationTest de
// verdade sobre um projeto Go mínimo (runtime + redisruntime vendorados)
// contra um Redis real — pula sem REDIS_URL (REQ-48.3/NFR-24).
func TestRedisRateLimitIntegration(t *testing.T) {
	if os.Getenv("REDIS_URL") == "" {
		t.Skip("REDIS_URL não definida — pulando teste de integração RateLimit Redis (REQ-48.3/NFR-24)")
	}

	files := make(map[string][]byte)
	files["go.mod"] = EmitGoMod(Options{ModulePath: "domainscript/generated"}, "", nil, false, false, []providerDep{rateLimitProviders["redis"]})

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

	files[path.Join("redisruntime", "ratelimit_integration_test.go")] = []byte(redisRateLimitIntegrationTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}
