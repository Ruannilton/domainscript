#!/usr/bin/env bash
# PreToolUse guard do agente `spec-writer` (.claude/agents/spec-writer.md).
#
# Torna estruturais as duas proibições do agente, em vez de deixá-las só no
# prompt: ele **não altera código** e **não executa testes**. Regras:
#
#   Write/Edit/NotebookEdit  -> só sob .claude/ (specs, state.md, issues)
#   Bash                     -> nega toolchain Go/make/dsc e mutadores de
#                               arquivo in-place; git de leitura/commit/push
#                               segue liberado
#
# Contrato de hook (PreToolUse): payload JSON no stdin, decisão em JSON no
# stdout. Sai 0 sempre — negar é `permissionDecision: deny`, não exit code.
set -uo pipefail

payload=$(cat)
tool=$(printf '%s' "$payload" | jq -r '.tool_name // ""')

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

# Um comando só conta quando está em posição de comando: início da linha ou
# logo após um separador (;, &, |, parêntese). Isso evita falso positivo com a
# palavra solta dentro de uma mensagem de commit (ex.: `git commit -m "... make ..."`).
cmd_pos='(^|[;&|(])[[:space:]]*'

case "$tool" in
Write | Edit | NotebookEdit)
  path=$(printf '%s' "$payload" | jq -r '.tool_input.file_path // .tool_input.notebook_path // ""')
  case "$path" in
  */.claude/*) ;;
  *)
    deny "spec-writer não altera código: Write/Edit só é permitido sob .claude/ (specs, state.md, issues). Caminho recusado: ${path:-<vazio>}. Um defeito de código encontrado durante a pesquisa vira issue (skill issue-generator), não um patch."
    ;;
  esac
  ;;
Bash)
  cmd=$(printf '%s' "$payload" | jq -r '.tool_input.command // ""')

  if printf '%s' "$cmd" | grep -Eq "${cmd_pos}(go[[:space:]]+(test|build|run|vet|generate)|gotestsum|gofmt|make|dsc)([[:space:]]|$)"; then
    deny "spec-writer não executa testes nem o toolchain (go test/build/run/vet, gotestsum, gofmt, make, dsc). Esta fase é de especificação; a validação roda em CI na PR. Comando recusado: $cmd"
  fi

  if printf '%s' "$cmd" | grep -Eq "${cmd_pos}(sed[[:space:]]+-i|perl[[:space:]]+-[a-zA-Z]*i|patch|rm|mv|cp|tee|truncate|dd)([[:space:]]|$)"; then
    deny "spec-writer não altera arquivos por shell (sed -i, patch, rm, mv, cp, tee, truncate, dd). Escreva sob .claude/ com Write/Edit. Comando recusado: $cmd"
  fi

  if printf '%s' "$cmd" | grep -Eq "${cmd_pos}git[[:space:]]+(apply|am|restore|revert|reset|clean)([[:space:]]|$)"; then
    deny "spec-writer não desfaz nem reescreve a árvore de trabalho (git apply/am/restore/revert/reset/clean). Comando recusado: $cmd"
  fi

  if printf '%s' "$cmd" | grep -Eq "${cmd_pos}git[[:space:]]+checkout[[:space:]]+--"; then
    deny "spec-writer não descarta mudanças com 'git checkout --'. Comando recusado: $cmd"
  fi
  ;;
esac

exit 0
