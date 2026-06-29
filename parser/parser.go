package parser

import (
	"domainscript/diag"
	"domainscript/token"
)

// parser é o estado do recursive-descent parser: um cursor sobre a sequência de
// tokens produzida pelo lexer e o DiagnosticBag onde os erros de sintaxe são
// acumulados. Esta fase implementa só a infraestrutura de leitura e recuperação;
// os construtos da gramática chegam na Fase 4.
type parser struct {
	toks []token.Token
	pos  int
	bag  *diag.DiagnosticBag
}

func newParser(toks []token.Token, bag *diag.DiagnosticBag) *parser {
	// Robustez (NFR-2): a sequência sempre termina em EOF, mesmo se o chamador
	// passar uma lista vazia ou sem o sentinela.
	if len(toks) == 0 || toks[len(toks)-1].Kind != token.EOF {
		toks = append(toks, token.Token{Kind: token.EOF})
	}
	return &parser{toks: toks, bag: bag}
}

// --- cursor ---

// cur devolve o token corrente; nunca ultrapassa o EOF.
func (p *parser) cur() token.Token { return p.toks[p.pos] }

// peek devolve o próximo token sem consumir.
func (p *parser) peek() token.Token { return p.peekAt(1) }

// peekAt devolve o token n posições à frente, saturando no EOF final.
func (p *parser) peekAt(n int) token.Token {
	i := p.pos + n
	if i >= len(p.toks) {
		return p.toks[len(p.toks)-1]
	}
	return p.toks[i]
}

// at reporta se o token corrente é do tipo k.
func (p *parser) at(k token.Kind) bool { return p.cur().Kind == k }

// atEnd reporta se o cursor chegou ao EOF.
func (p *parser) atEnd() bool { return p.cur().Kind == token.EOF }

// advance consome e devolve o token corrente; no EOF é no-op.
func (p *parser) advance() token.Token {
	t := p.cur()
	if t.Kind != token.EOF {
		p.pos++
	}
	return t
}

// accept consome o token corrente se for do tipo k e reporta se consumiu.
func (p *parser) accept(k token.Kind) bool {
	if p.at(k) {
		p.advance()
		return true
	}
	return false
}

// expect consome o token esperado k, aplicando recuperação local quando ele não
// está presente (REQ-3.2/3.3, §design 3.5):
//
//   - presente: consome e devolve true;
//   - ausente, mas o próximo token casa: trata o corrente como ruído (deleção de
//     token único), consome ambos e devolve true;
//   - ausente sem correspondência adjacente: reporta o esperado, não consome
//     (inserção virtual) e devolve false.
func (p *parser) expect(k token.Kind) bool {
	if p.at(k) {
		p.advance()
		return true
	}
	if p.peek().Kind == k {
		p.errorf(p.cur().Pos, "token inesperado %s; esperava %s", p.cur().Kind, k)
		p.advance() // descarta o ruído
		p.advance() // consome o token esperado
		return true
	}
	p.errorf(p.cur().Pos, "esperava %s, encontrei %s", k, p.cur().Kind)
	return false
}

// synchronize descarta tokens em modo de pânico até encontrar um token de
// sincronização de stop, um '}' de fechamento, ou o EOF — nenhum dos quais é
// consumido, para que o nível de cima feche o próprio bloco e o pânico nunca
// fure para fora da estrutura corrente (REQ-3.4, §design 3.5).
func (p *parser) synchronize(stop stopSet) {
	for !p.atEnd() {
		k := p.cur().Kind
		if k == token.RBRACE || stop.contains(k) {
			return
		}
		p.advance()
	}
}

// errorf registra um erro de sintaxe localizado no DiagnosticBag. A supressão
// anti-cascata (janela de silêncio) é adicionada na tarefa 3.3.
func (p *parser) errorf(pos token.Pos, format string, args ...any) {
	p.bag.Errorf(pos, format, args...)
}
