// Package symbols define a SymbolTable do front-end, com escopo por módulo e um
// nível público para símbolos compartilhados (PublicEvent em contracts/).
//
// É a fonte única de verdade para resolução de nomes e para regras semânticas
// como a exaustividade de match (REQ-4).
package symbols
