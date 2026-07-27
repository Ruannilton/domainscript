#!/usr/bin/env bash
# PreToolUse guard do agente `issue-registrar` (.claude/agents/issue-registrar.md).
#
# Este agente PODE rodar testes — é o único que pode, e é o ponto dele: só se
# registra issue depois de provar que o problema existe. Bash fica livre.
#
# O que ele não pode é **consertar**: nenhuma edição de código do repositório.
# Regra de caminho para Write/Edit/NotebookEdit:
#
#   .../docs/sdd/...       -> permitido (o arquivo da issue, o índice)
#   .../.claude/...        -> permitido (state, skills assets)
#   dentro do repositório  -> negado    (código, fixtures, docs, CI)
#   fora do repositório    -> permitido (/tmp, cópia isolada de reprodução)
#
# A terceira regra é deliberada: reproduzir um defeito às vezes exige mexer
# numa cópia da árvore, e o precedente do repo é exatamente esse ("investigando
# numa cópia de trabalho isolada, nunca commitada" — ver a issue do pizzeria).
#
# **Sem dependência de `jq`** (ver `spec-implementer-guard.sh`): há máquina de
# desenvolvimento sem `jq` instalado, e a versão anterior deste guard falhava
# ABERTO nela — `jq` erra, `tool` vem vazio, nenhum `case` casa e o script sai
# 0. A leitura dos campos agora é feita com `sed` sobre o payload cru, e tool
# ilegível cai na checagem de caminho em vez de liberar.
#
# Contrato de hook (PreToolUse): payload JSON no stdin, decisão em JSON no
# stdout. Sai 0 sempre — negar é `permissionDecision: deny`, não exit code.
set -uo pipefail

payload=$(cat | tr '\n' ' ')

# Lê o valor (string) de uma chave JSON do payload. O valor sai ainda escapado
# em JSON (`\"`, `\\`), o que basta para casar caminho e compor mensagem.
json_str() {
  printf '%s' "$payload" | sed -n -E 's/.*"'"$1"'"[[:space:]]*:[[:space:]]*"(([^"\\]|\\.)*)".*/\1/p'
}

tool=$(json_str tool_name)

case "$tool" in
Write | Edit | NotebookEdit) ;;
# Tool ilegível: falha fechado — segue para a checagem se houver caminho.
"") ;;
*) exit 0 ;;
esac

path=$(json_str file_path)
[ -n "$path" ] || path=$(json_str notebook_path)
# Sem caminho não há o que checar (e tool ilegível sem caminho não é edição).
[ -n "$path" ] || exit 0

root=$(json_str cwd)
[ -n "$root" ] || root="${CLAUDE_PROJECT_DIR:-}"

# Normaliza separador: no Windows o payload traz `C:\\Dev\\...\\.claude\\...`.
norm_path=$(printf '%s' "$path" | tr '\\' '/')
norm_root=$(printf '%s' "$root" | tr '\\' '/')

# Sem root não há como situar o caminho: mantém o comportamento permissivo.
[ -n "$norm_root" ] || exit 0

# Fora do repositório (cópia de reprodução, scratch): permitido.
base="${norm_root%/}/"
case "$norm_path" in
"$base"*) rel="/${norm_path#"$base"}" ;;
*) exit 0 ;;
esac

# Um worktree vive em `<root>/.claude/worktrees/<nome>/`: re-ancora nele, para
# que a regra valha sobre a árvore DO worktree e não sobre o `.claude/` que a
# hospeda.
rel=$(printf '%s' "$rel" | sed -E 's#^/\.claude/worktrees/[^/]+##')
[ -n "$rel" ] || rel=/

# Arquivo de issue / qualquer coisa sob .claude/ ou docs/sdd/: sempre permitido.
# A regra é avaliada no caminho RELATIVO ao root — sobre o caminho absoluto, a
# árvore inteira de um worktree casaria `*/.claude/*` e liberaria todo o código.
case "$rel" in
*/.claude/*) exit 0 ;;
*/docs/sdd/*) exit 0 ;;
esac

# Escapa o mínimo para um JSON válido: barra invertida, aspas e tabs.
reason="issue-registrar nao conserta nem altera o repositorio: Write/Edit dentro do repo so sob .claude/ ou docs/sdd/. Caminho recusado: $path. Para reproduzir um defeito, trabalhe numa copia fora do repositorio (ex.: sob /tmp) — rodar teste e permitido, editar o codigo versionado nao. O conserto e de outra task/agente."
reason=$(printf '%s' "$reason" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/\t/ /g')
printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"%s"}}\n' "$reason"
exit 0
