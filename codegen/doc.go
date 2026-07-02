// Package codegen é o gerador de Go do back-end do transpilador: consome um
// program.Program validado, um symbols.SymbolTable e um types.Model e produz um
// projeto Go completo, legível e determinístico (REQ-14).
//
// Só roda sobre um programa sem diagnósticos de erro — não re-lexa, não
// re-parseia e não revalida (§design codegen 1.1). O runtime de suporte
// vendorado (codegen/rtsrc), o emissor de Go formatado (codegen/emit) e o
// lowering de corpos executáveis (codegen/lower) vivem em subpacotes.
package codegen
