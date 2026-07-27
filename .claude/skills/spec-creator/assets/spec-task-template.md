---
id: TASK-[CODE]
title: "[NOME DA TAREFA]"
status: pending # pending | in_progress | completed
depends_on:
  - TASK-[CODE_ANTERIOR]
references:
  requirements: ["REQ-XX", "NFR-YY"]
  design: ["Secao X.Y"]
target_files:
  - "caminho/para/arquivo_novo_ou_editado.go"
  - "caminho/para/arquivo_de_teste_test.go"
---

# TASK-[CODE]: [NOME DA TAREFA]

## 1. Contexto & Objetivo
> **Instrução para o Claude Code:** Leia as seções de referência do `design.md` indicadas nos metadados acima antes de iniciar. Esta tarefa deve modificar **apenas** os arquivos listados em `target_files`.

[Breve resumo do que deve ser feito e o impacto no sistema.]

## 2. Passos de Implementação
- [ ] **Step 1:** [Descrição detalhada da ação]
- [ ] **Step 2:** [Descrição da próxima ação, ex: criar struct/interface]
- [ ] **Step 3:** [Descrição da integração ou lowering]

## 3. Testes & Validação
Para considerar esta tarefa concluída, os seguintes cenários devem passar:

### Casos de Teste Unitários / Golden Tests
- [ ] **TEST-1:** [Descrição do cenário positivo / entrada e saída esperada]
- [ ] **TEST-2:** [Descrição do cenário de erro ou edge case]


## 4. Revisão requerida
[Alguma preocupação que precisa ser revisada antes de implementar a task]

## 5. Mensagem de Commit
feat([escopo/pacote]): [resumo imperativo da task em até 50 chars]




