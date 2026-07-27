#!/usr/bin/env bash
# PreToolUse guard do agente `spec-writer` (.claude/agents/spec-writer.md).
#
# Torna estruturais as duas proibições do agente, em vez de deixá-las só no
# prompt: ele **não altera código** e **não executa testes**. Regras:
#
#   Write/Edit/NotebookEdit  -> só sob .claude/ ou docs/sdd/ (specs, state.md, issues)
#   Bash                     -> nega toolchain Go/make/dsc e mutadores de
#                               arquivo in-place; git de leitura/commit/push
#                               segue liberado
#
# **Sem dependência de `jq`** (ver `spec-implementer-guard.sh`): há máquina de
# desenvolvimento sem `jq` instalado, e a versão anterior deste guard falhava
# ABERTO nela — `jq` erra, `tool` vem vazio, nenhum `case` casa e o script sai
# 0. A leitura dos campos agora é feita com `sed` sobre o payload cru, e todo
# caminho de erro falha FECHADO: tool ilegível ⇒ ambas as checagens rodam.
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

# Um comando só conta quando está em posição de comando: início da linha ou
# logo após um separador (;, &, |, parêntese). Isso evita falso positivo com a
# palavra solta dentro de uma mensagem de commit (ex.: `git commit -m "... make ..."`).
cmd_pos='(^|[;&|(])[[:space:]]*'

check_path() {
  path=$(json_str file_path)
  [ -n "$path" ] || path=$(json_str notebook_path)

  # Normaliza separador: no Windows o payload traz `C:\\Dev\\...\\.claude\\...`.
  norm=$(printf '%s' "$path" | tr '\\' '/')

  # Dentro do repositório a regra é avaliada no caminho RELATIVO ao root — o
  # repo hospeda worktrees em `.claude/worktrees/<nome>/`, e o glob sobre o
  # caminho absoluto faria a árvore inteira de um worktree casar `*/.claude/*`,
  # liberando todo o código. O segmento do worktree é descartado para que a
  # regra valha sobre a árvore DO worktree.
  root=$(printf '%s' "$(json_str cwd)" | tr '\\' '/')
  if [ -n "$root" ]; then
    base="${root%/}/"
    case "$norm" in
    "$base"*) norm="/${norm#"$base"}" ;;
    esac
    norm=$(printf '%s' "$norm" | sed -E 's#^/\.claude/worktrees/[^/]+##')
    [ -n "$norm" ] || norm=/
  fi

  case "$norm" in
  */.claude/*) return 0 ;;
  */docs/sdd/*) return 0 ;;
  esac

  deny "spec-writer nao altera codigo: Write/Edit so e permitido sob .claude/ ou docs/sdd/ (specs, state.md, issues). Caminho recusado: ${path:-<vazio>}. Um defeito de codigo encontrado durante a pesquisa vira issue (skill issue-generator), nao um patch."
}

check_cmd() {
  cmd=$(json_str command)
  # Sem comando legível, casa contra o payload inteiro — falha fechado.
  haystack=$cmd
  [ -n "$haystack" ] || haystack=$payload

  if printf '%s' "$haystack" | grep -Eq "${cmd_pos}(go[[:space:]]+(test|build|run|vet|generate)|gotestsum|gofmt|make|dsc)([[:space:]]|\\\\?\"|\$)"; then
    deny "spec-writer nao executa testes nem o toolchain (go test/build/run/vet, gotestsum, gofmt, make, dsc). Esta fase e de especificacao; a validacao roda em CI na PR. Comando recusado: ${cmd:-<ilegivel>}"
  fi

  if printf '%s' "$haystack" | grep -Eq "${cmd_pos}(sed[[:space:]]+-i|perl[[:space:]]+-[a-zA-Z]*i|patch|rm|mv|cp|tee|truncate|dd)([[:space:]]|\\\\?\"|\$)"; then
    deny "spec-writer nao altera arquivos por shell (sed -i, patch, rm, mv, cp, tee, truncate, dd). Escreva sob .claude/ com Write/Edit. Comando recusado: ${cmd:-<ilegivel>}"
  fi

  if printf '%s' "$haystack" | grep -Eq "${cmd_pos}git[[:space:]]+(apply|am|restore|revert|reset|clean)([[:space:]]|\\\\?\"|\$)"; then
    deny "spec-writer nao desfaz nem reescreve a arvore de trabalho (git apply/am/restore/revert/reset/clean). Comando recusado: ${cmd:-<ilegivel>}"
  fi

  if printf '%s' "$haystack" | grep -Eq "${cmd_pos}git[[:space:]]+checkout[[:space:]]+--"; then
    deny "spec-writer nao descarta mudancas com 'git checkout --'. Comando recusado: ${cmd:-<ilegivel>}"
  fi
}

tool=$(json_str tool_name)

case "$tool" in
Write | Edit | NotebookEdit)
  check_path
  ;;
Bash)
  check_cmd
  ;;
"")
  # Payload ilegível: falha fechado. A checagem de caminho só entra se houver
  # de fato um campo de caminho, para não recusar tool de leitura.
  if [ -n "$(json_str file_path)$(json_str notebook_path)" ]; then
    check_path
  fi
  check_cmd
  ;;
esac

exit 0
