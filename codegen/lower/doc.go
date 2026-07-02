// Package lower traduz expressões e statements DomainScript para sua forma Go
// (REQ-22): o ambiente de tipos local (TypeEnv, §design codegen 3.6a) que
// estende types.Model com o tipo dos locais que load/list/match/lambda
// introduzem, e o lowering de expr/stmt/built-ins sobre esse ambiente.
package lower
