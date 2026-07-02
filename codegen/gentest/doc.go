// Package gentest reúne os helpers de teste do gerador (NFR-13/14/17): Golden
// compara uma saída com um artefato de referência versionado (e, com
// UPDATE_GOLDEN=1, regrava o artefato); Deterministic exige que gerar duas
// vezes produza bytes idênticos; SmokeCompile escreve um conjunto de arquivos
// num diretório temporário e roda "go build"/"go vet" sobre eles.
//
// É deliberadamente independente do pacote codegen (usa map[string][]byte, não
// o tipo File) para não criar um ciclo de import com os testes internos de
// codegen/emit/lower, que importam gentest.
package gentest
