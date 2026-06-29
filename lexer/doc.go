// Package lexer converte texto-fonte em uma sequência de tokens com posição,
// numa única passagem sobre os runes da entrada (REQ-1).
//
// Caracteres inválidos e strings não terminadas viram diagnósticos sem
// interromper a tokenização; o lexer sempre garante progresso (NFR-2).
package lexer
