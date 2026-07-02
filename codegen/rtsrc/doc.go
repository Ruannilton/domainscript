// Package rtsrc embute o código-fonte do runtime de suporte vendorado (event
// store, repositório de aggregate, dispatcher de eventos, unit of work,
// idempotency store, …) como arquivos .go.txt via //go:embed — o sufixo evita
// que compilem junto do compilador (REQ-16, §design codegen 2).
package rtsrc
