// Package diag define os diagnósticos do front-end: Severity, Diagnostic e o
// DiagnosticBag compartilhado por todas as fases (dedup exato, teto configurável,
// ordenação estável por posição e renderização).
//
// É o canal transversal do pipeline (REQ-6).
package diag
