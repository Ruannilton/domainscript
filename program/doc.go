// Package program agrega as ASTs de um diretório de projeto num modelo
// unificado antes da validação cross-file (REQ-7).
//
// Constrói o grafo módulo→service→canal a partir de topology.ds e mod.ds e dá
// às regras semânticas acesso simultâneo a todos os símbolos do programa.
package program
