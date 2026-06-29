// Package parser organiza os tokens numa AST tipada via recursive descent
// escrito à mão, com recuperação de erros (REQ-2, REQ-3).
//
// O coração do recovery: expect com deleção de token único e inserção virtual,
// synchronize com sync sets hierárquicos por nível, janela de silêncio
// anti-cascata e garantia de progresso em todo loop. O parser não conhece
// nenhuma regra semântica da §23 (NFR-6).
package parser
