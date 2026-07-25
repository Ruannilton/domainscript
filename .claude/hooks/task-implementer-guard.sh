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
# **Sem dependência de `jq`** (ver `spec-implementer-guard.sh`): há máquina de
# desenvolvimento sem `jq` instalado, e a versão anterior deste guard falhava
# ABERTO nela — `jq` erra, o campo vem vazio, o script sai 0 e nada é bloqueado.
# A leitura dos campos agora é feita com `sed` sobre o payload cru, e todo
# caminho de erro falha FECHADO: campo ilegível ⇒ a checagem roda mesmo assim.
#
# Contrato de hook (PreToolUse): payload JSON no stdin, decisão em JSON no
# stdout. Sai 0 sempre — negar é `permissionDecision: deny`, não exit code.
set -uo pipefail

payload=$(cat | tr '\n' ' ')

# Lê o valor (string) de uma chave JSON do payload. O valor sai ainda escapado
# em JSON (`\"`, `\\`), o que basta para casar padrão e compor mensagem.
json_str() {
  printf '%s' "$payload" | sed -n -E 's/.*"'"$1"'"[[:space:]]*:[[:space:]]*"(([^"\\]|\\.)*)".*/\1/p'
}

deny() {
  # Escapa o mínimo para um JSON válido: barra invertida, aspas e tabs.
  reason=$(printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/\t/ /g')
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"%s"}}\n' "$reason"
  exit 0
}

tool=$(json_str tool_name)
# Só Bash interessa. Tool ilegível (vazio) NÃO sai: cai na checagem abaixo.
if [ -n "$tool" ] && [ "$tool" != "Bash" ]; then
  exit 0
fi

cmd=$(json_str command)
# Sem comando legível, casa contra o payload inteiro — falha fechado.
haystack=$cmd
[ -n "$haystack" ] || haystack=$payload

reason_tail="Este agente nao executa testes em hipotese alguma — escreva os testes que a task especifica e deixe o CI da PR roda-los. Para checar que a arvore esta sa use 'go build ./...', 'go vet ./...' ou 'gofmt -l .'. Comando recusado: ${cmd:-<ilegivel>}"

# go test (inclusive prefixado por env vars ou precedido de cd/&&) e gotestsum.
if printf '%s' "$haystack" | grep -Eq '(^|[^[:alnum:]_./-])go[[:space:]]+test([[:space:]]|\\?"|$)'; then
  deny "'go test' bloqueado. $reason_tail"
fi

if printf '%s' "$haystack" | grep -Eq '(^|[^[:alnum:]_./-])(gotestsum|richgo|ginkgo|gotest)([[:space:]]|\\?"|$)'; then
  deny "Runner de teste bloqueado. $reason_tail"
fi

# make com alvo `test` (make/make build/make lint/make fmt seguem liberados —
# o alvo default deste Makefile é `build`).
if printf '%s' "$haystack" | grep -Eq '(^|[^[:alnum:]_./-])make[[:space:]]+([^;&|"]*[[:space:]])?test([[:space:]]|\\?"|$)'; then
  deny "'make test' bloqueado. $reason_tail"
fi

exit 0
