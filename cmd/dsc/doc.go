// Command dsc é a CLI do transpilador DomainScript, com dois subcomandos:
// "dsc check <arquivo|diretório>" roda o pipeline de validação do front-end e
// imprime o relatório de diagnósticos (REQ-8); "dsc gen <diretório> -o <saída>"
// valida e gera o projeto Go do back-end (REQ-32). Um único argumento
// posicional (sem subcomando) é tratado como "dsc check <path>" — retrocompat.
// Exit code: 0 sem erros, 1 com diagnóstico de erro, 2 erro de uso/IO.
package main
