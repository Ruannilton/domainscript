package diag

import (
	"fmt"
	"sort"
	"strings"

	"domainscript/token"
)

// Severity classifica um diagnóstico (REQ-6.2).
type Severity int

const (
	SeverityError Severity = iota
	SeverityWarning
)

// String devolve "error" ou "warning" para renderização (REQ-6.6).
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarning:
		return "warning"
	default:
		return fmt.Sprintf("Severity(%d)", int(s))
	}
}

// Code é o código estável de um diagnóstico (ex.: "E001", "W014"), reservado
// para tooling. Ainda não preenchido nesta fase (§design 3.4).
type Code string

// Diagnostic é uma mensagem localizada e acionável emitida por qualquer fase.
type Diagnostic struct {
	Severity Severity
	Pos      token.Pos
	Msg      string
	Code     Code // reservado; vazio por enquanto
}

// String renderiza o diagnóstico como "linha:coluna: severidade: mensagem".
func (d Diagnostic) String() string {
	return fmt.Sprintf("%d:%d: %s: %s", d.Pos.Line, d.Pos.Col, d.Severity, d.Msg)
}

// DefaultMaxErrors é o teto padrão de erros antes da supressão (REQ-6.5).
const DefaultMaxErrors = 100

// DiagnosticBag acumula diagnósticos de todas as fases numa coleção única, com
// deduplicação exata, teto de erros e ordenação estável por posição na saída
// (REQ-6.1/6.3/6.4/6.5). Não é seguro para uso concorrente.
type DiagnosticBag struct {
	items     []Diagnostic
	seen      map[string]bool
	maxErrors int
	errors    int
	truncated bool
}

// New cria um bag com o teto de erros padrão.
func New() *DiagnosticBag { return NewWithMax(DefaultMaxErrors) }

// NewWithMax cria um bag com teto de erros configurável.
func NewWithMax(maxErrors int) *DiagnosticBag {
	return &DiagnosticBag{seen: make(map[string]bool), maxErrors: maxErrors}
}

// Add insere d, ignorando duplicatas exatas (mesma posição, severidade e
// mensagem). Atingido o teto de erros, para de coletar e marca o bag como
// truncado; a sentinela de supressão aparece em Render (REQ-6.4/6.5).
func (b *DiagnosticBag) Add(d Diagnostic) {
	if b.truncated {
		return
	}
	key := fmt.Sprintf("%d:%d|%d|%s", d.Pos.Line, d.Pos.Col, d.Severity, d.Msg)
	if b.seen[key] {
		return
	}
	if d.Severity == SeverityError && b.errors >= b.maxErrors {
		b.truncated = true
		return
	}
	b.seen[key] = true
	b.items = append(b.items, d)
	if d.Severity == SeverityError {
		b.errors++
	}
}

// Errorf adiciona um erro formatado na posição dada.
func (b *DiagnosticBag) Errorf(pos token.Pos, format string, args ...any) {
	b.Add(Diagnostic{Severity: SeverityError, Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// Warningf adiciona um aviso formatado na posição dada.
func (b *DiagnosticBag) Warningf(pos token.Pos, format string, args ...any) {
	b.Add(Diagnostic{Severity: SeverityWarning, Pos: pos, Msg: fmt.Sprintf(format, args...)})
}

// HasErrors reporta se há ao menos um erro (ou se a coleta foi truncada),
// sinalizando falha para CLI/API (REQ-6.7).
func (b *DiagnosticBag) HasErrors() bool { return b.errors > 0 || b.truncated }

// Truncated reporta se o teto de erros foi atingido e a coleta interrompida.
func (b *DiagnosticBag) Truncated() bool { return b.truncated }

// Len é o número de diagnósticos armazenados (sem a sentinela de truncamento).
func (b *DiagnosticBag) Len() int { return len(b.items) }

// All devolve uma cópia dos diagnósticos ordenada de forma estável por posição
// (linha, depois coluna), garantindo determinismo (REQ-6.3, NFR-3).
func (b *DiagnosticBag) All() []Diagnostic {
	out := make([]Diagnostic, len(b.items))
	copy(out, b.items)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Pos.Less(out[j].Pos)
	})
	return out
}

// Render produz o relatório completo, uma linha por diagnóstico em ordem de
// posição, seguido da sentinela de supressão quando truncado (REQ-6.5/6.6).
func (b *DiagnosticBag) Render() string {
	var sb strings.Builder
	for i, d := range b.All() {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(d.String())
	}
	if b.truncated {
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "error: coleta interrompida após %d erros; demais diagnósticos suprimidos", b.maxErrors)
	}
	return sb.String()
}
