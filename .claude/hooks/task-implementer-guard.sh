#!/usr/bin/env bash
# PreToolUse guard do agente `task-implementer` (.claude/agents/task-implementer.md).
#
# Torna estrutural a proibição absoluta do agente: ele **não executa testes**.
# O único feedback de teste que ele tem direito é o do CI na PR.
#
# Bloqueia apenas EXECUÇÃO de teste — `go test`, `gotestsum` e `make` com alvo
# `test`. Compilar e formatar continua liberado (`go build`, `go vet`, `gofmt`,
# `make build|lint|fmt|vet|fmt-check`), porque o agente precisa entregar árvore
# que compila e formatada, e nada disso roda um caso de teste.
#
# A checagem é deliberadamente ampla (casa em qualquer posição da linha, não só
# em posição de comando) para pegar formas prefixadas como
# `CGO_ENABLED=0 go test ./...` e `cd pkg && go test`. O custo é recusar um
# comando que apenas MENCIONE "go test" em texto livre — aceitável dada a
# ênfase do requisito.
#
# Contrato de hook (PreToolUse): payload JSON no stdin, decisão em JSON no
# stdout. Sai 0 sempre — negar é `permissionDecision: deny`, não exit code.
set -uo pipefail

payload=$(cat)
tool=$(printf '%s' "$payload" | jq -r '.tool_name // ""')
[ "$tool" = "Bash" ] || exit 0

cmd=$(printf '%s' "$payload" | jq -r '.tool_input.command // ""')

deny() {
  jq -n --arg reason "$1" '{
    hookSpecificOutput: {
      hookEventName: "PreToolUse",
      permissionDecision: "deny",
      permissionDecisionReason: $reason
    }
  }'
  exit 0
}

reason_tail="Este agente não executa testes em hipótese alguma — escreva os testes que a task especifica e deixe o CI da PR rodá-los. Para checar que a árvore está sã use 'go build ./...', 'go vet ./...' ou 'gofmt -l .'. Comando recusado: $cmd"

# go test (inclusive prefixado por env vars ou precedido de cd/&&) e gotestsum.
if printf '%s' "$cmd" | grep -Eq '(^|[^[:alnum:]_./-])go[[:space:]]+test([[:space:]]|$)'; then
  deny "'go test' bloqueado. $reason_tail"
fi

if printf '%s' "$cmd" | grep -Eq '(^|[^[:alnum:]_./-])(gotestsum|richgo|ginkgo|gotest)([[:space:]]|$)'; then
  deny "Runner de teste bloqueado. $reason_tail"
fi

# make com alvo `test` (make/make build/make lint/make fmt seguem liberados —
# o alvo default deste Makefile é `build`).
if printf '%s' "$cmd" | grep -Eq '(^|[^[:alnum:]_./-])make[[:space:]]+([^;&|]*[[:space:]])?test([[:space:]]|$)'; then
  deny "'make test' bloqueado. $reason_tail"
fi

exit 0
