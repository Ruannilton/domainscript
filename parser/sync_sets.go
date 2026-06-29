package parser

import "domainscript/token"

// stopSet é um conjunto de tokens de sincronização usado pelo recovery em modo
// de pânico (REQ-3.4).
type stopSet map[token.Kind]bool

func newStopSet(kinds ...token.Kind) stopSet {
	s := make(stopSet, len(kinds))
	for _, k := range kinds {
		s[k] = true
	}
	return s
}

func (s stopSet) contains(k token.Kind) bool { return s[k] }

// union devolve um novo conjunto com os elementos de s e dos demais. Serve para
// compor um nível com os conjuntos de seus ancestrais (REQ-3.4).
func (s stopSet) union(others ...stopSet) stopSet {
	out := make(stopSet, len(s))
	for k := range s {
		out[k] = true
	}
	for _, o := range others {
		for k := range o {
			out[k] = true
		}
	}
	return out
}

// topLevelKeywords são as keywords que iniciam uma declaração de topo — as
// âncoras de máxima confiança do recovery (REQ-3.7): ao avistá-las durante o
// pânico, o parser reancora no nível de arquivo e reconhece a próxima declaração
// independentemente.
var topLevelKeywords = []token.Kind{
	token.VALUEOBJECT, token.ENUM, token.ERROR, token.EVENT, token.PUBLICEVENT,
	token.UPCAST, token.AGGREGATE, token.COMMAND, token.USECASE, token.VIEW,
	token.PROJECTION, token.QUERY, token.POLICY, token.WORKER, token.NOTIFICATION,
	token.ADAPTER, token.FOREIGN, token.SAGA, token.METRIC,
	token.MODULE, token.INTERFACE, token.TOPOLOGY, token.VERSION, token.TEST,
	token.FIXTURE,
}

// topLevelStop interrompe a sincronização na próxima declaração de topo ou no
// EOF — o conjunto-raiz do qual os níveis internos derivam.
var topLevelStop = newStopSet(append(append([]token.Kind{}, topLevelKeywords...), token.EOF)...)

// stmtStop interrompe a sincronização no início do próximo statement sem furar
// para fora do bloco corrente; inclui os ancestrais (nível de topo). Sets de
// membros de declaração específicos (Aggregate: Handle/Apply/state/access, ...)
// são definidos por construto na Fase 4, sobre estes conjuntos-base.
var stmtStop = newStopSet(
	token.ENSURE, token.MATCH, token.FOR, token.RETURN, token.EMIT, token.LOG,
	token.BREAK, token.CONTINUE,
).union(topLevelStop)

// listStop interrompe a sincronização nos delimitadores de elementos de lista.
var listStop = newStopSet(token.COMMA, token.RPAREN, token.RBRACK, token.RBRACE)
