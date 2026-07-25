#!/usr/bin/env bash
# PreToolUse guard do agente `spec-implementer` (.claude/agents/spec-implementer.md).
#
# O agente é um orquestrador puro: ele só escolhe a spec e dispara o
# `task-implementer`. Não tem `Write`/`Edit` (isso já sai da lista de tools);
# este hook fecha o buraco restante, que é o `Bash`.
#
# Liberado: leitura de estado — `git log|status|diff|show|fetch|ls-remote|
# rev-parse|symbolic-ref|branch --list`, `ls`, `cat`, `gh pr view|list`.
# Bloqueado: qualquer git que MUTA a árvore ou o remoto (`commit`, `push`,
# `checkout`, `merge`, `rebase`, `reset`, `add`, `stash`, `tag`, `cherry-pick`),
# execução de teste (`go test`, `gotestsum`, `make test`) e o toolchain de
# build (`go build`, `go vet`, `gofmt`) — nada disso é trabalho dele.
#
# **Sem dependência de `jq`.** Os outros guards deste diretório usam `jq` e, em
# máquina sem `jq` instalado, falham ABERTO (o campo vem vazio e o script sai 0).
# Aqui a checagem roda sobre o payload cru, deliberadamente ampla: casa em
# qualquer posição, o que também pega formas prefixadas (`cd x && git commit`,
# `CGO_ENABLED=0 go build`). O custo é recusar um comando que apenas MENCIONE o
# padrão em texto livre — aceitável para um agente que não deveria rodar nada
# além de leitura.
#
# Contrato de hook (PreToolUse): payload JSON no stdin, decisão em JSON no
# stdout. Sai 0 sempre — negar é `permissionDecision: deny`, não exit code.
set -uo pipefail

payload=$(cat | tr '\n' ' ')

# Só interessa Bash. Sem jq, casa o campo direto no payload.
printf '%s' "$payload" | grep -Eq '"tool_name"[[:space:]]*:[[:space:]]*"Bash"' || exit 0

deny() {
  # Escapa o mínimo para um JSON válido: barra invertida, aspas e tabs.
  reason=$(printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/\t/ /g')
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"%s"}}\n' "$reason"
  exit 0
}

tail_msg="Este agente apenas orquestra: ele escolhe a spec, dispara o 'task-implementer' e verifica o commit por leitura. Escrever codigo, commitar, dar push, buildar e acompanhar CI e trabalho do subagente."

# git que muta árvore, índice ou remoto.
if printf '%s' "$payload" | grep -Eq '(^|[^[:alnum:]_./-])git[[:space:]]+([^;&|"]*[[:space:]])?(commit|push|checkout|switch|merge|rebase|reset|revert|add|rm|mv|stash|tag|cherry-pick|apply|clean|am|worktree)([[:space:]]|\\?"|$)'; then
  deny "Comando git mutante bloqueado. $tail_msg"
fi

# Execução de teste.
if printf '%s' "$payload" | grep -Eq '(^|[^[:alnum:]_./-])go[[:space:]]+test([[:space:]]|\\?"|$)'; then
  deny "'go test' bloqueado. $tail_msg"
fi

if printf '%s' "$payload" | grep -Eq '(^|[^[:alnum:]_./-])(gotestsum|richgo|ginkgo|gotest)([[:space:]]|\\?"|$)'; then
  deny "Runner de teste bloqueado. $tail_msg"
fi

# Toolchain Go e build/format.
if printf '%s' "$payload" | grep -Eq '(^|[^[:alnum:]_./-])go[[:space:]]+(build|vet|run|generate|mod)([[:space:]]|\\?"|$)'; then
  deny "Toolchain Go bloqueado. $tail_msg"
fi

if printf '%s' "$payload" | grep -Eq '(^|[^[:alnum:]_./-])(make|gofmt|goimports)([[:space:]]|\\?"|$)'; then
  deny "Build/format bloqueado. $tail_msg"
fi

exit 0
