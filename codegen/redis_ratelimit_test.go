package codegen

import (
	"path"
	"testing"

	"domainscript/codegen/gentest"
	"domainscript/codegen/redisrt"
	"domainscript/codegen/rtsrc"
)

// redis_ratelimit_test.go prova a DoD de J4.2.c (REQ-44.2/44.3/44.5, §design
// infra-providers 3.4): montagem de chave/script e o fallback local do
// adapter redisruntime (codegen/redisrt/ratelimit.go.txt) — SEM abrir
// nenhuma conexão de rede real com Redis. Mesmo padrão de
// redis_cache_test.go (J4.1.c): `package redisruntime` white-box, embutido
// via gentest.WriteFiles/RunTests sobre um projeto Go efêmero real.
//
// Provar a CORREÇÃO dos três scripts Lua em si (token_bucket/sliding_window/
// fixed_window de verdade admitindo/negando conforme a cota) exige um Redis
// de verdade rodando o script — isso é o teste de integração
// (`redis_ratelimit_integration_test.go`, `//go:build integration`,
// guardado por REDIS_URL, mesmo padrão de sql_postgres_integration_test.go/
// channel_rabbitmq_integration_test.go). Este arquivo prova o que É
// testável sem Redis: (a) qual script é selecionado por algoritmo e com
// quais KEYS/ARGS é chamado (montagem de chave, "chave" no nome da task), e
// (b) que QUALQUER falha do lado Redis (erro de rede OU resposta
// malformada) faz Allow rotear pro Limiter local — e que esse fallback é um
// limitador DE VERDADE (nega depois de esgotar a cota), nunca um
// "libera tudo" disfarçado (REQ-44.5, "não fail-open total").
const redisRateLimitTest = `package redisruntime

import (
	"context"
	"errors"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// fakeScripter implementa redisScripter inteiramente em memória — grava
// script/keys/args de cada chamada (pra provar montagem de chave/script) e
// devolve uma resposta OU um erro programáveis.
type fakeScripter struct {
	lastScript string
	lastKeys   []string
	lastArgs   []interface{}

	result []interface{} // resposta canônica {allowed, remaining, retry_after, reset_in}; nil ⇒ usa err
	err    error
}

func (f *fakeScripter) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	f.lastScript = script
	f.lastKeys = keys
	f.lastArgs = args
	cmd := redis.NewCmd(ctx)
	if f.err != nil {
		cmd.SetErr(f.err)
		return cmd
	}
	cmd.SetVal(f.result)
	return cmd
}

// TestRedisLimiterSelectsScriptAndKeyPerAlgorithm prova que cada algoritmo
// ("token_bucket"/"sliding_window"/"fixed_window"/qualquer valor
// desconhecido, que cai no default token_bucket — mesma postura de
// runtime.NewLimiter) seleciona o script Lua certo, e que a chave passada
// em KEYS[1] é namespaced ("ratelimit:<namespace>:<key>") — nunca a chave
// crua, pra nunca colidir entre dimensões/rotas distintas sobre o MESMO
// client Redis.
func TestRedisLimiterSelectsScriptAndKeyPerAlgorithm(t *testing.T) {
	cases := []struct {
		algorithm  string
		wantScript string
	}{
		{"token_bucket", tokenBucketScript},
		{"sliding_window", slidingWindowScript},
		{"fixed_window", fixedWindowScript},
		{"bogus", tokenBucketScript}, // default, mesma postura de runtime.NewLimiter
	}
	for _, tc := range cases {
		t.Run(tc.algorithm, func(t *testing.T) {
			f := &fakeScripter{result: []interface{}{int64(1), int64(4), "0", "60"}}
			l := newRedisLimiter(f, "checkout-perIp", tc.algorithm, 5, time.Minute, 0)

			if _, _, err := l.Allow(context.Background(), "1.2.3.4"); err != nil {
				t.Fatalf("Allow: erro inesperado: %v", err)
			}
			if f.lastScript != tc.wantScript {
				t.Fatalf("script usado != esperado pro algoritmo %q", tc.algorithm)
			}
			wantKey := "ratelimit:checkout-perIp:1.2.3.4"
			if len(f.lastKeys) != 1 || f.lastKeys[0] != wantKey {
				t.Fatalf("KEYS[1] = %v, want [%q]", f.lastKeys, wantKey)
			}
		})
	}
}

// TestRedisLimiterParsesResultFields prova que Allow decodifica
// {allowed, remaining, retry_after, reset_in} corretamente pra
// runtime.RateLimitResult — incl. o caso allowed=0 com retry_after > 0.
func TestRedisLimiterParsesResultFields(t *testing.T) {
	f := &fakeScripter{result: []interface{}{int64(0), int64(0), "2.5", "10"}}
	l := newRedisLimiter(f, "ns", "sliding_window", 5, time.Minute, 0)

	allowed, result, err := l.Allow(context.Background(), "k")
	if err != nil {
		t.Fatalf("Allow: erro inesperado: %v", err)
	}
	if allowed {
		t.Fatal("allowed = true, want false (allowed=0 na resposta)")
	}
	if result.Remaining != 0 {
		t.Fatalf("Remaining = %d, want 0", result.Remaining)
	}
	if result.RetryAfter != 2500*time.Millisecond {
		t.Fatalf("RetryAfter = %v, want 2.5s", result.RetryAfter)
	}
	if result.Limit != 5 {
		t.Fatalf("Limit = %d, want 5 (sliding_window reporta o limit configurado)", result.Limit)
	}
}

// TestRedisLimiterFallsBackToLocalOnRedisError prova REQ-44.5 (ponto 6): um
// erro de Eval (rede/timeout/conexão recusada) NUNCA propaga pro chamador
// de Allow — roteia pro Limiter local IMEDIATAMENTE, e esse fallback é um
// limitador DE VERDADE (nega depois de esgotar a cota configurada), não um
// "libera tudo" disfarçado.
func TestRedisLimiterFallsBackToLocalOnRedisError(t *testing.T) {
	f := &fakeScripter{err: errors.New("dial tcp: connection refused")}
	l := newRedisLimiter(f, "ns", "fixed_window", 2, time.Minute, 0)

	ctx := context.Background()
	allowed1, _, err := l.Allow(ctx, "k")
	if err != nil {
		t.Fatalf("Allow (1ª, Redis fora): erro propagado, want nil (deveria ter usado o fallback local): %v", err)
	}
	if !allowed1 {
		t.Fatal("Allow (1ª): esperava true (dentro da cota local)")
	}
	allowed2, _, _ := l.Allow(ctx, "k")
	if !allowed2 {
		t.Fatal("Allow (2ª): esperava true (ainda dentro da cota local, limit=2)")
	}
	allowed3, _, _ := l.Allow(ctx, "k")
	if allowed3 {
		t.Fatal("Allow (3ª): esperava false — o fallback local NÃO é fail-open, tem que negar depois de esgotar a cota (limit=2)")
	}
}

// TestRedisLimiterFallsBackToLocalOnMalformedResponse prova que uma
// resposta do Eval fora do formato esperado (não um array de 4 elementos)
// TAMBÉM roteia pro fallback local, não só um erro de transporte — Allow
// nunca propaga um erro de decodificação pro chamador.
func TestRedisLimiterFallsBackToLocalOnMalformedResponse(t *testing.T) {
	f := &fakeScripter{result: []interface{}{"resposta", "totalmente", "inesperada"}}
	l := newRedisLimiter(f, "ns", "token_bucket", 3, time.Minute, 0)

	allowed, _, err := l.Allow(context.Background(), "k")
	if err != nil {
		t.Fatalf("Allow: erro propagado, want nil (deveria ter usado o fallback local): %v", err)
	}
	if !allowed {
		t.Fatal("Allow: esperava true (fallback local, dentro da cota)")
	}
}
`

// TestRedisRateLimitAdapter roda redisRateLimitTest de verdade sobre um
// projeto Go mínimo (runtime + redisruntime vendorados, mesmo material que
// codegen.Generate escreveria para um programa com RateLimit backend:
// "redis") — prova REQ-44.2/44.3/44.5 sem NENHUMA conexão Redis real.
func TestRedisRateLimitAdapter(t *testing.T) {
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

	files[path.Join("redisruntime", "ratelimit_adapter_test.go")] = []byte(redisRateLimitTest)

	dir := gentest.WriteFiles(t, files)
	gentest.RunTests(t, dir, "60s")
}
