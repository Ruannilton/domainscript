// Package resolver faz a coleta de símbolos e a resolução de nomes em duas
// passagens sobre a AST: registra cada declaração na SymbolTable e liga cada
// referência (ref, handles, on, tipos de campo/parâmetro) ao seu símbolo.
//
// Declarações duplicadas e referências não resolvidas viram diagnósticos;
// subárvores com nós de erro são puladas (REQ-4).
package resolver
