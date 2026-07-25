# Requirements — [Nome do Módulo, Estágio ou Feature]

## 1. Introdução

### 1.1. Objetivo
[Descreva o objetivo principal deste módulo. O que ele recebe como entrada e o que produz como saída? Use diagramas ASCII se ajudar a ilustrar o fluxo.]

### 1.2. Alinhamento filosófico com o spec
[Liste os princípios fundamentais que guiam esta implementação. Ex: "Transpilação, não interpretação", "Zero infraestrutura no domínio", etc.]
- **[Princípio 1]:** [Explicação curta].
- **[Princípio 2]:** [Explicação curta].

### 1.3. Escopo
[Descreva o que será coberto neste ciclo e as restrições explícitas do que NÃO será feito agora.]

| Em escopo | Fora de escopo |
|---|---|
| [Funcionalidade/Suporte A] | [Funcionalidade B (fica para o próximo marco)] |
| [Funcionalidade C] | [Otimização prematura X] |

### 1.4. Pré-condição e baseline
[O que o sistema assume como verdadeiro antes deste módulo rodar? Ex: A AST já deve ter passado pelo Type Checker e não conter erros de severidade alta.]

### 1.5. Glossário (incremental)
| Termo | Definição |
|---|---|
| [Termo 1] | [Definição 1] |
| [Termo 2] | [Definição 2] |

---

## 2. Requisitos Funcionais

> Formato EARS (**WHEN/WHILE/IF … THE SYSTEM SHALL …**). "O SISTEMA" = [Defina quem é o ator principal, ex: o analisador léxico, o gerador de Go].

### REQ-[X] — [Nome da Funcionalidade / Grupo de Requisitos]

**User story:** Como [ator], quero [ação], para que [valor/motivo].

**Critérios de aceitação:**
1. WHEN [condição/gatilho], THE SYSTEM SHALL [comportamento esperado].
2. WHEN [outra condição], THE SYSTEM SHALL [outro comportamento].
3. THE SYSTEM SHALL [comportamento incondicional ou garantia estrutural].

### REQ-[X+1] — [Nome da Funcionalidade / Grupo de Requisitos]

**User story:** Como [ator], quero [ação], para que [valor/motivo].

**Critérios de aceitação:**
1. THE SYSTEM SHALL [comportamento esperado].
2. IF [condição de erro], THE SYSTEM SHALL [tratamento ou diagnóstico emitido].

---

## 3. Requisitos Não-Funcionais (incrementais)

> Os NFRs dos ciclos anteriores continuam valendo. Abaixo, os adicionais deste ciclo.

### NFR-[Y] — [Nome do Requisito Não-Funcional]
[Descrição em prosa do requisito. Ex: Performance, determinismo, dependências mínimas, formato de saída, etc.]

---

## 4. Rastreabilidade

| Requisito | Tema | Marco (tasks.md) |
|---|---|---|
| REQ-[X] | [Resumo do tema] | [Letra/Nome do Marco] |
| REQ-[X+1] | [Resumo do tema] | [Letra/Nome do Marco] |

---

## 5. Critérios de Pronto (Definition of Done)

O [módulo/estágio] está completo quando:
1. [Critério global 1, ex: Todos os testes de unidade passam].
2. [Critério global 2, ex: A saída gerada é determinística e compila sem erros].
3. [Critério global 3].

### Entrega incremental (marcos)
- **Marco [A] — "[Nome/Foco do Marco]":** [O que é entregue nesta fatia].
- **Marco [B] — "[Nome/Foco do Marco]":** [O que é entregue nesta fatia].