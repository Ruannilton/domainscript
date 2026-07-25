#!/usr/bin/env bash
# PreToolUse guard do agente `issue-registrar` (.claude/agents/issue-registrar.md).
#
# Este agente PODE rodar testes — é o único que pode, e é o ponto dele: só se
# registra issue depois de provar que o problema existe. Bash fica livre.
#
# O que ele não pode é **consertar**: nenhuma edição de código do repositório.
# Regra de caminho para Write/Edit/NotebookEdit:
#
#   .../.claude/...        -> permitido (o arquivo da issue, o índice)
#   dentro do repositório  -> negado    (código, fixtures, docs, CI)
#   fora do repositório    -> permitido (/tmp, cópia isolada de reprodução)
#
# A terceira regra é deliberada: reproduzir um defeito às vezes exige mexer
# numa cópia da árvore, e o precedente do repo é exatamente esse ("investigando
# numa cópia de trabalho isolada, nunca commitada" — ver a issue do pizzeria).
#
# Contrato de hook (PreToolUse): payload JSON no stdin, decisão em JSON no
# stdout. Sai 0 sempre — negar é `permissionDecision: deny`, não exit code.
set -uo pipefail

payload=$(cat)
tool=$(printf '%s' "$payload" | jq -r '.tool_name // ""')

case "$tool" in
Write | Edit | NotebookEdit) ;;
*) exit 0 ;;
esac

path=$(printf '%s' "$payload" | jq -r '.tool_input.file_path // .tool_input.notebook_path // ""')
root=$(printf '%s' "$payload" | jq -r '.cwd // ""')
[ -n "$root" ] || root="${CLAUDE_PROJECT_DIR:-}"

# Arquivo de issue / qualquer coisa sob .claude/: sempre permitido.
case "$path" in
*/.claude/*) exit 0 ;;
esac

# Fora do repositório (cópia de reprodução, scratch): permitido.
if [ -z "$root" ] || [ "${path#"$root"/}" = "$path" ]; then
  exit 0
fi

jq -n --arg path "$path" '{
  hookSpecificOutput: {
    hookEventName: "PreToolUse",
    permissionDecision: "deny",
    permissionDecisionReason: ("issue-registrar não conserta nem altera o repositório: Write/Edit dentro do repo só sob .claude/. Caminho recusado: " + $path + ". Para reproduzir um defeito, trabalhe numa cópia fora do repositório (ex.: sob /tmp) — rodar teste é permitido, editar o código versionado não. O conserto é de outra task/agente.")
  }
}'
exit 0
