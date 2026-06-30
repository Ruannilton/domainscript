// Package astutil reúne percursos genéricos da AST, neutros quanto à fase.
//
// Os utilitários aqui eram não-exportados em sema/walk.go. Foram movidos para um
// pacote próprio quando o resolver passou a precisar deles para a resolução de
// nomes em corpos (REQ-9): o resolver não pode importar sema (violaria a direção
// de dependências sema → resolver → … → ast), então a travessia compartilhada
// vive abaixo de ambos, dependendo só de ast (NFR-8, §design type-checking 5).
//
// São deliberadamente totais sobre os nós existentes: adicionar um construto novo
// exige estendê-los aqui, num só lugar (NFR-5).
package astutil
